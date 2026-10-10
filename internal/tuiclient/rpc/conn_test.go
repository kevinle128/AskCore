package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// pipeTransport is one end of an in-memory link. The other end is the peer
// that the test scripts by hand.
type pipeTransport struct {
	in     chan json.RawMessage
	out    chan json.RawMessage
	closed chan struct{}
	once   *sync.Once
}

func newPipe() (client, peer *pipeTransport) {
	a, b := make(chan json.RawMessage, 256), make(chan json.RawMessage, 256)
	closed, once := make(chan struct{}), &sync.Once{}
	return &pipeTransport{in: a, out: b, closed: closed, once: once}, &pipeTransport{in: b, out: a, closed: closed, once: once}
}

func (p *pipeTransport) Send(raw json.RawMessage) error {
	select {
	case <-p.closed:
		return io.ErrClosedPipe
	case p.out <- append(json.RawMessage(nil), raw...):
		return nil
	}
}

func (p *pipeTransport) Next() (json.RawMessage, error) {
	select {
	case <-p.closed:
		return nil, io.EOF
	case m := <-p.in:
		return m, nil
	}
}

func (p *pipeTransport) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

type recorder struct {
	mu            sync.Mutex
	notifications []string
	requests      []string
	onNotify      func(method string)
}

func (r *recorder) Notification(method string, _ json.RawMessage) {
	r.mu.Lock()
	r.notifications = append(r.notifications, method)
	r.mu.Unlock()
	if r.onNotify != nil {
		r.onNotify(method)
	}
}

func (r *recorder) Request(_ json.RawMessage, method string, _ json.RawMessage) {
	r.mu.Lock()
	r.requests = append(r.requests, method)
	r.mu.Unlock()
}

func (r *recorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.notifications...)
}

func start(t *testing.T, h Handler) (*Conn, *pipeTransport) {
	t.Helper()
	cl, peer := newPipe()
	c := NewConn(cl, h)
	done := make(chan struct{})
	go func() { defer close(done); c.Run() }()
	t.Cleanup(func() {
		_ = c.Close()
		<-done
	})
	return c, peer
}

func readRequest(t *testing.T, peer *pipeTransport) (id json.RawMessage, method string) {
	t.Helper()
	select {
	case raw := <-peer.in:
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		require.NoError(t, json.Unmarshal(raw, &m))
		return m.ID, m.Method
	case <-time.After(5 * time.Second):
		t.Fatal("the peer got no request")
		return nil, ""
	}
}

func answer(t *testing.T, peer *pipeTransport, id json.RawMessage, result string) {
	t.Helper()
	require.NoError(t, peer.Send(json.RawMessage(`{"jsonrpc":"2.0","id":`+string(id)+`,"result":`+result+`}`)))
}

func TestCallGetsItsOwnAnswer(t *testing.T) {
	c, peer := start(t, &recorder{})
	const n = 20
	results := make(chan string, n)
	for i := range n {
		go func() {
			res, err := c.Call(context.Background(), "echo", map[string]int{"n": i})
			if err != nil {
				results <- "error: " + err.Error()
				return
			}
			results <- string(res)
		}()
	}
	// Answer in reverse order of arrival: each answer must still reach its caller.
	ids := make([]json.RawMessage, 0, n)
	for range n {
		id, _ := readRequest(t, peer)
		ids = append(ids, id)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		require.False(t, seen[string(id)], "request ids are unique")
		seen[string(id)] = true
	}
	for i := len(ids) - 1; i >= 0; i-- {
		answer(t, peer, ids[i], string(ids[i]))
	}
	for range n {
		select {
		case r := <-results:
			require.NotContains(t, r, "error")
		case <-time.After(5 * time.Second):
			t.Fatal("a call got no answer")
		}
	}
}

func TestCallReturnsFaultOfErrorAnswer(t *testing.T) {
	c, peer := start(t, &recorder{})
	errc := make(chan error, 1)
	go func() { _, err := c.Call(context.Background(), "x", nil); errc <- err }()
	id, _ := readRequest(t, peer)
	require.NoError(t, peer.Send(json.RawMessage(`{"jsonrpc":"2.0","id":`+string(id)+`,"error":{"code":-32015,"message":"no","data":{"kind":"not_driver"}}}`)))
	err := <-errc
	var f *Fault
	require.ErrorAs(t, err, &f)
	require.Equal(t, -32015, f.Code)
	require.Equal(t, "not_driver", f.Kind)
	require.Equal(t, "no", f.Message)
}

