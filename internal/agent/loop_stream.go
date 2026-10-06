package agent

import (
	"context"

	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

// streamAttempt prepares, dispatches and settles one complete model attempt.
func (l *loop) streamAttempt() (protocol.AssistantMessage, error) {
	msgs := l.requestMessages
	l.requestMessages = nil
	if msgs == nil {
		msgs = l.context().Messages
	}
	var llm []protocol.Message
	err := runGuarded(func() error { var convertErr error; llm, convertErr = l.convertToLLM(msgs); return convertErr })
	if err != nil {
		return protocol.AssistantMessage{}, l.preparationFailed(err)
	}
	req := providers.NormalizeRequest(providers.Request{Messages: llm})

	opts := l.cfg.Options
	opts.RequireBinding = l.binding

	// The request is frozen here. The stream call and the request log use this
	// copy, so no later edit of the history or of the request by a handler
	// changes what the log shows for the attempt.
	req.Messages = freezeMessages(req.Messages)
	// The adapter computes its effective values once, here. The log keeps the
	// result and the stream sends exactly those values, so the agent never
	// repeats a calculation of the adapter.
	var prepared *providers.Prepared
	if l.cfg.Prepare != nil {
		err = runGuarded(func() error {
			var prepErr error
			prepared, prepErr = l.cfg.Prepare(l.cfg.Model, req, opts)
			return prepErr
		})
		if err != nil {
			return protocol.AssistantMessage{}, l.preparationFailed(err)
		}
		opts.Prepared = prepared
	}
	if l.ctx.Err() != nil {
		return protocol.AssistantMessage{}, l.preparationFailed(l.ctx.Err())
	}
	if err := l.commitStaged(); err != nil {
		return protocol.AssistantMessage{}, err
	}
	key, err := l.apiKey()
	if err != nil {
		return protocol.AssistantMessage{}, err
	}
	opts.APIKey = key
	attemptID := newRunID()
	if err := l.logRequest(attemptID, req, l.cfg.Model, opts, prepared); err != nil {
		return protocol.AssistantMessage{}, err
	}

	policy := providers.DefaultRetryPolicy()
	provider := l.cfg.Model.Provider
	if prepared != nil {
		policy = prepared.RetryPolicy
		provider = prepared.Provider
	}
	ctx, cancel := context.WithCancel(l.ctx)
	defer cancel()
	var opened *providers.Stream
	var final protocol.AssistantMessage
	var resultErr error
	call := pipeline.ModelCall{Model: l.cfg.Model, Request: providers.TranscriptRequest{Messages: freezeMessages(req.Messages)}}
	outcome, err := l.cfg.Pipeline.ExecuteModel(ctx, call, func(nextCtx context.Context, _ pipeline.ModelCall) (*pipeline.ModelOutcome, error) {
		attemptCtx, attemptCancel := context.WithCancel(nextCtx)
		stop := context.AfterFunc(ctx, attemptCancel)
		defer stop()
		defer attemptCancel()
		if attemptCtx.Err() != nil {
			return nil, attemptCtx.Err()
		}
		if l.ctx.Err() != nil {
			return nil, l.ctx.Err()
		}
		streamOpts := opts
		if prepared != nil && prepared.PolicyOnly {
			streamOpts.Prepared = nil
		}
		opened = l.cfg.Stream(attemptCtx, l.cfg.Model, req, streamOpts)
		var consumeErr error
		final, resultErr, consumeErr = l.consumeAttempt(attemptCtx, attemptID, opened, attemptCancel)
		if consumeErr != nil {
			return nil, consumeErr
		}
		if attemptCtx.Err() != nil && l.ctx.Err() == nil {
			return nil, attemptCtx.Err()
		}
		return &pipeline.ModelOutcome{Message: final, Failure: resultErr, Binding: opened.Binding()}, nil
	})
	if err != nil {
		return protocol.AssistantMessage{}, err
	}
	final = l.normalizeOutcome(outcome.Message.Clone(), outcome.Failure)
	if opened == nil {
		if err := l.emit(&protocol.MessageStart{Message: final}); err != nil {
			return protocol.AssistantMessage{}, err
		}
	}
	resultErr = outcome.Failure
	if l.binding == nil {
		b := outcome.Binding
		l.binding = &b
	}
	return l.settleAndRecover(final, resultErr, provider, policy)
}

func (l *loop) consumeAttempt(ctx context.Context, attemptID string, stream *providers.Stream, cancel context.CancelFunc) (protocol.AssistantMessage, error, error) {
	// The attempt is live only when the stream exists and the run is not
	// cancelled. A cancel between the two skips the attempt events; the loop
	// below still drains the stream, which ends at once, and settles it.
	if l.ctx.Err() == nil && ctx.Err() == nil {
		if err := l.openAttempt(attemptID, stream.Binding()); err != nil {
			cancel()
			drain(stream)
			return protocol.AssistantMessage{}, nil, err
		}
	}

	addedPartial := false
	var emitErr error
	for item := range stream.Events() {
		if emitErr != nil {
			continue
		}
		switch ev := item.Event.(type) {
		case protocol.StartEvent:
			ev.Message = cleanAssistantError(ev.Message)
			l.ac.Messages = append(l.ac.Messages, ev.Message)
			addedPartial = true
			emitErr = l.emit(&protocol.MessageStart{Message: ev.Message})
		case protocol.BlockEvent:
			// Block events before start have no message to belong to.
			if addedPartial {
				emitErr = l.emit(&protocol.MessageUpdate{AssistantMessageEvent: ev, Usage: item.Usage})
			}
		}
		if emitErr != nil {
			cancel()
		}
	}
	if emitErr != nil {
		return protocol.AssistantMessage{}, nil, emitErr
	}

	// The channel is closed, so the result is settled and Result returns at once.
	final, resultErr := stream.Result(ctx)
	final = cleanAssistantError(final)
	if addedPartial {
		l.ac.Messages = l.ac.Messages[:len(l.ac.Messages)-1]
	}
	if !addedPartial {
		if err := l.emit(&protocol.MessageStart{Message: final}); err != nil {
			return protocol.AssistantMessage{}, nil, err
		}
	}
	return final, resultErr, nil
}

func (l *loop) finishAttempt(final protocol.AssistantMessage) (protocol.AssistantMessage, error) {
	if final.StopReason == protocol.StopError {
		final.Content = []protocol.AssistantBlock{protocol.Text{Text: ""}}
	}
	l.ac.Messages = append(l.ac.Messages, final)
	if err := l.commit(sessions.MessageEntry{Message: final}); err != nil {
		return protocol.AssistantMessage{}, err
	}
	if err := l.emit(&protocol.MessageEnd{Message: final}); err != nil {
		return protocol.AssistantMessage{}, err
	}
	return final, nil
}

// logRequest commits the RequestDelta of an attempt before its stream call. The
// baseline is the header of the run followed by the committed messages; what a
// handler or a custom conversion changed is the delta.
func (l *loop) logRequest(attemptID string, req providers.TranscriptRequest, model providers.Model, opts providers.StreamOptions, prepared *providers.Prepared) error {
	baseline := projectHistory(l.header, l.log.Messages())
	return l.commit(newRequestDelta(attemptID, baseline, req.Messages, model, opts, prepared))
}

// drain reads a stream to its end so that its producer goroutine can exit.
func drain(s *providers.Stream) {
	for range s.Events() {
	}
}

// convertToLLM and apiKey are the only readers of their config functions.
// Each keeps the default of a nil function.
func (l *loop) convertToLLM(msgs []protocol.Message) ([]protocol.Message, error) {
	if l.cfg.ConvertToLLM == nil {
		return providers.ConvertToLLM(msgs), nil
	}
	return l.cfg.ConvertToLLM(msgs)
}

// apiKey returns the key for the next request. BoundKey pins --api-key
// to one provider; an empty BoundKey keeps the Options.APIKey fallback.
func (l *loop) apiKey() (string, error) {
	return providers.ResolveKey(l.ctx, l.cfg.Model.Provider, l.cfg.BoundKey, l.cfg.GetAPIKey, l.cfg.Options.APIKey)
}

// normalizeOutcome applies stop rules to the answer accepted after middleware.
func (l *loop) normalizeOutcome(final protocol.AssistantMessage, cause error) protocol.AssistantMessage {
	l.failureCode = ""
	if l.ctx.Err() != nil {
		final.StopReason = protocol.StopAborted
	}
	if final.StopReason == protocol.StopError {
		l.failureCode = providers.CodeOf(cause)
	}
	level := l.thinkingLevel()
	final.ThinkingLevel = &level
	final = cleanAssistantError(final)
	switch final.StopReason {
	case protocol.StopLength:
		final.Content = withoutToolCalls(final.Content)
	case protocol.StopAborted:
		final.Content = interruptedContent(final.Content)
	}
	return final
}

// cleanAssistantError cleans diagnostics without changing model content.
func cleanAssistantError(message protocol.AssistantMessage) protocol.AssistantMessage {
	if message.ErrorMessage != nil {
		clean := failureText(*message.ErrorMessage)
		message.ErrorMessage = &clean
	}
	return message
}
