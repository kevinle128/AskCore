package leader

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func TestRouterLinkInitializeBeforeReady(t *testing.T) {
	h := startHarness(t)
	// Start returned, so the link initialize is done and it was the first message.
	h.agent.mu.Lock()
	require.NotEmpty(t, h.agent.got)
	require.Equal(t, "initialize", h.agent.got[0].Method)
	h.agent.mu.Unlock()
	require.Len(t, h.agent.requests("initialize"), 1)

	c := h.client()
	require.Equal(t, protocol.LeaderProtocolVersion, c.reg.Info.ProtocolVersion)
	require.True(t, c.reg.Info.Ready)
}

func TestRouterClientInitializeAnsweredFromCache(t *testing.T) {
	h := startHarness(t)
	a, b := h.client(), h.client()
	resA, e := a.call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{"terminal": true}})
	require.Nil(t, e)
	resB, e := b.call("initialize", map[string]any{"protocolVersion": 1})
	require.Nil(t, e)
	require.JSONEq(t, string(resA), string(resB))
	require.JSONEq(t, `{"protocolVersion":1,"agentCapabilities":{}}`, string(resA))
	require.Len(t, h.agent.requests("initialize"), 1, "no client initialize reaches the agent")
}

func TestRouterInvalidInitialize(t *testing.T) {
	h := startHarness(t)
	c := h.client()
	for name, params := range map[string]any{
		"missing version": map[string]any{},
		"string version":  map[string]any{"protocolVersion": "1"},
		"zero version":    map[string]any{"protocolVersion": 0},
		"array params":    []any{1},
	} {
		_, e := c.call("initialize", params)
		require.NotNil(t, e, name)
		require.Equal(t, -32602, e.Code, name)
	}
	_, e := c.call("session/new", map[string]any{"cwd": "/w"})
	require.NotNil(t, e)
	require.Equal(t, protocol.ACPCodeNotInitializedClient, e.Code, "a failed initialize leaves the client uninitialized")
}

func TestRouterRepeatedInitializeWhileInUse(t *testing.T) {
	h := startHarness(t)
	c := h.ready(map[string]any{"terminal": true})
	_, e := c.call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{"terminal": false}})
	require.Nil(t, e, "a repeated initialize is fine while the client uses nothing")
	c.newSession("/w")
	_, e = c.call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{"terminal": true}})
	require.NotNil(t, e)
	require.Equal(t, protocol.ACPCodeInvalidState, e.Code, "capabilities cannot change under a client that has sessions")
}

func TestRouterUninitializedClientRejected(t *testing.T) {
	h := startHarness(t)
	a := h.ready(nil)
	sid := a.newSession("/w")
	b := h.client() // never runs initialize, whatever A did
	for _, call := range []struct {
		method string
		params any
	}{
		{"session/new", map[string]any{"cwd": "/w"}},
		{"session/prompt", map[string]any{"sessionId": sid, "prompt": []any{}}},
		{protocol.ACPAttach, map[string]any{"sessionId": sid}},
		{protocol.ACPListLive, map[string]any{}},
		{protocol.ACPState, map[string]any{"sessionId": sid}},
	} {
		_, e := b.call(call.method, call.params)
		require.NotNil(t, e, call.method)
		require.Equal(t, protocol.ACPCodeNotInitializedClient, e.Code, call.method)
	}
	require.Len(t, h.agent.requests("session/new"), 1, "only A's call reached the agent")
	require.Empty(t, h.agent.requests("session/prompt"))
}

func TestRouterCreatorIsDriver(t *testing.T) {
	h := startHarness(t)
	a := h.ready(map[string]any{"terminal": true})
	sid := a.newSession("/work/a")

	reqs := h.agent.requests("session/new")
	require.Len(t, reqs, 1)
	require.Equal(t, "c1", reqs[0].Route.ClientID)
	require.JSONEq(t, `{"terminal":true}`, string(reqs[0].Route.Capabilities))

	a.mustCall("session/prompt", map[string]any{"sessionId": sid, "prompt": []any{}})
	prompt := h.agent.requests("session/prompt")
	require.Len(t, prompt, 1)
	require.Equal(t, protocol.ACPRouteMeta{ClientID: "c1", SessionID: sid, DriverGen: 1, LiveDriver: true, Capabilities: json.RawMessage(`{"terminal":true}`)}, prompt[0].Route)

	var live protocol.ACPListLiveResult
	require.NoError(t, json.Unmarshal(a.mustCall(protocol.ACPListLive, map[string]any{}), &live))
	require.Equal(t, []protocol.ACPLiveSession{{SessionID: sid, Cwd: "/work/a", Role: protocol.ACPRoleDriver, HasDriver: true, Subscribers: 1}}, live.Sessions)
}

func TestRouterNewSessionResultHasNoRouteMeta(t *testing.T) {
	h := startHarness(t)
	a := h.ready(nil)
	res := a.mustCall("session/new", map[string]any{"cwd": "/w"})
	require.JSONEq(t, `{"sessionId":"s1"}`, string(res), "the generation is for the router only")
}

