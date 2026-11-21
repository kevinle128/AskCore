package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func streamWithModel(t *testing.T, m providers.Model, sse string) protocol.AssistantMessage {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, sse)
	}))
	t.Cleanup(srv.Close)
	msg, err := resultOf(t, testProvider(srv).Stream(context.Background(), m, helloReq(), providers.StreamOptions{APIKey: "test-key"}))
	require.NoError(t, err)
	return msg
}

func TestAnthropicUsageHasCost(t *testing.T) {
	sse := textSSEWithUsage("end_turn", 3, 1_000_000, 100_000, 200_000, 400_000)

	priced := Model()
	priced.Cost = providers.Cost{Tiers: []providers.Tier{{Above: 0, Price: providers.Price{
		Input: 3_000_000, Output: 15_000_000, CacheRead: 300_000, CacheWrite: 3_750_000,
	}}}}
	require.True(t, priced.PriceKnown())
	msg := streamWithModel(t, priced, sse)
	assert.Equal(t, protocol.Cost{
		Input: 3_000_000, Output: 1_500_000, CacheRead: 60_000, CacheWrite: 1_500_000, Total: 6_060_000,
	}, msg.Usage.Cost)
	assert.Equal(t, int64(1_000_000), msg.Usage.Input, "the token split is unchanged")

	unpriced := providers.AnthropicSonnet46()
	require.False(t, unpriced.PriceKnown())
	msg = streamWithModel(t, unpriced, sse)
	assert.Equal(t, protocol.Cost{}, msg.Usage.Cost)
	assert.Equal(t, int64(1_000_000), msg.Usage.Input)
}
