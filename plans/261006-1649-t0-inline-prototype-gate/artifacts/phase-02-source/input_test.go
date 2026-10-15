package main

import (
	tea "charm.land/bubbletea/v2"
	"testing"
)

func TestInputPaste(t *testing.T) {
	m := model{}
	updated, _ := m.Update(tea.PasteMsg{Content: "a\r\nb\t界😀\x1b[13;2u"})
	got := updated.(model).text
	if got != "a\nb\t界😀\x1b[13;2u" {
		t.Fatalf("draft=%q want normalized intact paste", got)
	}
}
func TestInputAltEnter(t *testing.T) {
	m := model{text: "first", cursor: 5}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	if got := updated.(model).text; got != "first\n" {
		t.Fatalf("Alt+Enter draft=%q want first newline", got)
	}
}
