package sessions_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

func TestCloneEntryIsDeep(t *testing.T) {
	temp := 0.5
	usage := protocol.Usage{Input: 3}
	orig := []sessions.Entry{
		sessions.SystemSnapshot{SystemPrompt: "p", Tools: []protocol.ToolDecl{{Name: "t", Parameters: json.RawMessage(`{"a":1}`)}}},
		sessions.RequestDelta{
			AttemptID: "a",
			Added:     []protocol.Message{text("x")},
			Removed:   []protocol.Message{text("y")},
			Model:     sessions.ModelRef{HeaderNames: []string{"X-One"}},
			Options:   sessions.RequestOptions{Temperature: &temp},
		},
		sessions.AttemptSettled{AttemptID: "a", Usage: &usage, Failure: &sessions.Failure{Code: "error", Text: "t"}},
	}
	for _, e := range orig {
		c := sessions.Clone(e)
		assert.Equal(t, e, c)
	}

	snap := sessions.Clone(orig[0]).(sessions.SystemSnapshot)
	snap.Tools[0].Parameters[2] = 'z'
	assert.JSONEq(t, `{"a":1}`, string(orig[0].(sessions.SystemSnapshot).Tools[0].Parameters))

	delta := sessions.Clone(orig[1]).(sessions.RequestDelta)
	delta.Model.HeaderNames[0] = "changed"
	*delta.Options.Temperature = 9
	assert.Equal(t, "X-One", orig[1].(sessions.RequestDelta).Model.HeaderNames[0])
	assert.Equal(t, 0.5, temp)

	settled := sessions.Clone(orig[2]).(sessions.AttemptSettled)
	settled.Usage.Input = 99
	settled.Failure.Text = "changed"
	assert.Equal(t, 3, int(usage.Input))
	assert.Equal(t, "t", orig[2].(sessions.AttemptSettled).Failure.Text)
}

func TestEntriesEncodeAsJSON(t *testing.T) {
	entries := []sessions.Entry{
		sessions.MessageEntry{Message: text("hi")},
		sessions.CycleOpened{CycleID: "c"},
		sessions.CycleClosed{CycleID: "c", Reason: "aborted", Cause: "user"},
		sessions.TurnOpened{CycleID: "c"},
		sessions.TurnClosed{CycleID: "c"},
		sessions.AttemptSettled{AttemptID: "a", Outcome: "completed"},
		sessions.RequestDelta{AttemptID: "a"},
		sessions.SystemSnapshot{SystemPrompt: "p"},
		sessions.InputOutcome{InputID: "i", Accepted: true},
	}
	for _, e := range entries {
		_, err := json.Marshal(e)
		require.NoError(t, err, "%T", e)
	}
}
