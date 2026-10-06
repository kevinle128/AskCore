package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers/faux"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

const numSchema = `{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`

// scribble changes every '5' in b to '9' in place, the way a careless
// handler edits the bytes it was given.
func scribble(b []byte) {
	for i := range b {
		if b[i] == '5' {
			b[i] = '9'
		}
	}
}

// argsRecorder is a tool that stores the arguments of its calls.
type argsRecorder struct {
	mu   sync.Mutex
	args []string
}

func (r *argsRecorder) tool(name string) *funcTool {
	return &funcTool{name: name, schema: numSchema, run: func(_ context.Context, _ tools.Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.args = append(r.args, string(args))
		return textResult("ok"), nil
	}}
}

func (r *argsRecorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.args...)
}

// counter counts calls of a handler.
type counter struct {
	mu sync.Mutex
	n  int
}

func (c *counter) inc() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return c.n
}

func (c *counter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func TestBeforeToolCannotChangeArguments(t *testing.T) {
	t.Run("a handler that edits the bytes it received", func(t *testing.T) {
		rec := &argsRecorder{}
		hooks := pipeline.NewRegistry()
		hooks.OnBeforeTool(func(_ context.Context, info pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
			scribble(info.Args)
			scribble(info.Call.Arguments)
			return nil, nil
		})
		r := runTools(t, context.Background(), registry(t, rec.tool("num")), hooks, nil,
			faux.Reply(call("num", "c1", map[string]any{"n": "5"})), faux.Say("done"))

		require.Len(t, rec.seen(), 1)
		assert.JSONEq(t, `{"n":5}`, rec.seen()[0], "the tool runs with the validated arguments")
		var sent []protocol.ToolCall
		for _, m := range r.msgs {
			if a, ok := m.(protocol.AssistantMessage); ok {
				sent = append(sent, toolCallsOf(a)...)
			}
		}
		require.Len(t, sent, 1)
		assert.JSONEq(t, `{"n":"5"}`, string(sent[0].Arguments), "the logged call keeps what the model sent")
	})

	t.Run("a handler that passes other arguments to next", func(t *testing.T) {
		rec := &argsRecorder{}
		hooks := pipeline.NewRegistry()
		hooks.OnBeforeTool(func(ctx context.Context, info pipeline.ToolCallInfo, next nextBefore) (*pipeline.BeforeToolCallResult, error) {
			info.Args = json.RawMessage(`{"n":7}`)
			return next(ctx, info)
		})
		runTools(t, context.Background(), registry(t, rec.tool("num")), hooks, nil,
			faux.Reply(call("num", "c1", map[string]any{"n": 5})), faux.Say("done"))

		require.Len(t, rec.seen(), 1)
		assert.JSONEq(t, `{"n":5}`, rec.seen()[0])
	})

	t.Run("the decision type has no field for arguments", func(t *testing.T) {
		_, has := reflect.TypeOf(pipeline.BeforeToolCallResult{}).FieldByName("Args")
		assert.False(t, has)
	})
}

func toolCallsOf(m protocol.AssistantMessage) []protocol.ToolCall {
	var out []protocol.ToolCall
	for _, b := range m.Content {
		if c, ok := b.(protocol.ToolCall); ok {
			out = append(out, c)
		}
	}
	return out
}

func TestExecuteToolCannotChangeArgumentsOrTool(t *testing.T) {
	num := &argsRecorder{}
	other := &argsRecorder{}
	hooks := pipeline.NewRegistry()
	hooks.OnExecuteTool(func(ctx context.Context, in pipeline.ExecuteToolInput, next pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		scribble(in.Args)
		in.Args = json.RawMessage(`{"n":7}`)
		in.Call.Name = "other"
		return next(ctx, in)
	})
	r := runTools(t, context.Background(), registry(t, num.tool("num"), other.tool("other")), hooks, nil,
		faux.Reply(call("num", "c1", map[string]any{"n": 5})), faux.Say("done"))

	require.Len(t, num.seen(), 1)
	assert.JSONEq(t, `{"n":5}`, num.seen()[0])
	assert.Empty(t, other.seen(), "a handler cannot swap the tool")
	assert.False(t, toolResults(r.msgs)["c1"].IsError)
}

func TestToolStartEventShowsRawModelArguments(t *testing.T) {
	var executed, before, after json.RawMessage
	tool := &funcTool{name: "num", schema: numSchema, run: func(_ context.Context, tc tools.Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
		executed = bytes.Clone(args)
		tc.Update(textResult("half"))
		return textResult("ok"), nil
	}}
	hooks := pipeline.NewRegistry()
	hooks.OnBeforeTool(func(_ context.Context, info pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
		before = bytes.Clone(info.Args)
		return nil, nil
	})
	hooks.OnAfterTool(func(_ context.Context, info pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
		after = bytes.Clone(info.Args)
		return nil, nil
	})
	r := runTools(t, context.Background(), registry(t, tool), hooks, nil,
		faux.Reply(call("num", "c1", map[string]any{"n": "5"})), faux.Say("done"))

	assert.JSONEq(t, `{"n":5}`, string(before))
	assert.JSONEq(t, `{"n":5}`, string(executed))
	assert.JSONEq(t, `{"n":5}`, string(after))
	starts, updates := 0, 0
	for _, e := range r.rec.events {
		switch v := e.(type) {
		case *protocol.ToolExecutionStart:
			starts++
			assert.JSONEq(t, `{"n":"5"}`, string(v.Args), "the wire shows the raw model arguments, as Pi; the validated 5 would break JSON readers")
		case *protocol.ToolExecutionUpdate:
			updates++
			assert.JSONEq(t, `{"n":"5"}`, string(v.Args))
		}
	}
	assert.Equal(t, 1, starts)
	assert.Equal(t, 1, updates)
}

func TestToolStartEventShowsRawArgumentsWhenValidationFails(t *testing.T) {
	rec := &argsRecorder{}
	r := runTools(t, context.Background(), registry(t, rec.tool("num")), nil, nil,
		faux.Reply(call("num", "bad", map[string]any{"n": "five"}), call("ghost", "none", map[string]any{"n": "5"})), faux.Say("done"))

	starts := map[string]json.RawMessage{}
	for _, e := range r.rec.events {
		if v, ok := e.(*protocol.ToolExecutionStart); ok {
			starts[v.ToolCallID] = v.Args
		}
	}
	assert.JSONEq(t, `{"n":"five"}`, string(starts["bad"]), "a call that fails validation has no validated arguments")
	assert.JSONEq(t, `{"n":"5"}`, string(starts["none"]), "an unknown tool has no validated arguments")
	assert.Empty(t, rec.seen())
}

func TestToolArgumentsAreFrozenAfterValidation(t *testing.T) {
	var before, executed, after []byte
	tool := &funcTool{name: "num", schema: numSchema, run: func(_ context.Context, _ tools.Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
		executed = bytes.Clone(args)
		return textResult("ok"), nil
	}}
	hooks := pipeline.NewRegistry()
	hooks.OnBeforeTool(func(_ context.Context, info pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
		before = bytes.Clone(info.Args)
		return nil, nil
	})
	hooks.OnAfterTool(func(_ context.Context, info pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
		after = bytes.Clone(info.Args)
		return nil, nil
	})
	runTools(t, context.Background(), registry(t, tool), hooks, nil,
		faux.Reply(call("num", "c1", map[string]any{"n": "5"})), faux.Say("done"))

	require.NotEmpty(t, before)
	assert.Equal(t, before, executed, "Execute gets the exact bytes that BeforeTool saw")
	assert.Equal(t, before, after, "AfterTool gets the exact bytes that BeforeTool saw")
	_, has := reflect.TypeOf(pipeline.BeforeToolCallResult{}).FieldByName("Args")
	assert.False(t, has, "the decision has no field that replaces the arguments")
}

func TestAfterToolCallSeesToolError(t *testing.T) {
	tool := &funcTool{name: "boom", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		return protocol.ToolExecutionResult{}, errors.New("disk on fire")
	}}
	var sawError bool
	var sawText string
	hooks := pipeline.NewRegistry()
	hooks.OnAfterTool(func(_ context.Context, info pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
		sawError, sawText = info.IsError, resultText(info.Result.Content)
		return &pipeline.AfterToolCallResult{Content: []protocol.UserBlock{protocol.Text{Text: "recovered"}}, IsError: ptr(false)}, nil
	})
	r := runTools(t, context.Background(), registry(t, tool), hooks, nil,
		faux.Reply(call("boom", "c1", nil)), faux.Say("done"))

	assert.True(t, sawError, "AfterTool runs for a body that failed, with IsError true")
	assert.Equal(t, "disk on fire", sawText)
	res := toolResults(r.msgs)["c1"]
	assert.False(t, res.IsError, "the decision of AfterTool gives the final result")
	assert.Equal(t, "recovered", resultText(res.Content))
}

