package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// eventsOf returns the events of type T that the recorder saw, in order.
func eventsOf[T protocol.Event](r *recorder) []T {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []T
	for _, ev := range r.events {
		if e, ok := ev.(T); ok {
			out = append(out, e)
		}
	}
	return out
}

func cycleReasons(r *recorder) []string {
	var out []string
	for _, e := range eventsOf[*protocol.CycleEnd](r) {
		out = append(out, e.Reason)
	}
	return out
}

func TestCycleReasonStaysMaxTokensAfterLaterCompletedTurn(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("cut off").Stop(protocol.StopLength), faux.Say("done"))
	rec := &recorder{}
	a := newAgent(t, p, m, nil)
	a.Subscribe(rec.emit)
	a.Subscribe(onEvent("turn_start", 1, func() { mustSteer(t, a, "go on") }))

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	assert.Equal(t, 2, p.Calls(), "steering gave the cycle a second turn")
	assert.Equal(t, []string{"max-tokens"}, cycleReasons(rec),
		"the later turn completed, yet the cycle keeps the sticky reason")
}

func TestMaxTokensReasonDoesNotLeakIntoNextCycle(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("cut off").Stop(protocol.StopLength), faux.Say("clean"))
	rec := &recorder{}
	a := newAgent(t, p, m, nil)
	a.Subscribe(rec.emit)
	a.Subscribe(onEvent("turn_start", 1, func() { mustFollowUp(t, a, "more") }))

	require.NoError(t, a.Prompt(context.Background(), user("first")))

	assert.Equal(t, []string{"max-tokens", "completed"}, cycleReasons(rec),
		"the follow-up batch is a new cycle of the same run and starts with no reason")
	ends := eventsOf[*protocol.CycleEnd](rec)
	assert.NotEqual(t, ends[0].CycleID, ends[1].CycleID)
}

func TestContinueDecisionDoesNotExtendMaxTokensCycle(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("cut off").Stop(protocol.StopLength), faux.Say("unused"))
	rec := &recorder{}
	handlers := pipeline.NewRegistry()
	handlers.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
		return pipeline.Continue, nil
	})

	_, err := agent.Run(context.Background(), []protocol.Message{user("go")},
		pipeline.AgentContext{}, config(p, m, handlers), rec.emit)
	require.NoError(t, err)

	assert.Equal(t, 1, p.Calls(), "a cut-off reply asks for no extra request")
	assert.Equal(t, []string{"max-tokens"}, cycleReasons(rec))
}

func TestTruncatedMessageToolCallsAreDroppedAndNeverRun(t *testing.T) {
	var hooked []string
	hooks := pipeline.NewRegistry()
	hooks.OnBeforeTool(func(_ context.Context, info pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
		hooked = append(hooked, "before:"+info.Call.ID)
		return nil, nil
	})
	hooks.OnAfterTool(func(_ context.Context, info pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
		hooked = append(hooked, "after:"+info.Call.ID)
		return nil, nil
	})
	executed := 0
	tool := &funcTool{name: "write", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		executed++
		return textResult("ok"), nil
	}}
	r := runTools(t, context.Background(), registry(t, tool, tools.Echo{}), hooks, nil,
		faux.Reply(faux.Text("let me write"), call("write", "a", nil), call("echo", "b", map[string]any{"text": "x"})).Stop(protocol.StopLength),
		faux.Say("unused"))

	assert.Zero(t, executed, "the truncated call never runs")
	assert.Empty(t, hooked, "no tool hook runs")
	assert.Equal(t, 1, r.p.Calls(), "one request, no re-issue turn")
	assert.Empty(t, toolLabels(r.rec.eventLabels()), "no tool_execution event and no tool result message")
	assert.Empty(t, toolResults(r.msgs))

	final := lastAssistant(t, r.msgs)
	assert.Equal(t, protocol.StopLength, final.StopReason)
	assert.Equal(t, []protocol.AssistantBlock{protocol.Text{Text: "let me write"}}, final.Content,
		"the committed message holds no tool-call block")
	ends := eventsOf[*protocol.MessageEnd](r.rec)
	require.NotEmpty(t, ends)
	published := ends[len(ends)-1].Message.(protocol.AssistantMessage)
	assert.Equal(t, final.Content, published.Content, "the message_end event holds the stripped message too")
	assert.Equal(t, []string{"max-tokens"}, cycleReasons(r.rec))
}

