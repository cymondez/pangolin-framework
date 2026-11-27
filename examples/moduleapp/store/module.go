// Package store is the shared diamond dependency of the moduleapp
// demonstration: the catalog and pricing modules both declare a Type
// dependency on it, so the root loader plans and instantiates it exactly once.
package store

import (
	"context"
	"fmt"

	"github.com/cymondez/pangolin-framework/kernel/module"
)

// Store is the shared in-memory data service constructed once by the root DI
// container. Constructors return cleanup values only when they own resources;
// this demo owns none.
type Store struct {
	items map[string]string
}

// NewStore is the constructor registered by the store module.
func NewStore() *Store { return &Store{items: map[string]string{}} }

// Put remembers one entry.
func (s *Store) Put(id, title string) error {
	if id == "" {
		return fmt.Errorf("store: empty id")
	}
	s.items[id] = title
	return nil
}

// Get returns the remembered title for id.
func (s *Store) Get(id string) (string, bool) {
	title, ok := s.items[id]
	return title, ok
}

// Len reports the number of stored entries.
func (s *Store) Len() int { return len(s.items) }

// Module is the store root-mode module instance: zero value, metadata through
// ModuleMetadata, dependencies through DependsOn, ordinary constructor
// registration in Configure and optional lifecycle hooks as interfaces.
type Module struct{}

func (m *Module) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "store", Version: "0.1.0"}
}

func (m *Module) Configure(b module.RootBuilder) error {
	// Ordinary constructor registration with explicit Singleton opt-in; the
	// exact concrete type *Store is the resolution key.
	if err := module.AddSingleton(b.Services(), NewStore); err != nil {
		return err
	}
	// The public concrete service is visible across modules only through the
	// owner's own export (contract-owner-centric visibility).
	return b.Services().Export(module.Contract[*Store]())
}

func (m *Module) Start(ctx context.Context, rt module.Runtime) error {
	// Own-concrete resolution uses the typed façade and restricted per-module resolver.
	s, err := module.Resolve[*Store](ctx, rt.Services())
	if err != nil {
		return err
	}
	fmt.Printf("start store (shared instance %d)\n", s.Len())
	return nil
}

func (m *Module) Stop(context.Context, module.Runtime) error {
	fmt.Println("stop store")
	return nil
}
