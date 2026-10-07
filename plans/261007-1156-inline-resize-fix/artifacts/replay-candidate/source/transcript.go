package main

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"regexp"
	"strings"
)

type commitWrite struct {
	IDs          []string
	N, Requested int
	Err          error
}
type readyBlock int

var commitIDPattern = regexp.MustCompile(`G1\[[0-9]{4}\]`)

type transcript struct {
	blocks               []string
	ready                []bool
	scheduled, confirmed int
	pending              bool
	stopped              bool
	paused               bool
	entries              []string
	tailRows             int
	holdAt               int
}

func (t *transcript) start(lines int, oversized bool) {
	t.blocks = nil
	t.ready = nil
	t.scheduled = 0
	t.confirmed = 0
	t.pending = false
	t.stopped = false
	t.entries = nil
	t.tailRows = 1
	t.holdAt = 0
	for i := 0; i < lines; i++ {
		t.blocks = append(t.blocks, fmt.Sprintf("G1[%04d]", i))
		t.ready = append(t.ready, true)
	}
	if oversized {
		t.blocks = []string{strings.Join(t.blocks, "\n")}
		t.ready = []bool{true}
	}
}
func (t *transcript) schedule() tea.Cmd {
	if t.paused || t.pending || t.stopped || t.holdAt > 0 && t.scheduled >= t.holdAt || t.scheduled >= len(t.blocks) || !t.ready[t.scheduled] {
		return nil
	}
	body := t.blocks[t.scheduled]
	t.scheduled++
	t.pending = true
	return tea.Println(body)
}
func (t *transcript) observe(w commitWrite) tea.Cmd {
	if w.Err != nil || w.N != w.Requested {
		t.stopped = true
		return nil
	}
	if !t.pending || t.confirmed >= len(t.blocks) {
		return nil
	}
	want := commitIDPattern.FindAllString(t.blocks[t.confirmed], -1)
	if strings.Join(w.IDs, "\n") != strings.Join(want, "\n") {
		t.stopped = true
		return nil
	}
	t.entries = append(t.entries, t.blocks[t.confirmed])
	t.confirmed++
	t.pending = false
	return t.schedule()
}
