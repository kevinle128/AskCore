package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
)

// ErrConnectionLost means the link ended before the call had its answer. The
// agent may have done the call. A caller must not send a call again on this
// ground alone.
var ErrConnectionLost = errors.New("rpc: the connection was lost")

// Transport carries JSON-RPC bodies. Send is safe to call from many goroutines.
// Next is called by the reader only. An error from Next ends the connection.
type Transport interface {
	Send(raw json.RawMessage) error
	Next() (json.RawMessage, error)
	Close() error
}

// Handler receives what is not an answer. Both methods run on the reader, so a
// handler must not wait for an answer of its own connection: it hands such work
// to another goroutine.
type Handler interface {
	// Notification is a call of the agent without an id.
	Notification(method string, params json.RawMessage)
	// Request is a call of the agent that needs an answer: reply with Respond or
	// RespondError, from any goroutine.
	Request(id json.RawMessage, method string, params json.RawMessage)
}

// Fault is an error answer. Kind is the Ask error kind from the error data.
type Fault struct {
	Code    int
	Kind    string
	Message string
}

func (f *Fault) Error() string {
	if f.Kind == "" {
		return fmt.Sprintf("rpc: error %d: %s", f.Code, f.Message)
	}
	return fmt.Sprintf("rpc: error %d (%s): %s", f.Code, f.Kind, f.Message)
}

// PanicError is a panic that a goroutine of the client caught.
type PanicError struct {
	Where string
	Value any
	Stack []byte
}

func (p *PanicError) Error() string { return fmt.Sprintf("panic in %s: %v", p.Where, p.Value) }

// Recover turns a recovered panic value into a *PanicError. Call it as
// Recover(where, recover()) in a deferred function, and only when recover()
// gave a value.
func Recover(where string, v any) *PanicError {
	return &PanicError{Where: where, Value: v, Stack: debug.Stack()}
}

type reply struct {
	result json.RawMessage
	fault  *Fault
}

type pending struct {
	reply  chan reply
	commit func(json.RawMessage)
}

// Conn tracks the requests of one connection. It has one reader (Run).
type Conn struct {
	t Transport
	h Handler

	mu      sync.Mutex
	nextID  int64
	pending map[int64]pending
	err     error

	done      chan struct{}
	closeOnce sync.Once
}

// NewConn returns a connection. Nothing is read until Run.
func NewConn(t Transport, h Handler) *Conn {
	return &Conn{t: t, h: h, pending: map[int64]pending{}, done: make(chan struct{})}
}

// Done is closed when the reader ended.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err tells why the reader ended. It is nil while the connection runs.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close closes the transport. Run returns after the transport unblocks Next.
func (c *Conn) Close() error { return c.t.Close() }

// Run reads until the transport fails, and then fails every pending call. A
// panic of the handler or of a commit function ends the connection with a
// *PanicError in Err; it does not crash the process.
func (c *Conn) Run() {
	var runErr error
	defer func() {
		if v := recover(); v != nil {
			runErr = Recover("rpc reader", v)
			_ = c.t.Close() // the peer must see the end of a reader that cannot go on
		}
		c.mu.Lock()
		c.err = runErr
		c.mu.Unlock()
		c.closeOnce.Do(func() { close(c.done) })
	}()
	for {
		raw, err := c.t.Next()
		if err != nil {
			runErr = err
			return
		}
		c.handle(raw)
	}
}

type incoming struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func (c *Conn) handle(raw json.RawMessage) {
	var m incoming
	if json.Unmarshal(raw, &m) != nil {
		return
	}
	switch {
	case m.Method == "" && len(m.ID) > 0:
		c.answer(m)
	case m.Method != "" && len(m.ID) > 0 && string(m.ID) != "null":
		c.h.Request(m.ID, m.Method, m.Params)
	case m.Method != "":
		c.h.Notification(m.Method, m.Params)
	}
}

func (c *Conn) answer(m incoming) {
	var id int64
	if json.Unmarshal(m.ID, &id) != nil {
		return
	}
	r := reply{result: m.Result}
	if len(m.Error) > 0 && string(m.Error) != "null" {
		var e struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    struct {
				Kind string `json:"kind"`
			} `json:"data"`
		}
		_ = json.Unmarshal(m.Error, &e)
		r.fault = &Fault{Code: e.Code, Kind: e.Data.Kind, Message: e.Message}
	}
	c.mu.Lock()
	p, ok := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if !ok {
		return
	}
	// The commit runs here, on the reader, before the next frame is read.
	if p.commit != nil && r.fault == nil {
		p.commit(r.result)
	}
	p.reply <- r
}

// Call sends a request and waits for its answer. The error is a *Fault for an
// error answer, ErrConnectionLost when the link ends first, or the error of ctx.
func (c *Conn) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.CallCommit(ctx, method, params, nil)
}

// CallCommit is Call with a function that runs on the reader when a successful
// answer arrives, before the reader takes the next frame. A follow call needs
// this: the events of the new subscription can follow its answer in the same
// burst, and they must find the subscription already known.
func (c *Conn) CallCommit(ctx context.Context, method string, params any, commit func(json.RawMessage)) (json.RawMessage, error) {
	select {
	case <-c.done:
		return nil, ErrConnectionLost
	default:
	}
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan reply, 1)
	c.pending[id] = pending{reply: ch, commit: commit}
	c.mu.Unlock()
	forget := func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}
	payload, err := encode(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		forget()
		return nil, err
	}
	if err := c.t.Send(payload); err != nil {
		forget()
		return nil, fmt.Errorf("%w: %v", ErrConnectionLost, err)
	}
	select {
	case r := <-ch:
		if r.fault != nil {
			return nil, r.fault
		}
		return r.result, nil
	case <-c.done:
		// An answer that arrived together with the end still counts.
		select {
		case r := <-ch:
			if r.fault != nil {
				return nil, r.fault
			}
			return r.result, nil
		default:
		}
		forget()
		return nil, ErrConnectionLost
	case <-ctx.Done():
		forget()
		return nil, ctx.Err()
	}
}

// Notify sends a call that has no answer.
func (c *Conn) Notify(method string, params any) error {
	b, err := encode(map[string]any{"method": method, "params": params})
	if err != nil {
		return err
	}
	if err := c.t.Send(b); err != nil {
		return fmt.Errorf("%w: %v", ErrConnectionLost, err)
	}
	return nil
}

// Respond answers a request of the agent.
func (c *Conn) Respond(id json.RawMessage, result any) error {
	b, err := encode(map[string]any{"id": id, "result": result})
	if err != nil {
		return err
	}
	return c.t.Send(b)
}

// RespondError answers a request of the agent with an error.
func (c *Conn) RespondError(id json.RawMessage, code int, message string) error {
	b, err := encode(map[string]any{"id": id, "error": map[string]any{"code": code, "message": message}})
	if err != nil {
		return err
	}
	return c.t.Send(b)
}

func encode(fields map[string]any) (json.RawMessage, error) {
	fields["jsonrpc"] = "2.0"
	return json.Marshal(fields)
}
