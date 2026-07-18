// Package agent contains the loop: turn orchestration, tool dispatch,
// cancellation, and event emission.
package agent

import "encoding/json"

// EventKind identifies which fields of an Event are populated.
type EventKind int

// EventKind values. This set is the extension ABI in waiting (AGENTS.md
// invariant #2): add a variant freely, never repurpose or remove one.
const (
	KindTextDelta EventKind = iota
	KindThinkingDelta
	KindToolCallStart
	KindToolCallDelta
	KindToolCallEnd
	KindToolOutput
	KindToolCallResult
	KindUsage
	KindStopReason
	KindError
)

// String returns the constant's name, for logging and the headless renderer.
func (k EventKind) String() string {
	switch k {
	case KindTextDelta:
		return "text_delta"
	case KindThinkingDelta:
		return "thinking_delta"
	case KindToolCallStart:
		return "tool_call_start"
	case KindToolCallDelta:
		return "tool_call_delta"
	case KindToolCallEnd:
		return "tool_call_end"
	case KindToolOutput:
		return "tool_output"
	case KindToolCallResult:
		return "tool_call_result"
	case KindUsage:
		return "usage"
	case KindStopReason:
		return "stop_reason"
	case KindError:
		return "error"
	default:
		return "unknown"
	}
}

// StopReason is why the provider stopped generating.
type StopReason int

// StopReason values.
const (
	StopReasonUnknown StopReason = iota
	StopReasonEndTurn
	StopReasonToolUse
	StopReasonMaxTokens
	StopReasonStopSequence
)

// String returns the constant's name, for logging and the headless renderer.
func (r StopReason) String() string {
	switch r {
	case StopReasonUnknown:
		return "unknown"
	case StopReasonEndTurn:
		return "end_turn"
	case StopReasonToolUse:
		return "tool_use"
	case StopReasonMaxTokens:
		return "max_tokens"
	case StopReasonStopSequence:
		return "stop_sequence"
	default:
		return "unknown"
	}
}

// ToolCallStart marks the beginning of a tool call block in the stream.
type ToolCallStart struct {
	ID   string
	Name string
}

// ToolCallDelta carries a raw partial-JSON fragment of a tool call's input.
// Fragments must be buffered and concatenated; do not attempt to parse a
// ToolCallDelta on its own (AGENTS.md "known sharp edges").
type ToolCallDelta struct {
	ID         string
	InputDelta string
}

// ToolCallEnd marks a tool call block as closed, with its fully assembled
// input.
type ToolCallEnd struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolOutput carries one chunk of a running tool's partial output (e.g. a
// bash tool's live stdout/stderr), distinct from the tool's final
// ToolCallResult.
type ToolOutput struct {
	ID    string
	Chunk string
}

// ToolCallResult carries the outcome of executing a tool call back into the
// stream of events.
type ToolCallResult struct {
	ID      string
	Output  string
	IsError bool
}

// Usage carries token accounting for a turn. Providers normalize their own
// cached-token fields into these before emitting (AGENTS.md "known sharp
// edges").
type Usage struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
}

// Event is a discriminated union of everything the agent loop can emit onto
// its event channel: assistant text and thinking deltas, tool-call
// lifecycle, token usage, the stop reason, and errors. Exactly the fields
// matching Kind are meaningful; the rest are the zero value. Construct
// values with the New* helpers below rather than setting Kind by hand.
type Event struct {
	Kind EventKind

	TextDelta      string
	ThinkingDelta  string
	ToolCallStart  ToolCallStart
	ToolCallDelta  ToolCallDelta
	ToolCallEnd    ToolCallEnd
	ToolOutput     ToolOutput
	ToolCallResult ToolCallResult
	Usage          Usage
	StopReason     StopReason
	Err            error
}

// NewTextDelta builds a KindTextDelta event.
func NewTextDelta(text string) Event {
	return Event{Kind: KindTextDelta, TextDelta: text}
}

// NewThinkingDelta builds a KindThinkingDelta event.
func NewThinkingDelta(text string) Event {
	return Event{Kind: KindThinkingDelta, ThinkingDelta: text}
}

// NewToolCallStart builds a KindToolCallStart event.
func NewToolCallStart(id, name string) Event {
	return Event{Kind: KindToolCallStart, ToolCallStart: ToolCallStart{ID: id, Name: name}}
}

// NewToolCallDelta builds a KindToolCallDelta event.
func NewToolCallDelta(id, inputDelta string) Event {
	return Event{Kind: KindToolCallDelta, ToolCallDelta: ToolCallDelta{ID: id, InputDelta: inputDelta}}
}

// NewToolCallEnd builds a KindToolCallEnd event.
func NewToolCallEnd(id, name string, input json.RawMessage) Event {
	return Event{Kind: KindToolCallEnd, ToolCallEnd: ToolCallEnd{ID: id, Name: name, Input: input}}
}

// NewToolOutput builds a KindToolOutput event.
func NewToolOutput(id, chunk string) Event {
	return Event{Kind: KindToolOutput, ToolOutput: ToolOutput{ID: id, Chunk: chunk}}
}

// NewToolCallResult builds a KindToolCallResult event.
func NewToolCallResult(id, output string, isError bool) Event {
	return Event{Kind: KindToolCallResult, ToolCallResult: ToolCallResult{ID: id, Output: output, IsError: isError}}
}

// NewUsage builds a KindUsage event.
func NewUsage(u Usage) Event {
	return Event{Kind: KindUsage, Usage: u}
}

// NewStopReason builds a KindStopReason event.
func NewStopReason(r StopReason) Event {
	return Event{Kind: KindStopReason, StopReason: r}
}

// NewError builds a KindError event.
func NewError(err error) Event {
	return Event{Kind: KindError, Err: err}
}
