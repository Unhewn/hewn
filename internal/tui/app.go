// Package tui is the Bubble Tea front end: a view over the agent loop's
// event bus. It never calls a provider, executes a tool, or touches the
// database itself (AGENTS.md invariant #1) -- cmd/hewn builds the
// *agent.Loop (via the same buildLoop helper the headless and interactive
// modes use) and hands it, already wired, to Start.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/unhewn/hewn/internal/agent"
	"github.com/unhewn/hewn/internal/slash"
	"github.com/unhewn/hewn/internal/tool"
)

// tickInterval is how often streaming text gets flushed into the
// viewport, per HEWN.md §4: streaming must not flicker; re-render the
// viewport at ~30fps, not per-token.
const tickInterval = 33 * time.Millisecond

// reservedLines is how much vertical space the header, status bar, input,
// and their surrounding blank lines take up, leaving the rest for the
// viewport.
const reservedLines = 6

// pendingApproval is one tool call awaiting a decision, mirroring
// approvalRequest but held on the model between receiving it and the
// human answering it.
type pendingApproval struct {
	req      tool.ApprovalRequest
	response chan approvalResponse
}

// Model is the root Bubble Tea model.
type Model struct {
	loop         *agent.Loop
	approver     *Approver
	cwd          string
	providerName string

	slashRegistry *slash.Registry
	slashCtx      *slash.Context

	viewport viewport.Model
	input    textarea.Model
	width    int
	height   int

	transcript         []transcriptItem
	dirty              bool
	glamourRenderer    *glamour.TermRenderer
	expandedToolCallID string

	state      appState
	turnCancel context.CancelFunc
	events     <-chan agent.Event

	pendingApproval *pendingApproval
	lastKeyWasCtrlC bool
}

// NewModel builds the root Model. loop must already be fully wired (a
// Provider, Tools, and an Approval policy built from approver -- see
// buildLoop in cmd/hewn); Model only ever drives it through Run. slashCtx
// is built by the caller too (it needs a *session.Store, which this
// package must never import -- AGENTS.md invariant #1, "the TUI never
// touches the database").
func NewModel(loop *agent.Loop, approver *Approver, slashCtx *slash.Context, cwd, providerName string) Model {
	vp := viewport.New(80, 20)
	vp.MouseWheelEnabled = true
	vp.SetContent(styleSystem.Render("Welcome to hewn! Type a message, or /help for commands."))

	return Model{
		loop:            loop,
		approver:        approver,
		cwd:             cwd,
		providerName:    providerName,
		slashRegistry:   slashCtx.Registry,
		slashCtx:        slashCtx,
		viewport:        vp,
		input:           newInput(),
		glamourRenderer: newGlamourRenderer(80),
	}
}

// Start runs the TUI to completion. approver must be the same value
// passed as buildLoop's approver argument when loop was constructed.
func Start(loop *agent.Loop, approver *Approver, slashCtx *slash.Context, cwd, providerName string) error {
	m := NewModel(loop, approver, slashCtx, cwd, providerName)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

// --- messages -------------------------------------------------------------

type (
	agentEventMsg      agent.Event
	turnDoneMsg        struct{}
	approvalRequestMsg approvalRequest
	tickMsg            struct{}
)

func waitForEvent(events <-chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return turnDoneMsg{}
		}
		return agentEventMsg(ev)
	}
}

func waitForApproval(requests <-chan approvalRequest) tea.Cmd {
	return func() tea.Msg {
		return approvalRequestMsg(<-requests)
	}
}

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// --- Init / Update / View ---------------------------------------------------

