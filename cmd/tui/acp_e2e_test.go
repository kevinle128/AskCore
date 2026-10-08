package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"AskCore/internal/testsupport"
)

const e2eWait = 30 * time.Second

var (
	acpBuildOnce sync.Once
	acpBuildDir  string
	acpBuildPath string
	acpBuildErr  error
)

// acpBinary builds the production ask binary once for the whole test run.
func acpBinary(t *testing.T) string {
	t.Helper()
	acpBuildOnce.Do(func() {
		acpBuildDir, acpBuildErr = os.MkdirTemp("", "ask-acp-e2e-")
		if acpBuildErr != nil {
			return
		}
		acpBuildPath = filepath.Join(acpBuildDir, "ask")
		out, err := exec.Command("go", "build", "-o", acpBuildPath, ".").CombinedOutput()
		if err != nil {
			acpBuildErr = fmt.Errorf("go build: %w\n%s", err, out)
		}
	})
	require.NoError(t, acpBuildErr)
	return acpBuildPath
}

// removeACPBinary deletes the binary that acpBinary built.
func removeACPBinary() {
	if acpBuildDir != "" {
		_ = os.RemoveAll(acpBuildDir)
	}
}

type e2eFrame struct {
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

// errKind returns the Ask error kind of an error frame.
func (f e2eFrame) errKind(t *testing.T) string {
	t.Helper()
	require.NotNil(t, f.Error, "expected an error frame, got result %s", f.Result)
	var d struct {
		Kind string `json:"kind"`
	}
	require.NoError(t, json.Unmarshal(f.Error.Data, &d))
	return d.Kind
}

type e2eOptions struct {
	// env adds variables to the minimal environment of the child.
	env map[string]string
	// home is the ASK_HOME of the child. Empty makes a fresh temporary one.
	home string
	// args follow "acp". Used to prove that the command takes none.
	args []string
}

// e2ePeer runs the built binary on real pipes and speaks raw NDJSON to it.
type e2ePeer struct {
	t    *testing.T
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  io.ReadCloser
	home string
	root string
	// partialOK allows a cut last frame, for a test that closes the output on purpose.
	partialOK bool
	// gate stops the reader of stdout while it is locked, so the pipe fills.
	gate    sync.Mutex
	stderr  *lockedBuffer
	exit    chan int
	stopped chan int

	nextID atomic.Int64
	mu     sync.Mutex
	lines  []string
	frames []e2eFrame
	bad    []string
	done   chan struct{}
}

// gatedReader reads the child output only while the test leaves the gate open.
type gatedReader struct {
	p *e2ePeer
	r io.Reader
}

func (g gatedReader) Read(b []byte) (int, error) {
	g.p.gate.Lock()
	g.p.gate.Unlock() //nolint:staticcheck // the lock only waits for a paused reader
	return g.r.Read(b)
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// startE2E starts "ask acp". The child gets a minimal environment, so no key of
// the developer shell can reach it.
func startE2E(t *testing.T, o e2eOptions) *e2ePeer {
	t.Helper()
	bin := acpBinary(t)
	root := t.TempDir()
	home := o.home
	if home == "" {
		home = filepath.Join(root, "ask-home")
		require.NoError(t, os.MkdirAll(home, 0o700))
	}
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + root, "ASK_HOME=" + home, "TZ=Asia/Saigon"}
	for k, v := range o.env {
		env = append(env, k+"="+v)
	}
	cmd := exec.Command(bin, append([]string{"acp"}, o.args...)...)

	cmd.Env = env
	cmd.Dir = root
	in, err := cmd.StdinPipe()
	require.NoError(t, err)
	out, outWriter, err := os.Pipe()
	require.NoError(t, err)
	cmd.Stdout = outWriter
	p := &e2ePeer{t: t, cmd: cmd, in: in, out: out, home: home, root: root, stderr: &lockedBuffer{}, exit: make(chan int, 1), stopped: make(chan int, 1), done: make(chan struct{})}
	cmd.Stderr = p.stderr
	require.NoError(t, cmd.Start())
	require.NoError(t, outWriter.Close())
	t.Cleanup(func() { _ = out.Close() })
	go func() {
		defer close(p.done)
		sc := bufio.NewScanner(gatedReader{p: p, r: out})
		sc.Buffer(make([]byte, 0, 1<<16), 1<<26)
		for sc.Scan() {
			var f e2eFrame
			line := sc.Text()
			p.mu.Lock()
			p.lines = append(p.lines, line)
			if json.Unmarshal([]byte(line), &f) != nil {
				p.bad = append(p.bad, line)
			}
			p.frames = append(p.frames, f)
			p.mu.Unlock()
		}
	}()
	go func() {
		err := cmd.Wait()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
			if code == -1 {
				code = 128 + int(ee.Sys().(syscall.WaitStatus).Signal())
			}
		} else if err != nil {
			code = -2
		}
		p.stopped <- code
		<-p.done
		p.exit <- code
	}()
	t.Cleanup(func() {
		select {
		case code := <-p.exit:
			p.exit <- code
		default:
			// Only this test's own child is stopped.
			_ = cmd.Process.Kill()
			select {
			case code := <-p.exit:
				p.exit <- code
			case <-time.After(e2eWait):
				t.Error("child did not stop")
			}
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.partialOK && len(p.bad) == 1 && p.bad[0] == p.lines[len(p.lines)-1] {
			return // the output closed in the middle of the last frame
		}
		require.Empty(t, p.bad, "every stdout line must be one JSON frame")
	})
	return p
}

func (p *e2ePeer) sendRaw(method string, id any, params any) {
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

func (p *e2ePeer) send(method string, params any) int64 {
	id := p.nextID.Add(1)
	p.sendRaw(method, id, params)
	return id
}

func (p *e2ePeer) notify(method string, params any) { p.sendRaw(method, nil, params) }

func (p *e2ePeer) waitFor(what string, cond func() bool) {
	p.t.Helper()
	deadline := time.Now().Add(e2eWait)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	p.t.Fatalf("timeout: %s\n--- stdout\n%s\n--- stderr\n%s", what, strings.Join(p.snapshot(), "\n"), p.stderr.String())
}

func (p *e2ePeer) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.lines...)
}

func isReply(f e2eFrame, id int64) bool {
	var got int64
	return f.Method == "" && len(f.ID) > 0 && json.Unmarshal(f.ID, &got) == nil && got == id
}

// replyIndex returns the output position of the response of id, or -1.
func (p *e2ePeer) replyIndex(id int64) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, f := range p.frames {
		if isReply(f, id) {
			return i
		}
	}
	return -1
}

func (p *e2ePeer) await(id int64) e2eFrame {
	p.t.Helper()
	p.waitFor(fmt.Sprintf("response %d", id), func() bool { return p.replyIndex(id) >= 0 })
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, f := range p.frames {
		if isReply(f, id) {
			return f
		}
	}
	return e2eFrame{}
}

func (p *e2ePeer) call(method string, params any) e2eFrame {
	p.t.Helper()
	return p.await(p.send(method, params))
}

// ok calls method, requires a result and decodes it into out when given.
func (p *e2ePeer) ok(method string, params any, out any) {
	p.t.Helper()
	f := p.call(method, params)
	require.Nil(p.t, f.Error, "%s failed: %+v", method, f.Error)
	if out != nil {
		require.NoError(p.t, json.Unmarshal(f.Result, out))
	}
}

func (p *e2ePeer) initialize() e2eFrame {
	p.t.Helper()
	f := p.call("initialize", map[string]any{"protocolVersion": 1})
	require.Nil(p.t, f.Error)
	return f
}

func (p *e2ePeer) newSession() string {
	p.t.Helper()
	var res struct {
		SessionID string `json:"sessionId"`
	}
	p.ok("session/new", map[string]any{"cwd": p.t.TempDir(), "mcpServers": []any{}}, &res)
	require.NotEmpty(p.t, res.SessionID)
	return res.SessionID
}

func (p *e2ePeer) start() string {
	p.t.Helper()
	p.initialize()
	return p.newSession()
}

func e2ePrompt(sid, text string) map[string]any {
	return map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": text}}}
}

func e2eInput(sid, text string) map[string]any {
	return map[string]any{"sessionId": sid, "content": []any{map[string]any{"type": "text", "text": text}}}
}

func e2eSession(sid string) map[string]any { return map[string]any{"sessionId": sid} }

// updates returns the session/update frames of one session, in output order.
func (p *e2ePeer) updates(sid string) []e2eFrame {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []e2eFrame
	for _, f := range p.frames {
		if f.Method != "session/update" {
			continue
		}
		var n struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(f.Params, &n)
		if n.SessionID == sid {
			out = append(out, f)
		}
	}
	return out
}

