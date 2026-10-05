package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

var errHook = errors.New("hook failed")

func ptrTo[T any](v T) *T { return &v }

func fnPtr(f any) uintptr { return reflect.ValueOf(f).Pointer() }

func mustCompose(t *testing.T, hs ...Hooks) Hooks {
	t.Helper()
	h, err := Compose(hs...)
	require.NoError(t, err)
	return h
}

func userMsg(text string) protocol.Message {
	return protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: text}}}
}

func TestComposeIdentity(t *testing.T) {
	assert.Equal(t, Hooks{}, mustCompose(t))
	assert.Equal(t, Hooks{}, mustCompose(t, Hooks{}, Hooks{}))

	h := Hooks{
		TransformContext:    func(_ context.Context, m []protocol.Message) ([]protocol.Message, error) { return m, nil },
		ConvertToLLM:        func(m []protocol.Message) ([]protocol.Message, error) { return m, nil },
		GetAPIKey:           func(context.Context, string) (string, error) { return "k", nil },
		PrepareRequest:      func(context.Context, Request) (*RequestUpdate, error) { return nil, nil },
		FinishTurn:          func(context.Context, Turn) (TurnDecision, error) { return Proceed, nil },
		BeforeToolCall:      func(context.Context, ToolCallInfo) (*BeforeToolCallResult, error) { return nil, nil },
		AfterToolCall:       func(context.Context, ToolResultInfo) (*AfterToolCallResult, error) { return nil, nil },
		GetSteeringMessages: func(context.Context) ([]protocol.Message, error) { return nil, nil },
		GetFollowUpMessages: func(context.Context) ([]protocol.Message, error) { return nil, nil },
	}
	for name, got := range map[string]Hooks{"single": mustCompose(t, h), "with empty sets": mustCompose(t, Hooks{}, h, Hooks{})} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, fnPtr(h.TransformContext), fnPtr(got.TransformContext))
			assert.Equal(t, fnPtr(h.ConvertToLLM), fnPtr(got.ConvertToLLM))
			assert.Equal(t, fnPtr(h.GetAPIKey), fnPtr(got.GetAPIKey))
			assert.Equal(t, fnPtr(h.PrepareRequest), fnPtr(got.PrepareRequest))
			assert.Equal(t, fnPtr(h.FinishTurn), fnPtr(got.FinishTurn))
			assert.Equal(t, fnPtr(h.BeforeToolCall), fnPtr(got.BeforeToolCall))
			assert.Equal(t, fnPtr(h.AfterToolCall), fnPtr(got.AfterToolCall))
			assert.Equal(t, fnPtr(h.GetSteeringMessages), fnPtr(got.GetSteeringMessages))
			assert.Equal(t, fnPtr(h.GetFollowUpMessages), fnPtr(got.GetFollowUpMessages))
		})
	}
}

func TestComposeTransformContext(t *testing.T) {
	var order []string
	add := func(name string) func(context.Context, []protocol.Message) ([]protocol.Message, error) {
		return func(_ context.Context, m []protocol.Message) ([]protocol.Message, error) {
			order = append(order, name)
			return append(m, userMsg(name)), nil
		}
	}
	h := mustCompose(t, Hooks{TransformContext: add("a")}, Hooks{}, Hooks{TransformContext: add("b")})
	out, err := h.TransformContext(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, order)
	assert.Equal(t, []protocol.Message{userMsg("a"), userMsg("b")}, out)

	called := false
	h = mustCompose(t,
		Hooks{TransformContext: func(context.Context, []protocol.Message) ([]protocol.Message, error) { return nil, errHook }},
		Hooks{TransformContext: func(_ context.Context, m []protocol.Message) ([]protocol.Message, error) {
			called = true
			return m, nil
		}})
	_, err = h.TransformContext(context.Background(), nil)
	assert.ErrorIs(t, err, errHook)
	assert.False(t, called)
}

func TestComposeConvertToLLM(t *testing.T) {
	conv := func(m []protocol.Message) ([]protocol.Message, error) { return m, nil }
	h := mustCompose(t, Hooks{}, Hooks{ConvertToLLM: conv})
	assert.Equal(t, fnPtr(conv), fnPtr(h.ConvertToLLM))

	h, err := Compose(Hooks{ConvertToLLM: conv}, Hooks{ConvertToLLM: conv})
	require.ErrorIs(t, err, ErrMultipleConvertToLLM)
	assert.Equal(t, Hooks{}, h)
}

