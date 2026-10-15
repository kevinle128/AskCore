package agent_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/bus"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

// followed is one received follow event.
type followed struct {
	seq uint64
	ev  protocol.Event
}

// takeAll reads the follower until it ends and decodes every event.
func takeAll(t *testing.T, f *agent.Follow) []followed {
	t.Helper()
	var out []followed
	for it := range f.Events.Events() {
		ev, err := protocol.DecodeEvent(it.Data)
		require.NoError(t, err)
		out = append(out, followed{seq: it.Seq, ev: ev})
	}
	return out
}

// takeReady reads what the follower holds now.
func takeReady(t *testing.T, f *agent.Follow) []followed {
	t.Helper()
	var out []followed
	for {
		select {
		case it, ok := <-f.Events.Events():
			if !ok {
				return out
			}
			ev, err := protocol.DecodeEvent(it.Data)
			require.NoError(t, err)
			out = append(out, followed{seq: it.Seq, ev: ev})
		default:
			return out
		}
	}
}

func followLabels(evs []followed) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = label(e.ev)
	}
	return out
}

func requireContiguous(t *testing.T, from uint64, evs []followed) {
	t.Helper()
	for i, e := range evs {
		require.Equal(t, from+uint64(i), e.seq, "event %d (%s)", i, label(e.ev))
		require.Equal(t, e.seq, e.ev.Env().Seq)
	}
}

func messageEntries(entries []sessions.Entry) []protocol.Message {
	var out []protocol.Message
	for _, e := range entries {
		if m, ok := e.(sessions.MessageEntry); ok {
			out = append(out, m.Message)
		}
	}
	return out
}

func TestFollowGivesSnapshotThenEventsWithoutGapOrDuplicate(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, withLog(log))
	require.NoError(t, a.Prompt(context.Background(), user("a")))
	rec := &recorder{}
	a.Subscribe(rec.emit)

	f := a.Follow(agent.Cursor{})
	defer f.Events.Close()
	require.NoError(t, a.Prompt(context.Background(), user("b")))

	// The snapshot is the whole log of the first run; the cursor is its last event.
	assert.Equal(t, roles(messageEntries(f.Entries)), []string{"user", "assistant"})
	assert.Equal(t, rec.events[0].Env().Seq-1, f.Cursor.Seq)
	assert.NotEmpty(t, f.Cursor.Epoch)
	assert.Nil(t, f.Stream, "the agent was idle")
	f.Events.Close()
	got := takeAll(t, f)
	requireContiguous(t, f.Cursor.Seq+1, got)
	assert.Equal(t, rec.eventLabels(), followLabels(got), "every event once, in order")
	assert.NoError(t, f.Events.Err())

	// A client that applies the events to the snapshot gets the log.
	rebuilt := messageEntries(f.Entries)
	for _, e := range got {
		if me, ok := e.ev.(*protocol.MessageEnd); ok {
			rebuilt = append(rebuilt, me.Message)
		}
	}
	require.Len(t, rebuilt, 4)
	assert.JSONEq(t, toJSON(t, log.Messages()), toJSON(t, rebuilt))
}

func TestFollowJoinMidRunSplitsSnapshotAndEventsAtOneCut(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("answer"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, withLog(log))
	rec := &recorder{}
	a.Subscribe(rec.emit)
	var f *agent.Follow
	a.Subscribe(func(ev protocol.Event) error {
		if label(ev) == "message_end(user)" {
			f = a.Follow(agent.Cursor{})
		}
		return nil
	})

	require.NoError(t, a.Prompt(context.Background(), user("hi")))

	require.NotNil(t, f)
	f.Events.Close()
	got := takeAll(t, f)
	// The listener runs inside the dispatch of message_end(user): that event is
	// not yet published, so it comes through the follower, not the snapshot. The
	// cut is the message_start(user) that was published before it, and the
	// message was committed after that publication.
	require.NotEmpty(t, got)
	assert.Equal(t, "message_end(user)", label(got[0].ev))
	requireContiguous(t, f.Cursor.Seq+1, got)
	assert.Equal(t, []string{"message_start(user)"}, []string{label(rec.events[f.Cursor.Seq-1])})
	assert.Empty(t, messageEntries(f.Entries), "no message entry is in the snapshot at that cut")
	// seq of the last event in the snapshot is the seq before the first follower event.
	assert.Equal(t, got[0].seq-1, f.Cursor.Seq)
}

