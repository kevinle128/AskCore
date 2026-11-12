package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeThinkingSignatureNeedsTypeAndID(t *testing.T) {
	t.Parallel()
	for _, sig := range []string{`null`, `{}`, `{"type":"reasoning"}`, `{"id":"rs_1"}`, `{"type":"summary","id":"rs_1"}`, `not json`, ``} {
		_, ok := DecodeThinkingSignature(sig)
		assert.False(t, ok, sig)
	}
	full := EncodeThinkingSignature(ReasoningReplay{Type: "reasoning", ID: "rs_1", Status: "completed"})
	got, ok := DecodeThinkingSignature(full)
	require.True(t, ok)
	assert.Equal(t, "rs_1", got.ID)
}
