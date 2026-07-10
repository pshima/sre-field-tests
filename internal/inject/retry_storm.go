package inject

import (
	"context"
	"time"

	"github.com/pshima/sre-field-tests/internal/bootstrap"
	"github.com/pshima/sre-field-tests/internal/scenario"
)

// retryStorm is the fault driver for the retry-storm scenario. As with the other
// tier-0 scenarios, the failure mechanism is declared in the compose stack — a
// degraded downstream dependency, a fixed worker pool, unbounded retries against
// that dependency, and load beyond the worker count — so it is fully reproducible
// from committed files. Activation only needs to let the warm-up elapse.
type retryStorm struct{}

func init() { Register("retry-storm", retryStorm{}) }

func (retryStorm) Inject(ctx context.Context, env *bootstrap.Env, fault scenario.Fault) error {
	if d := fault.StartDelaySeconds; d > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(d) * time.Second):
		}
	}
	return nil
}

func (retryStorm) Clear(ctx context.Context, env *bootstrap.Env, fault scenario.Fault) error {
	return nil
}
