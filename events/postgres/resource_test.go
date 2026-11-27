package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/pangolin-framework/pangolin/persistence/uow"
)

type bareResource struct {
	tx         uow.Transaction
	err        error
	nilContext bool
}

func (r bareResource) Begin(c context.Context, _ uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	if r.nilContext {
		return nil, r.tx, r.err
	}
	return c, r.tx, r.err
}

type bareTransaction struct{}

func (bareTransaction) Commit(context.Context) error   { return nil }
func (bareTransaction) Rollback(context.Context) error { return nil }

type stagedTransaction struct {
	bareTransaction
	flushErr, beforeErr, prepareErr, releaseErr error
	outcome                                     uow.Outcome
}

func (t stagedTransaction) Flush(context.Context) error            { return t.flushErr }
func (t stagedTransaction) BeforeCommit(context.Context) error     { return t.beforeErr }
func (t stagedTransaction) Prepare(context.Context) error          { return t.prepareErr }
func (t stagedTransaction) Release(context.Context) error          { return t.releaseErr }
func (t stagedTransaction) CommitFailureOutcome(error) uow.Outcome { return t.outcome }
func TestResourceForwarding(t *testing.T) {
	first, second := &Resource{Base: bareResource{tx: bareTransaction{}}}, &Resource{Base: bareResource{tx: bareTransaction{}}}
	if first.DatabaseKey() == second.DatabaseKey() {
		t.Fatal("distinct unnamed resources share identity")
	}
	if _, _, e := (&Resource{}).Begin(context.Background(), uow.EffectiveOptions{}); !errors.Is(e, uow.ErrOptionsInvalid) {
		t.Fatal(e)
	}
	cause := errors.New("base failure")
	for _, base := range []bareResource{{err: cause}, {nilContext: true, tx: bareTransaction{}}, {}} {
		r := &Resource{Base: base}
		if r.DatabaseKey() == "" || !r.RequiresTransaction() {
			t.Fatal("fallback capability")
		}
		if _, _, e := r.Begin(context.Background(), uow.EffectiveOptions{}); e == nil {
			t.Fatal("invalid base accepted")
		}
	}
	ctx, tx, e := (&Resource{Base: bareResource{tx: bareTransaction{}}}).Begin(context.Background(), uow.EffectiveOptions{})
	if e != nil || ctx == nil {
		t.Fatal(e)
	}
	b := tx.(*transaction)
	if b.Flush(ctx) != nil || b.BeforeCommit(ctx) != nil || b.Prepare(ctx) != nil || b.Release(ctx) != nil || b.CommitFailureOutcome(cause) != uow.Unknown {
		t.Fatal("optional capabilities")
	}
	staged := stagedTransaction{flushErr: cause, beforeErr: cause, prepareErr: cause, releaseErr: cause, outcome: uow.RolledBack}
	b = &transaction{Transaction: staged}
	for _, e := range []error{b.Flush(ctx), b.BeforeCommit(ctx), b.Prepare(ctx), b.Release(ctx)} {
		if !errors.Is(e, cause) {
			t.Fatal(e)
		}
	}
	if b.CommitFailureOutcome(cause) != uow.RolledBack {
		t.Fatal("commit outcome lost")
	}
	b = &transaction{Transaction: bareTransaction{}, before: []func(context.Context) error{nil}}
	if e = b.BeforeCommit(ctx); !errors.Is(e, uow.ErrOptionsInvalid) {
		t.Fatal(e)
	}
	b.before = []func(context.Context) error{func(context.Context) error { return cause }}
	if e = b.BeforeCommit(ctx); !errors.Is(e, cause) {
		t.Fatal(e)
	}
}
