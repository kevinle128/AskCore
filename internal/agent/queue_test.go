package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

func countOf(labels []string, l string) int {
	n := 0
	for _, x := range labels {
		if x == l {
			n++
		}
	}
	return n
}

// nthIndex returns the index of the n-th (from 1) label l, or -1.
func nthIndex(labels []string, l string, n int) int {
	for i, x := range labels {
		if x == l {
			if n--; n == 0 {
				return i
			}
		}
	}
	return -1
}

// requestUsers returns the user texts that request i sent to the model.
func requestUsers(p *faux.Provider, i int) []string {
	return userTexts(p.Requests()[i].Transcript.Messages)
}

// slowStep is a reply that streams for long enough to act on it.
func slowStep() faux.Step { return faux.Say(strings.Repeat("slow words ", 50)).Pace(200) }

// idle waits until the agent has no run, including a run that a queued
// message started.
func idle(t testing.TB, a *agent.Agent) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, a.WaitForIdle(ctx))
}

// onEvent calls fn the n-th time that an event with label l is published.
func onEvent(l string, n int, fn func()) func(protocol.Event) error {
	var count atomic.Int32
	return func(ev protocol.Event) error {
		if label(ev) == l && int(count.Add(1)) == n {
			fn()
		}
		return nil
	}
}

// queueStates returns the queue_update events as "steering|followUp" strings.
func queueStates(r *recorder) []string {
	var out []string
	for _, e := range eventsOf[*protocol.QueueUpdate](r) {
		out = append(out, strings.Join(e.Steering, ",")+"|"+strings.Join(e.FollowUp, ","))
	}
	return out
}

// cycleEnds returns "reason" or "reason:cause" for each cycle_end.
func cycleEnds(r *recorder) []string {
	var out []string
	for _, e := range eventsOf[*protocol.CycleEnd](r) {
		s := e.Reason
		if e.Cause != "" {
			s += ":" + e.Cause
		}
		out = append(out, s)
	}
	return out
}

func mustSteer(t testing.TB, a *agent.Agent, text string) string {
	t.Helper()
	id, err := a.Steer(user(text))
	require.NoError(t, err)
	require.NotEmpty(t, id)
	return id
}

func mustFollowUp(t testing.TB, a *agent.Agent, text string) string {
	t.Helper()
	id, err := a.FollowUp(user(text))
	require.NoError(t, err)
	require.NotEmpty(t, id)
	return id
}

// gate blocks the listener at its first event with label l until release.
func gate(l string, reached chan<- struct{}, release <-chan struct{}) func(protocol.Event) error {
	var once sync.Once
	return func(ev protocol.Event) error {
		if label(ev) == l {
			once.Do(func() {
				close(reached)
				<-release
			})
		}
		return nil
	}
}

func twoCallsStep() faux.Step {
	return faux.Reply(
		faux.ToolCall("echo", map[string]any{"text": "a"}, faux.ID("c1")),
		faux.ToolCall("echo", map[string]any{"text": "b"}, faux.ID("c2")),
	)
}

// steeringTool steers from inside its first call. The second call waits for
// the first, so the steering message is queued while the batch still runs.
func steeringTool(a func() *agent.Agent, text string) *funcTool {
	steered := make(chan struct{})
	var once sync.Once
	return &funcTool{name: "echo", run: func(ctx context.Context, _ tools.Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
		first := false
		once.Do(func() { first = true })
		if first {
			if _, err := a().Steer(user(text)); err != nil {
				return protocol.ToolExecutionResult{}, err
			}
			close(steered)
			return textResult("one"), nil
		}
		select {
		case <-steered:
		case <-ctx.Done():
		}
		return textResult("two"), nil
	}}
}

