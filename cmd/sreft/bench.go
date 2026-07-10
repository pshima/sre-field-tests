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

	"github.com/pshima/sre-field-tests/internal/instance"
	"github.com/pshima/sre-field-tests/internal/scenario"
	"github.com/pshima/sre-field-tests/internal/score"
	"github.com/pshima/sre-field-tests/internal/suite"
)

// BenchCmd runs a whole suite: a matrix of scenarios × harness/model cells ×
// seeds, strictly sequentially, into a self-contained run directory.
type BenchCmd struct {
	Suite   string `kong:"arg,help='Path to a suite file (e.g. suites/cli-sweep.yaml).'"`
	RunsDir string `kong:"default='runs',help='Parent directory for run outputs.'"`
	Keep    bool   `kong:"help='Leave each environment running after its cell (debugging).'"`
}

func (cmd *BenchCmd) Run(c *ctx) error {
	s, err := suite.Load(cmd.Suite)
	if err != nil {
		return err
	}
	jobs := s.Expand()

	// Pre-flight: fail before we start, not 20 minutes in.
	if err := preflight(c); err != nil {
		return fmt.Errorf("pre-flight: %w", err)
	}

	// One signal context for the whole sweep: Ctrl-C aborts after the current
	// cell tears down (the cell's own agent context is derived from this).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	start := time.Now()
	runID := s.Name + "-" + start.UTC().Format("20060102T150405Z")
	runDir := filepath.Join(cmd.RunsDir, runID)
	man := &suite.Manifest{
		RunID: runID, Suite: s.Name, SuiteFile: cmd.Suite, Tier: s.Tier,
		GitSHA: gitSHA(), HarnessVer: version, StartedAt: start, TotalCells: len(jobs),
	}
	if err := man.Write(runDir); err != nil {
		return err
	}

	c.log.Info("bench starting", "suite", s.Name, "cells", len(jobs), "run", runID, "dir", runDir)
	fmt.Printf("Running suite %q: %d cells → %s\n", s.Name, len(jobs), runDir)

	specs := map[string]*scenario.Spec{}
	for i, job := range jobs {
		if ctx.Err() != nil {
			c.log.Warn("bench aborted by signal", "completed", i, "total", len(jobs))
			break
		}
		fmt.Printf("\n[cell %d/%d] %s / %s / seed %d\n", i+1, len(jobs), job.Scenario, job.Harness, job.Seed)

		spec, ok := specs[job.Scenario]
		if !ok {
			spec, err = c.loadScenario(job.Scenario)
			if err != nil {
				man.Cells = append(man.Cells, failedCell(job, err))
				_ = man.Write(runDir)
				continue
			}
			specs[job.Scenario] = spec
		}

		p := runParams{
			Model: job.Model, Harness: job.Harness, Tier: s.Tier, Seed: job.Seed,
			Keep: cmd.Keep, ResultsDir: runDir,
		}
		result, dir, rerr := runInstance(ctx, c, spec, p)
		man.Cells = append(man.Cells, cellResult(job, result, dir, rerr))
		_ = man.Write(runDir) // persist incrementally so a partial run is inspectable

		if !cmd.Keep {
			// Safety net between cells: nothing from this cell should linger.
			ensureNoSreftContainers(c)
		}
	}

	fin := time.Now()
	man.FinishedAt = &fin
	_ = man.Write(runDir)

	// Scorecard from the run's graded instances.
	card, err := writeRunScorecard(runDir)
	if err != nil {
		c.log.Warn("scorecard generation failed", "err", err)
	} else {
		fmt.Printf("\n%s\n", card)
	}
	ok, failed := tally(man)
	fmt.Printf("Suite %q complete in %s: %d cells graded, %d failed. Artifacts: %s\n",
		s.Name, fin.Sub(start).Round(time.Second), ok, failed, runDir)
	return nil
}

// RescoreCmd re-grades every instance in a run from its saved artifacts (no
// agent re-runs) — for iterating on the grader/rubric.
type RescoreCmd struct {
	RunID   string `kong:"arg,name='run',help='Run id or run directory to re-score.'"`
	RunsDir string `kong:"default='runs',help='Parent directory for run outputs.'"`
}

