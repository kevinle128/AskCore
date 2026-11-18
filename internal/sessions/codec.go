package sessions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	"AskCore/pkg/protocol"
)

// FormatVersion is the session file format that this build writes and the
// newest one that it reads.
const FormatVersion = 1

// headerType is the fixed "type" word of the first line of a session file.
const headerType = "session"

var (
	// ErrUnknownEntryType means a line names an entry type that this build does
	// not know and the line is not marked ignorable. A newer writer made it.
	ErrUnknownEntryType = errors.New("sessions: unknown entry type")
	// ErrFormatNewer means the header version is above FormatVersion.
	ErrFormatNewer = errors.New("sessions: session format is newer than this build")
	// ErrCorruptLine means a line is not valid JSON or does not have the shape
	// of a header or an entry.
	ErrCorruptLine = errors.New("sessions: corrupt line")
)

// Header is the first line of a session. ParentSession and ParentSeq name the
// session and the position that a forked session starts from; they are empty
// for a session that has no parent.
type Header struct {
	Version       int       `json:"version"`
	ID            string    `json:"id"`
	Cwd           string    `json:"cwd"`
	CreatedAt     time.Time `json:"createdAt"`
	ParentSession string    `json:"parentSession,omitempty"`
	ParentSeq     int       `json:"parentSeq,omitempty"`
}

// headerWire is the header with its fixed type word.
type headerWire struct {
	Type string `json:"type"`
	Header
}

// EncodeHeader returns the header line, without a trailing newline. A zero
// Version is written as FormatVersion.
func EncodeHeader(h Header) ([]byte, error) {
	if h.Version == 0 {
		h.Version = FormatVersion
	}
	if h.ID == "" || h.Cwd == "" {
		return nil, fmt.Errorf("%w: header needs an id and a cwd", ErrUnencodableEntry)
	}
	return json.Marshal(headerWire{Type: headerType, Header: h})
}

