package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/leader"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/settings"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

const routingWait = 5 * time.Second

func routingEventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	routingEventuallyWithin(t, what, routingWait, cond)
}

func routingEventuallyWithin(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timeout: %s", what)
}

// fauxLeaderParams builds the parameters of a leader whose sessions run a faux
// model. steps returns the script of one new session.
func fauxLeaderParams(t *testing.T, steps func() []faux.Step) LeaderParams {
	t.Helper()
	return fauxLeaderParamsWith(t, steps, nil)
}

// fauxLeaderParamsWith is fauxLeaderParams with a change to the agent config of each session.
func fauxLeaderParamsWith(t *testing.T, steps func() []faux.Step, edit func(*agent.Config)) LeaderParams {
	t.Helper()
	p := testACPParams(t)
	p.NewAgent = func(_ *auth.Service, id, cwd string, start providers.Model) (*agent.Agent, error) {
		fp, err := faux.New(faux.WithChunk(1000, 1000))
		if err != nil {
			return nil, err
		}
		m, _ := fp.Model("faux-1")
		fp.Set(steps()...)
		// The session runs on the model that the runtime chose. The demo model
		// is the faux row, so its scripted stream serves it.
		if start.Provider != "faux" {
			m = start
		}
		cfg := agent.Config{SessionID: id, LoopConfig: agent.LoopConfig{
			Model: m, Stream: fp.Stream, Cwd: cwd,
			Options: providers.StreamOptions{APIKey: "test-key"},
			Wait:    func(context.Context, time.Duration) error { return nil },
		}}
		if edit != nil {
			edit(&cfg)
		}
		return agent.New(cfg)
	}
	return LeaderParams{
		ACPParams: p,
		Server:    leader.ServerConfig{InstanceID: "inst-app", Build: "dev", RegisterTimeout: 2 * time.Second},
	}
}

func saying(texts ...string) func() []faux.Step {
	return func() []faux.Step {
		out := make([]faux.Step, len(texts))
		for i, text := range texts {
			out[i] = faux.Say(text)
		}
		return out
	}
}

// heldModel waits for release before it says its text, so a run stays active.
func heldModel(release <-chan struct{}) func() []faux.Step {
	return func() []faux.Step {
		return []faux.Step{faux.Func(func(ctx context.Context, _ faux.Call) (faux.Step, error) {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return faux.Say("done"), nil
		})}
	}
}

type runningLeader struct {
	t      *testing.T
	rt     *LeaderRuntime
	socket string
	cancel context.CancelFunc
	done   chan struct{} // closed when Serve returned
	err    error
}

func startLeader(t *testing.T, p LeaderParams) *runningLeader {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "askl-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	dir, err = filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	socket := filepath.Join(dir, "l.sock")
	ln, err := net.Listen("unix", socket)
	require.NoError(t, err)
	rt, err := NewLeaderRuntime(p)
	require.NoError(t, err)
	return serveLeaderRuntime(t, rt, ln, socket)
}

func serveLeaderRuntime(t *testing.T, rt *LeaderRuntime, ln net.Listener, socket string) *runningLeader {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	l := &runningLeader{t: t, rt: rt, socket: socket, cancel: cancel, done: make(chan struct{})}
	go func() { l.err = rt.Serve(ctx, ln); close(l.done) }()
	routingEventually(t, "leader serves", func() bool {
		c, err := net.Dial("unix", socket)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	})
	t.Cleanup(func() {
		cancel()
		select {
		case <-l.done:
		case <-time.After(10 * time.Second):
			t.Error("leader did not stop")
		}
	})
	return l
}

// ---- a client of the real leader socket ----

type routingClient struct {
	t    *testing.T
	conn net.Conn
	w    *leader.FrameWriter
	wmu  sync.Mutex

	mu   sync.Mutex
	next int64
	msgs []map[string]json.RawMessage
	done chan struct{}
}

func (l *runningLeader) client(caps map[string]any) *routingClient {
	l.t.Helper()
	conn, err := net.Dial("unix", l.socket)
	require.NoError(l.t, err)
	reg, err := leader.Register(conn, protocol.LeaderRegister{ClientKind: "test", ProtocolVersion: protocol.LeaderProtocolVersion, Build: "dev"}, 2*time.Second)
	require.NoError(l.t, err)
	c := &routingClient{t: l.t, conn: conn, w: reg.Writer, done: make(chan struct{})}
	go func() {
		defer close(c.done)
		for {
			f, err := reg.Reader.Next()
			if err != nil {
				return
			}
			if f.Type != protocol.LeaderFrameACP {
				continue
			}
			var m map[string]json.RawMessage
			if json.Unmarshal(f.Payload, &m) == nil {
				c.mu.Lock()
				c.msgs = append(c.msgs, m)
				c.mu.Unlock()
			}
		}
	}()
	l.t.Cleanup(func() { _ = conn.Close() })
	if caps == nil {
		caps = map[string]any{}
	}
	_, e := c.call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": caps})
	require.Nil(l.t, e)
	return c
}

