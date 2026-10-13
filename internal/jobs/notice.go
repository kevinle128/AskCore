//go:build unix

package jobs

import (
	"fmt"
	"strings"

	"AskCore/pkg/protocol"
)

// Notifier is the way from the table to the Agent. The table never imports the
// agent package: the composition root passes the methods of the Agent.
type Notifier struct {
	// FollowUp queues a message for a cycle of its own. The table uses it for
	// the completion notice of a job.
	FollowUp func(protocol.Message) (string, error)
	// Steer queues a message for the next turn boundary of the active run. The
	// table uses it for monitor events.
	Steer func(protocol.Message) (string, error)
	// Remove deletes a queued message that no run has taken yet. It reports
	// whether it did. The table uses it to merge monitor events.
	Remove func(inputID string) bool
}

// reminderMessage returns the user message that carries text as a system
// reminder, in the shape that protocol.IsSystemReminder accepts.
func (t *Table) reminderMessage(text string) protocol.UserMessage {
	return protocol.UserMessage{
		Content:   []protocol.UserBlock{protocol.Text{Text: protocol.SystemReminder(text)}},
		Timestamp: t.clock.Now().UnixMilli(),
	}
}

// statusLineLocked renders the state of j, for the notice, job_output and
// job_kill.
func (j *Job) statusLineLocked() string {
	switch j.status {
	case StatusCompleted:
		if j.exitCode != nil {
			return fmt.Sprintf("[status: completed, exit code: %d]", *j.exitCode)
		}
		return "[status: completed]"
	case StatusKilled:
		if j.killReason != "" {
			return "[status: killed, reason: " + j.killReason + "]"
		}
		return "[status: killed]"
	case StatusFailed:
		var detail []string
		if j.signal != "" {
			detail = append(detail, "signal: "+j.signal)
		}
		if j.failure != nil {
			detail = append(detail, "error: "+j.failure.Error())
		}
		if len(detail) == 0 && j.exitCode != nil {
			detail = append(detail, fmt.Sprintf("exit code: %d", *j.exitCode))
		}
		if len(detail) == 0 {
			return "[status: failed]"
		}
		return "[status: failed, " + strings.Join(detail, ", ") + "]"
	default:
		return "[status: " + string(j.status) + "]"
	}
}

// noticeLocked is the text of the completion notice of j.
func (j *Job) noticeLocked() string {
	return fmt.Sprintf("Background job %s (%s: %s) finished %s. Read its output with job_output.",
		j.id, j.kind, j.label, j.statusLineLocked())
}

// sendNotice queues the completion notice. The caller does not hold t.mu.
// A refusal goes to OnError; the job stays readable.
func (t *Table) sendNotice(j *Job) {
	t.mu.Lock()
	if !j.finishedLocked() || j.awaited || j.killedByModel || t.closing || j.noticeStarted {
		t.mu.Unlock()
		return
	}
	j.noticeStarted = true
	id, text := j.id, j.noticeLocked()
	n, hasNotifier := t.notifier, t.hasNote
	t.mu.Unlock()
	if !hasNotifier || n.FollowUp == nil {
		t.report(fmt.Errorf("notice for job %s: %w", id, ErrNoNotifier))
		return
	}
	if _, err := n.FollowUp(t.reminderMessage(text)); err != nil {
		t.report(fmt.Errorf("notice for job %s: %w", id, err))
	}
}

// notifierSnapshot returns the notifier under the lock.
func (t *Table) notifierSnapshot() (Notifier, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.notifier, t.hasNote
}

// eventsMuted reports whether monitor events of j must not be sent: the model
// stopped the job, or the table is closing.
func (t *Table) eventsMuted(j *Job) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return j.killedByModel || t.closing
}
