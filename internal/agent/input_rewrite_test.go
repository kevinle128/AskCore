package agent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/providers/faux"
	"AskCore/pkg/protocol"
)

func rewriteGo(text string) (string, bool) {
	rest, ok := strings.CutPrefix(text, "/go ")
	if !ok {
		return "", false
	}
	return "EXPANDED " + rest, true
}

func rewriteAgent(t *testing.T, rewrite func(string) (string, bool), steps ...faux.Step) (*agent.Agent, *faux.Provider) {
	t.Helper()
	p, m := newFaux(t)
	p.Set(steps...)
	return newAgent(t, p, m, func(c *agent.Config) { c.RewriteInput = rewrite }), p
}

func TestRewriteInputChangesWhatThePromptSendsToTheModel(t *testing.T) {
	t.Parallel()
	a, p := rewriteAgent(t, rewriteGo, faux.Say("a"), faux.Say("b"))
	require.NoError(t, a.Prompt(context.Background(), user("/go one")))
	require.NoError(t, a.Prompt(context.Background(), user("plain")))
	assert.Equal(t, []string{"EXPANDED one"}, requestUsers(p, 0))
	assert.Equal(t, []string{"EXPANDED one", "plain"}, requestUsers(p, 1))
}

func TestRewriteInputAppliesToSteerAndFollowUp(t *testing.T) {
	t.Parallel()
	a, p := rewriteAgent(t, rewriteGo, faux.Say("a"), faux.Say("b"))
	mustSteer(t, a, "/go steered")
	idle(t, a)
	mustFollowUp(t, a, "/go followed")
	idle(t, a)
	assert.Equal(t, []string{"EXPANDED steered"}, requestUsers(p, 0))
	assert.Equal(t, []string{"EXPANDED steered", "EXPANDED followed"}, requestUsers(p, 1))
}

func TestRewriteInputLeavesImagesAndOtherBlocksAlone(t *testing.T) {
	t.Parallel()
	a, p := rewriteAgent(t, rewriteGo, faux.Say("a"))
	msg := protocol.UserMessage{Content: []protocol.UserBlock{protocol.Image{Data: "x", MimeType: "image/png"}, protocol.Text{Text: "/go later"}}}
	require.NoError(t, a.Prompt(context.Background(), msg))
	assert.Equal(t, []string{"/go later"}, requestUsers(p, 0), "only the first block can start a command")
}

func TestRewriteInputKeepsTheCallersMessage(t *testing.T) {
	t.Parallel()
	a, _ := rewriteAgent(t, rewriteGo, faux.Say("a"))
	msg := &protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "/go one"}, protocol.Text{Text: "tail"}}}
	require.NoError(t, a.Prompt(context.Background(), msg))
	assert.Equal(t, "/go one", msg.Content[0].(protocol.Text).Text)
}

func TestRewriteInputKeepsOtherBlocksOfARewrittenMessage(t *testing.T) {
	t.Parallel()
	a, p := rewriteAgent(t, rewriteGo, faux.Say("a"))
	msg := protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "/go one"}, protocol.Text{Text: "tail"}}}
	require.NoError(t, a.Prompt(context.Background(), msg))
	assert.Equal(t, []string{"EXPANDED one", "tail"}, requestUsers(p, 0))
}

func TestNoRewriteInputSendsTheTextAsTyped(t *testing.T) {
	t.Parallel()
	a, p := rewriteAgent(t, nil, faux.Say("a"))
	require.NoError(t, a.Prompt(context.Background(), user("/go one")))
	assert.Equal(t, []string{"/go one"}, requestUsers(p, 0))
}