// Init starts listening for approval requests immediately -- a request can
// arrive from the very first turn, before any tick or event has ever
// fired.
func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, waitForApproval(m.approver.requests))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg), nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case agentEventMsg:
		m.handleAgentEvent(agent.Event(msg))
		return m, waitForEvent(m.events)

	case turnDoneMsg:
		m.closeCurrentAssistant()
		m.flush()
		m.state = stateIdle
		m.events = nil
		m.turnCancel = nil
		return m, nil

	case approvalRequestMsg:
		m.pendingApproval = &pendingApproval{req: msg.req, response: msg.response}
		m.state = stateAwaitingApproval
		return m, waitForApproval(m.approver.requests)

	case tickMsg:
		if m.dirty {
			m.flush()
		}
		if m.state == stateStreaming {
			return m, tick()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) handleResize(msg tea.WindowSizeMsg) Model {
	m.width = msg.Width
	m.height = msg.Height
	m.viewport.Width = msg.Width
	m.viewport.Height = msg.Height - reservedLines
	m.input.SetWidth(msg.Width)
	m.glamourRenderer = newGlamourRenderer(msg.Width)
	m.flush()
	return m
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	wasCtrlC := m.lastKeyWasCtrlC
	m.lastKeyWasCtrlC = key == "ctrl+c"

	if m.state == stateAwaitingApproval {
		return m.handleApprovalKey(key)
	}

	switch key {
	case "ctrl+c":
		if m.turnCancel != nil {
			m.turnCancel()
			m.turnCancel = nil
			return m, nil
		}
		if wasCtrlC {
			return m, tea.Quit
		}
		return m, nil

	case "ctrl+o":
		m.toggleExpand()
		return m, nil

	case "enter":
		if m.state != stateIdle {
			return m, nil
		}
		return m.startTurnOrDispatch()
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleApprovalKey routes a/A/d while a tool call is awaiting a decision.
// Any other key is ignored -- the input textarea is not editable in this
// state.
func (m Model) handleApprovalKey(key string) (tea.Model, tea.Cmd) {
	if m.pendingApproval == nil {
		m.state = stateIdle
		return m, nil
	}

	var decision tool.Decision
	switch key {
	case "a":
		decision = tool.DecisionAllowOnce
	case "A":
		decision = tool.DecisionAllowSession
	case "d":
		decision = tool.DecisionDeny
	default:
		return m, nil
	}

	// response is buffered (size 1, see Approver.RequestApproval), so this
	// send never blocks Update.
	m.pendingApproval.response <- approvalResponse{decision: decision}
	m.pendingApproval = nil
	m.state = stateStreaming
	return m, nil
}

// startTurnOrDispatch handles Enter while idle: a "/command" line goes
// through the slash registry (shared with --interactive); anything else
// starts a new turn.
func (m Model) startTurnOrDispatch() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	m.input.SetValue("")

	if result, handled := m.slashRegistry.Dispatch(context.Background(), m.slashCtx, text); handled {
		if result.ClearTranscript {
			m.transcript = nil
			m.expandedToolCallID = ""
		}
		m.transcript = append(m.transcript, newSystemItem(result.Output))
		m.flush()
		if result.Quit {
			return m, tea.Quit
		}
		return m, nil
	}

	m.transcript = append(m.transcript, newUserItem(text))
	m.flush()

	ctx, cancel := context.WithCancel(context.Background())
	m.turnCancel = cancel
	m.events = m.loop.Run(ctx, text)
	m.state = stateStreaming

	return m, tea.Batch(waitForEvent(m.events), tick())
}

// handleAgentEvent applies one agent.Event to the transcript. Cheap
// (string/slice bookkeeping only); the actual viewport re-render happens
// separately, on tick.
func (m *Model) handleAgentEvent(ev agent.Event) {
	switch ev.Kind {
	case agent.KindTextDelta:
		m.ensureAssistantItem()
		item := m.currentAssistant()
		item.raw.WriteString(ev.TextDelta)
		m.dirty = true

	case agent.KindThinkingDelta:
		// v0.1: not surfaced, same open question as the headless renderer.

	case agent.KindToolCallStart:
		m.closeCurrentAssistant()
		m.transcript = append(m.transcript, newToolCallItem(ev.ToolCallStart.ID, ev.ToolCallStart.Name))
		m.expandedToolCallID = "" // a new call becomes "most recent"; start collapsed
		m.dirty = true

	case agent.KindToolCallDelta:
		// raw partial JSON, not for display

	case agent.KindToolCallEnd:
		if tc := m.findToolCall(ev.ToolCallEnd.ID); tc != nil {
			tc.input = string(ev.ToolCallEnd.Input)
		}
		m.dirty = true

	case agent.KindToolOutput:
		if tc := m.findToolCall(ev.ToolOutput.ID); tc != nil {
			tc.output.WriteString(ev.ToolOutput.Chunk)
		}
		m.dirty = true

	case agent.KindToolCallResult:
		if tc := m.findToolCall(ev.ToolCallResult.ID); tc != nil {
			tc.done = true
			tc.result = ev.ToolCallResult.Output
			tc.isError = ev.ToolCallResult.IsError
		}
		m.dirty = true

	case agent.KindUsage:
		// status bar reads loop.TotalUsage() directly; nothing to store.

	case agent.KindStopReason:
		m.closeCurrentAssistant()

	case agent.KindError:
		m.closeCurrentAssistant()
		m.transcript = append(m.transcript, newSystemItem("error: "+ev.Err.Error()))
		m.dirty = true
	}
}

func (m *Model) ensureAssistantItem() {
	if n := len(m.transcript); n > 0 && m.transcript[n-1].kind == itemAssistant && !m.transcript[n-1].closed {
		return
	}
	m.transcript = append(m.transcript, newAssistantItem())
}

func (m *Model) currentAssistant() *transcriptItem {
	return &m.transcript[len(m.transcript)-1]
}

func (m *Model) closeCurrentAssistant() {
	if n := len(m.transcript); n > 0 && m.transcript[n-1].kind == itemAssistant && !m.transcript[n-1].closed {
		closeAssistant(&m.transcript[n-1], m.glamourRenderer)
	}
}

func (m *Model) findToolCall(id string) *toolCallItem {
	for i := range m.transcript {
		if m.transcript[i].kind == itemToolCall && m.transcript[i].tool.id == id {
			return m.transcript[i].tool
		}
	}
	return nil
}

func (m *Model) lastToolCallID() string {
	for i := len(m.transcript) - 1; i >= 0; i-- {
		if m.transcript[i].kind == itemToolCall {
			return m.transcript[i].tool.id
		}
	}
	return ""
}

func (m *Model) toggleExpand() {
	id := m.lastToolCallID()
	if id == "" {
		return
	}
	if m.expandedToolCallID == id {
		m.expandedToolCallID = ""
	} else {
		m.expandedToolCallID = id
	}
	m.flush()
}

// flush re-renders the viewport from the current transcript. The one
// place transcript state actually becomes pixels -- called from tick
// (only when dirty), turn completion (unconditionally, so the last
// partial buffer is never lost), and anywhere the transcript changes
// outside the streaming path (dispatch, resize, expand toggle).
func (m *Model) flush() {
	m.viewport.SetContent(renderTranscript(m.transcript, m.expandedToolCallID))
	m.viewport.GotoBottom()
	m.dirty = false
}

func (m Model) View() string {
	header := styleHeader.Render("hewn — minimalist agent harness")
	status := renderStatusBar(m.width, m.loop.Model, m.cwd, m.loop.TotalUsage(), m.state)
	help := styleHelp.Render("PgUp/PgDn or mouse wheel scroll • Enter send • /command • ctrl+o expand • ctrl+c interrupt/quit")

	sections := []string{header, m.viewport.View()}

	if m.state == stateAwaitingApproval && m.pendingApproval != nil {
		sections = append(sections, styleApprovalPrompt.Render(fmt.Sprintf(
			"approve %s %s ?  [a]llow once  [A]llow session  [d]eny",
			m.pendingApproval.req.Tool, string(m.pendingApproval.req.Params),
		)))
	}

	sections = append(sections, m.input.View())

	if suggestions := renderSuggestions(slashSuggestions(m.slashRegistry, m.input.Value())); suggestions != "" {
		sections = append(sections, suggestions)
	}

	sections = append(sections, help, status)

	return lipgloss.JoinVertical(lipgloss.Top, sections...)
}
