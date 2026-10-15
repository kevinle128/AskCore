package agent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/pkg/protocol"
)

// readyAgent is newAgent with a readiness check that accepts every model, so
// SetModel needs no key.
func readyAgent(t testing.TB, p *faux.Provider, m providers.Model) *agent.Agent {
	t.Helper()
	return newAgent(t, p, m, func(c *agent.Config) {
		c.Ready = func(context.Context, providers.Model, string) error { return nil }
	})
}

// otherModel is m under a second id.
func otherModel(m providers.Model) providers.Model {
	m.ID = "faux-other"
	return m
}

func modelUpdates(r *recorder) []*protocol.ModelUpdate {
	return eventsOf[*protocol.ModelUpdate](r)
}

// waitForModelUpdates waits for n model_update events: they are posted events,
// which a publisher goroutine delivers after the call that made them returns.
func waitForModelUpdates(t *testing.T, r *recorder, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return len(modelUpdates(r)) >= n }, 5*time.Second, 5*time.Millisecond)
}

func TestAgentSetModelPublishesModelUpdate(t *testing.T) {
	p, m := newFaux(t)
	a := readyAgent(t, p, m)
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.Error(t, a.SetModel(context.Background(), providers.Model{}), "an incomplete model is refused")
	next := otherModel(m)
	require.NoError(t, a.SetModel(context.Background(), next))
	require.NoError(t, a.Dispose())

	got := modelUpdates(rec)
	require.Len(t, got, 1, "a refused SetModel posts nothing")
	assert.Equal(t, next.Provider, got[0].Provider)
	assert.Equal(t, next.ID, got[0].ID)
	assert.Equal(t, string(next.API), got[0].API)
}

func TestAgentClearModelPublishesEmptyUpdate(t *testing.T) {
	p, m := newFaux(t)
	a := readyAgent(t, p, m)
	rec := &recorder{}
	a.Subscribe(rec.emit)

	assert.True(t, a.ClearModel())
	assert.Empty(t, a.State().Model.ID)
	assert.False(t, a.ClearModel(), "no model to clear")
	require.NoError(t, a.Dispose())

	got := modelUpdates(rec)
	require.Len(t, got, 1)
	assert.Equal(t, &protocol.ModelUpdate{Envelope: got[0].Envelope}, got[0])
}

func TestAgentSetModelAfterClearRestoresModel(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("ok"))
	a := readyAgent(t, p, m)
	require.True(t, a.ClearModel())
	require.NoError(t, a.SetModel(context.Background(), m))
	assert.Equal(t, m.ID, a.State().Model.ID)
	require.NoError(t, a.Prompt(context.Background(), user("hi")))
}

func TestAgentClearModelKeepsActiveRun(t *testing.T) {
	p, m := newFaux(t)
	p.Set(slowStep())
	a := readyAgent(t, p, m)
	rec := &recorder{}
	started := make(chan struct{})
	a.Subscribe(rec.emit)
	a.Subscribe(signalOn("agent_start", started))

	var wg sync.WaitGroup
	wg.Add(1)
	var promptErr error
	go func() {
		defer wg.Done()
		promptErr = a.Prompt(context.Background(), user("go"))
	}()
	<-started

	require.True(t, a.ClearModel())
	assert.Equal(t, m.ID, a.State().Model.ID, "the active run keeps its model")
	wg.Wait()
	require.NoError(t, promptErr)
	waitForModelUpdates(t, rec, 1)

	labels := rec.eventLabels()
	settled := len(labels) - 1 - indexOfLast(labels, "agent_settled")
	update := len(labels) - 1 - indexOfLast(labels, "model_update")
	assert.Greater(t, settled, update, "model_update comes after agent_settled")
	var ended *protocol.MessageEnd
	for _, e := range eventsOf[*protocol.MessageEnd](rec) {
		if _, ok := e.Message.(protocol.AssistantMessage); ok {
			ended = e
		}
	}
	require.NotNil(t, ended)
	assert.Equal(t, m.ID, ended.Message.(protocol.AssistantMessage).Model)

	assert.Empty(t, a.State().Model.ID)
	require.ErrorIs(t, a.Prompt(context.Background(), user("again")), agent.ErrNoModel)
}

func indexOfLast(labels []string, l string) int {
	for i := len(labels) - 1; i >= 0; i-- {
		if labels[i] == l {
			return i
		}
	}
	return -1
}

func TestAgentPromptWithoutModelRefusedBeforeStart(t *testing.T) {
	p, _ := newFaux(t)
	a := newAgent(t, p, providers.Model{}, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)

	require.ErrorIs(t, a.Prompt(context.Background(), user("hi")), agent.ErrNoModel)
	require.ErrorIs(t, a.Continue(context.Background()), agent.ErrNoModel)
	require.NoError(t, a.Dispose())

	assert.NotContains(t, rec.eventLabels(), "agent_start")
	assert.Equal(t, 0, p.Calls())
	assert.Equal(t, agent.Idle, a.State().Status)
}

func TestAgentEnqueueWithoutModelRefusedWhenIdle(t *testing.T) {
	p, _ := newFaux(t)
	a := newAgent(t, p, providers.Model{}, nil)
	rec := &recorder{}
	a.Subscribe(rec.emit)

	_, err := a.Steer(user("steer"))
	require.ErrorIs(t, err, agent.ErrNoModel)
	_, err = a.FollowUp(user("follow"))
	require.ErrorIs(t, err, agent.ErrNoModel)
	idle(t, a)
	require.NoError(t, a.Dispose())

	assert.Empty(t, eventsOf[*protocol.QueueUpdate](rec), "nothing was queued")
	assert.NotContains(t, rec.eventLabels(), "agent_start")
	assert.Equal(t, 0, p.Calls())
}

func TestAgentQueuedInputWithoutModelEndsCleanly(t *testing.T) {
	p, m := newFaux(t)
	p.Set(slowStep())
	a := readyAgent(t, p, m)
	rec := &recorder{}
	started := make(chan struct{})
	a.Subscribe(rec.emit)
	a.Subscribe(signalOn("agent_start", started))

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = a.Prompt(context.Background(), user("go"))
	}()
	<-started

	require.True(t, a.ClearModel())
	// The aborted run cannot take the message, so it waits for a run after it.
	a.Abort(agent.KeepQueued)
	_, err := a.FollowUp(user("later"))
	require.NoError(t, err)
	wg.Wait()
	idle(t, a)
	require.NoError(t, a.Dispose())

	labels := rec.eventLabels()
	assert.Equal(t, 1, countOf(labels, "agent_start"), "the queued run never starts")
	assert.Equal(t, 2, countOf(labels, "agent_settled"))
	var texts []string
	for _, e := range eventsOf[*protocol.MessageEnd](rec) {
		if am, ok := e.Message.(protocol.AssistantMessage); ok {
			texts = append(texts, errorText(am))
		}
	}
	require.NotEmpty(t, texts)
	assert.Contains(t, texts[len(texts)-1], agent.ErrNoModel.Error())
}
