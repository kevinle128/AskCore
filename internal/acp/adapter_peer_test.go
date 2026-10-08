package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

const peerWait = 5 * time.Second

// rpcFrame is one output frame of the adapter.
type rpcFrame struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

func (f rpcFrame) errKind(t *testing.T) protocol.ACPErrorKind {
	t.Helper()
	require.NotNil(t, f.Error, "expected an error frame, got result %s", f.Result)
	var d protocol.ACPErrorData
	require.NoError(t, json.Unmarshal(f.Error.Data, &d))
	return d.Kind
}

// adapterPeer drives a real Adapter through the real SDK connection over pipes.
type adapterPeer struct {
	t      *testing.T
	a      *Adapter
	conn   *sdk.AgentSideConnection
	in     *io.PipeWriter
	nextID atomic.Int64

	mu     sync.Mutex
	lines  []string
	frames []rpcFrame
}

// stepsFor returns the model script of one new session.
type stepsFor func() []faux.Step

// testConfig builds an adapter config with a faux model per session.
func testConfig(steps stepsFor, edit func(*agent.Config), opts ...faux.Option) Config {
	return Config{
		Factory: func(_ context.Context, id, cwd string) (*agent.Agent, error) {
			p, err := faux.New(append([]faux.Option{faux.WithChunk(1000, 1000)}, opts...)...)
			if err != nil {
				return nil, err
			}
			m, _ := p.Model("faux-1")
			p.Set(steps()...)
			cfg := agent.Config{SessionID: id, LoopConfig: agent.LoopConfig{
				Model: m, Stream: p.Stream, Cwd: cwd,
				Options: providers.StreamOptions{APIKey: "test-key"},
				Wait:    func(context.Context, time.Duration) error { return nil },
			}}
			if edit != nil {
				edit(&cfg)
			}
			return agent.New(cfg)
		},
		Info:        sdk.Implementation{Name: "ask", Version: "test"},
		AuthMethods: []sdk.AuthMethod{{Agent: &sdk.AuthMethodAgent{Id: "api-key", Name: "API key"}}},
		Authenticate: func(context.Context, string) error {
			return nil
		},
		Models:    providers.AvailableModels,
		FindModel: providers.Find,
	}
}

func says(texts ...string) stepsFor {
	return func() []faux.Step {
		out := make([]faux.Step, len(texts))
		for i, t := range texts {
			out[i] = faux.Say(t)
		}
		return out
	}
}

func newAdapterPeer(t *testing.T, cfg Config, wrapOut func(io.Writer) io.Writer) *adapterPeer {
	t.Helper()
	baseline := goleak.IgnoreCurrent()
	t.Cleanup(func() { goleak.VerifyNone(t, baseline) })
	a, err := NewAdapter(context.Background(), cfg)
	require.NoError(t, err)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	var w io.Writer = outW
	if wrapOut != nil {
		w = wrapOut(outW)
	}
	cw := NewCheckedWriter(w, func(err error) { a.Fail(err); _ = inR.CloseWithError(err) })
	conn := sdk.NewAgentSideConnection(a, cw, NewLineLimitReader(inR, 1<<20))
	a.Bind(conn, cw, outW)
	p := &adapterPeer{t: t, a: a, conn: conn, in: inW}
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 0, 1<<16), 1<<24)
		for sc.Scan() {
			line := sc.Text()
			var f rpcFrame
			_ = json.Unmarshal([]byte(line), &f)
			p.mu.Lock()
			p.lines = append(p.lines, line)
			p.frames = append(p.frames, f)
			p.mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = inW.Close()
		select {
		case <-conn.Done():
		case <-time.After(peerWait):
			t.Error("connection did not stop")
		}
		_ = a.Close()
		_ = outW.Close()
		_ = outR.Close()
		<-readerDone
		_ = inR.Close()
	})
	return p
}

func (p *adapterPeer) sendRaw(method string, id any, params any) {
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

// send starts a request and returns its ID.
func (p *adapterPeer) send(method string, params any) int64 {
	id := p.nextID.Add(1)
	p.sendRaw(method, id, params)
	return id
}

func (p *adapterPeer) notify(method string, params any) { p.sendRaw(method, nil, params) }

func (p *adapterPeer) waitFor(what string, cond func() bool) {
	p.t.Helper()
	deadline := time.Now().Add(peerWait)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	p.t.Fatalf("timeout: %s\n%s", what, strings.Join(p.snapshotLines(), "\n"))
}

func (p *adapterPeer) snapshotLines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.lines...)
}

func isID(f rpcFrame, id int64) bool {
	if len(f.ID) == 0 || f.Method != "" {
		return false
	}
	var got int64
	return json.Unmarshal(f.ID, &got) == nil && got == id
}

