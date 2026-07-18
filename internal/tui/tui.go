package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Model struct {
	viewport viewport.Model
	input    textarea.Model
	messages []string
	width    int
	height   int
	thinking bool
}

func InitialModel() Model {
	ta := textarea.New()
	ta.Placeholder = "Type your message... (/help for commands)"
	ta.Focus()
	ta.SetWidth(80)
	ta.SetHeight(3)

	vp := viewport.New(80, 20)
	vp.SetContent("Welcome to hewn! A clean minimalist agent.\n\n")

	return Model{
		viewport: vp,
		input:    ta,
		messages: []string{},
	}
}

func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width
		m.input.SetWidth(msg.Width)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit

		case "enter":
			input := strings.TrimSpace(m.input.Value())
			if input == "" {
				return m, nil
			}

			// Add user message
			m.messages = append(m.messages, "You: "+input)
			m.viewport.SetContent(strings.Join(m.messages, "\n\n"))

			// Clear input
			m.input.SetValue("")

			// Simulate thinking + response (replace with real LLM later)
			m.thinking = true
			respCmd := simulateResponse(&m)
			return m, respCmd
		}
	}

	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")).
		Render("hewn — minimalist agent harness")

	help := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("↑↓ scroll • Enter send • /command")

	return lipgloss.JoinVertical(lipgloss.Top,
		header,
		m.viewport.View(),
		m.input.View(),
		help,
	)
}

func Start() {
	p := tea.NewProgram(InitialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func simulateResponse(m *Model) tea.Cmd {
	return func() tea.Msg {
		// This will be replaced with real LLM streaming later
		response := "Assistant: I received your message. This is a placeholder response."
		m.messages = append(m.messages, response)
		m.viewport.SetContent(strings.Join(m.messages, "\n\n"))
		m.thinking = false
		return nil
	}
}
