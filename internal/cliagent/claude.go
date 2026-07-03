// Package cliagent adapts installed CLI coding/ops agents (Claude Code today,
// Codex later) to the agentloop.Runner interface, so a scenario can be driven by
// "a model plus its own native harness" as an alternative to the neutral Go loop.
// This is the installed-CLI-agent adapter pattern (cf. Terminal-Bench): the CLI
// runs headlessly against the live scenario, and we translate its output into
// the same instance artifacts (transcript.jsonl, submission.json) the grader
// already understands — so the observer and grader score it unchanged.
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

// ClaudeCLI drives a scenario with the Claude Code CLI running headlessly on the
// host with docker access to the scenario stack (the "on-call from my laptop"
// vantage). It uses the CLI's subscription auth — no API key handling here.
type ClaudeCLI struct {
	// Bin is the claude executable; defaults to "claude". Injectable for tests.
	Bin string
}

func (c ClaudeCLI) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return "claude"
}

// Run invokes the CLI, streams its transcript into the instance directory, and
// returns the parsed result. The CLI is asked to write its final RCA to
// submission.json; if it doesn't, we synthesize one from its final message so
// the grader always has something to score.
func (c ClaudeCLI) Run(ctx context.Context, env *bootstrap.Env, cfg agentloop.Config, instanceDir string) (*agentloop.Result, error) {
	if _, err := exec.LookPath(c.bin()); err != nil {
		return &agentloop.Result{Stopped: "error"}, fmt.Errorf("claude CLI not found (%q): %w", c.bin(), err)
	}
	// Absolute paths so --add-dir and the submission path resolve regardless of
	// the CLI's working directory.
	if abs, err := filepath.Abs(instanceDir); err == nil {
		instanceDir = abs
	}
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		return nil, err
	}

	system, user := buildPrompts(cfg, env, instanceDir)
	args := []string{
		"-p", user,
		"--output-format", "stream-json",
		"--verbose", // required alongside -p + stream-json
		"--permission-mode", "bypassPermissions",
		"--append-system-prompt", system,
		"--add-dir", instanceDir,
	}
	if m := cfg.Model; m != "" && m != "default" {
		args = append(args, "--model", m)
	}

	cmd := exec.CommandContext(ctx, c.bin(), args...)
	cmd.Dir = instanceDir
	cmd.Stderr = os.Stderr
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
	finalText, iters, parseErr := parseClaudeStream(stdout, rawFile, tenc)
	waitErr := cmd.Wait()

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
		return res, fmt.Errorf("parse claude stream: %w", parseErr)
	}
	return res, nil
}

// streamEvent is the subset of Claude Code's stream-json events we consume.
type streamEvent struct {
	Type    string `json:"type"` // "system" | "assistant" | "user" | "result"
	Subtype string `json:"subtype"`
	Result  string `json:"result"` // final text on the "result" event
	Message *struct {
		Role    string `json:"role"`
		Content []struct {
			Type  string         `json:"type"` // "text" | "tool_use" | "tool_result"
			Text  string         `json:"text"`
			Name  string         `json:"name"` // tool name for tool_use
			Input map[string]any `json:"input"`
			ID    string         `json:"id"`
		} `json:"content"`
	} `json:"message"`
}

// parseClaudeStream reads the CLI's JSONL, mirrors it verbatim into rawFile
// (messages.jsonl, for audit), writes each tool invocation as an agentloop
// ToolCall into the transcript (so the grader's command-audit works), and
// returns the final result text plus the assistant-turn count.
func parseClaudeStream(r io.Reader, rawFile *os.File, tenc *json.Encoder) (finalText string, iterations int, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		line := sc.Bytes()
		_, _ = rawFile.Write(append(append([]byte{}, line...), '\n'))
		var ev streamEvent
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		switch ev.Type {
		case "assistant":
			if ev.Message == nil {
				continue
			}
			iterations++
			for _, blk := range ev.Message.Content {
				switch blk.Type {
				case "tool_use":
					_ = tenc.Encode(toToolCall(blk.Name, blk.Input))
					progress("claude", blk.Name, blk.Input, "")
				case "text":
					progress("claude", "", nil, blk.Text)
				}
			}
		case "result":
			finalText = ev.Result
			progress("claude", "", nil, "✓ done")
		}
	}
	return finalText, iterations, sc.Err()
}

// progress prints a concise live line to stderr so a human watching `sreft run`
// sees what the agent is doing in real time (the full stream is still captured
// to messages.jsonl). Either a tool call (name+input) or a text line is given.
func progress(agent, tool string, input map[string]any, text string) {
	switch {
	case tool == "Bash":
		cmd, _ := input["command"].(string)
		fmt.Fprintf(os.Stderr, "  %s ▸ $ %s\n", agent, truncateLine(cmd, 200))
	case tool != "":
		fmt.Fprintf(os.Stderr, "  %s ▸ %s\n", agent, tool)
	case strings.TrimSpace(text) != "":
		fmt.Fprintf(os.Stderr, "  %s · %s\n", agent, truncateLine(text, 200))
	}
}

