package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := defaults()
	if cfg.Provider != "anthropic" {
		t.Errorf("default provider = %q, want anthropic", cfg.Provider)
	}
	if cfg.Model != "claude-opus-4-8" {
		t.Errorf("default model = %q, want claude-opus-4-8", cfg.Model)
	}
}

func TestLoadMissingFile(t *testing.T) {
	// Use a temp HOME so the user's real ~/.config/hewn/config.yaml
	// doesn't interfere.
	home := t.TempDir()
	origHOME := os.Getenv("HOME")
	origUP := os.Getenv("USERPROFILE")
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	defer func() {
		os.Setenv("HOME", origHOME)
		os.Setenv("USERPROFILE", origUP)
	}()

	cfg, err := Load("/nonexistent")
	if err != nil {
		t.Fatalf("Load(nonexistent): %v", err)
	}
	if cfg.Provider != "anthropic" {
		t.Errorf("after missing file, provider = %q, want default", cfg.Provider)
	}
}

func TestLoadEmptyFile(t *testing.T) {
	home := t.TempDir()
	origHOME := os.Getenv("HOME")
	origUP := os.Getenv("USERPROFILE")
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	defer func() {
		os.Setenv("HOME", origHOME)
		os.Setenv("USERPROFILE", origUP)
	}()

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte{}, 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load(empty): %v", err)
	}
	if cfg.Provider != "anthropic" {
		t.Errorf("after empty file, provider = %q, want default", cfg.Provider)
	}
}

func TestUserConfigOverrides(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".config", "hewn")
	os.MkdirAll(cfgDir, 0755)
	os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("provider: openai\nmodel: gpt-4o\n"), 0644)

	// Override HOME/USERPROFILE to point to our temp dir so userConfigPath()
	// resolves inside it.
	origHOME := os.Getenv("HOME")
	origUP := os.Getenv("USERPROFILE")
	t.Cleanup(func() {
		os.Setenv("HOME", origHOME)
		os.Setenv("USERPROFILE", origUP)
	})
	os.Setenv("HOME", dir)
	os.Setenv("USERPROFILE", dir)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "openai" {
		t.Errorf("user config: provider = %q, want openai", cfg.Provider)
	}
	if cfg.Model != "gpt-4o" {
		t.Errorf("user config: model = %q, want gpt-4o", cfg.Model)
	}
}

func TestProjectOverridesUser(t *testing.T) {
	dir := t.TempDir()

	// User config: anthropic
	os.MkdirAll(filepath.Join(dir, ".config", "hewn"), 0755)
	os.WriteFile(filepath.Join(dir, ".config", "hewn", "config.yaml"), []byte("provider: anthropic\nmodel: claude-opus-4-8\n"), 0644)

	// Project config: openai
	projectDir := filepath.Join(dir, "project")
	os.MkdirAll(filepath.Join(projectDir, ".hewn"), 0755)
	os.WriteFile(filepath.Join(projectDir, ".hewn", "config.yaml"), []byte("provider: openai\n"), 0644)

	// Override home
	origHOME := os.Getenv("HOME")
	origUP := os.Getenv("USERPROFILE")
	t.Cleanup(func() {
		os.Setenv("HOME", origHOME)
		os.Setenv("USERPROFILE", origUP)
	})
	os.Setenv("HOME", dir)
	os.Setenv("USERPROFILE", dir)

	cfg, err := Load(projectDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "openai" {
		t.Errorf("after project override: provider = %q, want openai", cfg.Provider)
	}
	if cfg.Model != "claude-opus-4-8" {
		t.Errorf("after project override: model = %q, want claude-opus-4-8 (from user config)", cfg.Model)
	}
}

func TestFlagsWin(t *testing.T) {
	cfg := Config{Provider: "openai", Model: "gpt-4o"}
	changed := map[string]bool{"provider": true, "model": false}
	ApplyFlags(&cfg, changed, struct {
		Provider string
		Model    string
		DB       string
		CWD      string
		NoTools  bool
		Yolo     bool
	}{Provider: "anthropic", Model: "other-model"})

	if cfg.Provider != "anthropic" {
		t.Errorf("after flag override: provider = %q, want anthropic", cfg.Provider)
	}
	if cfg.Model != "gpt-4o" {
		t.Errorf("after non-override: model = %q, want gpt-4o (preserved from config)", cfg.Model)
	}
}

func TestMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".hewn"), 0755)
	os.WriteFile(filepath.Join(dir, ".hewn", "config.yaml"), []byte("provider: [unclosed bracket\n"), 0644)

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestResolveDB(t *testing.T) {
	db := ResolveDB("")
	if !strings.HasSuffix(db, ".local/share/hewn/hewn.db") && !strings.HasSuffix(db, `.local\share\hewn\hewn.db`) {
		t.Errorf("ResolveDB() default path = %q, expected it to end with .local/share/hewn/hewn.db", db)
	}
}

func TestResolveDBExplicit(t *testing.T) {
	db := ResolveDB("/custom/path/hewn.db")
	if db != "/custom/path/hewn.db" {
		t.Errorf("ResolveDB(explicit) = %q, want /custom/path/hewn.db", db)
	}
}

func TestResolveDBTilde(t *testing.T) {
	db := ResolveDB("~/custom/hewn.db")
	if len(db) < 2 || db[0] == '~' {
		t.Errorf("ResolveDB(tilde) = %q, expected tilde to be expanded", db)
	}
	if !strings.HasSuffix(db, "custom/hewn.db") && !strings.HasSuffix(db, `custom\hewn.db`) {
		t.Errorf("ResolveDB(tilde) = %q, expected it to end with custom/hewn.db", db)
	}
}

func TestLoadProjectOnly(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".hewn"), 0755)
	os.WriteFile(filepath.Join(dir, ".hewn", "config.yaml"), []byte("yolo: true\nno-tools: false\n"), 0644)

	cfg := defaults()
	if err := LoadProject(&cfg, dir); err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if !cfg.Yolo {
		t.Error("expected yolo=true from project config")
	}
}