func TestBeforeToolCallCancelSkipsExecute(t *testing.T) {
	ran := &counter{}
	tool := &funcTool{name: "guarded", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		ran.inc()
		return textResult("ran"), nil
	}}
	hooks := pipeline.NewRegistry()
	hooks.OnBeforeTool(func(context.Context, pipeline.ToolCallInfo, nextBefore) (*pipeline.BeforeToolCallResult, error) {
		return &pipeline.BeforeToolCallResult{Cancel: true}, nil
	})
	r := runTools(t, context.Background(), registry(t, tool), hooks, nil,
		faux.Reply(call("guarded", "c1", nil)), faux.Say("done"))

	res := toolResults(r.msgs)["c1"]
	assert.True(t, res.IsError)
	assert.Contains(t, strings.ToLower(resultText(res.Content)), "aborted before dispatch")
	assert.NotEqual(t, "Operation aborted", resultText(res.Content), "a cancel decision has its own text, not the abort text")
	assert.Zero(t, ran.get(), "the body runs zero times")
	assert.Equal(t, 2, r.p.Calls(), "a cancel decision is a tool result, not the end of the run")
}

// failureCase builds the registry that makes one control point fail.
type failureCase struct {
	name  string
	setup func(reg *pipeline.Registry)
}

func TestCompleteStepHandlerErrorEndsRunWithError(t *testing.T) {
	boom := errors.New("finish failed")
	cases := []failureCase{
		{"error", func(reg *pipeline.Registry) {
			reg.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
				return pipeline.Proceed, boom
			})
		}},
		{"panic", func(reg *pipeline.Registry) {
			reg.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
				panic("finish exploded")
			})
		}},
	}
	runFailureCases(t, cases)
}

