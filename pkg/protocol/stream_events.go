package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Discriminator values of provider stream events.
const (
	StreamTypeStart         = "start"
	StreamTypeTextStart     = "text_start"
	StreamTypeTextDelta     = "text_delta"
	StreamTypeTextEnd       = "text_end"
	StreamTypeThinkingStart = "thinking_start"
	StreamTypeThinkingDelta = "thinking_delta"
	StreamTypeThinkingEnd   = "thinking_end"
	StreamTypeToolCallStart = "toolcall_start"
	StreamTypeToolCallDelta = "toolcall_delta"
	StreamTypeToolCallEnd   = "toolcall_end"
	StreamTypeDone          = "done"
	StreamTypeError         = "error"
)

// AssistantMessageEvent is one event of a provider response stream. The
// events carry deltas only; no event holds the cumulative message. All event
// types are used as values (not pointers).
type AssistantMessageEvent interface {
	EventType() string
	streamEvent()
}

// BlockEvent is an AssistantMessageEvent that belongs to one content block.
// It is the only kind that a message_update event can carry: start, done and
// error are never nested in an update.
type BlockEvent interface {
	AssistantMessageEvent
	blockEvent()
}

// StartEvent opens the stream. Message is the immutable seed: identity and
// metadata, empty content, stop reason pending.
type StartEvent struct{ Message AssistantMessage }

// TextStartEvent opens a text block. Content can already hold text.
type TextStartEvent struct {
	ContentIndex int
	Content      Text
}

// TextDeltaEvent appends text to an open text block.
type TextDeltaEvent struct {
	ContentIndex int
	Delta        string
}

// TextEndEvent closes a text block. Content and TextSignature are the
// complete final values; a nil TextSignature removes an earlier signature.
type TextEndEvent struct {
	ContentIndex  int
	Content       string
	TextSignature *string
}

// ThinkingStartEvent opens a thinking block. Content can already hold text
// and the redaction data.
type ThinkingStartEvent struct {
	ContentIndex int
	Content      Thinking
}

// ThinkingDeltaEvent appends text to an open thinking block.
type ThinkingDeltaEvent struct {
	ContentIndex int
	Delta        string
}

// ThinkingEndEvent closes a thinking block. The fields are the complete final
// values; nil metadata removes an earlier value.
type ThinkingEndEvent struct {
	ContentIndex      int
	Content           string
	ThinkingSignature *string
	Redacted          *bool
}

// ToolCallStartEvent opens a tool call block.
type ToolCallStartEvent struct {
	ContentIndex     int
	ID               string
	ToolName         string
	Arguments        json.RawMessage
	ThoughtSignature *string
	Namespace        *string
}

// ToolCallDeltaEvent appends raw argument JSON text to an open tool call.
type ToolCallDeltaEvent struct {
	ContentIndex int
	Delta        string
}

// ToolCallEndEvent closes a tool call block. ToolCall replaces the whole block.
type ToolCallEndEvent struct {
	ContentIndex int
	ToolCall     ToolCall
}

// DoneEvent ends a successful stream. Reason is stop, length, toolUse or deferred.
type DoneEvent struct {
	Reason  StopReason
	Message AssistantMessage
}

// ErrorEvent ends a failed stream. Reason is error or aborted. On the wire the
// message is in the field "error".
type ErrorEvent struct {
	Reason StopReason
	Error  AssistantMessage
}

func (StartEvent) EventType() string         { return StreamTypeStart }
func (TextStartEvent) EventType() string     { return StreamTypeTextStart }
func (TextDeltaEvent) EventType() string     { return StreamTypeTextDelta }
func (TextEndEvent) EventType() string       { return StreamTypeTextEnd }
func (ThinkingStartEvent) EventType() string { return StreamTypeThinkingStart }
func (ThinkingDeltaEvent) EventType() string { return StreamTypeThinkingDelta }
func (ThinkingEndEvent) EventType() string   { return StreamTypeThinkingEnd }
func (ToolCallStartEvent) EventType() string { return StreamTypeToolCallStart }
func (ToolCallDeltaEvent) EventType() string { return StreamTypeToolCallDelta }
func (ToolCallEndEvent) EventType() string   { return StreamTypeToolCallEnd }
func (DoneEvent) EventType() string          { return StreamTypeDone }
func (ErrorEvent) EventType() string         { return StreamTypeError }

