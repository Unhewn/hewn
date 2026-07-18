package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestLoadConfigMissingFile(t *testing.T) {
	servers, warning := LoadConfig(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if warning != "" {
		t.Errorf("warning = %q, want none", warning)
	}
	if len(servers) != 0 {
		t.Errorf("servers = %v, want none", servers)
	}
}

func TestLoadConfigValid(t *testing.T) {
	path := writeConfig(t, t.TempDir(), `{
		"mcpServers": {
			"example": {
				"command": "npx",
				"args": ["-y", "@some/mcp-server"],
				"env": {"API_KEY": "secret"}
			}
		}
	}`)

	servers, warning := LoadConfig(path)
	if warning != "" {
		t.Fatalf("warning = %q, want none", warning)
	}
	if len(servers) != 1 {
		t.Fatalf("servers = %v, want 1 entry", servers)
	}

	got, ok := servers["example"]
	if !ok {
		t.Fatalf("servers = %v, want key %q", servers, "example")
	}
	if got.Command != "npx" {
		t.Errorf("Command = %q, want %q", got.Command, "npx")
	}
	if len(got.Args) != 2 || got.Args[0] != "-y" || got.Args[1] != "@some/mcp-server" {
		t.Errorf("Args = %v", got.Args)
	}
	if got.Env["API_KEY"] != "secret" {
		t.Errorf("Env[API_KEY] = %q, want %q", got.Env["API_KEY"], "secret")
	}
}

func TestLoadConfigMalformedJSON(t *testing.T) {
	path := writeConfig(t, t.TempDir(), `{not valid json`)

	servers, warning := LoadConfig(path)
	if len(servers) != 0 {
		t.Errorf("servers = %v, want none", servers)
	}
	if warning == "" || !strings.Contains(warning, "parse") {
		t.Errorf("warning = %q, want one mentioning parse failure", warning)
	}
}

func TestLoadConfigEmptyServers(t *testing.T) {
	path := writeConfig(t, t.TempDir(), `{"mcpServers": {}}`)

	servers, warning := LoadConfig(path)
	if warning != "" {
		t.Errorf("warning = %q, want none", warning)
	}
	if len(servers) != 0 {
		t.Errorf("servers = %v, want none", servers)
	}
}
