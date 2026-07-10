// Package suite defines a benchmark suite — a committed, declarative description
// of a run matrix (scenarios × harness/model cells × seeds) — and the manifest
// recording one execution of it. A suite makes a benchmark run reproducible and
// shareable the same way a scenario spec makes a scenario reproducible.
package suite

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Suite is a benchmark definition (suites/<name>.yaml).
type Suite struct {
	// Name identifies the suite (used in the run id).
	Name string `yaml:"name"`
	// Description is human context.
	Description string `yaml:"description,omitempty"`
	// Tier is the infra tier every cell runs at.
	Tier string `yaml:"tier"`
	// Seeds is how many seeds to run per (scenario, cell); seeds are 1..Seeds.
	Seeds int `yaml:"seeds"`
	// Scenarios are the scenario IDs to run.
	Scenarios []string `yaml:"scenarios"`
	// Matrix are the harness/model combinations to run each scenario against.
	Matrix []Cell `yaml:"matrix"`
}

// Cell is one harness/model combination.
type Cell struct {
	Harness string `yaml:"harness"`
	Model   string `yaml:"model"`
}

// Job is one fully-resolved unit of work (one instance to run).
type Job struct {
	Scenario string
	Harness  string
	Model    string
	Seed     int
}

// Load reads and validates a suite file.
func Load(path string) (*Suite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read suite: %w", err)
	}
	var s Suite
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("parse suite %s: %w", path, err)
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("invalid suite %s: %w", path, err)
	}
	return &s, nil
}

// Validate checks required fields.
func (s *Suite) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("name is required")
	}
	if s.Tier == "" {
		return fmt.Errorf("tier is required")
	}
	if s.Seeds < 1 {
		return fmt.Errorf("seeds must be >= 1")
	}
	if len(s.Scenarios) == 0 {
		return fmt.Errorf("at least one scenario is required")
	}
	if len(s.Matrix) == 0 {
		return fmt.Errorf("at least one matrix cell is required")
	}
	for i, cell := range s.Matrix {
		if cell.Harness == "" {
			return fmt.Errorf("matrix[%d].harness is required", i)
		}
		if cell.Model == "" {
			return fmt.Errorf("matrix[%d].model is required", i)
		}
	}
	return nil
}

// Expand enumerates the jobs, in the order scenarios × matrix × seed. This is
// also the execution order: all of one scenario's cells run before the next
// scenario, and runs are strictly sequential.
func (s *Suite) Expand() []Job {
	var jobs []Job
	for _, scenario := range s.Scenarios {
		for _, cell := range s.Matrix {
			for seed := 1; seed <= s.Seeds; seed++ {
				jobs = append(jobs, Job{Scenario: scenario, Harness: cell.Harness, Model: cell.Model, Seed: seed})
			}
		}
	}
	return jobs
}
