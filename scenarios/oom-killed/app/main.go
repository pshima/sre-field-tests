// leaky-svc is the system under test for the oom-killed scenario.
//
// It plays two roles selected by MODE:
//
//	MODE=serve (default) — an HTTP "orders" service. Each /orders request
//	  appends a chunk of memory to an in-memory cache. If CACHE_MAX is unset (or
//	  0) the cache is UNBOUNDED — the leak — and under steady load the process
//	  RSS climbs until it hits the container's cgroup memory limit and the kernel
//	  OOM-kills it (exit 137). Setting CACHE_MAX to a positive number bounds the
//	  cache (LRU-ish eviction) and is the real fix the agent is meant to find.
//
//	MODE=load — a load generator that drives /orders on TARGET_URL with a fixed
//	  concurrency, so the leak actually manifests without any external tooling.
//
// The design goal is a crisp, deterministic failure edge: healthy at first,
// then repeating OOM kills under load, fully recovered once CACHE_MAX is set.
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
	"syscall"
	"time"
)

func main() {
	switch getenv("MODE", "serve") {
	case "load":
		runLoad()
	default:
		runServe()
	}
}

// ---- serve role -------------------------------------------------------------

// cache is the leak. Retained chunks keep RSS pinned; eviction (when bounded)
// lets it be reclaimed.
type cache struct {
	mu     sync.Mutex
	chunks [][]byte
	max    int // 0 = unbounded (the leak)
	served uint64
}

func (c *cache) add(chunk []byte) (retained, servedTotal int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chunks = append(c.chunks, chunk)
	if c.max > 0 && len(c.chunks) > c.max {
		// Drop oldest so the slice header no longer references them; this is
		// what makes CACHE_MAX a genuine fix rather than a slowdown.
		drop := len(c.chunks) - c.max
		c.chunks = append(c.chunks[:0], c.chunks[drop:]...)
	}
	c.served++
	return len(c.chunks), int(c.served)
}

func runServe() {
	addr := ":" + getenv("PORT", "8080")
	chunkBytes := getenvInt("CHUNK_BYTES", 1<<20) // 1 MiB per request by default
	c := &cache{max: getenvInt("CACHE_MAX", 0)}

	mux := http.NewServeMux()

	// /healthz reports process liveness. While the process is up it returns 200;
	// the incident manifests as the process being OOM-killed, so probes see
	// connection-refused / gaps during the kill+restart cycle rather than a 500.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})

	// /orders does "work" and leaks a chunk into the cache.
	mux.HandleFunc("/orders", func(w http.ResponseWriter, r *http.Request) {
		chunk := make([]byte, chunkBytes)
		// Touch every page so the allocation is resident (RSS), not lazy.
		for i := 0; i < len(chunk); i += 4096 {
			chunk[i] = byte(i)
		}
		retained, served := c.add(chunk)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"order_id":%d,"retained_chunks":%d,"cache_max":%d}`+"\n", served, retained, c.max)
	})

	// /debug exposes cache state for humans/agents inspecting the service.
	mux.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		retained, served := len(c.chunks), c.served
		c.mu.Unlock()
		fmt.Fprintf(w, "cache_max=%d retained_chunks=%d served=%d chunk_bytes=%d\n",
			c.max, retained, served, chunkBytes)
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		mode := "UNBOUNDED (leaking)"
		if c.max > 0 {
			mode = fmt.Sprintf("bounded at %d chunks", c.max)
		}
		log.Printf("leaky-svc serving on %s, cache %s, chunk=%d bytes", addr, mode, chunkBytes)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
}

// ---- load role --------------------------------------------------------------

func runLoad() {
	target := getenv("TARGET_URL", "http://orders:8080/orders")
	concurrency := getenvInt("CONCURRENCY", 8)
	// Small delay between requests per worker keeps the allocation rate high but
	// not instantaneous, so the climb-to-OOM is observable rather than a spike.
	delay := time.Duration(getenvInt("DELAY_MS", 50)) * time.Millisecond

	log.Printf("load: %d workers -> %s (delay %s)", concurrency, target, delay)
	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < concurrency; i++ {
		go func() {
			for {
				resp, err := client.Get(target)
				if err == nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}
				time.Sleep(delay)
			}
		}()
	}
	select {} // run until the container is stopped
}

// ---- helpers ----------------------------------------------------------------

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
