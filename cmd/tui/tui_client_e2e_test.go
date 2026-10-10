package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"AskCore/internal/leader"
	"AskCore/internal/tui"
	"AskCore/internal/tuiclient"
	"AskCore/pkg/protocol"
)

const clientWait = 30 * time.Second

// collector reads the messages of a client in the background and keeps them, so
// a test can look at the whole sequence.
type collector struct {
	t    *testing.T
	mu   sync.Mutex
	msgs []tui.Message
}

func collect(t *testing.T, c *tuiclient.Client) *collector {
	t.Helper()
	col := &collector{t: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for m := range c.Events() {
			col.mu.Lock()
			col.msgs = append(col.msgs, m)
			col.mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = c.Close()
		<-done
	})
	return col
}

func (c *collector) all() []tui.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]tui.Message(nil), c.msgs...)
}

// find returns the index and the message of the first message from index `from`
// that satisfies pred, or -1.
func (c *collector) find(from int, pred func(tui.Message) bool) (int, tui.Message) {
	msgs := c.all()
	for i := from; i < len(msgs); i++ {
		if pred(msgs[i]) {
			return i, msgs[i]
		}
	}
	return -1, nil
}

func (c *collector) waitFor(what string, pred func(tui.Message) bool) (int, tui.Message) {
	c.t.Helper()
	var idx int
	var msg tui.Message
	require.Eventuallyf(c.t, func() bool {
		idx, msg = c.find(0, pred)
		return idx >= 0
	}, clientWait, 10*time.Millisecond, "waiting for %s\nmessages so far:\n%s", what, describe(c.all()))
	return idx, msg
}

func describe(msgs []tui.Message) string {
	var b strings.Builder
	for i, m := range msgs {
		fmt.Fprintf(&b, "%d %s\n", i, msgName(m))
	}
	return b.String()
}

func msgName(m tui.Message) string {
	if e, ok := m.(tui.SessionEventMsg); ok {
		return "event:" + e.Event.EventType()
	}
	return fmt.Sprintf("%T", m)
}

func isEvent(kind string) func(tui.Message) bool {
	return func(m tui.Message) bool {
		e, ok := m.(tui.SessionEventMsg)
		return ok && e.Event.EventType() == kind
	}
}

func isType[T tui.Message](m tui.Message) bool { _, ok := m.(T); return ok }

// leaderClientConfig is the client config for the leader of a test home. env
// is added to the environment of a leader that the client starts.
func leaderClientConfig(bin, home string, env ...string) tuiclient.Config {
	cfg := e2eConnect(home, bin)
	cfg.Env = append(cfg.Env, env...)
	return tuiclient.Config{
		Dial:           tuiclient.LeaderDial(cfg),
		ClientName:     "ask-test",
		InitialBackoff: 20 * time.Millisecond,
		MaxBackoff:     200 * time.Millisecond,
	}
}

func openLeaderClient(t *testing.T, bin, home string, env ...string) (*tuiclient.Client, *collector) {
	t.Helper()
	c, err := tuiclient.Open(context.Background(), leaderClientConfig(bin, home, env...))
	require.NoError(t, err)
	return c, collect(t, c)
}

func newSessionOf(t *testing.T, c *tuiclient.Client) tui.SessionInfo {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	info, err := c.NewSession(ctx, t.TempDir())
	require.NoError(t, err)
	require.Equal(t, protocol.ACPRoleDriver, info.Role)
	return info
}

