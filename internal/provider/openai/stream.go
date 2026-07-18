package openai

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/unhewn/hewn/internal/provider"
	"github.com/unhewn/hewn/internal/provider/sse"
)

// wireChunk mirrors one OpenAI-compatible streaming chunk. Unlike
// Anthropic's distinctly-typed SSE events, every chunk carries the same
// shape: an incremental delta and (on the last one) a finish_reason.
// There's no explicit block-start/block-stop signal -- a tool call is
// "started" the first time its index carries an id+name, and "ended" when
// either the next index appears or finish_reason arrives.
type wireChunk struct {
	Choices []wireChoice    `json:"choices"`
	Usage   *wireChunkUsage `json:"usage,omitempty"`
}

type wireChoice struct {
	Delta        wireChunkDelta `json:"delta"`
	FinishReason *string        `json:"finish_reason"`
}

type wireChunkDelta struct {
	Content   string              `json:"content,omitempty"`
	ToolCalls []wireChunkToolCall `json:"tool_calls,omitempty"`
}

type wireChunkToolCall struct {
	Index    int                   `json:"index"`
	ID       string                `json:"id,omitempty"`
	Function wireChunkToolCallFunc `json:"function,omitempty"`
}

type wireChunkToolCallFunc struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type wireChunkUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// toolCallState tracks one in-flight tool call, keyed by its index in the
// delta's tool_calls array. Arguments arrive as partial JSON fragments and
// must be buffered whole before being parsed (same rule as Anthropic: do
// not attempt incremental parsing).
type toolCallState struct {
	id      string
	name    string
	args    strings.Builder
	started bool
}

// chunkStream implements provider.Stream over one OpenAI-compatible
// streaming HTTP response.
type chunkStream struct {
	body    io.ReadCloser
	reader  *sse.Reader
	calls   map[int]*toolCallState
	order   []int // insertion order, so finalization is deterministic
	pending []provider.Event
	err     error // sticky terminal error (io.EOF or real error)
}

func newChunkStream(body io.ReadCloser) *chunkStream {
	return &chunkStream{
		body:   body,
		reader: sse.NewReader(body, 1024*1024),
		calls:  map[int]*toolCallState{},
	}
}

func (s *chunkStream) Close() error {
	return s.body.Close()
}

func (s *chunkStream) Next() (provider.Event, error) {
	if ev, ok := s.popPending(); ok {
		return ev, nil
	}
	if s.err != nil {
		return provider.Event{}, s.err
	}

	for {
		data, ok := s.reader.Next()
		if !ok {
			s.err = io.EOF
			return provider.Event{}, s.err
		}
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			s.err = io.EOF
			return provider.Event{}, s.err
		}

		var chunk wireChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			s.err = fmt.Errorf("openai: decode stream chunk: %w", err)
			return provider.Event{}, s.err
		}

		s.handleChunk(chunk)
		if ev, ok := s.popPending(); ok {
			return ev, nil
		}
	}
}

// handleChunk queues every event one chunk produces onto pending; Next
// drains them one at a time.
func (s *chunkStream) handleChunk(chunk wireChunk) {
	if len(chunk.Choices) == 0 {
		s.queueUsage(chunk.Usage)
		return
	}

	choice := chunk.Choices[0]

	if choice.Delta.Content != "" {
		s.pending = append(s.pending, provider.Event{Kind: provider.KindTextDelta, TextDelta: choice.Delta.Content})
	}
	for _, tc := range choice.Delta.ToolCalls {
		s.applyToolCallDelta(tc)
	}

	if choice.FinishReason != nil {
		s.finalizeOpenToolCalls()
		s.queueUsage(chunk.Usage)
		s.pending = append(s.pending, provider.Event{
			Kind:       provider.KindStopReason,
			StopReason: toStopReason(*choice.FinishReason),
		})
	}
}

func (s *chunkStream) queueUsage(usage *wireChunkUsage) {
	if usage == nil {
		return
	}
	s.pending = append(s.pending, provider.Event{
		Kind: provider.KindUsage,
		Usage: provider.Usage{
			InputTokens:  usage.PromptTokens,
			OutputTokens: usage.CompletionTokens,
		},
	})
}

func (s *chunkStream) applyToolCallDelta(tc wireChunkToolCall) {
	state, exists := s.calls[tc.Index]
	if !exists {
		state = &toolCallState{}
		s.calls[tc.Index] = state
		s.order = append(s.order, tc.Index)
	}

	if !state.started && tc.ID != "" {
		state.id = tc.ID
		state.name = tc.Function.Name
		state.started = true
		s.pending = append(s.pending, provider.Event{
			Kind:          provider.KindToolCallStart,
			ToolCallStart: provider.ToolCallStart{ID: state.id, Name: state.name},
		})
	}

	if tc.Function.Arguments != "" {
		state.args.WriteString(tc.Function.Arguments)
		s.pending = append(s.pending, provider.Event{
			Kind:          provider.KindToolCallDelta,
			ToolCallDelta: provider.ToolCallDelta{ID: state.id, InputDelta: tc.Function.Arguments},
		})
	}
}

// finalizeOpenToolCalls emits a ToolCallEnd for every still-open tool
// call, in the order they started. Handles parallel tool calls (multiple
// concurrently-open indices) for free; in practice there's almost always
// at most one.
func (s *chunkStream) finalizeOpenToolCalls() {
	for _, idx := range s.order {
		state, ok := s.calls[idx]
		if !ok {
			continue
		}
		raw := state.args.String()
		if raw == "" {
			raw = "{}"
		}
		s.pending = append(s.pending, provider.Event{
			Kind: provider.KindToolCallEnd,
			ToolCallEnd: provider.ToolCallEnd{
				ID:    state.id,
				Name:  state.name,
				Input: json.RawMessage(raw),
			},
		})
	}
	s.calls = map[int]*toolCallState{}
	s.order = nil
}

func (s *chunkStream) popPending() (provider.Event, bool) {
	if len(s.pending) == 0 {
		return provider.Event{}, false
	}
	ev := s.pending[0]
	s.pending = s.pending[1:]
	return ev, true
}

func toStopReason(reason string) provider.StopReason {
	switch reason {
	case "stop":
		return provider.StopReasonEndTurn
	case "tool_calls", "function_call":
		return provider.StopReasonToolUse
	case "length":
		return provider.StopReasonMaxTokens
	default:
		return provider.StopReasonUnknown
	}
}
