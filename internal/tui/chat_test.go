package tui

import (
	"strings"
	"testing"
)

func TestCloseAssistant_NoRendererFallsBackToRaw(t *testing.T) {
	item := newAssistantItem()
	item.raw.WriteString("hello **world**")

	closeAssistant(&item, nil)

	if !item.closed {
		t.Fatal("closed = false, want true")
	}
	if item.rendered != "hello **world**" {
		t.Errorf("rendered = %q, want raw text unchanged", item.rendered)
	}
}

func TestCloseAssistant_IsIdempotent(t *testing.T) {
	item := newAssistantItem()
	item.raw.WriteString("first")
	closeAssistant(&item, nil)

	item.raw.WriteString("more text after close, should be ignored")
	closeAssistant(&item, nil)

	if item.rendered != "first" {
		t.Errorf("rendered = %q, want %q (second close must be a no-op)", item.rendered, "first")
	}
}

func TestCloseAssistant_WithRenderer(t *testing.T) {
	item := newAssistantItem()
	item.raw.WriteString("# heading")

	r := newGlamourRenderer(80)
	if r == nil {
		t.Fatal("newGlamourRenderer returned nil")
	}
	closeAssistant(&item, r)

	if item.rendered == "" {
		t.Fatal("rendered is empty")
	}
	if !item.closed {
		t.Error("closed = false, want true")
	}
}

func TestRenderToolCall_CollapsedWhileRunning(t *testing.T) {
	tc := &toolCallItem{name: "bash", input: `{"command":"go test"}`}
	out := renderToolCall(tc, false)

	if !strings.Contains(out, "running") {
		t.Errorf("renderToolCall(running, collapsed) = %q, want it to say running", out)
	}
	if strings.Contains(out, "\n") {
		t.Errorf("renderToolCall(collapsed) = %q, want a single line", out)
	}
}

func TestRenderToolCall_CollapsedWhenDoneShowsLineCount(t *testing.T) {
	tc := &toolCallItem{name: "read", done: true, result: "line one\nline two\nline three"}
	out := renderToolCall(tc, false)

	if !strings.Contains(out, "3 lines") {
		t.Errorf("renderToolCall(done, collapsed) = %q, want it to mention 3 lines", out)
	}
}

func TestRenderToolCall_ExpandedShowsResultEvenWithoutStreamedOutput(t *testing.T) {
	// Regression case: `read` never emits ToolOutput events, so its entire
	// content only ever arrives via the final result. Expanded view must
	// not render empty just because t.output is empty.
	tc := &toolCallItem{name: "read", done: true, result: "file contents here"}
	out := renderToolCall(tc, true)

	if !strings.Contains(out, "file contents here") {
		t.Errorf("renderToolCall(expanded) = %q, want it to include the result", out)
	}
}

func TestRenderToolCall_ExpandedWhileRunningShowsLiveOutput(t *testing.T) {
	tc := &toolCallItem{name: "bash"}
	tc.output.WriteString("partial output so far")
	out := renderToolCall(tc, true)

	if !strings.Contains(out, "partial output so far") {
		t.Errorf("renderToolCall(expanded, running) = %q, want the live tail", out)
	}
}

func TestRenderToolCall_ExpandedErrorUsesResultNotOutput(t *testing.T) {
	tc := &toolCallItem{name: "bash", done: true, isError: true, result: "exit status 1"}
	tc.output.WriteString("some stdout before the failure")
	out := renderToolCall(tc, true)

	if !strings.Contains(out, "exit status 1") {
		t.Errorf("renderToolCall(expanded, error) = %q, want the error result", out)
	}
}

func TestRenderTranscript_ExpandsOnlyTheGivenID(t *testing.T) {
	a := newToolCallItem("t1", "read")
	a.tool.done = true
	a.tool.result = "line one\nline two"

	b := newToolCallItem("t2", "bash")
	b.tool.done = true
	b.tool.result = "output line"

	out := renderTranscript([]transcriptItem{a, b}, "t2")

	if strings.Contains(out, "line one") {
		t.Error("non-expanded tool call t1 rendered its full content")
	}
	if !strings.Contains(out, "output line") {
		t.Error("expanded tool call t2 did not render its full content")
	}
}

func TestSlashSuggestions_FiltersByPrefix(t *testing.T) {
	reg := newTestSlashRegistry()

	got := slashSuggestions(reg, "/mo")
	if len(got) != 1 || got[0] != "model" {
		t.Errorf("slashSuggestions(/mo) = %v, want [model]", got)
	}
}

func TestSlashSuggestions_NonSlashInputReturnsNil(t *testing.T) {
	reg := newTestSlashRegistry()
	if got := slashSuggestions(reg, "hello"); got != nil {
		t.Errorf("slashSuggestions(hello) = %v, want nil", got)
	}
}

func TestSlashSuggestions_StopsAfterASpace(t *testing.T) {
	reg := newTestSlashRegistry()
	if got := slashSuggestions(reg, "/model claude"); got != nil {
		t.Errorf("slashSuggestions(/model claude) = %v, want nil (past the command name)", got)
	}
}
