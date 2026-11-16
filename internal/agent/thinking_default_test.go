package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// requireNoEffort checks that the only request of r has no reasoning object:
// Ask sent no level, so the word default never reached the adapter.
func requireNoEffort(t *testing.T, r *realRun) {
	t.Helper()
	calls := r.wire.taken()
	require.Len(t, calls, 1)
	effort, has := responsesEffort(t, calls[0].Body)
	assert.False(t, has, "the request has no reasoning object, got effort %q", effort)
}

// lastStamp returns the thinking level stamp of the last assistant message of
// the log.
func lastStamp(t *testing.T, r *realRun) protocol.ThinkingLevel {
	t.Helper()
	am := lastAssistant(t, r.log.Messages())
	require.NotNil(t, am.ThinkingLevel, "the assistant message has a thinking level stamp")
	return *am.ThinkingLevel
}

// TestStateReportsDefaultWithNoChosenLevel reads the state of a session with
// no chosen level, on a model with thinking, on a model with no thinking and
// with no model. Every one reports default.
func TestStateReportsDefaultWithNoChosenLevel(t *testing.T) {
	for _, m := range []providers.Model{providers.XAIGrok47(), {Provider: "local", ID: "alpha"}, {}} {
		r := newRealRun(t, m, withTestKey)
		assert.Equal(t, protocol.ThinkingDefault, r.agent.State().ThinkingLevel, "model %q", m.ID)
	}
}

// TestStartLevelDefaultStoresNoLevel starts an Agent with the level default,
// with a model and with no model. The request has no effort, and the state and
// the message stamp say default.
func TestStartLevelDefaultStoresNoLevel(t *testing.T) {
	t.Run("with a model", func(t *testing.T) {
		r := newRealRun(t, providers.XAIGrok47(), func(c *agent.Config) {
			withTestKey(c)
			c.Options.Reasoning = protocol.ThinkingDefault
		})
		assert.Equal(t, protocol.ThinkingDefault, r.agent.State().ThinkingLevel)
		r.prompt("hello")
		requireNoEffort(t, r)
		assert.Equal(t, protocol.ThinkingDefault, lastStamp(t, r))
	})
	t.Run("with no model", func(t *testing.T) {
		r := newRealRun(t, providers.Model{}, func(c *agent.Config) {
			withTestKey(c)
			c.Options.Reasoning = protocol.ThinkingDefault
		})
		require.NoError(t, r.agent.SetModel(context.Background(), providers.XAIGrok47()))
		assert.Equal(t, protocol.ThinkingDefault, r.agent.State().ThinkingLevel)
		r.prompt("hello")
		requireNoEffort(t, r)
	})
}

// TestSetThinkingLevelDefaultRemovesTheChoice sets default on a session at
// high. The level is removed with no clamp, and the request has no effort.
func TestSetThinkingLevelDefaultRemovesTheChoice(t *testing.T) {
	r := newRealRun(t, providers.XAIGrok47(), func(c *agent.Config) {
		withTestKey(c)
		c.Options.Reasoning = protocol.ThinkingHigh
	})
	require.NoError(t, r.agent.SetThinkingLevel(protocol.ThinkingDefault))
	assert.Equal(t, protocol.ThinkingDefault, r.agent.State().ThinkingLevel)
	r.prompt("hello")
	requireNoEffort(t, r)
}

// TestPrepareRequestHookForcesOff runs a hook on a session with no chosen
// level. The hook sees default; it sets off and passes the request on, so the
// change is kept: grok-4.7 gets its lowest effort. The message stamp records
// the stored level, which is off clamped to grok-4.7. A hook level is a change
// of the run: the level of the Agent stays as it was.
func TestPrepareRequestHookForcesOff(t *testing.T) {
	var seen protocol.ThinkingLevel
	r := newRealRun(t, providers.XAIGrok47(), func(c *agent.Config) {
		withTestKey(c)
		registryOf(c).OnPrepareRequest(func(ctx context.Context, in pipeline.Request, next pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
			seen = in.ThinkingLevel
			in.ThinkingLevel = protocol.ThinkingOff
			return next(ctx, in)
		})
	})
	r.prompt("hello")
	assert.Equal(t, protocol.ThinkingDefault, seen, "the hook sees default for no chosen level")
	calls := r.wire.taken()
	require.Len(t, calls, 1)
	effort, has := responsesEffort(t, calls[0].Body)
	require.True(t, has, "the request has a reasoning object")
	assert.Equal(t, "low", effort, "off gives the lowest effort of grok-4.7")
	assert.Equal(t, protocol.ThinkingLow, lastStamp(t, r))
}

// TestPrepareRequestHookRestoresDefault runs a hook on a session at high. The
// hook sets default and passes the request on: the request has no effort and
// the message stamp says default.
func TestPrepareRequestHookRestoresDefault(t *testing.T) {
	r := newRealRun(t, providers.XAIGrok47(), func(c *agent.Config) {
		withTestKey(c)
		c.Options.Reasoning = protocol.ThinkingHigh
		registryOf(c).OnPrepareRequest(func(ctx context.Context, in pipeline.Request, next pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
			in.ThinkingLevel = protocol.ThinkingDefault
			return next(ctx, in)
		})
	})
	r.prompt("hello")
	requireNoEffort(t, r)
	assert.Equal(t, protocol.ThinkingDefault, lastStamp(t, r))
}
