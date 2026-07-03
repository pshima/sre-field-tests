package scenario

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEveryScenarioHasWalkthrough enforces the project standard: every scenario
// directory ships a machine-readable spec.yaml AND a human-facing README.md
// walkthrough (see docs/scenario-walkthrough-template.md). This keeps the two in
// lockstep so no scenario can merge without the doc a person reads to understand
// it. A scenario spec that fails to load is also a failure here.
func TestEveryScenarioHasWalkthrough(t *testing.T) {
	root := filepath.Join("..", "..", "scenarios")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read scenarios dir: %v", err)
	}
	found := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "spec.yaml")); err != nil {
			// A directory without a spec.yaml is not a scenario (e.g. shared
			// assets); skip it rather than fail.
			continue
		}
		found++
		if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
			t.Errorf("scenario %q is missing README.md (walkthrough); see docs/scenario-walkthrough-template.md", e.Name())
		}
		if _, err := LoadDir(dir); err != nil {
			t.Errorf("scenario %q spec does not load/validate: %v", e.Name(), err)
		}
	}
	if found == 0 {
		t.Fatalf("no scenarios found under %s", root)
	}
}