type routingErr struct {
	Code int `json:"code"`
	Data struct {
		Kind string `json:"kind"`
	} `json:"data"`
}

func (c *routingClient) request(method string, params any) int64 {
	c.mu.Lock()
	c.next++
	id := c.next
	c.mu.Unlock()
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	c.wmu.Lock()
	defer c.wmu.Unlock()
	require.NoError(c.t, c.w.Write(protocol.LeaderFrame{Type: protocol.LeaderFrameACP, Payload: raw}))
	return id
}

func (c *routingClient) await(id int64) (json.RawMessage, *routingErr) {
	c.t.Helper()
	return c.awaitWithin(id, routingWait)
}

func (c *routingClient) awaitWithin(id int64, timeout time.Duration) (json.RawMessage, *routingErr) {
	c.t.Helper()
	var found map[string]json.RawMessage
	routingEventuallyWithin(c.t, fmt.Sprintf("response %d", id), timeout, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, m := range c.msgs {
			if _, isCall := m["method"]; !isCall && string(m["id"]) == fmt.Sprint(id) {
				found = m
				return true
			}
		}
		return false
	})
	if raw := found["error"]; raw != nil {
		var e routingErr
		require.NoError(c.t, json.Unmarshal(raw, &e))
		return nil, &e
	}
	return found["result"], nil
}

func (c *routingClient) call(method string, params any) (json.RawMessage, *routingErr) {
	c.t.Helper()
	return c.await(c.request(method, params))
}

func (c *routingClient) must(method string, params any) json.RawMessage {
	c.t.Helper()
	res, e := c.call(method, params)
	require.Nil(c.t, e, "%s: %+v", method, e)
	return res
}

