// Package sse reads Server-Sent Events from an io.Reader.
//
// The reader accepts the line ends \n, \r\n and bare \r. It keeps limits on
// line size and event size, and it does its work in linear time.
package sse

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"unicode/utf8"
)

const (
	// DefaultMaxBytes is the default limit for one line and for one event.
	DefaultMaxBytes = 32 << 20

	readBufferSize = 32 << 10
	maxRawLines    = 32  // Raw lines kept for each event.
	maxRawLineSize = 512 // Bytes kept for each Raw line.
)

var (
	// ErrLineTooLong means one line is longer than the line limit.
	ErrLineTooLong = errors.New("sse: line too long")
	// ErrEventTooLong means the lines of one event are longer than the event limit.
	ErrEventTooLong = errors.New("sse: event too long")
)

var bom = [3]byte{0xEF, 0xBB, 0xBF}

// Event is one dispatched Server-Sent Event.
type Event struct {
	Name string
	Data string
	// Raw holds the field lines of the event, for adapter error messages.
	// It has at most 32 lines and each line is cut to 512 bytes.
	// Comment lines are not kept.
	Raw []string
}

// Option changes a Reader.
type Option func(*Reader)

// WithMaxLineBytes sets the largest line size. A value below 1 is ignored.
func WithMaxLineBytes(n int) Option {
	return func(r *Reader) {
		if n > 0 {
			r.maxLine = n
		}
	}
}

// WithMaxEventBytes sets the largest total size of the field lines of one
// event. A value below 1 is ignored.
func WithMaxEventBytes(n int) Option {
	return func(r *Reader) {
		if n > 0 {
			r.maxEvent = n
		}
	}
}

// Reader reads events from a stream. It is not safe for concurrent use.
type Reader struct {
	br       *bufio.Reader
	maxLine  int
	maxEvent int

	err       error // Sticky failure, or io.EOF at the end of the stream.
	pendingCR bool  // The last line ended with \r; a following \n belongs to it.
	bomPos    int   // Count of BOM bytes matched and removed; bomDone ends the check.
	bomDone   bool
	line      []byte // Line in progress; it survives a cancelled call.
	ready     bool   // A blank line closed a frame that is not yet dispatched.

	name       string
	data       []byte
	dataLines  int
	raw        []string
	eventBytes int
}

// NewReader returns a Reader for r.
func NewReader(r io.Reader, opts ...Option) *Reader {
	rd := &Reader{
		br:       bufio.NewReaderSize(r, readBufferSize),
		maxLine:  DefaultMaxBytes,
		maxEvent: DefaultMaxBytes,
	}
	for _, o := range opts {
		o(rd)
	}
	return rd
}

// Next returns the next event. It returns io.EOF at the clean end of the
// stream, and a pending event at the end is returned first. After a limit
// error or a read error, every call returns the same error.
//
// Next checks ctx before each read and before each dispatch, also for events
// that are already buffered. A cancelled ctx returns ctx.Err() and keeps the
// reader usable. A cancelled ctx cannot stop a Read that is already blocked,
// and Next starts no goroutine to hide it. The owner of the input must make
// the blocked Read return, for example with a request body bound to ctx, or
// with an io.Pipe that context.AfterFunc closes. If ctx is done when such a
// Read fails, the failure is ctx.Err().
func (r *Reader) Next(ctx context.Context) (Event, error) {
	for {
		cerr := ctx.Err()
		if cerr != nil {
			return Event{}, cerr
		}
		if r.ready {
			r.ready = false
			return r.dispatch(), nil
		}
		if r.err != nil {
			if r.err == io.EOF && r.pending() {
				return r.dispatch(), nil
			}
			return Event{}, r.err
		}
		line, err := r.readLine(ctx)
		if err != nil {
			cerr = ctx.Err()
			switch {
			case cerr != nil && err == cerr:
				return Event{}, err // Cancelled; the reader stays usable.
			case cerr != nil && !isSticky(err):
				r.err = cerr
			default:
				r.err = err
			}
			continue
		}
		if len(line) > 0 {
			r.addLine(line)
		} else if r.pending() {
			// The blank line closes the frame. The next loop turn checks
			// ctx before it dispatches.
			r.ready = true
		} else {
			r.reset()
		}
	}
}

func isSticky(err error) bool {
	return err == io.EOF || err == ErrLineTooLong || err == ErrEventTooLong
}

func (r *Reader) pending() bool {
	return r.name != "" || r.dataLines > 0
}

