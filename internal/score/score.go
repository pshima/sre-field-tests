// Package score grades an instance and aggregates instances into a scorecard.
//
// The core score is state-based: after the agent finishes, the grader asserts
// properties of the recovered system (service healthy and sustained under load,
// OOM kills stopped) rather than judging the transcript. Diagnosis is checked
// against an answer key plus the fix actually applied; safety is a negative term
// for destructive/unnecessary actions; communication is a labeled secondary
// LLM-judge metric, never part of the core correctness number.
package score

import (
	"github.com/pshima/sre-field-tests/internal/instance"
)

// Verdict is the coarse per-instance outcome, mirroring SWE-bench-style
// full/partial/none resolution.
type Verdict string

const (
	VerdictFull    Verdict = "full"
	VerdictPartial Verdict = "partial"
	VerdictNone    Verdict = "none"
)

// Result is the grader's output for one instance, written to score.json.
type Result struct {
	InstanceID string  `json:"instance_id"`
	Verdict    Verdict `json:"verdict"`

	// Dimension sub-scores, each normalized 0..1.
	Diagnosis     float64 `json:"diagnosis"`
	Remediation   float64 `json:"remediation"`
	Communication float64 `json:"communication"`

	// SafetyPenalty is the total deduction (>=0) from detected violations.
	SafetyPenalty float64 `json:"safety_penalty"`

	// Composite is the final weighted score after the safety penalty, 0..1.
	Composite float64 `json:"composite"`

	// StageCredit records per-lifecycle-stage credit (detect/diagnose/mitigate/
	// resolve), each 0..1.
	StageCredit map[string]float64 `json:"stage_credit"`

	// MTTRSeconds is time from fault activation to first sustained recovery;
	// nil if the service was never recovered.
	MTTRSeconds *float64 `json:"mttr_seconds,omitempty"`

	// SafetyViolations lists the violation IDs that fired.
	SafetyViolations []string `json:"safety_violations,omitempty"`

	// Notes carries human-readable grader reasoning for auditability.
	Notes string `json:"notes,omitempty"`
}

// Grader scores a single completed instance from its on-disk artifacts.
type Grader interface {
	Grade(instanceDir string, meta *instance.Metadata) (*Result, error)
}

// Aggregate is a per-(model,scenario) rollup across seeds — the row that becomes
// a scorecard entry.
type Aggregate struct {
	Scenario string `json:"scenario"`
	Model    string `json:"model"`
	Harness  string `json:"harness"`
	N        int    `json:"n"` // number of instances

	// CompositeMean and CompositeSE are the headline value and its standard
	// error (SE via CLT over the per-instance composites).
	CompositeMean float64 `json:"composite_mean"`
	CompositeSE   float64 `json:"composite_se"`

	// PassAtK is the fraction of instances fully resolved (pass@1-style).
	// PassHatK is the reliability metric: probability ALL k seeds resolve.
	PassAtK  float64 `json:"pass_at_k"`
	PassHatK float64 `json:"pass_hat_k"`

	// Dimension means for the sub-table.
	DiagnosisMean       float64 `json:"diagnosis_mean"`
	RemediationMean     float64 `json:"remediation_mean"`
	CommunicationMean   float64 `json:"communication_mean"`
	SafetyViolationRate float64 `json:"safety_violation_rate"`

	// MTTRMedianSeconds over resolved instances (median, not mean: incident
	// durations are heavy-tailed, per the VOID).
	MTTRMedianSeconds *float64 `json:"mttr_median_seconds,omitempty"`
}
