package acp

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"AskCore/pkg/protocol"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// Pins of the SDK and its stable schema. The wire version is independent of both.
const (
	pinSDKModule  = "github.com/coder/acp-go-sdk"
	pinSDKVersion = "v0.13.5"
	pinSchemaTag  = "0.13.5"
	pinWireV      = 1
)

var pinSchemaSHA256 = map[string]string{
	"schema.json":          "0da9fe718746ccc2e8ef789efa6687e64a252dea3a8789faae9a1acbe60e0d3b",
	"meta.json":            "9a33a0049faec80db5e1fb11524e38ebbccb0811c60682272d4aefcf6d2af029",
	"schema.unstable.json": "9e1a31a28775e2a4194a60912278d6b34a03f3ab7e95d63aa4bab6e5618390e2",
	"meta.unstable.json":   "6e17db561428dda78dc1812a737f8ab529544da282a8302dba876a9908630278",
}

const confWait = 5 * time.Second

// confAgent is a minimal SDK agent. The embedded nil interface makes any
// method that a test does not set panic, so a test cannot reach it by chance.
type confAgent struct {
	sdk.Agent
	ext      func(ctx context.Context, method string, params json.RawMessage) (any, error)
	cancelFn func(sessionID string)
	prompt   func(ctx context.Context, sessionID string) (sdk.PromptResponse, error)
	newSess  func(req sdk.NewSessionRequest) (sdk.NewSessionResponse, error)
}

func (a *confAgent) Initialize(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
	return sdk.InitializeResponse{ProtocolVersion: pinWireV}, nil
}

func (a *confAgent) Cancel(_ context.Context, p sdk.CancelNotification) error {
	if a.cancelFn != nil {
		a.cancelFn(string(p.SessionId))
	}
	return nil
}

func (a *confAgent) NewSession(_ context.Context, p sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
	return a.newSess(p)
}

func (a *confAgent) Prompt(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
	return a.prompt(ctx, string(p.SessionId))
}

func (a *confAgent) LoadSession(context.Context, sdk.LoadSessionRequest) (sdk.LoadSessionResponse, error) {
	return sdk.LoadSessionResponse{}, sdk.NewMethodNotFound("session/load")
}

func (a *confAgent) HandleExtensionMethod(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if a.ext == nil {
		return nil, sdk.NewMethodNotFound(method)
	}
	return a.ext(ctx, method, params)
}

// noExtAgent has no extension hook, so every extension method is unknown.
type noExtAgent struct{ sdk.Agent }

// peer drives a real SDK agent connection over pipes with raw JSON-RPC frames.
type peer struct {
	t      *testing.T
	conn   *sdk.AgentSideConnection
	in     *io.PipeWriter // frames to the agent
	inR    *io.PipeReader
	frames chan map[string]json.RawMessage
	raw    chan string
	out    *io.PipeWriter
}

func newPeer(t *testing.T, a sdk.Agent, maxLine int, wrap func(io.Writer) io.Writer) *peer {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	p := &peer{t: t, in: inW, inR: inR, out: outW, frames: make(chan map[string]json.RawMessage, 4096), raw: make(chan string, 4096)}
	var w io.Writer = outW
	if wrap != nil {
		w = wrap(outW)
	}
	cw := NewCheckedWriter(w, func(err error) { inR.CloseWithError(err) })
	p.conn = sdk.NewAgentSideConnection(a, cw, NewLineLimitReader(inR, maxLine))
	p.conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 0, 1<<16), 1<<24)
		for sc.Scan() {
			line := sc.Text()
			p.raw <- line
			var m map[string]json.RawMessage
			if json.Unmarshal([]byte(line), &m) == nil {
				p.frames <- m
			}
		}
		close(p.frames)
	}()
	t.Cleanup(func() {
		_ = inW.Close()
		_ = inR.Close()
		_ = outW.Close()
		_ = outR.Close()
		select {
		case <-p.conn.Done():
		case <-time.After(confWait):
			t.Error("connection did not stop after cleanup")
		}
	})
	return p
}

