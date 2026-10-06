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

// commit writes one message entry and fails the test when the log refuses it.
func commit(t testing.TB, log *sessions.MemoryLog, m protocol.Message) {
	t.Helper()
	_, err := log.Append(sessions.MessageEntry{Message: m})
	require.NoError(t, err)
}

func TestMemoryLogAppendOrder(t *testing.T) {
	var log sessions.MemoryLog
	assert.Empty(t, log.Messages())
	for _, s := range []string{"a", "b", "c"} {
		commit(t, &log, text(s))
	}
	assert.Equal(t, []protocol.Message{text("a"), text("b"), text("c")}, log.Messages())
}

func TestMemoryLogCopyOnRead(t *testing.T) {
	var log sessions.MemoryLog
	commit(t, &log, text("a"))
	got := log.Messages()
	got[0] = text("changed")
	_ = append(got, text("extra"))
	assert.Equal(t, []protocol.Message{text("a")}, log.Messages())
}

func TestMemoryLogReadCannotChangeStoredBlocks(t *testing.T) {
	var log sessions.MemoryLog
	appended := protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "a"}}, Timestamp: 1}
	commit(t, &log, appended)

	// A change of a content block of the appended value, after Append.
	appended.Content[0] = protocol.Text{Text: "changed after append"}
	assert.Equal(t, []protocol.Message{text("a")}, log.Messages())

	// A change of a content block of a message that was read.
	read := log.Messages()[0].(protocol.UserMessage)
	read.Content[0] = protocol.Text{Text: "changed after read"}
	assert.Equal(t, []protocol.Message{text("a")}, log.Messages())

	// The same holds for an entry that was read with Entries.
	entry := log.Entries()[0].(sessions.MessageEntry).Message.(protocol.UserMessage)
	entry.Content[0] = protocol.Text{Text: "changed after entries"}
	assert.Equal(t, []protocol.Message{text("a")}, log.Messages())
}

func TestMemoryLogConcurrentUse(t *testing.T) {
	var log sessions.MemoryLog
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 50 {
				commit(t, &log, text("x"))
				_ = log.Messages()
			}
		})
	}
	wg.Wait()
	assert.Len(t, log.Messages(), 800)
}
