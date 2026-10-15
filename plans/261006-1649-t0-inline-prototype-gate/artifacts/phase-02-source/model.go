package main

import (
	tea "charm.land/bubbletea/v2"
	"crypto/sha256"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"os"
	"strings"
	"time"
)

type inspectDraft struct{}
type fixtureAction string
type model struct {
	status                      *os.File
	width, height               int
	text                        string
	cursor                      int
	pastes, submits             int
	negotiated, keyboardOff, g6 bool
}

func (m model) Init() tea.Cmd { return tea.Println("T0 startup") }
func (m *model) insert(s string) {
	m.cursor = min(max(m.cursor, 0), len(m.text))
	m.text = m.text[:m.cursor] + s + m.text[m.cursor:]
	m.cursor += len(s)
}
func (m model) report() {
	if m.status != nil {
		if err := writeStatus(m.status, "draft %x %d %d %d %d %t\n", sha256.Sum256([]byte(m.text)), strings.Count(m.text, "\n")+1, m.pastes, m.submits, len(m.text), m.negotiated); err != nil {
			panic(err)
		}
	}
}
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
		if m.status != nil {
			if err := writeStatus(m.status, "size %d %d\n", m.width, m.height); err != nil {
				return m, tea.Quit
			}
		}
	case tea.KeyboardEnhancementsMsg:
		m.negotiated = !m.keyboardOff && v.SupportsKeyDisambiguation()
		m.report()
	case inspectDraft:
		m.report()
	case fixtureAction:
		switch v {
		case "model-panic":
			panic("fixture model panic")
		case "command-panic":
			return m, func() tea.Msg { panic("fixture command panic") }
		case "output-error":
			m.insert("failed output trigger")
		}
	case tea.PasteMsg:
		m.pastes++
		m.insert(strings.ReplaceAll(v.Content, "\r\n", "\n"))
		m.report()
	case tea.KeyPressMsg:
		switch v.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "alt+enter":
			m.insert("\n")
		case "shift+enter", "ctrl+enter":
			if m.negotiated {
				m.insert("\n")
			}
		case "enter":
			m.submits++
		case "left":
			m.cursor = previousBoundary(m.text, m.cursor)
		case "right":
			m.cursor = nextBoundary(m.text, m.cursor)
		case "backspace":
			start := previousBoundary(m.text, m.cursor)
			m.text = m.text[:start] + m.text[m.cursor:]
			m.cursor = start
		default:
			if v.Text != "" {
				m.insert(v.Text)
			}
		}
		if m.g6 {
			m.report()
		}
	}
	return m, nil
}
func (m model) View() tea.View {
	text := m.text
	cursor := m.cursor
	if !m.g6 && cursor == 0 {
		cursor = len(text)
	}
	width, height := m.width, m.height
	if width <= 0 {
		width = 40
	}
	if height <= 0 {
		height = 3
	}
	liveHeight := height
	if m.g6 && height > 3 {
		liveHeight--
	}
	content, x, y := editorFrame(text, cursor, width, liveHeight)
	if m.g6 && m.height > 3 {
		help := "Alt+Enter: newline; Enter: submit"
		if m.negotiated {
			help = "Shift/Ctrl/Alt+Enter: newline"
		}
		content += "\n" + ansi.Truncate(help, width, "")
	}
	v := tea.NewView(content)
	v.Cursor = tea.NewCursor(x, y)
	if m.g6 && !m.keyboardOff {
		v.KeyboardEnhancements.ReportEventTypes = true
	}
	return v
}

// writeStatus bounds checkpoint I/O and keeps it separate from terminal output.
func writeStatus(file *os.File, format string, args ...any) error {
	if err := file.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}
	_, err := fmt.Fprintf(file, format, args...)
	return err
}
