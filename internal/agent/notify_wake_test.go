package agent_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers/faux"
	"AskCore/internal/tasks"
	"AskCore/pkg/protocol"
)

// A notice that arrives while a turn runs is claimed at the turn boundary. A
// turn that ends the run without that claim must not strand the notice: the
// run that follows takes it.

func TestWakeNoticeAfterFailedTurnStartsRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(
			faux.Reply(timerCall("c1", "late", "1s", "wake")),
			// The timer fires while this reply waits, and the reply ends in an error.
			faux.Say("failing").Delay(2*time.Second).Stop(protocol.StopError),
			faux.Say("noticed"),
		)
		a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, timerTool()) })

		require.NoError(t, a.Prompt(context.Background(), user("go")))
		require.NoError(t, a.WaitForIdle(context.Background()))
		synctest.Wait()

		require.Equal(t, 3, p.Calls(), "a second run took the notice")
		assert.Len(t, noticeUsers(p, 2), 1)
		time.Sleep(time.Hour)
		synctest.Wait()
		assert.Equal(t, 3, p.Calls(), "nothing else ran")
		require.NoError(t, a.Dispose())
	})
}

func TestWakeNoticeAfterEndDecisionStartsRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(
			faux.Reply(timerCall("c1", "late", "1s", "wake"), sleepCall("c2")),
			faux.Say("noticed"),
		)
		a := newAgent(t, p, m, func(c *agent.Config) {
			c.Tools = registry(t, timerTool(), sleepTool(2*time.Second))
			registryOf(c).OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
				return pipeline.End, nil
			})
		})

		require.NoError(t, a.Prompt(context.Background(), user("go")))
		require.NoError(t, a.WaitForIdle(context.Background()))
		synctest.Wait()

		require.Equal(t, 2, p.Calls(), "a second run took the notice")
		assert.Len(t, noticeUsers(p, 1), 1)
		time.Sleep(time.Hour)
		synctest.Wait()
		assert.Equal(t, 2, p.Calls(), "nothing else ran")
		require.NoError(t, a.Dispose())
	})
}

// The notice lands while the admission of a prompt runs. The admission rejects
// that prompt, so the turn ends with no claim. The follow-up run claims the
// notice and its admission rejects as well. The wake is used up by then, so the
// Agent goes idle and does not loop.
func TestWakeNoticeAfterRejectedAdmissionRunsOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Reply(timerCall("c1", "late", "3s", "wake")), faux.Say("started"))
		var calls int
		a := newAgent(t, p, m, func(c *agent.Config) {
			c.Tools = registry(t, timerTool())
			registryOf(c).OnAdmitStep(func(ctx context.Context, in pipeline.AdmitInput, next nextAdmit) (pipeline.AdmitDecision, error) {
				if len(in.Messages) == 0 {
					return next(ctx, in) // a turn after a tool batch has no input
				}
				calls++
				switch calls {
				case 1:
					return next(ctx, in)
				case 2:
					time.Sleep(5 * time.Second) // the timer fires meanwhile
				}
				return pipeline.AdmitDecision{Reject: true}, nil
			})
		})
		rec := &recorder{}
		a.Subscribe(rec.emit)

		require.NoError(t, a.Prompt(context.Background(), user("go")))
		require.NoError(t, a.Prompt(context.Background(), user("again")))
		require.NoError(t, a.WaitForIdle(context.Background()))
		synctest.Wait()

		assert.Equal(t, 3, countOf(rec.eventLabels(), "agent_start"), "one extra run")
		assert.Equal(t, []string{"completed", "blocked", "blocked"}, cycleReasons(rec))
		assert.Equal(t, 3, calls)
		time.Sleep(time.Hour)
		synctest.Wait()
		assert.Equal(t, 3, countOf(rec.eventLabels(), "agent_start"), "the wake did not loop")
		require.NoError(t, a.WaitForQuiescence(context.Background()))
		require.NoError(t, a.Dispose())
	})
}

func TestNotifyRejectsMalformedMessage(t *testing.T) {
	p, m := newFaux(t)
	a := newAgent(t, p, m, nil)
	var nilCustom *protocol.CustomMessage
	bad := map[string]protocol.Message{
		"nil":                nil,
		"typed nil":          nilCustom,
		"empty custom type":  protocol.CustomMessage{Content: []protocol.UserBlock{protocol.Text{Text: "x"}}},
		"pointer empty type": &protocol.CustomMessage{},
	}
	for name, msg := range bad {
		t.Run(name, func(t *testing.T) {
			n := plainNotice("tm-1", "bad", tasks.NoticeWake)
			n.Message = msg
			_, err := a.Notify(n)
			require.Error(t, err)
			assert.NotErrorIs(t, err, tasks.ErrNotDelivered, "the registry records failed, not discarded")
			assert.Equal(t, agent.Idle, a.State().Status, "no run started")
		})
	}
	require.NoError(t, a.Dispose())
}

// A Context notice asks for no model request. When it lands during the final
// reply of a run, the run ends with no extra request and the notice rides with
// the next prompt.
func TestContextNoticeDuringFinalReplyRidesWithNextPrompt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(
			faux.Reply(timerCall("c1", "quiet", "1s", "context")),
			faux.Say("final").Delay(2*time.Second), // the timer fires while this reply waits
			faux.Say("next"),
		)
		a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, timerTool()) })

		require.NoError(t, a.Prompt(context.Background(), user("go")))
		require.NoError(t, a.WaitForIdle(context.Background()))
		synctest.Wait()
		assert.Equal(t, 2, p.Calls(), "the notice caused no request")

		require.NoError(t, a.Prompt(context.Background(), user("again")))
		require.Equal(t, 3, p.Calls())
		assert.Len(t, noticeUsers(p, 2), 1, "the notice rides with the next prompt")
		require.NoError(t, a.Dispose())
	})
}
