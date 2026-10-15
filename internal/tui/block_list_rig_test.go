package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// listModelAt returns a model that has a session view and the compact session
// of the fold tests, at a size and with a color profile. No program runs: the
// tests call Update and read View.
func listModelAt(t *testing.T, p colorprofile.Profile, width, height int, events ...protocol.Event) *Model {
	t.Helper()
	m, _ := listModelWithTranslator(t, p, width, height, events...)
	return m
}

// listModelWithTranslator is listModelAt that also returns the translator, so
// a test can play more events into the same transcript.
func listModelWithTranslator(t *testing.T, p colorprofile.Profile, width, height int, events ...protocol.Event) (*Model, *Translator) {
	t.Helper()
	m := New(Options{Client: newFakeClient(), Profile: p, Location: time.UTC, TickInterval: -1})
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	trl := NewTranslator(TranslatorOptions{Location: time.UTC})
	if len(events) == 0 {
		events = foldSessionEvents()
	}
	playEvents(m, trl, events...)
	require.NoError(t, m.Err())
	m.View()
	return m, trl
}

// playEvents delivers agent events to the model through the translator and
// draws a frame.
func playEvents(m *Model, trl *Translator, events ...protocol.Event) {
	for _, ev := range events {
		for _, msg := range trl.Event(ev) {
			m.Update(msg)
		}
	}
	m.View()
}

func listModel(t *testing.T, width, height int) *Model {
	t.Helper()
	return listModelAt(t, colorprofile.NoTTY, width, height)
}

// press sends keys by name, as a terminal reports them, and draws a frame
// after each, as the program does.
func press(t *testing.T, m *Model, names ...string) {
	t.Helper()
	for _, n := range names {
		switch n {
		case "pgup":
			m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
		case "pgdown":
			m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		default:
			m.Update(keyNamed(t, n))
		}
		m.View()
	}
}

// typed sends text as typed keys.
func typed(t *testing.T, m *Model, text string) {
	t.Helper()
	for _, c := range text {
		m.Update(tea.KeyPressMsg{Code: c, Text: string(c)})
		m.View()
	}
}

// rowWith returns the plain screen row that holds the text, or fails.
func rowWith(t *testing.T, m *Model, text string) string {
	t.Helper()
	for _, r := range screenOf(m) {
		if strings.Contains(r, text) {
			return r
		}
	}
	t.Fatalf("no row holds %q:\n%s", text, strings.Join(screenOf(m), "\n"))
	return ""
}

func barOf(m *Model) string {
	rows := screenOf(m)
	return rows[len(rows)-1]
}
