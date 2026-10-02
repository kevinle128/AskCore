package protocol

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustApply(t *testing.T, b *Builder, evs ...AssistantMessageEvent) {
	t.Helper()
	for i, ev := range evs {
		require.NoError(t, b.Apply(ev), "event %d (%s)", i, ev.EventType())
	}
}

func exitSequence(final AssistantMessage) []AssistantMessageEvent {
	return []AssistantMessageEvent{
		StartEvent{Message: seedMessage()},
		ThinkingStartEvent{ContentIndex: 0},
		ThinkingDeltaEvent{ContentIndex: 0, Delta: "go"},
		ThinkingEndEvent{ContentIndex: 0, Content: "go"},
		TextStartEvent{ContentIndex: 1},
		TextDeltaEvent{ContentIndex: 1, Delta: "ok"},
		TextEndEvent{ContentIndex: 1, Content: "ok"},
		ToolCallStartEvent{ContentIndex: 2, ID: "a", ToolName: "echo"},
		ToolCallDeltaEvent{ContentIndex: 2, Delta: "{}"},
		ToolCallEndEvent{ContentIndex: 2, ToolCall: ToolCall{ID: "a", Name: "echo", Arguments: json.RawMessage(`{}`)}},
		ToolCallStartEvent{ContentIndex: 3, ID: "b", ToolName: "echo"},
		ToolCallDeltaEvent{ContentIndex: 3, Delta: "{}"},
		ToolCallEndEvent{ContentIndex: 3, ToolCall: ToolCall{ID: "b", Name: "echo", Arguments: json.RawMessage(`{}`)}},
		DoneEvent{Reason: StopToolUse, Message: final},
	}
}

func exitFinal() AssistantMessage {
	m := seedMessage()
	m.StopReason = StopToolUse
	m.Content = []AssistantBlock{
		Thinking{Thinking: "go"}, Text{Text: "ok"},
		ToolCall{ID: "a", Name: "echo", Arguments: json.RawMessage(`{}`)},
		ToolCall{ID: "b", Name: "echo", Arguments: json.RawMessage(`{}`)},
	}
	return m
}

func TestBuilderExitSequence(t *testing.T) {
	b := NewBuilder()
	assert.False(t, b.Started())
	final := exitFinal()
	evs := exitSequence(final)
	mustApply(t, b, evs[:len(evs)-1]...)
	assert.True(t, b.Started())
	_, done := b.Result()
	assert.False(t, done)
	assert.Empty(t, b.OpenBlocks())

	snap := b.Snapshot()
	assert.Equal(t, StopPending, snap.StopReason)
	assert.Equal(t, final.Content, snap.Content)

	mustApply(t, b, evs[len(evs)-1])
	res, done := b.Result()
	require.True(t, done)
	assert.Equal(t, final, res)
	assert.Equal(t, final, b.Snapshot())
}

func TestBuilderSnapshotShowsInProgressContent(t *testing.T) {
	b := NewBuilder()
	mustApply(t, b,
		StartEvent{Message: seedMessage()},
		TextStartEvent{ContentIndex: 0, Content: Text{Text: "he"}},
		ThinkingStartEvent{ContentIndex: 1, Content: Thinking{Thinking: "t"}},
		TextDeltaEvent{ContentIndex: 0, Delta: "llo"},
		ThinkingDeltaEvent{ContentIndex: 1, Delta: "hink"},
		ToolCallStartEvent{ContentIndex: 2, ID: "a", ToolName: "n"},
		ToolCallDeltaEvent{ContentIndex: 2, Delta: `{"x"`},
		ToolCallDeltaEvent{ContentIndex: 2, Delta: `:1`},
	)
	snap := b.Snapshot()
	assert.Equal(t, Text{Text: "hello"}, snap.Content[0])
	assert.Equal(t, Thinking{Thinking: "think"}, snap.Content[1])
	assert.Equal(t, ToolCall{ID: "a", Name: "n"}, snap.Content[2])
	assert.Equal(t, []int{0, 1, 2}, b.OpenBlocks())
	assert.Equal(t, `{"x":1`, string(b.RawToolJSON(2)))
	assert.Nil(t, b.RawToolJSON(0), "not a tool block")
	assert.Nil(t, b.RawToolJSON(9))
	assert.Nil(t, b.RawToolJSON(-1))
}

