package provider

import "encoding/json"

// EventKind identifies which fields of an Event are populated.
type EventKind int

// EventKind values.
const (
	KindTextDelta EventKind = iota
	KindThinkingDelta
	KindToolCallStart
	KindToolCallDelta
	KindToolCallEnd
	KindUsage
	KindStopReason
)

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

// ToolCallStart marks the beginning of a tool call block in the stream.
type ToolCallStart struct {
	ID   string
	Name string
}

// ToolCallDelta carries a raw partial-JSON fragment of a tool call's input.
// Fragments must be buffered and concatenated; do not attempt to parse one
// on its own.
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

// Usage carries token accounting for a turn, normalized from whatever
// cached-token fields the underlying provider uses.
type Usage struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
}

// Event is a discriminated union of everything a Stream can yield: assistant
// text and thinking deltas, tool-call lifecycle, token usage, and the stop
// reason. Exactly the fields matching Kind are meaningful.
type Event struct {
	Kind EventKind

	TextDelta     string
	ThinkingDelta string
	ToolCallStart ToolCallStart
	ToolCallDelta ToolCallDelta
	ToolCallEnd   ToolCallEnd
	Usage         Usage
	StopReason    StopReason
}
