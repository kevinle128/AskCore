package tools

import (
	"strings"
	"unicode/utf8"

	"AskCore/pkg/protocol"
)

// Default output limits of the file and search tools.
const (
	DefaultMaxLines = 2000
	DefaultMaxBytes = 50 * 1024
)

// HeadResult is the outcome of TruncateHead.
type HeadResult struct {
	// Text is the kept head of the input.
	Text string
	// Truncated is true when Text is shorter than the input.
	Truncated bool
	// Truncation holds the line and byte counts of the input and of Text.
	// TruncatedBy is empty when the input was not cut.
	Truncation protocol.ToolTruncation
	// NextOffset is the 1-based line of the input at which the cut text
	// continues. It is zero when the input was not cut. A caller that read the
	// input from line S adds S-1.
	NextOffset int
	// FirstLineExceedsLimit is true when the first line alone is longer than
	// the byte limit. Text then holds a part of that line (no whole line) and
	// the caller decides what to tell the model.
	FirstLineExceedsLimit bool
}

// TruncateHead keeps the first lines of text within maxLines lines and
// maxBytes bytes. It cuts at a line end, so it never keeps a partial line,
// except for a first line that is longer than maxBytes: that line is cut at
// the last rune start inside the limit. A limit of zero or less means the
// default. Line bytes count the "\n" between lines.
func TruncateHead(text string, maxLines, maxBytes int) HeadResult {
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	totalLines := strings.Count(text, "\n") + 1
	info := protocol.ToolTruncation{TotalLines: totalLines, TotalBytes: len(text)}
	if totalLines <= maxLines && len(text) <= maxBytes {
		info.OutputLines, info.OutputBytes = totalLines, len(text)
		return HeadResult{Text: text, Truncation: info}
	}

	// end is the byte length of the kept text: whole lines and the "\n"
	// between them, without a trailing "\n".
	end, kept := 0, 0
	by := "lines"
	for pos := 0; kept < maxLines; {
		lineEnd := len(text)
		if nl := strings.IndexByte(text[pos:], '\n'); nl >= 0 {
			lineEnd = pos + nl
		}
		if lineEnd > maxBytes {
			by = "bytes"
			break
		}
		end, kept = lineEnd, kept+1
		if lineEnd == len(text) {
			break
		}
		pos = lineEnd + 1
	}
	info.TruncatedBy = by
	res := HeadResult{Truncated: true, NextOffset: kept + 1}
	if kept == 0 {
		// The first line is too long. Cut inside it, never inside a rune.
		cut := maxBytes
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		end = cut
		res.FirstLineExceedsLimit = true
	}
	res.Text = text[:end]
	info.OutputLines, info.OutputBytes = kept, end
	res.Truncation = info
	return res
}
