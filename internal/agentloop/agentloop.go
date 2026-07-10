// Package agentloop is the neutral agent scaffold: a single tool-use loop that
// drives any model (routed via OpenRouter's OpenAI-compatible API) against a
// scenario's operator shell. Every model runs the identical loop and identical
// tools, so results measure the model, not a vendor's harness. The loop records
// a full transcript for auditability.
package agentloop

import (
	"context"
	"errors"
	"time"

	"github.com/pshima/sre-field-tests/internal/bootstrap"
	"github.com/pshima/sre-field-tests/internal/instance"
)

// ErrNotImplemented marks scaffolding not yet built (filled in M2).
var ErrNotImplemented = errors.New("agentloop: not implemented")

// Config parameterizes one agent run.
type Config struct {
	// Model is the OpenRouter model slug to route to.
	Model string
	// Temperature and TopP are the decoding parameters (recorded in metadata).
	Temperature float64
	TopP        float64
	// MaxIterations caps tool-use turns; WallClock caps total time.
	MaxIterations int
	WallClock     time.Duration
	// SystemPrompt frames the agent as an on-call SRE; TaskPrompt is the page.
	SystemPrompt string
	TaskPrompt   string
}

// ToolCall records one tool invocation and its result for the transcript.
type ToolCall struct {
	TS     time.Time      `json:"ts"`
	Tool   string         `json:"tool"`   // "shell" | "read_file" | "write_file" | "submit"
	Input  map[string]any `json:"input"`  // arguments
	Output string         `json:"output"` // captured result (truncated as needed)
	Err    string         `json:"err,omitempty"`
}

// Submission is the agent's final structured output: its diagnosis and the
// postmortem write-up used for the communication sub-score.
type Submission struct {
	RootCause  string `json:"root_cause"`
	Actions    string `json:"actions_taken"`
	Postmortem string `json:"postmortem"`
}

// Result is what a run returns beyond the on-disk transcript.
type Result struct {
	Submission *Submission
	Iterations int
	Stopped    string // "submitted" | "max_iterations" | "wall_clock" | "error"
	// Usage is the accumulated token + $ cost of the run (zero for harnesses
	// that report none, e.g. the reference/reflex baselines).
	Usage instance.Usage
}

// Runner executes the agent loop against an environment, writing transcript,
// messages, and submission artifacts into instanceDir.
type Runner interface {
	Run(ctx context.Context, env *bootstrap.Env, cfg Config, instanceDir string) (*Result, error)
}
