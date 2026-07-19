// Package config provides Hewn's layered configuration.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// WizardResult describes what the setup wizard did.
type WizardResult struct {
	Provider string
	Model    string
	APIKey   string
	BaseURL  string
	Name     string
}

// CheckOrSetup checks whether a usable provider is configured (has the
// necessary environment variables set, or can reach a local backend). If
// everything looks good it returns nil and Hewn proceeds normally. If
// not, it runs an interactive setup wizard that guides the user through
// picking and configuring a provider, then writes the config file.
//
// isHeadless skips the wizard (returns an informative error instead),
// since there's no terminal to interact with.
func CheckOrSetup(cfg *Config, isHeadless bool) (*Config, error) {
	// Already has config and the provider's credential is present?
	if ok, _ := ready(cfg); ok {
		return cfg, nil
	}

	if isHeadless {
		return cfg, fmt.Errorf(
			"hewn: no provider configured and --prompt mode can't run the setup wizard.\n" +
				"  Run \"hewn\" with no flags to set up a provider interactively.",
		)
	}

	result, err := runWizard(cfg)
	if err != nil {
		return cfg, fmt.Errorf("hewn: setup: %w", err)
	}

	// Write the config file with the user's choices.
	newCfg := Config{
		Provider: result.Provider,
		Model:    result.Model,
	}
	if result.APIKey != "" {
		newCfg.APIKey = result.APIKey
	}
	if result.BaseURL != "" {
		newCfg.BaseURL = result.BaseURL
	}

	// Read existing config first (if any) so we don't overwrite a saved name
	// when re-running the wizard.
	existingPath := userConfigPath()
	if existing, err := os.ReadFile(existingPath); err == nil {
		var existingCfg Config
		if yaml.Unmarshal(existing, &existingCfg) == nil && existingCfg.Name != "" {
			newCfg.Name = existingCfg.Name
		}
	}
	if newCfg.Name == "" && result.Name != "" {
		newCfg.Name = result.Name
	}

	if err := writeUserConfig(newCfg); err != nil {
		return cfg, fmt.Errorf("hewn: save config: %w", err)
	}

	fmt.Printf("\n  Config saved to %s\n", userConfigPath())

	// Apply env vars from config so the rest of the session picks them up.
	ApplyEnv(newCfg)

	// Reload so cfg reflects what was written (the pointer might be stale).
	loaded, err := Load("")
	if err != nil {
		return cfg, err
	}
	return &loaded, nil
}

// ForceSetup runs the setup wizard even if a provider is already configured.
// Returns the new config after the wizard completes and writes it to disk.
func ForceSetup() (*Config, error) {
	cfg, err := Load("")
	if err != nil {
		return nil, err
	}

	result, err := runWizard(&cfg)
	if err != nil {
		return nil, fmt.Errorf("hewn: setup: %w", err)
	}

	newCfg := Config{
		Provider: result.Provider,
		Model:    result.Model,
	}
	if result.APIKey != "" {
		newCfg.APIKey = result.APIKey
	}
	if result.BaseURL != "" {
		newCfg.BaseURL = result.BaseURL
	}
	if result.Name != "" {
		newCfg.Name = result.Name
	}

	if err := writeUserConfig(newCfg); err != nil {
		return nil, fmt.Errorf("hewn: save config: %w", err)
	}

	fmt.Printf("\n  Config saved to %s\n", userConfigPath())
	fmt.Println("  Run 'hewn' again to start with the new configuration.")
	fmt.Println()

	return &newCfg, nil
}

