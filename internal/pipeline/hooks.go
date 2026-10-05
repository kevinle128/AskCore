package pipeline

import (
	"context"
	"encoding/json"

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

// TurnDecision is the answer of FinishTurn.
type TurnDecision uint8

const (
	// Proceed keeps the normal scheduling.
	Proceed TurnDecision = iota
	// Continue makes sure one more model request happens. Tool results,
	// steering or follow-up messages satisfy it; otherwise the loop sends
	// one request with the current context.
	Continue
	// End ends the run after turn_end without polling any queue.
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
	Model   providers.Model
	// ThinkingLevel is "off" when the request asks for no reasoning.
	ThinkingLevel protocol.ThinkingLevel
}

// RequestUpdate replaces parts of the run state for this request and every
// later one. Nil and empty fields keep the current value.
type RequestUpdate struct {
	Context *AgentContext
	Model   *providers.Model
	// ThinkingLevel "off" turns reasoning off.
	ThinkingLevel protocol.ThinkingLevel
}

// ToolCallInfo describes one tool call to the tool hooks. Call holds the raw
// arguments the model sent; Args holds the prepared and validated ones.
type ToolCallInfo struct {
	AssistantMessage protocol.AssistantMessage
	Call             protocol.ToolCall
	Args             json.RawMessage
	Context          AgentContext
}

// ToolResultInfo is a tool call that executed, with its result before
// AfterToolCall changes it.
type ToolResultInfo struct {
	ToolCallInfo
	Result  protocol.ToolExecutionResult
	IsError bool
}

// BeforeToolCallResult is the answer of BeforeToolCall. Nil lets the call run.
type BeforeToolCallResult struct {
	// Block stops the call. The model gets an error result with Reason, or
	// "Tool execution was blocked" when Reason is empty.
	Block  bool
	Reason string
	// Terminate asks to stop after the batch when the call is blocked. The
	// batch stops only when every result in it asks to.
	Terminate bool
	// Args, when not nil, replaces the arguments that Execute sees. They are
	// not validated again.
	Args json.RawMessage
}

// AfterToolCallResult overrides fields of an executed result. A nil field
// keeps the executed value; an empty non-nil Content replaces it. Replacing
// Content without StructuredContent drops the structured content, because it
// may no longer match.
type AfterToolCallResult struct {
	Content           []protocol.UserBlock
	Details           json.RawMessage
	StructuredContent json.RawMessage
	IsError           *bool
	Usage             *protocol.Usage
	Terminate         *bool
}

// Apply returns res with the overrides of r and the new error flag. A nil r
// returns the input unchanged. The loop and Compose both use it, so the
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

// Hooks are the hook points of the agent loop, one function per point. A nil
// field gives the default behavior.
//
// An error from TransformContext, ConvertToLLM, GetAPIKey, PrepareRequest,
// FinishTurn, GetSteeringMessages or GetFollowUpMessages ends the run: the
// loop returns it and emits nothing more. An error or a panic in
// BeforeToolCall or AfterToolCall becomes an error result of that tool call.
type Hooks struct {
	// TransformContext changes the messages of one request (pruning,
	// injection). The result is never stored.
	TransformContext func(ctx context.Context, msgs []protocol.Message) ([]protocol.Message, error)
	// ConvertToLLM maps the log to model messages. Default:
	// providers.ConvertToLLM.
	ConvertToLLM func(msgs []protocol.Message) ([]protocol.Message, error)
	// GetAPIKey resolves the key before each request. An empty key falls
	// back to the configured one.
	GetAPIKey func(ctx context.Context, provider string) (string, error)
	// PrepareRequest runs before every request, after the pending messages
	// were appended. It does not poll the queues.
	PrepareRequest func(ctx context.Context, r Request) (*RequestUpdate, error)
	// FinishTurn runs after the tool results and before turn_end, also for
	// an error or aborted message, where its decision is ignored.
	FinishTurn func(ctx context.Context, t Turn) (TurnDecision, error)
	// BeforeToolCall runs after validation and can block the call.
	BeforeToolCall func(ctx context.Context, c ToolCallInfo) (*BeforeToolCallResult, error)
	// AfterToolCall runs only for calls that executed.
	AfterToolCall func(ctx context.Context, r ToolResultInfo) (*AfterToolCallResult, error)
	// GetSteeringMessages returns messages to add before the next request:
	// at run start, after each normal turn, and before a later turn when
	// the earlier poll was empty.
	GetSteeringMessages func(ctx context.Context) ([]protocol.Message, error)
	// GetFollowUpMessages returns messages to run when the agent would stop.
	GetFollowUpMessages func(ctx context.Context) ([]protocol.Message, error)
}
