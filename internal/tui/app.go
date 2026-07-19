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

	"github.com/charmbracelet/bubbles/spinner"
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

// reservedLines is how much vertical space the top status bar, input box
// (with its own border), help line, and the outer frame's top/bottom
// border take up, leaving the rest for the viewport.
const reservedLines = 9

// frameHorizontalOverhead is the outer frame's left/right border plus its
// horizontal padding -- subtracted from the terminal width to get the
// content width shared by the status bar, viewport, and input box.
const frameHorizontalOverhead = 4

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
	userName     string

	slashRegistry *slash.Registry
	slashCtx      *slash.Context

	viewport viewport.Model
	input    textarea.Model
	spinner  spinner.Model
	width    int
	height   int

	transcript         []transcriptItem
	dirty              bool
	glamourRenderer    *glamour.TermRenderer
	expandedToolCallID string
	turnProducedOutput bool // true once this turn has emitted any visible text or a tool call

	state      appState
	turnCancel context.CancelFunc
	events     <-chan agent.Event

	pendingApproval *pendingApproval
	lastKeyWasCtrlC bool

	// pendingTurnText holds the user's message between Enter and the
	// pre-flight estimate resolving -- the real Run doesn't start until
	// then. Run and EstimateNextTurn both read/write Loop's history, so
	// they cannot run concurrently against the same Loop; serializing
	// estimate-then-send is what keeps that safe, not just a UX choice.
	pendingTurnText string

	picker *picker
}

// NewModel builds the root Model. loop must already be fully wired (a
// Provider, Tools, and an Approval policy built from approver -- see
// buildLoop in cmd/hewn); Model only ever drives it through Run. slashCtx
// is built by the caller too (it needs a *session.Store, which this
// package must never import -- AGENTS.md invariant #1, "the TUI never
// touches the database").
func NewModel(loop *agent.Loop, approver *Approver, slashCtx *slash.Context, cwd, providerName, userName string) Model {
	vp := viewport.New(80, 20)
	vp.MouseWheelEnabled = true
	vp.SetContent(welcomeText())

	s := spinner.New()
	s.Style = lipgloss.NewStyle().Foreground(colorAccent)
	s.Spinner = spinner.MiniDot

	return Model{
		loop:            loop,
		approver:        approver,
		cwd:             cwd,
		providerName:    providerName,
		userName:        userName,
		slashRegistry:   slashCtx.Registry,
		slashCtx:        slashCtx,
		viewport:        vp,
		input:           newInput(),
		spinner:         s,
		glamourRenderer: newGlamourRenderer(80),
	}
}

// Start runs the TUI to completion. approver must be the same value
// passed as buildLoop's approver argument when loop was constructed.
func Start(loop *agent.Loop, approver *Approver, slashCtx *slash.Context, cwd, providerName, userName string) error {
	m := NewModel(loop, approver, slashCtx, cwd, providerName, userName)
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

// estimateMsg carries the result of a pre-flight cost projection for the
// turn that was just started, computed concurrently with the real request
// rather than serialized in front of it -- the turn's tokens are already
// fixed the moment it's sent, so racing the estimate against the response
// costs nothing but produces the same number a strictly-before-send
// estimate would.
type estimateMsg struct {
	est agent.Estimate
	err error
}

// estimateNextTurnCmd runs in the background, alongside the real turn
// (never in front of it -- see estimateMsg). The 5s timeout keeps a
// misbehaving network from leaving the estimate silently pending forever;
// nothing in this codepath needs cancellation tied to the real turn, since
// it is a single short request, not a stream.
func estimateNextTurnCmd(loop *agent.Loop, userMsg string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		est, err := loop.EstimateNextTurn(ctx, userMsg)
		return estimateMsg{est: est, err: err}
	}
}

