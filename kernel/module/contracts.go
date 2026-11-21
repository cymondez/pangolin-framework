// Package module defines module registration and lifecycle contracts, not a runtime.
package module

import (
	"context"

	"github.com/cymondez/pangolin-framework/kernel/config"
	"github.com/cymondez/pangolin-framework/kernel/di"
)

type Dependency struct {
	Name              string
	VersionConstraint string
}

type Builder interface {
	di.Builder
	SetDefault(string, string) error
}

// Runtime is a sealed service/config view. It cannot register providers.
type Runtime interface {
	Services() di.Resolver
	Config() config.Reader
}

type Module struct {
	Name               string
	Version            string
	DependsOn          []Dependency
	RequiredScopeKinds []string
	PreConfigure       func(Builder) error
	Configure          func(Builder) error
	PostConfigure      func(Builder) error
	Start              func(context.Context, Runtime) error
	Stop               func(context.Context, Runtime) error
}

// Plan is a detached snapshot, not a mutable handle into a live registry.
type Plan struct {
	Ordered []Module
	Layers  [][]string
}

type Registry interface {
	Register(Module) error
	Plan() (Plan, error)
}
