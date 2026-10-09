package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/app"
	"AskCore/internal/auth"
	"AskCore/internal/leader"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func TestListenerClosePreservesReplacementSocket(t *testing.T) {
	paths, err := leader.ResolvePaths(leaderHome(t))
	require.NoError(t, err)
	require.NoError(t, leader.EnsureHome(paths.Home))
	lock, err := leader.Acquire(paths)
	require.NoError(t, err)
	defer func() { _ = lock.Release() }()
	socket := paths.Socket
	old, ident, err := listenPrivate(socket)
	require.NoError(t, err)
	require.NoError(t, os.Remove(socket))
	replacement, err := net.Listen("unix", socket)
	require.NoError(t, err)
	defer func() { _ = replacement.Close() }()
	require.NoError(t, old.Close())
	require.ErrorIs(t, lock.RemoveSocket(ident), leader.ErrSocketInUse)
	_, err = os.Lstat(socket)
	require.NoError(t, err, "old listener removed a new socket before inode-checked cleanup")
}

// oneFrameSink reports the request write without using a socket owner.
type oneFrameSink struct{ wrote chan struct{} }

func (w oneFrameSink) Write(b []byte) (int, error) { close(w.wrote); return len(b), nil }

func TestFollowKeepsEventsImmediatelyAfterResult(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	wrote := make(chan struct{})
	var out bytes.Buffer
	c := newLineClient(&leader.Client{Registered: &leader.Registered{Writer: leader.NewFrameWriter(oneFrameSink{wrote}, protocol.LeaderMaxFrame)}}, &out)
	c.setView("s1")
	done := make(chan struct{})
	go func() { c.follow(nil); close(done) }()
	<-wrote
	// These two frames arrive back to back on the socket reader before the
	// command waiter gets time to install its subscription.
	c.handle(rpcBody(map[string]any{"id": 1, "result": map[string]any{"subscriptionId": "sub1", "cursor": map[string]any{"epoch": "e1", "seq": "1"}}}))
	c.handle(rpcBody(map[string]any{"method": protocol.ACPEvent, "params": map[string]any{"subscriptionId": "sub1", "sessionId": "s1", "epoch": "e1", "seq": "2", "event": map[string]any{"type": "agent_settled"}}}))
	<-done
	require.Contains(t, out.String(), "event session=s1 seq=2", "the first live event after a Follow result was discarded")
}

// signalHoldTool starts a real tool body and holds its drain until released.
type signalHoldTool struct {
	started chan struct{}
	release <-chan struct{}
}

func (*signalHoldTool) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{Name: "hold", Description: "hold", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (h *signalHoldTool) Execute(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
	close(h.started)
	<-h.release
	return protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: "done"}}}, nil
}

func TestLeaderSecondSignalEscapesStartedToolDrain(t *testing.T) {
	home := leaderHome(t)
	paths, err := leader.ResolvePaths(home)
	require.NoError(t, err)
	require.NoError(t, leader.EnsureHome(home))
	lock, err := leader.Acquire(paths)
	require.NoError(t, err)
	defer func() { _ = lock.Release() }()
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	defer free()
	tool := &signalHoldTool{started: make(chan struct{}), release: release}
	registry := &tools.Registry{}
	require.NoError(t, registry.Register(tool, tools.SourceInfo{Kind: tools.SourceBuiltin}))
	fp, err := faux.New(faux.WithChunk(1000, 1000))
	require.NoError(t, err)
	model, ok := fp.Model("faux-1")
	require.True(t, ok)
	fp.Set(faux.Reply(faux.ToolCall("hold", nil, faux.ID("call-1"))), faux.Say("after"))
	rt, err := app.NewLeaderRuntime(app.LeaderParams{
		ACPParams: app.ACPParams{Home: home, Initial: model,
			NewAgent: func(service *auth.Service, id, cwd string) (*agent.Agent, error) {
				return app.NewNativeAgent(app.NativeAgentConfig{Auth: service, InitialStream: fp.Stream,
					Config: agent.Config{SessionID: id, Tools: registry, LoopConfig: agent.LoopConfig{
						Model: model, Stream: fp.Stream, Cwd: cwd, Options: providers.StreamOptions{APIKey: "test-key"},
					}}})
			}},
		Server: leader.ServerConfig{InstanceID: "signal-test", Build: "dev"},
	})
	require.NoError(t, err)
	defer func() { free(); _ = rt.Stop(context.Background()) }()
	sigs := make(chan os.Signal, 2)
	var errOut bytes.Buffer
	exited := make(chan int, 1)
	go func() {
		exited <- serveLeaderRuntime(rt, lock, paths, leader.Owner{PID: os.Getpid(), Instance: "signal-test"}, &errOut, sigs)
	}()
	require.Eventually(t, func() bool { return leader.Serving(paths) }, 5*time.Second, time.Millisecond)
	cl, err := leader.Connect(context.Background(), e2eConnect(home, ""))
	require.NoError(t, err)
	c := newLineClient(cl, io.Discard)
	go c.readLoop()
	defer c.close()
	_, fault := c.call("initialize", map[string]any{"protocolVersion": 1})
	require.Nil(t, fault)
	require.True(t, c.newSession(t.TempDir()))
	c.prompt("hold the tool")
	select {
	case <-tool.started:
	case <-time.After(5 * time.Second):
		t.Fatal("tool did not start")
	}
	sigs <- syscall.SIGTERM
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		t.Fatal("first signal did not disconnect the client")
	}
	select {
	case <-exited:
		t.Fatal("leader returned before the started tool drained")
	default:
	}
	sigs <- syscall.SIGTERM
	select {
	case code := <-exited:
		require.Equal(t, 143, code)
		require.Contains(t, errOut.String(), "cleanup did not drain; forced exit")
	case <-time.After(5 * time.Second):
		t.Fatal("second signal waited for the started tool drain")
	}
	require.NoFileExists(t, paths.Socket)
	free()
	_ = rt.Stop(context.Background())
	c.inflight.Wait()
}
