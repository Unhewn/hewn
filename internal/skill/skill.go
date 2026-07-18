// Package skill loads declarative skill files: front-mattered Markdown
// bundles of a system prompt plus an optional allowed-tool subset, read
// from .hewn/skills/. It knows nothing about slash commands or tool
// registries -- turning a Skill into something invocable is the slash
// package's job (internal/slash/skills.go).
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill is one parsed .hewn/skills/*.md file.
type Skill struct {
	Name        string
	Description string
	Tools       []string
	Prompt      string
}

// frontMatter mirrors the YAML block between a skill file's "---"
// delimiters. It never leaves this package.
type frontMatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools"`
}

// Load reads every *.md file directly in dir (no recursion) and parses its
// front matter into a Skill. A missing dir is not an error -- it just
// means zero skills, the common case. Per-file problems (missing or
// malformed front matter, a duplicate name within dir) are collected as
// warnings and that file is skipped, rather than failing the whole load;
// err is reserved for a real I/O failure reading a dir that does exist.
func Load(dir string) (skills []Skill, warnings []string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("skill: read %s: %w", dir, err)
	}

	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		sk, err := parseFile(path, entry.Name())
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		if seen[sk.Name] {
			warnings = append(warnings, fmt.Sprintf("skill: %s: duplicate name %q, skipped", path, sk.Name))
			continue
		}
		seen[sk.Name] = true
		skills = append(skills, sk)
	}
	return skills, warnings, nil
}

func parseFile(path, filename string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, fmt.Errorf("skill: %s: %w", path, err)
	}

	fmBlock, body, ok := splitFrontMatter(string(data))
	if !ok {
		return Skill{}, fmt.Errorf(`skill: %s: missing front matter (file must start with a "---" line)`, path)
	}

	var fm frontMatter
	if err := yaml.Unmarshal([]byte(fmBlock), &fm); err != nil {
		return Skill{}, fmt.Errorf("skill: %s: parse front matter: %w", path, err)
	}

	name := fm.Name
	if name == "" {
		name = strings.TrimSuffix(filename, ".md")
	}
	if strings.ContainsAny(name, " \t\n") {
		return Skill{}, fmt.Errorf("skill: %s: name %q contains whitespace", path, name)
	}

	return Skill{
		Name:        name,
		Description: fm.Description,
		Tools:       fm.Tools,
		Prompt:      strings.TrimSpace(body),
	}, nil
}

// splitFrontMatter splits content on a leading "---" delimiter line and
// the next "---" delimiter line, returning the YAML in between and
// everything after. ok is false if content doesn't open with the
// delimiter or the closing delimiter is never found.
func splitFrontMatter(content string) (fmBlock, body string, ok bool) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", false
	}

	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.Join(lines[1:i], "\n"), strings.Join(lines[i+1:], "\n"), true
		}
	}
	return "", "", false
}
