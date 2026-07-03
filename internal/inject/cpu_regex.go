package inject

import (
	"context"
	"time"

	"github.com/pshima/sre-field-tests/internal/bootstrap"
	"github.com/pshima/sre-field-tests/internal/scenario"
)

// cpuRegex is the fault driver for the cpu-regex scenario. As with cgroup-oom,
// the failure mechanism is declared in the compose file — a CPU-capped service
// with a catastrophically-backtracking WAF rule, plus a load service sending the
// malicious payload — so the environment is fully reproducible from committed
// files. Activation only needs to let the warm-up elapse.
type cpuRegex struct{}

func init() { Register("cpu-regex", cpuRegex{}) }

func (cpuRegex) Inject(ctx context.Context, env *bootstrap.Env, fault scenario.Fault) error {
	if d := fault.StartDelaySeconds; d > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(d) * time.Second):
		}
	}
	return nil
}

func (cpuRegex) Clear(ctx context.Context, env *bootstrap.Env, fault scenario.Fault) error {
	return nil
}
