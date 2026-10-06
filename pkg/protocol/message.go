package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
)

// Role names of the built-in messages.
const (
	RoleSystem     = "system"
	RoleUser       = "user"
	RoleAssistant  = "assistant"
	RoleToolResult = "toolResult"
)

// StopReason tells why an assistant response ended.
type StopReason string

// Stop reasons. StopPending exists in memory only: it marks a message that is
// still streaming, and no terminal event or stored message can carry it.
const (
	StopPending  StopReason = "pending"
	StopStop     StopReason = "stop"
	StopLength   StopReason = "length"
	StopToolUse  StopReason = "toolUse"
	StopError    StopReason = "error"
	StopAborted  StopReason = "aborted"
	StopDeferred StopReason = "deferred"
)

func (r StopReason) valid() bool {
	switch r {
	case StopPending, StopStop, StopLength, StopToolUse, StopError, StopAborted, StopDeferred:
		return true
	}
	return false
}

// ThinkingLevel is the reasoning effort that the agent loop asked for.
type ThinkingLevel string

// Thinking levels.
const (
	ThinkingOff     ThinkingLevel = "off"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"
	ThinkingMax     ThinkingLevel = "max"
)

// Message is one entry of the message log. The built-in implementations are
// SystemMessage, UserMessage, AssistantMessage, ToolResultMessage and
// RawMessage (a role that has no registered decoder).
type Message interface{ Role() string }

// Section is one named part of a system prompt. A nil Value removes the
// section (it encodes as JSON null).
type Section struct {
	Name  string
	Value *string
}

// Sections is an ordered list of prompt sections. It encodes as a JSON
// object that keeps the order of the slice.
type Sections []Section

