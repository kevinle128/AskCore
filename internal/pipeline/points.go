package pipeline

import (
	"bytes"
	"encoding/json"
	"time"

	"AskCore/internal/providers"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// AgentContext is what one run of the loop works on: the message log the
// model sees and the tools it can execute.
type AgentContext struct {
	Messages []protocol.Message
	// Tools is nil when the run has no tools; every tool call then fails as
	// not found.
	Tools *tools.Registry
}

// TurnDecision is the answer of a decision point (CompleteStep, StopTurn).
type TurnDecision uint8

const (
	// Proceed keeps the normal scheduling.
	Proceed TurnDecision = iota
	// Continue makes sure one more model request happens. Tool results,
	// steering or follow-up messages satisfy it; otherwise the loop sends
	// one request with the current context.
	Continue
	// End ends the run after turn_end without claiming from any queue. Input
	// that is queued stays queued and starts no run; the next run delivers it
	// (the Pi rule; DeepSeek continues while input is pending).
	End
)

// Turn is a completed turn: the assistant message, its tool results, the
// context after both were appended, and the messages the run has added so far.
type Turn struct {
	Message     protocol.AssistantMessage
	ToolResults []protocol.ToolResultMessage
	Context     AgentContext
	NewMessages []protocol.Message
}

// Request is the state right before a model request.
type Request struct {
	Context AgentContext
	// StagedMessages are admitted input not yet committed to history.
	StagedMessages []protocol.Message
	Model          providers.Model
	// ThinkingLevel is "off" when the request asks for no reasoning.
	ThinkingLevel protocol.ThinkingLevel
}

// RequestUpdate replaces parts of the run state for this request and every
// later one. Nil and empty fields keep the current value.
type RequestUpdate struct {
	Context *AgentContext
	// RequestMessages replaces the messages of this request only: the run
	// context and the stored log keep their own. Nil keeps the context
	// messages.
	RequestMessages []protocol.Message
	Model           *providers.Model
	// ThinkingLevel "off" turns reasoning off.
	ThinkingLevel protocol.ThinkingLevel
}

// ToolCallInfo describes one tool call to the tool points. Call holds the raw
// arguments the model sent. Args holds the validated, coerced arguments that
// the tool runs with. Every value is a copy for this dispatch, including the
// tool-call arguments inside AssistantMessage and Context: a handler that edits
// bytes changes nothing outside its own view.
type ToolCallInfo struct {
	AssistantMessage protocol.AssistantMessage
	Call             protocol.ToolCall
	Args             json.RawMessage
	Context          AgentContext
}

// ToolResultInfo is a tool call that executed, with its result before
// AfterTool changes it.
type ToolResultInfo struct {
	ToolCallInfo
	Result  protocol.ToolExecutionResult
	IsError bool
}

// BeforeToolCallResult is the answer of BeforeTool. Nil allows the call. A
// handler cannot change the arguments: they are validated once and frozen, and
// Deny is the way to refuse a call.
type BeforeToolCallResult struct {
	// Block denies the call. The model gets an error result with Reason, or
	// "Tool execution was blocked" when Reason is empty.
	Block  bool
	Reason string
	// Terminate asks to stop after the batch when the call is blocked. The
	// batch stops only when every result in it asks to.
	Terminate bool
	// Cancel cancels the call before dispatch: the model gets the error result
	// "Tool call aborted before dispatch" and the body does not run. Cancel
	// wins over Block.
	Cancel bool
}

// AfterToolCallResult overrides fields of an executed result. A nil field
// keeps the executed value; an empty non-nil Content replaces it. Replacing
// Content without StructuredContent drops the structured content, because it
// may no longer match.
type AfterToolCallResult struct {
	// AddedContext is user input for the next turn. It does not wake an idle agent.
	AddedContext      []protocol.UserMessage
	Content           []protocol.UserBlock
	Details           json.RawMessage
	StructuredContent json.RawMessage
	IsError           *bool
	Usage             *protocol.Usage
	Terminate         *bool
}

// Clone returns a deep copy of the override and its added context.
// A nil override stays nil.
func (r *AfterToolCallResult) Clone() *AfterToolCallResult {
	if r == nil {
		return nil
	}
	out := *r
	out.Content = protocol.CloneMessage(protocol.UserMessage{Content: r.Content}).(protocol.UserMessage).Content
	out.Details = bytes.Clone(r.Details)
	out.StructuredContent = bytes.Clone(r.StructuredContent)
	if r.IsError != nil {
		value := *r.IsError
		out.IsError = &value
	}
	if r.Terminate != nil {
		value := *r.Terminate
		out.Terminate = &value
	}
	if r.Usage != nil {
		usage := r.Usage.Clone()
		out.Usage = &usage
	}
	if r.AddedContext != nil {
		out.AddedContext = make([]protocol.UserMessage, len(r.AddedContext))
		for i, message := range r.AddedContext {
			out.AddedContext[i] = protocol.CloneMessage(message).(protocol.UserMessage)
		}
	}
	return &out
}

// Apply returns res with the overrides of r and the new error flag. A nil r
// returns the input unchanged. Handlers and the loop both use it, so the
// override rule has one copy.
func (r *AfterToolCallResult) Apply(res protocol.ToolExecutionResult, isError bool) (protocol.ToolExecutionResult, bool) {
	if r == nil {
		return res, isError
	}
	if r.StructuredContent != nil {
		res.StructuredContent = r.StructuredContent
	} else if r.Content != nil {
		res.StructuredContent = nil
	}
	if r.Content != nil {
		res.Content = r.Content
	}
	if r.Details != nil {
		res.Details = r.Details
	}
	if r.Usage != nil {
		res.Usage = r.Usage
	}
	if r.Terminate != nil {
		res.Terminate = r.Terminate
	}
	if r.IsError != nil {
		isError = *r.IsError
	}
	return res, isError
}

// ModelCall is one model request as ExecuteModel handlers see it. The request is
// frozen at dispatch: it is the one that the request log recorded. A handler
// gets its own copy, so an edit of Request, or a different Request passed to
// next, does not reach the provider. The terminal sends the frozen request with
// the model and options that the credential was resolved for, so a handler
// cannot move a key to another host. A handler may wrap the call, for example
// to time it, or return its own valid stream without calling next.
type ModelCall struct {
	Model   providers.Model
	Request providers.TranscriptRequest
}

// ExecuteToolInput is one tool body call as ExecuteTool handlers see it. The
// tool itself stays with the loop; a handler can wrap, replace or skip the
// body, never swap the tool. The arguments and Context.Cwd are frozen: the
// terminal ignores what a handler passes on for them. A handler may wrap
// Context.Update.
type ExecuteToolInput struct {
	Call    protocol.ToolCall
	Args    json.RawMessage
	Context tools.Context
}

// AdmitInput is a proposed turn: the messages reserved for it. The loop claims
// the messages before it dispatches the point, so input that arrives while a
// handler runs is not part of them. A turn that continues after tool results
// has no messages. Messages are copies for this dispatch.
type AdmitInput struct {
	// CycleID names the input cycle that the turn belongs to.
	CycleID string
	// Turn counts the turns of the cycle from 1, this one included.
	Turn     int
	Messages []protocol.Message
}

// AdmitDecision is the answer of AdmitStep. The zero value of Reject enters
// the turn with Messages. A handler that rewrites Messages to none for the
// first turn of a cycle ends the cycle with no turn.
type AdmitDecision struct {
	// Messages are the messages that enter the turn.
	Messages []protocol.Message
	// Reject refuses the turn: the claimed messages are acknowledged as rejected
	// and the cycle ends with reason blocked.
	Reject bool
}

// RecoverInput is one failed model request that is eligible for recovery.
type RecoverInput struct {
	Failure  error
	Provider string
	Policy   providers.RetryPolicy
	Retry    int
}

// RecoverAction is the answer of RecoverModel. The zero value stops with the
// original failure.
type RecoverAction struct {
	// Retry asks for another model request.
	Retry bool
	Delay time.Duration
}

// StopInput is the state when a cycle has no required turn work left.
type StopInput struct {
	Context     AgentContext
	NewMessages []protocol.Message
}

// ModelOutcome is the settled answer of a complete model attempt.
// Failure is a provider error; Go errors from ExecuteModel are handler failures.
type ModelOutcome struct {
	Message protocol.AssistantMessage
	Failure error
	Binding providers.AuthBinding
}

// Clone keeps outcome messages private to each handler.
func (o *ModelOutcome) Clone() *ModelOutcome {
	if o == nil {
		return nil
	}
	out := *o
	out.Message = o.Message.Clone()
	return &out
}
