// Package ctxfile discovers and assembles AGENTS.md files into a system
// prompt. It knows nothing about the agent loop or slash commands --
// internal/skill's Skill and this package's assembled base string are
// composed by the slash package (internal/slash/skills.go).
package ctxfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Assemble concatenates every AGENTS.md relevant to cwd into one system
// prompt: the file at userAgentsPath (if non-empty and present) first,
// then each AGENTS.md found walking from cwd up to, and including, the
// nearest ancestor containing .git -- or the filesystem root, if none is
// found -- closest-to-cwd last. A missing file at any level is not an
// error; a real read error (permissions, a path that turns out to be a
// directory, etc.) becomes a warning, and that piece is just omitted.
func Assemble(cwd, userAgentsPath string) (system string, warnings []string, err error) {
	dirs, err := collectDirs(cwd)
	if err != nil {
		return "", nil, err
	}

	var pieces []string

	if userAgentsPath != "" {
		content, warning := readIfExists(userAgentsPath)
		if warning != "" {
			warnings = append(warnings, warning)
		}
		pieces = append(pieces, content)
	}

	for _, dir := range dirs {
		content, warning := readIfExists(filepath.Join(dir, "AGENTS.md"))
		if warning != "" {
			warnings = append(warnings, warning)
		}
		pieces = append(pieces, content)
	}

	return Join(pieces...), warnings, nil
}

// Join concatenates non-empty parts with a blank line between them,
// skipping empty ones.
func Join(parts ...string) string {
	var nonEmpty []string
	for _, p := range parts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, "\n\n")
}

// collectDirs walks from cwd upward, stopping once a directory containing
// .git is reached (that directory is included) or the filesystem root is
// reached, and returns the directories in root-most-first order.
func collectDirs(cwd string) ([]string, error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("ctxfile: resolve %s: %w", cwd, err)
	}

	var dirs []string
	current := abs
	for {
		dirs = append(dirs, current)
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break // filesystem root
		}
		current = parent
	}

	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}
	return dirs, nil
}

// readIfExists reads path, treating a missing file as "no content, no
// warning" and any other error as a warning with the content omitted.
func readIfExists(path string) (content, warning string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Sprintf("ctxfile: %s: %v", path, err)
		}
		return "", ""
	}
	return strings.TrimSpace(string(data)), ""
}