func TestBuilderInterleavedBlocks(t *testing.T) {
	b := NewBuilder()
	mustApply(t, b,
		StartEvent{Message: seedMessage()},
		TextStartEvent{ContentIndex: 0},
		TextStartEvent{ContentIndex: 1},
		TextDeltaEvent{ContentIndex: 1, Delta: "B1"},
		TextDeltaEvent{ContentIndex: 0, Delta: "A1"},
		TextDeltaEvent{ContentIndex: 1, Delta: "B2"},
		TextEndEvent{ContentIndex: 0, Content: "A1"},
		TextDeltaEvent{ContentIndex: 1, Delta: "B3"},
	)
	assert.Equal(t, []int{1}, b.OpenBlocks())
	assert.Equal(t, []AssistantBlock{Text{Text: "A1"}, Text{Text: "B1B2B3"}}, b.Snapshot().Content)
}

func TestBuilderInitialContentWithNoDeltas(t *testing.T) {
	b := NewBuilder()
	mustApply(t, b,
		StartEvent{Message: seedMessage()},
		TextStartEvent{ContentIndex: 0, Content: Text{Text: "whole", TextSignature: sp("sig")}},
		TextEndEvent{ContentIndex: 0, Content: "whole", TextSignature: sp("sig")},
		ThinkingStartEvent{ContentIndex: 1, Content: Thinking{Thinking: "plan"}},
		ToolCallStartEvent{ContentIndex: 2, ID: "a", ToolName: "n", Arguments: json.RawMessage(`{"k":1}`), Namespace: sp("ns")},
	)
	snap := b.Snapshot()
	assert.Equal(t, Text{Text: "whole", TextSignature: sp("sig")}, snap.Content[0])
	assert.Equal(t, Thinking{Thinking: "plan"}, snap.Content[1], "initial text survives with no delta")
	assert.Equal(t, ToolCall{ID: "a", Name: "n", Arguments: json.RawMessage(`{"k":1}`), Namespace: sp("ns")}, snap.Content[2])
}

func TestBuilderSignedEmptyBlocksAndRedactedThinking(t *testing.T) {
	b := NewBuilder()
	mustApply(t, b,
		StartEvent{Message: seedMessage()},
		TextStartEvent{ContentIndex: 0},
		TextEndEvent{ContentIndex: 0, Content: "", TextSignature: sp("")},
		ThinkingStartEvent{ContentIndex: 1},
		ThinkingEndEvent{ContentIndex: 1, Content: "", ThinkingSignature: sp("enc")},
		ThinkingStartEvent{ContentIndex: 2, Content: Thinking{ThinkingSignature: sp("blob"), Redacted: bp(true)}},
		ThinkingEndEvent{ContentIndex: 2, Content: "", ThinkingSignature: sp("blob"), Redacted: bp(true)},
		ThinkingStartEvent{ContentIndex: 3},
		ThinkingEndEvent{ContentIndex: 3, Content: "x", ThinkingSignature: sp(""), Redacted: bp(false)},
	)
	c := b.Snapshot().Content
	assert.Equal(t, Text{Text: "", TextSignature: sp("")}, c[0])
	assert.Equal(t, Thinking{ThinkingSignature: sp("enc")}, c[1])
	assert.Equal(t, Thinking{ThinkingSignature: sp("blob"), Redacted: bp(true)}, c[2])
	assert.Equal(t, Thinking{Thinking: "x", ThinkingSignature: sp(""), Redacted: bp(false)}, c[3])
}

