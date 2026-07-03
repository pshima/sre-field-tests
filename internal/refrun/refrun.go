// Package refrun provides reference "agents" that implement agentloop.Runner
// without an LLM: an oracle that applies a scenario's known-good fix, and a
// no-op that does nothing. They let the full run/observe/score pipeline be
// exercised with no API key, and they are the ground truth for the grader's
// correctness gate — the oracle must score FULL, the no-op ZERO.
package refrun

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/pshima/sre-field-tests/internal/agentloop"
	"github.com/pshima/sre-field-tests/internal/bootstrap"
	"github.com/pshima/sre-field-tests/internal/instance"
)

// Oracle applies a compose override (the scenario's reference fix) and submits a
// canned-correct diagnosis.
type Oracle struct {
	OverrideFile  string // absolute path to the oracle compose override
	TargetService string // compose service to recreate with the fix
	Submission    agentloop.Submission
}

func (o Oracle) Run(ctx context.Context, env *bootstrap.Env, _ agentloop.Config, instanceDir string) (*agentloop.Result, error) {
	if _, err := env.ComposeExec(ctx, []string{o.OverrideFile}, "up", "-d", o.TargetService); err != nil {
		return &agentloop.Result{Stopped: "error"}, err
	}
	sub := o.Submission
	if err := writeSubmission(instanceDir, &sub); err != nil {
		return &agentloop.Result{Stopped: "error"}, err
	}
	return &agentloop.Result{Submission: &sub, Iterations: 1, Stopped: "submitted"}, nil
}

// Noop does nothing — the incident is left to keep failing.
type Noop struct{}

func (Noop) Run(_ context.Context, _ *bootstrap.Env, _ agentloop.Config, _ string) (*agentloop.Result, error) {
	return &agentloop.Result{Iterations: 0, Stopped: "noop"}, nil
}

func writeSubmission(dir string, sub *agentloop.Submission) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sub, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, instance.SubmissionFile), data, 0o644)
}