// MarshalJSON encodes the sections as an ordered JSON object.
func (s Sections) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	seen := make(map[string]struct{}, len(s))
	buf.WriteByte('{')
	for i, sec := range s {
		if _, dup := seen[sec.Name]; dup {
			return nil, fmt.Errorf("protocol: duplicate section name %q", sec.Name)
		}
		seen[sec.Name] = struct{}{}
		if i > 0 {
			buf.WriteByte(',')
		}
		k, err := marshalJSON(sec.Name)
		if err != nil {
			return nil, err
		}
		v, err := marshalJSON(sec.Value)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON decodes a JSON object and keeps its key order. When a key
// repeats, the last value wins at the position of the first key.
func (s *Sections) UnmarshalJSON(b []byte) error {
	if isJSONNull(b) {
		*s = nil
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errors.New("protocol: sections must be a JSON object")
	}
	out := Sections{}
	index := map[string]int{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		name, ok := kt.(string)
		if !ok {
			return errors.New("protocol: section name is not a string")
		}
		var v *string
		if err := dec.Decode(&v); err != nil {
			return fmt.Errorf("section %q: %w", name, err)
		}
		if i, dup := index[name]; dup {
			out[i].Value = v
			continue
		}
		index[name] = len(out)
		out = append(out, Section{Name: name, Value: v})
	}
	*s = out
	return nil
}

// SystemMessage carries system instructions and tool-set changes at one point
// of the log. Content is always written as text blocks; a plain string is
// accepted on input.
type SystemMessage struct {
	Content      []Text
	Sections     Sections
	ToolsAdded   []ToolDecl
	ToolsRemoved []ToolRef
	Timestamp    int64
}

// UserMessage is input from the user. A plain string is accepted on input and
// becomes one text block.
type UserMessage struct {
	Content   []UserBlock
	Timestamp int64
}

// AssistantMessage is one model response. Pointer fields are absent when nil.
type AssistantMessage struct {
	Content               []AssistantBlock
	API                   string
	Provider              string
	Model                 string
	ResponseModel         *string
	ResponseID            *string
	ProviderThinkingLevel *string
	ThinkingLevel         *ThinkingLevel
	Diagnostics           []Diagnostic
	Usage                 Usage
	StopReason            StopReason
	ErrorMessage          *string
	RawStopReason         *string
	EndTurn               *bool
	Timestamp             int64
}

// ToolResultMessage is the result of one tool call. NestedCalls is kept in the
// log and never sent to the model.
type ToolResultMessage struct {
	ToolCallID  string
	ToolName    string
	Content     []UserBlock
	Details     json.RawMessage
	Usage       *Usage
	NestedCalls *NestedToolCalls
	IsError     bool
	Timestamp   int64
}

// RawMessage holds a message of a role that has no registered decoder. Data is
// the complete JSON object and re-encodes unchanged.
type RawMessage struct {
	RoleName string
	Data     json.RawMessage
}

// Role returns "system".
func (SystemMessage) Role() string { return RoleSystem }

// Role returns "user".
func (UserMessage) Role() string { return RoleUser }

// Role returns "assistant".
func (AssistantMessage) Role() string { return RoleAssistant }

// Role returns "toolResult".
func (ToolResultMessage) Role() string { return RoleToolResult }

// Role returns the role name found in the data.
func (m RawMessage) Role() string { return m.RoleName }

// Diagnostic is a redacted provider or runtime note about a failure or recovery.
type Diagnostic struct {
	Type      string           `json:"type"`
	Timestamp int64            `json:"timestamp"`
	Error     *DiagnosticError `json:"error,omitempty"`
	Details   json.RawMessage  `json:"details,omitempty"`
}

// DiagnosticError describes an error inside a Diagnostic. Code is absent, a
// JSON string or a finite JSON number, and keeps its original form.
type DiagnosticError struct {
	Name    *string         `json:"name,omitempty"`
	Message string          `json:"message"`
	Stack   *string         `json:"stack,omitempty"`
	Code    json.RawMessage `json:"code,omitempty"`
}

func (e DiagnosticError) validate() error {
	if len(e.Code) == 0 {
		return nil
	}
	t := bytes.TrimSpace(e.Code)
	if len(t) == 0 {
		return errors.New("protocol: diagnostic code is empty")
	}
	if t[0] == '"' {
		if !json.Valid(t) {
			return errors.New("protocol: diagnostic code is not valid JSON")
		}
		return nil
	}
	f, err := strconv.ParseFloat(string(t), 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return errors.New("protocol: diagnostic code must be a string or a finite number")
	}
	return nil
}

// MarshalJSON encodes the error and checks the code form.
func (e DiagnosticError) MarshalJSON() ([]byte, error) {
	if err := e.validate(); err != nil {
		return nil, err
	}
	type plain DiagnosticError
	return marshalJSON(plain(e))
}

// UnmarshalJSON decodes the error and checks the code form. A null code is absent.
func (e *DiagnosticError) UnmarshalJSON(b []byte) error {
	type plain DiagnosticError
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	if isJSONNull(p.Code) {
		p.Code = nil
	}
	out := DiagnosticError(p)
	if err := out.validate(); err != nil {
		return err
	}
	*e = out
	return nil
}

// MarshalJSON encodes the diagnostic. Details must be a JSON object.
func (d Diagnostic) MarshalJSON() ([]byte, error) {
	if len(d.Details) > 0 && !isJSONObject(d.Details) {
		return nil, errors.New("protocol: diagnostic details are not a JSON object")
	}
	type plain Diagnostic
	return marshalJSON(plain(d))
}

// UnmarshalJSON decodes the diagnostic. A null details value is absent.
func (d *Diagnostic) UnmarshalJSON(b []byte) error {
	type plain Diagnostic
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	if isJSONNull(p.Details) {
		p.Details = nil
	} else if len(p.Details) > 0 && !isJSONObject(p.Details) {
		return errors.New("protocol: diagnostic details are not a JSON object")
	}
	*d = Diagnostic(p)
	return nil
}

// Nested call status values.
const (
	NestedStatusOK         = "ok"
	NestedStatusError      = "error"
	NestedStatusUnfinished = "unfinished"
)

// NestedToolCallRecord records a tool call that another tool made. A nil
// Arguments means the arguments were omitted (see ArgumentsBytes); an empty
// object `{}` is kept as supplied.
type NestedToolCallRecord struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Arguments      json.RawMessage `json:"arguments,omitempty"`
	ArgumentsBytes *int64          `json:"argumentsBytes,omitempty"`
	Status         string          `json:"status"`
	DurationMs     *float64        `json:"durationMs,omitempty"`
	Error          *string         `json:"error,omitempty"`
}

func (r NestedToolCallRecord) validate() error {
	switch r.Status {
	case NestedStatusOK, NestedStatusError, NestedStatusUnfinished:
	default:
		return fmt.Errorf("protocol: nested call %q has invalid status %q", r.ID, r.Status)
	}
	if len(r.Arguments) > 0 && !isJSONObject(r.Arguments) {
		return fmt.Errorf("protocol: nested call %q arguments are not a JSON object", r.ID)
	}
	return nil
}

