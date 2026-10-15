package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolDeclJSON(t *testing.T) {
	got, err := json.Marshal(ToolDecl{Name: "echo", Description: "d"})
	require.NoError(t, err)
	assert.Equal(t, `{"name":"echo","description":"d","parameters":{}}`, string(got))

	got, err = json.Marshal(ToolDecl{Name: "echo", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)})
	require.NoError(t, err)
	assert.Equal(t, `{"name":"echo","description":"d","parameters":{"type":"object"}}`, string(got))

	got, err = json.Marshal(ToolRef{Name: "echo"})
	require.NoError(t, err)
	assert.Equal(t, `{"name":"echo"}`, string(got))
}

func TestToolResultDetailsRoundTrip(t *testing.T) {
	exit := 2
	in := ToolResultDetails{
		Path:        "/repo/a.go",
		Diff:        &ToolDiff{Path: "/repo/a.go", OldText: "a", NewText: "b"},
		Locations:   []ToolLocation{{Path: "/repo/a.go", Line: 7}},
		Truncation:  &ToolTruncation{TruncatedBy: "lines", TotalLines: 3000, TotalBytes: 90000, OutputLines: 2000, OutputBytes: 51200},
		NextOffset:  2001,
		SpillPath:   "/tmp/ask-jobs-1/bash-1.log",
		SpillCapped: true,
		ExitCode:    &exit,
		Signal:      "SIGKILL",
		JobID:       "bash-1",
		Status:      "killed",
		Cwd:         "/repo/sub",
		Kind:        ToolKindExecute,
	}
	raw, err := json.Marshal(in)
	require.NoError(t, err)

	var out ToolResultDetails
	require.NoError(t, json.Unmarshal(raw, &out))
	assert.Equal(t, in, out)

	var wire map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &wire))
	for _, key := range []string{"path", "diff", "locations", "truncation", "nextOffset", "spillPath", "spillCapped", "exitCode", "signal", "jobId", "status", "cwd", "kind"} {
		assert.Contains(t, wire, key)
	}
}

func TestToolResultDetailsOmitsAbsentFields(t *testing.T) {
	raw, err := json.Marshal(ToolResultDetails{})
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(raw))

	zero := 0
	raw, err = json.Marshal(ToolResultDetails{ExitCode: &zero})
	require.NoError(t, err)
	assert.JSONEq(t, `{"exitCode":0}`, string(raw))
}

func TestIsSystemReminderMatchesWrappedText(t *testing.T) {
	wrapped := SystemReminder("job bash-1 finished")
	assert.Equal(t, "<system-reminder>\njob bash-1 finished\n</system-reminder>", wrapped)

	assert.True(t, IsSystemReminder(UserMessage{Content: []UserBlock{Text{Text: wrapped}}}))
	assert.False(t, IsSystemReminder(UserMessage{Content: []UserBlock{Text{Text: "job bash-1 finished"}}}))
	assert.False(t, IsSystemReminder(UserMessage{Content: []UserBlock{Text{Text: wrapped}, Text{Text: wrapped}}}))
	assert.False(t, IsSystemReminder(UserMessage{Content: []UserBlock{Text{Text: "hi " + wrapped}}}))
	assert.False(t, IsSystemReminder(UserMessage{Content: []UserBlock{Text{Text: wrapped + " tail"}}}))
	assert.False(t, IsSystemReminder(UserMessage{}))
}
