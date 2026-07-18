package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/unhewn/hewn/internal/provider"
	"github.com/unhewn/hewn/internal/tool"
)

// Loop drives one conversation: user message -> stream assistant -> parse
// tool calls -> execute -> feed results -> repeat until a stop reason other
// than tool use. It owns its conversation history; Run must not be called
// concurrently with itself (the history is only ever touched from within
// a single in-flight Run's goroutine).
type Loop struct {
	Provider  provider.Provider
	Tools     *tool.Registry
	Approval  *tool.Policy
	Model     string
	System    string
	MaxTokens int

	history []provider.Message
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

	l.history = append(l.history, provider.Message{
		Role:    provider.RoleUser,
		Content: []provider.ContentBlock{{Kind: provider.ContentText, Text: userMsg}},
	})

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
	stream, err := l.Provider.Stream(ctx, provider.Request{
		Model:     l.Model,
		System:    l.System,
		Messages:  l.history,
		Tools:     l.toolDefs(),
		MaxTokens: l.MaxTokens,
	})
	if err != nil {
		return provider.StopReasonUnknown, fmt.Errorf("agent: start stream: %w", err)
	}
	defer stream.Close()

	var (
		text  strings.Builder
		calls []toolCall
		stop  provider.StopReason
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

	l.history = append(l.history, provider.Message{
		Role:    provider.RoleAssistant,
		Content: assistantContent(text.String(), calls),
	})

	if stop != provider.StopReasonToolUse || len(calls) == 0 {
		return stop, nil
	}

	l.history = append(l.history, provider.Message{
		Role:    provider.RoleUser,
		Content: l.executeToolCalls(ctx, calls, events),
	})
	return stop, nil
}

// executeToolCalls runs each call in order (matching the order the model
// requested them), gating every call through the approval policy first,
// and emits a ToolCallResult event for each.
func (l *Loop) executeToolCalls(ctx context.Context, calls []toolCall, events chan<- Event) []provider.ContentBlock {
	results := make([]provider.ContentBlock, 0, len(calls))
	for _, c := range calls {
		result := l.executeOne(ctx, c, events)
		events <- NewToolCallResult(c.id, result.Output, result.IsError)
		results = append(results, provider.ContentBlock{
			Kind:              provider.ContentToolResult,
			ToolResultID:      c.id,
			ToolResultContent: result.Output,
			ToolResultIsError: result.IsError,
		})
	}
	return results
}

func (l *Loop) executeOne(ctx context.Context, c toolCall, events chan<- Event) tool.Result {
	t, ok := l.Tools.Get(c.name)
	if !ok {
		return tool.Result{Output: fmt.Sprintf("unknown tool %q", c.name), IsError: true}
	}

	if err := l.Approval.Check(ctx, c.name, t.Risk(), c.input); err != nil {
		return tool.Result{Output: err.Error(), IsError: true}
	}

	input := c.input
	if input == nil {
		input = json.RawMessage("{}")
	}

	result, err := t.Execute(ctx, input, toolIOAdapter{id: c.id, events: events})
	if err != nil {
		return tool.Result{Output: err.Error(), IsError: true}
	}
	return result
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