func TestSteeringWaitsForWholeToolBatch(t *testing.T) {
	p, m := newFaux(t)
	p.Set(twoCallsStep(), faux.Say("done"))
	var a *agent.Agent
	tool := steeringTool(func() *agent.Agent { return a }, "steer me")
	a = newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, tool) })
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	labels := rec.eventLabels()
	lastCall := slices.Index(labels, "tool_execution_end(c2)")
	require.Positive(t, lastCall)
	assert.Less(t, lastCall, slices.Index(labels, "turn_end"), "the first turn closes after the whole batch")
	assert.Equal(t, 2, countOf(labels, "turn_end"))
	require.Equal(t, 2, p.Calls())
	assert.Equal(t, []string{"system", "user", "assistant", "toolResult:c1", "toolResult:c2", "user"},
		roles(p.Requests()[1].Transcript.Messages), "both results come before the steering message")
	assert.Equal(t, []string{"go", "steer me"}, requestUsers(p, 1))
}

func TestSteerDuringToolsEntersNextTurn(t *testing.T) {
	p, m := newFaux(t)
	p.Set(twoCallsStep(), faux.Say("done"))
	var a *agent.Agent
	tool := steeringTool(func() *agent.Agent { return a }, "steer me")
	a = newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, tool) })
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	labels := rec.eventLabels()
	secondStart := nthIndex(labels, "turn_start", 2)
	secondUser := nthIndex(labels, "message_start(user)", 2)
	secondAttempt := nthIndex(labels, "attempt_start", 2)
	assert.Less(t, secondStart, secondUser, "the steering message is committed after the second turn_start")
	assert.Less(t, secondUser, secondAttempt, "and before the second request")
	assert.Equal(t, 1, countOf(labels, "cycle_start"), "one cycle")
	assert.Contains(t, requestUsers(p, 1), "steer me")
}

func TestFollowUpOpensNewCycle(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	a.Subscribe(onEvent("turn_start", 1, func() { mustFollowUp(t, a, "more") }))

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	assert.Equal(t, []string{"completed", "completed"}, cycleReasons(rec))
	starts := eventsOf[*protocol.CycleStart](rec)
	require.Len(t, starts, 2)
	assert.NotEqual(t, starts[0].CycleID, starts[1].CycleID)
	assert.Equal(t, []string{"go"}, requestUsers(p, 0), "the follow-up is not part of the first cycle")
	assert.Equal(t, []string{"go", "more"}, requestUsers(p, 1))
}

func TestFollowUpDuringRunStartsNextCycle(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	var a *agent.Agent
	hooks := pipeline.NewRegistry()
	var once sync.Once
	// A follow-up sent from inside the model call, as an adapter would.
	hooks.OnExecuteModel(func(ctx context.Context, call pipeline.ModelCall, next pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
		once.Do(func() { mustFollowUp(t, a, "from adapter") })
		return next(ctx, call)
	})
	a = newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	require.Equal(t, 2, p.Calls())
	assert.Equal(t, []string{"go"}, requestUsers(p, 0))
	assert.Equal(t, []string{"go", "from adapter"}, requestUsers(p, 1))
}

func TestTurnBoundaryClaimsAllSteering(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	a.Subscribe(onEvent("turn_start", 1, func() {
		mustSteer(t, a, "s1")
		mustSteer(t, a, "s2")
		mustSteer(t, a, "s3")
	}))

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	require.Equal(t, 2, p.Calls(), "all three messages share one request")
	assert.Equal(t, []string{"go", "s1", "s2", "s3"}, requestUsers(p, 1))
	assert.Equal(t, 1, countOf(rec.eventLabels(), "cycle_start"))
}

func TestCycleBoundaryClaimsOneFollowUp(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"), faux.Say("three"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	a.Subscribe(onEvent("turn_start", 1, func() {
		mustFollowUp(t, a, "f1")
		mustFollowUp(t, a, "f2")
	}))

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	require.Equal(t, 3, p.Calls())
	assert.Equal(t, []string{"go", "f1"}, requestUsers(p, 1), "one follow-up opens the second cycle")
	assert.Equal(t, []string{"go", "f1", "f2"}, requestUsers(p, 2), "the other opens the third")
	assert.Equal(t, 3, countOf(rec.eventLabels(), "cycle_start"))
}

