package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	sdk "github.com/coder/acp-go-sdk"
)

// defaultMaxFrame is the byte cap of one inbound frame. A prompt can carry
// base64 images, so the cap is high, but it stays below the SDK scanner cap.
const defaultMaxFrame = 8 << 20

// defaultWriteStall is how long a single output write may run, during
// shutdown, before the server closes the output to interrupt it.
const defaultWriteStall = 2 * time.Second

// StdioConfig holds what the stdio server needs. The server owns In, Out and
// the adapter. It never reads or writes any other stream.
type StdioConfig struct {
	// Adapter configures the session adapter. Its Log is replaced by Diag.
	Adapter Config
	// In is the input of the connection. When it is an io.Closer the server
	// closes it on a failure, so that the reader stops.
	In io.Reader
	// Out is the output of the connection. When it is an io.Closer the server
	// closes it to interrupt a write that cannot finish.
	Out io.Writer
	// Signals delivers the shutdown signals of the process. The first one starts
	// a graceful shutdown. A second one forces the return. Nil means none.
	Signals <-chan os.Signal
	// Cleanup runs the drains that the composition owns, such as the wait for a
	// credential refresh. It runs on every shutdown path, in parallel with the
	// disposal of the host.
	Cleanup func(ctx context.Context) error
	// MaxFrame is the byte cap of one inbound frame. Zero takes the default.
	MaxFrame int
	// WriteStall bounds one output write during shutdown. A peer that stops
	// reading stdout cannot hold the shutdown longer: the server closes the
	// output and the write fails. Zero takes the default.
	WriteStall time.Duration
	// Diag receives the diagnostics of the SDK connection. Nil discards them.
	Diag io.Writer
}

// StdioExit tells how the stdio server ended.
type StdioExit struct {
	// Signal is the signal that ended the server, or nil.
	Signal os.Signal
	// Err is the first failure: a lost output, an oversize frame or a failed
	// disposal. It is nil after a clean end of input.
	Err error
	// Forced is true when a second signal ended the server before the cleanup
	// had drained. The Agent runs may still be active then.
	Forced bool
}

// ServeStdio serves one ACP connection over In and Out until the input ends,
// the output fails, or a signal comes. It then closes admission, disposes the
// host and runs Cleanup, and it returns when they have drained. A prompt result
// never follows a lost output.
func ServeStdio(ctx context.Context, cfg StdioConfig) StdioExit {
	diag := cfg.Diag
	if diag == nil {
		diag = io.Discard
	}
	acfg := cfg.Adapter
	acfg.Log = diag
	a, err := NewAdapter(ctx, acfg)
	if err != nil {
		return StdioExit{Err: err}
	}
	maxFrame := cfg.MaxFrame
	if maxFrame <= 0 {
		maxFrame = defaultMaxFrame
	}

	var inClosed atomic.Bool
	closeIn := func() {
		inClosed.Store(true)
		if c, ok := cfg.In.(io.Closer); ok {
			_ = c.Close()
		}
	}
	pend := &pendingResponses{ids: map[string]struct{}{}}
	reader := &recordReader{r: NewLineLimitReader(cfg.In, maxFrame), ignore: &inClosed, ready: make(chan struct{}), pend: pend, limit: maxFrame}
	out := &gatedOutput{a: a, closed: make(chan struct{})}
	if c, ok := cfg.Out.(io.Closer); ok {
		out.c = c
	}
	cw := NewCheckedWriter(cfg.Out, func(err error) {
		a.Fail(err)
		closeIn()
	})
	h := &tracked{Adapter: a, pend: pend}
	conn := sdk.NewAgentSideConnection(h, cw, reader)
	a.Bind(conn, cw, out)
	// The adapter observes follow results; the tracker sees every response.
	cw.Observe(func(frame []byte) {
		a.frameWritten(frame)
		pend.written(frame)
	})
	// The SDK reads at once, but it sets its logger in Bind. The first read waits
	// for the end of Bind, so a failing input cannot race with the logger.
	close(reader.ready)

	var sig os.Signal
	select {
	case <-conn.Done():
	case <-a.Failed():
	case <-ctx.Done():
	case sig = <-cfg.Signals:
		// A write that blocks cannot be interrupted any other way.
		out.force()
	}
	closeIn()

	stall := cfg.WriteStall
	if stall <= 0 {
		stall = defaultWriteStall
	}
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		tick := time.NewTicker(stall / 4)
		defer tick.Stop()
		for {
			select {
			case <-stopWatch:
				return
			case <-tick.C:
				if cw.Stalled() > stall {
					// The peer does not read: the write ends with an error.
					out.force()
					return
				}
			}
		}
	}()

	done := make(chan error, 1)
	go func() {
		cleaned := make(chan error, 1)
		go func() {
			if cfg.Cleanup == nil {
				cleaned <- nil
				return
			}
			cleaned <- cfg.Cleanup(context.Background())
		}()
		closeErr := a.Close()
		// Handlers that are still active return now: the host is closed. Their
		// responses leave before the output closes. A lost output releases the
		// wait, because no response can follow it.
		pend.closeAndWait(a.Failed(), out.closed)
		out.force()
		done <- errors.Join(closeErr, <-cleaned)
	}()

	exit := StdioExit{Signal: sig}
	select {
	case err := <-done:
		exit.Err = err
	case second := <-cfg.Signals:
		out.force()
		exit.Signal = second
		exit.Forced = true
	}
	exit.Err = errors.Join(a.Failure(), reader.failure(), exit.Err)
	return exit
}

