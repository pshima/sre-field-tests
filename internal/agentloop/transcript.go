package agentloop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/pshima/sre-field-tests/internal/instance"
)

// transcript writes two JSONL streams into the instance directory:
//
//	transcript.jsonl — one ToolCall per tool invocation (inputs + outputs). This
//	  is what the grader reads and what safety command-audit scans, so it holds
//	  actions only, never the model's prose (which would cause false positives).
//	messages.jsonl   — the full conversation (assistant turns), for HELM-style
//	  auditability of how the agent reasoned.
type transcript struct {
	tcFile  *os.File
	msgFile *os.File
	tcEnc   *json.Encoder
	msgEnc  *json.Encoder
}

func newTranscript(dir string) (*transcript, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	tcFile, err := os.OpenFile(filepath.Join(dir, instance.TranscriptFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	msgFile, err := os.OpenFile(filepath.Join(dir, "messages.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		_ = tcFile.Close()
		return nil, err
	}
	return &transcript{tcFile: tcFile, msgFile: msgFile, tcEnc: json.NewEncoder(tcFile), msgEnc: json.NewEncoder(msgFile)}, nil
}

func (t *transcript) toolCall(tc ToolCallMsg, output string, err error) {
	input := map[string]any{}
	if tc.Function.Arguments != "" {
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
	}
	rec := ToolCall{
		TS:     time.Now(),
		Tool:   tc.Function.Name,
		Input:  input,
		Output: output,
	}
	if err != nil {
		rec.Err = err.Error()
	}
	_ = t.tcEnc.Encode(rec)
}

func (t *transcript) message(m ChatMessage) { _ = t.msgEnc.Encode(m) }

func (t *transcript) close() {
	_ = t.tcFile.Close()
	_ = t.msgFile.Close()
}
