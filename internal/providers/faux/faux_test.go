package faux

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func TestNewValidatesOptions(t *testing.T) {
	cases := map[string][]Option{
		"empty model id":      {WithModels(ModelDef{ID: ""})},
		"duplicate model id":  {WithModels(ModelDef{ID: "a"}, ModelDef{ID: "a"})},
		"zero chunk min":      {WithChunk(0, 3)},
		"max below min":       {WithChunk(4, 3)},
		"negative pace":       {WithTokensPerSecond(-1)},
		"negative buffer":     {WithBuffer(-1)},
		"nil clock":           {WithClock(nil)},
		"empty api":           {WithAPI("")},
		"empty provider name": {WithProvider("")},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			p, err := New(opts...)
			require.Error(t, err)
			assert.Nil(t, p)
		})
	}
}

func TestModelLookupKeepsPerModelData(t *testing.T) {
	p := newProvider(t,
		WithAPI("faux-api"), WithProvider("faux-host"),
		WithModels(
			ModelDef{ID: "small", Reasoning: true, Input: []string{"text"}, ContextWindow: 1000, MaxTokens: 100},
			ModelDef{ID: "plain", Name: "Plain"},
		))
	small, ok := p.Model("small")
	require.True(t, ok)
	assert.Equal(t, providers.Model{
		ID: "small", Name: "small", API: "faux-api", Provider: "faux-host", Reasoning: true,
		Input: []string{"text"}, ContextWindow: 1000, MaxTokens: 100,
	}, small)
	plain, _ := p.Model("plain")
	assert.Equal(t, "Plain", plain.Name)
	assert.Equal(t, []string{"text", "image"}, plain.Input)
	assert.Equal(t, 128000, plain.ContextWindow)
	assert.Equal(t, 16384, plain.MaxTokens)

	miss, ok := p.Model("nope")
	assert.False(t, ok)
	assert.Equal(t, providers.Model{}, miss)

	small.Input[0] = "changed"
	again, _ := p.Model("small")
	assert.Equal(t, []string{"text"}, again.Input, "the returned model shares no slice with the provider")
	assert.Equal(t, "faux-api", p.API())
}

func TestQueueExhaustionGivesOneErrorEvent(t *testing.T) {
	clock := newFakeClock()
	p := newProvider(t, WithClock(clock))
	items, msg, err := play(t, p)
	require.Len(t, items, 1)
	ev, ok := items[0].Event.(protocol.ErrorEvent)
	require.True(t, ok)
	assert.Equal(t, protocol.StopError, ev.Reason)
	require.Error(t, err)
	assert.Equal(t, "No more faux responses queued", err.Error())
	assert.Equal(t, protocol.StopError, msg.StopReason)
	require.NotNil(t, msg.ErrorMessage)
	assert.Equal(t, "No more faux responses queued", *msg.ErrorMessage)
	assert.Equal(t, "faux", msg.API)
	assert.Equal(t, "faux", msg.Provider)
	assert.Equal(t, defaultModelID, msg.Model)
	assert.Equal(t, fixedNow, msg.Timestamp)
	assert.Empty(t, msg.Content)
	assert.Equal(t, int64(2), msg.Usage.Input, "the usage of an exhausted call is still estimated (user:hi is 7 runes)")
	assert.Equal(t, 1, p.Calls())
	assert.Len(t, p.Requests(), 1, "an exhausted call is recorded too")
}

func TestSetReplacesAppendAddsAndCallsKeepCounting(t *testing.T) {
	p := newProvider(t)
	p.Set(Say("a"), Say("b"))
	assert.Equal(t, 2, p.Pending())
	_, msg, _ := play(t, p)
	assert.Equal(t, protocol.Text{Text: "a"}, msg.Content[0])
	assert.Equal(t, 1, p.Pending())
	p.Append(Say("c"))
	assert.Equal(t, 2, p.Pending())
	p.Set(Say("z"))
	assert.Equal(t, 1, p.Pending())
	assert.Equal(t, 1, p.Calls(), "Set does not reset the call count")
	_, msg, _ = play(t, p)
	assert.Equal(t, protocol.Text{Text: "z"}, msg.Content[0])
	assert.Equal(t, 2, p.Calls())
	assert.Equal(t, 0, p.Pending())
}

