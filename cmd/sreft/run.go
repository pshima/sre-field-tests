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

// runParams is the resolved configuration for one instance run.
type runParams struct {
	Model       string
	Harness     string
	Tier        string
	Seed        int
	Temperature float64
	Keep        bool
	ResultsDir  string // directory the instance's results dir is created under
}

// runOneInstance runs a single instance with its own signal-cancelled context
// (Ctrl-C stops the agent; deferred teardown still runs). Used by `sreft run`.
func runOneInstance(c *ctx, spec *scenario.Spec, p runParams) (*score.Result, string, error) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return runInstance(ctx, c, spec, p)
}

// runInstance executes one full instance and grades it, writing artifacts under
// p.ResultsDir/<instance-id>. The caller owns ctx so both `sreft run` (one
// instance) and `sreft bench` (a sweep with a single sweep-wide signal context)
// can drive it. It returns the grade and the instance directory. A pipeline
// error (bootstrap/inject/grade) is returned; an agent that errors or times out
// still yields a graded (usually NONE) result rather than an error.
func runInstance(ctx context.Context, c *ctx, spec *scenario.Spec, p runParams) (*score.Result, string, error) {
	tier, ok := spec.Tiers[p.Tier]
	if !ok {
		return nil, "", fmt.Errorf("scenario %q has no tier %q", spec.ID, p.Tier)
	}
	boot, err := bootstrap.For(tier.Kind)
	if err != nil {
		return nil, "", err
	}

	c.log.Info("bootstrapping", "scenario", spec.ID, "tier", p.Tier)
	env, err := boot.Up(ctx, spec, tier)
	if err != nil {
		return nil, "", fmt.Errorf("bootstrap: %w", err)
	}
	if !p.Keep {
		defer func() {
			dctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			_ = boot.Down(dctx, env)
		}()
	}

	// Instance identity + directory.
	now := time.Now()
	id := instance.NewID(spec.ID, p.Model, p.Seed, now)
	dir := instance.Dir(p.ResultsDir, id)
	meta := &instance.Metadata{
		ID: id, Scenario: spec.ID, Tier: p.Tier, Model: p.Model, Harness: p.Harness,
		Seed: p.Seed, Sampling: instance.Sampling{Temperature: p.Temperature},
		StartedAt: now, HarnessVersion: version,
	}
	if err := meta.Write(dir); err != nil {
		return nil, dir, err
	}

	// Start the separate observer process (it must be a distinct program from
	// whatever drives/experiences the fault).
	obs, err := startObserver(spec, env, dir)
	if err != nil {
		return nil, dir, fmt.Errorf("start observer: %w", err)
	}
	defer obs.stop()

	// Arm the fault and mark its start as the MTTR zero point.
	if inj, err := inject.For(spec.Fault.Kind); err == nil {
		if err := inj.Inject(ctx, env, spec.Fault); err != nil {
			return nil, dir, fmt.Errorf("inject: %w", err)
		}
	}
	fs := time.Now()
	meta.FaultStartedAt = fs
	_ = meta.Write(dir)

	// Select the agent harness and run the incident loop.
	runner, err := selectRunner(c, spec, p)
	if err != nil {
		return nil, dir, err
	}
	cfg := agentloop.Config{
		Model: p.Model, Temperature: p.Temperature,
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
	c.log.Info("running agent", "harness", p.Harness, "model", p.Model, "instance", id,
		"max_seconds", int(cfg.WallClock.Seconds()))
	fmt.Printf("Agent working (harness=%s, budget=%ds). Live activity below; full log: %s\n",
		p.Harness, int(cfg.WallClock.Seconds()), filepath.Join(dir, "messages.jsonl"))
	res, rerr := runner.Run(agentCtx, env, cfg, dir)
	switch {
	case rerr != nil:
		c.log.Warn("agent run error", "err", rerr, "stopped", stoppedOf(res))
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
		return nil, dir, fmt.Errorf("grade: %w", gerr)
	}
	if err := result.Write(dir); err != nil {
		return nil, dir, err
	}
	fin := time.Now()
	meta.FinishedAt = &fin
	_ = meta.Write(dir)
	return result, dir, nil
}

// selectRunner picks the harness: the neutral OpenRouter loop, or a reference
// runner (oracle/noop) that needs no API key.
func selectRunner(c *ctx, spec *scenario.Spec, p runParams) (agentloop.Runner, error) {
	switch p.Harness {
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
	case "always-restart":
		// The universal reflex: bounce the service. A weak, generic submission
		// (no real root cause) so diagnosis scores low — this is a policy, not
		// an analysis.
		return refrun.Restart{
			TargetService: spec.Fault.Target,
			Submission: agentloop.Submission{
				RootCause:  "The service was unhealthy, so I restarted it.",
				Actions:    "Restarted the affected service (docker compose restart).",
				Postmortem: "Bounced the service to clear the immediate symptom.",
			},
		}, nil
	case "mask":
		// A masking baseline: apply the scenario's baselines/mask.override.yaml
		// (raise the limit / enlarge the pool / add workers). Scenarios that ship
		// no mask override do not support this harness.
		override, err := filepath.Abs(filepath.Join(c.scenariosDir, spec.ID, "baselines", "mask.override.yaml"))
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(override); err != nil {
			return nil, fmt.Errorf("scenario %q ships no baselines/mask.override.yaml (mask baseline unavailable)", spec.ID)
		}
		return refrun.Mask{
			OverrideFile:  override,
			TargetService: spec.Fault.Target,
			Submission: agentloop.Submission{
				RootCause:  "The service was under-resourced, so I gave it more headroom.",
				Actions:    "Raised the resource ceiling / capacity for the affected service.",
				Postmortem: "Added capacity to absorb the load.",
			},
		}, nil
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
