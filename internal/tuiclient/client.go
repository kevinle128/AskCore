package tuiclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"AskCore/internal/leader"
	"AskCore/internal/tui"
	"AskCore/internal/tuiclient/rpc"
	"AskCore/pkg/protocol"
)

// Reconnect timing from the design: 1 s, doubling to 30 s, with no limit.
const (
	defaultInitialBackoff = time.Second
	defaultMaxBackoff     = 30 * time.Second
	// requestTimeout bounds the calls that the client makes by itself, such as
	// initialize and the restore after a reconnect.
	requestTimeout = 20 * time.Second
)

// Link is one connection to an agent: its transport and who is on the other end.
type Link struct {
	Transport rpc.Transport
	Info      tui.ConnInfo
}

// Config says how to reach the agent.
type Config struct {
	// Dial opens a link. The client calls it once at Open and again after every
	// lost link.
	Dial func(ctx context.Context) (Link, error)
	// NoReconnect says that a lost link ends the client and is not dialed again.
	// The in-process agent sets it: its link cannot be lost without the agent.
	NoReconnect bool
	// ClientName and Version go into the initialize request.
	ClientName string
	Version    string
	// InitialBackoff and MaxBackoff default to 1 s and 30 s.
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	// OnClose runs after Close shut the link, for example to stop an in-process
	// agent and wait for its run to settle.
	OnClose func() error
}

func (c Config) withDefaults() Config {
	if c.InitialBackoff <= 0 {
		c.InitialBackoff = defaultInitialBackoff
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = defaultMaxBackoff
	}
	if c.ClientName == "" {
		c.ClientName = "ask"
	}
	return c
}

// LeaderDial returns a Dial function for the leader of one Ask home. It starts
// a leader when none runs, as ConnectOrSpawn does.
func LeaderDial(cfg leader.ConnectConfig) func(context.Context) (Link, error) {
	return func(ctx context.Context) (Link, error) {
		cl, err := leader.ConnectOrSpawn(ctx, cfg)
		if err != nil {
			return Link{}, err
		}
		return LinkFromLeader(cl), nil
	}
}

// LinkFromLeader wraps a connection that completed the handshake.
func LinkFromLeader(cl *leader.Client) Link {
	return Link{
		Transport: rpc.NewLeaderTransport(cl),
		Info:      tui.ConnInfo{Mode: tui.ModeLeader, InstanceID: cl.Info.InstanceID, ClientID: cl.Info.ClientID, Build: cl.Info.Build},
	}
}

type sessionState struct {
	has  bool
	gone bool
	id   string
	cwd  string
	role string
	// sub is the id of the live follow subscription. It is empty between a
	// resync and the follow that replaces it, so late events are dropped.
	sub   string
	epoch string
	seq   uint64
}

type promptState struct {
	id       tui.PromptID
	accepted bool
}

// Client implements tui.Client over an rpc connection.
type Client struct {
	cfg    Config
	events *mailbox
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu         sync.Mutex
	conn       *rpc.Conn
	info       tui.ConnInfo
	connected  bool
	closed     bool
	sess       sessionState
	prompt     *promptState
	nextPrompt tui.PromptID
	closeOnce  sync.Once
	closeErr   error
}

var _ tui.Client = (*Client)(nil)

// Open reaches the agent once. It returns the dial or initialize error as it is,
// so the caller can pick another client at startup. After a successful Open the
// client never gives up on a leader: it reconnects until Close.
func Open(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Dial == nil {
		return nil, errors.New("tuiclient: Dial is required")
	}
	cfg = cfg.withDefaults()
	runCtx, cancel := context.WithCancel(context.Background())
	c := &Client{cfg: cfg, events: newMailbox(), ctx: runCtx, cancel: cancel}
	link, err := cfg.Dial(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	if err := c.establish(ctx, link); err != nil {
		cancel()
		return nil, err
	}
	c.goSafe("event queue", true, c.events.pump)
	c.goSafe("link supervisor", true, c.supervise)
	return c, nil
}

// Events delivers the messages in order. It closes after Close.
func (c *Client) Events() <-chan tui.Message { return c.events.out }

// Info describes the current link.
func (c *Client) Info() tui.ConnInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.info
}

