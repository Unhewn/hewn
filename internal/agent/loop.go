package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/unhewn/hewn/internal/pricing"
	"github.com/unhewn/hewn/internal/provider"
	"github.com/unhewn/hewn/internal/session"
	"github.com/unhewn/hewn/internal/tool"
)

// Loop drives one conversation: user message -> stream assistant -> parse
// tool calls -> execute -> feed results -> repeat until a stop reason other
// than tool use. It owns its conversation history; Run must not be called
// concurrently with itself (the history is only ever touched from within
// a single in-flight Run's goroutine).
//
// Session and SessionID are optional: when Session is nil, persistence is
// a no-op, which is what every existing test (and any caller that just
// wants an in-memory loop) relies on.
type Loop struct {
	Provider  provider.Provider
	Tools     *tool.Registry
	Approval  *tool.Policy
	Model     string
	System    string
	MaxTokens int

	// ContextWindow is the model's context size in tokens, if known -- set
	// from config (the user's own knowledge of their model/provider limit;
	// Hewn has no way to discover it, especially for a local model whose
	// context size is whatever the user configured in Ollama). Zero means
	// unknown; callers show the raw token count instead of a percentage.
	ContextWindow int

	Session   *session.Store
	SessionID string

	history    []provider.Message
	totalUsage Usage
	lastUsage  Usage

	turnCount int     // completed turns with usage reported, for EstimateNextTurn's average
	totalCost float64 // running $ total, priced at each turn's active model
	costKnown bool    // true once any turn priced successfully; false means Model was never a priced model
}

// TotalCost returns the cumulative dollar cost of every turn this loop has
// run so far, priced at each turn's active model and rates in effect at
// the time (a later /model switch does not retroactively reprice earlier
// turns). ok is false if no turn so far ran against a model pricing knows
// -- e.g. a session run entirely against a local Ollama model, which has
// no meaningful $ cost.
func (l *Loop) TotalCost() (float64, bool) {
	return l.totalCost, l.costKnown
}

// TotalUsage returns the cumulative token usage across every turn this
// loop has run so far.
func (l *Loop) TotalUsage() Usage {
	return l.totalUsage
}

// LastUsage returns the most recent turn's usage. InputTokens here is the
// best available proxy for "how much is in context right now" -- unlike
// TotalUsage, which sums every turn in the session and isn't representative
// of current context size at all.
func (l *Loop) LastUsage() Usage {
	return l.lastUsage
}

// toolCall accumulates one tool-use content block across the
// ToolCallStart/Delta/End events of a single stream.
type toolCall struct {
	id    string
	name  string
	input json.RawMessage
}

// Run drives one user turn to completion, streaming every Event onto the
// returned channel. The channel is closed when the turn ends: normally,
// on error (as a preceding KindError event), or silently on cancellation
// (context.Canceled is the user pressing Ctrl+C, not a failure --
// AGENTS.md).
func (l *Loop) Run(ctx context.Context, userMsg string) <-chan Event {
	events := make(chan Event)
	go l.run(ctx, userMsg, events)
	return events
}

func (l *Loop) run(ctx context.Context, userMsg string, events chan<- Event) {
	defer close(events)

	userBlocks := []provider.ContentBlock{{Kind: provider.ContentText, Text: userMsg}}
	l.history = append(l.history, provider.Message{Role: provider.RoleUser, Content: userBlocks})
	l.persistMessage(ctx, provider.RoleUser, userBlocks, nil)

	for {
		stop, err := l.step(ctx, events)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			events <- NewError(err)
			return
		}
		if stop != provider.StopReasonToolUse {
			return
		}
	}
}

