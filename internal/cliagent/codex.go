package cliagent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pshima/sre-field-tests/internal/agentloop"
	"github.com/pshima/sre-field-tests/internal/bootstrap"
	"github.com/pshima/sre-field-tests/internal/instance"
)

// CodexCLI drives a scenario with the OpenAI Codex CLI (`codex exec`) running
// headlessly on the host with docker access, using its subscription auth. Like
// the Claude adapter it translates the CLI's output into the standard instance
// artifacts the grader understands.
type CodexCLI struct {
	// Bin is the codex executable; defaults to "codex". Injectable for tests.
	Bin string
}

func (c CodexCLI) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return "codex"
}

func (c CodexCLI) Run(ctx context.Context, env *bootstrap.Env, cfg agentloop.Config, instanceDir string) (*agentloop.Result, error) {
	if _, err := exec.LookPath(c.bin()); err != nil {
		return &agentloop.Result{Stopped: "error"}, fmt.Errorf("codex CLI not found (%q): %w", c.bin(), err)
	}
	// Absolute paths: the child's working dir is set to instanceDir, so a
	// relative -C / -o would be resolved against it twice ("os error 2").
	if abs, err := filepath.Abs(instanceDir); err == nil {
		instanceDir = abs
	}
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		return nil, err
	}

	system, user := buildPrompts(cfg, env, instanceDir)
	lastMsgFile := filepath.Join(instanceDir, "codex-last.txt")
	args := []string{
		"exec",
		// Codex has no separate system-prompt flag; prepend the framing.
		system + "\n\n" + user,
		"--json",
		"--dangerously-bypass-approvals-and-sandbox", // autonomous; host is the sandbox
		"--skip-git-repo-check",                      // the instance dir is not a repo
		"-C", instanceDir,
		"-o", lastMsgFile,
	}
	if m := cfg.Model; m != "" && m != "default" {
		args = append(args, "-m", m)
	}

	cmd := exec.CommandContext(ctx, c.bin(), args...)
	cmd.Dir = instanceDir
	cmd.Stderr = os.Stderr
	// `codex exec` reads stdin when it is not a TTY and blocks waiting for EOF.
	// Give it an empty stdin so it proceeds immediately with the prompt arg.
	cmd.Stdin = strings.NewReader("")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	rawFile, err := os.Create(filepath.Join(instanceDir, "messages.jsonl"))
	if err != nil {
		return nil, err
	}
	defer rawFile.Close()
	tf, err := os.Create(filepath.Join(instanceDir, instance.TranscriptFile))
	if err != nil {
		return nil, err
	}
	defer tf.Close()
	tenc := json.NewEncoder(tf)

	if err := cmd.Start(); err != nil {
		return &agentloop.Result{Stopped: "error"}, err
	}
	finalText, iters, parseErr := parseCodexStream(stdout, rawFile, tenc)
	waitErr := cmd.Wait()

	// Codex's -o file is the most reliable source of the final message.
	if b, err := os.ReadFile(lastMsgFile); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		finalText = string(b)
	}

	stopped := "completed"
	if ctx.Err() != nil {
		stopped = "wall_clock"
	} else if waitErr != nil {
		stopped = "error"
	}
	sub, agentWrote := readOrSynthesizeSubmission(instanceDir, finalText)
	if agentWrote {
		stopped = "submitted"
	}

	res := &agentloop.Result{Submission: sub, Iterations: iters, Stopped: stopped}
	if parseErr != nil {
		return res, fmt.Errorf("parse codex stream: %w", parseErr)
	}
	return res, nil
}

// codexEvent is the subset of `codex exec --json` events we consume (Codex
// v0.142). Each event is {"type":"item.started|item.completed|turn.completed|
// ...","item":{"type":"command_execution|agent_message|...","command":..,
// "text":..}}. We record a command when it starts (robust to interruption) and
// take agent messages when they complete.
type codexEvent struct {
	Type string `json:"type"`
	Item *struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Text    string `json:"text"`
	} `json:"item"`
}

func parseCodexStream(r io.Reader, rawFile *os.File, tenc *json.Encoder) (finalText string, iterations int, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		line := sc.Bytes()
		_, _ = rawFile.Write(append(append([]byte{}, line...), '\n'))
		var ev codexEvent
		if json.Unmarshal(line, &ev) != nil || ev.Item == nil {
			continue
		}
		switch ev.Item.Type {
		case "command_execution":
			// Record once, when the command is issued.
			if ev.Type == "item.started" && ev.Item.Command != "" {
				_ = tenc.Encode(agentloop.ToolCall{
					TS: time.Now(), Tool: "shell", Input: map[string]any{"cmd": ev.Item.Command},
				})
				progress("codex", "Bash", map[string]any{"command": ev.Item.Command}, "")
			}
		case "agent_message":
			if ev.Type == "item.completed" && ev.Item.Text != "" {
				iterations++
				finalText = ev.Item.Text
				progress("codex", "", nil, ev.Item.Text)
			}
		}
	}
	return finalText, iterations, sc.Err()
}
