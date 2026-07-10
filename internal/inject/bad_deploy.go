package inject

import (
	"context"
	"time"

	"github.com/pshima/sre-field-tests/internal/bootstrap"
	"github.com/pshima/sre-field-tests/internal/scenario"
)

// badDeploy is the fault driver for the bad-deploy scenario. The failure is the
// broken release itself (RELEASE=v2), declared in the compose file, so the
// environment is fully reproducible from committed files; activation only needs
// to let the warm-up elapse.
type badDeploy struct{}

func init() { Register("bad-deploy", badDeploy{}) }

func (badDeploy) Inject(ctx context.Context, env *bootstrap.Env, fault scenario.Fault) error {
	if d := fault.StartDelaySeconds; d > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(d) * time.Second):
		}
	}
	return nil
}

func (badDeploy) Clear(ctx context.Context, env *bootstrap.Env, fault scenario.Fault) error {
	return nil
}
