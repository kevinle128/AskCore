package agent_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

func lastSettledUsage(t *testing.T, rec *recorder) *protocol.UsageTotals {
	t.Helper()
	all := eventsOf[*protocol.AgentSettled](rec)
	require.NotEmpty(t, all)
	return all[len(all)-1].Usage
}

func TestAttemptStampsUnknownPrice(t *testing.T) {
	for _, tc := range []struct {
		name    string
		unknown bool
	}{{"unpriced model", true}, {"priced model", false}} {
		t.Run(tc.name, func(t *testing.T) {
			p, m := newFaux(t)
			m.CostUnknown = tc.unknown
			p.Set(faux.Say("hi").WithUsage(protocol.Usage{Input: 10, Output: 2}))
			log := &sessions.MemoryLog{}
			a := newAgent(t, p, m, withLog(log))
			require.NoError(t, a.Prompt(context.Background(), user("go")))

			settled := logged[sessions.AttemptSettled](log)
			require.Len(t, settled, 1)
			assert.Equal(t, tc.unknown, settled[0].CostUnknown)
		})
	}
}

// refusedStream ends at once with a failure and no output and no usage, as a
// provider that refuses the request before it streams.
func refusedStream(code string, status int) func(context.Context, providers.Model, providers.TranscriptRequest, providers.StreamOptions) *providers.Stream {
	return func(ctx context.Context, m providers.Model, _ providers.TranscriptRequest, _ providers.StreamOptions) *providers.Stream {
		seed := protocol.AssistantMessage{API: string(m.API), Provider: m.Provider, Model: m.ID}
		return providers.NewStream(ctx, 0, seed, func(a *providers.Assembler) {
			failure := providers.NewFailure(code, status, 0, "refused", nil)
			a.Fail(protocol.StopError, failure.Error(), failure)
		})
	}
}

func TestRefusedAttemptIsStampedNoOutput(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("done"))
	log := &sessions.MemoryLog{}
	calls := 0
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		c.Wait = func(context.Context, time.Duration) error { return nil }
		real := c.Stream
		refuse := refusedStream(providers.CodeRateLimit, 429)
		c.Stream = func(ctx context.Context, mm providers.Model, req providers.TranscriptRequest, o providers.StreamOptions) *providers.Stream {
			calls++
			if calls == 1 {
				return refuse(ctx, mm, req, o)
			}
			return real(ctx, mm, req, o)
		}
	}))
	require.NoError(t, a.Prompt(context.Background(), user("go")))

	settled := logged[sessions.AttemptSettled](log)
	require.Len(t, settled, 2)
	assert.Equal(t, "failed", settled[0].Outcome)
	assert.Nil(t, settled[0].Usage)
	assert.True(t, settled[0].NoOutput, "a refused request streamed nothing")
	assert.False(t, settled[1].NoOutput)
	_, totals := sessions.FoldUsage(log.Entries())
	assert.True(t, totals.Complete, "the refused attempt counts as zero")
}

func TestFailedAttemptWithOutputIsNotNoOutput(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("partial text").Err(providers.NewFailure(providers.CodeAuth, 401, 0, "denied", nil)))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, withLog(log))
	require.NoError(t, a.Prompt(context.Background(), user("go")))

	settled := logged[sessions.AttemptSettled](log)
	require.Len(t, settled, 1)
	assert.False(t, settled[0].NoOutput, "content was streamed before the failure")
}

// settleFailsOnce fails the first write of an attempt settlement, so the run
// ends in the failure path while the attempt is still open.
type settleFailsOnce struct {
	sessions.MemoryLog
	failed bool
}

func (l *settleFailsOnce) Append(entries ...sessions.Entry) (sessions.CommitRef, error) {
	for _, e := range entries {
		if _, ok := e.(sessions.AttemptSettled); ok && !l.failed {
			l.failed = true
			return sessions.CommitRef{}, errors.New("disk full")
		}
	}
	return l.MemoryLog.Append(entries...)
}

