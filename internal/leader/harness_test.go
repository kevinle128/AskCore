package leader

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

const waitFor = 5 * time.Second

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitFor)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timeout: %s", what)
}

// ---- fake agent ----

// agentRequest is one message that the leader sent to the agent.
type agentRequest struct {
	Method string
	ID     json.RawMessage
	Params map[string]json.RawMessage
	Route  protocol.ACPRouteMeta
	Raw    json.RawMessage
}

// fakeAgent speaks NDJSON on a pipe pair, like the real ACP stream. It answers
// by a script and records every message it gets. It is a unit fixture: the
// real host has its own tests in internal/app.
type fakeAgent struct {
	t       *testing.T
	toLead  *io.PipeWriter // agent -> leader
	fromLed *io.PipeReader // leader -> agent
	wmu     sync.Mutex

	mu       sync.Mutex
	got      []agentRequest
	responds []json.RawMessage // responses that the leader forwarded (reverse answers)
	sessions int
	subs     int
	gens     map[string]uint64
	busy     map[string]bool
	onPrompt func(a *fakeAgent, req agentRequest)
	silent   map[string]bool // methods the agent does not answer
	done     chan struct{}
}

type agentEnd struct {
	io.Reader
	io.Writer
	closers []io.Closer
}

func (e *agentEnd) Close() error {
	for _, c := range e.closers {
		_ = c.Close()
	}
	return nil
}

func newFakeAgent(t *testing.T) (*fakeAgent, *agentEnd) {
	t.Helper()
	leadR, agentW := io.Pipe()
	agentR, leadW := io.Pipe()
	a := &fakeAgent{t: t, toLead: agentW, fromLed: agentR, gens: map[string]uint64{}, busy: map[string]bool{},
		silent: map[string]bool{}, done: make(chan struct{})}
	go a.loop()
	return a, &agentEnd{Reader: leadR, Writer: leadW, closers: []io.Closer{leadR, leadW}}
}

func (a *fakeAgent) loop() {
	defer close(a.done)
	defer func() { _ = a.toLead.Close() }()
	sc := bufio.NewScanner(a.fromLed)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<27)
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		m, err := parseMessage(line)
		if err != nil {
			a.t.Errorf("leader sent an invalid message: %v: %s", err, line)
			continue
		}
		if !m.hasMethod { // an answer to a request of the agent
			a.mu.Lock()
			a.responds = append(a.responds, line)
			a.mu.Unlock()
			continue
		}
		req := agentRequest{Method: m.method, ID: m.id, Raw: line}
		if p, err := m.params(); err == nil {
			req.Params = p
			if meta, err := decodeObject(p["_meta"]); err == nil {
				_ = json.Unmarshal(meta[protocol.ACPRouteMetaKey], &req.Route)
			}
		}
		a.mu.Lock()
		a.got = append(a.got, req)
		a.mu.Unlock()
		if bad := routeProblem(req); bad != "" {
			// The real host refuses such a call, so a router that sends one has a bug.
			a.t.Errorf("the router sent %s with a route the host would refuse: %s", req.Method, bad)
			if m.hasID {
				a.send(errorResponse(req.ID, protocol.ACPErrInvalidParams))
			}
			continue
		}
		if m.hasID {
			a.respond(req)
		}
	}
}

// routeProblem applies the checks of the real host to the route context of a
// call: a route names its client, and it names the session that the call names.
func routeProblem(req agentRequest) string {
	meta, err := decodeObject(req.Params["_meta"])
	if err != nil {
		return ""
	}
	rawRoute, has := meta[protocol.ACPRouteMetaKey]
	if !has {
		return ""
	}
	var route protocol.ACPRouteMeta
	if json.Unmarshal(rawRoute, &route) != nil || route.ClientID == "" {
		return "no client id"
	}
	if sid := stringParam(req.Params, "sessionId"); sid != "" && route.SessionID != sid {
		return "session id " + sid + " in the params, " + route.SessionID + " in the route"
	}
	return ""
}

func (a *fakeAgent) send(raw json.RawMessage) {
	a.wmu.Lock()
	defer a.wmu.Unlock()
	_, _ = a.toLead.Write(append(append([]byte(nil), raw...), '\n'))
}

func (a *fakeAgent) result(id json.RawMessage, result any) {
	a.send(resultResponse(id, result))
}

