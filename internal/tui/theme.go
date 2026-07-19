package tui

import "github.com/charmbracelet/lipgloss"

// Color roles. accent is hewn's one brand color; swapping it later is a
// one-line change. Everything else is a fixed semantic role that reads
// correctly on both light and dark terminal backgrounds.
var (
	colorAccent  = lipgloss.AdaptiveColor{Light: "#1f6feb", Dark: "#5ec4ff"}
	colorSuccess = lipgloss.AdaptiveColor{Light: "#2a7a3b", Dark: "#5fd97a"}
	colorDanger  = lipgloss.AdaptiveColor{Light: "#b3392c", Dark: "#e0685a"}
	colorWarning = lipgloss.AdaptiveColor{Light: "#a8791f", Dark: "#e0b74a"}
	colorMuted   = lipgloss.AdaptiveColor{Light: "#8a8a96", Dark: "#6a6a76"}
	colorText    = lipgloss.AdaptiveColor{Light: "#111113", Dark: "#f2f2f5"}
)

var (
	styleWelcomeTitle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	styleHelp         = lipgloss.NewStyle().Foreground(colorMuted)
	styleUser         = lipgloss.NewStyle().Foreground(colorText).Bold(true)
	styleSystem       = lipgloss.NewStyle().Foreground(colorText).Italic(true)
	styleSuggestion   = lipgloss.NewStyle().Foreground(colorMuted)

	styleToolName    = lipgloss.NewStyle().Foreground(colorAccent)
	styleToolPending = lipgloss.NewStyle().Foreground(colorMuted)
	styleToolSuccess = lipgloss.NewStyle().Foreground(colorSuccess)
	styleToolError   = lipgloss.NewStyle().Foreground(colorDanger)

	styleStatusBar        = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(colorAccent)
	styleStatusBarWarning = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(colorWarning)
	styleApprovalPrompt   = lipgloss.NewStyle().Foreground(colorWarning).Bold(true)
)
