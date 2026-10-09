package leader

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"AskCore/pkg/protocol"
)

// MaxLine is the longest line on the agent link. It is the socket frame limit
// plus room for the route data that the router adds to a forwarded message.
const MaxLine = protocol.LeaderMaxFrame + 1<<20

var (
	// ErrFrameTooLarge means an encoded frame is above the limit.
	ErrFrameTooLarge = errors.New("leader frame too large")
	// ErrFrameInvalid means a frame is not a valid leader envelope.
	ErrFrameInvalid = errors.New("leader frame invalid")
	// ErrLineTooLong means a line on the agent link is above the limit.
	ErrLineTooLong = errors.New("leader link line too long")
)

var frameTypes = map[string]bool{
	protocol.LeaderFrameRegister:     true,
	protocol.LeaderFrameRegistered:   true,
	protocol.LeaderFrameReady:        true,
	protocol.LeaderFrameACP:          true,
	protocol.LeaderFrameControl:      true,
	protocol.LeaderFrameControlReply: true,
	protocol.LeaderFramePing:         true,
	protocol.LeaderFramePong:         true,
	protocol.LeaderFrameDisconnect:   true,
	protocol.LeaderFrameError:        true,
}

// FrameReader reads length-prefixed frames. One goroutine must own it. A read
// error ends the reader, so parsing never restarts inside a frame.
type FrameReader struct {
	r   io.Reader
	max int
	err error
}

// NewFrameReader returns a reader that accepts frames up to max bytes.
func NewFrameReader(r io.Reader, max int) *FrameReader { return &FrameReader{r: r, max: max} }

// Next returns the next frame.
func (fr *FrameReader) Next() (protocol.LeaderFrame, error) {
	if fr.err != nil {
		return protocol.LeaderFrame{}, fr.err
	}
	f, err := fr.next()
	if err != nil {
		fr.err = err
	}
	return f, err
}

func (fr *FrameReader) next() (protocol.LeaderFrame, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(fr.r, hdr[:]); err != nil {
		return protocol.LeaderFrame{}, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 {
		return protocol.LeaderFrame{}, fmt.Errorf("%w: empty frame", ErrFrameInvalid)
	}
	if uint64(n) > uint64(fr.max) {
		return protocol.LeaderFrame{}, fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, n)
	}
	// ReadAll grows with the bytes that arrive. A peer that claims a large frame
	// and sends nothing cannot make the leader allocate the claimed size.
	body, err := io.ReadAll(io.LimitReader(fr.r, int64(n)))
	if err != nil {
		return protocol.LeaderFrame{}, err
	}
	if len(body) != int(n) {
		return protocol.LeaderFrame{}, io.ErrUnexpectedEOF
	}
	return decodeFrame(body)
}

func decodeFrame(body []byte) (protocol.LeaderFrame, error) {
	var f protocol.LeaderFrame
	if err := json.Unmarshal(body, &f); err != nil {
		return protocol.LeaderFrame{}, fmt.Errorf("%w: %v", ErrFrameInvalid, err)
	}
	if !frameTypes[f.Type] {
		return protocol.LeaderFrame{}, fmt.Errorf("%w: unknown type", ErrFrameInvalid)
	}
	if f.Type == protocol.LeaderFrameACP {
		// One frame carries one JSON-RPC message. A batch or a scalar is refused.
		p := bytes.TrimLeft(f.Payload, " \t\r\n")
		if len(p) == 0 || p[0] != '{' {
			return protocol.LeaderFrame{}, fmt.Errorf("%w: acp payload is not one message", ErrFrameInvalid)
		}
	}
	return f, nil
}

// FrameWriter writes length-prefixed frames. The caller must keep one writer
// for each connection so that frames do not interleave.
type FrameWriter struct {
	w   io.Writer
	max int
}

// NewFrameWriter returns a writer that refuses frames above max bytes.
func NewFrameWriter(w io.Writer, max int) *FrameWriter { return &FrameWriter{w: w, max: max} }

// Write encodes f and writes it in one call. It refuses a frame above the
// limit before it writes any byte.
func (fw *FrameWriter) Write(f protocol.LeaderFrame) error {
	if !frameTypes[f.Type] {
		return fmt.Errorf("%w: unknown type", ErrFrameInvalid)
	}
	typ, err := json.Marshal(f.Type)
	if err != nil {
		return err
	}
	var payload bytes.Buffer
	if len(f.Payload) > 0 {
		// Compact keeps "<", ">" and "&" literal, so escaping cannot grow the frame.
		if err := json.Compact(&payload, f.Payload); err != nil {
			return fmt.Errorf("%w: %v", ErrFrameInvalid, err)
		}
	}
	size := len(`{"type":}`) + len(typ)
	if payload.Len() > 0 {
		size += len(`,"payload":`) + payload.Len()
	}
	if size > fw.max {
		return fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, size)
	}
	buf := make([]byte, 4, 4+size)
	binary.BigEndian.PutUint32(buf, uint32(size))
	buf = append(buf, `{"type":`...)
	buf = append(buf, typ...)
	if payload.Len() > 0 {
		buf = append(buf, `,"payload":`...)
		buf = append(buf, payload.Bytes()...)
	}
	buf = append(buf, '}')
	return writeAll(fw.w, buf)
}

func writeAll(w io.Writer, p []byte) error {
	n, err := w.Write(p)
	if err == nil && n < len(p) {
		return io.ErrShortWrite
	}
	return err
}

// LineReader reads newline-delimited JSON from the agent link.
type LineReader struct {
	r     *bufio.Reader
	max   int
	err   error
	carry []byte
}

// NewLineReader returns a reader for lines up to max bytes, not counting the newline.
func NewLineReader(r io.Reader, max int) *LineReader {
	return &LineReader{r: bufio.NewReaderSize(r, 64<<10), max: max}
}

// Next returns the next non-empty line without its newline. A final line
// without a newline is returned. An error ends the reader.
func (lr *LineReader) Next() ([]byte, error) {
	if lr.err != nil {
		return nil, lr.err
	}
	for {
		line, err := lr.readLine()
		if err != nil {
			lr.err = err
			return nil, err
		}
		if len(bytes.TrimSpace(line)) > 0 {
			// The caller may keep the line. It must not share the reuse buffer.
			return bytes.Clone(line), nil
		}
	}
}

func (lr *LineReader) readLine() ([]byte, error) {
	lr.carry = lr.carry[:0]
	for {
		chunk, err := lr.r.ReadSlice('\n')
		lr.carry = append(lr.carry, chunk...)
		content := len(lr.carry)
		if err == nil {
			content-- // the newline does not count
		}
		if content > lr.max {
			return nil, ErrLineTooLong
		}
		switch {
		case err == nil:
			return lr.carry[:content], nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(lr.carry) > 0:
			return lr.carry, nil
		default:
			return nil, err
		}
	}
}

// LineWriter writes one JSON message for each line to the agent link.
type LineWriter struct {
	w   io.Writer
	max int
}

// NewLineWriter returns a writer that refuses lines above max bytes.
func NewLineWriter(w io.Writer, max int) *LineWriter { return &LineWriter{w: w, max: max} }

// Write compacts msg to one physical line and writes it with its newline in
// one call. A message with line breaks must never become several messages.
func (lw *LineWriter) Write(msg json.RawMessage) error {
	var line bytes.Buffer
	if err := json.Compact(&line, msg); err != nil {
		return fmt.Errorf("%w: %v", ErrFrameInvalid, err)
	}
	if line.Len() > lw.max {
		return ErrLineTooLong
	}
	line.WriteByte('\n')
	return writeAll(lw.w, line.Bytes())
}