// step runs exactly one model call to completion (streaming its events),
// appends the assistant's turn to history, dispatches any tool calls it
// made, appends their results to history, and reports the stop reason the
// model gave.
func (l *Loop) step(ctx context.Context, events chan<- Event) (provider.StopReason, error) {
	stream, err := l.Provider.Stream(ctx, l.buildRequest())
	if err != nil {
		return provider.StopReasonUnknown, fmt.Errorf("agent: start stream: %w", err)
	}
	defer stream.Close()

	var (
		text      strings.Builder
		calls     []toolCall
		stop      provider.StopReason
		usage     provider.Usage
		usageSeen bool
	)

	for {
		ev, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return provider.StopReasonUnknown, fmt.Errorf("agent: stream: %w", err)
		}

		switch ev.Kind {
		case provider.KindTextDelta:
			text.WriteString(ev.TextDelta)
			events <- NewTextDelta(ev.TextDelta)
		case provider.KindThinkingDelta:
			events <- NewThinkingDelta(ev.ThinkingDelta)
		case provider.KindToolCallStart:
			calls = append(calls, toolCall{id: ev.ToolCallStart.ID, name: ev.ToolCallStart.Name})
			events <- NewToolCallStart(ev.ToolCallStart.ID, ev.ToolCallStart.Name)
		case provider.KindToolCallDelta:
			events <- NewToolCallDelta(ev.ToolCallDelta.ID, ev.ToolCallDelta.InputDelta)
		case provider.KindToolCallEnd:
			for i := range calls {
				if calls[i].id == ev.ToolCallEnd.ID {
					calls[i].input = ev.ToolCallEnd.Input
				}
			}
			events <- NewToolCallEnd(ev.ToolCallEnd.ID, ev.ToolCallEnd.Name, ev.ToolCallEnd.Input)
		case provider.KindUsage:
			usage = ev.Usage
			usageSeen = true
			l.totalUsage.InputTokens += ev.Usage.InputTokens
			l.totalUsage.OutputTokens += ev.Usage.OutputTokens
			l.totalUsage.CacheReadTokens += ev.Usage.CacheReadTokens
			l.totalUsage.CacheWriteTokens += ev.Usage.CacheWriteTokens
			l.lastUsage = Usage{
				InputTokens:      ev.Usage.InputTokens,
				OutputTokens:     ev.Usage.OutputTokens,
				CacheReadTokens:  ev.Usage.CacheReadTokens,
				CacheWriteTokens: ev.Usage.CacheWriteTokens,
			}
			l.turnCount++
			if rates, ok := pricing.Lookup(l.Model); ok {
				l.totalCost += rates.Cost(ev.Usage.InputTokens, ev.Usage.OutputTokens, ev.Usage.CacheReadTokens, ev.Usage.CacheWriteTokens)
				l.costKnown = true
			}
			events <- NewUsage(Usage{
				InputTokens:      ev.Usage.InputTokens,
				OutputTokens:     ev.Usage.OutputTokens,
				CacheReadTokens:  ev.Usage.CacheReadTokens,
				CacheWriteTokens: ev.Usage.CacheWriteTokens,
			})
		case provider.KindStopReason:
			stop = ev.StopReason
			events <- NewStopReason(toAgentStopReason(stop))
		}
	}

	assistantBlocks := assistantContent(text.String(), calls)
	l.history = append(l.history, provider.Message{Role: provider.RoleAssistant, Content: assistantBlocks})

	var sessUsage *session.Usage
	if usageSeen {
		sessUsage = &session.Usage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens}
	}
	assistantMsg := l.persistMessage(ctx, provider.RoleAssistant, assistantBlocks, sessUsage)

	if stop != provider.StopReasonToolUse || len(calls) == 0 {
		return stop, nil
	}

	results := l.executeToolCalls(ctx, calls, events, assistantMsg.ID)
	l.history = append(l.history, provider.Message{Role: provider.RoleUser, Content: results})
	l.persistMessage(ctx, provider.RoleUser, results, nil)

	return stop, nil
}

// executeToolCalls runs each call in order (matching the order the model
// requested them), gating every call through the approval policy first,
// emits a ToolCallResult event for each, and persists each one against
// messageID (the assistant message whose tool_use block requested it).
func (l *Loop) executeToolCalls(ctx context.Context, calls []toolCall, events chan<- Event, messageID string) []provider.ContentBlock {
	results := make([]provider.ContentBlock, 0, len(calls))
	for _, c := range calls {
		result, decision, duration := l.executeOne(ctx, c, events)
		events <- NewToolCallResult(c.id, result.Output, result.IsError)
		l.persistToolCall(ctx, messageID, c.name, c.input, result, decision, duration)
		results = append(results, provider.ContentBlock{
			Kind:              provider.ContentToolResult,
			ToolResultID:      c.id,
			ToolResultContent: result.Output,
			ToolResultIsError: result.IsError,
		})
	}
	return results
}

