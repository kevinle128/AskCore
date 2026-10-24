package tasks

import (
	"context"
	"errors"
	"time"

	"AskCore/pkg/protocol"
)

// Kind names a family of task. The set is closed: Start rejects other values,
// so a kind is safe to use in a file name.
type Kind string

// The task kinds. A timer has no body and no output file. Every other kind
// has a body and an output sink.
const (
	KindShell    Kind = "shell"
	KindSubagent Kind = "subagent"
	KindTimer    Kind = "timer"
	KindWatch    Kind = "watch"
	KindRemote   Kind = "remote"
)

func (k Kind) valid() bool {
	switch k {
	case KindShell, KindSubagent, KindTimer, KindWatch, KindRemote:
		return true
	}
	return false
}

// Status is the state of a task. A task starts as StatusRunning and ends in
// exactly one of the other four states.
type Status string

// The task states.
const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusStopped   Status = "stopped"
	StatusTimedOut  Status = "timed_out"
)

// NoticePolicy tells the Agent what to do with the notice of a task.
type NoticePolicy int

// The notice policies. The zero value is NoticeWake.
const (
	// NoticeWake gives the notice to the model and starts a run if the Agent
	// is idle.
	NoticeWake NoticePolicy = iota
	// NoticeContext adds the notice to the context of the next input. It does
	// not start a run.
	NoticeContext
	// NoticeSilent sends events only. The registry does not call Deliver.
	NoticeSilent
)

// NoticeState is the fate of the notice of one task, as the registry saw it.
type NoticeState string

// The notice states. The registry cannot see what the Agent does with a
// notice after Deliver returns.
const (
	NoticePending   NoticeState = "pending"   // the task has not ended yet, or Deliver is running
	NoticeDelivered NoticeState = "delivered" // Deliver accepted the notice
	NoticeSkipped   NoticeState = "silent"    // the policy was NoticeSilent
	NoticeDiscarded NoticeState = "discarded" // Deliver returned an error that wraps ErrNotDelivered
	NoticeFailed    NoticeState = "failed"    // Deliver returned any other error
)

// Body is the work of a task. It must return when ctx is done. Output goes to
// out. The registry recovers a panic of the body and records the task as
// failed.
type Body func(ctx context.Context, out *Sink) Result

// Spec describes one task for Start.
type Spec struct {
	Kind  Kind
	Title string // short text for the UI and the notice
	// After is the delay of a timer task. Other kinds ignore it.
	After time.Duration
	// Deadline is the time limit of a body. Zero means none. When it expires
	// the task ends as timed_out. A timer ignores it.
	Deadline time.Duration
	Notice   NoticePolicy
	Body     Body // nil for the timer kind, required for every other kind
}

// Result is what a body returns.
type Result struct {
	ExitCode *int
	Summary  string // one line for the notice
	Err      error
}

// Notice is the single hand-off of a task to the Agent.
type Notice struct {
	Origin  string // "task"; a source of another kind uses its own name
	TaskID  string // empty for a source
	Gen     uint64 // generation of the registry that made the notice
	Policy  NoticePolicy
	Message protocol.Message // a protocol.CustomMessage
}

// Handle identifies a started task.
type Handle struct {
	ID string
	// Done closes after the notice hand-off finished.
	Done <-chan struct{}
}

// Info is a copy of the state of one task.
type Info struct {
	ID         string
	Kind       Kind
	Title      string
	Status     Status
	Policy     NoticePolicy
	StartedAt  time.Time
	EndedAt    time.Time // zero while the task runs
	ExitCode   *int
	Summary    string
	Error      string // text of the body error or the panic value
	OutputFile string // empty when the task wrote no file
	// OutputError is the first error of the output file, if any.
	OutputError string
	// Cause is the reason of a stop or a deadline. It is nil for a task that
	// ended on its own.
	Cause       error
	Notice      NoticeState
	InputID     string // the ID that Deliver returned
	NoticeError string // text of the error that Deliver returned
}

// Control is the part of the registry that tools see.
type Control interface {
	Start(spec Spec) (Handle, error)
	Stop(id string) error
	List() []Info
}

var (
	// ErrClosed means that the registry stopped admitting tasks.
	ErrClosed = errors.New("tasks: registry is closed")
	// ErrNoTaskDir means that a task needs an output file but the registry has
	// no directory for it.
	ErrNoTaskDir = errors.New("tasks: no task directory configured")
	// ErrInvalidSpec means that Start refused the spec or the task ID.
	ErrInvalidSpec = errors.New("tasks: invalid spec")
	// ErrUnknownTask means that no task has the given ID.
	ErrUnknownTask = errors.New("tasks: unknown task")
	// ErrNotDelivered is the contract for a refused notice. Deliver wraps it
	// (for example with fmt.Errorf and %w) when the Agent is disposed or the
	// notice is of an old generation. The registry marks such a notice as
	// discarded. Any other Deliver error is recorded as a failure.
	ErrNotDelivered = errors.New("tasks: notice not delivered")
	// ErrStopped is the cause that Stop gives to a task.
	ErrStopped = errors.New("tasks: task stopped")
	// ErrDeadline is the cause of a task that reached its deadline.
	ErrDeadline = errors.New("tasks: task deadline exceeded")
)