func TestRequestPointFailureEndsRunWithError(t *testing.T) {
	runFailureCases(t, []failureCase{
		{"PrepareRequest error", func(reg *pipeline.Registry) {
			reg.OnPrepareRequest(func(context.Context, pipeline.Request, pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
				return nil, errors.New("prepare failed")
			})
		}},
		{"PrepareRequest panic", func(reg *pipeline.Registry) {
			reg.OnPrepareRequest(func(context.Context, pipeline.Request, pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
				panic("prepare exploded")
			})
		}},
		{"ExecuteModel error", func(reg *pipeline.Registry) {
			reg.OnExecuteModel(func(ctx context.Context, in pipeline.ModelCall, next pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
				return nil, errors.New("execute failed")
			})
		}},
		{"ExecuteModel panic", func(reg *pipeline.Registry) {
			reg.OnExecuteModel(func(ctx context.Context, in pipeline.ModelCall, next pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
				panic("execute exploded")
			})
		}},
	})
}

// runFailureCases runs each case on an Agent and checks that the run ends
// with an error and that the cycle reason is error.
func runFailureCases(t *testing.T, cases []failureCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, m := newFaux(t)
			p.Set(faux.Say("one"), faux.Say("two"))
			failing := pipeline.NewRegistry()
			tc.setup(failing)
			a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = failing })
			rec := &recorder{}
			a.Subscribe(rec.emit)

			err := a.Prompt(context.Background(), user("go"))
			require.Error(t, err)
			assert.Equal(t, []string{"error"}, cycleReasons(rec))
			assert.Equal(t, "agent_settled", rec.eventLabels()[len(rec.eventLabels())-1])
			assert.Equal(t, agent.Idle, a.State().Status)
		})
	}
}

