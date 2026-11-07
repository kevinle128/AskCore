package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"AskCore/internal/jobs"
	"AskCore/internal/sandbox"
	"AskCore/internal/tasks"
	"AskCore/pkg/protocol"
)

const (
	// bashDefaultTimeout is the wait budget when the model gives none.
	bashDefaultTimeout = 120 * time.Second
	// bashMaxTimeout is the largest wait budget; a larger value is cut to it.
	bashMaxTimeout = 600 * time.Second
	// bashUpdateEvery is the shortest time between two partial updates.
	bashUpdateEvery = 50 * time.Millisecond

	// noticePromise tells the model that it does not need to poll.
	noticePromise = "A notice will arrive when it finishes; do not poll job_output."

	trailingAmpersandText = "Do not end the command with &. Set run_in_background to true to run it in the background."
	noOutputText          = "(no output)"
)

// leadingCd matches a "cd DIR &&" at the start of a command.
var leadingCd = regexp.MustCompile(`^\s*cd\s+("[^"]*"|'[^']*'|[^\s;&|]+)\s*&&`)

// NewShellTools returns the tools that run commands and manage the jobs of one
// session: bash, job_output, job_kill, job_list and monitor. table is the job
// table of the session and must not be nil; the tools keep no other state.
func NewShellTools(table *jobs.Table) []Tool {
	if table == nil {
		panic("tools: NewShellTools needs a job table")
	}
	return []Tool{
		&Bash{table: table},
		&JobOutput{table: table},
		&JobKill{table: table},
		&JobList{table: table},
		&Monitor{table: table},
	}
}

// Bash runs a shell command as a job of the session table. It waits for the
// end inside the wait budget and otherwise leaves the job in the background.
type Bash struct {
	table *jobs.Table
}

type bashInput struct {
	Command         string   `json:"command"`
	Timeout         *float64 `json:"timeout"`
	RunInBackground *bool    `json:"run_in_background"`
}

