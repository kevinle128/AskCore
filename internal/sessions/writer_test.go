package sessions_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

func TestWriterRejectsEntryThatCannotBeEncoded(t *testing.T) {
	var log sessions.MemoryLog
	commit(t, &log, text("kept"))

	// The arguments are not valid JSON, so encoding/json refuses the message.
	bad := protocol.ToolResultMessage{ToolCallID: "c1", ToolName: "t", Details: json.RawMessage(`{broken`)}
	ref, err := log.Append(
		sessions.MessageEntry{Message: text("good, but in the same call")},
		sessions.MessageEntry{Message: bad},
	)
	require.ErrorIs(t, err, sessions.ErrUnencodableEntry)
	assert.Equal(t, sessions.CommitRef{}, ref)
	assert.Len(t, log.Entries(), 1, "an Append that fails writes none of its entries")
	assert.Equal(t, []protocol.Message{text("kept")}, log.Messages())

	_, err = log.Append(nil)
	require.ErrorIs(t, err, sessions.ErrUnencodableEntry)
	_, err = log.Append(sessions.MessageEntry{})
	require.ErrorIs(t, err, sessions.ErrUnencodableEntry)
	assert.Len(t, log.Entries(), 1)
}

func TestWriterAppendIsOneStepAndReportsPositions(t *testing.T) {
	var log sessions.MemoryLog
	ref, err := log.Append(sessions.CycleOpened{CycleID: "c"}, sessions.MessageEntry{Message: text("a")})
	require.NoError(t, err)
	assert.Equal(t, sessions.CommitRef{Start: 0, End: 2}, ref)

	ref, err = log.Append(sessions.CycleClosed{CycleID: "c", Reason: "completed"})
	require.NoError(t, err)
	assert.Equal(t, sessions.CommitRef{Start: 2, End: 3}, ref)

	assert.Equal(t, []sessions.Entry{
		sessions.CycleOpened{CycleID: "c"},
		sessions.MessageEntry{Message: text("a")},
		sessions.CycleClosed{CycleID: "c", Reason: "completed"},
	}, log.Entries())
	assert.Equal(t, []protocol.Message{text("a")}, log.Messages(), "Messages returns message entries only")
}

func TestMemoryLogCloseKeepsEntriesReadable(t *testing.T) {
	var log sessions.MemoryLog
	commit(t, &log, text("kept"))

	require.NoError(t, log.Close())
	require.NoError(t, log.Close(), "a second close is harmless")

	assert.Equal(t, []protocol.Message{text("kept")}, log.Messages())
}
