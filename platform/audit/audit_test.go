package audit

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSecurityLogSurvivesCanceledOperation(t *testing.T) {
	var b bytes.Buffer
	l, _ := NewSecurityLog(&b)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Record{Action: "books.create", Resource: "book:1", Result: "denied", Trace: "op-1"}
	if err := l.Write(ctx, r); err != nil || !strings.Contains(b.String(), "denied") {
		t.Fatal(err, b.String())
	}
	r.Action = "Bearer secret"
	if !errors.Is(l.Write(ctx, r), ErrInvalid) {
		t.Fatal("raw data accepted")
	}
	if _, err := NewSecurityLog(nil); err == nil {
		t.Fatal("nil writer")
	}
	if err := l.Write(nil, Record{}); err == nil {
		t.Fatal("nil context")
	}
	if err := (Recorder{}).Record(context.Background(), Record{}); err == nil {
		t.Fatal("invalid record")
	}
}
