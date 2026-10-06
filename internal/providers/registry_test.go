package providers

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func TestRegistryStreamUnknownAPI(t *testing.T) {
	r := NewRegistry()
	s := r.Stream(context.Background(), Model{API: "missing", ID: "x", Provider: "y"}, TranscriptRequest{}, StreamOptions{})
	require.NotNil(t, s)
	items := drain(s)
	msg, err := waitResult(t, s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
	assert.Equal(t, protocol.StopError, msg.StopReason)
	require.NotEmpty(t, items)
}

func TestRegistryStreamCallsRegistered(t *testing.T) {
	var called atomic.Bool
	r := NewRegistry()
	r.Register(APIOpenAIResponses, func(ctx context.Context, m Model, _ TranscriptRequest, _ StreamOptions) *Stream {
		called.Store(true)
		return NewStream(ctx, 0, protocol.AssistantMessage{API: string(m.API), Model: m.ID}, func(a *Assembler) {
			a.Fail(protocol.StopError, "fake", errors.New("fake"))
		})
	})
	s := r.Stream(context.Background(), Model{API: APIOpenAIResponses, ID: ModelGPT55, Provider: ProviderOpenAI}, TranscriptRequest{}, StreamOptions{})
	_ = drain(s)
	_, err := waitResult(t, s)
	require.Error(t, err)
	assert.True(t, called.Load())
}