func TestSetKeepsReplyInProgress(t *testing.T) {
	clock := newFakeClock()
	p := newProvider(t, WithClock(clock), WithChunk(1, 1), WithTokensPerSecond(4))
	p.Set(Say("abcdefgh"))
	s := start(t, p, context.Background())
	clock.waitArmed(t)
	p.Set(Say("other"))
	clock.Advance(250 * time.Millisecond)
	clock.waitArmed(t)
	clock.Advance(250 * time.Millisecond)
	items := drain(s)
	assert.Equal(t, "done", items[len(items)-1].Event.EventType())
	msg, err := result(t, s)
	require.NoError(t, err)
	assert.Equal(t, protocol.Text{Text: "abcdefgh"}, msg.Content[0])
	assert.Equal(t, 1, p.Pending())
}

func TestFuncSeesCallStateAndRequest(t *testing.T) {
	p := newProvider(t, WithModels(ModelDef{ID: "m1"}, ModelDef{ID: "m2"}))
	type ctxKey struct{}
	var seen []Call
	var ctxValues []any
	f := func(ctx context.Context, c Call) (Step, error) {
		seen = append(seen, c)
		ctxValues = append(ctxValues, ctx.Value(ctxKey{}))
		return Say(fmt.Sprintf("call %d", c.Number)), nil
	}
	p.Set(Func(f), Func(f))
	temp := 0.5
	ctx := context.WithValue(context.Background(), ctxKey{}, "request")
	m2, _ := p.Model("m2")
	s := p.Stream(ctx, m2, req(userMsg("one")), providers.StreamOptions{SessionID: "sid", Temperature: &temp, ToolChoice: "echo"})
	drain(s)
	m1, _ := p.Model("m1")
	s = p.Stream(ctx, m1, req(userMsg("two")), providers.StreamOptions{MaxTokens: 7})
	drain(s)

	require.Len(t, seen, 2)
	assert.Equal(t, 1, seen[0].Number)
	assert.Equal(t, 2, seen[1].Number)
	assert.Equal(t, "m2", seen[0].Model.ID)
	assert.Equal(t, "sid", seen[0].Options.SessionID)
	assert.Equal(t, "echo", seen[0].Options.ToolChoice)
	assert.InDelta(t, 0.5, *seen[0].Options.Temperature, 0)
	assert.Equal(t, 7, seen[1].Options.MaxTokens)
	assert.Equal(t, req(userMsg("one")), seen[0].Request)
	assert.Equal(t, []any{"request", "request"}, ctxValues)

	recs := p.Requests()
	require.Len(t, recs, 2)
	assert.Equal(t, 1, recs[0].Call)
	assert.Equal(t, "m2", recs[0].Model.ID)
	assert.Equal(t, req(userMsg("two")), recs[1].Transcript)
	// The records are copies.
	temp = 0.9
	*recs[0].Options.Temperature = 0.1
	recs[0].Options.ToolChoice = "changed"
	again := p.Requests()
	assert.Equal(t, "echo", again[0].Options.ToolChoice)
	assert.InDelta(t, 0.5, *again[0].Options.Temperature, 0)
}

func TestRequestRecordIsImmutableCopy(t *testing.T) {
	p := newProvider(t)
	p.Set(Say("x"))
	msgs := []protocol.Message{userMsg("orig")}
	s := p.Stream(context.Background(), defaultModel(t, p), providers.TranscriptRequest{Messages: msgs}, providers.StreamOptions{})
	msgs[0] = userMsg("changed")
	drain(s)
	got := p.Requests()[0].Transcript.Messages[0].(protocol.UserMessage)
	assert.Equal(t, "orig", got.Content[0].(protocol.Text).Text)
}

func TestFuncFailureModes(t *testing.T) {
	boom := errors.New("factory boom")
	cases := map[string]struct {
		step Step
		want string
	}{
		"error":       {Func(func(context.Context, Call) (Step, error) { return Step{}, boom }), "factory boom"},
		"panic":       {Func(func(context.Context, Call) (Step, error) { panic("oops") }), "factory panicked: oops"},
		"nil factory": {Func(nil), "no factory"},
		"nested func": {Func(func(context.Context, Call) (Step, error) {
			return Func(nil), nil
		}), "cannot return a Func step"},
		"inner timestamp": {Func(func(context.Context, Call) (Step, error) {
			return Say("x").Timestamp(5), nil
		}), "cannot set Timestamp"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := newProvider(t)
			p.Set(tc.step)
			items, msg, err := play(t, p)
			require.Len(t, items, 1, "a setup error has no start event")
			assert.Equal(t, "error", items[0].Event.EventType())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Equal(t, protocol.StopError, msg.StopReason)
			assert.Contains(t, *msg.ErrorMessage, tc.want)
			assert.Equal(t, defaultModelID, msg.Model)
		})
	}
	p := newProvider(t)
	p.Set(Func(func(context.Context, Call) (Step, error) { return Step{}, boom }))
	_, _, err := play(t, p)
	assert.ErrorIs(t, err, boom)
}