// hookLog is a log that calls after, with the entries, once an Append is stored.
type hookLog struct {
	sessions.MemoryLog
	mu    sync.Mutex
	after func([]sessions.Entry)
}

func (h *hookLog) Append(entries ...sessions.Entry) (sessions.CommitRef, error) {
	ref, err := h.MemoryLog.Append(entries...)
	h.mu.Lock()
	after := h.after
	h.mu.Unlock()
	if err == nil && after != nil {
		after(entries)
	}
	return ref, err
}

func TestFollowJoinBetweenCommitAndPublishGetsMessageOnce(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("answer"))
	log := &hookLog{}
	a := newAgent(t, p, m, withLog(log))
	var f *agent.Follow
	log.after = func(entries []sessions.Entry) {
		for _, e := range entries {
			if me, ok := e.(sessions.MessageEntry); ok && me.Message.Role() == protocol.RoleAssistant && f == nil {
				f = a.Follow(agent.Cursor{})
			}
		}
	}

	require.NoError(t, a.Prompt(context.Background(), user("hi")))

	require.NotNil(t, f)
	assert.Equal(t, []string{"user"}, roles(messageEntries(f.Entries)),
		"the assistant message is committed but its message_end is not published: not in the snapshot")
	f.Events.Close()
	got := takeAll(t, f)
	ends := 0
	for _, e := range got {
		if label(e.ev) == "message_end(assistant)" {
			ends++
		}
	}
	assert.Equal(t, 1, ends, "the follower gets the message once; a snapshot cut at the writer head would double it")
	requireContiguous(t, f.Cursor.Seq+1, got)
}

const fiveDeltas = "ABCDEFGHIJKLMNOPQRST" // four characters per faux delta

// deltaOf returns the delta of a text_delta or toolcall_delta update.
func deltaOf(ev protocol.Event) (string, bool) {
	if u, ok := ev.(*protocol.MessageUpdate); ok {
		switch d := u.AssistantMessageEvent.(type) {
		case protocol.TextDeltaEvent:
			return d.Delta, true
		case protocol.ToolCallDeltaEvent:
			return d.Delta, true
		}
	}
	return "", false
}

// joinAtDelta runs a prompt and joins the follow while the third delta is
// published, so the cut is after the second. It returns what the follower
// holds once the run is over, every published event, and the committed log.
func joinAtDelta(t *testing.T, step faux.Step) (f *agent.Follow, all []followed, got []followed, log *sessions.MemoryLog) {
	t.Helper()
	p, m := newFaux(t, faux.WithChunk(1, 1))
	p.Set(step, faux.Say("done"))
	log = &sessions.MemoryLog{}
	a := newAgent(t, p, m, withLog(log))
	n := 0
	a.Subscribe(func(ev protocol.Event) error {
		all = append(all, followed{seq: ev.Env().Seq, ev: ev})
		if _, ok := deltaOf(ev); ok {
			if n++; n == 3 {
				f = a.Follow(agent.Cursor{})
			}
		}
		return nil
	})
	require.NoError(t, a.Prompt(context.Background(), user("hi")))
	require.NotNil(t, f)
	f.Events.Close()
	return f, all, takeAll(t, f), log
}

// seqOfDelta is the seq of the nth delta of the run.
func seqOfDelta(all []followed, n int) uint64 {
	for _, e := range all {
		if _, ok := deltaOf(e.ev); ok {
			if n--; n == 0 {
				return e.seq
			}
		}
	}
	return 0
}

// rebuildFromBaseline applies the updates of got to a builder that the baseline
// seeds, and returns its message before message_end, with the message_end count.
func rebuildFromBaseline(t *testing.T, f *agent.Follow, got []followed) (protocol.AssistantMessage, *protocol.Builder, int) {
	t.Helper()
	b := protocol.NewBuilder()
	require.NoError(t, b.ResumeFrom(f.Stream.Partial))
	ends := 0
	for _, e := range got {
		switch ev := e.ev.(type) {
		case *protocol.MessageUpdate:
			require.NoError(t, b.ApplyAgentEvent(ev))
		case *protocol.MessageEnd:
			if ev.Message.Role() == protocol.RoleAssistant {
				ends++
				return b.Snapshot(), b, ends
			}
		}
	}
	return b.Snapshot(), b, ends
}

