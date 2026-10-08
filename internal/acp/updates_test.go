package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/bus"
	"AskCore/internal/providers/faux"
	"AskCore/pkg/protocol"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

func marshalUpdate(t *testing.T, u sdk.SessionUpdate) map[string]any {
	t.Helper()
	b, err := json.Marshal(u)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	return m
}

func blockEvent(b protocol.BlockEvent) protocol.Event {
	return &protocol.MessageUpdate{AssistantMessageEvent: b}
}

func TestACPUpdatesTextAndThinking(t *testing.T) {
	ups := standardUpdates(blockEvent(protocol.TextDeltaEvent{Delta: "hello"}))
	require.Len(t, ups, 1)
	m := marshalUpdate(t, ups[0])
	require.Equal(t, "agent_message_chunk", m["sessionUpdate"])
	require.Equal(t, "hello", m["content"].(map[string]any)["text"])

	ups = standardUpdates(blockEvent(protocol.ThinkingDeltaEvent{Delta: "hmm"}))
	require.Len(t, ups, 1)
	require.Equal(t, "agent_thought_chunk", marshalUpdate(t, ups[0])["sessionUpdate"])

	ups = standardUpdates(blockEvent(protocol.TextStartEvent{Content: protocol.Text{Text: "seed"}}))
	require.Len(t, ups, 1)
	require.Equal(t, "seed", marshalUpdate(t, ups[0])["content"].(map[string]any)["text"])

	require.Empty(t, standardUpdates(blockEvent(protocol.TextStartEvent{})), "an empty seed gives no frame")
	require.Empty(t, standardUpdates(blockEvent(protocol.TextDeltaEvent{})), "an empty delta gives no frame")
	require.Empty(t, standardUpdates(blockEvent(protocol.TextEndEvent{Content: "x"})))
}