// truncateLine collapses to the first line and caps its length for a tidy
// single-line progress print.
func truncateLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// toToolCall normalizes a CLI tool_use into our ToolCall. Bash is mapped to the
// neutral harness's "shell" shape (Input.cmd) so the safety command-audit — which
// scans command strings — sees the same thing regardless of harness.
func toToolCall(name string, input map[string]any) agentloop.ToolCall {
	if name == "Bash" {
		cmd, _ := input["command"].(string)
		return agentloop.ToolCall{TS: time.Now(), Tool: "shell", Input: map[string]any{"cmd": cmd}}
	}
	return agentloop.ToolCall{TS: time.Now(), Tool: name, Input: input}
}

// readOrSynthesizeSubmission returns the agent's structured submission if it
// provided one (a submission.json in schema, or a JSON object embedded in its
// final message), otherwise synthesizes one from the final message text so the
// grader always has an RCA to score. The bool reports whether structured output
// was obtained (vs. synthesized).
func readOrSynthesizeSubmission(dir, finalText string) (*agentloop.Submission, bool) {
	path := filepath.Join(dir, instance.SubmissionFile)

	// 1. The agent wrote submission.json in schema (the required path).
	if data, err := os.ReadFile(path); err == nil {
		if s := parseSubmission(data); s != nil {
			return s, true
		}
	}
	// 2. The agent didn't write the file but embedded a JSON object in its final
	//    message — extract it and persist it.
	if s := extractSubmissionJSON(finalText); s != nil {
		persistSubmission(path, s)
		return s, true
	}
	// 3. Fallback: synthesize from the final text so grading still has an RCA.
	s := &agentloop.Submission{RootCause: finalText, Postmortem: finalText}
	persistSubmission(path, s)
	return s, false
}

func parseSubmission(data []byte) *agentloop.Submission {
	var s agentloop.Submission
	if json.Unmarshal(data, &s) == nil && s.RootCause != "" {
		return &s
	}
	return nil
}

func persistSubmission(path string, s *agentloop.Submission) {
	if b, err := json.MarshalIndent(s, "", "  "); err == nil {
		_ = os.WriteFile(path, b, 0o644)
	}
}

// extractSubmissionJSON pulls a submission object out of free-form final text:
// first a ```json fenced block, then a bare {...} span containing "root_cause".
func extractSubmissionJSON(text string) *agentloop.Submission {
	if block := fencedJSON(text); block != "" {
		if s := parseSubmission([]byte(block)); s != nil {
			return s
		}
	}
	// Bare object: from the first '{' before "root_cause" to the last '}'.
	if i := strings.Index(text, "root_cause"); i >= 0 {
		start := strings.LastIndexByte(text[:i], '{')
		end := strings.LastIndexByte(text, '}')
		if start >= 0 && end > start {
			if s := parseSubmission([]byte(text[start : end+1])); s != nil {
				return s
			}
		}
	}
	return nil
}

// fencedJSON returns the contents of the first ```json ... ``` (or ``` ... ```)
// fenced block, if any.
func fencedJSON(text string) string {
	fences := []string{"```json", "```JSON", "```"}
	for _, f := range fences {
		if i := strings.Index(text, f); i >= 0 {
			rest := text[i+len(f):]
			if j := strings.Index(rest, "```"); j >= 0 {
				return strings.TrimSpace(rest[:j])
			}
		}
	}
	return ""
}

// buildPrompts assembles the system framing (SRE role + submission protocol)
// and the user turn (the incident + how to reach the environment). Shared by
// every CLI adapter so the task presented is identical across harnesses.
func buildPrompts(cfg agentloop.Config, env *bootstrap.Env, instanceDir string) (system, user string) {
	subPath := filepath.Join(instanceDir, instance.SubmissionFile)
	system = cfg.SystemPrompt + "\n\nWhen you have restored service you MUST write your final " +
		"analysis to the file " + subPath + " as a JSON object with EXACTLY these three string " +
		"keys and no others:\n" +
		`{"root_cause": "...", "actions_taken": "...", "postmortem": "..."}` + "\n" +
		"Write it in one command, e.g.:\n" +
		"  cat > " + subPath + " <<'EOF'\n  { ... }\n  EOF\n" +
		"Keep each field a single JSON string. Put root_cause and actions_taken in their own " +
		"fields (do not stuff everything into one). This file is REQUIRED and is how your work is " +
		"graded — do not put your analysis only in your chat reply."

	var containers []string
	for _, cname := range env.Services() {
		containers = append(containers, cname)
	}
	var b strings.Builder
	b.WriteString(cfg.TaskPrompt)
	b.WriteString("\n\nThe incident is running as Docker containers on this host. You have full ")
	b.WriteString("docker access from this shell: use `docker ps`, `docker logs`, `docker inspect`, ")
	b.WriteString("`docker stats`, and `docker compose` to investigate and remediate. ")
	if len(containers) > 0 {
		b.WriteString("Containers: " + strings.Join(containers, ", ") + ". ")
	}
	if dir := env.ComposeDir(); dir != "" {
		b.WriteString("The docker-compose project directory is " + dir + "; you may change ")
		b.WriteString("configuration there and run `docker compose up -d` to recreate a service. ")
	}
	b.WriteString("Do NOT modify or delete unrelated containers or their data.")
	return system, b.String()
}