func TestMaxTokensDropKeepsTextReplayDataAndResponseID(t *testing.T) {
	sig := "sig-1"
	respID := "resp_1"
	seed := protocol.AssistantMessage{API: "faux", Provider: "faux", Model: "faux-1", Timestamp: 3, StopReason: protocol.StopPending}
	final := seed
	final.StopReason = protocol.StopLength
	final.ResponseID = &respID
	final.Content = []protocol.AssistantBlock{
		protocol.Thinking{Thinking: "plan", ThinkingSignature: &sig},
		protocol.Text{Text: "partial"},
		protocol.ToolCall{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"x"}`)},
	}
	p, m := newFaux(t)
	p.Set(faux.Raw(
		protocol.StartEvent{Message: seed},
		protocol.ThinkingStartEvent{ContentIndex: 0, Content: protocol.Thinking{Thinking: "plan"}},
		protocol.ThinkingEndEvent{ContentIndex: 0, Content: "plan", ThinkingSignature: &sig},
		protocol.TextStartEvent{ContentIndex: 1, Content: protocol.Text{Text: "partial"}},
		protocol.TextEndEvent{ContentIndex: 1, Content: "partial"},
		protocol.ToolCallStartEvent{ContentIndex: 2, ID: "c1", ToolName: "echo"},
		protocol.ToolCallEndEvent{ContentIndex: 2, ToolCall: protocol.ToolCall{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"x"}`)}},
		protocol.DoneEvent{Reason: protocol.StopLength, Message: final},
	))
	rec := &recorder{}

	msgs, err := agent.Run(context.Background(), []protocol.Message{user("go")},
		pipeline.AgentContext{Tools: registry(t, tools.Echo{})}, config(p, m, nil), rec.emit)
	require.NoError(t, err)

	got := lastAssistant(t, msgs)
	require.Equal(t, protocol.StopLength, got.StopReason, "error: %s", errorText(got))
	assert.Equal(t, []protocol.AssistantBlock{
		protocol.Thinking{Thinking: "plan", ThinkingSignature: &sig},
		protocol.Text{Text: "partial"},
	}, got.Content, "text and thinking stay, with the thinking signature on its block")
	require.NotNil(t, got.ResponseID)
	assert.Equal(t, respID, *got.ResponseID)
}

func TestCycleBoundariesWrapTurns(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
	rec := &recorder{}

	_, err := agent.Run(context.Background(), []protocol.Message{user("echo hi")},
		pipeline.AgentContext{Tools: registry(t, tools.Echo{})}, config(p, m, nil), rec.emit)
	require.NoError(t, err)

	var boundaries []string
	for _, l := range rec.eventLabels() {
		switch l {
		case "cycle_start", "turn_start", "turn_end", "cycle_end", "agent_start", "agent_end":
			boundaries = append(boundaries, l)
		}
	}
	assert.Equal(t, []string{"agent_start", "cycle_start", "turn_start", "turn_end", "turn_start", "turn_end", "cycle_end", "agent_end"}, boundaries)
	assert.Equal(t, []string{"completed"}, cycleReasons(rec))
	reqs := p.Requests()
	require.Len(t, reqs, 2)
	assert.Contains(t, roles(reqs[1].Transcript.Messages), "toolResult:c1", "the second request holds the result of call c1")
}

func TestOneInputCycleWithTwoTurnsHasOneCycleID(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
	rec := &recorder{}

	_, err := agent.Run(context.Background(), []protocol.Message{user("echo hi")},
		pipeline.AgentContext{Tools: registry(t, tools.Echo{})}, config(p, m, nil), rec.emit)
	require.NoError(t, err)

	starts := eventsOf[*protocol.CycleStart](rec)
	require.Len(t, starts, 1)
	id := starts[0].CycleID
	assert.NotEmpty(t, id)
	turnStarts := eventsOf[*protocol.TurnStart](rec)
	turnEnds := eventsOf[*protocol.TurnEnd](rec)
	require.Len(t, turnStarts, 2)
	require.Len(t, turnEnds, 2)
	for _, e := range turnStarts {
		assert.Equal(t, id, e.CycleID)
	}
	for _, e := range turnEnds {
		assert.Equal(t, id, e.CycleID)
	}
	assert.Equal(t, id, eventsOf[*protocol.CycleEnd](rec)[0].CycleID)
}

