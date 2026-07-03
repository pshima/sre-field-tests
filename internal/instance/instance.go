// Package instance defines the metadata and on-disk layout for a single run of
// a scenario against a specific model/harness/seed.
//
// Vocabulary: a *scenario* is the reusable definition; an *instance* is one
// concrete run of it. "sonnet-5 x oom-killed seed=1" and "opus-4.8 x oom-killed
// seed=1" are two instances of the same scenario. Instance metadata is what lets
// us describe, compare, and reproduce specific runs.
package instance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Metadata fully identifies and describes one run. It is written to
// meta.json at the root of the instance directory and is the join key for all
// analysis. Everything needed to interpret or reproduce a run lives here.
type Metadata struct {
	// ID is the unique instance identifier (see NewID). Also the directory name.
	ID string `json:"id"`

	// Scenario is the scenario ID (e.g. "oom-killed").
	Scenario string `json:"scenario"`

	// ScenarioGitSHA pins the exact scenario definition used, so a later spec
	// edit cannot silently change what a historical instance meant.
	ScenarioGitSHA string `json:"scenario_git_sha"`

	// Tier is the infra tier used (e.g. "tier0-docker").
	Tier string `json:"tier"`

	// Model is the model identifier as routed (e.g. an OpenRouter model slug).
	Model string `json:"model"`

	// Harness identifies the agent scaffold. v1 is the neutral Go loop; this
	// field exists so "model + native CLI" adapters remain distinguishable.
	Harness string `json:"harness"`

	// Seed is the run seed, recorded for reproducibility and pass^k grouping.
	Seed int `json:"seed"`

	// Sampling records the decoding parameters (temperature, top_p, etc.). A
	// score is undefined without these, so they are first-class metadata.
	Sampling Sampling `json:"sampling"`

	// StartedAt / FinishedAt bound the run. FaultStartedAt is when the fault
	// activated, used as the zero point for MTTR.
	StartedAt      time.Time  `json:"started_at"`
	FaultStartedAt time.Time  `json:"fault_started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`

	// FailureMode is set when the instance did not complete normally, using the
	// closed enum below, so aggregate stats can exclude infra failures cleanly.
	FailureMode FailureMode `json:"failure_mode,omitempty"`

	// HarnessVersion / observer + tooling versions for full disclosure.
	HarnessVersion string `json:"harness_version,omitempty"`
}

// Sampling captures decoding parameters for reproducibility.
type Sampling struct {
	Temperature float64 `json:"temperature"`
	TopP        float64 `json:"top_p,omitempty"`
}

// FailureMode is the closed set of non-scoring terminal states, mirroring the
// enums Terminal-Bench and Cybench use so infra failures are not scored as
// agent failures.
type FailureMode string

const (
	FailureNone         FailureMode = ""
	FailureAgentTimeout FailureMode = "agent_timeout"
	FailureAgentError   FailureMode = "agent_error"
	FailureInfraError   FailureMode = "infra_error"
	FailureObserverDied FailureMode = "observer_died"
	FailureGraderError  FailureMode = "grader_error"
)

// Layout describes the on-disk contents of an instance directory. All paths are
// relative to the instance root. The directory is self-contained so results
// remain auditable (HELM-style) long after the run.
//
//	<results-root>/<instance-id>/
//	  meta.json          -- Metadata
//	  transcript.jsonl   -- full agent conversation + tool calls (agentloop)
//	  observer.jsonl     -- observer time-series + events (observe.Record)
//	  submission.json    -- the agent's final RCA/postmortem submission
//	  score.json         -- the grader's per-dimension result
const (
	MetaFile       = "meta.json"
	TranscriptFile = "transcript.jsonl"
	ObserverFile   = "observer.jsonl"
	SubmissionFile = "submission.json"
	ScoreFile      = "score.json"
)

// NewID builds a deterministic-ish, human-scannable instance ID. The caller
// supplies the timestamp (Date/Now is unavailable in some contexts and, more
// importantly, an explicit stamp keeps IDs reproducible in tests).
func NewID(scenario, model string, seed int, ts time.Time) string {
	// Model slugs contain '/' and ':'; flatten them for a filesystem-safe name.
	safeModel := sanitize(model)
	return fmt.Sprintf("%s__%s__seed%d__%s", scenario, safeModel, seed, ts.UTC().Format("20060102T150405Z"))
}

func sanitize(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			out = append(out, r)
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}

// Dir returns the instance directory path under a results root.
func Dir(resultsRoot, id string) string { return filepath.Join(resultsRoot, id) }

// Write persists Metadata to meta.json in dir, creating dir if needed.
func (m *Metadata) Write(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create instance dir: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal instance meta: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, MetaFile), data, 0o644)
}

// ReadMetadata loads meta.json from an instance directory.
func ReadMetadata(dir string) (*Metadata, error) {
	data, err := os.ReadFile(filepath.Join(dir, MetaFile))
	if err != nil {
		return nil, fmt.Errorf("read instance meta: %w", err)
	}
	var m Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse instance meta: %w", err)
	}
	return &m, nil
}
