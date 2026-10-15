package agent

import (
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
	"errors"
)

func (l *loop) preparationFailed(err error) error {
	entries := make([]sessions.Entry, 0, len(l.claimed))
	for _, in := range l.claimed {
		entries = append(entries, sessions.InputOutcome{InputID: in.id, Reason: "failed"})
	}
	if len(entries) > 0 {
		if writeErr := l.commit(entries...); writeErr != nil {
			err = errors.Join(err, writeErr)
		}
	}
	l.claimed = nil
	if l.onPreparationFailure != nil {
		l.onPreparationFailure()
	}
	return err
}

func (l *loop) commitStaged() error {
	if err := l.dropUnentered(l.claimed, l.stagedInputCount); err != nil {
		return err
	}
	for i, m := range l.staged {
		if err := l.emitInput(m, l.stagedIDs[i]); err != nil {
			return err
		}
		l.newMessages = append(l.newMessages, m)
	}
	l.staged = nil
	l.stagedIDs = nil
	l.claimed = nil
	return nil
}

func (l *loop) preparationAborted() error {
	msg := protocol.AssistantMessage{API: string(l.cfg.Model.API), Provider: l.cfg.Model.Provider, Model: l.cfg.Model.ID, StopReason: protocol.StopAborted, Content: []protocol.AssistantBlock{protocol.Text{Text: ""}}}
	l.newMessages = append(l.newMessages, msg)
	if err := l.emit(&protocol.MessageStart{Message: msg}); err != nil {
		return err
	}
	if err := l.emit(&protocol.MessageEnd{Message: msg}); err != nil {
		return err
	}
	if err := l.closeTurn(msg, []protocol.ToolResultMessage{}); err != nil {
		return err
	}
	l.cycle.settle(ReasonAborted)
	return l.endRun()
}