func (p *peer) send(line string) {
	p.t.Helper()
	_, err := p.in.Write([]byte(line + "\n"))
	require.NoError(p.t, err)
}

func (p *peer) next() map[string]json.RawMessage {
	p.t.Helper()
	select {
	case m, ok := <-p.frames:
		require.True(p.t, ok, "output closed")
		return m
	case <-time.After(confWait):
		p.t.Fatal("no frame within bound")
	}
	return nil
}

func (p *peer) noFrame(d time.Duration) {
	p.t.Helper()
	select {
	case m := <-p.frames:
		p.t.Fatalf("unexpected frame %v", m)
	case <-time.After(d):
	}
}

func (p *peer) done(what string) {
	p.t.Helper()
	select {
	case <-p.conn.Done():
	case <-time.After(confWait):
		p.t.Fatalf("connection still open after %s", what)
	}
}

func errCode(t *testing.T, m map[string]json.RawMessage) int {
	t.Helper()
	var e struct{ Code int }
	require.NoError(t, json.Unmarshal(m["error"], &e), "frame %v", m)
	return e.Code
}

func echoExt(_ context.Context, _ string, params json.RawMessage) (any, error) { return params, nil }

func sdkDir(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", pinSDKModule).Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func TestACPConformanceVersionSchema(t *testing.T) {
	dir := sdkDir(t)
	for name, want := range pinSchemaSHA256 {
		b, err := os.ReadFile(filepath.Join(dir, "schema", name))
		require.NoError(t, err)
		sum := sha256.Sum256(b)
		require.Equal(t, want, hex.EncodeToString(sum[:]), name)
	}
	v, err := os.ReadFile(filepath.Join(dir, "schema", "version"))
	require.NoError(t, err)
	require.Equal(t, pinSchemaTag, strings.TrimSpace(string(v)))
	require.True(t, strings.HasSuffix(dir, "@"+pinSDKVersion), dir)

	meta, err := os.ReadFile(filepath.Join(dir, "schema", "meta.json"))
	require.NoError(t, err)
	var m struct {
		Version      int
		AgentMethods map[string]string
	}
	require.NoError(t, json.Unmarshal(meta, &m))
	require.Equal(t, pinWireV, m.Version)
	require.Contains(t, m.AgentMethods, "session_set_config_option")
	require.NotContains(t, m.AgentMethods, "session_set_model", "stable schema has no set_model")

	p := newPeer(t, &confAgent{}, 1<<20, nil)
	p.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`)
	f := p.next()
	var res struct{ ProtocolVersion int }
	require.NoError(t, json.Unmarshal(f["result"], &res))
	require.Equal(t, pinWireV, res.ProtocolVersion)
}

func TestACPConformanceMetaPrecision(t *testing.T) {
	big := `{"n":9007199254740993,"u":18446744073709551615,"null":null,"arr":[1,{"x":9007199254740993}],"deep":{"k":{"z":null}},"unknown":"v"}`
	var got json.RawMessage
	var mu sync.Mutex
	p := newPeer(t, &confAgent{ext: func(ctx context.Context, m string, params json.RawMessage) (any, error) {
		mu.Lock()
		got = append(json.RawMessage(nil), params...)
		mu.Unlock()
		return echoExt(ctx, m, params)
	}}, 1<<20, nil)
	p.send(`{"jsonrpc":"2.0","id":"a","method":"_ask/session/state","params":` + big + `}`)
	f := p.next()
	require.JSONEq(t, `"a"`, string(f["id"]))
	require.Equal(t, big, string(f["result"]), "extension result keeps raw numbers")
	mu.Lock()
	require.Equal(t, big, string(got), "extension params keep raw numbers")
	mu.Unlock()

	// Typed event notification: the cursor counter travels as decimal text.
	ev := protocol.ACPEventNotification{SubscriptionID: "s", SessionID: "x", Epoch: "e", Seq: math.MaxUint64, Event: json.RawMessage(`{"type":"agent_start"}`)}
	frame, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": protocol.ACPEvent, "params": ev})
	require.NoError(t, err)
	p.send(string(frame))
	f = p.next()
	var back protocol.ACPEventNotification
	require.NoError(t, json.Unmarshal(f["result"], &back))
	require.Equal(t, ev.Seq, back.Seq)

	// Standard _meta decodes into map[string]any. A JSON number above 2^53 loses
	// exactness there, so Ask counters in _meta must be decimal strings.
	typed := make(chan sdk.NewSessionRequest, 1)
	p2 := newPeer(t, &confAgent{newSess: func(r sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
		typed <- r
		return sdk.NewSessionResponse{SessionId: "s1"}, nil
	}}, 1<<20, nil)
	p2.send(`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"cwd":"/","mcpServers":[],"_meta":{"seq":"18446744073709551615","num":9007199254740993,"nil":null,"nested":{"a":[1]}}}}`)
	p2.next()
	r := <-typed
	require.Equal(t, "18446744073709551615", r.Meta["seq"])
	require.Nil(t, r.Meta["nil"])
	require.Contains(t, r.Meta, "nil")
	t.Logf("typed numeric _meta decodes as %T %v", r.Meta["num"], r.Meta["num"])
	require.NotEqual(t, "9007199254740993", fmt.Sprintf("%.0f", r.Meta["num"]), "limit documented: typed _meta is not exact")
}

func TestACPConformanceAskDispatch(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	p := newPeer(t, &confAgent{ext: func(ctx context.Context, m string, params json.RawMessage) (any, error) {
		mu.Lock()
		seen = append(seen, m)
		mu.Unlock()
		switch m {
		case "_ask/session/compact":
			return nil, sdk.NewMethodNotFound(m)
		case "_ask/session/safe":
			return nil, &sdk.RequestError{Code: -32001, Message: "busy", Data: map[string]any{"reason": "busy"}}
		case "_ask/session/leaky":
			return nil, errors.New("provider body secret-token")
		}
		return echoExt(ctx, m, params)
	}}, 1<<20, nil)

	p.send(`{"jsonrpc":"2.0","id":1,"method":"_ask\/session\/state","params":{"sessionId":"s"}}`)
	require.JSONEq(t, `{"sessionId":"s"}`, string(p.next()["result"]))

	p.send(`{"jsonrpc":"2.0","id":2,"method":"_ask/session/safe","params":{}}`)
	f := p.next()
	var e struct {
		Code int
		Data map[string]string
	}
	require.NoError(t, json.Unmarshal(f["error"], &e))
	require.Equal(t, -32001, e.Code)
	require.Equal(t, "busy", e.Data["reason"])

	// An unmapped Go error puts its text into error data. The host must map
	// every error before it returns, so provider text never reaches the wire.
	p.send(`{"jsonrpc":"2.0","id":3,"method":"_ask/session/leaky","params":{}}`)
	f = p.next()
	require.Equal(t, -32603, errCode(t, f))
	require.Contains(t, string(f["error"]), "secret-token")

	p.send(`{"jsonrpc":"2.0","id":4,"method":"_ask/session/compact","params":{}}`)
	require.Equal(t, -32601, errCode(t, p.next()))

	// Notifications reach the handler and never produce a frame.
	p.send(`{"jsonrpc":"2.0","method":"_ask/session/event","params":{"seq":"1"}}`)
	p.send(`{"jsonrpc":"2.0","id":5,"method":"_ask/session/state","params":{}}`)
	require.JSONEq(t, "5", string(p.next()["id"]))
	mu.Lock()
	require.Contains(t, seen, "_ask/session/event")
	mu.Unlock()

	// No extension hook: unknown request is method-not-found, unknown notification is ignored.
	q := newPeer(t, noExtAgent{}, 1<<20, nil)
	q.send(`{"jsonrpc":"2.0","method":"_ask/session/event","params":{}}`)
	q.noFrame(100 * time.Millisecond)
	q.send(`{"jsonrpc":"2.0","id":9,"method":"_ask/session/state","params":{}}`)
	require.Equal(t, -32601, errCode(t, q.next()))
}

// reverseClient answers the reverse extension request after its gate opens.
type reverseClient struct {
	sdk.Client
	gate  chan struct{}
	asked chan string
}

func (c *reverseClient) HandleExtensionMethod(ctx context.Context, _ string, params json.RawMessage) (any, error) {
	var p struct{ SessionID string }
	_ = json.Unmarshal(params, &p)
	c.asked <- p.SessionID
	select {
	case <-c.gate:
		return map[string]string{"ok": p.SessionID}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestACPConformanceConcurrentReverseTraffic(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	cl := &reverseClient{gate: make(chan struct{}), asked: make(chan string, 4)}
	ag := &confAgent{}
	var asc *sdk.AgentSideConnection
	cancelled := make(chan string, 4)
	ag.cancelFn = func(id string) { cancelled <- id }
	ag.prompt = func(ctx context.Context, id string) (sdk.PromptResponse, error) {
		done := make(chan error, 1)
		go func() {
			_, err := asc.CallExtension(context.Background(), "_ask/reverse", map[string]string{"sessionId": id})
			done <- err
		}()
		select {
		case <-ctx.Done():
			return sdk.PromptResponse{StopReason: sdk.StopReasonCancelled}, nil
		case err := <-done:
			return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, err
		}
	}
	asc = sdk.NewAgentSideConnection(ag, NewCheckedWriter(outW, nil), NewLineLimitReader(inR, 1<<20))
	client := sdk.NewClientSideConnection(cl, inW, outR)
	t.Cleanup(func() {
		_ = inW.Close()
		_ = outW.Close()
		<-asc.Done()
		<-client.Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), confWait)
	defer cancel()
	type res struct {
		id  string
		r   sdk.PromptResponse
		err error
	}
	results := make(chan res, 2)
	for _, id := range []string{"A", "B"} {
		go func() {
			r, err := client.Prompt(ctx, sdk.PromptRequest{SessionId: sdk.SessionId(id), Prompt: []sdk.ContentBlock{sdk.TextBlock("hi")}})
			results <- res{id, r, err}
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-cl.asked:
		case <-ctx.Done():
			t.Fatal("reverse requests did not arrive while both prompts were held")
		}
	}
	require.NoError(t, client.Cancel(ctx, sdk.CancelNotification{SessionId: "A"}))
	got := <-results
	require.NoError(t, got.err)
	require.Equal(t, "A", got.id)
	require.Equal(t, sdk.StopReasonCancelled, got.r.StopReason)
	require.Equal(t, "A", <-cancelled)
	select {
	case other := <-results:
		t.Fatalf("session %s finished before its reverse reply", other.id)
	case <-time.After(100 * time.Millisecond):
	}
	close(cl.gate)
	got = <-results
	require.NoError(t, got.err)
	require.Equal(t, "B", got.id)
	require.Equal(t, sdk.StopReasonEndTurn, got.r.StopReason)
}

func TestACPConformanceRequestCancellation(t *testing.T) {
	var started = make(chan struct{}, 8)
	var release = make(chan struct{})
	p := newPeer(t, &confAgent{ext: func(ctx context.Context, m string, params json.RawMessage) (any, error) {
		if m != "_t/hold" {
			return echoExt(ctx, m, params)
		}
		started <- struct{}{}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return map[string]bool{"released": true}, nil
		}
	}}, 1<<20, nil)
	hold := func(id string) {
		p.send(`{"jsonrpc":"2.0","id":` + id + `,"method":"_t/hold","params":{}}`)
		<-started
	}
	cancel := func(id string) {
		p.send(`{"jsonrpc":"2.0","method":"$/cancel_request","params":{"requestId":` + id + `}}`)
	}

	// Numeric and string IDs are cancelled, and report the cancelled error code.
	for _, id := range []string{`7`, `"abc"`} {
		hold(id)
		cancel(id)
		f := p.next()
		require.JSONEq(t, id, string(f["id"]))
		require.Equal(t, -32800, errCode(t, f))
	}

	// Number 1 and string "1" are different IDs; a wrong-type cancel is a no-op.
	hold(`1`)
	cancel(`"1"`)
	p.noFrame(100 * time.Millisecond)
	cancel(`1`)
	require.Equal(t, -32800, errCode(t, p.next()))

	// Unknown ID and cancel before the request arrives are ignored; the later request runs.
	cancel(`99`)
	cancel(`100`)
	hold(`100`)
	p.noFrame(100 * time.Millisecond)
	release <- struct{}{}
	f := p.next()
	require.Contains(t, string(f["result"]), "released")

	// Cancel after the response is a no-op and the connection stays usable.
	cancel(`100`)
	p.send(`{"jsonrpc":"2.0","id":101,"method":"_ask/x","params":{}}`)
	require.JSONEq(t, "101", string(p.next()["id"]))
}

func TestACPConformanceInboundPressure(t *testing.T) {
	gate := make(chan struct{})
	var once sync.Once
	entered := make(chan struct{})
	var cancelled = make(chan string, 1)
	ag := &confAgent{cancelFn: func(id string) { cancelled <- id }}
	ag.ext = func(ctx context.Context, m string, params json.RawMessage) (any, error) {
		if m == "_t/block" {
			once.Do(func() { close(entered) })
			<-gate
			return nil, nil
		}
		return echoExt(ctx, m, params)
	}
	p := newPeer(t, ag, 1<<20, nil)
	released := false
	t.Cleanup(func() {
		if !released {
			close(gate)
		}
	})
	p.send(`{"jsonrpc":"2.0","method":"_t/block","params":{}}`)
	<-entered

	// Requests and $/cancel_request bypass the blocked ordered queue.
	p.send(`{"jsonrpc":"2.0","id":1,"method":"_ask/x","params":{}}`)
	require.JSONEq(t, "1", string(p.next()["id"]))

	// session/cancel is a notification: it waits behind the blocked callback.
	p.send(`{"jsonrpc":"2.0","method":"session/cancel","params":{"sessionId":"S"}}`)
	select {
	case <-cancelled:
		t.Fatal("session/cancel overtook a blocked notification callback")
	case <-time.After(150 * time.Millisecond):
	}

	// A full queue fails the connection in bounded time. It does not grow without limit.
	go func() {
		for i := 0; i < 1100; i++ {
			if _, err := p.in.Write([]byte(`{"jsonrpc":"2.0","method":"_t/n","params":{}}` + "\n")); err != nil {
				return
			}
		}
	}()
	p.done("queue overflow")
	released = true
	close(gate)
}

func TestACPConformanceOutboundFault(t *testing.T) {
	type sink struct {
		mu   sync.Mutex
		data []byte
	}
	cases := map[string]func(s *sink) io.Writer{
		"error": func(*sink) io.Writer {
			return writerFunc(func([]byte) (int, error) { return 0, errors.New("pipe broken") })
		},
		"zero-byte": func(*sink) io.Writer { return writerFunc(func([]byte) (int, error) { return 0, nil }) },
		"short": func(s *sink) io.Writer {
			return writerFunc(func(b []byte) (int, error) {
				s.mu.Lock()
				defer s.mu.Unlock()
				s.data = append(s.data, b[:len(b)/2]...)
				return len(b) / 2, nil
			})
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			s := &sink{}
			p := newPeer(t, &confAgent{ext: echoExt}, 1<<20, func(io.Writer) io.Writer { return mk(s) })
			p.send(`{"jsonrpc":"2.0","id":1,"method":"_ask/x","params":{}}`)
			p.done("lost response")
			p.noFrame(50 * time.Millisecond)
			s.mu.Lock()
			require.False(t, strings.HasSuffix(string(s.data), "\n"), "a partial frame is never complete")
			s.mu.Unlock()
		})
	}

	t.Run("latch", func(t *testing.T) {
		var fails int
		w := NewCheckedWriter(writerFunc(func([]byte) (int, error) { return 0, io.ErrClosedPipe }), func(error) { fails++ })
		_, err := w.Write([]byte("x"))
		require.ErrorIs(t, err, io.ErrClosedPipe)
		_, err = w.Write([]byte("y"))
		require.ErrorIs(t, err, io.ErrClosedPipe)
		require.Equal(t, 1, fails)
		require.ErrorIs(t, w.Err(), io.ErrClosedPipe)
	})

	t.Run("blocked", func(t *testing.T) {
		block, entered := make(chan error), make(chan struct{}, 1)
		p := newPeer(t, &confAgent{ext: echoExt}, 1<<20, func(io.Writer) io.Writer {
			return writerFunc(func([]byte) (int, error) { entered <- struct{}{}; return 0, <-block })
		})
		p.send(`{"jsonrpc":"2.0","id":1,"method":"_ask/x","params":{}}`)
		<-entered
		p.noFrame(100 * time.Millisecond)
		select {
		case <-p.conn.Done():
			t.Fatal("connection ended while the write was still blocked")
		default:
		}
		block <- errors.New("closed")
		p.done("blocked write failure")
	})
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(b []byte) (int, error) { return f(b) }

func TestACPConformanceIngressFault(t *testing.T) {
	t.Run("eof", func(t *testing.T) {
		p := newPeer(t, &confAgent{}, 1<<20, nil)
		_ = p.in.Close()
		p.done("EOF")
	})
	t.Run("partial frame then eof", func(t *testing.T) {
		p := newPeer(t, &confAgent{ext: echoExt}, 1<<20, nil)
		_, err := p.in.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"_ask/x","par`))
		require.NoError(t, err)
		_ = p.in.Close()
		p.done("partial frame EOF")
		p.noFrame(50 * time.Millisecond)
	})
	t.Run("first line over cap, split bytes", func(t *testing.T) {
		const limit = 64
		p := newPeer(t, &confAgent{ext: echoExt}, limit, nil)
		line := strings.Repeat("a", limit+1)
		go func() {
			for i := 0; i < len(line); i++ {
				if _, err := p.in.Write([]byte{line[i]}); err != nil {
					return
				}
			}
		}()
		p.done("over-cap line")
	})
	t.Run("line exactly at cap passes", func(t *testing.T) {
		head := `{"jsonrpc":"2.0","id":1,"method":"_ask/x","params":{"p":"`
		tail := `"}}`
		const limit = 256
		p := newPeer(t, &confAgent{ext: echoExt}, limit, nil)
		pad := limit - len(head) - len(tail)
		line := head + strings.Repeat("p", pad) + tail
		require.Len(t, line, limit)
		p.send(line)
		require.JSONEq(t, "1", string(p.next()["id"]))
	})
	t.Run("malformed json is skipped", func(t *testing.T) {
		p := newPeer(t, &confAgent{ext: echoExt}, 1<<20, nil)
		p.send(`{not json`)
		p.send(`{"jsonrpc":"2.0","id":1,"method":"_ask/x","params":{}}`)
		require.JSONEq(t, "1", string(p.next()["id"]), "no parse-error frame is sent for bad input")
	})
	t.Run("duplicate ids", func(t *testing.T) {
		gate := make(chan struct{})
		p := newPeer(t, &confAgent{ext: func(ctx context.Context, m string, params json.RawMessage) (any, error) {
			<-gate
			return echoExt(ctx, m, params)
		}}, 1<<20, nil)
		p.send(`{"jsonrpc":"2.0","id":1,"method":"_ask/x","params":{"n":1}}`)
		p.send(`{"jsonrpc":"2.0","id":1,"method":"_ask/x","params":{"n":2}}`)
		close(gate)
		a, b := p.next(), p.next()
		require.JSONEq(t, "1", string(a["id"]))
		require.JSONEq(t, "1", string(b["id"]))
	})
}

