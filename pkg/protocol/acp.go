package protocol

import (
	"encoding/json"
	"fmt"
)

// Ask ACP methods extend the standard agent protocol without changing its frames.
const (
	ACPState       = "_ask/session/state"
	ACPModels      = "_ask/session/get_available_models"
	ACPSetModel    = "_ask/session/set_model"
	ACPSetThinking = "_ask/session/set_thinking"
	ACPContinue    = "_ask/session/continue"
	ACPReset       = "_ask/session/reset"
	ACPSteer       = "_ask/session/steer"
	ACPFollowUp    = "_ask/session/follow_up"
	ACPRemove      = "_ask/session/remove"
	ACPFollow      = "_ask/session/follow"
	ACPUnfollow    = "_ask/session/unfollow"
	ACPUsage       = "_ask/session/usage"
	ACPEvent       = "_ask/session/event"
	ACPResync      = "_ask/session/resync"

	// Live session membership on a shared leader. They never read durable storage.
	ACPListLive = "_ask/session/list_live"
	ACPAttach   = "_ask/session/attach"
	ACPDetach   = "_ask/session/detach"
	ACPTake     = "_ask/session/take"

	// Owners of these three are not available yet. The host answers them with
	// the unsupported error and never lists them as a capability.
	ACPCompact = "_ask/session/compact"
	ACPFork    = "_ask/session/fork"
	ACPTree    = "_ask/session/tree"
)

// ACPCursor encodes the sequence as decimal text to preserve all uint64 values.
type ACPCursor struct {
	Epoch string `json:"epoch"`
	Seq   uint64 `json:"seq,string"`
}

// ACPStreamBaseline preserves the raw bytes of unfinished content blocks.
type ACPStreamBaseline struct {
	AttemptID string           `json:"attemptId"`
	Seq       uint64           `json:"seq,string"`
	Message   AssistantMessage `json:"message"`
	Open      map[int][]byte   `json:"open"`
}

// ACPSessionRequest identifies a session for a read or lifecycle operation.
type ACPSessionRequest struct {
	SessionID string `json:"sessionId"`
}

// ACPModelRequest constrains the existing host auth method when supplied.
type ACPModelRequest struct {
	SessionID    string `json:"sessionId"`
	ModelID      string `json:"modelId"`
	AuthMethodID string `json:"authMethodId,omitempty"`
}

type ACPThinkingRequest struct {
	SessionID string        `json:"sessionId"`
	Level     ThinkingLevel `json:"level"`
}

// ACPContentBlock is one block of a steer or follow-up message. It uses the
// ACP block names and fields for the two kinds that Ask can carry.
type ACPContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// ACPInputRequest holds the content of a queued input message.
type ACPInputRequest struct {
	SessionID string            `json:"sessionId"`
	Content   []ACPContentBlock `json:"content"`
}

// UserBlocks converts the wire blocks. It rejects an empty list, an unknown
// block type and an empty field, so the Agent never gets an empty message.
func (r ACPInputRequest) UserBlocks() ([]UserBlock, error) {
	if len(r.Content) == 0 {
		return nil, fmt.Errorf("content is empty")
	}
	out := make([]UserBlock, 0, len(r.Content))
	for i, b := range r.Content {
		switch b.Type {
		case "text":
			if b.Text == "" {
				return nil, fmt.Errorf("content[%d]: text is empty", i)
			}
			out = append(out, Text{Text: b.Text})
		case "image":
			if b.Data == "" || b.MimeType == "" {
				return nil, fmt.Errorf("content[%d]: image needs data and mimeType", i)
			}
			out = append(out, Image{Data: b.Data, MimeType: b.MimeType})
		default:
			return nil, fmt.Errorf("content[%d]: unsupported block type %q", i, b.Type)
		}
	}
	return out, nil
}

type ACPInputResult struct {
	InputID string `json:"inputId"`
}

type ACPRemoveRequest struct {
	SessionID string `json:"sessionId"`
	InputID   string `json:"inputId"`
}

type ACPRemoveResult struct {
	Removed bool `json:"removed"`
}

type ACPFollowRequest struct {
	SessionID string     `json:"sessionId"`
	Cursor    *ACPCursor `json:"cursor,omitempty"`
}

type ACPUnfollowRequest struct {
	SessionID      string `json:"sessionId"`
	SubscriptionID string `json:"subscriptionId"`
}

// ACPFollowResult is written before the subscription's live events.
// Entries hold safe session projections, never private request or auth data.
type ACPFollowResult struct {
	SubscriptionID string             `json:"subscriptionId"`
	Cursor         ACPCursor          `json:"cursor"`
	Entries        []json.RawMessage  `json:"entries,omitempty"`
	Stream         *ACPStreamBaseline `json:"stream,omitempty"`
	Resumed        bool               `json:"resumed"`
	Resync         bool               `json:"resync"`
}