func TestPendingCallsFailWhenLinkEnds(t *testing.T) {
	c, peer := start(t, &recorder{})
	errc := make(chan error, 1)
	go func() { _, err := c.Call(context.Background(), "slow", nil); errc <- err }()
	readRequest(t, peer)
	require.NoError(t, peer.Close())
	require.ErrorIs(t, <-errc, ErrConnectionLost)
	<-c.Done()
	_, err := c.Call(context.Background(), "after", nil)
	require.ErrorIs(t, err, ErrConnectionLost, "a call on an ended link is not sent")
}

func TestCallStopsWithItsContext(t *testing.T) {
	c, peer := start(t, &recorder{})
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := c.Call(ctx, "slow", nil); errc <- err }()
	readRequest(t, peer)
	cancel()
	require.ErrorIs(t, <-errc, context.Canceled)
}

// The result of a follow call must be committed on the reader before the reader
// takes the next frame, or an event that the leader wrote in the same burst
// would reach the handler first.
func TestCommitRunsBeforeNextFrame(t *testing.T) {
	var mu sync.Mutex
	committed := false
	var orderOK bool
	h := &recorder{}
	h.onNotify = func(string) {
		mu.Lock()
		orderOK = committed
		mu.Unlock()
	}
	c, peer := start(t, h)
	errc := make(chan error, 1)
	go func() {
		_, err := c.CallCommit(context.Background(), "follow", nil, func(json.RawMessage) {
			time.Sleep(20 * time.Millisecond) // a slow commit must still come first
			mu.Lock()
			committed = true
			mu.Unlock()
		})
		errc <- err
	}()
	id, _ := readRequest(t, peer)
	// The answer and the first event leave in one burst.
	answer(t, peer, id, `{}`)
	require.NoError(t, peer.Send(json.RawMessage(`{"jsonrpc":"2.0","method":"event","params":{}}`)))
	require.NoError(t, <-errc)
	require.Eventually(t, func() bool { return len(h.seen()) == 1 }, 5*time.Second, time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	require.True(t, orderOK, "the commit ran before the event reached the handler")
}

func TestNotificationAndRequestReachHandler(t *testing.T) {
	h := &recorder{}
	_, peer := start(t, h)
	require.NoError(t, peer.Send(json.RawMessage(`{"jsonrpc":"2.0","method":"note","params":{}}`)))
	require.NoError(t, peer.Send(json.RawMessage(`{"jsonrpc":"2.0","id":7,"method":"ask_me","params":{}}`)))
	require.Eventually(t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.notifications) == 1 && len(h.requests) == 1
	}, 5*time.Second, time.Millisecond)
}

type panicHandler struct{}

func (panicHandler) Notification(string, json.RawMessage)             { panic("handler bug") }
func (panicHandler) Request(json.RawMessage, string, json.RawMessage) {}

func TestHandlerPanicEndsConnectionWithError(t *testing.T) {
	c, peer := start(t, panicHandler{})
	errc := make(chan error, 1)
	go func() { _, err := c.Call(context.Background(), "x", nil); errc <- err }()
	readRequest(t, peer)
	require.NoError(t, peer.Send(json.RawMessage(`{"jsonrpc":"2.0","method":"note","params":{}}`)))
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the connection did not end")
	}
	var pe *PanicError
	require.True(t, errors.As(c.Err(), &pe), "the panic is reported as an error, got %v", c.Err())
	require.Contains(t, pe.Error(), "handler bug")
	require.ErrorIs(t, <-errc, ErrConnectionLost, "a pending call does not hang")
}

func TestNotifyAndRespond(t *testing.T) {
	c, peer := start(t, &recorder{})
	require.NoError(t, c.Notify("session/cancel", map[string]string{"sessionId": "s"}))
	require.NoError(t, c.Respond(json.RawMessage(`9`), map[string]string{"ok": "yes"}))
	require.NoError(t, c.RespondError(json.RawMessage(`10`), -32601, "not supported"))
	var got []string
	for range 3 {
		raw := <-peer.in
		got = append(got, string(raw))
	}
	require.Contains(t, got[0], `"method":"session/cancel"`)
	require.NotContains(t, got[0], `"id"`)
	require.Contains(t, got[1], `"id":9`)
	require.Contains(t, got[2], `"code":-32601`)
}
