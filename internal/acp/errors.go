package acp

import (
	"context"
	"errors"

	"AskCore/internal/agent"
	"AskCore/pkg/protocol"

	sdk "github.com/coder/acp-go-sdk"
)

// Sentinels that app callbacks and the adapter use to name a failure kind.
// The mapper never sends the text of any other error.
var (
	// ErrAuth means the configured credential is missing or does not match the
	// requested method. An app callback wraps its own error with it.
	ErrAuth = errors.New("acp: authentication required")
	// ErrOutputFailed marks a failure of the connection output.
	ErrOutputFailed = errors.New("acp: output failed")

	errUnknownSession = errors.New("acp: unknown session")
	errUnknownSub     = errors.New("acp: unknown subscription")
	errNotInitialized = errors.New("acp: not initialized")
	errUnsupported    = errors.New("acp: unsupported")
)

// kindError is an error that names its kind and a fixed detail text that the
// adapter wrote. The detail never holds input of the client.
type kindError struct {
	kind   protocol.ACPErrorKind
	detail string
}

func (e *kindError) Error() string { return string(e.kind) + ": " + e.detail }

func newKindError(kind protocol.ACPErrorKind, detail string) error {
	return &kindError{kind: kind, detail: detail}
}

// classify returns the kind of err.
func classify(err error) protocol.ACPErrorKind {
	var ke *kindError
	switch {
	case errors.As(err, &ke):
		return ke.kind
	case errors.Is(err, errUnknownSession):
		return protocol.ACPErrUnknownSession
	case errors.Is(err, errUnknownSub):
		return protocol.ACPErrUnknownSubscription
	case errors.Is(err, errNotInitialized):
		return protocol.ACPErrNotInitialized
	case errors.Is(err, errUnsupported):
		return protocol.ACPErrUnsupported
	case errors.Is(err, ErrOutputFailed), errors.Is(err, ErrQueueOverflow), errors.Is(err, agent.ErrOutputFailure):
		return protocol.ACPErrOutputFailure
	case errors.Is(err, agent.ErrBusy):
		return protocol.ACPErrBusy
	case errors.Is(err, agent.ErrDisposed), errors.Is(err, ErrHostClosed):
		return protocol.ACPErrDisposed
	case errors.Is(err, agent.ErrNoAPIKey), errors.Is(err, ErrAuth):
		return protocol.ACPErrNoAPIKey
	case errors.Is(err, agent.ErrQueueFull):
		return protocol.ACPErrQueueFull
	case errors.Is(err, agent.ErrNoInput):
		return protocol.ACPErrInvalidContent
	case errors.Is(err, agent.ErrContinueEmpty), errors.Is(err, agent.ErrContinueFromAssistant):
		return protocol.ACPErrInvalidState
	case errors.Is(err, context.Canceled):
		return protocol.ACPErrCancelled
	}
	return protocol.ACPErrInternal
}

// requestError maps err to the one error frame that Ask sends. Method-not-found
// errors of the SDK pass through, because they name only the method. Every
// other error becomes a fixed text and a kind: the SDK would put the text of an
// unmapped error into the frame.
func requestError(err error) *sdk.RequestError {
	if err == nil {
		return nil
	}
	var re *sdk.RequestError
	if errors.As(err, &re) && re.Code == -32601 {
		return re
	}
	kind := classify(err)
	msg := protocol.ACPErrorMessage(kind)
	var ke *kindError
	if errors.As(err, &ke) && ke.detail != "" {
		msg += ": " + ke.detail
	}
	return &sdk.RequestError{Code: protocol.ACPErrorCode(kind), Message: msg, Data: protocol.ACPErrorData{Kind: kind}}
}

// runError returns the error of a run that settled with a failed cycle. The
// Agent reports a model failure as an ended run, not as an error of the call,
// but a client needs a failure frame. The text has the failure code only.
func runError(res Result) error {
	if res.Reason != "error" {
		return nil
	}
	if res.Code == "AUTH" {
		return ErrAuth
	}
	return newKindError(protocol.ACPErrInternal, "model request failed: "+safeCode(res.Code))
}

// safeCode returns code when it is a short upper-case word, else UNKNOWN.
func safeCode(code string) string {
	if code == "" || len(code) > 32 {
		return "UNKNOWN"
	}
	for _, r := range code {
		if (r < 'A' || r > 'Z') && r != '_' {
			return "UNKNOWN"
		}
	}
	return code
}