func (*Bash) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{
		Name: "bash",
		Description: fmt.Sprintf("Execute a bash command in the current working directory and return its stdout and stderr. "+
			"The command runs without a terminal and with a non-interactive environment: no pager, no prompt, no color. "+
			"A result starts with the exit code. If the output is longer than %d lines or %dKB, the middle is left out and "+
			"the full output is saved to a file that the result names. "+
			"The command is a job. It runs in the foreground for at most `timeout` seconds (default %d, maximum %d). "+
			"A command that runs longer moves to the background and goes on as a job; it is not stopped. "+
			"Set run_in_background to true to start a long command, such as a server or a build, in the background at once. "+
			"A notice arrives when a background job finishes, so do not poll job_output. Stop a job with job_kill. "+
			"Do not end the command with &. Background jobs end when the session closes. "+
			"In a non-interactive run the run waits for its background jobs, and a background job stops after a time limit "+
			"(by default 10 minutes); with run_in_background, `timeout` sets that limit, up to 2 hours.",
			DefaultMaxLines, DefaultMaxBytes/1024, int(bashDefaultTimeout/time.Second), int(bashMaxTimeout/time.Second)),
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"command":{"type":"string","description":"Bash command to execute"},` +
			`"timeout":{"type":"number","description":"Seconds to wait before the command moves to the background (default 120, maximum 600). With run_in_background, the seconds the job may run in the background in a non-interactive run (default 600, maximum 7200)"},` +
			`"run_in_background":{"type":"boolean","description":"Start the command as a background job and return at once"}},` +
			`"required":["command"]}`),
	}
}

func (*Bash) PromptSnippet() string { return "Execute bash commands (ls, grep, find, etc.)" }

func (*Bash) PromptGuidelines() []string {
	return []string{"Use run_in_background for servers and long builds. A notice arrives when the job ends, so do not poll job_output."}
}

func (b *Bash) Execute(ctx context.Context, tc Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	var in bashInput
	if err := json.Unmarshal(args, &in); err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("bash: %w", err)
	}
	if strings.TrimSpace(in.Command) == "" {
		return protocol.ToolExecutionResult{}, failf("The command is empty.")
	}
	if endsInBackgroundOperator(in.Command) {
		return protocol.ToolExecutionResult{}, failf("%s", trailingAmpersandText)
	}
	background := in.RunInBackground != nil && *in.RunInBackground
	var budget, limit time.Duration
	clamped := false
	var err error
	if background {
		// With run_in_background, timeout is the background limit of a session
		// that has one (ask -p); the table caps it.
		limit, err = backgroundLimit(in.Timeout)
	} else {
		budget, clamped, err = waitBudget(in.Timeout)
	}
	if err != nil {
		return protocol.ToolExecutionResult{}, err
	}

	owner, err := jobOwnerOf(tc)
	if err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	// A foreground command is waited for below; its end must not also come as a notice.
	job, err := b.table.Start(jobs.StartSpec{Kind: jobs.KindBash, Command: in.Command, Cwd: tc.Cwd, Foreground: !background, Owner: owner, BackgroundLimit: limit})
	if err != nil {
		return protocol.ToolExecutionResult{}, startFailure(err)
	}
	details := protocol.ToolResultDetails{Kind: protocol.ToolKindExecute, JobID: job.ID(), Cwd: leadingCdDir(in.Command)}
	note := ""
	if clamped {
		note = fmt.Sprintf("[timeout limited to %d s]", int(bashMaxTimeout/time.Second))
	}

	if background {
		details.Status = string(jobs.StatusRunning)
		return textResult(joinNote(fmt.Sprintf("Started background job %s. %s", job.ID(), noticePromise), note), details, nil), nil
	}

	outcome := b.await(ctx, tc, job, budget)
	out := job.Snapshot()
	view, tr := shellView(out)
	spill(&details, out)

	switch outcome {
	case jobs.Done:
		res := job.Result()
		details.ExitCode, details.Signal = res.ExitCode, res.Signal
		details.Status = string(job.Status())
		if view == "" {
			view = noOutputText
		}
		text := exitHeader(res, job.KillReason()) + "\n" + view
		result := textResult(joinNote(text, note), details, &tr)
		if res.ExitCode == nil || *res.ExitCode != 0 {
			result.IsError = ptr(true)
		}
		return result, nil
	case jobs.Background:
		details.Status = string(jobs.StatusRunning)
		text := fmt.Sprintf("Command still running after %s s. It continues as background job %s. %s Use job_kill to stop it.",
			formatSeconds(budget), job.ID(), noticePromise)
		if view != "" {
			text += "\n" + view
		}
		return textResult(joinNote(text, note), details, &tr), nil
	case jobs.Cancelled:
		details.Status = string(jobs.StatusKilled)
		text := "Command aborted"
		if view != "" {
			text += "\n" + view
		}
		result := textResult(joinNote(text, note), details, &tr)
		result.IsError = ptr(true)
		return result, nil
	}
	return protocol.ToolExecutionResult{}, failf("Job %s is no longer available.", job.ID())
}

// await waits for the job with the wait budget. While it waits, it sends the
// output so far to tc.Update, at most once for each bashUpdateEvery and only
// when the output grew. It returns after the last update call.
func (b *Bash) await(ctx context.Context, tc Context, job *jobs.Job, budget time.Duration) jobs.WaitOutcome {
	outcome := make(chan jobs.WaitOutcome, 1)
	go func() { outcome <- b.table.Wait(ctx, job.ID(), budget) }()
	if tc.Update == nil {
		return <-outcome
	}
	tick := time.NewTicker(bashUpdateEvery)
	defer tick.Stop()
	sent := int64(0)
	for {
		select {
		case o := <-outcome:
			return o
		case <-tick.C:
			out := job.Snapshot()
			if out.Total == sent {
				continue
			}
			sent = out.Total
			view, tr := shellView(out)
			if view == "" {
				continue
			}
			details := protocol.ToolResultDetails{Kind: protocol.ToolKindExecute, JobID: job.ID(), Status: string(jobs.StatusRunning)}
			spill(&details, out)
			tc.Update(textResult(view, details, &tr))
		}
	}
}

// waitBudget returns the wait budget for the timeout argument in seconds, and
// whether the argument was cut to the maximum.
func waitBudget(timeout *float64) (budget time.Duration, clamped bool, err error) {
	if timeout == nil {
		return bashDefaultTimeout, false, nil
	}
	if math.IsNaN(*timeout) || *timeout <= 0 {
		return 0, false, failf("timeout must be more than 0 seconds. It is the time to wait before the command moves to the background.")
	}
	if *timeout >= bashMaxTimeout.Seconds() {
		return bashMaxTimeout, *timeout > bashMaxTimeout.Seconds(), nil
	}
	return max(time.Duration(*timeout*float64(time.Second)), time.Millisecond), false, nil
}

// backgroundLimit returns the background limit that timeout asks for, zero
// when it asks for none.
func backgroundLimit(timeout *float64) (time.Duration, error) {
	if timeout == nil {
		return 0, nil
	}
	if math.IsNaN(*timeout) || *timeout <= 0 {
		return 0, failf("timeout must be more than 0 seconds. With run_in_background it is the time the job may run in the background.")
	}
	// The job table caps the limit at its maximum; clamp here only so the
	// conversion cannot overflow.
	if *timeout >= float64(math.MaxInt64)/float64(time.Second) {
		return time.Duration(math.MaxInt64), nil
	}
	return max(time.Duration(*timeout*float64(time.Second)), time.Millisecond), nil
}

// startFailure turns an error of the job table into text for the model.
func startFailure(err error) error {
	switch {
	case errors.Is(err, jobs.ErrTooManyJobs):
		return failf("%s", err)
	case errors.Is(err, jobs.ErrClosed):
		return failf("Cannot start the command: the session is closing.")
	case errors.Is(err, jobs.ErrBackgroundDisabled):
		return failf("Background jobs are turned off in this session. Run the command in the foreground.")
	}
	return failf("Cannot start the command: %v", err)
}

// exitHeader is the first line of a finished command.
func exitHeader(res sandbox.Result, killReason string) string {
	var h string
	switch {
	case res.ExitCode != nil:
		h = fmt.Sprintf("exit: %d", *res.ExitCode)
	case res.Signal != "":
		h = "signal: " + res.Signal
	case res.Err != nil:
		h = "error: " + res.Err.Error()
	default:
		h = "exit: unknown"
	}
	if killReason != "" {
		h += " (" + killReason + ")"
	}
	return h
}

// shellView is the text that the model reads for the output of a command.
// Trailing line ends are cut; the spill file lines stay.
func shellView(out sandbox.Output) (string, sandbox.Truncation) {
	text, tr := sandbox.View(out, 0, 0)
	return strings.TrimRight(text, "\n"), tr
}

// spill copies the spill file facts of out into details.
func spill(d *protocol.ToolResultDetails, out sandbox.Output) {
	d.SpillPath, d.SpillCapped = out.SpillPath, out.SpillCapped
}

// toolTruncation converts the view truncation to the form of the details. The
// runner does not count the lines it drops, so TotalLines stays zero.
func toolTruncation(tr sandbox.Truncation) *protocol.ToolTruncation {
	if !tr.Truncated {
		return nil
	}
	by := "bytes"
	if tr.ShownLines >= sandbox.DefaultMaxLines {
		by = "lines"
	}
	return &protocol.ToolTruncation{
		TruncatedBy: by,
		TotalBytes:  int(tr.TotalBytes),
		OutputLines: tr.ShownLines,
		OutputBytes: tr.ShownBytes,
	}
}

// textResult returns one text block with its details. tr may be nil.
func textResult(text string, details protocol.ToolResultDetails, tr *sandbox.Truncation) protocol.ToolExecutionResult {
	if tr != nil {
		details.Truncation = toolTruncation(*tr)
	}
	return protocol.ToolExecutionResult{
		Content: []protocol.UserBlock{protocol.Text{Text: text}},
		Details: mustDetails(details),
	}
}

func joinNote(text, note string) string {
	if note == "" {
		return text
	}
	return text + "\n" + note
}

func ptr[T any](v T) *T { return &v }

// formatSeconds writes a duration as a number of seconds without a trailing
// zero fraction.
func formatSeconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', -1, 64)
}

// leadingCdDir returns the directory that a leading "cd DIR &&" of command
// names, or "".
func leadingCdDir(command string) string {
	m := leadingCd.FindStringSubmatch(command)
	if m == nil {
		return ""
	}
	return strings.Trim(m[1], `"'`)
}

