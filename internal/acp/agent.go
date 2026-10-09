package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"

	sdk "github.com/coder/acp-go-sdk"
)

// wireVersion is the only ACP wire version that the adapter speaks.
const wireVersion = 1

// notifier is the part of the SDK connection that sends frames to the client.
type notifier interface {
	SessionUpdate(ctx context.Context, p sdk.SessionNotification) error
	NotifyExtension(ctx context.Context, method string, params any) error
}

// Config holds what the adapter needs from the application. The adapter calls
// these functions and imports no composition code.
type Config struct {
	// Factory builds the Agent of one new session.
	Factory Factory
	// Info names the agent in the initialize result.
	Info sdk.Implementation
	// AuthMethods are the host sign-in methods that initialize lists. The
	// adapter never starts a sign-in: it only checks readiness.
	AuthMethods []sdk.AuthMethod
	// Authenticate checks that the configured credentials for methodID work.
	// An error is reported as "not signed in", never with its own text.
	Authenticate func(ctx context.Context, methodID string) error
	// Models lists the compiled catalog rows.
	Models func() []providers.Model
	// FindModel resolves one catalog row.
	FindModel func(ref providers.Ref) (providers.Model, error)
	// ModelAuth checks that m is ready under the sign-in method that the
	// client named. Nil means the client cannot name a method.
	ModelAuth func(ctx context.Context, m providers.Model, methodID string) error
	// Log receives the connection diagnostics, which hold no raw input.
	// Nil discards them.
	Log io.Writer
	// HostOptions change the session host.
	HostOptions []HostOption
	// RequireRoute makes the adapter refuse a call that has no route context.
	// The leader link sets it. The editor link leaves it off and has no
	// driver generation to check.
	RequireRoute bool
}

// Adapter implements the ACP Agent interface over a Host. Each session has its
// own Agent. The adapter holds no lock while it waits for an Agent.
type Adapter struct {
	cfg  Config
	host *Host

	initialized atomic.Bool
	out         atomic.Pointer[outbound]
	bound       chan struct{}
	bindOnce    sync.Once
	stop        chan struct{}
	stopOnce    sync.Once
	wg          sync.WaitGroup
	closeErr    error

	// ids holds the cycle and attempt that each session names. Only the writer
	// goroutine of a session touches its entry.
	idsMu sync.Mutex
	ids   map[string]*eventIDs

	// subs are the explicit follow subscriptions. unready holds those whose
	// follow result is not yet on the wire.
	subMu   sync.Mutex
	subs    map[string]*subscription
	unready map[string]*subscription
}

type outbound struct {
	n      notifier
	closer io.Closer
}

var _ sdk.Agent = (*Adapter)(nil)

// NewAdapter returns an adapter. Call Bind before the connection takes input.
func NewAdapter(ctx context.Context, cfg Config) (*Adapter, error) {
	switch {
	case cfg.Factory == nil:
		return nil, errors.New("acp: factory is required")
	case cfg.Models == nil || cfg.FindModel == nil:
		return nil, errors.New("acp: model catalog callbacks are required")
	}
	a := &Adapter{
		cfg: cfg, bound: make(chan struct{}), stop: make(chan struct{}),
		ids: map[string]*eventIDs{}, subs: map[string]*subscription{}, unready: map[string]*subscription{},
	}
	a.host = NewHost(ctx, cfg.Factory, a.writeEvent, cfg.HostOptions...)
	return a, nil
}

// Bind connects the adapter to the SDK connection. w is the checked writer of
// the connection output: the adapter learns from it when a follow result is on
// the wire. out is closed when the host fails and when the adapter closes, so
// a blocked write ends. Either may be nil.
func (a *Adapter) Bind(conn *sdk.AgentSideConnection, w *CheckedWriter, out io.Closer) {
	w2 := a.cfg.Log
	if w2 == nil {
		w2 = io.Discard
	}
	conn.SetLogger(NewQuietLogger(w2))
	if w != nil {
		w.Observe(a.frameWritten)
	}
	a.bindNotifier(conn, out)
}

func (a *Adapter) bindNotifier(n notifier, closer io.Closer) {
	a.bindOnce.Do(func() {
		a.out.Store(&outbound{n: n, closer: closer})
		close(a.bound)
		if closer != nil {
			a.wg.Add(1)
			go func() {
				defer a.wg.Done()
				select {
				case <-a.host.failed:
					_ = closer.Close()
				case <-a.stop:
				}
			}()
		}
	})
}