// responseIndex returns the position of the response of id, or -1.
func (p *adapterPeer) responseIndex(id int64) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, f := range p.frames {
		if isID(f, id) {
			return i
		}
	}
	return -1
}

// await waits for the response of id.
func (p *adapterPeer) await(id int64) rpcFrame {
	p.t.Helper()
	p.waitFor("response", func() bool { return p.responseIndex(id) >= 0 })
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, f := range p.frames {
		if isID(f, id) {
			return f
		}
	}
	return rpcFrame{}
}

func (p *adapterPeer) call(method string, params any) rpcFrame {
	p.t.Helper()
	return p.await(p.send(method, params))
}

// ok calls method, requires a result, and decodes it into out when given.
func (p *adapterPeer) ok(method string, params any, out any) {
	p.t.Helper()
	f := p.call(method, params)
	require.Nil(p.t, f.Error, "%s failed: %+v", method, f.Error)
	if out != nil {
		require.NoError(p.t, json.Unmarshal(f.Result, out))
	}
}

// start initializes the connection and opens one session.
func (p *adapterPeer) start() string {
	p.t.Helper()
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	return p.newSession()
}

func (p *adapterPeer) newSession() string {
	p.t.Helper()
	var res struct {
		SessionID string `json:"sessionId"`
	}
	p.ok("session/new", map[string]any{"cwd": p.t.TempDir(), "mcpServers": []any{}}, &res)
	require.NotEmpty(p.t, res.SessionID)
	return res.SessionID
}

func promptParams(sid, text string) map[string]any {
	return map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": text}}}
}

// notes returns the notifications of method, in output order.
func (p *adapterPeer) notes(method string) []rpcFrame {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []rpcFrame
	for _, f := range p.frames {
		if f.Method == method && len(f.ID) == 0 {
			out = append(out, f)
		}
	}
	return out
}

// updateKinds returns the sessionUpdate kinds of the session/update notifications.
func (p *adapterPeer) updateKinds() []string {
	var out []string
	for _, f := range p.notes("session/update") {
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

// lastNoteIndex returns the output position of the last notification of method, or -1.
func (p *adapterPeer) lastNoteIndex(method string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	last := -1
	for i, f := range p.frames {
		if f.Method == method && len(f.ID) == 0 {
			last = i
		}
	}
	return last
}

// heldStep is a model call that waits for release, then says text.
func heldStep(release <-chan struct{}, text string) faux.Step {
	return faux.Func(func(ctx context.Context, _ faux.Call) (faux.Step, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return faux.Say(text), nil
	})
}

// closeOnce returns a function that closes ch once.
func closeOnce(ch chan struct{}) func() {
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}

// gateClock holds every pacing wait of the faux provider until it is released,
// so a stream stays open in the middle of a block.
type gateClock struct {
	release chan struct{}
	waiting chan struct{}
	once    sync.Once
	passes  atomic.Int32
}

// newGateClock returns a clock that lets the first passes waits end at once.
func newGateClock(passes int32) *gateClock {
	g := &gateClock{release: make(chan struct{}), waiting: make(chan struct{})}
	g.passes.Store(passes)
	return g
}

func (g *gateClock) Now() time.Time { return time.Now() }

func (g *gateClock) After(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	if g.passes.Add(-1) >= 0 {
		ch <- time.Now()
		return ch
	}
	g.once.Do(func() { close(g.waiting) })
	go func() {
		<-g.release
		ch <- time.Now()
	}()
	return ch
}

func (g *gateClock) open() {
	select {
	case <-g.release:
	default:
		close(g.release)
	}
}

// preloaded returns an Agent edit whose log starts with a user message.
func preloaded(text string) func(*agent.Config) {
	return func(c *agent.Config) {
		c.NewContext = func() sessions.Writer {
			log := &sessions.MemoryLog{}
			_, _ = log.Append(sessions.MessageEntry{Message: protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: text}}}})
			return log
		}
	}
}

type ioWriter = io.Writer

// failingWriter fails every write after fail is set.
type failingWriter struct {
	w    io.Writer
	fail *atomic.Bool
}

func (f failingWriter) Write(p []byte) (int, error) {
	if f.fail.Load() {
		return 0, errors.New("pipe lost")
	}
	return f.w.Write(p)
}

// awaitRaw waits for the response whose ID has the given JSON text.
func (p *adapterPeer) awaitRaw(idJSON string) rpcFrame {
	p.t.Helper()
	var out rpcFrame
	p.waitFor("response "+idJSON, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, f := range p.frames {
			if f.Method == "" && string(f.ID) == idJSON {
				out = f
				return true
			}
		}
		return false
	})
	return out
}
