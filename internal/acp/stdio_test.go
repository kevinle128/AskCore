package acp_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"AskCore/internal/acp"
	"AskCore/internal/agent"
	"AskCore/internal/app"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

const stdioWait = 5 * time.Second

type frame struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	} `json:"error"`
}

// holdTool is a registered tool whose body ignores cancellation and returns
// only when the test releases it, like a real tool that cannot be interrupted.
type holdTool struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newHoldTool() *holdTool {
	return &holdTool{started: make(chan struct{}), release: make(chan struct{})}
}

func (*holdTool) ConcurrencySafe(json.RawMessage) bool { return true }

func (*holdTool) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{Name: "hold", Description: "Wait for release.", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}
}

func (h *holdTool) Execute(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
	h.once.Do(func() { close(h.started) })
	<-h.release
	return protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: "released"}}}, nil
}

func (h *holdTool) open() {
	select {
	case <-h.release:
	default:
		close(h.release)
	}
}

// faultLog is a real in-memory session log whose Append fails once armed.
type faultLog struct {
	*sessions.MemoryLog
	fail *atomic.Bool
}

func (f faultLog) Append(e ...sessions.Entry) (sessions.CommitRef, error) {
	if f.fail.Load() {
		return sessions.CommitRef{}, errors.New("disk full")
	}
	return f.MemoryLog.Append(e...)
}

type factoryOption struct {
	tools []tools.Tool
	log   func() sessions.Writer
}

// nativeFactory builds the Agent of a session the way the application does:
// through app.NewNativeAgent, with one scripted faux model for each session.
func nativeFactory(steps func() []faux.Step, o factoryOption) acp.Factory {
	return func(_ context.Context, id, cwd string) (*agent.Agent, error) {
		p, err := faux.New(faux.WithChunk(1000, 1000))
		if err != nil {
			return nil, err
		}
		m, _ := p.Model("faux-1")
		p.Set(steps()...)
		reg := &tools.Registry{}
		for _, t := range o.tools {
			if err := reg.Register(t, tools.SourceInfo{Kind: tools.SourceExtension, Name: "injected"}); err != nil {
				return nil, err
			}
		}
		cfg := agent.Config{SessionID: id, Tools: reg, NewContext: o.log, LoopConfig: agent.LoopConfig{
			Model: m, Stream: p.Stream, Cwd: cwd,
			Options: providers.StreamOptions{APIKey: "test-key"},
			Wait:    func(context.Context, time.Duration) error { return nil },
		}}
		return app.NewNativeAgent(app.NativeAgentConfig{Config: cfg, InitialStream: p.Stream})
	}
}

func adapterConfig(f acp.Factory) acp.Config {
	return acp.Config{
		Factory:     f,
		Info:        sdk.Implementation{Name: "ask", Version: "test"},
		AuthMethods: []sdk.AuthMethod{{Agent: &sdk.AuthMethodAgent{Id: "api-key", Name: "API key"}}},
		Models:      providers.AvailableModels,
		FindModel:   providers.Find,
	}
}

// stdioPeer drives ServeStdio over real pipes with raw NDJSON.
type stdioPeer struct {
	t       *testing.T
	in      *io.PipeWriter
	signals chan os.Signal
	exit    chan acp.StdioExit
	cleaned atomic.Bool
	diag    *safeBuffer

	nextID atomic.Int64
	mu     sync.Mutex
	lines  []string
	frames []frame
	done   chan struct{}
}

type safeBuffer struct {
	mu sync.Mutex
	sb strings.Builder
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.String()
}

type peerOptions struct {
	maxFrame int
}

func newStdioPeer(t *testing.T, cfg acp.Config, o peerOptions) *stdioPeer {
	t.Helper()
	baseline := goleak.IgnoreCurrent()
	t.Cleanup(func() { goleak.VerifyNone(t, baseline) })
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	p := &stdioPeer{t: t, in: inW, signals: make(chan os.Signal, 2), exit: make(chan acp.StdioExit, 1), diag: &safeBuffer{}, done: make(chan struct{})}
	go func() {
		p.exit <- acp.ServeStdio(context.Background(), acp.StdioConfig{
			Adapter: cfg, In: inR, Out: outW, Signals: p.signals, MaxFrame: o.maxFrame, Diag: p.diag,
			Cleanup: func(context.Context) error { p.cleaned.Store(true); return nil },
		})
	}()
	go func() {
		defer close(p.done)
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 0, 1<<16), 1<<24)
		for sc.Scan() {
			var f frame
			_ = json.Unmarshal(sc.Bytes(), &f)
			p.mu.Lock()
			p.lines = append(p.lines, sc.Text())
			p.frames = append(p.frames, f)
			p.mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = inW.Close()
		_ = outR.Close()
		select {
		case <-p.done:
		case <-time.After(stdioWait):
			t.Error("output reader did not stop")
		}
		_ = inR.Close()
	})
	return p
}