func TestStopTurnHandlerPanicEndsTurnWithError(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	calls := &counter{}
	hooks := pipeline.NewRegistry()
	hooks.OnStopTurn(func(context.Context, pipeline.StopInput) (pipeline.TurnDecision, error) {
		if calls.inc() == 1 {
			panic("broken continuation plugin")
		}
		return pipeline.Proceed, nil
	})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })
	rec := &recorder{}
	a.Subscribe(rec.emit)

	err := a.Prompt(context.Background(), user("first"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken continuation plugin")
	assert.Equal(t, []string{"error"}, cycleReasons(rec), "the panic ends the cycle with reason error")

	// The loop survives: the next Prompt runs normally.
	require.NoError(t, a.Prompt(context.Background(), user("second")))
	assert.Equal(t, []string{"error", "completed"}, cycleReasons(rec))
	assert.Equal(t, 2, p.Calls())
	assert.Equal(t, agent.Idle, a.State().Status)
}

func TestStopTurnCannotOverrideEnd(t *testing.T) {
	t.Run("End from CompleteStep stays End", func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Say("one"), faux.Say("unused"))
		var a *agent.Agent
		stops := &counter{}
		hooks := pipeline.NewRegistry()
		hooks.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
			return pipeline.End, nil
		})
		hooks.OnStopTurn(func(context.Context, pipeline.StopInput) (pipeline.TurnDecision, error) {
			stops.inc()
			mustSteer(t, a, "more")
			return pipeline.Continue, nil
		})
		a = newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })
		rec := &recorder{}
		a.Subscribe(rec.emit)
		err := a.Prompt(context.Background(), user("go"))
		require.NoError(t, err)
		assert.Equal(t, 1, p.Calls())
		assert.Zero(t, stops.get(), "an explicit End stops the run before StopTurn")
		assert.Equal(t, []string{"completed"}, cycleReasons(rec))
	})

	t.Run("End from StopTurn beats steering that another handler queued", func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Say("one"), faux.Say("unused"))
		var a *agent.Agent
		hooks := pipeline.NewRegistry()
		hooks.OnStopTurn(func(context.Context, pipeline.StopInput) (pipeline.TurnDecision, error) {
			mustSteer(t, a, "more")
			return pipeline.Proceed, nil
		})
		hooks.OnStopTurn(func(context.Context, pipeline.StopInput) (pipeline.TurnDecision, error) {
			return pipeline.End, nil
		})
		a = newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })
		rec := &recorder{}
		a.Subscribe(rec.emit)
		err := a.Prompt(context.Background(), user("go"))
		require.NoError(t, err)
		assert.Equal(t, 1, p.Calls())
		assert.Equal(t, []string{"more|"}, queueStates(rec), "End does not claim: the steering message stays queued")
	})
}