func TestFailureStepWriterStampsNoOutputAndPrice(t *testing.T) {
	for _, tc := range []struct {
		name         string
		stream       func(*faux.Provider) func(context.Context, providers.Model, providers.TranscriptRequest, providers.StreamOptions) *providers.Stream
		unknownPrice bool
		wantNoOutput bool
	}{
		{
			name: "no output on an unpriced model",
			stream: func(*faux.Provider) func(context.Context, providers.Model, providers.TranscriptRequest, providers.StreamOptions) *providers.Stream {
				return refusedStream(providers.CodeAuth, 401)
			},
			unknownPrice: true,
			wantNoOutput: true,
		},
		{
			name: "output streamed on a priced model",
			stream: func(p *faux.Provider) func(context.Context, providers.Model, providers.TranscriptRequest, providers.StreamOptions) *providers.Stream {
				return p.Stream
			},
			wantNoOutput: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, m := newFaux(t)
			m.CostUnknown = tc.unknownPrice
			p.Set(faux.Say("some words"))
			log := &settleFailsOnce{}
			a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) { c.Stream = tc.stream(p) }))
			require.Error(t, a.Prompt(context.Background(), user("go")))

			var settled []sessions.AttemptSettled
			for _, e := range log.Entries() {
				if s, ok := e.(sessions.AttemptSettled); ok {
					settled = append(settled, s)
				}
			}
			require.Len(t, settled, 1, "the failure path writes the settlement that the loop could not")
			assert.Equal(t, "failed", settled[0].Outcome)
			assert.Equal(t, tc.wantNoOutput, settled[0].NoOutput)
			assert.Equal(t, tc.unknownPrice, settled[0].CostUnknown)
		})
	}
}

func TestSettledUsageMatchesFold(t *testing.T) {
	p, m := newFaux(t)
	m.ContextWindow = 5000
	p.Set(
		faux.Say("flaky").Err(providers.NewFailure(providers.CodeServer, 503, 0, "busy", nil)).WithUsage(protocol.Usage{Input: 50, Output: 1, TotalTokens: 51}),
		faux.Say("first").WithUsage(protocol.Usage{Input: 100, Output: 10, TotalTokens: 110}),
		faux.Say("second").WithUsage(protocol.Usage{Input: 200, Output: 20, TotalTokens: 220}),
	)
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		c.Wait = func(context.Context, time.Duration) error { return nil }
	}))
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.NoError(t, a.Prompt(context.Background(), user("one")))
	require.NoError(t, a.Prompt(context.Background(), user("two")))

	_, want := sessions.FoldUsage(log.Entries())
	tokens, ok := sessions.EstimateContext(log.Entries())
	require.True(t, ok)
	want.ContextTokens, want.ContextWindow = tokens, 5000
	got := lastSettledUsage(t, rec)
	require.NotNil(t, got)
	assert.Equal(t, want, *got)
	assert.Equal(t, int64(51+110+220), got.Total.TotalTokens, "the retried attempt counts")
	assert.Equal(t, int64(220), got.ContextTokens)
}

func TestSettledUsageIsNilWithoutAttempt(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("unused"))
	a := newAgent(t, p, m, func(c *agent.Config) {
		registryOf(c).OnAdmitStep(func(context.Context, pipeline.AdmitInput, pipeline.Next[pipeline.AdmitInput, pipeline.AdmitDecision]) (pipeline.AdmitDecision, error) {
			return pipeline.AdmitDecision{Reject: true}, nil
		})
	})
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Nil(t, lastSettledUsage(t, rec))
}

func addedTokens(msgs []protocol.Message) int64 {
	var total int64
	for _, m := range msgs {
		n := 0
		switch v := m.(type) {
		case protocol.UserMessage:
			for _, b := range v.Content {
				if t, ok := b.(protocol.Text); ok {
					n += len(t.Text)
				}
			}
		case protocol.AssistantMessage:
			n = len(assistantText(v))
		}
		total += int64((n + 3) / 4)
	}
	return total
}