// The agent has no separate acceptance reply: session/prompt answers when the run
// settles. The client reports acceptance when the run starts, which is the
// agent_start event, and that comes before the first piece of the reply.
func TestClientPromptAcceptanceSignal(t *testing.T) {
	bin, home := newE2EHome(t)
	c, col := openLeaderClient(t, bin, home, pacedEnv(20)...)
	newSessionOf(t, c)

	id, err := c.Prompt("accept me before you settle")
	require.NoError(t, err)

	ai, accepted := col.waitFor("acceptance", isType[tui.PromptAcceptedMsg])
	require.Equal(t, id, accepted.(tui.PromptAcceptedMsg).ID)
	di, done := col.waitFor("settle", isType[tui.PromptDoneMsg])
	require.Equal(t, id, done.(tui.PromptDoneMsg).ID)
	require.Equal(t, "end_turn", done.(tui.PromptDoneMsg).StopReason)
	require.Less(t, ai, di, "acceptance comes before the settle")

	si, _ := col.find(0, isEvent(protocol.TypeAgentStart))
	ui, _ := col.find(0, isEvent(protocol.TypeMessageUpdate))
	require.GreaterOrEqual(t, si, 0)
	require.Greater(t, si, ai-1, "the acceptance message is delivered right before the run start event")
	require.Less(t, si, ui, "the run start comes before the first piece of the reply")
	require.Less(t, ui, di)
	// Exactly one acceptance and one end for the prompt.
	require.Equal(t, 1, countOf[tui.PromptAcceptedMsg](col))
	require.Equal(t, 1, countOf[tui.PromptDoneMsg](col))
	require.Zero(t, countOf[tui.PromptSendFailedMsg](col))
	require.Zero(t, countOf[tui.PromptRunErrorMsg](col))
}

func countOf[T tui.Message](c *collector) int {
	n := 0
	for _, m := range c.all() {
		if _, ok := m.(T); ok {
			n++
		}
	}
	return n
}

func TestClientRefusedPromptIsSendFailure(t *testing.T) {
	bin, home := newE2EHome(t)
	a, _ := openLeaderClient(t, bin, home)
	sess := newSessionOf(t, a)
	b, bcol := openLeaderClient(t, bin, home)
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	info, err := b.Attach(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, protocol.ACPRoleObserver, info.Role)

	id, err := b.Prompt("an observer cannot drive")
	require.NoError(t, err)
	_, msg := bcol.waitFor("a send failure", isType[tui.PromptSendFailedMsg])
	failed := msg.(tui.PromptSendFailedMsg)
	require.Equal(t, id, failed.ID)
	var re *tui.RequestError
	require.ErrorAs(t, failed.Err, &re)
	require.Equal(t, protocol.ACPCodeNotDriver, re.Code)
	require.False(t, failed.OutcomeUnknown)
	// The agent never started a run, so the prompt was not accepted.
	require.Zero(t, countOf[tui.PromptAcceptedMsg](bcol))
	require.Zero(t, countOf[tui.PromptRunErrorMsg](bcol))
	i, _ := bcol.find(0, isEvent(protocol.TypeAgentStart))
	require.Equal(t, -1, i, "no run started in the session")
	// The slot is free again: the observer may try another call.
	_, err = b.State(ctx)
	require.NoError(t, err)
}

// The faux model fails a prompt that starts with "fail". The run starts, so the
// prompt call ends with an error after the acceptance.
func TestClientRunFailureAfterAcceptanceIsRunError(t *testing.T) {
	bin, home := newE2EHome(t)
	c, col := openLeaderClient(t, bin, home)
	newSessionOf(t, c)
	id, err := c.Prompt("fail the provider said no")
	require.NoError(t, err)
	ai, _ := col.waitFor("acceptance", isType[tui.PromptAcceptedMsg])
	ri, msg := col.waitFor("a run error", isType[tui.PromptRunErrorMsg])
	require.Equal(t, id, msg.(tui.PromptRunErrorMsg).ID)
	require.Less(t, ai, ri)
	var re *tui.RequestError
	require.ErrorAs(t, msg.(tui.PromptRunErrorMsg).Err, &re)
	require.Zero(t, countOf[tui.PromptSendFailedMsg](col), "an error after acceptance is not a send failure")
	// The next prompt works: the failed run left the session usable.
	_, err = c.Prompt("hello again")
	require.NoError(t, err)
	col.waitFor("the second prompt settles", isType[tui.PromptDoneMsg])
}

