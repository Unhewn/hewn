package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/unhewn/hewn/internal/provider"
	"github.com/unhewn/hewn/internal/session"
	"github.com/unhewn/hewn/internal/tool"
)

func openTestSessionStore(t *testing.T) *session.Store {
	t.Helper()
	store, err := session.Open(context.Background(), t.TempDir()+"/hewn.db")
	if err != nil {
		t.Fatalf("session.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestLoop_PersistsMessagesAndUsage(t *testing.T) {
	ctx := context.Background()
	store := openTestSessionStore(t)
	sess, err := store.CreateSession(ctx, "/repo", "fake", "test-model", "read x")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	p := &fakeProvider{turns: [][]provider.Event{
		{
			{Kind: provider.KindToolCallStart, ToolCallStart: provider.ToolCallStart{ID: "t1", Name: "read"}},
			{Kind: provider.KindToolCallEnd, ToolCallEnd: provider.ToolCallEnd{ID: "t1", Name: "read", Input: json.RawMessage(`{"path":"x"}`)}},
			{Kind: provider.KindUsage, Usage: provider.Usage{InputTokens: 10, OutputTokens: 4}},
			{Kind: provider.KindStopReason, StopReason: provider.StopReasonToolUse},
		},
		{
			{Kind: provider.KindTextDelta, TextDelta: "done"},
			{Kind: provider.KindUsage, Usage: provider.Usage{InputTokens: 20, OutputTokens: 6}},
			{Kind: provider.KindStopReason, StopReason: provider.StopReasonEndTurn},
		},
	}}

	ft := &fakeTool{name: "read", risk: tool.RiskReadOnly, result: tool.Result{Output: "file contents"}}
	registry := tool.NewRegistry()
	registry.Register(ft)

	l := &Loop{
		Provider:  p,
		Tools:     registry,
		Approval:  tool.NewPolicy(fixedApprover{decision: tool.DecisionAllowOnce}, false),
		Model:     "test-model",
		Session:   store,
		SessionID: sess.ID,
	}

	drainEvents(l.Run(ctx, "read x"))

	messages, err := store.LoadMessages(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadMessages: %v", err)
	}
	if len(messages) != len(l.history) {
		t.Fatalf("persisted %d messages, want %d (matching in-memory history)", len(messages), len(l.history))
	}
	if len(messages) != 4 {
		t.Fatalf("persisted %d messages, want 4 (user, assistant tool-use, user tool-result, assistant text)", len(messages))
	}

	wantRoles := []session.Role{session.RoleUser, session.RoleAssistant, session.RoleUser, session.RoleAssistant}
	for i, msg := range messages {
		if msg.Role != wantRoles[i] {
			t.Errorf("messages[%d].Role = %s, want %s", i, msg.Role, wantRoles[i])
		}
		if msg.Seq != i+1 {
			t.Errorf("messages[%d].Seq = %d, want %d", i, msg.Seq, i+1)
		}
	}

	// Usage: message[1] (first assistant turn) and message[3] (second
	// assistant turn) should both carry the usage seen in their respective
	// stream; the two user messages should have none.
	if messages[1].Usage == nil || messages[1].Usage.InputTokens != 10 || messages[1].Usage.OutputTokens != 4 {
		t.Errorf("messages[1].Usage = %+v, want {10 4}", messages[1].Usage)
	}
	if messages[3].Usage == nil || messages[3].Usage.InputTokens != 20 || messages[3].Usage.OutputTokens != 6 {
		t.Errorf("messages[3].Usage = %+v, want {20 6}", messages[3].Usage)
	}
	if messages[0].Usage != nil || messages[2].Usage != nil {
		t.Error("user messages should not carry usage")
	}

	// read is RiskReadOnly, so its call is never gated: DecisionNotGated
	// persists as a nil approved column.
	calls, err := store.LoadToolCalls(ctx, messages[1].ID)
	if err != nil {
		t.Fatalf("LoadToolCalls: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d tool calls for message %s, want 1", len(calls), messages[1].ID)
	}
	if calls[0].Approved != nil {
		t.Errorf("calls[0].Approved = %v, want nil (read-only, never gated)", calls[0].Approved)
	}
	if calls[0].Tool != "read" || calls[0].Result != "file contents" {
		t.Errorf("calls[0] = %+v, unexpected tool/result", calls[0])
	}
}

func TestLoop_PersistsToolCallApprovedColumn(t *testing.T) {
	ctx := context.Background()
	store := openTestSessionStore(t)
	sess, err := store.CreateSession(ctx, "/repo", "fake", "test-model", "run tests")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	p := &fakeProvider{turns: [][]provider.Event{
		{
			{Kind: provider.KindToolCallStart, ToolCallStart: provider.ToolCallStart{ID: "t1", Name: "bash"}},
			{Kind: provider.KindToolCallEnd, ToolCallEnd: provider.ToolCallEnd{ID: "t1", Name: "bash", Input: json.RawMessage(`{}`)}},
			{Kind: provider.KindStopReason, StopReason: provider.StopReasonToolUse},
		},
		{
			{Kind: provider.KindStopReason, StopReason: provider.StopReasonEndTurn},
		},
	}}

	ft := &fakeTool{name: "bash", risk: tool.RiskArbitrary, result: tool.Result{Output: "ok"}}
	registry := tool.NewRegistry()
	registry.Register(ft)

	l := &Loop{
		Provider:  p,
		Tools:     registry,
		Approval:  tool.NewPolicy(fixedApprover{decision: tool.DecisionAllowSession}, false),
		Model:     "test-model",
		Session:   store,
		SessionID: sess.ID,
	}

	drainEvents(l.Run(ctx, "run tests"))

	messages, err := store.LoadMessages(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadMessages: %v", err)
	}
	assistantMsg := messages[1]

	calls, err := store.LoadToolCalls(ctx, assistantMsg.ID)
	if err != nil {
		t.Fatalf("LoadToolCalls: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d tool_calls for message %s, want 1", len(calls), assistantMsg.ID)
	}
	if calls[0].Approved == nil || *calls[0].Approved != int(tool.DecisionAllowSession) {
		t.Errorf("approved = %v, want pointer to %d (DecisionAllowSession)", calls[0].Approved, tool.DecisionAllowSession)
	}
	if calls[0].Tool != "bash" {
		t.Errorf("tool = %q, want %q", calls[0].Tool, "bash")
	}
}

func TestHistoryFromMessages_RoundTrip(t *testing.T) {
	ctx := context.Background()
	store := openTestSessionStore(t)
	sess, err := store.CreateSession(ctx, "/repo", "fake", "test-model", "hi")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	original := []provider.Message{
		{Role: provider.RoleUser, Content: []provider.ContentBlock{{Kind: provider.ContentText, Text: "hi"}}},
		{Role: provider.RoleAssistant, Content: []provider.ContentBlock{
			{Kind: provider.ContentToolUse, ToolUseID: "t1", ToolName: "read", ToolInput: json.RawMessage(`{"path":"x"}`)},
		}},
		{Role: provider.RoleUser, Content: []provider.ContentBlock{
			{Kind: provider.ContentToolResult, ToolResultID: "t1", ToolResultContent: "contents"},
		}},
	}

	for _, m := range original {
		raw, marshalErr := json.Marshal(m.Content)
		if marshalErr != nil {
			t.Fatalf("marshal content: %v", marshalErr)
		}
		if _, appendErr := store.AppendMessage(ctx, sess.ID, sessionRole(m.Role), raw, nil); appendErr != nil {
			t.Fatalf("AppendMessage: %v", appendErr)
		}
	}

	stored, err := store.LoadMessages(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadMessages: %v", err)
	}

	history, err := HistoryFromMessages(stored)
	if err != nil {
		t.Fatalf("HistoryFromMessages: %v", err)
	}
	if len(history) != len(original) {
		t.Fatalf("HistoryFromMessages returned %d messages, want %d", len(history), len(original))
	}
	for i, want := range original {
		got := history[i]
		if got.Role != want.Role {
			t.Errorf("history[%d].Role = %s, want %s", i, got.Role, want.Role)
		}
		if len(got.Content) != len(want.Content) {
			t.Fatalf("history[%d].Content = %+v, want %+v", i, got.Content, want.Content)
		}
		// Compare only the fields meaningful for each block's Kind. A nil
		// json.RawMessage field on a block that doesn't use ToolInput
		// round-trips through JSON as the literal bytes "null" rather than
		// staying nil (encoding/json marshals a nil RawMessage as `null`,
		// and RawMessage.UnmarshalJSON stores whatever bytes it's given) --
		// that's expected stdlib behavior, not a bug, so it's excluded here.
		gotBlock, wantBlock := got.Content[0], want.Content[0]
		if gotBlock.Kind != wantBlock.Kind || gotBlock.Text != wantBlock.Text ||
			gotBlock.ToolUseID != wantBlock.ToolUseID || gotBlock.ToolName != wantBlock.ToolName ||
			gotBlock.ToolResultID != wantBlock.ToolResultID || gotBlock.ToolResultContent != wantBlock.ToolResultContent {
			t.Errorf("history[%d].Content[0] = %+v, want %+v", i, gotBlock, wantBlock)
		}
		if wantBlock.Kind == provider.ContentToolUse && string(gotBlock.ToolInput) != string(wantBlock.ToolInput) {
			t.Errorf("history[%d].Content[0].ToolInput = %s, want %s", i, gotBlock.ToolInput, wantBlock.ToolInput)
		}
	}
}

func TestLoop_SeedHistoryReachesProvider(t *testing.T) {
	p := &fakeProvider{turns: [][]provider.Event{
		{
			{Kind: provider.KindTextDelta, TextDelta: "ok"},
			{Kind: provider.KindStopReason, StopReason: provider.StopReasonEndTurn},
		},
	}}

	l := &Loop{
		Provider: p,
		Tools:    tool.NewRegistry(),
		Approval: tool.NewPolicy(fixedApprover{decision: tool.DecisionDeny}, false),
		Model:    "test-model",
	}

	seeded := []provider.Message{
		{Role: provider.RoleUser, Content: []provider.ContentBlock{{Kind: provider.ContentText, Text: "earlier turn"}}},
		{Role: provider.RoleAssistant, Content: []provider.ContentBlock{{Kind: provider.ContentText, Text: "earlier reply"}}},
	}
	l.SeedHistory(seeded)

	drainEvents(l.Run(context.Background(), "continue"))

	p.mu.Lock()
	got := p.lastRequest.Messages
	p.mu.Unlock()

	// The seeded turns plus the new user message.
	if len(got) != 3 {
		t.Fatalf("provider saw %d messages, want 3 (2 seeded + 1 new)", len(got))
	}
	if got[0].Content[0].Text != "earlier turn" || got[1].Content[0].Text != "earlier reply" {
		t.Errorf("seeded history not passed to provider: %+v", got[:2])
	}
	if got[2].Content[0].Text != "continue" {
		t.Errorf("new user message = %+v, want %q", got[2], "continue")
	}
}

func TestLoop_NoSessionSkipsPersistenceCleanly(t *testing.T) {
	p := &fakeProvider{turns: [][]provider.Event{
		{
			{Kind: provider.KindTextDelta, TextDelta: "hi"},
			{Kind: provider.KindStopReason, StopReason: provider.StopReasonEndTurn},
		},
	}}

	l := &Loop{
		Provider: p,
		Tools:    tool.NewRegistry(),
		Approval: tool.NewPolicy(fixedApprover{decision: tool.DecisionDeny}, false),
		Model:    "test-model",
		// Session left nil deliberately.
	}

	events := drainEvents(l.Run(context.Background(), "hello"))
	for _, ev := range events {
		if ev.Kind == KindError {
			t.Fatalf("got KindError %+v with no Session configured; persistence must no-op silently", ev)
		}
	}
}