// MarshalJSON encodes the record and checks status and arguments.
func (r NestedToolCallRecord) MarshalJSON() ([]byte, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	type plain NestedToolCallRecord
	return marshalJSON(plain(r))
}

// UnmarshalJSON decodes the record and checks status and arguments.
func (r *NestedToolCallRecord) UnmarshalJSON(b []byte) error {
	type plain NestedToolCallRecord
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	if isJSONNull(p.Arguments) {
		p.Arguments = nil
	}
	out := NestedToolCallRecord(p)
	if err := out.validate(); err != nil {
		return err
	}
	*r = out
	return nil
}

// NestedToolCalls is a bounded record of the calls a tool made to other tools.
type NestedToolCalls struct {
	Calls    []NestedToolCallRecord `json:"calls"`
	Complete bool                   `json:"complete"`
}

// MarshalJSON encodes the record; nil Calls becomes [].
func (n NestedToolCalls) MarshalJSON() ([]byte, error) {
	if n.Calls == nil {
		n.Calls = []NestedToolCallRecord{}
	}
	type plain NestedToolCalls
	return marshalJSON(plain(n))
}

// UnmarshalJSON decodes the record; absent calls become an empty slice.
func (n *NestedToolCalls) UnmarshalJSON(b []byte) error {
	type plain NestedToolCalls
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	if p.Calls == nil {
		p.Calls = []NestedToolCallRecord{}
	}
	*n = NestedToolCalls(p)
	return nil
}

// ToolExecutionResult is the final or partial result of a running tool.
type ToolExecutionResult struct {
	Content           []UserBlock
	Details           json.RawMessage
	StructuredContent json.RawMessage
	Usage             *Usage
	IsError           *bool
	Terminate         *bool
}

