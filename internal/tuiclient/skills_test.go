package tuiclient

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/internal/tui"
	"AskCore/pkg/protocol"
)

func TestClientSkillsReadsTheCurrentSession(t *testing.T) {
	want := protocol.ACPSkillsResult{Skills: []protocol.ACPSkill{
		{Name: "demo", Description: "A demo", Path: "/p/demo/SKILL.md", Scope: protocol.ACPSkillProject, ModelInvocable: true},
	}}
	sc := newScript(map[string]func(sent) (any, string, bool){protocol.ACPSkills: fixed(want)})
	c := openScripted(t, sc, tui.ModeLeader, "leader-a")
	defer func() { require.NoError(t, c.Close()) }()

	_, err := c.Skills(context.Background())
	require.ErrorIs(t, err, tui.ErrNoSession, "a client without a session sends nothing")

	c.mu.Lock()
	c.sess = sessionState{has: true, id: "s"}
	c.mu.Unlock()
	got, err := c.Skills(context.Background())
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, protocol.ACPSessionRequest{SessionID: "s"}, decode[protocol.ACPSessionRequest](t, sc.last(protocol.ACPSkills).Params))
}

func TestClientSkillsFromAnOlderLeaderIsAnUnsupportedRequestError(t *testing.T) {
	sc := newScript(nil)
	sc.rules = map[string]func(sent) (any, string, bool){
		protocol.ACPSkills: refuse(sc, protocol.ACPErrUnsupported, protocol.ACPErrorCode(protocol.ACPErrUnsupported)),
	}
	c := openScripted(t, sc, tui.ModeLeader, "leader-a")
	defer func() { require.NoError(t, c.Close()) }()
	c.mu.Lock()
	c.sess = sessionState{has: true, id: "s"}
	c.mu.Unlock()

	_, err := c.Skills(context.Background())
	var re *tui.RequestError
	require.ErrorAs(t, err, &re)
	require.Equal(t, string(protocol.ACPErrUnsupported), re.Kind)
}
