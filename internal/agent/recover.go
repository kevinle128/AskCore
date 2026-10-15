package agent

import (
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
	"errors"
	"time"
)

func (l *loop) streamAssistantResponse() (msg protocol.AssistantMessage, err error) {
	l.retryNumber = 0
	l.retryBudgets = map[string]retryBudget{}
	l.retry = nil
	l.binding = nil
	defer func() {
		if closeErr := l.completeRetry(msg, err); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	for {
		l.retryPending = false
		err = runGuarded(func() error { var attemptErr error; msg, attemptErr = l.streamAttempt(); return attemptErr })
		if err != nil || !l.retryPending {
			return msg, err
		}
		scheduled := *l.retry
		err = l.wait(time.Duration(scheduled.DelayMs) * time.Millisecond)
		if err != nil || l.ctx.Err() != nil {
			if l.ctx.Err() == nil {
				return msg, err
			}
			msg.StopReason = protocol.StopAborted
			msg.Content = nil
			if err = l.emit(&protocol.MessageStart{Message: msg}); err != nil {
				return msg, err
			}
			return l.finishAttempt(msg)
		}
		started := sessions.RetryStarted{RetryID: scheduled.RetryID, Retry: scheduled.Retry}
		if err = l.commit(started); err != nil {
			return msg, err
		}
		if err = l.retryEvents(started); err != nil {
			return msg, err
		}
		l.ac.Messages = projectHistory(l.header, l.log.Messages())
		if err = runGuarded(l.prepareRequest); err != nil {
			return msg, l.preparationFailed(err)
		}
	}
}

func (l *loop) settleAndRecover(msg protocol.AssistantMessage, cause error, provider string, policy providers.RetryPolicy) (protocol.AssistantMessage, error) {
	budget := l.retryBudgets[provider]
	if budget.key != policy.Key {
		budget = retryBudget{key: policy.Key}
	}
	l.retryNumber = budget.count
	var action pipeline.RecoverAction
	if msg.StopReason == protocol.StopError && l.attempt != nil && l.ctx.Err() == nil {
		var err error
		action, err = l.cfg.Pipeline.RecoverModel(l.ctx, pipeline.RecoverInput{Failure: cause, Provider: provider, Policy: policy, Retry: l.retryNumber + 1})
		if err != nil {
			if settleErr := l.closeAttempt(msg, cause); settleErr != nil {
				return msg, settleErr
			}
			return msg, err
		}
	}
	if l.ctx.Err() != nil {
		action.Retry = false
		msg.StopReason = protocol.StopAborted
		msg.Content = interruptedContent(msg.Content)
	}
	if action.Retry {
		if err := l.emit(&protocol.MessageEnd{Message: msg}); err != nil {
			return msg, err
		}
		if err := l.closeAttempt(msg, cause); err != nil {
			return msg, err
		}
		l.retryNumber++
		l.retryBudgets[provider] = retryBudget{key: policy.Key, count: l.retryNumber}
		scheduled := sessions.RetryScheduled{RetryID: newRunID(), CycleID: l.cycleID(), Turn: l.cycle.turns, Provider: provider, PolicyKey: policy.Key, Retry: l.retryNumber, MaxRetries: policy.MaxRetries, DelayMs: action.Delay.Milliseconds(), Failure: sessions.Failure{Code: providers.CodeOf(cause), Text: failureTextOf(msg.ErrorMessage)}}
		if err := l.commit(scheduled); err != nil {
			return msg, err
		}
		l.retryMessage = msg
		l.retry = &scheduled
		l.retryPending = true
		return msg, l.retryEvents(scheduled)
	}
	var err error
	msg, err = l.finishAttempt(msg)
	if err != nil {
		return msg, err
	}
	if err := l.closeAttempt(msg, cause); err != nil {
		return msg, err
	}
	return msg, nil
}

func (l *loop) wait(d time.Duration) error {
	if l.ctx.Err() != nil {
		return l.ctx.Err()
	}
	if l.cfg.Wait != nil {
		return l.cfg.Wait(l.ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-l.ctx.Done():
		return l.ctx.Err()
	case <-timer.C:
		return nil
	}
}

type retryBudget struct {
	key   string
	count int
}
