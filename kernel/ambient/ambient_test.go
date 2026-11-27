package ambient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

var _ Slot = (*Var[int])(nil)

func TestVarBasicNestingAndIsolation(t *testing.T) {
	v1 := New[string]()
	v2 := New[string]()
	bg := context.Background()

	if got, ok := v1.Current(bg); ok || got != "" {
		t.Fatalf("empty Current = (%q, %v)", got, ok)
	}
	if got, ok := v1.Outer(bg); ok || got != "" {
		t.Fatalf("empty Outer = (%q, %v)", got, ok)
	}
	if d := v1.Depth(bg); d != 0 {
		t.Fatalf("empty Depth = %d", d)
	}

	c1 := v1.With(bg, "level-1")
	c2 := v1.With(c1, "level-2")
	c3 := v1.With(c2, "") // explicit zero value must still be present
	cOther := v2.With(c2, "other-var")

	// Distinct Vars of the same type never collide.
	if got, ok := v2.Current(c2); ok || got != "" {
		t.Fatalf("v2 leaked from v1: (%q, %v)", got, ok)
	}
	if got, ok := v1.Current(cOther); !ok || got != "level-2" {
		t.Fatalf("v1 overwritten by v2: (%q, %v)", got, ok)
	}
	if got, ok := v2.Current(cOther); !ok || got != "other-var" {
		t.Fatalf("v2 missing: (%q, %v)", got, ok)
	}

	// Verify immutable chain at each level.
	if got, ok := v1.Current(c1); !ok || got != "level-1" || v1.Depth(c1) != 1 {
		t.Fatalf("c1 state wrong: %q %v %d", got, ok, v1.Depth(c1))
	}
	if _, ok := v1.Outer(c1); ok {
		t.Fatal("c1 should have no outer")
	}

	if got, ok := v1.Current(c2); !ok || got != "level-2" || v1.Depth(c2) != 2 {
		t.Fatalf("c2 state wrong: %q %v %d", got, ok, v1.Depth(c2))
	}
	if out, ok := v1.Outer(c2); !ok || out != "level-1" {
		t.Fatalf("c2 outer wrong: %q %v", out, ok)
	}

	if got, ok := v1.Current(c3); !ok || got != "" || v1.Depth(c3) != 3 {
		t.Fatalf("c3 zero value not preserved: %q %v %d", got, ok, v1.Depth(c3))
	}
	if out, ok := v1.Outer(c3); !ok || out != "level-2" {
		t.Fatalf("c3 outer wrong: %q %v", out, ok)
	}

	// Parent contexts remain untouched.
	if got, ok := v1.Current(c1); !ok || got != "level-1" || v1.Depth(c1) != 1 {
		t.Fatalf("c1 mutated: %q %v %d", got, ok, v1.Depth(c1))
	}
}

func TestVarClearAndReenter(t *testing.T) {
	v := New[int]()
	bg := context.Background()

	// Clear on unset context is a no-op returning the same context.
	if cleared := v.Clear(bg); cleared != bg {
		t.Fatal("Clear on empty context wrapped unnecessarily")
	}

	c1 := v.With(bg, 10)
	c2 := v.With(c1, 20)
	cleared := v.Clear(c2)

	if got, ok := v.Current(cleared); ok || got != 0 {
		t.Fatalf("cleared Current = (%d, %v)", got, ok)
	}
	if got, ok := v.Outer(cleared); ok || got != 0 {
		t.Fatalf("cleared Outer = (%d, %v)", got, ok)
	}
	if d := v.Depth(cleared); d != 0 {
		t.Fatalf("cleared Depth = %d", d)
	}
	// Calling Clear again on an already-cleared context is a no-op.
	if again := v.Clear(cleared); again != cleared {
		t.Fatal("Clear on already-cleared context wrapped unnecessarily")
	}

	// Pushing onto a cleared context starts a fresh chain at depth 1.
	fresh := v.With(cleared, 99)
	if got, ok := v.Current(fresh); !ok || got != 99 || v.Depth(fresh) != 1 {
		t.Fatalf("fresh after Clear = (%d, %v, depth=%d)", got, ok, v.Depth(fresh))
	}
	if _, ok := v.Outer(fresh); ok {
		t.Fatal("fresh after Clear exposed pre-clear outer")
	}

	// Original c2 stays completely intact.
	if got, ok := v.Current(c2); !ok || got != 20 || v.Depth(c2) != 2 {
		t.Fatalf("c2 mutated by Clear: %d %v %d", got, ok, v.Depth(c2))
	}
	if out, ok := v.Outer(c2); !ok || out != 10 {
		t.Fatalf("c2 outer mutated by Clear: %d %v", out, ok)
	}
}

