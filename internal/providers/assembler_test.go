package providers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func runBody(t *testing.T, body func(a *Assembler)) (protocol.AssistantMessage, []StreamItem, error) {
	t.Helper()
	s := newTestStream(context.Background(), 256, body)
	items := drain(s)
	msg, err := waitResult(t, s)
	return msg, items, err
}

func toolAt(t *testing.T, msg protocol.AssistantMessage, i int) protocol.ToolCall {
	t.Helper()
	require.Greater(t, len(msg.Content), i)
	call, ok := msg.Content[i].(protocol.ToolCall)
	require.True(t, ok, "block %d is %T", i, msg.Content[i])
	return call
}

func TestAssemblerExitSequence(t *testing.T) {
	msg, items, err := runBody(t, func(a *Assembler) {
		a.Start()
		th := a.ThinkingStart("", nil, nil)
		a.ThinkingDelta(th, "go")
		a.ThinkingEnd(th, "go", nil, nil)
		tx := a.TextStart("")
		a.TextDelta(tx, "ok")
		a.TextEnd(tx, "ok", nil)
		for _, id := range []string{"a", "b"} {
			i := a.ToolStart(id, "echo", nil, nil, nil)
			a.ToolDelta(i, "{}")
			a.ToolEnd(i, nil)
		}
		a.Done(protocol.StopToolUse)
	})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"start",
		"thinking_start", "thinking_delta", "thinking_end",
		"text_start", "text_delta", "text_end",
		"toolcall_start", "toolcall_delta", "toolcall_end",
		"toolcall_start", "toolcall_delta", "toolcall_end",
		"done",
	}, types(items))
	assert.Equal(t, protocol.StopToolUse, msg.StopReason)
	require.Len(t, msg.Content, 4)
	assert.Equal(t, protocol.Thinking{Thinking: "go"}, msg.Content[0])
	assert.Equal(t, protocol.Text{Text: "ok"}, msg.Content[1])
	assert.Equal(t, "a", toolAt(t, msg, 2).ID)
	assert.Equal(t, "b", toolAt(t, msg, 3).ID)
	assert.JSONEq(t, `{}`, string(toolAt(t, msg, 3).Arguments))
	assert.Equal(t, 2, items[8].Event.(protocol.ToolCallDeltaEvent).ContentIndex, "tool deltas use their content index")
}

func TestAssemblerItemsRebuildTheResult(t *testing.T) {
	msg, items, err := runBody(t, func(a *Assembler) {
		a.Start()
		i := a.ToolStart("a", "echo", nil, sptr("sig"), sptr("ns"))
		a.ToolDelta(i, `{"x":`)
		a.ToolDelta(i, `1}`)
		a.ToolEnd(i, nil)
		a.Done(protocol.StopToolUse)
	})
	require.NoError(t, err)
	b := protocol.NewBuilder()
	for _, it := range items {
		require.NoError(t, b.Apply(it.Event))
	}
	got, ok := b.Result()
	require.True(t, ok)
	assert.Equal(t, msg, got)
	call := toolAt(t, msg, 0)
	assert.JSONEq(t, `{"x":1}`, string(call.Arguments))
	assert.Equal(t, "sig", *call.ThoughtSignature)
	assert.Equal(t, "ns", *call.Namespace)
}

func TestAssemblerInitialContentSignaturesAndRedaction(t *testing.T) {
	msg, _, err := runBody(t, func(a *Assembler) {
		a.Start()
		tx := a.TextStart("seed")
		a.TextEnd(tx, "seed text", sptr(""))
		th := a.ThinkingStart("", sptr("enc"), bptr(true))
		a.ThinkingEnd(th, "", sptr("enc"), bptr(true))
		empty := a.TextStart("")
		a.TextEnd(empty, "", nil)
		a.Done(protocol.StopStop)
	})
	require.NoError(t, err)
	assert.Equal(t, protocol.Text{Text: "seed text", TextSignature: sptr("")}, msg.Content[0])
	assert.Equal(t, protocol.Thinking{ThinkingSignature: sptr("enc"), Redacted: bptr(true)}, msg.Content[1])
	assert.Equal(t, protocol.Text{}, msg.Content[2])
}