// recordReader keeps the first read error that is not an end of input, so the
// server can tell an oversize frame from a clean end.
type recordReader struct {
	r      io.Reader
	ignore *atomic.Bool
	ready  chan struct{}
	mu     sync.Mutex
	err    error
	// pend gets the ID of each request that waits for an Agent, in input order,
	// before any handler runs.
	pend  *pendingResponses
	limit int
	line  []byte
}

func (r *recordReader) Read(p []byte) (int, error) {
	<-r.ready
	n, err := r.r.Read(p)
	r.tap(p[:n])
	if errors.Is(err, io.EOF) && len(r.line) > 0 {
		r.pend.request(r.line)
		r.line = nil
	}
	if err != nil && !errors.Is(err, io.EOF) && !r.ignore.Load() {
		r.mu.Lock()
		if r.err == nil {
			r.err = err
		}
		r.mu.Unlock()
	}
	return n, err
}

// tap splits the input into lines and registers every JSON-RPC request. A line over the limit is not kept: the line limit reader fails it.
func (r *recordReader) tap(b []byte) {
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			if len(r.line)+len(b) <= r.limit {
				r.line = append(r.line, b...)
			}
			return
		}
		if len(r.line)+i <= r.limit {
			r.line = append(r.line, b[:i]...)
			r.pend.request(r.line)
		}
		r.line = r.line[:0]
		b = b[i+1:]
	}
}

func (r *recordReader) failure() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// gatedOutput closes the output only when the output failed or the server
// forces it. A normal end keeps the output open until the answers of the
// active handlers are written.
type gatedOutput struct {
	a    *Adapter
	c    io.Closer
	once sync.Once
	err  error
	open atomic.Bool
	// closed ends when the output is closed, so no response can follow.
	closed chan struct{}
}

func (g *gatedOutput) Close() error {
	if g.c == nil || (!g.open.Load() && g.a.Failure() == nil) {
		return nil
	}
	g.once.Do(func() {
		g.err = g.c.Close()
		close(g.closed)
	})
	return g.err
}

func (g *gatedOutput) force() {
	g.open.Store(true)
	_ = g.Close()
}

// tracked refuses the requests that wait for an Agent once the server shuts
// down. The server waits for their responses before it closes the output.
type tracked struct {
	*Adapter
	pend *pendingResponses
}

func (t *tracked) Prompt(ctx context.Context, req sdk.PromptRequest) (sdk.PromptResponse, error) {
	if !t.pend.open() {
		return sdk.PromptResponse{}, requestError(ErrHostClosed)
	}
	return t.Adapter.Prompt(ctx, req)
}

func (t *tracked) HandleExtensionMethod(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	if !t.pend.open() {
		return nil, requestError(ErrHostClosed)
	}
	return t.Adapter.HandleExtensionMethod(ctx, method, raw)
}

// pendingResponses holds the IDs of the requests that wait for an Agent until
// their response frame is written. The SDK writes a response after its handler
// returns, so a handler count is not enough to know that the response left.
type pendingResponses struct {
	mu     sync.Mutex
	ids    map[string]struct{}
	closed bool
	idle   chan struct{}
}

// request registers requests that the SDK answers. A null ID is a notification
// in the SDK, so it must not add a response wait.
func (p *pendingResponses) request(line []byte) {
	// Match the SDK's complete envelope types. A frame rejected by its decoder
	// cannot produce a response, so it must not hold the shutdown barrier.
	var head struct {
		JSONRPC string            `json:"jsonrpc"`
		ID      *json.RawMessage  `json:"id"`
		Method  string            `json:"method"`
		Params  json.RawMessage   `json:"params"`
		Result  json.RawMessage   `json:"result"`
		Error   *sdk.RequestError `json:"error"`
	}
	if json.Unmarshal(line, &head) != nil || head.ID == nil || head.Method == "" {
		return
	}
	key := idKey(*head.ID)
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.ids[key] = struct{}{}
	}
}

// written releases the request that a written response frame answers.
func (p *pendingResponses) written(frame []byte) {
	p.mu.Lock()
	if len(p.ids) == 0 {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	var head struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if json.Unmarshal(frame, &head) != nil || head.Method != "" || len(head.ID) == 0 {
		return
	}
	key := idKey(head.ID)
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.ids[key]; ok {
		delete(p.ids, key)
		p.signalLocked()
	}
}

func (p *pendingResponses) signalLocked() {
	if p.closed && len(p.ids) == 0 && p.idle != nil {
		close(p.idle)
		p.idle = nil
	}
}

func (p *pendingResponses) open() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.closed
}

// closeAndWait refuses new requests and returns when every registered request
// has its response written, or when the output failed or closed.
func (p *pendingResponses) closeAndWait(failed, closed <-chan struct{}) {
	p.mu.Lock()
	p.closed = true
	if len(p.ids) == 0 {
		p.mu.Unlock()
		return
	}
	idle := make(chan struct{})
	p.idle = idle
	p.mu.Unlock()
	select {
	case <-idle:
	case <-failed:
	case <-closed:
	}
}

// idKey is the compact form of a JSON-RPC ID.
func idKey(raw json.RawMessage) string {
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return string(raw)
	}
	return b.String()
}