func TestFuncModifiersAreDefaultsForTheReturnedStep(t *testing.T) {
	p := newProvider(t)
	p.Set(Func(func(context.Context, Call) (Step, error) {
		return Say("x").ResponseID("inner"), nil
	}).ResponseID("outer").Stop(protocol.StopLength).Timestamp(0))
	_, msg, err := play(t, p)
	require.NoError(t, err)
	assert.Equal(t, protocol.StopLength, msg.StopReason)
	assert.Equal(t, "inner", *msg.ResponseID)
	assert.Equal(t, int64(0), msg.Timestamp, "an explicit zero timestamp stays zero")
}

func TestUnknownModelIsASetupErrorAndKeepsTheQueue(t *testing.T) {
	p := newProvider(t)
	p.Set(Say("kept"))
	s := p.Stream(context.Background(), providers.Model{ID: "ghost"}, req(), providers.StreamOptions{})
	items := drain(s)
	require.Len(t, items, 1)
	msg, err := result(t, s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown faux model: ghost")
	assert.Equal(t, "ghost", msg.Model)
	assert.Equal(t, 1, p.Calls())
	assert.Equal(t, 1, p.Pending(), "the script step is not spent on an invalid call")
}

func TestScriptedReplyEventOrderAndTwoTools(t *testing.T) {
	p := newProvider(t, WithChunk(1, 1))
	p.Set(Reply(Thinking("go"), Text("ok"), ToolCall("a", nil), ToolCall("b", map[string]int{"n": 1})))
	items, msg, err := play(t, p)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"start",
		"thinking_start", "thinking_delta", "thinking_end",
		"text_start", "text_delta", "text_end",
		"toolcall_start", "toolcall_delta", "toolcall_end",
		"toolcall_start", "toolcall_delta", "toolcall_delta", "toolcall_end", // {"n":1} is two chunks
		"done",
	}, kinds(items))
	assert.Equal(t, protocol.StopToolUse, msg.StopReason)
	require.Len(t, msg.Content, 4)
	assert.Equal(t, protocol.ToolCall{ID: "tool:1:1", Name: "a", Arguments: []byte(`{}`)}, msg.Content[2])
	assert.Equal(t, protocol.ToolCall{ID: "tool:1:2", Name: "b", Arguments: []byte(`{"n":1}`)}, msg.Content[3])
	start := items[7].Event.(protocol.ToolCallStartEvent)
	assert.Equal(t, "tool:1:1", start.ID)
	assert.Equal(t, "a", start.ToolName)
}

func TestToolIdsUseCallNumberAndKeepExplicitIds(t *testing.T) {
	p1 := newProvider(t)
	p2 := newProvider(t)
	p1.Set(Reply(ToolCall("x", nil), ToolCall("y", nil, ID("mine")), ToolCallRaw("z", "", `{}`)), Reply(ToolCall("x", nil)))
	p2.Set(Reply(ToolCall("x", nil)))
	_, m1, _ := play(t, p1)
	_, m2, _ := play(t, p2)
	_, m3, _ := play(t, p1)
	id := func(m protocol.AssistantMessage, i int) string { return m.Content[i].(protocol.ToolCall).ID }
	assert.Equal(t, "tool:1:1", id(m1, 0))
	assert.Equal(t, "mine", id(m1, 1))
	assert.Equal(t, "tool:1:2", id(m1, 2))
	assert.Equal(t, "tool:1:1", id(m2, 0), "the call number is per instance")
	assert.Equal(t, "tool:2:1", id(m3, 0))
}

func TestDefaultStopReason(t *testing.T) {
	p := newProvider(t)
	p.Set(Say("a"), Reply(Text("a"), ToolCall("t", nil)), Reply(), Say("a").Stop(protocol.StopLength), Say("a").Stop(protocol.StopDeferred))
	var got []protocol.StopReason
	for range 5 {
		_, msg, err := play(t, p)
		require.NoError(t, err)
		got = append(got, msg.StopReason)
	}
	assert.Equal(t, []protocol.StopReason{protocol.StopStop, protocol.StopToolUse, protocol.StopStop, protocol.StopLength, protocol.StopDeferred}, got)
}

