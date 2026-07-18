package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/unhewn/hewn/internal/sandbox"
)

func writeFixture(t *testing.T, sb *sandbox.Sandbox, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(sb.Dir(), name), []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}
}

func TestEdit_Success(t *testing.T) {
	sb := newTestSandbox(t)
	writeFixture(t, sb, "f.go", "package main\n\nfunc old() {}\n")

	e := NewEdit(sb)
	result, err := e.Execute(context.Background(), json.RawMessage(`{"path":"f.go","old_string":"func old() {}","new_string":"func newFn() {}"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute() IsError = true, output: %s", result.Output)
	}

	data, err := os.ReadFile(filepath.Join(sb.Dir(), "f.go"))
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	want := "package main\n\nfunc newFn() {}\n"
	if string(data) != want {
		t.Errorf("edited content = %q, want %q", data, want)
	}
}

func TestEdit_ReplaceAll(t *testing.T) {
	sb := newTestSandbox(t)
	writeFixture(t, sb, "f.txt", "foo foo foo")

	e := NewEdit(sb)
	result, err := e.Execute(context.Background(), json.RawMessage(`{"path":"f.txt","old_string":"foo","new_string":"bar","replace_all":true}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute() IsError = true, output: %s", result.Output)
	}

	data, err := os.ReadFile(filepath.Join(sb.Dir(), "f.txt"))
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	if string(data) != "bar bar bar" {
		t.Errorf("edited content = %q, want %q", data, "bar bar bar")
	}
}

func TestEdit_AmbiguousMatchRefused(t *testing.T) {
	sb := newTestSandbox(t)
	writeFixture(t, sb, "f.txt", "foo foo")

	e := NewEdit(sb)
	result, err := e.Execute(context.Background(), json.RawMessage(`{"path":"f.txt","old_string":"foo","new_string":"bar"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("Execute(ambiguous match) IsError = false, want true")
	}

	data, err := os.ReadFile(filepath.Join(sb.Dir(), "f.txt"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "foo foo" {
		t.Errorf("file was modified despite ambiguous match: %q", data)
	}
}

func TestEdit_NotFound(t *testing.T) {
	sb := newTestSandbox(t)
	writeFixture(t, sb, "f.txt", "hello")

	e := NewEdit(sb)
	result, err := e.Execute(context.Background(), json.RawMessage(`{"path":"f.txt","old_string":"missing","new_string":"x"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("Execute(not found) IsError = false, want true")
	}
}

func TestEdit_NoOpRefused(t *testing.T) {
	sb := newTestSandbox(t)
	writeFixture(t, sb, "f.txt", "hello")

	e := NewEdit(sb)
	result, err := e.Execute(context.Background(), json.RawMessage(`{"path":"f.txt","old_string":"hello","new_string":"hello"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("Execute(old==new) IsError = false, want true")
	}
}

func TestEdit_MissingFile(t *testing.T) {
	sb := newTestSandbox(t)
	e := NewEdit(sb)

	result, err := e.Execute(context.Background(), json.RawMessage(`{"path":"missing.txt","old_string":"a","new_string":"b"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("Execute(missing file) IsError = false, want true")
	}
}

func TestEdit_BadParams(t *testing.T) {
	sb := newTestSandbox(t)
	e := NewEdit(sb)

	result, err := e.Execute(context.Background(), json.RawMessage(`not json`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("Execute(bad params) IsError = false, want true")
	}
}

func TestEdit_MissingOldString(t *testing.T) {
	sb := newTestSandbox(t)
	writeFixture(t, sb, "f.txt", "hello")
	e := NewEdit(sb)

	result, err := e.Execute(context.Background(), json.RawMessage(`{"path":"f.txt","old_string":"","new_string":"x"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("Execute(empty old_string) IsError = false, want true")
	}
}

func TestEdit_PreservesFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("exec permission bits aren't meaningful on Windows")
	}

	sb := newTestSandbox(t)
	path := filepath.Join(sb.Dir(), "script.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatalf("seed executable fixture: %v", err)
	}

	e := NewEdit(sb)
	result, err := e.Execute(context.Background(), json.RawMessage(`{"path":"script.sh","old_string":"echo old","new_string":"echo new"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute() IsError = true, output: %s", result.Output)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat edited file: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("file mode after edit = %v, want 0755", info.Mode().Perm())
	}
}

func TestEdit_Risk(t *testing.T) {
	sb := newTestSandbox(t)
	e := NewEdit(sb)
	if e.Risk() != RiskMutating {
		t.Errorf("Risk() = %v, want RiskMutating", e.Risk())
	}
}
