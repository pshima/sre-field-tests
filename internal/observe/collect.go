package observe

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Config drives an observer run: which collectors to enable, the sampling
// interval, the containers to watch (logical name -> container name), and the
// host-reachable health URLs to probe (logical name -> URL).
type Config struct {
	Socket     string
	IntervalMS int
	Collectors []string
	Containers map[string]string
	HealthURLs map[string]string
}

// Run starts every enabled collector, fanning their records into w until ctx is
// cancelled. Collectors run concurrently and independently; one collector
// erroring does not stop the others (the whole point of a resilient observer).
func Run(ctx context.Context, cfg Config, w *Writer, log *slog.Logger) {
	client := newEngineClient(cfg.Socket)
	interval := time.Duration(cfg.IntervalMS) * time.Millisecond
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}

	// emit is the single write path; a per-record failure is logged but never
	// aborts collection.
	var wmu sync.Mutex
	emit := func(r Record) {
		wmu.Lock()
		defer wmu.Unlock()
		if err := w.Write(r); err != nil {
			log.Warn("observer write failed", "err", err)
		}
	}

	var wg sync.WaitGroup
	launch := func(fn func()) { wg.Add(1); go func() { defer wg.Done(); fn() }() }

	for _, id := range cfg.Collectors {
		switch id {
		case "cgroup-mem":
			launch(func() { collectContainerState(ctx, client, cfg.Containers, interval, emit, log) })
		case "docker-events":
			launch(func() { collectDockerEvents(ctx, client, cfg.Containers, emit, log) })
		case "http-health":
			launch(func() { collectHTTPHealth(ctx, cfg.HealthURLs, interval, emit) })
		case "proc-fd":
			// Per-process fd counts need a shell/proc in the target; the tier-0
			// distroless SUT has neither. Documented best-effort: skip loudly
			// rather than emit misleading zeros.
			log.Warn("collector not supported on tier0-docker distroless target; skipping", "collector", id)
		default:
			log.Warn("unknown collector; skipping", "collector", id)
		}
	}
	wg.Wait()
}

// collectContainerState polls inspect + one-shot stats for each target, emitting
// memory usage/limit, container-level health, and restart count. Restart count
// as a monotonic sample lets the grader diff it to count OOM-restart cycles even
// if an event is missed.
func collectContainerState(ctx context.Context, c *engineClient, targets map[string]string, interval time.Duration, emit func(Record), log *slog.Logger) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			for logical, cname := range targets {
				cs, err := c.inspect(ctx, cname)
				if err != nil {
					// Container may be mid-restart or gone; record it as down.
					emit(Sample(now, "cgroup-mem", logical, MetricHealthUp, 0, "1"))
					continue
				}
				up := 0.0
				if cs.State.Running {
					up = 1
				}
				emit(Sample(now, "cgroup-mem", logical, MetricHealthUp, up, "1"))
				emit(Sample(now, "cgroup-mem", logical, MetricRestartCount, float64(cs.RestartCount), "1"))
				if usage, limit, err := c.statsOneShot(ctx, cname); err == nil && limit > 0 {
					emit(Sample(now, "cgroup-mem", logical, MetricMemoryUsage, float64(usage), "By"))
					emit(Sample(now, "cgroup-mem", logical, MetricMemoryLimit, float64(limit), "By"))
				}
			}
		}
	}
}

// collectDockerEvents streams container events and emits discrete OOM/exit/
// restart records with the ordering the grader relies on.
func collectDockerEvents(ctx context.Context, c *engineClient, targets map[string]string, emit func(Record), log *slog.Logger) {
	// Build the container-name filter and a reverse map to logical names.
	names := make([]string, 0, len(targets))
	byName := make(map[string]string, len(targets))
	for logical, cname := range targets {
		names = append(names, cname)
		byName[cname] = logical
	}
	handle := func(ev engineEvent) {
		if ev.Type != "container" {
			return
		}
		logical := byName[ev.Actor.Attributes["name"]]
		ts := time.Now()
		switch {
		case ev.Action == "oom":
			emit(Event(ts, "docker-events", logical, EventOOMKill, nil))
		case ev.Action == "die":
			attrs := map[string]any{}
			if ec, err := strconv.Atoi(ev.Actor.Attributes["exitCode"]); err == nil {
				attrs["exit_code"] = ec
			}
			emit(Event(ts, "docker-events", logical, EventContainerExit, attrs))
		case ev.Action == "start", ev.Action == "restart":
			emit(Event(ts, "docker-events", logical, EventContainerRestart, nil))
		}
	}
	// Reconnect on stream errors until the context is cancelled.
	for ctx.Err() == nil {
		if err := c.streamEvents(ctx, names, handle); err != nil && ctx.Err() == nil {
			log.Warn("docker events stream ended; retrying", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}

// collectHTTPHealth probes each health URL from the observer's vantage point,
// emitting up/down and request latency. This is the external "is the service
// actually serving?" signal, complementary to the container-level health.
func collectHTTPHealth(ctx context.Context, urls map[string]string, interval time.Duration, emit func(Record)) {
	client := &http.Client{Timeout: 2 * time.Second}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			for logical, u := range urls {
				start := time.Now()
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
				resp, err := client.Do(req)
				dur := time.Since(start).Seconds() * 1000
				up := 0.0
				if err == nil {
					if resp.StatusCode < 500 {
						up = 1
					}
					_ = resp.Body.Close()
					emit(Sample(now, "http-health", logical, MetricHTTPDurationMS, dur, "ms"))
				}
				emit(Sample(now, "http-health", logical, MetricHealthUp, up, "1"))
			}
		}
	}
}