func (p *stdioPeer) sendRaw(method string, id any, params any) {
	p.t.Helper()
	m := map[string]any{"jsonrpc": "2.0", "method": method}
	if id != nil {
		m["id"] = id
	}
	if params != nil {
		m["params"] = params
	}
	b, err := json.Marshal(m)
	require.NoError(p.t, err)
	_, err = p.in.Write(append(b, '\n'))
	require.NoError(p.t, err)
}

func (p *stdioPeer) send(method string, params any) int64 {
	id := p.nextID.Add(1)
	p.sendRaw(method, id, params)
	return id
}

func (p *stdioPeer) waitFor(what string, cond func() bool) {
	p.t.Helper()
	deadline := time.Now().Add(stdioWait)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.t.Fatalf("timeout: %s\n%s", what, strings.Join(p.lines, "\n"))
}

func isResponse(f frame, id int64) bool {
	var got int64
	return f.Method == "" && len(f.ID) > 0 && json.Unmarshal(f.ID, &got) == nil && got == id
}

func (p *stdioPeer) responseIndex(id int64) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, f := range p.frames {
		if isResponse(f, id) {
			return i
		}
	}
	return -1
}

func (p *stdioPeer) await(id int64) frame {
	p.t.Helper()
	p.waitFor("response", func() bool { return p.responseIndex(id) >= 0 })
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.frames[p.responseIndexLocked(id)]
}

func (p *stdioPeer) responseIndexLocked(id int64) int {
	for i, f := range p.frames {
		if isResponse(f, id) {
			return i
		}
	}
	return -1
}

func (p *stdioPeer) call(method string, params any) frame {
	p.t.Helper()
	return p.await(p.send(method, params))
}

func (p *stdioPeer) start() string {
	p.t.Helper()
	require.Nil(p.t, p.call("initialize", map[string]any{"protocolVersion": 1}).Error)
	f := p.call("session/new", map[string]any{"cwd": p.t.TempDir(), "mcpServers": []any{}})
	require.Nil(p.t, f.Error)
	var res struct {
		SessionID string `json:"sessionId"`
	}
	require.NoError(p.t, json.Unmarshal(f.Result, &res))
	return res.SessionID
}

func (p *stdioPeer) updateKinds() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, f := range p.frames {
		if f.Method != "session/update" {
			continue
		}
		var n struct {
			Update struct {
				SessionUpdate string `json:"sessionUpdate"`
			} `json:"update"`
		}
		_ = json.Unmarshal(f.Params, &n)
		out = append(out, n.Update.SessionUpdate)
	}
	return out
}

func (p *stdioPeer) lastUpdateIndex() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	last := -1
	for i, f := range p.frames {
		if f.Method == "session/update" {
			last = i
		}
	}
	return last
}

func (p *stdioPeer) waitExit() acp.StdioExit {
	p.t.Helper()
	select {
	case e := <-p.exit:
		return e
	case <-time.After(stdioWait):
		p.t.Fatal("ServeStdio did not return")
		return acp.StdioExit{}
	}
}

func promptParams(sid, text string) map[string]any {
	return map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": text}}}
}

func calls(name string) func() []faux.Step {
	return func() []faux.Step {
		return []faux.Step{faux.Reply(faux.ToolCall(name, map[string]any{})), faux.Say("after")}
	}
}

