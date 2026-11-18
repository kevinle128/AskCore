package agent_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

func toolUseMessage(ids ...string) protocol.AssistantMessage {
	a := protocol.AssistantMessage{StopReason: protocol.StopToolUse}
	for _, id := range ids {
		a.Content = append(a.Content, protocol.ToolCall{ID: id, Name: "echo"})
	}
	return a
}

func repairedResult(t *testing.T, e sessions.Entry) (callID, text string) {
	t.Helper()
	me, ok := e.(sessions.MessageEntry)
	require.True(t, ok, "entry %T is not a message", e)
	r, ok := me.Message.(protocol.ToolResultMessage)
	require.True(t, ok, "message %T is not a tool result", me.Message)
	return r.ToolCallID, r.Content[0].(protocol.Text).Text
}

func TestOpenLogClosesTurnLeftOpenByCrash(t *testing.T) {
	p, m := newFaux(t)
	log := &sessions.MemoryLog{}
	// Entry 3 is the assistant message. Call "a" has an intent entry, call "b" has none.
	_, err := log.Append(
		sessions.CycleOpened{CycleID: "c1"},
		sessions.TurnOpened{CycleID: "c1"},
		sessions.MessageEntry{Message: user("work")},
		sessions.MessageEntry{Message: toolUseMessage("a", "b")},
		sessions.ToolCall{AssistantEntry: 3, CallID: "a"},
	)
	require.NoError(t, err)

	a := newAgent(t, p, m, withLog(log))

	entries := log.Entries()
	require.Len(t, entries, 9)
	id, text := repairedResult(t, entries[5])
	assert.Equal(t, "a", id)
	assert.Equal(t, "outcome unknown", text)
	id, text = repairedResult(t, entries[6])
	assert.Equal(t, "b", id)
	assert.Equal(t, "not started", text)
	assert.Equal(t, sessions.TurnClosed{CycleID: "c1"}, entries[7])
	assert.Equal(t, sessions.CycleClosed{CycleID: "c1", Reason: "aborted", Cause: "interrupted"}, entries[8])

	p.Set(faux.Say("ok"))
	require.NoError(t, a.Prompt(context.Background(), user("next")))
	assert.Equal(t, []string{"work", "next"}, userTexts(p.Requests()[0].Transcript.Messages))
}

func TestOpenLogClosesCycleThatHasNoOpenTurn(t *testing.T) {
	p, m := newFaux(t)
	log := &sessions.MemoryLog{}
	_, err := log.Append(sessions.CycleOpened{CycleID: "c1"}, sessions.MessageEntry{Message: user("work")})
	require.NoError(t, err)

	newAgent(t, p, m, withLog(log))

	entries := log.Entries()
	require.Len(t, entries, 3)
	assert.Equal(t, sessions.CycleClosed{CycleID: "c1", Reason: "aborted", Cause: "interrupted"}, entries[2])
}

func TestOpenLogLeavesClosedLogAsItIs(t *testing.T) {
	p, m := newFaux(t)
	log := &sessions.MemoryLog{}
	_, err := log.Append(
		sessions.CycleOpened{CycleID: "c1"},
		sessions.TurnOpened{CycleID: "c1"},
		sessions.MessageEntry{Message: user("work")},
		sessions.TurnClosed{CycleID: "c1"},
		sessions.CycleClosed{CycleID: "c1", Reason: "completed"},
	)
	require.NoError(t, err)

	newAgent(t, p, m, withLog(log))

	assert.Len(t, log.Entries(), 5)
}

func TestOpenLogRepairFailureFailsNew(t *testing.T) {
	p, m := newFaux(t)
	log := &failingLog{}
	_, _ = log.MemoryLog.Append(sessions.CycleOpened{CycleID: "c1"})

	_, err := agent.New(agent.Config{
		LoopConfig: agent.LoopConfig{Model: m, Stream: p.Stream},
		NewContext: func() (sessions.Writer, error) { return log, nil },
	})

	require.ErrorContains(t, err, "disk full")
}

