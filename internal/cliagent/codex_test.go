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

// fakeCodex writes a stub `codex` executable that emits canned --json events and
// writes the -o last-message file, exercising the adapter without the real CLI.
func fakeCodex(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub uses a POSIX shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	script := `#!/usr/bin/env bash
# locate the -o value
outfile=""
prev=""
for a in "$@"; do
  if [ "$prev" = "-o" ]; then outfile="$a"; fi
  prev="$a"
done
printf '%s\n' '{"id":"0","msg":{"type":"task_started"}}'
printf '%s\n' '{"id":"1","msg":{"type":"exec_command_begin","command":["bash","-lc","docker inspect sreft-orders"]}}'
printf '%s\n' '{"id":"2","msg":{"type":"exec_command_begin","command":["bash","-lc","docker compose up -d orders"]}}'
printf '%s\n' '{"id":"3","msg":{"type":"agent_message","message":"unbounded cache leak caused OOM; set CACHE_MAX"}}'
printf '%s\n' '{"id":"4","msg":{"type":"task_complete","last_agent_message":"unbounded cache leak caused OOM; set CACHE_MAX"}}'
if [ -n "$outfile" ]; then
  printf '%s' 'unbounded cache leak caused OOM; set CACHE_MAX and recreated orders' > "$outfile"
fi
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCodexCLIRunParsesTranscriptAndFinalMessage(t *testing.T) {
	bin := fakeCodex(t)
	instanceDir := t.TempDir()
	env := &bootstrap.Env{Project: "oom-killed", Endpoints: map[string]string{}}

	runner := CodexCLI{Bin: bin}
	res, err := runner.Run(context.Background(), env, agentloop.Config{
		Model: "default", SystemPrompt: "You are an SRE.", TaskPrompt: "orders is crashing.",
	}, instanceDir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Codex didn't write submission.json, so it's synthesized from the -o final
	// message: not "submitted", but the RCA is populated and persisted.
	if res.Submission == nil || res.Submission.RootCause == "" {
		t.Fatalf("submission not derived from final message: %+v", res.Submission)
	}
	if res.Iterations != 1 {
		t.Errorf("iterations = %d, want 1 (agent_message count)", res.Iterations)
	}

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
	if calls[0].Tool != "shell" {
		t.Errorf("codex exec not normalized to shell: %+v", calls[0])
	}
	// The command string should carry the full argv so command-audit can scan it.
	if cmd, _ := calls[0].Input["cmd"].(string); cmd == "" {
		t.Errorf("empty command in tool call: %+v", calls[0])
	}
}