func TestFollowUpStartsNewCycleWithNewID(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	rec := &recorder{}
	a := newAgent(t, p, m, nil)
	a.Subscribe(rec.emit)
	a.Subscribe(onEvent("turn_start", 1, func() { mustFollowUp(t, a, "more") }))

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	starts := eventsOf[*protocol.CycleStart](rec)
	require.Len(t, starts, 2)
	assert.NotEqual(t, starts[0].CycleID, starts[1].CycleID)
	var cycleLabels []string
	for _, l := range rec.eventLabels() {
		if l == "cycle_start" || l == "cycle_end" || l == "turn_start" {
			cycleLabels = append(cycleLabels, l)
		}
	}
	assert.Equal(t, []string{"cycle_start", "turn_start", "cycle_end", "cycle_start", "turn_start", "cycle_end"}, cycleLabels)
}

func TestEachModelCallOpensOneAttempt(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.NoError(t, a.Prompt(context.Background(), user("echo hi")))

	starts := eventsOf[*protocol.AttemptStart](rec)
	ends := eventsOf[*protocol.AttemptEnd](rec)
	require.Len(t, starts, 2, "two model calls, two attempts")
	require.Len(t, ends, 2)
	cycleID := eventsOf[*protocol.CycleStart](rec)[0].CycleID
	assert.NotEqual(t, starts[0].AttemptID, starts[1].AttemptID)
	for i := range starts {
		assert.Equal(t, i+1, starts[i].Number)
		assert.Equal(t, cycleID, starts[i].CycleID)
		assert.Equal(t, starts[i].AttemptID, ends[i].AttemptID)
		assert.Equal(t, "completed", ends[i].Outcome)
	}
	// Each assistant message lies between its attempt_start and attempt_end.
	var order []string
	for _, l := range rec.eventLabels() {
		if l == "attempt_start" || l == "attempt_end" || l == "message_start(assistant)" || l == "message_end(assistant)" {
			order = append(order, l)
		}
	}
	assert.Equal(t, []string{
		"attempt_start", "message_start(assistant)", "message_end(assistant)", "attempt_end",
		"attempt_start", "message_start(assistant)", "message_end(assistant)", "attempt_end",
	}, order)
}

func TestAttemptNumberCountsTheSessionAcrossCycles(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"), faux.Say("three"))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.NoError(t, a.Prompt(context.Background(), user("a")))
	require.NoError(t, a.Prompt(context.Background(), user("b")))
	require.NoError(t, a.Prompt(context.Background(), user("c")))

	var numbers []int
	for _, e := range eventsOf[*protocol.AttemptStart](rec) {
		numbers = append(numbers, e.Number)
	}
	assert.Equal(t, []int{1, 2, 3}, numbers, "the counter does not restart for each cycle or run")
}

// setupFailure runs a prompt whose first event fails in a listener, so the run
// fails before its cycle opens.
func setupFailure(t *testing.T) *recorder {
	t.Helper()
	p, m := newFaux(t)
	p.Set(faux.Say("unused"))
	// The log refuses the CycleOpened entry, so the run fails after agent_start
	// and before cycle_start is published.
	log := &commitGate{failWhen: func(entries []sessions.Entry) bool {
		for _, e := range entries {
			if _, ok := e.(sessions.CycleOpened); ok {
				return true
			}
		}
		return false
	}, err: errors.New("disk full")}
	a := newAgent(t, p, m, withLog(log))
	rec := &recorder{}
	a.Subscribe(rec.emit)
	require.ErrorContains(t, a.Prompt(context.Background(), user("hi")), "disk full")
	return rec
}

func TestRunFailureTailKeepsPiTurnEnd(t *testing.T) {
	rec := setupFailure(t)

	assert.Equal(t, []string{
		"agent_start",
		"message_start(assistant)", "message_end(assistant)", "turn_end", "agent_end", "agent_settled",
	}, rec.eventLabels(),
		"turn_end follows the failure message although no turn_start was published")
}

func TestNoCycleOrAttemptEndForUnopenedScope(t *testing.T) {
	rec := setupFailure(t)

	assert.Empty(t, eventsOf[*protocol.CycleStart](rec))
	assert.Empty(t, eventsOf[*protocol.CycleEnd](rec))
	assert.Empty(t, eventsOf[*protocol.AttemptStart](rec))
	assert.Empty(t, eventsOf[*protocol.AttemptEnd](rec))
}

