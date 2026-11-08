package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"AskCore/internal/jobs"
	"AskCore/internal/sandbox"
	"AskCore/pkg/protocol"
)

const (
	// jobOutputWaitDefault and jobOutputWaitMax bound the timeout_ms of a
	// job_output call that waits.
	jobOutputWaitDefault = 30 * time.Second
	jobOutputWaitMax     = 600 * time.Second
)

// unknownJob returns the error for an id that the table does not hold.
func unknownJob(id string) error {
	return failf("No job with id %s. Use job_list.", id)
}

// JobOutput reads the output of a job since the previous read, with its
// status.
type JobOutput struct {
	table *jobs.Table
}

type jobOutputInput struct {
	JobID     string   `json:"job_id"`
	Wait      *bool    `json:"wait"`
	TimeoutMs *float64 `json:"timeout_ms"`
}

func (j *jobOutputInput) waits() bool { return j.Wait != nil && *j.Wait }

// ConcurrencySafe is true for a read that answers at once. A read that waits
// for the end of the job runs alone.
func (*JobOutput) ConcurrencySafe(args json.RawMessage) bool {
	var in jobOutputInput
	if err := json.Unmarshal(args, &in); err != nil {
		return false
	}
	return !in.waits()
}

func (*JobOutput) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{
		Name: "job_output",
		Description: "Read the output that a background job wrote since the previous read, and its status. " +
			"A job that still runs answers at once with the output so far. " +
			"A notice arrives when a background job finishes, so do not poll job_output while a job runs. " +
			"Set wait to true only when you have nothing else to do and need the end of the job: the call then blocks " +
			"until the job ends or timeout_ms passes (default 30000, maximum 600000). " +
			"Reading the end of a job removes the job and no notice follows.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"job_id":{"type":"string","description":"Id of the job, such as bash-1 or monitor-1"},` +
			`"wait":{"type":"boolean","description":"Block until the job ends or timeout_ms passes"},` +
			`"timeout_ms":{"type":"number","description":"Longest wait in milliseconds when wait is true (default 30000, maximum 600000)"}},` +
			`"required":["job_id"]}`),
	}
}

func (*JobOutput) PromptSnippet() string { return "Read the output and status of a background job" }

func (*JobOutput) PromptGuidelines() []string { return nil }

func (t *JobOutput) Execute(ctx context.Context, tc Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	var in jobOutputInput
	if err := json.Unmarshal(args, &in); err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("job_output: %w", err)
	}
	timeout := jobOutputWaitDefault
	if in.TimeoutMs != nil && *in.TimeoutMs > 0 {
		timeout = time.Duration(min(*in.TimeoutMs, float64(jobOutputWaitMax/time.Millisecond)) * float64(time.Millisecond))
	}
	owner, err := jobOwnerOf(tc)
	if err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	res, err := t.table.Read(ctx, owner.Gen(), in.JobID, in.waits(), timeout)
	if errors.Is(err, jobs.ErrUnknownJob) {
		return protocol.ToolExecutionResult{}, unknownJob(in.JobID)
	}
	if err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("job_output: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}

	text, tr := jobOutputText(res)
	details := protocol.ToolResultDetails{
		Kind:        protocol.ToolKindExecute,
		JobID:       res.JobID,
		Status:      string(res.Status),
		ExitCode:    res.ExitCode,
		Signal:      res.Signal,
		SpillPath:   res.SpillPath,
		SpillCapped: res.SpillCapped,
		Truncation:  toolTruncation(tr),
	}
	return textResult(text, details, nil), nil
}

