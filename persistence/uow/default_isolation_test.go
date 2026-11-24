package uow

import (
	"context"
	"testing"
)

// recordingResource captures the effective options of each Begin.
type recordingResource struct {
	options []EffectiveOptions
}

func (r *recordingResource) Begin(ctx context.Context, o EffectiveOptions) (context.Context, Transaction, error) {
	r.options = append(r.options, o)
	return ctx, &testTx{}, nil
}

func (r *recordingResource) RequiresTransaction() bool { return false }

func TestDefaultIsolation(t *testing.T) {
	ctx := testContext(t)
	for _, tc := range []struct {
		name     string
		defaults DefaultOptions
		options  Options
		want     Isolation
	}{
		{
			name:     "historical-default-stays-read-committed",
			defaults: defaults(),
			options:  Options{Mode: ReadWrite},
			want:     ReadCommitted,
		},
		{
			name:     "explicit-default-isolation-applies",
			defaults: withDefaultIsolation(defaults(), Serializable),
			options:  Options{Mode: ReadWrite},
			want:     Serializable,
		},
		{
			name:     "explicit-request-wins-over-default",
			defaults: withDefaultIsolation(defaults(), Serializable),
			options:  Options{Mode: ReadWrite, Isolation: RepeatableRead},
			want:     RepeatableRead,
		},
		{
			name:     "non-transactional-read-keeps-default-level",
			defaults: withDefaultIsolation(defaults(), RepeatableRead),
			options:  Options{Mode: ReadOnly, Transaction: NonTransactional},
			want:     IsolationDefault,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resource := &recordingResource{}
			m, err := NewManager(resource, tc.defaults)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Within(ctx, tc.options, func(context.Context) error { return nil }); err != nil {
				t.Fatal(err)
			}
			if len(resource.options) != 1 {
				t.Fatalf("begin calls wrong: %d", len(resource.options))
			}
			if got := resource.options[0].Isolation; got != tc.want {
				t.Fatalf("isolation wrong: got %d want %d", got, tc.want)
			}
		})
	}
}

func TestDefaultIsolationInvalidRejected(t *testing.T) {
	resource := &recordingResource{}
	d := defaults()
	d.DefaultIsolation = Isolation(99)
	if _, err := NewManager(resource, d); err == nil {
		t.Fatal("invalid default isolation accepted at construction")
	}
}

func withDefaultIsolation(d DefaultOptions, level Isolation) DefaultOptions {
	d.DefaultIsolation = level
	return d
}
