package cliagent

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/pshima/sre-field-tests/internal/agentloop"
	"github.com/pshima/sre-field-tests/internal/bootstrap"
	"github.com/pshima/sre-field-tests/internal/instance"
)

// fakeClaude writes a stub `claude` executable that emits canned stream-json to
// stdout and writes a submission.json to the --add-dir path — exercising the
// adapter's parse + submission path without invoking the real CLI (no
// subscription spend).
func fakeClaude(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub uses a POSIX shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := `#!/usr/bin/env bash
# locate the --add-dir value
adddir=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--add-dir" ]; then adddir="$a"; fi
  prev="$a"
done
# emit canned stream-json
printf '%s\n' '{"type":"system","subtype":"init"}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"investigating"},{"type":"tool_use","name":"Bash","input":{"command":"docker inspect sreft-orders"},"id":"t1"}]}}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"docker compose up -d orders -e CACHE_MAX=64"},"id":"t2"}]}}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"bounded the cache"}'
# write the required submission
if [ -n "$adddir" ]; then
  cat > "$adddir/submission.json" <<'JSON'
{"root_cause":"unbounded cache leak caused OOM","actions_taken":"set CACHE_MAX and recreated orders","postmortem":"bounded the cache"}
JSON
fi
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestClaudeCLIRunParsesTranscriptAndSubmission(t *testing.T) {
	bin := fakeClaude(t)
	instanceDir := t.TempDir()
	env := &bootstrap.Env{Project: "oom-killed"}
	// give the env a services map so the prompt lists containers
	*env = bootstrap.Env{Project: "oom-killed", Endpoints: map[string]string{}}

	runner := ClaudeCLI{Bin: bin}
	res, err := runner.Run(context.Background(), env, agentloop.Config{
		Model: "default", SystemPrompt: "You are an SRE.", TaskPrompt: "orders is crashing.",
	}, instanceDir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Stopped != "submitted" {
		t.Errorf("stopped = %q, want submitted", res.Stopped)
	}
	if res.Iterations != 2 {
		t.Errorf("iterations = %d, want 2 (assistant turns)", res.Iterations)
	}
	if res.Submission == nil || res.Submission.RootCause == "" {
		t.Fatalf("submission not parsed: %+v", res.Submission)
	}

	// transcript.jsonl should hold 2 tool calls, both normalized to shell/cmd so
	// the safety command-audit can scan them.
	data, err := os.ReadFile(filepath.Join(instanceDir, instance.TranscriptFile))
	if err != nil {
		t.Fatal(err)
	}
	var calls []agentloop.ToolCall
	dec := json.NewDecoder(bytes.NewReader(data))
	for dec.More() {
		var tc agentloop.ToolCall
		if err := dec.Decode(&tc); err != nil {
			t.Fatalf("decode transcript: %v", err)
		}
		calls = append(calls, tc)
	}
	if len(calls) != 2 {
		t.Fatalf("transcript has %d tool calls, want 2", len(calls))
	}
	if calls[0].Tool != "shell" || calls[0].Input["cmd"] == "" {
		t.Errorf("first tool call not normalized to shell/cmd: %+v", calls[0])
	}
	// messages.jsonl (raw stream) should exist for audit.
	if _, err := os.Stat(filepath.Join(instanceDir, "messages.jsonl")); err != nil {
		t.Errorf("messages.jsonl not written: %v", err)
	}
}

// TestClaudeCLISynthesizesSubmission covers the fallback: if the agent doesn't
// write submission.json, one is synthesized from the final result text.
func TestClaudeCLISynthesizesSubmission(t *testing.T) {
	dir := t.TempDir()
	sub, wrote := readOrSynthesizeSubmission(dir, "the CPU was pinned by a regex")
	if wrote {
		t.Errorf("expected synthesized (agent did not write), got agentWrote=true")
	}
	if sub.RootCause == "" {
		t.Errorf("synthesized submission should carry the final text")
	}
	if _, err := os.Stat(filepath.Join(dir, instance.SubmissionFile)); err != nil {
		t.Errorf("synthesized submission not persisted: %v", err)
	}
}
