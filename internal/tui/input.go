package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"

	"github.com/unhewn/hewn/internal/slash"
)

func newInput() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Type your message... (/help for commands)"
	ta.Focus()
	ta.SetHeight(3)
	return ta
}

// slashSuggestions returns the registered command names matching the
// "/prefix" currently typed, or nil if input isn't a slash command at all.
// This is the "simple inline suggestion line" scope trim from HEWN.md's
// completion-popup ask: no selection state, Enter still just dispatches
// whatever text is actually typed.
func slashSuggestions(reg *slash.Registry, input string) []string {
	if !strings.HasPrefix(input, "/") || strings.ContainsAny(input, " \n") {
		return nil
	}
	prefix := strings.TrimPrefix(input, "/")

	var out []string
	for _, c := range reg.List() {
		if strings.HasPrefix(c.Name, prefix) {
			out = append(out, c.Name)
		}
	}
	return out
}

// renderSuggestions renders a slashSuggestions result as one line.
func renderSuggestions(names []string) string {
	if len(names) == 0 {
		return ""
	}
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = "/" + n
	}
	return styleSuggestion.Render(strings.Join(parts, "  "))
}