func TestBuilderEndIsAuthoritativeAndRemovesAbsentMetadata(t *testing.T) {
	b := NewBuilder()
	mustApply(t, b,
		StartEvent{Message: seedMessage()},
		TextStart(0, Text{Text: "a", TextSignature: sp("old")}),
		ThinkingStartEvent{ContentIndex: 1, Content: Thinking{Thinking: "b", ThinkingSignature: sp("old"), Redacted: bp(true)}},
		TextDeltaEvent{ContentIndex: 0, Delta: "ignored by end"},
		TextEndEvent{ContentIndex: 0, Content: "final"},
		ThinkingEndEvent{ContentIndex: 1, Content: "fin"},
	)
	c := b.Snapshot().Content
	assert.Equal(t, Text{Text: "final"}, c[0], "absent signature removes the early one")
	assert.Equal(t, Thinking{Thinking: "fin"}, c[1], "absent metadata removes early signature and redaction")
}

// TextStart builds a start event; it keeps the table above short.
func TextStart(i int, t Text) TextStartEvent { return TextStartEvent{ContentIndex: i, Content: t} }

func TestBuilderContentOnlyAtEnd(t *testing.T) {
	b := NewBuilder()
	mustApply(t, b,
		StartEvent{Message: seedMessage()},
		TextStartEvent{ContentIndex: 0},
		TextEndEvent{ContentIndex: 0, Content: "all at once"},
	)
	assert.Equal(t, Text{Text: "all at once"}, b.Snapshot().Content[0])
}

func TestBuilderToolCallEndReplacesIdentityAndArguments(t *testing.T) {
	b := NewBuilder()
	mustApply(t, b,
		StartEvent{Message: seedMessage()},
		ToolCallStartEvent{ContentIndex: 0, ID: "tmp", ToolName: "draft", Arguments: json.RawMessage(`{"a":1}`), ThoughtSignature: sp("t0"), Namespace: sp("n0")},
		ToolCallDeltaEvent{ContentIndex: 0, Delta: `{"broken"`},
		ToolCallEndEvent{ContentIndex: 0, ToolCall: ToolCall{ID: "final", Name: "real", Arguments: json.RawMessage(`{"b":2}`), Namespace: sp("n1")}},
	)
	assert.Equal(t, ToolCall{ID: "final", Name: "real", Arguments: json.RawMessage(`{"b":2}`), Namespace: sp("n1")}, b.Snapshot().Content[0])
	assert.Nil(t, b.RawToolJSON(0), "scratch bytes are released at end")

	// Final-only arguments, with no delta at all.
	b = NewBuilder()
	mustApply(t, b,
		StartEvent{Message: seedMessage()},
		ToolCallStartEvent{ContentIndex: 0, ID: "x", ToolName: "n"},
		ToolCallEndEvent{ContentIndex: 0, ToolCall: ToolCall{ID: "x", Name: "n", Arguments: json.RawMessage(`{"only":"end"}`)}},
	)
	assert.JSONEq(t, `{"only":"end"}`, string(b.Snapshot().Content[0].(ToolCall).Arguments))
}

func TestBuilderDoneAndErrorReplaceWholePartial(t *testing.T) {
	b := NewBuilder()
	mustApply(t, b, StartEvent{Message: seedMessage()}, TextStartEvent{ContentIndex: 0}, TextDeltaEvent{ContentIndex: 0, Delta: "partial"})
	final := seedMessage()
	final.StopReason = StopStop
	final.Content = []AssistantBlock{Text{Text: "other"}}
	final.ResponseID = sp("late")
	mustApply(t, b, DoneEvent{Reason: StopStop, Message: final})
	res, ok := b.Result()
	require.True(t, ok)
	assert.Equal(t, final, res)
	assert.Empty(t, b.OpenBlocks())

	b = NewBuilder()
	mustApply(t, b, StartEvent{Message: seedMessage()}, TextStartEvent{ContentIndex: 0}, TextDeltaEvent{ContentIndex: 0, Delta: "kept"})
	failed := seedMessage()
	failed.StopReason = StopAborted
	failed.ErrorMessage = sp("Request was aborted")
	failed.Content = []AssistantBlock{Text{Text: "kept"}}
	mustApply(t, b, ErrorEvent{Reason: StopAborted, Error: failed})
	res, _ = b.Result()
	assert.Equal(t, failed, res)
}