func TestEachFollowUpGetsOwnCycleInOrder(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("0"), faux.Say("1"), faux.Say("2"), faux.Say("3"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	a.Subscribe(onEvent("turn_start", 1, func() {
		for _, text := range []string{"f1", "f2", "f3"} {
			mustFollowUp(t, a, text)
		}
	}))

	require.NoError(t, a.Prompt(context.Background(), user("p0")))

	labels := rec.eventLabels()
	assert.Equal(t, 4, countOf(labels, "cycle_start"))
	assert.Equal(t, 1, countOf(labels, "agent_start"), "one running, then one idle")
	assert.Equal(t, 1, countOf(labels, "agent_settled"))
	for i, want := range [][]string{{"p0"}, {"p0", "f1"}, {"p0", "f1", "f2"}, {"p0", "f1", "f2", "f3"}} {
		assert.Equal(t, want, requestUsers(p, i), "request %d", i)
	}
}

func TestSteeringContinuesCycleAfterTerminatingBatch(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{}, faux.ID("c1"))), faux.Say("after"))
	var a *agent.Agent
	stop := true
	tool := &funcTool{name: "echo", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		mustSteer(t, a, "keep going")
		res := textResult("x")
		res.Terminate = &stop
		return res, nil
	}}
	a = newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, tool) })

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	require.Equal(t, 2, p.Calls(), "the steering message asks for one more request")
	assert.Contains(t, requestUsers(p, 1), "keep going")
}

func TestSteerAfterAbortRunsAsNextCycle(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep(), faux.Say("late reply"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	reached, release := make(chan struct{}), make(chan struct{})
	a.Subscribe(gate("message_update(text_delta)", reached, release))

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), reached, "the stream"))
	a.Abort()
	mustSteer(t, a, "after abort")
	close(release)
	require.NoError(t, <-done)
	idle(t, a)

	assert.Equal(t, []string{"aborted:user", "completed"}, cycleEnds(rec))
	require.Equal(t, 2, p.Calls())
	assert.Equal(t, []string{"go", "after abort"}, requestUsers(p, 1), "the message is the first input of the next run")
	assert.Equal(t, 2, countOf(rec.eventLabels(), "agent_start"), "a new run started")
}

func TestSteerAfterAbortIsNotDeliveredToAbortedCycle(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep(), faux.Say("late reply"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	reached, release := make(chan struct{}), make(chan struct{})
	a.Subscribe(gate("message_update(text_delta)", reached, release))

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), reached, "the stream"))
	a.Abort(agent.KeepQueued)
	mustSteer(t, a, "after abort")
	close(release)
	require.NoError(t, <-done)
	idle(t, a)

	assert.Equal(t, []string{"go"}, requestUsers(p, 0), "the aborted cycle never saw the message")
	labels := rec.eventLabels()
	firstEnd := slices.Index(labels, "cycle_end")
	for i, l := range labels[:firstEnd] {
		if l == "message_start(user)" {
			assert.Equal(t, "go", userTexts([]protocol.Message{rec.events[i].(*protocol.MessageStart).Message})[0])
		}
	}
	assert.Equal(t, 1, countOf(labels[:firstEnd], "message_start(user)"), "one input message in the aborted cycle")
}

func TestAbortDuringToolBatchDoesNotClaimSteering(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{}, faux.ID("c1"))), faux.Say("unused"))
	p.Append(faux.Say("ok"))
	var a *agent.Agent
	tool := &funcTool{name: "echo", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		mustSteer(t, a, "kept")
		a.Abort(agent.KeepQueued)
		return textResult("x"), nil
	}}
	a = newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, tool) })
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.NoError(t, a.Prompt(context.Background(), user("go")))
	idle(t, a)

	for i := range p.Requests() {
		assert.NotContains(t, requestUsers(p, i), "kept", "request %d", i)
	}
	assert.NotContains(t, userTexts(a.State().Messages), "kept", "the aborted run committed nothing from the queue")
	assert.Equal(t, []string{"kept"}, lastQueue(rec), "the message is still queued")
}

