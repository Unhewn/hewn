package tui

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/unhewn/hewn/internal/agent"
	"github.com/unhewn/hewn/internal/slash"
	"github.com/unhewn/hewn/internal/tool"
)

var errFake = errors.New("fake stream error")

// newTestModel builds a Model with a Loop that has no Provider configured.
// internal/tui must never import internal/provider at all (depguard's
// tui-layer rule, enforcing AGENTS.md invariant #1 -- and it applies to
// test files too, deliberately, unlike a few other linters), so no test in
// this package can exercise a real Loop.Run turn end to end; that's
// covered by internal/agent's own tests instead. Every test here either
// drives Update with synthetic messages or calls handleAgentEvent
// directly, never Loop.Run.
func newTestModel(t *testing.T) Model {
	t.Helper()
	tools := tool.NewRegistry()
	loop := &agent.Loop{
		Tools:    tools,
		Approval: tool.NewPolicy(nil, true), // yolo: nil approver is never consulted
		Model:    "test-model",
	}
	registry := newTestSlashRegistry()
	slashCtx := &slash.Context{Loop: loop, Tools: tools, Registry: registry, CWD: "/repo", ProviderName: "fake"}
	return NewModel(loop, NewApprover(), slashCtx, "/repo", "fake")
}

func asModel(t *testing.T, tm tea.Model) Model {
	t.Helper()
	m, ok := tm.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", tm)
	}
	return m
}

func TestUpdate_TextDeltaAccumulatesWithoutFlushingUntilTick(t *testing.T) {
	m := newTestModel(t)
	m.state = stateStreaming

	updated, _ := m.Update(agentEventMsg(agent.NewTextDelta("hello")))
	m2 := asModel(t, updated)

	if !m2.dirty {
		t.Error("dirty = false after a text delta, want true")
	}
	if len(m2.transcript) != 1 || m2.transcript[0].kind != itemAssistant {
		t.Fatalf("transcript = %+v, want one assistant item", m2.transcript)
	}
	if m2.transcript[0].raw.String() != "hello" {
		t.Errorf("raw = %q, want %q", m2.transcript[0].raw.String(), "hello")
	}

	updated2, cmd := m2.Update(tickMsg{})
	m3 := asModel(t, updated2)
	if m3.dirty {
		t.Error("dirty = true after tick, want false (tick should flush)")
	}
	if cmd == nil {
		t.Error("tick while streaming should re-arm another tick Cmd")
	}
}

func TestUpdate_TickWhileIdleDoesNotRearm(t *testing.T) {
	m := newTestModel(t)
	m.state = stateIdle

	_, cmd := m.Update(tickMsg{})
	if cmd != nil {
		t.Error("tick while idle should not re-arm another tick")
	}
}

func TestUpdate_CtrlC_InterruptsInFlightTurn(t *testing.T) {
	m := newTestModel(t)
	canceled := false
	m.turnCancel = func() { canceled = true }
	m.state = stateStreaming

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m2 := asModel(t, updated)

	if !canceled {
		t.Error("turnCancel was not called on Ctrl+C during a turn")
	}
	if m2.turnCancel != nil {
		t.Error("turnCancel not cleared after interrupt")
	}
	if cmd != nil {
		t.Error("interrupting should not itself quit")
	}
}

func TestUpdate_CtrlC_SinglePressWhileIdleDoesNotQuit(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil {
		t.Error("a single Ctrl+C while idle should not quit (never lose a session to a stray Ctrl+C)")
	}
}

func TestUpdate_CtrlC_SecondConsecutivePressQuits(t *testing.T) {
	m := newTestModel(t)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil {
		t.Fatal("first Ctrl+C should not quit")
	}
	m2 := asModel(t, updated)

	_, cmd2 := m2.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd2 == nil {
		t.Fatal("second consecutive Ctrl+C should quit")
	}
	if _, ok := cmd2().(tea.QuitMsg); !ok {
		t.Errorf("second Ctrl+C Cmd produced %T, want tea.QuitMsg", cmd2())
	}
}

func TestUpdate_CtrlC_ResetByAnotherKey(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m2 := asModel(t, updated)
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m3 := asModel(t, updated2)

	_, cmd := m3.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil {
		t.Error("Ctrl+C after an intervening keypress should not quit -- it's not consecutive anymore")
	}
}

func TestUpdate_ApprovalKey_Decisions(t *testing.T) {
	tests := []struct {
		key  string
		want tool.Decision
	}{
		{"a", tool.DecisionAllowOnce},
		{"A", tool.DecisionAllowSession},
		{"d", tool.DecisionDeny},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			m := newTestModel(t)
			respCh := make(chan approvalResponse, 1)
			m.pendingApproval = &pendingApproval{req: tool.ApprovalRequest{Tool: "bash"}, response: respCh}
			m.state = stateAwaitingApproval

			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
			m2 := asModel(t, updated)

			select {
			case resp := <-respCh:
				if resp.decision != tt.want {
					t.Errorf("decision = %v, want %v", resp.decision, tt.want)
				}
			default:
				t.Fatal("no response sent on the channel")
			}
			if m2.pendingApproval != nil {
				t.Error("pendingApproval not cleared")
			}
			if m2.state != stateStreaming {
				t.Errorf("state = %v, want stateStreaming (resuming the in-flight turn)", m2.state)
			}
		})
	}
}

