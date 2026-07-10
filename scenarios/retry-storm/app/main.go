// retry-svc is the system under test for the retry-storm scenario. It models the
// most important modern outage shape: a metastable overload driven by retry
// amplification against a degraded dependency.
//
// The web service (MODE=serve) handles requests from a FIXED worker pool. Every
// /order calls a downstream dependency (`pricing`, MODE=dep) that is *degraded*
// — slow, and failing — but NOT down. With no timeout and MAX_RETRIES retries
// and no backoff, each /order attempt waits out the slow dependency and retries,
// holding one worker for many seconds. Under load beyond the worker count, every
// worker is held in a retry loop, so /healthz — which just needs to acquire a
// worker slot — cannot and times out. The process is up, its CPU is near idle
// (the work is I/O wait, not compute), and the dependency is only slow: that is
// the diagnostic trap. web looks broken, but web's code is fine and `pricing` is
// merely degraded — the fault is retry amplification saturating web's own pool.
//
// The counterintuitive part an SRE must grasp: scaling web UP makes it WORSE
// (more concurrent retriers hammering the already-degraded dependency), and
// restarting web does nothing (the storm resumes within seconds).
//
// The fix baked into the environment: RETRY_STORM_DISABLED=1 caps retries to 0
// AND applies a tight per-attempt client timeout, so each /order fails fast and
// releases its worker immediately. web then sheds load and stays healthy under a
// degraded dependency (graceful degradation) — the real remedy is to bound the
// dependency call (timeout + retry budget / circuit breaker), not to add
// capacity.
//
//	MODE=serve (default) — the web service; calls the dependency per /order.
//	MODE=dep             — the degraded downstream dependency (slow + failing).
//	MODE=load            — drives /order traffic beyond the worker pool size.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func main() {
	switch getenv("MODE", "serve") {
	case "load":
		runLoad()
	case "dep":
		runDep()
	default:
		runServe()
	}
}

// ---- serve role (the web SUT) -----------------------------------------------

func runServe() {
	addr := ":" + getenv("PORT", "8080")
	depURL := getenv("DEP_URL", "http://pricing:8080/price")
	workers := getenvInt("WORKERS", 8)
	maxRetries := getenvInt("MAX_RETRIES", 4)
	// Per-attempt client timeout for the dependency call.
	depTimeout := time.Duration(getenvInt("DEP_TIMEOUT_MS", 5000)) * time.Millisecond

	// The fix: cap retries and tighten the timeout so a slow dependency can't
	// hold a worker. A single flag flips both — the retry budget and the bound.
	if getenv("RETRY_STORM_DISABLED", "") == "1" {
		maxRetries = 0
		depTimeout = 400 * time.Millisecond
	}

	// The fixed worker pool: a semaphore of `workers` slots. This is what a real
	// thread/worker pool bounds, and what retry amplification exhausts.
	slots := make(chan struct{}, workers)

	// pool tracks how long each in-flight /order has held its worker, so readiness
	// can be derived deterministically from worker STALL rather than from winning a
	// contended slot (which would race against the load and flap).
	pool := newTracker()

	// A worker is "stalled" once it has held its slot longer than this. Under the
	// storm each /order waits out and retries the ~2s dependency for up to ~10s, so
	// workers stall almost immediately; once retries are bounded and the timeout is
	// tight, each /order completes in well under this, so nothing stalls.
	stall := time.Duration(getenvInt("STALL_MS", 1500)) * time.Millisecond

	// A dedicated client per handler call uses the per-attempt timeout.
	client := &http.Client{Timeout: depTimeout}

	var retriesTotal int64

	mux := http.NewServeMux()

	// /healthz is the readiness signal, and it does NOT call the dependency: web's
	// readiness must not depend on a third party being fast. Instead it reports the
	// pool wedged when EVERY worker is stalled on a slow call — the outage signal.
	// This is deterministic (no slot race): under the storm all workers are stuck
	// for ~10s; once the call is bounded, none stall.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		stalled := pool.stalledCount(stall)
		if stalled >= workers {
			http.Error(w, fmt.Sprintf("pool wedged: all %d workers stalled on the dependency", workers), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})

	// /order acquires a worker slot, then calls the dependency with retries. Under
	// the storm each call holds its slot for up to (maxRetries+1)*depTimeout while
	// it waits out and retries the slow dependency.
	mux.HandleFunc("/order", func(w http.ResponseWriter, r *http.Request) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		case <-time.After(2 * time.Second):
			// Shed rather than queue unboundedly — but under the storm the held
			// workers, not the shedding, are what breaks readiness.
			http.Error(w, "no worker available: pool saturated", http.StatusServiceUnavailable)
			return
		}
		id := pool.start()
		defer pool.done(id)

		attempts := 0
		for {
			attempts++
			ctx, cancel := context.WithTimeout(r.Context(), depTimeout)
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, depURL, nil)
			resp, err := client.Do(req)
			if err == nil {
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				cancel()
				if resp.StatusCode == http.StatusOK {
					fmt.Fprintf(w, `{"ok":true,"attempts":%d,"price":%s}`+"\n", attempts, string(body))
					return
				}
			} else {
				cancel()
			}
			if attempts > maxRetries {
				// Retry budget exhausted — fail fast so the worker is released.
				http.Error(w, fmt.Sprintf("dependency degraded after %d attempts", attempts), http.StatusBadGateway)
				return
			}
			atomic.AddInt64(&retriesTotal, 1) // no backoff: the amplifier
		}
	})

	// /debug exposes the saturation an investigating SRE reads: how many workers
	// are held (and how many are stalled), the retry configuration, and the
	// cumulative retry count that is the amplification signal.
	mux.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		mode := "STORM (unbounded retries, no tight timeout)"
		if maxRetries == 0 {
			mode = "bounded (retries capped, tight timeout)"
		}
		fmt.Fprintf(w, "mode=%s workers=%d inflight=%d stalled=%d max_retries=%d dep_timeout_ms=%d retries_total=%d dep_url=%s\n",
			mode, workers, pool.inflight(), pool.stalledCount(stall), maxRetries,
			depTimeout.Milliseconds(), atomic.LoadInt64(&retriesTotal), depURL)
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		state := fmt.Sprintf("STORM: max_retries=%d dep_timeout=%s (a slow dependency can hold a worker for up to %s)",
			maxRetries, depTimeout, time.Duration(maxRetries+1)*depTimeout)
		if maxRetries == 0 {
			state = fmt.Sprintf("bounded: retries capped, dep_timeout=%s (fail fast, shed load)", depTimeout)
		}
		log.Printf("retry-svc serving on %s, workers=%d, %s, dep=%s", addr, workers, state, depURL)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	awaitSignal(srv)
}