// lastQueue returns the steering texts of the last queue_update.
func lastQueue(r *recorder) []string {
	evs := eventsOf[*protocol.QueueUpdate](r)
	if len(evs) == 0 {
		return nil
	}
	return evs[len(evs)-1].Steering
}

func TestAbortClearsQueuesByDefault(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep(), faux.Say("one"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))
	mustSteer(t, a, "s")
	mustFollowUp(t, a, "f")
	a.Abort()
	require.NoError(t, <-done)
	idle(t, a)

	assert.Equal(t, 1, p.Calls(), "nothing queued ran")
	require.NoError(t, a.Prompt(context.Background(), user("fresh")))
	assert.Equal(t, []string{"go", "fresh"}, requestUsers(p, 1), "the cleared messages are gone")
}

func TestAbortKeepQueuedKeepsQueues(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep(), faux.Say("one"), faux.Say("two"))
	a := newAgent(t, p, m, nil)
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))
	mustSteer(t, a, "s")
	mustFollowUp(t, a, "f")
	a.Abort(agent.KeepQueued)
	require.NoError(t, <-done)
	idle(t, a)
	require.Equal(t, 1, p.Calls(), "a kept queue does not wake the agent")

	require.NoError(t, a.Prompt(context.Background(), user("next")))
	idle(t, a)

	require.Equal(t, 3, p.Calls())
	assert.Equal(t, []string{"go", "next", "s"}, requestUsers(p, 1), "the next run claims the kept steering message")
	assert.Equal(t, []string{"go", "next", "s", "f"}, requestUsers(p, 2), "and the kept follow-up opens its own cycle")
}

func TestAbortClearRacesLateSteerDeterministically(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep(), faux.Say("late reply"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); a.Abort() }()
	go func() { defer wg.Done(); _, _ = a.Steer(user("late")) }()
	wg.Wait()
	require.NoError(t, <-done)
	idle(t, a)

	// Either the clear came last and the message is gone, or the message came
	// last and it ran as the next cycle. No other state is possible.
	switch p.Calls() {
	case 1:
		assert.NotContains(t, userTexts(a.State().Messages), "late")
	case 2:
		assert.Equal(t, []string{"go", "late"}, requestUsers(p, 1))
	default:
		t.Fatalf("unexpected number of requests: %d", p.Calls())
	}
	states := queueStates(rec)
	require.NotEmpty(t, states)
	assert.Equal(t, "|", states[len(states)-1], "no message is left queued")
}

func TestSteerOnIdleAgentStartsRun(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("reply"))
	a := newAgent(t, p, m, nil)

	id, err := a.Steer(user("wake up"))
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	assert.Equal(t, agent.Running, a.State().Status, "the call started a run")
	idle(t, a)

	require.Equal(t, 1, p.Calls())
	assert.Equal(t, []string{"wake up"}, requestUsers(p, 0))
	assert.Equal(t, []string{"user", "assistant"}, roles(a.State().Messages))
}

func TestSteerWhileIdleStartsCycle(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	a.Subscribe(onEvent("attempt_start", 1, func() { mustSteer(t, a, "two") }))

	mustSteer(t, a, "one")
	idle(t, a)

	labels := rec.eventLabels()
	assert.Equal(t, 1, countOf(labels, "cycle_start"))
	assert.Equal(t, 2, countOf(labels, "turn_start"))
	require.Equal(t, 2, p.Calls())
	assert.Equal(t, []string{"one"}, requestUsers(p, 0))
	assert.Equal(t, []string{"one", "two"}, requestUsers(p, 1))
}

