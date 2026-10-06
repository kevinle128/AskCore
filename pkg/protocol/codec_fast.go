package protocol

import (
	"encoding/json"
	"strconv"
	"unicode/utf8"
)

// encodeMessageUpdate is the reflection encoding of a message_update. The
// fast path below must produce the same bytes.
func encodeMessageUpdate(e *MessageUpdate) ([]byte, error) {
	inner, err := MarshalStreamEvent(e.AssistantMessageEvent)
	if err != nil {
		return nil, err
	}
	return marshalJSON(struct {
		envelopeWire
		AssistantMessageEvent json.RawMessage `json:"assistantMessageEvent"`
		Usage                 Usage           `json:"usage"`
	}{newEnvelopeWire(&e.Envelope, e.EventType()), inner, e.Usage})
}

// appendDeltaUpdate encodes a text or thinking delta message_update without
// reflection. It is most of what JSON mode writes, one event per streamed
// chunk. It reports false when a string needs escaping, so the caller falls
// back to encodeMessageUpdate and the bytes stay identical either way.
func appendDeltaUpdate(e *MessageUpdate) ([]byte, bool) {
	var typ, delta string
	var index int
	switch d := e.AssistantMessageEvent.(type) {
	case TextDeltaEvent:
		typ, index, delta = StreamTypeTextDelta, d.ContentIndex, d.Delta
	case ThinkingDeltaEvent:
		typ, index, delta = StreamTypeThinkingDelta, d.ContentIndex, d.Delta
	default:
		return nil, false
	}
	if !plainJSONString(delta) || !plainJSONString(e.SessionID) || !plainJSONString(e.RunID) {
		return nil, false
	}
	b := make([]byte, 0, 384+len(e.SessionID)+len(e.RunID)+len(delta))
	b = strconv.AppendUint(append(b, `{"seq":`...), e.Seq, 10)
	b = appendInt(b, `,"ts":`, e.TS)
	b = appendPlain(b, `,"sessionId":`, e.SessionID)
	b = appendPlain(b, `,"runId":`, e.RunID)
	b = appendPlain(b, `,"type":`, TypeMessageUpdate)
	b = appendPlain(b, `,"assistantMessageEvent":{"type":`, typ)
	b = appendInt(b, `,"contentIndex":`, int64(index))
	b = appendPlain(b, `,"delta":`, delta)
	b = appendUsage(append(b, `},"usage":`...), e.Usage)
	return append(b, '}'), true
}

// appendUsage writes u in the field order and omitempty rules of its struct
// tags.
func appendUsage(b []byte, u Usage) []byte {
	b = appendInt(b, `{"input":`, u.Input)
	b = appendInt(b, `,"output":`, u.Output)
	b = appendInt(b, `,"cacheRead":`, u.CacheRead)
	b = appendInt(b, `,"cacheWrite":`, u.CacheWrite)
	if u.CacheWrite1h != nil {
		b = appendInt(b, `,"cacheWrite1h":`, *u.CacheWrite1h)
	}
	if u.Reasoning != nil {
		b = appendInt(b, `,"reasoning":`, *u.Reasoning)
	}
	b = appendInt(b, `,"totalTokens":`, u.TotalTokens)
	c := u.Cost
	b = appendInt(b, `,"cost":{"input":`, c.Input)
	b = appendInt(b, `,"output":`, c.Output)
	b = appendInt(b, `,"cacheRead":`, c.CacheRead)
	b = appendInt(b, `,"cacheWrite":`, c.CacheWrite)
	b = appendInt(b, `,"total":`, c.Total)
	return append(b, "}}"...)
}

func appendInt(b []byte, prefix string, v int64) []byte {
	return strconv.AppendInt(append(b, prefix...), v, 10)
}

// appendPlain writes s quoted; s must pass plainJSONString.
func appendPlain(b []byte, prefix, s string) []byte {
	b = append(append(b, prefix...), '"')
	return append(append(b, s...), '"')
}

// plainJSONString reports whether encoding/json writes s between quotes with
// no escape: valid UTF-8 with no control byte, quote, backslash, U+2028 or
// U+2029. HTML characters count as plain because the codec turns HTML
// escaping off.
func plainJSONString(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == '"' || c == '\\' {
			return false
		}
		if c == 0xE2 && i+2 < len(s) && s[i+1] == 0x80 && (s[i+2] == 0xA8 || s[i+2] == 0xA9) {
			return false
		}
	}
	return utf8.ValidString(s)
}
