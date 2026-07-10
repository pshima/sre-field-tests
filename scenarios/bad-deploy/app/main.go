// deploy-svc is the system under test for the bad-deploy scenario. It models the
// most common outage class of all: a change to a live system. A new release
// (RELEASE=v2) carries a regression — its health check fails (503) and its main
// endpoint errors (500) — so the deploy is "out" but broken. The process stays
// up (it is not a crash loop), which is exactly what makes a bad deploy insidious.
//
// The remedy is the on-call reflex a resource fix can't substitute for: roll
// back. Setting RELEASE=v1 (the previous good release) and redeploying restores
// service. Restarting or scaling the broken release does not help.
//
//	MODE=serve (default) — the web service; RELEASE selects the release.
//	MODE=load            — drives /orders so the regression shows under traffic.
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
	"sync/atomic"
	"syscall"
	"time"
)

const goodRelease = "v1"

func main() {
	switch getenv("MODE", "serve") {
	case "load":
		runLoad()
	default:
		runServe()
	}
}

func runServe() {
	addr := ":" + getenv("PORT", "8080")
	release := getenv("RELEASE", goodRelease)
	broken := release == "v2" // the regression shipped in v2

	var errCount int64

	mux := http.NewServeMux()

	// /healthz is the readiness signal. The v2 regression fails it, so the
	// broken release is "up" but never becomes healthy.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if broken {
			http.Error(w, "unhealthy: readiness check failing", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})

	// /orders is the business endpoint. Under the regression it errors.
	mux.HandleFunc("/orders", func(w http.ResponseWriter, r *http.Request) {
		if broken {
			atomic.AddInt64(&errCount, 1)
			http.Error(w, "internal error processing order", http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, `{"ok":true,"release":%q}`+"\n", release)
	})

	// /debug exposes the release state an SRE would check — including the
	// previous release a rollback would target.
	mux.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		status := "healthy"
		if broken {
			status = "degraded"
		}
		fmt.Fprintf(w, "release=%s status=%s previous_release=%s order_errors=%d\n",
			release, status, goodRelease, atomic.LoadInt64(&errCount))
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		log.Printf("deploy-svc starting: RELEASE=%s listening on %s", release, addr)
		if broken {
			log.Printf("ERROR: RELEASE=%s failing readiness and returning 500s on /orders "+
				"(regression introduced in this release; previous good release was %s)", release, goodRelease)
		}
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

func runLoad() {
	target := getenv("TARGET_URL", "http://web:8080/orders")
	concurrency := getenvInt("CONCURRENCY", 6)
	delay := time.Duration(getenvInt("DELAY_MS", 100)) * time.Millisecond
	log.Printf("load: %d workers -> %s", concurrency, target)
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
	select {}
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
