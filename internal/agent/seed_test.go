package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

func seedItem(customType, text string) protocol.CustomMessage {
	return protocol.CustomMessage{CustomType: customType, Content: []protocol.UserBlock{protocol.Text{Text: text}}}
}

func twoSeeds() []protocol.CustomMessage {
	return []protocol.CustomMessage{
		seedItem(protocol.CustomContextFiles, "ctx"),
		seedItem(protocol.CustomSkillList, "skills"),
	}
}

func withSeed(seed ...protocol.CustomMessage) func(*agent.Config) {
	return func(c *agent.Config) { c.Seed = seed }
}

func messageTexts(msgs []protocol.Message) []string {
	var out []string
	for _, m := range msgs {
		var blocks []protocol.UserBlock
		switch v := m.(type) {
		case protocol.UserMessage:
			blocks = v.Content
		case protocol.CustomMessage:
			blocks = v.Content
		}
		for _, b := range blocks {
			if t, ok := b.(protocol.Text); ok {
				out = append(out, t.Text)
			}
		}
	}
	return out
}

func TestSeedPrecedesFirstPromptInProviderRequest(t *testing.T) {
	t.Parallel()
	p, m := newFaux(t)
	p.Set(faux.Say("ok"))
	a := newAgent(t, p, m, edits(withSeed(twoSeeds()...), func(c *agent.Config) { c.SystemPrompt = "be brief" }))

	require.NoError(t, a.Prompt(context.Background(), user("hi")))

	msgs := p.Requests()[0].Transcript.Messages
	assert.Equal(t, []string{"system", "user", "user", "user"}, roles(msgs))
	assert.Equal(t, []string{"ctx", "skills", "hi"}, messageTexts(msgs))
}

func TestSeedIsCommittedBeforeInputWithoutInputID(t *testing.T) {
	t.Parallel()
	p, m := newFaux(t)
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, edits(withLog(log), withSeed(twoSeeds()...)))

	seedEntries := log.Entries()
	require.Len(t, seedEntries, 2)
	for i, e := range seedEntries {
		me, ok := e.(sessions.MessageEntry)
		require.True(t, ok)
		assert.Empty(t, me.InputID)
		assert.Equal(t, twoSeeds()[i].CustomType, me.Message.(protocol.CustomMessage).CustomType)
	}
	assert.Equal(t, []string{"custom", "custom"}, roles(a.State().Messages))
}

func TestSeedIsInFirstFollowSnapshot(t *testing.T) {
	t.Parallel()
	p, m := newFaux(t)
	a := newAgent(t, p, m, withSeed(twoSeeds()...))

	f := a.Follow(agent.Cursor{})
	t.Cleanup(f.Events.Close)

	assert.Equal(t, []string{"ctx", "skills"}, messageTexts(sessions.MessagesOf(f.Entries)))
}

func TestResetSeedsTheFreshLog(t *testing.T) {
	t.Parallel()
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	a := newAgent(t, p, m, withSeed(twoSeeds()...))
	require.NoError(t, a.Prompt(context.Background(), user("first")))

	require.NoError(t, a.Reset())
	assert.Equal(t, []string{"custom", "custom"}, roles(a.State().Messages))
	f := a.Follow(agent.Cursor{})
	t.Cleanup(f.Events.Close)
	assert.Equal(t, []string{"ctx", "skills"}, messageTexts(sessions.MessagesOf(f.Entries)))

	require.NoError(t, a.Prompt(context.Background(), user("second")))
	assert.Equal(t, []string{"ctx", "skills", "second"}, messageTexts(p.Requests()[1].Transcript.Messages))
}

func TestPrefilledLogIsNotSeededTwice(t *testing.T) {
	t.Parallel()
	p, m := newFaux(t)
	log := &sessions.MemoryLog{}
	_, err := log.Append(sessions.MessageEntry{Message: seedItem(protocol.CustomContextFiles, "old ctx")}, sessions.MessageEntry{Message: user("earlier")})
	require.NoError(t, err)

	a := newAgent(t, p, m, edits(withLog(log), withSeed(twoSeeds()...)))

	assert.Equal(t, []string{"old ctx", "earlier", "skills"}, messageTexts(a.State().Messages), "only the missing type is added")
	require.NoError(t, a.Reset())
	assert.Len(t, log.Entries(), 3, "a shared log that holds both types gets nothing more")
}

func TestNewRejectsBadSeedTypes(t *testing.T) {
	t.Parallel()
	p, m := newFaux(t)
	for name, seed := range map[string][]protocol.CustomMessage{
		"empty":     {seedItem("", "x")},
		"duplicate": {seedItem("a", "x"), seedItem("a", "y")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := agent.New(agent.Config{LoopConfig: agent.LoopConfig{Model: m, Stream: p.Stream}, Seed: seed})
			require.Error(t, err)
		})
	}
}

func TestContinueTreatsSeedOnlyLogAsEmpty(t *testing.T) {
	t.Parallel()
	p, m := newFaux(t)
	p.Set(faux.Say("answer"))
	a := newAgent(t, p, m, withSeed(twoSeeds()...))
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.ErrorIs(t, a.Continue(context.Background()), agent.ErrContinueEmpty)
	assert.Empty(t, rec.eventLabels())
	assert.Empty(t, p.Requests())
}

func TestSeededSessionRebuildsExactRequest(t *testing.T) {
	t.Parallel()
	p, m := newFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, edits(withLog(log), withSeed(twoSeeds()...), func(c *agent.Config) { c.SystemPrompt = "be brief" }))
	var events []protocol.Event
	a.Subscribe(func(ev protocol.Event) error { events = append(events, ev); return nil })

	require.NoError(t, a.Prompt(context.Background(), user("one")))
	require.NoError(t, a.Prompt(context.Background(), user("two")))

	reqs, ids := p.Requests(), attemptIDs(events)
	require.Len(t, reqs, 2)
	require.Len(t, ids, 2)
	for i, rec := range reqs {
		got, err := agent.RebuildRequest(log.Entries(), ids[i])
		require.NoError(t, err)
		assert.JSONEq(t, toJSON(t, rec.Transcript.Messages), toJSON(t, got.Messages), "attempt %d", i)
	}
	assert.Equal(t, []string{"ctx", "skills", "one"}, messageTexts(reqs[0].Transcript.Messages))
}

type failingLog struct{ sessions.MemoryLog }

func (*failingLog) Append(...sessions.Entry) (sessions.CommitRef, error) {
	return sessions.CommitRef{}, errors.New("disk full")
}

func TestSeedWriteFailureFailsNew(t *testing.T) {
	t.Parallel()
	p, m := newFaux(t)
	_, err := agent.New(agent.Config{
		LoopConfig: agent.LoopConfig{Model: m, Stream: p.Stream},
		NewContext: func() sessions.Writer { return &failingLog{} },
		Seed:       twoSeeds(),
	})
	require.ErrorContains(t, err, "disk full")
}
