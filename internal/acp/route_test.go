package acp

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/providers/faux"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

// route builds the _meta that the leader router adds to a forwarded call.
func route(client string, gen uint64, live bool, caps string) map[string]any {
	r := map[string]any{"clientId": client, "driverGen": decimal(gen)}
	if live {
		r["liveDriver"] = true
	}
	if caps != "" {
		r["capabilities"] = json.RawMessage(caps)
	}
	return map[string]any{protocol.ACPRouteMetaKey: r}
}

func decimal(n uint64) string { b, _ := json.Marshal(n); return string(b) }

// withRoute adds the route to the params. It names the session of the params in
// the route, as the router does, so a call is valid on the leader link.
func withRoute(params map[string]any, meta map[string]any) map[string]any {
	if sid, ok := params["sessionId"].(string); ok {
		if r, ok := meta[protocol.ACPRouteMetaKey].(map[string]any); ok {
			r["sessionId"] = sid
		}
	}
	params["_meta"] = meta
	return params
}

// routedPeer is an adapter peer as the leader uses it: every session call must carry a route.
func routedPeer(t *testing.T, steps stepsFor) *adapterPeer {
	t.Helper()
	cfg := testConfig(steps, nil)
	cfg.RequireRoute = true
	p := newAdapterPeer(t, cfg, nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	return p
}

func (p *adapterPeer) newRouted(client, caps string) string {
	p.t.Helper()
	var res struct {
		SessionID string         `json:"sessionId"`
		Meta      map[string]any `json:"_meta"`
	}
	p.ok("session/new", withRoute(map[string]any{"cwd": p.t.TempDir(), "mcpServers": []any{}}, route(client, 0, false, caps)), &res)
	require.NotEmpty(p.t, res.SessionID)
	var rm protocol.ACPRouteMeta
	b, _ := json.Marshal(res.Meta[protocol.ACPRouteMetaKey])
	require.NoError(p.t, json.Unmarshal(b, &rm))
	require.Equal(p.t, uint64(1), rm.DriverGen, "a new session starts at generation 1")
	return res.SessionID
}

func (p *adapterPeer) take(sid, client string, live bool, caps string) rpcFrame {
	p.t.Helper()
	return p.call(protocol.ACPTake, withRoute(map[string]any{"sessionId": sid}, route(client, 1, live, caps)))
}

func (p *adapterPeer) takeGen(sid, client string, live bool, caps string) uint64 {
	p.t.Helper()
	f := p.take(sid, client, live, caps)
	require.Nil(p.t, f.Error, "take failed: %+v", f.Error)
	var res protocol.ACPTakeResult
	require.NoError(p.t, json.Unmarshal(f.Result, &res))
	return res.DriverGen
}

func TestAdapterRequiresRouteOnTheLeaderLink(t *testing.T) {
	p := routedPeer(t, says("ok"))
	f := p.call("session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}})
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
	sid := p.newRouted("c1", "")
	f = p.call("session/prompt", promptParams(sid, "x"))
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t), "a call without a route is refused on the leader link")
	f = p.call(protocol.ACPReset, map[string]any{"sessionId": sid})
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
}

func TestEditorLinkNeedsNoRouteAndAddsNoMeta(t *testing.T) {
	p := newAdapterPeer(t, testConfig(says("ok"), nil), nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	f := p.call("session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}})
	require.Nil(t, f.Error)
	require.NotContains(t, string(f.Result), "_meta", "the editor result is unchanged")
}

func TestMalformedRouteIsNeverTreatedAsEditor(t *testing.T) {
	for name, cfgEdit := range map[string]bool{"require route": true, "editor": false} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig(says("ok"), nil)
			cfg.RequireRoute = cfgEdit
			p := newAdapterPeer(t, cfg, nil)
			p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
			sid := p.newRouted("c1", "")
			for label, meta := range map[string]map[string]any{
				"numeric generation": {protocol.ACPRouteMetaKey: map[string]any{"clientId": "c1", "driverGen": 1}},
				"zero generation":    {protocol.ACPRouteMetaKey: map[string]any{"clientId": "c1", "driverGen": "0"}},
				"huge generation":    {protocol.ACPRouteMetaKey: map[string]any{"clientId": "c1", "driverGen": "99999999999999999999999"}},
				"no client":          {protocol.ACPRouteMetaKey: map[string]any{"driverGen": "1"}},
				"not an object":      {protocol.ACPRouteMetaKey: "c1"},
			} {
				f := p.call("session/prompt", withRoute(promptParams(sid, "x"), meta))
				require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t), label)
			}
			require.Empty(t, p.notes("session/update"), "no malformed call started a run")
		})
	}
}

