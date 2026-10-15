package leader

import (
	"encoding/json"
	"fmt"
	"testing"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func eventFor(sub, sid string, seq int) json.RawMessage {
	return notification(protocol.ACPEvent, map[string]any{"subscriptionId": sub, "sessionId": sid, "epoch": "e1", "seq": fmt.Sprint(seq), "event": map[string]any{"type": "x"}})
}

func followOf(t *testing.T, c *testClient, sid string) string {
	t.Helper()
	var res protocol.ACPFollowResult
	require.NoError(t, json.Unmarshal(c.mustCall(protocol.ACPFollow, map[string]any{"sessionId": sid}), &res))
	require.NotEmpty(t, res.SubscriptionID)
	return res.SubscriptionID
}

func TestRouterFollowEventsOwnerOnly(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	sub := followOf(t, a, sid)

	h.agent.send(eventFor(sub, sid, 2))
	h.agent.send(notification(protocol.ACPResync, map[string]any{"subscriptionId": sub, "sessionId": sid, "reason": "gap"}))
	a.waitIncoming(protocol.ACPEvent, 1)
	a.waitIncoming(protocol.ACPResync, 1)
	b.barrier()
	require.Empty(t, b.incoming(protocol.ACPEvent), "a follow frame goes to its owner only")
	require.Empty(t, b.incoming(protocol.ACPResync))
}

func TestRouterUnfollowByOtherRejected(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	sub := followOf(t, a, sid)

	_, e := b.call(protocol.ACPUnfollow, map[string]any{"sessionId": sid, "subscriptionId": sub})
	require.NotNil(t, e)
	require.Equal(t, "unknown_subscription", e.Data.Kind)
	require.Empty(t, h.agent.requests(protocol.ACPUnfollow), "the host never sees the call")

	a.mustCall(protocol.ACPUnfollow, map[string]any{"sessionId": sid, "subscriptionId": sub})
	a.mustCall(protocol.ACPUnfollow, map[string]any{"sessionId": sid, "subscriptionId": sub})
	require.Len(t, h.agent.requests(protocol.ACPUnfollow), 2, "a repeated unfollow by the owner stays idempotent in the host")

	_, e = b.call(protocol.ACPUnfollow, map[string]any{"sessionId": sid, "subscriptionId": sub})
	require.NotNil(t, e, "an ended subscription is not a way around the owner check")
	require.Equal(t, "unknown_subscription", e.Data.Kind)
	require.Len(t, h.agent.requests(protocol.ACPUnfollow), 2)
}

func TestRouterDetachUnfollowsOwnedAndFencesOldEvents(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	sub := followOf(t, b, sid)

	for i := range 300 {
		h.agent.send(eventFor(sub, sid, i+2))
	}
	b.mustCall(protocol.ACPDetach, map[string]any{"sessionId": sid})
	ack := b.mark()
	for i := range 300 {
		h.agent.send(eventFor(sub, sid, i+400))
	}
	b.barrier()
	for _, m := range b.since(ack) {
		require.NotEqual(t, protocol.ACPEvent, m.method, "no frame of the old subscription follows the detach answer")
	}

	unf := h.agent.waitRequests(protocol.ACPUnfollow, 1)
	require.Equal(t, sub, stringParam(unf[0].Params, "subscriptionId"), "the router ended the follow in the host")
	require.Equal(t, "c2", unf[0].Route.ClientID)

	// After a new attach the old subscription still has no reader.
	b.attach(sid)
	h.agent.send(eventFor(sub, sid, 999))
	b.barrier()
	for _, m := range b.since(ack) {
		require.NotEqual(t, protocol.ACPEvent, m.method)
	}
	// The detach did not touch the run or the driver.
	a.mustCall("session/prompt", map[string]any{"sessionId": sid, "prompt": []any{}})
}

func TestRouterDetachIsIdempotentAndKeepsDetached(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	b.mustCall(protocol.ACPDetach, map[string]any{"sessionId": sid})
	b.mustCall(protocol.ACPDetach, map[string]any{"sessionId": sid})
	h.agent.send(h.agent.update(sid, "after"))
	a.waitIncoming("session/update", 1)
	b.barrier()
	require.Empty(t, b.incoming("session/update"), "a detached client stays detached")
	_, e := b.call(protocol.ACPDetach, map[string]any{"sessionId": "ghost"})
	require.NotNil(t, e)
}

func TestRouterDriverDetachLeavesNoDriver(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	a.mustCall(protocol.ACPDetach, map[string]any{"sessionId": sid})
	var live protocol.ACPListLiveResult
	require.NoError(t, json.Unmarshal(b.mustCall(protocol.ACPListLive, map[string]any{}), &live))
	require.False(t, live.Sessions[0].HasDriver)
	_, e := a.call("session/prompt", map[string]any{"sessionId": sid})
	require.Equal(t, protocol.ACPCodeNotDriver, e.Code, "a detached driver is an outsider")
	require.Empty(t, h.agent.requests("session/cancel"), "a detach does not abort the run")
}

func TestRouterTakeForwardedWithLiveDriverFlag(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(map[string]any{"terminal": true})
	sid := a.newSession("/w")
	b.attach(sid)

	h.agent.setBusy(sid, true)
	_, e := b.call(protocol.ACPTake, map[string]any{"sessionId": sid})
	require.NotNil(t, e)
	require.Equal(t, protocol.ACPCodeBusy, e.Code, "a busy session with a live foreign driver is not taken")
	take := h.agent.requests(protocol.ACPTake)
	require.Len(t, take, 1)
	require.True(t, take[0].Route.LiveDriver)
	require.Equal(t, "c2", take[0].Route.ClientID)
	require.Equal(t, uint64(1), take[0].Route.DriverGen)
	a.mustCall("session/prompt", map[string]any{"sessionId": sid, "prompt": []any{}}) // A still drives
}

func TestRouterTakeOfIdleSessionAndCopiedGeneration(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(map[string]any{"terminal": true})
	sid := a.newSession("/w")
	b.attach(sid)

	var res protocol.ACPTakeResult
	require.NoError(t, json.Unmarshal(b.mustCall(protocol.ACPTake, map[string]any{"sessionId": sid}), &res))
	require.Equal(t, uint64(2), res.DriverGen)

	b.mustCall("session/prompt", map[string]any{"sessionId": sid, "prompt": []any{}})
	prompt := h.agent.requests("session/prompt")[0]
	require.Equal(t, uint64(2), prompt.Route.DriverGen, "the router copies the generation that the host returned")
	require.Equal(t, "c2", prompt.Route.ClientID)
	require.JSONEq(t, `{"terminal":true}`, string(prompt.Route.Capabilities), "the new driver's capabilities apply")

	_, e := a.call("session/prompt", map[string]any{"sessionId": sid})
	require.Equal(t, protocol.ACPCodeNotDriver, e.Code, "the former driver is an observer now")

	// The driver can take again without a new host call.
	require.NoError(t, json.Unmarshal(b.mustCall(protocol.ACPTake, map[string]any{"sessionId": sid}), &res))
	require.Equal(t, uint64(2), res.DriverGen)
	require.Len(t, h.agent.requests(protocol.ACPTake), 1)
}

func TestRouterTakeWithoutDriverWhileBusy(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	h.agent.setBusy(sid, true)
	a.close()
	a.waitClosed()
	eventually(t, "driver gone", func() bool {
		var live protocol.ACPListLiveResult
		_ = json.Unmarshal(b.mustCall(protocol.ACPListLive, map[string]any{}), &live)
		return len(live.Sessions) == 1 && !live.Sessions[0].HasDriver
	})

	var res protocol.ACPTakeResult
	require.NoError(t, json.Unmarshal(b.mustCall(protocol.ACPTake, map[string]any{"sessionId": sid}), &res))
	require.Equal(t, uint64(2), res.DriverGen)
	take := h.agent.requests(protocol.ACPTake)
	require.False(t, take[0].Route.LiveDriver, "no live driver: the host accepts even a busy session")
}

func TestRouterOnlyOneTakeAtATime(t *testing.T) {
	h := startHarness(t)
	a, b, c := h.ready(nil), h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	c.attach(sid)
	h.agent.setSilent(protocol.ACPTake, true)

	first := b.request(protocol.ACPTake, map[string]any{"sessionId": sid})
	h.agent.waitRequests(protocol.ACPTake, 1)
	_, e := c.call(protocol.ACPTake, map[string]any{"sessionId": sid})
	require.NotNil(t, e)
	require.Equal(t, protocol.ACPCodeBusy, e.Code, "a second take waits for the first host result")

	req := h.agent.requests(protocol.ACPTake)[0]
	h.agent.result(req.ID, protocol.ACPTakeResult{SessionID: sid, DriverGen: 2})
	_, e = b.await(first)
	require.Nil(t, e)
}

func TestRouterLateResultsAfterClientLeft(t *testing.T) {
	t.Run("new session", func(t *testing.T) {
		h := startHarness(t)
		h.agent.setSilent("session/new", true)
		a := h.ready(nil)
		a.request("session/new", map[string]any{"cwd": "/late"})
		req := h.agent.waitRequests("session/new", 1)[0]
		a.close()
		a.waitClosed()
		b := h.ready(nil)
		eventually(t, "client gone", func() bool { return h.clientCount() == 1 })

		h.agent.result(req.ID, map[string]any{"sessionId": "late1", "_meta": map[string]any{protocol.ACPRouteMetaKey: protocol.ACPRouteMeta{DriverGen: 1}}})
		eventually(t, "session registered", func() bool {
			var live protocol.ACPListLiveResult
			_ = json.Unmarshal(b.mustCall(protocol.ACPListLive, map[string]any{}), &live)
			return len(live.Sessions) == 1 && !live.Sessions[0].HasDriver && live.Sessions[0].Cwd == "/late"
		})
		require.Equal(t, protocol.ACPRoleObserver, b.attach("late1").Role)
	})
	t.Run("follow", func(t *testing.T) {
		h := startHarness(t)
		a := h.ready(nil)
		sid := a.newSession("/w")
		h.agent.setSilent(protocol.ACPFollow, true)
		a.request(protocol.ACPFollow, map[string]any{"sessionId": sid})
		req := h.agent.waitRequests(protocol.ACPFollow, 1)[0]
		a.close()
		a.waitClosed()
		h.agent.result(req.ID, protocol.ACPFollowResult{SubscriptionID: "orphan"})
		unf := h.agent.waitRequests(protocol.ACPUnfollow, 1)
		require.Equal(t, "orphan", stringParam(unf[0].Params, "subscriptionId"), "a follow with no owner is ended, not leaked")
	})
	t.Run("follow after detach", func(t *testing.T) {
		h := startHarness(t)
		a, b := h.ready(nil), h.ready(nil)
		sid := a.newSession("/w")
		b.attach(sid)
		h.agent.setSilent(protocol.ACPFollow, true)
		b.request(protocol.ACPFollow, map[string]any{"sessionId": sid})
		req := h.agent.waitRequests(protocol.ACPFollow, 1)[0]
		b.mustCall(protocol.ACPDetach, map[string]any{"sessionId": sid})
		mark := b.mark()
		h.agent.result(req.ID, protocol.ACPFollowResult{SubscriptionID: "stale"})
		h.agent.waitRequests(protocol.ACPUnfollow, 1)
		b.barrier()
		for _, m := range b.since(mark) {
			require.NotContains(t, string(m.raw), "stale", "the detached client gets no follow result")
		}
	})
	t.Run("take", func(t *testing.T) {
		h := startHarness(t)
		a, b := h.ready(nil), h.ready(nil)
		sid := a.newSession("/w")
		b.attach(sid)
		h.agent.setSilent(protocol.ACPTake, true)
		b.request(protocol.ACPTake, map[string]any{"sessionId": sid})
		req := h.agent.waitRequests(protocol.ACPTake, 1)[0]
		b.close()
		b.waitClosed()
		eventually(t, "client gone", func() bool { return h.clientCount() == 1 })
		h.agent.setGen(sid, 2)
		h.agent.result(req.ID, protocol.ACPTakeResult{SessionID: sid, DriverGen: 2})

		eventually(t, "driver cleared", func() bool {
			_, e := a.call("session/prompt", map[string]any{"sessionId": sid})
			return e != nil && e.Code == protocol.ACPCodeNotDriver
		})
		var live protocol.ACPListLiveResult
		require.NoError(t, json.Unmarshal(a.mustCall(protocol.ACPListLive, map[string]any{}), &live))
		require.False(t, live.Sessions[0].HasDriver, "an absent caller does not become the live driver")
		h.agent.setSilent(protocol.ACPTake, false)
		var res protocol.ACPTakeResult
		require.NoError(t, json.Unmarshal(a.mustCall(protocol.ACPTake, map[string]any{"sessionId": sid}), &res))
		require.Equal(t, uint64(3), res.DriverGen)
	})
}

func (h *harness) clientCount() int {
	n := -1
	h.srv.r.call(func() { n = len(h.srv.r.clients) })
	return n
}

func TestRouterClientLossKeepsAgentStream(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	h.agent.setSilent("session/prompt", true)
	a.request("session/prompt", map[string]any{"sessionId": sid, "prompt": []any{}})
	held := h.agent.waitRequests("session/prompt", 1)[0]

	a.close()
	a.waitClosed()
	eventually(t, "client gone", func() bool { return h.clientCount() == 1 })
	select {
	case <-h.srv.Done():
		t.Fatal("a client loss stopped the leader")
	default:
	}
	require.Empty(t, h.agent.requests("session/cancel"), "the run is not cancelled")

	// The agent answers the held prompt later. Nobody is there to read it.
	h.agent.result(held.ID, map[string]any{"stopReason": "end_turn"})
	h.agent.send(h.agent.update(sid, "still running"))
	b.waitIncoming("session/update", 1)

	h.agent.setSilent("session/prompt", false)
	b.mustCall(protocol.ACPTake, map[string]any{"sessionId": sid})
	b.mustCall("session/prompt", map[string]any{"sessionId": sid, "prompt": []any{}})
}

func TestServerDisconnectFrameDetaches(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	b.attach(sid)
	a.write(protocol.LeaderFrame{Type: protocol.LeaderFrameDisconnect})
	a.waitClosed()
	eventually(t, "driver cleared", func() bool {
		var live protocol.ACPListLiveResult
		_ = json.Unmarshal(b.mustCall(protocol.ACPListLive, map[string]any{}), &live)
		return len(live.Sessions) == 1 && !live.Sessions[0].HasDriver && live.Sessions[0].Subscribers == 1
	})
}

func TestServerPingPong(t *testing.T) {
	h := startHarness(t)
	a := h.client()
	a.write(protocol.LeaderFrame{Type: protocol.LeaderFramePing})
	eventually(t, "pong", func() bool {
		for _, typ := range a.frameTypes() {
			if typ == protocol.LeaderFramePong {
				return true
			}
		}
		return false
	})
}
