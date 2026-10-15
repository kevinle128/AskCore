package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func textParams(sid, text string) map[string]any {
	return map[string]any{"sessionId": sid, "content": []any{map[string]any{"type": "text", "text": text}}}
}

func (p *adapterPeer) state(sid string) protocol.ACPStateResult {
	p.t.Helper()
	var st protocol.ACPStateResult
	p.ok(protocol.ACPState, map[string]any{"sessionId": sid}, &st)
	return st
}

func (p *adapterPeer) running(sid string) {
	p.t.Helper()
	s, _ := p.a.host.Session(sid)
	require.Eventually(p.t, func() bool { return s.Agent().State().Status == agent.Running }, peerWait, time.Millisecond)
}

func TestAdapterStateIsACopyOfCurrentState(t *testing.T) {
	release := make(chan struct{})
	rel := closeOnce(release)
	t.Cleanup(rel)
	p := newAdapterPeer(t, testConfig(func() []faux.Step { return []faux.Step{heldStep(release, "answer")} }, nil), nil)
	sid := p.start()
	idle := p.state(sid)
	require.False(t, idle.Running)
	require.Equal(t, sid, idle.SessionID)
	require.NotEmpty(t, idle.Epoch)
	require.Equal(t, 0, idle.MessageCount)
	require.Equal(t, protocol.ThinkingOff, idle.ThinkingLevel)
	raw := p.call(protocol.ACPState, map[string]any{"sessionId": sid})
	require.Contains(t, string(raw.Result), `"steering":[]`)
	require.Contains(t, string(raw.Result), `"followUp":[]`)

	id := p.send("session/prompt", promptParams(sid, "go"))
	p.running(sid)
	require.True(t, p.state(sid).Running)
	rel()
	p.await(id)
	done := p.state(sid)
	require.False(t, done.Running)
	require.Equal(t, 2, done.MessageCount)
	require.Equal(t, idle.Epoch, done.Epoch)
	f := p.call(protocol.ACPState, map[string]any{"sessionId": "sess_missing"})
	require.Equal(t, protocol.ACPErrUnknownSession, f.errKind(t))
	f = p.call(protocol.ACPState, map[string]any{})
	require.Equal(t, protocol.ACPErrUnknownSession, f.errKind(t))
	f = p.call(protocol.ACPState, "not an object")
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
}

