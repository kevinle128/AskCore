package leader

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func TestServerSlowClientDoesNotBlockOthers(t *testing.T) {
	h := startHarness(t)
	const updates = 1500
	pad := strings.Repeat("x", 8<<10)
	h.agent.onPrompt = func(a *fakeAgent, req agentRequest) {
		sid := stringParam(req.Params, "sessionId")
		for i := range updates {
			a.send(a.update(sid, fmt.Sprintf("%06d%s", i, pad)))
		}
		a.result(req.ID, map[string]any{"stopReason": "end_turn"})
	}
	a, slow := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	slow.attach(sid)
	slow.pause()
	slow.barrier() // one frame may still be read, then the client stops reading

	// 12 MB is more than the socket buffers hold, so the slow client's writer blocks.
	a.mustCall("session/prompt", map[string]any{"sessionId": sid, "prompt": []any{}})
	require.Len(t, a.incoming("session/update"), updates, "the healthy client got every update and the result")
	require.Less(t, len(slow.incoming("session/update")), updates, "the slow client has not read them")

	// The slow client is still connected and later reads every frame in order.
	a.mustCall(protocol.ACPState, map[string]any{"sessionId": sid})
	slow.resume()
	got := slow.waitIncoming("session/update", updates)
	for i, m := range got[:updates] {
		var p struct {
			Update struct {
				Content struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"update"`
		}
		require.NoError(t, json.Unmarshal(m.fields["params"], &p))
		require.True(t, strings.HasPrefix(p.Update.Content.Text, fmt.Sprintf("%06d", i)), "update %d is out of order", i)
	}
	slow.mustCall(protocol.ACPState, map[string]any{"sessionId": sid})
}

func TestServerOversizeOutgoingClosesOnlyRecipient(t *testing.T) {
	h := startHarness(t, harnessOpts{edit: func(c *Config) { c.Server.MaxFrame = 4096 }})
	a, b := h.ready(nil), h.ready(nil)
	s1, s2 := a.newSession("/a"), b.newSession("/b")
	_ = s1

	// Only B is in s2. The frame is too big for the socket limit, so B is closed.
	h.agent.send(h.agent.update(s2, strings.Repeat("y", 8192)))
	b.waitClosed()

	// A and the agent link are not affected.
	a.mustCall("session/prompt", map[string]any{"sessionId": s1, "prompt": []any{}})
	select {
	case <-h.srv.Done():
		t.Fatal("an oversize frame for one client stopped the leader")
	default:
	}
	require.Empty(t, h.agent.requests("session/cancel"))
}

func TestServerStatusAndShutdownForRegisteredClient(t *testing.T) {
	h := startHarness(t, harnessOpts{edit: func(c *Config) { c.PID = 4242; c.SpawnedByClient = true }})
	a, b := h.ready(nil), h.ready(nil)
	a.newSession("/w")
	_ = b

	a.write(protocol.LeaderFrame{Type: protocol.LeaderFrameControl, Payload: marshalPlain(protocol.LeaderControl{Command: protocol.LeaderControlStatus})})
	st := waitControlResult[protocol.LeaderStatus](t, a)
	require.Equal(t, protocol.LeaderStatus{InstanceID: "inst-1", Build: "dev", ProtocolVersion: 1, PID: 4242, Clients: 2, Sessions: 1, SpawnedByClient: true}, st)

	a.write(protocol.LeaderFrame{Type: protocol.LeaderFrameControl, Payload: marshalPlain(protocol.LeaderControl{Command: protocol.LeaderControlShutdown, InstanceID: "other"})})
	waitFrameType(t, a, protocol.LeaderFrameError)
	select {
	case <-h.srv.Done():
		t.Fatal("a shutdown for another instance stopped the leader")
	default:
	}

	a.write(protocol.LeaderFrame{Type: protocol.LeaderFrameControl, Payload: marshalPlain(protocol.LeaderControl{Command: protocol.LeaderControlShutdown, InstanceID: "inst-1"})})
	select {
	case <-h.srv.Done():
	case <-time.After(waitFor):
		t.Fatal("the leader did not stop")
	}
	b.waitClosed()
	require.Contains(t, b.frameTypes(), protocol.LeaderFrameError, "every client is told before its socket closes")
}

func waitFrameType(t *testing.T, c *testClient, typ string) protocol.LeaderFrame {
	t.Helper()
	var out protocol.LeaderFrame
	eventually(t, "frame "+typ, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, f := range c.frames {
			if f.Type == typ {
				out = f
				return true
			}
		}
		return false
	})
	return out
}

func waitControlResult[T any](t *testing.T, c *testClient) T {
	t.Helper()
	var out T
	require.NoError(t, json.Unmarshal(waitFrameType(t, c, protocol.LeaderFrameControlReply).Payload, &out))
	return out
}

func TestServerMismatchedClientIsManagementOnly(t *testing.T) {
	var idle atomic.Bool
	h := startHarness(t, harnessOpts{edit: func(c *Config) { c.QuiesceIfIdle = idle.Load; c.SpawnedByClient = true }})
	// Each refusal ends the management connection, so every command gets a new one.
	send := func(ctl protocol.LeaderControl) protocol.LeaderFrame {
		conn, err := net.Dial("unix", h.paths.Socket)
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()
		_, err = Register(conn, hello(9), 2*time.Second)
		var remote *RemoteError
		require.ErrorAs(t, err, &remote)
		require.Equal(t, protocol.LeaderErrVersionMismatch, remote.Kind)
		require.NoError(t, NewFrameWriter(conn, protocol.LeaderMaxFrame).Write(protocol.LeaderFrame{Type: protocol.LeaderFrameControl, Payload: marshalPlain(ctl)}))
		f, err := NewFrameReader(conn, protocol.LeaderMaxFrame).Next()
		require.NoError(t, err)
		return f
	}
	f := send(protocol.LeaderControl{Command: protocol.LeaderControlStatus})
	require.Equal(t, protocol.LeaderFrameControlReply, f.Type)
	var st protocol.LeaderStatus
	require.NoError(t, json.Unmarshal(f.Payload, &st))
	require.Equal(t, "inst-1", st.InstanceID)

	f = send(protocol.LeaderControl{Command: protocol.LeaderControlShutdown})
	require.Equal(t, protocol.LeaderFrameError, f.Type, "another version may stop the leader only when it is idle")
	f = send(protocol.LeaderControl{Command: protocol.LeaderControlShutdown, IfIdle: true})
	require.Equal(t, protocol.LeaderFrameError, f.Type, "a busy leader refuses")
	f = send(protocol.LeaderControl{Command: protocol.LeaderControlShutdown, IfIdle: true, InstanceID: "other"})
	require.Equal(t, protocol.LeaderFrameError, f.Type, "the instance id must match")
	select {
	case <-h.srv.Done():
		t.Fatal("a refused shutdown stopped the leader")
	default:
	}

	idle.Store(true)
	f = send(protocol.LeaderControl{Command: protocol.LeaderControlShutdown, IfIdle: true, InstanceID: "inst-1"})
	require.Equal(t, protocol.LeaderFrameControlReply, f.Type)
	select {
	case <-h.srv.Done():
	case <-time.After(waitFor):
		t.Fatal("an idle leader did not stop")
	}
}

func TestServerSupervisedLeaderRefusesIdleShutdown(t *testing.T) {
	h := startHarness(t, harnessOpts{edit: func(c *Config) { c.QuiesceIfIdle = func() bool { return true } }}) // not spawned by a client
	a := h.ready(nil)
	a.write(protocol.LeaderFrame{Type: protocol.LeaderFrameControl, Payload: marshalPlain(protocol.LeaderControl{Command: protocol.LeaderControlShutdown, IfIdle: true})})
	f := waitFrameType(t, a, protocol.LeaderFrameError)
	require.Contains(t, string(f.Payload), "supervisor")
	select {
	case <-h.srv.Done():
		t.Fatal("a supervised leader was stopped by a client")
	default:
	}
}

func TestServerMismatchedClientNeverReachesRouter(t *testing.T) {
	h := startHarness(t)
	conn, err := net.Dial("unix", h.paths.Socket)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, err = Register(conn, hello(0), 2*time.Second)
	require.Error(t, err)
	w := NewFrameWriter(conn, protocol.LeaderMaxFrame)
	require.NoError(t, w.Write(protocol.LeaderFrame{Type: protocol.LeaderFrameACP, Payload: request("1", "session/new")}))
	_ = conn.SetReadDeadline(time.Now().Add(waitFor))
	_, err = NewFrameReader(conn, protocol.LeaderMaxFrame).Next()
	require.Error(t, err, "the leader closes the connection")
	require.Empty(t, h.agent.requests("session/new"))
	require.Zero(t, h.clientCount())
}

func TestServerAgentLinkFailureStopsLeader(t *testing.T) {
	h := startHarness(t)
	a := h.ready(nil)
	a.newSession("/w")
	_ = h.agent.toLead.CloseWithError(fmt.Errorf("agent crashed"))
	select {
	case <-h.srv.Done():
	case <-time.After(waitFor):
		t.Fatal("the leader kept running without its agent")
	}
	require.Error(t, h.srv.Err())
	a.waitClosed()
	require.Contains(t, a.frameTypes(), protocol.LeaderFrameError)
}

func TestServerCloseTellsClients(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	require.NoError(t, h.srv.Close())
	a.waitClosed()
	b.waitClosed()
	for _, c := range []*testClient{a, b} {
		f := waitFrameType(t, c, protocol.LeaderFrameError)
		var e protocol.LeaderError
		require.NoError(t, json.Unmarshal(f.Payload, &e))
		require.Equal(t, protocol.LeaderErrShuttingDown, e.Kind)
	}
	require.NoError(t, h.srv.Close(), "Close is safe to call twice")
}

func TestServerInvalidFirstFrameClosed(t *testing.T) {
	h := startHarness(t)
	conn, err := net.Dial("unix", h.paths.Socket)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	require.NoError(t, NewFrameWriter(conn, protocol.LeaderMaxFrame).Write(protocol.LeaderFrame{Type: protocol.LeaderFrameACP, Payload: request("1", "session/new")}))
	_ = conn.SetReadDeadline(time.Now().Add(waitFor))
	f, err := NewFrameReader(conn, protocol.LeaderMaxFrame).Next()
	require.NoError(t, err)
	require.Equal(t, protocol.LeaderFrameError, f.Type)
	require.Empty(t, h.agent.requests("session/new"))
}

func TestServerStartFailsWhenAgentRefusesInitialize(t *testing.T) {
	fa, end := newFakeAgent(t)
	fa.silent["initialize"] = true
	srv, err := NewServer(Config{Agent: end, LinkTimeout: 100 * time.Millisecond})
	require.NoError(t, err)
	require.ErrorContains(t, srv.Start(), "did not answer initialize")
	<-fa.done
}

func TestServerCloseWithoutStart(t *testing.T) {
	fa, end := newFakeAgent(t)
	srv, err := NewServer(Config{Agent: end})
	require.NoError(t, err)
	require.NoError(t, srv.Close())
	<-fa.done
}

func TestServerIdleShutdownWaitsForOpenQuestion(t *testing.T) {
	var host atomic.Bool
	host.Store(true) // the host reports idle all the time
	h := startHarness(t, harnessOpts{edit: func(c *Config) { c.QuiesceIfIdle = host.Load; c.SpawnedByClient = true }})
	a := h.ready(nil)
	sid := a.newSession("/w")
	h.agent.send(permissionRequest("q1", sid))
	req := a.waitIncoming("session/request_permission", 1)[0]

	ask := func() protocol.LeaderFrame {
		a.write(protocol.LeaderFrame{Type: protocol.LeaderFrameControl, Payload: marshalPlain(protocol.LeaderControl{Command: protocol.LeaderControlShutdown, IfIdle: true})})
		return waitFrameTypeAfter(t, a)
	}
	f := ask()
	require.Equal(t, protocol.LeaderFrameError, f.Type, "an open question is work, whatever the host says")
	select {
	case <-h.srv.Done():
		t.Fatal("the leader stopped with a question open")
	default:
	}

	a.sendRaw(answer(req.id, "allow"))
	eventually(t, "answered", func() bool { return len(h.agent.answersFor(`"q1"`)) == 1 })
	f = ask()
	require.Equal(t, protocol.LeaderFrameControlReply, f.Type)
	select {
	case <-h.srv.Done():
	case <-time.After(waitFor):
		t.Fatal("an idle leader did not stop")
	}
}

// waitFrameTypeAfter returns the next control answer (a reply or an error)
// that arrives after the call, by counting the frames seen so far.
func waitFrameTypeAfter(t *testing.T, c *testClient) protocol.LeaderFrame {
	t.Helper()
	isAnswer := func(f protocol.LeaderFrame) bool {
		return f.Type == protocol.LeaderFrameControlReply || f.Type == protocol.LeaderFrameError
	}
	c.mu.Lock()
	seen := 0
	for _, f := range c.frames {
		if isAnswer(f) {
			seen++
		}
	}
	c.mu.Unlock()
	var out protocol.LeaderFrame
	eventually(t, "control answer", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		n := 0
		for _, f := range c.frames {
			if isAnswer(f) {
				n++
				if n > seen {
					out = f
					return true
				}
			}
		}
		return false
	})
	return out
}

// A client whose handshake ends after the router stopped must be refused, not
// left with a writer that waits for ever. The race is a coin toss inside the
// router, so the test tries it many times.
func TestServerRefusesAClientAfterTheRouterStopped(t *testing.T) {
	h := startHarness(t)
	require.NoError(t, h.srv.Close())
	for i := range 40 {
		server, client := socketPair(t)
		done := make(chan struct{})
		go func() {
			defer close(done)
			h.srv.handleConn(server)
		}()
		_, _ = Register(client, hello(protocol.LeaderProtocolVersion), 2*time.Second)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatalf("attempt %d: the connection of a client after the stop was left open", i)
		}
		_ = client.Close()
	}
}

func TestRouterPostRefusesAfterStop(t *testing.T) {
	h := startHarness(t)
	require.NoError(t, h.srv.Close())
	for range 100 {
		require.False(t, h.srv.r.post(clientGoneEvent{}), "a stopped router takes no event")
	}
}