// A started tool body is never interrupted by session/cancel: the prompt result
// waits for the body, and every update of the run is written before it.
func TestACPStdioToolDrain(t *testing.T) {
	hold := newHoldTool()
	t.Cleanup(hold.open)
	p := newStdioPeer(t, adapterConfig(nativeFactory(calls("hold"), factoryOption{tools: []tools.Tool{hold}})), peerOptions{})
	sid := p.start()
	id := p.send("session/prompt", promptParams(sid, "go"))
	select {
	case <-hold.started:
	case <-time.After(stdioWait):
		t.Fatal("tool body did not start")
	}
	p.sendRaw("session/cancel", nil, map[string]any{"sessionId": sid})
	// The state call proves the cancel notification was handled and the run is
	// still draining.
	var running bool
	p.waitFor("cancel handled", func() bool {
		f := p.call("_ask/session/state", map[string]any{"sessionId": sid})
		var st struct {
			Running bool `json:"running"`
		}
		_ = json.Unmarshal(f.Result, &st)
		running = st.Running
		return true
	})
	require.True(t, running, "the run must still drain the started tool")
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, -1, p.responseIndex(id), "no result while the tool body runs")

	hold.open()
	f := p.await(id)
	require.Nil(t, f.Error, "%s", f.Result)
	require.Contains(t, string(f.Result), `"cancelled"`)
	require.Greater(t, p.responseIndex(id), p.lastUpdateIndex(), "every update precedes the result")
	require.Contains(t, p.updateKinds(), "tool_call_update")

	_ = p.in.Close()
	e := p.waitExit()
	require.NoError(t, e.Err)
	require.Nil(t, e.Signal)
	require.True(t, p.cleaned.Load())
}

