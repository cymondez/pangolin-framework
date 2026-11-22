// This local teaching program uses the disposable Bookstore database.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	dsn := os.Getenv("PANGOLIN_DSN")
	if dsn == "" {
		return errors.New("set PANGOLIN_DSN after preparing the Bookstore demo")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	manager, err := uow.NewManager(&postgres.Resource{Pool: pool}, uow.DefaultOptions{DefaultTimeout: 5 * time.Second, CleanupTimeout: 2 * time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 4})
	if err != nil {
		return err
	}
	// Fixed identity is only for this local command-line demonstration. An API
	// supplies IdentityReader from its verified authentication adapter instead.
	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "local-demo", Kind: "user"}, "tenant-a", true
	})
	bound, err := resolver.Bind(ctx, scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	if err != nil {
		return err
	}
	parentID, childID := rand.Text(), rand.Text()
	rollback := errors.New("demonstrate parent rollback")
	var ended context.Context
	var requiredShared, newRoot bool
	err = manager.Within(bound, uow.Options{Mode: uow.ReadWrite}, func(parent context.Context) error {
		ended = parent
		root, _ := manager.Current(parent)
		if err := writeBook(parent, parentID, "Parent transaction"); err != nil {
			return err
		}
		if err := manager.Within(parent, uow.Options{Mode: uow.ReadOnly}, func(child context.Context) error {
			view, _ := manager.Current(child)
			requiredShared = view.ID() == root.ID()
			return nil
		}); err != nil {
			return err
		}
		if err := manager.Within(parent, uow.Options{Mode: uow.ReadWrite, Propagation: uow.RequiresNew}, func(child context.Context) error {
			view, _ := manager.Current(child)
			newRoot = view.ID() != root.ID()
			return writeBook(child, childID, "Independent child transaction")
		}); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		return fmt.Errorf("expected rollback: %w", err)
	}
	var parentCount, childCount int
	err = manager.Within(bound, uow.Options{Mode: uow.ReadOnly}, func(work context.Context) error {
		executor, err := postgres.Current(work, false)
		if err != nil {
			return err
		}
		return executor.QueryRow(work, "SELECT count(*) FILTER(WHERE id=$2),count(*) FILTER(WHERE id=$3) FROM pangolin.books WHERE tenant_id=$1 AND id IN ($2,$3)", "tenant-a", parentID, childID).Scan(&parentCount, &childCount)
	})
	if err != nil {
		return err
	}
	_, stillActive := manager.Current(ended)
	fmt.Println("Required shares root:", requiredShared)
	fmt.Println("RequiresNew has new root:", newRoot)
	fmt.Println("Parent rolled back:", parentCount == 0)
	fmt.Println("Child committed:", childCount == 1)
	fmt.Println("Scope closed:", !stillActive)
	if !requiredShared || !newRoot || parentCount != 0 || childCount != 1 || stillActive {
		return errors.New("unexpected transaction result")
	}
	return nil
}
func writeBook(ctx context.Context, id, title string) error {
	executor, err := postgres.Current(ctx, true)
	if err != nil {
		return err
	}
	binding, _ := scope.Current(ctx)
	_, err = executor.Exec(ctx, "INSERT INTO pangolin.books(tenant_id,id,title,version) VALUES($1,$2,$3,1)", binding.Ref.TenantID, id, title)
	return err
}
