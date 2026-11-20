package agent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

const overflowWindow = 1000

// overflowCase runs one prompt against a model with a small window.
func overflowCase(t *testing.T, steps ...faux.Step) (*agent.Agent, *faux.Provider, *sessions.MemoryLog, *recorder) {
	t.Helper()
	p, m := newFaux(t)
	m.ContextWindow = overflowWindow
	p.Set(steps...)
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		c.Wait = func(context.Context, time.Duration) error { return nil }
	}))
	rec := &recorder{}
	a.Subscribe(rec.emit)
	return a, p, log, rec
}

func TestExplicitOverflowEndsCycleWithCause(t *testing.T) {
	a, p, log, rec := overflowCase(t,
		faux.Say("too much").Err(providers.NewFailure(providers.CodeContextWindow, 400, 0, "prompt is too long", nil)),
		faux.Say("must not run"),
	)
	require.NoError(t, a.Prompt(context.Background(), user("go")))

	assert.Equal(t, 1, p.Calls(), "an overflow is not retried")
	assert.Len(t, eventsOf[*protocol.AttemptStart](rec), 1)
	assert.Empty(t, logged[sessions.RetryScheduled](log))
	ends := eventsOf[*protocol.CycleEnd](rec)
	require.Len(t, ends, 1)
	assert.Equal(t, "error", ends[0].Reason)
	assert.Equal(t, providers.CodeContextWindow, ends[0].Code)
	assert.Equal(t, "context_overflow", ends[0].Cause)
	closed := logged[sessions.CycleClosed](log)
	require.Len(t, closed, 1)
	assert.Equal(t, "context_overflow", closed[0].Cause)
}

func TestSilentOverflowKeepsReplyAndEndsCycle(t *testing.T) {
	a, p, log, rec := overflowCase(t,
		faux.Say("a large answer").WithUsage(protocol.Usage{Input: 900, CacheRead: 200, Output: 5}),
		faux.Say("follow-up must not run"),
	)
	var once sync.Once
	a.Subscribe(func(ev protocol.Event) error {
		if _, ok := ev.(*protocol.CycleStart); ok {
			once.Do(func() { _, _ = a.FollowUp(user("later")) })
		}
		return nil
	})
	require.NoError(t, a.Prompt(context.Background(), user("go")))

	assert.Equal(t, 1, p.Calls(), "the queued follow-up does not start")
	assert.Equal(t, []string{"later"}, a.State().FollowUp)
	assert.Equal(t, "a large answer", assistantText(lastAssistant(t, a.State().Messages)))
	settled := logged[sessions.AttemptSettled](log)
	require.Len(t, settled, 1)
	assert.Equal(t, "completed", settled[0].Outcome)
	require.NotNil(t, settled[0].Usage)
	assert.Equal(t, int64(900), settled[0].Usage.Input)
	assert.Nil(t, settled[0].Failure)
	ends := eventsOf[*protocol.CycleEnd](rec)
	require.Len(t, ends, 1)
	assert.Equal(t, "error", ends[0].Reason)
	assert.Equal(t, providers.CodeContextWindow, ends[0].Code)
	assert.Equal(t, "context_overflow", ends[0].Cause)
}

func TestSilentOverflowRunsNoToolCalls(t *testing.T) {
	a, _, log, rec := overflowCase(t,
		faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))).Stop(protocol.StopStop).WithUsage(protocol.Usage{Input: 1500, Output: 5}),
	)
	require.NoError(t, a.Prompt(context.Background(), user("go")))

	assert.Empty(t, eventsOf[*protocol.ToolExecutionStart](rec))
	assert.Empty(t, logged[sessions.ToolCall](log), "no call is recorded as started")
	for _, b := range lastAssistant(t, a.State().Messages).Content {
		_, isCall := b.(protocol.ToolCall)
		assert.False(t, isCall, "the logged reply keeps no call that never ran")
	}
	assert.Equal(t, "context_overflow", eventsOf[*protocol.CycleEnd](rec)[0].Cause)
}

func TestLengthStopAtWindowIsOverflow(t *testing.T) {
	a, _, _, rec := overflowCase(t,
		faux.Reply().Stop(protocol.StopLength).WithUsage(protocol.Usage{Input: 995, Output: 0}),
	)
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	ends := eventsOf[*protocol.CycleEnd](rec)
	require.Len(t, ends, 1)
	assert.Equal(t, "error", ends[0].Reason)
	assert.Equal(t, "context_overflow", ends[0].Cause)
}

func TestLengthStopBelowWindowIsMaxTokens(t *testing.T) {
	a, _, _, rec := overflowCase(t,
		faux.Say("cut").Stop(protocol.StopLength).WithUsage(protocol.Usage{Input: 400, Output: 50}),
	)
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	ends := eventsOf[*protocol.CycleEnd](rec)
	require.Len(t, ends, 1)
	assert.Equal(t, "max-tokens", ends[0].Reason)
	assert.Empty(t, ends[0].Cause)
}

func TestRateLimitIsNotOverflow(t *testing.T) {
	a, p, log, rec := overflowCase(t,
		faux.Say("slow").Err(providers.NewFailure(providers.CodeRateLimit, 429, 0, "too many tokens per minute", nil)),
		faux.Say("done"),
	)
	require.NoError(t, a.Prompt(context.Background(), user("go")))

	assert.Equal(t, 2, p.Calls(), "a rate limit is retried")
	assert.Len(t, logged[sessions.RetryScheduled](log), 1)
	ends := eventsOf[*protocol.CycleEnd](rec)
	require.Len(t, ends, 1)
	assert.Equal(t, "completed", ends[0].Reason)
	assert.Empty(t, ends[0].Cause)
}
