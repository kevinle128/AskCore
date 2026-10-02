package partialjson

import (
	"strconv"
	"strings"
)

// Repair fixes two common faults inside JSON string literals:
//   - a raw control character (U+0000 to U+001F) becomes its escape;
//   - a backslash before an invalid escape character is doubled.
//
// A backslash at the end of the text is doubled. A "\u" followed by four hex
// digits is copied unchanged. Text outside string literals is copied unchanged.
// Repair does not make the text valid JSON in all cases. Callers must parse the
// result again.
func Repair(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	inString := false

	for i := 0; i < len(s); i++ {
		c := s[i]

		if !inString {
			b.WriteByte(c)
			if c == '"' {
				inString = true
			}
			continue
		}

		switch {
		case c == '"':
			b.WriteByte(c)
			inString = false
		case c == '\\':
			if i+1 >= len(s) {
				b.WriteString(`\\`)
				continue
			}
			next := s[i+1]
			if next == 'u' && i+6 <= len(s) && isHex4(s[i+2:i+6]) {
				b.WriteString(s[i : i+6])
				i += 5
				continue
			}
			if isValidEscape(next) {
				b.WriteByte(c)
				b.WriteByte(next)
				i++
				continue
			}
			// The next byte is handled in the next iteration.
			b.WriteString(`\\`)
		case c < 0x20:
			writeControlEscape(&b, c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func isValidEscape(c byte) bool {
	switch c {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't', 'u':
		return true
	}
	return false
}

func isHex4(s string) bool {
	if len(s) != 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func writeControlEscape(b *strings.Builder, c byte) {
	switch c {
	case '\b':
		b.WriteString(`\b`)
	case '\f':
		b.WriteString(`\f`)
	case '\n':
		b.WriteString(`\n`)
	case '\r':
		b.WriteString(`\r`)
	case '\t':
		b.WriteString(`\t`)
	default:
		h := strconv.FormatUint(uint64(c), 16)
		b.WriteString(`\u`)
		b.WriteString(strings.Repeat("0", 4-len(h)))
		b.WriteString(h)
	}
}
