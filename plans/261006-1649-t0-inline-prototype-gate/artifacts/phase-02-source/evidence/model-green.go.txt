package main

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"os"
)

type model struct {
	status        *os.File
	width, height int
	text          string
}

func (m model) Init() tea.Cmd { return tea.Println("T0 startup") }
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
		if m.status != nil {
			if _, err := fmt.Fprintf(m.status, "size %d %d\n", m.width, m.height); err != nil {
				return m, tea.Quit
			}
		}
	case tea.KeyPressMsg:
		if v.String() == "ctrl+c" {
			return m, tea.Quit
		}
		m.text += v.Text
	}
	return m, nil
}
func (m model) View() tea.View {
	v := tea.NewView("editor> " + m.text)
	v.Cursor = tea.NewCursor(8+len(m.text), 0)
	return v
}