func TestClientSteerFollowUpRemove(t *testing.T) {
	bin, home := newE2EHome(t)
	c, col := openLeaderClient(t, bin, home, pacedEnv(10)...)
	newSessionOf(t, c)
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()

	_, err := c.Prompt(strings.Repeat("a long answer that lasts a while ", 6))
	require.NoError(t, err)
	col.waitFor("acceptance", isType[tui.PromptAcceptedMsg])

	steerID, err := c.Steer(ctx, "go left")
	require.NoError(t, err)
	followID, err := c.FollowUp(ctx, "then say bye")
	require.NoError(t, err)
	require.NotEmpty(t, steerID)
	require.NotEmpty(t, followID)
	col.waitFor("a queue update with both rows", func(m tui.Message) bool {
		e, ok := m.(tui.SessionEventMsg)
		if !ok {
			return false
		}
		q, ok := e.Event.(*protocol.QueueUpdate)
		return ok && len(q.Steering) == 1 && len(q.FollowUp) == 1
	})
	st, err := c.State(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"go left"}, st.Steering)
	require.Equal(t, []string{"then say bye"}, st.FollowUp)

	removed, err := c.Remove(ctx, followID)
	require.NoError(t, err)
	require.True(t, removed)
	removed, err = c.Remove(ctx, followID)
	require.NoError(t, err)
	require.False(t, removed, "a row can leave the queue once")
	col.waitFor("a queue update without the follow-up", func(m tui.Message) bool {
		e, ok := m.(tui.SessionEventMsg)
		if !ok {
			return false
		}
		q, ok := e.Event.(*protocol.QueueUpdate)
		return ok && len(q.Steering) == 1 && len(q.FollowUp) == 0
	})

	require.NoError(t, c.Cancel())
	col.waitFor("the run settles", isEvent(protocol.TypeAgentSettled))
}

// A model whose provider has no saved sign-in fails its readiness check. The
// call returns the failure and the session keeps its model.
func TestClientSetModelReadinessFailure(t *testing.T) {
	bin, home := newE2EHome(t)
	// The developer machine may hold provider keys in its environment. The
	// leader of this test must not see them, or every model would be ready.
	cfg := e2eConnect(home, bin)
	cfg.Env = withoutCredentialEnv(cfg.Env)
	c, err := tuiclient.Open(context.Background(), tuiclient.Config{Dial: tuiclient.LeaderDial(cfg)})
	require.NoError(t, err)
	collect(t, c)
	newSessionOf(t, c)
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	models, err := c.Models(ctx)
	require.NoError(t, err)
	var other string
	for _, m := range models.Models {
		if m.ModelID != models.Current && m.Provider != "faux" {
			other = m.ModelID
			break
		}
	}
	require.NotEmpty(t, other, "the catalog has a model of a provider that is not signed in")
	_, err = c.SetModel(ctx, other)
	var re *tui.RequestError
	require.ErrorAs(t, err, &re, "the readiness failure comes back as a request error")
	st, err := c.State(ctx)
	require.NoError(t, err)
	require.Equal(t, models.Current, st.ModelID, "the model did not change")

	// A ready model changes.
	res, err := c.SetModel(ctx, models.Current)
	require.NoError(t, err)
	require.Equal(t, models.Current, res.ModelID)
}

// withoutCredentialEnv removes the variables that can carry a provider key.
func withoutCredentialEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		up := strings.ToUpper(name)
		if strings.Contains(up, "KEY") || strings.Contains(up, "TOKEN") || strings.Contains(up, "SECRET") ||
			strings.HasPrefix(up, "ANTHROPIC") || strings.HasPrefix(up, "OPENAI") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// dropper dials the leader and keeps the socket of the newest link, so a test can
// close only the socket while the leader stays up.
type dropper struct {
	cfg  leader.ConnectConfig
	mu   sync.Mutex
	conn net.Conn
}

func (d *dropper) Dial(ctx context.Context) (tuiclient.Link, error) {
	cl, err := leader.ConnectOrSpawn(ctx, d.cfg)
	if err != nil {
		return tuiclient.Link{}, err
	}
	d.mu.Lock()
	d.conn = cl.Conn
	d.mu.Unlock()
	return tuiclient.LinkFromLeader(cl), nil
}

func (d *dropper) drop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	_ = d.conn.Close()
}