func TestRouterObserverMutationRejected(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	require.Equal(t, protocol.ACPRoleObserver, b.attach(sid).Role)

	for _, call := range []struct {
		method string
		params map[string]any
	}{
		{"session/prompt", map[string]any{"sessionId": sid, "prompt": []any{}}},
		{protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": "m"}},
		{protocol.ACPSteer, map[string]any{"sessionId": sid, "content": []any{}}},
		{protocol.ACPReset, map[string]any{"sessionId": sid}},
		{protocol.ACPContinue, map[string]any{"sessionId": sid}},
	} {
		_, e := b.call(call.method, call.params)
		require.NotNil(t, e, call.method)
		require.Equal(t, protocol.ACPCodeNotDriver, e.Code, call.method)
	}
	require.Empty(t, h.agent.requests("session/prompt"))
	require.Empty(t, h.agent.requests(protocol.ACPSetModel))

	b.mustCall(protocol.ACPState, map[string]any{"sessionId": sid})
	b.mustCall(protocol.ACPUsage, map[string]any{"sessionId": sid})
	require.Len(t, h.agent.requests(protocol.ACPState), 1, "an observer may read")
}

func TestRouterObserverCancelNotificationDropped(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)

	b.notify("session/cancel", map[string]any{"sessionId": sid})
	b.barrier()
	require.Empty(t, h.agent.requests("session/cancel"), "an observer cannot cancel the run")

	a.notify("session/cancel", map[string]any{"sessionId": sid})
	got := h.agent.waitRequests("session/cancel", 1)
	require.Equal(t, protocol.ACPRouteMeta{ClientID: "c1", SessionID: sid, DriverGen: 1, LiveDriver: true}, withoutCaps(got[0].Route), "route meta covers notifications")
}

func withoutCaps(m protocol.ACPRouteMeta) protocol.ACPRouteMeta {
	m.Capabilities = nil
	return m
}

func TestRouterUnknownAndMalformedCalls(t *testing.T) {
	h := startHarness(t)
	a := h.ready(nil)
	sid := a.newSession("/w")

	_, e := a.call("session/prompt", map[string]any{"sessionId": "nope"})
	require.Equal(t, -32002, e.Code)
	_, e = a.call("session/prompt", map[string]any{})
	require.Equal(t, -32602, e.Code)
	_, e = a.call("session/prompt", []any{1})
	require.Equal(t, -32602, e.Code)
	_, e = a.call("fs/read_text_file", map[string]any{"sessionId": sid})
	require.Equal(t, -32601, e.Code, "a method without a policy has no authority")
	_, e = a.call("session/load", map[string]any{"sessionId": sid})
	require.Equal(t, -32601, e.Code)
	require.Empty(t, h.agent.requests("fs/read_text_file"))

	a.sendRaw(json.RawMessage(`{"jsonrpc":"1.0","id":9,"method":"session/prompt"}`))
	a.sendRaw(json.RawMessage(`{"jsonrpc":"2.0","id":[1],"method":"session/prompt"}`))
	a.barrier()
	require.Empty(t, h.agent.requests("session/prompt"))

	// A frame that is not one message breaks the framing, so the connection ends.
	b := h.ready(nil)
	b.sendRaw(json.RawMessage(`[]`))
	b.waitClosed()
	a.barrier()
}

func TestRouterRouteMetaOverwritesClientValue(t *testing.T) {
	h := startHarness(t)
	a := h.ready(nil)
	sid := a.newSession("/w")
	a.mustCall(protocol.ACPState, map[string]any{
		"sessionId": sid,
		"_meta": map[string]any{
			protocol.ACPRouteMetaKey: map[string]any{"clientId": "evil", "driverGen": "99", "liveDriver": false},
			"other":                  "kept",
		},
	})
	got := h.agent.requests(protocol.ACPState)[0]
	require.Equal(t, "c1", got.Route.ClientID)
	require.Equal(t, uint64(1), got.Route.DriverGen)
	meta, _ := decodeObject(got.Params["_meta"])
	require.JSONEq(t, `"kept"`, string(meta["other"]))
}

func TestRouterNewChangesOnlyCallerView(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	s1 := a.newSession("/a")
	b.attach(s1)
	s2 := b.newSession("/b")
	require.NotEqual(t, s1, s2)

	var live protocol.ACPListLiveResult
	require.NoError(t, json.Unmarshal(a.mustCall(protocol.ACPListLive, map[string]any{}), &live))
	roles := map[string]string{}
	for _, row := range live.Sessions {
		roles[row.SessionID] = row.Role
	}
	require.Equal(t, map[string]string{s1: protocol.ACPRoleDriver, s2: ""}, roles, "A still drives s1 and has no part in s2")

	a.mustCall("session/prompt", map[string]any{"sessionId": s1, "prompt": []any{}})
	h.agent.send(h.agent.update(s2, "for b only"))
	b.waitIncoming("session/update", 1)
	a.barrier()
	require.Empty(t, a.incoming("session/update"), "a session that A never joined sends A nothing")
}

