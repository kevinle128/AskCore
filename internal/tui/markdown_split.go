package tui

import (
	"regexp"
	"strings"
)

// Markdown chunks.
//
// A reply is cut into chunks that can be drawn one by one. A chunk is a fenced
// code block, or the prose between two blank lines. Blank lines that belong to
// a construct do not cut it: a fenced block, a loose list with its indented
// paragraphs, a block quote, an indented code block and an open HTML comment
// stay in one chunk. A cut that is too small would change how the document
// looks, so the scanner prefers to keep text together when in doubt.
//
// A chunk is closed when no later text can change it: a code block after its
// closing fence line is complete, and a prose chunk after the first complete
// line of the chunk that follows. The last chunk is always open while the text streams.

type mdChunk struct {
	// body is the text of the chunk. For a code chunk it is the code between
	// the fences, without the line break after the last line.
	body string
	// code marks a fenced code block; lang is the first word of its info string.
	code bool
	lang string
	// closed means that more text cannot change this chunk.
	closed bool
	// start and end are byte offsets in the split text. src[start:end] is the
	// source of the chunk, fences included, without the blank lines around it.
	start, end int
}

type fenceState struct {
	char byte
	n    int
}

// chunkScan is the chunk that the scanner builds.
type chunkScan struct {
	lines    []string
	code     bool
	lang     string
	fence    fenceState
	inFence  bool
	hasList  bool
	hasQuote bool
	endsNL   bool // the last line had a line break
	start    int
	end      int
}

var listMarker = regexp.MustCompile(`^ {0,3}(?:[-*+]|\d{1,9}[.)])(?: |\t|$)`)

// splitMarkdown cuts text into chunks. Line breaks are "\n"; callers clean the
// text first.
func splitMarkdown(src string) []mdChunk {
	parts := strings.Split(src, "\n")
	type line struct {
		text     string
		complete bool
	}
	var lines []line
	for i, p := range parts {
		switch {
		case i < len(parts)-1:
			lines = append(lines, line{p, true})
		case p != "":
			lines = append(lines, line{p, false})
		}
	}

	var (
		chunks  []mdChunk
		cur     *chunkScan
		blanks  int
		off     int // byte offset of the start of the current line
		flushed = func(c *chunkScan, closed bool) {
			chunks = append(chunks, c.chunk(closed))
		}
	)
	for _, ln := range lines {
		text := ln.text
		lineStart := off
		off += len(text)
		if ln.complete {
			off++
		}
		lineEnd := off
		if cur != nil && cur.inFence {
			// A closing fence at the end of the text counts as a closer even
			// without a line break, so a finished reply never shows it as code.
			// The chunk is closed only when the line is complete.
			if isFenceCloser(text, cur.fence) {
				cur.inFence = false
				cur.end = lineEnd
				if cur.code {
					flushed(cur, ln.complete)
					cur = nil
					blanks = 0
					continue
				}
			}
			cur.add(text, ln.complete, lineEnd)
			continue
		}
		if strings.TrimSpace(text) == "" {
			blanks++
			continue
		}
		if fence, lang, ok := parseFenceOpen(text, cur != nil && cur.hasList); ok {
			nested := cur != nil && cur.hasList && leadingSpaces(text) >= 2
			if nested {
				cur.addBlanks(blanks)
				blanks = 0
				cur.add(text, ln.complete, lineEnd)
				cur.fence, cur.inFence = fence, true
				continue
			}
			if cur != nil {
				flushed(cur, ln.complete)
			}
			cur = &chunkScan{code: true, lang: lang, fence: fence, inFence: true, endsNL: true, start: lineStart, end: lineEnd}
			blanks = 0
			continue
		}
		switch {
		case cur == nil:
			cur = &chunkScan{start: lineStart}
		case blanks > 0 && cur.mergesWith(text):
			cur.addBlanks(blanks)
		case blanks > 0:
			flushed(cur, ln.complete)
			cur = &chunkScan{start: lineStart}
		}
		blanks = 0
		cur.add(text, ln.complete, lineEnd)
	}
	if cur != nil {
		flushed(cur, false)
	}
	return chunks
}

func (c *chunkScan) add(text string, complete bool, end int) {
	c.lines = append(c.lines, text)
	c.endsNL = complete
	c.end = end
	if !c.code {
		if listMarker.MatchString(text) {
			c.hasList = true
		}
		if strings.HasPrefix(strings.TrimLeft(text, " "), ">") {
			c.hasQuote = true
		}
	}
}

func (c *chunkScan) addBlanks(n int) {
	for i := 0; i < n; i++ {
		c.lines = append(c.lines, "")
	}
}

func (c *chunkScan) chunk(closed bool) mdChunk {
	body := strings.Join(c.lines, "\n")
	if c.code {
		return mdChunk{body: body, code: true, lang: c.lang, closed: closed, start: c.start, end: c.end}
	}
	if c.endsNL {
		body += "\n"
	}
	return mdChunk{body: body, closed: closed, start: c.start, end: c.end}
}

var rawHTMLEnd = map[string]string{
	"<pre": "</pre>", "<script": "</script>", "<style": "</style>", "<textarea": "</textarea>", "<!--": "-->",
}

// mergesWith reports whether a prose line after blank lines continues this
// chunk instead of starting a new one.
func (c *chunkScan) mergesWith(next string) bool {
	first := c.lines[0]
	lower := strings.ToLower(strings.TrimLeft(first, " "))
	for start, end := range rawHTMLEnd {
		if strings.HasPrefix(lower, start) && !strings.Contains(strings.ToLower(strings.Join(c.lines, "\n")), end) {
			return true
		}
	}
	switch {
	case c.hasList:
		return listMarker.MatchString(next) || leadingSpaces(next) >= 2
	case c.hasQuote:
		return strings.HasPrefix(strings.TrimLeft(next, " "), ">")
	case leadingSpaces(first) >= 4:
		return leadingSpaces(next) >= 4
	}
	return false
}

func leadingSpaces(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

// parseFenceOpen reads an opening code fence. A fence indented four columns or
// more is an indented code block, except inside a list item.
func parseFenceOpen(text string, inList bool) (fenceState, string, bool) {
	if leadingSpaces(text) >= 4 && !inList {
		return fenceState{}, "", false
	}
	t := strings.TrimLeft(text, " ")
	if t == "" || (t[0] != '`' && t[0] != '~') {
		return fenceState{}, "", false
	}
	char := t[0]
	n := 0
	for n < len(t) && t[n] == char {
		n++
	}
	if n < 3 {
		return fenceState{}, "", false
	}
	info := strings.TrimSpace(t[n:])
	if char == '`' && strings.Contains(info, "`") {
		return fenceState{}, "", false
	}
	lang := ""
	if f := strings.Fields(info); len(f) > 0 {
		lang = f[0]
	}
	return fenceState{char: char, n: n}, lang, true
}

// isFenceCloser reports whether a complete line closes the open fence.
func isFenceCloser(text string, f fenceState) bool {
	t := strings.TrimLeft(text, " ")
	n := 0
	for n < len(t) && t[n] == f.char {
		n++
	}
	return n >= f.n && strings.TrimSpace(t[n:]) == ""
}