func TestAdapterCatalogRoundTripsThroughSetModel(t *testing.T) {
	p := newAdapterPeer(t, testConfig(says("x"), nil), nil)
	sid := p.start()
	var cat protocol.ACPModelsResult
	p.ok(protocol.ACPModels, map[string]any{"sessionId": sid}, &cat)
	require.Len(t, cat.Models, len(providers.AvailableModels()))
	st := p.state(sid)
	require.Equal(t, st.ModelID, cat.Current)
	require.Contains(t, cat.Current, "faux/faux-1@", "the current model is reported apart from the catalog rows")
	seen := map[string]bool{}
	for _, m := range cat.Models {
		require.False(t, seen[m.ModelID], "duplicate %s", m.ModelID)
		seen[m.ModelID] = true
		require.Equal(t, fmt.Sprintf("%s/%s@%s", m.Provider, m.ID, m.API), m.ModelID)
		require.NotEqual(t, cat.Current, m.ModelID)
		var set protocol.ACPSetModelResult
		p.ok(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": m.ModelID}, &set)
		require.Equal(t, m.ModelID, set.ModelID)
		require.Equal(t, m.ModelID, p.state(sid).ModelID)
	}
	tp := providers.TokenPlanMessages()
	for _, bad := range []string{tp.Provider + "/" + tp.ID, "nope", "", "faux/missing@x", tp.Provider + "/" + tp.ID + "@missing-api"} {
		f := p.call(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": bad})
		require.Equal(t, protocol.ACPErrInvalidModel, f.errKind(t), "%q", bad)
	}
	require.Equal(t, cat.Models[len(cat.Models)-1].ModelID, p.state(sid).ModelID, "a refused switch keeps the model")
}

func TestAdapterSetModelAuthConstraintAndReadiness(t *testing.T) {
	cfg := testConfig(says("x"), nil)
	cfg.ModelAuth = func(_ context.Context, _ providers.Model, method string) error {
		if method != "api-key" {
			return fmt.Errorf("%w: configured method differs", ErrAuth)
		}
		return nil
	}
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	before := p.state(sid)
	target := providers.AnthropicSonnet46()
	id := fmt.Sprintf("%s/%s@%s", target.Provider, target.ID, target.API)

	f := p.call(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": id, "authMethodId": "subscription"})
	require.Equal(t, protocol.ACPErrNoAPIKey, f.errKind(t))
	require.Equal(t, before, p.state(sid), "a mismatch changes nothing")
	p.ok(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": id, "authMethodId": "api-key"}, nil)
	require.Equal(t, id, p.state(sid).ModelID)
	p.ok(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": id}, nil)

	// The reverse direction: a callback that refuses api-key and accepts subscription.
	cfg2 := testConfig(says("x"), nil)
	cfg2.ModelAuth = func(_ context.Context, _ providers.Model, method string) error {
		if method == "api-key" {
			return fmt.Errorf("%w: the configured method is a subscription", ErrAuth)
		}
		return nil
	}
	p3 := newAdapterPeer(t, cfg2, nil)
	sid3 := p3.start()
	before3 := p3.state(sid3)
	f = p3.call(protocol.ACPSetModel, map[string]any{"sessionId": sid3, "modelId": id, "authMethodId": "api-key"})
	require.Equal(t, protocol.ACPErrNoAPIKey, f.errKind(t))
	require.Equal(t, before3, p3.state(sid3))
	p3.ok(protocol.ACPSetModel, map[string]any{"sessionId": sid3, "modelId": id, "authMethodId": "subscription"}, nil)
	require.Equal(t, id, p3.state(sid3).ModelID)

	// Without a callback the client cannot name a method.
	p2 := newAdapterPeer(t, testConfig(says("x"), nil), nil)
	sid2 := p2.start()
	f = p2.call(protocol.ACPSetModel, map[string]any{"sessionId": sid2, "modelId": id, "authMethodId": "api-key"})
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
}

func TestAdapterSetModelReadinessFailureKeepsState(t *testing.T) {
	cfg := testConfig(says("x"), func(c *agent.Config) {
		c.Ready = func(_ context.Context, m providers.Model, _ string) error {
			if m.Provider == "faux" {
				return nil
			}
			return fmt.Errorf("credential store says secret-detail")
		}
	})
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	before := p.state(sid)
	target := providers.OpenAIGPT55()
	f := p.call(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": fmt.Sprintf("%s/%s@%s", target.Provider, target.ID, target.API)})
	require.Equal(t, protocol.ACPErrNoAPIKey, f.errKind(t))
	require.NotContains(t, f.Error.Message, "secret-detail")
	require.Equal(t, before, p.state(sid))
}

func TestAdapterThinkingClampsAndRejectsBusy(t *testing.T) {
	release := make(chan struct{})
	rel := closeOnce(release)
	t.Cleanup(rel)
	p := newAdapterPeer(t, testConfig(func() []faux.Step { return []faux.Step{heldStep(release, "x")} }, nil), nil)
	sid := p.start()
	s, _ := p.a.host.Session(sid)
	var res protocol.ACPThinkingResult
	p.ok(protocol.ACPSetThinking, map[string]any{"sessionId": sid, "level": "high"}, &res)
	require.Equal(t, providers.ClampThinkingLevel(s.Agent().State().Model, protocol.ThinkingHigh), res.Level)
	require.Equal(t, res.Level, p.state(sid).ThinkingLevel)
	target := providers.OpenAIGPT55()
	p.ok(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": fmt.Sprintf("%s/%s@%s", target.Provider, target.ID, target.API)}, nil)
	p.ok(protocol.ACPSetThinking, map[string]any{"sessionId": sid, "level": "high"}, &res)
	require.Equal(t, providers.ClampThinkingLevel(target, protocol.ThinkingHigh), res.Level)

	f := p.call(protocol.ACPSetThinking, map[string]any{"sessionId": sid, "level": "ultra"})
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))

	// A running session refuses both controls and keeps its state.
	busy := p.newSession()
	before := p.state(busy)
	id := p.send("session/prompt", promptParams(busy, "go"))
	p.running(busy)
	f = p.call(protocol.ACPSetThinking, map[string]any{"sessionId": busy, "level": "low"})
	require.Equal(t, protocol.ACPErrBusy, f.errKind(t))
	f = p.call(protocol.ACPSetModel, map[string]any{"sessionId": busy, "modelId": fmt.Sprintf("%s/%s@%s", target.Provider, target.ID, target.API)})
	require.Equal(t, protocol.ACPErrBusy, f.errKind(t))
	during := p.state(busy)
	require.Equal(t, before.ModelID, during.ModelID)
	require.Equal(t, before.ThinkingLevel, during.ThinkingLevel)
	rel()
	p.await(id)
}

func TestAdapterSteerFollowUpRemoveQueueLifecycle(t *testing.T) {
	release := make(chan struct{})
	rel := closeOnce(release)
	t.Cleanup(rel)
	cfg := testConfig(func() []faux.Step {
		return []faux.Step{heldStep(release, "first"), faux.Say("second"), faux.Say("third")}
	}, func(c *agent.Config) { c.MaxQueuedInputs = 3 })
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	id := p.send("session/prompt", promptParams(sid, "go"))
	p.running(sid)

	var steer, follow, removed protocol.ACPInputResult
	p.ok(protocol.ACPSteer, textParams(sid, "steer text"), &steer)
	p.ok(protocol.ACPFollowUp, textParams(sid, "follow text"), &follow)
	require.NotEmpty(t, steer.InputID)
	require.NotEqual(t, steer.InputID, follow.InputID)
	require.Eventually(t, func() bool {
		st := p.state(sid)
		return len(st.Steering) == 1 && len(st.FollowUp) == 1
	}, peerWait, 5*time.Millisecond)
	st := p.state(sid)
	require.Equal(t, []string{"steer text"}, st.Steering)
	require.Equal(t, []string{"follow text"}, st.FollowUp)

	var rm protocol.ACPRemoveResult
	p.ok(protocol.ACPRemove, map[string]any{"sessionId": sid, "inputId": follow.InputID}, &rm)
	require.True(t, rm.Removed)
	p.ok(protocol.ACPRemove, map[string]any{"sessionId": sid, "inputId": follow.InputID}, &rm)
	require.False(t, rm.Removed, "an input leaves the queue once")
	p.ok(protocol.ACPSteer, textParams(sid, "second steer"), &removed)
	p.ok(protocol.ACPSteer, textParams(sid, "third steer"), &removed)
	f := p.call(protocol.ACPSteer, textParams(sid, "over the bound"))
	require.Equal(t, protocol.ACPErrQueueFull, f.errKind(t))
	f = p.call(protocol.ACPSteer, map[string]any{"sessionId": sid, "content": []any{}})
	require.Equal(t, protocol.ACPErrInvalidContent, f.errKind(t))
	f = p.call(protocol.ACPFollowUp, map[string]any{"sessionId": sid, "content": []any{map[string]any{"type": "audio", "data": "x", "mimeType": "audio/wav"}}})
	require.Equal(t, protocol.ACPErrInvalidContent, f.errKind(t))
	rel()
	prompt := p.await(id)
	require.Nil(t, prompt.Error)
	s, _ := p.a.host.Session(sid)
	require.NoError(t, s.Agent().WaitForIdle(hostDeadline(t)))
	require.Empty(t, p.state(sid).Steering)
	p.ok(protocol.ACPRemove, map[string]any{"sessionId": sid, "inputId": steer.InputID}, &rm)
	require.False(t, rm.Removed, "a claimed input cannot be removed")
}

func TestAdapterFollowUpOnIdleSessionStartsARun(t *testing.T) {
	p := newAdapterPeer(t, testConfig(says("woken"), nil), nil)
	sid := p.start()
	var res protocol.ACPInputResult
	p.ok(protocol.ACPFollowUp, textParams(sid, "wake"), &res)
	require.NotEmpty(t, res.InputID)
	s, _ := p.a.host.Session(sid)
	require.NoError(t, s.Agent().WaitForIdle(hostDeadline(t)))
	require.Equal(t, 2, p.state(sid).MessageCount)
	p.waitFor("update of the woken run", func() bool { return len(p.updateKinds()) > 0 })
}

func TestAdapterResetStartsNewEpochAndAcceptsPrompt(t *testing.T) {
	p := newAdapterPeer(t, testConfig(says("one", "two"), nil), nil)
	sid := p.start()
	p.ok("session/prompt", promptParams(sid, "first"), nil)
	old := p.state(sid)
	var fol protocol.ACPFollowResult
	p.ok(protocol.ACPFollow, map[string]any{"sessionId": sid}, &fol)
	require.Equal(t, old.Epoch, fol.Cursor.Epoch)

	var reset protocol.ACPResetResult
	p.ok(protocol.ACPReset, map[string]any{"sessionId": sid}, &reset)
	require.Equal(t, sid, reset.SessionID)
	require.NotEqual(t, old.Epoch, reset.Epoch)
	st := p.state(sid)
	require.Equal(t, reset.Epoch, st.Epoch)
	require.Equal(t, 0, st.MessageCount)

	// The old subscription gets an explicit resync and the old cursor is refused.
	p.waitFor("resync notification", func() bool { return len(p.notes(protocol.ACPResync)) == 1 })
	var rs protocol.ACPResyncNotification
	require.NoError(t, json.Unmarshal(p.notes(protocol.ACPResync)[0].Params, &rs))
	require.Equal(t, fol.SubscriptionID, rs.SubscriptionID)
	require.Equal(t, reset.Epoch, rs.Cursor.Epoch)
	var again protocol.ACPFollowResult
	p.ok(protocol.ACPFollow, map[string]any{"sessionId": sid, "cursor": fol.Cursor}, &again)
	require.True(t, again.Resync)
	require.False(t, again.Resumed)
	require.Equal(t, reset.Epoch, again.Cursor.Epoch)

	// The next prompt completes in the new epoch with updates written first.
	id := p.send("session/prompt", promptParams(sid, "second"))
	f := p.await(id)
	require.Nil(t, f.Error, "%+v", f.Error)
	require.Greater(t, p.responseIndex(id), p.lastNoteIndex("session/update"))
	require.Equal(t, 2, p.state(sid).MessageCount)
	for _, n := range p.notes("session/update") {
		var m struct {
			Meta struct {
				Ask struct {
					Epoch string `json:"epoch"`
				} `json:"ask"`
			} `json:"_meta"`
		}
		require.NoError(t, json.Unmarshal(n.Params, &m))
		require.Contains(t, []string{old.Epoch, reset.Epoch}, m.Meta.Ask.Epoch)
	}
	last := p.notes("session/update")[len(p.notes("session/update"))-1]
	require.Contains(t, string(last.Params), reset.Epoch)

	f = p.call(protocol.ACPReset, map[string]any{"sessionId": "sess_missing"})
	require.Equal(t, protocol.ACPErrUnknownSession, f.errKind(t))
}

func TestAdapterResetRejectedWhileRunning(t *testing.T) {
	release := make(chan struct{})
	rel := closeOnce(release)
	t.Cleanup(rel)
	p := newAdapterPeer(t, testConfig(func() []faux.Step { return []faux.Step{heldStep(release, "x")} }, nil), nil)
	sid := p.start()
	id := p.send("session/prompt", promptParams(sid, "go"))
	p.running(sid)
	f := p.call(protocol.ACPReset, map[string]any{"sessionId": sid})
	require.Equal(t, protocol.ACPErrBusy, f.errKind(t))
	rel()
	p.await(id)
}

func TestAdapterUsageReportsExactAttemptRows(t *testing.T) {
	u1 := protocol.Usage{Input: 10, Output: 5, TotalTokens: 15, Cost: protocol.Cost{Input: 7, Output: 3, Total: 10}}
	u2 := protocol.Usage{Input: 20, Output: 9, TotalTokens: 29, Cost: protocol.Cost{Input: 14, Output: 6, Total: 20}}
	p := newAdapterPeer(t, testConfig(func() []faux.Step { return []faux.Step{faux.Say("a").WithUsage(u1), faux.Say("b").WithUsage(u2)} }, nil), nil)
	sid := p.start()
	var empty protocol.ACPUsageResult
	p.ok(protocol.ACPUsage, map[string]any{"sessionId": sid}, &empty)
	require.Empty(t, empty.Attempts)
	require.True(t, empty.Complete)
	p.ok("session/prompt", promptParams(sid, "one"), nil)
	p.ok("session/prompt", promptParams(sid, "two"), nil)
	var res protocol.ACPUsageResult
	p.ok(protocol.ACPUsage, map[string]any{"sessionId": sid}, &res)
	require.Len(t, res.Attempts, 2)
	require.NotEqual(t, res.Attempts[0].AttemptID, res.Attempts[1].AttemptID)
	for _, row := range res.Attempts {
		require.Equal(t, "completed", row.Outcome)
		require.NotEmpty(t, row.CycleID)
	}
	require.Equal(t, u1, *res.Attempts[0].Usage)
	require.Equal(t, u2, *res.Attempts[1].Usage)
	require.EqualValues(t, 30, res.Total.Input)
	require.EqualValues(t, 14, res.Total.Output)
	require.EqualValues(t, 44, res.Total.TotalTokens)
	require.EqualValues(t, 30, res.Total.Cost.Total)
	require.True(t, res.Complete)
}

func TestAdapterUsageKeepsFailedAttemptsWithoutProviderText(t *testing.T) {
	secret := "secret-provider-text"
	retryable := faux.Say("partial").Err(providers.NewFailure(providers.CodeServer, 500, 0, "busy "+secret, nil))
	p := newAdapterPeer(t, testConfig(func() []faux.Step { return []faux.Step{retryable, faux.Say("done")} }, nil), nil)
	sid := p.start()
	var fol protocol.ACPFollowResult
	p.ok(protocol.ACPFollow, map[string]any{"sessionId": sid}, &fol)
	p.ok("session/prompt", promptParams(sid, "go"), nil)
	var res protocol.ACPUsageResult
	p.ok(protocol.ACPUsage, map[string]any{"sessionId": sid}, &res)
	require.Len(t, res.Attempts, 2)
	require.Equal(t, "failed", res.Attempts[0].Outcome)
	require.Equal(t, "completed", res.Attempts[1].Outcome)
	raw := p.call(protocol.ACPUsage, map[string]any{"sessionId": sid})
	require.NotContains(t, string(raw.Result), secret)
	// The retry facts reach a follower whole.
	p.waitFor("retry events", func() bool {
		var kinds []string
		for _, n := range p.notes(protocol.ACPEvent) {
			var ev protocol.ACPEventNotification
			_ = json.Unmarshal(n.Params, &ev)
			var head struct{ Type string }
			_ = json.Unmarshal(ev.Event, &head)
			kinds = append(kinds, head.Type)
		}
		return contains(kinds, protocol.TypeAgentSettled) && contains(kinds, "auto_retry_start") && contains(kinds, "auto_retry_end")
	})
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestAdapterUsageProjectionSumsKnownValuesOnly(t *testing.T) {
	one, two := int64(2), int64(3)
	entries := []sessions.Entry{
		sessions.CycleOpened{CycleID: "c1"},
		sessions.AttemptSettled{AttemptID: "a1", Outcome: "completed", Usage: &protocol.Usage{Input: 1, Reasoning: &one, CacheWrite1h: &one, Cost: protocol.Cost{Total: 5}}},
		sessions.AttemptSettled{AttemptID: "a2", Outcome: "failed", Failure: &sessions.Failure{Code: "SERVER", Text: "private text"}},
		sessions.CycleClosed{CycleID: "c1", Reason: "error"},
		sessions.CycleOpened{CycleID: "c2"},
		sessions.AttemptSettled{AttemptID: "a3", Outcome: "completed", Usage: &protocol.Usage{Input: 4, Reasoning: &two, Cost: protocol.Cost{Total: 6}}},
	}
	res := usageResult(entries)
	require.Len(t, res.Attempts, 3)
	require.Equal(t, "c1", res.Attempts[0].CycleID)
	require.Equal(t, "c1", res.Attempts[1].CycleID)
	require.Equal(t, "c2", res.Attempts[2].CycleID)
	require.Nil(t, res.Attempts[1].Usage)
	require.False(t, res.Complete, "an attempt with no reported usage makes the total a lower bound")
	require.EqualValues(t, 5, res.Total.Input)
	require.EqualValues(t, 11, res.Total.Cost.Total)
	require.NotNil(t, res.Total.Reasoning)
	require.EqualValues(t, 5, *res.Total.Reasoning)
	require.NotNil(t, res.Total.CacheWrite1h)
	require.EqualValues(t, 2, *res.Total.CacheWrite1h)
	b, err := json.Marshal(res)
	require.NoError(t, err)
	require.NotContains(t, string(b), "private text")
}
