package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/unhewn/hewn/internal/sandbox"
	"github.com/unhewn/hewn/internal/tool"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectTimeout bounds how long connecting to and listing tools from one
// server may take, so a single hanging server can't block Hewn from
// starting.
const connectTimeout = 15 * time.Second

// Servers holds every MCP server session that connected successfully.
type Servers struct {
	sessions []*sdk.ClientSession
	tools    []tool.Tool
}

// Tools returns every tool discovered across all connected servers.
func (s *Servers) Tools() []tool.Tool {
	return s.tools
}

// Close closes every live session, terminating its subprocess. A nil
// *Servers (no config, or Connect never called) closes cleanly.
func (s *Servers) Close() error {
	if s == nil {
		return nil
	}
	var errs []error
	for _, sess := range s.sessions {
		if err := sess.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Connect loads .hewn/mcp.json under cwd and connects to each configured
// server in turn, listing its tools. A server that fails to connect or
// list tools within connectTimeout is skipped (reported in warnings) --
// Hewn always starts regardless of MCP health. The returned *Servers is
// never nil, even when zero servers are configured or all of them fail.
func Connect(ctx context.Context, cwd string) (*Servers, []string, error) {
	configPath := filepath.Join(cwd, ".hewn", "mcp.json")
	configs, warning := LoadConfig(configPath)

	var warnings []string
	if warning != "" {
		warnings = append(warnings, warning)
	}

	servers := &Servers{}
	client := sdk.NewClient(&sdk.Implementation{Name: "hewn", Version: "dev"}, nil)

	for name, cfg := range configs {
		session, tools, err := connectOne(ctx, client, name, cfg)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("mcp: %s: %v", name, err))
			continue
		}
		servers.sessions = append(servers.sessions, session)
		servers.tools = append(servers.tools, tools...)
	}

	return servers, warnings, nil
}

// connectOne spawns cfg's subprocess, connects, and lists its tools.
// The subprocess's environment is filtered through sandbox.FilterEnv --
// an MCP server is arbitrary third-party code, so it gets at least the
// same ambient-secret protection as Hewn's own bash tool -- with cfg.Env
// always applied on top, since the user configured those explicitly for
// this server.
func connectOne(ctx context.Context, client *sdk.Client, name string, cfg ServerConfig) (*sdk.ClientSession, []tool.Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = sandbox.FilterEnv(os.Environ(), nil)
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	session, err := client.Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("connect: %w", err)
	}

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		_ = session.Close()
		return nil, nil, fmt.Errorf("list tools: %w", err)
	}

	prefix := sanitizeName(name)
	tools := make([]tool.Tool, 0, len(result.Tools))
	for _, def := range result.Tools {
		tools = append(tools, &mcpTool{session: session, def: def, namePrefix: prefix})
	}
	return session, tools, nil
}

// sanitizeName maps any character outside [a-zA-Z0-9_-] to '_', so a
// model-facing tool name (mcp__<server>__<tool>) is always a clean
// identifier regardless of how a user wrote the config key.
func sanitizeName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			return r
		default:
			return '_'
		}
	}, name)
}

// mcpTool adapts one MCP server's tool as a tool.Tool. Risk is always
// RiskMutating: Hewn can't verify what a third-party server's tool
// actually does, so every call is approval-gated regardless of any
// server-supplied hint.
type mcpTool struct {
	session    *sdk.ClientSession
	def        *sdk.Tool
	namePrefix string
}

func (t *mcpTool) Name() string         { return fmt.Sprintf("mcp__%s__%s", t.namePrefix, t.def.Name) }
func (t *mcpTool) Description() string  { return t.def.Description }
func (t *mcpTool) Risk() tool.RiskLevel { return tool.RiskMutating }

func (t *mcpTool) Schema() json.RawMessage {
	if t.def.InputSchema == nil {
		return json.RawMessage("{}")
	}
	raw, err := json.Marshal(t.def.InputSchema)
	if err != nil {
		return json.RawMessage("{}")
	}
	return raw
}

func (t *mcpTool) Execute(ctx context.Context, params json.RawMessage, _ tool.IO) (tool.Result, error) {
	var args map[string]any
	if len(params) > 0 {
		if err := json.Unmarshal(params, &args); err != nil {
			return tool.Result{}, fmt.Errorf("mcp: decode params for %s: %w", t.def.Name, err)
		}
	}

	result, err := t.session.CallTool(ctx, &sdk.CallToolParams{Name: t.def.Name, Arguments: args})
	if err != nil {
		return tool.Result{}, fmt.Errorf("mcp: call %s: %w", t.def.Name, err)
	}

	return tool.Result{Output: extractText(result.Content), IsError: result.IsError}, nil
}

// extractText concatenates every TextContent block in content, one per
// line. Other content kinds (images, audio) get a short placeholder --
// v1 has no non-text tool-result support, matching Hewn's own built-in
// tools, which are text-only today.
func extractText(content []sdk.Content) string {
	parts := make([]string, 0, len(content))
	for _, c := range content {
		if text, ok := c.(*sdk.TextContent); ok {
			parts = append(parts, text.Text)
			continue
		}
		parts = append(parts, "[non-text content omitted]")
	}
	return strings.Join(parts, "\n")
}