// text joins the text of every agent_message_chunk of a session.
func (p *e2ePeer) text(sid string) string {
	var b strings.Builder
	for _, f := range p.updates(sid) {
		var n struct {
			Update struct {
				SessionUpdate string `json:"sessionUpdate"`
				Content       struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"update"`
		}
		_ = json.Unmarshal(f.Params, &n)
		if n.Update.SessionUpdate == "agent_message_chunk" {
			b.WriteString(n.Update.Content.Text)
		}
	}
	return b.String()
}

// waitExit waits for the child to end and returns its exit code.
func (p *e2ePeer) waitExit() int {
	p.t.Helper()
	select {
	case code := <-p.exit:
		p.exit <- code
		return code
	case <-time.After(e2eWait):
		p.t.Fatalf("child did not exit\n--- stdout\n%s\n--- stderr\n%s", strings.Join(p.snapshot(), "\n"), p.stderr.String())
		return -1
	}
}

func (p *e2ePeer) signal(sig syscall.Signal) {
	p.t.Helper()
	require.NoError(p.t, p.cmd.Process.Signal(sig))
}

// waitRunning waits until the session runs and has sent its first update.
func (p *e2ePeer) waitRunning(sid string) {
	p.t.Helper()
	p.waitFor("first update", func() bool { return len(p.updates(sid)) > 0 })
}

// fastEnv makes the faux model answer at once.
func fastEnv() map[string]string { return map[string]string{"ASK_FAUX_TPS": "0"} }

// busyEnv makes the faux model say a long text slowly, so a run stays open
// until the test cancels it.
func busyEnv() map[string]string { return map[string]string{"ASK_FAUX_TPS": "20"} }

var longPromptText = strings.TrimSpace(strings.Repeat("word ", 600))

func TestACPE2EInitialize(t *testing.T) {
	const sentinel = "SECRET-SENTINEL-8841"
	p := startE2E(t, e2eOptions{})
	// A call before initialize fails with a kind, never with an unmapped text.
	f := p.call("session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}})
	require.Equal(t, "not_initialized", f.errKind(t))

	init := p.call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}})
	require.Nil(t, init.Error)
	var res struct {
		ProtocolVersion   int `json:"protocolVersion"`
		AgentCapabilities struct {
			LoadSession        bool           `json:"loadSession"`
			PromptCapabilities map[string]any `json:"promptCapabilities"`
			Session            map[string]any `json:"sessionCapabilities"`
		} `json:"agentCapabilities"`
		AgentInfo   struct{ Name string } `json:"agentInfo"`
		AuthMethods []struct {
			ID          string `json:"id"`
			Description string `json:"description"`
		} `json:"authMethods"`
	}
	require.NoError(t, json.Unmarshal(init.Result, &res))
	require.Equal(t, 1, res.ProtocolVersion)
	require.Equal(t, "ask", res.AgentInfo.Name)
	require.False(t, res.AgentCapabilities.LoadSession, "no durable load is advertised")
	require.Equal(t, true, res.AgentCapabilities.PromptCapabilities["image"])
	require.Empty(t, res.AgentCapabilities.Session, "no session list, resume or close capability is advertised")
	ids := map[string]bool{}
	for _, m := range res.AuthMethods {
		ids[m.ID] = true
		require.Contains(t, m.Description, "ask auth login")
	}
	for _, want := range []string{"api-key", "anthropic-oauth", "openai-chatgpt", "xai-oauth"} {
		require.True(t, ids[want], want)
	}

	// A newer wire version is answered with version 1.
	p2 := startE2E(t, e2eOptions{})
	var v struct {
		ProtocolVersion int `json:"protocolVersion"`
	}
	p2.ok("initialize", map[string]any{"protocolVersion": 7}, &v)
	require.Equal(t, 1, v.ProtocolVersion)

	// The ID type is kept: a string ID gets a string ID back.
	p.sendRaw("_ask/session/state", "req-α", e2eSession("none"))
	p.waitFor("string ID answer", func() bool {
		for _, l := range p.snapshot() {
			if strings.Contains(l, `"id":"req-α"`) {
				return true
			}
		}
		return false
	})

	// A malformed line is skipped, never echoed, and the connection stays up.
	_, err := p.in.Write([]byte("{not json " + sentinel + "\n"))
	require.NoError(t, err)
	require.Equal(t, "unknown_session", p.call("_ask/session/state", e2eSession("none")).errKind(t))
	require.NotContains(t, p.stderr.String(), sentinel)
	for _, l := range p.snapshot() {
		require.NotContains(t, l, sentinel)
	}

	// Closing stdin ends the process cleanly with nothing on stderr.
	require.NoError(t, p.in.Close())
	require.Equal(t, 0, p.waitExit())
	// The SDK reports the bad line on stderr without its content.
	for _, l := range strings.Split(strings.TrimSpace(p.stderr.String()), "\n") {
		require.True(t, strings.HasPrefix(l, "time="), "diagnostics only: %q", l)
	}
}

func TestACPE2EInitializeOversizeFrame(t *testing.T) {
	p := startE2E(t, e2eOptions{})
	p.initialize()
	chunk := bytes.Repeat([]byte("a"), 1<<20)
	go func() {
		for i := 0; i < 10; i++ {
			if _, err := p.in.Write(chunk); err != nil {
				return
			}
		}
	}()
	require.Equal(t, 1, p.waitExit())
	require.Contains(t, p.stderr.String(), "too long")
}

func TestACPE2ESessionIsolation(t *testing.T) {
	p := startE2E(t, e2eOptions{env: fastEnv()})
	p.initialize()
	require.Equal(t, "invalid_params", p.call("session/new", map[string]any{"cwd": "relative/dir", "mcpServers": []any{}}).errKind(t))
	require.Equal(t, "invalid_params", p.call("session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{map[string]any{"name": "x", "command": "y", "args": []any{}, "env": []any{}}}}).errKind(t))
	a, b := p.newSession(), p.newSession()
	require.NotEqual(t, a, b)

	require.Nil(t, p.call("session/prompt", e2ePrompt(a, "alpha")).Error)
	require.Nil(t, p.call("session/prompt", e2ePrompt(b, "beta")).Error)
	require.Nil(t, p.call("session/prompt", e2ePrompt(b, "gamma")).Error)
	require.Equal(t, "alpha", p.text(a))
	require.Equal(t, "betagamma", p.text(b))

	var sa, sb struct {
		MessageCount int    `json:"messageCount"`
		Epoch        string `json:"epoch"`
	}
	p.ok("_ask/session/state", e2eSession(a), &sa)
	p.ok("_ask/session/state", e2eSession(b), &sb)
	require.Equal(t, 2, sa.MessageCount, "each session has its own log")
	require.Equal(t, 4, sb.MessageCount)
	require.NotEqual(t, sa.Epoch, sb.Epoch)
	require.Equal(t, "unknown_session", p.call("session/prompt", e2ePrompt("sess_missing", "x")).errKind(t))
}

func TestACPE2EPrompt(t *testing.T) {
	p := startE2E(t, e2eOptions{env: busyEnv()})
	sid := p.start()
	id := p.send("session/prompt", e2ePrompt(sid, longPromptText))
	p.waitRunning(sid)

	// A second prompt on the running session is refused at once and the first
	// run is not stopped by it.
	require.Equal(t, "busy", p.call("session/prompt", e2ePrompt(sid, "again")).errKind(t))
	var st struct {
		Running bool `json:"running"`
	}
	p.ok("_ask/session/state", e2eSession(sid), &st)
	require.True(t, st.Running)
	require.Equal(t, -1, p.replyIndex(id), "the busy call must not release the first prompt")

	p.notify("session/cancel", e2eSession(sid))
	r := p.await(id)
	require.Nil(t, r.Error)
	require.Contains(t, string(r.Result), `"cancelled"`)
	require.Greater(t, p.replyIndex(id), p.lastUpdateIndexOf(sid))

	// The session takes the next turn, and the result follows every update.
	p2 := startE2E(t, e2eOptions{env: fastEnv()})
	s2 := p2.start()
	for i, want := range []string{"hello", "hellohello"} {
		f := p2.call("session/prompt", e2ePrompt(s2, "hello"))
		require.Nil(t, f.Error)
		require.Contains(t, string(f.Result), `"end_turn"`)
		require.Equal(t, want, p2.text(s2), "turn %d", i)
		require.Less(t, p2.lastUpdateIndexOf(s2), p2.replyIndexLast())
	}
	// A prompt with an unsupported block is refused with a kind.
	f := p2.call("session/prompt", map[string]any{"sessionId": s2, "prompt": []any{map[string]any{"type": "audio", "data": "x", "mimeType": "audio/wav"}}})
	require.Equal(t, "invalid_content", f.errKind(t))
	f = p2.call("session/prompt", map[string]any{"sessionId": s2, "prompt": []any{}})
	require.Equal(t, "invalid_content", f.errKind(t))
}

// lastUpdateIndexOf returns the output position of the last update of a session.
func (p *e2ePeer) lastUpdateIndexOf(sid string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	last := -1
	for i, f := range p.frames {
		if f.Method != "session/update" {
			continue
		}
		var n struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(f.Params, &n)
		if n.SessionID == sid {
			last = i
		}
	}
	return last
}

// replyIndexLast returns the output position of the last response frame.
func (p *e2ePeer) replyIndexLast() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	last := -1
	for i, f := range p.frames {
		if f.Method == "" && len(f.ID) > 0 {
			last = i
		}
	}
	return last
}

func TestACPE2EContinue(t *testing.T) {
	p := startE2E(t, e2eOptions{env: fastEnv()})
	sid := p.start()
	start := time.Now()
	// An empty log has nothing to continue. The error comes at once, with no
	// event of any run.
	require.Equal(t, "invalid_state", p.call("_ask/session/continue", e2eSession(sid)).errKind(t))
	require.Less(t, time.Since(start), 2*time.Second)
	require.Empty(t, p.updates(sid))

	// A log that ends with an assistant message cannot continue either.
	require.Nil(t, p.call("session/prompt", e2ePrompt(sid, "hi")).Error)
	before := len(p.updates(sid))
	require.Equal(t, "invalid_state", p.call("_ask/session/continue", e2eSession(sid)).errKind(t))
	require.Len(t, p.updates(sid), before, "a refused continue emits no event")

	// The next prompt is not affected by the refusals.
	f := p.call("session/prompt", e2ePrompt(sid, "again"))
	require.Nil(t, f.Error)
	require.Equal(t, "hiagain", p.text(sid))
	require.Equal(t, "unknown_session", p.call("_ask/session/continue", e2eSession("sess_missing")).errKind(t))
}

func TestACPE2ECancel(t *testing.T) {
	p := startE2E(t, e2eOptions{env: busyEnv()})
	sid := p.start()
	// A cancel with nothing to cancel, or for an unknown session, is harmless.
	p.notify("session/cancel", e2eSession(sid))
	p.notify("session/cancel", e2eSession("sess_missing"))
	id := p.send("session/prompt", e2ePrompt(sid, longPromptText))
	p.waitRunning(sid)
	p.notify("session/cancel", e2eSession(sid))
	r := p.await(id)
	require.Nil(t, r.Error)
	require.Contains(t, string(r.Result), `"cancelled"`)
	require.Greater(t, p.replyIndex(id), p.lastUpdateIndexOf(sid), "the result follows every pending update")

	// The cancelled session takes the next prompt.
	var st struct {
		Running bool `json:"running"`
	}
	p.ok("_ask/session/state", e2eSession(sid), &st)
	require.False(t, st.Running)
}

func TestACPE2ERequestCancel(t *testing.T) {
	p := startE2E(t, e2eOptions{env: busyEnv()})
	p.initialize()
	a, b := p.newSession(), p.newSession()
	// A cancelled request is not a cancelled run: both ID types are tried.
	p.sendRaw("session/prompt", "req-a", e2ePrompt(a, longPromptText))
	p.sendRaw("session/prompt", 4242, e2ePrompt(b, longPromptText))
	p.waitRunning(a)
	p.waitRunning(b)
	p.notify("$/cancel_request", map[string]any{"requestId": "req-a"})
	p.notify("$/cancel_request", map[string]any{"requestId": 4242})
	time.Sleep(300 * time.Millisecond)
	for _, sid := range []string{a, b} {
		var st struct {
			Running bool `json:"running"`
		}
		p.ok("_ask/session/state", e2eSession(sid), &st)
		require.True(t, st.Running, "a request cancel must not stop the run")
	}
	// The run ends with session/cancel only, and then both results come.
	p.notify("session/cancel", e2eSession(a))
	p.notify("session/cancel", e2eSession(b))
	p.waitFor("both prompt answers", func() bool {
		n := 0
		for _, l := range p.snapshot() {
			if strings.Contains(l, `"stopReason":"cancelled"`) || strings.Contains(l, `"code":-32800`) {
				n++
			}
		}
		return n == 2
	})
}

func TestACPE2EState(t *testing.T) {
	p := startE2E(t, e2eOptions{env: busyEnv()})
	sid := p.start()
	type state struct {
		SessionID     string   `json:"sessionId"`
		Epoch         string   `json:"epoch"`
		Running       bool     `json:"running"`
		ModelID       string   `json:"modelId"`
		ThinkingLevel string   `json:"thinkingLevel"`
		MessageCount  int      `json:"messageCount"`
		Steering      []string `json:"steering"`
		FollowUp      []string `json:"followUp"`
	}
	var idle state
	p.ok("_ask/session/state", e2eSession(sid), &idle)
	require.Equal(t, state{SessionID: sid, Epoch: idle.Epoch, ModelID: "faux/faux-1@faux", ThinkingLevel: "off", Steering: []string{}, FollowUp: []string{}}, idle)
	require.NotEmpty(t, idle.Epoch)

	id := p.send("session/prompt", e2ePrompt(sid, longPromptText))
	p.waitRunning(sid)
	var running state
	p.ok("_ask/session/state", e2eSession(sid), &running)
	require.True(t, running.Running)
	require.Equal(t, idle.Epoch, running.Epoch)
	require.Equal(t, 1, running.MessageCount)
	p.notify("session/cancel", e2eSession(sid))
	p.await(id)
	var after state
	p.ok("_ask/session/state", e2eSession(sid), &after)
	require.False(t, after.Running)
	require.Equal(t, 2, after.MessageCount)
	require.Equal(t, "unknown_session", p.call("_ask/session/state", e2eSession("sess_missing")).errKind(t))
	require.Equal(t, "invalid_params", p.call("_ask/session/state", "not an object").errKind(t))
}

func TestACPE2EReset(t *testing.T) {
	for _, withFollow := range []bool{false, true} {
		t.Run(fmt.Sprintf("follow=%v", withFollow), func(t *testing.T) {
			p := startE2E(t, e2eOptions{env: fastEnv()})
			sid := p.start()
			require.Nil(t, p.call("session/prompt", e2ePrompt(sid, "one")).Error)
			var old struct {
				Cursor struct {
					Epoch string `json:"epoch"`
					Seq   string `json:"seq"`
				} `json:"cursor"`
				SubscriptionID string `json:"subscriptionId"`
			}
			if withFollow {
				p.ok("_ask/session/follow", e2eSession(sid), &old)
			} else {
				var st struct {
					Epoch string `json:"epoch"`
				}
				p.ok("_ask/session/state", e2eSession(sid), &st)
				old.Cursor.Epoch, old.Cursor.Seq = st.Epoch, "1"
			}
			var reset struct {
				SessionID string `json:"sessionId"`
				Epoch     string `json:"epoch"`
			}
			p.ok("_ask/session/reset", e2eSession(sid), &reset)
			require.Equal(t, sid, reset.SessionID, "reset keeps the session")
			require.NotEqual(t, old.Cursor.Epoch, reset.Epoch)

			// The next prompt completes at once and its frames name the new epoch.
			before := len(p.updates(sid))
			f := p.call("session/prompt", e2ePrompt(sid, "two"))
			require.Nil(t, f.Error)
			require.Contains(t, string(f.Result), `"end_turn"`)
			fresh := p.updates(sid)[before:]
			require.NotEmpty(t, fresh)
			for _, u := range fresh {
				require.Contains(t, string(u.Params), `"epoch":"`+reset.Epoch+`"`)
			}
			var st struct {
				MessageCount int `json:"messageCount"`
			}
			p.ok("_ask/session/state", e2eSession(sid), &st)
			require.Equal(t, 2, st.MessageCount, "the old log is gone")

			// A cursor of the old epoch gets a resync, not a silent gap.
			var res struct {
				Cursor struct {
					Epoch string `json:"epoch"`
				} `json:"cursor"`
				Resync bool `json:"resync"`
			}
			p.ok("_ask/session/follow", map[string]any{"sessionId": sid, "cursor": map[string]any{"epoch": old.Cursor.Epoch, "seq": old.Cursor.Seq}}, &res)
			require.True(t, res.Resync)
			require.Equal(t, reset.Epoch, res.Cursor.Epoch)
		})
	}
	p := startE2E(t, e2eOptions{env: busyEnv()})
	sid := p.start()
	id := p.send("session/prompt", e2ePrompt(sid, longPromptText))
	p.waitRunning(sid)
	require.Equal(t, "busy", p.call("_ask/session/reset", e2eSession(sid)).errKind(t))
	p.notify("session/cancel", e2eSession(sid))
	p.await(id)
}

func TestACPE2ECatalog(t *testing.T) {
	p := startE2E(t, e2eOptions{})
	sid := p.start()
	var res struct {
		Current string `json:"current"`
		Models  []struct {
			ModelID  string `json:"modelId"`
			Provider string `json:"provider"`
			ID       string `json:"id"`
			API      string `json:"api"`
		} `json:"models"`
	}
	p.ok("_ask/session/get_available_models", e2eSession(sid), &res)
	require.Equal(t, "faux/faux-1@faux", res.Current)
	require.Equal(t, "faux/faux-1@faux", res.Models[0].ModelID, "the initial model is listed first")
	seen := map[string]bool{}
	tokenPlan := 0
	for _, m := range res.Models {
		require.False(t, seen[m.ModelID], "model IDs are unique: %s", m.ModelID)
		seen[m.ModelID] = true
		require.Equal(t, m.Provider+"/"+m.ID+"@"+m.API, m.ModelID, "the name reverses to one row")
		if m.Provider == "alibaba-token-plan" {
			tokenPlan++
		}
	}
	require.Equal(t, 2, tokenPlan, "two rows share a provider and an ID and differ by API")
	require.True(t, seen["openai/gpt-5.5@openai-responses"])
	require.Equal(t, "unknown_session", p.call("_ask/session/get_available_models", e2eSession("sess_missing")).errKind(t))
}

func TestACPE2EThinking(t *testing.T) {
	p := startE2E(t, e2eOptions{})
	sid := p.start()
	var lv struct {
		Level string `json:"level"`
	}
	// The faux model has no reasoning, so every level is clamped to off.
	p.ok("_ask/session/set_thinking", map[string]any{"sessionId": sid, "level": "high"}, &lv)
	require.Equal(t, "off", lv.Level)
	require.Equal(t, "invalid_params", p.call("_ask/session/set_thinking", map[string]any{"sessionId": sid, "level": "extreme"}).errKind(t))
	require.Equal(t, "unknown_session", p.call("_ask/session/set_thinking", map[string]any{"sessionId": "sess_missing", "level": "high"}).errKind(t))
}

func TestACPE2ESteer(t *testing.T) {
	p := startE2E(t, e2eOptions{env: busyEnv()})
	sid := p.start()
	id := p.send("session/prompt", e2ePrompt(sid, longPromptText))
	p.waitRunning(sid)
	var a, b struct {
		InputID string `json:"inputId"`
	}
	p.ok("_ask/session/steer", e2eInput(sid, "turn left"), &a)
	p.ok("_ask/session/steer", e2eInput(sid, "turn right"), &b)
	require.NotEmpty(t, a.InputID)
	require.NotEqual(t, a.InputID, b.InputID)
	require.Equal(t, -1, p.replyIndex(id), "an input ID is not the prompt result")
	var st struct {
		Steering []string `json:"steering"`
	}
	p.ok("_ask/session/state", e2eSession(sid), &st)
	require.Equal(t, []string{"turn left", "turn right"}, st.Steering)

	require.Equal(t, "invalid_content", p.call("_ask/session/steer", map[string]any{"sessionId": sid, "content": []any{}}).errKind(t))
	require.Equal(t, "invalid_content", p.call("_ask/session/steer", e2eInput(sid, "")).errKind(t))
	p.notify("session/cancel", e2eSession(sid))
	p.await(id)
}

func TestACPE2EFollowUp(t *testing.T) {
	p := startE2E(t, e2eOptions{env: busyEnv()})
	sid := p.start()
	id := p.send("session/prompt", e2ePrompt(sid, longPromptText))
	p.waitRunning(sid)
	var a, b struct {
		InputID string `json:"inputId"`
	}
	p.ok("_ask/session/follow_up", e2eInput(sid, "next one"), &a)
	p.ok("_ask/session/follow_up", e2eInput(sid, "next two"), &b)
	require.NotEqual(t, a.InputID, b.InputID)
	var st struct {
		FollowUp []string `json:"followUp"`
		Steering []string `json:"steering"`
	}
	p.ok("_ask/session/state", e2eSession(sid), &st)
	require.Equal(t, []string{"next one", "next two"}, st.FollowUp, "follow-ups wait for the next cycle")
	require.Empty(t, st.Steering)
	p.notify("session/cancel", e2eSession(sid))
	p.await(id)

	// An idle session accepts a follow-up as the start of a new cycle.
	q := startE2E(t, e2eOptions{env: fastEnv()})
	s2 := q.start()
	var c struct {
		InputID string `json:"inputId"`
	}
	q.ok("_ask/session/follow_up", e2eInput(s2, "wake up"), &c)
	require.NotEmpty(t, c.InputID)
	q.waitFor("the woken cycle says the text", func() bool { return q.text(s2) == "wake up" })
}

func TestACPE2ERemove(t *testing.T) {
	p := startE2E(t, e2eOptions{env: busyEnv()})
	sid := p.start()
	id := p.send("session/prompt", e2ePrompt(sid, longPromptText))
	p.waitRunning(sid)
	var a struct {
		InputID string `json:"inputId"`
	}
	p.ok("_ask/session/follow_up", e2eInput(sid, "later"), &a)
	var rm struct {
		Removed bool `json:"removed"`
	}
	p.ok("_ask/session/remove", map[string]any{"sessionId": sid, "inputId": a.InputID}, &rm)
	require.True(t, rm.Removed, "an input is removable before it is claimed")
	p.ok("_ask/session/remove", map[string]any{"sessionId": sid, "inputId": a.InputID}, &rm)
	require.False(t, rm.Removed, "the second remove finds nothing")
	p.ok("_ask/session/remove", map[string]any{"sessionId": sid, "inputId": "in-missing"}, &rm)
	require.False(t, rm.Removed)
	var st struct {
		FollowUp []string `json:"followUp"`
	}
	p.ok("_ask/session/state", e2eSession(sid), &st)
	require.Empty(t, st.FollowUp)

	p.notify("session/cancel", e2eSession(sid))
	p.await(id)

	// A steering input on an idle session starts a run that claims it at once,
	// so it can no longer be removed.
	q := startE2E(t, e2eOptions{env: fastEnv()})
	s2 := q.start()
	var c struct {
		InputID string `json:"inputId"`
	}
	q.ok("_ask/session/steer", e2eInput(s2, "claimed"), &c)
	q.waitFor("the run says the text", func() bool { return q.text(s2) == "claimed" })
	q.ok("_ask/session/remove", map[string]any{"sessionId": s2, "inputId": c.InputID}, &rm)
	require.False(t, rm.Removed, "a claimed input is not removable")
	require.Equal(t, "unknown_session", p.call("_ask/session/remove", map[string]any{"sessionId": "sess_missing", "inputId": "x"}).errKind(t))
}

// askAuth runs the built "ask auth" command on the given home with input on stdin.
func askAuth(t *testing.T, home, input string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(acpBinary(t), append([]string{"auth"}, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "ASK_HOME=" + home}
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// loginKey saves an API key through the supported host login.
func loginKey(t *testing.T, home, provider, key string) {
	t.Helper()
	out, err := askAuth(t, home, key+"\n", "login", "--provider", provider, "--method", "api-key")
	require.NoError(t, err, out)
	require.NotContains(t, out, key)
}

func fileHash(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

// noSecret fails when a secret appears in any stdout frame or on stderr.
func (p *e2ePeer) noSecret(secrets ...string) {
	p.t.Helper()
	all := strings.Join(p.snapshot(), "\n") + "\n" + p.stderr.String()
	for _, s := range secrets {
		require.NotContains(p.t, all, s)
	}
}

func TestACPE2EModels(t *testing.T) {
	const key = "sk-sentinel-openai-0001"
	home := filepath.Join(t.TempDir(), "ask-home")
	loginKey(t, home, "openai", key)
	authFile := filepath.Join(home, "auth.json")
	info, err := os.Stat(authFile)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	p := startE2E(t, e2eOptions{home: home})
	sid := p.start()
	current := func() string {
		var st struct {
			ModelID string `json:"modelId"`
		}
		p.ok("_ask/session/state", e2eSession(sid), &st)
		return st.ModelID
	}
	set := func(model, method string) e2eFrame {
		params := map[string]any{"sessionId": sid, "modelId": model}
		if method != "" {
			params["authMethodId"] = method
		}
		return p.call("_ask/session/set_model", params)
	}
	before := fileHash(t, authFile)

	// Names that do not give one row are refused and change nothing.
	for _, bad := range []string{"alibaba-token-plan/deepseek-v4.1-flash", "nope/none@x", "faux", "/x", "openai/@"} {
		require.Equal(t, "invalid_model", set(bad, "").errKind(t), bad)
	}
	// A model without a credential is refused and the model stays.
	require.Equal(t, "no_api_key", set("anthropic/claude-sonnet-4-6@anthropic-messages", "").errKind(t))
	require.Equal(t, "faux/faux-1@faux", current())
	// The requested method must be the configured one, in both directions.
	require.Equal(t, "no_api_key", set("openai/gpt-5.5@openai-responses", "openai-chatgpt").errKind(t))
	require.Equal(t, "no_api_key", set("faux/faux-1@faux", "api-key").errKind(t))
	require.Equal(t, "faux/faux-1@faux", current())
	require.Equal(t, before, fileHash(t, authFile), "a refused switch must not change the credential")

	// The ready model switches, with or without the matching method.
	var res struct {
		ModelID       string `json:"modelId"`
		ThinkingLevel string `json:"thinkingLevel"`
	}
	p.ok("_ask/session/set_model", map[string]any{"sessionId": sid, "modelId": "openai/gpt-5.5@openai-responses", "authMethodId": "api-key"}, &res)
	require.Equal(t, "openai/gpt-5.5@openai-responses", res.ModelID)
	require.Equal(t, "openai/gpt-5.5@openai-responses", current())
	var lv struct {
		Level string `json:"level"`
	}
	p.ok("_ask/session/set_thinking", map[string]any{"sessionId": sid, "level": "xhigh"}, &lv)
	require.NotEmpty(t, lv.Level, "a reasoning model reports the clamped level")
	var back struct {
		ModelID string `json:"modelId"`
	}
	p.ok("_ask/session/set_model", map[string]any{"sessionId": sid, "modelId": "faux/faux-1@faux"}, &back)
	require.Equal(t, "faux/faux-1@faux", back.ModelID)
	require.Equal(t, before, fileHash(t, authFile))
	p.noSecret(key)
}

func TestACPE2EAuth(t *testing.T) {
	const key = "sk-sentinel-auth-0002"
	home := filepath.Join(t.TempDir(), "ask-home")
	require.NoError(t, os.MkdirAll(home, 0o700))
	p := startE2E(t, e2eOptions{home: home})
	require.Equal(t, "not_initialized", p.call("authenticate", map[string]any{"methodId": "api-key"}).errKind(t))
	p.initialize()
	require.Equal(t, "invalid_params", p.call("authenticate", map[string]any{"methodId": "magic"}).errKind(t))
	f := p.call("authenticate", map[string]any{"methodId": "api-key"})
	require.Equal(t, "no_api_key", f.errKind(t))
	require.Contains(t, f.Error.Message, "ask auth", "the error names the host command")
	// The connection never reads a secret: the next request still works.
	require.Nil(t, p.call("session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}}).Error)

	loginKey(t, home, "openai", key)
	before := fileHash(t, filepath.Join(home, "auth.json"))
	require.Nil(t, p.call("authenticate", map[string]any{"methodId": "api-key"}).Error)
	require.Equal(t, "no_api_key", p.call("authenticate", map[string]any{"methodId": "openai-chatgpt"}).errKind(t))
	require.Equal(t, "no_api_key", p.call("authenticate", map[string]any{"methodId": "xai-oauth"}).errKind(t))
	require.Equal(t, before, fileHash(t, filepath.Join(home, "auth.json")), "authenticate never changes a credential")

	// An environment key counts as a configured API key.
	q := startE2E(t, e2eOptions{env: map[string]string{"ANTHROPIC_API_KEY": "sk-env-sentinel-0003"}})
	q.initialize()
	require.Nil(t, q.call("authenticate", map[string]any{"methodId": "api-key"}).Error)
	require.Equal(t, "no_api_key", q.call("authenticate", map[string]any{"methodId": "anthropic-oauth"}).errKind(t))
	p.noSecret(key)
	q.noSecret("sk-env-sentinel-0003")
}

type followResult struct {
	SubscriptionID string `json:"subscriptionId"`
	Cursor         struct {
		Epoch string `json:"epoch"`
		Seq   string `json:"seq"`
	} `json:"cursor"`
	Entries []json.RawMessage `json:"entries"`
	Stream  *struct {
		AttemptID string         `json:"attemptId"`
		Open      map[string]any `json:"open"`
	} `json:"stream"`
	Resumed bool `json:"resumed"`
	Resync  bool `json:"resync"`
}

// events returns the _ask/session/event frames of one subscription, in output order.
func (p *e2ePeer) events(sub string) []e2eFrame {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []e2eFrame
	for _, f := range p.frames {
		if f.Method != "_ask/session/event" {
			continue
		}
		var n struct {
			SubscriptionID string `json:"subscriptionId"`
		}
		_ = json.Unmarshal(f.Params, &n)
		if n.SubscriptionID == sub {
			out = append(out, f)
		}
	}
	return out
}

func TestACPE2EFollow(t *testing.T) {
	p := startE2E(t, e2eOptions{env: busyEnv()})
	sid := p.start()
	id := p.send("session/prompt", e2ePrompt(sid, longPromptText))
	p.waitRunning(sid)

	// A follow in the middle of an open block returns the cut: the cursor, the
	// committed entries and the baseline of the open stream.
	var res followResult
	p.ok("_ask/session/follow", e2eSession(sid), &res)
	require.NotEmpty(t, res.SubscriptionID)
	require.NotEmpty(t, res.Cursor.Epoch)
	require.NotEmpty(t, res.Entries)
	require.NotNil(t, res.Stream, "a stream is open")
	require.NotEmpty(t, res.Stream.AttemptID)
	require.NotEmpty(t, res.Stream.Open)
	require.False(t, res.Resync)
	resultAt := p.replyIndex(p.nextID.Load())

	p.notify("session/cancel", e2eSession(sid))
	p.await(id)
	p.waitFor("events after the cut", func() bool { return len(p.events(res.SubscriptionID)) >= 3 })
	cut, err := strconv.ParseUint(res.Cursor.Seq, 10, 64)
	require.NoError(t, err)
	last := cut
	for _, f := range p.events(res.SubscriptionID) {
		var n struct {
			SessionID string `json:"sessionId"`
			Epoch     string `json:"epoch"`
			Seq       string `json:"seq"`
		}
		require.NoError(t, json.Unmarshal(f.Params, &n))
		require.Equal(t, sid, n.SessionID)
		require.Equal(t, res.Cursor.Epoch, n.Epoch)
		seq, err := strconv.ParseUint(n.Seq, 10, 64)
		require.NoError(t, err)
		require.Equal(t, last+1, seq, "events follow the cut with no gap and no repeat")
		last = seq
	}
	require.Greater(t, p.firstEventIndex(res.SubscriptionID), resultAt, "events leave after the follow result")

	// A cursor of a stale epoch gets a resync with the current cut.
	var stale followResult
	p.ok("_ask/session/follow", map[string]any{"sessionId": sid, "cursor": map[string]any{"epoch": "0000000000000000", "seq": "3"}}, &stale)
	require.True(t, stale.Resync)
	require.Equal(t, res.Cursor.Epoch, stale.Cursor.Epoch)
	// A cursor that is current resumes.
	var cur followResult
	p.ok("_ask/session/follow", map[string]any{"sessionId": sid, "cursor": map[string]any{"epoch": res.Cursor.Epoch, "seq": strconv.FormatUint(last, 10)}}, &cur)
	require.True(t, cur.Resumed)
	require.False(t, cur.Resync)
	require.Equal(t, "invalid_params", p.call("_ask/session/follow", map[string]any{"sessionId": sid, "cursor": map[string]any{"epoch": "x", "seq": "-1"}}).errKind(t))
	require.Equal(t, "unknown_session", p.call("_ask/session/follow", e2eSession("sess_missing")).errKind(t))
}

func (p *e2ePeer) firstEventIndex(sub string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, f := range p.frames {
		if f.Method != "_ask/session/event" {
			continue
		}
		var n struct {
			SubscriptionID string `json:"subscriptionId"`
		}
		_ = json.Unmarshal(f.Params, &n)
		if n.SubscriptionID == sub {
			return i
		}
	}
	return -1
}

func TestACPE2EUnfollow(t *testing.T) {
	p := startE2E(t, e2eOptions{env: busyEnv()})
	sid := p.start()
	var res followResult
	p.ok("_ask/session/follow", e2eSession(sid), &res)
	id := p.send("session/prompt", e2ePrompt(sid, longPromptText))
	p.waitFor("the follower gets events", func() bool { return len(p.events(res.SubscriptionID)) >= 2 })

	var ok map[string]any
	p.ok("_ask/session/unfollow", map[string]any{"sessionId": sid, "subscriptionId": res.SubscriptionID}, &ok)
	n := len(p.events(res.SubscriptionID))
	// The prompt barrier is unaffected: the run still ends with its result after
	// the standard updates, and the stopped subscription gets nothing more.
	p.notify("session/cancel", e2eSession(sid))
	r := p.await(id)
	require.Contains(t, string(r.Result), `"cancelled"`)
	require.Greater(t, p.replyIndex(id), p.lastUpdateIndexOf(sid))
	time.Sleep(200 * time.Millisecond)
	require.LessOrEqual(t, len(p.events(res.SubscriptionID)), n+1, "at most one event that was already in flight")
	p.ok("_ask/session/unfollow", map[string]any{"sessionId": sid, "subscriptionId": res.SubscriptionID}, nil)
}

func TestACPE2EUnsupported(t *testing.T) {
	p := startE2E(t, e2eOptions{})
	sid := p.start()
	for _, m := range []struct {
		method string
		params any
		kind   string
		code   int
	}{
		{"session/load", map[string]any{"sessionId": sid, "cwd": t.TempDir(), "mcpServers": []any{}}, "", -32601},
		{"session/list", map[string]any{}, "unsupported", -32601},
		{"session/resume", map[string]any{"sessionId": sid, "cwd": t.TempDir()}, "unsupported", -32601},
		{"session/close", map[string]any{"sessionId": sid}, "unsupported", -32601},
		{"session/set_mode", map[string]any{"sessionId": sid, "modeId": "x"}, "unsupported", -32601},
		{"session/set_config_option", map[string]any{"sessionId": sid, "configId": "x", "value": "y"}, "unsupported", -32601},
		{"logout", map[string]any{}, "unsupported", -32601},
		{"_ask/session/compact", e2eSession(sid), "unsupported", 0},
		{"_ask/session/fork", e2eSession(sid), "unsupported", 0},
		{"_ask/session/tree", e2eSession(sid), "unsupported", 0},
		{"_ask/session/nothing", e2eSession(sid), "", -32601},
		{"made/up", map[string]any{}, "", -32601},
	} {
		f := p.call(m.method, m.params)
		require.NotNil(t, f.Error, "%s must not return a result: %s", m.method, f.Result)
		require.Empty(t, f.Result, m.method)
		if m.kind != "" {
			require.Equal(t, m.kind, f.errKind(t), m.method)
		}
		if m.code != 0 {
			require.Equal(t, m.code, f.Error.Code, m.method)
		}
	}
	// The session is intact after the refusals.
	var st struct {
		SessionID string `json:"sessionId"`
	}
	p.ok("_ask/session/state", e2eSession(sid), &st)
	require.Equal(t, sid, st.SessionID)
}

// quietStderr requires that stderr holds connection notices only: no error, no panic.
func quietStderr(t *testing.T, p *e2ePeer) {
	t.Helper()
	for _, l := range strings.Split(strings.TrimSpace(p.stderr.String()), "\n") {
		if l == "" {
			continue
		}
		require.True(t, strings.HasPrefix(l, "time="), "diagnostics only: %q", l)
		require.Contains(t, l, "level=INFO", l)
	}
}

func TestACPE2EShutdown(t *testing.T) {
	// startRunning opens one session with a prompt that stays open.
	startRunning := func(t *testing.T, env map[string]string) (*e2ePeer, int64) {
		t.Helper()
		p := startE2E(t, e2eOptions{env: env})
		sid := p.start()
		id := p.send("session/prompt", e2ePrompt(sid, longPromptText))
		p.waitRunning(sid)
		return p, id
	}
	// noSuccess fails when the prompt result of a shut-down run reports success.
	noSuccess := func(t *testing.T, p *e2ePeer, id int64) {
		t.Helper()
		if i := p.replyIndex(id); i >= 0 {
			require.NotContains(t, p.snapshot()[i], "end_turn")
		}
	}

	t.Run("EOF while idle", func(t *testing.T) {
		p := startE2E(t, e2eOptions{})
		p.start()
		require.NoError(t, p.in.Close())
		require.Equal(t, 0, p.waitExit())
		quietStderr(t, p)
	})

	t.Run("EOF while a run is open", func(t *testing.T) {
		p, id := startRunning(t, busyEnv())
		require.NoError(t, p.in.Close())
		require.Equal(t, 0, p.waitExit())
		noSuccess(t, p, id)
		quietStderr(t, p)
	})

	for _, tc := range []struct {
		name string
		sig  syscall.Signal
		code int
	}{{"SIGINT", syscall.SIGINT, 130}, {"SIGTERM", syscall.SIGTERM, 143}, {"SIGHUP", syscall.SIGHUP, 129}} {
		t.Run(tc.name, func(t *testing.T) {
			p, id := startRunning(t, busyEnv())
			p.signal(tc.sig)
			require.Equal(t, tc.code, p.waitExit())
			require.Equal(t, -1, p.replyIndex(id), "no prompt result may follow a signal")
			require.NotContains(t, p.stderr.String(), "panic")
		})
	}

	t.Run("signal while idle", func(t *testing.T) {
		p := startE2E(t, e2eOptions{})
		p.start()
		p.signal(syscall.SIGTERM)
		require.Equal(t, 143, p.waitExit())
	})

	t.Run("output pipe closed", func(t *testing.T) {
		p, id := startRunning(t, busyEnv())
		require.NoError(t, p.out.Close())
		require.Equal(t, 1, p.waitExit())
		require.Contains(t, p.stderr.String(), "output failed")
		require.Equal(t, -1, p.replyIndex(id))
	})

	t.Run("signal while the output is stalled", func(t *testing.T) {
		p := startE2E(t, e2eOptions{env: busyEnv()})
		sid := p.start()
		// The log holds one large message, so the follow result is one frame far
		// larger than the pipe buffer.
		id := p.send("session/prompt", e2ePrompt(sid, strings.Repeat("x", 600_000)))
		p.waitRunning(sid)
		p.notify("session/cancel", e2eSession(sid))
		p.await(id)
		// The peer stops reading, then asks for that frame: the child blocks
		// inside one write.
		p.gate.Lock()
		unlocked := false
		unlock := func() {
			if !unlocked {
				unlocked = true
				p.gate.Unlock()
			}
		}
		t.Cleanup(unlock)
		p.partialOK = true
		follow := p.send("_ask/session/follow", e2eSession(sid))
		time.Sleep(700 * time.Millisecond)
		require.Equal(t, -1, p.replyIndex(follow))
		p.signal(syscall.SIGTERM)
		select {
		case code := <-p.stopped:
			require.Equal(t, 143, code, "the child exits while stdout remains unread")
		case <-time.After(e2eWait):
			t.Fatal("the child required stdout reading to stop")
		}
		unlock()
		require.Equal(t, 143, p.waitExit(), "a blocked write must not hold the shutdown")
		require.NotContains(t, p.stderr.String(), "forced exit", "one signal is enough")
	})
}

func TestACPE2ENoListener(t *testing.T) {
	lsof, err := exec.LookPath("lsof")
	if err != nil {
		t.Skip("lsof is not installed")
	}
	p := startE2E(t, e2eOptions{env: busyEnv()})
	sid := p.start()
	id := p.send("session/prompt", e2ePrompt(sid, longPromptText))
	p.waitRunning(sid)
	var res followResult
	p.ok("_ask/session/follow", e2eSession(sid), &res)

	out, err := exec.Command(lsof, "-p", fmt.Sprint(p.cmd.Process.Pid), "-n", "-P", "-F", "t").Output()
	require.NoError(t, err, "lsof must list the child")
	types := map[string]bool{}
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "t") {
			types[l[1:]] = true
		}
	}
	require.NotEmpty(t, types)
	for _, bad := range []string{"IPv4", "IPv6", "unix", "sock"} {
		require.False(t, types[bad], "the child holds no %s file: %v", bad, types)
	}

	p.notify("session/cancel", e2eSession(sid))
	p.await(id)
	require.NoError(t, p.in.Close())
	require.Equal(t, 0, p.waitExit())
	// No database or socket file appears in the home or the working directory.
	require.NoError(t, filepath.WalkDir(p.root, func(path string, d os.DirEntry, err error) error {
		require.NoError(t, err)
		name := strings.ToLower(d.Name())
		for _, suffix := range []string{".db", ".sqlite", ".sqlite3", "-wal", "-shm", ".sock"} {
			require.False(t, strings.HasSuffix(name, suffix), "unexpected file %s", path)
		}
		require.Zero(t, d.Type()&os.ModeSocket, "unexpected socket %s", path)
		return nil
	}))
}

func TestACPE2EPromptIncludesFollowCompletion(t *testing.T) {
	p := startE2E(t, e2eOptions{})
	sid := p.start()
	var sub followResult
	p.ok("_ask/session/follow", e2eSession(sid), &sub)
	for range 20 {
		id := p.send("session/prompt", e2ePrompt(sid, "hi"))
		r := p.await(id)
		require.Nil(t, r.Error)
		response := p.replyIndex(id)
		frames := p.snapshot()
		settled := -1
		for i := response - 1; i >= 0; i-- {
			if strings.Contains(frames[i], `"type":"agent_settled"`) {
				settled = i
				break
			}
			if strings.Contains(frames[i], `"type":"agent_start"`) {
				break
			}
		}
		require.NotEqual(t, -1, settled, "prompt response overtook the followed settled event: %v", frames)
	}
}

func TestACPE2EShutdownNullID(t *testing.T) {
	p := startE2E(t, e2eOptions{})
	p.start()
	_, err := io.WriteString(p.in, `{"jsonrpc":"2.0","id":null,"method":"_ask/session/state","params":{"sessionId":"missing"}}`+"\n")
	require.NoError(t, err)
	require.NoError(t, p.in.Close())
	select {
	case code := <-p.exit:
		p.exit <- code
		require.Zero(t, code)
	case <-time.After(2 * time.Second):
		t.Fatal("EOF waits for a response to a null-ID notification")
	}
}

func TestACPE2EEOFMalformedEnvelope(t *testing.T) {
	for _, raw := range []string{
		`{"jsonrpc":2,"id":1,"method":"initialize","params":{"protocolVersion":1}}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","error":"bad"}`,
	} {
		t.Run(raw, func(t *testing.T) {
			p := startE2E(t, e2eOptions{})
			_, err := io.WriteString(p.in, raw+"\n")
			require.NoError(t, err)
			require.NoError(t, p.in.Close())
			select {
			case code := <-p.exit:
				p.exit <- code
				require.Zero(t, code)
			case <-time.After(2 * time.Second):
				t.Fatal("EOF waited for a rejected envelope")
			}
		})
	}
}

func TestACPE2EEOFDeliversUnterminatedRequest(t *testing.T) {
	p := startE2E(t, e2eOptions{})
	sid := p.start()
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 999, "method": "_ask/session/state", "params": e2eSession(sid)})
	require.NoError(t, err)
	_, err = p.in.Write(append([]byte("  "), raw...))
	require.NoError(t, err)
	require.NoError(t, p.in.Close())
	require.Zero(t, p.waitExit())
	require.GreaterOrEqual(t, p.replyIndex(999), 0, "EOF lost a request without a trailing newline")
}

