package main

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"AskCore/internal/leader"
	"AskCore/internal/tui"
	"AskCore/pkg/protocol"
)

// linkProxy forwards the leader socket of one home to the leader of another
// home. Dropping it closes the links of the clients and keeps the leader. While
// it is stalled it takes connections and answers none, so a client waits and
// never starts a leader of its own.
type linkProxy struct {
	t      *testing.T
	ln     net.Listener
	target string

	mu      sync.Mutex
	conns   []net.Conn
	stalled bool
	held    []net.Conn
	// toLeader and fromLeader hold every byte that crossed the proxy.
	toLeader, fromLeader bytes.Buffer
}

// tapWriter copies what it writes into the buffer of the proxy.
type tapWriter struct {
	lp  *linkProxy
	dst io.Writer
	buf *bytes.Buffer
}

func (w tapWriter) Write(p []byte) (int, error) {
	w.lp.mu.Lock()
	w.buf.Write(p)
	w.lp.mu.Unlock()
	return w.dst.Write(p)
}

// frames returns the bytes sent to the leader and the bytes it sent back.
func (lp *linkProxy) frames() (toLeader, fromLeader string) {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	return lp.toLeader.String(), lp.fromLeader.String()
}

func newLinkProxy(t *testing.T, socket, target string) *linkProxy {
	t.Helper()
	ln, err := net.Listen("unix", socket)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(socket, 0o600))
	lp := &linkProxy{t: t, ln: ln, target: target}
	t.Cleanup(func() {
		_ = ln.Close()
		lp.mu.Lock()
		defer lp.mu.Unlock()
		for _, c := range append(lp.conns, lp.held...) {
			_ = c.Close()
		}
	})
	go lp.serve()
	return lp
}

func (lp *linkProxy) serve() {
	for {
		c, err := lp.ln.Accept()
		if err != nil {
			return
		}
		lp.mu.Lock()
		if lp.stalled {
			lp.held = append(lp.held, c)
			lp.mu.Unlock()
			continue
		}
		up, err := net.Dial("unix", lp.target)
		if err != nil {
			lp.mu.Unlock()
			_ = c.Close()
			continue
		}
		lp.conns = append(lp.conns, c, up)
		lp.mu.Unlock()
		go func() {
			_, _ = io.Copy(tapWriter{lp: lp, dst: up, buf: &lp.toLeader}, c)
			_ = up.Close()
			_ = c.Close()
		}()
		go func() {
			_, _ = io.Copy(tapWriter{lp: lp, dst: c, buf: &lp.fromLeader}, up)
			_ = c.Close()
			_ = up.Close()
		}()
	}
}

// drop closes every link and stalls new ones until resume.
func (lp *linkProxy) drop() {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	lp.stalled = true
	for _, c := range lp.conns {
		_ = c.Close()
	}
	lp.conns = nil
}

func (lp *linkProxy) resume() {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	lp.stalled = false
	for _, c := range lp.held {
		_ = c.Close()
	}
	lp.held = nil
}

