// Package scenario defines the on-disk contract for an SRE Field Test scenario.
//
// A scenario is a declarative, git-versioned description of a single SRE
// activity: the fault to inject, how to bootstrap the infrastructure that hosts
// it, the task text handed to the AI agent, the rubric used to grade the run,
// and what the observer should capture. The control plane (cmd/sreft) loads a
// Spec and drives every phase from it; nothing about a scenario is hard-coded in
// Go beyond the driver each field selects.
package scenario

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// SpecVersion is the schema version understood by this build. Loading a spec
// with a higher major version is a hard error so we never silently
// misinterpret a newer format.
const SpecVersion = 1

// Spec is the top-level scenario definition (scenarios/<id>/spec.yaml).
type Spec struct {
	// SchemaVersion pins the spec format; see SpecVersion.
	SchemaVersion int `yaml:"schema_version"`

	// ID is the stable, kebab-case scenario identifier (e.g. "oom-killed").
	// It must match the containing directory name.
	ID string `yaml:"id"`

	// Title is a short human-readable name.
	Title string `yaml:"title"`

	// Summary is a one-paragraph description of the scenario for humans.
	Summary string `yaml:"summary"`

	// Category groups scenarios by failure class (Gunawi-2016-style taxonomy),
	// e.g. "resource-exhaustion", "bad-change", "network", "database".
	Category string `yaml:"category"`

	// Difficulty is an ordinal hint calibrated (loosely) by human first-resolve
	// time: "easy", "medium", "hard".
	Difficulty string `yaml:"difficulty"`

	// HumanResolveMinutes is the estimated time a competent SRE needs to resolve
	// the incident, used to calibrate difficulty and time-based scoring.
	HumanResolveMinutes int `yaml:"human_resolve_minutes"`

	// References are real post-mortems / incidents this scenario is modeled on.
	References []Reference `yaml:"references"`

	// Fault declares the fault(s) injected into the system under test.
	Fault Fault `yaml:"fault"`

	// Tiers maps an infra-tier name (e.g. "tier0-docker") to its bootstrap.
	// v1 ships only the local Docker tier; cloud/Terraform tiers come later.
	Tiers map[string]InfraTier `yaml:"tiers"`

	// Task is what the agent is told and how it interacts with the environment.
	Task AgentTask `yaml:"task"`

	// Observer configures what the observer binary captures for this scenario.
	Observer ObserverConfig `yaml:"observer"`

	// Rubric defines how a run is graded.
	Rubric Rubric `yaml:"rubric"`

	// Oracle holds the reference-solution answer for this scenario, used by the
	// `oracle` harness (the grader's FULL correctness gate). The fix itself is
	// the compose override at oracle/fix.override.yaml; this is the matching
	// diagnosis a correct responder would submit.
	Oracle OracleSpec `yaml:"oracle"`
}

// OracleSpec is the scenario's reference solution answer.
type OracleSpec struct {
	Submission SubmissionSpec `yaml:"submission"`
}

// SubmissionSpec mirrors the agent's final submission (root cause + actions +
// postmortem) as authored for the reference solution.
type SubmissionSpec struct {
	RootCause  string `yaml:"root_cause"`
	Actions    string `yaml:"actions"`
	Postmortem string `yaml:"postmortem"`
}

// Reference is a citation to a real incident/post-mortem backing the scenario.
type Reference struct {
	Title string `yaml:"title"`
	URL   string `yaml:"url"`
	// Note optionally explains what aspect of the incident this models.
	Note string `yaml:"note,omitempty"`
}

// Fault declares the injected failure. Kind selects the injector driver;
// Params carries driver-specific settings so the Go type stays open to new
// fault classes without a schema change.
type Fault struct {
	// Kind is the fault driver, e.g. "cgroup-oom", "cpu-hog", "net-latency",
	// "process-kill", "toxiproxy". See internal/inject for the registry.
	Kind string `yaml:"kind"`

	// Target names the SUT component the fault applies to (a compose service).
	Target string `yaml:"target"`

	// Params are driver-specific (e.g. memory_limit, cpu_load, latency_ms).
	Params map[string]any `yaml:"params,omitempty"`

	// StartDelaySeconds delays fault activation after bootstrap, for scenarios
	// that need a healthy warm-up period first.
	StartDelaySeconds int `yaml:"start_delay_seconds,omitempty"`
}

// InfraTier describes one way to stand up the environment for a scenario.
type InfraTier struct {
	// Kind selects the bootstrap driver: "docker-compose" (v1) or "terraform".
	Kind string `yaml:"kind"`

	// Path is the tier's directory relative to the scenario root (e.g. the
	// docker-compose project dir).
	Path string `yaml:"path"`

	// Cost is an informational tier label: "free-local", "cloud-cheap", etc.
	Cost string `yaml:"cost,omitempty"`
}

// AgentTask is the interface presented to the AI agent for a scenario.
type AgentTask struct {
	// Prompt is the incident description handed to the agent (the "page").
	Prompt string `yaml:"prompt"`

	// OperatorService is the compose service the agent gets a shell in. It has
	// the access a real on-call operator would (e.g. docker socket).
	OperatorService string `yaml:"operator_service"`

	// MaxIterations caps agent tool-use turns (0 = harness default).
	MaxIterations int `yaml:"max_iterations,omitempty"`

	// WallClockSeconds caps total agent wall-clock time (0 = harness default).
	WallClockSeconds int `yaml:"wall_clock_seconds,omitempty"`
}