func (cmd *RescoreCmd) Run(c *ctx) error {
	runDir := resolveRunDir(cmd.RunsDir, cmd.RunID)
	results, err := score.LoadResults(runDir)
	if err != nil {
		return fmt.Errorf("load run %s: %w", runDir, err)
	}
	n := 0
	for _, ir := range results {
		spec, err := c.loadScenario(ir.Meta.Scenario)
		if err != nil {
			c.log.Warn("rescore: skip", "instance", ir.Meta.ID, "err", err)
			continue
		}
		dir := instance.Dir(runDir, ir.Meta.ID)
		res, err := score.NewStateGrader(spec, nil).Grade(dir, ir.Meta)
		if err != nil {
			c.log.Warn("rescore: grade failed", "instance", ir.Meta.ID, "err", err)
			continue
		}
		if err := res.Write(dir); err != nil {
			return err
		}
		n++
	}
	c.log.Info("re-scored", "instances", n, "run", runDir)
	// Refresh the scorecard and the manifest composites.
	if man, err := suite.ReadManifest(runDir); err == nil {
		refreshManifestScores(man, runDir)
		_ = man.Write(runDir)
	}
	card, err := writeRunScorecard(runDir)
	if err != nil {
		return err
	}
	fmt.Printf("Re-scored %d instances in %s.\n\n%s\n", n, runDir, card)
	return nil
}

// --- helpers -----------------------------------------------------------------

func preflight(c *ctx) error {
	if err := dockerReachable(); err != nil {
		return fmt.Errorf("docker not reachable: %w", err)
	}
	if p := observerBinary(); p != "observer" {
		// A resolved path means the binary exists; "observer" means rely on PATH.
		c.log.Debug("observer binary", "path", p)
	}
	ensureNoSreftContainers(c)
	return nil
}

func dockerReachable() error {
	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	return cmd.Run()
}

// ensureNoSreftContainers removes any leftover sreft- containers so a run starts
// (and the next cell continues) from a clean slate.
func ensureNoSreftContainers(c *ctx) {
	out, err := exec.Command("docker", "ps", "-aq", "--filter", "name=sreft-").Output()
	if err != nil {
		return
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return
	}
	c.log.Warn("removing leftover sreft containers", "count", len(ids))
	_ = exec.Command("docker", append([]string{"rm", "-f"}, ids...)...).Run()
}

func gitSHA() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	sha := strings.TrimSpace(string(out))
	if dirty := exec.Command("git", "diff", "--quiet").Run(); dirty != nil {
		sha += "-dirty"
	}
	return sha
}

func failedCell(job suite.Job, err error) suite.CellResult {
	return suite.CellResult{
		Scenario: job.Scenario, Harness: job.Harness, Model: job.Model, Seed: job.Seed,
		Status: "failed", Error: err.Error(),
	}
}

func cellResult(job suite.Job, result *score.Result, dir string, rerr error) suite.CellResult {
	cr := suite.CellResult{
		Scenario: job.Scenario, Harness: job.Harness, Model: job.Model, Seed: job.Seed,
	}
	if dir != "" {
		cr.InstanceID = filepath.Base(dir)
		if meta, err := instance.ReadMetadata(dir); err == nil {
			cr.FailureMode = string(meta.FailureMode)
		}
	}
	if rerr != nil {
		cr.Status = "failed"
		cr.Error = rerr.Error()
		return cr
	}
	cr.Status = "ok"
	if result != nil {
		cr.Verdict = string(result.Verdict)
		cr.Composite = result.Composite
		cr.MTTRSeconds = result.MTTRSeconds
	}
	return cr
}

func tally(m *suite.Manifest) (ok, failed int) {
	for _, cr := range m.Cells {
		if cr.Status == "ok" {
			ok++
		} else {
			failed++
		}
	}
	return ok, failed
}

// resolveRunDir accepts either a run id (under runsDir) or a directory path.
func resolveRunDir(runsDir, run string) string {
	if strings.ContainsRune(run, filepath.Separator) {
		return run
	}
	return filepath.Join(runsDir, run)
}

func writeRunScorecard(runDir string) (string, error) {
	results, err := score.LoadResults(runDir)
	if err != nil {
		return "", err
	}
	card := score.Scorecard(score.AggregateResults(results))
	if err := os.WriteFile(filepath.Join(runDir, "scorecard.md"), []byte(card), 0o644); err != nil {
		return "", err
	}
	return card, nil
}

func refreshManifestScores(m *suite.Manifest, runDir string) {
	for i := range m.Cells {
		if m.Cells[i].InstanceID == "" {
			continue
		}
		if res, err := score.ReadResult(instance.Dir(runDir, m.Cells[i].InstanceID)); err == nil {
			m.Cells[i].Verdict = string(res.Verdict)
			m.Cells[i].Composite = res.Composite
			m.Cells[i].MTTRSeconds = res.MTTRSeconds
		}
	}
}