// safely runs fn and turns a panic into a ClientErrorMsg. Every goroutine of the
// client runs through it, so a bug in a handler never crashes the process before
// the UI has restored the terminal.
func (c *Client) safely(where string, fatal bool, fn func()) {
	defer func() {
		if v := recover(); v != nil {
			c.events.push(tui.ClientErrorMsg{Err: rpc.Recover(where, v), Fatal: fatal})
		}
	}()
	fn()
}

// goSafe starts fn in a goroutine that Close waits for. It starts nothing after
// Close.
func (c *Client) goSafe(where string, fatal bool, fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.safely(where, fatal, fn)
	}()
}

// handler is the rpc handler of one connection.
type handler struct {
	c    *Client
	conn *rpc.Conn
}

// establish makes a dialed link the current connection: it starts the reader and
// runs initialize. It closes the link when initialize fails.
func (c *Client) establish(ctx context.Context, link Link) error {
	h := &handler{c: c}
	conn := rpc.NewConn(link.Transport, h)
	h.conn = conn
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = conn.Close()
		return tui.ErrClosed
	}
	c.wg.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.wg.Done()
		conn.Run()
	}()
	initCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	_, err := conn.Call(initCtx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{},
		"clientInfo":         map[string]any{"name": c.cfg.ClientName, "version": c.cfg.Version},
	})
	if err != nil {
		_ = conn.Close()
		<-conn.Done()
		return fmt.Errorf("tuiclient: initialize: %w", err)
	}
	c.mu.Lock()
	c.conn, c.info, c.connected = conn, link.Info, true
	c.mu.Unlock()
	c.events.push(tui.ConnectedMsg{Info: link.Info})
	return nil
}

// supervise watches the link. A lost link is reconnected without a limit, with
// the backoff of the design, and the session is restored. It never starts an
// agent of its own: only Open chooses the client.
func (c *Client) supervise() {
	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		select {
		case <-c.ctx.Done():
			return
		case <-conn.Done():
		}
		if c.ctx.Err() != nil {
			return
		}
		// A pending prompt reports its own end: its call fails on the lost link.
		c.mu.Lock()
		c.connected = false
		c.mu.Unlock()
		cause := conn.Err()
		var pe *rpc.PanicError
		if errors.As(cause, &pe) {
			c.events.push(tui.ClientErrorMsg{Err: pe})
		}
		if c.cfg.NoReconnect {
			c.events.push(tui.DisconnectedMsg{Err: cause})
			c.events.push(tui.ClientErrorMsg{Err: fmt.Errorf("tuiclient: the agent link ended: %v", cause), Fatal: true})
			return
		}
		c.events.push(tui.DisconnectedMsg{Err: cause})
		if !c.reconnect() {
			return
		}
	}
}

// reconnect dials until a link works or the client closes. It returns false
// after Close.
func (c *Client) reconnect() bool {
	wait := c.cfg.InitialBackoff
	for attempt := 1; ; attempt++ {
		link, err := c.cfg.Dial(c.ctx)
		if err == nil {
			err = c.establish(c.ctx, link)
		}
		if err == nil {
			break
		}
		if c.ctx.Err() != nil {
			return false
		}
		c.events.push(tui.ReconnectingMsg{Attempt: attempt, Wait: wait, Err: err})
		select {
		case <-c.ctx.Done():
			return false
		case <-time.After(wait):
		}
		wait = min(wait*2, c.cfg.MaxBackoff)
	}
	c.restore()
	return true
}

