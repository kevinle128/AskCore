package agent

import (
	"context"
	"errors"
	"strings"

	"AskCore/internal/providers"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

// CycleReason says why an input cycle ended. It is the reason of cycle_end.
type CycleReason string

// The reasons of an input cycle.
const (
	// ReasonCompleted: the model finished and nothing more was queued.
	ReasonCompleted CycleReason = "completed"
	// ReasonBlocked: a control point rejected the input.
	ReasonBlocked CycleReason = "blocked"
	// ReasonMaxTokens: a model answer hit the output token limit. The reason
	// is sticky: a later turn of the same cycle does not replace it with
	// ReasonCompleted.
	ReasonMaxTokens CycleReason = "max-tokens"
	// ReasonAborted: the run was cancelled.
	ReasonAborted CycleReason = "aborted"
	// ReasonError: the run failed, or a model answer ended in an error.
	ReasonError CycleReason = "error"
	// ReasonContinuationLimit: the cycle stopped after too many empty
	// continuations.
	ReasonContinuationLimit CycleReason = "continuation-limit"
)

// The outcomes of a model request, as attempt_end reports them.
const (
	attemptCompleted = "completed"
	attemptFailed    = "failed"
	attemptAborted   = "aborted"
)

// cycleState is one input cycle: the turns that one batch of input causes. A
// cycle opens when a run starts or when queued follow-up input starts, and it
// closes when the model stops or the run ends.
type cycleState struct {
	id     string
	reason CycleReason
	// code is the failure code of the turn that set reason error. It is empty
	// for any other reason.
	code string
	// turns counts the turns that opened in the cycle.
	turns int
}

// settle records how a turn ended. Every reason replaces the earlier one,
// except that a completed turn never replaces max-tokens.
func (c *cycleState) settle(r CycleReason) {
	if c == nil { // a loop that runs outside Run or Continue has no cycle
		return
	}
	if c.reason == ReasonMaxTokens && r == ReasonCompleted {
		return
	}
	c.reason = r
}

// settleTurn records how a turn ended: the reason that its stop reason maps to,
// and for an error the failure code of the model answer.
func (c *cycleState) settleTurn(stop protocol.StopReason, code string) {
	if c == nil {
		return
	}
	reason := reasonOf(stop)
	c.settle(reason)
	if c.reason == ReasonError {
		c.code = code
	} else {
		c.code = ""
	}
}

// attemptState is one model request. binding is the credential binding that
// the stream reported.
type attemptState struct {
	id      string
	binding providers.AuthBinding
}

// openCycle starts a new cycle and publishes cycle_start. It comes before the
// turn_start of the cycle.
func (l *loop) openCycle() error {
	l.cycle = &cycleState{id: newRunID()}
	if err := l.commit(sessions.CycleOpened{CycleID: l.cycle.id}); err != nil {
		return err
	}
	return l.emit(&protocol.CycleStart{CycleID: l.cycle.id})
}

// openTurn commits and publishes the start of a turn of the open cycle.
func (l *loop) openTurn() error {
	if l.cycle != nil {
		l.cycle.turns++
	}
	if err := l.commit(sessions.TurnOpened{CycleID: l.cycleID()}); err != nil {
		return err
	}
	return l.emit(&protocol.TurnStart{CycleID: l.cycleID()})
}

// closeTurn commits and publishes the end of a turn.
func (l *loop) closeTurn(msg protocol.AssistantMessage, results []protocol.ToolResultMessage) error {
	if err := l.commit(sessions.TurnClosed{CycleID: l.cycleID()}); err != nil {
		return err
	}
	return l.emit(&protocol.TurnEnd{CycleID: l.cycleID(), Message: msg, ToolResults: results})
}

// closeCycle publishes cycle_end for the open cycle. A cycle that was not
// settled by a turn ends as completed.
func (l *loop) closeCycle() error {
	c := l.cycle
	if c == nil {
		return nil
	}
	l.cycle = nil
	if c.reason == "" {
		c.reason = ReasonCompleted
	}
	end := &protocol.CycleEnd{CycleID: c.id, Reason: string(c.reason)}
	if c.reason == ReasonAborted {
		end.Cause = abortCause(l.ctx)
	}
	if c.reason == ReasonError {
		end.Code = c.code
	}
	if err := l.commit(sessions.CycleClosed{CycleID: c.id, Reason: end.Reason, Cause: end.Cause, Code: end.Code}); err != nil {
		return err
	}
	return l.emit(end)
}

// cycleID is the ID of the open cycle, or empty when none is open.
func (l *loop) cycleID() string {
	if l.cycle == nil {
		return ""
	}
	return l.cycle.id
}

// openAttempt publishes attempt_start for a model request whose stream exists.
// id is the ID that the request log of the attempt already carries. The loop
// leaves Number empty: the Agent counts the attempts of its session and fills
// it in, as it fills the envelope.
func (l *loop) openAttempt(id string, binding providers.AuthBinding) error {
	l.attempt = &attemptState{id: id, binding: binding}
	return l.emit(&protocol.AttemptStart{AttemptID: l.attempt.id, CycleID: l.cycleID()})
}

// closeAttempt commits the settlement of the open attempt, if there is one,
// and then publishes attempt_end. final is the message that the attempt made
// and cause the error of its stream, which carries the failure code.
func (l *loop) closeAttempt(final protocol.AssistantMessage, cause error) error {
	a := l.attempt
	if a == nil {
		return nil
	}
	l.attempt = nil
	settled := sessions.AttemptSettled{AttemptID: a.id, Outcome: attemptOutcomeOf(final.StopReason)}
	if usageReported(final.Usage) {
		usage := final.Usage.Clone()
		settled.Usage = &usage
	}
	if final.StopReason == protocol.StopError {
		settled.Failure = &sessions.Failure{Code: providers.CodeOf(cause), Text: failureTextOf(final.ErrorMessage)}
	}
	if b := a.binding; b != (providers.AuthBinding{}) {
		settled.Binding = &sessions.AuthBinding{Provider: b.Provider, Method: b.Method, Profile: b.Profile, BillingHint: b.BillingHint}
	}
	if err := l.commit(settled); err != nil {
		return err
	}
	l.lastSettlement = settled
	return l.emit(&protocol.AttemptEnd{AttemptID: a.id, Outcome: settled.Outcome})
}

// usageReported tells a usage that the provider sent from an empty one. A
// stream that sent no usage leaves the zero value, which is unknown and not a
// count of zero tokens.
func usageReported(u protocol.Usage) bool {
	return u.Input != 0 || u.Output != 0 || u.CacheRead != 0 || u.CacheWrite != 0 || u.TotalTokens != 0 ||
		u.CacheWrite1h != nil || u.Reasoning != nil || u.Cost != protocol.Cost{}
}

// failureTextOf cleans the error text of a message, which may be nil.
func failureTextOf(text *string) string {
	if text == nil {
		return ""
	}
	return failureText(*text)
}

// attemptOutcomeOf maps the end of a model answer to the outcome of its
// attempt: a call that returned an answer, even a cut-off one, completed.
func attemptOutcomeOf(stop protocol.StopReason) string {
	switch stop {
	case protocol.StopError:
		return attemptFailed
	case protocol.StopAborted:
		return attemptAborted
	}
	return attemptCompleted
}

// reasonOf maps the end of a model answer to a cycle reason.
func reasonOf(stop protocol.StopReason) CycleReason {
	switch stop {
	case protocol.StopError:
		return ReasonError
	case protocol.StopAborted:
		return ReasonAborted
	case protocol.StopLength:
		return ReasonMaxTokens
	}
	return ReasonCompleted
}

// The fixed words of cycle_end.cause. The wire never carries the text of a
// cancel cause, which can name internal state.
const (
	causeUser     = "user"
	causeDeadline = "deadline"
	causeCanceled = "canceled"
	causeOutput   = "output"
	// causeDisposed names the cancel of Agent.Dispose.
	causeDisposed = "disposed"
)

// abortCause names who cancelled ctx: "user" for Agent.Abort, "output" for
// ErrOutputFailure, "disposed" for Agent.Dispose, "deadline" when a deadline
// passed, else "canceled". The first cancel wins, as context.Cause does.
func abortCause(ctx context.Context) string {
	cause := context.Cause(ctx)
	switch {
	case cause == nil:
		return ""
	case errors.Is(cause, errUserAbort):
		return causeUser
	case errors.Is(cause, ErrOutputFailure):
		return causeOutput
	case errors.Is(cause, errDisposeAbort):
		return causeDisposed
	case errors.Is(cause, context.DeadlineExceeded):
		return causeDeadline
	}
	return causeCanceled
}

// withoutToolCalls returns content without its tool-call blocks. It never
// writes into content, which the stream result may share.
func withoutToolCalls(content []protocol.AssistantBlock) []protocol.AssistantBlock {
	out := make([]protocol.AssistantBlock, 0, len(content))
	for _, b := range content {
		if _, ok := b.(protocol.ToolCall); !ok {
			out = append(out, b)
		}
	}
	return out
}

// interruptedContent keeps only meaningful text and thinking from an abort.
func interruptedContent(content []protocol.AssistantBlock) []protocol.AssistantBlock {
	out := make([]protocol.AssistantBlock, 0, len(content))
	for _, b := range content {
		switch v := b.(type) {
		case protocol.Text:
			if strings.TrimSpace(v.Text) != "" {
				out = append(out, v)
			}
		case protocol.Thinking:
			if strings.TrimSpace(v.Thinking) != "" {
				out = append(out, v)
			}
		}
	}
	return out
}