// A log that cannot append fails the prompt at once. The failure never waits for
// a start event and never binds the other session.
func TestACPStdioNoStartFailure(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var created atomic.Int32
	logs := func() sessions.Writer {
		// Only the first session gets the faulting log.
		if created.Add(1) == 1 {
			return faultLog{MemoryLog: &sessions.MemoryLog{}, fail: &fail}
		}
		return &sessions.MemoryLog{}
	}
	p := newStdioPeer(t, adapterConfig(nativeFactory(func() []faux.Step { return []faux.Step{faux.Say("ok")} }, factoryOption{log: logs})), peerOptions{})
	require.Nil(t, p.call("initialize", map[string]any{"protocolVersion": 1}).Error)
	newSession := func() string {
		f := p.call("session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}})
		require.Nil(t, f.Error)
		var res struct {
			SessionID string `json:"sessionId"`
		}
		require.NoError(t, json.Unmarshal(f.Result, &res))
		return res.SessionID
	}
	bad, good := newSession(), newSession()
	start := time.Now()
	f := p.call("session/prompt", promptParams(bad, "go"))
	require.NotNil(t, f.Error, "the prompt must fail")
	require.Less(t, time.Since(start), 2*time.Second)
	require.NotContains(t, string(f.Error.Data), "disk full")
	require.NotContains(t, strings.Join(p.lines, "\n"), "disk full")

	ok := p.call("session/prompt", promptParams(good, "go"))
	require.Nil(t, ok.Error, "%s", ok.Result)
	require.Contains(t, string(ok.Result), "end_turn")

	_ = p.in.Close()
	require.NoError(t, p.waitExit().Err)
}

// Closing stdin ends the server with a clean exit, after the host is disposed.
func TestStdioEOFClosesIdleHost(t *testing.T) {
	p := newStdioPeer(t, adapterConfig(nativeFactory(func() []faux.Step { return []faux.Step{faux.Say("ok")} }, factoryOption{})), peerOptions{})
	sid := p.start()
	f := p.call("session/prompt", promptParams(sid, "hi"))
	require.Nil(t, f.Error)
	_ = p.in.Close()
	e := p.waitExit()
	require.NoError(t, e.Err)
	require.False(t, e.Forced)
	require.Nil(t, e.Signal)
	require.True(t, p.cleaned.Load(), "owned cleanup must run on EOF")
}

// A signal runs the owned cleanup and reports the signal. The held tool makes
// the drain wait until the test releases it.
func TestStdioSignalDrainsThenExits(t *testing.T) {
	hold := newHoldTool()
	t.Cleanup(hold.open)
	p := newStdioPeer(t, adapterConfig(nativeFactory(calls("hold"), factoryOption{tools: []tools.Tool{hold}})), peerOptions{})
	sid := p.start()
	id := p.send("session/prompt", promptParams(sid, "go"))
	<-hold.started
	p.signals <- syscall.SIGTERM
	select {
	case e := <-p.exit:
		t.Fatalf("exit before the tool drained: %+v", e)
	case <-time.After(200 * time.Millisecond):
	}
	hold.open()
	e := p.waitExit()
	require.Equal(t, syscall.SIGTERM, e.Signal)
	require.False(t, e.Forced)
	require.True(t, p.cleaned.Load())
	// The output was closed first, so the prompt result never reached the peer.
	require.Equal(t, -1, p.responseIndex(id), "no success may follow a signal")
}

// A second signal forces the exit while a started tool still runs.
func TestStdioSecondSignalForces(t *testing.T) {
	hold := newHoldTool()
	t.Cleanup(hold.open)
	p := newStdioPeer(t, adapterConfig(nativeFactory(calls("hold"), factoryOption{tools: []tools.Tool{hold}})), peerOptions{})
	sid := p.start()
	p.send("session/prompt", promptParams(sid, "go"))
	<-hold.started
	p.signals <- syscall.SIGINT
	select {
	case e := <-p.exit:
		t.Fatalf("exit before the second signal: %+v", e)
	case <-time.After(150 * time.Millisecond):
	}
	p.signals <- syscall.SIGINT
	e := p.waitExit()
	require.Equal(t, syscall.SIGINT, e.Signal)
	require.True(t, e.Forced)
	// Let the stuck body end so the test leaves no goroutine behind.
	hold.open()
	time.Sleep(50 * time.Millisecond)
}

// A write that cannot finish is interrupted by the close of the output. The peer
// never reads, so the first response blocks inside the write.
func TestStdioSignalInterruptsBlockedWrite(t *testing.T) {
	baseline := goleak.IgnoreCurrent()
	t.Cleanup(func() { goleak.VerifyNone(t, baseline) })
	inR, inW := io.Pipe()
	_, outW := io.Pipe()
	signals := make(chan os.Signal, 2)
	exit := make(chan acp.StdioExit, 1)
	cfg := adapterConfig(nativeFactory(func() []faux.Step { return nil }, factoryOption{}))
	go func() {
		exit <- acp.ServeStdio(context.Background(), acp.StdioConfig{Adapter: cfg, In: inR, Out: outW, Signals: signals, Diag: io.Discard})
	}()
	_, err := inW.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}` + "\n"))
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
	signals <- syscall.SIGHUP
	select {
	case e := <-exit:
		require.Equal(t, syscall.SIGHUP, e.Signal)
		require.False(t, e.Forced)
	case <-time.After(stdioWait):
		t.Fatal("a blocked write held the shutdown")
	}
	_ = inW.Close()
}

// The output failing ends the server with an output error and no prompt success.
func TestStdioOutputFailure(t *testing.T) {
	cfg := adapterConfig(nativeFactory(func() []faux.Step { return []faux.Step{faux.Say("ok")} }, factoryOption{}))
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	exit := make(chan acp.StdioExit, 1)
	go func() {
		exit <- acp.ServeStdio(context.Background(), acp.StdioConfig{Adapter: cfg, In: inR, Out: outW, Diag: io.Discard})
	}()
	go func() { _, _ = io.Copy(io.Discard, outR) }()
	// The server may already have failed and closed its input: a write error
	// of the peer is then the expected end, not a test failure.
	write := func(s string) { _, _ = inW.Write([]byte(s + "\n")) }
	write(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`)
	// The reader of the output goes away: the next frame cannot be written.
	_ = outR.Close()
	write(`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"` + strings.ReplaceAll(t.TempDir(), `\`, `\\`) + `","mcpServers":[]}}`)
	select {
	case e := <-exit:
		require.Error(t, e.Err)
		require.ErrorIs(t, e.Err, acp.ErrOutputFailed)
	case <-time.After(stdioWait):
		t.Fatal("ServeStdio did not end after the output failed")
	}
	_ = inW.Close()
}

// One inbound frame over the cap ends the connection and names the cause.
func TestStdioOversizeFrame(t *testing.T) {
	p := newStdioPeer(t, adapterConfig(nativeFactory(func() []faux.Step { return nil }, factoryOption{})), peerOptions{maxFrame: 256})
	go func() { _, _ = p.in.Write([]byte(strings.Repeat("a", 1024))) }()
	e := p.waitExit()
	require.ErrorIs(t, e.Err, acp.ErrLineTooLong)
}

// A malformed line never reaches the diagnostics with its content.
func TestStdioDiagnosticsHoldNoInput(t *testing.T) {
	p := newStdioPeer(t, adapterConfig(nativeFactory(func() []faux.Step { return nil }, factoryOption{})), peerOptions{})
	p.start()
	_, err := p.in.Write([]byte("{not json SECRET-SENTINEL-1234\n"))
	require.NoError(t, err)
	require.Nil(t, p.call("_ask/session/state", map[string]any{"sessionId": "none"}).Result)
	_ = p.in.Close()
	require.NoError(t, p.waitExit().Err)
	require.NotContains(t, p.diag.String(), "SECRET-SENTINEL")
}

// A log that ends with a user message continues through the stdio connection:
// the result follows every update of the continued run.
func TestStdioContinueFromUserTail(t *testing.T) {
	logs := func() sessions.Writer {
		log := &sessions.MemoryLog{}
		_, _ = log.Append(sessions.MessageEntry{Message: protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "question"}}}})
		return log
	}
	p := newStdioPeer(t, adapterConfig(nativeFactory(func() []faux.Step { return []faux.Step{faux.Say("answer")} }, factoryOption{log: logs})), peerOptions{})
	sid := p.start()
	id := p.send("_ask/session/continue", map[string]any{"sessionId": sid})
	f := p.await(id)
	require.Nil(t, f.Error, "%s", f.Result)
	require.Contains(t, string(f.Result), "end_turn")
	require.Greater(t, p.responseIndex(id), p.lastUpdateIndex())
	require.Contains(t, p.updateKinds(), "agent_message_chunk")
	// The log now ends with the answer, so a second continue is refused at once.
	g := p.call("_ask/session/continue", map[string]any{"sessionId": sid})
	require.NotNil(t, g.Error)
	_ = p.in.Close()
	require.NoError(t, p.waitExit().Err)
}