// endsInBackgroundOperator reports whether command ends with a single
// unquoted "&", which would start the last command in the background of the
// shell and leave the job without output and exit code. "&&", "2>&1" and a "&"
// inside quotes or after "#" do not count.
func endsInBackgroundOperator(command string) bool {
	var (
		prev, last         byte // the last two characters of the code; 'q' stands for a quoted one
		inSingle, inDouble bool
		escaped, inComment bool
		startOfWord        = true
	)
	push := func(c byte) { prev, last = last, c }
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case inComment:
			if c == '\n' {
				inComment, startOfWord = false, true
			}
		case escaped:
			escaped = false
			push('q')
		case inSingle:
			inSingle = c != '\''
			push('q')
		case inDouble:
			switch c {
			case '"':
				inDouble = false
			case '\\':
				escaped = true
			}
			push('q')
		case c == '\\':
			escaped = true
			push('q')
		case c == '\'':
			inSingle, startOfWord = true, false
			push('q')
		case c == '"':
			inDouble, startOfWord = true, false
			push('q')
		case c == '#' && startOfWord:
			inComment = true
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			startOfWord = true
		default:
			startOfWord = c == ';' || c == '&' || c == '|' || c == '(' || c == ')'
			push(c)
		}
	}
	return last == '&' && prev != '&' && prev != '>' && prev != '<' && prev != '|'
}

// errNoConversation refuses a job tool that runs outside an Agent: a job needs
// a conversation for its notices and its visibility.
var errNoConversation = errors.New("this tool needs an agent conversation; it has none")

// jobOwner is the conversation of a tool call as the job table sees it: the
// notices of the job wake that conversation at its next turn boundary.
type jobOwner struct{ port tasks.Port }

func (o jobOwner) Gen() uint64 { return o.port.Gen() }

func (o jobOwner) Notify(msg protocol.Message) (string, error) {
	return o.port.Notify(tasks.NoticeWake, msg)
}

func (o jobOwner) Remove(id string) bool { return o.port.Remove(id) }

// jobOwnerOf returns the owner of the jobs that the call starts or sees. It
// never falls back to a default generation.
func jobOwnerOf(tc Context) (jobOwner, error) {
	if tc.Notices == nil {
		return jobOwner{}, errNoConversation
	}
	return jobOwner{port: tc.Notices}, nil
}
