package rpc

import (
	"encoding/json"
	"errors"
	"io"
	"sync"

	"AskCore/internal/leader"
	"AskCore/pkg/protocol"
)

// LeaderError is an error frame of the leader. The leader sends it before it
// closes the link, for example when it shuts down.
type LeaderError struct{ Kind, Message string }

func (e *LeaderError) Error() string { return "leader: " + e.Kind + ": " + e.Message }

// LeaderTransport carries the ACP bodies of one leader connection. It answers
// the pings of the leader itself.
type LeaderTransport struct {
	cl  *leader.Client
	wmu sync.Mutex // one writer: complete frames never interleave
}

// NewLeaderTransport wraps a connection that completed the handshake.
func NewLeaderTransport(cl *leader.Client) *LeaderTransport { return &LeaderTransport{cl: cl} }

// Client returns the connection, for its identity.
func (t *LeaderTransport) Client() *leader.Client { return t.cl }

func (t *LeaderTransport) write(f protocol.LeaderFrame) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	return t.cl.Writer.Write(f)
}

// Send writes one ACP body.
func (t *LeaderTransport) Send(raw json.RawMessage) error {
	return t.write(protocol.LeaderFrame{Type: protocol.LeaderFrameACP, Payload: raw})
}

// Next returns the next ACP body. A ping is answered and skipped. An error
// frame ends the link with a *LeaderError. A disconnect frame ends it with io.EOF.
func (t *LeaderTransport) Next() (json.RawMessage, error) {
	for {
		f, err := t.cl.Reader.Next()
		if err != nil {
			return nil, err
		}
		switch f.Type {
		case protocol.LeaderFramePing:
			if err := t.write(protocol.LeaderFrame{Type: protocol.LeaderFramePong}); err != nil {
				return nil, err
			}
		case protocol.LeaderFrameError:
			var e protocol.LeaderError
			_ = json.Unmarshal(f.Payload, &e)
			return nil, &LeaderError{Kind: e.Kind, Message: e.Message}
		case protocol.LeaderFrameDisconnect:
			return nil, io.EOF
		case protocol.LeaderFrameACP:
			return f.Payload, nil
		}
	}
}

// Close sends the disconnect frame, under the writer lock, and closes the
// socket. The leader and the sessions of this client go on.
func (t *LeaderTransport) Close() error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	return t.cl.Close()
}

// LineTransport carries one JSON-RPC body for each line over a byte stream. The
// in-process agent uses it over a pipe.
type LineTransport struct {
	r       *leader.LineReader
	w       *leader.LineWriter
	wmu     sync.Mutex
	closers []io.Closer
	once    sync.Once
}

// NewLineTransport reads lines from r and writes lines to w. Close closes the
// given closers.
func NewLineTransport(r io.Reader, w io.Writer, closers ...io.Closer) *LineTransport {
	return &LineTransport{r: leader.NewLineReader(r, leader.MaxLine), w: leader.NewLineWriter(w, leader.MaxLine), closers: closers}
}

func (t *LineTransport) Send(raw json.RawMessage) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	return t.w.Write(raw)
}

func (t *LineTransport) Next() (json.RawMessage, error) {
	line, err := t.r.Next()
	return json.RawMessage(line), err
}

func (t *LineTransport) Close() error {
	var err error
	t.once.Do(func() {
		var errs []error
		for _, c := range t.closers {
			errs = append(errs, c.Close())
		}
		err = errors.Join(errs...)
	})
	return err
}