func TestStaleGenerationRejectedOnEveryDriverCall(t *testing.T) {
	p := routedPeer(t, says("one", "two", "three", "four"))
	sid := p.newRouted("c1", "")
	require.Equal(t, uint64(2), p.takeGen(sid, "c2", false, ""))

	stale := route("c1", 1, true, "")
	fresh := route("c2", 2, true, "")
	calls := []struct {
		method string
		params func(meta map[string]any) map[string]any
	}{
		{"session/prompt", func(m map[string]any) map[string]any { return withRoute(promptParams(sid, "x"), m) }},
		{protocol.ACPContinue, func(m map[string]any) map[string]any { return withRoute(map[string]any{"sessionId": sid}, m) }},
		{protocol.ACPReset, func(m map[string]any) map[string]any { return withRoute(map[string]any{"sessionId": sid}, m) }},
		{protocol.ACPSetThinking, func(m map[string]any) map[string]any {
			return withRoute(map[string]any{"sessionId": sid, "level": "low"}, m)
		}},
		{protocol.ACPSetModel, func(m map[string]any) map[string]any {
			return withRoute(map[string]any{"sessionId": sid, "modelId": "nope/none@none"}, m)
		}},
		{protocol.ACPSteer, func(m map[string]any) map[string]any {
			return withRoute(map[string]any{"sessionId": sid, "content": []any{map[string]any{"type": "text", "text": "s"}}}, m)
		}},
		{protocol.ACPFollowUp, func(m map[string]any) map[string]any {
			return withRoute(map[string]any{"sessionId": sid, "content": []any{map[string]any{"type": "text", "text": "f"}}}, m)
		}},
		{protocol.ACPRemove, func(m map[string]any) map[string]any {
			return withRoute(map[string]any{"sessionId": sid, "inputId": "none"}, m)
		}},
	}
	for _, c := range calls {
		f := p.call(c.method, c.params(stale))
		require.Equal(t, protocol.ACPErrNotDriver, f.errKind(t), "%s with the old generation", c.method)
	}
	for _, c := range calls {
		f := p.call(c.method, c.params(fresh))
		if f.Error != nil {
			require.NotEqual(t, protocol.ACPErrNotDriver, f.errKind(t), "%s with the current generation", c.method)
		}
	}
	// Reads are open to every generation.
	f := p.call(protocol.ACPState, withRoute(map[string]any{"sessionId": sid}, stale))
	require.Nil(t, f.Error)
}

func TestStaleCancelDoesNotAbort(t *testing.T) {
	release := make(chan struct{})
	p := routedPeer(t, func() []faux.Step { return []faux.Step{heldStep(release, "done")} })
	sid := p.newRouted("c1", "")
	require.Equal(t, uint64(2), p.takeGen(sid, "c2", false, ""))
	id := p.send("session/prompt", withRoute(promptParams(sid, "go"), route("c2", 2, true, "")))
	p.waitFor("run started", func() bool { return p.a.host.ActiveRuns() == 1 })

	p.notify("session/cancel", withRoute(map[string]any{"sessionId": sid}, route("c1", 1, true, "")))
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, -1, p.responseIndex(id), "a cancel from the old driver does not stop the run")

	p.notify("session/cancel", withRoute(map[string]any{"sessionId": sid}, route("c2", 2, true, "")))
	f := p.await(id)
	require.Nil(t, f.Error)
	require.Contains(t, string(f.Result), "cancelled")
	close(release)
}

func TestHostTakeBusyWithLiveDriverRejected(t *testing.T) {
	release := make(chan struct{})
	p := routedPeer(t, func() []faux.Step { return []faux.Step{heldStep(release, "done")} })
	sid := p.newRouted("c1", "")
	id := p.send("session/prompt", withRoute(promptParams(sid, "go"), route("c1", 1, true, "")))
	p.waitFor("run started", func() bool { return p.a.host.ActiveRuns() == 1 })

	f := p.take(sid, "c2", true, "")
	require.Equal(t, protocol.ACPErrBusy, f.errKind(t), "a busy session with a live driver is not taken")
	s, _ := p.a.host.Session(sid)
	require.Equal(t, uint64(1), s.Gen(), "a refused take changes nothing")

	require.Equal(t, uint64(2), p.takeGen(sid, "c2", false, ""), "with no live driver the take works while the run goes on")
	close(release)
	f = p.await(id)
	require.Nil(t, f.Error, "the old run settles for its caller")
	require.Contains(t, string(f.Result), "end_turn")
}

