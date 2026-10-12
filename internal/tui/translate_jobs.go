package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"AskCore/pkg/protocol"
)

// Background jobs.
//
// The harness tells the model about a finished job, or about new output of a
// monitor, with a user message in a system reminder. The transcript never
// shows that message as a prompt: a job or monitor notice becomes one job
// line, and any other reminder draws nothing. The running jobs come from the
// details of tool results: a result with a job id and the status running adds
// the job, a final status or a notice removes it.

// reminder draws a system reminder that the agent took.
func (t *Translator) reminder(body string, out *[]tea.Msg) {
	if n, ok := protocol.ParseJobNotice(body); ok {
		t.dropJob(n.JobID)
		status := n.Status
		if n.Detail != "" {
			status += " · " + n.Detail
		}
		name := "job " + n.JobID
		if n.Status == "completed" {
			emitAppend(out, t.nextID("j"), newJobNotice(name, n.Label, ToolNoted, status, "", t.view), true)
		} else {
			emitAppend(out, t.nextID("j"), newJobNotice(name, n.Label, ToolFailed, "", status, t.view), true)
		}
		return
	}
	events, ok := protocol.ParseMonitorEvents(body)
	if !ok {
		return
	}
	for _, ev := range events {
		label := ev.Description
		for _, j := range t.jobs {
			if j.ID == ev.JobID && j.Label != "" {
				label = j.Label
			}
		}
		word := "events"
		if ev.Events == 1 {
			word = "event"
		}
		emitAppend(out, t.nextID("j"), newJobNotice("job "+ev.JobID, label, ToolNoted, fmt.Sprintf("%d %s", ev.Events, word), "", t.view), true)
	}
}

// noteJob updates the running jobs from the details of a tool result.
func (t *Translator) noteJob(e *protocol.ToolExecutionEnd, name, args string) {
	if len(e.Result.Details) == 0 {
		return
	}
	var d protocol.ToolResultDetails
	if json.Unmarshal(e.Result.Details, &d) != nil || d.JobID == "" || d.Status == "" {
		return
	}
	if d.Status != "running" && d.Status != "stopping" {
		t.dropJob(d.JobID)
		return
	}
	for _, j := range t.jobs {
		if j.ID == d.JobID {
			return
		}
	}
	if strings.HasPrefix(name, "job_") {
		args = "" // the arguments of job_output name the job, not its command
	}
	t.jobs = append(t.jobs, RunningJob{ID: d.JobID, Label: args})
}

func (t *Translator) dropJob(id string) {
	kept := t.jobs[:0]
	for _, j := range t.jobs {
		if j.ID != id {
			kept = append(kept, j)
		}
	}
	t.jobs = kept
}
