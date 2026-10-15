package tuiclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"AskCore/internal/tui"
	"AskCore/internal/tuiclient/rpc"
	"AskCore/pkg/protocol"
)

// releaseTimeout bounds the cancels that Close and Detach send for logins that
// are still open. The leader also cancels the logins of a client that is gone,
// so a cancel that does not arrive in time costs nothing.
const releaseTimeout = 2 * time.Second

// providerConn returns the connection for a provider call. In process it
// returns ErrNeedsLeader before anything is sent: only the leader serves them.
func (c *Client) providerConn() (*rpc.Conn, error) {
	if err := c.needsLeader(); err != nil {
		return nil, err
	}
	return c.live()
}

// trackLogin remembers an open login id, so Close and Detach can cancel it.
func (c *Client) trackLogin(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.logins == nil {
		c.logins = map[string]struct{}{}
	}
	c.logins[id] = struct{}{}
}

func (c *Client) untrackLogin(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.logins, id)
}

// Providers lists the login options and the providers generation.
func (c *Client) Providers(ctx context.Context) (protocol.ACPProvidersResult, error) {
	var out protocol.ACPProvidersResult
	conn, err := c.providerConn()
	if err != nil {
		return out, err
	}
	err = c.request(ctx, conn, protocol.ACPProvidersList, map[string]any{}, &out)
	return out, err
}

// SaveAPIKey sends the key once with the login id. The call may last as long as
// the key check of the provider, so the caller picks the context.
func (c *Client) SaveAPIKey(ctx context.Context, loginID, provider, key string) (protocol.ACPLoginOutcome, error) {
	var out protocol.ACPLoginOutcome
	conn, err := c.providerConn()
	if err != nil {
		return out, err
	}
	c.trackLogin(loginID)
	defer c.untrackLogin(loginID)
	err = c.secretRequest(ctx, conn, protocol.ACPProvidersAPIKey, key,
		protocol.ACPAPIKeyRequest{LoginID: loginID, ProviderID: provider, Key: key}, &out)
	return out, err
}

// StartLogin opens a browser, address or device sign-in. The login stays open
// until WaitLogin or CancelLogin ends it, or the link is lost.
func (c *Client) StartLogin(ctx context.Context, provider, method string) (protocol.ACPLoginStartResult, error) {
	var out protocol.ACPLoginStartResult
	conn, err := c.providerConn()
	if err != nil {
		return out, err
	}
	if err := c.request(ctx, conn, protocol.ACPLoginStart,
		protocol.ACPLoginStartRequest{ProviderID: provider, MethodID: method}, &out); err != nil {
		return protocol.ACPLoginStartResult{}, err
	}
	if out.LoginID != "" {
		c.trackLogin(out.LoginID)
	}
	return out, nil
}

// SendLoginCode gives a pasted code or callback address to a sign-in.
func (c *Client) SendLoginCode(ctx context.Context, loginID, code string) error {
	conn, err := c.providerConn()
	if err != nil {
		return err
	}
	return c.secretRequest(ctx, conn, protocol.ACPLoginInput, code,
		protocol.ACPLoginInputRequest{LoginID: loginID, Code: code}, nil)
}

// WaitLogin blocks until the sign-in ends. Ending ctx stops the wait and leaves
// the sign-in open; CancelLogin ends it. A lost link gives ErrOutcomeUnknown
// and nothing is sent again.
func (c *Client) WaitLogin(ctx context.Context, loginID string) (protocol.ACPLoginOutcome, error) {
	var out protocol.ACPLoginOutcome
	conn, err := c.providerConn()
	if err != nil {
		return out, err
	}
	err = c.request(ctx, conn, protocol.ACPLoginWait, protocol.ACPLoginIDRequest{LoginID: loginID}, &out)
	if ctx.Err() == nil {
		// The sign-in ended, or the leader does not know it, or the link broke:
		// no cancel is left to send.
		c.untrackLogin(loginID)
	}
	return out, err
}

// CancelLogin stops a sign-in or a key check.
func (c *Client) CancelLogin(ctx context.Context, loginID string) (bool, error) {
	conn, err := c.providerConn()
	if err != nil {
		return false, err
	}
	var out protocol.ACPLoginCancelResult
	err = c.request(ctx, conn, protocol.ACPLoginCancel, protocol.ACPLoginIDRequest{LoginID: loginID}, &out)
	c.untrackLogin(loginID)
	return out.Cancelled, err
}

// Logout removes the saved credential of one login option. The current session,
// when there is one, is named so that the leader can report its cleared model.
func (c *Client) Logout(ctx context.Context, provider, method string) (protocol.ACPLogoutResult, error) {
	var out protocol.ACPLogoutResult
	conn, err := c.providerConn()
	if err != nil {
		return out, err
	}
	req := protocol.ACPLogoutRequest{ProviderID: provider, MethodID: method}
	c.mu.Lock()
	if c.sess.has && !c.sess.gone {
		req.SessionID = c.sess.id
	}
	c.mu.Unlock()
	err = c.request(ctx, conn, protocol.ACPProvidersLogout, req, &out)
	return out, err
}

// secretRequest sends a call that carries a secret. No error of it holds the
// secret: an answer that cannot be read gets a fixed text, and the text of a
// refusal or a transport error is scrubbed of the value.
func (c *Client) secretRequest(ctx context.Context, conn *rpc.Conn, method, secret string, params, out any) error {
	res, err := conn.Call(ctx, method, params)
	if err != nil {
		return scrubSecret(translate(err), secret)
	}
	if out != nil {
		if json.Unmarshal(res, out) != nil {
			return fmt.Errorf("tuiclient: %s: the answer is not valid", method)
		}
	}
	return nil
}

// scrubSecret removes secret from the text of err. The sentinel errors of the
// client and the context errors have fixed text and pass as they are.
func scrubSecret(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}
	var re *tui.RequestError
	switch {
	case errors.As(err, &re):
		cp := *re
		cp.Message = strings.ReplaceAll(re.Message, secret, "")
		return &cp
	case errors.Is(err, tui.ErrOutcomeUnknown), errors.Is(err, tui.ErrDisconnected),
		errors.Is(err, tui.ErrClosed), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case strings.Contains(err.Error(), secret):
		return errors.New("tuiclient: the call failed")
	}
	return err
}

// releaseLogins sends one cancel for each login that is still open. It is best
// effort: the leader cancels the logins of a gone client by itself.
func (c *Client) releaseLogins() {
	c.mu.Lock()
	var ids []string
	for id := range c.logins {
		ids = append(ids, id)
	}
	c.logins = nil
	conn, ok := c.conn, c.connected && !c.closed && c.info.Mode == tui.ModeLeader
	c.mu.Unlock()
	if !ok || conn == nil || len(ids) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
	defer cancel()
	for _, id := range ids {
		_, _ = conn.Call(ctx, protocol.ACPLoginCancel, protocol.ACPLoginIDRequest{LoginID: id})
	}
}
