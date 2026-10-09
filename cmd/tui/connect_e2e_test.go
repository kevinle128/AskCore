package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"AskCore/internal/leader"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

const connectWait = 20 * time.Second

// pacedEnv slows the faux model, so that a run lasts long enough to act in the middle of it.
func pacedEnv(tokensPerSecond int) []string {
	return []string{"ASK_FAUX_TPS=" + strconv.Itoa(tokensPerSecond)}
}

// connectProc is a real `ask connect` process. Its stdin stays open until the
// test ends it, so the process reads commands as the test sends them.
type connectProc struct {
	t     *testing.T
	cmd   *exec.Cmd
	stdin io.WriteCloser
	done  chan struct{}
	err   error

	mu    sync.Mutex
	lines []string
}

func startConnect(t *testing.T, bin, home string, extraEnv []string, args ...string) *connectProc {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"connect"}, args...)...)
	cmd.Env = append(leaderEnv(home), extraEnv...)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	var errOut strings.Builder
	cmd.Stderr = &errOut
	require.NoError(t, cmd.Start())
	p := &connectProc{t: t, cmd: cmd, stdin: stdin, done: make(chan struct{})}
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
		for sc.Scan() {
			p.mu.Lock()
			p.lines = append(p.lines, sc.Text())
			p.mu.Unlock()
		}
		p.err = cmd.Wait()
		close(p.done)
	}()
	t.Cleanup(func() {
		_ = stdin.Close()
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-p.done
		}
	})
	return p
}

func (p *connectProc) send(line string) {
	p.t.Helper()
	_, err := io.WriteString(p.stdin, line+"\n")
	require.NoError(p.t, err)
}

func (p *connectProc) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.lines...)
}

// waitLine waits for a line that satisfies pred and returns it.
func (p *connectProc) waitLine(what string, pred func(string) bool) string {
	p.t.Helper()
	var found string
	require.Eventuallyf(p.t, func() bool {
		for _, l := range p.snapshot() {
			if pred(l) {
				found = l
				return true
			}
		}
		return false
	}, connectWait, 10*time.Millisecond, "%s\noutput so far:\n%s", what, strings.Join(p.snapshot(), "\n"))
	return found
}

func hasPrefix(prefix string) func(string) bool {
	return func(l string) bool { return strings.HasPrefix(l, prefix) }
}

