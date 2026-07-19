package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"

	"github.com/unhewn/hewn/internal/slash"
)

func newInput() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Type your message... (/help for commands)"
	ta.Prompt = "❯ "
	ta.ShowLineNumbers = false
	ta.Focus()
	ta.SetHeight(3)

	for _, s := range []*textarea.Style{&ta.FocusedStyle, &ta.BlurredStyle} {
		s.Prompt = lipgloss.NewStyle().Foreground(colorAccent)
		s.Placeholder = lipgloss.NewStyle().Foreground(colorMuted)
		s.Text = lipgloss.NewStyle().Foreground(colorText)
	}

	return ta
}

// inputBorderColor picks the input box's border color from the app's
// state, so the color alone signals whether typing will do anything right
// now: accent while idle (ready), muted while a turn is streaming (typing
// still queues, but nothing will send until it's your turn again), warning
// while a tool call is awaiting approval (look up, not down).
func inputBorderColor(state appState) lipgloss.TerminalColor {
	switch state {
	case stateIdle:
		return colorAccent
	case stateStreaming:
		return colorMuted
	case stateAwaitingApproval:
		return colorWarning
	case statePicker:
		// Unreachable in practice -- the picker box replaces the input box
		// entirely in View while open (see picker.go) -- but exhaustive
		// coverage here means a future new state can't slip through
		// unhandled either.
		return colorAccent
	default:
		return colorAccent
	}
}

// renderInputBox wraps the textarea's rendered view in a border whose
// color reflects state (see inputBorderColor), so the color alone signals
// whether typing will do anything right now.
func renderInputBox(view string, contentWidth int, state appState) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(inputBorderColor(state)).
		Width(contentWidth - 2).
		Render(view)
}

// slashSuggestions returns the registered command names matching the
// "/prefix" currently typed, or nil if input isn't a slash command at all.
// Shown as an inline hint line by renderSuggestions, and Tab-completable
// via completeSlashCommand.
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

// completeSlashCommand extends the input to the longest common prefix of
// matching slash commands -- classic shell-style Tab completion. When that
// leaves exactly one match, it also appends a trailing space, ready for
// arguments. A no-op if there's nothing to complete or the input already
// is the completion.
func (m *Model) completeSlashCommand() {
	names := slashSuggestions(m.slashRegistry, m.input.Value())
	if len(names) == 0 {
		return
	}

	common := longestCommonPrefix(names)
	if common == "" {
		return
	}

	completed := "/" + common
	if len(names) == 1 {
		completed += " "
	}
	if completed == m.input.Value() {
		return
	}
	m.input.SetValue(completed)
}

// longestCommonPrefix returns the longest string every element of strs
// starts with. strs must be non-empty.
func longestCommonPrefix(strs []string) string {
	prefix := strs[0]
	for _, s := range strs[1:] {
		for !strings.HasPrefix(s, prefix) {
			prefix = prefix[:len(prefix)-1]
			if prefix == "" {
				return ""
			}
		}
	}
	return prefix
}
