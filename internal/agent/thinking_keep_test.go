package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// responsesEffort returns the reasoning effort of a Responses request body,
// and whether the body has a reasoning object.
func responsesEffort(t *testing.T, body []byte) (string, bool) {
	t.Helper()
	var req struct {
		Reasoning *struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
	}
	require.NoError(t, json.Unmarshal(body, &req))
	if req.Reasoning == nil {
		return "", false
	}
	return req.Reasoning.Effort, true
}

// catalogGPT55 is the gpt-5.5 Responses row of the runtime catalog. Unlike
// providers.OpenAIGPT55, it has the thinking map, so the adapter sends an
// effort word for each level.
func catalogGPT55(t *testing.T) providers.Model {
	t.Helper()
	for _, m := range providers.GeneratedCatalog() {
		if m.Provider == providers.ProviderOpenAI && m.ID == providers.ModelGPT55 && m.API == providers.APIOpenAIResponses {
			require.NotNil(t, m.ThinkingLevelMap[protocol.ThinkingOff], "the gpt-5.5 row maps off")
			return m
		}
	}
	t.Fatal("the catalog has no gpt-5.5 Responses row")
	return providers.Model{}
}

// withTestKey lets SetModel find a key for every provider.
func withTestKey(c *agent.Config) {
	c.GetAPIKey = func(context.Context, string) (string, error) { return "test-key", nil }
}

// TestSetModelKeepsTheLevelSelector changes the model on the real Responses
// adapter. A level that the user did not choose stays empty, so the request
// has no effort and the vendor default applies. A chosen level is clamped to
// the new model.
func TestSetModelKeepsTheLevelSelector(t *testing.T) {
	grok := providers.XAIGrok47()
	cases := []struct {
		name  string
		start protocol.ThinkingLevel
		want  string
	}{
		{"empty stays empty", "", ""},
		{"off clamps to the lowest level", protocol.ThinkingOff, "low"},
		{"high stays high", protocol.ThinkingHigh, "high"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRealRun(t, catalogGPT55(t), func(c *agent.Config) {
				withTestKey(c)
				c.Options.Reasoning = tc.start
			})
			require.NoError(t, r.agent.SetModel(context.Background(), grok))
			r.prompt("hello")
			calls := r.wire.taken()
			require.Len(t, calls, 1)
			effort, has := responsesEffort(t, calls[0].Body)
			if tc.want == "" {
				assert.False(t, has, "the request has no reasoning object")
				return
			}
			assert.Equal(t, tc.want, effort)
		})
	}
}

// TestSetModelBackKeepsAnEmptyLevel goes from gpt-5.5 to grok-4.7 and back
// with no chosen level. No request carries an effort.
func TestSetModelBackKeepsAnEmptyLevel(t *testing.T) {
	gpt := catalogGPT55(t)
	r := newRealRun(t, gpt, withTestKey)
	require.NoError(t, r.agent.SetModel(context.Background(), providers.XAIGrok47()))
	require.NoError(t, r.agent.SetModel(context.Background(), gpt))
	r.prompt("hello")
	calls := r.wire.taken()
	require.Len(t, calls, 1)
	effort, _ := responsesEffort(t, calls[0].Body)
	assert.Empty(t, effort, "an empty level sends no effort")
}

// TestPrepareRequestLevelIsClamped sets the level from a PrepareRequest hook.
// The hook level goes through ClampThinkingLevel like SetThinkingLevel: off
// stays off, so a model that cannot turn thinking off gets its lowest level.
// When the update also changes the model, the level is clamped to the new
// model. The run starts with a chosen level, because the hook sees an empty
// level as off and an equal level makes no update.
func TestPrepareRequestLevelIsClamped(t *testing.T) {
	grok := providers.XAIGrok47()
	gpt := catalogGPT55(t)
	cases := []struct {
		name  string
		start providers.Model
		model *providers.Model
		level protocol.ThinkingLevel
		want  string
	}{
		{"off on grok-4.7", grok, nil, protocol.ThinkingOff, "low"},
		{"max on grok-4.7", grok, nil, protocol.ThinkingMax, "xhigh"},
		// gpt-5.5 can turn thinking off: the adapter sends the word none.
		{"off on gpt-5.5", gpt, nil, protocol.ThinkingOff, "none"},
		{"off with a switch to grok-4.7", gpt, &grok, protocol.ThinkingOff, "low"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRealRun(t, tc.start, func(c *agent.Config) {
				c.Options.Reasoning = protocol.ThinkingHigh
				registryOf(c).OnPrepareRequest(func(context.Context, pipeline.Request, pipeline.Next[pipeline.Request, *pipeline.RequestUpdate]) (*pipeline.RequestUpdate, error) {
					return &pipeline.RequestUpdate{Model: tc.model, ThinkingLevel: tc.level}, nil
				})
			})
			r.prompt("hello")
			calls := r.wire.taken()
			require.Len(t, calls, 1)
			effort, has := responsesEffort(t, calls[0].Body)
			require.True(t, has, "the request has a reasoning object")
			assert.Equal(t, tc.want, effort)
		})
	}
}
