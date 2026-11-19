package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

// resumedLog is a log of an earlier session: its context file text, one
// finished exchange and nothing open.
func resumedLog(t *testing.T, contextText string) *sessions.MemoryLog {
	t.Helper()
	log := &sessions.MemoryLog{}
	_, err := log.Append(
		sessions.MessageEntry{Message: seedItem(protocol.CustomContextFiles, contextText)},
		sessions.MessageEntry{Message: seedItem(protocol.CustomSkillList, "skills")},
		sessions.MessageEntry{Message: user("earlier")},
		sessions.MessageEntry{Message: protocol.AssistantMessage{Content: []protocol.AssistantBlock{protocol.Text{Text: "answer"}}, StopReason: protocol.StopStop}},
	)
	require.NoError(t, err)
	return log
}

func seedTexts(log sessions.Writer) []string {
	var out []string
	for _, e := range log.Entries() {
		if me, ok := e.(sessions.MessageEntry); ok {
			if c, ok := me.Message.(protocol.CustomMessage); ok {
				out = append(out, messageTexts([]protocol.Message{c})...)
			}
		}
	}
	return out
}

func occurrences(texts []string, want string) int {
	n := 0
	for _, s := range texts {
		if s == want {
			n++
		}
	}
	return n
}

func TestResumeAfterContextFileEditSendsNewTextOnce(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	log := resumedLog(t, "old rules")
	seed := []protocol.CustomMessage{seedItem(protocol.CustomContextFiles, "new rules"), seedItem(protocol.CustomSkillList, "skills")}

	a := newAgent(t, p, m, edits(withLog(log), withSeed(seed...)))
	require.NoError(t, a.Prompt(context.Background(), user("next")))
	require.NoError(t, a.Prompt(context.Background(), user("again")))

	assert.Equal(t, []string{"old rules", "skills", "new rules"}, seedTexts(log), "the logged items stay and the new text follows them")
	first := messageTexts(p.Requests()[0].Transcript.Messages)
	assert.Equal(t, []string{"old rules", "skills", "earlier", "new rules", "next"}, first)
	assert.Equal(t, 1, occurrences(messageTexts(p.Requests()[1].Transcript.Messages), "new rules"))
}

func TestResumeWithoutEditAppendsNoSeedItem(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("one"))
	log := resumedLog(t, "rules")
	before := len(log.Entries())
	seed := []protocol.CustomMessage{seedItem(protocol.CustomContextFiles, "rules"), seedItem(protocol.CustomSkillList, "skills")}

	a := newAgent(t, p, m, edits(withLog(log), withSeed(seed...)))

	assert.Len(t, log.Entries(), before)
	require.NoError(t, a.Prompt(context.Background(), user("next")))
	assert.Equal(t, []string{"rules", "skills"}, seedTexts(log))
}

func TestResumeIgnoresSeedTimestamp(t *testing.T) {
	p, m := newFaux(t)
	log := resumedLog(t, "rules")
	stamped := seedItem(protocol.CustomContextFiles, "rules")
	stamped.Timestamp = 1700000000000

	newAgent(t, p, m, edits(withLog(log), withSeed(stamped)))

	assert.Equal(t, []string{"rules", "skills"}, seedTexts(log))
}

func TestResumeComparesAgainstNewestLoggedItem(t *testing.T) {
	p, m := newFaux(t)
	log := resumedLog(t, "v1")
	_, err := log.Append(sessions.MessageEntry{Message: seedItem(protocol.CustomContextFiles, "v2")})
	require.NoError(t, err)

	newAgent(t, p, m, edits(withLog(log), withSeed(seedItem(protocol.CustomContextFiles, "v2"))))
	assert.Equal(t, []string{"v1", "skills", "v2"}, seedTexts(log), "v2 is the newest item, so nothing is added")

	newAgent(t, p, m, edits(withLog(log), withSeed(seedItem(protocol.CustomContextFiles, "v1"))))
	assert.Equal(t, []string{"v1", "skills", "v2", "v1"}, seedTexts(log), "a text that matches an older item only is new")
}
