package inject

import (
	"context"
	"time"

	"github.com/pshima/sre-field-tests/internal/bootstrap"
	"github.com/pshima/sre-field-tests/internal/scenario"
)

// none is the fault driver for abstention (no-change) scenarios: there is no
// real fault to inject. The system is healthy and the correct behavior is to
// change nothing. Injecting only honors the warm-up delay so the stack settles
// before grading, exactly like the other declaratively-faulted scenarios.
type none struct{}

func init() { Register("none", none{}) }

func (none) Inject(ctx context.Context, env *bootstrap.Env, fault scenario.Fault) error {
	if d := fault.StartDelaySeconds; d > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(d) * time.Second):
		}
	}
	return nil
}

func (none) Clear(ctx context.Context, env *bootstrap.Env, fault scenario.Fault) error {
	return nil
}
