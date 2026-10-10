package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Paste handling. A paste is one insertion. A paste of more than
// pasteMarkerLines lines shows a short marker in the draft and the full text
// goes out when the prompt is sent.

const pasteMarkerLines = 10

var markerPattern = regexp.MustCompile(`\[paste #(\d+), (\d+) lines\]`)

// pasteStore keeps the full text of every large paste of the session, so that
// undo and history can bring a marker back.
type pasteStore struct {
	next  int
	texts map[int]string
}

func (p *pasteStore) add(text string) int {
	if p.texts == nil {
		p.texts = make(map[int]string)
	}
	p.next++
	p.texts[p.next] = text
	return p.next
}

func lineCount(s string) int { return strings.Count(s, "\n") + 1 }

// markerSpan is the byte range of a paste marker in the draft.
type markerSpan struct{ start, end, id int }

// markerSpans finds the markers that the editor itself created. Text that only
// looks like a marker is ordinary text.
func (e *Editor) markerSpans() []markerSpan {
	if len(e.pastes.texts) == 0 {
		return nil
	}
	var spans []markerSpan
	for _, m := range markerPattern.FindAllStringSubmatchIndex(e.text, -1) {
		id, _ := strconv.Atoi(e.text[m[2]:m[3]])
		lines, _ := strconv.Atoi(e.text[m[4]:m[5]])
		full, ok := e.pastes.texts[id]
		if ok && lineCount(full) == lines {
			spans = append(spans, markerSpan{start: m[0], end: m[1], id: id})
		}
	}
	return spans
}

func (e *Editor) snapToMarkerStart(pos int) int {
	for _, sp := range e.markerSpans() {
		if pos > sp.start && pos < sp.end {
			return sp.start
		}
	}
	return pos
}

func (e *Editor) snapToMarkerEnd(pos int) int {
	for _, sp := range e.markerSpans() {
		if pos > sp.start && pos < sp.end {
			return sp.end
		}
	}
	return pos
}

// expand replaces the markers of text with the pasted text.
func (e *Editor) expand(text string) string {
	spans := e.markerSpans()
	if len(spans) == 0 {
		return text
	}
	// The spans belong to e.text; expand the draft only.
	var b strings.Builder
	prev := 0
	for _, sp := range spans {
		b.WriteString(text[prev:sp.start])
		b.WriteString(e.pastes.texts[sp.id])
		prev = sp.end
	}
	b.WriteString(text[prev:])
	return b.String()
}

// cleanInput makes pasted or typed text safe for the draft. Line ends become
// line feeds. Tabs and line feeds stay; every other control character is
// dropped so that pasted escape sequences never reach the agent or the screen.
func cleanInput(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			return -1
		}
		return r
	}, s)
}

// paste inserts pasted text as one edit.
func (e *Editor) paste(content string) {
	text := cleanInput(content)
	if text == "" {
		return
	}
	if lineCount(text) > pasteMarkerLines {
		id := e.pastes.add(text)
		text = fmt.Sprintf("[paste #%d, %d lines]", id, lineCount(text))
	}
	e.insert(opOther, text)
}