func (StartEvent) streamEvent()         {}
func (TextStartEvent) streamEvent()     {}
func (TextDeltaEvent) streamEvent()     {}
func (TextEndEvent) streamEvent()       {}
func (ThinkingStartEvent) streamEvent() {}
func (ThinkingDeltaEvent) streamEvent() {}
func (ThinkingEndEvent) streamEvent()   {}
func (ToolCallStartEvent) streamEvent() {}
func (ToolCallDeltaEvent) streamEvent() {}
func (ToolCallEndEvent) streamEvent()   {}
func (DoneEvent) streamEvent()          {}
func (ErrorEvent) streamEvent()         {}

func (TextStartEvent) blockEvent()     {}
func (TextDeltaEvent) blockEvent()     {}
func (TextEndEvent) blockEvent()       {}
func (ThinkingStartEvent) blockEvent() {}
func (ThinkingDeltaEvent) blockEvent() {}
func (ThinkingEndEvent) blockEvent()   {}
func (ToolCallStartEvent) blockEvent() {}
func (ToolCallDeltaEvent) blockEvent() {}
func (ToolCallEndEvent) blockEvent()   {}

// validDoneReason and validErrorReason hold the terminal reason rules:
// pending is never terminal, and success and failure reasons do not mix.
func validDoneReason(r StopReason) bool {
	switch r {
	case StopStop, StopLength, StopToolUse, StopDeferred:
		return true
	}
	return false
}

func validErrorReason(r StopReason) bool { return r == StopError || r == StopAborted }

// checkDone checks the reason of a done event and the message that it holds.
// A nil msg means the message is absent.
func checkDone(reason StopReason, msg *AssistantMessage) error {
	if !validDoneReason(reason) {
		return fmt.Errorf("protocol: invalid done reason %q", reason)
	}
	if msg == nil {
		return errors.New("protocol: done has no message")
	}
	if msg.StopReason != reason {
		return fmt.Errorf("protocol: done reason %q does not match message stop reason %q", reason, msg.StopReason)
	}
	return nil
}

// checkError checks the reason of an error event and the message that it
// holds. A nil msg means the message is absent.
func checkError(reason StopReason, msg *AssistantMessage) error {
	if !validErrorReason(reason) {
		return fmt.Errorf("protocol: invalid error reason %q", reason)
	}
	if msg == nil {
		return errors.New("protocol: error event has no error message")
	}
	if msg.StopReason != reason {
		return fmt.Errorf("protocol: error reason %q does not match message stop reason %q", reason, msg.StopReason)
	}
	return nil
}