// ACPEventNotification preserves the source cursor independently of derived frames.
// CycleID and AttemptID are set when the event body has them. A turn has no
// ID of its own: it is named by its cycle and its position in the sequence.
type ACPEventNotification struct {
	SubscriptionID string          `json:"subscriptionId"`
	SessionID      string          `json:"sessionId"`
	Epoch          string          `json:"epoch"`
	Seq            uint64          `json:"seq,string"`
	RunID          string          `json:"runId,omitempty"`
	CycleID        string          `json:"cycleId,omitempty"`
	AttemptID      string          `json:"attemptId,omitempty"`
	Event          json.RawMessage `json:"event"`
}

type ACPResyncNotification struct {
	SubscriptionID string    `json:"subscriptionId"`
	SessionID      string    `json:"sessionId"`
	Cursor         ACPCursor `json:"cursor"`
	Reason         string    `json:"reason"`
}

// ACPStateResult is the safe copy of the current session state.
type ACPStateResult struct {
	SessionID     string        `json:"sessionId"`
	Epoch         string        `json:"epoch"`
	Running       bool          `json:"running"`
	ModelID       string        `json:"modelId"`
	ThinkingLevel ThinkingLevel `json:"thinkingLevel"`
	MessageCount  int           `json:"messageCount"`
	Steering      []string      `json:"steering"`
	FollowUp      []string      `json:"followUp"`
}

