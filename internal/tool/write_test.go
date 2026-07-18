package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWrite_NewFile(t *testing.T) {
	sb := newTestSandbox(t)
	w := NewWrite(sb)

	result, err := w.Execute(context.Background(), json.RawMessage(`{"path":"hello.txt","content":"hi there"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute() IsError = true, output: %s", result.Output)
	}

	data, err := os.ReadFile(filepath.Join(sb.Dir(), "hello.txt"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(data) != "hi there" {
		t.Errorf("written content = %q, want %q", data, "hi there")
	}
}

func TestWrite_CreatesParentDirs(t *testing.T) {
	sb := newTestSandbox(t)
	w := NewWrite(sb)

	result, err := w.Execute(context.Background(), json.RawMessage(`{"path":"a/b/c.txt","content":"nested"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute() IsError = true, output: %s", result.Output)
	}

	data, err := os.ReadFile(filepath.Join(sb.Dir(), "a", "b", "c.txt"))
	if err != nil {
		t.Fatalf("read nested written file: %v", err)
	}
	if string(data) != "nested" {
		t.Errorf("written content = %q, want %q", data, "nested")
	}
}

func TestWrite_OverwritesExisting(t *testing.T) {
	sb := newTestSandbox(t)
	if err := os.WriteFile(filepath.Join(sb.Dir(), "existing.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed existing file: %v", err)
	}

	w := NewWrite(sb)
	result, err := w.Execute(context.Background(), json.RawMessage(`{"path":"existing.txt","content":"new"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute() IsError = true, output: %s", result.Output)
	}

	data, err := os.ReadFile(filepath.Join(sb.Dir(), "existing.txt"))
	if err != nil {
		t.Fatalf("read overwritten file: %v", err)
	}
	if string(data) != "new" {
		t.Errorf("overwritten content = %q, want %q", data, "new")
	}
}

func TestWrite_EscapeRefused(t *testing.T) {
	sb := newTestSandbox(t)
	w := NewWrite(sb)

	result, err := w.Execute(context.Background(), json.RawMessage(`{"path":"../outside.txt","content":"x"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("Execute(path escape) IsError = false, want true")
	}
}

func TestWrite_BadParams(t *testing.T) {
	sb := newTestSandbox(t)
	w := NewWrite(sb)

	result, err := w.Execute(context.Background(), json.RawMessage(`not json`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("Execute(bad params) IsError = false, want true")
	}
}

func TestWrite_MissingPath(t *testing.T) {
	sb := newTestSandbox(t)
	w := NewWrite(sb)

	result, err := w.Execute(context.Background(), json.RawMessage(`{"content":"x"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("Execute(missing path) IsError = false, want true")
	}
}

func TestWrite_Risk(t *testing.T) {
	sb := newTestSandbox(t)
	w := NewWrite(sb)
	if w.Risk() != RiskMutating {
		t.Errorf("Risk() = %v, want RiskMutating", w.Risk())
	}
}