func TestEstimateAgreesWithNextRequest(t *testing.T) {
	p, m := newFaux(t)
	p.Set(
		faux.Say("the first answer is a bit longer").WithUsage(protocol.Usage{Input: 100, Output: 10, TotalTokens: 110}),
		faux.Say("second"),
	)
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, withLog(log))
	require.NoError(t, a.Prompt(context.Background(), user("one")))
	_, err := log.Append(sessions.MessageEntry{Message: user("a second question of some length")})
	require.NoError(t, err)
	estimate, ok := sessions.EstimateContext(log.Entries())
	require.True(t, ok)
	require.NoError(t, a.Continue(context.Background()))

	ids := attemptsOf(log)
	require.Len(t, ids, 2)
	first, err := agent.RebuildRequest(log.Entries(), ids[0])
	require.NoError(t, err)
	second, err := agent.RebuildRequest(log.Entries(), ids[1])
	require.NoError(t, err)
	require.Greater(t, len(second.Messages), len(first.Messages))
	added := second.Messages[len(first.Messages):]
	assert.Equal(t, []string{"assistant", "user"}, roles(added), "the request adds the reply and the new message after the anchor request")
	// The anchor usage already counts the reply it answered with, so only the
	// messages after the reply are estimated.
	assert.Equal(t, int64(110)+addedTokens(added[1:]), estimate)
}

func TestResumeAfterErrorReplyKeepsLastGoodAnchor(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Fail("provider is down"))
	log := &sessions.MemoryLog{}
	_, err := log.Append(
		sessions.MessageEntry{Message: user("earlier")},
		sessions.MessageEntry{Message: protocol.AssistantMessage{Content: []protocol.AssistantBlock{protocol.Text{Text: "reply"}}, StopReason: protocol.StopStop}},
		sessions.AttemptSettled{AttemptID: "a0", Outcome: "completed", Usage: &protocol.Usage{Input: 150, Output: 50, TotalTokens: 200}},
		sessions.MessageEntry{Message: user("a question that is not answered yet")},
	)
	require.NoError(t, err)
	rec := &recorder{}
	a := newAgent(t, p, m, withLog(log))
	a.Subscribe(rec.emit)
	require.NoError(t, a.Continue(context.Background()))

	got := lastSettledUsage(t, rec)
	require.NotNil(t, got)
	tail := int64((len("a question that is not answered yet") + 3) / 4)
	assert.Equal(t, 200+tail, got.ContextTokens, "the failed attempt and its error reply add nothing")
}

func TestUsageFoldOfLargeLogIsFast(t *testing.T) {
	entries := make([]sessions.Entry, 0, 10000)
	for i := 0; i < 5000; i++ {
		entries = append(entries,
			sessions.MessageEntry{Message: user("message " + strconv.Itoa(i))},
			sessions.AttemptSettled{AttemptID: strconv.Itoa(i), Outcome: "completed", Usage: &protocol.Usage{Input: 10, Output: 1, TotalTokens: 11}},
		)
	}
	best := time.Hour
	for i := 0; i < 5; i++ {
		start := time.Now()
		_, totals := sessions.FoldUsage(entries)
		_, _ = sessions.EstimateContext(entries)
		best = min(best, time.Since(start))
		require.Equal(t, int64(55000), totals.Total.TotalTokens)
	}
	assert.Less(t, best, 10*time.Millisecond)
}

func TestFailureStepWriterKeepsPriceStateOfAttemptModel(t *testing.T) {
	p, m := newFaux(t)
	m.CostUnknown = true
	p.Set(faux.Say("some words"))
	log := &settleFailsOnce{}
	a := newAgent(t, p, m, edits(withLog(log), func(c *agent.Config) {
		// A request hook moves the run to a priced model, so the configured
		// model is not the one that the attempt ran on.
		registryOf(c).OnPrepareRequest(func(_ context.Context, r pipeline.Request, _ pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
			priced := r.Model
			priced.CostUnknown = false
			return &pipeline.RequestUpdate{Model: &priced}, nil
		})
	}))
	require.Error(t, a.Prompt(context.Background(), user("go")))

	var settled []sessions.AttemptSettled
	for _, e := range log.Entries() {
		if s, ok := e.(sessions.AttemptSettled); ok {
			settled = append(settled, s)
		}
	}
	require.Len(t, settled, 1)
	assert.False(t, settled[0].CostUnknown, "the attempt ran on a priced model")
}
