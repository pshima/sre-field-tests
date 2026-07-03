package scenario

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadOOMScenario loads the real shipped oom-killed spec and checks the
// fields the rest of the pipeline depends on. This doubles as a guard that the
// committed spec.yaml stays valid against the schema.
func TestLoadOOMScenario(t *testing.T) {
	// Resolve the repo's scenarios/oom-killed dir relative to this package.
	dir := filepath.Join("..", "..", "scenarios", "oom-killed")
	if _, err := os.Stat(filepath.Join(dir, "spec.yaml")); err != nil {
		t.Skipf("oom-killed spec not present: %v", err)
	}
	s, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if s.ID != "oom-killed" {
		t.Errorf("id = %q, want oom-killed", s.ID)
	}
	if s.Fault.Kind != "cgroup-oom" {
		t.Errorf("fault.kind = %q, want cgroup-oom", s.Fault.Kind)
	}
	if _, ok := s.Tiers["tier0-docker"]; !ok {
		t.Errorf("missing tier0-docker tier")
	}
	if s.Rubric.HealthCheck.SustainSeconds <= 0 {
		t.Errorf("health_check.sustain_seconds should be positive")
	}
	if len(s.Rubric.SafetyViolations) == 0 {
		t.Errorf("expected safety violations defined")
	}
	// Positive-dimension weights should sum to 1.0 (safety is a separate penalty).
	w := s.Rubric.Weights
	sum := w.Diagnosis + w.Remediation + w.Communication
	if sum < 0.999 || sum > 1.001 {
		t.Errorf("dimension weights sum = %.3f, want 1.0", sum)
	}
}

func TestRejectUnknownField(t *testing.T) {
	dir := t.TempDir()
	spec := `schema_version: 1
id: x
fault:
  kind: cgroup-oom
tiers:
  tier0-docker:
    kind: docker-compose
    path: .
task:
  prompt: "do the thing"
bogus_field: 123
`
	if err := os.WriteFile(filepath.Join(dir, "spec.yaml"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(filepath.Join(dir, "spec.yaml")); err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
}

func TestRejectNewerSchema(t *testing.T) {
	s := &Spec{SchemaVersion: SpecVersion + 1, ID: "x", Fault: Fault{Kind: "k"},
		Tiers: map[string]InfraTier{"t": {}}, Task: AgentTask{Prompt: "p"}}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for newer schema version")
	}
}