func TestSteerRacingRunEndIsDeliveredOrRejected(t *testing.T) {
	for _, variant := range []string{"immediate", "at cycle end"} {
		t.Run(variant, func(t *testing.T) {
			p, m := newFaux(t)
			p.Set(faux.Say("a"), faux.Say("b"), faux.Say("c"))
			a := newAgent(t, p, m, nil)
			steerNow := make(chan struct{})
			if variant == "at cycle end" {
				a.Subscribe(signalOn("cycle_end", steerNow))
			} else {
				close(steerNow)
			}
			prompted := make(chan error, 1)
			go func() { prompted <- a.Prompt(context.Background(), user("go")) }()
			<-steerNow
			_, err := a.Steer(user("racer"))
			require.NoError(t, err, "the agent is not disposed, so the message is accepted")
			if perr := <-prompted; perr != nil {
				require.ErrorIs(t, perr, agent.ErrBusy, "the steer message started the run first")
			}
			idle(t, a)

			count := 0
			for _, text := range userTexts(a.State().Messages) {
				if text == "racer" {
					count++
				}
			}
			assert.Equal(t, 1, count, "the accepted message was delivered exactly once")
		})
	}
}

func TestResetClearsQueues(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep(), faux.Say("fresh reply"))
	a := newAgent(t, p, m, nil)
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))
	mustSteer(t, a, "s")
	mustFollowUp(t, a, "f")
	a.Abort(agent.KeepQueued)
	require.NoError(t, <-done)
	idle(t, a)

	require.NoError(t, a.Reset())
	require.NoError(t, a.Prompt(context.Background(), user("fresh")))
	idle(t, a)

	require.Equal(t, 2, p.Calls(), "the reset dropped both queued messages")
	assert.Equal(t, []string{"fresh"}, requestUsers(p, 1))
}

func TestQueueFullReturnsError(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep())
	a := newAgent(t, p, m, nil)
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))
	for i := range 100 {
		mustSteer(t, a, fmt.Sprintf("m%d", i))
	}
	_, err := a.Steer(user("one too many"))
	require.ErrorIs(t, err, agent.ErrQueueFull)
	_, err = a.FollowUp(user("one too many"))
	require.ErrorIs(t, err, agent.ErrQueueFull, "the bound covers both queues together")
	a.Abort()
	require.NoError(t, <-done)
	idle(t, a)
}

func TestRemovePendingInputByID(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("unused"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	var removed, again bool
	a.Subscribe(onEvent("turn_start", 1, func() {
		id := mustSteer(t, a, "take back")
		removed = a.Remove(id)
		again = a.Remove(id)
	}))

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	assert.True(t, removed)
	assert.False(t, again, "a second removal finds nothing")
	assert.Equal(t, 1, p.Calls(), "the removed message started no turn")
	assert.Equal(t, []string{"take back|", "|"}, queueStates(rec))
	assert.NotContains(t, userTexts(a.State().Messages), "take back")
}

func TestRemoveAfterClaimChangesNothing(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	a := newAgent(t, p, m, nil)
	var id string
	a.Subscribe(onEvent("turn_start", 1, func() { id = mustSteer(t, a, "claimed") }))
	var removed atomic.Bool
	removed.Store(true)
	a.Subscribe(onEvent("turn_start", 2, func() { removed.Store(a.Remove(id)) }))

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	assert.False(t, removed.Load(), "the claim already took the message")
	require.Equal(t, 2, p.Calls())
	assert.Equal(t, []string{"go", "claimed"}, requestUsers(p, 1), "the claimed message is in the request")
}

func TestRemovedLatchedInputStartsNoCycle(t *testing.T) {
	t.Run("remove", func(t *testing.T) {
		p, m := newFaux(t, faux.WithChunk(2, 2))
		p.Set(slowStep(), faux.Say("unused"))
		a := newAgent(t, p, m, nil)
		rec := &recorder{}
		a.Subscribe(rec.emit)
		reached, release := make(chan struct{}), make(chan struct{})
		a.Subscribe(gate("message_update(text_delta)", reached, release))

		done := make(chan error, 1)
		go func() { done <- a.Prompt(context.Background(), user("go")) }()
		require.NoError(t, waitOrFail(context.Background(), reached, "the stream"))
		a.Abort(agent.KeepQueued)
		id := mustSteer(t, a, "latched")
		require.True(t, a.Remove(id))
		close(release)
		require.NoError(t, <-done)
		idle(t, a)

		assert.Equal(t, []string{"aborted:user"}, cycleEnds(rec), "one cycle only")
		assert.Equal(t, 1, p.Calls())
		assert.Equal(t, agent.Idle, a.State().Status)
	})
	t.Run("later default abort", func(t *testing.T) {
		p, m := newFaux(t, faux.WithChunk(2, 2))
		p.Set(slowStep(), faux.Say("unused"))
		a := newAgent(t, p, m, nil)
		rec := &recorder{}
		a.Subscribe(rec.emit)
		reached, release := make(chan struct{}), make(chan struct{})
		a.Subscribe(gate("message_update(text_delta)", reached, release))

		done := make(chan error, 1)
		go func() { done <- a.Prompt(context.Background(), user("go")) }()
		require.NoError(t, waitOrFail(context.Background(), reached, "the stream"))
		a.Abort(agent.KeepQueued)
		mustSteer(t, a, "latched")
		a.Abort()
		close(release)
		require.NoError(t, <-done)
		idle(t, a)

		assert.Equal(t, []string{"aborted:user"}, cycleEnds(rec), "the default abort cleared the latch too")
		assert.Equal(t, 1, p.Calls())
	})
}

func TestSendDuringAbortStartsNextCycle(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep(), faux.Say("b reply"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	reached, release := make(chan struct{}), make(chan struct{})
	a.Subscribe(gate("message_update(text_delta)", reached, release))

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), reached, "the stream"))
	a.Abort(agent.KeepQueued)
	mustFollowUp(t, a, "B")
	close(release)
	require.NoError(t, <-done)
	idle(t, a)

	assert.Equal(t, []string{"aborted:user", "completed"}, cycleEnds(rec))
	assert.Equal(t, []string{"go", "B"}, requestUsers(p, 1))
	states := queueStates(rec)
	assert.Equal(t, "|", states[len(states)-1], "the queue is empty after")
}