// formatEstimate renders a pre-flight Estimate for the transcript. Both
// costs are worst-case-on-input (no cache credit -- see Estimate's doc
// comment); "typical" additionally has no basis to guess output length
// until this session has a completed turn to average, so it's omitted
// rather than shown as a fabricated number.
func formatEstimate(est agent.Estimate) string {
	if est.TypicalOutputTokens == 0 {
		return fmt.Sprintf("~ %d input tokens · up to $%.4f for this turn", est.InputTokens, est.MaxCost)
	}
	return fmt.Sprintf("~$%.4f typical · up to $%.4f worst case for this turn", est.TypicalCost, est.MaxCost)
}

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
	return tea.Batch(textarea.Blink, waitForApproval(m.approver.requests), m.spinner.Tick)
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
		if !m.turnProducedOutput {
			m.transcript = append(m.transcript, newSystemItem(
				"(no reply -- the model likely ran out of its response budget while reasoning; try a shorter question, or /model a less "+
					"reasoning-heavy model)"))
		}
		m.turnProducedOutput = false
		m.flush()
		m.state = stateIdle
		m.events = nil
		m.turnCancel = nil
		return m, nil

	case approvalRequestMsg:
		m.pendingApproval = &pendingApproval{req: msg.req, response: msg.response}
		m.state = stateAwaitingApproval
		return m, waitForApproval(m.approver.requests)

	case estimateMsg:
		if msg.err == nil && msg.est.PricingKnown {
			m.transcript = append(m.transcript, newSystemItem(formatEstimate(msg.est)))
			m.flush()
		}
		text := m.pendingTurnText
		m.pendingTurnText = ""
		ctx, cancel := context.WithCancel(context.Background())
		m.turnCancel = cancel
		m.events = m.loop.Run(ctx, text)
		return m, waitForEvent(m.events)

	case tickMsg:
		if m.dirty {
			m.flush()
		}
		if m.state == stateStreaming {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, tea.Batch(tick(), cmd)
		}
		return m, nil

	case spinner.TickMsg:
		if m.state == stateStreaming {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// contentWidth is the width available inside the outer frame's border and
// padding -- shared by the header, viewport, input box, and status bar so
// everything lines up flush against the frame.
func (m Model) contentWidth() int {
	w := m.width - frameHorizontalOverhead
	if w < 0 {
		return 0
	}
	return w
}

func (m Model) handleResize(msg tea.WindowSizeMsg) Model {
	m.width = msg.Width
	m.height = msg.Height
	cw := m.contentWidth()
	m.viewport.Width = cw
	m.viewport.Height = msg.Height - reservedLines
	m.input.SetWidth(cw - 2) // minus the input box's own left/right border
	m.glamourRenderer = newGlamourRenderer(cw)
	if m.picker != nil {
		m.picker.list.SetWidth(cw - 2) // minus the picker box's own left/right border
	}
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

	if m.state == statePicker {
		return m.handlePickerKey(msg)
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

	case "tab":
		if m.state != stateIdle {
			return m, nil
		}
		m.completeSlashCommand()
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

// handlePickerKey routes keys while a Choices picker is open. Esc/Ctrl+C
// always closes it outright -- even mid-filter, where bubbles/list would
// otherwise treat Esc as "clear the filter, keep browsing" -- since
// reopening the picker is one keystroke away and a single, predictable
// "get me out" key is worth more than that nuance. Enter selects whatever
// is currently highlighted, which is also correct mid-filter: list keeps
// the top match selected as you type. Everything else (arrows, typed
// filter characters) forwards to the list itself.
func (m Model) handlePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.picker == nil {
		m.state = stateIdle
		return m, nil
	}

	switch msg.String() {
	case "esc", "ctrl+c":
		m.picker = nil
		m.state = stateIdle
		return m, nil

	case "enter":
		value, ok := m.picker.selectedValue()
		selectCommand := m.picker.selectCommand
		m.picker = nil
		m.state = stateIdle
		if !ok {
			return m, nil
		}
		return m.dispatchSlash("/" + selectCommand + " " + value)
	}

	var cmd tea.Cmd
	m.picker.list, cmd = m.picker.list.Update(msg)
	return m, cmd
}

// applySlashResult applies a dispatched slash command's Result to the
// model. A Result carrying Choices opens a picker instead of appending
// Output to the transcript -- the eventual selection (via dispatchSlash)
// is what actually lands there, not the list that led to it.
func (m Model) applySlashResult(result slash.Result) (tea.Model, tea.Cmd) {
	if result.ClearTranscript {
		m.transcript = nil
		m.expandedToolCallID = ""
	}

	if len(result.Choices) > 0 {
		p := newPicker(result.Choices, result.SelectCommand, m.contentWidth()-2)
		m.picker = &p
		m.state = statePicker
		return m, nil
	}

	m.transcript = append(m.transcript, newSystemItem(result.Output))
	m.flush()
	if result.Quit {
		return m, tea.Quit
	}
	return m, nil
}

// dispatchSlash runs a fully-formed "/command args" line through the slash
// registry and applies its Result. Used both for user-typed input and to
// re-dispatch "/model <choice>" after a picker selection.
func (m Model) dispatchSlash(line string) (tea.Model, tea.Cmd) {
	result, _ := m.slashRegistry.Dispatch(context.Background(), m.slashCtx, line)
	return m.applySlashResult(result)
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
		return m.applySlashResult(result)
	}

	m.transcript = append(m.transcript, newUserItem(text))
	m.flush()

	// The real turn doesn't start until the estimate resolves (see
	// pendingTurnText's doc comment) -- state flips to streaming here
	// regardless, so the spinner and input-lock cover both phases as one
	// continuous "working on it" from the user's perspective.
	m.pendingTurnText = text
	m.state = stateStreaming
	m.turnProducedOutput = false

	return m, tea.Batch(estimateNextTurnCmd(m.loop, text), tick(), m.spinner.Tick)
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
		m.turnProducedOutput = true
		m.dirty = true

	case agent.KindThinkingDelta:
		// Reasoning streams in, but the TUI no longer renders a live tail
		// of it -- it changed too fast to read. The status bar's spinner +
		// "thinking…" already shows the agent is active; nothing else to
		// do with this event here.

	case agent.KindToolCallStart:
		m.closeCurrentAssistant()
		m.transcript = append(m.transcript, newToolCallItem(ev.ToolCallStart.ID, ev.ToolCallStart.Name))
		m.expandedToolCallID = "" // a new call becomes "most recent"; start collapsed
		m.turnProducedOutput = true
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
	m.viewport.SetContent(renderTranscript(m.transcript, m.expandedToolCallID, m.userName))
	m.viewport.GotoBottom()
	m.dirty = false
}

// welcomeText is shown in the viewport before the first message is sent:
// hewn's identity plus the keybindings a first-time user needs, so nobody
// has to discover them by trial and error or by spotting the dim help
// line at the bottom of the screen.
func welcomeText() string {
	lines := []string{
		styleWelcomeTitle.Render("hewn") + styleSystem.Render("  —  minimalist agent harness"),
		"",
		"  Type a message and press " + styleToolName.Render("Enter") + " to send.",
		"  " + styleToolName.Render("/help") + " lists slash commands.",
		"  " + styleToolName.Render("ctrl+o") + " expands the most recent tool call.",
		"  " + styleToolName.Render("ctrl+c") + " interrupts a turn; twice quits.",
	}
	return strings.Join(lines, "\n")
}

func (m Model) View() string {
	cw := m.contentWidth()

	// Activity indicator: just the spinner while streaming -- state's own
	// String() already supplies the word "thinking" on the right side of
	// the status bar, so a label here duplicated it.
	activity := ""
	if m.state == stateStreaming {
		activity = m.spinner.View() + " "
	}

	// The status bar (model / current context size / session total / cwd /
	// state) is the top bar, per HEWN.md §4's own mockup -- it's the one
	// line that's always present and always informative, so it belongs
	// where it's actually seen, not buried under the help line at the
	// bottom or behind /cost.
	totalUsage := m.loop.TotalUsage()
	totalCost, costKnown := m.loop.TotalCost()
	status := renderStatusBar(cw, m.loop.Model, m.cwd, m.loop.LastUsage().InputTokens, m.loop.ContextWindow,
		totalUsage.InputTokens+totalUsage.OutputTokens, totalCost, costKnown, m.state, activity)
	help := styleHelp.Render("PgUp/PgDn or mouse wheel scroll • Enter send • /command • ctrl+o expand • ctrl+c interrupt/quit")
	if m.state == statePicker {
		help = styleHelp.Render("↑/↓ navigate • type to filter • enter select • esc cancel")
	}

	sections := []string{status, m.viewport.View()}

	if m.state == stateAwaitingApproval && m.pendingApproval != nil {
		sections = append(sections, renderApprovalBox(cw, m.pendingApproval.req))
	}

	if m.state == statePicker && m.picker != nil {
		sections = append(sections, renderPickerBox(m.picker.list.View(), cw))
	} else {
		sections = append(sections, renderInputBox(m.input.View(), cw, m.state))
	}

	if m.state != statePicker {
		if suggestions := renderSuggestions(slashSuggestions(m.slashRegistry, m.input.Value())); suggestions != "" {
			sections = append(sections, suggestions)
		}
	}

	sections = append(sections, help)

	body := lipgloss.JoinVertical(lipgloss.Top, sections...)

	// lipgloss.Style.Width sets the *total* content+padding width (padding
	// is subtracted from it when wrapping, then added back before
	// aligning), so the frame's own Padding(0, 1) needs +2 here on top of
	// cw -- the width every child line (status bar, viewport, input box)
	// already targets -- to land flush with no wrap.
	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(colorAccent).
		Padding(0, 1).
		Width(cw + 2).
		Render(body)
}
