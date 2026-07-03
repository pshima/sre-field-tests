// Command observer records what happens to a scenario's system under test while
// it is degraded. It is a separate static binary (not part of sreft) precisely
// because the process that injects/experiences a fault often cannot reliably
// monitor at the same time. It writes an append-only, fsync'd JSONL stream
// locally so results survive even total host/network failure.
//
// For the tier-0 (local Docker) tier the observer runs on the host and reads the
// Docker Engine API over the unix socket (stdlib only) — memory vs limit, OOM
// kills, exit codes, restarts — plus an external HTTP health probe. Host/process
// collectors (/proc, eBPF) are added for tiers where the observer runs on the
// degraded host itself.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/pshima/sre-field-tests/internal/observe"
)

var version = "dev"

func main() {
	var (
		out        = flag.String("out", "observer.jsonl", "Path to the append-only JSONL result stream.")
		socket     = flag.String("socket", "/var/run/docker.sock", "Docker Engine unix socket.")
		intervalMS = flag.Int("interval-ms", 500, "Sampling interval in milliseconds.")
		collectors = flag.String("collectors", "cgroup-mem,docker-events,http-health", "Comma-separated collector IDs to enable.")
		containers = flag.String("containers", "", "Comma-separated logical=container pairs to watch (e.g. orders=sreft-orders).")
		health     = flag.String("health", "", "Comma-separated logical=URL pairs to probe (e.g. orders=http://localhost:8080/healthz).")
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

	cfg := observe.Config{
		Socket:     *socket,
		IntervalMS: *intervalMS,
		Collectors: splitCSV(*collectors),
		Containers: parsePairs(*containers),
		HealthURLs: parsePairs(*health),
	}

	w, err := observe.OpenWriter(*out, *syncEach)
	if err != nil {
		log.Error("open observer stream", "err", err)
		os.Exit(1)
	}
	defer w.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	start := time.Now()
	_ = w.Write(observe.Event(start, "observer", "", "start", map[string]any{
		"version": version, "interval_ms": *intervalMS, "collectors": cfg.Collectors,
	}))
	log.Info("observer started", "out", *out, "collectors", cfg.Collectors,
		"containers", cfg.Containers, "health", cfg.HealthURLs)

	observe.Run(ctx, cfg, w, log)

	_ = w.Write(observe.Event(time.Now(), "observer", "", "stop", map[string]any{
		"uptime_seconds": time.Since(start).Seconds(),
	}))
	log.Info("observer stopped", "uptime_s", time.Since(start).Seconds())
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parsePairs parses "k1=v1,k2=v2" into a map. Malformed entries are skipped.
func parsePairs(s string) map[string]string {
	m := map[string]string{}
	for _, p := range splitCSV(s) {
		if k, v, ok := strings.Cut(p, "="); ok {
			if k = strings.TrimSpace(k); k != "" {
				m[k] = strings.TrimSpace(v)
			}
		}
	}
	return m
}
