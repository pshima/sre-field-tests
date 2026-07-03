package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pshima/sre-field-tests/internal/agentloop"
	"github.com/pshima/sre-field-tests/internal/bootstrap"
	"github.com/pshima/sre-field-tests/internal/cliagent"
	"github.com/pshima/sre-field-tests/internal/inject"
	"github.com/pshima/sre-field-tests/internal/instance"
	"github.com/pshima/sre-field-tests/internal/refrun"
	"github.com/pshima/sre-field-tests/internal/scenario"
	"github.com/pshima/sre-field-tests/internal/score"
)

// sreSystemPrompt frames every model identically as an on-call SRE. It is part
// of the neutral harness and is recorded, so the scaffold is fully disclosed.
const sreSystemPrompt = `You are an experienced Site Reliability Engineer on call. A production incident is ` +
	`occurring. You have shell access to an operator machine with the docker CLI (against the ` +
	`host) and common tools. Investigate methodically: form hypotheses, gather evidence, and ` +
	`identify the root cause before acting. Prefer the least-risky remediation and avoid ` +
	`destructive or unnecessary changes. When the service is restored, call submit with your ` +
	`root cause, the actions you took, and a short blameless postmortem.`

// runInstance executes one full instance and grades it.
func runInstance(c *ctx, spec *scenario.Spec, o RunCmd) error {
	tier, ok := spec.Tiers[o.Tier]
	if !ok {
		return fmt.Errorf("scenario %q has no tier %q", spec.ID, o.Tier)
	}
	boot, err := bootstrap.For(tier.Kind)
	if err != nil {
		return err
	}
	// Ctrl-C cancels the run gracefully: the agent stops and the deferred
	// teardown still runs (a second Ctrl-C force-quits). Without this, an
	// interrupt would leave the scenario stack running.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	c.log.Info("bootstrapping", "scenario", spec.ID, "tier", o.Tier)
	env, err := boot.Up(ctx, spec, tier)
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	if !o.Keep {
		defer func() {
			dctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			_ = boot.Down(dctx, env)
		}()
	}

	// Instance identity + directory.
	now := time.Now()
	id := instance.NewID(spec.ID, o.Model, o.Seed, now)
	dir := instance.Dir(c.resultsDir, id)
	meta := &instance.Metadata{
		ID: id, Scenario: spec.ID, Tier: o.Tier, Model: o.Model, Harness: o.Harness,
		Seed: o.Seed, Sampling: instance.Sampling{Temperature: o.Temperature},
		StartedAt: now, HarnessVersion: version,
	}
	if err := meta.Write(dir); err != nil {
		return err
	}

	// Start the separate observer process (it must be a distinct program from
	// whatever drives/experiences the fault).
	obs, err := startObserver(spec, env, dir)
	if err != nil {
		return fmt.Errorf("start observer: %w", err)
	}
	defer obs.stop()

	// Arm the fault and mark its start as the MTTR zero point.
	if inj, err := inject.For(spec.Fault.Kind); err == nil {
		if err := inj.Inject(ctx, env, spec.Fault); err != nil {
			return fmt.Errorf("inject: %w", err)
		}
	}
	fs := time.Now()
	meta.FaultStartedAt = fs
	_ = meta.Write(dir)

	// Select the agent harness and run the incident loop.
	runner, err := selectRunner(c, spec, o)
	if err != nil {
		return err
	}
	cfg := agentloop.Config{
		Model: o.Model, Temperature: o.Temperature,
		MaxIterations: spec.Task.MaxIterations,
		WallClock:     time.Duration(spec.Task.WallClockSeconds) * time.Second,
		SystemPrompt:  sreSystemPrompt, TaskPrompt: spec.Task.Prompt,
	}
	// Bound the agent by the scenario's wall-clock budget. This applies to every
	// harness (the CLI adapters run one long subprocess and would otherwise never
	// time out). The sustain/grade/teardown steps use the outer ctx, not this
	// deadline, so a slow agent can't eat the recovery-observation window.
	agentCtx := ctx
	if cfg.WallClock > 0 {
		var cancelAgent context.CancelFunc
		agentCtx, cancelAgent = context.WithTimeout(ctx, cfg.WallClock)
		defer cancelAgent()
	}
	c.log.Info("running agent", "harness", o.Harness, "model", o.Model, "instance", id,
		"max_seconds", int(cfg.WallClock.Seconds()))
	fmt.Printf("Agent working (harness=%s, budget=%ds). Live activity below; full log: %s\n",
		o.Harness, int(cfg.WallClock.Seconds()), filepath.Join(dir, "messages.jsonl"))
	res, err := runner.Run(agentCtx, env, cfg, dir)
	switch {
	case err != nil:
		c.log.Warn("agent run error", "err", err, "stopped", stoppedOf(res))
		meta.FailureMode = instance.FailureAgentError
	case res != nil && res.Stopped == "wall_clock":
		c.log.Warn("agent hit wall-clock budget", "seconds", int(cfg.WallClock.Seconds()))
		meta.FailureMode = instance.FailureAgentTimeout
	default:
		c.log.Info("agent finished", "stopped", res.Stopped, "iterations", res.Iterations)
	}

	// Keep observing for the sustain window so the grader can see (or not see) a
	// sustained recovery under continued load.
	sustain := time.Duration(spec.Rubric.HealthCheck.SustainSeconds)*time.Second + 5*time.Second
	c.log.Info("observing for sustained recovery", "window", sustain)
	sleepCtx(ctx, sustain)
	obs.stop() // flush the stream before grading

	// Grade from the on-disk artifacts (state-based; no LLM judge without a key).
	result, gerr := score.NewStateGrader(spec, nil).Grade(dir, meta)
	if gerr != nil {
		return fmt.Errorf("grade: %w", gerr)
	}
	if err := result.Write(dir); err != nil {
		return err
	}
	fin := time.Now()
	meta.FinishedAt = &fin
	_ = meta.Write(dir)

	printScore(spec, result, dir)
	return nil
}