func TestACPUpdatesToolLifecycle(t *testing.T) {
	ups := standardUpdates(blockEvent(protocol.ToolCallStartEvent{ID: "c1", ToolName: "echo", Arguments: json.RawMessage(`{"a":`)}))
	require.Len(t, ups, 1)
	m := marshalUpdate(t, ups[0])
	require.Equal(t, "tool_call", m["sessionUpdate"])
	require.Equal(t, "c1", m["toolCallId"])
	require.Equal(t, "echo", m["title"])
	require.Equal(t, "pending", m["status"])
	require.NotContains(t, m, "rawInput", "an unfinished argument text is not valid JSON")

	ups = standardUpdates(blockEvent(protocol.ToolCallEndEvent{ToolCall: protocol.ToolCall{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{"a":1}`)}}))
	require.Len(t, ups, 1)
	m = marshalUpdate(t, ups[0])
	require.Equal(t, "tool_call_update", m["sessionUpdate"])
	require.Equal(t, map[string]any{"a": float64(1)}, m["rawInput"])

	ups = standardUpdates(&protocol.ToolExecutionStart{ToolCallID: "c1", ToolName: "echo"})
	require.Len(t, ups, 1)
	require.Equal(t, "in_progress", marshalUpdate(t, ups[0])["status"])

	res := protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: "out"}}}
	ups = standardUpdates(&protocol.ToolExecutionEnd{ToolCallID: "c1", ToolName: "echo", Result: res})
	require.Len(t, ups, 1)
	m = marshalUpdate(t, ups[0])
	require.Equal(t, "completed", m["status"])
	content := m["content"].([]any)
	require.Len(t, content, 1)
	require.Equal(t, "out", content[0].(map[string]any)["content"].(map[string]any)["text"])

	ups = standardUpdates(&protocol.ToolExecutionEnd{ToolCallID: "c1", Result: res, IsError: true})
	require.Equal(t, "failed", marshalUpdate(t, ups[0])["status"])
}

func TestACPUpdatesLifecycleEventsHaveNoStandardForm(t *testing.T) {
	for _, ev := range []protocol.Event{&protocol.AgentStart{}, &protocol.AgentSettled{}, &protocol.QueueUpdate{}, &protocol.AutoRetryStart{}, &protocol.AttemptEnd{}, &protocol.CycleEnd{}} {
		require.Empty(t, standardUpdates(ev), ev.EventType())
	}
}

func TestACPMetaCarriesDecimalCounters(t *testing.T) {
	env := protocol.Envelope{Seq: 1<<53 + 1, RunID: "r1", SessionID: "s1"}
	ids := eventIDs{cycleID: "cy", attemptID: "at"}
	m := frameMeta("ep", env, ids, 1, 3)
	b, err := json.Marshal(m)
	require.NoError(t, err)
	require.JSONEq(t, `{"ask":{"epoch":"ep","seq":"9007199254740993","runId":"r1","cycleId":"cy","attemptId":"at","frameIndex":1,"frameCount":3}}`, string(b))
	m = frameMeta("ep", protocol.Envelope{Seq: 2}, eventIDs{}, 0, 1)
	b, _ = json.Marshal(m)
	require.JSONEq(t, `{"ask":{"epoch":"ep","seq":"2","frameIndex":0,"frameCount":1}}`, string(b))
}

func TestACPMetaIdentifiersFollowEvents(t *testing.T) {
	var ids eventIDs
	ids.note(&protocol.CycleStart{CycleID: "c1"})
	ids.note(&protocol.AttemptStart{AttemptID: "a1", CycleID: "c1"})
	require.Equal(t, eventIDs{cycleID: "c1", attemptID: "a1"}, ids)
	ids.note(&protocol.AttemptEnd{AttemptID: "a1"})
	require.Equal(t, eventIDs{cycleID: "c1"}, ids)
	ids.note(&protocol.CycleEnd{CycleID: "c1"})
	require.Equal(t, eventIDs{}, ids)
}

// followEvents decodes the _ask/session/event notifications of one subscription.
func (p *adapterPeer) followEvents(subID string) []protocol.ACPEventNotification {
	var out []protocol.ACPEventNotification
	for _, n := range p.notes(protocol.ACPEvent) {
		var ev protocol.ACPEventNotification
		if err := json.Unmarshal(n.Params, &ev); err == nil && ev.SubscriptionID == subID {
			out = append(out, ev)
		}
	}
	return out
}

func eventType(t *testing.T, ev protocol.ACPEventNotification) string {
	t.Helper()
	var head struct{ Type string }
	require.NoError(t, json.Unmarshal(ev.Event, &head))
	return head.Type
}

func (p *adapterPeer) follow(sid string, cursor *protocol.ACPCursor) protocol.ACPFollowResult {
	p.t.Helper()
	params := map[string]any{"sessionId": sid}
	if cursor != nil {
		params["cursor"] = cursor
	}
	var res protocol.ACPFollowResult
	p.ok(protocol.ACPFollow, params, &res)
	require.NotEmpty(p.t, res.SubscriptionID)
	return res
}

func TestACPFollowSnapshotThenOrderedEventsAfterResult(t *testing.T) {
	p := newAdapterPeer(t, testConfig(says("one", "two"), nil), nil)
	sid := p.start()
	p.ok("session/prompt", promptParams(sid, "first"), nil)

	id := p.send(protocol.ACPFollow, map[string]any{"sessionId": sid})
	f := p.await(id)
	require.Nil(t, f.Error)
	var res protocol.ACPFollowResult
	require.NoError(t, json.Unmarshal(f.Result, &res))
	require.NotEmpty(t, res.Entries, "a fresh follow carries the snapshot")
	require.False(t, res.Resumed)
	require.False(t, res.Resync)
	require.Nil(t, res.Stream)
	require.NotZero(t, res.Cursor.Seq)
	for _, raw := range res.Entries {
		require.NotContains(t, string(raw), `"systemPrompt"`, "request data is not part of the safe projection")
		require.NotContains(t, string(raw), `"prepared"`)
	}

	p.ok("session/prompt", promptParams(sid, "second"), nil)
	p.waitFor("settled event", func() bool {
		evs := p.followEvents(res.SubscriptionID)
		return len(evs) > 0 && eventType(t, evs[len(evs)-1]) == protocol.TypeAgentSettled
	})
	evs := p.followEvents(res.SubscriptionID)
	require.Greater(t, p.lastNoteIndex(protocol.ACPEvent), p.responseIndex(id))
	for i, ev := range evs {
		require.Equal(t, res.Cursor.Seq+uint64(i)+1, ev.Seq, "events continue strictly after the cut with no gap")
		require.Equal(t, res.Cursor.Epoch, ev.Epoch)
		require.Equal(t, sid, ev.SessionID)
		if i > 0 {
			require.NotEmpty(t, ev.RunID)
		}
		var env struct{ Seq uint64 }
		require.NoError(t, json.Unmarshal(ev.Event, &env))
		require.Equal(t, ev.Seq, env.Seq)
	}
	types := make([]string, len(evs))
	for i, ev := range evs {
		types[i] = eventType(t, ev)
	}
	require.Equal(t, protocol.TypeAgentStart, types[0])
	require.Contains(t, types, protocol.TypeAttemptStart)
	require.Contains(t, types, protocol.TypeAttemptEnd)
	for _, ev := range evs {
		if eventType(t, ev) == protocol.TypeAttemptStart {
			require.NotEmpty(t, ev.AttemptID)
			require.NotEmpty(t, ev.CycleID)
		}
	}
	// A subscriber does not duplicate the standard updates.
	updates := 0
	for _, k := range p.updateKinds() {
		if k == "agent_message_chunk" {
			updates++
		}
	}
	require.Equal(t, 2, updates)
}

func TestACPFollowResumeAndResyncCursors(t *testing.T) {
	p := newAdapterPeer(t, testConfig(says("one", "two"), nil), nil)
	sid := p.start()
	p.ok("session/prompt", promptParams(sid, "first"), nil)
	first := p.follow(sid, nil)
	p.ok("session/prompt", promptParams(sid, "second"), nil)

	// A held cursor resumes with the events after it and no snapshot.
	resumed := p.follow(sid, &first.Cursor)
	require.True(t, resumed.Resumed)
	require.False(t, resumed.Resync)
	require.Empty(t, resumed.Entries)
	require.Equal(t, first.Cursor, resumed.Cursor)
	p.waitFor("replayed events", func() bool { return len(p.followEvents(resumed.SubscriptionID)) > 0 })
	evs := p.followEvents(resumed.SubscriptionID)
	require.Equal(t, first.Cursor.Seq+1, evs[0].Seq)

	for name, bad := range map[string]protocol.ACPCursor{
		"wrong epoch": {Epoch: "other", Seq: first.Cursor.Seq},
		"future seq":  {Epoch: first.Cursor.Epoch, Seq: first.Cursor.Seq + 100000},
	} {
		res := p.follow(sid, &bad)
		require.True(t, res.Resync, name)
		require.False(t, res.Resumed, name)
		require.NotEmpty(t, res.Entries, name)
		require.Equal(t, first.Cursor.Epoch, res.Cursor.Epoch, name)
	}
	f := p.call(protocol.ACPFollow, map[string]any{"sessionId": "sess_missing"})
	require.Equal(t, protocol.ACPErrUnknownSession, f.errKind(t))
}

func TestACPFollowOpenStreamBaselinePreservesToolArguments(t *testing.T) {
	gate := newGateClock(3)
	args := `{"path":"/tmp/some/long/file/name.txt","content":"` + strings.Repeat("abcdefghij", 30) + `"}`
	cfg := testConfig(func() []faux.Step {
		return []faux.Step{faux.Reply(faux.ToolCallRaw("write", "call-1", args)).Pace(1), faux.Say("done")}
	}, nil, faux.WithClock(gate), faux.WithChunk(2, 2))
	p := newAdapterPeer(t, cfg, nil)
	t.Cleanup(gate.open)
	sid := p.start()
	id := p.send("session/prompt", promptParams(sid, "go"))
	select {
	case <-gate.waiting:
	case <-time.After(peerWait):
		t.Fatal("stream never paused")
	}
	s, _ := p.a.host.Session(sid)
	require.Eventually(t, func() bool {
		f := s.Agent().Follow(agent.Cursor{})
		f.Events.Close()
		return f.Stream != nil && len(f.Stream.Open[0]) > 0
	}, peerWait, time.Millisecond, "the Agent published the open arguments")
	res := p.follow(sid, nil)
	require.NotNil(t, res.Stream, "an assistant message is open at the cut")
	require.NotEmpty(t, res.Stream.AttemptID)
	require.Equal(t, res.Cursor.Seq, res.Stream.Seq)
	require.NotEmpty(t, res.Stream.Open[0], "the raw bytes of open blocks travel with the baseline")
	require.True(t, strings.HasPrefix(args, string(res.Stream.Open[0])), "the baseline holds an unfinished prefix of the arguments")
	require.NotEqual(t, args, string(res.Stream.Open[0]))

	gate.open()
	f := p.await(id)
	require.Nil(t, f.Error, "%+v", f.Error)
	p.waitFor("settled", func() bool {
		evs := p.followEvents(res.SubscriptionID)
		return len(evs) > 0 && eventType(t, evs[len(evs)-1]) == protocol.TypeAgentSettled
	})
	evs := p.followEvents(res.SubscriptionID)
	require.Equal(t, res.Cursor.Seq+1, evs[0].Seq, "the first event follows the cut")

	// Baseline plus later events rebuild the final assistant message exactly.
	b := protocol.NewBuilder()
	require.NoError(t, b.ResumeFrom(protocol.Partial{Message: res.Stream.Message, Open: res.Stream.Open}))
	var final *protocol.AssistantMessage
	for _, ev := range evs {
		decoded, err := protocol.DecodeEvent(ev.Event)
		require.NoError(t, err)
		switch e := decoded.(type) {
		case *protocol.MessageUpdate:
			require.NoError(t, b.ApplyAgentEvent(e))
		case *protocol.MessageEnd:
			if am, ok := e.Message.(protocol.AssistantMessage); ok && final == nil {
				final = &am
			}
		}
		if final != nil {
			break
		}
	}
	require.NotNil(t, final, "the open message ended in the stream")
	rebuilt := b.Snapshot()
	require.Len(t, rebuilt.Content, 1)
	require.Len(t, final.Content, 1)
	got, ok := rebuilt.Content[0].(protocol.ToolCall)
	require.True(t, ok)
	want, ok := final.Content[0].(protocol.ToolCall)
	require.True(t, ok)
	require.JSONEq(t, args, string(want.Arguments))
	require.JSONEq(t, args, string(got.Arguments), "no byte of the arguments was lost or repeated")
}

func TestACPFollowHoldsLiveEventsUntilResultIsWritten(t *testing.T) {
	cfg := testConfig(says("first", "second"), nil)
	a, err := NewAdapter(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = a.Close() })
	fake := &fakeNotifier{}
	a.bindNotifier(fake, nil)
	_, err = a.Initialize(context.Background(), sdk.InitializeRequest{ProtocolVersion: 1})
	require.NoError(t, err)
	ns, err := a.NewSession(context.Background(), sdk.NewSessionRequest{Cwd: t.TempDir(), McpServers: []sdk.McpServer{}})
	require.NoError(t, err)
	sid := string(ns.SessionId)

	raw, err := a.HandleExtensionMethod(context.Background(), protocol.ACPFollow, json.RawMessage(`{"sessionId":"`+sid+`"}`))
	require.NoError(t, err)
	res := raw.(protocol.ACPFollowResult)
	promptDone := make(chan error, 1)
	go func() {
		_, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: ns.SessionId, Prompt: []sdk.ContentBlock{sdk.TextBlock("go")}})
		promptDone <- err
	}()
	time.Sleep(50 * time.Millisecond)
	require.Zero(t, fake.extensionCount(protocol.ACPEvent), "no live event before the follow result is on the wire")

	// A frame that only quotes the ID does not release the events.
	quote, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 8, "method": "session/prompt", "params": map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": `"result" ` + res.SubscriptionID}}}})
	a.frameWritten(quote)
	other, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 7, "result": map[string]any{"note": res.SubscriptionID}})
	a.frameWritten(other)
	time.Sleep(50 * time.Millisecond)
	require.Zero(t, fake.extensionCount(protocol.ACPEvent), "only the response that holds the ID in its result releases the events")

	frame, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 9, "result": res})
	select {
	case <-promptDone:
		t.Fatal("prompt completed before the followed events could be written")
	default:
	}
	a.frameWritten(frame)
	select {
	case err := <-promptDone:
		require.NoError(t, err)
	case <-time.After(peerWait):
		t.Fatal("prompt did not complete after follow release")
	}
	require.Eventually(t, func() bool { return fake.extensionCount(protocol.ACPEvent) > 3 }, peerWait, time.Millisecond)
}

func TestACPUnfollowStopsSubscriptionWithoutAffectingPrompt(t *testing.T) {
	p := newAdapterPeer(t, testConfig(says("one", "two"), nil), nil)
	sid := p.start()
	res := p.follow(sid, nil)
	p.ok("session/prompt", promptParams(sid, "first"), nil)
	p.waitFor("events", func() bool { return len(p.followEvents(res.SubscriptionID)) > 3 })
	var settled bool
	p.waitFor("settled", func() bool {
		evs := p.followEvents(res.SubscriptionID)
		settled = eventType(t, evs[len(evs)-1]) == protocol.TypeAgentSettled
		return settled
	})

	p.ok(protocol.ACPUnfollow, map[string]any{"sessionId": sid, "subscriptionId": res.SubscriptionID}, nil)
	count := len(p.followEvents(res.SubscriptionID))
	id := p.send("session/prompt", promptParams(sid, "second"))
	f := p.await(id)
	require.Nil(t, f.Error)
	require.Greater(t, p.responseIndex(id), p.lastNoteIndex("session/update"), "the barrier still holds")
	require.Equal(t, count, len(p.followEvents(res.SubscriptionID)), "no event after unfollow")
	f = p.call(protocol.ACPUnfollow, map[string]any{"sessionId": sid, "subscriptionId": res.SubscriptionID})
	require.Nil(t, f.Error)
	require.JSONEq(t, `{}`, string(f.Result))
	f = p.call(protocol.ACPUnfollow, map[string]any{"sessionId": "sess_missing", "subscriptionId": res.SubscriptionID})
	require.Equal(t, protocol.ACPErrUnknownSession, f.errKind(t))
}

func TestACPFollowOversizedEventGivesExplicitResync(t *testing.T) {
	cfg := testConfig(says(strings.Repeat("long answer ", 40)), func(c *agent.Config) { c.FollowLimits = bus.Limits{EventBytes: 400} })
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	res := p.follow(sid, nil)
	p.ok("session/prompt", promptParams(sid, "go"), nil)
	p.waitFor("resync", func() bool { return len(p.notes(protocol.ACPResync)) == 1 })
	var rs protocol.ACPResyncNotification
	require.NoError(t, json.Unmarshal(p.notes(protocol.ACPResync)[0].Params, &rs))
	require.Equal(t, res.SubscriptionID, rs.SubscriptionID)
	require.Equal(t, sid, rs.SessionID)
	require.NotEmpty(t, rs.Reason)
	// The subscription is over. A cursor from before the gap still resumes, and
	// its subscription reaches the gap and resyncs again.
	again := p.follow(sid, &res.Cursor)
	require.True(t, again.Resumed)
	p.waitFor("second resync", func() bool { return len(p.notes(protocol.ACPResync)) == 2 })
	// A fresh follow takes the full state without a resend of the prompt.
	fresh := p.follow(sid, nil)
	require.NotEmpty(t, fresh.Entries)
	require.Equal(t, 2, p.state(sid).MessageCount, "the prompt was not sent again")
}

func TestACPFollowSlowConsumerOverflowResyncsAndKeepsPromptBarrier(t *testing.T) {
	hold := make(chan struct{})
	release := closeOnce(hold)
	t.Cleanup(release)
	cfg := testConfig(says(strings.Repeat("word ", 50)), func(c *agent.Config) { c.FollowLimits = bus.Limits{Buffer: 2} }, faux.WithChunk(1, 1))
	p := newAdapterPeer(t, cfg, func(w io.Writer) io.Writer { return gatedWriter{w: w, hold: hold, match: []byte(protocol.ACPEvent)} })
	sid := p.start()
	res := p.follow(sid, nil)
	id := p.send("session/prompt", promptParams(sid, "go"))
	s, _ := p.a.host.Session(sid)
	require.NoError(t, s.Agent().WaitForIdle(hostDeadline(t)), "a stalled subscriber does not stall the run")
	release()
	f := p.await(id)
	require.Nil(t, f.Error, "%+v", f.Error)
	p.waitFor("resync", func() bool { return len(p.notes(protocol.ACPResync)) == 1 })
	var rs protocol.ACPResyncNotification
	require.NoError(t, json.Unmarshal(p.notes(protocol.ACPResync)[0].Params, &rs))
	require.Equal(t, res.SubscriptionID, rs.SubscriptionID)
	require.Greater(t, p.responseIndex(id), p.lastNoteIndex("session/update"))
}

// fakeNotifier records outbound frames for tests that call the adapter directly.
type fakeNotifier struct {
	mu      sync.Mutex
	updates []sdk.SessionNotification
	exts    []string
}

func (f *fakeNotifier) SessionUpdate(_ context.Context, n sdk.SessionNotification) error {
	f.mu.Lock()
	f.updates = append(f.updates, n)
	f.mu.Unlock()
	return nil
}

func (f *fakeNotifier) NotifyExtension(_ context.Context, method string, _ any) error {
	f.mu.Lock()
	f.exts = append(f.exts, method)
	f.mu.Unlock()
	return nil
}

func (f *fakeNotifier) extensionCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, m := range f.exts {
		if m == method {
			n++
		}
	}
	return n
}

// gatedWriter holds a write whose frame holds match until hold closes.
type gatedWriter struct {
	w     io.Writer
	hold  <-chan struct{}
	match []byte
}

func (g gatedWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, g.match) {
		<-g.hold
	}
	return g.w.Write(p)
}
