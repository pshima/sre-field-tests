package inject

import (
	"context"
	"time"

	"github.com/pshima/sre-field-tests/internal/bootstrap"
	"github.com/pshima/sre-field-tests/internal/scenario"
)

// cgroupOOM is the fault driver for the oom-killed scenario. The failure
// mechanism itself is declared in the compose file (a memory cap with swap
// disabled) and the load that drives RSS past the cap is a compose service, so
// activation is essentially "let the warm-up elapse and confirm the fault is
// armed." Keeping the mechanism in the bootstrap keeps the scenario fully
// reproducible from committed files; the injector marks the fault's start time.
type cgroupOOM struct{}

func init() { Register("cgroup-oom", cgroupOOM{}) }

func (cgroupOOM) Inject(ctx context.Context, env *bootstrap.Env, fault scenario.Fault) error {
	if d := fault.StartDelaySeconds; d > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(d) * time.Second):
		}
	}
	return nil
}

func (cgroupOOM) Clear(ctx context.Context, env *bootstrap.Env, fault scenario.Fault) error {
	// The cap and load are torn down with the compose project; nothing to undo.
	return nil
}
