package tuiclient

import (
	"context"
	"errors"

	"AskCore/internal/tui"
	"AskCore/pkg/protocol"
)

// ErrTrustNotAvailable means that the agent host does not serve the project
// trust methods: an older leader, or a host that has no trust store. The UI
// then shows no trust prompt; it is not an error to show. It is the error of
// the UI contract, so that the UI can test it with errors.Is.
var ErrTrustNotAvailable = tui.ErrTrustNotAvailable

// TrustState asks the host for the trust decision of cwd, as a session that
// starts now would get it from a client that can ask the user.
func (c *Client) TrustState(ctx context.Context, cwd string) (protocol.ACPTrustStateResult, error) {
	var out protocol.ACPTrustStateResult
	conn, err := c.live()
	if err != nil {
		return out, err
	}
	err = trustError(c.request(ctx, conn, protocol.ACPTrustState, protocol.ACPTrustStateRequest{Cwd: cwd}, &out))
	return out, err
}

// SetTrust saves a trust decision on the host and returns the new state.
func (c *Client) SetTrust(ctx context.Context, req protocol.ACPTrustSetRequest) (protocol.ACPTrustStateResult, error) {
	var out protocol.ACPTrustStateResult
	conn, err := c.live()
	if err != nil {
		return out, err
	}
	err = trustError(c.request(ctx, conn, protocol.ACPTrustSet, req, &out))
	return out, err
}

// trustError maps the answer of a host without the trust methods to
// ErrTrustNotAvailable. A leader answers unsupported; a host without the
// method answers method not found.
func trustError(err error) error {
	var re *tui.RequestError
	if errors.As(err, &re) && (re.Kind == string(protocol.ACPErrUnsupported) || re.Code == -32601) {
		return ErrTrustNotAvailable
	}
	return err
}
