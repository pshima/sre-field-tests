package suite

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandOrder(t *testing.T) {
	s := &Suite{
		Name: "t", Tier: "tier0-docker", Seeds: 2,
		Scenarios: []string{"a", "b"},
		Matrix:    []Cell{{Harness: "claude-cli", Model: "default"}, {Harness: "codex-cli", Model: "default"}},
	}
	jobs := s.Expand()
	// 2 scenarios × 2 cells × 2 seeds = 8 jobs.
	if len(jobs) != 8 {
		t.Fatalf("got %d jobs, want 8", len(jobs))
	}
	// All of scenario "a" comes before "b" (scenario-at-a-time).
	if jobs[0].Scenario != "a" || jobs[len(jobs)-1].Scenario != "b" {
		t.Errorf("unexpected scenario ordering: %+v ... %+v", jobs[0], jobs[len(jobs)-1])
	}
	// Within a scenario+cell, seeds increment.
	if jobs[0].Seed != 1 || jobs[1].Seed != 2 {
		t.Errorf("seeds not enumerated: %+v %+v", jobs[0], jobs[1])
	}
	if jobs[0].Harness != "claude-cli" || jobs[2].Harness != "codex-cli" {
		t.Errorf("cells not enumerated: %+v %+v", jobs[0], jobs[2])
	}
}

func TestLoadValidatesAndRejectsUnknown(t *testing.T) {
	dir := t.TempDir()
	good := `name: s
tier: tier0-docker
seeds: 1
scenarios: [oom-killed]
matrix:
  - { harness: oracle, model: oracle }
`
	p := filepath.Join(dir, "s.yaml")
	if err := os.WriteFile(p, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err != nil {
		t.Fatalf("valid suite failed to load: %v", err)
	}

	bad := good + "bogus: 1\n"
	if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestValidateRequiresFields(t *testing.T) {
	cases := []*Suite{
		{Tier: "t", Seeds: 1, Scenarios: []string{"a"}, Matrix: []Cell{{Harness: "h", Model: "m"}}},            // no name
		{Name: "n", Seeds: 1, Scenarios: []string{"a"}, Matrix: []Cell{{Harness: "h", Model: "m"}}},            // no tier
		{Name: "n", Tier: "t", Seeds: 0, Scenarios: []string{"a"}, Matrix: []Cell{{Harness: "h", Model: "m"}}}, // seeds<1
		{Name: "n", Tier: "t", Seeds: 1, Matrix: []Cell{{Harness: "h", Model: "m"}}},                           // no scenarios
		{Name: "n", Tier: "t", Seeds: 1, Scenarios: []string{"a"}},                                             // no matrix
		{Name: "n", Tier: "t", Seeds: 1, Scenarios: []string{"a"}, Matrix: []Cell{{Model: "m"}}},               // cell no harness
	}
	for i, s := range cases {
		if err := s.Validate(); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}