func TestFollowJoinDuringStreamGetsBaselineThenDeltas(t *testing.T) {
	f, all, got, log := joinAtDelta(t, faux.Say(fiveDeltas))

	require.NotNil(t, f.Stream, "an assistant message is open at the cut")
	second := seqOfDelta(all, 2)
	assert.Equal(t, second, f.Stream.Seq, "the watermark is the seq of delta 2")
	assert.Equal(t, second, f.Cursor.Seq)
	assert.NotEmpty(t, f.Stream.AttemptID)
	baseline := assistantText(f.Stream.Message)
	assert.Equal(t, fiveDeltas[:8], baseline, "the baseline holds the text of deltas 1-2")
	assert.Equal(t, protocol.StopPending, f.Stream.Message.StopReason)

	requireContiguous(t, second+1, got)
	deltas := 0
	for _, e := range got {
		if _, ok := deltaOf(e.ev); ok {
			deltas++
		}
	}
	assert.Equal(t, 3, deltas, "deltas 3-5, once each")
	built, _, ends := rebuildFromBaseline(t, f, got)
	assert.Equal(t, 1, ends, "message_end once")
	committed := lastAssistant(t, log.Messages())
	assert.Equal(t, committed.Content, built.Content, "baseline plus later events is the committed message")
	assert.NotEqual(t, assistantText(committed), fiveDeltas[8:],
		"without the baseline the follower would show deltas 3-5 only")
}

func TestFollowJoinDuringToolCallGetsRawArgumentBytes(t *testing.T) {
	text := strings.Repeat("0123456789", 3)
	f, all, got, log := joinAtDelta(t, faux.Reply(faux.ToolCall("echo", map[string]any{"text": text}, faux.ID("c1"))))

	require.NotNil(t, f.Stream)
	require.Equal(t, seqOfDelta(all, 2), f.Stream.Seq)
	var before []string
	for _, e := range all {
		if d, ok := deltaOf(e.ev); ok && e.seq <= f.Stream.Seq {
			before = append(before, d)
		}
	}
	require.Len(t, before, 2)
	require.Len(t, f.Stream.Open, 1, "the tool call block is open")
	for _, raw := range f.Stream.Open {
		assert.Equal(t, strings.Join(before, ""), string(raw), "the baseline holds the argument bytes of deltas 1-2")
	}
	requireContiguous(t, f.Stream.Seq+1, got)

	built, _, ends := rebuildFromBaseline(t, f, got)

	assert.Equal(t, 1, ends)
	var committed protocol.AssistantMessage
	for _, m := range log.Messages() {
		if am, ok := m.(protocol.AssistantMessage); ok {
			committed = am
			break
		}
	}
	require.NotEmpty(t, committed.Content)
	assert.Equal(t, committed.Content, built.Content)
}

func TestFollowJoinBetweenMessagesHasNoBaseline(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(1, 1))
	p.Set(faux.Say(fiveDeltas))
	a := newAgent(t, p, m, nil)
	var f *agent.Follow
	a.Subscribe(func(ev protocol.Event) error {
		// turn_end is published after message_end(assistant): the pair has
		// already seen the end, so the stream is closed.
		if label(ev) == "turn_end" {
			f = a.Follow(agent.Cursor{})
		}
		return nil
	})

	require.NoError(t, a.Prompt(context.Background(), user("hi")))

	require.NotNil(t, f)
	defer f.Events.Close()
	assert.Nil(t, f.Stream)
	assert.Equal(t, []string{"user", "assistant"}, roles(messageEntries(f.Entries)))
}

func TestFollowJoinDuringResetGetsResync(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.NewContext = func() sessions.Writer { return &sessions.MemoryLog{} }
	})
	require.NoError(t, a.Prompt(context.Background(), user("a")))
	f := a.Follow(agent.Cursor{})
	old := f.Cursor

	require.NoError(t, a.Reset())
	require.NoError(t, a.Prompt(context.Background(), user("b")))

	assert.Empty(t, takeAll(t, f), "a follower cut before Reset gets no event of the new context")
	assert.ErrorIs(t, f.Events.Err(), bus.ErrResync)
	again := a.Follow(old)
	defer again.Events.Close()
	assert.True(t, again.Resync, "a cursor of the old epoch is refused")
	assert.NotEqual(t, old.Epoch, again.Cursor.Epoch)
	assert.Equal(t, []string{"user", "assistant"}, roles(messageEntries(again.Entries)))
}