func TestEmptyContinuationStopsAtLimit(t *testing.T) {
	cont := func() *pipeline.Registry {
		hooks := pipeline.NewRegistry()
		hooks.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
			return pipeline.Continue, nil
		})
		return hooks
	}
	say := func(n int) []faux.Step {
		steps := make([]faux.Step, n)
		for i := range steps {
			steps[i] = faux.Say("again")
		}
		return steps
	}

	t.Run("four empty continuations send four requests", func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(say(10)...)
		rec := &recorder{}
		_, err := agent.Run(context.Background(), []protocol.Message{user("go")}, pipeline.AgentContext{},
			config(p, m, cont()), rec.emit)
		require.NoError(t, err)
		assert.Equal(t, 4, p.Calls(), "the first request and three continuations; the fourth continuation is not sent")
		assert.Equal(t, []string{"continuation-limit"}, cycleReasons(rec))
	})

	t.Run("a continuation that carries new input resets the count", func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(say(20)...)
		// The third turn ends with steering queued, so that Continue is not
		// empty and the count starts again.
		var a *agent.Agent
		turns := &counter{}
		hooks := pipeline.NewRegistry()
		hooks.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
			if turns.inc() == 3 {
				mustSteer(t, a, "new input")
			}
			return pipeline.Continue, nil
		})
		a = newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })
		rec := &recorder{}
		a.Subscribe(rec.emit)
		err := a.Prompt(context.Background(), user("go"))
		require.NoError(t, err)
		assert.Equal(t, 7, p.Calls(), "two empty continuations, one with input that resets the count, then four requests more")
		assert.Equal(t, []string{"continuation-limit"}, cycleReasons(rec))
	})
}

func TestStopTurnSteerContinuesAfterMaxTokens(t *testing.T) {
	t.Run("a handler that steers gives a second request", func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Say("cut off").Stop(protocol.StopLength), faux.Say("finished"))
		var a *agent.Agent
		stops := &counter{}
		hooks := pipeline.NewRegistry()
		hooks.OnStopTurn(func(context.Context, pipeline.StopInput) (pipeline.TurnDecision, error) {
			if stops.inc() == 1 {
				mustSteer(t, a, "please go on")
			}
			return pipeline.Proceed, nil
		})
		a = newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })
		rec := &recorder{}
		a.Subscribe(rec.emit)
		err := a.Prompt(context.Background(), user("go"))
		require.NoError(t, err)
		assert.Equal(t, 2, p.Calls(), "StopTurn runs after a length stop, and its steering starts the next turn")
		assert.Equal(t, 2, stops.get(), "StopTurn runs again when the cycle stops for good")
		assert.Equal(t, []string{"max-tokens"}, cycleReasons(rec), "the max-tokens reason stays")
	})

	t.Run("without steering the cycle ends with max-tokens", func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Say("cut off").Stop(protocol.StopLength), faux.Say("unused"))
		stops := &counter{}
		hooks := pipeline.NewRegistry()
		hooks.OnStopTurn(func(context.Context, pipeline.StopInput) (pipeline.TurnDecision, error) {
			stops.inc()
			return pipeline.Proceed, nil
		})
		rec := &recorder{}
		_, err := agent.Run(context.Background(), []protocol.Message{user("go")}, pipeline.AgentContext{}, config(p, m, hooks), rec.emit)
		require.NoError(t, err)
		assert.Equal(t, 1, p.Calls())
		assert.Equal(t, 1, stops.get())
		assert.Equal(t, []string{"max-tokens"}, cycleReasons(rec))
	})

	t.Run("a Continue decision of StopTurn alone does not continue after a length stop", func(t *testing.T) {
		p, m := newFaux(t)
		p.Set(faux.Say("cut off").Stop(protocol.StopLength), faux.Say("unused"))
		hooks := pipeline.NewRegistry()
		hooks.OnStopTurn(func(context.Context, pipeline.StopInput) (pipeline.TurnDecision, error) {
			return pipeline.Continue, nil
		})
		rec := &recorder{}
		_, err := agent.Run(context.Background(), []protocol.Message{user("go")}, pipeline.AgentContext{}, config(p, m, hooks), rec.emit)
		require.NoError(t, err)
		assert.Equal(t, 1, p.Calls())
		assert.Equal(t, []string{"max-tokens"}, cycleReasons(rec))
	})
}

