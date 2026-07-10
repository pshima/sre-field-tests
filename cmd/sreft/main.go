// Command sreft is the SRE Field Tests control plane. It loads a scenario spec
// and drives every phase: bootstrap the infra tier, inject the fault, run an AI
// agent through the incident, observe, and score. v1 wires the plumbing and the
// data contracts; the fault/agent/grader drivers are filled in across M1-M2.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/alecthomas/kong"

	"github.com/pshima/sre-field-tests/internal/bootstrap"
	_ "github.com/pshima/sre-field-tests/internal/inject" // register fault drivers
	"github.com/pshima/sre-field-tests/internal/instance"
	"github.com/pshima/sre-field-tests/internal/scenario"
	"github.com/pshima/sre-field-tests/internal/score"
	"github.com/pshima/sre-field-tests/internal/selftest"
)

// version is stamped at build time via -ldflags; see the Makefile.
var version = "dev"

// CLI is the kong command tree.
type CLI struct {
	ScenariosDir string `kong:"name='scenarios-dir',default='scenarios',help='Directory containing scenario definitions.'"`
	ResultsDir   string `kong:"name='results-dir',default='results',help='Directory where instance results are written.'"`
	Verbose      bool   `kong:"short='v',help='Verbose logging.'"`

	Up      UpCmd      `kong:"cmd,help='Bootstrap a scenario environment at a tier.'"`
	Down    DownCmd    `kong:"cmd,help='Tear down a scenario environment.'"`
	Inject  InjectCmd  `kong:"cmd,help='Inject the scenario fault into a running environment.'"`
	Run     RunCmd     `kong:"cmd,help='Run an instance: an agent works the incident end-to-end.'"`
	Bench   BenchCmd   `kong:"cmd,help='Run a suite (a matrix of scenarios × harnesses × seeds) into a run directory.'"`
	Rescore RescoreCmd `kong:"cmd,help='Re-grade a run from its saved artifacts (no agent re-runs).'"`
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
	boot, err := bootstrap.For(tier.Kind)
	if err != nil {
		return err
	}
	c.log.Info("bootstrapping", "scenario", spec.ID, "tier", cmd.Tier, "kind", tier.Kind)
	env, err := boot.Up(context.Background(), spec, tier)
	if err != nil {
		return err
	}
	c.log.Info("environment up",
		"operator", env.OperatorContainer, "services", env.Services(), "endpoints", env.Endpoints)
	fmt.Printf("Environment for %q is up.\n  operator shell: docker exec -it %s bash\n  down: sreft down %s\n",
		spec.ID, env.OperatorContainer, spec.ID)
	return nil
}

type DownCmd struct {
	Scenario string `kong:"arg,help='Scenario ID.'"`
	Tier     string `kong:"default='tier0-docker',help='Infra tier.'"`
}

func (cmd *DownCmd) Run(c *ctx) error {
	spec, err := c.loadScenario(cmd.Scenario)
	if err != nil {
		return err
	}
	tier, ok := spec.Tiers[cmd.Tier]
	if !ok {
		return fmt.Errorf("scenario %q has no tier %q", spec.ID, cmd.Tier)
	}
	boot, err := bootstrap.For(tier.Kind)
	if err != nil {
		return err
	}
	dir, err := bootstrap.ResolveTierDir(spec, tier)
	if err != nil {
		return err
	}
	c.log.Info("tearing down", "scenario", spec.ID, "dir", dir)
	return boot.Down(context.Background(), bootstrap.EnvForDir(dir))
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
	Model       string  `kong:"required,help='Model slug (OpenRouter) or a reference harness name.'"`
	Seed        int     `kong:"default='1',help='Run seed (for reproducibility and pass^k grouping).'"`
	Tier        string  `kong:"default='tier0-docker',help='Infra tier.'"`
	Temperature float64 `kong:"default='0.0',help='Decoding temperature.'"`
	Harness     string  `kong:"default='neutral-go',enum='neutral-go,oracle,noop,always-restart,mask,claude-cli,codex-cli',help='Agent harness: neutral-go (OpenRouter), claude-cli, codex-cli, oracle, noop, or a reflex baseline (always-restart, mask).'"`
	Keep        bool    `kong:"help='Leave the environment running after the run (debugging).'"`
}

