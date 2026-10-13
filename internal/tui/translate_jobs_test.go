package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func reminderEvents(body string, ms int64) []protocol.Event {
	return userEvents(protocol.SystemReminder(body), ms)
}

func jobNotice(id, label, status string) string {
	return "Background job " + id + " (bash: " + label + ") finished [status: " + status + "]. Read its output with job_output."
}

func toolEndDetails(id, name string, details protocol.ToolResultDetails, ms int64) protocol.Event {
	raw, _ := json.Marshal(details)
	return &protocol.ToolExecutionEnd{
		Envelope: at(ms), ToolCallID: id, ToolName: name,
		Result: protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: "ok"}}, Details: raw},
	}
}

func requireNoReminderText(t *testing.T, rows []string) {
	t.Helper()
	for _, r := range rows {
		require.NotContains(t, r, "system-reminder")
		require.NotContains(t, r, "Background job")
		require.NotContains(t, r, "❯")
	}
}

func TestTranslateJobNoticeIsAJobLine(t *testing.T) {
	for _, tc := range []struct{ status, want string }{
		{"completed, exit code: 0", "completed · exit 0"},
		{"failed, exit code: 2", "✗ failed · exit 2"},
		{"killed, reason: timeout", "✗ killed · timeout"},
		{"completed", "completed"},
	} {
		x := newTx(t)
		x.feed(reminderEvents(jobNotice("bash-1", "npm run build", tc.status), 0)...)
		rows := x.rows()
		require.Len(t, rows, 1, tc.status)
		require.True(t, strings.HasPrefix(rows[0], "  ◆ job bash-1 npm run build"), rows[0])
		require.True(t, strings.HasSuffix(rows[0], tc.want), rows[0])
		requireNoReminderText(t, rows)
	}
}

func TestTranslateMonitorNoticeCountsEvents(t *testing.T) {
	x := newTx(t)
	x.feed(toolStart("c1", "monitor", `{"command":"tail -f server.log","description":"server log"}`, 0),
		toolEndDetails("c1", "monitor", protocol.ToolResultDetails{JobID: "monitor-1", Status: "running"}, 100))
	x.feed(reminderEvents("<monitor-event job=\"monitor-1\" description=\"server log\">\na\nb\nc\n</monitor-event>", 200)...)
	rows := x.rows()
	last := rows[len(rows)-1]
	require.True(t, strings.HasPrefix(last, "  ◆ job monitor-1 tail -f server.log"), last)
	require.True(t, strings.HasSuffix(last, "3 events"), last)
	requireNoReminderText(t, rows)
	require.Equal(t, []RunningJob{{ID: "monitor-1", Label: "tail -f server.log"}}, x.trl.RunningJobs(), "a monitor runs on after its events")
}

func TestTranslateUnknownReminderDrawsNothing(t *testing.T) {
	x := newTx(t)
	x.feed(reminderEvents("Something the harness said.", 0)...)
	require.Empty(t, x.rows())
}

func TestTranslateTracksRunningJobs(t *testing.T) {
	x := newTx(t)
	x.feed(toolStart("c1", "bash", `{"command":"npm run build","run_in_background":true}`, 0),
		toolEndDetails("c1", "bash", protocol.ToolResultDetails{JobID: "bash-1", Status: "running"}, 100),
		toolStart("c2", "bash", `{"command":"go test ./...","run_in_background":true}`, 200),
		toolEndDetails("c2", "bash", protocol.ToolResultDetails{JobID: "bash-3", Status: "running"}, 300))
	require.Equal(t, []RunningJob{{ID: "bash-1", Label: "npm run build"}, {ID: "bash-3", Label: "go test ./..."}}, x.trl.RunningJobs())

	// The model stopped bash-3: there is no notice for it.
	x.feed(toolStart("c3", "job_kill", `{"job_id":"bash-3"}`, 400),
		toolEndDetails("c3", "job_kill", protocol.ToolResultDetails{JobID: "bash-3", Status: "killed"}, 500))
	require.Equal(t, []RunningJob{{ID: "bash-1", Label: "npm run build"}}, x.trl.RunningJobs())

	// job_output of a running job keeps it.
	x.feed(toolStart("c4", "job_output", `{"job_id":"bash-1"}`, 600),
		toolEndDetails("c4", "job_output", protocol.ToolResultDetails{JobID: "bash-1", Status: "running"}, 700))
	require.Len(t, x.trl.RunningJobs(), 1)

	x.feed(reminderEvents(jobNotice("bash-1", "npm run build", "completed, exit code: 0"), 800)...)
	require.Empty(t, x.trl.RunningJobs())
}

func TestTranslateJobsSurviveAResetAndEndWithForget(t *testing.T) {
	x := newTx(t)
	x.feed(toolStart("c1", "bash", `{"command":"sleep 9"}`, 0),
		toolEndDetails("c1", "bash", protocol.ToolResultDetails{JobID: "bash-1", Status: "running"}, 100))
	x.trl.Reset() // a resync replaces the history of the same session
	require.Len(t, x.trl.RunningJobs(), 1)
	x.trl.ForgetJobs() // the view follows another session
	require.Empty(t, x.trl.RunningJobs())
}

func TestTranslateSnapshotDrawsAJobLine(t *testing.T) {
	x := newTx(t)
	m := protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: protocol.SystemReminder(jobNotice("bash-2", "make", "failed, exit code: 2"))}}, Timestamp: baseMs}
	x.apply(x.trl.Snapshot(SnapshotMsg{Entries: []SnapshotEntry{{Kind: "message", Message: m}}}))
	rows := x.rows()
	require.True(t, strings.HasPrefix(rows[0], "  ◆ job bash-2 make"), rows[0])
	require.True(t, strings.HasSuffix(rows[0], "✗ failed · exit 2"), rows[0])
	requireNoReminderText(t, rows)
}