func (p *connectProc) count(prefix string) int {
	n := 0
	for _, l := range p.snapshot() {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

// field reads key=value from an output line. A quoted value is returned unquoted.
func field(line, key string) string {
	for _, part := range strings.Fields(line) {
		if v, ok := strings.CutPrefix(part, key+"="); ok {
			if u, err := strconv.Unquote(v); err == nil {
				return u
			}
			return v
		}
	}
	return ""
}

func (p *connectProc) sessionID() string {
	return field(p.waitLine("a session line", hasPrefix("session id=")), "id")
}

// exitCode waits for the process to end and returns its exit code.
func (p *connectProc) exitCode() int {
	p.t.Helper()
	select {
	case <-p.done:
	case <-time.After(connectWait):
		p.t.Fatalf("the process did not end\n%s", strings.Join(p.snapshot(), "\n"))
	}
	if p.err == nil {
		return 0
	}
	var ee *exec.ExitError
	require.ErrorAs(p.t, p.err, &ee)
	return ee.ExitCode()
}

func (p *connectProc) kill() {
	_ = p.cmd.Process.Kill()
	<-p.done
}

// newE2EHome returns a home with a leader that stops with the test.
func newE2EHome(t *testing.T) (bin, home string) {
	t.Helper()
	bin = acpBinary(t)
	home = leaderHome(t)
	stopLeaderAtEnd(t, bin, home)
	return bin, home
}

// updateTexts joins the text of every update line. The text is the last field of
// the line and is quoted, so it can hold spaces.
func updateTexts(p *connectProc) string {
	var b strings.Builder
	for _, l := range p.snapshot() {
		if !strings.HasPrefix(l, "update ") {
			continue
		}
		_, quoted, found := strings.Cut(l, " text=")
		if !found {
			continue
		}
		if text, err := strconv.Unquote(quoted); err == nil {
			b.WriteString(text)
		}
	}
	return b.String()
}

// The roadmap exit: two `ask` clients share one auto-started leader. The first
// starts it, the second joins, and both see the same updates of one run.
func TestLeaderE2ETwoClientsShareLeader(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startConnect(t, bin, home, nil)
	connA := a.waitLine("connected", hasPrefix("connected "))
	sid := a.sessionID()

	b := startConnect(t, bin, home, nil, "--attach", sid)
	connB := b.waitLine("connected", hasPrefix("connected "))
	require.Equal(t, field(connA, "instance"), field(connB, "instance"), "one leader for both")
	require.NotEqual(t, field(connA, "client"), field(connB, "client"))
	require.Equal(t, protocol.ACPRoleObserver, field(b.waitLine("session", hasPrefix("session id=")), "role"))

	a.send("hello shared world")
	a.waitLine("result", hasPrefix("result session="+sid+" stop=end_turn"))
	require.Eventually(t, func() bool { return b.count("update ") == a.count("update ") && a.count("update ") > 0 }, connectWait, 20*time.Millisecond,
		"the observer sees every update of the run")
	require.Equal(t, updateTexts(a), updateTexts(b))
	require.Contains(t, updateTexts(a), "hello shared world", "the model said the prompt back")
	require.Len(t, leaderProcesses(bin), 1, "one leader process")

	a.send("/quit")
	b.send("/quit")
	require.Zero(t, a.exitCode())
	require.Zero(t, b.exitCode())
	paths, _ := leader.ResolvePaths(home)
	require.True(t, leader.Serving(paths), "leaving a client does not stop the leader")
}

func TestLeaderE2EIndependentSessions(t *testing.T) {
	bin, home := newE2EHome(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	a := startConnect(t, bin, home, nil, "--cwd", dirA)
	b := startConnect(t, bin, home, nil, "--cwd", dirB)
	sa, sb := a.sessionID(), b.sessionID()
	require.NotEqual(t, sa, sb)

	a.send("/sessions")
	a.waitLine("sessions", hasPrefix("sessions count=2"))
	live := a.snapshot()
	rows := map[string]string{}
	for _, l := range live {
		if strings.HasPrefix(l, "live ") {
			rows[field(l, "id")] = field(l, "cwd")
		}
	}
	realA, _ := filepath.EvalSymlinks(dirA)
	realB, _ := filepath.EvalSymlinks(dirB)
	require.Contains(t, []string{dirA, realA}, rows[sa])
	require.Contains(t, []string{dirB, realB}, rows[sb])

	a.send("only for a")
	a.waitLine("result", hasPrefix("result session="+sa))
	b.send("/sessions")
	b.waitLine("sessions", hasPrefix("sessions count=2"))
	require.Zero(t, b.count("update "), "B never joined the session of A")
}

func TestLeaderE2EObserverCannotDriveAndFailedAttachKeepsView(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startConnect(t, bin, home, nil)
	sid := a.sessionID()
	b := startConnect(t, bin, home, nil, "--attach", sid)
	b.waitLine("observer", func(l string) bool { return strings.HasPrefix(l, "session id=") && field(l, "role") == "observer" })

	b.send("try to drive")
	b.waitLine("refused", func(l string) bool {
		return strings.HasPrefix(l, "error session="+sid+" op=prompt") && field(l, "code") == strconv.Itoa(protocol.ACPCodeNotDriver)
	})

	// A failed attach leaves the current session as it was.
	a.send("/attach no-such-session")
	a.waitLine("attach refused", func(l string) bool { return strings.HasPrefix(l, "error op=attach") && field(l, "code") == "-32002" })
	a.send("still mine")
	a.waitLine("result in the old session", hasPrefix("result session="+sid+" stop=end_turn"))
}

func TestLeaderE2EDriverKilledRunSurvivesAndAnotherTakes(t *testing.T) {
	bin, home := newE2EHome(t)
	env := pacedEnv(25)
	a := startConnect(t, bin, home, env)
	connA := a.waitLine("connected", hasPrefix("connected "))
	sid := a.sessionID()
	b := startConnect(t, bin, home, env, "--attach", sid)
	b.waitLine("attached", hasPrefix("session id="))
	b.send("/follow")
	b.waitLine("following", hasPrefix("follow session="))

	a.send(strings.Repeat("a long answer that takes a few seconds ", 6))
	b.waitLine("the run started", hasPrefix("update session="+sid))
	a.kill() // the driver dies in the middle of the run

	b.waitLine("the run still settles", func(l string) bool {
		return strings.HasPrefix(l, "event session="+sid) && field(l, "type") == "agent_settled"
	})
	paths, _ := leader.ResolvePaths(home)
	require.True(t, leader.Serving(paths))
	st, err := leader.Status(context.Background(), e2eConnect(home, bin))
	require.NoError(t, err)
	require.Equal(t, field(connA, "instance"), st.InstanceID, "the same leader instance")
	require.Equal(t, 0, st.ActiveRuns)

	// The driver is gone, so any client may take the session, and then it can prompt.
	b.send("/take")
	b.waitLine("driver", func(l string) bool {
		return strings.HasPrefix(l, "session id=") && field(l, "role") == "driver" && field(l, "generation") != ""
	})
	b.send("after the takeover")
	b.waitLine("result", hasPrefix("result session="+sid+" stop=end_turn"))
}

func TestLeaderE2EBusyForeignSwitchRejected(t *testing.T) {
	bin, home := newE2EHome(t)
	env := pacedEnv(25)
	a := startConnect(t, bin, home, env)
	sid := a.sessionID()
	b := startConnect(t, bin, home, env, "--attach", sid)
	b.waitLine("attached", hasPrefix("session id="))

	a.send(strings.Repeat("keep the model busy for a while ", 6))
	a.waitLine("the run started", hasPrefix("update session="+sid))
	b.send("/take")
	b.waitLine("take refused", func(l string) bool {
		return strings.HasPrefix(l, "error op=take") && field(l, "code") == strconv.Itoa(protocol.ACPCodeBusy)
	})
	a.waitLine("A still drives to the end", hasPrefix("result session="+sid+" stop=end_turn"))
}

func TestLeaderE2ECancelDuringPendingPrompt(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startConnect(t, bin, home, pacedEnv(10))
	sid := a.sessionID()
	a.send(strings.Repeat("a very long answer that must be cut short ", 10))
	a.waitLine("the run started", hasPrefix("update session="+sid))
	a.send("/cancel") // the input is usable while the prompt is pending
	a.waitLine("cancelled", hasPrefix("result session="+sid+" stop=cancelled"))
}

func TestLeaderE2EQuitDuringRunLeavesTheRun(t *testing.T) {
	bin, home := newE2EHome(t)
	env := pacedEnv(25)
	a := startConnect(t, bin, home, env)
	sid := a.sessionID()
	b := startConnect(t, bin, home, env, "--attach", sid)
	b.waitLine("attached", hasPrefix("session id="))
	b.send("/follow")
	b.waitLine("following", hasPrefix("follow session="))

	a.send(strings.Repeat("this run belongs to the leader ", 6))
	a.waitLine("the run started", hasPrefix("update session="+sid))
	a.send("/quit")
	require.Zero(t, a.exitCode(), "leaving detaches only")
	b.waitLine("the run settles without its client", func(l string) bool {
		return strings.HasPrefix(l, "event session="+sid) && field(l, "type") == "agent_settled"
	})
}

func TestLeaderE2EEndOfInputWaitsForThePrompt(t *testing.T) {
	bin, home := newE2EHome(t)
	cmd := exec.Command(bin, "connect")
	cmd.Env = append(leaderEnv(home), pacedEnv(40)...)
	cmd.Stdin = strings.NewReader("say this and then the input ends\n")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "result session=")
	require.Contains(t, string(out), "stop=end_turn")
	require.True(t, strings.HasSuffix(strings.TrimSpace(string(out)), "bye"))
}

func TestLeaderE2ESessionSurvivesWithoutClients(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startConnect(t, bin, home, nil)
	connA := a.waitLine("connected", hasPrefix("connected "))
	sid := a.sessionID()
	a.send("remember this line")
	a.waitLine("result", hasPrefix("result session="+sid))
	a.send("/quit")
	require.Zero(t, a.exitCode())
	// No client is connected now.
	paths, _ := leader.ResolvePaths(home)
	require.Eventually(t, func() bool {
		st, err := leader.Status(context.Background(), e2eConnect(home, bin))
		return err == nil && st.Sessions == 1 && st.Clients <= 1
	}, connectWait, 50*time.Millisecond)
	require.True(t, leader.Serving(paths))

	b := startConnect(t, bin, home, nil, "--attach", sid)
	connB := b.waitLine("connected", hasPrefix("connected "))
	require.Equal(t, field(connA, "instance"), field(connB, "instance"))
	b.send("/follow")
	line := b.waitLine("follow", hasPrefix("follow session="))
	require.NotEqual(t, "0", field(line, "entries"), "the history of the session is still there")
}

func TestLeaderE2EReconnectFollowsFromCursor(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startConnect(t, bin, home, nil)
	sid := a.sessionID()
	a.send("/follow")
	first := a.waitLine("follow", hasPrefix("follow session="))
	a.send("first run")
	a.waitLine("result", hasPrefix("result session="+sid))
	var epoch, seq string
	require.Eventually(t, func() bool {
		for _, l := range a.snapshot() {
			if strings.HasPrefix(l, "event ") && field(l, "type") == "agent_settled" {
				seq = field(l, "seq")
				return true
			}
		}
		return false
	}, connectWait, 20*time.Millisecond)
	epoch = field(first, "epoch")
	require.NotEmpty(t, seq)
	a.send("/quit")
	require.Zero(t, a.exitCode())

	// A new client has a new id. It attaches explicitly and follows from the last cursor.
	b := startConnect(t, bin, home, nil, "--attach", sid)
	b.waitLine("attached", hasPrefix("session id="))
	b.send(fmt.Sprintf("/follow %s %s", epoch, seq))
	resumed := b.waitLine("follow", hasPrefix("follow session="))
	require.Equal(t, "true", field(resumed, "resumed"), "the follow continues from the cursor")
	require.Equal(t, "false", field(resumed, "resync"))
	require.Zero(t, b.count("result "), "no prompt was sent again")
	st, err := leader.Status(context.Background(), e2eConnect(home, bin))
	require.NoError(t, err)
	require.Equal(t, 0, st.ActiveRuns, "no duplicate admission")

	// A stale epoch gets an explicit resync and a new baseline.
	b.send("/unfollow")
	b.waitLine("unfollowed", hasPrefix("unfollowed "))
	b.send("/follow stale-epoch 1")
	stale := b.waitLine("second follow", func(l string) bool {
		return strings.HasPrefix(l, "follow ") && field(l, "sub") != field(resumed, "sub")
	})
	require.Equal(t, "true", field(stale, "resync"))
}

func TestLeaderE2EIDsNeverCollide(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startConnect(t, bin, home, nil)
	b := startConnect(t, bin, home, nil)
	a.sessionID()
	b.sessionID()
	const n = 40
	var wg sync.WaitGroup
	for _, p := range []*connectProc{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range n {
				p.send("/sessions") // every client numbers its requests from 1
			}
		}()
	}
	wg.Wait()
	require.Eventually(t, func() bool { return a.count("sessions count=2") == n && b.count("sessions count=2") == n }, connectWait, 20*time.Millisecond,
		"every request got its own answer: A %d, B %d", a.count("sessions count=2"), b.count("sessions count=2"))
	require.Zero(t, a.count("error "))
	require.Zero(t, b.count("error "))
}

// A client that does not read must not hold up the others. The slow client is a
// raw socket peer next to the real `ask connect` process, with an explicit latch.
func TestLeaderE2ESlowClientDoesNotBlockOthers(t *testing.T) {
	bin, home := newE2EHome(t)
	a := startConnect(t, bin, home, pacedEnv(0)) // 0: no pacing
	connA := a.waitLine("connected", hasPrefix("connected "))
	sid := a.sessionID()
	paths, _ := leader.ResolvePaths(home)

	conn, err := net.Dial("unix", paths.Socket)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	reg, err := leader.Register(conn, protocol.LeaderRegister{ClientKind: "slow", ProtocolVersion: protocol.LeaderProtocolVersion, Build: "e2e"}, 5*time.Second)
	require.NoError(t, err)
	call := func(id int, method string, params any) {
		raw := rpcBodyFor(id, method, params)
		require.NoError(t, reg.Writer.Write(protocol.LeaderFrame{Type: protocol.LeaderFrameACP, Payload: raw}))
		for {
			f, err := reg.Reader.Next()
			require.NoError(t, err)
			var m struct {
				ID     *int   `json:"id"`
				Method string `json:"method"`
			}
			if f.Type == protocol.LeaderFrameACP && json.Unmarshal(f.Payload, &m) == nil && m.Method == "" && m.ID != nil && *m.ID == id {
				return
			}
		}
	}
	call(1, "initialize", map[string]any{"protocolVersion": 1})
	call(2, protocol.ACPAttach, map[string]any{"sessionId": sid})
	// From here the raw peer reads nothing until the latch opens.

	// 32 KiB said back by the model is about 2000 updates, some 800 KB for each run.
	big := strings.Repeat("0123456789abcdef ", 32*1024/17)
	const runs = 4
	for i := 1; i <= runs; i++ {
		a.send(big) // one run at a time: a session takes one run
		require.Eventually(t, func() bool { return a.count("result session="+sid) == i }, connectWait, 20*time.Millisecond,
			"the healthy client finished run %d while the other one did not read", i)
	}
	updates := a.count("update ")
	require.Greater(t, updates, 0)

	st, err := leader.Status(context.Background(), e2eConnect(home, bin))
	require.NoError(t, err)
	require.Equal(t, field(connA, "instance"), st.InstanceID, "the leader is alive and is the same one")

	// Open the latch: the slow client reads every retained frame, in order.
	got := 0
	var slowText strings.Builder
	_ = conn.SetReadDeadline(time.Now().Add(connectWait))
	for got < updates {
		f, err := reg.Reader.Next()
		require.NoError(t, err, "after %d of %d updates", got, updates)
		var m struct {
			Method string `json:"method"`
			Params struct {
				Update struct {
					Content struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"update"`
			} `json:"params"`
		}
		if f.Type == protocol.LeaderFrameACP && json.Unmarshal(f.Payload, &m) == nil && m.Method == "session/update" {
			got++
			slowText.WriteString(m.Params.Update.Content.Text)
		}
	}
	require.Equal(t, updates, got)
	require.Equal(t, updateTexts(a), slowText.String(), "the slow client got the same frames in the same order")
}

func rpcBodyFor(id int, method string, params any) []byte {
	return rpcBody(map[string]any{"id": id, "method": method, "params": params})
}

// The transport fixture sends a Follow result and its first notifications in
// one socket write. The client is the built ask binary.
func TestConnectE2EFollowResultCommitsBeforeNextFrames(t *testing.T) {
	bin := acpBinary(t)
	home := leaderHome(t)
	paths, err := leader.ResolvePaths(home)
	require.NoError(t, err)
	require.NoError(t, leader.EnsureHome(home))
	ln, err := net.Listen("unix", paths.Socket)
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	served := make(chan error, 1)
	go func() {
		served <- serveFollowBurst(ln)
	}()
	p := startConnect(t, bin, home, []string{"GOMAXPROCS=1"})
	require.Equal(t, "s1", p.sessionID())
	const follows = 30
	for range follows {
		p.send("/follow")
	}
	require.Eventually(t, func() bool {
		return p.count("follow ") == follows && p.count("event ") == follows && p.count("resync ") == follows
	}, connectWait, time.Millisecond, "the client must keep the first event and resync of each subscription")
	p.send("/quit")
	require.Zero(t, p.exitCode())
	select {
	case err := <-served:
		require.NoError(t, err)
	case <-time.After(connectWait):
		t.Fatal("transport fixture did not finish")
	}
}

func serveFollowBurst(ln net.Listener) error {
	conn, err := ln.Accept()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	acc, err := leader.Accept(conn, leader.ServerConfig{Ready: true, InstanceID: "follow-burst", Build: "dev"}, "client1")
	if err != nil {
		return err
	}
	for sub := 0; ; {
		frame, err := acc.Reader.Next()
		if err != nil {
			return err
		}
		if frame.Type == protocol.LeaderFrameDisconnect {
			return nil
		}
		var request incoming
		if err := json.Unmarshal(frame.Payload, &request); err != nil {
			return err
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1}
		case "session/new":
			result = map[string]any{"sessionId": "s1"}
		case protocol.ACPFollow:
			sub++
			result = protocol.ACPFollowResult{SubscriptionID: fmt.Sprintf("sub%d", sub), Cursor: protocol.ACPCursor{Epoch: "e1", Seq: 1}}
		default:
			return fmt.Errorf("unexpected method %q", request.Method)
		}
		var batch bytes.Buffer
		writer := leader.NewFrameWriter(&batch, protocol.LeaderMaxFrame)
		if err := writer.Write(protocol.LeaderFrame{Type: protocol.LeaderFrameACP, Payload: rpcBody(map[string]any{"id": request.ID, "result": result})}); err != nil {
			return err
		}
		if request.Method == protocol.ACPFollow {
			for _, notification := range []map[string]any{
				{"method": protocol.ACPEvent, "params": map[string]any{"subscriptionId": fmt.Sprintf("sub%d", sub), "sessionId": "s1", "epoch": "e1", "seq": "2", "event": map[string]any{"type": "agent_settled"}}},
				{"method": protocol.ACPResync, "params": map[string]any{"subscriptionId": fmt.Sprintf("sub%d", sub), "sessionId": "s1", "reason": "gap", "cursor": map[string]any{"epoch": "e1", "seq": "3"}}},
			} {
				if err := writer.Write(protocol.LeaderFrame{Type: protocol.LeaderFrameACP, Payload: rpcBody(notification)}); err != nil {
					return err
				}
			}
		}
		if _, err := conn.Write(batch.Bytes()); err != nil {
			return err
		}
	}
}