func (cmd *RunCmd) Run(c *ctx) error {
	spec, err := c.loadScenario(cmd.Scenario)
	if err != nil {
		return err
	}
	p := runParams{
		Model: cmd.Model, Harness: cmd.Harness, Tier: cmd.Tier, Seed: cmd.Seed,
		Temperature: cmd.Temperature, Keep: cmd.Keep, ResultsDir: c.resultsDir,
	}
	result, dir, err := runOneInstance(c, spec, p)
	if err != nil {
		return err
	}
	printScore(spec, result, dir)
	return nil
}

type ScoreCmd struct {
	InstanceDir string `kong:"arg,help='Path to an instance directory.'"`
}

func (cmd *ScoreCmd) Run(c *ctx) error {
	meta, err := instance.ReadMetadata(cmd.InstanceDir)
	if err != nil {
		return err
	}
	spec, err := c.loadScenario(meta.Scenario)
	if err != nil {
		return err
	}
	res, err := score.NewStateGrader(spec, nil).Grade(cmd.InstanceDir, meta)
	if err != nil {
		return err
	}
	if err := res.Write(cmd.InstanceDir); err != nil {
		return err
	}
	printScore(spec, res, cmd.InstanceDir)
	return nil
}

type ReportCmd struct {
	RunID   string `kong:"name='run',help='Report on a run id/directory under runs/ instead of the results dir.'"`
	RunsDir string `kong:"default='runs',help='Parent directory for run outputs.'"`
	Format  string `kong:"default='markdown',enum='markdown,json',help='Scorecard output format.'"`
	Out     string `kong:"help='Write the scorecard to this file instead of stdout.'"`
}

func (cmd *ReportCmd) Run(c *ctx) error {
	resultsRoot := c.resultsDir
	if cmd.RunID != "" {
		resultsRoot = resolveRunDir(cmd.RunsDir, cmd.RunID)
	}
	results, err := score.LoadResults(resultsRoot)
	if err != nil {
		return fmt.Errorf("load results from %s: %w", resultsRoot, err)
	}
	if len(results) == 0 {
		return fmt.Errorf("no graded instances found in %s (run some first)", resultsRoot)
	}
	aggs := score.AggregateResults(results)

	var out string
	if cmd.Format == "json" {
		b, err := json.MarshalIndent(aggs, "", "  ")
		if err != nil {
			return err
		}
		out = string(b)
	} else {
		out = score.Scorecard(aggs)
	}
	if cmd.Out != "" {
		if err := os.WriteFile(cmd.Out, []byte(out), 0o644); err != nil {
			return err
		}
		c.log.Info("scorecard written", "path", cmd.Out, "groups", len(aggs), "instances", len(results))
		return nil
	}
	fmt.Println(out)
	return nil
}

type VerifyCmd struct {
	Scenario string `kong:"arg,help='Scenario ID.'"`
	Tier     string `kong:"default='tier0-docker',help='Infra tier.'"`
}

func (cmd *VerifyCmd) Run(c *ctx) error {
	spec, err := c.loadScenario(cmd.Scenario)
	if err != nil {
		return err
	}
	// The oracle override lives at scenarios/<id>/oracle/fix.override.yaml.
	oracle, err := filepath.Abs(filepath.Join(c.scenariosDir, spec.ID, "oracle", "fix.override.yaml"))
	if err != nil {
		return err
	}
	if _, err := os.Stat(oracle); err != nil {
		oracle = "" // no oracle shipped; skip the recovery check
	}
	rep, err := selftest.Run(context.Background(), spec, cmd.Tier, selftest.Options{
		OracleOverride: oracle,
		Log:            c.log,
	})
	if err != nil {
		return err
	}
	fmt.Printf("\nSelf-test: %s (tier %s)\n", spec.ID, cmd.Tier)
	for _, ck := range rep.Checks {
		mark := "FAIL"
		if ck.Pass {
			mark = "PASS"
		}
		fmt.Printf("  [%s] %-20s %s\n", mark, ck.Name, ck.Detail)
	}
	if !rep.Passed {
		return fmt.Errorf("self-test FAILED for %s", spec.ID)
	}
	fmt.Printf("  => scenario verified: fault manifests, no-op stays broken, oracle recovers\n")
	return nil
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

	// Point the bootstrap drivers at the same scenarios directory the CLI uses.
	bootstrap.SetScenarioRoot(cli.ScenariosDir)

	appCtx := &ctx{log: log, scenariosDir: cli.ScenariosDir, resultsDir: cli.ResultsDir}
	if err := kctx.Run(appCtx); err != nil {
		log.Error("command failed", "err", err)
		os.Exit(1)
	}
}
