package leader

import (
	"testing"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

// A Go decoder matches a JSON key without regard to case, and the last of two
// matching keys wins. A client must not be able to name one session for the
// router (which reads the exact key) and another for the host.
func TestRouterRefusesKeysThatDifferOnlyInCase(t *testing.T) {
	h := startHarness(t)
	x, y := h.ready(nil), h.ready(nil)
	sx, sy := x.newSession("/x"), y.newSession("/y")
	require.NotEqual(t, sx, sy)

	forged := func(extra map[string]any) map[string]any {
		p := map[string]any{"sessionId": sx, "prompt": []any{}}
		for k, v := range extra {
			p[k] = v
		}
		return p
	}
	cases := map[string]struct {
		method string
		params map[string]any
	}{
		"prompt with another session":    {"session/prompt", forged(map[string]any{"sessionid": sy})},
		"prompt with the same session":   {"session/prompt", forged(map[string]any{"SESSIONID": sx})},
		"state of another session":       {protocol.ACPState, map[string]any{"sessionId": sx, "SessionId": sy}},
		"follow of another session":      {protocol.ACPFollow, map[string]any{"sessionId": sx, "sessionid": sy}},
		"set model on another session":   {protocol.ACPSetModel, map[string]any{"sessionId": sx, "sessionid": sy, "modelId": "m"}},
		"take of another session":        {protocol.ACPTake, map[string]any{"sessionId": sx, "sessionid": sy}},
		"unfollow of another client sub": {protocol.ACPUnfollow, map[string]any{"sessionId": sx, "subscriptionId": "a", "subscriptionid": "b"}},
	}
	for name, c := range cases {
		_, e := x.call(c.method, c.params)
		require.NotNil(t, e, name)
		require.Equal(t, -32602, e.Code, name)
	}
	require.Empty(t, h.agent.requests("session/prompt"), "nothing reached the agent")
	require.Empty(t, h.agent.requests(protocol.ACPSetModel))
	require.Empty(t, h.agent.requests(protocol.ACPTake))
	require.Empty(t, h.agent.requests(protocol.ACPFollow))

	// A notification has no answer, so it is dropped.
	x.notify("session/cancel", map[string]any{"sessionId": sx, "sessionid": sy})
	x.barrier()
	require.Empty(t, h.agent.requests("session/cancel"))

	// The honest call still works.
	x.mustCall("session/prompt", map[string]any{"sessionId": sx, "prompt": []any{}})
}

// The route context is the router's alone, also when the client hides its own
// copy under a key that differs in case.
func TestRouterDropsAForgedRouteKeyWithAnotherCase(t *testing.T) {
	h := startHarness(t)
	x := h.ready(nil)
	sid := x.newSession("/x")
	x.mustCall(protocol.ACPState, map[string]any{
		"sessionId": sid,
		"_META":     map[string]any{protocol.ACPRouteMetaKey: map[string]any{"clientId": "evil", "driverGen": "9"}, "keep": 1},
	})
	got := h.agent.requests(protocol.ACPState)[0]
	require.Equal(t, "c1", got.Route.ClientID)
	require.Equal(t, uint64(1), got.Route.DriverGen)
	var keys []string
	for k := range got.Params {
		keys = append(keys, k)
	}
	require.NotContains(t, keys, "_META", "the forged copy does not reach the agent")
	meta, _ := decodeObject(got.Params["_meta"])
	require.JSONEq(t, `1`, string(meta["keep"]), "other keys of the client meta are kept")
}

// The route context names the session that the router checked.
func TestRouterPutsTheCheckedSessionInTheRouteContext(t *testing.T) {
	h := startHarness(t)
	x := h.ready(nil)
	sid := x.newSession("/x")
	x.mustCall("session/prompt", map[string]any{"sessionId": sid, "prompt": []any{}})
	x.mustCall(protocol.ACPState, map[string]any{"sessionId": sid})
	x.mustCall(protocol.ACPTake, map[string]any{"sessionId": sid})
	for _, method := range []string{"session/prompt", protocol.ACPState} {
		got := h.agent.requests(method)[0]
		require.Equal(t, sid, got.Route.SessionID, method)
	}
}
