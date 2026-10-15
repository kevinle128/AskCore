package agent_test

import (
	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoRetryEventsAgreeWithRetryEntries(t *testing.T) {
	_, _, log, rec, _ := retryCase(t, []faux.Step{retryFailure(providers.CodeServer, 0), faux.Say("done")}, nil)
	scheduled := logged[sessions.RetryScheduled](log)
	started := logged[sessions.RetryStarted](log)
	wire := eventsOf[*protocol.AutoRetryStart](rec)
	require.Len(t, wire, 1)
	assert.Equal(t, scheduled[0].Retry, wire[0].Attempt)
	assert.Equal(t, scheduled[0].DelayMs, wire[0].DelayMs)
	assert.Equal(t, scheduled[0].Failure.Text, wire[0].ErrorMessage)
	assert.Equal(t, scheduled[0].RetryID, started[0].RetryID)
	end := eventsOf[*protocol.AutoRetryEnd](rec)
	require.Len(t, end, 1)
	assert.True(t, end[0].Success)
	assert.Equal(t, scheduled[0].Retry, end[0].Attempt)
}
func TestRetryAfterVisibleOutputClosesFailedMessageOnWire(t *testing.T) {
	a, _, _, rec, _ := retryCase(t, []faux.Step{retryFailure(providers.CodeTransport, 0), faux.Say("done")}, nil)
	var at int
	for i, e := range rec.events {
		if end, ok := e.(*protocol.MessageEnd); ok {
			if msg, ok := end.Message.(protocol.AssistantMessage); ok && msg.StopReason == protocol.StopError {
				at = i
				assert.Equal(t, "busy", errorText(msg))
				break
			}
		}
	}
	require.Positive(t, at)
	assert.Equal(t, []string{"message_end(assistant)", "attempt_end", "turn_end", "agent_end", "auto_retry_start", "agent_start", "turn_start", "attempt_start"}, rec.eventLabels()[at:at+8])
	ends := eventsOf[*protocol.AgentEnd](rec)
	assert.True(t, ends[0].WillRetry)
	assert.False(t, ends[len(ends)-1].WillRetry)
	assert.Equal(t, "done", assistantText(lastAssistant(t, a.State().Messages)))
}
func abortedBackoff(t *testing.T, dispose bool) (*faux.Provider, *sessions.MemoryLog, *recorder) {
	t.Helper()
	p, m := newFaux(t)
	p.Set(retryFailure(providers.CodeServer, 0), faux.Say("unused"))
	log := &sessions.MemoryLog{}
	entered := make(chan struct{})
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		c.Wait = func(ctx context.Context, _ time.Duration) error { close(entered); <-ctx.Done(); return ctx.Err() }
	}))
	rec := &recorder{}
	a.Subscribe(rec.emit)
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	<-entered
	if dispose {
		require.NoError(t, a.Dispose())
	} else {
		a.Abort()
	}
	require.NoError(t, <-done)
	return p, log, rec
}
func TestCancelDuringBackoffEndsWithoutAnotherAttempt(t *testing.T) {
	p, log, rec := abortedBackoff(t, false)
	assert.Equal(t, 1, p.Calls())
	assert.Empty(t, logged[sessions.RetryStarted](log))
	assert.Equal(t, "aborted", eventsOf[*protocol.CycleEnd](rec)[0].Reason)
}
func TestDisposeDuringBackoffStartsNoAttempt(t *testing.T) {
	p, log, rec := abortedBackoff(t, true)
	assert.Equal(t, 1, p.Calls())
	assert.Empty(t, logged[sessions.RetryStarted](log))
	assert.Equal(t, "disposed", eventsOf[*protocol.CycleEnd](rec)[0].Cause)
}
func TestCancelDuringBackoffEndsWithAutoRetryEndFailure(t *testing.T) {
	_, _, rec := abortedBackoff(t, false)
	end := eventsOf[*protocol.AutoRetryEnd](rec)
	require.Len(t, end, 1)
	assert.False(t, end[0].Success)
	assert.Equal(t, 1, end[0].Attempt)
}
func TestAbortFromRetryScheduledListenerStopsBackoff(t *testing.T) {
	p, m := newFaux(t)
	p.Set(retryFailure(providers.CodeServer, 0))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, edits(withLog(log)))
	rec := &recorder{}
	a.Subscribe(rec.emit)
	a.Subscribe(func(e protocol.Event) error {
		if _, ok := e.(*protocol.AutoRetryStart); ok {
			a.Abort()
		}
		return nil
	})
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Equal(t, 1, p.Calls())
	assert.Len(t, logged[sessions.RetryScheduled](log), 1)
	assert.Empty(t, logged[sessions.RetryStarted](log))
	assert.Equal(t, "aborted", eventsOf[*protocol.CycleEnd](rec)[0].Reason)
}
func TestRetryDecisionAfterAbortIsIgnored(t *testing.T) {
	p, m := newFaux(t)
	p.Set(retryFailure(providers.CodeServer, 0))
	var a *agent.Agent
	a = newAgent(t, p, m, func(c *agent.Config) {
		registryOf(c).OnRecoverModel(func(context.Context, pipeline.RecoverInput, pipeline.Next[pipeline.RecoverInput, pipeline.RecoverAction]) (pipeline.RecoverAction, error) {
			a.Abort()
			return pipeline.RecoverAction{Retry: true}, nil
		})
	})
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Equal(t, 1, p.Calls())
	assert.Empty(t, eventsOf[*protocol.AutoRetryStart](rec))
	assert.Equal(t, "aborted", eventsOf[*protocol.CycleEnd](rec)[0].Reason)
}
