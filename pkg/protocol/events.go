package protocol

import "encoding/json"

// Discriminator values of agent events.
const (
	TypeAgentStart          = "agent_start"
	TypeAgentEnd            = "agent_end"
	TypeTurnStart           = "turn_start"
	TypeTurnEnd             = "turn_end"
	TypeMessageStart        = "message_start"
	TypeMessageUpdate       = "message_update"
	TypeMessageEnd          = "message_end"
	TypeToolExecutionStart  = "tool_execution_start"
	TypeToolExecutionUpdate = "tool_execution_update"
	TypeToolExecutionEnd    = "tool_execution_end"
)

// Envelope is the sequencing data around every agent event. On the wire it is
// flat: seq, ts, sessionId and runId sit at the same level as the event
// fields and the "type" key.
type Envelope struct {
	Seq       uint64
	TS        int64
	SessionID string
	RunID     string
}

// Env returns the envelope so that an emitter can fill it.
func (e *Envelope) Env() *Envelope { return e }

// Event is one agent event with its envelope. The event structs use pointer
// receivers for Env, so pass and receive them as pointers (for example
// *MessageEnd).
type Event interface {
	EventType() string
	Env() *Envelope
}

// AgentStart opens a run.
type AgentStart struct{ Envelope }

// AgentEnd closes a run and lists the messages that the run added.
type AgentEnd struct {
	Envelope
	Messages []Message
}

// TurnStart opens one model response and its tool calls.
type TurnStart struct{ Envelope }

// TurnEnd closes a turn.
type TurnEnd struct {
	Envelope
	Message     Message
	ToolResults []ToolResultMessage
}

// MessageStart opens a message. For an assistant message it holds the seed.
type MessageStart struct {
	Envelope
	Message Message
}

// MessageUpdate carries one block event of the assistant message that is
// streaming, and the latest usage. It never holds start, done or error.
type MessageUpdate struct {
	Envelope
	AssistantMessageEvent BlockEvent
	Usage                 Usage
}

// MessageEnd closes a message and holds the final authoritative value.
type MessageEnd struct {
	Envelope
	Message Message
}

// ToolExecutionStart reports that a tool began to run.
type ToolExecutionStart struct {
	Envelope
	ToolCallID string
	ToolName   string
	Args       json.RawMessage
}

// ToolExecutionUpdate carries a partial tool result.
type ToolExecutionUpdate struct {
	Envelope
	ToolCallID    string
	ToolName      string
	Args          json.RawMessage
	PartialResult ToolExecutionResult
}

// ToolExecutionEnd reports the final tool result.
type ToolExecutionEnd struct {
	Envelope
	ToolCallID string
	ToolName   string
	Result     ToolExecutionResult
	IsError    bool
}

// RawEvent holds an event of a type that this package does not know, for
// example a session-level event. Data is the complete JSON object, envelope
// included. On encode, the Envelope and Type fields replace the envelope keys
// in Data, so an emitter can set seq and runId; the other keys are kept.
type RawEvent struct {
	Envelope
	Type string
	Data json.RawMessage
}

// EventType returns the discriminator.
func (*AgentStart) EventType() string          { return TypeAgentStart }
func (*AgentEnd) EventType() string            { return TypeAgentEnd }
func (*TurnStart) EventType() string           { return TypeTurnStart }
func (*TurnEnd) EventType() string             { return TypeTurnEnd }
func (*MessageStart) EventType() string        { return TypeMessageStart }
func (*MessageUpdate) EventType() string       { return TypeMessageUpdate }
func (*MessageEnd) EventType() string          { return TypeMessageEnd }
func (*ToolExecutionStart) EventType() string  { return TypeToolExecutionStart }
func (*ToolExecutionUpdate) EventType() string { return TypeToolExecutionUpdate }
func (*ToolExecutionEnd) EventType() string    { return TypeToolExecutionEnd }
func (e *RawEvent) EventType() string          { return e.Type }

// envelopeWire holds the flat envelope keys. The wire structs embed it so the
// keys stay at the top level of the object.
type envelopeWire struct {
	Seq       uint64 `json:"seq"`
	TS        int64  `json:"ts"`
	SessionID string `json:"sessionId"`
	RunID     string `json:"runId"`
	Type      string `json:"type"`
}

func newEnvelopeWire(e *Envelope, typ string) envelopeWire {
	return envelopeWire{Seq: e.Seq, TS: e.TS, SessionID: e.SessionID, RunID: e.RunID, Type: typ}
}

func (w envelopeWire) envelope() Envelope {
	return Envelope{Seq: w.Seq, TS: w.TS, SessionID: w.SessionID, RunID: w.RunID}
}

// anyMessage adapts the Message interface to encoding/json.
type anyMessage struct{ m Message }

func (a anyMessage) MarshalJSON() ([]byte, error) { return MarshalMessage(a.m) }

func (a *anyMessage) UnmarshalJSON(b []byte) error {
	m, err := UnmarshalMessage(b)
	if err != nil {
		return err
	}
	a.m = m
	return nil
}
