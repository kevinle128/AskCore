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

func TestParseJobNoticeStallAndLimit(t *testing.T) {
	for text, want := range map[string]JobNotice{
		`Background command "npm run dev" (job bash-3) printed no output for 5 minutes. It still runs. Read it with job_output, stop it with job_kill, or leave it running.`: {
			JobID: "bash-3", Kind: "bash", Label: "npm run dev", Status: "running", Detail: "no output for 5 minutes"},
		`Background command "echo \"a)\" (x)" (job bash-4) printed no output for 1 second. It still runs. Read it with job_output, stop it with job_kill, or leave it running.`: {
			JobID: "bash-4", Kind: "bash", Label: `echo "a)" (x)`, Status: "running", Detail: "no output for 1 second"},
		`Background command "sleep 9" (job bash-5) printed no output for 1.5s. It still runs. Read it with job_output, stop it with job_kill, or leave it running.`: {
			JobID: "bash-5", Kind: "bash", Label: "sleep 9", Status: "running", Detail: "no output for 1.5s"},
		`Background command "make" was stopped after reaching its background time limit. It was job bash-6. Read its output with job_output; start it again with a longer timeout if the work still needs it.`: {
			JobID: "bash-6", Kind: "bash", Label: "make", Status: "killed", Detail: "background time limit"},
		`Background command "echo \"a)\" (job bash-9)" was stopped after reaching its background time limit. It was job bash-7. Read its output with job_output; start it again with a longer timeout if the work still needs it.`: {
			JobID: "bash-7", Kind: "bash", Label: `echo "a)" (job bash-9)`, Status: "killed", Detail: "background time limit"},
	} {
		got, ok := ParseJobNotice(text)
		require.True(t, ok, text)
		require.Equal(t, want, got, text)
	}
	for _, bad := range []string{
		`Background command "x" (job bash-1) printed no output for . It still runs.`,
		`Background command x (job bash-1) printed no output for 5 minutes. It still runs. Read it with job_output, stop it with job_kill, or leave it running.`,
	} {
		_, ok := ParseJobNotice(bad)
		require.False(t, ok, bad)
	}
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