func TestBuilderSetupErrorBeforeStart(t *testing.T) {
	b := NewBuilder()
	failed := seedMessage()
	failed.StopReason = StopError
	failed.ErrorMessage = sp("No more faux responses queued")
	mustApply(t, b, ErrorEvent{Reason: StopError, Error: failed})
	res, ok := b.Result()
	require.True(t, ok)
	assert.Equal(t, failed, res)
	assert.True(t, b.Started())
	require.Error(t, b.Apply(StartEvent{Message: seedMessage()}), "event after terminal")
}

func TestBuilderSeedWithBlocksStartsThemClosed(t *testing.T) {
	seed := seedMessage()
	seed.Content = []AssistantBlock{Text{Text: "pre"}}
	b := NewBuilder()
	mustApply(t, b, StartEvent{Message: seed}, TextStartEvent{ContentIndex: 1})
	require.Error(t, b.Apply(TextDeltaEvent{ContentIndex: 0, Delta: "x"}))
	assert.Equal(t, []int{1}, b.OpenBlocks())
	assert.Equal(t, Text{Text: "pre"}, b.Snapshot().Content[0])
}

func TestBuilderRejectsBadSequences(t *testing.T) {
	started := func() *Builder {
		b := NewBuilder()
		mustApply(t, b, StartEvent{Message: seedMessage()})
		return b
	}
	withText := func() *Builder {
		b := started()
		mustApply(t, b, TextStartEvent{ContentIndex: 0})
		return b
	}
	ended := func() *Builder {
		b := withText()
		mustApply(t, b, TextEndEvent{ContentIndex: 0, Content: "x"})
		return b
	}
	withTool := func() *Builder {
		b := started()
		mustApply(t, b, ToolCallStartEvent{ContentIndex: 0, ID: "a", ToolName: "n"})
		return b
	}
	okDone := DoneEvent{Reason: StopStop, Message: func() AssistantMessage { m := seedMessage(); m.StopReason = StopStop; return m }()}
	cases := []struct {
		name  string
		setup func() *Builder
		ev    AssistantMessageEvent
	}{
		{"nil event", started, nil},
		{"update before start", NewBuilder, TextStartEvent{ContentIndex: 0}},
		{"delta before start", NewBuilder, TextDeltaEvent{ContentIndex: 0}},
		{"done before start", NewBuilder, okDone},
		{"duplicate start", started, StartEvent{Message: seedMessage()}},
		{"negative index", started, TextStartEvent{ContentIndex: -1}},
		{"gap index", started, TextStartEvent{ContentIndex: 1}},
		{"gap after block", withText, ThinkingStartEvent{ContentIndex: 2}},
		{"duplicate block start", withText, TextStartEvent{ContentIndex: 0}},
		{"duplicate start of other kind", withText, ThinkingStartEvent{ContentIndex: 0}},
		{"delta unknown index", started, TextDeltaEvent{ContentIndex: 0}},
		{"negative delta index", withText, TextDeltaEvent{ContentIndex: -1}},
		{"wrong kind delta", withText, ThinkingDeltaEvent{ContentIndex: 0}},
		{"wrong kind tool delta", withText, ToolCallDeltaEvent{ContentIndex: 0}},
		{"wrong kind end", withText, ToolCallEndEvent{ContentIndex: 0}},
		{"text delta on tool", withTool, TextDeltaEvent{ContentIndex: 0}},
		{"thinking end on tool", withTool, ThinkingEndEvent{ContentIndex: 0}},
		{"delta after end", ended, TextDeltaEvent{ContentIndex: 0, Delta: "x"}},
		{"duplicate end", ended, TextEndEvent{ContentIndex: 0, Content: "x"}},
		{"end unknown index", started, TextEndEvent{ContentIndex: 3}},
		{"done reason pending", started, DoneEvent{Reason: StopPending, Message: okDone.Message}},
		{"done reason error", started, DoneEvent{Reason: StopError, Message: okDone.Message}},
		{"error reason stop", started, ErrorEvent{Reason: StopStop, Error: okDone.Message}},
		{"error reason empty", NewBuilder, ErrorEvent{Error: okDone.Message}},
		{"done reason differs from message", started, DoneEvent{Reason: StopLength, Message: okDone.Message}},
		{"error reason differs from message", started, ErrorEvent{Reason: StopAborted, Error: func() AssistantMessage { m := okDone.Message; m.StopReason = StopError; return m }()}},
		{"setup error reason differs from message", NewBuilder, ErrorEvent{Reason: StopError, Error: okDone.Message}},
		{"event after done", func() *Builder { b := started(); mustApply(t, b, okDone); return b }, TextStartEvent{ContentIndex: 0}},
		{"second done", func() *Builder { b := started(); mustApply(t, b, okDone); return b }, okDone},
		{"event after error", func() *Builder {
			b := started()
			failed := okDone.Message
			failed.StopReason = StopError
			mustApply(t, b, ErrorEvent{Reason: StopError, Error: failed})
			return b
		}, okDone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := tc.setup()
			before := b.Snapshot()
			var err error
			require.NotPanics(t, func() { err = b.Apply(tc.ev) })
			require.Error(t, err)
			assert.Equal(t, before, b.Snapshot(), "a rejected event leaves the state unchanged")
		})
	}
}