// MarshalStreamEvent encodes a provider event as one JSON object. The field
// names follow Pi: type, contentIndex, delta, content, toolCall, reason, error.
// toolcall_start carries id and toolName, and the end events carry the final
// block metadata (Ask extensions that keep delta-only streams lossless).
func MarshalStreamEvent(ev AssistantMessageEvent) ([]byte, error) {
	switch e := ev.(type) {
	case StartEvent:
		return marshalJSON(struct {
			Type    string           `json:"type"`
			Message AssistantMessage `json:"message"`
		}{StreamTypeStart, e.Message})
	case TextStartEvent:
		return marshalJSON(struct {
			Type         string `json:"type"`
			ContentIndex int    `json:"contentIndex"`
			Content      Text   `json:"content"`
		}{StreamTypeTextStart, e.ContentIndex, e.Content})
	case TextDeltaEvent:
		return marshalDelta(StreamTypeTextDelta, e.ContentIndex, e.Delta)
	case TextEndEvent:
		return marshalJSON(struct {
			Type          string  `json:"type"`
			ContentIndex  int     `json:"contentIndex"`
			Content       string  `json:"content"`
			TextSignature *string `json:"textSignature,omitempty"`
		}{StreamTypeTextEnd, e.ContentIndex, e.Content, e.TextSignature})
	case ThinkingStartEvent:
		return marshalJSON(struct {
			Type         string   `json:"type"`
			ContentIndex int      `json:"contentIndex"`
			Content      Thinking `json:"content"`
		}{StreamTypeThinkingStart, e.ContentIndex, e.Content})
	case ThinkingDeltaEvent:
		return marshalDelta(StreamTypeThinkingDelta, e.ContentIndex, e.Delta)
	case ThinkingEndEvent:
		return marshalJSON(struct {
			Type              string  `json:"type"`
			ContentIndex      int     `json:"contentIndex"`
			Content           string  `json:"content"`
			ThinkingSignature *string `json:"thinkingSignature,omitempty"`
			Redacted          *bool   `json:"redacted,omitempty"`
		}{StreamTypeThinkingEnd, e.ContentIndex, e.Content, e.ThinkingSignature, e.Redacted})
	case ToolCallStartEvent:
		if len(e.Arguments) > 0 && !isJSONObject(e.Arguments) {
			return nil, errors.New("protocol: toolcall_start arguments are not a JSON object")
		}
		return marshalJSON(struct {
			Type             string          `json:"type"`
			ContentIndex     int             `json:"contentIndex"`
			ID               string          `json:"id"`
			ToolName         string          `json:"toolName"`
			Arguments        json.RawMessage `json:"arguments,omitempty"`
			ThoughtSignature *string         `json:"thoughtSignature,omitempty"`
			Namespace        *string         `json:"namespace,omitempty"`
		}{StreamTypeToolCallStart, e.ContentIndex, e.ID, e.ToolName, e.Arguments, e.ThoughtSignature, e.Namespace})
	case ToolCallDeltaEvent:
		return marshalDelta(StreamTypeToolCallDelta, e.ContentIndex, e.Delta)
	case ToolCallEndEvent:
		return marshalJSON(struct {
			Type         string   `json:"type"`
			ContentIndex int      `json:"contentIndex"`
			ToolCall     ToolCall `json:"toolCall"`
		}{StreamTypeToolCallEnd, e.ContentIndex, e.ToolCall})
	case DoneEvent:
		if err := checkDone(e.Reason, &e.Message); err != nil {
			return nil, err
		}
		return marshalJSON(struct {
			Type    string           `json:"type"`
			Reason  StopReason       `json:"reason"`
			Message AssistantMessage `json:"message"`
		}{StreamTypeDone, e.Reason, e.Message})
	case ErrorEvent:
		if err := checkError(e.Reason, &e.Error); err != nil {
			return nil, err
		}
		return marshalJSON(struct {
			Type   string           `json:"type"`
			Reason StopReason       `json:"reason"`
			Error  AssistantMessage `json:"error"`
		}{StreamTypeError, e.Reason, e.Error})
	case nil:
		return nil, errors.New("protocol: stream event is nil")
	default:
		return nil, fmt.Errorf("protocol: unsupported stream event %T (use the value type)", ev)
	}
}

func marshalDelta(typ string, index int, delta string) ([]byte, error) {
	return marshalJSON(struct {
		Type         string `json:"type"`
		ContentIndex int    `json:"contentIndex"`
		Delta        string `json:"delta"`
	}{typ, index, delta})
}

type streamWire struct {
	Type              string            `json:"type"`
	ContentIndex      *int              `json:"contentIndex"`
	Content           json.RawMessage   `json:"content"`
	Delta             *string           `json:"delta"`
	TextSignature     *string           `json:"textSignature"`
	ThinkingSignature *string           `json:"thinkingSignature"`
	Redacted          *bool             `json:"redacted"`
	ID                *string           `json:"id"`
	ToolName          *string           `json:"toolName"`
	Arguments         json.RawMessage   `json:"arguments"`
	ThoughtSignature  *string           `json:"thoughtSignature"`
	Namespace         *string           `json:"namespace"`
	ToolCall          json.RawMessage   `json:"toolCall"`
	Reason            StopReason        `json:"reason"`
	Message           *AssistantMessage `json:"message"`
	Error             *AssistantMessage `json:"error"`
}