func (c *routingClient) newSession(cwd string) string {
	c.t.Helper()
	var out struct {
		SessionID string         `json:"sessionId"`
		Meta      map[string]any `json:"_meta"`
	}
	require.NoError(c.t, json.Unmarshal(c.must("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}}), &out))
	require.NotEmpty(c.t, out.SessionID)
	require.Nil(c.t, out.Meta, "the route context stays inside the leader")
	return out.SessionID
}

func (c *routingClient) count(method string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.msgs {
		var name string
		if json.Unmarshal(m["method"], &name) == nil && name == method {
			n++
		}
	}
	return n
}

func (c *routingClient) waitCount(method string, n int) {
	c.t.Helper()
	routingEventually(c.t, fmt.Sprintf("%d %s messages", n, method), func() bool { return c.count(method) >= n })
}

func (c *routingClient) barrier() { _, _ = c.call(protocol.ACPListLive, map[string]any{}) }

func promptArgs(sid string) map[string]any {
	return map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": "hello"}}}
}

// ---- tests ----

func TestLeaderModuleValidatesWithoutServers(t *testing.T) {
	p := fauxLeaderParams(t, saying("ok"))
	require.NoError(t, fx.ValidateApp(LeaderModule, fx.Supply(p), fx.Invoke(func(*LeaderRuntime) {})))
}

func TestLeaderRuntimeRequiresAgentConstructor(t *testing.T) {
	p := fauxLeaderParams(t, saying("ok"))
	p.NewAgent = nil
	_, err := NewLeaderRuntime(p)
	require.Error(t, err)
}

func TestLeaderStopWithoutServe(t *testing.T) {
	rt, err := NewLeaderRuntime(fauxLeaderParams(t, saying("ok")))
	require.NoError(t, err)
	require.NoError(t, rt.Stop(context.Background()))
	require.NoError(t, rt.Stop(context.Background()), "a second stop is safe")
}

// Two clients with different directories and capabilities share one host and
// get independent sessions.
func TestLeaderRoutingIndependentSessions(t *testing.T) {
	l := startLeader(t, fauxLeaderParams(t, saying("a", "b")))
	a, b := l.client(map[string]any{"terminal": true}), l.client(nil)
	sa, sb := a.newSession("/work/a"), b.newSession("/work/b")
	require.NotEqual(t, sa, sb)

	var live protocol.ACPListLiveResult
	require.NoError(t, json.Unmarshal(a.must(protocol.ACPListLive, map[string]any{}), &live))
	cwd := map[string]string{}
	role := map[string]string{}
	for _, row := range live.Sessions {
		cwd[row.SessionID], role[row.SessionID] = row.Cwd, row.Role
	}
	require.Equal(t, map[string]string{sa: "/work/a", sb: "/work/b"}, cwd)
	require.Equal(t, map[string]string{sa: protocol.ACPRoleDriver, sb: ""}, role)

	s, ok := l.rt.Adapter.Session(sa)
	require.True(t, ok)
	require.JSONEq(t, `{"terminal":true}`, string(s.DriverCaps()), "the host holds the capabilities of the driver of that session")
}

// A prompt by the driver reaches the real Agent. The observer sees the same
// updates. The prompt result comes after the updates of its caller.
func TestLeaderRoutingSharedRun(t *testing.T) {
	l := startLeader(t, fauxLeaderParams(t, saying("hello world")))
	a, b := l.client(nil), l.client(nil)
	sid := a.newSession("/w")
	b.must(protocol.ACPAttach, map[string]any{"sessionId": sid})

	_, e := b.call("session/prompt", promptArgs(sid))
	require.NotNil(t, e)
	require.Equal(t, protocol.ACPCodeNotDriver, e.Code, "an observer cannot prompt")

	res := a.must("session/prompt", promptArgs(sid))
	require.Contains(t, string(res), "end_turn")
	require.Greater(t, a.count("session/update"), 0, "the driver got the updates before the result")
	b.waitCount("session/update", a.count("session/update"))
}

func TestLeaderRoutingTakeAndGeneration(t *testing.T) {
	l := startLeader(t, fauxLeaderParams(t, saying("one", "two")))
	a, b := l.client(nil), l.client(nil)
	sid := a.newSession("/w")
	b.must(protocol.ACPAttach, map[string]any{"sessionId": sid})

	var res protocol.ACPTakeResult
	require.NoError(t, json.Unmarshal(b.must(protocol.ACPTake, map[string]any{"sessionId": sid}), &res))
	require.Equal(t, uint64(2), res.DriverGen, "the generation comes from the host")
	s, _ := l.rt.Adapter.Session(sid)
	require.Equal(t, uint64(2), s.Gen())

	_, e := a.call("session/prompt", promptArgs(sid))
	require.NotNil(t, e)
	require.Equal(t, protocol.ACPCodeNotDriver, e.Code, "the former driver is an observer")
	require.Contains(t, string(b.must("session/prompt", promptArgs(sid))), "end_turn", "the new driver prompts with the new generation")
}

func TestLeaderRoutingFollowIsPrivate(t *testing.T) {
	l := startLeader(t, fauxLeaderParams(t, saying("hi")))
	a, b := l.client(nil), l.client(nil)
	sid := a.newSession("/w")
	b.must(protocol.ACPAttach, map[string]any{"sessionId": sid})

	var f protocol.ACPFollowResult
	require.NoError(t, json.Unmarshal(a.must(protocol.ACPFollow, map[string]any{"sessionId": sid}), &f))
	require.NotEmpty(t, f.SubscriptionID)
	a.must("session/prompt", promptArgs(sid))
	a.waitCount(protocol.ACPEvent, 1)
	b.barrier()
	require.Zero(t, b.count(protocol.ACPEvent), "follow frames go to the client that followed")

	_, e := b.call(protocol.ACPUnfollow, map[string]any{"sessionId": sid, "subscriptionId": f.SubscriptionID})
	require.NotNil(t, e)
	require.Equal(t, "unknown_subscription", e.Data.Kind)
	a.must(protocol.ACPUnfollow, map[string]any{"sessionId": sid, "subscriptionId": f.SubscriptionID})
}

// A client that leaves in the middle of a run neither fails the host nor
// cancels the run. Another client takes over and the run settles.
func TestLeaderClientLossDoesNotFailHost(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	l := startLeader(t, fauxLeaderParams(t, heldModel(release)))
	a, b := l.client(nil), l.client(nil)
	sid := a.newSession("/w")
	b.must(protocol.ACPAttach, map[string]any{"sessionId": sid})
	a.request("session/prompt", promptArgs(sid))
	routingEventually(t, "run active", func() bool { return l.rt.Adapter.ActiveRuns() == 1 })

	_ = a.conn.Close()
	<-a.done
	routingEventually(t, "driver gone", func() bool {
		var live protocol.ACPListLiveResult
		_ = json.Unmarshal(b.must(protocol.ACPListLive, map[string]any{}), &live)
		return len(live.Sessions) == 1 && !live.Sessions[0].HasDriver
	})
	select {
	case <-l.rt.Adapter.Failed():
		t.Fatal("a client loss latched the host")
	default:
	}
	require.NoError(t, l.rt.Adapter.Failure())
	require.Equal(t, 1, l.rt.Adapter.ActiveRuns(), "the run goes on without its client")

	// Another client takes the session while it is busy, because it has no driver.
	var res protocol.ACPTakeResult
	require.NoError(t, json.Unmarshal(b.must(protocol.ACPTake, map[string]any{"sessionId": sid}), &res))
	require.Equal(t, uint64(2), res.DriverGen)
	_, e := b.call("session/prompt", promptArgs(sid))
	require.NotNil(t, e)
	require.Equal(t, protocol.ACPErrBusy, protocol.ACPErrorKind(e.Data.Kind), "the run of the lost client is still the only run")

	free()
	routingEventually(t, "run settled", func() bool { return l.rt.Adapter.ActiveRuns() == 0 })
	require.Equal(t, 0, b.count("session/cancel"))
}

// A client of another protocol version asks for an idle shutdown while another
// client runs a tool. The leader refuses, the tool body and the run go on to
// their end, and only then does the idle shutdown work.
func TestLeaderQuiesceThroughManagement(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	tool := &holdTool{started: make(chan struct{}), release: release}
	registry := &tools.Registry{}
	require.NoError(t, registry.Register(tool, tools.SourceInfo{Kind: "builtin", Name: "hold"}))
	steps := func() []faux.Step {
		return []faux.Step{faux.Reply(faux.ToolCall("hold", nil, faux.ID("call-1"))), faux.Say("after")}
	}
	p := fauxLeaderParamsWith(t, steps, func(c *agent.Config) { c.Tools = registry })
	p.SpawnedByClient = true
	l := startLeader(t, p)
	a := l.client(nil)
	sid := a.newSession("/w")
	prompt := a.request("session/prompt", promptArgs(sid))
	select {
	case <-tool.started:
	case <-time.After(routingWait):
		t.Fatal("the tool body did not start")
	}

	shutdown := func() protocol.LeaderFrame {
		conn, err := net.Dial("unix", l.socket)
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()
		_, err = leader.Register(conn, protocol.LeaderRegister{ClientKind: "old", ProtocolVersion: 99}, 2*time.Second)
		require.Error(t, err)
		payload, _ := json.Marshal(protocol.LeaderControl{Command: protocol.LeaderControlShutdown, IfIdle: true, InstanceID: "inst-app"})
		require.NoError(t, leader.NewFrameWriter(conn, protocol.LeaderMaxFrame).Write(protocol.LeaderFrame{Type: protocol.LeaderFrameControl, Payload: payload}))
		f, err := leader.NewFrameReader(conn, protocol.LeaderMaxFrame).Next()
		require.NoError(t, err)
		return f
	}
	for range 5 { // repeated checks must leave the host as it was
		require.Equal(t, protocol.LeaderFrameError, shutdown().Type, "a leader with a running tool is not replaced")
	}
	select {
	case <-l.done:
		t.Fatal("a refused shutdown stopped the leader")
	default:
	}

	// The run of the other client is unaffected: the tool ends and the prompt completes.
	free()
	res, e := a.await(prompt)
	require.Nil(t, e, "the run was not disturbed by the refused shutdowns")
	require.Contains(t, string(res), "end_turn")
	a.newSession("/other") // new work is still admitted after the refusals

	routingEventually(t, "run settled", func() bool { return l.rt.Adapter.ActiveRuns() == 0 })
	routingEventually(t, "idle shutdown accepted", func() bool { return shutdown().Type == protocol.LeaderFrameControlReply })
	select {
	case <-l.done:
		require.NoError(t, l.err)
	case <-time.After(routingWait):
		t.Fatal("the leader did not stop")
	}
	_, ok := l.rt.Adapter.Session(sid)
	require.False(t, ok, "stop disposes every session")
}

func TestLeaderStopDisposesSessionsAndClosesClients(t *testing.T) {
	l := startLeader(t, fauxLeaderParams(t, saying("ok")))
	a := l.client(nil)
	sid := a.newSession("/w")
	l.cancel()
	select {
	case <-l.done:
		require.NoError(t, l.err)
	case <-time.After(routingWait):
		t.Fatal("the leader did not stop")
	}
	<-a.done
	_, ok := l.rt.Adapter.Session(sid)
	require.False(t, ok)
}

// holdTool is a tool whose body ignores every cancel until the test releases it.
type holdTool struct {
	started chan struct{}
	release <-chan struct{}
}

func (*holdTool) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{Name: "hold", Description: "hold", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (h *holdTool) Execute(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
	close(h.started)
	<-h.release
	return protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: "tool output"}}}, nil
}

// TestLeaderShutdownOrder checks the order of a stop. Admission closes and the
// clients are told first. The host then disposes the sessions and waits for a
// started tool body that ignores the cancel, so Serve does not return before it ends.
func TestLeaderShutdownOrder(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	tool := &holdTool{started: make(chan struct{}), release: release}
	registry := &tools.Registry{}
	require.NoError(t, registry.Register(tool, tools.SourceInfo{Kind: "builtin", Name: "hold"}))
	steps := func() []faux.Step {
		return []faux.Step{faux.Reply(faux.ToolCall("hold", nil, faux.ID("call-1"))), faux.Say("after")}
	}
	l := startLeader(t, fauxLeaderParamsWith(t, steps, func(c *agent.Config) { c.Tools = registry }))
	a := l.client(nil)
	sid := a.newSession("/w")
	a.request("session/prompt", promptArgs(sid))
	select {
	case <-tool.started:
	case <-time.After(routingWait):
		t.Fatal("the tool body did not start")
	}

	l.cancel() // the stop begins

	// 1. The listener closes, and the client is told and disconnected.
	routingEventually(t, "admission closed", func() bool {
		c, err := net.Dial("unix", l.socket)
		if err != nil {
			return true
		}
		_ = c.Close()
		return false
	})
	select {
	case <-a.done:
	case <-time.After(routingWait):
		t.Fatal("the client was not disconnected")
	}
	// 2. The drain has not ended, so the stop has not ended.
	select {
	case <-l.done:
		t.Fatal("Serve returned before the started tool body drained")
	case <-time.After(300 * time.Millisecond):
	}
	// 3. When the body ends, the stop ends and every session is gone.
	free()
	select {
	case <-l.done:
		require.NoError(t, l.err)
	case <-time.After(routingWait):
		t.Fatal("the leader did not stop after the drain")
	}
	_, ok := l.rt.Adapter.Session(sid)
	require.False(t, ok)
}

// ---- provider calls through the real host ----

// authFixture stands in for the provider endpoints: the key probe, the device
// code request and the token poll. It never holds a real credential.
type authFixture struct {
	mu      sync.Mutex
	refused map[string]bool // keys that the probe refuses
	probes  int
}

func (f *authFixture) RoundTrip(r *http.Request) (*http.Response, error) {
	reply := func(code int, body string) (*http.Response, error) {
		return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	}
	switch {
	case r.URL.Host == "auth.x.ai" && strings.HasSuffix(r.URL.Path, "/device/code"):
		return reply(200, `{"device_code":"dev-private","user_code":"USER-CODE","verification_uri":"https://auth.x.ai/device","expires_in":600,"interval":1}`)
	case r.URL.Host == "auth.x.ai":
		return reply(400, `{"error":"authorization_pending"}`)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models"):
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key == "" {
			key = r.Header.Get("x-api-key")
		}
		f.mu.Lock()
		f.probes++
		refused := f.refused[key]
		f.mu.Unlock()
		if refused {
			// A provider can repeat the key in its body. The host must not.
			return reply(401, `{"error":{"code":"invalid_api_key","message":"bad key `+key+`"}}`)
		}
		return reply(200, `{"data":[]}`)
	}
	return nil, fmt.Errorf("unexpected request to %s", r.URL.Host)
}

// providerSetupLeader starts a leader with the faux demo model and the auth fixture.
func providerSetupLeader(t *testing.T, edit func(*LeaderParams)) (*runningLeader, *authFixture, LeaderParams) {
	t.Helper()
	fixture := &authFixture{refused: map[string]bool{}}
	p := fauxLeaderParams(t, saying("ok"))
	p.AuthHTTP = &http.Client{Transport: fixture}
	p.Wait = func(ctx context.Context, _ time.Duration) error { <-ctx.Done(); return ctx.Err() }
	if edit != nil {
		edit(&p)
	}
	return startLeader(t, p), fixture, p
}

func decodeInto[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(raw, &v))
	return v
}

