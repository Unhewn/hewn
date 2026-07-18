// Package mcp connects to external MCP (Model Context Protocol) servers
// declared in .hewn/mcp.json and exposes their tools as tool.Tool values.
// It depends on internal/tool but nothing depends on it in turn -- it's
// consumed only by cmd/hewn's wiring, the same one-way shape as
// internal/skill and internal/ctxfile.
package mcp

import (
	"encoding/json"
	"fmt"
	"os"
)

// ServerConfig is one entry of .hewn/mcp.json's "mcpServers" map.
type ServerConfig struct {
	Command string
	Args    []string
	Env     map[string]string
}

type wireConfig struct {
	MCPServers map[string]wireServerConfig `json:"mcpServers"`
}

type wireServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// LoadConfig reads .hewn/mcp.json. A missing file is not an error -- it
// just means zero servers, the common case. Malformed JSON becomes a
// warning with zero servers rather than an error: consistent with
// skill.Load and ctxfile.Assemble, a broken opt-in config file never
// blocks Hewn from starting.
func LoadConfig(path string) (servers map[string]ServerConfig, warning string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ""
		}
		return nil, fmt.Sprintf("mcp: %s: %v", path, err)
	}

	var wire wireConfig
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Sprintf("mcp: %s: parse: %v", path, err)
	}

	servers = make(map[string]ServerConfig, len(wire.MCPServers))
	for name, sc := range wire.MCPServers {
		servers[name] = ServerConfig{Command: sc.Command, Args: sc.Args, Env: sc.Env}
	}
	return servers, ""
}