func TestAbortDropsPendingSteering(t *testing.T) {
	t.Run("during a stream", func(t *testing.T) {
		p, m := newFaux(t, faux.WithChunk(2, 2))
		p.Set(slowStep(), faux.Say("unused"))
		log := &sessions.MemoryLog{}
		a := newAgent(t, p, m, withLog(log))
		streaming := make(chan struct{})
		a.Subscribe(signalOn("message_update(text_delta)", streaming))

		done := make(chan error, 1)
		go func() { done <- a.Prompt(context.Background(), user("go")) }()
		require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))
		mustSteer(t, a, "gone")
		a.Abort()
		require.NoError(t, <-done)
		idle(t, a)

		assert.Equal(t, 1, p.Calls())
		assert.NotContains(t, userTexts(a.State().Messages), "gone")
		assert.NotContains(t, userTexts(sessions.MessagesOf(log.Entries())), "gone")
		assert.Equal(t, agent.Idle, a.State().Status)
	})
	t.Run("during a tool batch", func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{}, faux.ID("c1"))), faux.Say("unused"))
		var a *agent.Agent
		tool := &funcTool{name: "echo", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
			mustSteer(t, a, "gone")
			a.Abort()
			return textResult("x"), nil
		}}
		log := &sessions.MemoryLog{}
		a = newAgent(t, p, m, func(c *agent.Config) {
			c.Tools = registry(t, tool)
			c.NewContext = func() sessions.Writer { return log }
		})

		require.NoError(t, a.Prompt(context.Background(), user("go")))
		idle(t, a)

		for i := range p.Requests() {
			assert.NotContains(t, requestUsers(p, i), "gone")
		}
		assert.NotContains(t, userTexts(sessions.MessagesOf(log.Entries())), "gone", "the abort dropped the steering message")
	})
}

func TestFollowUpWhileBusyIsQueuedButPromptIsRejected(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep(), faux.Say("second"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))
	require.ErrorIs(t, a.Prompt(context.Background(), user("busy")), agent.ErrBusy)
	mustFollowUp(t, a, "queued")
	require.NoError(t, <-done)
	idle(t, a)

	require.Equal(t, 2, p.Calls(), "the rejected prompt started nothing")
	assert.Equal(t, []string{"go", "queued"}, requestUsers(p, 1))
	assert.Equal(t, []string{"completed", "completed"}, cycleReasons(rec))
}

