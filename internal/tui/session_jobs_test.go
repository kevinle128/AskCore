package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// backgroundJob runs one background bash call to its end and leaves the session idle.
func backgroundJob(r *viewRig, call, id, command string) {
	r.t.Helper()
	r.event(&protocol.AgentStart{},
		toolStart(call, "bash", `{"command":"`+command+`","run_in_background":true}`, 0),
		toolEndDetails(call, "bash", protocol.ToolResultDetails{JobID: id, Status: "running"}, 100),
		&protocol.AgentEnd{}, &protocol.AgentSettled{})
}

// statusRow returns the row of the screen that starts with the status rail.
func statusRow(r *viewRig) string {
	r.t.Helper()
	for _, row := range r.h.grid() {
		if strings.HasPrefix(row, "┊") {
			return strings.TrimRight(row, " ")
		}
	}
	return ""
}

func requireNoReminderOnScreen(t *testing.T, r *viewRig) {
	t.Helper()
	for _, row := range r.h.text() {
		require.NotContains(t, row, "system-reminder")
		require.NotContains(t, row, "Background job")
	}
}

func TestJobCountOnTheIdleRail(t *testing.T) {
	r := startView(t, 80, 24)
	backgroundJob(r, "c1", "bash-1", "npm run build")
	r.h.waitText("┊ ◌ 1 background job · bash-1 npm run build")

	backgroundJob(r, "c2", "bash-3", "go test ./...")
	r.h.waitText("┊ ◌ 2 background jobs · bash-1, bash-3")
}

func TestJobCountOnTheIdleRailNarrow(t *testing.T) {
	r := startView(t, 40, 14)
	backgroundJob(r, "c1", "bash-1", "npm run build")
	r.h.waitFor("short job rail", func(*screenHarness) bool { return statusRow(r) == "┊ ◌ 1 job" })
}

func TestJobCountJoinsTheWorkingTail(t *testing.T) {
	r := startView(t, 80, 24)
	backgroundJob(r, "c1", "bash-1", "npm run build")
	r.event(&protocol.AgentStart{})
	r.h.waitFor("working tail", func(*screenHarness) bool {
		return strings.HasSuffix(statusRow(r), "esc to interrupt · 1 job")
	})
}

func TestFinishedJobIsAJobLineAndLeavesTheRail(t *testing.T) {
	r := startView(t, 80, 24)
	backgroundJob(r, "c1", "bash-1", "npm run build")
	r.h.waitText("1 background job")
	r.event(&protocol.AgentStart{})
	r.event(reminderEvents(jobNotice("bash-1", "npm run build", "completed, exit code: 0"), 200)...)
	r.event(&protocol.AgentEnd{}, &protocol.AgentSettled{})
	r.h.waitText("  ◆ job bash-1 npm run build")
	r.h.waitText("completed · exit 0")
	r.waitGone("background job")
	requireNoReminderOnScreen(t, r)
}

func TestFailedAndKilledJobLines(t *testing.T) {
	r := startView(t, 80, 24)
	r.event(&protocol.AgentStart{})
	r.event(reminderEvents(jobNotice("bash-1", "npm run build", "failed, exit code: 2"), 0)...)
	r.event(reminderEvents(jobNotice("bash-3", "go test ./...", "killed, reason: timeout"), 100)...)
	r.event(&protocol.AgentEnd{}, &protocol.AgentSettled{})
	r.h.waitText("✗ failed · exit 2")
	r.h.waitText("✗ killed · timeout")
	requireNoReminderOnScreen(t, r)
}

func TestMonitorNoticeLine(t *testing.T) {
	r := startView(t, 80, 24)
	r.event(&protocol.AgentStart{})
	r.event(reminderEvents("<monitor-event job=\"bash-2\" description=\"tail -f server.log\">\na\nb\nc\n</monitor-event>", 0)...)
	r.event(&protocol.AgentEnd{}, &protocol.AgentSettled{})
	r.h.waitText("  ◆ job bash-2 tail -f server.log")
	r.h.waitText("3 events")
	requireNoReminderOnScreen(t, r)
}

func TestQueuedNoticeHasNoRow(t *testing.T) {
	r := startView(t, 80, 24)
	r.event(&protocol.AgentStart{})
	r.typeText("also check the lint")
	r.key("alt+enter")
	r.waitCall("followup:also check the lint")
	notice := protocol.SystemReminder(jobNotice("bash-1", "npm run build", "completed, exit code: 0"))
	r.event(&protocol.QueueUpdate{FollowUp: []string{"also check the lint", notice}})
	r.h.waitText("┆ ⧗ next    also check the lint")
	r.h.waitText("┆ alt+↑ edit queue")
	require.Equal(t, 1, r.h.count("⧗"), "one queue row")
	requireNoReminderOnScreen(t, r)
}

func TestOnlyANoticeQueuedDrawsNoQueue(t *testing.T) {
	r := startView(t, 80, 24)
	r.event(&protocol.AgentStart{})
	r.h.waitText("Working…")
	notice := protocol.SystemReminder(jobNotice("bash-1", "npm run build", "completed, exit code: 0"))
	r.event(&protocol.QueueUpdate{FollowUp: []string{notice}})
	require.Zero(t, r.h.count("┆"))

	r.key("alt+up")
	r.h.settle()
	require.Zero(t, r.h.count("Some queued input stays"), "a hidden notice is not an input of another client")
	m := r.stopped()
	require.NotContains(t, m.sv.editor.Text(), "system-reminder")
}

// The screens of the approved design, at the sizes it states.
func TestJobNoticeGoldens(t *testing.T) {
	for _, size := range []struct{ w, h int }{{80, 24}, {40, 14}} {
		t.Run(fmt.Sprintf("running_%d", size.w), func(t *testing.T) {
			r := startView(t, size.w, size.h)
			r.event(userEvents("run the build in the background and tell me when it is done", 0)...)
			backgroundJob(r, "c1", "bash-1", "npm run build")
			r.h.waitText("1 ")
			requireContentGolden(t, r.h)
		})
		t.Run(fmt.Sprintf("finished_%d", size.w), func(t *testing.T) {
			r := startView(t, size.w, size.h)
			r.event(userEvents("run the build in the background and tell me when it is done", 0)...)
			backgroundJob(r, "c1", "bash-1", "npm run build")
			r.event(&protocol.AgentStart{})
			r.event(reminderEvents(jobNotice("bash-1", "npm run build", "completed, exit code: 0"), 200)...)
			r.h.waitText("completed")
			requireContentGolden(t, r.h)
		})
	}
}
