package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pshima/sre-field-tests/internal/bootstrap"
	"github.com/pshima/sre-field-tests/internal/instance"
)

// ToolExecutor performs the side-effecting tools against a live environment. It
// is an interface so the loop can be unit-tested with a fake (no Docker) while
// production uses the docker-exec implementation.
type ToolExecutor interface {
	Shell(ctx context.Context, env *bootstrap.Env, cmd string) (string, error)
	ReadFile(ctx context.Context, env *bootstrap.Env, path string) (string, error)
	WriteFile(ctx context.Context, env *bootstrap.Env, path, content string) error
}

// Loop is the neutral agent scaffold. The same Loop drives every model, so a
// scorecard reflects the model rather than a bespoke harness.
type Loop struct {
	Client ChatClient
	Exec   ToolExecutor
	// MaxOutputChars truncates each tool result fed back to the model, bounding
	// context growth under a chatty command. 0 uses a sane default.
	MaxOutputChars int
}

// defaultMaxIterations bounds tool-use turns when the scenario sets none.
const defaultMaxIterations = 40

// Run drives the incident loop, writing transcript.jsonl (tool calls, for
// grading + audit) and messages.jsonl (full conversation, for audit) into
// instanceDir, and submission.json when the agent submits.
func (l *Loop) Run(ctx context.Context, env *bootstrap.Env, cfg Config, instanceDir string) (*Result, error) {
	tw, err := newTranscript(instanceDir)
	if err != nil {
		return nil, err
	}
	defer tw.close()

	maxIter := cfg.MaxIterations
	if maxIter <= 0 {
		maxIter = defaultMaxIterations
	}
	var deadline time.Time
	if cfg.WallClock > 0 {
		deadline = time.Now().Add(cfg.WallClock)
	}

	msgs := []ChatMessage{
		{Role: "system", Content: cfg.SystemPrompt},
		{Role: "user", Content: cfg.TaskPrompt},
	}
	tools := toolDefs()

	for i := 0; i < maxIter; i++ {
		if !deadline.IsZero() && time.Now().After(deadline) {
			return &Result{Iterations: i, Stopped: "wall_clock"}, nil
		}
		resp, err := l.Client.Complete(ctx, ChatRequest{
			Model: cfg.Model, Messages: msgs, Tools: tools,
			Temperature: cfg.Temperature, TopP: cfg.TopP,
		})
		if err != nil {
			return &Result{Iterations: i, Stopped: "error"}, err
		}
		asst := resp.Message
		asst.Role = "assistant"
		msgs = append(msgs, asst)
		tw.message(asst)

		if len(asst.ToolCalls) == 0 {
			// The model replied with prose and no action. Nudge it once toward
			// the tools; the iteration cap prevents an infinite stall.
			msgs = append(msgs, ChatMessage{Role: "user",
				Content: "Use the tools to investigate and fix the issue on the host. When the service is restored, call submit with your root cause and postmortem."})
			continue
		}

		for _, tc := range asst.ToolCalls {
			out, sub, done, terr := l.dispatch(ctx, env, tc)
			out = l.truncate(out)
			tw.toolCall(tc, out, terr)
			msgs = append(msgs, ChatMessage{Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: toolResultContent(out, terr)})
			if done {
				if werr := writeSubmission(instanceDir, sub); werr != nil {
					return &Result{Submission: sub, Iterations: i + 1, Stopped: "submitted"}, werr
				}
				return &Result{Submission: sub, Iterations: i + 1, Stopped: "submitted"}, nil
			}
		}
	}
	return &Result{Iterations: maxIter, Stopped: "max_iterations"}, nil
}

// dispatch executes one tool call, returning its textual output, an optional
// submission, whether the loop should stop, and any execution error.
func (l *Loop) dispatch(ctx context.Context, env *bootstrap.Env, tc ToolCallMsg) (string, *Submission, bool, error) {
	args := map[string]any{}
	if tc.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return fmt.Sprintf("could not parse arguments: %v", err), nil, false, nil
		}
	}
	switch tc.Function.Name {
	case "shell":
		out, err := l.Exec.Shell(ctx, env, str(args["cmd"]))
		return out, nil, false, err
	case "read_file":
		out, err := l.Exec.ReadFile(ctx, env, str(args["path"]))
		return out, nil, false, err
	case "write_file":
		err := l.Exec.WriteFile(ctx, env, str(args["path"]), str(args["content"]))
		if err != nil {
			return "", nil, false, err
		}
		return "wrote " + str(args["path"]), nil, false, nil
	case "submit":
		sub := &Submission{
			RootCause:  str(args["root_cause"]),
			Actions:    str(args["actions_taken"]),
			Postmortem: str(args["postmortem"]),
		}
		return "submission received", sub, true, nil
	default:
		return "unknown tool: " + tc.Function.Name, nil, false, nil
	}
}

func (l *Loop) truncate(s string) string {
	max := l.MaxOutputChars
	if max <= 0 {
		max = 8000
	}
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("\n...[truncated %d bytes]", len(s)-max)
}

func toolResultContent(out string, err error) string {
	if err != nil {
		return fmt.Sprintf("ERROR: %v\n%s", err, out)
	}
	if out == "" {
		return "(no output)"
	}
	return out
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// toolDefs describes the tools exposed to the model. Kept minimal and shell-
// centric to mirror a real on-call operator session.
func toolDefs() []ToolDef {
	strProp := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	return []ToolDef{
		{Type: "function", Function: FunctionDef{
			Name:        "shell",
			Description: "Run a shell command in the operator container (has the docker CLI against the host, plus curl, ps, etc.). Use it to investigate and remediate.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"cmd": strProp("The shell command to run.")},
				"required":   []string{"cmd"},
			},
		}},
		{Type: "function", Function: FunctionDef{
			Name:        "read_file",
			Description: "Read a file from the operator container filesystem.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": strProp("Absolute path to read.")},
				"required":   []string{"path"},
			},
		}},
		{Type: "function", Function: FunctionDef{
			Name:        "write_file",
			Description: "Write content to a file in the operator container filesystem.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    strProp("Absolute path to write."),
					"content": strProp("File content."),
				},
				"required": []string{"path", "content"},
			},
		}},
		{Type: "function", Function: FunctionDef{
			Name:        "submit",
			Description: "Submit your final root-cause analysis and postmortem once the service is restored. Ends the session.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"root_cause":    strProp("The root cause of the incident."),
					"actions_taken": strProp("What you changed to remediate."),
					"postmortem":    strProp("A short blameless postmortem."),
				},
				"required": []string{"root_cause", "actions_taken"},
			},
		}},
	}
}

func writeSubmission(dir string, sub *Submission) error {
	if sub == nil {
		return nil
	}
	data, err := json.MarshalIndent(sub, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, instance.SubmissionFile), data, 0o644)
}