func TestClientFollowResumeAfterSocketDrop(t *testing.T) {
	bin, home := newE2EHome(t)
	cfg := e2eConnect(home, bin)
	cfg.Env = append(cfg.Env, pacedEnv(15)...)
	d := &dropper{cfg: cfg}
	ccfg := leaderClientConfig(bin, home, pacedEnv(15)...)
	ccfg.Dial = d.Dial
	c, err := tuiclient.Open(context.Background(), ccfg)
	require.NoError(t, err)
	col := collect(t, c)
	first := c.Info()
	sess := newSessionOf(t, c)

	id, err := c.Prompt(strings.Repeat("a reply that is still going when the socket drops ", 4))
	require.NoError(t, err)
	col.waitFor("acceptance", isType[tui.PromptAcceptedMsg])
	col.waitFor("the first piece", isEvent(protocol.TypeMessageUpdate))

	d.drop() // only the socket; the leader and its run go on

	col.waitFor("the loss", isType[tui.DisconnectedMsg])
	_, restored := col.waitFor("the restored session", isType[tui.SessionRestoredMsg])
	r := restored.(tui.SessionRestoredMsg)
	require.Equal(t, sess.ID, r.Session.ID)
	require.True(t, r.Resumed, "the cursor was still valid on the same leader")
	require.Equal(t, protocol.ACPRoleDriver, r.Session.Role, "the idle-or-orphaned session is taken back")
	_, connected := col.waitFor("the second link", func(m tui.Message) bool {
		cm, ok := m.(tui.ConnectedMsg)
		return ok && cm.Info.ClientID != first.ClientID
	})
	require.Equal(t, first.InstanceID, connected.(tui.ConnectedMsg).Info.InstanceID, "the same leader")

	// The call that was in flight reports an unknown outcome and is not sent again.
	_, msg := col.waitFor("the prompt ends", func(m tui.Message) bool {
		_, run := m.(tui.PromptRunErrorMsg)
		_, send := m.(tui.PromptSendFailedMsg)
		return run || send
	})
	run, ok := msg.(tui.PromptRunErrorMsg)
	require.True(t, ok, "the run was accepted before the drop")
	require.Equal(t, id, run.ID)
	require.True(t, run.OutcomeUnknown)
	require.ErrorIs(t, run.Err, tui.ErrOutcomeUnknown)

	// The run finishes on the leader, and its events reach the client once, in order.
	col.waitFor("the run settles", isEvent(protocol.TypeAgentSettled))
	var last uint64
	seen := map[uint64]bool{}
	for _, m := range col.all() {
		e, ok := m.(tui.SessionEventMsg)
		if !ok {
			continue
		}
		require.False(t, seen[e.Seq], "event %d arrived twice", e.Seq)
		seen[e.Seq] = true
		require.Greater(t, e.Seq, last, "events arrive in order")
		last = e.Seq
	}
	// No second admission: the session holds one user message and one reply.
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	st, err := c.State(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, st.MessageCount, "the prompt was not sent again")
	status, err := leader.Status(ctx, cfg)
	require.NoError(t, err)
	require.Equal(t, first.InstanceID, status.InstanceID)
}

func TestClientSessionGoneAfterLeaderRestart(t *testing.T) {
	bin, home := newE2EHome(t)
	c, col := openLeaderClient(t, bin, home)
	first := c.Info()
	sess := newSessionOf(t, c)
	_, err := c.Prompt("remember this")
	require.NoError(t, err)
	col.waitFor("settle", isType[tui.PromptDoneMsg])
	prompts := countOf[tui.PromptAcceptedMsg](col)

	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	res, err := leader.Stop(ctx, e2eConnect(home, bin), 20*time.Second)
	require.NoError(t, err)
	require.True(t, res.WasRunning)

	col.waitFor("the loss", isType[tui.DisconnectedMsg])
	// The client starts a new leader while it reconnects.
	_, connected := col.waitFor("the new leader", func(m tui.Message) bool {
		cm, ok := m.(tui.ConnectedMsg)
		return ok && cm.Info.InstanceID != first.InstanceID
	})
	require.NotEmpty(t, connected.(tui.ConnectedMsg).Info.InstanceID)
	_, gone := col.waitFor("the session is gone", isType[tui.SessionGoneMsg])
	require.Equal(t, sess.ID, gone.(tui.SessionGoneMsg).SessionID)
	require.Zero(t, countOf[tui.SessionRestoredMsg](col))

	// Nothing was sent again, and the client made no session of its own.
	require.Equal(t, prompts, countOf[tui.PromptAcceptedMsg](col))
	status, err := leader.Status(ctx, e2eConnect(home, bin))
	require.NoError(t, err)
	require.Zero(t, status.Sessions)
	require.Zero(t, status.ActiveRuns)

	_, err = c.Prompt("into the void")
	require.ErrorIs(t, err, tui.ErrSessionGone)
	// A new session works on the new leader.
	again := newSessionOf(t, c)
	require.NotEqual(t, sess.ID, again.ID)
	_, err = c.Prompt("fresh start")
	require.NoError(t, err)
}