func (a *fakeAgent) respond(req agentRequest) {
	a.mu.Lock()
	if a.silent[req.Method] {
		a.mu.Unlock()
		return
	}
	sid := stringParam(req.Params, "sessionId")
	switch req.Method {
	case "initialize":
		a.mu.Unlock()
		a.result(req.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}})
	case "session/new":
		a.sessions++
		sid = fmt.Sprintf("s%d", a.sessions)
		a.gens[sid] = 1
		a.mu.Unlock()
		a.result(req.ID, map[string]any{"sessionId": sid, "_meta": map[string]any{protocol.ACPRouteMetaKey: protocol.ACPRouteMeta{DriverGen: 1}}})
	case protocol.ACPFollow:
		a.subs++
		sub := fmt.Sprintf("sub%d", a.subs)
		a.mu.Unlock()
		a.result(req.ID, protocol.ACPFollowResult{SubscriptionID: sub, Cursor: protocol.ACPCursor{Epoch: "e1", Seq: 1}})
	case protocol.ACPTake:
		if req.Route.LiveDriver && a.busy[sid] {
			a.mu.Unlock()
			a.send(errorResponse(req.ID, protocol.ACPErrBusy))
			return
		}
		a.gens[sid]++
		gen := a.gens[sid]
		a.mu.Unlock()
		a.result(req.ID, protocol.ACPTakeResult{SessionID: sid, DriverGen: gen})
	case "session/prompt":
		hook := a.onPrompt
		a.mu.Unlock()
		if hook != nil {
			hook(a, req)
			return
		}
		a.result(req.ID, map[string]any{"stopReason": "end_turn"})
	default:
		a.mu.Unlock()
		a.result(req.ID, map[string]any{})
	}
}

func (a *fakeAgent) setSilent(method string, silent bool) {
	a.mu.Lock()
	a.silent[method] = silent
	a.mu.Unlock()
}

func (a *fakeAgent) setGen(sid string, gen uint64) {
	a.mu.Lock()
	a.gens[sid] = gen
	a.mu.Unlock()
}

func (a *fakeAgent) setBusy(sid string, busy bool) {
	a.mu.Lock()
	a.busy[sid] = busy
	a.mu.Unlock()
}

func (a *fakeAgent) requests(method string) []agentRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []agentRequest
	for _, r := range a.got {
		if r.Method == method {
			out = append(out, r)
		}
	}
	return out
}

func (a *fakeAgent) waitRequests(method string, n int) []agentRequest {
	a.t.Helper()
	var got []agentRequest
	eventually(a.t, fmt.Sprintf("%d %s requests", n, method), func() bool {
		got = a.requests(method)
		return len(got) >= n
	})
	return got
}

func (a *fakeAgent) answers() []json.RawMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]json.RawMessage(nil), a.responds...)
}

func (a *fakeAgent) update(sid string, text string) json.RawMessage {
	return notification("session/update", map[string]any{"sessionId": sid, "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text}}})
}

// ---- leader under test ----

type harness struct {
	t      *testing.T
	srv    *Server
	agent  *fakeAgent
	paths  Paths
	ln     net.Listener
	served chan struct{}
}

type harnessOpts struct {
	edit func(*Config)
}

func startHarness(t *testing.T, opts ...harnessOpts) *harness {
	t.Helper()
	fa, end := newFakeAgent(t)
	p := testPaths(t)
	cfg := Config{
		Server: ServerConfig{InstanceID: "inst-1", Build: "dev", Controls: []string{"status", "shutdown"}, RegisterTimeout: 2 * time.Second},
		Agent:  end, LinkTimeout: 2 * time.Second,
	}
	for _, o := range opts {
		if o.edit != nil {
			o.edit(&cfg)
		}
	}
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.Start())
	ln, err := net.Listen("unix", p.Socket)
	require.NoError(t, err)
	h := &harness{t: t, srv: srv, agent: fa, paths: p, ln: ln, served: make(chan struct{})}
	go func() { defer close(h.served); _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = srv.Close()
		<-h.served
		<-fa.done
	})
	return h
}

// ---- client ----

// testClient is a real client of the leader socket.
type testClient struct {
	t    *testing.T
	conn net.Conn
	reg  *Registered
	w    *FrameWriter
	wmu  sync.Mutex
	next int64

	mu     sync.Mutex
	frames []protocol.LeaderFrame
	acp    []*rpcMessage
	closed bool
	paused bool
	cond   *sync.Cond
	done   chan struct{}
}

func (h *harness) dial() (*testClient, error) {
	conn, err := net.Dial("unix", h.paths.Socket)
	if err != nil {
		return nil, err
	}
	reg, err := Register(conn, hello(protocol.LeaderProtocolVersion), 2*time.Second)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	c := &testClient{t: h.t, conn: conn, reg: reg, w: reg.Writer, done: make(chan struct{})}
	c.cond = sync.NewCond(&c.mu)
	go c.readLoop()
	h.t.Cleanup(c.close)
	return c, nil
}

func (h *harness) client() *testClient {
	h.t.Helper()
	c, err := h.dial()
	require.NoError(h.t, err)
	return c
}

// ready returns a client that has run initialize with the given capabilities.
func (h *harness) ready(caps map[string]any) *testClient {
	h.t.Helper()
	c := h.client()
	if caps == nil {
		caps = map[string]any{}
	}
	res, rpcErr := c.call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": caps})
	require.Nil(h.t, rpcErr)
	require.NotNil(h.t, res)
	return c
}