func TestBuilderRejectedEventDoesNotBreakFollowingValidEvents(t *testing.T) {
	b := NewBuilder()
	mustApply(t, b, StartEvent{Message: seedMessage()}, TextStartEvent{ContentIndex: 0})
	require.Error(t, b.Apply(TextStartEvent{ContentIndex: 5}))
	mustApply(t, b, TextDeltaEvent{ContentIndex: 0, Delta: "ok"}, TextEndEvent{ContentIndex: 0, Content: "ok"})
	assert.Equal(t, Text{Text: "ok"}, b.Snapshot().Content[0])
}

func TestBuilderSnapshotBeforeStart(t *testing.T) {
	snap := NewBuilder().Snapshot()
	assert.NotNil(t, snap.Content)
	_, ok := NewBuilder().Result()
	assert.False(t, ok)
}

func TestBuilderOwnsItsState(t *testing.T) {
	sig := "sig"
	args := json.RawMessage(`{"a":1}`)
	ns := "ns"
	seed := seedMessage()
	seed.ResponseID = sp("r")
	start := StartEvent{Message: seed}
	textStart := TextStartEvent{ContentIndex: 0, Content: Text{Text: "t", TextSignature: &sig}}
	toolStart := ToolCallStartEvent{ContentIndex: 1, ID: "a", ToolName: "n", Arguments: args, Namespace: &ns}
	endCall := ToolCall{ID: "a", Name: "n", Arguments: json.RawMessage(`{"z":9}`)}
	toolEnd := ToolCallEndEvent{ContentIndex: 1, ToolCall: endCall}

	b := NewBuilder()
	mustApply(t, b, start, textStart, toolStart)

	// The producer changes its own copies after the events were applied.
	*seed.ResponseID = "mutated"
	sig = "mutated"
	args[2] = 'Z'
	ns = "mutated"
	snap := b.Snapshot()
	assert.Equal(t, "r", *snap.ResponseID)
	assert.Equal(t, "sig", *snap.Content[0].(Text).TextSignature)
	assert.JSONEq(t, `{"a":1}`, string(snap.Content[1].(ToolCall).Arguments))
	assert.Equal(t, "ns", *snap.Content[1].(ToolCall).Namespace)

	// A reader changes a snapshot.
	*snap.ResponseID = "reader"
	snap.Content[1].(ToolCall).Arguments[2] = 'Q'
	snap.Content = nil
	snap2 := b.Snapshot()
	assert.Equal(t, "r", *snap2.ResponseID)
	assert.JSONEq(t, `{"a":1}`, string(snap2.Content[1].(ToolCall).Arguments))

	mustApply(t, b, toolEnd)
	endCall.Arguments[2] = 'Z'
	assert.JSONEq(t, `{"z":9}`, string(b.Snapshot().Content[1].(ToolCall).Arguments))

	final := seedMessage()
	final.StopReason = StopToolUse
	final.Content = []AssistantBlock{ToolCall{ID: "a", Name: "n", Arguments: json.RawMessage(`{"f":1}`)}}
	mustApply(t, b, DoneEvent{Reason: StopToolUse, Message: final})
	final.Content[0].(ToolCall).Arguments[2] = 'Z'

	res1, _ := b.Result()
	res1.Content[0].(ToolCall).Arguments[2] = 'Q'
	res2, _ := b.Result()
	assert.JSONEq(t, `{"f":1}`, string(res2.Content[0].(ToolCall).Arguments), "repeated results do not share raw JSON")
	assert.NotSame(t, &res1.Content[0], &res2.Content[0])
	assert.JSONEq(t, `{"f":1}`, string(b.Snapshot().Content[0].(ToolCall).Arguments))
}

