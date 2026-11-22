package bootstrap

import (
	"bytes"
	"context"
	"testing"
)

func TestStartupRejectsInvalidConfiguration(t *testing.T) {
	for _, args := range [][]string{{"-unknown"}, {"positional"}, {"-run-for", "-1s"}} {
		if e := Run(context.Background(), "api", args, new(bytes.Buffer), nil); e == nil {
			t.Fatal(args)
		}
	}
	if e := Run(context.Background(), "other", nil, new(bytes.Buffer), nil); e == nil {
		t.Fatal("unknown mode")
	}
	t.Setenv("PANGOLIN_DSN", "")
	if e := Run(context.Background(), "api", nil, new(bytes.Buffer), nil); e == nil {
		t.Fatal("missing database")
	}
}
