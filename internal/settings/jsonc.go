package settings

import (
	"bytes"
	"fmt"
)

// stripJSONC returns a copy of data in which each `//` comment, each `/* */`
// comment and each trailing comma before `}` or `]` is replaced with spaces.
// Text inside strings does not change. Newlines stay, so a byte offset in the
// result is the same offset in data, and a line number of a later JSON error
// is the line number in the file. An unterminated block comment is an error
// that gives its line number and no content.
func stripJSONC(data []byte) ([]byte, error) {
	out := make([]byte, len(data))
	copy(out, data)
	inString, escaped := false, false
	pendingComma := -1
	for i := 0; i < len(out); i++ {
		c := out[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch {
		case c == '/' && i+1 < len(out) && out[i+1] == '/':
			for i < len(out) && out[i] != '\n' && out[i] != '\r' {
				out[i] = ' '
				i++
			}
			i-- // The loop increment moves past the newline.
		case c == '/' && i+1 < len(out) && out[i+1] == '*':
			end := bytes.Index(out[i+2:], []byte("*/"))
			if end < 0 {
				return nil, fmt.Errorf("unterminated comment at line %d", lineAt(data, i))
			}
			last := i + 2 + end + 1
			for ; i <= last; i++ {
				if out[i] != '\n' && out[i] != '\r' {
					out[i] = ' '
				}
			}
			i--
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
		case c == ',':
			pendingComma = i
		case c == '}' || c == ']':
			if pendingComma >= 0 {
				out[pendingComma] = ' '
			}
			pendingComma = -1
		default:
			pendingComma = -1
			if c == '"' {
				inString = true
			}
		}
	}
	return out, nil
}

// lineAt returns the 1-based line number of the byte at offset in data.
func lineAt(data []byte, offset int) int {
	if offset > len(data) {
		offset = len(data)
	}
	if offset < 0 {
		offset = 0
	}
	return bytes.Count(data[:offset], []byte("\n")) + 1
}