// ACPModel is one catalog row. ModelID is "provider/id@api" and names one row,
// because two rows can share provider and id and differ in the wire API.
type ACPModel struct {
	ModelID       string   `json:"modelId"`
	Provider      string   `json:"provider"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	API           string   `json:"api"`
	Reasoning     bool     `json:"reasoning"`
	Input         []string `json:"input"`
	ContextWindow int      `json:"contextWindow"`
	MaxTokens     int      `json:"maxTokens"`
}

type ACPModelsResult struct {
	Current string     `json:"current"`
	Models  []ACPModel `json:"models"`
}

type ACPSetModelResult struct {
	ModelID       string        `json:"modelId"`
	ThinkingLevel ThinkingLevel `json:"thinkingLevel"`
}

type ACPThinkingResult struct {
	Level ThinkingLevel `json:"level"`
}

// ACPContinueResult is sent after the continued run settles.
type ACPContinueResult struct {
	StopReason string `json:"stopReason"`
}

// ACPResetResult names the fresh epoch of the same session.
type ACPResetResult struct {
	SessionID string `json:"sessionId"`
	Epoch     string `json:"epoch"`
}

// ACPUsageRow is the usage of one model attempt. A nil Usage means the
// provider reported none for that attempt.
type ACPUsageRow struct {
	AttemptID string `json:"attemptId"`
	CycleID   string `json:"cycleId,omitempty"`
	Outcome   string `json:"outcome"`
	Usage     *Usage `json:"usage,omitempty"`
}

// ACPUsageResult lists the attempts and the sum of the known values.
// Complete is false when any attempt has no reported usage, so Total is then a lower bound.
type ACPUsageResult struct {
	Attempts []ACPUsageRow `json:"attempts"`
	Total    Usage         `json:"total"`
	Complete bool          `json:"complete"`
}

// ACPErrorKind names a failure once for every method.
type ACPErrorKind string

const (
	ACPErrUnknownSession ACPErrorKind = "unknown_session"
	// ACPErrUnknownSubscription means no follow subscription has the ID.
	ACPErrUnknownSubscription ACPErrorKind = "unknown_subscription"
	ACPErrNotInitialized      ACPErrorKind = "not_initialized"
	ACPErrBusy                ACPErrorKind = "busy"
	ACPErrDisposed            ACPErrorKind = "disposed"
	ACPErrNoAPIKey            ACPErrorKind = "no_api_key"
	ACPErrQueueFull           ACPErrorKind = "queue_full"
	ACPErrInvalidModel        ACPErrorKind = "invalid_model"
	ACPErrInvalidContent      ACPErrorKind = "invalid_content"
	ACPErrInvalidParams       ACPErrorKind = "invalid_params"
	ACPErrInvalidState        ACPErrorKind = "invalid_state"
	ACPErrCancelled           ACPErrorKind = "cancelled"
	ACPErrUnsupported         ACPErrorKind = "unsupported"
	ACPErrOutputFailure       ACPErrorKind = "output_failure"
	ACPErrInternal            ACPErrorKind = "internal"

	// ACPErrNotDriver means the caller is not the driver of the session.
	ACPErrNotDriver ACPErrorKind = "not_driver"
	// ACPErrClientNotInitialized means this client has not run initialize on the leader.
	ACPErrClientNotInitialized ACPErrorKind = "client_not_initialized"
)

// Ask-only codes use the reserved implementation range and avoid the codes
// that the ACP schema already defines (-32000 and -32002).
const (
	ACPCodeBusy          = -32010
	ACPCodeDisposed      = -32011
	ACPCodeQueueFull     = -32012
	ACPCodeOutputFailure = -32013
	ACPCodeInvalidState  = -32014

	// Leader codes: the caller is not the driver, or has not initialized on this link.
	ACPCodeNotDriver            = -32015
	ACPCodeNotInitializedClient = -32016
)

// ACPRouteMetaKey is the _meta key of the route context. Only the leader router
// writes it, and it replaces any value a client sends.
const ACPRouteMetaKey = "ask.dev/route"

// ACPRouteMeta is the route context the router adds to a forwarded request.
// DriverGen is decimal text because SDK _meta numbers decode as float64.
type ACPRouteMeta struct {
	ClientID string `json:"clientId"`
	// SessionID is the session that the router checked the caller against. The
	// host refuses a call whose own session id differs from it.
	SessionID    string          `json:"sessionId,omitempty"`
	DriverGen    uint64          `json:"driverGen,string"`
	LiveDriver   bool            `json:"liveDriver,omitempty"`
	Capabilities json.RawMessage `json:"capabilities,omitempty"`
}

// ACPErrorCode returns the JSON-RPC code of a kind. An unknown kind is an internal error.
func ACPErrorCode(k ACPErrorKind) int {
	switch k {
	case ACPErrUnknownSession:
		return -32002
	case ACPErrNotInitialized:
		return -32600
	case ACPErrBusy:
		return ACPCodeBusy
	case ACPErrDisposed:
		return ACPCodeDisposed
	case ACPErrNoAPIKey:
		return -32000
	case ACPErrQueueFull:
		return ACPCodeQueueFull
	case ACPErrInvalidModel, ACPErrInvalidContent, ACPErrInvalidParams, ACPErrUnknownSubscription:
		return -32602
	case ACPErrInvalidState:
		return ACPCodeInvalidState
	case ACPErrCancelled:
		return -32800
	case ACPErrUnsupported:
		return -32601
	case ACPErrOutputFailure:
		return ACPCodeOutputFailure
	case ACPErrNotDriver:
		return ACPCodeNotDriver
	case ACPErrClientNotInitialized:
		return ACPCodeNotInitializedClient
	default:
		return -32603
	}
}

// ACPErrorMessage returns the fixed text that goes with a kind. It never holds
// provider text, a path or a credential fact. The no-key text names the host
// command that signs in, because the protocol connection never asks for a
// secret.
func ACPErrorMessage(k ACPErrorKind) string {
	switch k {
	case ACPErrUnknownSession:
		return "unknown session"
	case ACPErrUnknownSubscription:
		return "unknown subscription"
	case ACPErrNotInitialized:
		return "initialize the connection first"
	case ACPErrBusy:
		return "the session is busy"
	case ACPErrDisposed:
		return "the session is closed"
	case ACPErrNoAPIKey:
		return "not signed in for this model; run `ask auth` on the host and retry"
	case ACPErrQueueFull:
		return "the input queue is full"
	case ACPErrInvalidModel:
		return "unknown or ambiguous model"
	case ACPErrInvalidContent:
		return "unsupported or empty content"
	case ACPErrInvalidParams:
		return "invalid parameters"
	case ACPErrInvalidState:
		return "the session state does not allow this call"
	case ACPErrCancelled:
		return "the request was cancelled"
	case ACPErrUnsupported:
		return "not supported"
	case ACPErrOutputFailure:
		return "the output of the connection failed"
	case ACPErrNotDriver:
		return "only the driver of the session can do this; take the session first"
	case ACPErrClientNotInitialized:
		return "initialize this connection first"
	default:
		return "internal error"
	}
}

// ACPErrorData is the only error data Ask sends. It holds no provider text and no credential facts.
type ACPErrorData struct {
	Kind ACPErrorKind `json:"kind"`
}

// Roles of a client in a live session.
const (
	ACPRoleDriver   = "driver"
	ACPRoleObserver = "observer"
)

// ACPTakeResult names the driver generation that the host committed.
// DriverGen is decimal text, like the other Ask counters.
type ACPTakeResult struct {
	SessionID string `json:"sessionId"`
	DriverGen uint64 `json:"driverGen,string"`
}

// ACPAttachResult is the answer to a live attach.
type ACPAttachResult struct {
	SessionID string `json:"sessionId"`
	Role      string `json:"role"`
}

// ACPLiveSession is one row of the live session list. Role is the role of the caller.
type ACPLiveSession struct {
	SessionID   string `json:"sessionId"`
	Cwd         string `json:"cwd"`
	Role        string `json:"role,omitempty"`
	HasDriver   bool   `json:"hasDriver"`
	Subscribers int    `json:"subscribers"`
}

// ACPListLiveResult lists the sessions that live in the running leader.
type ACPListLiveResult struct {
	Sessions []ACPLiveSession `json:"sessions"`
}