func TestAbortInStopHookEndsCycleAborted(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	var a *agent.Agent
	hooks := pipeline.NewRegistry()
	hooks.OnStopTurn(func(context.Context, pipeline.StopInput) (pipeline.TurnDecision, error) {
		a.Abort()
		return pipeline.Proceed, nil
	})
	hooks.OnStopTurn(func(context.Context, pipeline.StopInput) (pipeline.TurnDecision, error) {
		return pipeline.Continue, nil
	})
	a = newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.NoError(t, a.Prompt(context.Background(), user("go")))

	assert.Equal(t, 1, p.Calls(), "the turn that the Continue would start does not run")
	assert.Len(t, eventsOf[*protocol.TurnEnd](rec), 1)
	ends := eventsOf[*protocol.CycleEnd](rec)
	require.Len(t, ends, 1)
	assert.Equal(t, "aborted", ends[0].Reason)
	assert.Equal(t, "user", ends[0].Cause)
}

func TestAbortReleasesHookWaitingOnContext(t *testing.T) {
	// Each case blocks one control point on ctx.Done(). The test aborts the
	// Agent once the handler waits, and the run must settle with an aborted
	// cycle whose cause is the user.
	block := func(entered chan<- struct{}) func(ctx context.Context) error {
		var once sync.Once
		return func(ctx context.Context) error {
			once.Do(func() { close(entered) })
			<-ctx.Done()
			return ctx.Err()
		}
	}
	cases := []struct {
		name  string
		setup func(reg *pipeline.Registry, wait func(context.Context) error)
	}{
		{"AdmitStep", func(reg *pipeline.Registry, wait func(context.Context) error) {
			reg.OnAdmitStep(func(ctx context.Context, _ pipeline.AdmitInput, _ pipeline.Next[pipeline.AdmitInput, pipeline.AdmitDecision]) (pipeline.AdmitDecision, error) {
				return pipeline.AdmitDecision{}, wait(ctx)
			})
		}},
		{"PrepareRequest", func(reg *pipeline.Registry, wait func(context.Context) error) {
			reg.OnPrepareRequest(func(ctx context.Context, _ pipeline.Request, _ pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
				return nil, wait(ctx)
			})
		}},
		{"ExecuteModel", func(reg *pipeline.Registry, wait func(context.Context) error) {
			reg.OnExecuteModel(func(ctx context.Context, _ pipeline.ModelCall, _ pipeline.Next[pipeline.ModelCall, *pipeline.ModelOutcome]) (*pipeline.ModelOutcome, error) {
				return nil, wait(ctx)
			})
		}},
		{"BeforeTool", func(reg *pipeline.Registry, wait func(context.Context) error) {
			reg.OnBeforeTool(func(ctx context.Context, _ pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
				return nil, wait(ctx)
			})
		}},
		{"ExecuteTool", func(reg *pipeline.Registry, wait func(context.Context) error) {
			reg.OnExecuteTool(func(ctx context.Context, _ pipeline.ExecuteToolInput, _ pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
				return protocol.ToolExecutionResult{}, wait(ctx)
			})
		}},
		{"AfterTool", func(reg *pipeline.Registry, wait func(context.Context) error) {
			reg.OnAfterTool(func(ctx context.Context, _ pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
				return nil, wait(ctx)
			})
		}},
		{"CompleteStep", func(reg *pipeline.Registry, wait func(context.Context) error) {
			reg.OnCompleteStep(func(ctx context.Context, _ pipeline.Turn) (pipeline.TurnDecision, error) {
				return pipeline.Proceed, wait(ctx)
			})
		}},
		{"StopTurn", func(reg *pipeline.Registry, wait func(context.Context) error) {
			reg.OnStopTurn(func(ctx context.Context, _ pipeline.StopInput) (pipeline.TurnDecision, error) {
				return pipeline.Proceed, wait(ctx)
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, m := newFaux(t)
			p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"}, faux.ID("c1"))), faux.Say("done"))
			entered := make(chan struct{})
			hooks := pipeline.NewRegistry()
			tc.setup(hooks, block(entered))
			a := newAgent(t, p, m, func(c *agent.Config) { c.Pipeline = hooks })
			rec := &recorder{}
			a.Subscribe(rec.emit)

			done := make(chan error, 1)
			go func() { done <- a.Prompt(context.Background(), user("go")) }()
			require.NoError(t, waitOrFail(context.Background(), entered, "the handler to wait"))
			a.Abort()

			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("run did not settle after Abort")
			}
			ends := eventsOf[*protocol.CycleEnd](rec)
			require.NotEmpty(t, ends)
			last := ends[len(ends)-1]
			assert.Equal(t, "aborted", last.Reason)
			assert.Equal(t, "user", last.Cause)
			assert.Equal(t, "agent_settled", rec.eventLabels()[len(rec.eventLabels())-1])
		})
	}
}

func TestAgentScopedHandlerDoesNotRunForSiblingAgent(t *testing.T) {
	shared := pipeline.NewRegistry()
	sharedRuns := &counter{}
	shared.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
		sharedRuns.inc()
		return pipeline.Proceed, nil
	})
	build := func(name string, runs *counter) *agent.Agent {
		p, m := newFaux(t)
		p.Set(faux.Say(name))
		own := pipeline.NewRegistry()
		own.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
			runs.inc()
			return pipeline.Proceed, nil
		})
		return newAgent(t, p, m, func(c *agent.Config) {
			c.Application = shared
			c.Pipeline = own
		})
	}
	aRuns, bRuns := &counter{}, &counter{}
	a, b := build("a", aRuns), build("b", bRuns)

	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Equal(t, 1, aRuns.get())
	assert.Zero(t, bRuns.get(), "a handler of one Agent never runs for a sibling")
	assert.Equal(t, 1, sharedRuns.get())

	require.NoError(t, b.Prompt(context.Background(), user("go")))
	assert.Equal(t, 1, aRuns.get())
	assert.Equal(t, 1, bRuns.get())
	assert.Equal(t, 2, sharedRuns.get(), "an application handler runs for every Agent")
}