// ObserverConfig configures the observer for a scenario. Collectors names the
// metric collectors to enable; IntervalMS is the sampling period.
type ObserverConfig struct {
	// Collectors are the enabled collector IDs, e.g. "cgroup-mem",
	// "docker-events", "http-health", "proc-fd". See cmd/observer.
	Collectors []string `yaml:"collectors"`

	// IntervalMS is the sampling interval in milliseconds.
	IntervalMS int `yaml:"interval_ms"`

	// Targets maps a logical target name to a compose service the observer
	// watches (containers, health URLs).
	Targets map[string]string `yaml:"targets,omitempty"`
}

// Rubric defines grading. The core score is state-based (assert the recovered
// system state); the LLM judge is a labeled secondary metric only.
type Rubric struct {
	// Weights roll the per-dimension sub-scores into the composite. They should
	// sum to 1.0 across the positive dimensions; Safety is applied as a penalty.
	Weights Weights `yaml:"weights"`

	// Stages are the lifecycle stages graded for partial credit
	// (Detect -> Diagnose -> Mitigate -> Resolve).
	Stages []Stage `yaml:"stages"`

	// HealthCheck defines the state assertion for "service recovered": the
	// grader probes this and requires sustained success.
	HealthCheck HealthCheck `yaml:"health_check"`

	// RootCauseKey is the answer key for the diagnosis dimension: keywords or
	// phrases the agent's submitted RCA must cover.
	RootCauseKey []string `yaml:"root_cause_key"`

	// SafetyViolations enumerate destructive/risky actions that incur penalties.
	SafetyViolations []SafetyViolation `yaml:"safety_violations"`
}

// Weights are the composite-score weights for the positive dimensions plus the
// safety penalty scale.
type Weights struct {
	Diagnosis     float64 `yaml:"diagnosis"`
	Remediation   float64 `yaml:"remediation"`
	Communication float64 `yaml:"communication"`
	// SafetyPenalty scales the total deduction from detected safety violations.
	SafetyPenalty float64 `yaml:"safety_penalty"`
}

// Stage is one lifecycle stage worth partial credit.
type Stage struct {
	// ID is one of "detect", "diagnose", "mitigate", "resolve".
	ID string `yaml:"id"`
	// Weight is this stage's share of the diagnosis/remediation credit.
	Weight float64 `yaml:"weight"`
	// Description documents what earns this stage's credit.
	Description string `yaml:"description"`
}

// HealthCheck is the state assertion the grader uses to decide "recovered".
type HealthCheck struct {
	// URL is probed for a 2xx (from the grader's vantage point).
	URL string `yaml:"url"`
	// SustainSeconds is how long the service must stay healthy to count as
	// resolved (guards against a transient restart looking like recovery).
	SustainSeconds int `yaml:"sustain_seconds"`
	// UnderLoad requires the load driver to remain active during the sustain
	// window, so a "fix" that only works at idle does not pass.
	UnderLoad bool `yaml:"under_load"`
}

// SafetyViolation is a detectable destructive/risky action and its penalty.
type SafetyViolation struct {
	// ID is a stable slug, e.g. "rm-rf-data", "killed-neighbor", "mask-limit".
	ID string `yaml:"id"`
	// Description explains the violation.
	Description string `yaml:"description"`
	// Penalty is the points deducted (0..1 scale) when detected.
	Penalty float64 `yaml:"penalty"`
	// Detector selects how the violation is detected: "sentinel-file",
	// "container-gone", "command-audit". See internal/score.
	Detector string `yaml:"detector"`
	// Params are detector-specific settings.
	Params map[string]any `yaml:"params,omitempty"`
}

// Load reads and validates a scenario spec from a spec.yaml file path.
func Load(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read scenario spec: %w", err)
	}
	var s Spec
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // reject unknown fields so typos surface early
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("parse scenario spec %s: %w", path, err)
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("invalid scenario spec %s: %w", path, err)
	}
	return &s, nil
}

// LoadDir loads scenarios/<id>/spec.yaml and checks the ID matches the dir.
func LoadDir(dir string) (*Spec, error) {
	s, err := Load(filepath.Join(dir, "spec.yaml"))
	if err != nil {
		return nil, err
	}
	if want := filepath.Base(dir); s.ID != want {
		return nil, fmt.Errorf("scenario id %q does not match directory %q", s.ID, want)
	}
	return s, nil
}

// Validate checks required fields and version compatibility.
func (s *Spec) Validate() error {
	if s.SchemaVersion == 0 {
		return fmt.Errorf("schema_version is required")
	}
	if s.SchemaVersion > SpecVersion {
		return fmt.Errorf("schema_version %d is newer than supported %d", s.SchemaVersion, SpecVersion)
	}
	if s.ID == "" {
		return fmt.Errorf("id is required")
	}
	if s.Fault.Kind == "" {
		return fmt.Errorf("fault.kind is required")
	}
	if len(s.Tiers) == 0 {
		return fmt.Errorf("at least one infra tier is required")
	}
	if s.Task.Prompt == "" {
		return fmt.Errorf("task.prompt is required")
	}
	return nil
}
