package anthropic

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

// fixtures under testdata/ are hand-built against the documented Anthropic
// Messages API streaming format, not recorded from a live call -- this
// environment has no network access or API key. Replace with a real
// recording (go test -tags=integration) when one is available.

func openFixture(t *testing.T, name string) io.ReadCloser {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	return f
}

func drain(t *testing.T, s *sseStream) []provider.Event {
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

func TestSSEStream_TextResponse(t *testing.T) {
	s := newSSEStream(openFixture(t, "text_response.sse"))
	defer s.Close()

	events := drain(t, s)

	want := []provider.Event{
		{Kind: provider.KindTextDelta, TextDelta: "Hello"},
		{Kind: provider.KindTextDelta, TextDelta: ", world."},
		{Kind: provider.KindUsage, Usage: provider.Usage{InputTokens: 10, OutputTokens: 5}},
		{Kind: provider.KindStopReason, StopReason: provider.StopReasonEndTurn},
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

func TestSSEStream_ToolCall(t *testing.T) {
	s := newSSEStream(openFixture(t, "tool_call.sse"))
	defer s.Close()

	events := drain(t, s)

	if len(events) != 4 {
		t.Fatalf("got %d events, want 4: %+v", len(events), events)
	}

	if got := events[0]; got.Kind != provider.KindToolCallStart ||
		got.ToolCallStart.ID != "toolu_01" || got.ToolCallStart.Name != "read" {
		t.Errorf("event 0 = %+v, want ToolCallStart{toolu_01, read}", got)
	}

	end := events[1]
	if end.Kind != provider.KindToolCallEnd || end.ToolCallEnd.ID != "toolu_01" || end.ToolCallEnd.Name != "read" {
		t.Errorf("event 1 = %+v, want ToolCallEnd{toolu_01, read, ...}", end)
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

	if want := (provider.Event{Kind: provider.KindUsage, Usage: provider.Usage{InputTokens: 20, OutputTokens: 15}}); !reflect.DeepEqual(events[2], want) {
		t.Errorf("event 2 = %+v, want %+v", events[2], want)
	}
	if want := (provider.Event{Kind: provider.KindStopReason, StopReason: provider.StopReasonToolUse}); !reflect.DeepEqual(events[3], want) {
		t.Errorf("event 3 = %+v, want %+v", events[3], want)
	}
}

func TestSSEStream_ErrorEvent(t *testing.T) {
	raw := `event: error
data: {"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}

`
	s := newSSEStream(io.NopCloser(strings.NewReader(raw)))
	defer s.Close()

	_, err := s.Next()
	if err == nil {
		t.Fatal("Next: expected error, got nil")
	}
}