func TestComposeGetAPIKey(t *testing.T) {
	key := func(k string, calls *int) Hooks {
		return Hooks{GetAPIKey: func(_ context.Context, p string) (string, error) { *calls++; return k + p, nil }}
	}
	empty := func(calls *int) Hooks {
		return Hooks{GetAPIKey: func(context.Context, string) (string, error) { *calls++; return "", nil }}
	}
	var c1, c2, c3 int
	got, err := mustCompose(t, empty(&c1), key("k-", &c2), key("late-", &c3)).GetAPIKey(context.Background(), "p")
	require.NoError(t, err)
	assert.Equal(t, "k-p", got)
	assert.Equal(t, []int{1, 1, 0}, []int{c1, c2, c3})

	got, err = mustCompose(t, empty(&c1), empty(&c2)).GetAPIKey(context.Background(), "p")
	require.NoError(t, err)
	assert.Equal(t, "", got)

	c3 = 0
	failing := Hooks{GetAPIKey: func(context.Context, string) (string, error) { return "", errHook }}
	_, err = mustCompose(t, failing, key("x", &c3)).GetAPIKey(context.Background(), "p")
	assert.ErrorIs(t, err, errHook)
	assert.Zero(t, c3)
}

func TestComposePrepareRequest(t *testing.T) {
	ctxA := &AgentContext{Messages: []protocol.Message{userMsg("a")}}
	modelB := &providers.Model{ID: "b"}
	upd := func(u *RequestUpdate, seen *Request) Hooks {
		return Hooks{PrepareRequest: func(_ context.Context, r Request) (*RequestUpdate, error) {
			if seen != nil {
				*seen = r
			}
			return u, nil
		}}
	}
	run := func(hs ...Hooks) (*RequestUpdate, error) {
		return mustCompose(t, hs...).PrepareRequest(context.Background(), Request{ThinkingLevel: protocol.ThinkingOff})
	}

	var seen Request
	got, err := run(upd(&RequestUpdate{Context: ctxA, ThinkingLevel: "high"}, nil), upd(&RequestUpdate{Model: modelB}, &seen))
	require.NoError(t, err)
	assert.Equal(t, *ctxA, seen.Context, "second sees the earlier context")
	assert.Equal(t, protocol.ThinkingLevel("high"), seen.ThinkingLevel)
	assert.Equal(t, &RequestUpdate{Context: ctxA, Model: modelB, ThinkingLevel: "high"}, got)

	ctxC := &AgentContext{Messages: []protocol.Message{userMsg("c")}}
	got, err = run(upd(&RequestUpdate{Context: ctxA, ThinkingLevel: "high"}, nil), upd(&RequestUpdate{Context: ctxC, ThinkingLevel: "low"}, nil))
	require.NoError(t, err)
	assert.Equal(t, &RequestUpdate{Context: ctxC, ThinkingLevel: "low"}, got, "later non-nil wins")

	got, err = run(upd(&RequestUpdate{Context: ctxA}, nil), upd(nil, nil))
	require.NoError(t, err)
	assert.Equal(t, &RequestUpdate{Context: ctxA}, got, "nil keeps the earlier update")

	got, err = run(upd(nil, nil), upd(nil, nil))
	require.NoError(t, err)
	assert.Nil(t, got)

	called := false
	_, err = run(
		Hooks{PrepareRequest: func(context.Context, Request) (*RequestUpdate, error) { return nil, errHook }},
		Hooks{PrepareRequest: func(context.Context, Request) (*RequestUpdate, error) { called = true; return nil, nil }})
	assert.ErrorIs(t, err, errHook)
	assert.False(t, called)
}

func TestComposeFinishTurn(t *testing.T) {
	tests := []struct {
		name string
		a, b TurnDecision
		want TurnDecision
	}{
		{"proceed proceed", Proceed, Proceed, Proceed},
		{"proceed continue", Proceed, Continue, Continue},
		{"continue proceed", Continue, Proceed, Continue},
		{"continue end", Continue, End, End},
		{"end continue", End, Continue, End},
		{"end proceed", End, Proceed, End},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			fin := func(d TurnDecision) Hooks {
				return Hooks{FinishTurn: func(context.Context, Turn) (TurnDecision, error) { calls++; return d, nil }}
			}
			got, err := mustCompose(t, fin(tt.a), fin(tt.b)).FinishTurn(context.Background(), Turn{})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, 2, calls, "every function runs")
		})
	}

	called := false
	h := mustCompose(t,
		Hooks{FinishTurn: func(context.Context, Turn) (TurnDecision, error) { return End, errHook }},
		Hooks{FinishTurn: func(context.Context, Turn) (TurnDecision, error) { called = true; return Proceed, nil }})
	_, err := h.FinishTurn(context.Background(), Turn{})
	assert.ErrorIs(t, err, errHook)
	assert.False(t, called)
}