func TestApplicationHandlersRunBeforeAgentHandlers(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("hi"))
	var order []string
	mark := func(name string) pipeline.CompleteStepHandler {
		return func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
			order = append(order, name)
			return pipeline.Proceed, nil
		}
	}
	app, own := pipeline.NewRegistry(), pipeline.NewRegistry()
	app.OnCompleteStep(mark("application"))
	own.OnCompleteStep(mark("agent"))
	a := newAgent(t, p, m, func(c *agent.Config) { c.Application, c.Pipeline = app, own })

	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Equal(t, []string{"application", "agent"}, order)
}

func TestDisposedApplicationOwnerDoesNotRunInNextRun(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	runs := &counter{}
	app := pipeline.NewRegistry()
	owner := pipeline.NewOwner()
	app.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
		runs.inc()
		return pipeline.Proceed, nil
	}, pipeline.WithOwner(owner))
	a := newAgent(t, p, m, func(c *agent.Config) { c.Application = app })

	require.NoError(t, a.Prompt(context.Background(), user("one")))
	owner.Dispose()
	require.NoError(t, a.Prompt(context.Background(), user("two")))
	assert.Equal(t, 1, runs.get())
}

// scribbleCalls edits, in place, the argument bytes of every tool call that a
// handler can reach through an assistant message or the context.
func scribbleCalls(assistant protocol.AssistantMessage, msgs []protocol.Message) {
	for _, c := range toolCallsOf(assistant) {
		scribble(c.Arguments)
	}
	for _, m := range msgs {
		if a, ok := m.(protocol.AssistantMessage); ok {
			for _, c := range toolCallsOf(a) {
				scribble(c.Arguments)
			}
		}
	}
}