// When the leader changed the model twice while a client was gone, the client
// that comes back shows the model it ends on and no change line for the two
// changes it missed.
func TestInteractiveReconnectDrawsNoMissedModelChanges(t *testing.T) {
	bin, home := newE2EHome(t)
	placeholders := []string{
		"ANTHROPIC_API_KEY=sk-ant-placeholder-for-readiness", "OPENAI_API_KEY=sk-placeholder-for-readiness",
	}
	env := append(withoutCredentialEnv(leaderEnv(home)), placeholders...)
	raw := openRawLeader(t, bin, home, env)
	paths, err := leader.ResolvePaths(home)
	require.NoError(t, err)

	// The client reaches the leader through a proxy home, so only its link can
	// be dropped.
	proxyHome := leaderHome(t)
	require.NoError(t, os.Mkdir(proxyHome, 0o700))
	lp := newLinkProxy(t, filepath.Join(proxyHome, "leader.sock"), paths.Socket)

	a := startAskTerm(t, bin, proxyHome, askTermOptions{})
	a.waitText("Ask anything")
	a.waitFor("the leader footer", func(rows []string) bool { return hasRow(rows, "⌂", "leader ") && hasRow(rows, "faux-1") })
	ids := liveSessionIDs(t, bin, home)
	require.Len(t, ids, 1)
	sid := ids[0]

	lp.drop()
	a.waitRow("✗ Disconnected from the leader")

	// Two changes while the client is away. The raw client takes the idle
	// session, which the leader released when the link closed.
	rawCall[protocol.ACPAttachResult](t, raw, protocol.ACPAttach, map[string]any{"sessionId": sid})
	rawCall[map[string]any](t, raw, protocol.ACPTake, map[string]any{"sessionId": sid})
	models := rawCall[protocol.ACPModelsResult](t, raw, protocol.ACPModels, map[string]any{"sessionId": sid, "usableOnly": true})
	pick := func(provider, id string) string {
		for _, m := range models.Models {
			if m.Provider == provider && m.ID == id {
				return m.ModelID
			}
		}
		t.Fatalf("model %s/%s is not listed", provider, id)
		return ""
	}
	for _, id := range []string{pick("anthropic", "claude-sonnet-4-6"), pick("openai", "gpt-5.5")} {
		rawCall[protocol.ACPSetModelResult](t, raw, protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": id})
	}
	rawCall[map[string]any](t, raw, protocol.ACPDetach, map[string]any{"sessionId": sid})

	lp.resume()
	a.waitRow("↻ Reconnected. Session " + tui.ShortID(sid) + " restored.")
	a.waitFor("the model the session ends on", func(rows []string) bool {
		return hasRow(rows, "leader ", "gpt-5.5") && hasRow(rows, "  model  openai/gpt-5.5")
	})
	// Give any replayed event time to draw a line before the check.
	time.Sleep(700 * time.Millisecond)
	require.False(t, containsRow(a.rows(), "· was "), "a missed change drew a line:\n%s", a.screen())
	require.False(t, strings.Contains(a.screen(), "model  anthropic/"), "a missed change drew a line:\n%s", a.screen())
	a.quitDetached()
}

// The leader names where the replay of a resumed follow ends, so a client can
// tell the events it missed from the ones that come after the follow.
func TestClientResumedFollowNamesReplayHead(t *testing.T) {
	bin, home := newE2EHome(t)
	env := append(withoutCredentialEnv(leaderEnv(home)),
		"ANTHROPIC_API_KEY=sk-ant-placeholder-for-readiness", "OPENAI_API_KEY=sk-placeholder-for-readiness")
	raw := openRawLeader(t, bin, home, env)
	paths, err := leader.ResolvePaths(home)
	require.NoError(t, err)
	proxyHome := leaderHome(t)
	require.NoError(t, os.Mkdir(proxyHome, 0o700))
	lp := newLinkProxy(t, filepath.Join(proxyHome, "leader.sock"), paths.Socket)

	c, col := openLeaderClient(t, bin, proxyHome)
	sess := newSessionOf(t, c)
	_, first := col.waitFor("the first snapshot", isType[tui.SnapshotMsg])
	require.Equal(t, first.(tui.SnapshotMsg).Cursor, first.(tui.SnapshotMsg).Head, "a snapshot is cut at the head")

	lp.drop()
	col.waitFor("the loss", isType[tui.DisconnectedMsg])
	rawCall[protocol.ACPAttachResult](t, raw, protocol.ACPAttach, map[string]any{"sessionId": sess.ID})
	rawCall[map[string]any](t, raw, protocol.ACPTake, map[string]any{"sessionId": sess.ID})
	models := rawCall[protocol.ACPModelsResult](t, raw, protocol.ACPModels, map[string]any{"sessionId": sess.ID, "usableOnly": true})
	var ids []string
	for _, m := range models.Models {
		if m.Provider == "anthropic" && m.ID == "claude-sonnet-4-6" || m.Provider == "openai" && m.ID == "gpt-5.5" {
			ids = append(ids, m.ModelID)
		}
	}
	require.Len(t, ids, 2)
	for _, id := range ids {
		rawCall[protocol.ACPSetModelResult](t, raw, protocol.ACPSetModel, map[string]any{"sessionId": sess.ID, "modelId": id})
	}
	rawCall[map[string]any](t, raw, protocol.ACPDetach, map[string]any{"sessionId": sess.ID})
	lp.resume()

	_, restored := col.waitFor("the restored session", isType[tui.SessionRestoredMsg])
	require.True(t, restored.(tui.SessionRestoredMsg).Resumed)
	at, snap := col.waitFor("the resumed snapshot", func(m tui.Message) bool {
		s, ok := m.(tui.SnapshotMsg)
		return ok && s.Resumed
	})
	s := snap.(tui.SnapshotMsg)
	require.Equal(t, first.(tui.SnapshotMsg).Cursor, s.Cursor, "the cursor of the client stays")
	require.Equal(t, s.Cursor.Epoch, s.Head.Epoch)
	require.GreaterOrEqual(t, s.Head.Seq, s.Cursor.Seq+2, "the two model changes lie between the cursor and the head")

	// Both replayed events arrive after the snapshot, and none lies above the head.
	var replayed []uint64
	require.Eventually(t, func() bool {
		replayed = replayed[:0]
		for _, m := range col.all()[at:] {
			if e, ok := m.(tui.SessionEventMsg); ok {
				if _, isModel := e.Event.(*protocol.ModelUpdate); isModel {
					replayed = append(replayed, e.Seq)
				}
			}
		}
		return len(replayed) == 2
	}, clientWait, 20*time.Millisecond)
	for _, seq := range replayed {
		require.Greater(t, seq, s.Cursor.Seq)
		require.LessOrEqual(t, seq, s.Head.Seq)
	}
}
