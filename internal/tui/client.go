package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"AskCore/pkg/protocol"
)

// This file is the whole contract between the terminal UI and an agent link.
// The composition root supplies a Client. The UI never imports a transport.
// Only pkg/protocol may be imported here, so every type below is plain data.

// Errors that every Client implementation returns, so the UI can test them with
// errors.Is.
var (
	// ErrNeedsLeader means the call exists only on a shared leader. The in-process
	// client returns it for sessions, attach, detach and take.
	ErrNeedsLeader = errors.New("this call needs the leader; the session runs in process")
	// ErrDisconnected means the call was not sent, because the link is down. The
	// client reconnects by itself. Nothing was sent, so a retry is safe.
	ErrDisconnected = errors.New("not connected to the agent")
	// ErrOutcomeUnknown means the link broke while the call was in flight. The
	// agent may or may not have done it. The client never sends it again.
	ErrOutcomeUnknown = errors.New("the link broke while the call was in flight; the outcome is unknown")
	// ErrSessionGone means the session no longer exists on the agent, for example
	// after a restart of the leader. The next prompt needs NewSession.
	ErrSessionGone = errors.New("the session is gone")
	// ErrNoSession means the call needs a current session and the client has none.
	ErrNoSession = errors.New("no current session")
	// ErrPromptPending means a prompt is still in flight. A session takes one
	// prompt at a time; use Steer or FollowUp during a run.
	ErrPromptPending = errors.New("a prompt is already in flight")
	// ErrClosed means the client was closed.
	ErrClosed = errors.New("the client is closed")
)

// RequestError is an error answer of the agent or of the leader. Kind is the
// Ask error kind (a protocol.ACPErrorKind value, for example "not_driver"); it
// is empty when the answer has none.
type RequestError struct {
	Code    int
	Kind    string
	Message string
}

func (e *RequestError) Error() string {
	if e.Kind == "" {
		return fmt.Sprintf("request failed (%d): %s", e.Code, e.Message)
	}
	return fmt.Sprintf("request failed (%s, %d): %s", e.Kind, e.Code, e.Message)
}

// Mode tells which kind of link a Client uses.
type Mode string

const (
	// ModeLeader is a link to the shared local leader over its Unix socket.
	ModeLeader Mode = "leader"
	// ModeInProcess is an agent that runs inside this process.
	ModeInProcess Mode = "in-process"
)

// ConnInfo identifies the link. InstanceID is the leader id that the footer
// shows; it is empty in process.
type ConnInfo struct {
	Mode       Mode
	InstanceID string
	ClientID   string
	Build      string
}

// SessionInfo is the session that a call made current.
type SessionInfo struct {
	ID string
	// Role is protocol.ACPRoleDriver or protocol.ACPRoleObserver.
	Role string
	Cwd  string
}

