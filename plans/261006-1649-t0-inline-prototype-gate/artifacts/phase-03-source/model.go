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
	g1                          bool
	transcript                  transcript
	manualTranscript            bool
	transcriptLines             int
}

func (m model) Init() tea.Cmd {
	if m.manualTranscript {
		return tea.Sequence(tea.Println("T0 startup"), func() tea.Msg { return fixtureAction("g1-manual") })
	}
	return tea.Println("T0 startup")
}
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
		if m.g1 {
			m.commitReport()
		}
	case commitWrite:
		cmd := m.transcript.observe(v)
		m.commitReport()
		return m, cmd
	case readyBlock:
		if int(v) >= 0 && int(v) < len(m.transcript.ready) {
			m.transcript.ready[int(v)] = true
		}
		return m, m.transcript.schedule()
	case fixtureAction:
		switch v {
		case "g1-manual":
			m.transcript.start(m.transcriptLines, false)
			return m, m.transcript.schedule()
		case "g1-stream":
			m.g1 = true
			m.transcript.start(2000, false)
			m.transcript.holdAt = 500
			return m, m.transcript.schedule()
		case "g1-continue":
			m.transcript.holdAt += 500
			return m, m.transcript.schedule()
		case "g1-shrink-prepare":
			m.g1 = true
			m.transcript.start(10, false)
			for i := range m.transcript.ready {
				m.transcript.ready[i] = false
			}
			m.transcript.tailRows = 3
			m.commitReport()
		case "g1-release":
			for i := range m.transcript.ready {
				m.transcript.ready[i] = true
			}
			return m, m.transcript.schedule()
		case "g1-start":
			m.g1 = true
			m.transcript.start(2000, false)
			return m, m.transcript.schedule()
		case "g1-oversized":
			m.g1 = true
			m.transcript.start(30, true)
			return m, m.transcript.schedule()
		case "g1-out-of-order":
			m.g1 = true
			m.transcript.start(3, false)
			m.transcript.ready = []bool{false, false, false}
			m.commitReport()
		case "g1-shrink":
			m.g1 = true
			m.transcript.start(10, false)
			m.transcript.tailRows = 3
			return m, m.transcript.schedule()
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
		if m.g6 || m.g1 {
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
	if m.g1 && height > 1 {
		tail := fmt.Sprintf("tail %04d %s", m.transcript.confirmed, strings.Repeat(".", m.transcript.confirmed%20))
		if m.transcript.tailRows > 1 && m.transcript.confirmed == 0 {
			tail += "\ntail extra\ntail extra"
		}
		if m.transcript.tailRows > 0 {
			content += "\n" + ansi.Truncate(tail, width, "")
		}
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

func (m model) commitReport() {
	if m.status != nil {
		if err := writeStatus(m.status, "commit %d %d %t\n", m.transcript.scheduled, m.transcript.confirmed, m.transcript.stopped); err != nil {
			panic(err)
		}
	}
}
