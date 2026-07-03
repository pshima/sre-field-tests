// Package bootstrap stands up (and tears down) the infrastructure that hosts a
// scenario, at a selected tier. v1 implements the local docker-compose tier;
// a Terraform/cloud tier implementing the same interface comes later.
package bootstrap

import (
	"context"
	"errors"

	"github.com/pshima/sre-field-tests/internal/scenario"
)

// ErrNotImplemented is returned by tier drivers that are not built yet.
var ErrNotImplemented = errors.New("bootstrap: not implemented")

// Env describes a live environment after Up succeeds: how to reach the operator
// shell and the service endpoints the agent, injector, and observer need.
type Env struct {
	// Tier is the tier name that produced this env.
	Tier string
	// Project is a driver-specific handle (e.g. the compose project name).
	Project string
	// OperatorContainer is the container ID/name the agent gets a shell in.
	OperatorContainer string
	// Endpoints maps logical names to reachable addresses (e.g. "health" ->
	// "http://localhost:8080/healthz").
	Endpoints map[string]string
}

// Bootstrapper stands up and tears down one infra tier for a scenario.
type Bootstrapper interface {
	// Up provisions the environment described by tier for spec.
	Up(ctx context.Context, spec *scenario.Spec, tier scenario.InfraTier) (*Env, error)
	// Down destroys the environment, freeing all resources.
	Down(ctx context.Context, env *Env) error
}

// For selects a Bootstrapper for a tier kind. Drivers register here as they are
// implemented (M1 adds "docker-compose").
func For(kind string) (Bootstrapper, error) {
	if b, ok := registry[kind]; ok {
		return b, nil
	}
	return nil, errors.New("bootstrap: no driver for tier kind " + kind)
}

var registry = map[string]Bootstrapper{}

// Register adds a Bootstrapper driver under a tier kind.
func Register(kind string, b Bootstrapper) { registry[kind] = b }
