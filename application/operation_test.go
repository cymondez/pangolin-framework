package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/uow"
)

type manager struct {
	options     uow.Options
	commitErr   error
	withinCalls int
}

func (m *manager) Within(ctx context.Context, o uow.Options, f func(context.Context) error) error {
	m.withinCalls++
	m.options = o
	if err := f(ctx); err != nil {
		return err
	}
	return m.commitErr
}
func (m *manager) Current(context.Context) (uow.Scope, bool) { return nil, false }

type customManager struct {
	withinCalls int
}

func (m *customManager) Within(ctx context.Context, _ uow.Options, callback func(context.Context) error) error {
	if m != nil {
		m.withinCalls++
	}
	return callback(ctx)
}

func (m *customManager) Current(context.Context) (uow.Scope, bool) { return nil, false }

type customContext struct{}

var customContextValueCalls int

func (*customContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*customContext) Done() <-chan struct{}       { return nil }
func (*customContext) Err() error                  { return nil }
func (*customContext) Value(any) any {
	customContextValueCalls++
	return nil
}

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

func TestNilCallbacks(t *testing.T) {
	m := &manager{}

	if err := Command(context.Background(), m, nil); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatalf("nil command callback: %v", err)
	}
	if m.withinCalls != 0 {
		t.Fatalf("nil command callback entered UOW: calls=%d", m.withinCalls)
	}

	if err := Query(context.Background(), m, nil); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatalf("nil query callback: %v", err)
	}
	if m.withinCalls != 0 {
		t.Fatalf("nil query callback entered UOW: calls=%d", m.withinCalls)
	}

	commandValue, err := CommandResult[int](context.Background(), m, nil)
	if !errors.Is(err, uow.ErrOptionsInvalid) || commandValue != 0 {
		t.Fatalf("nil command result callback: value=%d err=%v", commandValue, err)
	}
	if m.withinCalls != 0 {
		t.Fatalf("nil command result callback entered UOW: calls=%d", m.withinCalls)
	}

	queryValue, err := QueryResult[string](context.Background(), m, nil)
	if !errors.Is(err, uow.ErrOptionsInvalid) || queryValue != "" {
		t.Fatalf("nil query result callback: value=%q err=%v", queryValue, err)
	}
	if m.withinCalls != 0 {
		t.Fatalf("nil query result callback entered UOW: calls=%d", m.withinCalls)
	}
}

