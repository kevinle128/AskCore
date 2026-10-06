package agent_test

import (
	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/providers/fantasykit"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimitTwiceThenSuccessIsOneCycleThreeAttempts(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("failed one").Err(providers.NewFailure(providers.CodeRateLimit, 429, 0, "wait", nil)), faux.Say("failed two").Err(providers.NewFailure(providers.CodeServer, 503, 0, "busy", nil)), faux.Say("done"))
	log := &sessions.MemoryLog{}
	var delays []time.Duration
	admit, prepare := 0, 0
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		c.Wait = func(_ context.Context, d time.Duration) error { delays = append(delays, d); return nil }
		r := registryOf(c)
		r.OnAdmitStep(func(ctx context.Context, in pipeline.AdmitInput, next pipeline.Next[pipeline.AdmitInput, pipeline.AdmitDecision]) (pipeline.AdmitDecision, error) {
			admit++
			return next(ctx, in)
		})
		r.OnPrepareRequest(func(ctx context.Context, in pipeline.Request, next pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
			prepare++
			return next(ctx, in)
		})
	}))
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Equal(t, 3, p.Calls())
	assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second}, delays)
	assert.Equal(t, 1, admit)
	assert.Equal(t, 3, prepare)
	assert.Equal(t, []string{"user", "assistant"}, roles(a.State().Messages))
	assert.Equal(t, "done", assistantText(lastAssistant(t, a.State().Messages)))
	assert.Len(t, eventsOf[*protocol.CycleStart](rec), 1)
	assert.Len(t, eventsOf[*protocol.CycleEnd](rec), 1)
	assert.Len(t, eventsOf[*protocol.AttemptStart](rec), 3)
	assert.Len(t, logged[sessions.AttemptSettled](log), 3)
}

