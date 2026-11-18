package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

func attemptsOf(log sessions.Writer) []string {
	var ids []string
	for _, d := range logged[sessions.RequestDelta](log) {
		ids = append(ids, d.AttemptID)
	}
	return ids
}

func positionOfUser(t *testing.T, log sessions.Writer, text string) int {
	t.Helper()
	for i, e := range log.Entries() {
		if me, ok := e.(sessions.MessageEntry); ok {
			if u, ok := me.Message.(protocol.UserMessage); ok && userTexts([]protocol.Message{u})[0] == text {
				return i
			}
		}
	}
	require.Failf(t, "user message not found", "%q", text)
	return -1
}

func TestRebuildIgnoresLaterContextEdit(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("a1"), faux.Say("a2"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, withLog(log))
	require.NoError(t, a.Prompt(context.Background(), user("one")))
	before, err := agent.RebuildRequest(log.Entries(), attemptsOf(log)[0])
	require.NoError(t, err)

	_, err = log.Append(sessions.ContextEdit{Target: positionOfUser(t, log, "one")})
	require.NoError(t, err)
	require.NoError(t, a.Prompt(context.Background(), user("two")))

	ids := attemptsOf(log)
	require.Len(t, ids, 2)
	first, err := agent.RebuildRequest(log.Entries(), ids[0])
	require.NoError(t, err)
	assert.Equal(t, before, first, "the first request is the same after a later edit")
	assert.Contains(t, userTexts(first.Messages), "one")
	second, err := agent.RebuildRequest(log.Entries(), ids[1])
	require.NoError(t, err)
	assert.Equal(t, []string{"two"}, userTexts(second.Messages), "an edit before an attempt shapes its request")
	assert.Equal(t, userTexts(p.Requests()[1].Transcript.Messages), userTexts(second.Messages))
}

func TestContextEditOmitsMessageFromNextRequest(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("ok"))
	log := &sessions.MemoryLog{}
	_, err := log.Append(
		sessions.MessageEntry{Message: user("remove me")},
		sessions.MessageEntry{Message: protocol.AssistantMessage{Content: []protocol.AssistantBlock{protocol.Text{Text: "earlier answer"}}, StopReason: protocol.StopStop}},
		sessions.ContextEdit{Target: 0},
	)
	require.NoError(t, err)
	a := newAgent(t, p, m, withLog(log))

	require.NoError(t, a.Prompt(context.Background(), user("next")))

	assert.Equal(t, []string{"next"}, userTexts(p.Requests()[0].Transcript.Messages), "the request lacks the target")
	assert.Equal(t, "remove me", userTexts([]protocol.Message{log.Entries()[0].(sessions.MessageEntry).Message})[0], "the log still holds it")
}
