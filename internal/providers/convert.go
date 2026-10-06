package providers

import "AskCore/pkg/protocol"

// ConvertToLLM is the default filter from the agent message log to the model
// message list. It keeps system, user, assistant and tool result messages and
// drops every other role (custom and raw messages). It returns a new slice and
// does not change msgs; the kept messages are the same values, not copies.
func ConvertToLLM(msgs []protocol.Message) []protocol.Message {
	out := make([]protocol.Message, 0, len(msgs))
	for _, m := range msgs {
		switch m.(type) {
		case protocol.SystemMessage, *protocol.SystemMessage,
			protocol.UserMessage, *protocol.UserMessage,
			protocol.AssistantMessage, *protocol.AssistantMessage,
			protocol.ToolResultMessage, *protocol.ToolResultMessage:
			out = append(out, m)
		}
	}
	return out
}

// NormalizeRequest makes the provider-facing request. A non-empty system
// prompt or tool list becomes one leading system message with timestamp 0; an
// empty prompt with no tools adds nothing. The result has its own message slice
// and its own copy of the tool list and schemas. The caller's messages are
// appended by value, so the result shares their inner slices and raw JSON with
// r; this function never changes them. Replay of system sections and tool changes inside the log is
// not part of this function.
func NormalizeRequest(r Request) TranscriptRequest {
	msgs := make([]protocol.Message, 0, len(r.Messages)+1)
	if r.SystemPrompt != "" || len(r.Tools) > 0 {
		sys := protocol.SystemMessage{Content: []protocol.Text{}, ToolsAdded: r.Tools}
		if r.SystemPrompt != "" {
			sys.Content = []protocol.Text{{Text: r.SystemPrompt}}
		}
		// The clone copies the caller's tool slice and the raw JSON schemas.
		msgs = append(msgs, protocol.CloneMessage(sys))
	}
	return TranscriptRequest{Messages: append(msgs, r.Messages...)}
}
