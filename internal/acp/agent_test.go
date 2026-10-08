package acp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func TestAdapterInitializeGateAndCapabilities(t *testing.T) {
	p := newAdapterPeer(t, testConfig(says("x"), nil), nil)
	for method, params := range map[string]any{
		"session/new":     map[string]any{"cwd": "/", "mcpServers": []any{}},
		protocol.ACPState: map[string]any{"sessionId": "s"},
		"session/prompt":  promptParams("s", "hi"),
	} {
		f := p.call(method, params)
		require.Equal(t, protocol.ACPErrNotInitialized, f.errKind(t), method)
		require.Equal(t, -32600, f.Error.Code)
	}
	var res map[string]any
	p.ok("initialize", map[string]any{"protocolVersion": 1}, &res)
	require.EqualValues(t, 1, res["protocolVersion"])
	caps := res["agentCapabilities"].(map[string]any)
	require.NotEqual(t, true, caps["loadSession"], "session/load is not available")
	prompt := caps["promptCapabilities"].(map[string]any)
	require.Equal(t, true, prompt["image"])
	require.NotEqual(t, true, prompt["audio"])
	require.NotEqual(t, true, prompt["embeddedContext"])
	if sc, ok := caps["sessionCapabilities"].(map[string]any); ok {
		for _, k := range []string{"list", "resume", "close", "fork"} {
			require.NotContains(t, sc, k, "an unavailable capability is not advertised")
		}
	}
	require.Equal(t, "ask", res["agentInfo"].(map[string]any)["name"])
	require.Len(t, res["authMethods"], 1)
	// A higher client version still gets version 1.
	p.ok("initialize", map[string]any{"protocolVersion": 7}, &res)
	require.EqualValues(t, 1, res["protocolVersion"])
}

func TestAdapterAuthenticateChecksReadinessOnly(t *testing.T) {
	secret := "sk-live-secret-value"
	var calls atomic.Int32
	cfg := testConfig(says("x"), nil)
	cfg.Authenticate = func(_ context.Context, id string) error {
		calls.Add(1)
		if id == "api-key" && calls.Load() == 1 {
			return errors.New("token " + secret + " expired")
		}
		return nil
	}
	p := newAdapterPeer(t, cfg, nil)
	f := p.call("authenticate", map[string]any{"methodId": "api-key"})
	require.Equal(t, protocol.ACPErrNotInitialized, f.errKind(t))
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)

	f = p.call("authenticate", map[string]any{"methodId": "api-key"})
	require.Equal(t, protocol.ACPErrNoAPIKey, f.errKind(t))
	require.Equal(t, -32000, f.Error.Code)
	require.Contains(t, f.Error.Message, "ask auth", "safe sign-in guidance")
	f = p.call("authenticate", map[string]any{"methodId": "unknown-method"})
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
	p.ok("authenticate", map[string]any{"methodId": "api-key"}, nil)
	require.EqualValues(t, 2, calls.Load(), "an unknown method never reaches the callback")
	for _, l := range p.snapshotLines() {
		require.NotContains(t, l, secret)
	}
}

