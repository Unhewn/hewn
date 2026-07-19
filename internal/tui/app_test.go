package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	return NewModel(loop, NewApprover(), slashCtx, "/repo", "fake", "you")
}

func asModel(t *testing.T, tm tea.Model) Model {
	t.Helper()
	m, ok := tm.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", tm)
	}
	return m
}

// TestView_FramedOutputFillsExactlyToHeight guards the frame/border width
// math in View and handleResize: every nested box (the input box in
// particular) must land within the outer frame's own wrap budget. Get the
// arithmetic wrong -- as a prior version of this code did, off by the
// frame's own Padding(0, 1) -- and lipgloss word-wraps the offending box's
// border onto an extra line, which this test catches as a line count that
// no longer matches the terminal height it was sized for.
func TestView_FramedOutputFillsExactlyToHeight(t *testing.T) {
	m := newTestModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2 := asModel(t, updated)

	lines := strings.Split(m2.View(), "\n")
	if len(lines) != 30 {
		t.Fatalf("View() produced %d lines, want exactly 30 (msg.Height) -- a nested box likely wrapped onto an extra line", len(lines))
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 100 {
			t.Errorf("line %d width = %d, want 100 (full terminal width): %q", i, w, l)
		}
	}
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
	m := NewModel(loop, NewApprover(), slashCtx, "/repo", "fake", "you")
	m.transcript = []transcriptItem{newUserItem("old message")}
	m.input.SetValue("/wipe")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := asModel(t, updated)

	if len(m2.transcript) != 1 || m2.transcript[0].text != "wiped" {
		t.Errorf("transcript = %+v, want just the wipe command's own output", m2.transcript)
	}
}

// TestPicker_SelectionDispatchesAndAppendsOnlyOneSystemItem drives the full
// /model picker flow: dispatching the no-arg command opens a picker instead
// of appending its listing to the transcript, and confirming a selection
// re-dispatches "/model <choice>" -- so the only thing that ever lands in
// the transcript is that final confirmation, not the list that led to it.
// This is the fix for the "appends info" complaint on repeated slash use.
func TestPicker_SelectionDispatchesAndAppendsOnlyOneSystemItem(t *testing.T) {
	reg := slash.NewRegistry()
	reg.Register(slash.Command{
		Name: "model",
		Run: func(_ context.Context, _ *slash.Context, args string) slash.Result {
			if args == "" {
				return slash.Result{
					Output:        "current model: a\nmodels available:\n  a\n  b",
					Choices:       []string{"a", "b"},
					SelectCommand: "model",
				}
			}
			return slash.Result{Output: "model set to " + args}
		},
	})
	loop := &agent.Loop{Tools: tool.NewRegistry(), Approval: tool.NewPolicy(nil, true), Model: "a"}
	slashCtx := &slash.Context{Loop: loop, Registry: reg}
	m := NewModel(loop, NewApprover(), slashCtx, "/repo", "fake", "you")

	resized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = asModel(t, resized)

	m.input.SetValue("/model")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := asModel(t, updated)

	if m2.state != statePicker || m2.picker == nil {
		t.Fatalf("state = %v, picker = %v, want an open picker after /model with no args", m2.state, m2.picker)
	}
	if len(m2.transcript) != 0 {
		t.Errorf("transcript = %+v, want nothing appended while the picker is open", m2.transcript)
	}

	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := asModel(t, updated2)

	if m3.state != stateIdle || m3.picker != nil {
		t.Errorf("state = %v, picker = %v, want idle with the picker closed after selecting", m3.state, m3.picker)
	}
	if len(m3.transcript) != 1 {
		t.Fatalf("transcript = %+v, want exactly one item -- the selection's own confirmation, not the listing that opened the picker", m3.transcript)
	}
	if m3.transcript[0].text != "model set to a" {
		t.Errorf("transcript[0] = %+v, want the confirmation for choice %q (the first, highlighted one)", m3.transcript[0], "a")
	}
}

func TestPicker_EscCancelsWithoutDispatching(t *testing.T) {
	reg := slash.NewRegistry()
	reg.Register(slash.Command{
		Name: "model",
		Run: func(_ context.Context, _ *slash.Context, args string) slash.Result {
			if args == "" {
				return slash.Result{Output: "listing", Choices: []string{"a", "b"}, SelectCommand: "model"}
			}
			t.Fatal("model command re-dispatched with args after Esc, want no dispatch at all")
			return slash.Result{}
		},
	})
	loop := &agent.Loop{Tools: tool.NewRegistry(), Approval: tool.NewPolicy(nil, true), Model: "a"}
	slashCtx := &slash.Context{Loop: loop, Registry: reg}
	m := NewModel(loop, NewApprover(), slashCtx, "/repo", "fake", "you")

	resized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = asModel(t, resized)

	m.input.SetValue("/model")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := asModel(t, updated)

	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m3 := asModel(t, updated2)

	if m3.state != stateIdle || m3.picker != nil {
		t.Errorf("state = %v, picker = %v, want idle with the picker closed after Esc", m3.state, m3.picker)
	}
	if len(m3.transcript) != 0 {
		t.Errorf("transcript = %+v, want nothing appended on cancel", m3.transcript)
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
