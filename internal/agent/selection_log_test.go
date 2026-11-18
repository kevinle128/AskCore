package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

func newReasoningFaux(t testing.TB) (*faux.Provider, providers.Model, providers.Model) {
	t.Helper()
	p, err := faux.New(faux.WithChunk(1000, 1000), faux.WithModels(faux.ModelDef{ID: "think-1", Reasoning: true}, faux.ModelDef{ID: "think-2", Reasoning: true}))
	require.NoError(t, err)
	first, ok := p.Model("think-1")
	require.True(t, ok)
	second, ok := p.Model("think-2")
	require.True(t, ok)
	return p, first, second
}

func withLevel(level protocol.ThinkingLevel) func(*agent.Config) {
	return func(c *agent.Config) { c.Options.Reasoning = level }
}

func TestRunStartLogsModelAndThinkingChange(t *testing.T) {
	p, m, _ := newReasoningFaux(t)
	p.Set(faux.Say("ok"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, edits(withLog(log), withLevel(protocol.ThinkingHigh)))
	require.Empty(t, logged[sessions.ModelChange](log), "nothing is logged before a run")

	require.NoError(t, a.Prompt(context.Background(), user("hi")))

	models := logged[sessions.ModelChange](log)
	require.Len(t, models, 1)
	assert.Equal(t, m.ID, models[0].Model.ID)
	assert.Equal(t, m.Provider, models[0].Model.Provider)
	levels := logged[sessions.ThinkingChange](log)
	require.Len(t, levels, 1)
	assert.Equal(t, protocol.ThinkingHigh, levels[0].Level)

	// Both entries come before the request of the run.
	entries := log.Entries()
	firstDelta := -1
	for i, e := range entries {
		if _, ok := e.(sessions.RequestDelta); ok {
			firstDelta = i
			break
		}
	}
	require.Positive(t, firstDelta)
	for i, e := range entries {
		switch e.(type) {
		case sessions.ModelChange, sessions.ThinkingChange:
			assert.Less(t, i, firstDelta)
		}
	}
}

func TestRunStartLogsLaterThinkingAndModelChange(t *testing.T) {
	p, m1, m2 := newReasoningFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"), faux.Say("three"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m1, edits(withLog(log), withLevel(protocol.ThinkingHigh)))
	require.NoError(t, a.Prompt(context.Background(), user("one")))

	require.NoError(t, a.SetThinkingLevel(protocol.ThinkingLow))
	require.NoError(t, a.Prompt(context.Background(), user("two")))
	levels := logged[sessions.ThinkingChange](log)
	require.Len(t, levels, 2)
	assert.Equal(t, protocol.ThinkingLow, levels[1].Level)
	assert.Len(t, logged[sessions.ModelChange](log), 1)

	// A new Agent on the same log, started on another model, logs that model.
	b := newAgent(t, p, m2, edits(withLog(log), withLevel(protocol.ThinkingLow)))
	require.NoError(t, b.Prompt(context.Background(), user("three")))
	models := logged[sessions.ModelChange](log)
	require.Len(t, models, 2)
	assert.Equal(t, m2.ID, models[1].Model.ID)
	assert.Len(t, logged[sessions.ThinkingChange](log), 2, "the level did not change")
}

func TestRunStartSkipsUnchangedSelection(t *testing.T) {
	p, m, _ := newReasoningFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"), faux.Say("three"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, edits(withLog(log), withLevel(protocol.ThinkingHigh)))
	require.NoError(t, a.Prompt(context.Background(), user("one")))
	require.NoError(t, a.Prompt(context.Background(), user("two")))
	assert.Len(t, logged[sessions.ModelChange](log), 1)
	assert.Len(t, logged[sessions.ThinkingChange](log), 1)

	// A resumed Agent with the same selection adds nothing either.
	b := newAgent(t, p, m, edits(withLog(log), withLevel(protocol.ThinkingHigh)))
	require.NoError(t, b.Prompt(context.Background(), user("three")))
	assert.Len(t, logged[sessions.ModelChange](log), 1)
	assert.Len(t, logged[sessions.ThinkingChange](log), 1)
}

func TestRunStartLogsNoThinkingChangeForDefaultLevel(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("ok"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, withLog(log))

	require.NoError(t, a.Prompt(context.Background(), user("hi")))

	assert.Len(t, logged[sessions.ModelChange](log), 1)
	assert.Empty(t, logged[sessions.ThinkingChange](log))
}

func TestResetLogsSelectionAgainInNewLog(t *testing.T) {
	p, m, _ := newReasoningFaux(t)
	p.Set(faux.Say("one"), faux.Say("two"))
	var logs []*sessions.MemoryLog
	a := newAgent(t, p, m, edits(withLevel(protocol.ThinkingHigh), func(c *agent.Config) {
		c.NewContext = func() (sessions.Writer, error) {
			l := &sessions.MemoryLog{}
			logs = append(logs, l)
			return l, nil
		}
	}))
	require.NoError(t, a.Prompt(context.Background(), user("one")))
	require.NoError(t, a.Reset())
	require.NoError(t, a.Prompt(context.Background(), user("two")))

	require.Len(t, logs, 2)
	for _, l := range logs {
		assert.Len(t, logged[sessions.ModelChange](l), 1)
		assert.Len(t, logged[sessions.ThinkingChange](l), 1)
	}
}

func settled(attempt, method string) sessions.AttemptSettled {
	return sessions.AttemptSettled{AttemptID: attempt, Outcome: "completed", Binding: &sessions.AuthBinding{Provider: "openai", Method: method}}
}

func TestLoggedSelectionPrefersChangeEntries(t *testing.T) {
	entries := []sessions.Entry{
		sessions.RequestDelta{AttemptID: "a1", Model: sessions.ModelRef{Provider: "openai", API: "responses", ID: "old"}, Options: sessions.RequestOptions{Reasoning: protocol.ThinkingLow}},
		settled("a1", "api_key"),
		sessions.ModelChange{Model: sessions.ModelRef{Provider: "anthropic", API: "messages", ID: "new"}},
		sessions.ThinkingChange{Level: protocol.ThinkingHigh},
		sessions.ModelChange{Model: sessions.ModelRef{Provider: "openai", API: "responses", ID: "newest"}},
		settled("a2", "oauth"),
		sessions.AttemptSettled{AttemptID: "a3", Outcome: "failed"},
	}

	model, method, level, ok := agent.LoggedSelection(entries)

	require.True(t, ok)
	assert.Equal(t, "newest", model.ID)
	assert.Equal(t, "oauth", method, "an attempt with no binding does not clear the method")
	assert.Equal(t, protocol.ThinkingHigh, level)
}

func TestLoggedSelectionKeepsDefaultLevelOfLaterChange(t *testing.T) {
	entries := []sessions.Entry{
		sessions.ThinkingChange{Level: protocol.ThinkingHigh},
		sessions.ThinkingChange{Level: ""},
		sessions.ModelChange{Model: sessions.ModelRef{ID: "m"}},
	}

	_, _, level, ok := agent.LoggedSelection(entries)

	require.True(t, ok)
	assert.Equal(t, protocol.ThinkingLevel(""), level)
}

func TestLoggedSelectionFallsBackToRequestDelta(t *testing.T) {
	entries := []sessions.Entry{
		sessions.RequestDelta{AttemptID: "a1", Model: sessions.ModelRef{Provider: "openai", API: "responses", ID: "first"}, Options: sessions.RequestOptions{Reasoning: protocol.ThinkingLow}},
		sessions.RequestDelta{AttemptID: "a2", Model: sessions.ModelRef{Provider: "openai", API: "responses", ID: "second"}, Options: sessions.RequestOptions{Reasoning: protocol.ThinkingMedium}},
		settled("a2", "api_key"),
	}

	model, method, level, ok := agent.LoggedSelection(entries)

	require.True(t, ok)
	assert.Equal(t, "second", model.ID)
	assert.Equal(t, "api_key", method)
	assert.Equal(t, protocol.ThinkingMedium, level)
}

func TestLoggedSelectionOfEmptyLogIsNotOK(t *testing.T) {
	_, method, level, ok := agent.LoggedSelection([]sessions.Entry{sessions.CycleOpened{CycleID: "c"}})

	assert.False(t, ok)
	assert.Empty(t, method)
	assert.Empty(t, level)
}

func TestLoggedSelectionDropsMethodOfAnotherModel(t *testing.T) {
	openai := sessions.ModelRef{Provider: "openai", API: "responses", ID: "gpt"}
	demo := sessions.ModelRef{Provider: "faux", API: "faux", ID: "demo"}
	entries := []sessions.Entry{
		sessions.ModelChange{Model: openai},
		sessions.RequestDelta{AttemptID: "a1", Model: openai},
		settled("a1", "oauth"),
		sessions.ModelChange{Model: demo},
		sessions.RequestDelta{AttemptID: "a2", Model: demo},
		sessions.AttemptSettled{AttemptID: "a2", Outcome: "completed"},
	}

	model, method, _, ok := agent.LoggedSelection(entries)

	require.True(t, ok)
	assert.Equal(t, "faux", model.Provider)
	assert.Empty(t, method, "the login method of an earlier model does not carry over")
}

func TestLoggedSelectionKeepsMethodAfterSwitchBack(t *testing.T) {
	openai := sessions.ModelRef{Provider: "openai", API: "responses", ID: "gpt"}
	demo := sessions.ModelRef{Provider: "faux", API: "faux", ID: "demo"}
	entries := []sessions.Entry{
		sessions.RequestDelta{AttemptID: "a1", Model: openai},
		settled("a1", "oauth"),
		sessions.RequestDelta{AttemptID: "a2", Model: demo},
		sessions.AttemptSettled{AttemptID: "a2", Outcome: "completed"},
		sessions.ModelChange{Model: openai},
	}

	_, method, _, ok := agent.LoggedSelection(entries)

	require.True(t, ok)
	assert.Equal(t, "oauth", method)
}