type toolExecutionResultWire struct {
	Content           json.RawMessage `json:"content"`
	Details           json.RawMessage `json:"details,omitempty"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	Usage             *Usage          `json:"usage,omitempty"`
	IsError           *bool           `json:"isError,omitempty"`
	Terminate         *bool           `json:"terminate,omitempty"`
}

// MarshalJSON encodes the result; nil Content becomes [].
func (r ToolExecutionResult) MarshalJSON() ([]byte, error) {
	content, err := marshalBlocks(r.Content)
	if err != nil {
		return nil, err
	}
	return marshalJSON(toolExecutionResultWire{content, r.Details, r.StructuredContent, r.Usage, r.IsError, r.Terminate})
}

// UnmarshalJSON decodes the result; absent or null content becomes an empty slice.
func (r *ToolExecutionResult) UnmarshalJSON(b []byte) error {
	var w toolExecutionResultWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	content, err := decodeUserBlocks(w.Content, false)
	if err != nil {
		return err
	}
	*r = ToolExecutionResult{content, w.Details, w.StructuredContent, w.Usage, w.IsError, w.Terminate}
	return nil
}

// --- JSON codec of the four built-in messages ---

// checkRole returns an error when the decoded role is not the wanted one.
func checkRole(got, want string) error {
	if got != want {
		return fmt.Errorf("protocol: role %q is not %q", got, want)
	}
	return nil
}

type systemWire struct {
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	Sections     Sections        `json:"sections,omitempty"`
	ToolsAdded   []ToolDecl      `json:"toolsAdded,omitempty"`
	ToolsRemoved []ToolRef       `json:"toolsRemoved,omitempty"`
	Timestamp    int64           `json:"timestamp"`
}

// MarshalJSON encodes the message with role "system".
func (m SystemMessage) MarshalJSON() ([]byte, error) {
	content, err := marshalBlocks(m.Content)
	if err != nil {
		return nil, err
	}
	return marshalJSON(systemWire{RoleSystem, content, m.Sections, m.ToolsAdded, m.ToolsRemoved, m.Timestamp})
}

// UnmarshalJSON decodes a system message. String content becomes one text block.
func (m *SystemMessage) UnmarshalJSON(b []byte) error {
	var w systemWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	if err := checkRole(w.Role, RoleSystem); err != nil {
		return err
	}
	content, err := decodeTexts(w.Content)
	if err != nil {
		return err
	}
	*m = SystemMessage{content, w.Sections, w.ToolsAdded, w.ToolsRemoved, w.Timestamp}
	return nil
}

type userWire struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Timestamp int64           `json:"timestamp"`
}

// MarshalJSON encodes the message with role "user".
func (m UserMessage) MarshalJSON() ([]byte, error) {
	content, err := marshalBlocks(m.Content)
	if err != nil {
		return nil, err
	}
	return marshalJSON(userWire{RoleUser, content, m.Timestamp})
}

// UnmarshalJSON decodes a user message. String content becomes one text block.
func (m *UserMessage) UnmarshalJSON(b []byte) error {
	var w userWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	if err := checkRole(w.Role, RoleUser); err != nil {
		return err
	}
	content, err := decodeUserBlocks(w.Content, true)
	if err != nil {
		return err
	}
	*m = UserMessage{content, w.Timestamp}
	return nil
}

type assistantWire struct {
	Role                  string          `json:"role"`
	Content               json.RawMessage `json:"content"`
	API                   string          `json:"api"`
	Provider              string          `json:"provider"`
	Model                 string          `json:"model"`
	ResponseModel         *string         `json:"responseModel,omitempty"`
	ResponseID            *string         `json:"responseId,omitempty"`
	ProviderThinkingLevel *string         `json:"providerThinkingLevel,omitempty"`
	ThinkingLevel         *ThinkingLevel  `json:"thinkingLevel,omitempty"`
	Diagnostics           []Diagnostic    `json:"diagnostics,omitempty"`
	Usage                 Usage           `json:"usage"`
	StopReason            StopReason      `json:"stopReason"`
	ErrorMessage          *string         `json:"errorMessage,omitempty"`
	RawStopReason         *string         `json:"rawStopReason,omitempty"`
	EndTurn               *bool           `json:"endTurn,omitempty"`
	Timestamp             int64           `json:"timestamp"`
}

// MarshalJSON encodes the message with role "assistant". The stop reason must
// be one of the known values.
func (m AssistantMessage) MarshalJSON() ([]byte, error) {
	if !m.StopReason.valid() {
		return nil, fmt.Errorf("protocol: invalid stop reason %q", m.StopReason)
	}
	content, err := marshalBlocks(m.Content)
	if err != nil {
		return nil, err
	}
	return marshalJSON(assistantWire{
		RoleAssistant, content, m.API, m.Provider, m.Model, m.ResponseModel, m.ResponseID,
		m.ProviderThinkingLevel, m.ThinkingLevel, m.Diagnostics, m.Usage, m.StopReason,
		m.ErrorMessage, m.RawStopReason, m.EndTurn, m.Timestamp,
	})
}

// UnmarshalJSON decodes an assistant message. Unknown or missing stop reasons
// are errors, and so are blocks that an assistant cannot hold (such as images).
func (m *AssistantMessage) UnmarshalJSON(b []byte) error {
	var w assistantWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	if err := checkRole(w.Role, RoleAssistant); err != nil {
		return err
	}
	if !w.StopReason.valid() {
		return fmt.Errorf("protocol: invalid stop reason %q", w.StopReason)
	}
	content, err := decodeAssistantBlocks(w.Content)
	if err != nil {
		return err
	}
	*m = AssistantMessage{
		content, w.API, w.Provider, w.Model, w.ResponseModel, w.ResponseID,
		w.ProviderThinkingLevel, w.ThinkingLevel, w.Diagnostics, w.Usage, w.StopReason,
		w.ErrorMessage, w.RawStopReason, w.EndTurn, w.Timestamp,
	}
	return nil
}

type toolResultWire struct {
	Role        string           `json:"role"`
	ToolCallID  string           `json:"toolCallId"`
	ToolName    string           `json:"toolName"`
	Content     json.RawMessage  `json:"content"`
	Details     json.RawMessage  `json:"details,omitempty"`
	Usage       *Usage           `json:"usage,omitempty"`
	NestedCalls *NestedToolCalls `json:"nestedCalls,omitempty"`
	IsError     bool             `json:"isError"`
	Timestamp   int64            `json:"timestamp"`
}

// MarshalJSON encodes the message with role "toolResult".
func (m ToolResultMessage) MarshalJSON() ([]byte, error) {
	content, err := marshalBlocks(m.Content)
	if err != nil {
		return nil, err
	}
	return marshalJSON(toolResultWire{
		RoleToolResult, m.ToolCallID, m.ToolName, content, m.Details, m.Usage, m.NestedCalls, m.IsError, m.Timestamp,
	})
}

// UnmarshalJSON decodes a tool result. Content must be an array (or absent).
func (m *ToolResultMessage) UnmarshalJSON(b []byte) error {
	var w toolResultWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	if err := checkRole(w.Role, RoleToolResult); err != nil {
		return err
	}
	content, err := decodeUserBlocks(w.Content, false)
	if err != nil {
		return err
	}
	*m = ToolResultMessage{
		w.ToolCallID, w.ToolName, content, w.Details, w.Usage, w.NestedCalls, w.IsError, w.Timestamp,
	}
	return nil
}

// --- role registry ---

var roles = struct {
	sync.RWMutex
	decoders map[string]func([]byte) (Message, error)
}{decoders: map[string]func([]byte) (Message, error){}}

func reservedRole(name string) bool {
	switch name {
	case RoleSystem, RoleUser, RoleAssistant, RoleToolResult:
		return true
	}
	return false
}

// RegisterRole installs a decoder for a custom role. It rejects an empty name,
// a built-in role, a duplicate, and a nil decoder. It is safe to call while
// other goroutines decode, but register during setup so that decoding is
// stable. The decoder receives its own copy of the JSON object.
func RegisterRole(name string, decode func([]byte) (Message, error)) error {
	if name == "" {
		return errors.New("protocol: role name is empty")
	}
	if decode == nil {
		return errors.New("protocol: role decoder is nil")
	}
	if reservedRole(name) {
		return fmt.Errorf("protocol: role %q is reserved", name)
	}
	roles.Lock()
	defer roles.Unlock()
	if _, dup := roles.decoders[name]; dup {
		return fmt.Errorf("protocol: role %q is already registered", name)
	}
	roles.decoders[name] = decode
	return nil
}

// MarshalMessage encodes a message as one JSON object. A RawMessage writes its
// data unchanged. A custom message type is encoded with encoding/json and must
// include its own "role" field.
func MarshalMessage(m Message) ([]byte, error) {
	switch v := m.(type) {
	case nil:
		return nil, errors.New("protocol: message is nil")
	case SystemMessage:
		return v.MarshalJSON()
	case *SystemMessage:
		return v.MarshalJSON()
	case UserMessage:
		return v.MarshalJSON()
	case *UserMessage:
		return v.MarshalJSON()
	case AssistantMessage:
		return v.MarshalJSON()
	case *AssistantMessage:
		return v.MarshalJSON()
	case ToolResultMessage:
		return v.MarshalJSON()
	case *ToolResultMessage:
		return v.MarshalJSON()
	case RawMessage:
		return marshalRawMessage(v)
	case *RawMessage:
		return marshalRawMessage(*v)
	default:
		return marshalJSON(m)
	}
}

func marshalRawMessage(m RawMessage) ([]byte, error) {
	if !isJSONObject(m.Data) {
		return nil, fmt.Errorf("protocol: raw message %q data is not a JSON object", m.RoleName)
	}
	return compactObject(m.Data)
}

// UnmarshalMessage decodes one JSON object by its "role" field. An unknown
// role without a registered decoder becomes a RawMessage. Invalid content in a
// known role is an error.
func UnmarshalMessage(data []byte) (Message, error) {
	var head struct {
		Role *string `json:"role"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	if head.Role == nil || *head.Role == "" {
		return nil, errors.New("protocol: message has no role")
	}
	switch *head.Role {
	case RoleSystem:
		var m SystemMessage
		if err := m.UnmarshalJSON(data); err != nil {
			return nil, err
		}
		return m, nil
	case RoleUser:
		var m UserMessage
		if err := m.UnmarshalJSON(data); err != nil {
			return nil, err
		}
		return m, nil
	case RoleAssistant:
		var m AssistantMessage
		if err := m.UnmarshalJSON(data); err != nil {
			return nil, err
		}
		return m, nil
	case RoleToolResult:
		var m ToolResultMessage
		if err := m.UnmarshalJSON(data); err != nil {
			return nil, err
		}
		return m, nil
	}
	own := append([]byte(nil), bytes.TrimSpace(data)...)
	roles.RLock()
	decode := roles.decoders[*head.Role]
	roles.RUnlock()
	if decode != nil {
		return decode(own)
	}
	return RawMessage{RoleName: *head.Role, Data: own}, nil
}