// executeOne runs a single call, returning its result, the approval
// decision that let it proceed (or DecisionNotGated if it never reached
// approval at all, e.g. an unknown tool name), and how long Execute itself
// took (zero if Execute never ran).
func (l *Loop) executeOne(ctx context.Context, c toolCall, events chan<- Event) (tool.Result, tool.Decision, time.Duration) {
	t, ok := l.Tools.Get(c.name)
	if !ok {
		return tool.Result{Output: fmt.Sprintf("unknown tool %q", c.name), IsError: true}, tool.DecisionNotGated, 0
	}

	decision, err := l.Approval.Check(ctx, c.name, t.Risk(), c.input)
	if err != nil {
		return tool.Result{Output: err.Error(), IsError: true}, decision, 0
	}

	input := c.input
	if input == nil {
		input = json.RawMessage("{}")
	}

	start := time.Now()
	result, err := t.Execute(ctx, input, toolIOAdapter{id: c.id, events: events})
	duration := time.Since(start)
	if err != nil {
		return tool.Result{Output: err.Error(), IsError: true}, decision, duration
	}
	return result, decision, duration
}

// buildRequest assembles one model call's Request, marking cacheable
// breakpoints: the system prompt (rarely changes turn to turn) and the
// last content block of the last history message (rolls the breakpoint
// forward -- everything before it was already cached on the prior turn).
// Two breakpoints total, safely under Anthropic's 4-per-request cap;
// providers without cache_control support just ignore the marker.
func (l *Loop) buildRequest() provider.Request {
	return l.buildRequestWithHistory(l.history)
}

// buildRequestWithHistory is buildRequest parameterized on the message
// history to send, so EstimateNextTurn can price a hypothetical next turn
// against the exact request shape buildRequest would produce, without
// mutating l.history to do it.
func (l *Loop) buildRequestWithHistory(history []provider.Message) provider.Request {
	req := provider.Request{
		Model:     l.Model,
		Messages:  withLastBlockCacheable(history),
		Tools:     l.toolDefs(),
		MaxTokens: l.MaxTokens,
	}
	if l.System != "" {
		req.System = []provider.ContentBlock{{Kind: provider.ContentText, Text: l.System, Cacheable: true}}
	}
	return req
}

// Estimate is a pre-flight cost projection for a turn, computed before it
// is sent. InputTokens is exact for a provider with a real counting
// endpoint (Anthropic) and an approximation otherwise (provider.Provider's
// CountTokens contract). Both costs price InputTokens at full input rate,
// deliberately never crediting a prompt-cache discount: Hewn cannot know
// ahead of time whether the request will actually hit cache, and the
// useful number before committing to send is the conservative one --
// what you'd actually pay in the worst case, not an optimistic guess.
type Estimate struct {
	InputTokens         int
	TypicalOutputTokens int // 0 until this session has a completed turn to average
	MaxOutputTokens     int // 0 if MaxTokens is unset
	TypicalCost         float64
	MaxCost             float64
	PricingKnown        bool // false for a local/unpriced model -- both costs are 0 in that case
}

// EstimateNextTurn projects the cost of sending userMsg as the next turn,
// without mutating history or sending anything. InputTokens comes from the
// provider's CountTokens on the exact request buildRequest would build for
// this message; output is bounded between this session's average
// completed-turn size (Typical) and MaxTokens (Max).
func (l *Loop) EstimateNextTurn(ctx context.Context, userMsg string) (Estimate, error) {
	hypothetical := append(append([]provider.Message(nil), l.history...), provider.Message{
		Role:    provider.RoleUser,
		Content: []provider.ContentBlock{{Kind: provider.ContentText, Text: userMsg}},
	})

	inputTokens, err := l.Provider.CountTokens(ctx, l.buildRequestWithHistory(hypothetical))
	if err != nil {
		return Estimate{}, fmt.Errorf("agent: estimate: %w", err)
	}

	rates, known := pricing.Lookup(l.Model)
	est := Estimate{InputTokens: inputTokens, PricingKnown: known}
	if !known {
		return est, nil
	}

	if l.turnCount > 0 {
		est.TypicalOutputTokens = l.totalUsage.OutputTokens / l.turnCount
	}
	est.MaxOutputTokens = l.MaxTokens

	est.TypicalCost = rates.Cost(inputTokens, est.TypicalOutputTokens, 0, 0)
	est.MaxCost = rates.Cost(inputTokens, est.MaxOutputTokens, 0, 0)
	return est, nil
}

// withLastBlockCacheable returns a copy of messages with the last content
// block of the last message marked Cacheable. It copies rather than
// mutating in place: messages is l.history, shared across every future
// turn, and must stay free of stale breakpoints from earlier turns.
func withLastBlockCacheable(messages []provider.Message) []provider.Message {
	if len(messages) == 0 {
		return messages
	}

	out := make([]provider.Message, len(messages))
	copy(out, messages)

	last := out[len(out)-1]
	if len(last.Content) == 0 {
		return out
	}
	content := make([]provider.ContentBlock, len(last.Content))
	copy(content, last.Content)
	content[len(content)-1].Cacheable = true
	last.Content = content
	out[len(out)-1] = last

	return out
}

