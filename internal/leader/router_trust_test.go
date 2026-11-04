package leader

import (
	"encoding/json"
	"testing"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

// The trust calls carry no session: the router forwards them with the client
// id only, as it does for authenticate. A trust method that is not in the table
// is unsupported.
func TestLeaderRoutesTrustMethods(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	for _, m := range []string{protocol.ACPTrustState, protocol.ACPTrustSet} {
		a.mustCall(m, map[string]any{
			"cwd": "/w", "target": "folder", "decision": "trust",
			"_meta": map[string]any{protocol.ACPRouteMetaKey: map[string]any{"clientId": "forged", "sessionId": "s9"}},
		})
		got := h.agent.requests(m)
		require.Len(t, got, 1, m)
		require.Equal(t, protocol.ACPRouteMeta{ClientID: "c1"}, got[0].Route, m)
		require.Equal(t, "/w", stringParam(got[0].Params, "cwd"), m)
	}
	b.mustCall(protocol.ACPTrustState, map[string]any{"cwd": "/w"})
	got := h.agent.requests(protocol.ACPTrustState)
	require.Len(t, got, 2)
	require.Equal(t, "c2", got[1].Route.ClientID)

	_, e := a.call("_ask/trust/x", map[string]any{})
	require.NotNil(t, e)
	require.Equal(t, protocol.ACPErrorCode(protocol.ACPErrUnsupported), e.Code)
	require.Empty(t, h.agent.requests("_ask/trust/x"))

	// A client that has not run initialize is refused first.
	c := h.client()
	_, e = c.call(protocol.ACPTrustState, map[string]any{"cwd": "/w"})
	require.NotNil(t, e)
	require.Equal(t, protocol.ACPCodeNotInitializedClient, e.Code)
}

// The trust override of a new session reaches the host next to the route.
func TestLeaderKeepsTrustMetaOfNewSession(t *testing.T) {
	h := startHarness(t)
	a := h.ready(nil)
	a.mustCall("session/new", map[string]any{"cwd": "/w", "mcpServers": []any{}, "_meta": map[string]any{protocol.ACPTrustMetaKey: "approve"}})
	got := h.agent.requests("session/new")
	require.Len(t, got, 1)
	var meta map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got[0].Params["_meta"], &meta))
	require.JSONEq(t, `"approve"`, string(meta[protocol.ACPTrustMetaKey]))
	require.Equal(t, "c1", got[0].Route.ClientID)
}
