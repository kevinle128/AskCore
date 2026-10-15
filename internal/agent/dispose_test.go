package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// closingLog counts the calls of Close and returns err from each of them.
type closingLog struct {
	*sessions.MemoryLog
	closes atomic.Int32
	err    error
}

func (c *closingLog) Close() error {
	c.closes.Add(1)
	return c.err
}

func TestDisposeEndsCycleWithDisposedCause(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep(), faux.Say("unused"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	// A listener that panics at the last event does not stop the disposal.
	a.Subscribe(func(ev protocol.Event) error {
		if ev.EventType() == protocol.TypeAgentDisposed {
			panic("listener failed")
		}
		return nil
	})
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))
	require.NoError(t, a.Dispose())
	require.NoError(t, <-done)

	assert.Equal(t, []string{"aborted:disposed"}, cycleEnds(rec))
	labels := rec.eventLabels()
	require.GreaterOrEqual(t, len(labels), 3)
	assert.Equal(t, []string{"agent_end", "agent_settled", "agent_disposed"}, labels[len(labels)-3:],
		"agent_disposed comes after agent_settled")
	assert.Equal(t, protocol.StopAborted, lastAssistant(t, a.State().Messages).StopReason, "no error event")
	assert.Equal(t, 1, p.Calls(), "no queued work started")
	assert.Equal(t, agent.Idle, a.State().Status)
}

func TestDisposeIsMemoized(t *testing.T) {
	boom := errors.New("close failed")
	p, m := newFaux(t)
	log := &closingLog{MemoryLog: &sessions.MemoryLog{}, err: boom}
	a := newAgent(t, p, m, func(c *agent.Config) { c.NewContext = func() sessions.Writer { return log } })
	rec := &recorder{}
	a.Subscribe(rec.emit)

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = a.Dispose() }()
	}
	wg.Wait()

	for _, err := range errs {
		require.ErrorIs(t, err, boom, "every call returns the same result")
	}
	assert.EqualValues(t, 1, log.closes.Load(), "the writer closes once")
	assert.Equal(t, 1, countOf(rec.eventLabels(), "agent_disposed"), "one event")
	assert.Same(t, errs[0], a.Dispose(), "a later call returns the stored result")
}

func TestDisposeClosesIdleAgent(t *testing.T) {
	p, m := newFaux(t)
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.NoError(t, a.Dispose())

	assert.Equal(t, []string{"agent_disposed"}, rec.eventLabels())
	require.ErrorIs(t, a.Prompt(context.Background(), user("go")), agent.ErrDisposed)
}

func TestSendAfterDisposeStartsNoCycle(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep(), faux.Say("unused"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))
	require.NoError(t, a.Dispose())
	require.NoError(t, <-done)

	_, err := a.FollowUp(user("late"))
	require.ErrorIs(t, err, agent.ErrDisposed)
	_, err = a.Steer(user("late"))
	require.ErrorIs(t, err, agent.ErrDisposed)
	assert.False(t, a.Remove("any"))
	require.ErrorIs(t, a.Prompt(context.Background(), user("late")), agent.ErrDisposed)
	require.ErrorIs(t, a.Continue(context.Background()), agent.ErrDisposed)
	require.ErrorIs(t, a.Reset(), agent.ErrDisposed)
	require.ErrorIs(t, a.SetModel(context.Background(), m), agent.ErrDisposed)
	idle(t, a)

	assert.Equal(t, 1, p.Calls(), "the provider got one request")
	assert.Equal(t, 1, countOf(rec.eventLabels(), "cycle_start"))
}

// An Abort, unlike Dispose, lets the next message start a cycle.
func TestSendAfterAbortStartsCycleButNotAfterDispose(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep(), faux.Say("second"))
	a := newAgent(t, p, m, nil)
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))

	a.Abort()
	mustFollowUp(t, a, "after abort")
	require.NoError(t, <-done)
	idle(t, a)

	assert.Equal(t, 2, p.Calls(), "the message after Abort started a new cycle")
	require.NoError(t, a.Dispose())
	_, err := a.FollowUp(user("after dispose"))
	require.ErrorIs(t, err, agent.ErrDisposed)
}

func TestDisposeClearsQueuesAndRunsNone(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep(), faux.Say("unused"), faux.Say("unused"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))
	mustFollowUp(t, a, "f1")
	mustFollowUp(t, a, "f2")
	require.NoError(t, a.Dispose())
	require.NoError(t, <-done)

	assert.Equal(t, []string{"|f1", "|f1,f2", "|"}, queueStates(rec), "one update with both queues empty")
	labels := rec.eventLabels()
	cleared := -1
	for i, ev := range rec.events {
		if q, ok := ev.(*protocol.QueueUpdate); ok && len(q.Steering)+len(q.FollowUp) == 0 {
			cleared = i
		}
	}
	require.GreaterOrEqual(t, cleared, 0)
	assert.Less(t, cleared, slicesIndexOf(labels, "agent_settled"), "published before agent_settled")
	assert.Equal(t, 1, p.Calls(), "neither follow-up ran")
	assert.Equal(t, 1, countOf(labels, "cycle_start"))
}

func slicesIndexOf(labels []string, l string) int { return nthIndex(labels, l, 1) }

func TestDisposeWaitsForStartedToolBody(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{}, faux.ID("c1"))), faux.Say("unused"))
	started := make(chan struct{})
	var finished atomic.Bool
	var once sync.Once
	tool := &funcTool{name: "echo", run: func(ctx context.Context, _ tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		time.Sleep(150 * time.Millisecond)
		finished.Store(true)
		return textResult("late"), nil
	}}
	a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, tool) })

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), started, "the tool body"))
	require.NoError(t, a.Dispose())

	assert.True(t, finished.Load(), "Dispose returned only after the body returned")
	require.NoError(t, <-done)
}
