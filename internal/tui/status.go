package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// appState is the loop's current activity, shown in the status bar.
type appState int

const (
	stateIdle appState = iota
	stateStreaming
	stateAwaitingApproval
	statePicker
)

func (s appState) String() string {
	switch s {
	case stateIdle:
		return "idle"
	case stateStreaming:
		return "thinking"
	case stateAwaitingApproval:
		return "awaiting approval"
	case statePicker:
		return "selecting"
	default:
		return "unknown"
	}
}

// contextWarnRatio is how full context needs to be (as a fraction of
// contextWindow) before the status bar switches to the warning color --
// only meaningful when contextWindow is known.
const contextWarnRatio = 0.75

// renderStatusBar shows model / current context size / session total / cwd
// / state / activity, per HEWN.md §4's mockup (extended with the session
// total so it's visible without typing /cost). The context figure is the
// *last turn's* input tokens (how much is actually in context right now),
// shown as a bare count unless contextWindow is known (Loop.ContextWindow,
// an opt-in config value) since Hewn has no general way to discover a
// model's real limit; totalTokens is the cumulative input+output for the
// whole session, same number /cost reports. totalCost is omitted entirely
// (not shown as $0.00) when costKnown is false -- a local/unpriced model
// genuinely has no dollar figure to report, and showing zero would read as
// "this is free" rather than unknown.
func renderStatusBar(width int, model, cwd string, contextTokens, contextWindow, totalTokens int, totalCost float64, costKnown bool, state appState, activity string) string {
	left := fmt.Sprintf(" hewn · %s · %s · %s · Σ%.1fk", model, cwd, formatContextTokens(contextTokens, contextWindow), float64(totalTokens)/1000)
	if costKnown {
		left += fmt.Sprintf(" · $%.4f", totalCost)
	}
	left += " "
	right := fmt.Sprintf(" %s%s ", activity, state)

	pad := width - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 0 {
		pad = 0
	}

	bar := styleStatusBar
	if contextWindow > 0 && float64(contextTokens)/float64(contextWindow) >= contextWarnRatio {
		bar = styleStatusBarWarning
	}
	return bar.Width(width).Render(left + strings.Repeat(" ", pad) + right)
}

// formatContextTokens renders the current-context token count, with a
// percentage of contextWindow when it's known (> 0).
func formatContextTokens(tokens, contextWindow int) string {
	if contextWindow <= 0 {
		return fmt.Sprintf("%.1fk tok", float64(tokens)/1000)
	}
	pct := float64(tokens) / float64(contextWindow) * 100
	return fmt.Sprintf("%.1fk / %.0fk tok (%.0f%%)", float64(tokens)/1000, float64(contextWindow)/1000, pct)
}
