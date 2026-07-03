// Package inject applies faults to a running system under test. Fault drivers
// are built on Linux primitives (cgroups v2, stress-ng, tc/netem) and the Docker
// API rather than a Kubernetes-centric framework, so scenarios run in plain
// local Docker. Each driver is selected by scenario.Fault.Kind.
package inject

import (
	"context"
	"errors"

	"github.com/pshima/sre-field-tests/internal/bootstrap"
	"github.com/pshima/sre-field-tests/internal/scenario"
)

// ErrNotImplemented is returned by fault drivers that are not built yet.
var ErrNotImplemented = errors.New("inject: not implemented")

// Injector activates and clears a single fault against a live environment.
type Injector interface {
	// Inject activates the fault. For faults expressed purely through bootstrap
	// (e.g. a cgroup memory limit set at compose time), Inject may only need to
	// start the load that triggers the failure.
	Inject(ctx context.Context, env *bootstrap.Env, fault scenario.Fault) error
	// Clear reverses the fault where possible (best-effort for teardown).
	Clear(ctx context.Context, env *bootstrap.Env, fault scenario.Fault) error
}

// For selects an Injector for a fault kind. Drivers register as implemented
// (M1 adds "cgroup-oom").
func For(kind string) (Injector, error) {
	if i, ok := registry[kind]; ok {
		return i, nil
	}
	return nil, errors.New("inject: no driver for fault kind " + kind)
}

var registry = map[string]Injector{}

// Register adds an Injector driver under a fault kind.
func Register(kind string, i Injector) { registry[kind] = i }