// --- deep copies ---

// Clone returns a deep copy: slices, pointers and raw JSON are not shared.
func (m AssistantMessage) Clone() AssistantMessage {
	m.Content = cloneAssistantBlocks(m.Content)
	m.ResponseModel = clonePtr(m.ResponseModel)
	m.ResponseID = clonePtr(m.ResponseID)
	m.ProviderThinkingLevel = clonePtr(m.ProviderThinkingLevel)
	m.ThinkingLevel = clonePtr(m.ThinkingLevel)
	m.Diagnostics = cloneDiagnostics(m.Diagnostics)
	m.Usage = m.Usage.Clone()
	m.ErrorMessage = clonePtr(m.ErrorMessage)
	m.RawStopReason = clonePtr(m.RawStopReason)
	m.EndTurn = clonePtr(m.EndTurn)
	return m
}

func cloneDiagnostics(in []Diagnostic) []Diagnostic {
	if in == nil {
		return nil
	}
	out := make([]Diagnostic, len(in))
	for i, d := range in {
		d.Details = cloneRaw(d.Details)
		if d.Error != nil {
			e := *d.Error
			e.Name = clonePtr(e.Name)
			e.Stack = clonePtr(e.Stack)
			e.Code = cloneRaw(e.Code)
			d.Error = &e
		}
		out[i] = d
	}
	return out
}

