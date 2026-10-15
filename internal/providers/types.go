package providers

import (
	"context"

	"AskCore/pkg/protocol"
)

// Request is the raw input of one model call: the system prompt and the tool
// declarations travel next to the message log.
type Request struct {
	SystemPrompt string
	Messages     []protocol.Message
	Tools        []protocol.ToolDecl
}

// TranscriptRequest is the provider-facing form of a Request. The system
// prompt and the tools are already part of a leading system message (see
// NormalizeRequest).
type TranscriptRequest struct {
	Messages []protocol.Message
}

// Cache retention values for StreamOptions.CacheRetention.
const (
	CacheRetentionShort = "short"
	CacheRetentionLong  = "long"
	CacheRetentionNone  = "none"
)

// StreamOptions are the per-call options of a stream. Cancel a request with
// the context that goes with the call.
type StreamOptions struct {
	SessionID string
	// CacheRetention is "", "short", "long" or "none". Empty means the
	// provider default.
	CacheRetention string
	// MaxTokens limits the output. Zero means the model default.
	MaxTokens int
	// Temperature is optional. Nil means the provider default.
	Temperature *float64
	Reasoning   protocol.ThinkingLevel
	APIKey      string
	// ToolChoice selects one declared tool by its canonical name.
	ToolChoice string
	// Auth is a request-local typed credential. Its zero value preserves legacy APIKey callers.
	Auth AuthSnapshot
	// Prepared holds the effective values of the request (see Registry.Prepare).
	// An adapter sends exactly these values. When it is nil, the adapter
	// computes them itself.
	Prepared *Prepared
	// RequireBinding pins the login method, profile and billing class for retries.
	RequireBinding *AuthBinding
}

// StreamFn starts one model call. It never fails at call time: a failure is a
// terminal error event of the returned stream.
type StreamFn func(ctx context.Context, m Model, req TranscriptRequest, opts StreamOptions) *Stream

// Provider is an endpoint of a model host that speaks one API.
type Provider interface {
	// API returns the id of the wire protocol that the provider speaks.
	API() string
	// Stream has the same contract as StreamFn.
	Stream(ctx context.Context, m Model, req TranscriptRequest, opts StreamOptions) *Stream
}

// StreamItem is one delivery of a stream: a provider event and a copy of the
// latest usage at the time of the event. The copy shares no memory with the
// producer, so a consumer can keep it.
type StreamItem struct {
	Event protocol.AssistantMessageEvent
	Usage protocol.Usage
}
