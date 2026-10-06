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
	msgs, err := l.transformContext(l.context().Messages)
	if err != nil {
		return protocol.AssistantMessage{}, err
	}
	llm, err := l.convertToLLM(msgs)
	if err != nil {
		return protocol.AssistantMessage{}, err
	}
	req := providers.NormalizeRequest(providers.Request{Messages: llm})

	opts := l.cfg.Options
	key, err := l.apiKey()
	if err != nil {
		return protocol.AssistantMessage{}, err
	}
	opts.APIKey = key

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

// transformContext, convertToLLM and apiKey are the only readers of their
// hooks. Each keeps the default of a nil hook.
func (l *loop) transformContext(msgs []protocol.Message) ([]protocol.Message, error) {
	hook := l.cfg.Hooks.TransformContext
	if hook == nil {
		return msgs, nil
	}
	return hook(l.ctx, msgs)
}

func (l *loop) convertToLLM(msgs []protocol.Message) ([]protocol.Message, error) {
	hook := l.cfg.Hooks.ConvertToLLM
	if hook == nil {
		return providers.ConvertToLLM(msgs), nil
	}
	return hook(msgs)
}

// apiKey returns the key for the next request. BoundKey pins --api-key
// to one provider; an empty BoundKey keeps the Options.APIKey fallback.
func (l *loop) apiKey() (string, error) {
	return providers.ResolveKey(l.ctx, l.cfg.Model.Provider, l.cfg.BoundKey, l.cfg.Hooks.GetAPIKey, l.cfg.Options.APIKey)
}