func TestComposeBeforeToolCall(t *testing.T) {
	before := func(r *BeforeToolCallResult, seen *json.RawMessage, calls *int) Hooks {
		return Hooks{BeforeToolCall: func(_ context.Context, c ToolCallInfo) (*BeforeToolCallResult, error) {
			if calls != nil {
				*calls++
			}
			if seen != nil {
				*seen = c.Args
			}
			return r, nil
		}}
	}
	run := func(hs ...Hooks) (*BeforeToolCallResult, error) {
		return mustCompose(t, hs...).BeforeToolCall(context.Background(), ToolCallInfo{Args: json.RawMessage(`{"orig":1}`)})
	}

	var calls int
	got, err := run(before(&BeforeToolCallResult{Block: true, Reason: "no", Terminate: true}, nil, nil), before(nil, nil, &calls))
	require.NoError(t, err)
	assert.Equal(t, &BeforeToolCallResult{Block: true, Reason: "no", Terminate: true}, got)
	assert.Zero(t, calls, "a block stops the chain")

	var seen json.RawMessage
	got, err = run(before(&BeforeToolCallResult{Args: json.RawMessage(`{"a":1}`)}, nil, nil), before(nil, &seen, nil))
	require.NoError(t, err)
	assert.JSONEq(t, `{"a":1}`, string(seen), "args of the first reach the second")
	assert.Equal(t, &BeforeToolCallResult{Args: json.RawMessage(`{"a":1}`)}, got)

	got, err = run(before(nil, nil, nil), before(&BeforeToolCallResult{}, nil, nil))
	require.NoError(t, err)
	assert.Nil(t, got)

	got, err = run(before(&BeforeToolCallResult{Args: json.RawMessage(`{"a":1}`)}, nil, nil), before(&BeforeToolCallResult{Block: true, Reason: "late"}, nil, nil))
	require.NoError(t, err)
	assert.Equal(t, &BeforeToolCallResult{Block: true, Reason: "late"}, got)

	calls = 0
	_, err = run(Hooks{BeforeToolCall: func(context.Context, ToolCallInfo) (*BeforeToolCallResult, error) { return nil, errHook }}, before(nil, nil, &calls))
	assert.ErrorIs(t, err, errHook)
	assert.Zero(t, calls)
}

func TestComposeAfterToolCall(t *testing.T) {
	text := func(s string) []protocol.UserBlock { return []protocol.UserBlock{protocol.Text{Text: s}} }
	after := func(r *AfterToolCallResult, seen *ToolResultInfo) Hooks {
		return Hooks{AfterToolCall: func(_ context.Context, c ToolResultInfo) (*AfterToolCallResult, error) {
			if seen != nil {
				*seen = c
			}
			return r, nil
		}}
	}
	run := func(hs ...Hooks) (*AfterToolCallResult, error) {
		return mustCompose(t, hs...).AfterToolCall(context.Background(), ToolResultInfo{
			Result: protocol.ToolExecutionResult{Content: text("orig"), StructuredContent: json.RawMessage(`{"s":0}`)},
		})
	}

	var seen ToolResultInfo
	got, err := run(
		after(&AfterToolCallResult{Content: text("one"), IsError: ptrTo(true)}, nil),
		after(&AfterToolCallResult{Details: json.RawMessage(`{"d":1}`)}, &seen))
	require.NoError(t, err)
	assert.Equal(t, text("one"), seen.Result.Content, "second sees the first override")
	assert.Nil(t, seen.Result.StructuredContent, "content alone dropped the structured content")
	assert.True(t, seen.IsError)
	assert.Equal(t, &AfterToolCallResult{Content: text("one"), IsError: ptrTo(true), Details: json.RawMessage(`{"d":1}`)}, got)

	got, err = run(
		after(&AfterToolCallResult{Content: text("one"), StructuredContent: json.RawMessage(`{"s":1}`)}, nil),
		after(&AfterToolCallResult{Content: text("two")}, nil))
	require.NoError(t, err)
	assert.Equal(t, text("two"), got.Content)
	assert.Nil(t, got.StructuredContent, "later content alone clears earlier structured content")

	got, err = run(
		after(&AfterToolCallResult{IsError: ptrTo(true), Terminate: ptrTo(true)}, nil),
		after(&AfterToolCallResult{IsError: ptrTo(false), Terminate: ptrTo(false)}, nil))
	require.NoError(t, err)
	assert.False(t, *got.IsError)
	assert.False(t, *got.Terminate)

	got, err = run(after(nil, nil), after(nil, nil))
	require.NoError(t, err)
	assert.Nil(t, got)

	called := false
	_, err = run(
		Hooks{AfterToolCall: func(context.Context, ToolResultInfo) (*AfterToolCallResult, error) { return nil, errHook }},
		Hooks{AfterToolCall: func(context.Context, ToolResultInfo) (*AfterToolCallResult, error) { called = true; return nil, nil }})
	assert.ErrorIs(t, err, errHook)
	assert.False(t, called)
}