func TestResetOpensNewLogAndClosesOld(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	var mu sync.Mutex
	var logs []*closingLog
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.NewContext = func() (sessions.Writer, error) {
			mu.Lock()
			defer mu.Unlock()
			l := &closingLog{MemoryLog: &sessions.MemoryLog{}}
			logs = append(logs, l)
			return l, nil
		}
	})
	require.NoError(t, a.Prompt(context.Background(), user("first")))

	require.NoError(t, a.Reset())
	require.NoError(t, a.Prompt(context.Background(), user("second")))

	require.Len(t, logs, 2)
	assert.EqualValues(t, 1, logs[0].closes.Load(), "the old log is closed once")
	assert.EqualValues(t, 0, logs[1].closes.Load())
	assert.Equal(t, []string{"first"}, userTexts(logs[0].Messages()))
	assert.Equal(t, []string{"second"}, userTexts(logs[1].Messages()))
}

func TestResetKeepsOldLogWhenOpenFails(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	first := &closingLog{MemoryLog: &sessions.MemoryLog{}}
	opened := 0
	errOpen := errors.New("no space for a new session")
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.NewContext = func() (sessions.Writer, error) {
			opened++
			if opened > 1 {
				return nil, errOpen
			}
			return first, nil
		}
	})
	require.NoError(t, a.Prompt(context.Background(), user("first")))
	epoch := a.Follow(agent.Cursor{}).Head.Epoch

	err := a.Reset()

	require.ErrorIs(t, err, errOpen)
	assert.EqualValues(t, 0, first.closes.Load(), "the old log stays open")
	assert.Equal(t, epoch, a.Follow(agent.Cursor{}).Head.Epoch, "the epoch does not change")
	require.NoError(t, a.Prompt(context.Background(), user("second")))
	assert.Equal(t, []string{"first", "second"}, userTexts(first.Messages()))
}

func TestNewReturnsOpenError(t *testing.T) {
	p, m := newFaux(t)
	errOpen := errors.New("session is held by another process")

	_, err := agent.New(agent.Config{
		LoopConfig: agent.LoopConfig{Model: m, Stream: p.Stream},
		NewContext: func() (sessions.Writer, error) { return nil, errOpen },
	})

	require.ErrorIs(t, err, errOpen)
}

// lostLog fails every Append from the failAt-th call on with ErrWriterLost, as
// a file writer does after a failed write.
type lostLog struct {
	*sessions.MemoryLog
	failAt int
	mu     sync.Mutex
	calls  int
}

func (l *lostLog) Append(entries ...sessions.Entry) (sessions.CommitRef, error) {
	l.mu.Lock()
	l.calls++
	lost := l.calls >= l.failAt
	l.mu.Unlock()
	if lost {
		return sessions.CommitRef{}, sessions.ErrWriterLost
	}
	return l.MemoryLog.Append(entries...)
}

func (l *lostLog) appendCalls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

func TestLostWriterEndsRunWithClearError(t *testing.T) {
	// The second append fails at the start of the run; the sixth fails inside the loop.
	for _, failAt := range []int{2, 6} {
		p, m := newFaux(t)
		p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
		log := &lostLog{MemoryLog: &sessions.MemoryLog{}, failAt: failAt}
		a := newAgent(t, p, m, withLog(log))

		err := a.Prompt(context.Background(), user("go"))

		require.ErrorIs(t, err, sessions.ErrWriterLost, "failAt %d", failAt)
		assert.ErrorContains(t, err, "can no longer be written", "failAt %d", failAt)
		assert.ErrorContains(t, err, "reopened", "failAt %d", failAt)
		assert.ErrorContains(t, err, "run with `--no-session` to run without saving", "failAt %d", failAt)
		assert.Equal(t, failAt, log.appendCalls(), "no repair or closing entry is tried after the loss (failAt %d)", failAt)
		require.NoError(t, a.Dispose())
	}
}
