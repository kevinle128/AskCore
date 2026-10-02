package agent

import (
	"context"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// streamAssistantResponse runs one model request and returns the final
// assistant message. The message is in the run context when it returns: the
// seed of the start event is appended and the final message replaces it.
func (l *loop) streamAssistantResponse() (protocol.AssistantMessage, error) {
	hooks := l.cfg.Hooks
	msgs := l.context().Messages
	var err error
	if hooks.TransformContext != nil {
		if msgs, err = hooks.TransformContext(l.ctx, msgs); err != nil {
			return protocol.AssistantMessage{}, err
		}
	}
	var llm []protocol.Message
	if hooks.ConvertToLLM != nil {
		if llm, err = hooks.ConvertToLLM(msgs); err != nil {
			return protocol.AssistantMessage{}, err
		}
	} else {
		llm = providers.ConvertToLLM(msgs)
	}
	req := providers.NormalizeRequest(providers.Request{Messages: llm})

	opts := l.cfg.Options
	if hooks.GetAPIKey != nil {
		key, err := hooks.GetAPIKey(l.ctx, l.cfg.Model.Provider)
		if err != nil {
			return protocol.AssistantMessage{}, err
		}
		if key != "" {
			opts.APIKey = key
		}
	}

	// The stream gets its own cancel so that an emit failure can stop the
	// producer before the channel is drained.
	ctx, cancel := context.WithCancel(l.ctx)
	defer cancel()
	stream := l.cfg.Stream(ctx, l.cfg.Model, req, opts)

	addedPartial := false
	var emitErr error
	for item := range stream.Events() {
		if emitErr != nil {
			continue
		}
		switch ev := item.Event.(type) {
		case protocol.StartEvent:
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
		return protocol.AssistantMessage{}, emitErr
	}

	// The channel is closed, so the result is settled and Result returns at once.
	final, _ := stream.Result(ctx)
	level := l.thinkingLevel()
	final.ThinkingLevel = &level
	if addedPartial {
		l.ac.Messages[len(l.ac.Messages)-1] = final
	} else {
		l.ac.Messages = append(l.ac.Messages, final)
		if err := l.emit(&protocol.MessageStart{Message: final}); err != nil {
			return protocol.AssistantMessage{}, err
		}
	}
	return final, l.emit(&protocol.MessageEnd{Message: final})
}