func TestComposeQueues(t *testing.T) {
	src := func(msgs ...protocol.Message) func(context.Context) ([]protocol.Message, error) {
		return func(context.Context) ([]protocol.Message, error) { return msgs, nil }
	}
	fail := func(context.Context) ([]protocol.Message, error) { return nil, errHook }

	tests := map[string]struct {
		build func(f func(context.Context) ([]protocol.Message, error)) Hooks
		poll  func(Hooks) func(context.Context) ([]protocol.Message, error)
	}{
		"steering": {
			build: func(f func(context.Context) ([]protocol.Message, error)) Hooks { return Hooks{GetSteeringMessages: f} },
			poll:  func(h Hooks) func(context.Context) ([]protocol.Message, error) { return h.GetSteeringMessages },
		},
		"follow-up": {
			build: func(f func(context.Context) ([]protocol.Message, error)) Hooks { return Hooks{GetFollowUpMessages: f} },
			poll:  func(h Hooks) func(context.Context) ([]protocol.Message, error) { return h.GetFollowUpMessages },
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			h := mustCompose(t, tt.build(src(userMsg("a"))), tt.build(src()), tt.build(nil), tt.build(src(userMsg("b"), userMsg("c"))))
			got, err := tt.poll(h)(context.Background())
			require.NoError(t, err)
			assert.Equal(t, []protocol.Message{userMsg("a"), userMsg("b"), userMsg("c")}, got)

			got, err = tt.poll(mustCompose(t, tt.build(src()), tt.build(src())))(context.Background())
			require.NoError(t, err)
			assert.Empty(t, got)

			_, err = tt.poll(mustCompose(t, tt.build(fail), tt.build(src(userMsg("x")))))(context.Background())
			assert.ErrorIs(t, err, errHook)
		})
	}
}

func TestAfterToolCallResultApply(t *testing.T) {
	text := func(s string) []protocol.UserBlock { return []protocol.UserBlock{protocol.Text{Text: s}} }
	base := protocol.ToolExecutionResult{Content: text("orig"), StructuredContent: json.RawMessage(`{"s":0}`), Details: json.RawMessage(`{"d":0}`)}

	tests := []struct {
		name    string
		r       *AfterToolCallResult
		want    protocol.ToolExecutionResult
		wantErr bool
	}{
		{"nil receiver", nil, base, false},
		{"empty override", &AfterToolCallResult{}, base, false},
		{"empty non-nil content replaces", &AfterToolCallResult{Content: []protocol.UserBlock{}},
			protocol.ToolExecutionResult{Content: []protocol.UserBlock{}, Details: base.Details}, false},
		{"content drops structured content", &AfterToolCallResult{Content: text("new")},
			protocol.ToolExecutionResult{Content: text("new"), Details: base.Details}, false},
		{"content with structured content keeps both", &AfterToolCallResult{Content: text("new"), StructuredContent: json.RawMessage(`{"s":1}`)},
			protocol.ToolExecutionResult{Content: text("new"), StructuredContent: json.RawMessage(`{"s":1}`), Details: base.Details}, false},
		{"error flag", &AfterToolCallResult{IsError: ptrTo(true)}, base, true},
		{"terminate", &AfterToolCallResult{Terminate: ptrTo(true)},
			protocol.ToolExecutionResult{Content: base.Content, StructuredContent: base.StructuredContent, Details: base.Details, Terminate: ptrTo(true)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, isErr := tt.r.Apply(base, false)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantErr, isErr)
		})
	}
}
