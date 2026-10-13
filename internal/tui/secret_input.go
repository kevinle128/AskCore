package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Secret input.
//
// An API key, a code or a pasted callback address lives only in this buffer.
// It never goes to the editor, the prompt history, the undo or kill ring, a
// transcript block, a note, a notice, the title, the bell or the clipboard.
// The screen shows the fixed text "(hidden)", never the text or its length.
// The buffer is zeroed and dropped when the box sends, cancels, is refused or
// closes.

const (
	// secretLimit is the largest secret in bytes. The leader has its own limit;
	// a text above this one is not a key.
	secretLimit = 16 << 10

	secretHiddenText     = "(hidden)"
	secretInnerBreakText = "The text has a line break or space inside. Paste it again."
	secretTooLongText    = "That is too long to be a key."
)

type secretInput struct {
	buf []byte
	// refusal is the row that tells why the last paste was refused. It never
	// repeats the paste.
	refusal string
}

func (in *secretInput) empty() bool { return len(in.buf) == 0 }

// clear zeroes the buffer and drops it.
func (in *secretInput) clear() {
	for i := range in.buf {
		in.buf[i] = 0
	}
	in.buf = nil
}

// reset clears the buffer and the refusal.
func (in *secretInput) reset() {
	in.clear()
	in.refusal = ""
}

// typed appends the text of a key press. Control characters and spaces are
// dropped: a key, a code and an address have none.
func (in *secretInput) typed(text string) {
	in.refusal = ""
	for _, r := range text {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			continue
		}
		if len(in.buf)+utf8.RuneLen(r) > secretLimit {
			in.clear()
			in.refusal = secretTooLongText
			return
		}
		in.buf = utf8.AppendRune(in.buf, r)
	}
}

// paste inserts a paste as one insertion. A line break or a space inside, or a
// text above the limit, refuses the paste, clears the buffer and sets the
// refusal row. It reports whether the paste was taken.
func (in *secretInput) paste(text string) bool {
	in.refusal = ""
	text = strings.TrimRight(text, "\r\n ")
	switch {
	case len(in.buf)+len(text) > secretLimit:
		in.clear()
		in.refusal = secretTooLongText
		return false
	case strings.ContainsAny(text, "\r\n \t") || strings.ContainsFunc(text, unicode.IsControl):
		in.clear()
		in.refusal = secretInnerBreakText
		return false
	}
	in.buf = append(in.buf, text...)
	return true
}

// backspace removes the last character.
func (in *secretInput) backspace() {
	in.refusal = ""
	if len(in.buf) == 0 {
		return
	}
	_, n := utf8.DecodeLastRune(in.buf)
	for i := len(in.buf) - n; i < len(in.buf); i++ {
		in.buf[i] = 0
	}
	in.buf = in.buf[:len(in.buf)-n]
}

// take returns the secret for one call and clears the buffer.
func (in *secretInput) take() string {
	out := string(in.buf)
	in.clear()
	return out
}
