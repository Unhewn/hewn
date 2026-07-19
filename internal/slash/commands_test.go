package slash

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unhewn/hewn/internal/agent"
	"github.com/unhewn/hewn/internal/provider"
	"github.com/unhewn/hewn/internal/sandbox"
	"github.com/unhewn/hewn/internal/session"
	"github.com/unhewn/hewn/internal/tool"
)

// newTestContext builds a real (Provider-nil) Context: none of the
// built-in commands ever call Provider.Stream, so no fake provider is
// needed at all.
func newTestContext(t *testing.T) *Context {
	t.Helper()

	store, err := session.Open(context.Background(), filepath.Join(t.TempDir(), "hewn.db"))
	if err != nil {
		t.Fatalf("session.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	sb, err := sandbox.New(t.TempDir())
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	t.Cleanup(func() { _ = sb.Close() })

	tools := tool.NewRegistry()
	tools.Register(tool.NewRead(sb))
	tools.Register(tool.NewBash(sb, nil))

	sess, err := store.CreateSession(context.Background(), "/repo", "anthropic", "claude-opus-4-8", "hello")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	loop := &agent.Loop{
		Model:     "claude-opus-4-8",
		Session:   store,
		SessionID: sess.ID,
	}

	reg := NewRegistry()
	Register(reg)

	return &Context{
		Loop:         loop,
		Store:        store,
		Tools:        tools,
		Registry:     reg,
		CWD:          "/repo",
		ProviderName: "anthropic",
	}
}

func TestHelpCommand_ListsAllNine(t *testing.T) {
	c := newTestContext(t)

	result, handled := c.Registry.Dispatch(context.Background(), c, "/help")
	if !handled {
		t.Fatal("Dispatch(/help) handled = false")
	}

	want := []string{"/help", "/model", "/new", "/clear", "/compact", "/quit", "/tools", "/cost", "/export"}
	for _, w := range want {
		if !strings.Contains(result.Output, w) {
			t.Errorf("/help output missing %q:\n%s", w, result.Output)
		}
	}
}

func TestModelCommand_ShowAndSet(t *testing.T) {
	c := newTestContext(t)
	ctx := context.Background()

	result, _ := c.Registry.Dispatch(ctx, c, "/model")
	if !strings.Contains(result.Output, "claude-opus-4-8") {
		t.Errorf("/model (show) = %q, want it to mention the current model", result.Output)
	}

	result, _ = c.Registry.Dispatch(ctx, c, "/model claude-haiku-4-5")
	if !strings.Contains(result.Output, "claude-haiku-4-5") {
		t.Errorf("/model claude-haiku-4-5 = %q", result.Output)
	}
	if c.Loop.Model != "claude-haiku-4-5" {
		t.Errorf("Loop.Model = %q, want %q", c.Loop.Model, "claude-haiku-4-5")
	}
}

// fakeModelProvider implements just enough of provider.Provider to test
// the /model and /models Choices path -- Stream is never called by any
// slash command, so it's left unimplemented.
type fakeModelProvider struct {
	models []provider.ModelInfo
}

func (p fakeModelProvider) Name() string { return "fake" }

func (p fakeModelProvider) Models(context.Context) ([]provider.ModelInfo, error) {
	return p.models, nil
}

func (p fakeModelProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	panic("not implemented: no slash command should call Stream")
}

func TestModelCommand_NoArgsOffersChoices(t *testing.T) {
	c := newTestContext(t)
	c.Loop.Provider = fakeModelProvider{models: []provider.ModelInfo{
		{ID: "claude-opus-4-8"}, {ID: "claude-haiku-4-5"},
	}}

	result, handled := c.Registry.Dispatch(context.Background(), c, "/model")
	if !handled {
		t.Fatal("Dispatch(/model) handled = false")
	}
	if result.SelectCommand != "model" {
		t.Errorf("SelectCommand = %q, want %q", result.SelectCommand, "model")
	}
	want := []string{"claude-opus-4-8", "claude-haiku-4-5"}
	if len(result.Choices) != len(want) {
		t.Fatalf("Choices = %v, want %v", result.Choices, want)
	}
	for i, id := range want {
		if result.Choices[i] != id {
			t.Errorf("Choices[%d] = %q, want %q", i, result.Choices[i], id)
		}
	}
	if !strings.Contains(result.Output, "current model") || !strings.Contains(result.Output, "claude-opus-4-8") {
		t.Errorf("Output = %q, want it to still carry the same info as plain text for non-picker frontends", result.Output)
	}
}

func TestModelCommand_WithArgsNeverOffersChoices(t *testing.T) {
	c := newTestContext(t)
	c.Loop.Provider = fakeModelProvider{models: []provider.ModelInfo{{ID: "claude-opus-4-8"}}}

	result, _ := c.Registry.Dispatch(context.Background(), c, "/model claude-haiku-4-5")
	if result.Choices != nil {
		t.Errorf("Choices = %v, want nil -- setting a model directly shouldn't open a picker", result.Choices)
	}
}

func TestNewCommand_StartsFreshSession(t *testing.T) {
	c := newTestContext(t)
	ctx := context.Background()
	oldSessionID := c.Loop.SessionID

	result, handled := c.Registry.Dispatch(ctx, c, "/new")
	if !handled {
		t.Fatal("Dispatch(/new) handled = false")
	}
	if c.Loop.SessionID == oldSessionID {
		t.Error("SessionID unchanged after /new, want a new session ID")
	}
	if !strings.Contains(result.Output, c.Loop.SessionID) {
		t.Errorf("/new output = %q, want it to mention the new session ID %s", result.Output, c.Loop.SessionID)
	}
	if !result.ClearTranscript {
		t.Error("/new ClearTranscript = false, want true")
	}

	// The new session should actually exist in the store.
	if _, err := c.Store.LoadSession(ctx, c.Loop.SessionID); err != nil {
		t.Errorf("new session %s not found in store: %v", c.Loop.SessionID, err)
	}
}

func TestClearCommand_KeepsSameSession(t *testing.T) {
	c := newTestContext(t)
	ctx := context.Background()
	sessionID := c.Loop.SessionID

	result, handled := c.Registry.Dispatch(ctx, c, "/clear")
	if !handled {
		t.Fatal("Dispatch(/clear) handled = false")
	}
	if c.Loop.SessionID != sessionID {
		t.Errorf("SessionID changed after /clear: %s -> %s, want unchanged", sessionID, c.Loop.SessionID)
	}
	if !strings.Contains(result.Output, "cleared") {
		t.Errorf("/clear output = %q, want it to mention clearing", result.Output)
	}
	if !result.ClearTranscript {
		t.Error("/clear ClearTranscript = false, want true")
	}
}

func TestCompactCommand_NoOpOnShortHistory(t *testing.T) {
	c := newTestContext(t)
	result, handled := c.Registry.Dispatch(context.Background(), c, "/compact")
	if !handled {
		t.Fatal("Dispatch(/compact) handled = false")
	}
	if !strings.Contains(result.Output, "nothing to compact") {
		t.Errorf("/compact = %q, want it to report there's nothing to compact yet (fresh context has no history)", result.Output)
	}
}

func TestQuitCommand_SetsQuit(t *testing.T) {
	c := newTestContext(t)
	result, handled := c.Registry.Dispatch(context.Background(), c, "/quit")
	if !handled {
		t.Fatal("Dispatch(/quit) handled = false")
	}
	if !result.Quit {
		t.Error("/quit Result.Quit = false, want true")
	}
}

func TestToolsCommand_ListsRegisteredTools(t *testing.T) {
	c := newTestContext(t)
	result, handled := c.Registry.Dispatch(context.Background(), c, "/tools")
	if !handled {
		t.Fatal("Dispatch(/tools) handled = false")
	}
	for _, want := range []string{"read", "bash"} {
		if !strings.Contains(result.Output, want) {
			t.Errorf("/tools output missing %q:\n%s", want, result.Output)
		}
	}
}

func TestCostCommand_ReflectsTotalUsage(t *testing.T) {
	c := newTestContext(t)

	result, handled := c.Registry.Dispatch(context.Background(), c, "/cost")
	if !handled {
		t.Fatal("Dispatch(/cost) handled = false")
	}
	if !strings.Contains(result.Output, "input: 0") {
		t.Errorf("/cost before any turn = %q, want zeroed usage", result.Output)
	}
}

func TestExportCommand_WritesJSONFile(t *testing.T) {
	c := newTestContext(t)
	ctx := context.Background()

	if _, err := c.Store.AppendMessage(ctx, c.Loop.SessionID, session.RoleUser, json.RawMessage(`[{"Kind":0,"Text":"hi"}]`), nil); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "export.json")
	result, handled := c.Registry.Dispatch(ctx, c, "/export "+path)
	if !handled {
		t.Fatal("Dispatch(/export) handled = false")
	}
	if !strings.Contains(result.Output, "exported") {
		t.Errorf("/export result = %q, want it to confirm the export", result.Output)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read exported file: %v", err)
	}
	var messages []session.Message
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatalf("exported file is not valid JSON: %v", err)
	}
	if len(messages) != 1 || messages[0].Role != session.RoleUser {
		t.Errorf("exported messages = %+v, want one user message", messages)
	}
}

func TestExportCommand_DefaultPath(t *testing.T) {
	c := newTestContext(t)
	ctx := context.Background()

	dir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	result, _ := c.Registry.Dispatch(ctx, c, "/export")
	if !strings.Contains(result.Output, c.Loop.SessionID) {
		t.Errorf("/export (default path) = %q, want it to mention the session ID in the default filename", result.Output)
	}
}