func retryCase(t *testing.T, steps []faux.Step, edit func(*agent.Config)) (*agent.Agent, *faux.Provider, *sessions.MemoryLog, *recorder, []time.Duration) {
	t.Helper()
	p, m := newFaux(t)
	p.Set(steps...)
	log := &sessions.MemoryLog{}
	var waits []time.Duration
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		c.Wait = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
		if edit != nil {
			edit(c)
		}
	}))
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	return a, p, log, rec, waits
}
func retryFailure(code string, after time.Duration) faux.Step {
	return faux.Say("partial").Err(providers.NewFailure(code, 500, after, "busy", nil))
}
func TestRetryDelaysFollowPolicyWithoutJitter(t *testing.T) {
	_, p, _, _, waits := retryCase(t, []faux.Step{retryFailure(providers.CodeServer, 0), retryFailure(providers.CodeServer, 0), retryFailure(providers.CodeServer, 0), retryFailure(providers.CodeServer, 0), retryFailure(providers.CodeServer, 0), retryFailure(providers.CodeServer, 0)}, nil)
	assert.Equal(t, 6, p.Calls())
	assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}, waits)
}
func TestRetryStopsAfterFiveRetries(t *testing.T) {
	_, p, log, rec, _ := retryCase(t, []faux.Step{retryFailure(providers.CodeServer, 0), retryFailure(providers.CodeServer, 0), retryFailure(providers.CodeServer, 0), retryFailure(providers.CodeServer, 0), retryFailure(providers.CodeServer, 0), retryFailure(providers.CodeServer, 0)}, nil)
	assert.Equal(t, 6, p.Calls())
	assert.Len(t, logged[sessions.RetryScheduled](log), 5)
	assert.Len(t, logged[sessions.AttemptSettled](log), 6)
	assert.Equal(t, "error", eventsOf[*protocol.CycleEnd](rec)[0].Reason)
}
func TestRetryAfterUnderCapIsHonored(t *testing.T) {
	_, p, _, _, waits := retryCase(t, []faux.Step{retryFailure(providers.CodeRateLimit, 3*time.Second), faux.Say("done")}, nil)
	assert.Equal(t, 2, p.Calls())
	assert.Equal(t, []time.Duration{3 * time.Second}, waits)
}
func TestRetryAfterAboveCapIsNotRetried(t *testing.T) {
	a, p, _, _, waits := retryCase(t, []faux.Step{retryFailure(providers.CodeRateLimit, 11*time.Second)}, nil)
	assert.Equal(t, 1, p.Calls())
	assert.Empty(t, waits)
	assert.Equal(t, "busy", errorText(lastAssistant(t, a.State().Messages)))
}
func TestQuotaIsNotRetried(t *testing.T) {
	_, p, log, _, _ := retryCase(t, []faux.Step{retryFailure(providers.CodeQuota, 0)}, nil)
	assert.Equal(t, 1, p.Calls())
	assert.Empty(t, logged[sessions.RetryScheduled](log))
}
func TestCleanPrematureEndIsNotRetried(t *testing.T) {
	_, p, log, _, _ := retryCase(t, []faux.Step{retryFailure(providers.CodeStreamClosed, 0)}, nil)
	assert.Equal(t, 1, p.Calls())
	assert.Empty(t, logged[sessions.RetryScheduled](log))
}
func TestEmptyResponseIsRetriedAndNeverCommitted(t *testing.T) {
	a, p, _, _, _ := retryCase(t, []faux.Step{faux.Reply().Err(providers.NewFailure(providers.CodeEmptyResponse, 0, 0, "empty", nil)), faux.Say("done")}, nil)
	require.Equal(t, 2, p.Calls())
	assert.Equal(t, p.Requests()[0].Transcript, p.Requests()[1].Transcript)
	assert.Equal(t, []string{"user", "assistant"}, roles(a.State().Messages))
	assert.Equal(t, "done", assistantText(lastAssistant(t, a.State().Messages)))
}
func TestRetryDoesNotRecommitUserInput(t *testing.T) {
	a, _, log, _, _ := retryCase(t, []faux.Step{retryFailure(providers.CodeServer, 0), faux.Say("done")}, nil)
	assert.Equal(t, []string{"user", "assistant"}, roles(log.Messages()))
	assert.Equal(t, []string{"user", "assistant"}, roles(a.State().Messages))
	assert.Len(t, logged[sessions.InputOutcome](log), 1)
}
func TestRetryKeepsCycleWithNewAttemptID(t *testing.T) {
	_, _, log, rec, _ := retryCase(t, []faux.Step{retryFailure(providers.CodeServer, 0), faux.Say("done")}, nil)
	starts := eventsOf[*protocol.AttemptStart](rec)
	require.Len(t, starts, 2)
	assert.NotEqual(t, starts[0].AttemptID, starts[1].AttemptID)
	assert.Equal(t, starts[0].CycleID, starts[1].CycleID)
	assert.Len(t, logged[sessions.TurnOpened](log), 1)
}
func TestRetryRerunsPrepareRequestButNotAdmission(t *testing.T) {
	admit, prep := 0, 0
	retryCase(t, []faux.Step{retryFailure(providers.CodeServer, 0), faux.Say("done")}, func(c *agent.Config) {
		r := registryOf(c)
		r.OnAdmitStep(func(ctx context.Context, in pipeline.AdmitInput, next pipeline.Next[pipeline.AdmitInput, pipeline.AdmitDecision]) (pipeline.AdmitDecision, error) {
			admit++
			return next(ctx, in)
		})
		r.OnPrepareRequest(func(ctx context.Context, in pipeline.Request, next pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
			prep++
			return next(ctx, in)
		})
	})
	assert.Equal(t, 1, admit)
	assert.Equal(t, 2, prep)
}
func TestRetryEntriesAreWrittenBeforeAndAfterWait(t *testing.T) {
	p, m := newFaux(t)
	p.Set(retryFailure(providers.CodeServer, 0), faux.Say("done"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		c.Wait = func(context.Context, time.Duration) error {
			assert.Len(t, logged[sessions.RetryScheduled](log), 1)
			assert.Empty(t, logged[sessions.RetryStarted](log))
			return nil
		}
	}))
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	scheduled := logged[sessions.RetryScheduled](log)
	started := logged[sessions.RetryStarted](log)
	require.Len(t, started, 1)
	assert.Equal(t, scheduled[0].RetryID, started[0].RetryID)
}
func TestRetryEntryIsLogOnlyAndAddsNoMessage(t *testing.T) {
	_, _, log, _, _ := retryCase(t, []faux.Step{retryFailure(providers.CodeServer, 0), faux.Say("done")}, nil)
	entries := logged[sessions.RetryScheduled](log)
	require.Len(t, entries, 1)
	assert.Equal(t, "busy", entries[0].Failure.Text)
	assert.Equal(t, []string{"user", "assistant"}, roles(log.Messages()))
}
func TestRetryOfTwoFailuresStaysInOneCycle(t *testing.T) {
	p, m := newFaux(t)
	p.Set(retryFailure(providers.CodeRateLimit, 0), retryFailure(providers.CodeServer, 0), faux.Say("done"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) { c.Wait = func(context.Context, time.Duration) error { return nil } }))
	rec := &recorder{}
	a.Subscribe(rec.emit)
	var statuses []agent.Status
	recordStatus := func() {
		state := a.State().Status
		if len(statuses) == 0 || statuses[len(statuses)-1] != state {
			statuses = append(statuses, state)
		}
	}
	a.Subscribe(func(protocol.Event) error { recordStatus(); return nil })
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	recordStatus()
	assert.Equal(t, []agent.Status{agent.Running, agent.Idle}, statuses)
	assert.Len(t, eventsOf[*protocol.CycleStart](rec), 1)
	assert.Len(t, logged[sessions.TurnOpened](log), 1)
	assert.Len(t, logged[sessions.SystemSnapshot](log), 1)
	assert.Len(t, logged[sessions.AttemptSettled](log), 3)
}

