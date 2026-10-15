package jobs

import (
	"errors"
	"testing"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

// The TUI reads the notices with the protocol parsers. These tests keep the
// text that the table writes and the parsers in step.
func TestNoticeTextParsesInTheProtocol(t *testing.T) {
	zero, two := 0, 2
	for _, tc := range []struct {
		job  Job
		want protocol.JobNotice
	}{
		{Job{id: "bash-1", kind: KindBash, label: "npm run build", status: StatusCompleted, exitCode: &zero},
			protocol.JobNotice{JobID: "bash-1", Kind: "bash", Label: "npm run build", Status: "completed", Detail: "exit 0"}},
		{Job{id: "bash-2", kind: KindBash, label: "make", status: StatusFailed, exitCode: &two},
			protocol.JobNotice{JobID: "bash-2", Kind: "bash", Label: "make", Status: "failed", Detail: "exit 2"}},
		{Job{id: "bash-3", kind: KindBash, label: "sleep 9", status: StatusKilled, killReason: "timeout"},
			protocol.JobNotice{JobID: "bash-3", Kind: "bash", Label: "sleep 9", Status: "killed", Detail: "timeout"}},
		{Job{id: "bash-4", kind: KindBash, label: "./crash", status: StatusFailed, signal: "SIGSEGV", failure: errors.New("boom")},
			protocol.JobNotice{JobID: "bash-4", Kind: "bash", Label: "./crash", Status: "failed", Detail: "signal: SIGSEGV, error: boom"}},
	} {
		text := tc.job.noticeLocked()
		got, ok := protocol.ParseJobNotice(text)
		require.True(t, ok, text)
		require.Equal(t, tc.want, got, text)
	}
}

func TestMonitorEventTextParsesInTheProtocol(t *testing.T) {
	element := monitorElement("monitor-1", "log <x>", "a\nb")
	got, ok := protocol.ParseMonitorEvents(element)
	require.True(t, ok)
	require.Equal(t, []protocol.MonitorEvents{{JobID: "monitor-1", Description: "log <x>", Events: 2}}, got)
}
