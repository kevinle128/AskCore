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
	_, err = c.NewSession(ctx, "/w", tui.NewSessionOptions{})
	require.NoError(t, err)
	_, err = c.NewSession(ctx, "/w", tui.NewSessionOptions{Trust: protocol.ACPTrustDeny})
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

func TestTUIClientSessionFlags(t *testing.T) {
	sc := newScript(map[string]func(sent) (any, string, bool){
		"session/new":      fixed(map[string]any{"sessionId": "s1"}),
		protocol.ACPFollow: fixed(protocol.ACPFollowResult{SubscriptionID: "sub", Cursor: protocol.ACPCursor{Epoch: "e", Seq: 1}}),
	})
	c := openScripted(t, sc, tui.ModeInProcess, "")
	defer func() { require.NoError(t, c.Close()) }()
	ctx := context.Background()
	flags := protocol.ACPSessionOptions{AppendSystemPrompt: []string{"be terse"}, Tools: []string{"read"}}

	info, err := c.NewSession(ctx, "/w", tui.NewSessionOptions{})
	require.NoError(t, err)
	require.False(t, info.FlagsIgnored, "no flags, nothing to acknowledge")
	info, err = c.NewSession(ctx, "/w", tui.NewSessionOptions{Flags: flags})
	require.NoError(t, err)
	require.True(t, info.FlagsIgnored, "flags without an ack mean an older host")
	_, err = c.NewSession(ctx, "/w", tui.NewSessionOptions{Trust: protocol.ACPTrustApprove, Flags: protocol.ACPSessionOptions{NoTools: true}})
	require.NoError(t, err)

	news := sc.byMethod("session/new")
	require.Len(t, news, 3)
	require.NotContains(t, string(news[0].Params), "_meta")
	require.JSONEq(t, `{"cwd":"/w","mcpServers":[],"_meta":{"ask.dev/session":{"appendSystemPrompt":["be terse"],"tools":["read"]}}}`, string(news[1].Params))
	require.JSONEq(t, `{"cwd":"/w","mcpServers":[],"_meta":{"ask.dev/trust":"approve","ask.dev/session":{"noTools":true}}}`, string(news[2].Params))
}

func TestTUIClientSessionFlagsAcknowledged(t *testing.T) {
	sc := newScript(map[string]func(sent) (any, string, bool){
		"session/new":      fixed(map[string]any{"sessionId": "s1", "_meta": map[string]any{protocol.ACPSessionOptionsMetaKey: true}}),
		protocol.ACPFollow: fixed(protocol.ACPFollowResult{SubscriptionID: "sub", Cursor: protocol.ACPCursor{Epoch: "e", Seq: 1}}),
	})
	c := openScripted(t, sc, tui.ModeInProcess, "")
	defer func() { require.NoError(t, c.Close()) }()
	info, err := c.NewSession(context.Background(), "/w", tui.NewSessionOptions{Flags: protocol.ACPSessionOptions{NoTools: true}})
	require.NoError(t, err)
	require.False(t, info.FlagsIgnored)
}

// A host that does not echo the model options is older than this client and
// dropped them. The start fails with a clear message, and the session that the
// host made is closed.
func TestSessionStartFailsWhenHostDoesNotEchoModel(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags protocol.ACPSessionOptions
		echo  any
		want  string
	}{
		{"model", protocol.ACPSessionOptions{Model: "openai/gpt-5.5:high"}, nil, "the leader did not apply --model; restart it with this build"},
		{"thinking", protocol.ACPSessionOptions{Thinking: protocol.ThinkingLow}, nil, "the leader did not apply --thinking; restart it with this build"},
		{"models", protocol.ACPSessionOptions{Models: []string{"openai/*"}}, nil, "the leader did not apply --models; restart it with this build"},
		{"part of the echo", protocol.ACPSessionOptions{Model: "m", Models: []string{"openai/*"}}, protocol.ACPAppliedOptions{Model: "m"}, "the leader did not apply --models; restart it with this build"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta := map[string]any{protocol.ACPSessionOptionsMetaKey: true}
			if tc.echo != nil {
				meta[protocol.ACPAppliedOptionsMetaKey] = tc.echo
			}
			sc := newScript(map[string]func(sent) (any, string, bool){
				"session/new":      fixed(map[string]any{"sessionId": "s1", "_meta": meta}),
				"session/close":    fixed(map[string]any{}),
				protocol.ACPFollow: fixed(protocol.ACPFollowResult{SubscriptionID: "sub", Cursor: protocol.ACPCursor{Epoch: "e", Seq: 1}}),
			})
			c := openScripted(t, sc, tui.ModeInProcess, "")
			defer func() { require.NoError(t, c.Close()) }()

			_, err := c.NewSession(context.Background(), "/w", tui.NewSessionOptions{Flags: tc.flags})
			require.EqualError(t, err, tc.want)
			closes := sc.byMethod("session/close")
			require.Len(t, closes, 1)
			require.JSONEq(t, `{"sessionId":"s1"}`, string(closes[0].Params))
			require.Empty(t, sc.byMethod(protocol.ACPFollow), "the client does not follow the dropped session")
		})
	}
}

func TestSessionStartAcceptsEchoedModelOptions(t *testing.T) {
	flags := protocol.ACPSessionOptions{Model: "openai/gpt-5.5:high", Thinking: protocol.ThinkingLow, Models: []string{"openai/*"}}
	sc := newScript(map[string]func(sent) (any, string, bool){
		"session/new": fixed(map[string]any{"sessionId": "s1", "_meta": map[string]any{
			protocol.ACPSessionOptionsMetaKey: true, protocol.ACPAppliedOptionsMetaKey: flags.ModelOptions(),
		}}),
		protocol.ACPFollow: fixed(protocol.ACPFollowResult{SubscriptionID: "sub", Cursor: protocol.ACPCursor{Epoch: "e", Seq: 1}}),
	})
	c := openScripted(t, sc, tui.ModeInProcess, "")
	defer func() { require.NoError(t, c.Close()) }()
	info, err := c.NewSession(context.Background(), "/w", tui.NewSessionOptions{Flags: flags})
	require.NoError(t, err)
	require.False(t, info.FlagsIgnored)
	require.JSONEq(t, `{"cwd":"/w","mcpServers":[],"_meta":{"ask.dev/session":{"model":"openai/gpt-5.5:high","thinking":"low","models":["openai/*"]}}}`,
		string(sc.byMethod("session/new")[0].Params))
}
