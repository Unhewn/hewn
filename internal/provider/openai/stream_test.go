package openai

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/unhewn/hewn/internal/provider"
)

// fixtures under testdata/ are hand-built against the documented
// OpenAI-compatible Chat Completions streaming format (the same one
// Ollama, llama.cpp's server, and LM Studio implement), not recorded from
// a live call -- this environment has no way to record fixtures from an
// arbitrary backend the way it can for a single vendor's API.

func openFixture(t *testing.T, name string) io.ReadCloser {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	return f
}

func drain(t *testing.T, s *chunkStream) []provider.Event {
	t.Helper()
	var events []provider.Event
	for {
		ev, err := s.Next()
		if errors.Is(err, io.EOF) {
			return events
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		events = append(events, ev)
	}
}

func TestChunkStream_TextResponse(t *testing.T) {
	s := newChunkStream(openFixture(t, "text_response.sse"))
	defer s.Close()

	events := drain(t, s)

	want := []provider.Event{
		{Kind: provider.KindTextDelta, TextDelta: "Hello"},
		{Kind: provider.KindTextDelta, TextDelta: ", world."},
		{Kind: provider.KindStopReason, StopReason: provider.StopReasonEndTurn},
		{Kind: provider.KindUsage, Usage: provider.Usage{InputTokens: 10, OutputTokens: 5}},
	}
	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(want), events)
	}
	for i, w := range want {
		if !reflect.DeepEqual(events[i], w) {
			t.Errorf("event %d = %+v, want %+v", i, events[i], w)
		}
	}
}

func TestChunkStream_ToolCall(t *testing.T) {
	s := newChunkStream(openFixture(t, "tool_call.sse"))
	defer s.Close()

	events := drain(t, s)
	if len(events) != 6 {
		t.Fatalf("got %d events, want 6: %+v", len(events), events)
	}

	if got := events[0]; got.Kind != provider.KindToolCallStart ||
		got.ToolCallStart.ID != "call_01" || got.ToolCallStart.Name != "read" {
		t.Errorf("event 0 = %+v, want ToolCallStart{call_01, read}", got)
	}

	if got := events[1]; got.Kind != provider.KindToolCallDelta || got.ToolCallDelta.InputDelta != `{"path":` {
		t.Errorf("event 1 = %+v", got)
	}
	if got := events[2]; got.Kind != provider.KindToolCallDelta || got.ToolCallDelta.InputDelta != `"main.go"}` {
		t.Errorf("event 2 = %+v", got)
	}

	end := events[3]
	if end.Kind != provider.KindToolCallEnd || end.ToolCallEnd.ID != "call_01" || end.ToolCallEnd.Name != "read" {
		t.Errorf("event 3 = %+v, want ToolCallEnd{call_01, read, ...}", end)
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(end.ToolCallEnd.Input, &input); err != nil {
		t.Fatalf("unmarshal assembled tool input: %v", err)
	}
	if input.Path != "main.go" {
		t.Errorf("assembled tool input path = %q, want %q", input.Path, "main.go")
	}

	if want := (provider.Event{Kind: provider.KindStopReason, StopReason: provider.StopReasonToolUse}); !reflect.DeepEqual(events[4], want) {
		t.Errorf("event 4 = %+v, want %+v", events[4], want)
	}
	if want := (provider.Event{Kind: provider.KindUsage, Usage: provider.Usage{InputTokens: 20, OutputTokens: 15}}); !reflect.DeepEqual(events[5], want) {
		t.Errorf("event 5 = %+v, want %+v", events[5], want)
	}
}

func TestChunkStream_ErrorChunk(t *testing.T) {
	raw := `data: {"error":{"message":"model not found"}}` + "\n\n"
	s := newChunkStream(io.NopCloser(strings.NewReader(raw)))
	defer s.Close()

	// An error chunk still decodes (Choices is simply empty and Usage nil),
	// so it's treated the same as any other no-choices chunk: nothing to
	// emit, then the stream ends normally at EOF. Actual HTTP-level errors
	// are caught before streaming even starts (see openai.go's apiError),
	// this just confirms a malformed/empty chunk doesn't crash the parser.
	events := drain(t, s)
	if len(events) != 0 {
		t.Errorf("got %d events for an error-shaped chunk with no choices, want 0: %+v", len(events), events)
	}
}

func TestChunkStream_MalformedJSON(t *testing.T) {
	raw := "data: not json\n\n"
	s := newChunkStream(io.NopCloser(strings.NewReader(raw)))
	defer s.Close()

	_, err := s.Next()
	if err == nil {
		t.Fatal("Next: expected a decode error, got nil")
	}
}
