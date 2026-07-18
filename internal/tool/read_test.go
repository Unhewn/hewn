package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/unhewn/hewn/internal/sandbox"
)

func newTestSandbox(t *testing.T) *sandbox.Sandbox {
	t.Helper()
	sb, err := sandbox.New(t.TempDir())
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	t.Cleanup(func() { sb.Close() })
	return sb
}

func TestRead_Success(t *testing.T) {
	sb := newTestSandbox(t)
	if err := os.WriteFile(filepath.Join(sb.Dir(), "hello.txt"), []byte("hi there"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	r := NewRead(sb)
	result, err := r.Execute(context.Background(), json.RawMessage(`{"path":"hello.txt"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute() IsError = true, output: %s", result.Output)
	}
	if result.Output != "hi there" {
		t.Errorf("Execute() Output = %q, want %q", result.Output, "hi there")
	}
}

func TestRead_MissingFile(t *testing.T) {
	sb := newTestSandbox(t)
	r := NewRead(sb)

	result, err := r.Execute(context.Background(), json.RawMessage(`{"path":"missing.txt"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("Execute(missing file) IsError = false, want true")
	}
}

func TestRead_EscapeRefused(t *testing.T) {
	sb := newTestSandbox(t)
	r := NewRead(sb)

	result, err := r.Execute(context.Background(), json.RawMessage(`{"path":"../outside.txt"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("Execute(path escape) IsError = false, want true")
	}
}

func TestRead_BadParams(t *testing.T) {
	sb := newTestSandbox(t)
	r := NewRead(sb)

	result, err := r.Execute(context.Background(), json.RawMessage(`not json`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("Execute(bad params) IsError = false, want true")
	}
}
