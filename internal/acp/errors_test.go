package acp

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"AskCore/internal/agent"
	"AskCore/pkg/protocol"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

func TestAdapterErrorMapperKinds(t *testing.T) {
	secret := "sk-secret-token-value"
	cases := []struct {
		name string
		err  error
		kind protocol.ACPErrorKind
	}{
		{"busy", fmt.Errorf("wrap %s: %w", secret, agent.ErrBusy), protocol.ACPErrBusy},
		{"disposed", agent.ErrDisposed, protocol.ACPErrDisposed},
		{"host closed", ErrHostClosed, protocol.ACPErrDisposed},
		{"no key", agent.ErrNoAPIKey, protocol.ACPErrNoAPIKey},
		{"auth", fmt.Errorf("%w: %s", ErrAuth, secret), protocol.ACPErrNoAPIKey},
		{"queue full", agent.ErrQueueFull, protocol.ACPErrQueueFull},
		{"no input", agent.ErrNoInput, protocol.ACPErrInvalidContent},
		{"continue empty", agent.ErrContinueEmpty, protocol.ACPErrInvalidState},
		{"continue tail", agent.ErrContinueFromAssistant, protocol.ACPErrInvalidState},
		{"cancelled", context.Canceled, protocol.ACPErrCancelled},
		{"overflow", ErrQueueOverflow, protocol.ACPErrOutputFailure},
		{"output", fmt.Errorf("%w: %w", ErrOutputFailed, errors.New(secret)), protocol.ACPErrOutputFailure},
		{"unknown session", errUnknownSession, protocol.ACPErrUnknownSession},
		{"not initialized", errNotInitialized, protocol.ACPErrNotInitialized},
		{"unsupported", errUnsupported, protocol.ACPErrUnsupported},
		{"internal", errors.New("provider said " + secret), protocol.ACPErrInternal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			re := requestError(c.err)
			require.NotNil(t, re)
			require.Equal(t, protocol.ACPErrorCode(c.kind), re.Code)
			require.Equal(t, protocol.ACPErrorData{Kind: c.kind}, re.Data)
			require.NotContains(t, re.Message, secret)
			require.NotContains(t, re.Error(), secret)
		})
	}
}

func TestAdapterErrorMapperDetailAndPassthrough(t *testing.T) {
	re := requestError(newKindError(protocol.ACPErrInvalidParams, "mcp servers are not supported"))
	require.Contains(t, re.Message, "mcp servers are not supported")
	require.Equal(t, protocol.ACPErrorData{Kind: protocol.ACPErrInvalidParams}, re.Data)
	nf := sdk.NewMethodNotFound("_ask/session/nope")
	require.Same(t, nf, requestError(nf))
	require.Nil(t, requestError(nil))
}
