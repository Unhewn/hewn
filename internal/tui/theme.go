package tui

import "github.com/charmbracelet/lipgloss"

var (
	styleHeader         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleHelp           = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleUser           = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	styleSystem         = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	styleToolName       = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	styleToolError      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleStatusBar      = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("12"))
	styleApprovalPrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	styleSuggestion     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)