func (l *Loop) toolDefs() []provider.ToolDef {
	tools := l.Tools.List()
	defs := make([]provider.ToolDef, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, provider.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.Schema(),
		})
	}
	return defs
}

// persistMessage appends one message to the session store and bumps its
// updated_at, if persistence is configured. It returns the created
// session.Message (the zero value when persistence is off, or when the
// write itself fails), so callers can attach tool_calls to the right
// message ID. Persistence failures degrade silently rather than aborting
// the turn: the conversation itself succeeded regardless of whether its
// bookkeeping write did.
func (l *Loop) persistMessage(ctx context.Context, role provider.Role, content []provider.ContentBlock, usage *session.Usage) session.Message {
	if l.Session == nil {
		return session.Message{}
	}

	raw, err := json.Marshal(content)
	if err != nil {
		return session.Message{}
	}

	msg, err := l.Session.AppendMessage(ctx, l.SessionID, sessionRole(role), raw, usage)
	if err != nil {
		return session.Message{}
	}
	_ = l.Session.Touch(ctx, l.SessionID)
	return msg
}

// persistToolCall records one tool invocation against messageID, if
// persistence is configured. Best-effort, like persistMessage.
func (l *Loop) persistToolCall(
	ctx context.Context,
	messageID, toolName string,
	params json.RawMessage,
	result tool.Result,
	decision tool.Decision,
	duration time.Duration,
) {
	if l.Session == nil || messageID == "" {
		return
	}

	var approved *int
	if decision != tool.DecisionNotGated {
		v := int(decision)
		approved = &v
	}

	_, _ = l.Session.AppendToolCall(ctx, messageID, toolName, params, result.Output, result.IsError, approved, duration)
}

func sessionRole(r provider.Role) session.Role {
	switch r {
	case provider.RoleUser:
		return session.RoleUser
	case provider.RoleAssistant:
		return session.RoleAssistant
	default:
		return session.RoleUser
	}
}

func providerRole(r session.Role) provider.Role {
	switch r {
	case session.RoleAssistant:
		return provider.RoleAssistant
	case session.RoleUser, session.RoleTool, session.RoleSystem:
		return provider.RoleUser
	default:
		return provider.RoleUser
	}
}

// SeedHistory replaces the loop's conversation history. For resuming a
// persisted session; must be called before the first Run.
func (l *Loop) SeedHistory(history []provider.Message) {
	l.history = history
}

// defaultCompactKeepRecent is how many of the most recent messages Compact
// leaves untouched, verbatim -- recent turns are exactly the ones still
// worth full detail; older ones are what's safe to compress.
const defaultCompactKeepRecent = 6

// CompactResult reports what Compact did, for /compact to tell the user.
type CompactResult struct {
	MessagesBefore int
	MessagesAfter  int
	TokensBefore   int // input tokens the summarization call itself used, reading the discarded history
}

// Compact summarizes everything in history except the last keepRecent
// messages (defaultCompactKeepRecent if keepRecent <= 0) into one synthetic
// message, then replaces history with [summary, ...recent]. This only
// rewrites the in-memory history the next request sends -- it never
// deletes or rewrites persisted session rows, so /export and session
// resume still see the full original conversation.
func (l *Loop) Compact(ctx context.Context, keepRecent int) (CompactResult, error) {
	if keepRecent <= 0 {
		keepRecent = defaultCompactKeepRecent
	}
	before := len(l.history)
	if before <= keepRecent {
		return CompactResult{MessagesBefore: before, MessagesAfter: before}, nil
	}

	split := before - keepRecent
	older, recent := l.history[:split], l.history[split:]

	summary, usage, err := l.summarize(ctx, older)
	if err != nil {
		return CompactResult{}, fmt.Errorf("agent: compact: %w", err)
	}
	l.totalUsage.InputTokens += usage.InputTokens
	l.totalUsage.OutputTokens += usage.OutputTokens
	l.totalUsage.CacheReadTokens += usage.CacheReadTokens
	l.totalUsage.CacheWriteTokens += usage.CacheWriteTokens

	newHistory := make([]provider.Message, 0, 1+len(recent))
	newHistory = append(newHistory, provider.Message{
		Role: provider.RoleUser,
		Content: []provider.ContentBlock{{
			Kind: provider.ContentText,
			Text: "[Compacted summary of earlier conversation]\n" + summary,
		}},
	})
	newHistory = append(newHistory, recent...)

	l.SeedHistory(newHistory)

	return CompactResult{
		MessagesBefore: before,
		MessagesAfter:  len(newHistory),
		TokensBefore:   usage.InputTokens,
	}, nil
}

