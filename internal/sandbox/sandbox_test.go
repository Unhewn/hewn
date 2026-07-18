package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReadWriteFile(t *testing.T) {
	dir := t.TempDir()
	sb, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer sb.Close()

	if writeErr := sb.WriteFile("nested/dir/file.txt", []byte("hello"), 0o644); writeErr != nil {
		t.Fatalf("WriteFile: %v", writeErr)
	}

	data, err := sb.ReadFile("nested/dir/file.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("ReadFile = %q, want %q", data, "hello")
	}
}

func TestReadFile_AbsolutePathInsideRoot(t *testing.T) {
	dir := t.TempDir()
	sb, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer sb.Close()

	if writeErr := sb.WriteFile("file.txt", []byte("hi"), 0o644); writeErr != nil {
		t.Fatalf("WriteFile: %v", writeErr)
	}

	data, err := sb.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatalf("ReadFile with absolute in-root path: %v", err)
	}
	if string(data) != "hi" {
		t.Errorf("ReadFile = %q, want %q", data, "hi")
	}
}

func TestReadFile_RefusesRelativeEscape(t *testing.T) {
	dir := t.TempDir()
	sb, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer sb.Close()

	if _, err := sb.ReadFile("../outside.txt"); err == nil {
		t.Fatal("ReadFile(\"../outside.txt\"): expected error, got nil")
	}
}

func TestReadFile_RefusesAbsoluteEscape(t *testing.T) {
	dir := t.TempDir()
	sb, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer sb.Close()

	outside := t.TempDir() // a different temp dir, outside sb's root
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("nope"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	if _, err := sb.ReadFile(outsideFile); err == nil {
		t.Fatal("ReadFile(outside absolute path): expected error, got nil")
	}
}

func TestReadFile_RefusesSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("nope"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	link := filepath.Join(root, "escape")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	sb, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer sb.Close()

	if _, err := sb.ReadFile("escape"); err == nil {
		t.Fatal("ReadFile through symlink escape: expected error, got nil")
	}
}

func TestFilterEnv(t *testing.T) {
	in := []string{
		"AWS_SECRET_ACCESS_KEY=leaked",
		"GITHUB_TOKEN=leaked",
		"ANTHROPIC_API_KEY=keepme",
		"PATH=/usr/bin",
		"HOME=/home/user",
	}

	out := FilterEnv(in, []string{"ANTHROPIC_API_KEY"})

	want := map[string]bool{
		"ANTHROPIC_API_KEY=keepme": true,
		"PATH=/usr/bin":            true,
		"HOME=/home/user":          true,
	}
	if len(out) != len(want) {
		t.Fatalf("FilterEnv = %v, want exactly %v", out, want)
	}
	for _, kv := range out {
		if !want[kv] {
			t.Errorf("FilterEnv kept unexpected var %q", kv)
		}
	}
}

func TestReadFile_NotExist(t *testing.T) {
	dir := t.TempDir()
	sb, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer sb.Close()

	_, err = sb.ReadFile("missing.txt")
	if err == nil {
		t.Fatal("ReadFile(missing.txt): expected error, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadFile(missing.txt) error = %v, want wrapping os.ErrNotExist", err)
	}
}