func TestScriptedTerminalFailures(t *testing.T) {
	cases := map[string]struct {
		step    Step
		reason  protocol.StopReason
		message string
	}{
		"error":           {Say("part").Stop(protocol.StopError).Error("overloaded"), protocol.StopError, "overloaded"},
		"error default":   {Say("part").Stop(protocol.StopError), protocol.StopError, msgFailed},
		"Fail":            {Fail("nope"), protocol.StopError, "nope"},
		"aborted":         {Say("part").Stop(protocol.StopAborted), protocol.StopAborted, "Request was aborted"},
		"aborted message": {Say("part").Stop(protocol.StopAborted).Error("user stop"), protocol.StopAborted, "user stop"},
		"pending":         {Say("part").Stop(protocol.StopPending), protocol.StopError, "Faux response ended without a stop reason"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := newProvider(t)
			p.Set(tc.step)
			items, msg, err := play(t, p)
			require.Error(t, err)
			assert.Equal(t, tc.message, err.Error())
			assert.False(t, errors.Is(err, context.Canceled))
			assert.Equal(t, tc.reason, msg.StopReason)
			assert.Equal(t, tc.message, *msg.ErrorMessage)
			last := items[len(items)-1].Event
			errEv, ok := last.(protocol.ErrorEvent)
			require.True(t, ok)
			assert.Equal(t, tc.reason, errEv.Reason)
			assert.Equal(t, "start", items[0].Event.EventType(), "a scripted failure streams its blocks first")
			if name != "Fail" {
				assert.Equal(t, protocol.Text{Text: "part"}, msg.Content[0])
			}
		})
	}
}

func TestIdentityRewriteAndInputScriptIsNotChanged(t *testing.T) {
	p := newProvider(t, WithAPI("api-x"), WithProvider("prov-x"), WithClock(newFakeClock()))
	step := Reply(Text("hello"), ToolCall("t", map[string]any{"a": 1}))
	blocks := append([]Block(nil), step.blocks...)
	p.Set(step, step)
	_, m1, _ := play(t, p)
	_, m2, _ := play(t, p)
	assert.Equal(t, "api-x", m1.API)
	assert.Equal(t, "prov-x", m1.Provider)
	assert.Equal(t, defaultModelID, m1.Model)
	assert.Equal(t, blocks, step.blocks, "the script is not changed")
	assert.Equal(t, "tool:1:1", m1.Content[1].(protocol.ToolCall).ID)
	assert.Equal(t, "tool:2:1", m2.Content[1].(protocol.ToolCall).ID, "ids are generated per call, not stored in the script")
	// Modifiers return copies.
	base := Say("x")
	_ = base.ResponseID("r").Stop(protocol.StopLength).Truncate(1)
	assert.Nil(t, base.respID)
	assert.Nil(t, base.stop)
	assert.Nil(t, base.truncate)
}

func TestEmptyBlocksGiveOneEmptyDelta(t *testing.T) {
	p := newProvider(t)
	p.Set(Reply(Text(""), Thinking("")))
	items, msg, err := play(t, p)
	require.NoError(t, err)
	assert.Equal(t, []string{"start", "text_start", "text_delta", "text_end", "thinking_start", "thinking_delta", "thinking_end", "done"}, kinds(items))
	assert.Equal(t, protocol.TextDeltaEvent{ContentIndex: 0, Delta: ""}, items[2].Event)
	assert.Equal(t, protocol.Text{Text: ""}, msg.Content[0])
	assert.Equal(t, protocol.Thinking{Thinking: ""}, msg.Content[1])
}

func TestMetadataAndTimestamp(t *testing.T) {
	clock := newFakeClock()
	p := newProvider(t, WithClock(clock))
	p.Set(Say("a").ResponseID("resp-1"), Say("b").Timestamp(0), Say("c").Timestamp(77), Say("d"))
	_, m1, _ := play(t, p)
	clock.Advance(5 * time.Second)
	_, m2, _ := play(t, p)
	_, m3, _ := play(t, p)
	_, m4, _ := play(t, p)
	assert.Equal(t, "resp-1", *m1.ResponseID)
	assert.Equal(t, fixedNow, m1.Timestamp)
	assert.Nil(t, m2.ResponseID)
	assert.Equal(t, int64(0), m2.Timestamp)
	assert.Equal(t, int64(77), m3.Timestamp)
	assert.Equal(t, fixedNow+5000, m4.Timestamp)
}

