package ctxfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gitMarker makes dir look like a repo root, so collectDirs's walk always
// terminates inside the test's own t.TempDir() sandbox rather than
// escaping onto the real filesystem.
func gitMarker(t *testing.T, dir string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestAssembleNoFiles(t *testing.T) {
	root := t.TempDir()
	gitMarker(t, root)

	system, warnings, err := Assemble(root, "")
	if err != nil {
		t.Fatalf("Assemble() error = %v", err)
	}
	if system != "" || len(warnings) != 0 {
		t.Errorf("Assemble() = %q, %v, want empty", system, warnings)
	}
}

func TestAssembleOrdersRootFirstCwdLast(t *testing.T) {
	root := t.TempDir()
	gitMarker(t, root)
	writeFile(t, filepath.Join(root, "AGENTS.md"), "root")
	sub := filepath.Join(root, "sub")
	writeFile(t, filepath.Join(sub, "AGENTS.md"), "sub")
	subsub := filepath.Join(sub, "subsub") // no AGENTS.md here
	if err := os.MkdirAll(subsub, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", subsub, err)
	}

	system, warnings, err := Assemble(subsub, "")
	if err != nil {
		t.Fatalf("Assemble() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if system != "root\n\nsub" {
		t.Errorf("Assemble() = %q, want %q", system, "root\n\nsub")
	}
}

func TestAssembleStopsAtGitBoundary(t *testing.T) {
	outer := t.TempDir()
	gitMarker(t, outer)
	writeFile(t, filepath.Join(outer, "AGENTS.md"), "outer, should not appear")

	repo := filepath.Join(outer, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", repo, err)
	}
	gitMarker(t, repo)
	writeFile(t, filepath.Join(repo, "AGENTS.md"), "repo")

	system, _, err := Assemble(repo, "")
	if err != nil {
		t.Fatalf("Assemble() error = %v", err)
	}
	if system != "repo" {
		t.Errorf("Assemble() = %q, want just %q (walk should stop at repo's own .git)", system, "repo")
	}
}

func TestAssembleUserAgentsPathFirst(t *testing.T) {
	root := t.TempDir()
	gitMarker(t, root)
	writeFile(t, filepath.Join(root, "AGENTS.md"), "project")

	userPath := filepath.Join(t.TempDir(), "AGENTS.md")
	writeFile(t, userPath, "global")

	system, warnings, err := Assemble(root, userPath)
	if err != nil {
		t.Fatalf("Assemble() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if system != "global\n\nproject" {
		t.Errorf("Assemble() = %q, want %q", system, "global\n\nproject")
	}
}

func TestAssembleMissingUserAgentsPathIsFine(t *testing.T) {
	root := t.TempDir()
	gitMarker(t, root)

	system, warnings, err := Assemble(root, filepath.Join(t.TempDir(), "does-not-exist.md"))
	if err != nil {
		t.Fatalf("Assemble() error = %v", err)
	}
	if system != "" || len(warnings) != 0 {
		t.Errorf("Assemble() = %q, %v, want empty, no warnings", system, warnings)
	}
}

func TestAssembleUnreadableUserAgentsPathWarns(t *testing.T) {
	root := t.TempDir()
	gitMarker(t, root)

	// A directory, not a file: os.ReadFile fails with a non-IsNotExist
	// error, a portable way to exercise the warning path.
	notAFile := t.TempDir()

	system, warnings, err := Assemble(root, notAFile)
	if err != nil {
		t.Fatalf("Assemble() error = %v, want nil (a bad piece is a warning, not fatal)", err)
	}
	if system != "" {
		t.Errorf("Assemble() system = %q, want empty", system)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], notAFile) {
		t.Errorf("warnings = %v, want one mentioning %s", warnings, notAFile)
	}
}

func TestJoin(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		want  string
	}{
		{"all empty", []string{"", ""}, ""},
		{"one part", []string{"a"}, "a"},
		{"skips empties", []string{"a", "", "b"}, "a\n\nb"},
		{"no parts", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Join(tt.parts...); got != tt.want {
				t.Errorf("Join(%v) = %q, want %q", tt.parts, got, tt.want)
			}
		})
	}
}
