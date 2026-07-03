package inject

import (
	"context"
	"time"

	"github.com/pshima/sre-field-tests/internal/bootstrap"
	"github.com/pshima/sre-field-tests/internal/scenario"
)

// connPool is the fault driver for the conn-pool scenario. As with the other
// tier-0 scenarios, the failure mechanism is declared in the compose stack — a
// small connection pool, slow queries that hold connections, and load beyond the
// pool size — so it is fully reproducible from committed files. Activation only
// needs to let the warm-up elapse.
type connPool struct{}

func init() { Register("conn-pool", connPool{}) }

func (connPool) Inject(ctx context.Context, env *bootstrap.Env, fault scenario.Fault) error {
	if d := fault.StartDelaySeconds; d > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(d) * time.Second):
		}
	}
	return nil
}

func (connPool) Clear(ctx context.Context, env *bootstrap.Env, fault scenario.Fault) error {
	return nil
}