func TestNilAndTypedNilInputs(t *testing.T) {
	t.Run("literal nil context", func(t *testing.T) {
		m := &customManager{}
		called := 0
		if err := Command(nil, m, func(context.Context) error { called++; return nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
			t.Fatalf("Command(nil context): %v", err)
		}
		if err := Query(nil, m, func(context.Context) error { called++; return nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
			t.Fatalf("Query(nil context): %v", err)
		}
		commandValue, err := CommandResult[int](nil, m, func(context.Context) (int, error) { called++; return 42, nil })
		if !errors.Is(err, uow.ErrOptionsInvalid) || commandValue != 0 {
			t.Fatalf("CommandResult(nil context): value=%d err=%v", commandValue, err)
		}
		queryValue, err := QueryResult[string](nil, m, func(context.Context) (string, error) { called++; return "book", nil })
		if !errors.Is(err, uow.ErrOptionsInvalid) || queryValue != "" {
			t.Fatalf("QueryResult(nil context): value=%q err=%v", queryValue, err)
		}
		if called != 0 || m.withinCalls != 0 {
			t.Fatalf("nil context entered code: callback=%d manager=%d", called, m.withinCalls)
		}
	})

	t.Run("literal nil manager", func(t *testing.T) {
		called := 0
		if err := Command(context.Background(), nil, func(context.Context) error { called++; return nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
			t.Fatalf("Command(nil manager): %v", err)
		}
		if err := Query(context.Background(), nil, func(context.Context) error { called++; return nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
			t.Fatalf("Query(nil manager): %v", err)
		}
		commandValue, err := CommandResult[int](context.Background(), nil, func(context.Context) (int, error) { called++; return 42, nil })
		if !errors.Is(err, uow.ErrOptionsInvalid) || commandValue != 0 {
			t.Fatalf("CommandResult(nil manager): value=%d err=%v", commandValue, err)
		}
		queryValue, err := QueryResult[string](context.Background(), nil, func(context.Context) (string, error) { called++; return "book", nil })
		if !errors.Is(err, uow.ErrOptionsInvalid) || queryValue != "" {
			t.Fatalf("QueryResult(nil manager): value=%q err=%v", queryValue, err)
		}
		if called != 0 {
			t.Fatalf("nil manager entered callback: calls=%d", called)
		}
	})

	t.Run("typed nil manager", func(t *testing.T) {
		var m *customManager
		called := 0
		if err := Command(context.Background(), m, func(context.Context) error { called++; return nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
			t.Fatalf("Command(typed-nil manager): %v", err)
		}
		if err := Query(context.Background(), m, func(context.Context) error { called++; return nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
			t.Fatalf("Query(typed-nil manager): %v", err)
		}
		commandValue, err := CommandResult[int](context.Background(), m, func(context.Context) (int, error) { called++; return 42, nil })
		if !errors.Is(err, uow.ErrOptionsInvalid) || commandValue != 0 {
			t.Fatalf("CommandResult(typed-nil manager): value=%d err=%v", commandValue, err)
		}
		queryValue, err := QueryResult[string](context.Background(), m, func(context.Context) (string, error) { called++; return "book", nil })
		if !errors.Is(err, uow.ErrOptionsInvalid) || queryValue != "" {
			t.Fatalf("QueryResult(typed-nil manager): value=%q err=%v", queryValue, err)
		}
		if called != 0 {
			t.Fatalf("typed-nil manager entered callback: calls=%d", called)
		}
	})

	t.Run("typed nil context", func(t *testing.T) {
		m := &customManager{}
		var ctx *customContext
		called := 0
		customContextValueCalls = 0
		if err := Command(ctx, m, func(context.Context) error { called++; return nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
			t.Fatalf("Command(typed-nil context): %v", err)
		}
		if err := Query(ctx, m, func(context.Context) error { called++; return nil }); !errors.Is(err, uow.ErrOptionsInvalid) {
			t.Fatalf("Query(typed-nil context): %v", err)
		}
		commandValue, err := CommandResult[int](ctx, m, func(context.Context) (int, error) { called++; return 42, nil })
		if !errors.Is(err, uow.ErrOptionsInvalid) || commandValue != 0 {
			t.Fatalf("CommandResult(typed-nil context): value=%d err=%v", commandValue, err)
		}
		queryValue, err := QueryResult[string](ctx, m, func(context.Context) (string, error) { called++; return "book", nil })
		if !errors.Is(err, uow.ErrOptionsInvalid) || queryValue != "" {
			t.Fatalf("QueryResult(typed-nil context): value=%q err=%v", queryValue, err)
		}
		if called != 0 || m.withinCalls != 0 || customContextValueCalls != 0 {
			t.Fatalf("typed-nil context entered code: callback=%d manager=%d observability=%d", called, m.withinCalls, customContextValueCalls)
		}
	})
}

func TestCommandAndQueryResult(t *testing.T) {
	m := &manager{}
	commandValue, err := CommandResult(context.Background(), m, func(context.Context) (int, error) {
		return 42, nil
	})
	if err != nil || commandValue != 42 || m.options.Mode != uow.ReadWrite || m.options.Transaction != uow.Transactional {
		t.Fatalf("command result: value=%d err=%v options=%+v", commandValue, err, m.options)
	}

	queryValue, err := QueryResult(context.Background(), m, func(context.Context) (string, error) {
		return "book", nil
	})
	if err != nil || queryValue != "book" || m.options.Mode != uow.ReadOnly {
		t.Fatalf("query result: value=%q err=%v options=%+v", queryValue, err, m.options)
	}

	commitErr := errors.New("commit failed")
	m.commitErr = commitErr
	value, err := CommandResult(context.Background(), m, func(context.Context) (int, error) {
		return 99, nil
	})
	if !errors.Is(err, commitErr) || value != 0 {
		t.Fatalf("commit error: value=%d err=%v", value, err)
	}

	callbackErr := errors.New("callback failed")
	m.commitErr = nil
	value, err = QueryResult(context.Background(), m, func(context.Context) (int, error) {
		return 100, callbackErr
	})
	if !errors.Is(err, callbackErr) || value != 0 {
		t.Fatalf("callback error: value=%d err=%v", value, err)
	}

	value, err = CommandResult(context.Background(), nil, func(context.Context) (int, error) {
		return 99, nil
	})
	if !errors.Is(err, uow.ErrOptionsInvalid) || value != 0 {
		t.Fatalf("nil manager: value=%d err=%v", value, err)
	}
	_, err = QueryResult(context.Background(), nil, func(context.Context) (string, error) {
		return "ignored", nil
	})
	if !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatalf("nil query manager: %v", err)
	}
}