func TestMissingUsageIsUnknownNotZero(t *testing.T) {
	_, _, log, _, _ := retryCase(t, []faux.Step{retryFailure(providers.CodeServer, 0).WithUsage(protocol.Usage{}), faux.Say("done").WithUsage(protocol.Usage{})}, nil)
	for _, s := range logged[sessions.AttemptSettled](log) {
		assert.Nil(t, s.Usage)
	}
}
func TestFailedAttemptUsageCountedOnce(t *testing.T) {
	u := protocol.Usage{Input: 10, Output: 3, TotalTokens: 13}
	_, _, log, _, _ := retryCase(t, []faux.Step{retryFailure(providers.CodeServer, 0).WithUsage(u), faux.Say("done").WithUsage(protocol.Usage{})}, nil)
	settled := logged[sessions.AttemptSettled](log)
	require.Len(t, settled, 2)
	require.NotNil(t, settled[0].Usage)
	assert.Equal(t, u, *settled[0].Usage)
	assert.Nil(t, settled[1].Usage)
}
func TestFinalUsageEqualToLastChunkCountedOnce(t *testing.T) {
	u := protocol.Usage{Input: 10, Output: 3, TotalTokens: 13}
	_, _, log, _, _ := retryCase(t, []faux.Step{faux.Say("done").WithUsage(u)}, nil)
	settled := logged[sessions.AttemptSettled](log)
	require.Len(t, settled, 1)
	assert.Equal(t, u, *settled[0].Usage)
}