// providerFixture is an external provider and OAuth service. The test process
// serves HTTPS behind a CONNECT proxy. The production child uses its normal
// HTTP clients and verifies the fixture certificate with a temporary CA file.
type providerFixture struct {
	t        *testing.T
	server   *httptest.Server
	certFile string
	mu       sync.Mutex
	// inference answers the n-th inference request (from 0).
	inference func(n int, r *http.Request, body []byte) (status int, contentType, body2 string)
	// token answers the OAuth token endpoint.
	token func(grant map[string]string) (int, any)

	inferenceCalls int
	tokenGrants    []map[string]string
	authHeaders    []string
}

func newProviderFixture(t *testing.T) *providerFixture {
	t.Helper()
	f := &providerFixture{t: t}
	backend := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		switch "https://" + r.Host + r.URL.Path {
		case "https://api.anthropic.com/v1/messages":
			n := f.inferenceCalls
			f.inferenceCalls++
			f.authHeaders = append(f.authHeaders, r.Header.Get("Authorization")+"|"+r.Header.Get("X-Api-Key"))
			status, ct, out := f.inference(n, r, body)
			w.Header().Set("Content-Type", ct)
			w.WriteHeader(status)
			_, _ = io.WriteString(w, out)
		case "https://platform.claude.com/v1/oauth/token":
			var grant map[string]string
			_ = json.Unmarshal(body, &grant)
			f.tokenGrants = append(f.tokenGrants, grant)
			status, v := f.token(grant)
			oauthJSON(w, status, v)
		default:
			f.t.Errorf("unexpected destination %q", r.Host+r.URL.Path)
			http.Error(w, "bad", http.StatusBadRequest)
		}
	}))
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "ACP fixture"}, DNSNames: []string{"api.anthropic.com", "platform.claude.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, pub, key)
	require.NoError(t, err)
	backend.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	backend.StartTLS()
	t.Cleanup(backend.Close)
	f.certFile = filepath.Join(t.TempDir(), "fixture-ca.pem")
	require.NoError(t, os.WriteFile(f.certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || (r.Host != "api.anthropic.com:443" && r.Host != "platform.claude.com:443") {
			http.Error(w, "destination refused", http.StatusForbidden)
			return
		}
		upstream, err := net.Dial("tcp", backend.Listener.Addr().String())
		if err != nil {
			http.Error(w, "fixture unavailable", http.StatusBadGateway)
			return
		}
		defer func() { _ = upstream.Close() }()
		peer, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer func() { _ = peer.Close() }()
		_, err = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		if err != nil {
			return
		}
		if err := buffered.Flush(); err != nil {
			return
		}
		done := make(chan struct{})
		go func() { _, _ = io.Copy(upstream, buffered); _ = upstream.Close(); close(done) }()
		_, _ = io.Copy(peer, upstream)
		_ = peer.Close()
		<-done
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *providerFixture) env() map[string]string {
	return map[string]string{"HTTPS_PROXY": f.server.URL, "SSL_CERT_FILE": f.certFile, "GODEBUG": "x509sslcertoverrideplatform=1"}
}

func (f *providerFixture) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inferenceCalls
}

// pongSSE is one Anthropic answer with exact usage: 10 input and 3 output tokens.
func pongSSE() string { return sseText("pong") }

const anthropicModel = "anthropic/claude-sonnet-4-6@anthropic-messages"

func TestACPE2ERetryUsageProvider(t *testing.T) {
	const key = "sk-ant-sentinel-key-7001"
	const bodyText = "provider-body-sentinel-5521"
	f := newProviderFixture(t)
	f.inference = func(n int, r *http.Request, body []byte) (int, string, string) {
		switch {
		case n == 0:
			return 500, "application/json", `{"type":"error","error":{"type":"api_error","message":"` + bodyText + `"}}`
		case n == 1:
			return 200, "text/event-stream", pongSSE()
		case n >= 2 && n < 8:
			return 500, "application/json", `{"type":"error","error":{"type":"api_error","message":"` + bodyText + `"}}`
		default:
			return 401, "application/json", `{"type":"error","error":{"type":"authentication_error","message":"bad credential ` + key + `"}}`
		}
	}
	env := f.env()
	env["ANTHROPIC_API_KEY"] = key
	p := startE2E(t, e2eOptions{env: env})
	sid := p.start()
	var set struct {
		ModelID string `json:"modelId"`
	}
	p.ok("_ask/session/set_model", map[string]any{"sessionId": sid, "modelId": anthropicModel, "authMethodId": "api-key"}, &set)
	require.Equal(t, anthropicModel, set.ModelID)
	require.Zero(t, f.calls(), "switching the model starts no inference")
	var sub followResult
	p.ok("_ask/session/follow", e2eSession(sid), &sub)

	// A retryable failure then success: one admission, two requests, safe retry
	// facts, and the exact usage of the attempt that answered.
	r := p.call("session/prompt", e2ePrompt(sid, "ping"))
	require.Nil(t, r.Error, "%+v", r.Error)
	require.Contains(t, string(r.Result), `"end_turn"`)
	require.Equal(t, "pong", p.text(sid))
	require.Equal(t, 2, f.calls())
	var u usageResult
	p.ok("_ask/session/usage", e2eSession(sid), &u)
	require.Len(t, u.Attempts, 2, "the failed and the answering attempt")
	require.Equal(t, "failed", u.Attempts[0].Outcome)
	require.Nil(t, u.Attempts[0].Usage, "the failed attempt reported no usage")
	require.False(t, u.Complete, "a missing row makes the total a lower bound")
	require.NotNil(t, u.Attempts[1].Usage)
	require.EqualValues(t, 10, u.Attempts[1].Usage.Input)
	require.EqualValues(t, 3, u.Attempts[1].Usage.Output)
	require.EqualValues(t, 10, u.Total.Input)
	require.EqualValues(t, 3, u.Total.Output)
	p.waitFor("the retry event reaches the follower", func() bool {
		for _, e := range p.events(sub.SubscriptionID) {
			if strings.Contains(string(e.Params), `"type":"retry`) || strings.Contains(string(e.Params), "retry") {
				return true
			}
		}
		return false
	})

	// Retries that never succeed end in a mapped failure that holds the code only.
	r = p.call("session/prompt", e2ePrompt(sid, "ping again"))
	require.Equal(t, "internal", r.errKind(t))
	require.Contains(t, r.Error.Message, "model request failed: SERVER")
	require.NotContains(t, r.Error.Message, bodyText)
	require.Equal(t, 8, f.calls(), "six requests of one admission, no duplicate")

	// An authentication failure is not retried and maps to the sign-in kind.
	r = p.call("session/prompt", e2ePrompt(sid, "ping third"))
	require.Equal(t, "no_api_key", r.errKind(t))
	require.Equal(t, 9, f.calls())
	require.NotContains(t, r.Error.Message, key)

	// The session is usable and the state is exact after the failures.
	var st struct {
		Running bool `json:"running"`
	}
	p.ok("_ask/session/state", e2eSession(sid), &st)
	require.False(t, st.Running)
	require.NoError(t, p.in.Close())
	require.Equal(t, 0, p.waitExit())
	// No credential leaves the process on any frame, in any event of the
	// follower, or on stderr.
	p.noSecret(key)
	// The check above is meaningful: the failure text is on the wire, without the key.
	require.Contains(t, strings.Join(p.snapshot(), "\n"), "bad credential")
}

// startProductionOAuth uses the same binary as the ACP peer and the supported
// copy-code login. Only the external OAuth service is replaced.
func startProductionOAuth(t *testing.T, home string, env map[string]string) *oauthProcess {
	t.Helper()
	cmd := exec.Command(acpBinary(t), "auth", "login", "--provider", "anthropic", "--method", "anthropic-oauth", "--interaction", "copy-code")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "ASK_HOME=" + home}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return startOAuthProcess(t, cmd)
}