func TestAssemblerAuthoritativeFinalArgumentsWithoutDeltas(t *testing.T) {
	final := protocol.ToolCall{ID: "final-id", Name: "final-name", Arguments: json.RawMessage(`{ "b": 2,  "a": [1] }`), Namespace: sptr("ns2")}
	msg, items, err := runBody(t, func(a *Assembler) {
		a.Start()
		i := a.ToolStart("start-id", "start-name", json.RawMessage(`{"old":true}`), sptr("sig"), nil)
		a.ToolEnd(i, &final)
		a.Done(protocol.StopToolUse)
	})
	require.NoError(t, err)
	call := toolAt(t, msg, 0)
	assert.Equal(t, `{ "b": 2,  "a": [1] }`, string(call.Arguments), "a supplied final object is never parsed again")
	assert.Equal(t, "final-id", call.ID, "authoritative metadata replaces the start values")
	assert.Equal(t, "final-name", call.Name)
	assert.Nil(t, call.ThoughtSignature, "the end replaces the whole block, so the start signature is gone")
	assert.Equal(t, "ns2", *call.Namespace)
	end := items[2].Event.(protocol.ToolCallEndEvent)
	assert.Equal(t, "final-id", end.ToolCall.ID)
}

func TestAssemblerFinalWithEmptyFieldsFallsBackToStart(t *testing.T) {
	msg, _, err := runBody(t, func(a *Assembler) {
		a.Start()
		i := a.ToolStart("start-id", "start-name", nil, nil, nil)
		a.ToolEnd(i, &protocol.ToolCall{})
		a.Done(protocol.StopToolUse)
	})
	require.NoError(t, err)
	call := toolAt(t, msg, 0)
	assert.Equal(t, "start-id", call.ID)
	assert.Equal(t, "start-name", call.Name)
	assert.JSONEq(t, `{}`, string(call.Arguments))
}

func TestAssemblerFinalWithNonObjectArgumentsFailsStream(t *testing.T) {
	msg, items, err := runBody(t, func(a *Assembler) {
		a.Start()
		i := a.ToolStart("a", "echo", nil, nil, nil)
		a.ToolEnd(i, &protocol.ToolCall{Arguments: json.RawMessage(`[1]`)})
		a.Done(protocol.StopToolUse)
	})
	require.Error(t, err)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	assert.Equal(t, "error", items[len(items)-1].Event.EventType())
}