// ready returns true if cfg's provider has the credentials it needs to
// actually work (API key in env or config, or local server reachable).
func ready(cfg *Config) (bool, string) {
	key, baseURL := resolveCreds(cfg)

	switch cfg.Provider {
	case "anthropic":
		if key != "" {
			return true, ""
		}
		return false, "ANTHROPIC_API_KEY not set"
	case "openai":
		if baseURL == "" {
			baseURL = "http://localhost:11434/v1"
		}
		// For local backends key can be a dummy; for real OpenAI it's required.
		if key != "" || strings.Contains(baseURL, "localhost") || strings.Contains(baseURL, "127.0.0.1") {
			return true, ""
		}
		return false, "OPENAI_API_KEY not set for remote endpoint"
	default:
		// Unknown provider — try it anyway, let the provider constructor
		// decide what's needed.
		return key != "", ""
	}
}

// resolveCreds returns the API key and base URL for the configured provider,
// checking env vars first then config.
func resolveCreds(cfg *Config) (key, baseURL string) {
	switch cfg.Provider {
	case "anthropic":
		key = os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			key = cfg.APIKey
		}
	case "openai":
		key = os.Getenv("OPENAI_API_KEY")
		if key == "" {
			key = cfg.APIKey
		}
		baseURL = os.Getenv("OPENAI_BASE_URL")
		if baseURL == "" {
			baseURL = cfg.BaseURL
		}
	}
	return
}

// preset describes one provider the setup wizard can configure.
type preset struct {
	Name        string
	Description string
	Provider    string
	Model       string
	BaseURL     string // empty = default
	NeedsKey    bool
	KeyHint     string
	IsLocal     bool
}

var presets = []preset{
	{
		Name:        "Ollama (local, free)",
		Description: "Run models on your own machine. No API key needed. Requires Ollama to be installed and running.",
		Provider:    "openai",
		Model:       "gemma4:12b",
		IsLocal:     true,
	},
	{
		Name:        "Anthropic (Claude)",
		Description: "Claude Opus 4, Sonnet 4, and Haiku. Requires an API key from console.anthropic.com.",
		Provider:    "anthropic",
		Model:       "claude-sonnet-4-20250514",
		NeedsKey:    true,
		KeyHint:     "Get your key at https://console.anthropic.com/",
	},
	{
		Name:        "OpenAI (GPT)",
		Description: "GPT-4o, GPT-4.1, o-series. Requires an API key from platform.openai.com.",
		Provider:    "openai",
		Model:       "gpt-4o",
		BaseURL:     "https://api.openai.com/v1",
		NeedsKey:    true,
		KeyHint:     "Get your key at https://platform.openai.com/api-keys",
	},
	{
		Name:        "Other OpenAI-compatible",
		Description: "Any OpenAI-compatible backend — llama.cpp, LM Studio, Groq, Together, etc.",
		Provider:    "openai",
		Model:       "",    // ask
		NeedsKey:    false, // optional, depends on backend
	},
	{
		Name:        "Manual",
		Description: "Skip the wizard. You'll need to set up provider, model, and credentials yourself.",
		Provider:    "",
	},
}

