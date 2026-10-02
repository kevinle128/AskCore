package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// EncodeEvent encodes an agent event as one compact JSON object with the
// envelope keys at the top level. The output holds no raw line feed, so it is
// safe for JSONL framing.
func EncodeEvent(ev Event) ([]byte, error) {
	switch e := ev.(type) {
	case nil:
		return nil, errors.New("protocol: event is nil")
	case *AgentStart, *TurnStart:
		return marshalJSON(struct{ envelopeWire }{newEnvelopeWire(e.Env(), e.EventType())})
	case *AgentEnd:
		msgs := make([]anyMessage, len(e.Messages))
		for i, m := range e.Messages {
			msgs[i] = anyMessage{m}
		}
		return marshalJSON(struct {
			envelopeWire
			Messages []anyMessage `json:"messages"`
		}{newEnvelopeWire(&e.Envelope, e.EventType()), msgs})
	case *TurnEnd:
		results := e.ToolResults
		if results == nil {
			results = []ToolResultMessage{}
		}
		return marshalJSON(struct {
			envelopeWire
			Message     anyMessage          `json:"message"`
			ToolResults []ToolResultMessage `json:"toolResults"`
		}{newEnvelopeWire(&e.Envelope, e.EventType()), anyMessage{e.Message}, results})
	case *MessageStart:
		return marshalJSON(struct {
			envelopeWire
			Message anyMessage `json:"message"`
		}{newEnvelopeWire(&e.Envelope, e.EventType()), anyMessage{e.Message}})
	case *MessageEnd:
		return marshalJSON(struct {
			envelopeWire
			Message anyMessage `json:"message"`
		}{newEnvelopeWire(&e.Envelope, e.EventType()), anyMessage{e.Message}})
	case *MessageUpdate:
		if e.AssistantMessageEvent == nil {
			return nil, errors.New("protocol: message_update has no assistant message event")
		}
		inner, err := MarshalStreamEvent(e.AssistantMessageEvent)
		if err != nil {
			return nil, err
		}
		return marshalJSON(struct {
			envelopeWire
			AssistantMessageEvent json.RawMessage `json:"assistantMessageEvent"`
			Usage                 Usage           `json:"usage"`
		}{newEnvelopeWire(&e.Envelope, e.EventType()), inner, e.Usage})
	case *ToolExecutionStart:
		return marshalJSON(struct {
			envelopeWire
			ToolCallID string          `json:"toolCallId"`
			ToolName   string          `json:"toolName"`
			Args       json.RawMessage `json:"args"`
		}{newEnvelopeWire(&e.Envelope, e.EventType()), e.ToolCallID, e.ToolName, e.Args})
	case *ToolExecutionUpdate:
		return marshalJSON(struct {
			envelopeWire
			ToolCallID    string              `json:"toolCallId"`
			ToolName      string              `json:"toolName"`
			Args          json.RawMessage     `json:"args"`
			PartialResult ToolExecutionResult `json:"partialResult"`
		}{newEnvelopeWire(&e.Envelope, e.EventType()), e.ToolCallID, e.ToolName, e.Args, e.PartialResult})
	case *ToolExecutionEnd:
		return marshalJSON(struct {
			envelopeWire
			ToolCallID string              `json:"toolCallId"`
			ToolName   string              `json:"toolName"`
			Result     ToolExecutionResult `json:"result"`
			IsError    bool                `json:"isError"`
		}{newEnvelopeWire(&e.Envelope, e.EventType()), e.ToolCallID, e.ToolName, e.Result, e.IsError})
	case *RawEvent:
		return encodeRawEvent(e)
	default:
		return nil, fmt.Errorf("protocol: unsupported event %T", ev)
	}
}