func TestRetryUsesPolicyCapturedAtPrepare(t *testing.T) {
	p, m := newFaux(t)
	p.Set(retryFailure(providers.CodeServer, 0), retryFailure(providers.CodeServer, 0), faux.Say("done"))
	reg := providers.NewRegistry()
	aModel := m
	aModel.API = "a"
	aModel.Provider = "a"
	bModel := m
	bModel.API = "b"
	bModel.Provider = "b"
	policyA := providers.DefaultRetryPolicy()
	policyA.Key = "a"
	policyA.BaseDelay = 2 * time.Second
	policyB := providers.DefaultRetryPolicy()
	policyB.Key = "b"
	policyB.BaseDelay = 3 * time.Second
	reg.Register(aModel.API, p.Stream, policyA)
	reg.Register(bModel.API, p.Stream, policyB)
	n := 0
	var waits []time.Duration
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, aModel, edits(withLog(log), func(c *agent.Config) {
		c.Registry = reg
		c.Stream = reg.Stream
		c.Prepare = reg.Prepare
		c.Wait = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
		registryOf(c).OnPrepareRequest(func(ctx context.Context, in pipeline.Request, next pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
			n++
			if n == 1 {
				in.Model = bModel
			} else {
				in.Model = aModel
			}
			return next(ctx, in)
		})
	}))
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Equal(t, []time.Duration{3 * time.Second, 2 * time.Second}, waits)
	s := logged[sessions.RetryScheduled](log)
	require.Len(t, s, 2)
	assert.Equal(t, 1, s[0].Retry)
	assert.Equal(t, 1, s[1].Retry)
	assert.Equal(t, "b", s[0].Provider)
	assert.Equal(t, "a", s[1].Provider)
}
func TestRetryBudgetIsPerProvider(t *testing.T) {
	p, m := newFaux(t)
	p.Set(retryFailure(providers.CodeServer, 0), retryFailure(providers.CodeServer, 0), retryFailure(providers.CodeServer, 0))
	reg := providers.NewRegistry()
	policy := providers.DefaultRetryPolicy()
	policy.MaxRetries = 1
	reg.Register(m.API, p.Stream, policy)
	n := 0
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Prepare = reg.Prepare
		c.Wait = func(context.Context, time.Duration) error { return nil }
		registryOf(c).OnPrepareRequest(func(ctx context.Context, in pipeline.Request, next pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
			n++
			if n > 1 {
				in.Model.Provider = "second"
			}
			return next(ctx, in)
		})
	})
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Equal(t, 3, p.Calls())
}
func TestRetryBudgetResetsAtTurnStart(t *testing.T) {
	p, m := newFaux(t)
	p.Set(retryFailure(providers.CodeServer, 0), faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"})), retryFailure(providers.CodeServer, 0), faux.Say("done"))
	reg := providers.NewRegistry()
	policy := providers.DefaultRetryPolicy()
	policy.MaxRetries = 1
	reg.Register(m.API, p.Stream, policy)
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		c.Prepare = reg.Prepare
		c.Tools = registry(t, tools.Echo{})
		c.Wait = func(context.Context, time.Duration) error { return nil }
	}))
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Equal(t, 4, p.Calls())
	s := logged[sessions.RetryScheduled](log)
	require.Len(t, s, 2)
	assert.Equal(t, 1, s[0].Retry)
	assert.Equal(t, 1, s[1].Retry)
	assert.Equal(t, 2, s[1].Turn)
}
func TestRetryRunsNoToolsFromFailedAttempt(t *testing.T) {
	a, p, log, rec, _ := retryCase(t, []faux.Step{faux.Reply(faux.Text("partial"), faux.ToolCall("echo", map[string]any{"text": "bad"})).Err(providers.NewFailure(providers.CodeServer, 503, 0, "busy", nil)), faux.Say("done")}, func(c *agent.Config) { c.Tools = registry(t, tools.Echo{}) })
	assert.Equal(t, 2, p.Calls())
	assert.Empty(t, eventsOf[*protocol.ToolExecutionStart](rec))
	assert.Equal(t, []string{"user", "assistant"}, roles(a.State().Messages))
	assert.Equal(t, "failed", logged[sessions.AttemptSettled](log)[0].Outcome)
}
func TestMiddlewareErrorIsNotRetried(t *testing.T) {
	p, m := newFaux(t)
	p.Set(retryFailure(providers.CodeServer, 0))
	a := newAgent(t, p, m, func(c *agent.Config) {
		registryOf(c).OnExecuteModel(func(ctx context.Context, in pipeline.ModelCall, next pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
			_, err := next(ctx, in)
			require.NoError(t, err)
			return nil, errors.New("middleware failed")
		})
	})
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.Error(t, a.Prompt(context.Background(), user("go")))
	assert.Equal(t, 1, p.Calls())
	assert.Empty(t, eventsOf[*protocol.AutoRetryStart](rec))
}
func TestRecoveryHandlerFailureEndsCycleWithoutRetry(t *testing.T) {
	p, m := newFaux(t)
	p.Set(retryFailure(providers.CodeRateLimit, 0))
	a := newAgent(t, p, m, func(c *agent.Config) {
		registryOf(c).OnRecoverModel(func(context.Context, pipeline.RecoverInput, pipeline.Next[pipeline.RecoverInput, pipeline.RecoverAction]) (pipeline.RecoverAction, error) {
			return pipeline.RecoverAction{}, errors.New("recovery failed")
		})
	})
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.Error(t, a.Prompt(context.Background(), user("go")))
	assert.Equal(t, 1, p.Calls())
	assert.Equal(t, "error", eventsOf[*protocol.CycleEnd](rec)[0].Reason)
	_, p, _, _, _ = retryCase(t, []faux.Step{retryFailure(providers.CodeRateLimit, 0), faux.Say("done")}, nil)
	assert.Equal(t, 2, p.Calls())
}