// restore puts the session back after a reconnect: attach, then follow from the
// last cursor. It sends nothing else, and never a prompt.
func (c *Client) restore() {
	c.mu.Lock()
	s, conn := c.sess, c.conn
	c.mu.Unlock()
	if !s.has || s.gone {
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, requestTimeout)
	defer cancel()
	live, err := c.listLive(ctx, conn)
	if err != nil {
		c.reportRestoreError(err)
		return
	}
	found := false
	for _, l := range live {
		if l.SessionID == s.id {
			found = true
		}
	}
	if !found {
		c.mu.Lock()
		c.sess.gone, c.sess.sub = true, ""
		c.mu.Unlock()
		c.events.push(tui.SessionGoneMsg{SessionID: s.id})
		return
	}
	res, err := conn.Call(ctx, protocol.ACPAttach, map[string]any{"sessionId": s.id})
	if err != nil {
		c.reportRestoreError(err)
		return
	}
	var att protocol.ACPAttachResult
	if json.Unmarshal(res, &att) != nil || att.SessionID == "" {
		c.reportRestoreError(errors.New("tuiclient: the attach answer is not valid"))
		return
	}
	role := att.Role
	if s.role == protocol.ACPRoleDriver && role != protocol.ACPRoleDriver {
		// The new link has a new client id. The old driver is gone, so a take
		// works when the session is idle; if it does not, the client observes.
		if _, terr := conn.Call(ctx, protocol.ACPTake, map[string]any{"sessionId": s.id}); terr == nil {
			role = protocol.ACPRoleDriver
		}
	}
	c.mu.Lock()
	c.sess.role, c.sess.sub = role, ""
	cursor := &protocol.ACPCursor{Epoch: c.sess.epoch, Seq: c.sess.seq}
	c.mu.Unlock()
	if cursor.Epoch == "" {
		cursor = nil
	}
	if err := c.follow(ctx, conn, s.id, cursor, followRestore); err != nil {
		c.reportRestoreError(err)
	}
}

func (c *Client) reportRestoreError(err error) {
	if errors.Is(err, rpc.ErrConnectionLost) || c.ctx.Err() != nil {
		return // the supervisor sees the lost link and tries again
	}
	c.events.push(tui.ClientErrorMsg{Err: fmt.Errorf("tuiclient: restore the session: %w", err)})
}

// ---- calls ----

// live returns the current connection, or the error that says why a call cannot
// be sent. Nothing is sent when it fails, so a retry is safe.
func (c *Client) live() (*rpc.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.closed:
		return nil, tui.ErrClosed
	case !c.connected:
		return nil, tui.ErrDisconnected
	}
	return c.conn, nil
}

// withSession returns the connection and the id of the current session.
func (c *Client) withSession() (*rpc.Conn, string, error) {
	conn, err := c.live()
	if err != nil {
		return nil, "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case !c.sess.has:
		return nil, "", tui.ErrNoSession
	case c.sess.gone:
		return nil, "", tui.ErrSessionGone
	}
	return conn, c.sess.id, nil
}

// needsLeader returns ErrNeedsLeader for an in-process agent.
func (c *Client) needsLeader() error {
	if c.Info().Mode != tui.ModeLeader {
		return tui.ErrNeedsLeader
	}
	return nil
}

// translate maps an rpc error to the error that the UI sees.
func translate(err error) error {
	var f *rpc.Fault
	switch {
	case err == nil:
		return nil
	case errors.As(err, &f):
		return &tui.RequestError{Code: f.Code, Kind: f.Kind, Message: f.Message}
	case errors.Is(err, rpc.ErrConnectionLost):
		return tui.ErrOutcomeUnknown
	}
	return err
}

// request sends one call and decodes the answer into out when out is not nil.
func (c *Client) request(ctx context.Context, conn *rpc.Conn, method string, params, out any) error {
	res, err := conn.Call(ctx, method, params)
	if err != nil {
		return translate(err)
	}
	if out != nil {
		if err := json.Unmarshal(res, out); err != nil {
			return fmt.Errorf("tuiclient: %s: the answer is not valid: %w", method, err)
		}
	}
	return nil
}

