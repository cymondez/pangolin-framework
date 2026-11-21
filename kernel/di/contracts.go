// Package di defines the M0 provider contracts. A runtime container is delivered in M1.
package di

import (
	"context"
	"reflect"
)

type Lifetime uint8

const (
	Singleton Lifetime = iota
	Transient
	Operation
)

// Token identity is the pair (Name, Type); the registry rejects empty names.
type Token interface {
	Name() string
	Type() reflect.Type
}

// Key preserves the service type at declaration sites without scanning objects.
type Key[T any] struct{ name string }

func NewKey[T any](name string) Key[T] { return Key[T]{name: name} }
func (k Key[T]) Name() string          { return k.name }
func (k Key[T]) Type() reflect.Type    { return reflect.TypeFor[T]() }

type Cleanup func(context.Context) error
type Factory func(context.Context, Resolver) (any, Cleanup, error)

type Provider struct {
	Key       Token
	Lifetime  Lifetime
	DependsOn []Token
	Factory   Factory
}

// Builder cannot resolve services. Replace requires a nonempty reason.
type Builder interface {
	Provide(Provider) error
	Replace(Provider, string) error
}

type Resolver interface {
	Resolve(context.Context, Token) (any, error)
}

type Scope interface {
	Resolver
	Close(context.Context) error
}

type Container interface {
	Resolver
	OpenScope(context.Context) (Scope, error)
	Close(context.Context) error
}

type Registry interface {
	Builder
	Seal(context.Context) (Container, error)
}