func TestHostTakeIdleBumpsGeneration(t *testing.T) {
	p := routedPeer(t, says("ok"))
	sid := p.newRouted("c1", "")
	require.Equal(t, uint64(2), p.takeGen(sid, "c2", true, ""))
	require.Equal(t, uint64(3), p.takeGen(sid, "c3", true, ""))
	f := p.call(protocol.ACPTake, map[string]any{"sessionId": sid})
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t), "take needs a route")
	f = p.call(protocol.ACPTake, withRoute(map[string]any{"sessionId": "ghost"}, route("c1", 1, false, "")))
	require.Equal(t, protocol.ACPErrUnknownSession, f.errKind(t))
}

func TestAdapterRouteMetaCapabilitiesPerSession(t *testing.T) {
	p := routedPeer(t, says("ok"))
	a := p.newRouted("c1", `{"terminal":true}`)
	b := p.newRouted("c2", `{"fs":{"readTextFile":true}}`)
	sa, _ := p.a.host.Session(a)
	sb, _ := p.a.host.Session(b)
	require.JSONEq(t, `{"terminal":true}`, string(sa.DriverCaps()))
	require.JSONEq(t, `{"fs":{"readTextFile":true}}`, string(sb.DriverCaps()), "each session has the capabilities of its own driver")

	p.takeGen(a, "c3", false, `{"terminal":false}`)
	require.JSONEq(t, `{"terminal":false}`, string(sa.DriverCaps()), "a take replaces the driver capabilities")
	require.JSONEq(t, `{"fs":{"readTextFile":true}}`, string(sb.DriverCaps()))
}

func TestQuiesceIfIdle(t *testing.T) {
	release := make(chan struct{})
	p := routedPeer(t, func() []faux.Step { return []faux.Step{heldStep(release, "done")} })
	sid := p.newRouted("c1", "")
	id := p.send("session/prompt", withRoute(promptParams(sid, "go"), route("c1", 1, true, "")))
	p.waitFor("run started", func() bool { return p.a.host.ActiveRuns() == 1 })

	require.False(t, p.a.QuiesceIfIdle(), "an active run is not idle")
	f := p.call("session/new", withRoute(map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}}, route("c2", 0, false, "")))
	require.Nil(t, f.Error, "a refused quiesce leaves the host open")

	close(release)
	p.await(id)
	p.waitFor("idle", func() bool { return p.a.host.ActiveRuns() == 0 })
	require.True(t, p.a.QuiesceIfIdle())
	f = p.call("session/new", withRoute(map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}}, route("c2", 0, false, "")))
	require.Equal(t, protocol.ACPErrDisposed, f.errKind(t), "admission is closed")
	f = p.call(protocol.ACPSetThinking, withRoute(map[string]any{"sessionId": sid, "level": "low"}, route("c1", 1, true, "")))
	require.Equal(t, protocol.ACPErrDisposed, f.errKind(t))
	f = p.call(protocol.ACPState, withRoute(map[string]any{"sessionId": sid}, route("c1", 1, true, "")))
	require.Nil(t, f.Error, "reads still work while the leader stops")
}

func TestQuiesceWaitsForSessionCreation(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	cfg := testConfig(says("ok"), nil)
	cfg.RequireRoute = true
	inner := cfg.Factory
	var once sync.Once
	cfg.Factory = func(ctx context.Context, id, cwd string) (*agent.Agent, error) {
		once.Do(func() { close(entered); <-release })
		return inner(ctx, id, cwd)
	}
	p := newAdapterPeer(t, cfg, nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	id := p.send("session/new", withRoute(map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}}, route("c1", 0, false, "")))
	<-entered
	require.False(t, p.a.QuiesceIfIdle(), "a session that is being built is work")
	close(release)
	require.Nil(t, p.await(id).Error)
	require.True(t, p.a.QuiesceIfIdle())
}

// TestQuiesceNeverLosesAnAdmittedCall hammers prompts while the host tries to
// quiesce. After a true answer no call may start a run.
func TestQuiesceNeverLosesAnAdmittedCall(t *testing.T) {
	p := routedPeer(t, says("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t"))
	sid := p.newRouted("c1", "")
	var stop atomic.Bool
	var quiesced atomic.Bool
	var violations atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			after := quiesced.Load()
			f := p.call("session/prompt", withRoute(promptParams(sid, "x"), route("c1", 1, true, "")))
			if after && f.Error == nil {
				violations.Add(1)
			}
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !p.a.QuiesceIfIdle() {
		time.Sleep(time.Millisecond)
	}
	quiesced.Store(true)
	time.Sleep(50 * time.Millisecond)
	stop.Store(true)
	wg.Wait()
	require.True(t, quiesced.Load())
	require.Zero(t, violations.Load(), "no prompt may succeed after the quiesce answered true")
	require.Zero(t, p.a.host.ActiveRuns())
}

