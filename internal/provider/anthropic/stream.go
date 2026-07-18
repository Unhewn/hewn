package anthropic

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/unhewn/hewn/internal/provider"
)

// sseEvent mirrors the union of Anthropic Messages API streaming event
// payloads. Only the fields relevant to the event's own Type are populated.
type sseEvent struct {
	Type string `json:"type"`

	Message *struct {
		Usage wireUsage `json:"usage"`
	} `json:"message,omitempty"`

	Index int `json:"index"`

	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id,omitempty"`
		Name string `json:"name,omitempty"`
	} `json:"content_block,omitempty"`

	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text,omitempty"`
		Thinking    string `json:"thinking,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
		StopReason  string `json:"stop_reason,omitempty"`
	} `json:"delta,omitempty"`

	Usage *wireUsage `json:"usage,omitempty"`

	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type wireUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// blockState tracks one in-flight content block, keyed by its SSE index,
// across content_block_start/delta/stop. Tool-call input arrives as partial
// JSON fragments and must be buffered whole before it is parsed (do not
// attempt incremental parsing).
type blockState struct {
	kind string // "text" | "thinking" | "tool_use"
	id   string
	name string
	json strings.Builder
}

// sseStream implements provider.Stream over one Anthropic Messages API
// streaming HTTP response.
type sseStream struct {
	body       io.ReadCloser
	scan       *bufio.Scanner
	blocks     map[int]*blockState
	pending    []provider.Event
	inputUsage wireUsage
	err        error // sticky terminal error (io.EOF or real error)
}

func newSSEStream(body io.ReadCloser) *sseStream {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &sseStream{
		body:   body,
		scan:   scanner,
		blocks: map[int]*blockState{},
	}
}

func (s *sseStream) Close() error {
	return s.body.Close()
}

func (s *sseStream) Next() (provider.Event, error) {
	if len(s.pending) > 0 {
		ev := s.pending[0]
		s.pending = s.pending[1:]
		return ev, nil
	}
	if s.err != nil {
		return provider.Event{}, s.err
	}

	for {
		data, ok := s.nextDataLine()
		if !ok {
			s.err = io.EOF
			return provider.Event{}, s.err
		}
		if data == "" {
			continue
		}

		var env sseEvent
		if err := json.Unmarshal([]byte(data), &env); err != nil {
			s.err = fmt.Errorf("anthropic: decode SSE event: %w", err)
			return provider.Event{}, s.err
		}

		ev, emit, term := s.handle(env)
		if term != nil {
			s.err = term
			return provider.Event{}, s.err
		}
		if emit {
			return ev, nil
		}
	}
}

// handle processes one decoded SSE event. It returns (event, true, nil) when
// an event should be emitted immediately, (_, false, nil) to keep reading,
// or (_, _, err) when the stream has ended (io.EOF) or failed.
func (s *sseStream) handle(env sseEvent) (provider.Event, bool, error) {
	switch env.Type {
	case "message_start":
		if env.Message != nil {
			s.inputUsage = env.Message.Usage
		}
		return provider.Event{}, false, nil

	case "ping":
		return provider.Event{}, false, nil

	case "content_block_start":
		if env.ContentBlock == nil {
			return provider.Event{}, false, nil
		}
		bs := &blockState{kind: env.ContentBlock.Type, id: env.ContentBlock.ID, name: env.ContentBlock.Name}
		s.blocks[env.Index] = bs
		if bs.kind == "tool_use" {
			return provider.Event{
				Kind:          provider.KindToolCallStart,
				ToolCallStart: provider.ToolCallStart{ID: bs.id, Name: bs.name},
			}, true, nil
		}
		return provider.Event{}, false, nil

	case "content_block_delta":
		return s.handleDelta(env)

	case "content_block_stop":
		return s.handleBlockStop(env)

	case "message_delta":
		s.queueMessageDelta(env)
		return s.drainPending()

	case "message_stop":
		return provider.Event{}, false, io.EOF

	case "error":
		msg := "unknown error"
		if env.Error != nil {
			msg = env.Error.Message
		}
		return provider.Event{}, false, fmt.Errorf("anthropic: stream error: %s", msg)

	default:
		return provider.Event{}, false, nil
	}
}

func (s *sseStream) handleDelta(env sseEvent) (provider.Event, bool, error) {
	if env.Delta == nil {
		return provider.Event{}, false, nil
	}
	switch env.Delta.Type {
	case "text_delta":
		return provider.Event{Kind: provider.KindTextDelta, TextDelta: env.Delta.Text}, true, nil
	case "thinking_delta":
		return provider.Event{Kind: provider.KindThinkingDelta, ThinkingDelta: env.Delta.Thinking}, true, nil
	case "input_json_delta":
		if bs, ok := s.blocks[env.Index]; ok {
			bs.json.WriteString(env.Delta.PartialJSON)
		}
		return provider.Event{}, false, nil
	default:
		return provider.Event{}, false, nil // signature_delta and future kinds
	}
}

func (s *sseStream) handleBlockStop(env sseEvent) (provider.Event, bool, error) {
	bs, ok := s.blocks[env.Index]
	if !ok {
		return provider.Event{}, false, nil
	}
	delete(s.blocks, env.Index)
	if bs.kind != "tool_use" {
		return provider.Event{}, false, nil
	}

	raw := bs.json.String()
	if raw == "" {
		raw = "{}"
	}
	return provider.Event{
		Kind: provider.KindToolCallEnd,
		ToolCallEnd: provider.ToolCallEnd{
			ID:    bs.id,
			Name:  bs.name,
			Input: json.RawMessage(raw),
		},
	}, true, nil
}

func (s *sseStream) queueMessageDelta(env sseEvent) {
	if env.Usage != nil {
		s.pending = append(s.pending, provider.Event{
			Kind: provider.KindUsage,
			Usage: provider.Usage{
				InputTokens:      s.inputUsage.InputTokens,
				OutputTokens:     env.Usage.OutputTokens,
				CacheReadTokens:  s.inputUsage.CacheReadInputTokens,
				CacheWriteTokens: s.inputUsage.CacheCreationInputTokens,
			},
		})
	}
	if env.Delta != nil && env.Delta.StopReason != "" {
		s.pending = append(s.pending, provider.Event{
			Kind:       provider.KindStopReason,
			StopReason: toStopReason(env.Delta.StopReason),
		})
	}
}

func (s *sseStream) drainPending() (provider.Event, bool, error) {
	if len(s.pending) == 0 {
		return provider.Event{}, false, nil
	}
	ev := s.pending[0]
	s.pending = s.pending[1:]
	return ev, true, nil
}

// nextDataLine reads one SSE event's concatenated "data:" payload, returning
// ok=false once the underlying reader is exhausted with no further data.
func (s *sseStream) nextDataLine() (string, bool) {
	var data strings.Builder
	found := false
	for s.scan.Scan() {
		line := s.scan.Text()
		if line == "" {
			if found {
				return data.String(), true
			}
			continue
		}
		if rest, ok := strings.CutPrefix(line, "data:"); ok {
			found = true
			data.WriteString(strings.TrimPrefix(rest, " "))
		}
	}
	if found {
		return data.String(), true
	}
	return "", false
}

func toStopReason(s string) provider.StopReason {
	switch s {
	case "end_turn":
		return provider.StopReasonEndTurn
	case "tool_use":
		return provider.StopReasonToolUse
	case "max_tokens":
		return provider.StopReasonMaxTokens
	case "stop_sequence":
		return provider.StopReasonStopSequence
	default:
		return provider.StopReasonUnknown
	}
}