// jobOutputText returns the model text of a read. The text of the job table holds
// the output, the status line and, for a running job, the line that says a
// notice will arrive. When the output is over the limits of the model view,
// only the output part is cut to its head and tail.
func jobOutputText(res jobs.ReadResult) (string, sandbox.Truncation) {
	at := strings.LastIndex(res.Text, res.StatusLine)
	if at < 0 {
		return res.Text, sandbox.Truncation{}
	}
	body, tail := res.Text[:at], res.Text[at:]
	view, tr := sandbox.View(sandbox.Output{Head: []byte(body), Total: int64(len(body))}, 0, 0)
	if !tr.Truncated {
		return res.Text, sandbox.Truncation{}
	}
	return strings.TrimRight(view, "\n") + "\n" + tail, tr
}

// JobKill stops a job and returns its final output and status.
type JobKill struct {
	table *jobs.Table
}

type jobKillInput struct {
	JobID  string `json:"job_id"`
	Reason string `json:"reason"`
}

func (*JobKill) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{
		Name: "job_kill",
		Description: "Stop a background job and its whole process group. Returns the output of the job and its final status. " +
			"No notice follows. Use job_list to see the ids.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"job_id":{"type":"string","description":"Id of the job, such as bash-1 or monitor-1"},` +
			`"reason":{"type":"string","description":"Why the job is stopped; it appears in the final status"}},` +
			`"required":["job_id"]}`),
	}
}

func (*JobKill) PromptSnippet() string { return "Stop a background job" }

func (*JobKill) PromptGuidelines() []string { return nil }

func (t *JobKill) Execute(ctx context.Context, tc Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	var in jobKillInput
	if err := json.Unmarshal(args, &in); err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("job_kill: %w", err)
	}
	owner, err := jobOwnerOf(tc)
	if err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	job, err := t.table.Kill(owner.Gen(), in.JobID, in.Reason)
	if errors.Is(err, jobs.ErrUnknownJob) {
		return protocol.ToolExecutionResult{}, unknownJob(in.JobID)
	}
	if err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("job_kill: %w", err)
	}

	out := job.Snapshot()
	view, tr := shellView(out)
	headline := fmt.Sprintf("Job %s killed.", job.ID())
	if job.Status() != jobs.StatusKilled {
		headline = fmt.Sprintf("Job %s had already finished.", job.ID())
	}
	text := headline
	if view != "" {
		text += "\n" + view
	}
	text += "\n" + job.StatusLine()

	res := job.Result()
	details := protocol.ToolResultDetails{
		Kind:     protocol.ToolKindExecute,
		JobID:    job.ID(),
		Status:   string(job.Status()),
		ExitCode: res.ExitCode,
		Signal:   res.Signal,
	}
	spill(&details, out)
	return textResult(text, details, &tr), nil
}

// JobList lists the jobs of the session table.
type JobList struct {
	table *jobs.Table
}

func (*JobList) ConcurrencySafe(json.RawMessage) bool { return true }

func (*JobList) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{
		Name: "job_list",
		Description: "List the jobs of this session: running background jobs and monitors, and finished jobs whose end " +
			"you have not read. One line for each job: id, kind, status, elapsed time and command.",
		Parameters: json.RawMessage(`{"type":"object","properties":{}}`),
	}
}

func (*JobList) PromptSnippet() string { return "List the background jobs of this session" }

func (*JobList) PromptGuidelines() []string { return nil }

func (t *JobList) Execute(ctx context.Context, tc Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	owner, err := jobOwnerOf(tc)
	if err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	list := t.table.List(owner.Gen())
	text := "No jobs."
	if len(list) > 0 {
		lines := make([]string, len(list))
		for i, j := range list {
			lines[i] = fmt.Sprintf("%s [%s] %s %s - %s", j.ID, j.Kind, j.Status, formatElapsed(j.Elapsed), j.Label)
		}
		text = strings.Join(lines, "\n")
	}
	return textResult(text, protocol.ToolResultDetails{Kind: protocol.ToolKindExecute}, nil), nil
}

// formatElapsed writes a duration as 12s, 3m05s or 2h07m.
func formatElapsed(d time.Duration) string {
	s := int(d.Seconds())
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	}
	return fmt.Sprintf("%dh%02dm", s/3600, s%3600/60)
}