// Client is what the UI needs from an agent. Calls block until they are
// answered, except Prompt, whose outcome comes as messages. Every method is safe
// to call from many goroutines. Run each call in a command, never in Update.
//
// The client has one current session. NewSession and Attach switch to a session
// and follow its events. After a lost link the client reconnects by itself,
// restores the session and reports it with messages.
type Client interface {
	// Events delivers the messages of this file in order. The channel closes after
	// Close. The client never blocks on a slow reader: it queues without bound.
	Events() <-chan Message
	// Info describes the current link.
	Info() ConnInfo

	NewSession(ctx context.Context, cwd string) (SessionInfo, error)
	// Attach joins a live session of the leader as an observer and follows it.
	Attach(ctx context.Context, sessionID string) (SessionInfo, error)
	// Detach leaves the current session. Its run goes on.
	Detach(ctx context.Context) error
	// Take makes this client the driver of the current session.
	Take(ctx context.Context) (protocol.ACPTakeResult, error)
	ListSessions(ctx context.Context) ([]protocol.ACPLiveSession, error)

	// Prompt sends a prompt and returns its id at once. PromptAcceptedMsg,
	// PromptSendFailedMsg, PromptRunErrorMsg and PromptDoneMsg carry that id.
	// The returned error means nothing was sent.
	Prompt(text string) (PromptID, error)
	// Cancel asks the agent to stop the current run. It sends no answer back; the
	// run settles with an agent_settled event.
	Cancel() error
	Steer(ctx context.Context, text string) (inputID string, err error)
	FollowUp(ctx context.Context, text string) (inputID string, err error)
	Remove(ctx context.Context, inputID string) (removed bool, err error)

	// State reads the current session. ModelFallback names the saved default
	// that the session did not start on, and why.
	State(ctx context.Context) (protocol.ACPStateResult, error)
	// Models lists the models of providers that can serve now. The result has
	// UsableOnly true when the leader filtered the list; a leader that predates
	// the flag sends every row and leaves it false. Default is the saved default.
	Models(ctx context.Context) (protocol.ACPModelsResult, error)
	// SetModel changes the model of an idle session. A readiness failure comes
	// back as a *RequestError and the model stays as it was. With saveDefault the
	// leader also keeps the choice as the default of new sessions; the result's
	// DefaultSaved tells whether it did.
	SetModel(ctx context.Context, modelID string, saveDefault bool) (protocol.ACPSetModelResult, error)
	SetThinking(ctx context.Context, level protocol.ThinkingLevel) (protocol.ThinkingLevel, error)
	Usage(ctx context.Context) (protocol.ACPUsageResult, error)

	// The provider calls need the shared leader. In process each of them returns
	// ErrNeedsLeader and sends nothing. A secret (key, code or address) goes out
	// once, in one call, and is not kept or put into any error. A leader that
	// predates these calls answers with a *RequestError of Kind "unsupported".

	// Providers lists the login options with their state, and the providers
	// generation of the leader.
	Providers(ctx context.Context) (protocol.ACPProvidersResult, error)
	// SaveAPIKey checks and saves an API key under loginID, which the caller
	// makes and keeps unique. It blocks until the leader answers, up to the
	// check of the provider, so the caller gives it a context of its own and not
	// the short call context. CancelLogin with the same id stops it before the
	// key is stored; the answer then has status cancelled.
	SaveAPIKey(ctx context.Context, loginID, provider, key string) (protocol.ACPLoginOutcome, error)
	// StartLogin opens a browser, address or device sign-in. A second sign-in
	// to a provider that one client already signs in to comes back as a
	// *RequestError of Kind "provider_busy". A sign-in that cannot start for
	// another reason has Kind "internal" and a fixed text.
	StartLogin(ctx context.Context, provider, method string) (protocol.ACPLoginStartResult, error)
	// SendLoginCode gives a pasted code or callback address to a sign-in. The
	// answer says only that the leader took it; the result of a wrong value
	// comes from WaitLogin as status failed, reason internal.
	SendLoginCode(ctx context.Context, loginID, code string) error
	// WaitLogin blocks until the sign-in ends or ctx ends. The caller cancels ctx
	// to stop waiting; that does not cancel the sign-in. A link lost during the
	// wait gives ErrOutcomeUnknown and the call is not sent again. A sign-in
	// that another client's removal cancelled has status cancelled and reason
	// removed.
	WaitLogin(ctx context.Context, loginID string) (protocol.ACPLoginOutcome, error)
	// CancelLogin stops a sign-in or a key check. cancelled is false when the
	// credential was already saved. Close and Detach send it for every login
	// that is still open.
	CancelLogin(ctx context.Context, loginID string) (cancelled bool, err error)
	// Logout removes the saved credential of one login option. When the client
	// has a current session, the result names that session's model if the
	// removal made it unusable.
	Logout(ctx context.Context, provider, method string) (protocol.ACPLogoutResult, error)

	// Close leaves the leader without stopping its runs, or stops the in-process
	// agent and its run. It ends the reconnect loop and closes Events.
	Close() error
}

// PromptID names one prompt of this client. Messages about the prompt carry it,
// so the UI can ignore a message of an older prompt.
type PromptID uint64

// Message is a value that Events delivers. All message types of this file
// implement it.
type Message interface{ clientMessage() }

// ConnectedMsg reports a new link: the first one, or one after a reconnect.
type ConnectedMsg struct{ Info ConnInfo }

// DisconnectedMsg reports a lost link. The client now reconnects.
type DisconnectedMsg struct{ Err error }

// ReconnectingMsg reports one reconnect attempt that failed, or is about to
// start. Attempt counts from 1 and resets after a success. Wait is how long the
// client waits before the next attempt.
type ReconnectingMsg struct {
	Attempt int
	Wait    time.Duration
	Err     error
}

