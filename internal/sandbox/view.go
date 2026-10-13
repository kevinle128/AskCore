package sandbox

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// DefaultMaxLines and DefaultMaxBytes are the limits of the model view.
	DefaultMaxLines = 2000
	DefaultMaxBytes = 50 * 1024
)

// ansiPattern matches CSI and OSC sequences, other two-byte escapes, and an
// escape sequence that the cut of the buffer left incomplete at the end.
var ansiPattern = regexp.MustCompile(
	`\x1b\[[0-?]*[ -/]*[@-~]` +
		`|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)` +
		`|\x1b[@-Z\\-_]` +
		`|\x1b(?:\[[0-?]*[ -/]*|\][^\x07\x1b]*)?$`)

// Truncation describes what View left out of the text.
type Truncation struct {
	// Truncated is true when the text does not hold the whole output.
	Truncated bool
	// ElidedBytes counts the bytes that are in the output and not in the text.
	// It is measured before ANSI and carriage return cleanup of the kept parts.
	ElidedBytes int64
	// TotalBytes is the number of bytes the process wrote.
	TotalBytes int64
	// ShownLines and ShownBytes measure the kept head and tail.
	ShownLines int
	ShownBytes int
	// SpillPath and SpillCapped repeat the fields of Output.
	SpillPath   string
	SpillCapped bool
}

// View builds the text that the model reads from out: the head, a marker with
// the count of elided bytes, the tail, and the spill file lines. The head
// keeps two fifths of maxLines and maxBytes and the tail three fifths, which
// is 20 KB and 30 KB at the default limits. It strips ANSI sequences, keeps
// the text after the last carriage return of each line, and never splits a
// UTF-8 rune. A limit of zero or less selects the default.
func View(out Output, maxLines, maxBytes int) (string, Truncation) {
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	headLines, headBytes := max(maxLines*2/5, 1), max(maxBytes*2/5, 1)
	tailLines, tailBytes := max(maxLines-headLines, 1), max(maxBytes-headBytes, 1)

	tr := Truncation{
		TotalBytes:  out.Total,
		SpillPath:   out.SpillPath,
		SpillCapped: out.SpillCapped,
	}
	var text string
	if out.Dropped == 0 {
		full := sanitize(string(out.Head) + string(out.Tail))
		if countLines(full) <= maxLines && len(full) <= maxBytes {
			text = full
			tr.ShownLines, tr.ShownBytes = countLines(full), len(full)
		} else {
			head := cutHead(full, headLines, headBytes)
			tail := cutTail(full[len(head):], tailLines, tailBytes)
			tr.ElidedBytes = int64(len(full) - len(head) - len(tail))
			text = joinElided(head, tail, tr.ElidedBytes)
			tr.ShownLines, tr.ShownBytes = countLines(head)+countLines(tail), len(head)+len(tail)
		}
	} else {
		headRaw, tailRaw := string(out.Head), string(out.Tail)
		lost := out.Dropped
		trimmedHead := trimIncompleteRuneEnd(headRaw)
		trimmedTail := trimContinuationStart(tailRaw)
		lost += int64(len(headRaw)-len(trimmedHead)) + int64(len(tailRaw)-len(trimmedTail))
		headClean, tailClean := sanitize(trimmedHead), sanitize(trimmedTail)
		head := cutHead(headClean, headLines, headBytes)
		tail := cutTail(tailClean, tailLines, tailBytes)
		lost += int64(len(headClean)-len(head)) + int64(len(tailClean)-len(tail))
		tr.ElidedBytes = lost
		text = joinElided(head, tail, lost)
		tr.ShownLines, tr.ShownBytes = countLines(head)+countLines(tail), len(head)+len(tail)
	}
	tr.Truncated = tr.ElidedBytes > 0

	if out.SpillPath != "" {
		text = endLine(text) + "[full output: " + out.SpillPath + "]"
		if out.SpillCapped {
			text += "\n[spill file capped at 64 MB]"
		}
	}
	return text, tr
}

func joinElided(head, tail string, elided int64) string {
	return head + fmt.Sprintf("\n[... %d bytes elided ...]\n", elided) + tail
}

// endLine returns s with a final newline, unless s is empty.
func endLine(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

// sanitize strips escape sequences and, for each line, keeps only the text
// after the last carriage return that has text behind it. A CRLF line end
// therefore loses only its CR, and a progress line shows its last state.
func sanitize(s string) string {
	s = ansiPattern.ReplaceAllString(s, "")
	if !strings.Contains(s, "\r") {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = lastSegment(line)
	}
	return strings.Join(lines, "\n")
}

func lastSegment(line string) string {
	if !strings.Contains(line, "\r") {
		return line
	}
	parts := strings.Split(line, "\r")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			return parts[i]
		}
	}
	return ""
}

// countLines counts text lines; a final newline does not start a new line.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// cutHead keeps the first maxLines lines and at most maxBytes bytes of s, and
// ends at a rune boundary.
func cutHead(s string, maxLines, maxBytes int) string {
	end := len(s)
	if end > maxBytes {
		end = maxBytes
		for end > 0 && !utf8.RuneStart(s[end]) {
			end--
		}
	}
	pos := 0
	for range maxLines {
		i := strings.IndexByte(s[pos:end], '\n')
		if i < 0 {
			return s[:end]
		}
		pos += i + 1
	}
	return s[:pos]
}

// cutTail keeps the last maxLines lines and at most maxBytes bytes of s, and
// starts at a rune boundary.
func cutTail(s string, maxLines, maxBytes int) string {
	start := 0
	if len(s) > maxBytes {
		start = len(s) - maxBytes
		for start < len(s) && !utf8.RuneStart(s[start]) {
			start++
		}
	}
	end := len(s)
	if end > 0 && s[end-1] == '\n' {
		end--
	}
	seen := 0
	for i := end - 1; i >= start; i-- {
		if s[i] != '\n' {
			continue
		}
		seen++
		if seen == maxLines {
			start = i + 1
			break
		}
	}
	return s[start:]
}

// trimIncompleteRuneEnd removes a rune that the buffer cut in two at the end
// of s.
func trimIncompleteRuneEnd(s string) string {
	for i := len(s) - 1; i >= 0 && i >= len(s)-utf8.UTFMax; i-- {
		if !utf8.RuneStart(s[i]) {
			continue
		}
		if !utf8.FullRuneInString(s[i:]) {
			return s[:i]
		}
		break
	}
	return s
}

// trimContinuationStart removes the tail of a rune that the buffer cut in two
// at the start of s.
func trimContinuationStart(s string) string {
	i := 0
	for i < len(s) && i < utf8.UTFMax && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}