func TestVarScopeAndScopeResult(t *testing.T) {
	v := New[string]()
	root := v.With(context.Background(), "outer")
	sentinel := errors.New("boom")

	if err := v.Scope(root, "inner", nil); err != nil {
		t.Fatalf("nil fn error: %v", err)
	}
	if res, err := ScopeResult[string, int](v, root, "inner", nil); err != nil || res != 0 {
		t.Fatalf("nil ScopeResult = (%d, %v)", res, err)
	}

	err := v.Scope(root, "inner", func(ctx context.Context) error {
		if got, ok := v.Current(ctx); !ok || got != "inner" {
			t.Fatalf("Scope Current = (%q, %v)", got, ok)
		}
		if out, ok := v.Outer(ctx); !ok || out != "outer" {
			t.Fatalf("Scope Outer = (%q, %v)", out, ok)
		}
		if v.Depth(ctx) != 2 {
			t.Fatalf("Scope Depth = %d", v.Depth(ctx))
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Scope error = %v", err)
	}
	if got, ok := v.Current(root); !ok || got != "outer" || v.Depth(root) != 1 {
		t.Fatalf("root changed after Scope: %q %v %d", got, ok, v.Depth(root))
	}

	n, err := ScopeResult(v, root, "typed", func(ctx context.Context) (int, error) {
		got, ok := v.Current(ctx)
		if !ok || got != "typed" {
			return 0, fmt.Errorf("unexpected %q %v", got, ok)
		}
		return len(got), nil
	})
	if err != nil || n != 5 {
		t.Fatalf("ScopeResult = (%d, %v)", n, err)
	}
}

func TestVarNilContextAndUninitialized(t *testing.T) {
	v := New[string]()

	if got, ok := v.Current(nil); ok || got != "" {
		t.Fatalf("nil ctx Current = (%q, %v)", got, ok)
	}
	if got, ok := v.Outer(nil); ok || got != "" {
		t.Fatalf("nil ctx Outer = (%q, %v)", got, ok)
	}
	if d := v.Depth(nil); d != 0 {
		t.Fatalf("nil ctx Depth = %d", d)
	}

	c := v.With(nil, "from-nil")
	if c == nil {
		t.Fatal("With(nil) returned nil")
	}
	if got, ok := v.Current(c); !ok || got != "from-nil" || v.Depth(c) != 1 {
		t.Fatalf("With(nil) state = (%q, %v, %d)", got, ok, v.Depth(c))
	}

	cleared := v.Clear(nil)
	if cleared == nil {
		t.Fatal("Clear(nil) returned nil")
	}
	if _, ok := v.Current(cleared); ok {
		t.Fatal("Clear(nil) had value")
	}

	if err := v.Scope(nil, "scoped-nil", func(ctx context.Context) error {
		if got, ok := v.Current(ctx); !ok || got != "scoped-nil" {
			return fmt.Errorf("bad %q %v", got, ok)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Uninitialized and nil Vars fail closed on reads and panic on writes.
	for _, bad := range []*Var[int]{nil, {}} {
		if _, ok := bad.Current(context.Background()); ok {
			t.Fatal("uninitialized Current returned true")
		}
		if _, ok := bad.Outer(context.Background()); ok {
			t.Fatal("uninitialized Outer returned true")
		}
		if bad.Depth(context.Background()) != 0 {
			t.Fatal("uninitialized Depth non-zero")
		}
		for name, call := range map[string]func(){
			"With":        func() { _ = bad.With(context.Background(), 1) },
			"Clear":       func() { _ = bad.Clear(context.Background()) },
			"Scope":       func() { _ = bad.Scope(context.Background(), 1, nil) },
			"ScopeResult": func() { _, _ = ScopeResult[int, int](bad, context.Background(), 1, nil) },
		} {
			func() {
				defer func() {
					if recover() == nil {
						t.Fatalf("%s on uninitialized Var did not panic", name)
					}
				}()
				call()
			}()
		}
	}
}

func TestVarConcurrentCopyOnWrite(t *testing.T) {
	v := New[string]()
	parent := v.With(context.Background(), "root")

	const workers = 64
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for i := range workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			tag := fmt.Sprintf("worker-%d", id)
			child := v.With(parent, tag)
			if got, ok := v.Current(child); !ok || got != tag || v.Depth(child) != 2 {
				errs <- fmt.Errorf("worker %d child = (%q, %v, %d)", id, got, ok, v.Depth(child))
				return
			}
			if out, ok := v.Outer(child); !ok || out != "root" {
				errs <- fmt.Errorf("worker %d child outer = (%q, %v)", id, out, ok)
				return
			}

			grandTag := tag + "-sub"
			if err := v.Scope(child, grandTag, func(gc context.Context) error {
				if got, ok := v.Current(gc); !ok || got != grandTag || v.Depth(gc) != 3 {
					return fmt.Errorf("worker %d grand = (%q, %v, %d)", id, got, ok, v.Depth(gc))
				}
				if out, ok := v.Outer(gc); !ok || out != tag {
					return fmt.Errorf("worker %d grand outer = (%q, %v)", id, out, ok)
				}
				cleared := v.Clear(gc)
				if _, ok := v.Current(cleared); ok || v.Depth(cleared) != 0 {
					return fmt.Errorf("worker %d cleared not empty", id)
				}
				restarted := v.With(cleared, "fresh")
				if got, ok := v.Current(restarted); !ok || got != "fresh" || v.Depth(restarted) != 1 {
					return fmt.Errorf("worker %d restarted wrong", id)
				}
				return nil
			}); err != nil {
				errs <- err
				return
			}

			// Parent remains immutable across all concurrent children.
			if got, ok := v.Current(parent); !ok || got != "root" || v.Depth(parent) != 1 {
				errs <- fmt.Errorf("worker %d observed mutated parent: (%q, %v, %d)", id, got, ok, v.Depth(parent))
			}
		}(i)
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatal(err)
	}
	if got, ok := v.Current(parent); !ok || got != "root" || v.Depth(parent) != 1 {
		t.Fatalf("parent corrupted after concurrent fan-out: (%q, %v, %d)", got, ok, v.Depth(parent))
	}
}