// UnmarshalStreamEvent decodes one provider event by its "type" field. Unknown
// types, missing required fields, negative content indexes and invalid
// terminal reasons are errors.
func UnmarshalStreamEvent(data []byte) (AssistantMessageEvent, error) {
	var w streamWire
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, err
	}
	needIndex := func() (int, error) {
		if w.ContentIndex == nil {
			return 0, fmt.Errorf("protocol: %s has no contentIndex", w.Type)
		}
		if *w.ContentIndex < 0 {
			return 0, fmt.Errorf("protocol: %s has negative contentIndex", w.Type)
		}
		return *w.ContentIndex, nil
	}
	needDelta := func() (int, string, error) {
		i, err := needIndex()
		if err != nil {
			return 0, "", err
		}
		if w.Delta == nil {
			return 0, "", fmt.Errorf("protocol: %s has no delta", w.Type)
		}
		return i, *w.Delta, nil
	}
	needContentString := func() (string, error) {
		var s string
		if len(w.Content) == 0 {
			return "", fmt.Errorf("protocol: %s has no content", w.Type)
		}
		if err := json.Unmarshal(w.Content, &s); err != nil {
			return "", fmt.Errorf("protocol: %s content is not a string: %w", w.Type, err)
		}
		return s, nil
	}
	switch w.Type {
	case StreamTypeStart:
		if w.Message == nil {
			return nil, errors.New("protocol: start has no message")
		}
		return StartEvent{Message: *w.Message}, nil
	case StreamTypeTextStart:
		i, err := needIndex()
		if err != nil {
			return nil, err
		}
		var t Text
		// Pi omits the initial content, which then means an empty block.
		if len(w.Content) > 0 && string(w.Content) != "null" {
			if err := t.UnmarshalJSON(w.Content); err != nil {
				return nil, fmt.Errorf("protocol: text_start content: %w", err)
			}
		}
		return TextStartEvent{ContentIndex: i, Content: t}, nil
	case StreamTypeTextDelta:
		i, d, err := needDelta()
		if err != nil {
			return nil, err
		}
		return TextDeltaEvent{ContentIndex: i, Delta: d}, nil
	case StreamTypeTextEnd:
		i, err := needIndex()
		if err != nil {
			return nil, err
		}
		s, err := needContentString()
		if err != nil {
			return nil, err
		}
		return TextEndEvent{ContentIndex: i, Content: s, TextSignature: w.TextSignature}, nil
	case StreamTypeThinkingStart:
		i, err := needIndex()
		if err != nil {
			return nil, err
		}
		var t Thinking
		// Pi omits the initial content, which then means an empty block.
		if len(w.Content) > 0 && string(w.Content) != "null" {
			if err := t.UnmarshalJSON(w.Content); err != nil {
				return nil, fmt.Errorf("protocol: thinking_start content: %w", err)
			}
		}
		return ThinkingStartEvent{ContentIndex: i, Content: t}, nil
	case StreamTypeThinkingDelta:
		i, d, err := needDelta()
		if err != nil {
			return nil, err
		}
		return ThinkingDeltaEvent{ContentIndex: i, Delta: d}, nil
	case StreamTypeThinkingEnd:
		i, err := needIndex()
		if err != nil {
			return nil, err
		}
		s, err := needContentString()
		if err != nil {
			return nil, err
		}
		return ThinkingEndEvent{ContentIndex: i, Content: s, ThinkingSignature: w.ThinkingSignature, Redacted: w.Redacted}, nil
	case StreamTypeToolCallStart:
		i, err := needIndex()
		if err != nil {
			return nil, err
		}
		if w.ID == nil || w.ToolName == nil {
			return nil, errors.New("protocol: toolcall_start needs id and toolName")
		}
		args := w.Arguments
		if isJSONNull(args) {
			args = nil
		} else if !isJSONObject(args) {
			return nil, errors.New("protocol: toolcall_start arguments are not a JSON object")
		}
		return ToolCallStartEvent{
			ContentIndex: i, ID: *w.ID, ToolName: *w.ToolName, Arguments: args,
			ThoughtSignature: w.ThoughtSignature, Namespace: w.Namespace,
		}, nil
	case StreamTypeToolCallDelta:
		i, d, err := needDelta()
		if err != nil {
			return nil, err
		}
		return ToolCallDeltaEvent{ContentIndex: i, Delta: d}, nil
	case StreamTypeToolCallEnd:
		i, err := needIndex()
		if err != nil {
			return nil, err
		}
		var c ToolCall
		if err := c.UnmarshalJSON(w.ToolCall); err != nil {
			return nil, fmt.Errorf("protocol: toolcall_end toolCall: %w", err)
		}
		return ToolCallEndEvent{ContentIndex: i, ToolCall: c}, nil
	case StreamTypeDone:
		if err := checkDone(w.Reason, w.Message); err != nil {
			return nil, err
		}
		return DoneEvent{Reason: w.Reason, Message: *w.Message}, nil
	case StreamTypeError:
		if err := checkError(w.Reason, w.Error); err != nil {
			return nil, err
		}
		return ErrorEvent{Reason: w.Reason, Error: *w.Error}, nil
	default:
		return nil, fmt.Errorf("protocol: unknown stream event type %q", w.Type)
	}
}