func TestFailureBeforeStreamReturnsEmitsNoAttemptEvents(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("unused"))
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.GetAPIKey = func(context.Context, string) (string, error) {
			return "", errors.New("no api key for faux")
		}
	})
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.EqualError(t, a.Prompt(context.Background(), user("hi")), "no api key for faux")

	assert.Empty(t, eventsOf[*protocol.AttemptStart](rec))
	assert.Empty(t, eventsOf[*protocol.AttemptEnd](rec))
	assert.Equal(t, []string{"error"}, cycleReasons(rec))
	assert.Equal(t, 0, p.Calls())
}

func TestCancelAfterStreamReturnsBeforeFirstReadOpensNoAttempt(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("never read"))
	var a *agent.Agent
	a = newAgent(t, p, m, func(c *agent.Config) {
		c.Stream = func(ctx context.Context, model providers.Model, req providers.TranscriptRequest, opts providers.StreamOptions) *providers.Stream {
			s := p.Stream(ctx, model, req, opts)
			a.Abort()
			return s
		}
	})
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.NoError(t, a.Prompt(context.Background(), user("hi")))

	assert.Empty(t, eventsOf[*protocol.AttemptStart](rec))
	assert.Empty(t, eventsOf[*protocol.AttemptEnd](rec))
	assert.Equal(t, []string{"aborted"}, cycleReasons(rec))
}

func TestStreamThatFailsOnFirstEventIsSettledAttempt(t *testing.T) {
	p, m := newFaux(t)
	failed := protocol.AssistantMessage{
		API: "faux", Provider: "faux", Model: "faux-1", Timestamp: 3,
		StopReason: protocol.StopError, ErrorMessage: ptr("provider refused"),
		Content: []protocol.AssistantBlock{},
	}
	p.Set(faux.Raw(protocol.ErrorEvent{Reason: protocol.StopError, Error: failed}))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.NoError(t, a.Prompt(context.Background(), user("hi")))

	starts := eventsOf[*protocol.AttemptStart](rec)
	ends := eventsOf[*protocol.AttemptEnd](rec)
	require.Len(t, starts, 1, "the stream was created, so the attempt started")
	require.Len(t, ends, 1)
	assert.Equal(t, starts[0].AttemptID, ends[0].AttemptID)
	assert.Equal(t, "failed", ends[0].Outcome)
	assert.Equal(t, []string{"error"}, cycleReasons(rec))
}

func TestAbortReasonCarriesCause(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(faux.Say(strings.Repeat("slow words ", 200)).Pace(40))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))
	a.Abort()

	select {
	case err := <-done:
		require.NoError(t, err, "an abort is not a run error")
	case <-time.After(5 * time.Second):
		t.Fatal("run did not settle after Abort")
	}
	ends := eventsOf[*protocol.CycleEnd](rec)
	require.Len(t, ends, 1)
	assert.Equal(t, "aborted", ends[0].Reason)
	assert.Equal(t, "user", ends[0].Cause)
	attemptEnds := eventsOf[*protocol.AttemptEnd](rec)
	require.Len(t, attemptEnds, 1)
	assert.Equal(t, "aborted", attemptEnds[0].Outcome)
}

func TestParentContextCancelNamesItsCause(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(faux.Say(strings.Repeat("slow words ", 200)).Pace(40))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	streaming := make(chan struct{})
	a.Subscribe(signalOn("message_update(text_delta)", streaming))
	ctx, cancel := context.WithCancelCause(context.Background())

	done := make(chan error, 1)
	go func() { done <- a.Prompt(ctx, user("go")) }()
	require.NoError(t, waitOrFail(context.Background(), streaming, "the stream"))
	cancel(errors.New("parent gave up: secret internal state"))

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("run did not settle after the parent cancel")
	}
	ends := eventsOf[*protocol.CycleEnd](rec)
	require.Len(t, ends, 1)
	assert.Equal(t, "aborted", ends[0].Reason)
	assert.Equal(t, "canceled", ends[0].Cause, "the wire carries a fixed word, not the cause text")
}

func TestDeadlineCancelNamesDeadlineCause(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(faux.Say(strings.Repeat("slow words ", 200)).Pace(40))
	a := newAgent(t, p, m, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	require.NoError(t, a.Prompt(ctx, user("go")))

	ends := eventsOf[*protocol.CycleEnd](rec)
	require.Len(t, ends, 1)
	assert.Equal(t, "aborted", ends[0].Reason)
	assert.Equal(t, "deadline", ends[0].Cause)
}

func ptr[T any](v T) *T { return &v }