func TestListenerCannotWriteToSessionLog(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, withLog(log))
	var before, after int
	var promptErr error
	a.Subscribe(onEvent("message_end(assistant)", 1, func() {
		before = len(log.Entries())
		promptErr = a.Prompt(context.Background(), user("from listener"))
		mustFollowUp(t, a, "queued by listener")
		after = len(log.Entries())
	}))

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	require.ErrorIs(t, promptErr, agent.ErrBusy)
	assert.Equal(t, before, after, "no call of the listener appended to the log")
	assert.Equal(t, []string{"go", "queued by listener"}, userTexts(sessions.MessagesOf(log.Entries())),
		"the queued message is committed at the next cycle boundary")
}

func TestQueueUpdateEventFollowsEveryQueueChange(t *testing.T) {
	t.Run("insert and claim", func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Say("one"), faux.Say("two"), faux.Say("three"))
		a := newAgent(t, p, m, nil)
		rec := &recorder{}
		a.Subscribe(rec.emit)
		a.Subscribe(onEvent("turn_start", 1, func() {
			mustSteer(t, a, "s")
			mustFollowUp(t, a, "f")
		}))

		require.NoError(t, a.Prompt(context.Background(), user("go")))

		assert.Equal(t, []string{"s|", "s|f", "|f", "|"}, queueStates(rec),
			"two inserts, the turn claim of the steering message, the cycle claim of the follow-up")
	})
	t.Run("abort", func(t *testing.T) {
		p, m := newFaux(t, faux.WithChunk(2, 2))
		p.Set(slowStep())
		a := newAgent(t, p, m, nil)
		rec := &recorder{}
		a.Subscribe(rec.emit)
		streaming := make(chan struct{})
		a.Subscribe(signalOn("message_update(text_delta)", streaming))

		done := make(chan error, 1)
		go func() { done <- a.Prompt(context.Background(), user("go")) }()
		require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))
		mustSteer(t, a, "s")
		a.Abort()
		require.NoError(t, <-done)

		assert.Equal(t, []string{"s|", "|"}, queueStates(rec))
	})
}

func TestAbortFromInsideHandlerDoesNotDeadlock(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("never"))
	var a *agent.Agent
	hooks := pipeline.NewRegistry()
	hooks.OnPrepareRequest(func(ctx context.Context, req pipeline.Request, next pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
		a.Abort()
		return next(ctx, req)
	})
	a = newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })
	rec := &recorder{}
	a.Subscribe(rec.emit)

	within(t, 5*time.Second, func() { _ = a.Prompt(context.Background(), user("go")) })

	assert.Equal(t, []string{"aborted:user"}, cycleEnds(rec))
}

func TestAbortFromInsideListenerEndsCycleAborted(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep())
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	a.Subscribe(onEvent("message_update(text_delta)", 1, func() { a.Abort() }))

	within(t, 5*time.Second, func() { _ = a.Prompt(context.Background(), user("go")) })

	assert.Equal(t, []string{"aborted:user"}, cycleEnds(rec))
	assert.Equal(t, agent.Idle, a.State().Status)
}

func TestPromptAfterCancelledCycleRunsNormally(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep(), faux.Say("fine"))
	a := newAgent(t, p, m, nil)
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))
	a.Abort()
	require.NoError(t, <-done)

	require.NoError(t, a.Prompt(context.Background(), user("again")))

	require.Equal(t, 2, p.Calls(), "one request for the new prompt")
	assert.Equal(t, []string{"go", "again"}, requestUsers(p, 1))
	assert.Equal(t, "fine", assistantText(lastAssistant(t, a.State().Messages)))
}

func TestWaitForIdleReturnsAfterCancelledRun(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep())
	a := newAgent(t, p, m, nil)
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))
	go func() { _ = a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))

	waited := make(chan error, 1)
	go func() { waited <- a.WaitForIdle(context.Background()) }()
	a.Abort()

	select {
	case err := <-waited:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("WaitForIdle did not return after the cancelled run")
	}
}

