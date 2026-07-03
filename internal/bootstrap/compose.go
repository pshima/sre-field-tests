package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pshima/sre-field-tests/internal/scenario"
)

// composeBootstrapper stands up a scenario's tier-0 environment with
// docker-compose. Orchestration (build ordering, dependency waits, teardown of
// the whole project) is exactly what the compose CLI does well, so this driver
// shells out to it rather than reimplementing it over the raw Engine API. The
// driver stays thin; the compose file is the source of truth.
type composeBootstrapper struct{}

func init() { Register("docker-compose", composeBootstrapper{}) }

// composeService is the subset of `docker compose ps --format json` we consume.
type composeService struct {
	Service    string `json:"Service"`
	Name       string `json:"Name"`
	State      string `json:"State"`
	Publishers []struct {
		URL           string `json:"URL"`
		TargetPort    int    `json:"TargetPort"`
		PublishedPort int    `json:"PublishedPort"`
		Protocol      string `json:"Protocol"`
	} `json:"Publishers"`
}

func (composeBootstrapper) Up(ctx context.Context, spec *scenario.Spec, tier scenario.InfraTier) (*Env, error) {
	dir, err := tierDir(spec, tier)
	if err != nil {
		return nil, err
	}
	// Build and start detached. --build guarantees the SUT image reflects the
	// committed app source; pinning that avoids a stale-image reproducibility trap.
	if out, err := composeRun(ctx, dir, nil, "up", "-d", "--build"); err != nil {
		return nil, fmt.Errorf("compose up: %w: %s", err, out)
	}
	env := &Env{Tier: tier.Kind, Project: spec.ID, Endpoints: map[string]string{}}
	if err := env.refresh(ctx, dir); err != nil {
		return nil, err
	}
	// Resolve the operator container the agent will get a shell in.
	if svc := spec.Task.OperatorService; svc != "" {
		env.OperatorContainer = env.containerFor(svc)
	}
	env.composeDir = dir
	return env, nil
}

func (composeBootstrapper) Down(ctx context.Context, env *Env) error {
	if env == nil || env.composeDir == "" {
		return nil
	}
	// -v removes named/anonymous volumes so a re-run starts from a clean slate.
	if out, err := composeRun(ctx, env.composeDir, nil, "down", "-v", "--remove-orphans"); err != nil {
		return fmt.Errorf("compose down: %w: %s", err, out)
	}
	return nil
}

// refresh populates the service->container map and health/service endpoints from
// `docker compose ps`.
func (e *Env) refresh(ctx context.Context, dir string) error {
	out, err := composeRun(ctx, dir, nil, "ps", "--format", "json")
	if err != nil {
		return fmt.Errorf("compose ps: %w: %s", err, out)
	}
	services, err := parseComposePS(out)
	if err != nil {
		return err
	}
	if e.services == nil {
		e.services = map[string]string{}
	}
	for _, s := range services {
		e.services[s.Service] = s.Name
		// Surface the first published TCP port per service as an http endpoint.
		for _, p := range s.Publishers {
			if p.PublishedPort > 0 && p.Protocol == "tcp" {
				e.Endpoints[s.Service] = fmt.Sprintf("http://localhost:%d", p.PublishedPort)
				break
			}
		}
	}
	return nil
}

// containerFor returns the container name for a compose service.
func (e *Env) containerFor(service string) string { return e.services[service] }

// ComposeDir returns the directory compose commands run in (build contexts and
// override files resolve relative to it).
func (e *Env) ComposeDir() string { return e.composeDir }

// Services returns a copy of the service->container-name map.
func (e *Env) Services() map[string]string {
	out := make(map[string]string, len(e.services))
	for k, v := range e.services {
		out[k] = v
	}
	return out
}

// parseComposePS handles both the NDJSON and JSON-array shapes different compose
// versions emit.
func parseComposePS(out string) ([]composeService, error) {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	if strings.HasPrefix(out, "[") {
		var arr []composeService
		if err := json.Unmarshal([]byte(out), &arr); err != nil {
			return nil, fmt.Errorf("parse compose ps array: %w", err)
		}
		return arr, nil
	}
	var svcs []composeService
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var s composeService
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, fmt.Errorf("parse compose ps line: %w", err)
		}
		svcs = append(svcs, s)
	}
	return svcs, nil
}

// composeRun runs `docker compose [-f extra...] <args>` in dir and returns its
// combined output. extraFiles are additional -f overrides (e.g. the oracle fix),
// layered after the base file.
func composeRun(ctx context.Context, dir string, extraFiles []string, args ...string) (string, error) {
	full := []string{"compose", "-f", "docker-compose.yaml"}
	for _, f := range extraFiles {
		full = append(full, "-f", f)
	}
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, "docker", full...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ComposeExec runs an arbitrary compose subcommand in the env's project dir with
// optional -f overrides. Exposed so the injector, oracle, and self-test can
// drive the same project without duplicating the invocation logic.
func (e *Env) ComposeExec(ctx context.Context, extraFiles []string, args ...string) (string, error) {
	return composeRun(ctx, e.composeDir, extraFiles, args...)
}

// tierDir resolves the absolute directory of a tier's bootstrap, given the
// scenario's own directory is derivable from its ID under scenarios/.
func tierDir(spec *scenario.Spec, tier scenario.InfraTier) (string, error) {
	if tier.Path == "" {
		return "", fmt.Errorf("tier for scenario %q has no path", spec.ID)
	}
	// Scenario directories live at scenarios/<id>; the tier path is relative to
	// that. The scenarios root is resolved by the caller via ScenarioRoot.
	dir := filepath.Join(scenarioRoot, spec.ID, tier.Path)
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// scenarioRoot is the directory scenarios live under; set by SetScenarioRoot so
// the control plane can point at a non-default location.
var scenarioRoot = "scenarios"

// SetScenarioRoot configures where scenario directories are found.
func SetScenarioRoot(root string) { scenarioRoot = root }

// ResolveTierDir returns the absolute bootstrap directory for a scenario tier.
// Exported so the control plane can tear down (or inspect) an environment
// without a live Env handle.
func ResolveTierDir(spec *scenario.Spec, tier scenario.InfraTier) (string, error) {
	return tierDir(spec, tier)
}

// EnvForDir builds a minimal Env pointing at an existing compose project
// directory, sufficient for Down without having called Up in this process.
func EnvForDir(dir string) *Env { return &Env{composeDir: dir} }