func TestClientTwoClientsIsolation(t *testing.T) {
	bin, home := newE2EHome(t)
	a, acol := openLeaderClient(t, bin, home)
	b, bcol := openLeaderClient(t, bin, home)
	require.NotEqual(t, a.Info().ClientID, b.Info().ClientID)
	require.Equal(t, a.Info().InstanceID, b.Info().InstanceID)
	sess := newSessionOf(t, a)
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()

	live, err := b.ListSessions(ctx)
	require.NoError(t, err)
	require.Len(t, live, 1)
	require.Equal(t, sess.ID, live[0].SessionID)
	_, err = b.State(ctx)
	require.ErrorIs(t, err, tui.ErrNoSession, "B has no current session until it attaches")

	info, err := b.Attach(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, protocol.ACPRoleObserver, info.Role)
	// Both clients number their requests from 1; each answer must reach its caller.
	var wg sync.WaitGroup
	for _, c := range []*tuiclient.Client{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				st, err := c.State(ctx)
				if err != nil || st.SessionID != sess.ID {
					t.Errorf("state: %v %v", st.SessionID, err)
					return
				}
			}
		}()
	}
	wg.Wait()

	_, err = a.Prompt("both of us see this")
	require.NoError(t, err)
	acol.waitFor("A settles", isType[tui.PromptDoneMsg])
	bcol.waitFor("B sees the settle", isEvent(protocol.TypeAgentSettled))
	require.Equal(t, eventSeqs(acol), eventSeqs(bcol), "the observer sees the same events")
	require.Zero(t, countOf[tui.PromptAcceptedMsg](bcol), "a prompt of A is not a prompt of B")
}

func eventSeqs(c *collector) []uint64 {
	var out []uint64
	for _, m := range c.all() {
		if e, ok := m.(tui.SessionEventMsg); ok {
			out = append(out, e.Seq)
		}
	}
	return out
}

func inProcessDeps(home string, extra map[string]string) runDependencies {
	return runDependencies{getenv: func(k string) string {
		if v, ok := extra[k]; ok {
			return v
		}
		if k == "ASK_HOME" {
			return home
		}
		return ""
	}}
}

func TestClientInProcessAgent(t *testing.T) {
	home := leaderHome(t)
	c, err := openInProcessClient(context.Background(), inProcessDeps(home, map[string]string{fauxPaceEnv: "30"}))
	require.NoError(t, err)
	col := collect(t, c)
	require.Equal(t, tui.ModeInProcess, c.Info().Mode)
	require.Empty(t, c.Info().InstanceID, "an in-process agent has no leader id")
	newSessionOf(t, c)
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()

	// Prompt and stream.
	id, err := c.Prompt("hello in process")
	require.NoError(t, err)
	col.waitFor("acceptance", isType[tui.PromptAcceptedMsg])
	_, done := col.waitFor("settle", isType[tui.PromptDoneMsg])
	require.Equal(t, id, done.(tui.PromptDoneMsg).ID)
	require.GreaterOrEqual(t, countOf[tui.SessionEventMsg](col), 4)

	// A tool call.
	_, err = c.Prompt("echo tool line")
	require.NoError(t, err)
	col.waitFor("the tool ran", isEvent(protocol.TypeToolExecutionEnd))
	require.Eventually(t, func() bool { return countOf[tui.PromptDoneMsg](col) == 2 }, clientWait, 10*time.Millisecond,
		"the prompt call ends after the run settles")

	// Cancel a long run, with the queue.
	waitIdle := func() {
		require.Eventually(t, func() bool {
			st, err := c.State(ctx)
			return err == nil && !st.Running
		}, clientWait, 20*time.Millisecond)
	}
	waitIdle()
	_, err = c.Prompt(strings.Repeat("a long answer to cancel ", 12))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		st, err := c.State(ctx)
		return err == nil && st.Running
	}, clientWait, 10*time.Millisecond)
	inputID, err := c.FollowUp(ctx, "later")
	require.NoError(t, err)
	st, err := c.State(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"later"}, st.FollowUp)
	removed, err := c.Remove(ctx, inputID)
	require.NoError(t, err)
	require.True(t, removed)
	require.NoError(t, c.Cancel())
	col.waitFor("the cancelled prompt", func(m tui.Message) bool {
		d, ok := m.(tui.PromptDoneMsg)
		return ok && d.StopReason == "cancelled"
	})

	// Models and model change.
	waitIdle()
	models, err := c.Models(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, models.Models)
	res, err := c.SetModel(ctx, models.Current)
	require.NoError(t, err)
	require.Equal(t, models.Current, res.ModelID)
	_, err = c.Usage(ctx)
	require.NoError(t, err)

	// The calls that need a leader say so.
	_, err = c.ListSessions(ctx)
	require.ErrorIs(t, err, tui.ErrNeedsLeader)
	_, err = c.Attach(ctx, "any")
	require.ErrorIs(t, err, tui.ErrNeedsLeader)
	require.ErrorIs(t, c.Detach(ctx), tui.ErrNeedsLeader)
	_, err = c.Take(ctx)
	require.ErrorIs(t, err, tui.ErrNeedsLeader)

	// Closing stops the agent and ends the event stream.
	require.NoError(t, c.Close())
	require.Eventually(t, func() bool { _, open := <-c.Events(); return !open }, 5*time.Second, time.Millisecond)
}

