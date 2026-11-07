package agent

import (
	"slices"

	"AskCore/pkg/protocol"
)

// rewriteInputs applies Config.RewriteInput to each message of a human input.
func (a *Agent) rewriteInputs(msgs []protocol.Message) []protocol.Message {
	if a.cfg.RewriteInput == nil {
		return msgs
	}
	out := make([]protocol.Message, len(msgs))
	for i, m := range msgs {
		out[i] = a.rewriteInput(m)
	}
	return out
}

// rewriteInput hands the text of the first block of a user message to
// Config.RewriteInput. A message that starts with an image, or with another
// kind of block, is left as it is. The caller's message is never changed.
func (a *Agent) rewriteInput(msg protocol.Message) protocol.Message {
	if a.cfg.RewriteInput == nil {
		return msg
	}
	var user protocol.UserMessage
	switch m := msg.(type) {
	case protocol.UserMessage:
		user = m
	case *protocol.UserMessage:
		if m == nil {
			return msg
		}
		user = *m
	default:
		return msg
	}
	if len(user.Content) == 0 {
		return msg
	}
	first, ok := user.Content[0].(protocol.Text)
	if !ok {
		return msg
	}
	text, changed := a.cfg.RewriteInput(first.Text)
	if !changed {
		return msg
	}
	first.Text = text
	user.Content = slices.Clone(user.Content)
	user.Content[0] = first
	return user
}