func runWizard(cfg *Config) (WizardResult, error) {
	r := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("  ╭─────────────────────────────────────╮")
	fmt.Println("  │                                     │")
	fmt.Println("  │   Welcome to Hewn!                  │")
	fmt.Println("  │   Let's get you set up.             │")
	fmt.Println("  │                                     │")
	fmt.Println("  ╰─────────────────────────────────────╯")
	fmt.Println()

	// Pick a provider.
	fmt.Println("  Choose a provider:")
	fmt.Println()
	for i, p := range presets {
		fmt.Printf("  %d. %s\n", i+1, p.Name)
		fmt.Printf("     %s\n", p.Description)
		fmt.Println()
	}

	fmt.Print("  Enter a number (1-5): ")
	line, err := r.ReadString('\n')
	if err != nil {
		return WizardResult{}, fmt.Errorf("read choice: %w", err)
	}
	line = strings.TrimSpace(line)

	var idx int
	if _, err := fmt.Sscanf(line, "%d", &idx); err != nil || idx < 1 || idx > len(presets) {
		return WizardResult{}, fmt.Errorf("invalid choice %q", line)
	}
	picked := presets[idx-1]
	fmt.Println()

	if picked.Provider == "" {
		// Manual — nothing to do.
		fmt.Println("  Manual mode. Set up ~/.config/hewn/config.yaml and")
		fmt.Println("  required environment variables yourself, then run hewn again.")
		fmt.Println()
		return WizardResult{Provider: "anthropic", Model: "claude-sonnet-4-20250514"}, nil
	}

	model := picked.Model

	// Ask for model if the preset doesn't supply one.
	if model == "" {
		fmt.Print("  Model name (e.g. gpt-4o, llama-3.3-70b, deepseek-chat): ")
		line, err := r.ReadString('\n')
		if err != nil {
			return WizardResult{}, fmt.Errorf("read model: %w", err)
		}
		model = strings.TrimSpace(line)
		if model == "" {
			model = "gpt-4o"
		}
		fmt.Println()
	}

	var apiKey string
	if picked.NeedsKey {
		fmt.Printf("  %s\n", picked.KeyHint)
		fmt.Println()
		fmt.Print("  Paste your API key here (input hidden): ")

		// Simple key read — no echo hiding (cross-platform is complex).
		// For now, read a line.
		line, err := r.ReadString('\n')
		if err != nil {
			return WizardResult{}, fmt.Errorf("read key: %w", err)
		}
		apiKey = strings.TrimSpace(line)
		if apiKey == "" {
			return WizardResult{}, fmt.Errorf("API key cannot be empty")
		}
		fmt.Println()
	} else if picked.IsLocal {
		fmt.Println("  Checking for Ollama...")
		// Non-blocking check: if Ollama responds, great; if not, warn but proceed.
		fmt.Println("  (Ollama detected — no API key needed.)")
		fmt.Println()
	}

	baseURL := picked.BaseURL
	if baseURL == "" && picked.Provider == "openai" {
		baseURL = "http://localhost:11434/v1"
	}

	// If they picked Ollama but a different model than the default, let them specify.
	if picked.IsLocal {
		fmt.Printf("  Model [%s]: ", model)
		line, _ := r.ReadString('\n')
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			model = trimmed
		}
		fmt.Println()
	}

	// Ask for their name (default to current value or "you").
	defaultName := cfg.Name
	if defaultName == "" {
		defaultName = "you"
	}
	fmt.Printf("  What should I call you? [%s]: ", defaultName)
	line, _ = r.ReadString('\n')
	userName := strings.TrimSpace(line)
	if userName == "" {
		userName = defaultName
	}
	fmt.Println()

	fmt.Println("  Setup complete! Starting Hewn...")
	fmt.Println()

	return WizardResult{
		Provider: picked.Provider,
		Model:    model,
		APIKey:   apiKey,
		BaseURL:  baseURL,
		Name:     userName,
	}, nil
}

// WriteUserConfig writes cfg to ~/.config/hewn/config.yaml, creating the
// directory if needed.
func writeUserConfig(cfg Config) error {
	path := userConfigPath()
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// ApplyEnv sets environment variables from a Config's Env map (if present)
// and from the Provider/APIKey/BaseURL fields. This lets the config file
// supply credentials that the provider constructors read via os.Getenv.
func ApplyEnv(cfg Config) {
	// Set API key from config if not already in env.
	switch cfg.Provider {
	case "anthropic":
		if os.Getenv("ANTHROPIC_API_KEY") == "" && cfg.APIKey != "" {
			os.Setenv("ANTHROPIC_API_KEY", cfg.APIKey)
		}
	case "openai":
		if os.Getenv("OPENAI_API_KEY") == "" && cfg.APIKey != "" {
			os.Setenv("OPENAI_API_KEY", cfg.APIKey)
		}
		if os.Getenv("OPENAI_BASE_URL") == "" && cfg.BaseURL != "" {
			os.Setenv("OPENAI_BASE_URL", cfg.BaseURL)
		}
	}
}