func TestFollowResumeFromCursorGetsEventsAfterIt(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	a := newAgent(t, p, m, nil)
	first := a.Follow(agent.Cursor{})
	require.NoError(t, a.Prompt(context.Background(), user("a")))
	got := takeReady(t, first)
	require.NotEmpty(t, got)
	cursor := agent.Cursor{Epoch: first.Cursor.Epoch, Seq: got[len(got)-1].seq}
	first.Events.Close()
	require.NoError(t, a.Prompt(context.Background(), user("b")))

	resumed := a.Follow(cursor)
	defer resumed.Events.Close()

	assert.False(t, resumed.Resync)
	assert.True(t, resumed.Resumed)
	assert.Nil(t, resumed.Entries)
	later := takeReady(t, resumed)
	requireContiguous(t, cursor.Seq+1, later)
	assert.Equal(t, "agent_start", label(later[0].ev))
	assert.Equal(t, "agent_settled", label(later[len(later)-1].ev))
}

func TestFollowOldAndFutureCursorsGetResync(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	a := newAgent(t, p, m, func(c *agent.Config) { c.FollowLimits = bus.Limits{Events: 4} })
	first := a.Follow(agent.Cursor{})
	defer first.Events.Close()
	require.NoError(t, a.Prompt(context.Background(), user("a")))
	head := a.Follow(agent.Cursor{})
	defer head.Events.Close()

	old := a.Follow(agent.Cursor{Epoch: first.Cursor.Epoch, Seq: first.Cursor.Seq})
	defer old.Events.Close()
	future := a.Follow(agent.Cursor{Epoch: head.Cursor.Epoch, Seq: head.Cursor.Seq + 1})
	defer future.Events.Close()

	assert.True(t, old.Resync, "the events after the cursor are no longer held")
	assert.True(t, future.Resync)
	assert.Equal(t, head.Cursor.Seq, future.Cursor.Seq)
}

func TestFollowRingHoldsNoCredentialCanary(t *testing.T) {
	p, m := newFaux(t)
	m.Headers = map[string]string{"X-Canary": canaryHeader}
	p.Set(faux.Say("fine"))
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Options.APIKey = canaryAPIKey
		c.Options.Auth.AccessToken = canaryToken
	})
	first := a.Follow(agent.Cursor{})
	require.NoError(t, a.Prompt(context.Background(), user("hi")))
	first.Events.Close()
	var out strings.Builder
	for _, e := range takeAll(t, first) {
		b, err := protocol.EncodeEvent(e.ev)
		require.NoError(t, err)
		out.Write(b)
	}
	resumed := a.Follow(agent.Cursor{Epoch: first.Cursor.Epoch, Seq: first.Cursor.Seq})
	defer resumed.Events.Close()
	require.True(t, resumed.Resumed)
	for _, e := range takeReady(t, resumed) {
		b, err := protocol.EncodeEvent(e.ev)
		require.NoError(t, err)
		out.Write(b)
	}
	for _, e := range a.Follow(agent.Cursor{}).Entries {
		out.WriteString(toJSON(t, e))
	}

	require.NotEmpty(t, out.String())
	for _, c := range []string{canaryHeader, canaryAPIKey, canaryToken} {
		assert.NotContains(t, out.String(), c)
	}
}

func TestAgentStatsCountContainedAndDetached(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"))
	a := newAgent(t, p, m, func(c *agent.Config) { c.FollowLimits = bus.Limits{Buffer: 2} })
	assert.Equal(t, agent.FollowStats{}, a.Stats())
	a.Subscribe((&recorder{failOn: "turn_start"}).emit)
	a.Subscribe((&recorder{failOn: "turn_end"}).emit)
	slow := a.Follow(agent.Cursor{}) // never read: its buffer of 2 overflows
	defer slow.Events.Close()

	require.NoError(t, a.Prompt(context.Background(), user("hi")))

	st := a.Stats()
	assert.Equal(t, uint64(2), st.ListenerFailures)
	assert.Equal(t, uint64(1), st.DetachedFollowers)
	assert.ErrorIs(t, slow.Events.Err(), bus.ErrResync)
	assert.Equal(t, uint64(0), st.Gaps, "the events of one Agent never break the sequence")
}