func TestACPE2EAuthRefresh(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	const refreshedAccess = "rotated-access-canary"
	f := newProviderFixture(t)
	f.token = func(grant map[string]string) (int, any) {
		switch grant["grant_type"] {
		case "authorization_code":
			tokens := oauthTokens()
			tokens["expires_in"] = 1
			return 200, tokens
		case "refresh_token":
			if grant["refresh_token"] != "private-refresh-canary" {
				return 400, map[string]any{"error": "invalid_grant"}
			}
			return 200, map[string]any{"access_token": refreshedAccess, "refresh_token": "rotated-refresh-canary", "expires_in": 3600, "token_type": "Bearer", "scope": "user:inference"}
		}
		return 400, map[string]any{"error": "unsupported_grant_type"}
	}
	f.inference = func(n int, r *http.Request, body []byte) (int, string, string) {
		if r.Header.Get("Authorization") != "Bearer "+refreshedAccess {
			return 401, "application/json", `{"type":"error","error":{"type":"authentication_error","message":"stale credential"}}`
		}
		return 200, "text/event-stream", pongSSE()
	}
	home := filepath.Join(t.TempDir(), "ask-home")

	// The credential comes from the supported host login.
	login := startProductionOAuth(t, home, f.env())
	completeOAuth(t, login, false)
	login.finish(t, true)
	require.Len(t, f.tokenGrants, 1)

	// After the saved credential expires, Authenticate refreshes
	// it through the normal resolver and never asks for input.
	time.Sleep(1100 * time.Millisecond)
	p := startE2E(t, e2eOptions{home: home, env: f.env()})
	p.initialize()
	require.Nil(t, p.call("authenticate", map[string]any{"methodId": "anthropic-oauth"}).Error)
	require.Len(t, f.tokenGrants, 2, "exactly one refresh")
	require.Equal(t, "refresh_token", f.tokenGrants[1]["grant_type"])
	require.Equal(t, "no_api_key", p.call("authenticate", map[string]any{"methodId": "api-key"}).errKind(t))

	sid := p.newSession()
	p.ok("_ask/session/set_model", map[string]any{"sessionId": sid, "modelId": anthropicModel, "authMethodId": "anthropic-oauth"}, nil)
	require.Equal(t, "no_api_key", p.call("_ask/session/set_model", map[string]any{"sessionId": sid, "modelId": anthropicModel, "authMethodId": "api-key"}).errKind(t))
	require.Zero(t, f.calls(), "no inference before the prompt")
	r := p.call("session/prompt", e2ePrompt(sid, "ping"))
	require.Nil(t, r.Error, "%+v", r.Error)
	require.Equal(t, "pong", p.text(sid))
	f.mu.Lock()
	require.Equal(t, []string{"Bearer " + refreshedAccess + "|"}, f.authHeaders, "the request uses the refreshed credential only")
	f.mu.Unlock()
	require.Len(t, f.tokenGrants, 2, "the prompt does not refresh again")

	// Shutdown waits for the refresh drain and leaves the credential stored.
	p.signal(syscall.SIGTERM)
	require.Equal(t, 143, p.waitExit())
	info, err := os.Stat(filepath.Join(home, "auth.json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	p.noSecret("private-access-canary", "private-refresh-canary", refreshedAccess, "rotated-refresh-canary", "private-code-canary")
}

type usageResult struct {
	Attempts []struct {
		AttemptID string `json:"attemptId"`
		CycleID   string `json:"cycleId"`
		Outcome   string `json:"outcome"`
		Usage     *struct {
			Input       int64 `json:"input"`
			Output      int64 `json:"output"`
			TotalTokens int64 `json:"totalTokens"`
		} `json:"usage"`
	} `json:"attempts"`
	Total struct {
		Input       int64 `json:"input"`
		Output      int64 `json:"output"`
		TotalTokens int64 `json:"totalTokens"`
	} `json:"total"`
	Complete bool `json:"complete"`
}

func TestACPE2ERetryUsage(t *testing.T) {
	p := startE2E(t, e2eOptions{env: fastEnv()})
	sid := p.start()
	var res usageResult
	p.ok("_ask/session/usage", e2eSession(sid), &res)
	require.Empty(t, res.Attempts)
	require.True(t, res.Complete)

	require.Nil(t, p.call("session/prompt", e2ePrompt(sid, "hello")).Error)
	p.ok("_ask/session/usage", e2eSession(sid), &res)
	require.Len(t, res.Attempts, 1)
	require.NotNil(t, res.Attempts[0].Usage)
	require.Positive(t, res.Attempts[0].Usage.TotalTokens)
	require.Equal(t, res.Attempts[0].Usage.TotalTokens, res.Total.TotalTokens, "the total is the exact sum")
	first := res.Total

	// A model failure ends the run with an error frame that holds the failure
	// code only: the provider text never leaves.
	f := p.call("session/prompt", e2ePrompt(sid, "fail boom-sentinel-text"))
	require.Equal(t, "internal", f.errKind(t))
	require.Contains(t, f.Error.Message, "model request failed")
	require.NotContains(t, f.Error.Message, "boom-sentinel-text")
	p.ok("_ask/session/usage", e2eSession(sid), &res)
	require.Len(t, res.Attempts, 2)
	require.Equal(t, "failed", res.Attempts[1].Outcome)
	require.GreaterOrEqual(t, res.Total.TotalTokens, first.TotalTokens)
	p.noSecret("boom-sentinel-text")
	require.Equal(t, "unknown_session", p.call("_ask/session/usage", e2eSession("sess_missing")).errKind(t))
	// The session takes the next prompt after the failure.
	require.Nil(t, p.call("session/prompt", e2ePrompt(sid, "again")).Error)
}
