// Package sandbox is the only place internal/tool is allowed to touch the
// filesystem or spawn processes (AGENTS.md invariant #4). It owns the
// project-directory root and the environment-variable denylist.
//
// This is honest, not bulletproof: file access is jailed via os.Root, but
// bash is arbitrary execution gated by approval, not a sandbox. See
// HEWN.md §3.
package sandbox

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Sandbox confines file access to a root directory.
type Sandbox struct {
	root     *os.Root
	rootPath string
}

// New opens a Sandbox rooted at dir.
func New(dir string) (*Sandbox, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("sandbox: resolve root: %w", err)
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("sandbox: open root %s: %w", abs, err)
	}
	return &Sandbox{root: root, rootPath: abs}, nil
}

// Close releases the root directory handle.
func (s *Sandbox) Close() error {
	return s.root.Close()
}

// Dir returns the absolute path the sandbox is rooted at.
func (s *Sandbox) Dir() string {
	return s.rootPath
}

// rel resolves path to a root-relative path, refusing anything that would
// escape the root. path may be given relative to the root or as an
// absolute path that falls under it.
func (s *Sandbox) rel(path string) (string, error) {
	if filepath.IsAbs(path) {
		r, err := filepath.Rel(s.rootPath, path)
		if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("sandbox: path %q escapes root %q", path, s.rootPath)
		}
		return r, nil
	}
	clean := filepath.Clean(path)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("sandbox: path %q escapes root", path)
	}
	return clean, nil
}

// ReadFile reads a file rooted at the sandbox root. os.Root refuses symlink
// escapes on the caller's behalf.
func (s *Sandbox) ReadFile(path string) ([]byte, error) {
	rel, err := s.rel(path)
	if err != nil {
		return nil, err
	}
	f, err := s.root.Open(rel)
	if err != nil {
		return nil, fmt.Errorf("sandbox: open %q: %w", path, err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("sandbox: read %q: %w", path, err)
	}
	return data, nil
}

// WriteFile writes data to a file rooted at the sandbox root, creating
// parent directories as needed.
func (s *Sandbox) WriteFile(path string, data []byte, perm os.FileMode) error {
	rel, err := s.rel(path)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(rel); dir != "." {
		if mkErr := s.mkdirAll(dir); mkErr != nil {
			return fmt.Errorf("sandbox: mkdir %q: %w", dir, mkErr)
		}
	}

	f, err := s.root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("sandbox: create %q: %w", path, err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("sandbox: write %q: %w", path, err)
	}
	return nil
}

// Stat reports whether a root-relative path exists and, if so, its info.
func (s *Sandbox) Stat(path string) (os.FileInfo, error) {
	rel, err := s.rel(path)
	if err != nil {
		return nil, err
	}
	info, err := s.root.Stat(rel)
	if err != nil {
		return nil, fmt.Errorf("sandbox: stat %q: %w", path, err)
	}
	return info, nil
}

func (s *Sandbox) mkdirAll(dir string) error {
	if dir == "." || dir == "" {
		return nil
	}
	segments := strings.Split(filepath.ToSlash(dir), "/")
	cur := ""
	for _, seg := range segments {
		if seg == "" || seg == "." {
			continue
		}
		if cur == "" {
			cur = seg
		} else {
			cur += "/" + seg
		}
		if err := s.root.Mkdir(cur, 0o755); err != nil && !os.IsExist(err) {
			return err
		}
	}
	return nil
}
