package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
}

func TestLoadMissingDir(t *testing.T) {
	skills, warnings, err := Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if len(skills) != 0 || len(warnings) != 0 {
		t.Errorf("Load() = %v, %v, want empty", skills, warnings)
	}
}

func TestLoadValidWithTools(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "reviewer.md", "---\n"+
		"description: review a diff\n"+
		"tools: [read, bash]\n"+
		"---\n"+
		"You are reviewing a change.\n")

	skills, warnings, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if len(skills) != 1 {
		t.Fatalf("skills = %v, want 1", skills)
	}

	got := skills[0]
	if got.Name != "reviewer" {
		t.Errorf("Name = %q, want %q (from filename)", got.Name, "reviewer")
	}
	if got.Description != "review a diff" {
		t.Errorf("Description = %q", got.Description)
	}
	if len(got.Tools) != 2 || got.Tools[0] != "read" || got.Tools[1] != "bash" {
		t.Errorf("Tools = %v, want [read bash]", got.Tools)
	}
	if got.Prompt != "You are reviewing a change." {
		t.Errorf("Prompt = %q", got.Prompt)
	}
}

func TestLoadValidNoTools(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "writer.md", "---\n"+
		"description: a persona with no tool restriction\n"+
		"---\n"+
		"Be a writer.\n")

	skills, _, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("skills = %v, want 1", skills)
	}
	if skills[0].Tools != nil {
		t.Errorf("Tools = %v, want nil (no restriction)", skills[0].Tools)
	}
}

func TestLoadNameOverride(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "cr.md", "---\n"+
		"name: code-review\n"+
		"---\n"+
		"Review code.\n")

	skills, _, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "code-review" {
		t.Fatalf("skills = %v, want name %q", skills, "code-review")
	}
}

func TestLoadMissingFrontMatter(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "broken.md", "Just a prompt, no front matter.\n")

	skills, warnings, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil (per-file problems are warnings)", err)
	}
	if len(skills) != 0 {
		t.Errorf("skills = %v, want none", skills)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "front matter") {
		t.Errorf("warnings = %v, want one mentioning front matter", warnings)
	}
}

func TestLoadDuplicateName(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "a-first.md", "---\nname: dup\n---\nfirst\n")
	writeSkill(t, dir, "b-second.md", "---\nname: dup\n---\nsecond\n")

	skills, warnings, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(skills) != 1 || skills[0].Prompt != "first" {
		t.Fatalf("skills = %v, want just the first-seen %q", skills, "dup")
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "duplicate name") {
		t.Errorf("warnings = %v, want one mentioning duplicate name", warnings)
	}
}

func TestLoadIgnoresNonMarkdown(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "notes.txt", "not a skill\n")
	writeSkill(t, dir, "real.md", "---\n---\nhello\n")

	skills, _, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "real" {
		t.Fatalf("skills = %v, want just %q", skills, "real")
	}
}
