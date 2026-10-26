//go:build unix

package jobs

import (
	"fmt"
	"strings"

	"AskCore/pkg/protocol"
)

// Owner is the conversation that started a job. The completion notice and
// the monitor events of the job go to that conversation only. The table never
// imports the agent package: the tool that starts the job passes the owner.
type Owner interface {
	// Gen is the generation of the conversation. Only callers of the same
	// generation see the job.
	Gen() uint64
	// Notify queues msg for the model. A refusal of a reset or closed
	// conversation is expected; see Options.IsExpectedDrop.
	Notify(msg protocol.Message) (string, error)
	// Remove deletes a queued message that no run has taken yet. It reports
	// whether it did. The table uses it to merge monitor events.
	Remove(id string) bool
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
	if j.status == StatusKilled && j.killReason == limitReason {
		return fmt.Sprintf("Background command %q was stopped after reaching its background time limit. It was job %s. Read its output with job_output; start it again with a longer timeout if the work still needs it.",
			j.label, j.id)
	}
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
	t.mu.Unlock()
	if j.owner == nil {
		t.report(fmt.Errorf("notice for job %s: %w", id, ErrNoOwner))
		return
	}
	if _, err := j.owner.Notify(t.reminderMessage(text)); err != nil {
		t.refused(j, fmt.Errorf("notice for job %s: %w", id, err))
	}
}

// refused handles a refusal of the owner. An expected refusal, such as the
// notice of a reset conversation, mutes the job and is not reported.
func (t *Table) refused(j *Job, err error) {
	if t.expected != nil && t.expected(err) {
		t.mu.Lock()
		j.muted = true
		t.mu.Unlock()
		return
	}
	t.report(err)
}

// eventsMuted reports whether monitor events of j must not be sent: the model
// stopped the job, the owner refused it as expected, or the table is closing.
func (t *Table) eventsMuted(j *Job) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return j.killedByModel || j.muted || t.closing
}
