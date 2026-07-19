package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// itemKind identifies which fields of a transcriptItem are meaningful.
type itemKind int

const (
	itemUser itemKind = iota
	itemAssistant
	itemToolCall
	itemSystem
)

// toolCallItem tracks one tool call's lifecycle for display: its
// arguments (once known), live-streamed output, and final result.
type toolCallItem struct {
	id      string
	name    string
	input   string
	output  strings.Builder
	result  string
	isError bool
	done    bool
}

// transcriptItem is one entry in the TUI's view-layer transcript, built
// from observed agent.Event values (AGENTS.md invariant #1 -- the TUI
// never reads Loop's internal history directly).
type transcriptItem struct {
	kind itemKind

	// itemUser / itemSystem
	text string

	// itemAssistant: raw accumulates while streaming; rendered is the
	// glamour output, computed once in closeAssistant (not lazily inside
	// View, which must stay cheap and pure -- AGENTS.md's Bubble Tea
	// rules).
	raw      strings.Builder
	rendered string
	closed   bool

	// itemToolCall
	tool *toolCallItem
}

func newUserItem(text string) transcriptItem   { return transcriptItem{kind: itemUser, text: text} }
func newSystemItem(text string) transcriptItem { return transcriptItem{kind: itemSystem, text: text} }
func newAssistantItem() transcriptItem         { return transcriptItem{kind: itemAssistant} }

func newToolCallItem(id, name string) transcriptItem {
	return transcriptItem{kind: itemToolCall, tool: &toolCallItem{id: id, name: name}}
}

// closeAssistant finalizes an itemAssistant's text once its segment ends
// (a stop reason, or the next content block starting), rendering it
// through glamour. Called from Update, never from View.
func closeAssistant(item *transcriptItem, renderer *glamour.TermRenderer) {
	if item.kind != itemAssistant || item.closed {
		return
	}
	item.closed = true

	raw := item.raw.String()
	if renderer == nil {
		item.rendered = raw
		return
	}
	out, err := renderer.Render(raw)
	if err != nil {
		item.rendered = raw
		return
	}
	item.rendered = strings.TrimRight(out, "\n")
}

// newGlamourRenderer builds a renderer word-wrapped to width. Returns nil
// (callers fall back to raw text) if construction fails -- assistant
// output must never disappear just because rendering isn't available.
func newGlamourRenderer(width int) *glamour.TermRenderer {
	if width <= 0 {
		width = 80
	}
	r, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(width))
	if err != nil {
		return nil
	}
	return r
}

// indent is the left padding applied to every line of the transcript.
const indent = "  "

// renderTranscript renders every item into the string handed to
// viewport.SetContent. expandToolCallID, if non-empty, is the ID of the
// one tool call (always the most recent, per the session-5 scope trim) to
// render expanded; every other tool call always renders collapsed.
func renderTranscript(items []transcriptItem, expandToolCallID, userName string) string {
	if userName == "" {
		userName = "you"
	}
	var b strings.Builder
	for i, item := range items {
		if i > 0 {
			b.WriteString("\n\n")
		}
		switch item.kind {
		case itemUser:
			label := styleUser.Render(userName + "  ")
			// Indent continuation lines of multi-line user messages.
			text := strings.ReplaceAll(item.text, "\n", "\n"+strings.Repeat(" ", lipgloss.Width(label)))
			b.WriteString(indent + label + text)
		case itemSystem:
			b.WriteString(indent + styleSystem.Render(item.text))
		case itemAssistant:
			if item.closed {
				b.WriteString(indent + item.rendered)
			} else {
				b.WriteString(indent + item.raw.String())
			}
		case itemToolCall:
			b.WriteString(indent + renderToolCall(item.tool, item.tool.id == expandToolCallID && expandToolCallID != ""))
		}
	}
	return b.String()
}

// renderToolCall renders one tool call. In collapsed view (the default)
// it shows only the tool name and status -- no raw JSON params. Expand
// with ctrl+o to see the full input and output.
func renderToolCall(t *toolCallItem, expanded bool) string {
	status := "●"
	switch {
	case t.done && t.isError:
		status = "✗"
	case t.done:
		status = "✓"
	}

	head := fmt.Sprintf("%s %s", status, styleToolName.Render(t.name))

	if !expanded {
		switch {
		case !t.done:
			head += "  [running...]"
		case t.result != "":
			lines := strings.Count(strings.TrimRight(t.result, "\n"), "\n") + 1
			head += fmt.Sprintf("  [%d lines ▸]", lines)
		default:
			head += "  [done]"
		}
		return head
	}

	// Expanded: show input params and output.
	var b strings.Builder
	b.WriteString(head)
	if t.input != "" {
		b.WriteString(" ")
		b.WriteString(t.input)
	}
	b.WriteString("\n")
	switch {
	case !t.done:
		b.WriteString(indent + t.output.String())
	case t.isError:
		b.WriteString(indent + styleToolError.Render(t.result))
	default:
		b.WriteString(indent + t.result)
	}
	return b.String()
}