func TestBuilderRawToolJSONIsACopy(t *testing.T) {
	b := NewBuilder()
	mustApply(t, b, StartEvent{Message: seedMessage()}, ToolCallStartEvent{ContentIndex: 0, ID: "a", ToolName: "n"}, ToolCallDeltaEvent{ContentIndex: 0, Delta: "{}"})
	raw := b.RawToolJSON(0)
	raw[0] = 'X'
	assert.Equal(t, "{}", string(b.RawToolJSON(0)))
}

func TestBuilderApplyAgentEvents(t *testing.T) {
	b := NewBuilder()
	usage := Usage{Input: 4, Output: 1, TotalTokens: 5}
	final := exitFinal()
	final.Usage = usage
	require.NoError(t, b.ApplyAgentEvent(&AgentStart{}))
	require.NoError(t, b.ApplyAgentEvent(&MessageStart{Message: UserMessage{}}), "other roles are ignored")
	assert.False(t, b.Started())
	require.NoError(t, b.ApplyAgentEvent(&MessageStart{Message: seedMessage()}))
	require.NoError(t, b.ApplyAgentEvent(&MessageUpdate{AssistantMessageEvent: TextStartEvent{ContentIndex: 0}}))
	require.NoError(t, b.ApplyAgentEvent(&MessageUpdate{AssistantMessageEvent: TextDeltaEvent{ContentIndex: 0, Delta: "hi"}, Usage: usage}))
	snap := b.Snapshot()
	assert.Equal(t, Text{Text: "hi"}, snap.Content[0])
	assert.Equal(t, usage, snap.Usage, "mid-stream usage is applied")

	require.Error(t, b.ApplyAgentEvent(&MessageUpdate{AssistantMessageEvent: TextDeltaEvent{ContentIndex: 4}}))
	require.Error(t, b.ApplyAgentEvent(&MessageUpdate{}))
	pending := exitFinal()
	pending.StopReason = StopPending
	require.Error(t, b.ApplyAgentEvent(&MessageEnd{Message: pending}))

	require.NoError(t, b.ApplyAgentEvent(&MessageEnd{Message: UserMessage{}}), "other roles are ignored")
	require.NoError(t, b.ApplyAgentEvent(&MessageEnd{Message: final}))
	res, ok := b.Result()
	require.True(t, ok)
	assert.Equal(t, final, res)
	require.Error(t, b.ApplyAgentEvent(&MessageEnd{Message: final}))
}

func TestBuilderApplyAgentEventSetupError(t *testing.T) {
	failed := seedMessage()
	failed.StopReason = StopError
	failed.ErrorMessage = sp("bad model")
	b := NewBuilder()
	require.NoError(t, b.ApplyAgentEvent(&MessageStart{Message: seedMessage()}))
	require.NoError(t, b.ApplyAgentEvent(&MessageEnd{Message: failed}))
	res, ok := b.Result()
	require.True(t, ok)
	assert.Equal(t, failed, res)
}

