// regex-svc is the system under test for the cpu-regex scenario. It reproduces a
// ReDoS (regular-expression denial of service) outage in the style of the
// Cloudflare 2019 and Stack Overflow 2016 incidents.
//
// A "WAF rule" matches request input against a catastrophically-backtracking
// regular expression (^(a+)+$). Go's standard library regexp is RE2-based and
// immune to backtracking, so this uses dlclark/regexp2, a real backtracking
// engine. A crafted input (many 'a's followed by a non-matching char) forces
// exponential backtracking that pins a CPU for seconds per request.
//
// Requests are served from a fixed worker pool (like a real server's threads).
// A handful of malicious requests hold every worker for seconds, so ordinary
// requests — including the health check — cannot get a worker and time out. That
// is the outage: the process is "up" but unresponsive.
//
// The fix baked into the environment: WAF_RULE_DISABLED=1 skips the rule (the
// analog of rolling back the bad WAF rule / using the kill switch), which frees
// the workers and restores service.
//
//	MODE=serve (default) — the vulnerable HTTP service.
//	MODE=load            — sends the malicious payload to keep the pool saturated.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/dlclark/regexp2"
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

func runServe() {
	addr := ":" + getenv("PORT", "8080")
	workers := getenvInt("WORKERS", 4)
	ruleDisabled := getenv("WAF_RULE_DISABLED", "") == "1"

	// The catastrophically-backtracking "WAF rule". No match timeout — that
	// missing complexity limit is the vulnerability.
	rule := regexp2.MustCompile(`^(a+)+$`, regexp2.None)

	// pool is the fixed worker pool: a request must hold a token to be served,
	// so slow requests starve everyone else — including the health check.
	pool := make(chan struct{}, workers)
	var inflight int64

	acquire := func() { pool <- struct{}{}; atomic.AddInt64(&inflight, 1) }
	release := func() { atomic.AddInt64(&inflight, -1); <-pool }

	mux := http.NewServeMux()

	// /healthz goes through the same worker pool, so when the pool is saturated
	// by malicious /check requests, the health probe cannot get a worker and the
	// caller times out — the outage signal.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		acquire()
		defer release()
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})

	// /check applies the WAF rule to the q parameter.
	mux.HandleFunc("/check", func(w http.ResponseWriter, r *http.Request) {
		acquire()
		defer release()
		q := r.URL.Query().Get("q")
		if ruleDisabled {
			fmt.Fprintf(w, `{"allowed":true,"rule":"disabled"}`+"\n")
			return
		}
		start := time.Now()
		matched, _ := rule.MatchString(q) // blocks under catastrophic backtracking
		elapsed := time.Since(start)
		if elapsed > time.Second {
			log.Printf("WARN: WAF rule evaluation took %s for q (len=%d) — possible catastrophic backtracking", elapsed.Round(time.Millisecond), len(q))
		}
		fmt.Fprintf(w, `{"allowed":%t,"rule":"enabled","eval_ms":%d}`+"\n", !matched, elapsed.Milliseconds())
	})

	// /debug exposes the rule state and current load for an investigating SRE.
	mux.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "waf_rule_enabled=%t pattern=%q workers=%d inflight=%d\n",
			!ruleDisabled, `^(a+)+$`, workers, atomic.LoadInt64(&inflight))
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		state := "ENABLED (vulnerable)"
		if ruleDisabled {
			state = "disabled"
		}
		log.Printf("regex-svc serving on %s, WAF rule %s, workers=%d", addr, state, workers)
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
	target := getenv("TARGET_URL", "http://regex-svc:8080/check")
	concurrency := getenvInt("CONCURRENCY", 6)
	payloadLen := getenvInt("PAYLOAD_LEN", 34)
	// Many 'a's plus a non-matching char forces the ^(a+)+$ rule to explore
	// every partition of the run before failing — exponential backtracking.
	payload := strings.Repeat("a", payloadLen) + "!"
	attack := target + "?q=" + url.QueryEscape(payload)

	log.Printf("load: %d workers hammering %s (payload len %d)", concurrency, target, payloadLen)
	client := &http.Client{Timeout: 60 * time.Second}
	for i := 0; i < concurrency; i++ {
		go func() {
			for {
				resp, err := client.Get(attack)
				if err == nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}
			}
		}()
	}
	select {}
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
