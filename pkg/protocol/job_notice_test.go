package protocol

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSystemReminderText(t *testing.T) {
	require.True(t, IsSystemReminderText(SystemReminder("x")))
	require.False(t, IsSystemReminderText("x"))
	require.False(t, IsSystemReminderText("<system-reminder>\nopen only"))

	body, ok := SystemReminderBody(UserMessage{Content: []UserBlock{Text{Text: SystemReminder("hello\nworld")}}})
	require.True(t, ok)
	require.Equal(t, "hello\nworld", body)
	_, ok = SystemReminderBody(UserMessage{Content: []UserBlock{Text{Text: "hello"}}})
	require.False(t, ok)
}

func TestParseJobNotice(t *testing.T) {
	for text, want := range map[string]JobNotice{
		"Background job bash-1 (bash: npm run build) finished [status: completed, exit code: 0]. Read its output with job_output.": {
			JobID: "bash-1", Kind: "bash", Label: "npm run build", Status: "completed", Detail: "exit 0"},
		"Background job bash-2 (bash: make (all)) finished [status: failed, exit code: 2]. Read its output with job_output.": {
			JobID: "bash-2", Kind: "bash", Label: "make (all)", Status: "failed", Detail: "exit 2"},
		"Background job bash-3 (bash: go test ./...) finished [status: killed, reason: timeout]. Read its output with job_output.": {
			JobID: "bash-3", Kind: "bash", Label: "go test ./...", Status: "killed", Detail: "timeout"},
		"Background job bash-4 (bash: ./crash) finished [status: failed, signal: SIGSEGV]. Read its output with job_output.": {
			JobID: "bash-4", Kind: "bash", Label: "./crash", Status: "failed", Detail: "signal: SIGSEGV"},
		"Background job bash-5 (bash: true) finished [status: completed]. Read its output with job_output.": {
			JobID: "bash-5", Kind: "bash", Label: "true", Status: "completed"},
	} {
		got, ok := ParseJobNotice(text)
		require.True(t, ok, text)
		require.Equal(t, want, got, text)
	}
	_, ok := ParseJobNotice("Background job finished")
	require.False(t, ok)
}

func TestParseMonitorEvents(t *testing.T) {
	text := "<monitor-event job=\"monitor-1\" description=\"server log\">\nline one\nline two\n[2 events suppressed by the flood guard]\nline three\n</monitor-event>"
	got, ok := ParseMonitorEvents(text)
	require.True(t, ok)
	require.Equal(t, []MonitorEvents{{JobID: "monitor-1", Description: "server log", Events: 3}}, got)

	quoted := "<monitor-event job=\"monitor-2\" description=\"a &amp; b\">\n[... more lines cut]\nx\n</monitor-event>"
	got, ok = ParseMonitorEvents(quoted)
	require.True(t, ok)
	require.Equal(t, []MonitorEvents{{JobID: "monitor-2", Description: "a & b", Events: 1}}, got)

	_, ok = ParseMonitorEvents("no element")
	require.False(t, ok)
}