func TestWaitForIdleReturnsAfterFailedRun(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("unused"))
	boom := errors.New("prepare failed")
	proceed := make(chan struct{})
	hooks := pipeline.NewRegistry()
	hooks.OnPrepareRequest(func(context.Context, pipeline.Request, pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
		<-proceed
		return nil, boom
	})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })
	running := make(chan struct{})
	a.Subscribe(signalOn("turn_start", running))
	errs := make(chan error, 1)
	go func() { errs <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), running, "the run"))

	waited := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		waited <- a.WaitForIdle(context.Background())
	}()
	require.NoError(t, waitOrFail(context.Background(), started, "the waiter"))
	close(proceed)

	select {
	case err := <-waited:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("WaitForIdle did not return after the failed run")
	}
	require.ErrorIs(t, <-errs, boom)
}

func TestStatusIsRunningBetweenAgentStartAndSettled(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("hi"))
	a := newAgent(t, p, m, nil)
	var atStart, atSettled agent.Status = 99, 99
	a.Subscribe(onEvent("agent_start", 1, func() { atStart = a.State().Status }))
	a.Subscribe(onEvent("agent_settled", 1, func() { atSettled = a.State().Status }))

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	assert.Equal(t, agent.Running, atStart)
	assert.Equal(t, agent.Running, atSettled, "the run is over only when the settled event has been published")
	assert.Equal(t, agent.Idle, a.State().Status)
}

func TestAbortWhileIdleKeepsNextPrompt(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("hi"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)

	within(t, time.Second, func() { a.Abort() })
	_, err := a.FollowUp(user("next"))
	require.NoError(t, err)
	idle(t, a)

	assert.Equal(t, []string{"completed"}, cycleEnds(rec), "no marker was armed")
	assert.Equal(t, []string{"next"}, userTexts(a.State().Messages))
}

func TestQueuedMessageIsIsolatedFromCaller(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep(), faux.Say("second"))
	var seen []string
	var mu sync.Mutex
	hooks := pipeline.NewRegistry()
	hooks.OnAdmitStep(func(ctx context.Context, in pipeline.AdmitInput, next pipeline.Next[pipeline.AdmitInput, pipeline.AdmitDecision]) (pipeline.AdmitDecision, error) {
		mu.Lock()
		seen = append(seen, userTexts(in.Messages)...)
		mu.Unlock()
		return next(ctx, in)
	})
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Pipeline = hooks
		c.NewContext = func() sessions.Writer { return log }
	})
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))
	msg := protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "original"}}, Timestamp: 1}
	_, err := a.FollowUp(msg)
	require.NoError(t, err)
	msg.Content[0] = protocol.Text{Text: "changed by the caller"}
	require.NoError(t, <-done)
	idle(t, a)

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, seen, "original", "the admission handler saw the message as sent")
	assert.NotContains(t, seen, "changed by the caller")
	texts := userTexts(sessions.MessagesOf(log.Entries()))
	assert.Equal(t, []string{"go", "original"}, texts)
}

func TestSteerReturnsErrorForNilMessage(t *testing.T) {
	p, m := newFaux(t)
	a := newAgent(t, p, m, nil)
	_, err := a.Steer(nil)
	require.ErrorIs(t, err, agent.ErrNoInput)
	_, err = a.FollowUp(nil)
	require.ErrorIs(t, err, agent.ErrNoInput)
	assert.Equal(t, agent.Idle, a.State().Status, "a refused message starts no run")
}

// Each ID that Steer and FollowUp return is new.
func TestInputIDsAreUnique(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(slowStep())
	a := newAgent(t, p, m, nil)
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))

	seen := map[string]bool{}
	for i := range 10 {
		id := mustSteer(t, a, fmt.Sprint(i))
		assert.False(t, seen[id])
		seen[id] = true
		id = mustFollowUp(t, a, fmt.Sprint(i))
		assert.False(t, seen[id])
		seen[id] = true
	}
	a.Abort()
	require.NoError(t, <-done)
}