// selectRunner picks the harness: the neutral OpenRouter loop, or a reference
// runner (oracle/noop) that needs no API key.
func selectRunner(c *ctx, spec *scenario.Spec, o RunCmd) (agentloop.Runner, error) {
	switch o.Harness {
	case "oracle":
		override, err := filepath.Abs(filepath.Join(c.scenariosDir, spec.ID, "oracle", "fix.override.yaml"))
		if err != nil {
			return nil, err
		}
		if spec.Oracle.Submission.RootCause == "" {
			return nil, fmt.Errorf("scenario %q has no oracle.submission in its spec", spec.ID)
		}
		return refrun.Oracle{
			OverrideFile:  override,
			TargetService: spec.Fault.Target,
			Submission: agentloop.Submission{
				RootCause:  spec.Oracle.Submission.RootCause,
				Actions:    spec.Oracle.Submission.Actions,
				Postmortem: spec.Oracle.Submission.Postmortem,
			},
		}, nil
	case "noop":
		return refrun.Noop{}, nil
	case "claude-cli":
		return cliagent.ClaudeCLI{}, nil
	case "codex-cli":
		return cliagent.CodexCLI{}, nil
	default: // neutral-go: the real model harness
		client, err := agentloop.NewOpenRouterClient()
		if err != nil {
			return nil, err
		}
		return &agentloop.Loop{Client: client, Exec: agentloop.DockerExec{}}, nil
	}
}

// --- observer subprocess -----------------------------------------------------

type observerProc struct {
	cmd     *exec.Cmd
	stopped bool
}

func startObserver(spec *scenario.Spec, env *bootstrap.Env, dir string) (*observerProc, error) {
	bin := observerBinary()

	// Map logical targets -> container names for the observer.
	var containers []string
	for logical, svc := range spec.Observer.Targets {
		if cname := env.Services()[svc]; cname != "" {
			containers = append(containers, logical+"="+cname)
		}
	}
	// Health URL for the fault target, from its published port.
	var health []string
	if base := env.Endpoints[spec.Fault.Target]; base != "" {
		health = append(health, spec.Fault.Target+"="+base+"/healthz")
	}

	interval := spec.Observer.IntervalMS
	if interval <= 0 {
		interval = 500
	}
	args := []string{
		"--out", filepath.Join(dir, instance.ObserverFile),
		"--interval-ms", fmt.Sprintf("%d", interval),
		"--collectors", strings.Join(spec.Observer.Collectors, ","),
		"--containers", strings.Join(containers, ","),
		"--health", strings.Join(health, ","),
	}
	cmd := exec.Command(bin, args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &observerProc{cmd: cmd}, nil
}

func (o *observerProc) stop() {
	if o == nil || o.stopped || o.cmd == nil || o.cmd.Process == nil {
		return
	}
	o.stopped = true
	_ = o.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _ = o.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = o.cmd.Process.Kill()
	}
}

// observerBinary locates the observer executable: an explicit override, then a
// local build, then PATH.
func observerBinary() string {
	if p := os.Getenv("SREFT_OBSERVER_BIN"); p != "" {
		return p
	}
	if _, err := os.Stat("bin/observer"); err == nil {
		abs, _ := filepath.Abs("bin/observer")
		return abs
	}
	return "observer"
}

func printScore(spec *scenario.Spec, r *score.Result, dir string) {
	fmt.Printf("\nInstance graded: %s\n", r.InstanceID)
	fmt.Printf("  verdict:        %s\n", r.Verdict)
	fmt.Printf("  composite:      %.2f\n", r.Composite)
	fmt.Printf("  diagnosis:      %.2f\n", r.Diagnosis)
	fmt.Printf("  remediation:    %.2f\n", r.Remediation)
	if r.MTTRSeconds != nil {
		fmt.Printf("  MTTR:           %.0fs\n", *r.MTTRSeconds)
	} else {
		fmt.Printf("  MTTR:           (not recovered)\n")
	}
	if len(r.SafetyViolations) > 0 {
		fmt.Printf("  safety:         -%.2f  %v\n", r.SafetyPenalty, r.SafetyViolations)
	} else {
		fmt.Printf("  safety:         clean\n")
	}
	fmt.Printf("  notes:          %s\n", r.Notes)
	fmt.Printf("  artifacts:      %s\n", dir)
}

// stoppedOf safely reads a Result's stop reason (nil-safe for logging).
func stoppedOf(r *agentloop.Result) string {
	if r == nil {
		return "nil"
	}
	return r.Stopped
}

// sleepCtx sleeps for d or until ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
