package application

import (
	"context"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"testing"
)

type manager struct{ options uow.Options }

func (m *manager) Within(ctx context.Context, o uow.Options, f func(context.Context) error) error {
	m.options = o
	return f(ctx)
}
func (m *manager) Current(context.Context) (uow.Scope, bool) { return nil, false }
func TestA19(t *testing.T) {
	for _, entry := range []string{"http", "job", "consumer"} {
		t.Run(entry, func(t *testing.T) {
			m := &manager{}
			called := 0
			cb := func(context.Context) error { called++; return nil }
			if e := Command(context.Background(), m, cb); e != nil || m.options.Mode != uow.ReadWrite || m.options.Transaction != uow.Transactional {
				t.Fatal(e, m.options)
			}
			if e := Query(context.Background(), m, cb); e != nil || m.options.Mode != uow.ReadOnly || called != 2 {
				t.Fatal(e, m.options, called)
			}
		})
	}
	if Command(context.Background(), nil, nil) == nil || Query(context.Background(), nil, nil) == nil {
		t.Fatal("nil manager")
	}
}
