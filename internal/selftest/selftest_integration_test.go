package selftest_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pshima/sre-field-tests/internal/bootstrap"
	"github.com/pshima/sre-field-tests/internal/scenario"
	"github.com/pshima/sre-field-tests/internal/selftest"

	_ "github.com/pshima/sre-field-tests/internal/inject" // register fault drivers
)

// TestOOMScenarioSelfTest is the end-to-end guard that the oom-killed scenario
// matches its description: the fault manifests, a no-op stays broken, and the
// oracle recovers it. It requires a working Docker daemon and takes ~60s, so it
// is opt-in via SREFT_DOCKER_IT=1 (CI sets this on a Docker-capable runner).
func TestOOMScenarioSelfTest(t *testing.T) {
	if os.Getenv("SREFT_DOCKER_IT") == "" {
		t.Skip("set SREFT_DOCKER_IT=1 to run the Docker-backed self-test")
	}
	root := "../../scenarios"
	bootstrap.SetScenarioRoot(root)
	spec, err := scenario.LoadDir(root + "/oom-killed")
	if err != nil {
		t.Fatalf("load scenario: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	oracle, err := filepath.Abs(root + "/oom-killed/oracle/fix.override.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := selftest.Run(ctx, spec, "tier0-docker", selftest.Options{
		OracleOverride: oracle,
		// Keep the windows short so CI stays quick while still proving the edge.
		ManifestWindow: 25 * time.Second,
		SustainWindow:  15 * time.Second,
	})
	if err != nil {
		t.Fatalf("self-test run: %v", err)
	}
	for _, c := range rep.Checks {
		t.Logf("[%v] %s: %s", c.Pass, c.Name, c.Detail)
		if !c.Pass {
			t.Errorf("self-test check failed: %s (%s)", c.Name, c.Detail)
		}
	}
	if !rep.Passed {
		t.Fatalf("scenario %s self-test failed", spec.ID)
	}
}