func TestLeaderRoutingProviderCalls(t *testing.T) {
	l, fx, _ := providerSetupLeader(t, nil)
	a, b := l.client(nil), l.client(nil)

	list := decodeInto[protocol.ACPProvidersResult](t, a.must(protocol.ACPProvidersList, map[string]any{}))
	require.Len(t, list.Options, 7)
	require.Equal(t, uint64(0), list.Generation)

	// A key is checked by the probe, then saved. Both clients hear of it.
	key := "sk-fake-openai-key"
	out := decodeInto[protocol.ACPLoginOutcome](t, a.must(protocol.ACPProvidersAPIKey, map[string]any{"loginId": "k1", "providerId": "openai", "key": key}))
	require.Equal(t, protocol.ACPLoginStatusSaved, out.Status)
	require.False(t, out.NotChecked)
	require.Equal(t, uint64(1), out.Generation)
	require.Equal(t, 1, fx.probes)
	a.waitCount(protocol.ACPProvidersChanged, 1)
	b.waitCount(protocol.ACPProvidersChanged, 1)

	list = decodeInto[protocol.ACPProvidersResult](t, b.must(protocol.ACPProvidersList, map[string]any{}))
	for _, o := range list.Options {
		if o.ProviderID == "openai" && o.MethodID == "api-key" {
			require.Equal(t, protocol.ACPLoginStateAPIKey, o.State)
		}
	}

	// The model list of a session names only usable providers.
	sid := a.newSession("/w")
	all := decodeInto[protocol.ACPModelsResult](t, a.must(protocol.ACPModels, map[string]any{"sessionId": sid}))
	usable := decodeInto[protocol.ACPModelsResult](t, a.must(protocol.ACPModels, map[string]any{"sessionId": sid, "usableOnly": true}))
	require.False(t, all.UsableOnly)
	require.True(t, usable.UsableOnly)
	providersSeen := map[string]bool{}
	for _, m := range usable.Models {
		providersSeen[m.Provider] = true
	}
	require.Equal(t, map[string]bool{"faux": true, "openai": true}, providersSeen)
	require.Greater(t, len(all.Models), len(usable.Models))

	// A device sign-in: start, wait, cancel.
	start := decodeInto[protocol.ACPLoginStartResult](t, a.must(protocol.ACPLoginStart, map[string]any{"providerId": "xai", "methodId": "xai-oauth"}))
	require.Equal(t, protocol.ACPLoginKindDevice, start.Kind)
	require.Equal(t, "USER-CODE", start.UserCode)
	require.NotEmpty(t, start.LoginID)
	waitID := a.request(protocol.ACPLoginWait, map[string]any{"loginId": start.LoginID})
	_, e := b.call(protocol.ACPLoginCancel, map[string]any{"loginId": start.LoginID})
	require.NotNil(t, e, "another client cannot reach the sign-in")
	cancel := decodeInto[protocol.ACPLoginCancelResult](t, a.must(protocol.ACPLoginCancel, map[string]any{"loginId": start.LoginID}))
	require.True(t, cancel.Cancelled)
	res, werr := a.await(waitID)
	require.Nil(t, werr)
	require.Equal(t, protocol.ACPLoginStatusCancelled, decodeInto[protocol.ACPLoginOutcome](t, res).Status)

	// A removal reaches both clients and clears the model of a session on that provider.
	set := a.must(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": acp2ID(providers.OpenAIGPT55())})
	require.Contains(t, string(set), "openai/gpt-5.5")
	lo := decodeInto[protocol.ACPLogoutResult](t, a.must(protocol.ACPProvidersLogout, map[string]any{"providerId": "openai", "methodId": "api-key", "sessionId": sid}))
	require.True(t, lo.Removed)
	require.Equal(t, acp2ID(providers.OpenAIGPT55()), lo.ClearedModel)
	require.Equal(t, uint64(2), lo.Generation)
	a.waitCount(protocol.ACPProvidersChanged, 2)
	b.waitCount(protocol.ACPProvidersChanged, 2)
	st := decodeInto[protocol.ACPStateResult](t, a.must(protocol.ACPState, map[string]any{"sessionId": sid}))
	require.Equal(t, "", st.ModelID)
}

func acp2ID(m providers.Model) string { return fmt.Sprintf("%s/%s@%s", m.Provider, m.ID, m.API) }

func TestHostLogoutCancelsOtherClientSignIn(t *testing.T) {
	l, _, _ := providerSetupLeader(t, nil)
	a, b := l.client(nil), l.client(nil)
	start := decodeInto[protocol.ACPLoginStartResult](t, a.must(protocol.ACPLoginStart, map[string]any{"providerId": "xai", "methodId": "xai-oauth"}))
	waitID := a.request(protocol.ACPLoginWait, map[string]any{"loginId": start.LoginID})

	// A second sign-in to the same provider is refused while the first waits.
	_, e := b.call(protocol.ACPLoginStart, map[string]any{"providerId": "xai", "methodId": "xai-oauth"})
	require.NotNil(t, e)
	require.Equal(t, "provider_busy", e.Data.Kind)

	lo := decodeInto[protocol.ACPLogoutResult](t, b.must(protocol.ACPProvidersLogout, map[string]any{"providerId": "xai", "methodId": "xai-oauth"}))
	require.True(t, lo.Removed)
	res, werr := a.await(waitID)
	require.Nil(t, werr)
	out := decodeInto[protocol.ACPLoginOutcome](t, res)
	require.Equal(t, protocol.ACPLoginStatusCancelled, out.Status)
	require.Equal(t, protocol.ACPLoginReasonRemoved, out.Reason)
	a.waitCount(protocol.ACPProvidersChanged, 1)
	b.waitCount(protocol.ACPProvidersChanged, 1)
}

func TestLeaderStopWithPendingLoginIsQuick(t *testing.T) {
	l, _, _ := providerSetupLeader(t, nil)
	a := l.client(nil)
	start := decodeInto[protocol.ACPLoginStartResult](t, a.must(protocol.ACPLoginStart, map[string]any{"providerId": "xai", "methodId": "xai-oauth"}))
	a.request(protocol.ACPLoginWait, map[string]any{"loginId": start.LoginID})
	a.barrier() // the wait request was read before the stop starts

	began := time.Now()
	require.NoError(t, l.rt.Stop(context.Background()))
	require.Less(t, time.Since(began), 5*time.Second, "a waiting sign-in does not hold the refresh drain")
}

func TestHostNeverLogsSecrets(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	fixtureKey, code := "sk-fake-refused-key-value", "fake-code-value-7788"
	l, fx, _ := providerSetupLeader(t, func(p *LeaderParams) { p.Logger = zap.New(core) })
	fx.refused[fixtureKey] = true
	a := l.client(nil)

	out := decodeInto[protocol.ACPLoginOutcome](t, a.must(protocol.ACPProvidersAPIKey, map[string]any{"loginId": "k1", "providerId": "openai", "key": fixtureKey}))
	require.Equal(t, protocol.ACPLoginStatusRefused, out.Status)
	require.Equal(t, 401, out.HTTPStatus)
	require.Equal(t, "invalid_api_key", out.ProviderCode)
	_, e := a.call(protocol.ACPLoginInput, map[string]any{"loginId": "unknown", "code": code})
	require.NotNil(t, e)
	a.barrier()

	for _, entry := range logs.All() {
		text := entry.Message
		for k, v := range entry.ContextMap() {
			text += " " + k + "=" + fmt.Sprint(v)
		}
		require.NotContains(t, text, fixtureKey)
		require.NotContains(t, text, code)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, m := range a.msgs {
		raw, _ := json.Marshal(m)
		require.NotContains(t, string(raw), fixtureKey, "no frame to a client repeats the key")
		require.NotContains(t, string(raw), code)
	}
}

// A session on a leader with no usable provider has no model and refuses a prompt.
func TestLeaderSessionWithoutModelRefusesPrompt(t *testing.T) {
	l, _, _ := providerSetupLeader(t, func(p *LeaderParams) { p.Initial = providers.Model{} })
	a := l.client(nil)
	sid := a.newSession("/w")
	st := decodeInto[protocol.ACPStateResult](t, a.must(protocol.ACPState, map[string]any{"sessionId": sid}))
	require.Equal(t, "", st.ModelID)
	models := decodeInto[protocol.ACPModelsResult](t, a.must(protocol.ACPModels, map[string]any{"sessionId": sid, "usableOnly": true}))
	require.Empty(t, models.Models)
	require.Equal(t, "", models.Current)
	_, e := a.call("session/prompt", promptArgs(sid))
	require.NotNil(t, e)
	require.Equal(t, "no_model", e.Data.Kind)

	// A new session after a key save starts on the recommended model.
	out := decodeInto[protocol.ACPLoginOutcome](t, a.must(protocol.ACPProvidersAPIKey, map[string]any{"loginId": "k1", "providerId": "openai", "key": "sk-fake-openai-key"}))
	require.Equal(t, protocol.ACPLoginStatusSaved, out.Status)
	other := a.newSession("/w")
	st = decodeInto[protocol.ACPStateResult](t, a.must(protocol.ACPState, map[string]any{"sessionId": other}))
	require.Equal(t, acp2ID(providers.OpenAIGPT55()), st.ModelID)
	require.Nil(t, st.ModelFallback)
}

func TestDefaultModelSavedOnSelect(t *testing.T) {
	l, _, p := providerSetupLeader(t, func(p *LeaderParams) { p.Initial = providers.Model{} })
	prefs, err := settings.NewSettingsStore(p.Home)
	require.NoError(t, err)
	a := l.client(nil)
	a.must(protocol.ACPProvidersAPIKey, map[string]any{"loginId": "k1", "providerId": "openai", "key": "sk-fake-openai-key"})
	a.must(protocol.ACPProvidersAPIKey, map[string]any{"loginId": "k2", "providerId": "anthropic", "key": "sk-fake-anthropic-key"})
	sid := a.newSession("/w")
	saved := func() string {
		id, err := prefs.DefaultModel(context.Background())
		require.NoError(t, err)
		return id
	}
	require.Empty(t, saved(), "a sign-in sets no default")
	_, statErr := os.Stat(filepath.Join(p.Home, "settings.json"))
	require.True(t, os.IsNotExist(statErr), "a sign-in writes no settings file")

	sol := acp2ID(providers.OpenAIGPT56Sol())
	res := decodeInto[protocol.ACPSetModelResult](t, a.must(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": sol}))
	require.False(t, res.DefaultSaved)
	require.Empty(t, saved(), "no flag, nothing saved")

	_, e := a.call(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": "openai/none@x", "saveDefault": true})
	require.NotNil(t, e)
	require.Empty(t, saved(), "a refused switch saves nothing")

	res = decodeInto[protocol.ACPSetModelResult](t, a.must(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": sol, "saveDefault": true}))
	require.True(t, res.DefaultSaved)
	require.Equal(t, sol, saved())
	info, err := os.Stat(filepath.Join(p.Home, "settings.json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	// The next session starts on the saved default, and the list names it.
	next := a.newSession("/w")
	st := decodeInto[protocol.ACPStateResult](t, a.must(protocol.ACPState, map[string]any{"sessionId": next}))
	require.Equal(t, sol, st.ModelID)
	require.Nil(t, st.ModelFallback)
	models := decodeInto[protocol.ACPModelsResult](t, a.must(protocol.ACPModels, map[string]any{"sessionId": next, "usableOnly": true}))
	require.Equal(t, sol, models.Default)
	require.Equal(t, sol, models.Current)

	// Removing the provider of the default: a new session falls back and says so.
	a.must(protocol.ACPProvidersLogout, map[string]any{"providerId": "openai", "methodId": "api-key"})
	after := a.newSession("/w")
	st = decodeInto[protocol.ACPStateResult](t, a.must(protocol.ACPState, map[string]any{"sessionId": after}))
	require.Equal(t, acp2ID(providers.AnthropicSonnet46()), st.ModelID)
	require.NotNil(t, st.ModelFallback)
	require.Equal(t, protocol.ACPModelFallback{SavedDefault: sol, Reason: protocol.ACPFallbackNoCredential}, *st.ModelFallback)
	require.Equal(t, sol, saved(), "a fallback keeps the saved default")
}

func TestDefaultNotSetBySignIn(t *testing.T) {
	l, _, p := providerSetupLeader(t, func(p *LeaderParams) { p.Initial = providers.Model{} })
	a := l.client(nil)
	sid := a.newSession("/w")
	a.must(protocol.ACPProvidersAPIKey, map[string]any{"loginId": "k1", "providerId": "openai", "key": "sk-fake-openai-key"})
	a.must(protocol.ACPProvidersAPIKey, map[string]any{"loginId": "k2", "providerId": "alibaba-token-plan", "key": "fake-token-plan-key"})
	_, err := os.Stat(filepath.Join(p.Home, "settings.json"))
	require.True(t, os.IsNotExist(err), "a sign-in writes no default")
	st := decodeInto[protocol.ACPStateResult](t, a.must(protocol.ACPState, map[string]any{"sessionId": sid}))
	require.Equal(t, "", st.ModelID, "a sign-in does not change the model of a session")
}

func TestModelsReportsSavedDefault(t *testing.T) {
	l, _, p := providerSetupLeader(t, nil)
	prefs, err := settings.NewSettingsStore(p.Home)
	require.NoError(t, err)
	a := l.client(nil)
	sid := a.newSession("/w")
	models := decodeInto[protocol.ACPModelsResult](t, a.must(protocol.ACPModels, map[string]any{"sessionId": sid}))
	require.Empty(t, models.Default)
	id := acp2ID(providers.AnthropicSonnet46())
	require.NoError(t, prefs.SetDefaultModel(context.Background(), id))
	models = decodeInto[protocol.ACPModelsResult](t, a.must(protocol.ACPModels, map[string]any{"sessionId": sid}))
	require.Equal(t, id, models.Default, "the list names the saved default whether or not it is usable")
}
