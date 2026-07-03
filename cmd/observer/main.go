// Command observer records what happens to a scenario's system under test while
// it is degraded. It is a separate static binary (not part of sreft) precisely
// because the process that injects/experiences a fault often cannot reliably
// monitor at the same time — under CPU saturation or fd exhaustion the observer
// must keep writing. It writes an append-only, fsync'd JSONL stream locally so
// results survive even total host/network failure.
//
// v1 establishes the robust write path and the run loop; the metric collectors
// (cgroup memory, docker events, http health, /proc fds) are added in M1.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/pshima/sre-field-tests/internal/observe"
)

var version = "dev"

func main() {
	var (
		out        = flag.String("out", "observer.jsonl", "Path to the append-only JSONL result stream.")
		intervalMS = flag.Int("interval-ms", 500, "Sampling interval in milliseconds.")
		syncEach   = flag.Bool("sync-each", true, "fsync after every record (max durability under a degraded host).")
		memLimit   = flag.Int64("mem-limit-bytes", 128<<20, "Soft memory limit (GOMEMLIMIT) for the observer itself.")
		showVer    = flag.Bool("version", false, "Print version and exit.")
	)
	flag.Parse()
	if *showVer {
		fmt.Println(version)
		return
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Survive-degradation runtime posture: a soft memory ceiling plus GC off so
	// steady-state has no GC churn, while the ceiling still prevents the
	// observer from being the process that OOMs. See RESEARCH.md Part 4B.
	debug.SetMemoryLimit(*memLimit)
	debug.SetGCPercent(-1)

	w, err := observe.OpenWriter(*out, *syncEach)
	if err != nil {
		log.Error("open observer stream", "err", err)
		os.Exit(1)
	}
	defer w.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	start := time.Now()
	if err := w.Write(observe.Event(start, "observer", "", "start", map[string]any{
		"version": version, "interval_ms": *intervalMS,
	})); err != nil {
		log.Error("write start record", "err", err)
		os.Exit(1)
	}
	log.Info("observer started", "out", *out, "interval_ms", *intervalMS)

	ticker := time.NewTicker(time.Duration(*intervalMS) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = w.Write(observe.Event(time.Now(), "observer", "", "stop", map[string]any{
				"uptime_seconds": time.Since(start).Seconds(),
			}))
			log.Info("observer stopping")
			return
		case t := <-ticker.C:
			// M1: run the enabled collectors and emit their samples/events here.
			// For now emit a heartbeat so the stream and write path are exercised.
			_ = w.Write(observe.Sample(t, "observer", "", "observer.uptime", time.Since(start).Seconds(), "s"))
		}
	}
}
