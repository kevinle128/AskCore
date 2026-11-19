package tuiclient

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/internal/tui"
	"AskCore/pkg/protocol"
)

func followRule() func(sent) (any, string, bool) {
	return fixed(protocol.ACPFollowResult{SubscriptionID: "sub", Cursor: protocol.ACPCursor{Epoch: "e", Seq: 1}})
}

// A load sends session/load with the trust and flags of the start, makes the
// stored session current and follows it. An in-process agent has one client,
// which drives, and a flag that the host cannot apply to a loaded session never
// closes the session.
func TestNewSessionLoadSendsLoadWithSameMeta(t *testing.T) {
	sc := newScript(map[string]func(sent) (any, string, bool){
		"session/load":     fixed(map[string]any{"_meta": map[string]any{protocol.ACPSessionOptionsMetaKey: true}}),
		protocol.ACPFollow: followRule(),
	})
	c := openScripted(t, sc, tui.ModeInProcess, "")
	defer func() { require.NoError(t, c.Close()) }()

	info, err := c.NewSession(context.Background(), "/w", tui.NewSessionOptions{
		Trust: protocol.ACPTrustDeny, Load: "1863000000000000001",
		Flags: protocol.ACPSessionOptions{Model: "faux/faux-1", NoTools: true},
	})
	require.NoError(t, err)
	require.Equal(t, "1863000000000000001", info.ID)
	require.Equal(t, protocol.ACPRoleDriver, info.Role)
	require.False(t, info.FlagsIgnored)
	require.Empty(t, sc.byMethod("session/new"))
	require.Empty(t, sc.byMethod("session/close"), "a load never closes the session")
	require.Empty(t, sc.byMethod(protocol.ACPAttach), "an in-process agent has no attach")
	loads := sc.byMethod("session/load")
	require.Len(t, loads, 1)
	require.JSONEq(t, `{"cwd":"/w","mcpServers":[],"sessionId":"1863000000000000001","_meta":{"ask.dev/trust":"deny","ask.dev/session":{"model":"faux/faux-1","noTools":true}}}`, string(loads[0].Params))
	follows := sc.byMethod(protocol.ACPFollow)
	require.Len(t, follows, 1)
	require.Contains(t, string(follows[0].Params), `"sessionId":"1863000000000000001"`)
}

// A leader that is older than this build answers session/load with
// unsupported. The error says what to do.
func TestNewSessionLoadOnOldLeaderExplainsRestart(t *testing.T) {
	sc := newScript(nil)
	sc.rules = map[string]func(sent) (any, string, bool){
		"session/load": refuse(sc, protocol.ACPErrUnsupported, protocol.ACPErrorCode(protocol.ACPErrUnsupported)),
	}
	c := openScripted(t, sc, tui.ModeLeader, "leader-a")
	defer func() { require.NoError(t, c.Close()) }()

	_, err := c.NewSession(context.Background(), "/w", tui.NewSessionOptions{Load: "42"})
	require.EqualError(t, err, "the leader does not support session load; restart it with ask leader stop")
	require.Empty(t, sc.byMethod("session/new"), "no new session replaces the one that was asked for")
	require.Empty(t, sc.byMethod(protocol.ACPFollow))
}

// Another refusal of a load keeps its kind for the UI.
func TestNewSessionLoadRefusalKeepsKind(t *testing.T) {
	sc := newScript(nil)
	sc.rules = map[string]func(sent) (any, string, bool){
		"session/load": refuse(sc, protocol.ACPErrSessionInUse, protocol.ACPCodeSessionInUse),
	}
	c := openScripted(t, sc, tui.ModeLeader, "leader-a")
	defer func() { require.NoError(t, c.Close()) }()

	_, err := c.NewSession(context.Background(), "/w", tui.NewSessionOptions{Load: "42"})
	var re *tui.RequestError
	require.ErrorAs(t, err, &re)
	require.Equal(t, string(protocol.ACPErrSessionInUse), re.Kind)
}

// On a leader the loader learns its role from attach. A session that the
// router just registered for it has the loader as driver. A joined session
// with no driver is taken; one with a driver stays with it.
func TestNewSessionLoadRoleOnLeader(t *testing.T) {
	for _, tc := range []struct {
		name       string
		role       string
		hasDriver  bool
		wantRole   string
		wantTakes  int
		takeResult any
	}{
		{"registered for the loader", protocol.ACPRoleDriver, true, protocol.ACPRoleDriver, 0, nil},
		{"joined and nobody drives", protocol.ACPRoleObserver, false, protocol.ACPRoleDriver, 1, protocol.ACPTakeResult{SessionID: "42", DriverGen: 2}},
		{"joined and another client drives", protocol.ACPRoleObserver, true, protocol.ACPRoleObserver, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc := newScript(map[string]func(sent) (any, string, bool){
				"session/load":       fixed(map[string]any{}),
				protocol.ACPAttach:   fixed(protocol.ACPAttachResult{SessionID: "42", Role: tc.role}),
				protocol.ACPListLive: fixed(protocol.ACPListLiveResult{Sessions: []protocol.ACPLiveSession{{SessionID: "42", HasDriver: tc.hasDriver}}}),
				protocol.ACPTake:     fixed(tc.takeResult),
				protocol.ACPFollow:   followRule(),
			})
			c := openScripted(t, sc, tui.ModeLeader, "leader-a")
			defer func() { require.NoError(t, c.Close()) }()

			info, err := c.NewSession(context.Background(), "/w", tui.NewSessionOptions{Load: "42"})
			require.NoError(t, err)
			require.Equal(t, tc.wantRole, info.Role)
			require.Len(t, sc.byMethod(protocol.ACPTake), tc.wantTakes)
			require.Len(t, sc.byMethod(protocol.ACPFollow), 1)
			require.Empty(t, sc.byMethod("session/close"))
		})
	}
}
