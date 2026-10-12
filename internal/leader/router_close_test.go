package leader

import (
	"testing"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func TestRouterSessionCloseByTheDriverForgetsTheSession(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	sub := followOf(t, b, sid)

	a.mustCall("session/close", map[string]any{"sessionId": sid})
	closes := h.agent.waitRequests("session/close", 1)
	require.Equal(t, sid, closes[0].Route.SessionID)
	require.Equal(t, "c1", closes[0].Route.ClientID)

	for _, c := range []*testClient{a, b} {
		_, e := c.call(protocol.ACPState, map[string]any{"sessionId": sid})
		require.NotNil(t, e)
		require.Equal(t, "unknown_session", e.Data.Kind, "the router forgets a closed session")
	}
	ack := b.mark()
	h.agent.send(eventFor(sub, sid, 50))
	b.barrier()
	for _, m := range b.since(ack) {
		require.NotEqual(t, protocol.ACPEvent, m.method, "the follow of a closed session ends")
	}
	require.Empty(t, h.agent.requests(protocol.ACPUnfollow), "the host ended the follows with the session")
}

func TestRouterSessionCloseNeedsTheDriver(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	_, e := b.call("session/close", map[string]any{"sessionId": sid})
	require.NotNil(t, e)
	require.Equal(t, "not_driver", e.Data.Kind)
	require.Empty(t, h.agent.requests("session/close"), "the host never sees the call")
	a.mustCall(protocol.ACPState, map[string]any{"sessionId": sid})
}

func TestRouterSessionCloseEndsOpenQuestions(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	h.agent.send(permissionRequest("q9", sid))
	theCall(t, b, "session/request_permission")
	a.mustCall("session/close", map[string]any{"sessionId": sid})
	waitAnswers(t, h, `"q9"`, 1)
}

func TestRouterSessionCloseKeepsOtherSessions(t *testing.T) {
	h := startHarness(t)
	a := h.ready(nil)
	one, two := a.newSession("/one"), a.newSession("/two")
	a.mustCall("session/close", map[string]any{"sessionId": one})
	a.mustCall(protocol.ACPState, map[string]any{"sessionId": two})
}
