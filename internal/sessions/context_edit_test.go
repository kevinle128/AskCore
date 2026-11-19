package sessions_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

func TestContextEditOmitsFromMessagesKeepsEntries(t *testing.T) {
	var log sessions.MemoryLog
	commit(t, &log, text("a"))
	commit(t, &log, text("b"))
	commit(t, &log, text("c"))
	_, err := log.Append(sessions.ContextEdit{Target: 1})
	require.NoError(t, err)

	assert.Equal(t, []protocol.Message{text("a"), text("c")}, log.Messages())
	entries := log.Entries()
	require.Len(t, entries, 4)
	assert.Equal(t, sessions.MessageEntry{Message: text("b")}, entries[1])
	assert.Equal(t, []protocol.Message{text("a"), text("c")}, sessions.MessagesOf(entries))
}

func TestContextEditReplacesMessage(t *testing.T) {
	var log sessions.MemoryLog
	commit(t, &log, text("a"))
	commit(t, &log, text("b"))
	_, err := log.Append(sessions.ContextEdit{Target: 0, Replacement: &sessions.ContextReplacement{Message: text("short")}})
	require.NoError(t, err)
	assert.Equal(t, []protocol.Message{text("short"), text("b")}, log.Messages())
}

func TestContextEditLatestWins(t *testing.T) {
	var log sessions.MemoryLog
	commit(t, &log, text("a"))
	commit(t, &log, text("b"))
	_, err := log.Append(sessions.ContextEdit{Target: 0})
	require.NoError(t, err)
	_, err = log.Append(sessions.ContextEdit{Target: 0, Replacement: &sessions.ContextReplacement{Message: text("back")}})
	require.NoError(t, err)
	assert.Equal(t, []protocol.Message{text("back"), text("b")}, log.Messages())

	_, err = log.Append(sessions.ContextEdit{Target: 0})
	require.NoError(t, err)
	assert.Equal(t, []protocol.Message{text("b")}, log.Messages())
}

func TestContextEditIgnoresBadTargets(t *testing.T) {
	var log sessions.MemoryLog
	commit(t, &log, text("a"))
	_, err := log.Append(
		sessions.ContextEdit{Target: 5},
		sessions.ContextEdit{Target: -1},
		sessions.ContextEdit{Target: 1}, // targets itself
	)
	require.NoError(t, err)
	assert.Equal(t, []protocol.Message{text("a")}, log.Messages())
}

func TestContextEditDoesNotReachEarlierPrefix(t *testing.T) {
	var log sessions.MemoryLog
	commit(t, &log, text("a"))
	commit(t, &log, text("b"))
	_, err := log.Append(sessions.ContextEdit{Target: 0})
	require.NoError(t, err)
	entries := log.Entries()
	assert.Equal(t, []protocol.Message{text("a"), text("b")}, sessions.MessagesOf(entries[:2]))
}