func TestAdapterSessionIsolationAndFailedNew(t *testing.T) {
	var built atomic.Int32
	cfg := testConfig(says("one", "two"), nil)
	inner := cfg.Factory
	cfg.Factory = func(ctx context.Context, id, cwd string) (*agent.Agent, error) {
		if built.Add(1) == 3 {
			return nil, errors.New("factory failed with secret-path /home/x")
		}
		return inner(ctx, id, cwd)
	}
	p := newAdapterPeer(t, cfg, nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	a, b := p.newSession(), p.newSession()
	require.NotEqual(t, a, b)
	sa, _ := p.a.host.Session(a)
	sb, _ := p.a.host.Session(b)
	require.NotSame(t, sa.Agent(), sb.Agent())
	require.NotEqual(t, sa.CWD, sb.CWD)

	f := p.call("session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}})
	require.Equal(t, protocol.ACPErrInternal, f.errKind(t))
	require.NotContains(t, f.Error.Message, "secret-path")
	p.mu.Lock()
	require.Len(t, p.a.host.sessions, 2, "a failed session/new leaves no entry")
	p.mu.Unlock()

	f = p.call("session/new", map[string]any{"cwd": "relative/dir", "mcpServers": []any{}})
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
	f = p.call("session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{map[string]any{"name": "m", "command": "x", "args": []any{}, "env": []any{}}}})
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
	require.Contains(t, f.Error.Message, "mcp")
	f = p.call("session/prompt", promptParams("sess_missing", "hi"))
	require.Equal(t, protocol.ACPErrUnknownSession, f.errKind(t))

	// Each session keeps its own history and model script.
	p.ok("session/prompt", promptParams(a, "hello"), nil)
	var st protocol.ACPStateResult
	p.ok(protocol.ACPState, map[string]any{"sessionId": a}, &st)
	require.Equal(t, 2, st.MessageCount)
	p.ok(protocol.ACPState, map[string]any{"sessionId": b}, &st)
	require.Equal(t, 0, st.MessageCount)
}

func TestAdapterPromptSettlesAfterUpdatesAndRejectsBusy(t *testing.T) {
	release := make(chan struct{})
	rel := closeOnce(release)
	t.Cleanup(rel)
	cfg := testConfig(func() []faux.Step { return []faux.Step{heldStep(release, "first answer"), faux.Say("second answer")} }, nil)
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	id := p.send("session/prompt", promptParams(sid, "go"))
	require.Eventually(t, func() bool { s, _ := p.a.host.Session(sid); return s.Agent().State().Status == agent.Running }, peerWait, time.Millisecond)

	busy := p.call("session/prompt", promptParams(sid, "again"))
	require.Equal(t, protocol.ACPErrBusy, busy.errKind(t))
	require.Equal(t, protocol.ACPCodeBusy, busy.Error.Code)
	require.Equal(t, -1, p.responseIndex(id), "the busy call must not release the first prompt")

	rel()
	f := p.await(id)
	require.Nil(t, f.Error, "%+v", f.Error)
	require.JSONEq(t, `{"stopReason":"end_turn"}`, string(f.Result))
	respAt := p.responseIndex(id)
	require.Greater(t, respAt, p.lastNoteIndex("session/update"), "the result follows every update")
	require.Contains(t, p.updateKinds(), "agent_message_chunk")

	var meta struct {
		Meta struct {
			Ask map[string]any `json:"ask"`
		} `json:"_meta"`
	}
	n := p.notes("session/update")[0]
	require.NoError(t, json.Unmarshal(n.Params, &meta))
	require.Contains(t, meta.Meta.Ask["seq"], "", "seq is decimal text")
	_, isString := meta.Meta.Ask["seq"].(string)
	require.True(t, isString)
	require.NotEmpty(t, meta.Meta.Ask["runId"])
	require.NotEmpty(t, meta.Meta.Ask["epoch"])

	// The same session takes the next prompt.
	p.ok("session/prompt", promptParams(sid, "second"), nil)
}

func TestAdapterPromptRejectsUnsupportedContent(t *testing.T) {
	p := newAdapterPeer(t, testConfig(says("x"), nil), nil)
	sid := p.start()
	for _, block := range []map[string]any{
		{"type": "audio", "data": "AAAA", "mimeType": "audio/wav"},
		{"type": "resource_link", "uri": "file:///x", "name": "x"},
		{"type": "resource", "resource": map[string]any{"uri": "file:///x", "text": "t"}},
	} {
		f := p.call("session/prompt", map[string]any{"sessionId": sid, "prompt": []any{block}})
		require.Equal(t, protocol.ACPErrInvalidContent, f.errKind(t), "%v", block["type"])
	}
	f := p.call("session/prompt", map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": ""}}})
	require.Equal(t, protocol.ACPErrInvalidContent, f.errKind(t))
	// Nothing reached the Agent.
	s, _ := p.a.host.Session(sid)
	require.Empty(t, s.Agent().State().Messages)
	// An image block is accepted.
	p.ok("session/prompt", map[string]any{"sessionId": sid, "prompt": []any{
		map[string]any{"type": "text", "text": "look"},
		map[string]any{"type": "image", "data": "aGk=", "mimeType": "image/png"},
	}}, nil)
	msg := s.Agent().State().Messages[0].(protocol.UserMessage)
	require.Len(t, msg.Content, 2)
}

func TestAdapterCancelStopsRunAndReturnsCancelled(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(closeOnce(release))
	cfg := testConfig(func() []faux.Step { return []faux.Step{heldStep(release, "never")} }, nil)
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	id := p.send("session/prompt", promptParams(sid, "go"))
	s, _ := p.a.host.Session(sid)
	require.Eventually(t, func() bool { return s.Agent().State().Status == agent.Running }, peerWait, time.Millisecond)
	p.notify("session/cancel", map[string]any{"sessionId": sid})
	f := p.await(id)
	require.Nil(t, f.Error, "%+v", f.Error)
	require.JSONEq(t, `{"stopReason":"cancelled"}`, string(f.Result))
	// A cancel of an unknown or idle session changes nothing and sends no frame.
	before := len(p.snapshotLines())
	p.notify("session/cancel", map[string]any{"sessionId": "sess_missing"})
	p.notify("session/cancel", map[string]any{"sessionId": sid})
	p.ok(protocol.ACPState, map[string]any{"sessionId": sid}, nil)
	require.Equal(t, before+1, len(p.snapshotLines()))
}

func TestAdapterRequestCancellationIsNotRunCancellation(t *testing.T) {
	release := make(chan struct{})
	rel := closeOnce(release)
	t.Cleanup(rel)
	cfg := testConfig(func() []faux.Step { return []faux.Step{heldStep(release, "kept answer")} }, nil)
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	id := p.send("session/prompt", promptParams(sid, "go"))
	s, _ := p.a.host.Session(sid)
	require.Eventually(t, func() bool { return s.Agent().State().Status == agent.Running }, peerWait, time.Millisecond)
	p.notify("$/cancel_request", map[string]any{"requestId": id})
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, agent.Running, s.Agent().State().Status, "cancelling the request does not abort the run")
	rel()
	f := p.await(id)
	require.Nil(t, f.Error, "%+v", f.Error)
	msgs := s.Agent().State().Messages
	require.Equal(t, protocol.StopStop, msgs[len(msgs)-1].(protocol.AssistantMessage).StopReason)
}

func TestAdapterRequestCancellationWithStringID(t *testing.T) {
	release := make(chan struct{})
	rel := closeOnce(release)
	t.Cleanup(rel)
	cfg := testConfig(func() []faux.Step { return []faux.Step{heldStep(release, "kept")} }, nil)
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	p.sendRaw("session/prompt", "abc", promptParams(sid, "go"))
	p.running(sid)
	p.notify("$/cancel_request", map[string]any{"requestId": "abc"})
	time.Sleep(50 * time.Millisecond)
	s, _ := p.a.host.Session(sid)
	require.Equal(t, agent.Running, s.Agent().State().Status)
	rel()
	f := p.awaitRaw(`"abc"`)
	require.Nil(t, f.Error, "%+v", f.Error)
	require.JSONEq(t, `{"stopReason":"end_turn"}`, string(f.Result))
}

func TestAdapterNoStartFailureReturnsMappedErrorAndNextPromptIsFree(t *testing.T) {
	secret := "log fixture failure"
	cfg := testConfig(says("one", "two"), func(c *agent.Config) {
		c.NewContext = func() sessions.Writer { return &hostBrokenLog{} }
	})
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	for i := 0; i < 2; i++ {
		id := p.send("session/prompt", promptParams(sid, "go"))
		f := p.await(id)
		require.Equal(t, protocol.ACPErrInternal, f.errKind(t))
		require.NotContains(t, f.Error.Message, secret)
		require.Greater(t, p.responseIndex(id), p.lastNoteIndex("session/update"), "the failure frame follows every written update")
	}
	for _, l := range p.snapshotLines() {
		require.NotContains(t, l, secret)
	}
	s, _ := p.a.host.Session(sid)
	require.NoError(t, s.Agent().WaitForIdle(hostDeadline(t)))
}

func TestAdapterContinue(t *testing.T) {
	p := newAdapterPeer(t, testConfig(says("one"), nil), nil)
	sid := p.start()
	f := p.call(protocol.ACPContinue, map[string]any{"sessionId": sid})
	require.Equal(t, protocol.ACPErrInvalidState, f.errKind(t), "an empty log has nothing to continue")
	p.ok("session/prompt", promptParams(sid, "go"), nil)
	f = p.call(protocol.ACPContinue, map[string]any{"sessionId": sid})
	require.Equal(t, protocol.ACPErrInvalidState, f.errKind(t), "the log ends with an assistant message")
	// The script is exhausted: the run fails with a failure frame, and the session lives.
	f = p.call("session/prompt", promptParams(sid, "never needed"))
	require.Equal(t, protocol.ACPErrInternal, f.errKind(t))
	require.GreaterOrEqual(t, p.state(sid).MessageCount, 2)
}

func TestAdapterContinueFromUserTail(t *testing.T) {
	p := newAdapterPeer(t, testConfig(says("continued"), preloaded("seed")), nil)
	sid := p.start()
	var res protocol.ACPContinueResult
	id := p.send(protocol.ACPContinue, map[string]any{"sessionId": sid})
	f := p.await(id)
	require.Nil(t, f.Error, "%+v", f.Error)
	require.NoError(t, json.Unmarshal(f.Result, &res))
	require.Equal(t, "end_turn", res.StopReason)
	require.Greater(t, p.responseIndex(id), p.lastNoteIndex("session/update"))
	require.Contains(t, p.updateKinds(), "agent_message_chunk")
}

func TestAdapterProviderErrorTextNeverLeaves(t *testing.T) {
	secret := "sk-provider-secret-text"
	cfg := testConfig(func() []faux.Step {
		return []faux.Step{faux.Fail("provider rejected key " + secret).Err(providers.NewFailure(providers.CodeAuth, 401, 0, "bad key "+secret, nil))}
	}, nil)
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	f := p.call("session/prompt", promptParams(sid, "go"))
	require.Equal(t, protocol.ACPErrNoAPIKey, f.errKind(t), "a failed model request is a failure frame")
	require.Contains(t, f.Error.Message, "ask auth")
	for _, l := range p.snapshotLines() {
		if strings.Contains(l, "session/update") {
			continue
		}
		require.NotContains(t, l, secret)
	}
	for _, n := range p.notes("session/update") {
		require.NotContains(t, string(n.Params), secret)
	}
}

func TestAdapterModelFailureCodeIsTheOnlyDetail(t *testing.T) {
	secret := "sk-rate-limit-secret"
	cfg := testConfig(func() []faux.Step {
		return []faux.Step{faux.Say("x").Err(providers.NewFailure(providers.CodeRateLimit, 429, 11*time.Second, "slow down "+secret, nil))}
	}, nil)
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	f := p.call("session/prompt", promptParams(sid, "go"))
	require.Equal(t, protocol.ACPErrInternal, f.errKind(t))
	require.Contains(t, f.Error.Message, "RATE_LIMIT")
	for _, l := range p.snapshotLines() {
		require.NotContains(t, l, secret)
	}
	require.Greater(t, p.responseIndex(int64(3)), p.lastNoteIndex("session/update"), "the failure frame follows the written updates")
}

func TestAdapterDeferredMethodsAreExplicitlyUnsupported(t *testing.T) {
	p := newAdapterPeer(t, testConfig(says("x"), nil), nil)
	sid := p.start()
	for _, m := range []string{protocol.ACPCompact, protocol.ACPFork, protocol.ACPTree} {
		f := p.call(m, map[string]any{"sessionId": sid})
		require.Equal(t, protocol.ACPErrUnsupported, f.errKind(t), m)
		require.Equal(t, -32601, f.Error.Code)
	}
	f := p.call("session/load", map[string]any{"sessionId": sid, "cwd": "/", "mcpServers": []any{}})
	require.Equal(t, -32601, f.Error.Code)
	f = p.call("_ask/session/nope", map[string]any{"sessionId": sid})
	require.Equal(t, -32601, f.Error.Code)
	for _, m := range []string{"session/list", "session/resume", "session/close", "logout", "session/set_mode", "session/set_config_option"} {
		f = p.call(m, map[string]any{"sessionId": sid, "cwd": "/", "modeId": "m", "configId": "c", "value": "v", "mcpServers": []any{}})
		require.NotNil(t, f.Error, m)
		require.Contains(t, []int{-32601, -32602}, f.Error.Code, m)
	}
}

func TestAdapterOutputFailureFailsConnectionWithoutSuccess(t *testing.T) {
	var fail atomic.Bool
	p := newAdapterPeer(t, testConfig(says("answer"), nil), func(w ioWriter) ioWriter {
		return failingWriter{w: w, fail: &fail}
	})
	sid := p.start()
	fail.Store(true)
	id := p.send("session/prompt", promptParams(sid, "go"))
	select {
	case <-p.conn.Done():
	case <-time.After(peerWait):
		t.Fatal("connection stayed open after a lost write")
	}
	require.Equal(t, -1, p.responseIndex(id), "no result after lost output")
	require.Error(t, p.a.host.Failure())
}

func TestAdapterCloseDisposesSessions(t *testing.T) {
	p := newAdapterPeer(t, testConfig(says("x"), nil), nil)
	sid := p.start()
	s, _ := p.a.host.Session(sid)
	require.NoError(t, p.a.Close())
	require.NoError(t, p.a.Close(), "close is idempotent")
	_, err := s.Prompt(context.Background(), hostUser("late"))
	require.Error(t, err)
	id := p.send("session/prompt", promptParams(sid, "late"))
	select {
	case <-p.conn.Done():
	case <-time.After(peerWait):
		t.Fatal("the connection stays open after the adapter closed its output")
	}
	require.Equal(t, -1, p.responseIndex(id))
}

// holdTool is a real tool whose started body waits for release and ignores
// cancellation, like a body that cannot stop at once.
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

func holdRegistry(t *testing.T, h *holdTool) *tools.Registry {
	t.Helper()
	r := &tools.Registry{}
	require.NoError(t, r.Register(h, tools.SourceInfo{Kind: "builtin", Name: "hold"}))
	return r
}

func TestAdapterToolUpdatesPrecedeResult(t *testing.T) {
	started := make(chan struct{})
	released := make(chan struct{})
	close(released)
	tool := &holdTool{started: started, release: released}
	cfg := testConfig(func() []faux.Step {
		return []faux.Step{faux.Reply(faux.ToolCall("hold", nil, faux.ID("call-1"))), faux.Say("after")}
	}, func(c *agent.Config) { c.Tools = holdRegistry(t, tool) })
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	id := p.send("session/prompt", promptParams(sid, "go"))
	f := p.await(id)
	require.Nil(t, f.Error, "%+v", f.Error)
	kinds := p.updateKinds()
	require.Contains(t, kinds, "tool_call")
	require.Contains(t, kinds, "tool_call_update")
	var statuses []string
	for _, n := range p.notes("session/update") {
		var u struct {
			Update struct {
				SessionUpdate string `json:"sessionUpdate"`
				ToolCallID    string `json:"toolCallId"`
				Status        string `json:"status"`
			} `json:"update"`
		}
		require.NoError(t, json.Unmarshal(n.Params, &u))
		if u.Update.ToolCallID == "call-1" {
			statuses = append(statuses, u.Update.SessionUpdate+":"+u.Update.Status)
		}
	}
	require.Equal(t, "tool_call:pending", statuses[0])
	require.Equal(t, "tool_call_update:completed", statuses[len(statuses)-1])
	require.Contains(t, statuses, "tool_call_update:in_progress")
	require.Greater(t, p.responseIndex(id), p.lastNoteIndex("session/update"))
}

func TestAdapterCancelWaitsForStartedToolBody(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	rel := closeOnce(release)
	t.Cleanup(rel)
	tool := &holdTool{started: started, release: release}
	cfg := testConfig(func() []faux.Step {
		return []faux.Step{faux.Reply(faux.ToolCall("hold", nil)), faux.Say("never")}
	}, func(c *agent.Config) { c.Tools = holdRegistry(t, tool) })
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	id := p.send("session/prompt", promptParams(sid, "go"))
	select {
	case <-started:
	case <-time.After(peerWait):
		t.Fatal("tool body never started")
	}
	p.notify("session/cancel", map[string]any{"sessionId": sid})
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, -1, p.responseIndex(id), "no result while a started body runs")
	rel()
	f := p.await(id)
	require.Nil(t, f.Error, "%+v", f.Error)
	require.JSONEq(t, `{"stopReason":"cancelled"}`, string(f.Result))
	require.Greater(t, p.responseIndex(id), p.lastNoteIndex("session/update"))
}

func TestAdapterFailStopsRunsAndClosesOutput(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(closeOnce(release))
	cfg := testConfig(func() []faux.Step { return []faux.Step{heldStep(release, "never")} }, nil)
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	id := p.send("session/prompt", promptParams(sid, "go"))
	p.running(sid)
	s, _ := p.a.host.Session(sid)
	cause := errors.New("stdout lost")
	p.a.Fail(cause)
	select {
	case <-p.a.Failed():
	case <-time.After(peerWait):
		t.Fatal("failure not signalled")
	}
	require.ErrorIs(t, p.a.Failure(), ErrOutputFailed)
	require.ErrorIs(t, p.a.Failure(), cause)
	require.NoError(t, s.Agent().WaitForIdle(hostDeadline(t)), "the run stops after its output failed")
	fol := s.Agent().Follow(agent.Cursor{})
	fol.Events.Close()
	var causes []string
	for _, e := range fol.Entries {
		if c, ok := e.(sessions.CycleClosed); ok {
			causes = append(causes, c.Reason+"/"+c.Cause)
		}
	}
	require.Equal(t, []string{"aborted/output"}, causes, "the cycle ends with the output cause")
	require.Equal(t, -1, p.responseIndex(id), "no success after lost output")
	f := p.a.Failure()
	p.a.Fail(errors.New("second failure"))
	require.Equal(t, f, p.a.Failure(), "the first failure stays")
}