// summarize asks the model to summarize messages in one tool-free,
// history-independent call -- it never touches l.history itself, so a
// failed or unwanted summarization leaves the real conversation untouched.
// Used only by Compact.
func (l *Loop) summarize(ctx context.Context, messages []provider.Message) (string, provider.Usage, error) {
	msgs := make([]provider.Message, 0, len(messages)+1)
	msgs = append(msgs, messages...)
	msgs = append(msgs, provider.Message{
		Role: provider.RoleUser,
		Content: []provider.ContentBlock{{
			Kind: provider.ContentText,
			Text: "Summarize the conversation above concisely: preserve concrete facts, " +
				"decisions, file paths, and any state a continuation would need. This " +
				"summary replaces the full history above, so it needs to stand alone.",
		}},
	})

	stream, err := l.Provider.Stream(ctx, provider.Request{
		Model:     l.Model,
		Messages:  msgs,
		MaxTokens: l.MaxTokens,
	})
	if err != nil {
		return "", provider.Usage{}, fmt.Errorf("start summarization stream: %w", err)
	}
	defer stream.Close()

	var text strings.Builder
	var usage provider.Usage
	for {
		ev, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", provider.Usage{}, fmt.Errorf("summarization stream: %w", err)
		}
		switch ev.Kind {
		case provider.KindTextDelta:
			text.WriteString(ev.TextDelta)
		case provider.KindUsage:
			usage = ev.Usage
		case provider.KindThinkingDelta, provider.KindToolCallStart, provider.KindToolCallDelta,
			provider.KindToolCallEnd, provider.KindStopReason:
			// No tools were offered (buildRequest is never used here), so
			// these should never actually arrive; nothing to do with them
			// if a provider sends one anyway.
		}
	}

	return text.String(), usage, nil
}

// HistoryFromMessages reconstructs provider.Message history from persisted
// session.Message rows, reversing the JSON encoding persistMessage uses.
// Pairs with SeedHistory to resume a session.
func HistoryFromMessages(messages []session.Message) ([]provider.Message, error) {
	history := make([]provider.Message, 0, len(messages))
	for _, m := range messages {
		var blocks []provider.ContentBlock
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			return nil, fmt.Errorf("agent: decode stored message %s: %w", m.ID, err)
		}
		history = append(history, provider.Message{Role: providerRole(m.Role), Content: blocks})
	}
	return history, nil
}

// assistantContent builds the assistant message content blocks for one
// step: an optional text block, followed by one tool-use block per call.
func assistantContent(text string, calls []toolCall) []provider.ContentBlock {
	var blocks []provider.ContentBlock
	if text != "" {
		blocks = append(blocks, provider.ContentBlock{Kind: provider.ContentText, Text: text})
	}
	for _, c := range calls {
		input := c.input
		if input == nil {
			input = json.RawMessage("{}")
		}
		blocks = append(blocks, provider.ContentBlock{
			Kind:      provider.ContentToolUse,
			ToolUseID: c.id,
			ToolName:  c.name,
			ToolInput: input,
		})
	}
	return blocks
}

func toAgentStopReason(r provider.StopReason) StopReason {
	switch r {
	case provider.StopReasonUnknown:
		return StopReasonUnknown
	case provider.StopReasonEndTurn:
		return StopReasonEndTurn
	case provider.StopReasonToolUse:
		return StopReasonToolUse
	case provider.StopReasonMaxTokens:
		return StopReasonMaxTokens
	case provider.StopReasonStopSequence:
		return StopReasonStopSequence
	default:
		return StopReasonUnknown
	}
}

// toolIOAdapter turns a Tool's partial-output callback into ToolOutput
// events on the loop's event channel.
type toolIOAdapter struct {
	id     string
	events chan<- Event
}

// Output sends a ToolOutput event. A tool's subprocess can outlive the
// call that spawned it -- e.g. a command that backgrounds a child, holding
// bash's output pipe open past sandbox.Command's WaitDelay -- so a stray
// chunk can arrive after this turn has already finished and the event
// channel has been closed. Drop it rather than let the send panic the
// whole process.
func (a toolIOAdapter) Output(chunk string) {
	defer func() { _ = recover() }()
	a.events <- NewToolOutput(a.id, chunk)
}
