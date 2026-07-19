package tui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// pickerMaxHeight caps how tall a picker gets, matching the input box's own
// 3-line height exactly -- the picker replaces that box in View, and
// reservedLines' vertical budget was sized around it. A longer choice list
// scrolls (pagination dots) or narrows via type-to-filter instead of
// growing the box and reopening the same clipping bug fixed last round.
const pickerMaxHeight = 3

// choiceItem is one selectable value in a picker (e.g. a model ID).
type choiceItem string

func (c choiceItem) FilterValue() string { return string(c) }

// choiceDelegate renders a picker row as one line: an accent cursor and
// highlighted text on the selected row, plain muted text otherwise. No
// description line, no extra chrome -- these are short, self-explanatory
// values (model IDs today; any other command's Choices tomorrow).
type choiceDelegate struct{}

func (choiceDelegate) Height() int                         { return 1 }
func (choiceDelegate) Spacing() int                        { return 0 }
func (choiceDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (choiceDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	c, ok := item.(choiceItem)
	if !ok {
		return
	}
	if index == m.Index() {
		_, _ = fmt.Fprint(w, styleUser.Render("› "+string(c)))
		return
	}
	_, _ = fmt.Fprint(w, styleSystem.Render("  "+string(c)))
}

// picker is an open arrow-key (or type-to-filter) selection prompt. It
// replaces the input box until the user picks a value or cancels (Esc).
// Selecting re-dispatches "/<selectCommand> <value>" through the normal
// slash registry, so the only thing that lands in the transcript is that
// command's own confirmation -- not the full list that led to it.
type picker struct {
	list          list.Model
	selectCommand string
}

// newPicker builds a picker sized to width columns, tall enough for
// choices up to pickerMaxHeight rows.
func newPicker(choices []string, selectCommand string, width int) picker {
	items := make([]list.Item, len(choices))
	for i, c := range choices {
		items[i] = choiceItem(c)
	}

	height := len(choices)
	if height > pickerMaxHeight {
		height = pickerMaxHeight
	}
	if height < 1 {
		height = 1
	}

	l := list.New(items, choiceDelegate{}, width, height)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(len(choices) > height)
	l.SetFilteringEnabled(true)
	l.KeyMap.Quit.SetEnabled(false) // "q" should filter/type, not quit

	return picker{list: l, selectCommand: selectCommand}
}

// selectedValue returns the currently highlighted choice.
func (p picker) selectedValue() (string, bool) {
	item, ok := p.list.SelectedItem().(choiceItem)
	if !ok {
		return "", false
	}
	return string(item), true
}

// renderPickerBox wraps the picker's rendered list in the same style of
// border as the input box it replaces, so the layout doesn't visually
// jump when one opens.
func renderPickerBox(view string, contentWidth int) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Width(contentWidth - 2).
		Render(view)
}