func TestChunksAreSeededAndOnRuneBoundaries(t *testing.T) {
	text := strings.Repeat("héllo wörld 😀 ", 20)
	chunksOf := func(seed int64) []string {
		p := newProvider(t, WithSeed(seed), WithChunk(1, 3))
		p.Set(Say(text))
		items, msg, err := play(t, p)
		require.NoError(t, err)
		assert.Equal(t, protocol.Text{Text: text}, msg.Content[0])
		var out []string
		for _, it := range items {
			if d, ok := it.Event.(protocol.TextDeltaEvent); ok {
				assert.True(t, utf8.ValidString(d.Delta), "chunk %q is valid UTF-8", d.Delta)
				assert.LessOrEqual(t, utf8.RuneCountInString(d.Delta), 12)
				out = append(out, d.Delta)
			}
		}
		assert.Equal(t, text, strings.Join(out, ""))
		return out
	}
	a := chunksOf(7)
	assert.Equal(t, a, chunksOf(7), "the same seed gives the same chunks")
	assert.NotEqual(t, a, chunksOf(8), "another seed gives other chunks")
}

func TestFixedScriptChunksDoNotDependOnScheduling(t *testing.T) {
	steps := []Step{Say(strings.Repeat("a", 90)), Say(strings.Repeat("b", 90)), Say(strings.Repeat("c", 90))}
	reference := func() [][]string {
		p := newProvider(t, WithChunk(1, 4))
		p.Set(steps...)
		var all [][]string
		for range steps {
			items, _, _ := play(t, p)
			all = append(all, deltas(items))
		}
		return all
	}()
	for range 5 {
		p := newProvider(t, WithChunk(1, 4))
		p.Set(steps...)
		streams := make([]*providers.Stream, len(steps))
		for i := range steps {
			streams[i] = start(t, p, context.Background())
		}
		got := make([][]string, len(steps))
		var wg sync.WaitGroup
		for i := len(steps) - 1; i >= 0; i-- {
			wg.Add(1)
			go func() {
				defer wg.Done()
				got[i] = deltas(drain(streams[i]))
			}()
		}
		wg.Wait()
		assert.Equal(t, reference, got)
	}
}

func deltas(items []providers.StreamItem) []string {
	var out []string
	for _, it := range items {
		if d, ok := it.Event.(protocol.TextDeltaEvent); ok {
			out = append(out, d.Delta)
		}
	}
	return out
}

func TestToolCallRawSendsExactBytes(t *testing.T) {
	p := newProvider(t, WithChunk(1, 1))
	broken := `{"a":1,"b":"x` // cut inside a string
	good := "{ \"k\" : \"😀😀😀😀😀\" }"
	p.Set(Reply(ToolCallRaw("broken", "r1", broken), ToolCallRaw("good", "", good)))
	items, msg, err := play(t, p)
	require.NoError(t, err)
	var raw []string
	for _, it := range items {
		if d, ok := it.Event.(protocol.ToolCallDeltaEvent); ok {
			raw = append(raw, d.Delta)
		}
	}
	assert.Equal(t, broken+good, strings.Join(raw, ""))
	c0 := msg.Content[0].(protocol.ToolCall)
	assert.Equal(t, "r1", c0.ID)
	assert.JSONEq(t, `{"a":1,"b":"x"}`, string(c0.Arguments), "broken JSON is repaired once at the end")
	c1 := msg.Content[1].(protocol.ToolCall)
	assert.Equal(t, "tool:1:1", c1.ID)
	assert.Equal(t, `{"k":"😀😀😀😀😀"}`, string(c1.Arguments), "valid JSON keeps its bytes, compacted")
	for _, it := range items {
		if d, ok := it.Event.(protocol.ToolCallDeltaEvent); ok && strings.Contains(good, d.Delta) {
			assert.True(t, utf8.ValidString(d.Delta))
		}
	}
}

func TestToolCallAuthoritativeEnd(t *testing.T) {
	type args struct {
		B int    `json:"b"`
		A string `json:"a"`
	}
	p := newProvider(t, WithChunk(1, 1))
	p.Set(Reply(ToolCall("t", args{B: 2, A: "<x>"})))
	items, msg, err := play(t, p)
	require.NoError(t, err)
	want := protocol.ToolCall{ID: "tool:1:1", Name: "t", Arguments: []byte(`{"b":2,"a":"<x>"}`)}
	end := items[len(items)-2].Event.(protocol.ToolCallEndEvent)
	assert.Equal(t, want, end.ToolCall, "the end event holds the script value, key order included")
	assert.Equal(t, want, msg.Content[0])
}

func TestToolCallArgumentsMustBeAnObject(t *testing.T) {
	for name, args := range map[string]any{"array": []int{1}, "string": "x", "number": 3, "unmarshalable": make(chan int)} {
		t.Run(name, func(t *testing.T) {
			p := newProvider(t)
			p.Set(Reply(ToolCall("t", args)))
			items, _, err := play(t, p)
			require.Error(t, err)
			assert.Equal(t, []string{"error"}, kinds(items), "a bad script fails before start")
		})
	}
}