func TestRetryPreparationFailureClosesSeries(t *testing.T) {
	p, m := newFaux(t)
	p.Set(retryFailure(providers.CodeServer, 0), faux.Say("unused"))
	log := &sessions.MemoryLog{}
	n := 0
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		c.Wait = func(context.Context, time.Duration) error { return nil }
		registryOf(c).OnPrepareRequest(func(ctx context.Context, in pipeline.Request, next pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
			n++
			if n == 2 {
				return nil, errors.New("retry preparation failed")
			}
			return next(ctx, in)
		})
	}))
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.Error(t, a.Prompt(context.Background(), user("go")))
	assert.Equal(t, 1, p.Calls())
	assert.Equal(t, []string{"user"}, roles(log.Messages()))
	ends := eventsOf[*protocol.AutoRetryEnd](rec)
	require.Len(t, ends, 1, "retry series must close after preparation error")
	assert.False(t, ends[0].Success)
}
func TestRetryShortCircuitSuccessClosesSeriesAsSuccess(t *testing.T) {
	p, m := newFaux(t)
	p.Set(retryFailure(providers.CodeServer, 0))
	n := 0
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Wait = func(context.Context, time.Duration) error { return nil }
		registryOf(c).OnExecuteModel(func(ctx context.Context, in pipeline.ModelCall, next pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
			n++
			if n == 2 {
				return &pipeline.ModelOutcome{Message: protocol.AssistantMessage{Content: []protocol.AssistantBlock{protocol.Text{Text: "provided"}}, StopReason: protocol.StopStop}}, nil
			}
			return next(ctx, in)
		})
	})
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Equal(t, 1, p.Calls())
	ends := eventsOf[*protocol.AutoRetryEnd](rec)
	require.Len(t, ends, 1)
	assert.True(t, ends[0].Success, "successful synthetic reply must close series as success")
	assert.Empty(t, ends[0].FinalError)
}

func TestReplacementErrorCarriesCycleCode(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("done"))
	a := newAgent(t, p, m, func(c *agent.Config) {
		registryOf(c).OnExecuteModel(func(ctx context.Context, in pipeline.ModelCall, next pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
			o, e := next(ctx, in)
			if e != nil {
				return nil, e
			}
			o.Message.StopReason = protocol.StopError
			o.Failure = providers.NewFailure(providers.CodeQuota, 429, 0, "quota", nil)
			text := "quota"
			o.Message.ErrorMessage = &text
			return o, nil
		})
	})
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	ends := eventsOf[*protocol.CycleEnd](rec)
	require.Len(t, ends, 1)
	assert.Equal(t, "error", ends[0].Reason)
	assert.Equal(t, providers.CodeQuota, ends[0].Code)
}
func TestReplacementLengthNeverRunsTools(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "bad"})), faux.Say("done"))
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Tools = registry(t, tools.Echo{})
		registryOf(c).OnExecuteModel(func(ctx context.Context, in pipeline.ModelCall, next pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
			o, e := next(ctx, in)
			if e != nil {
				return nil, e
			}
			o.Message.StopReason = protocol.StopLength
			return o, nil
		})
	})
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Empty(t, eventsOf[*protocol.ToolExecutionStart](rec), "truncated replacement must not dispatch tools")
	assert.Empty(t, assistantToolCalls(lastAssistant(t, a.State().Messages)))
}
func assistantToolCalls(m protocol.AssistantMessage) []protocol.ToolCall {
	var calls []protocol.ToolCall
	for _, b := range m.Content {
		if c, ok := b.(protocol.ToolCall); ok {
			calls = append(calls, c)
		}
	}
	return calls
}

func TestFailedTerminalPartialIsLogOnly(t *testing.T) {
	a, _, _, _, _ := retryCase(t, []faux.Step{retryFailure(providers.CodeQuota, 0)}, nil)
	final := lastAssistant(t, a.State().Messages)
	assert.Empty(t, assistantText(final), "D20 may save an error wrapper, but failed attempt content is log-only")
}

