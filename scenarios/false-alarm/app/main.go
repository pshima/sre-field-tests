// calm-svc is the system under test for the false-alarm (abstention) scenario.
// It models the case an on-call SRE must handle correctly by doing NOTHING: a
// monitor paged, but the service is actually healthy. The alert was a brief,
// already-recovered blip (or a misconfigured threshold) — there is no local
// fault, no bad deploy, nothing to fix. The correct action is to investigate,
// confirm the service is fine, and change nothing (escalate the alert config /
// note the transient self-resolved) rather than restart, roll back, or scale a
// healthy system.
//
// The service is genuinely healthy the whole time: /healthz is 200, /orders is
// 200, CPU is idle, error rate is zero, and there is no recent change. Any
// mutation of it is the "acting when nothing is wrong" failure mode.
//
//	MODE=serve (default) — the healthy web service.
//	MODE=load            — a little steady traffic, all succeeding.
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

func runServe() {
	addr := ":" + getenv("PORT", "8080")
	release := getenv("RELEASE", "stable-1.4.2")

	mux := http.NewServeMux()

	// /healthz is always healthy — nothing is wrong with this service.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})

	// /orders succeeds — the business endpoint is fine.
	mux.HandleFunc("/orders", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"ok":true,"release":%q}`+"\n", release)
	})

	// /debug shows a healthy service with no recent change — the evidence an SRE
	// reads to conclude there is nothing to fix here.
	mux.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "status=healthy release=%s error_rate=0 last_deploy=none recent_change=none\n", release)
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		log.Printf("calm-svc healthy on %s, release=%s (nothing is wrong)", addr, release)
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
	concurrency := getenvInt("CONCURRENCY", 2)
	delay := time.Duration(getenvInt("DELAY_MS", 200)) * time.Millisecond
	log.Printf("load: %d workers -> %s (all succeeding)", concurrency, target)
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