func TestBuilderRebuildsFromJSONLProjection(t *testing.T) {
	final := exitFinal()
	var evs []Event
	seq := uint64(0)
	next := func() Envelope { seq++; return env(seq) }
	for _, ev := range exitSequence(final) {
		switch e := ev.(type) {
		case StartEvent:
			evs = append(evs, &MessageStart{Envelope: next(), Message: e.Message})
		case DoneEvent:
			evs = append(evs, &MessageEnd{Envelope: next(), Message: e.Message})
		case BlockEvent:
			evs = append(evs, &MessageUpdate{Envelope: next(), AssistantMessageEvent: e})
		}
	}
	var sb strings.Builder
	w := NewJSONLWriter(&sb)
	for _, ev := range evs {
		require.NoError(t, w.Write(ev))
	}
	b := NewBuilder()
	r := NewJSONLReader(strings.NewReader(sb.String()))
	for {
		line, err := r.Next()
		if err != nil {
			break
		}
		ev, err := DecodeEvent(line)
		require.NoError(t, err)
		require.NoError(t, b.ApplyAgentEvent(ev))
	}
	res, ok := b.Result()
	require.True(t, ok)
	assert.Equal(t, final, res)
}

// measure returns the bytes and the number of heap allocations that
// applying n deltas of fixed size costs. The events are built beforehand.
func measureDeltas(t *testing.T, n int, mk func(i int) AssistantMessageEvent, startEv AssistantMessageEvent) (bytes, mallocs uint64) {
	t.Helper()
	b := NewBuilder()
	mustApply(t, b, StartEvent{Message: seedMessage()}, startEv)
	evs := make([]AssistantMessageEvent, n)
	for i := range evs {
		evs[i] = mk(i)
	}
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	for _, ev := range evs {
		if err := b.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	runtime.ReadMemStats(&m1)
	return m1.TotalAlloc - m0.TotalAlloc, m1.Mallocs - m0.Mallocs
}

// minMeasure repeats measureDeltas and keeps the smallest result. Allocation by
// other goroutines can only add to a reading, so the minimum is the closest
// reading to the cost of the builder alone.
func minMeasure(t *testing.T, n int, mk func(i int) AssistantMessageEvent, startEv AssistantMessageEvent) (bytes, mallocs uint64) {
	t.Helper()
	bytes, mallocs = ^uint64(0), ^uint64(0)
	for range 7 {
		b, m := measureDeltas(t, n, mk, startEv)
		bytes, mallocs = min(bytes, b), min(mallocs, m)
	}
	return bytes, mallocs
}

func TestBuilderDeltaWorkScalesLinearly(t *testing.T) {
	// A builder that copies the message or joins strings per delta would use
	// about 100 times more memory for 10 times more input (quadratic).
	// This test checks bytes and allocation counts, never wall-clock time.
	const small, large = 2_000, 20_000
	delta := strings.Repeat("d", 64)
	kinds := map[string]struct {
		start AssistantMessageEvent
		mk    func(int) AssistantMessageEvent
	}{
		"text": {TextStartEvent{ContentIndex: 0}, func(int) AssistantMessageEvent { return TextDeltaEvent{ContentIndex: 0, Delta: delta} }},
		"thinking": {ThinkingStartEvent{ContentIndex: 0}, func(int) AssistantMessageEvent {
			return ThinkingDeltaEvent{ContentIndex: 0, Delta: delta}
		}},
		"tool": {ToolCallStartEvent{ContentIndex: 0, ID: "a", ToolName: "n"}, func(int) AssistantMessageEvent {
			return ToolCallDeltaEvent{ContentIndex: 0, Delta: delta}
		}},
	}
	for name, k := range kinds {
		t.Run(name, func(t *testing.T) {
			bs, ms := minMeasure(t, small, k.mk, k.start)
			bl, ml := minMeasure(t, large, k.mk, k.start)
			assert.LessOrEqual(t, bl, bs*20, "bytes: %d at %d deltas, %d at %d deltas", bs, small, bl, large)
			assert.Less(t, ml, uint64(large/10), "allocations at %d deltas: %d", large, ml)
			assert.Less(t, ms, uint64(small/10), "allocations at %d deltas: %d", small, ms)
			assert.Less(t, bl, uint64(large*len(delta)*8), "total bytes stay within a small multiple of the input (append growth is geometric)")
		})
	}
}

func TestSnapshotBeforeStartIsPending(t *testing.T) {
	snap := NewBuilder().Snapshot()
	assert.Equal(t, StopPending, snap.StopReason)
	assert.NotNil(t, snap.Content)
	assert.Empty(t, snap.Content)
}