func encodeRawEvent(e *RawEvent) ([]byte, error) {
	if len(bytes.TrimSpace(e.Data)) == 0 {
		if e.Type == "" {
			return nil, errors.New("protocol: raw event has no type")
		}
		return marshalJSON(struct{ envelopeWire }{newEnvelopeWire(&e.Envelope, e.Type)})
	}
	if !isJSONObject(e.Data) {
		return nil, fmt.Errorf("protocol: raw event %q data is not a JSON object", e.Type)
	}
	// The emitter sets the envelope through Env(), so the struct fields win
	// over the envelope keys in Data. When nothing changed, the bytes stay as
	// they were decoded.
	var old envelopeWire
	if err := json.Unmarshal(e.Data, &old); err != nil {
		return nil, fmt.Errorf("protocol: raw event data is not valid JSON: %w", err)
	}
	typ := e.Type
	if typ == "" {
		typ = old.Type
	}
	if typ == "" {
		return nil, errors.New("protocol: raw event has no type")
	}
	cur := newEnvelopeWire(&e.Envelope, typ)
	if cur == old {
		return compactObject(e.Data)
	}
	return rewriteEnvelope(e.Data, cur)
}

// envelopeKeys are the JSON keys of envelopeWire.
var envelopeKeys = map[string]bool{"seq": true, "ts": true, "sessionId": true, "runId": true, "type": true}

// rewriteEnvelope writes env first and then every non-envelope key of data in
// its original order.
func rewriteEnvelope(data []byte, env envelopeWire) ([]byte, error) {
	head, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.Write(head[:len(head)-1]) // drop the closing brace
	dec := json.NewDecoder(bytes.NewReader(data))
	if _, err := dec.Token(); err != nil { // opening brace
		return nil, fmt.Errorf("protocol: raw event data is not valid JSON: %w", err)
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("protocol: raw event data is not valid JSON: %w", err)
		}
		key, _ := tok.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, fmt.Errorf("protocol: raw event data is not valid JSON: %w", err)
		}
		if envelopeKeys[key] {
			continue
		}
		k, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buf.WriteByte(',')
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(val)
	}
	buf.WriteByte('}')
	return compactObject(buf.Bytes())
}

// compactObject removes all insignificant white space from a JSON object, so
// that the result holds no line feed.
func compactObject(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, data); err != nil {
		return nil, fmt.Errorf("protocol: raw data is not valid JSON: %w", err)
	}
	return buf.Bytes(), nil
}