// The route names the session that the router checked. A call that names
// another session in its own params is refused, whatever keys it uses.
func TestHostRefusesACallForAnotherSessionThanTheRoute(t *testing.T) {
	p := routedPeer(t, says("one", "two", "three"))
	s1 := p.newRouted("c1", "")
	s2 := p.newRouted("c2", "")
	asS1 := func(params map[string]any) map[string]any {
		meta := route("c1", 1, true, "")
		meta[protocol.ACPRouteMetaKey].(map[string]any)["sessionId"] = s1
		params["_meta"] = meta
		return params
	}

	f := p.call("session/prompt", asS1(promptParams(s2, "x")))
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t), "the prompt names a session that the route does not")
	f = p.call(protocol.ACPState, asS1(map[string]any{"sessionId": s2}))
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
	f = p.call(protocol.ACPSetThinking, asS1(map[string]any{"sessionId": s2, "level": "low"}))
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))

	// Two keys that a decoder takes for one field: the last one wins in the host.
	forged := promptParams(s1, "x")
	forged["sessionid"] = s2
	f = p.call("session/prompt", asS1(forged))
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t), "the host does not act on the session of a forged key")
	f = p.call(protocol.ACPState, asS1(map[string]any{"sessionId": s1, "sessionid": s2}))
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
	require.Empty(t, p.notes("session/update"), "no run started")

	// A cancel for another session than the route is dropped.
	p.notify("session/cancel", asS1(map[string]any{"sessionId": s2}))

	// The honest call works.
	f = p.call("session/prompt", asS1(promptParams(s1, "x")))
	require.Nil(t, f.Error, "%+v", f.Error)
}

func TestHostNeedsTheRouteSessionOnTheLeaderLink(t *testing.T) {
	p := routedPeer(t, says("one"))
	sid := p.newRouted("c1", "")
	f := p.call(protocol.ACPState, map[string]any{"sessionId": sid, "_meta": route("c1", 1, true, "")}) // no session in the route
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t), "the leader link needs the session id in the route")
	f = p.call("session/prompt", map[string]any{"sessionId": sid, "prompt": []any{}, "_meta": route("c1", 1, true, "")})
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
}

// A refused idle check must leave no trace: work that is allowed keeps being
// allowed while another client probes the host.
func TestQuiesceCheckThatFailsRefusesNoOtherWork(t *testing.T) {
	release := make(chan struct{})
	p := routedPeer(t, func() []faux.Step { return []faux.Step{heldStep(release, "done")} })
	sid := p.newRouted("c1", "")
	id := p.send("session/prompt", withRoute(promptParams(sid, "go"), route("c1", 1, true, "")))
	p.waitFor("run started", func() bool { return p.a.host.ActiveRuns() == 1 })

	var stop atomic.Bool
	var probes sync.WaitGroup
	for range 2 {
		probes.Add(1)
		go func() {
			defer probes.Done()
			for !stop.Load() {
				if p.a.QuiesceIfIdle() { // a run is active, so this is never true
					t.Error("the host quiesced with a run active")
					return
				}
			}
		}()
	}
	for i := range 150 {
		f := p.call("session/new", withRoute(map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}}, route("c2", 0, false, "")))
		require.Nil(t, f.Error, "session %d was refused while another client only probed the host: %+v", i, f.Error)
	}
	stop.Store(true)
	probes.Wait()
	close(release)
	p.await(id)
}

// The router ends the follows of a client that left with a request of its own.
// The real host must take it, or the follower stays in the host.
func TestHostAcceptsTheRoutersInternalUnfollow(t *testing.T) {
	p := routedPeer(t, says("one"))
	sid := p.newRouted("c1", "")
	var f protocol.ACPFollowResult
	p.ok(protocol.ACPFollow, withRoute(map[string]any{"sessionId": sid}, route("c1", 1, true, "")), &f)
	require.NotEmpty(t, f.SubscriptionID)

	internal := route("leader", 1, true, "")
	res := p.call(protocol.ACPUnfollow, withRoute(map[string]any{"sessionId": sid, "subscriptionId": f.SubscriptionID}, internal))
	require.Nil(t, res.Error, "%+v", res.Error)
}