func (r *Reader) reset() {
	r.name = ""
	r.data = r.data[:0]
	r.dataLines = 0
	r.raw = nil
	r.eventBytes = 0
}

func (r *Reader) dispatch() Event {
	data := r.data
	if r.dataLines > 0 {
		data = data[:len(data)-1] // Remove the last \n.
	}
	ev := Event{Name: r.name, Data: string(data), Raw: r.raw}
	r.reset()
	return ev
}

// addLine applies one non-blank line to the frame in progress.
func (r *Reader) addLine(line []byte) {
	if line[0] == ':' {
		return
	}
	field, value := line, []byte(nil)
	if i := bytes.IndexByte(line, ':'); i >= 0 {
		field, value = line[:i], line[i+1:]
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
	}
	switch string(field) {
	case "event":
		r.name = string(value)
	case "data":
		r.data = append(r.data, value...)
		r.data = append(r.data, '\n')
		r.dataLines++
	default:
		return
	}
	r.eventBytes += len(line)
	if len(r.raw) < maxRawLines {
		r.raw = append(r.raw, truncate(line, maxRawLineSize))
	}
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	b = b[:n]
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b)
}

// readLine returns the next line without its line end. At the end of the
// stream, a final unterminated line is returned first and io.EOF after it.
// The returned slice is valid until the next call.
func (r *Reader) readLine(ctx context.Context) ([]byte, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := r.br.Peek(1); err != nil {
			if err == io.EOF {
				// A partial BOM prefix at the end is ordinary text.
				if !r.bomDone && r.bomPos > 0 {
					r.line = append(r.line, bom[:r.bomPos]...)
				}
				r.bomDone = true
				if len(r.line) > 0 {
					line := r.line
					r.line = r.line[:0]
					r.err = io.EOF
					return line, nil
				}
			}
			return nil, err
		}
		buf, _ := r.br.Peek(r.br.Buffered())

		if r.pendingCR {
			r.pendingCR = false
			if buf[0] == '\n' {
				_, _ = r.br.Discard(1)
				continue
			}
		}
		if !r.bomDone {
			n := 0
			for n < len(buf) && r.bomPos < len(bom) && buf[n] == bom[r.bomPos] {
				n++
				r.bomPos++
			}
			if r.bomPos == len(bom) {
				r.bomDone = true
			} else if n < len(buf) {
				// A mismatch: the matched prefix is text.
				r.line = append(r.line, bom[:r.bomPos]...)
				r.bomDone = true
				if err := r.checkSize(len(r.line)); err != nil {
					return nil, err
				}
			}
			_, _ = r.br.Discard(n)
			continue
		}

		end := -1
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			end = i
			if j := bytes.IndexByte(buf[:i], '\r'); j >= 0 {
				end = j
			}
		} else if j := bytes.IndexByte(buf, '\r'); j >= 0 {
			end = j
		}
		chunk := buf
		if end >= 0 {
			chunk = buf[:end]
		}
		if err := r.checkSize(len(r.line) + len(chunk)); err != nil {
			return nil, err
		}
		if len(r.line) == 0 && end == 0 {
			// A blank line needs no copy.
			r.consumeEnd(buf[0])
			return nil, nil
		}
		r.line = append(r.line, chunk...)
		if end < 0 {
			if err := r.checkLineAgainstEvent(); err != nil {
				return nil, err
			}
			_, _ = r.br.Discard(len(chunk))
			continue
		}
		term := buf[end]
		_, _ = r.br.Discard(end)
		r.consumeEnd(term)
		if err := r.checkLineAgainstEvent(); err != nil {
			return nil, err
		}
		line := r.line
		r.line = r.line[:0]
		return line, nil
	}
}

func (r *Reader) consumeEnd(term byte) {
	_, _ = r.br.Discard(1)
	if term == '\r' {
		r.pendingCR = true
	}
}

// checkSize applies the line limit.
func (r *Reader) checkSize(n int) error {
	if n > r.maxLine {
		r.err = ErrLineTooLong
		return ErrLineTooLong
	}
	return nil
}

// checkLineAgainstEvent applies the event limit to the line in progress.
// Comment lines are not stored, so they do not count.
func (r *Reader) checkLineAgainstEvent() error {
	if len(r.line) == 0 || r.line[0] == ':' {
		return nil
	}
	if r.eventBytes+len(r.line) > r.maxEvent {
		r.err = ErrEventTooLong
		return ErrEventTooLong
	}
	return nil
}