func TestAbortBeforeStartGivesOneEvent(t *testing.T) {
	p := newProvider(t)
	p.Set(Say("never"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := start(t, p, ctx)
	items := drain(s)
	require.Equal(t, []string{"error"}, kinds(items))
	msg, err := result(t, s)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, protocol.StopAborted, msg.StopReason)
	assert.Equal(t, "Request was aborted", *msg.ErrorMessage)
	assert.Equal(t, defaultModelID, msg.Model)
	assert.Empty(t, msg.Content)
}

func TestAbortDuringChunkWaitDropsTheChunk(t *testing.T) {
	blocks := map[string]struct {
		block Block
		first string
		delta string
		want  protocol.AssistantBlock
	}{
		"text":     {Text("abcdefgh"), "text_start", "text_delta", protocol.Text{Text: "abcd"}},
		"thinking": {Thinking("abcdefgh"), "thinking_start", "thinking_delta", protocol.Thinking{Thinking: "abcd"}},
		"tool":     {ToolCallRaw("t", "id1", `{"a":"bcdefg"}`), "toolcall_start", "toolcall_delta", protocol.ToolCall{ID: "id1", Name: "t"}},
	}
	for name, tc := range blocks {
		t.Run(name, func(t *testing.T) {
			clock := newFakeClock()
			p := newProvider(t, WithClock(clock), WithChunk(1, 1), WithTokensPerSecond(4))
			p.Set(Reply(tc.block))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := start(t, p, ctx)
			clock.waitArmed(t)
			clock.Advance(250 * time.Millisecond)
			clock.waitArmed(t) // the second chunk is waiting
			cancel()
			items := drain(s)
			assert.Equal(t, []string{"start", tc.first, tc.delta, "error"}, kinds(items), "no *_end and no second delta")
			msg, err := result(t, s)
			assert.ErrorIs(t, err, context.Canceled)
			assert.Equal(t, protocol.StopAborted, msg.StopReason)
			require.Len(t, msg.Content, 1)
			if call, ok := tc.want.(protocol.ToolCall); ok {
				got := msg.Content[0].(protocol.ToolCall)
				assert.Equal(t, call.ID, got.ID)
				assert.Equal(t, call.Name, got.Name)
				assert.JSONEq(t, `{}`, string(got.Arguments), "the open call has no complete argument object")
			} else {
				assert.Equal(t, tc.want, msg.Content[0])
			}
		})
	}
}

func TestAbortBetweenBlocksAndDuringDelay(t *testing.T) {
	t.Run("before the next block", func(t *testing.T) {
		clock := newFakeClock()
		p := newProvider(t, WithClock(clock), WithChunk(1, 1), WithTokensPerSecond(4))
		p.Set(Reply(Text("abcd"), Text("efgh")))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := start(t, p, ctx)
		clock.waitArmed(t)
		cancel()
		items := drain(s)
		assert.Equal(t, []string{"start", "text_start", "error"}, kinds(items))
	})
	t.Run("during the step delay", func(t *testing.T) {
		clock := newFakeClock()
		p := newProvider(t, WithClock(clock))
		p.Set(Say("x").Delay(time.Second))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := start(t, p, ctx)
		clock.waitArmed(t)
		cancel()
		assert.Equal(t, []string{"error"}, kinds(drain(s)))
		_, err := result(t, s)
		assert.ErrorIs(t, err, context.Canceled)
	})
	t.Run("delay ends then the reply streams", func(t *testing.T) {
		clock := newFakeClock()
		p := newProvider(t, WithClock(clock))
		p.Set(Say("x").Delay(time.Second))
		s := start(t, p, context.Background())
		clock.waitArmed(t)
		clock.Advance(time.Second)
		assert.Equal(t, "start", (<-s.Events()).Event.EventType())
		drain(s)
	})
}

func TestPaceModifierOverridesProviderPace(t *testing.T) {
	clock := newFakeClock()
	p := newProvider(t, WithClock(clock), WithChunk(1, 1), WithTokensPerSecond(1))
	p.Set(Say("abcd").Pace(0), Say("abcd").Pace(2))
	_, msg, err := play(t, p) // Pace(0) never waits, so no clock advance is needed
	require.NoError(t, err)
	assert.Equal(t, protocol.Text{Text: "abcd"}, msg.Content[0])
	s := start(t, p, context.Background())
	clock.waitArmed(t)
	clock.Advance(499 * time.Millisecond)
	select {
	case <-s.Events():
		// Only start and text_start can come before the chunk wait ends.
		<-s.Events()
		select {
		case it := <-s.Events():
			t.Fatalf("a delta came before the pace time: %v", it.Event.EventType())
		case <-time.After(20 * time.Millisecond):
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no start event")
	}
	clock.Advance(time.Millisecond)
	drain(s)
}

func TestRawEvents(t *testing.T) {
	seed := protocol.AssistantMessage{API: "a", Provider: "p", Model: "m", Timestamp: 3, StopReason: protocol.StopPending}
	final := seed
	final.Content = []protocol.AssistantBlock{protocol.Text{Text: "hi"}}
	final.StopReason = protocol.StopStop
	t.Run("valid sequence settles with its own message", func(t *testing.T) {
		p := newProvider(t)
		p.Set(Raw(
			protocol.StartEvent{Message: seed},
			protocol.TextStartEvent{ContentIndex: 0, Content: protocol.Text{Text: "hi"}},
			protocol.TextEndEvent{ContentIndex: 0, Content: "hi"},
			protocol.DoneEvent{Reason: protocol.StopStop, Message: final},
		))
		items, msg, err := play(t, p)
		require.NoError(t, err)
		assert.Equal(t, []string{"start", "text_start", "text_end", "done"}, kinds(items))
		assert.Equal(t, final, msg)
	})
	t.Run("invalid event fails the stream with the builder error", func(t *testing.T) {
		p := newProvider(t)
		p.Set(Raw(protocol.StartEvent{Message: seed}, protocol.TextDeltaEvent{ContentIndex: 4, Delta: "x"}))
		items, msg, err := play(t, p)
		require.Error(t, err)
		assert.Equal(t, []string{"start", "error"}, kinds(items))
		assert.Equal(t, protocol.StopError, msg.StopReason)
		assert.Equal(t, err.Error(), *msg.ErrorMessage)
	})
	t.Run("no terminal event is incomplete", func(t *testing.T) {
		p := newProvider(t)
		p.Set(Raw(protocol.StartEvent{Message: seed}))
		_, msg, err := play(t, p)
		assert.ErrorIs(t, err, providers.ErrStreamIncomplete)
		assert.Equal(t, protocol.StopError, msg.StopReason)
	})
	t.Run("events after a raw terminal event are ignored", func(t *testing.T) {
		p := newProvider(t)
		p.Set(Raw(
			protocol.ErrorEvent{Reason: protocol.StopError, Error: errMsg(seed, "raw failure")},
			protocol.StartEvent{Message: seed},
		))
		items, msg, err := play(t, p)
		assert.Equal(t, []string{"error"}, kinds(items))
		require.Error(t, err)
		assert.Equal(t, "raw failure", *msg.ErrorMessage)
	})
}

func errMsg(m protocol.AssistantMessage, text string) protocol.AssistantMessage {
	m.StopReason = protocol.StopError
	m.ErrorMessage = &text
	return m
}

func TestConcurrentCallsAndInstanceIsolation(t *testing.T) {
	const n = 24
	p := newProvider(t)
	other := newProvider(t)
	steps := make([]Step, n)
	for i := range steps {
		steps[i] = Say(fmt.Sprintf("reply-%d", i))
	}
	p.Set(steps...)
	other.Set(Say("other"))
	var wg sync.WaitGroup
	texts := make([]string, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := start(t, p, context.Background(), providers.StreamOptions{SessionID: "shared"})
			drain(s)
			msg, err := s.Result(context.Background())
			if assert.NoError(t, err) {
				texts[i] = msg.Content[0].(protocol.Text).Text
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, n, p.Calls())
	assert.Equal(t, 0, p.Pending())
	assert.Equal(t, 1, other.Pending(), "another instance is not touched")
	assert.Equal(t, 0, other.Calls())
	seen := map[string]bool{}
	for _, s := range texts {
		seen[s] = true
	}
	assert.Len(t, seen, n, "every step was taken exactly once")
	for i, r := range p.Requests() {
		assert.Equal(t, i+1, r.Call)
	}
}

func TestBufferFloorLetsResultSettleWithoutReader(t *testing.T) {
	p := newProvider(t, WithBuffer(1))
	p.Set(Say("a short reply"))
	s := start(t, p, context.Background())
	msg, err := result(t, s) // no reader: the buffer holds the whole short reply
	require.NoError(t, err)
	assert.Equal(t, protocol.Text{Text: "a short reply"}, msg.Content[0])
	drain(s)
}

func TestFuncStepDoesNotChangeLaterReplyChunksAndIds(t *testing.T) {
	reply := Reply(Text(strings.Repeat("b", 90)), ToolCall("t", map[string]int{"n": 1}))
	inner := Say("x")
	run := func(first Step, concurrent bool) (chunks []string, id string) {
		p := newProvider(t, WithChunk(1, 4))
		p.Set(first, reply)
		var items []providers.StreamItem
		if concurrent {
			s1 := start(t, p, context.Background())
			s2 := start(t, p, context.Background())
			done := make(chan struct{})
			go func() { drain(s1); close(done) }()
			items = drain(s2)
			<-done
		} else {
			_, _, _ = play(t, p)
			items, _, _ = play(t, p)
		}
		for _, it := range items {
			switch e := it.Event.(type) {
			case protocol.TextDeltaEvent:
				chunks = append(chunks, e.Delta)
			case protocol.ToolCallStartEvent:
				id = e.ID
			}
		}
		return chunks, id
	}
	fn := Func(func(context.Context, Call) (Step, error) { return inner, nil })
	wantChunks, wantID := run(Say("a"), false)
	require.NotEmpty(t, wantChunks)
	for range 10 {
		for _, concurrent := range []bool{false, true} {
			gotChunks, gotID := run(fn, concurrent)
			assert.Equal(t, wantChunks, gotChunks)
			assert.Equal(t, wantID, gotID)
		}
	}
}

func TestRequestRecordKeepsPointerMessages(t *testing.T) {
	p := newProvider(t)
	p.Set(Func(func(_ context.Context, c Call) (Step, error) {
		if _, ok := c.Request.Messages[0].(*protocol.UserMessage); !ok {
			return Step{}, fmt.Errorf("factory got %T", c.Request.Messages[0])
		}
		return Say("ok"), nil
	}))
	s, err := p.Stream(context.Background(), providers.Model{ID: defaultModelID}, providers.TranscriptRequest{
		Messages: []protocol.Message{&protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}}},
	}, providers.StreamOptions{}), error(nil)
	require.NoError(t, err)
	drain(s)
	_, rerr := result(t, s)
	require.NoError(t, rerr)
	assert.IsType(t, &protocol.UserMessage{}, p.Requests()[0].Transcript.Messages[0])
}

func TestFauxStepErrReturnsTypedFailure(t *testing.T) {
	want := providers.NewFailure(providers.CodeRateLimit, 429, 9*time.Second, "slow down", nil)
	p := newProvider(t)
	p.Set(Say("partial").Err(want), Fail("plain failure"))

	_, msg, err := play(t, p)
	require.Error(t, err)
	got, ok := providers.AsFailure(err)
	require.True(t, ok, "errors.As finds the typed failure")
	assert.Equal(t, providers.CodeRateLimit, got.Code)
	assert.Equal(t, 429, got.Status)
	assert.Equal(t, 9*time.Second, got.RetryAfter)
	assert.ErrorIs(t, err, providers.ErrRateLimited)
	assert.Equal(t, protocol.StopError, msg.StopReason)
	require.NotNil(t, msg.ErrorMessage)
	assert.Equal(t, "slow down", *msg.ErrorMessage)

	// A step that has no typed failure ends with a plain error: no code.
	_, _, err = play(t, p)
	require.Error(t, err)
	_, typed := providers.AsFailure(err)
	assert.False(t, typed)
	assert.Equal(t, providers.CodeUnknown, providers.CodeOf(err))
}

func TestRequestRecordsClonePreparedPolicyAndRequiredBinding(t *testing.T) {
	p, err := New()
	require.NoError(t, err)
	m, ok := p.Model("faux-1")
	require.True(t, ok)
	p.Set(Say("hello"))
	prepared := &providers.Prepared{Provider: m.Provider, API: string(m.API), Model: m.ID, RetryPolicy: providers.DefaultRetryPolicy()}
	binding := &providers.AuthBinding{Provider: m.Provider, Method: "api-key", Profile: "api-key", BillingHint: "api-key"}
	s := p.Stream(context.Background(), m, req(userMsg("hi")), providers.StreamOptions{Prepared: prepared, RequireBinding: binding})
	for range s.Events() {
	}
	_, err = s.Result(context.Background())
	require.NoError(t, err)
	prepared.RetryPolicy.Key = "changed"
	binding.BillingHint = "changed"
	records := p.Requests()
	require.Equal(t, "default", records[0].Options.Prepared.RetryPolicy.Key)
	require.Equal(t, "api-key", records[0].Options.RequireBinding.BillingHint)
	records[0].Options.Prepared.RetryPolicy.Key = "caller changed"
	records[0].Options.RequireBinding.BillingHint = "caller changed"
	require.Equal(t, "default", p.Requests()[0].Options.Prepared.RetryPolicy.Key)
	require.Equal(t, "api-key", p.Requests()[0].Options.RequireBinding.BillingHint)
}
