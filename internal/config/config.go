// Package config provides Hewn's layered configuration: YAML files at the
// user level (~/.config/hewn/config.yaml) and project level (.hewn/config.yaml)
// overlaid with CLI flags. Precedence: flags > project > user > built-in
// defaults.
//
// A missing or empty config file is never an error -- every field has a
// sensible zero value that the caller can interpret as "not set, use flag
// default." Only a malformed YAML file (syntax error on a file that exists)
// produces an error.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the union of all configurable Hewn settings. YAML tags match
// the config file keys.
type Config struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	DB       string `yaml:"db"`
	CWD      string `yaml:"cwd"`
	NoTools  bool   `yaml:"no-tools"`
	Yolo     bool   `yaml:"yolo"`
	APIKey   string `yaml:"api-key,omitempty"`
	BaseURL  string `yaml:"base-url,omitempty"`
}

// Load layered configuration: user config first, then project config
// (relative to cwd if resolvable), returning the merged result. A missing
// or unreadable user or project config is silently skipped -- only a
// malformed YAML syntax error is returned.
//
// At this point cwd may still be empty (not yet resolved from flags). The
// caller is responsible for resolving it before the second call (LoadProject)
// or by passing the resolved cwd here.
func Load(cwd string) (Config, error) {
	cfg := defaults()

	if err := mergeFile(&cfg, userConfigPath()); err != nil {
		return cfg, err
	}

	if cwd != "" {
		if err := mergeFile(&cfg, filepath.Join(cwd, ".hewn", "config.yaml")); err != nil {
			return cfg, err
		}
	}

	return cfg, nil
}

// LoadProject reads only the project-level config file from .hewn/config.yaml
// under cwd. Use this when cwd is resolved after flags are parsed (the config
// system's split call pattern).
func LoadProject(cfg *Config, cwd string) error {
	return mergeFile(cfg, filepath.Join(cwd, ".hewn", "config.yaml"))
}

// ApplyFlags copies non-zero flag values onto cfg. This is the highest
// precedence layer: a flag that was explicitly set (changed) wins over
// whatever user or project config says.
func ApplyFlags(cfg *Config, changed map[string]bool, flags struct {
	Provider string
	Model    string
	DB       string
	CWD      string
	NoTools  bool
	Yolo     bool
}) {
	if changed["provider"] {
		cfg.Provider = flags.Provider
	}
	if changed["model"] {
		cfg.Model = flags.Model
	}
	if changed["db"] {
		cfg.DB = flags.DB
	}
	if changed["cwd"] {
		cfg.CWD = flags.CWD
	}
	if changed["no-tools"] {
		cfg.NoTools = flags.NoTools
	}
	if changed["yolo"] {
		cfg.Yolo = flags.Yolo
	}
}

// ResolveDB expands ~ and default-relative DB paths.
func ResolveDB(db string) string {
	if db == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, ".local", "share", "hewn", "hewn.db")
	}
	return expandHome(db)
}

// defaults returns Hewn's built-in config defaults.
func defaults() Config {
	return Config{
		Provider: "anthropic",
		Model:    "claude-opus-4-8",
		DB:       "", // resolved lazily via ResolveDB
	}
}

// userConfigPath returns ~/.config/hewn/config.yaml.
func userConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "hewn", "config.yaml")
}

// mergeFile reads a single YAML file and merges its non-zero values into cfg.
// A missing or unreadable file is silently skipped.
func mergeFile(cfg *Config, path string) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	if len(data) == 0 {
		return nil
	}

	var fileCfg Config
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return fmt.Errorf("config: parse %s: %w", path, err)
	}
	merge(cfg, fileCfg)
	return nil
}

// merge overlays src onto dst, non-zero fields only.
func merge(dst *Config, src Config) {
	if src.Provider != "" {
		dst.Provider = src.Provider
	}
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.DB != "" {
		dst.DB = src.DB
	}
	if src.CWD != "" {
		dst.CWD = src.CWD
	}
	if src.NoTools {
		dst.NoTools = true
	}
	if src.Yolo {
		dst.Yolo = true
	}
	if src.APIKey != "" {
		dst.APIKey = src.APIKey
	}
	if src.BaseURL != "" {
		dst.BaseURL = src.BaseURL
	}
}

// expandHome replaces a leading "~/" or "~\" with the user's home directory.
func expandHome(path string) string {
	if len(path) < 2 || path[0] != '~' {
		return path
	}
	if path[1] != '/' && path[1] != os.PathSeparator {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}
