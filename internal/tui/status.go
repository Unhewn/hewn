package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/unhewn/hewn/internal/agent"
)

// appState is the loop's current activity, shown in the status bar.
type appState int

const (
	stateIdle appState = iota
	stateStreaming
	stateAwaitingApproval
)

func (s appState) String() string {
	switch s {
	case stateIdle:
		return "idle"
	case stateStreaming:
		return "thinking"
	case stateAwaitingApproval:
		return "awaiting approval"
	default:
		return "unknown"
	}
}

// renderStatusBar shows model / cumulative tokens / cwd / state / activity, per
// HEWN.md §4's mockup.
func renderStatusBar(width int, model, cwd string, usage agent.Usage, state appState, activity string) string {
	totalTok := usage.InputTokens + usage.OutputTokens
	left := fmt.Sprintf(" hewn · %s · %s · %.1fk tok ", model, cwd, float64(totalTok)/1000)
	right := fmt.Sprintf(" %s%s ", activity, state)

	pad := width - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 0 {
		pad = 0
	}

	return styleStatusBar.Width(width).Render(left + strings.Repeat(" ", pad) + right)
}