// End of input with a prompt still open must still deliver the response of that
// prompt before the output closes.
func TestStdioEOFWithOpenPromptDeliversResponse(t *testing.T) {
	for i := 0; i < 10; i++ {
		hold := newHoldTool()
		p := newStdioPeer(t, adapterConfig(nativeFactory(calls("hold"), factoryOption{tools: []tools.Tool{hold}})), peerOptions{})
		sid := p.start()
		id := p.send("session/prompt", promptParams(sid, "go"))
		<-hold.started
		_ = p.in.Close()
		time.Sleep(20 * time.Millisecond)
		hold.open()
		e := p.waitExit()
		require.False(t, e.Forced)
		p.waitFor("prompt response after EOF", func() bool { return p.responseIndex(id) >= 0 })
	}
}

// A peer that stops reading stdout must not hold the shutdown after end of
// input: the write that cannot finish is interrupted after a bound.
func TestStdioEOFWithStalledStdoutPeerIsBounded(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	exit := make(chan acp.StdioExit, 1)
	cfg := adapterConfig(nativeFactory(func() []faux.Step { return []faux.Step{faux.Say("ok")} }, factoryOption{}))
	go func() {
		exit <- acp.ServeStdio(context.Background(), acp.StdioConfig{Adapter: cfg, In: inR, Out: outW, Diag: io.Discard})
	}()
	var sid string
	initSeen := make(chan struct{})
	sessionSeen := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 0, 1<<16), 1<<24)
		for sc.Scan() {
			var f frame
			_ = json.Unmarshal(sc.Bytes(), &f)
			if isResponse(f, 1) {
				close(initSeen)
			}
			var res struct {
				SessionID string `json:"sessionId"`
			}
			if json.Unmarshal(f.Result, &res) == nil && res.SessionID != "" {
				sid = res.SessionID
				close(sessionSeen)
				return // The peer stops reading here.
			}
		}
	}()
	write := func(s string) {
		_, err := inW.Write([]byte(s + "\n"))
		require.NoError(t, err)
	}
	write(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`)
	<-initSeen
	write(`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"` + strings.ReplaceAll(t.TempDir(), `\`, `\\`) + `","mcpServers":[]}}`)
	<-sessionSeen
	b, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "session/prompt", "params": promptParams(sid, "go")})
	require.NoError(t, err)
	write(string(b))
	time.Sleep(200 * time.Millisecond)
	_ = inW.Close()
	select {
	case result := <-exit:
		require.ErrorIs(t, result.Err, acp.ErrOutputFailed)
	case <-time.After(8 * time.Second):
		t.Fatal("a stalled stdout peer held the shutdown after end of input")
	}
	_ = outR.Close()
}
