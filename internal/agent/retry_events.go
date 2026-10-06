package agent

import (
	"AskCore/internal/providers"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

// retryEvents is the wire projection of durable retry records.
func (l *loop) retryEvents(entry sessions.Entry) error {
	var events []protocol.Event
	switch e := entry.(type) {
	case sessions.RetryScheduled:
		events = []protocol.Event{&protocol.TurnEnd{CycleID: e.CycleID, Message: l.retryMessage.Clone(), ToolResults: []protocol.ToolResultMessage{}}, &protocol.AgentEnd{Messages: []protocol.Message{l.retryMessage.Clone()}, WillRetry: true}, &protocol.AutoRetryStart{Attempt: e.Retry, MaxAttempts: e.MaxRetries, DelayMs: e.DelayMs, ErrorMessage: e.Failure.Text}}
	case sessions.RetryStarted:
		events = []protocol.Event{&protocol.AgentStart{}, &protocol.TurnStart{CycleID: l.cycleID()}}
	case sessions.AttemptSettled:
		ev := &protocol.AutoRetryEnd{Success: e.Outcome == attemptCompleted, Attempt: l.retry.Retry}
		if e.Failure != nil {
			ev.FinalError = e.Failure.Text
		}
		events = []protocol.Event{ev}
	}
	for _, ev := range events {
		if err := l.emit(ev); err != nil {
			return err
		}
	}
	return nil
}

// completeRetry closes every series from the current accepted answer or error.
func (l *loop) completeRetry(msg protocol.AssistantMessage, cause error) error {
	if l.retry == nil {
		return nil
	}
	settled := l.lastSettlement
	settled.Outcome = attemptOutcomeOf(msg.StopReason)
	settled.Failure = nil
	if cause != nil {
		settled.Outcome = attemptFailed
		settled.Failure = &sessions.Failure{Code: providers.CodeOf(cause), Text: providers.CleanDiagnostic(cause)}
	} else if msg.StopReason == protocol.StopError || msg.StopReason == protocol.StopAborted {
		settled.Failure = &sessions.Failure{Text: failureTextOf(msg.ErrorMessage)}
	}
	err := l.retryEvents(settled)
	l.retry = nil
	return err
}
