// Package catalog owns the published catalog contract. Its module registers
// its implementation with an ordinary constructor and exports the separate
// contracts package interface from the owning module; consumers depend only
// on this module and resolve the contract.
package catalog

import (
	"context"
	"fmt"

	"example.com/moduleapp/catalog/contracts"
	"example.com/moduleapp/store"
	"github.com/cymondez/pangolin-framework/kernel/module"
)

// Service is the catalog implementation. The concrete type serves the
// contracts.Catalog interface, but only this owner's export exposes it.
type Service struct {
	store *store.Store
}

// NewService is the constructor registered by the catalog module.
func NewService(s *store.Store) *Service {
	return &Service{store: s}
}

// Put writes through to the shared store and mirrors the item in the catalog.
func (s *Service) Put(id, title string) error {
	if err := s.store.Put(id, title); err != nil {
		return err
	}
	return nil
}

// Get reads through to the shared store.
func (s *Service) Get(id string) (contracts.Item, bool) {
	title, ok := s.store.Get(id)
	return contracts.Item{ID: id, Title: title}, ok
}

// Len reports the number of catalog entries.
func (s *Service) Len() int { return s.store.Len() }

// Module is the catalog root-mode module instance. It is the contract owner
// for contracts.Catalog: contract exports are centralized here.
type Module struct{}

func (m *Module) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "catalog", Version: "0.1.0"}
}

// DependsOn declares the direct Type dependency on the shared store.
func (m *Module) DependsOn() []module.Ref {
	return []module.Ref{module.Of[store.Module]()}
}

func (m *Module) Configure(b module.RootBuilder) error {
	if err := b.Services().Add(NewService, module.Singleton()); err != nil {
		return err
	}
	// The contract owner exports its own interface contract; one owner per
	// contract type.
	return b.Services().Export(module.Contract[contracts.Catalog]())
}

func (m *Module) Start(ctx context.Context, rt module.Runtime) error {
	value, err := rt.Services().Resolve(ctx, module.Contract[contracts.Catalog]())
	if err != nil {
		return err
	}
	service, ok := value.(contracts.Catalog)
	if !ok {
		return fmt.Errorf("catalog: implementation for contract is not registered: %T", value)
	}
	if err := service.Put("demo", "root-composed"); err != nil {
		return err
	}
	item, ok := service.Get("demo")
	fmt.Printf("start catalog via %T: %v/%v=%q\n", value, item.ID, ok, item.Title)
	return nil
}

func (m *Module) Stop(context.Context, module.Runtime) error {
	fmt.Println("stop catalog")
	return nil
}