func cloneNestedCalls(n *NestedToolCalls) *NestedToolCalls {
	if n == nil {
		return nil
	}
	out := &NestedToolCalls{Complete: n.Complete}
	if n.Calls != nil {
		out.Calls = make([]NestedToolCallRecord, len(n.Calls))
		for i, c := range n.Calls {
			c.Arguments = cloneRaw(c.Arguments)
			c.ArgumentsBytes = clonePtr(c.ArgumentsBytes)
			c.DurationMs = clonePtr(c.DurationMs)
			c.Error = clonePtr(c.Error)
			out.Calls[i] = c
		}
	}
	return out
}

// CloneMessage returns a deep copy of a built-in message. A pointer message
// gives a pointer to the copy, and a value message gives a value. A custom message
// type is returned as is, because its fields are not known here.
func CloneMessage(m Message) Message {
	switch v := m.(type) {
	case SystemMessage:
		return v.clone()
	case *SystemMessage:
		if v == nil {
			return m
		}
		c := v.clone()
		return &c
	case UserMessage:
		v.Content = cloneUserBlocks(v.Content)
		return v
	case *UserMessage:
		if v == nil {
			return m
		}
		c := *v
		c.Content = cloneUserBlocks(c.Content)
		return &c
	case AssistantMessage:
		return v.Clone()
	case *AssistantMessage:
		if v == nil {
			return m
		}
		c := v.Clone()
		return &c
	case ToolResultMessage:
		return v.clone()
	case *ToolResultMessage:
		if v == nil {
			return m
		}
		c := v.clone()
		return &c
	case RawMessage:
		v.Data = cloneRaw(v.Data)
		return v
	case *RawMessage:
		if v == nil {
			return m
		}
		c := *v
		c.Data = cloneRaw(c.Data)
		return &c
	default:
		return m
	}
}

func (m SystemMessage) clone() SystemMessage {
	if m.Content != nil {
		c := make([]Text, len(m.Content))
		for i, t := range m.Content {
			c[i] = cloneText(t)
		}
		m.Content = c
	}
	if m.Sections != nil {
		s := make(Sections, len(m.Sections))
		for i, sec := range m.Sections {
			sec.Value = clonePtr(sec.Value)
			s[i] = sec
		}
		m.Sections = s
	}
	m.ToolsAdded = cloneToolDecls(m.ToolsAdded)
	if m.ToolsRemoved != nil {
		m.ToolsRemoved = append([]ToolRef{}, m.ToolsRemoved...)
	}
	return m
}

func (m ToolResultMessage) clone() ToolResultMessage {
	m.Content = cloneUserBlocks(m.Content)
	m.Details = cloneRaw(m.Details)
	if m.Usage != nil {
		u := m.Usage.Clone()
		m.Usage = &u
	}
	m.NestedCalls = cloneNestedCalls(m.NestedCalls)
	return m
}