func TestHandlersCannotEditArgumentsThroughSharedViews(t *testing.T) {
	for _, point := range []string{"BeforeTool", "AfterTool"} {
		for _, mode := range []string{"parallel", "sequential"} {
			t.Run(point+" "+mode, func(t *testing.T) {
				rec := &argsRecorder{}
				var tool tools.Tool = rec.tool("num")
				if mode == "sequential" {
					tool = exclusiveTool{rec.tool("num")}
				}
				hooks := pipeline.NewRegistry()
				switch point {
				case "BeforeTool":
					hooks.OnBeforeTool(func(_ context.Context, info pipeline.ToolCallInfo, _ nextBefore) (*pipeline.BeforeToolCallResult, error) {
						scribbleCalls(info.AssistantMessage, info.Context.Messages)
						return nil, nil
					})
				case "AfterTool":
					hooks.OnAfterTool(func(_ context.Context, info pipeline.ToolResultInfo, _ nextAfter) (*pipeline.AfterToolCallResult, error) {
						scribbleCalls(info.AssistantMessage, info.Context.Messages)
						return nil, nil
					})
				}
				r := runTools(t, context.Background(), registry(t, tool), hooks, nil,
					faux.Reply(call("num", "c1", map[string]any{"n": 5}), call("num", "c2", map[string]any{"n": 5})), faux.Say("done"))

				seen := rec.seen()
				require.Len(t, seen, 2)
				for _, a := range seen {
					assert.JSONEq(t, `{"n":5}`, a, "no body sees edited bytes")
				}
				var logged []protocol.ToolCall
				for _, m := range r.msgs {
					if a, ok := m.(protocol.AssistantMessage); ok {
						logged = append(logged, toolCallsOf(a)...)
					}
				}
				require.Len(t, logged, 2)
				for _, c := range logged {
					assert.JSONEq(t, `{"n":5}`, string(c.Arguments), "the logged assistant message keeps what the model sent")
				}
			})
		}
	}
}

func TestToolTurnIsNotAnEmptyContinuationEvenWhenEveryResultTerminates(t *testing.T) {
	p, m := newFaux(t)
	steps := make([]faux.Step, 0, 16)
	for i := range 6 {
		steps = append(steps, faux.Reply(call("stop", fmt.Sprint("c", i), nil)))
	}
	for range 6 {
		steps = append(steps, faux.Say("again"))
	}
	p.Set(steps...)
	body := &counter{}
	stopTool := &funcTool{name: "stop", run: func(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
		body.inc()
		res := textResult("done")
		res.Terminate = ptr(true)
		return res, nil
	}}
	hooks := pipeline.NewRegistry()
	hooks.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
		return pipeline.Continue, nil
	})
	cfg := config(p, m, hooks)
	rec := &recorder{}
	_, err := agent.Run(context.Background(), []protocol.Message{user("go")}, pipeline.AgentContext{Tools: registry(t, stopTool)}, cfg, rec.emit)
	require.NoError(t, err)

	assert.Equal(t, 6, body.get(), "six turns ran a tool body")
	assert.Equal(t, 10, p.Calls(), "the tool turns do not count: after them the bound allows three empty continuations")
	assert.Equal(t, []string{"continuation-limit"}, cycleReasons(rec))
}

func TestExecuteToolCannotChangeWorkingDirectory(t *testing.T) {
	var gotCwd string
	var updates int
	tool := &funcTool{name: "num", schema: numSchema, run: func(_ context.Context, tc tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		gotCwd = tc.Cwd
		tc.Update(textResult("half"))
		return textResult("ok"), nil
	}}
	hooks := pipeline.NewRegistry()
	hooks.OnExecuteTool(func(ctx context.Context, in pipeline.ExecuteToolInput, next pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		in.Context.Cwd = "/elsewhere"
		send := in.Context.Update
		in.Context.Update = func(r protocol.ToolExecutionResult) { updates++; send(r) }
		return next(ctx, in)
	})
	p, m := newFaux(t)
	p.Set(faux.Reply(call("num", "c1", map[string]any{"n": 5})), faux.Say("done"))
	cfg := config(p, m, hooks)
	cfg.Cwd = "/work"
	rec := &recorder{}
	_, err := agent.Run(context.Background(), []protocol.Message{user("go")}, pipeline.AgentContext{Tools: registry(t, tool)}, cfg, rec.emit)
	require.NoError(t, err)

	assert.Equal(t, "/work", gotCwd, "the body runs in the directory of the run")
	assert.Equal(t, 1, updates, "a handler may wrap Update")
}
