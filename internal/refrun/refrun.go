// Package refrun provides reference "agents" that implement agentloop.Runner
// without an LLM. They fall in three tiers and let the whole run/observe/score
// pipeline be exercised with no API key:
//
//   - Oracle — applies a scenario's known-good fix; the grader's FULL gate.
//   - Noop — does nothing; the grader's ZERO gate.
//   - Restart / Mask — deterministic *reflex* baselines (the "just bounce it" /
//     "just throw resources at it" policies a real agent is tempted by). A
//     credible scenario must defeat these: they should score PARTIAL/NONE, never
//     FULL. If a one-line reflex scores FULL, the scenario measures nothing.
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
	return applyOverride(ctx, env, o.OverrideFile, o.TargetService, instanceDir, o.Submission)
}

// Mask applies a *masking* compose override (raise the limit, enlarge the pool,
// add workers) — the reflex that buys headroom without addressing the root
// cause. Where masking transiently restores health it should score PARTIAL (real
// remediation credit, but weak diagnosis and, for a real agent, a mask-* safety
// penalty); where it does nothing (a bad deploy, a ReDoS rule) it stays broken.
// Mechanically identical to Oracle — the point is the override it points at.
type Mask struct {
	OverrideFile  string
	TargetService string
	Submission    agentloop.Submission
}

func (m Mask) Run(ctx context.Context, env *bootstrap.Env, _ agentloop.Config, instanceDir string) (*agentloop.Result, error) {
	return applyOverride(ctx, env, m.OverrideFile, m.TargetService, instanceDir, m.Submission)
}

// Restart is the universal reflex: bounce the affected service and hope. Our
// scenarios are designed so a restart never durably fixes the fault (the leak
// leaks again, the pool re-exhausts, the bad release is still bad), so this
// should score PARTIAL/NONE — the sharpest non-triviality check.
type Restart struct {
	TargetService string
	Submission    agentloop.Submission
}

func (r Restart) Run(ctx context.Context, env *bootstrap.Env, _ agentloop.Config, instanceDir string) (*agentloop.Result, error) {
	if _, err := env.ComposeExec(ctx, nil, "restart", r.TargetService); err != nil {
		return &agentloop.Result{Stopped: "error"}, err
	}
	sub := r.Submission
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

// applyOverride layers a compose override onto the target, recreates it, and
// writes the canned submission. Shared by Oracle (the correct fix) and Mask (a
// masking baseline) — they differ only in which override and submission.
func applyOverride(ctx context.Context, env *bootstrap.Env, overrideFile, target, instanceDir string, sub agentloop.Submission) (*agentloop.Result, error) {
	if _, err := env.ComposeExec(ctx, []string{overrideFile}, "up", "-d", target); err != nil {
		return &agentloop.Result{Stopped: "error"}, err
	}
	s := sub
	if err := writeSubmission(instanceDir, &s); err != nil {
		return &agentloop.Result{Stopped: "error"}, err
	}
	return &agentloop.Result{Submission: &s, Iterations: 1, Stopped: "submitted"}, nil
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