// makeHomeUnsafe turns the home into a symlink, which the leader code refuses to
// follow.
func makeHomeUnsafe(t *testing.T, home string) {
	t.Helper()
	require.NoError(t, os.Symlink(t.TempDir(), home))
}

func TestClientStartupFallbackOnlyAtStartup(t *testing.T) {
	bin, home := newE2EHome(t)
	opts := tuiClientOptions{callerExe: bin, env: leaderEnv(home), initialBackoff: 20 * time.Millisecond, maxBackoff: 100 * time.Millisecond}
	deps := inProcessDeps(home, nil)

	// An unsafe home at startup selects the in-process agent.
	unsafe := leaderHome(t)
	makeHomeUnsafe(t, unsafe)
	fb, err := openTUIClient(context.Background(), inProcessDeps(unsafe, nil), tuiClientOptions{callerExe: bin, env: leaderEnv(unsafe)})
	require.NoError(t, err)
	require.Equal(t, tui.ModeInProcess, fb.Info().Mode)
	require.ErrorIs(t, fb.FallbackReason, leader.ErrUnsafeEndpoint)
	require.NoError(t, fb.Close())

	// A healthy home selects the leader.
	ok, err := openTUIClient(context.Background(), deps, opts)
	require.NoError(t, err)
	require.NoError(t, ok.FallbackReason)
	require.Equal(t, tui.ModeLeader, ok.Info().Mode)
	require.NotEmpty(t, ok.Info().InstanceID)
	col := collect(t, ok.Client)

	// After the start, a leader that cannot be reached is retried, never replaced.
	res, err := leader.Stop(context.Background(), e2eConnect(home, bin), 20*time.Second)
	require.NoError(t, err)
	require.True(t, res.WasRunning)
	col.waitFor("the loss", isType[tui.DisconnectedMsg])
	// The home turns unsafe, so every attempt fails.
	_ = os.RemoveAll(home)
	makeHomeUnsafe(t, home)
	require.Eventually(t, func() bool {
		return countOf[tui.ReconnectingMsg](col) >= 3
	}, clientWait, 20*time.Millisecond, "the client keeps trying")
	for _, m := range col.all() {
		if cm, isConnected := m.(tui.ConnectedMsg); isConnected {
			require.Equal(t, tui.ModeLeader, cm.Info.Mode, "no in-process link after the start")
		}
	}
	require.Equal(t, tui.ModeLeader, ok.Info().Mode)
	for _, m := range col.all() {
		if r, isRe := m.(tui.ReconnectingMsg); isRe {
			require.Error(t, r.Err)
			require.False(t, errors.Is(r.Err, tui.ErrNeedsLeader))
		}
	}
}