func TestQuotaAfterVisibleContentSavesOnlyErrorWrapper(t *testing.T) {
	usage := protocol.Usage{Input: 12, Output: 4, TotalTokens: 16}
	step := faux.Reply(faux.Text("failed partial"), faux.ToolCall("echo", map[string]any{"text": "must not run"})).Err(providers.NewFailure(providers.CodeQuota, 429, 0, "quota exhausted", nil)).WithUsage(usage)
	a, p, log, rec, _ := retryCase(t, []faux.Step{step}, func(c *agent.Config) { c.Tools = registry(t, tools.Echo{}) })
	assert.Equal(t, 1, p.Calls())
	assert.Empty(t, eventsOf[*protocol.ToolExecutionStart](rec))
	assert.Empty(t, eventsOf[*protocol.AutoRetryStart](rec))
	final := lastAssistant(t, a.State().Messages)
	assert.Equal(t, protocol.StopError, final.StopReason)
	assert.Equal(t, "quota exhausted", errorText(final))
	assert.Empty(t, assistantText(final))
	assert.Empty(t, assistantToolCalls(final))
	assert.Equal(t, usage, final.Usage)
	settled := logged[sessions.AttemptSettled](log)
	require.Len(t, settled, 1)
	require.NotNil(t, settled[0].Usage)
	assert.Equal(t, usage, *settled[0].Usage)
	assert.Equal(t, providers.CodeQuota, settled[0].Failure.Code)
	var visible bool
	for _, e := range eventsOf[*protocol.MessageUpdate](rec) {
		if delta, ok := e.AssistantMessageEvent.(protocol.TextDeltaEvent); ok && delta.Delta == "failed partial" {
			visible = true
		}
	}
	assert.True(t, visible)
}

func TestRetryWaitErrorClosesSeries(t *testing.T) {
	p, m := newFaux(t)
	p.Set(retryFailure(providers.CodeServer, 0))
	waitErr := errors.New("wait failed")
	a := newAgent(t, p, m, func(c *agent.Config) { c.Wait = func(context.Context, time.Duration) error { return waitErr } })
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.ErrorIs(t, a.Prompt(context.Background(), user("go")), waitErr)
	assert.Equal(t, 1, p.Calls())
	ends := eventsOf[*protocol.AutoRetryEnd](rec)
	require.Len(t, ends, 1)
	assert.False(t, ends[0].Success)
	assert.Equal(t, "wait failed", ends[0].FinalError)
}

func TestPrestartFailureTextIsCleanBeforeWire(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("recovered"))
	n := 0
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Wait = func(context.Context, time.Duration) error { return nil }
		c.Stream = func(ctx context.Context, model providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) *providers.Stream {
			n++
			if n < 3 {
				return providers.NewStream(ctx, 0, protocol.AssistantMessage{}, func(s *providers.Assembler) {
					raw := "Authorization: Bearer SECRET_PRESTART_TOKEN"
					fantasykit.Fail(s, ctx, ctx, nil, providers.NewFailure(providers.CodeServer, 500, 0, raw, nil))
				})
			}
			return p.Stream(ctx, model, req, opts)
		}
	})
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	for _, e := range rec.events {
		b, err := protocol.EncodeEvent(e)
		require.NoError(t, err)
		assert.NotContains(t, string(b), "SECRET_PRESTART_TOKEN", "event %s leaked raw failure text", e.EventType())
	}
}

func TestStreamStartErrorTextIsCleanWithoutChangingContent(t *testing.T) {
	p, m := newFaux(t)
	raw := "Authorization: Bearer SECRET_START_TOKEN"
	text := "Authorization: Bearer public-example"
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Stream = func(ctx context.Context, _ providers.Model, _ providers.TranscriptRequest, _ providers.StreamOptions) *providers.Stream {
			return providers.NewStream(ctx, 0, protocol.AssistantMessage{ErrorMessage: &raw}, func(s *providers.Assembler) {
				s.Start()
				i := s.TextStart(text)
				s.TextEnd(i, text, nil)
				s.Done(protocol.StopStop)
			})
		}
	})
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	for _, event := range rec.events {
		wire, err := protocol.EncodeEvent(event)
		require.NoError(t, err)
		assert.NotContains(t, string(wire), "SECRET_START_TOKEN", event.EventType())
	}
	assert.Equal(t, text, assistantText(lastAssistant(t, a.State().Messages)))
}
