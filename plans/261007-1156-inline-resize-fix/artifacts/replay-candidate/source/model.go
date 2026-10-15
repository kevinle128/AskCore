package main

import (
	tea "charm.land/bubbletea/v2"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"time"
)

type resizeSettled struct{ Generation uint64 }
type inspectDraft struct{}
type fixtureAction string
type model struct {
	resizeGeneration                             uint64
	repairPending, repairSettled, repairInFlight bool
	status                                       *os.File
	width, height                                int
	text                                         string
	cursor                                       int
	pastes, submits                              int
	negotiated, keyboardOff, g6                  bool
	g1                                           bool
	transcript                                   transcript
	manualTranscript                             bool
	transcriptLines                              int
	selector                                     selector
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
		changed := m.width > 0 && m.height > 0 && (m.width != v.Width || m.height != v.Height) || m.repairPending
		if v.Generation != 0 {
			m.resizeGeneration = v.Generation
		} else {
			m.resizeGeneration++
		}
		m.width, m.height = v.Width, v.Height
		if changed {
			m.repairPending = true
			m.repairInFlight = false
			m.repairSettled = false
			m.transcript.paused = true
		}
		if m.status != nil {
			if err := writeStatus(m.status, "size %d %d\n", m.width, m.height); err != nil {
				return m, tea.Quit
			}
		}
		if m.status != nil {
			if err := writeStatus(m.status, "resize-generation %d %d %d\n", m.resizeGeneration, m.width, m.height); err != nil {
				panic(err)
			}
		}
		if changed {
			generation := m.resizeGeneration
			return m, tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return resizeSettled{generation} })
		}
	case resizeSettled:
		if v.Generation != m.resizeGeneration || !m.repairPending {
			return m, nil
		}
		m.repairSettled = true
		return m, m.repairCommand()
	case tea.InlineReplayResult:
		if v.Generation != m.resizeGeneration {
			return m, nil
		}
		m.repairInFlight = false
		if v.Stale {
			return m, nil
		}
		if v.Err != nil {
			m.transcript.stopped = true
			return m, tea.Quit
		}
		m.repairPending = false
		m.repairSettled = false
		m.transcript.paused = false
		if m.status != nil {
			if err := writeStatus(m.status, "replay %d %d %d %d %d %d\n", v.Generation, v.Entries, m.transcript.confirmed, m.transcript.scheduled, m.width, m.height); err != nil {
				panic(err)
			}
		}
		return m, m.transcript.schedule()
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
		if m.repairPending {
			return m, m.repairCommand()
		}
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
		if m.selector.open {
			return m, nil
		}
		m.pastes++
		m.insert(strings.ReplaceAll(v.Content, "\r\n", "\n"))
		m.report()
	case tea.KeyPressMsg:
		if v.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.selector.open {
			switch v.String() {
			case "up":
				m.selector.selected = max(0, m.selector.selected-1)
			case "down":
				m.selector.selected = min(11, m.selector.selected+1)
			case "esc", "enter", "tab":
				m.selector.open = false
			}
			return m, nil
		}
		if v.String() == "tab" {
			m.selector.open = true
			return m, nil
		}
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
	content, x, y := m.liveFrame()
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

func (m *model) repairCommand() tea.Cmd {
	if !m.repairPending || !m.repairSettled || m.repairInFlight || m.transcript.pending || m.transcript.stopped {
		return nil
	}
	m.repairInFlight = true
	entries := append([]string{"T0 startup"}, m.transcript.entries...)
	return tea.ReplayInlineTranscript(m.width, m.height, m.resizeGeneration, entries)
}
