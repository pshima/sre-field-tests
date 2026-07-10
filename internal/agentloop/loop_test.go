package agentloop

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pshima/sre-field-tests/internal/bootstrap"
	"github.com/pshima/sre-field-tests/internal/instance"
)

// scriptedClient returns a pre-baked sequence of responses, so the loop can be
// exercised end-to-end without an API key or network.
type scriptedClient struct {
	responses []ChatResponse
	i         int
	seen      []ChatRequest
}

func (c *scriptedClient) Complete(_ context.Context, req ChatRequest) (ChatResponse, error) {
	c.seen = append(c.seen, req)
	r := c.responses[c.i]
	if c.i < len(c.responses)-1 {
		c.i++
	}
	return r, nil
}

// fakeExec records shell commands and returns canned output.
type fakeExec struct{ cmds []string }

func (f *fakeExec) Shell(_ context.Context, _ *bootstrap.Env, cmd string) (string, error) {
	f.cmds = append(f.cmds, cmd)
	return "ok: " + cmd, nil
}
func (f *fakeExec) ReadFile(_ context.Context, _ *bootstrap.Env, path string) (string, error) {
	return "contents of " + path, nil
}
func (f *fakeExec) WriteFile(_ context.Context, _ *bootstrap.Env, _, _ string) error { return nil }

func toolCallResp(id, name, args string) ChatResponse {
	return ChatResponse{Message: ChatMessage{Role: "assistant", ToolCalls: []ToolCallMsg{
		{ID: id, Type: "function", Function: FunctionCall{Name: name, Arguments: args}},
	}}}
}

// TestLoopRunsToolsThenSubmits drives a shell call then a submit, and checks the
// loop writes transcript + submission and returns the parsed submission.
func TestLoopRunsToolsThenSubmits(t *testing.T) {
	client := &scriptedClient{responses: []ChatResponse{
		toolCallResp("1", "shell", `{"cmd":"docker inspect sreft-orders"}`),
		toolCallResp("2", "shell", `{"cmd":"docker compose up -d orders -e CACHE_MAX=64"}`),
		toolCallResp("3", "submit", `{"root_cause":"unbounded cache leak -> OOM","actions_taken":"set CACHE_MAX","postmortem":"bounded the cache"}`),
	}}
	exec := &fakeExec{}
	loop := &Loop{Client: client, Exec: exec}

	dir := t.TempDir()
	env := &bootstrap.Env{OperatorContainer: "sreft-operator"}
	res, err := loop.Run(context.Background(), env, Config{Model: "test/model", MaxIterations: 10}, dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Stopped != "submitted" {
		t.Errorf("stopped = %q, want submitted", res.Stopped)
	}
	if res.Submission == nil || !strings.Contains(res.Submission.RootCause, "unbounded cache") {
		t.Fatalf("submission not parsed: %+v", res.Submission)
	}
	if len(exec.cmds) != 2 {
		t.Errorf("expected 2 shell commands, got %d: %v", len(exec.cmds), exec.cmds)
	}

	// transcript.jsonl should hold 3 tool calls (2 shell + submit).
	tcData, _ := os.ReadFile(filepath.Join(dir, instance.TranscriptFile))
	if n := countLines(string(tcData)); n != 3 {
		t.Errorf("transcript has %d lines, want 3", n)
	}
	// submission.json should be written and parseable.
	subData, err := os.ReadFile(filepath.Join(dir, instance.SubmissionFile))
	if err != nil {
		t.Fatalf("submission.json missing: %v", err)
	}
	var sub Submission
	if err := json.Unmarshal(subData, &sub); err != nil {
		t.Fatalf("submission.json invalid: %v", err)
	}
	// The model should have been given the tool definitions each turn.
	if len(client.seen) == 0 || len(client.seen[0].Tools) != 4 {
		t.Errorf("expected 4 tools offered, got %d", len(client.seen[0].Tools))
	}
}

// TestLoopAccumulatesUsage checks the loop sums per-turn token + $ usage and
// reports it on the Result, and that it opts into usage accounting on the wire.
func TestLoopAccumulatesUsage(t *testing.T) {
	withUsage := func(r ChatResponse, u instance.Usage) ChatResponse { r.Usage = u; return r }
	client := &scriptedClient{responses: []ChatResponse{
		withUsage(toolCallResp("1", "shell", `{"cmd":"ls"}`), instance.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, CostUSD: 0.001}),
		withUsage(toolCallResp("2", "submit", `{"root_cause":"x","actions_taken":"y","postmortem":"z"}`), instance.Usage{PromptTokens: 200, CompletionTokens: 30, TotalTokens: 230, CostUSD: 0.002}),
	}}
	loop := &Loop{Client: client, Exec: &fakeExec{}}
	res, err := loop.Run(context.Background(), &bootstrap.Env{}, Config{Model: "test/model", MaxIterations: 10}, t.TempDir())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Usage.TotalTokens != 350 || res.Usage.PromptTokens != 300 || res.Usage.CompletionTokens != 50 {
		t.Errorf("usage tokens = %+v, want prompt=300 completion=50 total=350", res.Usage)
	}
	if res.Usage.CostUSD < 0.0029 || res.Usage.CostUSD > 0.0031 {
		t.Errorf("usage cost = %v, want ~0.003", res.Usage.CostUSD)
	}
	if len(client.seen) == 0 || client.seen[0].Usage == nil || !client.seen[0].Usage.Include {
		t.Errorf("loop did not request usage accounting (usage.include)")
	}
}

// TestLoopStopsAtMaxIterations ensures a model that never submits is bounded.
func TestLoopStopsAtMaxIterations(t *testing.T) {
	client := &scriptedClient{responses: []ChatResponse{
		toolCallResp("1", "shell", `{"cmd":"ls"}`),
	}}
	loop := &Loop{Client: client, Exec: &fakeExec{}}
	res, err := loop.Run(context.Background(), &bootstrap.Env{OperatorContainer: "op"}, Config{MaxIterations: 3}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if res.Stopped != "max_iterations" || res.Iterations != 3 {
		t.Errorf("got stopped=%q iters=%d, want max_iterations/3", res.Stopped, res.Iterations)
	}
}

func countLines(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}
