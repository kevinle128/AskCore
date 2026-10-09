package leader

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func TestNonMemberDetachFencesPendingTake(t *testing.T) {
	h := startHarness(t)
	a, b := h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	h.agent.setSilent(protocol.ACPTake, true)
	id := b.request(protocol.ACPTake, map[string]any{"sessionId": sid})
	req := h.agent.waitRequests(protocol.ACPTake, 1)[0]
	b.mustCall(protocol.ACPDetach, map[string]any{"sessionId": sid})
	// Reattaching must not restore ownership of the request before detach.
	b.attach(sid)
	h.agent.result(req.ID, protocol.ACPTakeResult{SessionID: sid, DriverGen: 2})
	_, e := b.await(id)
	require.NotNil(t, e)
	require.Equal(t, protocol.ACPErrorCode(protocol.ACPErrCancelled), e.Code)
	h.srv.r.call(func() {
		s := h.srv.r.sessions[sid]
		require.Nil(t, s.driver)
		require.Equal(t, uint64(2), s.gen)
	})
}

func TestConcurrentTakesCommitOneDriver(t *testing.T) {
	h := startHarness(t)
	a, b, c := h.ready(nil), h.ready(nil), h.ready(nil)
	sid := a.newSession("/w")
	h.agent.setSilent(protocol.ACPTake, true)
	id := b.request(protocol.ACPTake, map[string]any{"sessionId": sid})
	req := h.agent.waitRequests(protocol.ACPTake, 1)[0]
	_, e := c.call(protocol.ACPTake, map[string]any{"sessionId": sid})
	require.NotNil(t, e)
	require.Equal(t, protocol.ACPCodeBusy, e.Code)
	require.Len(t, h.agent.requests(protocol.ACPTake), 1)
	h.agent.result(req.ID, protocol.ACPTakeResult{SessionID: sid, DriverGen: 2})
	_, e = b.await(id)
	require.Nil(t, e)
	h.srv.r.call(func() {
		s := h.srv.r.sessions[sid]
		require.Equal(t, h.srv.r.clients[b.reg.Info.ClientID], s.driver)
		require.Equal(t, uint64(2), s.gen)
	})
}

func TestIdleBarrierIncludesRequestsQueuedBeforeHostAdmission(t *testing.T) {
	calls := 0
	q := newQueue[[]byte]()
	r := newRouter(Config{QuiesceIfIdle: func() bool { calls++; return true }}, q)
	c := &client{id: "c1"}
	m, err := parseMessage([]byte(`{"jsonrpc":"2.0","id":1,"method":"authenticate","params":{}}`))
	require.NoError(t, err)
	require.True(t, r.forward(c, m, protocol.ACPRouteMeta{ClientID: c.id}, nil))
	// No link writer or host exists: the request is still in the outbound queue.
	require.False(t, r.quiesce())
	require.Zero(t, calls, "host admission must stay open until the queued request finishes")
	r.ids.DropClient(c.id)
	require.False(t, r.quiesce(), "disconnect does not finish host work")
	require.Equal(t, 1, len(q.items))
	out, err := parseMessage(q.items[0])
	require.NoError(t, err)
	r.ids.Restore(resultResponse(out.id, map[string]any{}))
	require.True(t, r.quiesce())
	require.Equal(t, 1, calls)
}

func TestForwardChecksRewrittenLineBoundary(t *testing.T) {
	// Legal socket envelopes cannot reach this boundary. Drive the shared
	// transformation directly to check the limit after route and id expansion.
	for _, extra := range []int{-1, 0, 1} {
		t.Run(strconv.Itoa(MaxLine+extra), func(t *testing.T) {
			q := newQueue[[]byte]()
			r := newRouter(Config{}, q)
			c := &client{id: "c1", q: newQueue[outItem]()}
			meta := protocol.ACPRouteMeta{ClientID: c.id}
			base := `{"jsonrpc":"2.0","id":1,"method":"authenticate","params":{"padding":""}}`
			m, err := parseMessage([]byte(base))
			require.NoError(t, err)
			params, err := m.params()
			require.NoError(t, err)
			fields := m.fields
			fields["params"] = injectRoute(params, meta)
			fields["id"] = json.RawMessage(`"c1:1"`)
			padding := MaxLine + extra - len(marshalPlain(fields))
			m, err = parseMessage([]byte(strings.Replace(base, `"padding":""`, `"padding":"`+strings.Repeat("x", padding)+`"`, 1)))
			require.NoError(t, err)
			ok := r.forward(c, m, meta, nil)
			require.Equal(t, extra <= 0, ok)
			if ok {
				require.Len(t, q.items[0], MaxLine+extra)
			} else {
				require.Empty(t, q.items)
				require.False(t, r.ids.hasPending(), "a refused line leaves no idle blocker")
			}
		})
	}
}