// Fail records a failure of the connection output, stops every active run and
// closes the output. The owner of the checked writer calls it from the failure
// hook of the writer, because the SDK drops the error of a response write.
func (a *Adapter) Fail(err error) { a.host.latch(err) }

// CloseOutput closes the output of the connection now. A write that blocks
// cannot be interrupted any other way, so the owner calls it on a signal path
// before it waits for Close. It is safe to call more than once.
func (a *Adapter) CloseOutput() error {
	if out := a.out.Load(); out != nil && out.closer != nil {
		return out.closer.Close()
	}
	return nil
}

// Failed is closed when the output of the connection failed. The owner of the
// input side closes it then, so the connection ends.
func (a *Adapter) Failed() <-chan struct{} { return a.host.failed }

// Failure returns the output error that failed the adapter, or nil.
func (a *Adapter) Failure() error { return a.host.Failure() }

// Close ends the subscriptions, disposes every session and closes the output.
// It is safe to call more than once.
func (a *Adapter) Close() error {
	a.stopOnce.Do(func() {
		close(a.stop)
		a.subMu.Lock()
		list := make([]*subscription, 0, len(a.subs))
		for _, s := range a.subs {
			list = append(list, s)
		}
		a.subMu.Unlock()
		for _, s := range list {
			s.cancel()
		}
		a.closeErr = a.host.Close()
		a.wg.Wait()
		_ = a.CloseOutput()
	})
	return a.closeErr
}

// gate fails a call that comes before initialize.
func (a *Adapter) gate() error {
	if !a.initialized.Load() {
		return errNotInitialized
	}
	return nil
}

func (a *Adapter) session(id string) (*Session, error) {
	s, ok := a.host.Session(id)
	if !ok {
		return nil, errUnknownSession
	}
	return s, nil
}

// Initialize negotiates the wire version and lists what the adapter can do.
// The adapter speaks version 1 whatever the client asks for. It lists no
// capability that has no owner.
func (a *Adapter) Initialize(_ context.Context, _ sdk.InitializeRequest) (sdk.InitializeResponse, error) {
	a.initialized.Store(true)
	info := a.cfg.Info
	return sdk.InitializeResponse{
		ProtocolVersion:   sdk.ProtocolVersion(wireVersion),
		AgentCapabilities: sdk.AgentCapabilities{PromptCapabilities: sdk.PromptCapabilities{Image: true}},
		AgentInfo:         &info,
		AuthMethods:       append([]sdk.AuthMethod{}, a.cfg.AuthMethods...),
	}, nil
}

func authMethodID(m sdk.AuthMethod) string {
	switch {
	case m.Agent != nil:
		return m.Agent.Id
	case m.EnvVar != nil:
		return m.EnvVar.Id
	case m.Terminal != nil:
		return m.Terminal.Id
	}
	return ""
}

// Authenticate checks that the configured method is ready. It never starts a
// sign-in and never reads input: the operator signs in on the host.
func (a *Adapter) Authenticate(ctx context.Context, req sdk.AuthenticateRequest) (sdk.AuthenticateResponse, error) {
	if err := a.gate(); err != nil {
		return sdk.AuthenticateResponse{}, requestError(err)
	}
	listed := false
	for _, m := range a.cfg.AuthMethods {
		if authMethodID(m) == req.MethodId {
			listed = true
		}
	}
	if !listed || a.cfg.Authenticate == nil {
		return sdk.AuthenticateResponse{}, requestError(newKindError(protocol.ACPErrInvalidParams, "unknown authentication method"))
	}
	if err := a.host.beginOperation(); err != nil {
		return sdk.AuthenticateResponse{}, requestError(err)
	}
	defer a.host.endOperation()
	if err := a.cfg.Authenticate(ctx, req.MethodId); err != nil {
		return sdk.AuthenticateResponse{}, requestError(fmt.Errorf("%w: %w", ErrAuth, err))
	}
	return sdk.AuthenticateResponse{}, nil
}