// ---- dep role (the degraded dependency) -------------------------------------

func runDep() {
	addr := ":" + getenv("PORT", "8080")
	// The dependency is degraded, not down: it responds slowly and (mostly)
	// fails. It is intrinsically degraded — you cannot "fix" a third party; you
	// bound your calls to it. There is no fix env on this service by design.
	latency := time.Duration(getenvInt("LATENCY_MS", 2000)) * time.Millisecond
	failEvery := getenvInt("FAIL_EVERY", 1) // 1 => every request fails (persistently degraded)
	var n int64

	mux := http.NewServeMux()
	// /price is the endpoint web depends on. It always takes `latency`, and fails
	// on the configured cadence — slow enough that web's default (no) timeout
	// waits it out, so an un-bounded caller retries.
	mux.HandleFunc("/price", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(latency):
		case <-r.Context().Done():
			return
		}
		i := atomic.AddInt64(&n, 1)
		if failEvery > 0 && i%int64(failEvery) == 0 {
			http.Error(w, "pricing degraded", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintf(w, `{"price":42}`)
	})
	// /healthz reports the dependency itself as up — it is degraded, not crashed.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		log.Printf("pricing (degraded dependency) serving on %s, latency=%s fail_every=%d", addr, latency, failEvery)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()
	awaitSignal(srv)
}

// ---- load role --------------------------------------------------------------

func runLoad() {
	target := getenv("TARGET_URL", "http://web:8080/order")
	concurrency := getenvInt("CONCURRENCY", 16)
	log.Printf("load: %d workers hammering %s", concurrency, target)
	// A generous client timeout so the load itself isn't what fails first; the
	// point is that web's own workers are saturated by their retry loops.
	client := &http.Client{Timeout: 60 * time.Second}
	for i := 0; i < concurrency; i++ {
		go func() {
			for {
				resp, err := client.Get(target)
				if err == nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}
			}
		}()
	}
	select {}
}

// ---- worker tracker ---------------------------------------------------------

// tracker records the start time of each in-flight /order so readiness can be
// derived from how long workers have been HELD, deterministically, rather than
// from a contended slot acquisition that would race against the load generator.
type tracker struct {
	mu      sync.Mutex
	next    uint64
	started map[uint64]time.Time
}

func newTracker() *tracker { return &tracker{started: make(map[uint64]time.Time)} }

// start records a newly-held worker and returns its id.
func (t *tracker) start() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	id := t.next
	t.next++
	t.started[id] = time.Now()
	return id
}

// done releases the worker with the given id.
func (t *tracker) done(id uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.started, id)
}

// inflight is the number of workers currently held.
func (t *tracker) inflight() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.started)
}

// stalledCount is how many held workers have been busy longer than d.
func (t *tracker) stalledCount(d time.Duration) int {
	cutoff := time.Now().Add(-d)
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, s := range t.started {
		if s.Before(cutoff) {
			n++
		}
	}
	return n
}

// ---- helpers ----------------------------------------------------------------

func awaitSignal(srv *http.Server) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
