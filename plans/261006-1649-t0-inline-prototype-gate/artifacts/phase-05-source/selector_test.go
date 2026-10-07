package main

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"
)

func TestSelectorRouting(t *testing.T) {
	m := model{g6: true, width: 40, height: 12, text: "draft", cursor: 2}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = next.(model)
	if !strings.Contains(m.View().Content, "item 01") {
		t.Fatalf("selector did not open: %q", m.View().Content)
	}
	for i := 0; i < 11; i++ {
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		m = next.(model)
	}
	if !strings.Contains(m.View().Content, "item 12") || strings.Contains(m.View().Content, "item 01") {
		t.Fatalf("invalid item window: %q", m.View().Content)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(model)
	if m.text != "draft" || m.cursor != 2 || strings.Contains(m.View().Content, "item ") {
		t.Fatalf("draft/focus not restored: %+v", m)
	}
}
func TestLayoutBoundedLiveRegion(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {2, 1}, {10, 2}, {40, 3}} {
		m := model{g1: true, g6: true, width: size[0], height: size[1], text: "a\nb\nc", cursor: 5, transcript: transcript{tailRows: 3}}
		v := m.View()
		if strings.Count(v.Content, "\n")+1 > size[1] {
			t.Fatalf("live region exceeds %v: %q", size, v.Content)
		}
	}
}

func TestSelectorBoundaryGeometry(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {2, 1}, {10, 2}, {40, 12}} {
		for _, selected := range []int{0, 5, 11} {
			m := model{width: size[0], height: size[1], selector: selector{open: true, selected: selected}}
			v := m.View()
			if strings.Count(v.Content, "\n")+1 > min(3, size[1]) || v.Cursor.X < 0 || v.Cursor.X >= size[0] || v.Cursor.Y < 0 || v.Cursor.Y >= min(3, size[1]) {
				t.Fatalf("invalid selector geometry size=%v selected=%d view=%+v", size, selected, v)
			}
		}
	}
}
