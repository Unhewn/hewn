package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type collectingIO struct {
	chunks []string
}

func (c *collectingIO) Output(chunk string) { c.chunks = append(c.chunks, chunk) }

func TestBash_Success(t *testing.T) {
	sb := newTestSandbox(t)
	b := NewBash(sb, nil)
	out := &collectingIO{}

	result, err := b.Execute(context.Background(), json.RawMessage(`{"command":"echo hello"}`), out)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute() IsError = true, output: %s", result.Output)
	}
	if !strings.Contains(result.Output, "hello") {
		t.Errorf("Execute() Output = %q, want to contain %q", result.Output, "hello")
	}
	if len(out.chunks) == 0 {
		t.Error("Output() was never called; expected streamed chunks")
	}
}

func TestBash_NonZeroExit(t *testing.T) {
	sb := newTestSandbox(t)
	b := NewBash(sb, nil)

	result, err := b.Execute(context.Background(), json.RawMessage(`{"command":"exit 1"}`), &collectingIO{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("Execute(exit 1) IsError = false, want true")
	}
}

func TestBash_Cancellation(t *testing.T) {
	sb := newTestSandbox(t)
	b := NewBash(sb, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	result, err := b.Execute(ctx, json.RawMessage(`{"command":"sleep 10"}`), &collectingIO{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("Execute(canceled) IsError = false, want true")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Execute took %s, want cancellation well under the 10s sleep", elapsed)
	}
}

func TestBash_BadParams(t *testing.T) {
	sb := newTestSandbox(t)
	b := NewBash(sb, nil)

	result, err := b.Execute(context.Background(), json.RawMessage(`not json`), &collectingIO{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("Execute(bad params) IsError = false, want true")
	}
}