// DecodeHeader reads a header line. The version is checked before the other
// fields, so a newer format gives ErrFormatNewer and not a field error.
func DecodeHeader(line []byte) (Header, error) {
	var head struct {
		Type    string `json:"type"`
		Version *int   `json:"version"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		return Header{}, fmt.Errorf("%w: header: %w", ErrCorruptLine, err)
	}
	if head.Type != headerType || head.Version == nil || *head.Version < 1 {
		return Header{}, fmt.Errorf("%w: not a session header", ErrCorruptLine)
	}
	if *head.Version > FormatVersion {
		return Header{}, fmt.Errorf("%w: version %d, supported %d", ErrFormatNewer, *head.Version, FormatVersion)
	}
	var h Header
	if err := json.Unmarshal(line, &h); err != nil {
		return Header{}, fmt.Errorf("%w: header: %w", ErrCorruptLine, err)
	}
	if h.ID == "" || h.Cwd == "" {
		return Header{}, fmt.Errorf("%w: header has no id or cwd", ErrCorruptLine)
	}
	return h, nil
}

// envelope is one entry line. Unknown envelope fields are refused, because a
// field that changes how to read the line must not be dropped. Ignorable lets
// an old reader skip a type that it does not know.
type envelope struct {
	Seq       *int            `json:"seq"`
	Type      string          `json:"type"`
	Ignorable bool            `json:"ignorable,omitempty"`
	Data      json.RawMessage `json:"data"`
}

// kind ties one Go entry type to its fixed wire word.
type kind struct {
	word   string
	typ    reflect.Type
	encode func(Entry) (json.RawMessage, error)
	decode func(json.RawMessage) (Entry, error)
}

// kinds is the only place that maps entry types to wire words. Never change a
// word: stored files carry it.
var kinds = []kind{
	messageKind(),
	plainKind[CycleOpened]("cycle_opened"),
	plainKind[CycleClosed]("cycle_closed"),
	plainKind[TurnOpened]("turn_opened"),
	plainKind[TurnClosed]("turn_closed"),
	plainKind[AttemptSettled]("attempt_settled"),
	plainKind[ToolCall]("tool_call"),
	plainKind[RetryScheduled]("retry_scheduled"),
	plainKind[RetryStarted]("retry_started"),
	requestDeltaKind(),
	plainKind[SystemSnapshot]("system_snapshot"),
	plainKind[InputOutcome]("input_outcome"),
	plainKind[ModelChange]("model_change"),
	plainKind[ThinkingChange]("thinking_change"),
	contextEditKind(),
}

var (
	kindByType = map[reflect.Type]*kind{}
	kindByWord = map[string]*kind{}
)

func init() {
	for i := range kinds {
		k := &kinds[i]
		if _, dup := kindByWord[k.word]; dup {
			panic("sessions: duplicate wire word " + k.word)
		}
		kindByType[k.typ] = k
		kindByWord[k.word] = k
	}
}

// WireWords returns the wire word of every entry type, keyed by a zero value
// of the type. A test uses it to check that no entry type is missing.
func WireWords() map[string]Entry {
	out := make(map[string]Entry, len(kinds))
	for _, k := range kinds {
		out[k.word] = reflect.Zero(k.typ).Interface().(Entry)
	}
	return out
}

// EncodeEntry returns the line of entry e at log position seq, without a
// trailing newline. It fails with ErrUnencodableEntry for an entry that holds
// no value, has no wire word, or cannot be encoded.
func EncodeEntry(seq int, e Entry) ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("%w: nil entry", ErrUnencodableEntry)
	}
	k, ok := kindByType[reflect.TypeOf(e)]
	if !ok {
		return nil, fmt.Errorf("%w: no wire word for %T", ErrUnencodableEntry, e)
	}
	data, err := k.encode(e)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUnencodableEntry, k.word, err)
	}
	s := seq
	return json.Marshal(envelope{Seq: &s, Type: k.word, Data: data})
}

// DecodeEntry reads one entry line and returns its log position. For an
// unknown type that the line marks ignorable it returns a nil entry and no
// error; the caller skips the line. Without that mark an unknown type gives
// ErrUnknownEntryType. A line that is not valid gives ErrCorruptLine.
func DecodeEntry(line []byte) (int, Entry, error) {
	var env envelope
	if err := decodeStrict(line, &env); err != nil {
		return 0, nil, fmt.Errorf("%w: %w", ErrCorruptLine, err)
	}
	if env.Seq == nil || *env.Seq < 0 || env.Type == "" {
		return 0, nil, fmt.Errorf("%w: entry has no seq or type", ErrCorruptLine)
	}
	k, ok := kindByWord[env.Type]
	if !ok {
		if env.Ignorable {
			return *env.Seq, nil, nil
		}
		return 0, nil, fmt.Errorf("%w: %q", ErrUnknownEntryType, env.Type)
	}
	if len(env.Data) == 0 {
		return 0, nil, fmt.Errorf("%w: %s has no data", ErrCorruptLine, env.Type)
	}
	e, err := k.decode(env.Data)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %s: %w", ErrCorruptLine, env.Type, err)
	}
	return *env.Seq, e, nil
}

// decodeStrict decodes one JSON value, refuses unknown fields on v itself and
// refuses text after the value.
func decodeStrict(line []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("text after the JSON value")
	}
	return nil
}

// plainKind covers an entry type whose fields are plain JSON values. Data is
// decoded without DisallowUnknownFields, so a field that a later build adds
// stays readable.
func plainKind[T Entry](word string) kind {
	var zero T
	return kind{
		word: word,
		typ:  reflect.TypeOf(zero),
		encode: func(e Entry) (json.RawMessage, error) {
			return json.Marshal(e.(T))
		},
		decode: func(data json.RawMessage) (Entry, error) {
			var v T
			if err := json.Unmarshal(data, &v); err != nil {
				return nil, err
			}
			return v, nil
		},
	}
}

func encodeMessage(m protocol.Message) (json.RawMessage, error) {
	return protocol.MarshalMessage(m)
}

func encodeMessages(msgs []protocol.Message) ([]json.RawMessage, error) {
	if len(msgs) == 0 {
		return nil, nil
	}
	out := make([]json.RawMessage, len(msgs))
	for i, m := range msgs {
		raw, err := encodeMessage(m)
		if err != nil {
			return nil, err
		}
		out[i] = raw
	}
	return out, nil
}

func decodeMessages(raws []json.RawMessage) ([]protocol.Message, error) {
	if len(raws) == 0 {
		return nil, nil
	}
	out := make([]protocol.Message, len(raws))
	for i, raw := range raws {
		m, err := protocol.UnmarshalMessage(raw)
		if err != nil {
			return nil, err
		}
		out[i] = m
	}
	return out, nil
}

// The wire structs below embed the entry fields and shadow the message fields
// with raw JSON. encoding/json lets the shallower field win, so a field added
// to the entry type is carried without a change here.

type messageEntryBase MessageEntry

type messageEntryWire struct {
	messageEntryBase
	Message json.RawMessage `json:"message"`
}

func messageKind() kind {
	return kind{
		word: "message",
		typ:  reflect.TypeOf(MessageEntry{}),
		encode: func(e Entry) (json.RawMessage, error) {
			v := e.(MessageEntry)
			raw, err := encodeMessage(v.Message)
			if err != nil {
				return nil, err
			}
			return json.Marshal(messageEntryWire{messageEntryBase: messageEntryBase(v), Message: raw})
		},
		decode: func(data json.RawMessage) (Entry, error) {
			var w messageEntryWire
			if err := json.Unmarshal(data, &w); err != nil {
				return nil, err
			}
			m, err := protocol.UnmarshalMessage(w.Message)
			if err != nil {
				return nil, err
			}
			v := MessageEntry(w.messageEntryBase)
			v.Message = m
			return v, nil
		},
	}
}

type requestDeltaBase RequestDelta

type requestDeltaWire struct {
	requestDeltaBase
	Removed []json.RawMessage `json:"removed,omitempty"`
	Added   []json.RawMessage `json:"added,omitempty"`
}

func requestDeltaKind() kind {
	return kind{
		word: "request_delta",
		typ:  reflect.TypeOf(RequestDelta{}),
		encode: func(e Entry) (json.RawMessage, error) {
			v := e.(RequestDelta)
			w := requestDeltaWire{requestDeltaBase: requestDeltaBase(v)}
			var err error
			if w.Removed, err = encodeMessages(v.Removed); err != nil {
				return nil, err
			}
			if w.Added, err = encodeMessages(v.Added); err != nil {
				return nil, err
			}
			return json.Marshal(w)
		},
		decode: func(data json.RawMessage) (Entry, error) {
			var w requestDeltaWire
			if err := json.Unmarshal(data, &w); err != nil {
				return nil, err
			}
			v := RequestDelta(w.requestDeltaBase)
			var err error
			if v.Removed, err = decodeMessages(w.Removed); err != nil {
				return nil, err
			}
			if v.Added, err = decodeMessages(w.Added); err != nil {
				return nil, err
			}
			return v, nil
		},
	}
}

type contextEditWire struct {
	Target      int                     `json:"target"`
	Replacement *contextReplacementWire `json:"replacement,omitempty"`
}

type contextReplacementWire struct {
	Message json.RawMessage `json:"message"`
}

func contextEditKind() kind {
	return kind{
		word: "context_edit",
		typ:  reflect.TypeOf(ContextEdit{}),
		encode: func(e Entry) (json.RawMessage, error) {
			v := e.(ContextEdit)
			w := contextEditWire{Target: v.Target}
			if v.Replacement != nil {
				raw, err := encodeMessage(v.Replacement.Message)
				if err != nil {
					return nil, err
				}
				w.Replacement = &contextReplacementWire{Message: raw}
			}
			return json.Marshal(w)
		},
		decode: func(data json.RawMessage) (Entry, error) {
			var w contextEditWire
			if err := json.Unmarshal(data, &w); err != nil {
				return nil, err
			}
			v := ContextEdit{Target: w.Target}
			if w.Replacement != nil {
				m, err := protocol.UnmarshalMessage(w.Replacement.Message)
				if err != nil {
					return nil, err
				}
				v.Replacement = &ContextReplacement{Message: m}
			}
			return v, nil
		},
	}
}