// NewSession builds one independent Agent and publishes the session.
func (a *Adapter) NewSession(ctx context.Context, req sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
	if err := a.gate(); err != nil {
		return sdk.NewSessionResponse{}, requestError(err)
	}
	switch {
	case len(req.McpServers) > 0:
		return sdk.NewSessionResponse{}, requestError(newKindError(protocol.ACPErrInvalidParams, "mcp servers are not supported"))
	case len(req.AdditionalDirectories) > 0:
		return sdk.NewSessionResponse{}, requestError(newKindError(protocol.ACPErrInvalidParams, "additional directories are not supported"))
	case !filepath.IsAbs(req.Cwd):
		return sdk.NewSessionResponse{}, requestError(newKindError(protocol.ACPErrInvalidParams, "cwd must be an absolute path"))
	}
	route, err := routeFromMeta(req.Meta)
	if err == nil {
		err = a.needRoute(route)
	}
	if err != nil {
		return sdk.NewSessionResponse{}, requestError(err)
	}
	s, err := a.host.NewSession(ctx, req.Cwd)
	if err != nil {
		return sdk.NewSessionResponse{}, requestError(err)
	}
	res := sdk.NewSessionResponse{SessionId: sdk.SessionId(s.ID)}
	if route != nil {
		// The router needs the generation that the host committed. The editor link has no router.
		s.setDriver(route)
		res.Meta = map[string]any{protocol.ACPRouteMetaKey: protocol.ACPRouteMeta{DriverGen: s.Gen()}}
	}
	return res, nil
}

// userMessage converts prompt blocks. It accepts text and image blocks only.
func userMessage(blocks []sdk.ContentBlock) (protocol.Message, error) {
	in := protocol.ACPInputRequest{Content: make([]protocol.ACPContentBlock, 0, len(blocks))}
	for _, b := range blocks {
		switch {
		case b.Text != nil:
			in.Content = append(in.Content, protocol.ACPContentBlock{Type: "text", Text: b.Text.Text})
		case b.Image != nil:
			in.Content = append(in.Content, protocol.ACPContentBlock{Type: "image", Data: b.Image.Data, MimeType: b.Image.MimeType})
		default:
			in.Content = append(in.Content, protocol.ACPContentBlock{Type: "unsupported"})
		}
	}
	return inputMessage(in)
}

func inputMessage(in protocol.ACPInputRequest) (protocol.Message, error) {
	blocks, err := in.UserBlocks()
	if err != nil {
		return nil, newKindError(protocol.ACPErrInvalidContent, "only non-empty text and image blocks are supported")
	}
	return protocol.UserMessage{Content: blocks}, nil
}

// stopReason maps the reason of the last cycle of a run to an ACP stop reason.
func stopReason(reason string) sdk.StopReason {
	switch reason {
	case "aborted":
		return sdk.StopReasonCancelled
	case "max-tokens":
		return sdk.StopReasonMaxTokens
	case "blocked":
		return sdk.StopReasonRefusal
	case "continuation-limit":
		return sdk.StopReasonMaxTurnRequests
	}
	return sdk.StopReasonEndTurn
}

// Prompt runs one turn. It returns after the run settled and every update of
// the run was written. The request context does not stop the run: use
// session/cancel for that.
func (a *Adapter) Prompt(ctx context.Context, req sdk.PromptRequest) (sdk.PromptResponse, error) {
	if err := a.gate(); err != nil {
		return sdk.PromptResponse{}, requestError(err)
	}
	s, err := a.session(string(req.SessionId))
	if err != nil {
		return sdk.PromptResponse{}, requestError(err)
	}
	// The authority of the call is checked before its content is read.
	route, err := a.metaRoute(req.Meta)
	if err == nil {
		err = a.checkRouteSession(route, string(req.SessionId))
	}
	if err != nil {
		return sdk.PromptResponse{}, requestError(err)
	}
	msg, err := userMessage(req.Prompt)
	if err != nil {
		return sdk.PromptResponse{}, requestError(err)
	}
	release, err := s.guard(route)
	if err != nil {
		return sdk.PromptResponse{}, requestError(err)
	}
	defer release()
	res, err := s.Prompt(s.mutationContext(ctx, route), msg)
	if err != nil {
		return sdk.PromptResponse{}, requestError(err)
	}
	if err := runError(res); err != nil {
		return sdk.PromptResponse{}, requestError(err)
	}
	return sdk.PromptResponse{StopReason: stopReason(res.Reason)}, nil
}