func TestAssemblerRawToolJSONIsParsedOnceAtEnd(t *testing.T) {
	cases := []struct {
		name   string
		deltas []string
		want   string
	}{
		{"valid object keeps its bytes as compact JSON", []string{`{"b": 1,`, ` "a": 2}`}, `{"b":1,"a":2}`},
		{"raw tab in an open string is repaired", []string{"{\"a\":\"x\ty"}, `{"a":"x\ty"}`},
		{"truncated object is completed", []string{`{"a":"b`}, `{"a":"b"}`},
		{"html characters are not escaped", []string{`{"a":"<b>`}, `{"a":"<b>"}`},
		{"not an object becomes empty", []string{`[1,2`}, `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, _, err := runBody(t, func(a *Assembler) {
				a.Start()
				i := a.ToolStart("a", "echo", nil, nil, nil)
				for _, d := range tc.deltas {
					a.ToolDelta(i, d)
				}
				a.ToolEnd(i, nil)
				a.Done(protocol.StopToolUse)
			})
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(toolAt(t, msg, 0).Arguments))
		})
	}
}

func TestAssemblerOpenToolSalvage(t *testing.T) {
	cases := []struct {
		name  string
		start json.RawMessage
		raw   string
		want  string
	}{
		{"broken json is repaired", nil, "{\"path\":\"a\tb", `{"path":"a\tb"}`},
		{"empty buffer falls back to start arguments", json.RawMessage(`{"seed":1}`), "", `{"seed":1}`},
		{"empty buffer and no start arguments gives empty object", nil, "", `{}`},
		{"blank buffer falls back to start arguments", json.RawMessage(`{"seed":1}`), "  \n", `{"seed":1}`},
		{"the delta buffer wins over the start arguments", json.RawMessage(`{"seed":1}`), `{"k":2`, `{"k":2}`},
	}
	finishes := map[string]func(a *Assembler){
		"fail":   func(a *Assembler) { a.Fail(protocol.StopError, "network", nil) },
		"length": func(a *Assembler) { a.Done(protocol.StopLength) },
		"close":  func(a *Assembler) {},
	}
	for _, tc := range cases {
		for name, finish := range finishes {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				msg, items, _ := runBody(t, func(a *Assembler) {
					a.Start()
					done := a.TextStart("")
					a.TextEnd(done, "kept", nil)
					i := a.ToolStart("", "write", tc.start, nil, nil)
					if tc.raw != "" {
						a.ToolDelta(i, tc.raw)
					}
					finish(a)
				})
				require.Len(t, msg.Content, 2)
				assert.Equal(t, protocol.Text{Text: "kept"}, msg.Content[0], "completed blocks are kept")
				call := toolAt(t, msg, 1)
				assert.Equal(t, tc.want, string(call.Arguments))
				assert.Equal(t, "write", call.Name)
				assert.Equal(t, "call_1", call.ID, "an open call without an id gets a generated one")
				for _, it := range items {
					_, isEnd := it.Event.(protocol.ToolCallEndEvent)
					assert.False(t, isEnd, "salvage makes no block end event")
				}
			})
		}
	}
}

func TestAssemblerLengthKeepsOpenTextAndSuccessReason(t *testing.T) {
	msg, items, err := runBody(t, func(a *Assembler) {
		a.Start()
		i := a.TextStart("")
		a.TextDelta(i, "cut off")
		a.Done(protocol.StopLength)
	})
	require.NoError(t, err)
	assert.Equal(t, protocol.StopLength, msg.StopReason)
	assert.Equal(t, protocol.Text{Text: "cut off"}, msg.Content[0])
	assert.Equal(t, "done", items[len(items)-1].Event.EventType())
}

func TestAssemblerDoneWithOpenBlockIsProducerBug(t *testing.T) {
	msg, items, err := runBody(t, func(a *Assembler) {
		a.Start()
		i := a.ToolStart("a", "echo", nil, nil, nil)
		a.ToolDelta(i, `{"x":`)
		a.Done(protocol.StopToolUse)
	})
	require.Error(t, err)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	require.NotNil(t, msg.ErrorMessage)
	assert.Contains(t, *msg.ErrorMessage, "unfinished content block 0")
	assert.JSONEq(t, `{}`, string(toolAt(t, msg, 0).Arguments), "the partial call is kept, not run")
	assert.Equal(t, "error", items[len(items)-1].Event.EventType())
}

func TestAssemblerFinalRecordHasNoScratchFields(t *testing.T) {
	msg, items, _ := runBody(t, func(a *Assembler) {
		a.Start()
		a.ToolDelta(a.ToolStart("a", "echo", nil, nil, nil), `{"a":`)
		a.TextDelta(a.TextStart(""), "partial")
		a.Fail(protocol.StopError, "boom", nil)
	})
	raw, err := protocol.MarshalMessage(msg)
	require.NoError(t, err)
	var generic map[string]any
	require.NoError(t, json.Unmarshal(raw, &generic))
	blocks := generic["content"].([]any)
	require.Len(t, blocks, 2)
	tool := blocks[0].(map[string]any)
	assert.ElementsMatch(t, []string{"type", "id", "name", "arguments"}, keys(tool))
	text := blocks[1].(map[string]any)
	assert.ElementsMatch(t, []string{"type", "text"}, keys(text))
	assert.ElementsMatch(t, []string{"role", "content", "api", "provider", "model", "usage", "stopReason", "errorMessage", "timestamp"}, keys(generic))
	assert.Equal(t, "error", items[len(items)-1].Event.EventType())
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestAssemblerUsageReachesItemsAndResult(t *testing.T) {
	u1 := protocol.Usage{Input: 10, CacheWrite1h: i64(4), TotalTokens: 10}
	u2 := protocol.Usage{Input: 10, Output: 5, Reasoning: i64(2), TotalTokens: 15}
	msg, items, err := runBody(t, func(a *Assembler) {
		a.Start()
		a.SetUsage(u1)
		i := a.TextStart("")
		a.TextDelta(i, "a")
		a.SetUsage(u2)
		a.TextDelta(i, "b")
		a.TextEnd(i, "ab", nil)
		a.Done(protocol.StopStop)
	})
	require.NoError(t, err)
	require.Len(t, items, 6)
	assert.Equal(t, protocol.Usage{}, items[0].Usage, "start carries the seed usage")
	assert.Equal(t, u1, items[1].Usage)
	assert.Equal(t, u1, items[2].Usage)
	assert.Equal(t, u2, items[3].Usage)
	assert.Equal(t, u2, items[5].Usage, "the terminal item carries the latest usage")
	assert.Equal(t, u2, msg.Usage)
	assert.Equal(t, u2, items[5].Event.(protocol.DoneEvent).Message.Usage)
}

func TestAssemblerUsageIsCopied(t *testing.T) {
	u := protocol.Usage{Input: 1, Reasoning: i64(2)}
	var itemsSeen []StreamItem
	s := newTestStream(context.Background(), 16, func(a *Assembler) {
		a.Start()
		a.SetUsage(u)
		*u.Reasoning = 99 // the producer changes its own copy
		a.TextStart("")
		a.Done(protocol.StopLength)
	})
	itemsSeen = drain(s)
	assert.EqualValues(t, 2, *itemsSeen[1].Usage.Reasoning)
	*itemsSeen[1].Usage.Reasoning = 7 // the consumer changes its own copy
	assert.EqualValues(t, 2, *itemsSeen[2].Usage.Reasoning)
	msg, err := waitResult(t, s)
	require.NoError(t, err)
	assert.EqualValues(t, 2, *msg.Usage.Reasoning)
}

func TestAssemblerLateMetadataReachesResult(t *testing.T) {
	diag := protocol.Diagnostic{Type: "retry", Timestamp: 5}
	seed := testSeed()
	seed.ResponseID = sptr("from-seed")
	s := NewStream(context.Background(), 16, seed, func(a *Assembler) {
		a.Start()
		i := a.TextStart("")
		a.TextEnd(i, "x", nil)
		level := protocol.ThinkingHigh
		a.SetMetadata(Metadata{
			ResponseModel: sptr("real-model"), ThinkingLevel: &level, ProviderThinkingLevel: sptr("hi"),
			Diagnostics: []protocol.Diagnostic{diag}, RawStopReason: sptr("end_turn"), EndTurn: bptr(true),
		})
		a.SetMetadata(Metadata{Diagnostics: []protocol.Diagnostic{diag}}) // appended
		a.Done(protocol.StopStop)
	})
	items := drain(s)
	msg, err := waitResult(t, s)
	require.NoError(t, err)
	assert.Equal(t, "from-seed", *msg.ResponseID, "an absent field keeps the seed value")
	assert.Equal(t, "real-model", *msg.ResponseModel)
	assert.Equal(t, protocol.ThinkingHigh, *msg.ThinkingLevel)
	assert.Equal(t, "hi", *msg.ProviderThinkingLevel)
	assert.Equal(t, "end_turn", *msg.RawStopReason)
	assert.True(t, *msg.EndTurn)
	assert.Equal(t, []protocol.Diagnostic{diag, diag}, msg.Diagnostics)
	done := items[len(items)-1].Event.(protocol.DoneEvent)
	assert.Equal(t, msg, done.Message)
}

func TestAssemblerMetadataSurvivesFailure(t *testing.T) {
	msg, _, err := runBody(t, func(a *Assembler) {
		a.SetMetadata(Metadata{ResponseID: sptr("r9")})
		a.SetUsage(protocol.Usage{Input: 3, TotalTokens: 3})
		a.Fail(protocol.StopError, "setup failed", nil)
	})
	require.EqualError(t, err, "setup failed")
	assert.Equal(t, "r9", *msg.ResponseID)
	assert.Equal(t, int64(3), msg.Usage.Input)
	assert.Equal(t, "test-model", msg.Model, "a failure before start builds the message from the seed")
}

func TestAssemblerFailBeforeStartMakesOnlyAnErrorEvent(t *testing.T) {
	cause := assert.AnError
	msg, items, err := runBody(t, func(a *Assembler) {
		a.Fail(protocol.StopError, "setup failed", cause)
		a.Start() // ignored: the stream is settled
	})
	require.ErrorIs(t, err, cause)
	assert.Equal(t, []string{"error"}, types(items))
	assert.Equal(t, "setup failed", *msg.ErrorMessage)
}

func TestAssemblerFailReasonIsCoerced(t *testing.T) {
	msg, items, err := runBody(t, func(a *Assembler) {
		a.Start()
		a.Fail(protocol.StopToolUse, "x", nil)
	})
	require.Error(t, err)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	assert.Equal(t, protocol.StopError, items[1].Event.(protocol.ErrorEvent).Reason)
}

func TestAssemblerScriptedAbortKeepsReason(t *testing.T) {
	msg, items, err := runBody(t, func(a *Assembler) {
		a.Start()
		a.Fail(protocol.StopAborted, "Request was aborted", nil)
	})
	require.Error(t, err)
	assert.Equal(t, protocol.StopAborted, msg.StopReason)
	assert.Equal(t, protocol.StopAborted, items[1].Event.(protocol.ErrorEvent).Reason)
}

func TestAssemblerToolIDCollisionAvoidance(t *testing.T) {
	msg, _, err := runBody(t, func(a *Assembler) {
		a.Start()
		first := a.ToolStart("", "echo", nil, nil, nil)
		second := a.ToolStart("call_1", "echo", nil, nil, nil)
		third := a.ToolStart("", "echo", nil, nil, nil)
		a.ToolEnd(first, nil)
		a.ToolEnd(second, nil)
		a.ToolEnd(third, &protocol.ToolCall{Name: "echo", Arguments: json.RawMessage(`{}`)})
		fourth := a.ToolStart("", "echo", nil, nil, nil)
		a.ToolDelta(fourth, `{"a"`) // left open: salvage also generates a free id
		a.Fail(protocol.StopError, "stop here", nil)
	})
	require.Error(t, err)
	ids := []string{toolAt(t, msg, 0).ID, toolAt(t, msg, 1).ID, toolAt(t, msg, 2).ID, toolAt(t, msg, 3).ID}
	assert.Equal(t, []string{"call_2", "call_1", "call_3", "call_4"}, ids)
}

func TestAssemblerIDChangeAtEndIsFinalizedByIndex(t *testing.T) {
	msg, _, err := runBody(t, func(a *Assembler) {
		a.Start()
		i := a.ToolStart("temp", "old", nil, nil, nil)
		j := a.ToolStart("other", "echo", nil, nil, nil)
		a.ToolEnd(j, nil)
		a.ToolEnd(i, &protocol.ToolCall{ID: "real", Name: "new", Arguments: json.RawMessage(`{}`)})
		a.Done(protocol.StopToolUse)
	})
	require.NoError(t, err)
	assert.Equal(t, "real", toolAt(t, msg, 0).ID)
	assert.Equal(t, "new", toolAt(t, msg, 0).Name)
	assert.Equal(t, "other", toolAt(t, msg, 1).ID)
}

func TestAssemblerQueuedItemsAreImmutable(t *testing.T) {
	sig := "sig"
	ns := "ns"
	args := json.RawMessage(`{"k":"v"}`)
	finalSig := "sig"
	final := protocol.ToolCall{ID: "i", Name: "n", Arguments: json.RawMessage(`{"z":1}`), ThoughtSignature: &finalSig}
	endSig := "end-sig"
	s := newTestStream(context.Background(), 64, func(a *Assembler) {
		a.Start()
		i := a.ToolStart("i", "n", args, &sig, &ns)
		// The producer changes its own values after each call.
		args[2] = 'X'
		sig = "changed"
		ns = "changed"
		a.ToolEnd(i, &final)
		final.Arguments[2] = 'Y'
		*final.ThoughtSignature = "changed"
		t0 := a.TextStart("")
		a.TextEnd(t0, "t", &endSig)
		endSig = "changed"
		a.Done(protocol.StopToolUse)
	})
	items := drain(s)
	start := items[1].Event.(protocol.ToolCallStartEvent)
	assert.JSONEq(t, `{"k":"v"}`, string(start.Arguments))
	assert.Equal(t, "sig", *start.ThoughtSignature)
	assert.Equal(t, "ns", *start.Namespace)
	end := items[2].Event.(protocol.ToolCallEndEvent)
	assert.JSONEq(t, `{"z":1}`, string(end.ToolCall.Arguments))
	assert.Equal(t, "sig", *end.ToolCall.ThoughtSignature)
	textEnd := items[4].Event.(protocol.TextEndEvent)
	assert.Equal(t, "end-sig", *textEnd.TextSignature)
	msg, err := waitResult(t, s)
	require.NoError(t, err)
	assert.JSONEq(t, `{"z":1}`, string(toolAt(t, msg, 0).Arguments))
	assert.Equal(t, "end-sig", *msg.Content[1].(protocol.Text).TextSignature)
}

func TestAssemblerSeedIsCopiedAndNormalized(t *testing.T) {
	seed := testSeed()
	seed.ResponseID = sptr("r")
	seed.StopReason = protocol.StopStop
	seed.ErrorMessage = sptr("stale")
	seed.Content = []protocol.AssistantBlock{protocol.Text{Text: "stale"}}
	s := NewStream(context.Background(), 8, seed, func(a *Assembler) {
		a.Start()
		a.Done(protocol.StopStop)
	})
	*seed.ResponseID = "changed"
	items := drain(s)
	start := items[0].Event.(protocol.StartEvent)
	assert.Equal(t, protocol.StopPending, start.Message.StopReason)
	assert.Empty(t, start.Message.Content)
	assert.Nil(t, start.Message.ErrorMessage)
	assert.Equal(t, "r", *start.Message.ResponseID)
	msg, err := waitResult(t, s)
	require.NoError(t, err)
	assert.Empty(t, msg.Content)
}

func TestAssemblerBuilderErrorsFailTheStreamWithoutPanic(t *testing.T) {
	cases := map[string]func(a *Assembler){
		"duplicate start":       func(a *Assembler) { a.Start() },
		"delta for unknown":     func(a *Assembler) { a.TextDelta(5, "x") },
		"wrong kind delta":      func(a *Assembler) { a.ToolDelta(a.TextStart(""), "x") },
		"end twice":             func(a *Assembler) { i := a.TextStart(""); a.TextEnd(i, "", nil); a.TextEnd(i, "", nil) },
		"tool end unknown":      func(a *Assembler) { a.ToolEnd(3, nil) },
		"tool end twice":        func(a *Assembler) { i := a.ToolStart("a", "b", nil, nil, nil); a.ToolEnd(i, nil); a.ToolEnd(i, nil) },
		"bad start arguments":   func(a *Assembler) { a.ToolStart("a", "b", json.RawMessage(`[]`), nil, nil) },
		"thinking end for text": func(a *Assembler) { a.ThinkingEnd(a.TextStart(""), "", nil, nil) },
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			msg, items, err := runBody(t, func(a *Assembler) {
				a.Start()
				body(a)
				assert.True(t, a.Settled())
			})
			require.Error(t, err)
			assert.Equal(t, protocol.StopError, msg.StopReason)
			require.NotNil(t, msg.ErrorMessage)
			assert.NotEmpty(t, *msg.ErrorMessage)
			assert.Equal(t, "error", items[len(items)-1].Event.EventType())
		})
	}
}

func TestAssemblerDoneBeforeStartFailsTheStream(t *testing.T) {
	msg, items, err := runBody(t, func(a *Assembler) { a.Done(protocol.StopStop) })
	require.Error(t, err)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	assert.Equal(t, []string{"error"}, types(items))
}

func TestAssemblerEmitValidatesThroughTheBuilder(t *testing.T) {
	msg, items, err := runBody(t, func(a *Assembler) {
		seed := testSeed()
		seed.StopReason = protocol.StopPending
		require.NoError(t, a.Emit(protocol.StartEvent{Message: seed}))
		require.Error(t, a.Emit(protocol.TextDeltaEvent{ContentIndex: 0, Delta: "x"}), "no block yet")
		assert.False(t, a.Settled(), "a rejected raw event leaves the stream open")
		require.NoError(t, a.Emit(protocol.ToolCallStartEvent{ContentIndex: 0, ID: "call_1", ToolName: "echo"}))
		require.NoError(t, a.Emit(protocol.ToolCallDeltaEvent{ContentIndex: 0, Delta: `{"a":1}`}))
		// The assembler knows the raw ids: the next generated id skips call_1.
		i := a.ToolStart("", "echo", nil, nil, nil)
		assert.Equal(t, 1, i)
		a.ToolEnd(i, nil)
		a.ToolEnd(0, nil)
		final := testSeed()
		final.StopReason = protocol.StopToolUse
		final.Content = []protocol.AssistantBlock{}
		require.NoError(t, a.Emit(protocol.DoneEvent{Reason: protocol.StopToolUse, Message: final}))
		assert.ErrorIs(t, a.Emit(protocol.TextStartEvent{}), ErrStreamClosed)
	})
	require.NoError(t, err)
	assert.Equal(t, protocol.StopToolUse, msg.StopReason, "a raw terminal event settles with its own message")
	assert.Equal(t, "done", items[len(items)-1].Event.EventType())
	assert.Equal(t, 7, len(items))
	end := items[4].Event.(protocol.ToolCallEndEvent)
	assert.Equal(t, "call_2", end.ToolCall.ID)
}

func TestAssemblerEmitRawErrorEvent(t *testing.T) {
	msg, items, err := runBody(t, func(a *Assembler) {
		failed := testSeed()
		failed.StopReason = protocol.StopError
		failed.ErrorMessage = sptr("scripted failure")
		require.NoError(t, a.Emit(protocol.ErrorEvent{Reason: protocol.StopError, Error: failed}))
	})
	require.EqualError(t, err, "scripted failure")
	assert.Equal(t, protocol.StopError, msg.StopReason)
	assert.Equal(t, []string{"error"}, types(items))
}

func TestAssemblerEmitCopiesTheEvent(t *testing.T) {
	sig := "s"
	s := newTestStream(context.Background(), 8, func(a *Assembler) {
		a.Start()
		require.NoError(t, a.Emit(protocol.ThinkingStartEvent{ContentIndex: 0, Content: protocol.Thinking{ThinkingSignature: &sig}}))
		sig = "changed"
		a.Done(protocol.StopLength)
	})
	items := drain(s)
	assert.Equal(t, "s", *items[1].Event.(protocol.ThinkingStartEvent).Content.ThinkingSignature)
	msg, err := waitResult(t, s)
	require.NoError(t, err)
	assert.Equal(t, "s", *msg.Content[0].(protocol.Thinking).ThinkingSignature)
}

func TestAssemblerDoneWithFailureReasonGivesAnError(t *testing.T) {
	for _, reason := range []protocol.StopReason{protocol.StopError, protocol.StopAborted} {
		msg, _, err := runBody(t, func(a *Assembler) {
			a.Start()
			a.Done(reason)
		})
		require.Error(t, err, reason)
		require.Equal(t, protocol.StopError, msg.StopReason, reason)
	}
}

func TestAssemblerEmitDoneWithOpenBlockFailsTheStream(t *testing.T) {
	msg, items, err := runBody(t, func(a *Assembler) {
		seed := testSeed()
		seed.StopReason = protocol.StopPending
		require.NoError(t, a.Emit(protocol.StartEvent{Message: seed}))
		require.NoError(t, a.Emit(protocol.TextStartEvent{ContentIndex: 0}))
		final := testSeed()
		final.StopReason = protocol.StopStop
		require.Error(t, a.Emit(protocol.DoneEvent{Reason: protocol.StopStop, Message: final}))
		assert.True(t, a.Settled())
	})
	require.Error(t, err)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	assert.Equal(t, "error", items[len(items)-1].Event.EventType())
}

func TestAssemblerEmitDoneLengthKeepsOpenBlock(t *testing.T) {
	msg, _, err := runBody(t, func(a *Assembler) {
		seed := testSeed()
		seed.StopReason = protocol.StopPending
		require.NoError(t, a.Emit(protocol.StartEvent{Message: seed}))
		require.NoError(t, a.Emit(protocol.TextStartEvent{ContentIndex: 0}))
		final := testSeed()
		final.StopReason = protocol.StopLength
		require.NoError(t, a.Emit(protocol.DoneEvent{Reason: protocol.StopLength, Message: final}))
	})
	require.NoError(t, err)
	assert.Equal(t, protocol.StopLength, msg.StopReason)
}

func TestAssemblerEmitToolEndWithNonObjectArgumentsFailsTheStream(t *testing.T) {
	msg, items, err := runBody(t, func(a *Assembler) {
		seed := testSeed()
		seed.StopReason = protocol.StopPending
		require.NoError(t, a.Emit(protocol.StartEvent{Message: seed}))
		require.NoError(t, a.Emit(protocol.ToolCallStartEvent{ContentIndex: 0, ID: "c", ToolName: "t"}))
		bad := protocol.ToolCall{ID: "c", Name: "t", Arguments: json.RawMessage(`[1]`)}
		require.Error(t, a.Emit(protocol.ToolCallEndEvent{ContentIndex: 0, ToolCall: bad}))
	})
	require.Error(t, err)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	for _, it := range items {
		_, encErr := protocol.MarshalStreamEvent(it.Event)
		assert.NoError(t, encErr, "every queued event encodes")
	}
	_, encErr := protocol.MarshalMessage(msg)
	assert.NoError(t, encErr)
}