func (c *Client) listLive(ctx context.Context, conn *rpc.Conn) ([]protocol.ACPLiveSession, error) {
	var out protocol.ACPListLiveResult
	if err := c.request(ctx, conn, protocol.ACPListLive, map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Sessions, nil
}

// NewSession creates a session in cwd, makes it current and follows it.
func (c *Client) NewSession(ctx context.Context, cwd string) (tui.SessionInfo, error) {
	conn, err := c.live()
	if err != nil {
		return tui.SessionInfo{}, err
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return tui.SessionInfo{}, fmt.Errorf("tuiclient: working directory: %w", err)
	}
	var out struct {
		SessionID string `json:"sessionId"`
	}
	if err := c.request(ctx, conn, "session/new", map[string]any{"cwd": abs, "mcpServers": []any{}}, &out); err != nil {
		return tui.SessionInfo{}, err
	}
	if out.SessionID == "" {
		return tui.SessionInfo{}, errors.New("tuiclient: session/new gave no session id")
	}
	info := tui.SessionInfo{ID: out.SessionID, Role: protocol.ACPRoleDriver, Cwd: abs}
	c.setSession(info)
	if err := c.follow(ctx, conn, info.ID, nil, followFresh); err != nil {
		return info, err
	}
	return info, nil
}

// Attach joins a live session as an observer and follows it. When the call
// fails, the current session stays as it was.
func (c *Client) Attach(ctx context.Context, sessionID string) (tui.SessionInfo, error) {
	if err := c.needsLeader(); err != nil {
		return tui.SessionInfo{}, err
	}
	conn, err := c.live()
	if err != nil {
		return tui.SessionInfo{}, err
	}
	var out protocol.ACPAttachResult
	if err := c.request(ctx, conn, protocol.ACPAttach, map[string]any{"sessionId": sessionID}, &out); err != nil {
		return tui.SessionInfo{}, err
	}
	if out.SessionID == "" {
		return tui.SessionInfo{}, errors.New("tuiclient: attach gave no session id")
	}
	info := tui.SessionInfo{ID: out.SessionID, Role: out.Role}
	c.setSession(info)
	if err := c.follow(ctx, conn, info.ID, nil, followFresh); err != nil {
		return info, err
	}
	return info, nil
}

func (c *Client) setSession(info tui.SessionInfo) {
	c.mu.Lock()
	c.sess = sessionState{has: true, id: info.ID, cwd: info.Cwd, role: info.Role}
	c.mu.Unlock()
}

// Detach leaves the current session. Its run goes on.
func (c *Client) Detach(ctx context.Context) error {
	if err := c.needsLeader(); err != nil {
		return err
	}
	conn, sid, err := c.withSession()
	if err != nil {
		return err
	}
	if err := c.request(ctx, conn, protocol.ACPDetach, map[string]any{"sessionId": sid}, nil); err != nil {
		return err
	}
	c.mu.Lock()
	c.sess = sessionState{}
	c.mu.Unlock()
	return nil
}

// Take makes this client the driver of the current session.
func (c *Client) Take(ctx context.Context) (protocol.ACPTakeResult, error) {
	var out protocol.ACPTakeResult
	if err := c.needsLeader(); err != nil {
		return out, err
	}
	conn, sid, err := c.withSession()
	if err != nil {
		return out, err
	}
	if err := c.request(ctx, conn, protocol.ACPTake, map[string]any{"sessionId": sid}, &out); err != nil {
		return out, err
	}
	c.mu.Lock()
	if c.sess.id == sid {
		c.sess.role = protocol.ACPRoleDriver
	}
	c.mu.Unlock()
	return out, nil
}

// ListSessions lists the live sessions of the leader.
func (c *Client) ListSessions(ctx context.Context) ([]protocol.ACPLiveSession, error) {
	if err := c.needsLeader(); err != nil {
		return nil, err
	}
	conn, err := c.live()
	if err != nil {
		return nil, err
	}
	return c.listLive(ctx, conn)
}

// Cancel asks the agent to stop the current run.
func (c *Client) Cancel() error {
	conn, sid, err := c.withSession()
	if err != nil {
		return err
	}
	return translate(conn.Notify("session/cancel", map[string]any{"sessionId": sid}))
}

func textInput(sid, text string) protocol.ACPInputRequest {
	return protocol.ACPInputRequest{SessionID: sid, Content: []protocol.ACPContentBlock{{Type: "text", Text: text}}}
}

// Steer queues a message for the next turn of the run.
func (c *Client) Steer(ctx context.Context, text string) (string, error) {
	return c.enqueue(ctx, protocol.ACPSteer, text)
}

// FollowUp queues a message for after the run.
func (c *Client) FollowUp(ctx context.Context, text string) (string, error) {
	return c.enqueue(ctx, protocol.ACPFollowUp, text)
}

func (c *Client) enqueue(ctx context.Context, method, text string) (string, error) {
	conn, sid, err := c.withSession()
	if err != nil {
		return "", err
	}
	var out protocol.ACPInputResult
	if err := c.request(ctx, conn, method, textInput(sid, text), &out); err != nil {
		return "", err
	}
	return out.InputID, nil
}

// Remove takes a queued message out of the queue.
func (c *Client) Remove(ctx context.Context, inputID string) (bool, error) {
	conn, sid, err := c.withSession()
	if err != nil {
		return false, err
	}
	var out protocol.ACPRemoveResult
	if err := c.request(ctx, conn, protocol.ACPRemove, protocol.ACPRemoveRequest{SessionID: sid, InputID: inputID}, &out); err != nil {
		return false, err
	}
	return out.Removed, nil
}

// State reads the state of the current session.
func (c *Client) State(ctx context.Context) (protocol.ACPStateResult, error) {
	var out protocol.ACPStateResult
	conn, sid, err := c.withSession()
	if err != nil {
		return out, err
	}
	err = c.request(ctx, conn, protocol.ACPState, protocol.ACPSessionRequest{SessionID: sid}, &out)
	return out, err
}

// Models lists the model catalog and the current model.
func (c *Client) Models(ctx context.Context) (protocol.ACPModelsResult, error) {
	var out protocol.ACPModelsResult
	conn, sid, err := c.withSession()
	if err != nil {
		return out, err
	}
	err = c.request(ctx, conn, protocol.ACPModels, protocol.ACPSessionRequest{SessionID: sid}, &out)
	return out, err
}

// SetModel changes the model of an idle session.
func (c *Client) SetModel(ctx context.Context, modelID string) (protocol.ACPSetModelResult, error) {
	var out protocol.ACPSetModelResult
	conn, sid, err := c.withSession()
	if err != nil {
		return out, err
	}
	err = c.request(ctx, conn, protocol.ACPSetModel, protocol.ACPModelRequest{SessionID: sid, ModelID: modelID}, &out)
	return out, err
}

// SetThinking changes the thinking level of an idle session.
func (c *Client) SetThinking(ctx context.Context, level protocol.ThinkingLevel) (protocol.ThinkingLevel, error) {
	var out protocol.ACPThinkingResult
	conn, sid, err := c.withSession()
	if err != nil {
		return "", err
	}
	err = c.request(ctx, conn, protocol.ACPSetThinking, protocol.ACPThinkingRequest{SessionID: sid, Level: level}, &out)
	return out.Level, err
}

// Usage reads the usage of the session, attempt by attempt.
func (c *Client) Usage(ctx context.Context) (protocol.ACPUsageResult, error) {
	var out protocol.ACPUsageResult
	conn, sid, err := c.withSession()
	if err != nil {
		return out, err
	}
	err = c.request(ctx, conn, protocol.ACPUsage, protocol.ACPSessionRequest{SessionID: sid}, &out)
	return out, err
}

// Close leaves the agent and ends the client. A leader and its runs go on. An
// in-process agent stops through OnClose.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		conn := c.conn
		c.mu.Unlock()
		c.cancel()
		c.events.close() // messages after Close have no reader
		if conn != nil {
			_ = conn.Close()
		}
		c.wg.Wait()
		if c.cfg.OnClose != nil {
			c.closeErr = c.cfg.OnClose()
		}
	})
	return c.closeErr
}