// SessionRestoredMsg reports that the session lives on the leader and the
// client follows it again. Role is the role this client has now. Events that
// were missed arrive as SessionEventMsg after it, or as a SnapshotMsg when the
// leader could not resume from the last cursor.
type SessionRestoredMsg struct {
	Session SessionInfo
	Resumed bool
}

// SessionGoneMsg reports that the session is not on the leader any more. The
// UI keeps its transcript and calls NewSession at the next prompt.
type SessionGoneMsg struct{ SessionID string }

// SnapshotEntry is one row of the log of a session, in the safe form that a
// follower gets. Message is set for Kind "message". Raw keeps the whole row.
type SnapshotEntry struct {
	Kind    string
	Message protocol.Message
	InputID string
	Raw     json.RawMessage
}

// SnapshotMsg is the history of a session at one cut. It comes after a new
// follow, and after a resync. Resumed is true when the cursor was still valid:
// then there are no entries and only later events follow. Resync is true when
// the leader refused the cursor and sent a new snapshot, so the UI must replace
// what it holds. Stream is set when an assistant message was open at the cut.
type SnapshotMsg struct {
	SessionID string
	Cursor    protocol.ACPCursor
	// Head is where the leader's log stood when it answered the follow. A
	// resumed follow keeps the cursor of the client, so the events between the
	// cursor and the head are a replay of what the client missed. The zero
	// value means the leader did not say.
	Head    protocol.ACPCursor
	Entries []SnapshotEntry
	Stream  *protocol.ACPStreamBaseline
	Resumed bool
	Resync  bool
}

// SessionEventMsg is one agent event of the followed session. The client
// delivers each event once, in order, also across a reconnect.
type SessionEventMsg struct {
	SessionID string
	Epoch     string
	Seq       uint64
	Event     protocol.Event
}

// PromptAcceptedMsg reports that the agent started the run of the prompt. The UI
// clears the draft now.
type PromptAcceptedMsg struct{ ID PromptID }

// PromptSendFailedMsg reports a prompt that the agent never started. Err is a
// *RequestError (for example Kind "not_driver"), or
// ErrOutcomeUnknown when the link broke before acceptance. OutcomeUnknown is
// true in the second case: the client does not send the prompt again.
type PromptSendFailedMsg struct {
	ID             PromptID
	Err            error
	OutcomeUnknown bool
}

// PromptRunErrorMsg reports a prompt that the agent accepted and whose run then
// failed. Err is a *RequestError, or ErrOutcomeUnknown when the link broke while
// the run was going on; the run itself continues on the leader.
type PromptRunErrorMsg struct {
	ID             PromptID
	Err            error
	OutcomeUnknown bool
}

// PromptDoneMsg reports a prompt whose run settled. StopReason is an ACP stop
// reason such as "end_turn" or "cancelled".
type PromptDoneMsg struct {
	ID         PromptID
	StopReason string
}

// ProvidersChangedMsg reports that the credentials of a provider changed on the
// leader, by this client or by another. Generation grows by one for each change
// of the leader instance, and the client drops a message that is not newer than
// the last one of the same instance. The UI fetches the lists again.
type ProvidersChangedMsg struct {
	Generation uint64
	Provider   string
	Change     protocol.ACPProviderChange
}

// ClientErrorMsg reports a failure inside the client that no call can carry,
// such as a panic of a client goroutine. Fatal is true when the client stopped.
type ClientErrorMsg struct {
	Err   error
	Fatal bool
}

func (ConnectedMsg) clientMessage()        {}
func (DisconnectedMsg) clientMessage()     {}
func (ReconnectingMsg) clientMessage()     {}
func (SessionRestoredMsg) clientMessage()  {}
func (SessionGoneMsg) clientMessage()      {}
func (SnapshotMsg) clientMessage()         {}
func (SessionEventMsg) clientMessage()     {}
func (PromptAcceptedMsg) clientMessage()   {}
func (PromptSendFailedMsg) clientMessage() {}
func (PromptRunErrorMsg) clientMessage()   {}
func (PromptDoneMsg) clientMessage()       {}
func (ProvidersChangedMsg) clientMessage() {}
func (ClientErrorMsg) clientMessage()      {}