func TestACPConformanceStableSchema(t *testing.T) {
	dir := sdkDir(t)
	f, err := os.Open(filepath.Join(dir, "schema", "schema.json"))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	doc, err := jsonschema.UnmarshalJSON(f)
	require.NoError(t, err)
	c := jsonschema.NewCompiler()
	require.NoError(t, c.AddResource("schema.json", doc))
	compile := func(def string) *jsonschema.Schema {
		s, err := c.Compile("schema.json#/$defs/" + def)
		require.NoError(t, err, def)
		return s
	}
	validate := func(def string, v any) {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		inst, err := jsonschema.UnmarshalJSON(strings.NewReader(string(b)))
		require.NoError(t, err)
		require.NoError(t, compile(def).Validate(inst), string(b))
	}

	usage := sdk.SessionNotification{SessionId: "s", Update: sdk.SessionUpdate{UsageUpdate: &sdk.SessionUsageUpdate{SessionUpdate: "usage_update", Size: 1000, Used: 12, Meta: map[string]any{"seq": "18446744073709551615"}}}}
	// usage_update is unstable in the pinned schema: the stable schema rejects it,
	// so Ask reports usage only through its own extension method.
	ub, err := json.Marshal(usage)
	require.NoError(t, err)
	uinst, err := jsonschema.UnmarshalJSON(strings.NewReader(string(ub)))
	require.NoError(t, err)
	require.Error(t, compile("SessionNotification").Validate(uinst), "stable schema must not accept usage_update")
	stable, err := os.ReadFile(filepath.Join(dir, "schema", "schema.json"))
	require.NoError(t, err)
	require.NotContains(t, string(stable), "usage_update")
	unstable, err := os.ReadFile(filepath.Join(dir, "schema", "schema.unstable.json"))
	require.NoError(t, err)
	require.Contains(t, string(unstable), "usage_update")
	validate("SessionNotification", map[string]any{"sessionId": "s", "update": map[string]any{"sessionUpdate": "config_option_update", "configOptions": []any{map[string]any{
		"id": "model", "name": "Model", "type": "select", "currentValue": "faux/faux-1", "options": []any{map[string]any{"value": "faux/faux-1", "name": "faux-1"}}}}}})
	validate("PromptResponse", sdk.PromptResponse{StopReason: sdk.StopReasonCancelled})

	// Unsupported owners answer method-not-found, so no capability is implied.
	p := newPeer(t, &confAgent{}, 1<<20, nil)
	for i, m := range []string{"session/load", "session/set_model", "_ask/session/compact"} {
		p.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":{"sessionId":"s","cwd":"/","mcpServers":[],"modelId":"m"}}`, i+1, m))
		require.Equal(t, -32601, errCode(t, p.next()), m)
	}
}

func TestACPConformanceNoGoroutineLeak(t *testing.T) {
	before := goleak.IgnoreCurrent()
	t.Run("eof", func(t *testing.T) {
		p := newPeer(t, &confAgent{ext: echoExt}, 1<<20, nil)
		_ = p.in.Close()
		p.done("EOF")
	})
	t.Run("blocked writer", func(t *testing.T) {
		block, entered := make(chan error), make(chan struct{}, 1)
		p := newPeer(t, &confAgent{ext: echoExt}, 1<<20, func(io.Writer) io.Writer {
			return writerFunc(func([]byte) (int, error) { entered <- struct{}{}; return 0, <-block })
		})
		p.send(`{"jsonrpc":"2.0","id":1,"method":"_ask/x","params":{}}`)
		<-entered
		block <- io.ErrClosedPipe
		p.done("blocked writer released")
	})
	goleak.VerifyNone(t, before)
}