func TestRouterSessionUpdateToSubscribersOnly(t *testing.T) {
	h := startHarness(t)
	a, b, c := h.ready(nil), h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)

	h.agent.send(h.agent.update(sid, "hello"))
	a.waitIncoming("session/update", 1)
	b.waitIncoming("session/update", 1)
	c.barrier()
	require.Empty(t, c.incoming("session/update"))
}

func TestRouterOrphanNotificationDropped(t *testing.T) {
	h := startHarness(t)
	a := h.ready(nil)
	a.newSession("/w")
	h.agent.send(h.agent.update("unknown", "x"))
	h.agent.send(notification("x/other", map[string]any{}))
	h.agent.send(notification(protocol.ACPEvent, map[string]any{"subscriptionId": "ghost"}))
	a.barrier()
	require.Empty(t, a.incoming("session/update"))
	require.Empty(t, a.incoming(protocol.ACPEvent))
}

func TestRouterImplicitSubscribeOnSessionTraffic(t *testing.T) {
	h := startHarness(t)
	a, c := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")

	// A driver-only call from a stranger fails and joins nothing.
	_, e := c.call("session/prompt", map[string]any{"sessionId": sid})
	require.Equal(t, protocol.ACPCodeNotDriver, e.Code)
	// So do calls that name no valid session.
	_, e = c.call(protocol.ACPState, map[string]any{"sessionId": "ghost"})
	require.NotNil(t, e)
	h.agent.send(h.agent.update(sid, "one"))
	a.waitIncoming("session/update", 1)
	c.barrier()
	require.Empty(t, c.incoming("session/update"), "a refused call does not subscribe")

	// A valid read joins as an observer, never as the driver.
	c.mustCall(protocol.ACPState, map[string]any{"sessionId": sid})
	h.agent.send(h.agent.update(sid, "two"))
	c.waitIncoming("session/update", 1)
	_, e = c.call("session/prompt", map[string]any{"sessionId": sid})
	require.Equal(t, protocol.ACPCodeNotDriver, e.Code)
}

func TestRouterPromptResultAfterPrecedingUpdates(t *testing.T) {
	h := startHarness(t)
	h.agent.onPrompt = func(a *fakeAgent, req agentRequest) {
		sid := stringParam(req.Params, "sessionId")
		for i := range 20 {
			a.send(a.update(sid, fmt.Sprint(i)))
		}
		a.result(req.ID, map[string]any{"stopReason": "end_turn"})
	}
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	a.mustCall("session/prompt", map[string]any{"sessionId": sid, "prompt": []any{}})

	order := a.order()
	require.Equal(t, "response", order[len(order)-1], "the prompt result comes last")
	require.Len(t, a.incoming("session/update"), 20, "a client that has the result has every update before it")
	b.waitIncoming("session/update", 20)
}

func TestRouterConcurrentRequestsKeepIdentity(t *testing.T) {
	h := startHarness(t)
	const clients, calls = 8, 60
	var cs []*testClient
	var sids []string
	for range clients {
		c := h.ready(nil)
		cs = append(cs, c)
		sids = append(sids, c.newSession("/w"))
	}
	var wg sync.WaitGroup
	for i, c := range cs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range calls {
				// The same id 1..calls on every client: only the leader keeps them apart.
				res, e := c.call(protocol.ACPState, map[string]any{"sessionId": sids[i]})
				if e != nil || res == nil {
					t.Errorf("client %d: %v", i, e)
					return
				}
			}
		}()
	}
	wg.Wait()
	require.Len(t, h.agent.requests(protocol.ACPState), clients*calls)
}

func TestRouterBoundsClientCapabilities(t *testing.T) {
	h := startHarness(t)
	c := h.client()
	big := map[string]any{"padding": strings.Repeat("x", 80<<10)}
	_, e := c.call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": big})
	require.NotNil(t, e)
	require.Equal(t, -32602, e.Code, "capabilities go into every call, so their size is bounded")
	_, e = c.call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{"terminal": true}})
	require.Nil(t, e)
}

func TestRouterEndedFollowsAreBounded(t *testing.T) {
	h := startHarness(t)
	h.srv.r.call(func() {
		for i := range maxEndedFollows + 100 {
			h.srv.r.noteEnded("sub"+strconv.Itoa(i), "c1")
		}
		require.LessOrEqual(t, len(h.srv.r.ended), maxEndedFollows)
		_, newest := h.srv.r.ended["sub"+strconv.Itoa(maxEndedFollows+99)]
		_, oldest := h.srv.r.ended["sub0"]
		require.True(t, newest)
		require.False(t, oldest, "the oldest record goes first")
	})
}
