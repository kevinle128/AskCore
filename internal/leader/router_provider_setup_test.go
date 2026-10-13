package leader

import (
	"encoding/json"
	"testing"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

var providerSetupMethods = []string{
	protocol.ACPProvidersList, protocol.ACPProvidersAPIKey, protocol.ACPLoginStart, protocol.ACPLoginInput,
	protocol.ACPLoginWait, protocol.ACPLoginCancel, protocol.ACPProvidersLogout,
}

func TestRouterForwardsProviderSetupCallsWithClientRoute(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	for _, m := range providerSetupMethods {
		// A route that the client writes is replaced, and a session is not needed.
		a.mustCall(m, map[string]any{
			"loginId": "l1", "providerId": "openai",
			"_meta": map[string]any{protocol.ACPRouteMetaKey: map[string]any{"clientId": "forged", "sessionId": "s9"}},
		})
		got := h.agent.requests(m)
		require.Len(t, got, 1, m)
		require.Equal(t, protocol.ACPRouteMeta{ClientID: "c1", DriverGen: 0}, got[0].Route, m)
	}
	b.mustCall(protocol.ACPProvidersList, map[string]any{})
	list := h.agent.requests(protocol.ACPProvidersList)
	require.Len(t, list, 2)
	require.Equal(t, "c2", list[1].Route.ClientID)

	// The answer returns to the caller by request id.
	var res map[string]any
	require.NoError(t, json.Unmarshal(a.mustCall(protocol.ACPProvidersList, map[string]any{}), &res))
}

func TestRouterProviderSetupCallsNeedInitialize(t *testing.T) {
	h := startHarness(t)
	c := h.client()
	_, e := c.call(protocol.ACPProvidersList, map[string]any{})
	require.NotNil(t, e)
	require.Equal(t, protocol.ACPCodeNotInitializedClient, e.Code)
	require.Empty(t, h.agent.requests(protocol.ACPProvidersList))
}

func TestRouterBroadcastsProvidersChanged(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	early := h.client() // connected, but never ran initialize
	sid := a.newSession("/w")
	require.NotEmpty(t, sid)
	// b follows nothing and is no member of the session: the notice is not a session event.
	markA, markB, markEarly := a.mark(), b.mark(), early.mark()

	h.agent.send(notification(protocol.ACPProvidersChanged, protocol.ACPProvidersChangedNotification{
		Generation: 4, ProviderID: "openai", Change: protocol.ACPProviderChangeSaved,
	}))

	for name, c := range map[string]*testClient{"a": a, "b": b} {
		mark := markA
		if name == "b" {
			mark = markB
		}
		eventually(t, name+" gets the notice", func() bool { return len(c.since(mark)) > 0 })
		got := c.since(mark)
		require.Len(t, got, 1, name)
		require.Equal(t, protocol.ACPProvidersChanged, got[0].method, name)
		var n protocol.ACPProvidersChangedNotification
		require.NoError(t, json.Unmarshal(got[0].fields["params"], &n))
		require.Equal(t, uint64(4), n.Generation)
		require.Equal(t, "openai", n.ProviderID)
	}
	// Both initialized clients have it, so the router has decided about the early one.
	require.Empty(t, early.since(markEarly), "a client before initialize gets no notice")
}

func TestRouterReleasesLoginsOfGoneClient(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	a.mustCall(protocol.ACPLoginStart, map[string]any{"providerId": "xai", "methodId": "xai-oauth"})
	b.mustCall(protocol.ACPProvidersList, map[string]any{})
	idle := h.ready(nil) // never made a provider setup call

	a.close()
	rel := h.agent.waitRequests(protocol.ACPProvidersReleaseClient, 1)
	require.Equal(t, "c1", rel[0].Route.ClientID)
	require.Empty(t, rel[0].Route.SessionID)

	idle.close()
	b.barrier()
	require.Len(t, h.agent.requests(protocol.ACPProvidersReleaseClient), 1, "one release for each gone client that used provider setup, none for the other")
	b.close()
	rel = h.agent.waitRequests(protocol.ACPProvidersReleaseClient, 2)
	require.Equal(t, "c2", rel[1].Route.ClientID)
}

func TestRouterRefusesReleaseClientFromClient(t *testing.T) {
	h := startHarness(t)
	a := h.ready(nil)
	_, e := a.call(protocol.ACPProvidersReleaseClient, map[string]any{"clientId": "c2"})
	require.NotNil(t, e)
	require.Equal(t, -32601, e.Code)
	a.notify(protocol.ACPProvidersReleaseClient, map[string]any{})
	a.barrier()
	require.Empty(t, h.agent.requests(protocol.ACPProvidersReleaseClient))
}
