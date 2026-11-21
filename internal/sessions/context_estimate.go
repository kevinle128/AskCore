package sessions

import (
	"encoding/json"

	"AskCore/pkg/protocol"
)

// imageChars is the fixed size, in characters, that an image counts for. The
// base64 data says nothing about what the model charges for the image.
const imageChars = 4800

// charsPerToken is the ratio of the estimate. It is rough: it under-counts CJK
// text and JSON. There is no tokenizer by design.
const charsPerToken = 4

// EstimateContext estimates the token size of the next model request.
//
// The anchor is the latest completed attempt that reported a non-zero usage and
// that comes after the latest context edit. Its usage is exact for the request
// that it answered and for the reply, because the assistant message of an
// attempt is logged before its settlement. The estimate is that usage plus
// ceil(chars/4) of each message logged after the anchor. A context edit changes
// the messages that came before the anchor, so an earlier anchor is dropped.
//
// Without an anchor the estimate is ceil(chars/4) of the latest system
// snapshot (prompt and tool declarations) plus all messages after the context
// edits. ok is false when the log holds nothing to estimate.
//
// Limit: a snapshot or model change after the anchor is not re-estimated.
func EstimateContext(entries []Entry) (tokens int64, ok bool) {
	lastEdit := -1
	for i, e := range entries {
		if _, isEdit := e.(ContextEdit); isEdit {
			lastEdit = i
		}
	}
	for i := len(entries) - 1; i > lastEdit; i-- {
		s, isSettled := entries[i].(AttemptSettled)
		if !isSettled || s.Outcome != "completed" || s.Usage == nil {
			continue
		}
		anchor := anchorTokens(*s.Usage)
		if anchor <= 0 {
			continue
		}
		// No context edit follows the anchor, so the later messages need no
		// projection.
		var chars int64
		for _, later := range entries[i+1:] {
			if m, isMessage := later.(MessageEntry); isMessage {
				chars += tokensOf(messageChars(m.Message))
			}
		}
		return anchor + chars, true
	}

	var total int64
	found := false
	for i := len(entries) - 1; i >= 0; i-- {
		if snap, isSnap := entries[i].(SystemSnapshot); isSnap {
			total += tokensOf(snapshotChars(snap))
			found = true
			break
		}
	}
	msgs := MessagesOf(entries)
	for _, m := range msgs {
		total += tokensOf(messageChars(m))
	}
	return total, found || len(msgs) > 0
}

// anchorTokens is the token count of a usage: the provider total, or the sum of
// the parts when the provider sent no total.
func anchorTokens(u protocol.Usage) int64 {
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.Input + u.Output + u.CacheRead + u.CacheWrite
}

// tokensOf rounds a character count up to tokens.
func tokensOf(chars int) int64 {
	return int64((chars + charsPerToken - 1) / charsPerToken)
}

func snapshotChars(s SystemSnapshot) int {
	n := len(s.SystemPrompt)
	if len(s.Tools) == 0 {
		return n
	}
	if b, err := json.Marshal(s.Tools); err == nil {
		return n + len(b)
	}
	for _, d := range s.Tools {
		n += len(d.Name) + len(d.Description) + len(d.Parameters)
	}
	return n
}

// messageChars counts the characters that a message adds to a request. An error
// reply counts 0, because it is not sent to the model again.
func messageChars(msg protocol.Message) int {
	switch m := msg.(type) {
	case protocol.SystemMessage:
		n := 0
		for _, t := range m.Content {
			n += len(t.Text)
		}
		return n
	case protocol.UserMessage:
		return userChars(m.Content)
	case protocol.CustomMessage:
		return userChars(m.Content)
	case protocol.ToolResultMessage:
		return userChars(m.Content)
	case protocol.AssistantMessage:
		if m.StopReason == protocol.StopError {
			return 0
		}
		n := 0
		for _, b := range m.Content {
			switch v := b.(type) {
			case protocol.Text:
				n += len(v.Text)
			case protocol.Thinking:
				n += len(v.Thinking)
			case protocol.ToolCall:
				n += len(v.Name) + len(v.Arguments)
			}
		}
		return n
	case protocol.RawMessage:
		return len(m.Data)
	}
	return 0
}

func userChars(blocks []protocol.UserBlock) int {
	n := 0
	for _, b := range blocks {
		switch v := b.(type) {
		case protocol.Text:
			n += len(v.Text)
		case protocol.Image:
			n += imageChars
		}
	}
	return n
}