func TestUpdate_ApprovalKey_IgnoresOtherKeys(t *testing.T) {
	m := newTestModel(t)
	respCh := make(chan approvalResponse, 1)
	m.pendingApproval = &pendingApproval{req: tool.ApprovalRequest{Tool: "bash"}, response: respCh}
	m.state = stateAwaitingApproval

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m2 := asModel(t, updated)

	select {
	case <-respCh:
		t.Fatal("unexpected response sent for an unrelated key")
	default:
	}
	if m2.pendingApproval == nil {
		t.Error("pendingApproval cleared for an unrelated key")
	}
	if m2.state != stateAwaitingApproval {
		t.Error("state changed for an unrelated key")
	}
}

func TestUpdate_EnterIgnoredWhileStreaming(t *testing.T) {
	m := newTestModel(t)
	m.state = stateStreaming
	m.input.SetValue("hello")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := asModel(t, updated)

	if cmd != nil {
		t.Error("Enter mid-turn should not start anything")
	}
	if m2.input.Value() != "hello" {
		t.Error("input should be untouched while a turn is in flight")
	}
}

func TestUpdate_EnterDispatchesSlashCommand(t *testing.T) {
	m := newTestModel(t)
	m.transcript = []transcriptItem{newUserItem("previous")}
	m.input.SetValue("/help")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := asModel(t, updated)

	if len(m2.transcript) != 2 {
		t.Fatalf("transcript = %+v, want the previous item plus the help output", m2.transcript)
	}
	if m2.input.Value() != "" {
		t.Error("input not cleared after dispatch")
	}
}

func TestUpdate_ClearTranscriptWipesDisplay(t *testing.T) {
	reg := slash.NewRegistry()
	reg.Register(slash.Command{
		Name: "wipe",
		Run: func(context.Context, *slash.Context, string) slash.Result {
			return slash.Result{Output: "wiped", ClearTranscript: true}
		},
	})
	loop := &agent.Loop{Tools: tool.NewRegistry(), Approval: tool.NewPolicy(nil, true), Model: "test-model"}
	slashCtx := &slash.Context{Loop: loop, Registry: reg}
	m := NewModel(loop, NewApprover(), slashCtx, "/repo", "fake")
	m.transcript = []transcriptItem{newUserItem("old message")}
	m.input.SetValue("/wipe")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := asModel(t, updated)

	if len(m2.transcript) != 1 || m2.transcript[0].text != "wiped" {
		t.Errorf("transcript = %+v, want just the wipe command's own output", m2.transcript)
	}
}

func TestHandleAgentEvent_ToolCallLifecycle(t *testing.T) {
	m := newTestModel(t)

	m.handleAgentEvent(agent.NewToolCallStart("t1", "read"))
	m.handleAgentEvent(agent.NewToolCallEnd("t1", "read", []byte(`{"path":"x"}`)))
	m.handleAgentEvent(agent.NewToolCallResult("t1", "file contents", false))

	if len(m.transcript) != 1 || m.transcript[0].kind != itemToolCall {
		t.Fatalf("transcript = %+v, want one tool call item", m.transcript)
	}
	tc := m.transcript[0].tool
	if tc.name != "read" || tc.input != `{"path":"x"}` || !tc.done || tc.result != "file contents" || tc.isError {
		t.Errorf("tool call item = %+v", tc)
	}
}

func TestHandleAgentEvent_ErrorAppearsAsSystemItem(t *testing.T) {
	m := newTestModel(t)
	m.handleAgentEvent(agent.NewTextDelta("partial"))
	m.handleAgentEvent(agent.NewError(errFake))

	if len(m.transcript) != 2 {
		t.Fatalf("transcript = %+v, want the assistant segment plus a system error item", m.transcript)
	}
	if !m.transcript[0].closed {
		t.Error("assistant segment should be closed once an error arrives")
	}
	if m.transcript[1].kind != itemSystem {
		t.Errorf("transcript[1].kind = %v, want itemSystem", m.transcript[1].kind)
	}
}

func TestToggleExpand_OnlyAffectsMostRecentToolCall(t *testing.T) {
	m := newTestModel(t)
	m.handleAgentEvent(agent.NewToolCallStart("t1", "read"))
	m.handleAgentEvent(agent.NewToolCallResult("t1", "result1", false))
	m.handleAgentEvent(agent.NewToolCallStart("t2", "bash"))
	m.handleAgentEvent(agent.NewToolCallResult("t2", "result2", false))

	m.toggleExpand()
	if m.expandedToolCallID != "t2" {
		t.Errorf("expandedToolCallID = %q, want t2 (the most recent)", m.expandedToolCallID)
	}

	m.toggleExpand()
	if m.expandedToolCallID != "" {
		t.Errorf("expandedToolCallID = %q, want empty after toggling off", m.expandedToolCallID)
	}
}