// DecodeEvent decodes one JSON object into an agent event pointer. An unknown
// type becomes a *RawEvent. Invalid content in a known event is an error.
func DecodeEvent(data []byte) (Event, error) {
	var head envelopeWire
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	if head.Type == "" {
		return nil, errors.New("protocol: event has no type")
	}
	env := head.envelope()
	switch head.Type {
	case TypeAgentStart:
		return &AgentStart{Envelope: env}, nil
	case TypeTurnStart:
		return &TurnStart{Envelope: env}, nil
	case TypeAgentEnd:
		var w struct {
			Messages []anyMessage `json:"messages"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		msgs := make([]Message, len(w.Messages))
		for i, m := range w.Messages {
			msgs[i] = m.m
		}
		return &AgentEnd{Envelope: env, Messages: msgs}, nil
	case TypeTurnEnd:
		var w struct {
			Message     *anyMessage         `json:"message"`
			ToolResults []ToolResultMessage `json:"toolResults"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		if w.Message == nil {
			return nil, errors.New("protocol: turn_end has no message")
		}
		if w.ToolResults == nil {
			w.ToolResults = []ToolResultMessage{}
		}
		return &TurnEnd{Envelope: env, Message: w.Message.m, ToolResults: w.ToolResults}, nil
	case TypeMessageStart, TypeMessageEnd:
		var w struct {
			Message *anyMessage `json:"message"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		if w.Message == nil {
			return nil, fmt.Errorf("protocol: %s has no message", head.Type)
		}
		if head.Type == TypeMessageStart {
			return &MessageStart{Envelope: env, Message: w.Message.m}, nil
		}
		return &MessageEnd{Envelope: env, Message: w.Message.m}, nil
	case TypeMessageUpdate:
		var w struct {
			AssistantMessageEvent json.RawMessage `json:"assistantMessageEvent"`
			Usage                 Usage           `json:"usage"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		inner, err := UnmarshalStreamEvent(w.AssistantMessageEvent)
		if err != nil {
			return nil, fmt.Errorf("protocol: message_update: %w", err)
		}
		block, ok := inner.(BlockEvent)
		if !ok {
			return nil, fmt.Errorf("protocol: message_update cannot carry %q", inner.EventType())
		}
		return &MessageUpdate{Envelope: env, AssistantMessageEvent: block, Usage: w.Usage}, nil
	case TypeToolExecutionStart:
		var w struct {
			ToolCallID string          `json:"toolCallId"`
			ToolName   string          `json:"toolName"`
			Args       json.RawMessage `json:"args"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return &ToolExecutionStart{Envelope: env, ToolCallID: w.ToolCallID, ToolName: w.ToolName, Args: w.Args}, nil
	case TypeToolExecutionUpdate:
		var w struct {
			ToolCallID    string              `json:"toolCallId"`
			ToolName      string              `json:"toolName"`
			Args          json.RawMessage     `json:"args"`
			PartialResult ToolExecutionResult `json:"partialResult"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return &ToolExecutionUpdate{
			Envelope: env, ToolCallID: w.ToolCallID, ToolName: w.ToolName, Args: w.Args, PartialResult: w.PartialResult,
		}, nil
	case TypeToolExecutionEnd:
		var w struct {
			ToolCallID string              `json:"toolCallId"`
			ToolName   string              `json:"toolName"`
			Result     ToolExecutionResult `json:"result"`
			IsError    bool                `json:"isError"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return &ToolExecutionEnd{
			Envelope: env, ToolCallID: w.ToolCallID, ToolName: w.ToolName, Result: w.Result, IsError: w.IsError,
		}, nil
	default:
		return &RawEvent{Envelope: env, Type: head.Type, Data: append(json.RawMessage(nil), bytes.TrimSpace(data)...)}, nil
	}
}

// JSONLWriter writes events as JSON Lines: one object per line, ended by LF.
// It is safe for concurrent use; each event is one Write call on the target.
type JSONLWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// NewJSONLWriter returns a writer that frames events on w.
func NewJSONLWriter(w io.Writer) *JSONLWriter { return &JSONLWriter{w: w} }

// Write encodes ev and writes it as one line. A failed encode writes nothing.
func (j *JSONLWriter) Write(ev Event) error {
	line, err := EncodeEvent(ev)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	j.mu.Lock()
	defer j.mu.Unlock()
	n, err := j.w.Write(line)
	if err == nil && n < len(line) {
		err = io.ErrShortWrite
	}
	return err
}

// JSONLReader splits a stream into lines. It splits on LF only: a CR before
// the LF is stripped (one), and U+2028 and U+2029 are ordinary characters. A
// line has no size limit.
type JSONLReader struct{ r *bufio.Reader }

// NewJSONLReader returns a reader over r.
func NewJSONLReader(r io.Reader) *JSONLReader { return &JSONLReader{r: bufio.NewReader(r)} }

// Next returns the next line without its terminator. The returned slice is
// owned by the caller. A final line without LF is returned like any other
// line, and the call after it returns io.EOF. An empty line is returned as an
// empty slice. A read error other than io.EOF is returned as is, and the
// unfinished line is dropped.
func (j *JSONLReader) Next() ([]byte, error) {
	line, err := j.r.ReadBytes('\n')
	if err != nil {
		if !errors.Is(err, io.EOF) {
			return nil, err
		}
		if len(line) == 0 {
			return nil, io.EOF
		}
	} else {
		line = line[:len(line)-1]
	}
	return bytes.TrimSuffix(line, []byte{'\r'}), nil
}
