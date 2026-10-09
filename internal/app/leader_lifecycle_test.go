package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"AskCore/internal/acp"
	"AskCore/internal/agent"
	"AskCore/internal/leader"
	"AskCore/internal/providers/faux"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

func TestLeaderAuthCleanupStartsWhileToolDrains(t *testing.T) {
	release := make(chan struct{})
	tool := &holdTool{started: make(chan struct{}), release: release}
	registry := &tools.Registry{}
	require.NoError(t, registry.Register(tool, tools.SourceInfo{Kind: "builtin", Name: "hold"}))
	l := startLeader(t, fauxLeaderParamsWith(t, func() []faux.Step {
		return []faux.Step{faux.Reply(faux.ToolCall("hold", nil, faux.ID("call-1"))), faux.Say("after")}
	}, func(c *agent.Config) { c.Tools = registry }))
	t.Cleanup(func() { close(release) })
	cleaned := make(chan struct{})
	original := l.rt.cleanup
	l.rt.cleanup = func(ctx context.Context) error { close(cleaned); return original(ctx) }
	a := l.client(nil)
	a.request("session/prompt", promptArgs(a.newSession("/w")))
	select {
	case <-tool.started:
	case <-time.After(routingWait):
		t.Fatal("tool did not start")
	}
	l.cancel()
	select {
	case <-cleaned:
	case <-time.After(routingWait):
		t.Fatal("auth cleanup did not start during tool drain")
	}
	select {
	case <-l.done:
		t.Fatal("stop returned before tool drain")
	default:
	}
}

// This fixture uses the real adapter and SDK. It retains the SDK connection to
// issue a reverse request, because product permission tool owners do not exist.
func permissionLeader(t *testing.T, p LeaderParams) (*runningLeader, *sdk.AgentSideConnection) {
	t.Helper()
	runtime, err := NewACPRuntime(p.ACPParams)
	require.NoError(t, err)
	cfg := runtime.Config
	cfg.RequireRoute = true
	a, err := acp.NewAdapter(context.Background(), cfg)
	require.NoError(t, err)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	cw := acp.NewCheckedWriter(outW, func(err error) { a.Fail(err); _ = inR.CloseWithError(err) })
	conn := sdk.NewAgentSideConnection(a, cw, acp.NewLineLimitReader(inR, leader.MaxLine))
	a.Bind(conn, cw, outW)
	srv, err := leader.NewServer(leader.Config{Server: p.Server, Agent: &linkEnd{Reader: outR, Writer: inW, closers: []io.Closer{outR, inW}}, ActiveRuns: a.ActiveRuns, QuiesceIfIdle: a.QuiesceIfIdle})
	require.NoError(t, err)
	rt := &LeaderRuntime{Server: srv, Adapter: a, cleanup: runtime.Cleanup}
	dir, err := os.MkdirTemp("/tmp", "askq-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "l.sock")
	ln, err := net.Listen("unix", socket)
	require.NoError(t, err)
	return serveLeaderRuntime(t, rt, ln, socket), conn
}

func TestLeaderDriverLossCancelsQuestionAndKeepsRealRun(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	l, conn := permissionLeader(t, fauxLeaderParams(t, heldModel(release)))
	t.Cleanup(free)
	a, b := l.client(nil), l.client(nil)
	sid := a.newSession("/w")
	b.must(protocol.ACPAttach, map[string]any{"sessionId": sid})
	a.request("session/prompt", promptArgs(sid))
	routingEventually(t, "active run", func() bool { return l.rt.Adapter.ActiveRuns() == 1 })
	question := make(chan sdk.RequestPermissionResponse, 1)
	questionErr := make(chan error, 1)
	ask := func() {
		res, err := conn.RequestPermission(context.Background(), sdk.RequestPermissionRequest{SessionId: sdk.SessionId(sid), ToolCall: sdk.ToolCallUpdate{ToolCallId: "tool-1"}, Options: []sdk.PermissionOption{{OptionId: "allow", Name: "Allow", Kind: sdk.PermissionOptionKindAllowOnce}, {OptionId: "reject", Name: "Reject", Kind: sdk.PermissionOptionKindRejectOnce}}})
		question <- res
		questionErr <- err
	}
	go ask()
	a.waitCount("session/request_permission", 1)
	b.waitCount("session/request_permission", 1)
	_ = a.conn.Close()
	select {
	case <-question:
		var cancelled *sdk.RequestError
		require.True(t, errors.As(<-questionErr, &cancelled))
		require.Equal(t, -32800, cancelled.Code)
	case <-time.After(routingWait):
		t.Fatal("question not cancelled after driver loss")
	}
	b.waitCount("$/cancel_request", 1)
	require.NoError(t, l.rt.Adapter.Failure())
	require.Equal(t, 1, l.rt.Adapter.ActiveRuns(), "the question cancellation must not abort the run")
	b.must(protocol.ACPTake, map[string]any{"sessionId": sid})
	permissionID := func(index int) json.RawMessage {
		b.mu.Lock()
		defer b.mu.Unlock()
		for _, m := range b.msgs {
			if string(m["method"]) == `"session/request_permission"` {
				index--
				if index == 0 {
					return m["id"]
				}
			}
		}
		t.Fatal("permission request missing")
		return nil
	}
	answer := func(id json.RawMessage, option string) {
		raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": option}}})
		require.NoError(t, err)
		b.wmu.Lock()
		defer b.wmu.Unlock()
		require.NoError(t, b.w.Write(protocol.LeaderFrame{Type: protocol.LeaderFrameACP, Payload: raw}))
	}
	go ask()
	b.waitCount("session/request_permission", 2)
	answer(permissionID(1), "allow") // A late answer must not resolve the new generation's question.
	answer(permissionID(2), "reject")
	select {
	case res := <-question:
		require.NoError(t, <-questionErr)
		require.NotNil(t, res.Outcome.Selected)
		require.Equal(t, sdk.PermissionOptionId("reject"), res.Outcome.Selected.OptionId)
	case <-time.After(routingWait):
		t.Fatal("new question did not resolve")
	}
	// Releasing the provider ends the original run. Its updates still reach B.
	free()
	routingEventually(t, "run settled", func() bool { return l.rt.Adapter.ActiveRuns() == 0 })
	b.waitCount("session/update", 1)
	var state protocol.ACPStateResult
	require.NoError(t, json.Unmarshal(b.must(protocol.ACPState, map[string]any{"sessionId": sid}), &state))
}