func (c *testClient) readLoop() {
	defer close(c.done)
	for {
		c.mu.Lock()
		for c.paused {
			c.cond.Wait()
		}
		c.mu.Unlock()
		f, err := c.reg.Reader.Next()
		if err != nil {
			c.mu.Lock()
			c.closed = true
			c.mu.Unlock()
			return
		}
		c.mu.Lock()
		c.frames = append(c.frames, f)
		if f.Type == protocol.LeaderFrameACP {
			if m, err := parseMessage(f.Payload); err == nil {
				c.acp = append(c.acp, m)
			}
		}
		c.mu.Unlock()
	}
}

func (c *testClient) close() {
	_ = c.conn.Close()
	c.resume() // a paused reader must be able to end
}

// pause stops the client from reading its socket after the frame it reads now.
func (c *testClient) pause() {
	c.mu.Lock()
	c.paused = true
	c.mu.Unlock()
}

func (c *testClient) resume() {
	c.mu.Lock()
	c.paused = false
	c.cond.Broadcast()
	c.mu.Unlock()
}

// mark returns how many ACP messages the client has now.
func (c *testClient) mark() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.acp)
}

// since returns the ACP messages that arrived after a mark.
func (c *testClient) since(mark int) []*rpcMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*rpcMessage(nil), c.acp[mark:]...)
}

func (c *testClient) write(f protocol.LeaderFrame) {
	c.t.Helper()
	c.wmu.Lock()
	defer c.wmu.Unlock()
	require.NoError(c.t, c.w.Write(f))
}

func (c *testClient) sendRaw(raw json.RawMessage) {
	c.write(protocol.LeaderFrame{Type: protocol.LeaderFrameACP, Payload: raw})
}

func (c *testClient) request(method string, params any) int64 {
	id := c.nextID()
	c.sendRaw(marshalPlain(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}))
	return id
}

func (c *testClient) nextID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	return c.next
}

func (c *testClient) notify(method string, params any) {
	c.sendRaw(notification(method, params))
}

type rpcErr struct {
	Code int `json:"code"`
	Data struct {
		Kind string `json:"kind"`
	} `json:"data"`
}

func isResponse(m *rpcMessage, id int64) bool {
	return !m.hasMethod && m.hasID && string(m.id) == fmt.Sprint(id)
}

// await waits for the response of a request.
func (c *testClient) await(id int64) (json.RawMessage, *rpcErr) {
	c.t.Helper()
	var found *rpcMessage
	eventually(c.t, fmt.Sprintf("response %d", id), func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, m := range c.acp {
			if isResponse(m, id) {
				found = m
				return true
			}
		}
		return false
	})
	if raw := found.fields["error"]; raw != nil {
		var e rpcErr
		require.NoError(c.t, json.Unmarshal(raw, &e))
		return nil, &e
	}
	return found.fields["result"], nil
}

func (c *testClient) call(method string, params any) (json.RawMessage, *rpcErr) {
	c.t.Helper()
	return c.await(c.request(method, params))
}

func (c *testClient) mustCall(method string, params any) json.RawMessage {
	c.t.Helper()
	res, e := c.call(method, params)
	require.Nil(c.t, e, "%s failed: %+v", method, e)
	return res
}

// newSession creates a session and returns its id.
func (c *testClient) newSession(cwd string) string {
	c.t.Helper()
	res := c.mustCall("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
	var out struct {
		SessionID string `json:"sessionId"`
	}
	require.NoError(c.t, json.Unmarshal(res, &out))
	require.NotEmpty(c.t, out.SessionID)
	return out.SessionID
}

func (c *testClient) attach(sid string) protocol.ACPAttachResult {
	c.t.Helper()
	var out protocol.ACPAttachResult
	require.NoError(c.t, json.Unmarshal(c.mustCall(protocol.ACPAttach, map[string]any{"sessionId": sid}), &out))
	return out
}

// barrier makes sure that everything the router queued for this client before
// now has arrived. The answer comes through the same FIFO.
func (c *testClient) barrier() {
	c.t.Helper()
	_, _ = c.call(protocol.ACPListLive, map[string]any{})
}

// messages returns the ACP messages that have a method (notifications and requests).
func (c *testClient) incoming(method string) []*rpcMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []*rpcMessage
	for _, m := range c.acp {
		if m.hasMethod && m.method == method {
			out = append(out, m)
		}
	}
	return out
}

func (c *testClient) waitIncoming(method string, n int) []*rpcMessage {
	c.t.Helper()
	var got []*rpcMessage
	eventually(c.t, fmt.Sprintf("%d %s messages", n, method), func() bool {
		got = c.incoming(method)
		return len(got) >= n
	})
	return got
}

func (c *testClient) waitClosed() {
	c.t.Helper()
	select {
	case <-c.done:
	case <-time.After(waitFor):
		c.t.Fatal("the connection did not close")
	}
}

func (c *testClient) frameTypes() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.frames))
	for i, f := range c.frames {
		out[i] = f.Type
	}
	return out
}

// order returns the index of every ACP message in arrival order as "method" or "response".
func (c *testClient) order() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.acp))
	for i, m := range c.acp {
		if m.hasMethod {
			out[i] = m.method
		} else {
			out[i] = "response"
		}
	}
	return out
}
