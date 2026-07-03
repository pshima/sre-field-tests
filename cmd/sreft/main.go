// Command sreft is the SRE Field Tests control plane. It loads a scenario spec
// and drives every phase: bootstrap the infra tier, inject the fault, run an AI
// agent through the incident, observe, and score. v1 wires the plumbing and the
// data contracts; the fault/agent/grader drivers are filled in across M1-M2.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/alecthomas/kong"

	"github.com/pshima/sre-field-tests/internal/instance"
	"github.com/pshima/sre-field-tests/internal/scenario"
)

// version is stamped at build time via -ldflags; see the Makefile.
var version = "dev"

// CLI is the kong command tree.
type CLI struct {
	ScenariosDir string `kong:"name='scenarios-dir',default='scenarios',help='Directory containing scenario definitions.'"`
	ResultsDir   string `kong:"name='results-dir',default='results',help='Directory where instance results are written.'"`
	Verbose      bool   `kong:"short='v',help='Verbose logging.'"`

	Up      UpCmd      `kong:"cmd,help='Bootstrap a scenario environment at a tier.'"`
	Inject  InjectCmd  `kong:"cmd,help='Inject the scenario fault into a running environment.'"`
	Run     RunCmd     `kong:"cmd,help='Run an instance: an agent works the incident end-to-end.'"`
	Score   ScoreCmd   `kong:"cmd,help='Grade a completed instance directory.'"`
	Report  ReportCmd  `kong:"cmd,help='Aggregate instances into a scorecard.'"`
	Verify  VerifyCmd  `kong:"cmd,help='Self-test a scenario (fault manifests; oracle=FULL; no-op=ZERO).'"`
	Version VersionCmd `kong:"cmd,help='Print version.'"`
}

// ctx carries shared config to command Run methods.
type ctx struct {
	log          *slog.Logger
	scenariosDir string
	resultsDir   string
}

// loadScenario resolves and loads a scenario by ID from the scenarios dir.
func (c *ctx) loadScenario(id string) (*scenario.Spec, error) {
	return scenario.LoadDir(filepath.Join(c.scenariosDir, id))
}

type UpCmd struct {
	Scenario string `kong:"arg,help='Scenario ID (e.g. oom-killed).'"`
	Tier     string `kong:"default='tier0-docker',help='Infra tier to bootstrap.'"`
}

func (cmd *UpCmd) Run(c *ctx) error {
	spec, err := c.loadScenario(cmd.Scenario)
	if err != nil {
		return err
	}
	tier, ok := spec.Tiers[cmd.Tier]
	if !ok {
		return fmt.Errorf("scenario %q has no tier %q", spec.ID, cmd.Tier)
	}
	c.log.Info("bootstrap plan",
		"scenario", spec.ID, "tier", cmd.Tier, "kind", tier.Kind, "path", tier.Path,
		"fault", spec.Fault.Kind, "operator", spec.Task.OperatorService)
	// M1 wires internal/bootstrap.For(tier.Kind).Up(...) here.
	return notImplemented("bootstrap driver", "M1")
}

type InjectCmd struct {
	Scenario string `kong:"arg,help='Scenario ID.'"`
}

func (cmd *InjectCmd) Run(c *ctx) error {
	spec, err := c.loadScenario(cmd.Scenario)
	if err != nil {
		return err
	}
	c.log.Info("inject plan", "scenario", spec.ID, "fault", spec.Fault.Kind, "target", spec.Fault.Target)
	return notImplemented("fault injector", "M1")
}

type RunCmd struct {
	Scenario    string  `kong:"arg,help='Scenario ID.'"`
	Model       string  `kong:"required,help='OpenRouter model slug to route to.'"`
	Seed        int     `kong:"default='1',help='Run seed (for reproducibility and pass^k grouping).'"`
	Tier        string  `kong:"default='tier0-docker',help='Infra tier.'"`
	Temperature float64 `kong:"default='0.0',help='Decoding temperature.'"`
	Harness     string  `kong:"default='neutral-go',help='Agent harness identifier.'"`
}

func (cmd *RunCmd) Run(c *ctx) error {
	spec, err := c.loadScenario(cmd.Scenario)
	if err != nil {
		return err
	}
	// Build the instance metadata now so the identity/plumbing is exercised
	// even before the agent driver lands.
	now := time.Now()
	id := instance.NewID(spec.ID, cmd.Model, cmd.Seed, now)
	meta := &instance.Metadata{
		ID:             id,
		Scenario:       spec.ID,
		Tier:           cmd.Tier,
		Model:          cmd.Model,
		Harness:        cmd.Harness,
		Seed:           cmd.Seed,
		Sampling:       instance.Sampling{Temperature: cmd.Temperature},
		StartedAt:      now,
		HarnessVersion: version,
	}
	dir := instance.Dir(c.resultsDir, id)
	if err := meta.Write(dir); err != nil {
		return err
	}
	c.log.Info("instance created", "id", id, "dir", dir, "model", cmd.Model, "seed", cmd.Seed)
	// M2 wires bootstrap -> observe -> inject -> agentloop -> score here.
	return notImplemented("agent run pipeline", "M2")
}

type ScoreCmd struct {
	InstanceDir string `kong:"arg,help='Path to an instance directory.'"`
}

func (cmd *ScoreCmd) Run(c *ctx) error {
	meta, err := instance.ReadMetadata(cmd.InstanceDir)
	if err != nil {
		return err
	}
	c.log.Info("scoring", "instance", meta.ID, "scenario", meta.Scenario, "model", meta.Model)
	return notImplemented("grader", "M2")
}

type ReportCmd struct {
	Format string `kong:"default='markdown',enum='markdown,json',help='Scorecard output format.'"`
}

func (cmd *ReportCmd) Run(c *ctx) error {
	c.log.Info("report", "results", c.resultsDir, "format", cmd.Format)
	return notImplemented("scorecard aggregation", "M3")
}

type VerifyCmd struct {
	Scenario string `kong:"arg,help='Scenario ID.'"`
	Tier     string `kong:"default='tier0-docker',help='Infra tier.'"`
}

func (cmd *VerifyCmd) Run(c *ctx) error {
	// Even before the self-test harness exists, verify that the spec loads and
	// validates — the cheapest guard that a scenario is well-formed.
	spec, err := c.loadScenario(cmd.Scenario)
	if err != nil {
		return err
	}
	c.log.Info("spec valid", "scenario", spec.ID, "title", spec.Title,
		"references", len(spec.References), "safety_checks", len(spec.Rubric.SafetyViolations))
	return notImplemented("scenario self-test (fault manifests / oracle=FULL / no-op=ZERO)", "M1")
}

type VersionCmd struct{}

func (cmd *VersionCmd) Run(_ *ctx) error {
	fmt.Println(version)
	return nil
}

// notImplemented returns a clear, actionable error for scaffolding not yet built.
func notImplemented(what, milestone string) error {
	return fmt.Errorf("%s is not implemented yet (planned for %s)", what, milestone)
}

func main() {
	var cli CLI
	kctx := kong.Parse(&cli,
		kong.Name("sreft"),
		kong.Description("SRE Field Tests — benchmark AI agents on SRE scenarios."),
		kong.UsageOnError(),
	)

	level := slog.LevelInfo
	if cli.Verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	appCtx := &ctx{log: log, scenariosDir: cli.ScenariosDir, resultsDir: cli.ResultsDir}
	if err := kctx.Run(appCtx); err != nil {
		log.Error("command failed", "err", err)
		os.Exit(1)
	}
}
