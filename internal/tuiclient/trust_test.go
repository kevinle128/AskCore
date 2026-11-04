package tuiclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/internal/tui"
	"AskCore/pkg/protocol"
)

// refuse answers a request with an error frame of kind.
func refuse(sc *script, kind protocol.ACPErrorKind, code int) func(sent) (any, string, bool) {
	return func(s sent) (any, string, bool) {
		sc.link.replies <- json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":%d,"message":"refused","data":{"kind":%q}}}`, s.ID, code, kind))
		return nil, "", true
	}
}

func TestTUIClientTrustUnsupportedLeader(t *testing.T) {
	sc := newScript(nil)
	sc.rules = map[string]func(sent) (any, string, bool){
		protocol.ACPTrustState: refuse(sc, protocol.ACPErrUnsupported, protocol.ACPErrorCode(protocol.ACPErrUnsupported)),
		protocol.ACPTrustSet:   refuse(sc, "", -32601),
	}
	c := openScripted(t, sc, tui.ModeLeader, "leader-a")
	defer func() { require.NoError(t, c.Close()) }()
	ctx := context.Background()
	_, err := c.TrustState(ctx, "/w")
	require.ErrorIs(t, err, ErrTrustNotAvailable, "an old leader answers unsupported")
	_, err = c.SetTrust(ctx, protocol.ACPTrustSetRequest{Cwd: "/w", Target: protocol.ACPTrustTargetFolder, Decision: protocol.ACPTrustDecisionTrust})
	require.ErrorIs(t, err, ErrTrustNotAvailable, "a host without the method answers method not found")
}

func TestTUIClientTrustCalls(t *testing.T) {
	sc := newScript(map[string]func(sent) (any, string, bool){
		protocol.ACPTrustState: fixed(protocol.ACPTrustStateResult{Root: "/w", Source: "prompt", NeedsDecision: true, Gated: []string{".ask/settings.json"}}),
		protocol.ACPTrustSet:   fixed(protocol.ACPTrustStateResult{Root: "/w", Trusted: true, Source: "saved", EntryPath: "/w"}),
		"session/new":          fixed(map[string]any{"sessionId": "s1"}),
		protocol.ACPFollow:     fixed(protocol.ACPFollowResult{SubscriptionID: "sub", Cursor: protocol.ACPCursor{Epoch: "e", Seq: 1}}),
	})
	c := openScripted(t, sc, tui.ModeInProcess, "")
	defer func() { require.NoError(t, c.Close()) }()
	ctx := context.Background()

	st, err := c.TrustState(ctx, "/w")
	require.NoError(t, err)
	require.True(t, st.NeedsDecision)
	set, err := c.SetTrust(ctx, protocol.ACPTrustSetRequest{Cwd: "/w", Target: protocol.ACPTrustTargetFolder, Decision: protocol.ACPTrustDecisionTrust})
	require.NoError(t, err)
	require.True(t, set.Trusted)
	sent := sc.byMethod(protocol.ACPTrustSet)
	require.Len(t, sent, 1)
	require.JSONEq(t, `{"cwd":"/w","target":"folder","decision":"trust"}`, string(sent[0].Params))

	// Another failure stays an error of its own kind.
	sc.mu.Lock()
	sc.rules[protocol.ACPTrustState] = refuse(sc, protocol.ACPErrInternal, protocol.ACPErrorCode(protocol.ACPErrInternal))
	sc.mu.Unlock()
	_, err = c.TrustState(ctx, "/w")
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrTrustNotAvailable))

	// An empty override sends no _meta; another override goes into _meta.
	_, err = c.NewSession(ctx, "/w", "")
	require.NoError(t, err)
	_, err = c.NewSession(ctx, "/w", protocol.ACPTrustDeny)
	require.NoError(t, err)
	news := sc.byMethod("session/new")
	require.Len(t, news, 2)
	require.NotContains(t, string(news[0].Params), "_meta")
	require.JSONEq(t, `{"cwd":"/w","mcpServers":[],"_meta":{"ask.dev/trust":"deny"}}`, string(news[1].Params))
}

// byMethod returns the requests of method in send order.
func (sc *script) byMethod(method string) []sent {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	var out []sent
	for _, s := range sc.sent {
		if s.Method == method {
			out = append(out, s)
		}
	}
	return out
}
