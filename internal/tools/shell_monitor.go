package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"AskCore/internal/jobs"
	"AskCore/pkg/protocol"
)

// monitorMaxTimeoutMs is the default and the largest timeout of a monitor
// that is not persistent, in milliseconds: 10 hours.
const monitorMaxTimeoutMs = int64(36_000_000)

// Monitor starts a command whose every stdout line reaches the model as a
// notice. It differs from a background bash job in its timeout: a monitor
// stops at timeout_ms unless it is persistent.
type Monitor struct {
	table *jobs.Table
}

type monitorInput struct {
	Command     string   `json:"command"`
	Description string   `json:"description"`
	TimeoutMs   *float64 `json:"timeout_ms"`
	Persistent  *bool    `json:"persistent"`
}

// monitorDetails adds the resolved limits to the details of a job.
type monitorDetails struct {
	protocol.ToolResultDetails
	TimeoutMs  int64 `json:"timeoutMs"`
	Persistent bool  `json:"persistent"`
}

func (*Monitor) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{
		Name: "monitor",
		Description: "Start a background monitor that streams events from a long-running script. " +
			"Each stdout line is an event; you can keep working and notifications arrive in the chat. Exit ends the watch. " +
			"Output volume: every stdout line wakes the agent. Print only DONE/FAILED/CANCELLED lines, no progress lines. " +
			"Use `grep --line-buffered` in pipes (plain `grep` buffers and delays events by minutes). " +
			"Responsiveness: print FAILED at once when any tracked item fails; never wait for unrelated work to finish. " +
			"Set persistent: true for session-length watches (PR monitoring, log tails); the monitor then runs until you " +
			"call job_kill or the session ends. Otherwise it stops at timeout_ms (default 10 h).",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"command":{"type":"string","description":"Shell command to run; each stdout line is an event"},` +
			`"description":{"type":"string","description":"Short name of the watch, shown with each event"},` +
			`"timeout_ms":{"type":"number","description":"Stop the monitor after this many milliseconds (default 36000000, which is 10 h, and also the maximum unless persistent is true)"},` +
			`"persistent":{"type":"boolean","description":"Run until job_kill or the end of the session; there is no timeout"}},` +
			`"required":["command","description"]}`),
	}
}

func (*Monitor) PromptSnippet() string {
	return "Watch a long-running command: each of its stdout lines arrives as a notice"
}

func (*Monitor) PromptGuidelines() []string { return nil }

func (m *Monitor) Execute(ctx context.Context, tc Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	var in monitorInput
	if err := json.Unmarshal(args, &in); err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("monitor: %w", err)
	}
	if strings.TrimSpace(in.Command) == "" {
		return protocol.ToolExecutionResult{}, failf("The command is empty.")
	}
	if strings.TrimSpace(in.Description) == "" {
		return protocol.ToolExecutionResult{}, failf("A monitor needs a description.")
	}
	persistent := in.Persistent != nil && *in.Persistent
	timeoutMs := monitorMaxTimeoutMs
	if in.TimeoutMs != nil {
		if math.IsNaN(*in.TimeoutMs) || *in.TimeoutMs <= 0 {
			return protocol.ToolExecutionResult{}, failf("timeout_ms must be more than 0.")
		}
		if !persistent && *in.TimeoutMs > float64(monitorMaxTimeoutMs) {
			return protocol.ToolExecutionResult{}, failf("persistent must be true when timeout_ms exceeds %d ms", monitorMaxTimeoutMs)
		}
		timeoutMs = int64(math.Ceil(math.Min(*in.TimeoutMs, float64(monitorMaxTimeoutMs))))
	}
	if persistent {
		timeoutMs = 0
	}

	job, err := m.table.StartMonitor(jobs.MonitorSpec{
		Command:     in.Command,
		Cwd:         tc.Cwd,
		Description: in.Description,
		Timeout:     time.Duration(timeoutMs) * time.Millisecond,
		Persistent:  persistent,
	})
	if err != nil {
		return protocol.ToolExecutionResult{}, startFailure(err)
	}

	details := monitorDetails{
		ToolResultDetails: protocol.ToolResultDetails{
			Kind:   protocol.ToolKindExecute,
			JobID:  job.ID(),
			Status: string(jobs.StatusRunning),
		},
		TimeoutMs:  timeoutMs,
		Persistent: persistent,
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("monitor: encode details: %w", err)
	}
	text := fmt.Sprintf("Started %s (%s). Each stdout line arrives as a notice. Stop it with job_kill.", job.ID(), in.Description)
	return protocol.ToolExecutionResult{
		Content: []protocol.UserBlock{protocol.Text{Text: text}},
		Details: raw,
	}, nil
}