// Cancel aborts the active run of a session. It never blocks, because the SDK
// queues later notifications behind it.
func (a *Adapter) Cancel(_ context.Context, req sdk.CancelNotification) error {
	if !a.initialized.Load() {
		return nil
	}
	if s, ok := a.host.Session(string(req.SessionId)); ok {
		// A notification has no answer. A cancel without a valid route, or from
		// an old driver generation, is dropped.
		route, err := a.metaRoute(req.Meta)
		if err != nil || (route != nil && route.DriverGen == 0) || a.checkRouteSession(route, string(req.SessionId)) != nil {
			return nil
		}
		done, err := s.guard(route)
		if err != nil {
			return nil
		}
		defer done()
		release, err := s.mutationCommit(route)
		if err != nil {
			return nil
		}
		s.Abort()
		release()
	}
	return nil
}

func unsupported(method string) error {
	return fmt.Errorf("%w: %s", errUnsupported, method)
}

// Session returns a live session by ID, for the composition that owns the host.
func (a *Adapter) Session(id string) (*Session, bool) { return a.host.Session(id) }

// QuiesceIfIdle closes admission when nothing is active. See Host.QuiesceIfIdle.
func (a *Adapter) QuiesceIfIdle() bool { return a.host.QuiesceIfIdle() }

// ActiveRuns counts the sessions that are not idle.
func (a *Adapter) ActiveRuns() int { return a.host.ActiveRuns() }

func (a *Adapter) Logout(context.Context, sdk.LogoutRequest) (sdk.LogoutResponse, error) {
	return sdk.LogoutResponse{}, requestError(unsupported("logout"))
}

func (a *Adapter) CloseSession(context.Context, sdk.CloseSessionRequest) (sdk.CloseSessionResponse, error) {
	return sdk.CloseSessionResponse{}, requestError(unsupported("session/close"))
}

func (a *Adapter) ListSessions(context.Context, sdk.ListSessionsRequest) (sdk.ListSessionsResponse, error) {
	return sdk.ListSessionsResponse{}, requestError(unsupported("session/list"))
}

func (a *Adapter) ResumeSession(context.Context, sdk.ResumeSessionRequest) (sdk.ResumeSessionResponse, error) {
	return sdk.ResumeSessionResponse{}, requestError(unsupported("session/resume"))
}

func (a *Adapter) SetSessionConfigOption(context.Context, sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
	return sdk.SetSessionConfigOptionResponse{}, requestError(unsupported("session/set_config_option"))
}

func (a *Adapter) SetSessionMode(context.Context, sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error) {
	return sdk.SetSessionModeResponse{}, requestError(unsupported("session/set_mode"))
}

// waitOut returns the bound connection. A call that comes before Bind waits for
// it: the SDK reads input as soon as the connection exists.
func (a *Adapter) waitOut(ctx context.Context) (*outbound, error) {
	select {
	case <-a.bound:
		return a.out.Load(), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// writeEvent is the host writer. It sends the standard updates of one Agent
// event, in the order of the session. An error fails the host.
func (a *Adapter) writeEvent(ctx context.Context, sessionID, epoch string, ev protocol.Event) error {
	out, err := a.waitOut(ctx)
	if err != nil {
		return err
	}
	a.idsMu.Lock()
	ids := a.ids[sessionID]
	if ids == nil {
		ids = &eventIDs{}
		a.ids[sessionID] = ids
	}
	a.idsMu.Unlock()
	ids.note(ev)
	ups := standardUpdates(ev)
	for i, u := range ups {
		err := out.n.SessionUpdate(ctx, sdk.SessionNotification{
			SessionId: sdk.SessionId(sessionID),
			Update:    u,
			Meta:      frameMeta(epoch, *ev.Env(), *ids, i, len(ups)),
		})
		if err != nil {
			return err
		}
	}
	return a.waitFollowers(ctx, sessionID, epoch, ev.Env().Seq)
}

// decodeParams decodes the parameters of an Ask method. The error text of the
// decoder is not sent, because it can quote the input.
func decodeParams(raw json.RawMessage, out any) error {
	if err := json.Unmarshal(raw, out); err != nil {
		return newKindError(protocol.ACPErrInvalidParams, "malformed parameters")
	}
	return nil
}
