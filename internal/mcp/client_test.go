package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unhewn/hewn/internal/tool"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type echoArgs struct {
	Message string `json:"message" jsonschema:"the message to echo"`
}

// fixtureSession builds an in-process fake MCP server with two tools --
// one that succeeds and one that always reports a tool-level error -- and
// connects a real *sdk.Client to it over an in-memory transport. No real
// subprocess, no network: this is the hermetic path AGENTS.md's testing
// rules ask for.
func fixtureSession(t *testing.T) *sdk.ClientSession {
	t.Helper()

	server := sdk.NewServer(&sdk.Implementation{Name: "fixture", Version: "test"}, nil)
	sdk.AddTool(server, &sdk.Tool{Name: "echo", Description: "echoes the message back"},
		func(_ context.Context, _ *sdk.CallToolRequest, args echoArgs) (*sdk.CallToolResult, any, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: args.Message}}}, nil, nil
		})
	sdk.AddTool(server, &sdk.Tool{Name: "fail", Description: "always reports a tool-level error"},
		func(_ context.Context, _ *sdk.CallToolRequest, _ echoArgs) (*sdk.CallToolResult, any, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "boom"}}, IsError: true}, nil, nil
		})

	serverT, clientT := sdk.NewInMemoryTransports()
	ctx := context.Background()

	serverSession, err := server.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession
}

// findTool lists tools from session and returns the one named name, or
// fails the test.
func findTool(t *testing.T, session *sdk.ClientSession, name string) *sdk.Tool {
	t.Helper()
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, def := range result.Tools {
		if def.Name == name {
			return def
		}
	}
	t.Fatalf("tool %q not found in %v", name, result.Tools)
	return nil
}

func TestMCPTool_NameAndRisk(t *testing.T) {
	session := fixtureSession(t)
	def := findTool(t, session, "echo")

	mt := &mcpTool{session: session, def: def, namePrefix: "myserver"}
	if got, want := mt.Name(), "mcp__myserver__echo"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if mt.Risk() != tool.RiskMutating {
		t.Errorf("Risk() = %v, want RiskMutating (MCP tools are never auto-read-only)", mt.Risk())
	}
	if mt.Description() != "echoes the message back" {
		t.Errorf("Description() = %q", mt.Description())
	}
}

func TestMCPTool_SchemaIsValidJSON(t *testing.T) {
	session := fixtureSession(t)
	def := findTool(t, session, "echo")
	mt := &mcpTool{session: session, def: def, namePrefix: "s"}

	var schema map[string]any
	if err := json.Unmarshal(mt.Schema(), &schema); err != nil {
		t.Fatalf("Schema() = %s, not valid JSON: %v", mt.Schema(), err)
	}
	if schema["type"] != "object" {
		t.Errorf("Schema()[type] = %v, want %q (input struct is a struct)", schema["type"], "object")
	}
}

func TestMCPTool_ExecuteSuccess(t *testing.T) {
	session := fixtureSession(t)
	def := findTool(t, session, "echo")
	mt := &mcpTool{session: session, def: def, namePrefix: "s"}

	result, err := mt.Execute(context.Background(), json.RawMessage(`{"message":"hello"}`), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() IsError = true, want false")
	}
	if result.Output != "hello" {
		t.Errorf("Execute() Output = %q, want %q", result.Output, "hello")
	}
}

func TestMCPTool_ExecuteToolLevelError(t *testing.T) {
	session := fixtureSession(t)
	def := findTool(t, session, "fail")
	mt := &mcpTool{session: session, def: def, namePrefix: "s"}

	result, err := mt.Execute(context.Background(), json.RawMessage(`{"message":"x"}`), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil (a tool-level failure is Result.IsError, not a Go error)", err)
	}
	if !result.IsError {
		t.Errorf("Execute() IsError = false, want true")
	}
	if result.Output != "boom" {
		t.Errorf("Execute() Output = %q, want %q", result.Output, "boom")
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"myserver", "myserver"},
		{"my-server_1", "my-server_1"},
		{"my server.exe", "my_server_exe"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := sanitizeName(tt.in); got != tt.want {
			t.Errorf("sanitizeName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// hewnDir returns a cwd whose .hewn/ subdirectory contains a mcp.json with
// the given content, matching Connect's real lookup path (cwd/.hewn/mcp.json).
func hewnDir(t *testing.T, content string) string {
	t.Helper()
	cwd := t.TempDir()
	hewnSub := filepath.Join(cwd, ".hewn")
	if err := os.MkdirAll(hewnSub, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", hewnSub, err)
	}
	writeConfig(t, hewnSub, content)
	return cwd
}

func TestConnectNoConfig(t *testing.T) {
	servers, warnings, err := Connect(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if len(servers.Tools()) != 0 {
		t.Errorf("Tools() = %v, want none", servers.Tools())
	}
	if err := servers.Close(); err != nil {
		t.Errorf("Close() on an empty Servers = %v, want nil", err)
	}
}

func TestConnectBadCommandWarns(t *testing.T) {
	cwd := hewnDir(t, `{"mcpServers": {"broken": {"command": "hewn-test-nonexistent-binary-xyz"}}}`)

	servers, warnings, err := Connect(context.Background(), cwd)
	if err != nil {
		t.Fatalf("Connect() error = %v, want nil (a broken server is a warning, not fatal)", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "broken") {
		t.Fatalf("warnings = %v, want one mentioning the broken server", warnings)
	}
	if len(servers.Tools()) != 0 {
		t.Errorf("Tools() = %v, want none", servers.Tools())
	}
}

func TestServersCloseNil(t *testing.T) {
	var servers *Servers
	if err := servers.Close(); err != nil {
		t.Errorf("Close() on nil *Servers = %v, want nil", err)
	}
}

func TestExtractText(t *testing.T) {
	content := []sdk.Content{
		&sdk.TextContent{Text: "line one"},
		&sdk.TextContent{Text: "line two"},
	}
	if got, want := extractText(content), "line one\nline two"; got != want {
		t.Errorf("extractText() = %q, want %q", got, want)
	}
}
