package suite

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ManifestFile is the manifest's name within a run directory.
const ManifestFile = "manifest.json"

// Manifest records one execution of a suite: what was run, in what environment,
// and the outcome of each cell. It is the run's disclosure record — enough to
// reproduce it and to interpret the scorecard.
type Manifest struct {
	RunID      string       `json:"run_id"`
	Suite      string       `json:"suite"`
	SuiteFile  string       `json:"suite_file,omitempty"`
	Tier       string       `json:"tier"`
	GitSHA     string       `json:"git_sha,omitempty"`
	HarnessVer string       `json:"harness_version,omitempty"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt *time.Time   `json:"finished_at,omitempty"`
	TotalCells int          `json:"total_cells"`
	Cells      []CellResult `json:"cells"`
}

// CellResult is the outcome of one job in the run.
type CellResult struct {
	Scenario    string   `json:"scenario"`
	Harness     string   `json:"harness"`
	Model       string   `json:"model"`
	Seed        int      `json:"seed"`
	InstanceID  string   `json:"instance_id,omitempty"`
	Status      string   `json:"status"` // "ok" | "failed"
	Verdict     string   `json:"verdict,omitempty"`
	Composite   float64  `json:"composite"`
	MTTRSeconds *float64 `json:"mttr_seconds,omitempty"`
	FailureMode string   `json:"failure_mode,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// Write persists the manifest into the run directory (created if needed).
func (m *Manifest) Write(runDir string) error {
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runDir, ManifestFile), data, 0o644)
}

// ReadManifest loads a run's manifest.
func ReadManifest(runDir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(runDir, ManifestFile))
	if err != nil {
		return nil, fmt.Errorf("read run manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse run manifest: %w", err)
	}
	return &m, nil
}
