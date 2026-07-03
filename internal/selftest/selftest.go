// Package selftest verifies that a scenario matches its description: the fault
// actually manifests, an untouched system stays broken, and the oracle fix
// recovers it. This is the guard the user insisted on — "tests to ensure the
// scenario we start matches what we describe" — and it is also the grader's
// correctness gate (oracle => FULL, no-op => ZERO). It drives the same
// bootstrap/inject drivers a real run uses.
package selftest

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/pshima/sre-field-tests/internal/bootstrap"
	"github.com/pshima/sre-field-tests/internal/inject"
	"github.com/pshima/sre-field-tests/internal/observe"
	"github.com/pshima/sre-field-tests/internal/scenario"
)

// Check is one assertion in the self-test.
type Check struct {
	Name   string
	Pass   bool
	Detail string
}

// Report is the full self-test outcome.
type Report struct {
	Scenario string
	Checks   []Check
	Passed   bool
}

// Options tune the self-test timings and environment.
type Options struct {
	Socket         string
	ManifestWindow time.Duration // time to watch the fault manifest
	SustainWindow  time.Duration // time the oracle fix must hold healthy
	OracleOverride string        // absolute path to the oracle compose override
	KeepUp         bool          // leave the stack running afterward (debugging)
	Log            *slog.Logger
}

// Run executes the self-test for spec at the named tier.
func Run(ctx context.Context, spec *scenario.Spec, tierName string, opts Options) (*Report, error) {
	if opts.Log == nil {
		opts.Log = slog.New(slog.NewTextHandler(discard{}, nil))
	}
	if opts.ManifestWindow == 0 {
		opts.ManifestWindow = 25 * time.Second
	}
	if opts.SustainWindow == 0 {
		opts.SustainWindow = 20 * time.Second
	}
	if opts.Socket == "" {
		opts.Socket = "/var/run/docker.sock"
	}

	tier, ok := spec.Tiers[tierName]
	if !ok {
		return nil, fmt.Errorf("scenario %q has no tier %q", spec.ID, tierName)
	}
	boot, err := bootstrap.For(tier.Kind)
	if err != nil {
		return nil, err
	}

	opts.Log.Info("self-test: bootstrapping", "scenario", spec.ID, "tier", tierName)
	env, err := boot.Up(ctx, spec, tier)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	if !opts.KeepUp {
		defer func() {
			downCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			_ = boot.Down(downCtx, env)
		}()
	}

	// Arm the fault (honors any warm-up delay).
	if inj, err := inject.For(spec.Fault.Kind); err == nil {
		if err := inj.Inject(ctx, env, spec.Fault); err != nil {
			return nil, fmt.Errorf("inject: %w", err)
		}
	}

	target := env.Services()[spec.Fault.Target]
	if target == "" {
		return nil, fmt.Errorf("fault target service %q not found in environment", spec.Fault.Target)
	}
	rep := &Report{Scenario: spec.ID}

	// Check 1: the fault manifests — restart count climbs as the target is
	// repeatedly OOM-killed within the observation window.
	r0, _ := restartCount(ctx, opts.Socket, target)
	opts.Log.Info("self-test: watching for fault", "target", target, "window", opts.ManifestWindow)
	deltaMid := waitForRestartDelta(ctx, opts.Socket, target, r0, 2, opts.ManifestWindow)
	rMid, _ := restartCount(ctx, opts.Socket, target)
	rep.add("fault-manifests", deltaMid >= 2,
		fmt.Sprintf("restart count %d -> %d (delta %d, need >=2)", r0, rMid, rMid-r0))

	// Check 2: an untouched system stays broken — it keeps OOM-killing rather
	// than self-healing over a second window.
	half := opts.ManifestWindow / 2
	if half < 8*time.Second {
		half = 8 * time.Second
	}
	sleepCtx(ctx, half)
	rEnd, _ := restartCount(ctx, opts.Socket, target)
	rep.add("no-op-stays-broken", rEnd > rMid,
		fmt.Sprintf("restart count still climbing %d -> %d during no-op", rMid, rEnd))

	// Check 3: the oracle fix recovers it — apply the override, then require the
	// target to stay running with a frozen restart count and a healthy endpoint
	// throughout the sustain window (recovery under continued load).
	if opts.OracleOverride != "" {
		opts.Log.Info("self-test: applying oracle fix", "override", opts.OracleOverride)
		if out, err := env.ComposeExec(ctx, []string{opts.OracleOverride}, "up", "-d", spec.Fault.Target); err != nil {
			rep.add("oracle-recovers", false, fmt.Sprintf("apply oracle failed: %v: %s", err, out))
		} else {
			pass, detail := sustainHealthy(ctx, opts.Socket, target, healthURL(env, spec), opts.SustainWindow)
			rep.add("oracle-recovers", pass, detail)
		}
	}

	rep.Passed = true
	for _, c := range rep.Checks {
		if !c.Pass {
			rep.Passed = false
		}
	}
	return rep, nil
}

func (r *Report) add(name string, pass bool, detail string) {
	r.Checks = append(r.Checks, Check{Name: name, Pass: pass, Detail: detail})
}

// waitForRestartDelta polls until the restart count rises by at least want above
// base, or the window elapses; returns the observed delta.
func waitForRestartDelta(ctx context.Context, socket, name string, base, want int, window time.Duration) int {
	deadline := time.Now().Add(window)
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		cur, _ := restartCount(ctx, socket, name)
		if cur-base >= want {
			return cur - base
		}
		if time.Now().After(deadline) {
			return cur - base
		}
		select {
		case <-ctx.Done():
			return cur - base
		case <-t.C:
		}
	}
}

// sustainHealthy asserts the target stays running with a frozen restart count
// and a healthy endpoint for the whole window.
func sustainHealthy(ctx context.Context, socket, name, healthURL string, window time.Duration) (bool, string) {
	// Let the recreate settle before sampling the baseline restart count.
	sleepCtx(ctx, 3*time.Second)
	base, _ := restartCount(ctx, socket, name)
	deadline := time.Now().Add(window)
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for time.Now().Before(deadline) {
		st, err := observe.QueryContainer(ctx, socket, name)
		if err != nil || !st.Running {
			return false, fmt.Sprintf("target not running during sustain window (err=%v)", err)
		}
		if st.RestartCount > base {
			return false, fmt.Sprintf("target restarted during sustain window (%d -> %d)", base, st.RestartCount)
		}
		if healthURL != "" && !probeHealthy(ctx, healthURL) {
			return false, "health endpoint not healthy during sustain window"
		}
		select {
		case <-ctx.Done():
			return false, "cancelled"
		case <-t.C:
		}
	}
	return true, fmt.Sprintf("stayed running & healthy for %s, restart count frozen at %d", window, base)
}

func restartCount(ctx context.Context, socket, name string) (int, error) {
	st, err := observe.QueryContainer(ctx, socket, name)
	if err != nil {
		return 0, err
	}
	return st.RestartCount, nil
}

func healthURL(env *bootstrap.Env, spec *scenario.Spec) string {
	base := env.Endpoints[spec.Fault.Target]
	if base == "" {
		return ""
	}
	return base + "/healthz"
}

func probeHealthy(ctx context.Context, url string) bool {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 500
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// discard is an io.Writer that drops everything (default silent logger sink).
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
