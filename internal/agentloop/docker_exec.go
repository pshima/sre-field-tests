package agentloop

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/pshima/sre-field-tests/internal/bootstrap"
)

// DockerExec runs the agent's tools inside the scenario's operator container via
// `docker exec`. The operator container holds the docker CLI (against the host
// socket) plus common tools, so a shell command there is exactly the on-call
// operator's vantage point.
type DockerExec struct{}

func (DockerExec) Shell(ctx context.Context, env *bootstrap.Env, cmd string) (string, error) {
	if env.OperatorContainer == "" {
		return "", fmt.Errorf("no operator container in environment")
	}
	out, err := run(ctx, "docker", "exec", env.OperatorContainer, "bash", "-lc", cmd)
	// A non-zero exit is normal operator feedback, not a harness failure: return
	// the output and let the model see the exit via stderr text.
	if ee, ok := err.(*exec.ExitError); ok {
		return out + fmt.Sprintf("\n[exit code %d]", ee.ExitCode()), nil
	}
	return out, err
}

func (DockerExec) ReadFile(ctx context.Context, env *bootstrap.Env, path string) (string, error) {
	return run(ctx, "docker", "exec", env.OperatorContainer, "cat", path)
}

func (DockerExec) WriteFile(ctx context.Context, env *bootstrap.Env, path, content string) error {
	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", env.OperatorContainer, "sh", "-c", "cat > "+shellQuote(path))
	cmd.Stdin = strings.NewReader(content)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("write_file: %w: %s", err, stderr.String())
	}
	return nil
}

func run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// shellQuote single-quotes a path for safe interpolation into sh -c.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
