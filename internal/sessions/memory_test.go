package sessions_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

func text(s string) protocol.Message {
	return protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: s}}, Timestamp: 1}
}

func TestMemoryLogAppendOrder(t *testing.T) {
	var log sessions.MemoryLog
	assert.Empty(t, log.Messages())
	for _, s := range []string{"a", "b", "c"} {
		require.NoError(t, log.Append(text(s)))
	}
	assert.Equal(t, []protocol.Message{text("a"), text("b"), text("c")}, log.Messages())
}

func TestMemoryLogCopyOnRead(t *testing.T) {
	var log sessions.MemoryLog
	require.NoError(t, log.Append(text("a")))
	got := log.Messages()
	got[0] = text("changed")
	_ = append(got, text("extra"))
	assert.Equal(t, []protocol.Message{text("a")}, log.Messages())
}

func TestMemoryLogConcurrentUse(t *testing.T) {
	var log sessions.MemoryLog
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 50 {
				_ = log.Append(text("x"))
				_ = log.Messages()
			}
		})
	}
	wg.Wait()
	assert.Len(t, log.Messages(), 800)
}
