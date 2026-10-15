package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"regexp"
	"sync"
	"time"

	"AskCore/internal/providers"
)

// Broker errors. None of them carries provider text or a secret.
var (
	ErrProviderBusy  = errors.New("auth: another sign-in for this provider is running")
	ErrLoginNotFound = errors.New("auth: login not found")
	ErrInvalidState  = errors.New("auth: login cannot take this call now")
	ErrBrokerClosed  = errors.New("auth: broker closed")
)

// Kinds of interaction that Start reports.
const (
	KindBrowser        = "browser"         // open the URL; the leader host takes the callback
	KindBrowserAddress = "browser_address" // as browser, and a pasted callback address also works
	KindCopyCode       = "copy_code"       // open the URL and paste the code that the page shows
	KindDevice         = "device"          // open the URL and enter the user code
)

// Outcome statuses and reasons. Reasons are fixed tokens, never error text.
const (
	OutcomeSignedIn  = "signed_in" // a sign-in kept its token
	OutcomeSaved     = "saved"     // a key save kept the key
	OutcomeRefused   = "refused"
	OutcomeFailed    = "failed"
	OutcomeExpired   = "expired"
	OutcomeCancelled = "cancelled"

	ReasonInvalidKey   = "invalid_key"
	ReasonAccessDenied = "access_denied"
	ReasonExpired      = "expired"
	ReasonNetwork      = "network"
	ReasonCancelled    = "cancelled"
	ReasonRemoved      = "removed" // another client removed the provider
	ReasonBusy         = "busy"
	ReasonInternal     = "internal"
)

// Change tells what happened to the saved credentials of one provider.
type Change string

const (
	ChangeSaved   Change = "saved"
	ChangeRemoved Change = "removed"
)

// BrokerOptions holds the outside parts of a Broker.
type BrokerOptions struct {
	// CheckKey probes an API key of a provider for which KeyCheckable is true.
	CheckKey func(ctx context.Context, provider, key string) error
	// HostID returns the stable host UUID that the ChatGPT sign-in needs.
	HostID func(ctx context.Context) (string, error)
	// OnChange runs after every saved credential and every removal, in the order
	// of the generation numbers. It must not call the Broker.
	OnChange func(generation uint64, provider string, change Change)
}

// StartInfo is what a client needs to show for a started sign-in.
type StartInfo struct {
	LoginID   string
	Kind      string
	URL       string
	UserCode  string
	ExpiresAt time.Time // device sign-in only
}

// Outcome is the end of a sign-in or a key save. It holds no free text.
type Outcome struct {
	Status     string
	Reason     string
	HTTPStatus int    // refused only
	Code       string // refused only: short provider code, may be empty
	NotChecked bool   // the key was saved without a live check
	Account    string // display name of the account, may be empty
	Generation uint64 // generation after the save; zero otherwise
}

// LogoutInfo is the result of a removal.
type LogoutInfo struct {
	// EnvVar names the environment variable that still makes the provider usable.
	EnvVar string
}

// StartError is the error of a sign-in that ended before it showed anything.
type StartError struct{ Outcome Outcome }

func (e *StartError) Error() string { return "auth: sign-in did not start: " + e.Outcome.Reason }

// Broker runs the sign-ins of the clients of one leader process. It has no
// transport code. One sign-in or key save runs for each provider at a time,
// and a removal wins over a running one.
type Broker struct {
	svc  *Service
	opts BrokerOptions

	root       context.Context
	rootCancel context.CancelFunc
	wg         sync.WaitGroup

	mu       sync.Mutex
	closed   bool
	logins   map[string]*login
	active   map[string]*login
	removing map[string]chan struct{}
	released map[string]bool

	changeMu   sync.Mutex
	generation uint64
}

// NewBroker returns a broker over svc. Call Close when the leader stops.
func NewBroker(svc *Service, opts BrokerOptions) *Broker {
	root, cancel := context.WithCancel(context.Background())
	return &Broker{svc: svc, opts: opts, root: root, rootCancel: cancel,
		logins: map[string]*login{}, active: map[string]*login{}, removing: map[string]chan struct{}{}, released: map[string]bool{}}
}

// Generation returns the number of the last credential change.
func (b *Broker) Generation() uint64 {
	b.changeMu.Lock()
	defer b.changeMu.Unlock()
	return b.generation
}

type startResult struct {
	info StartInfo
	err  error
}

// login is one sign-in or key save. It holds ids and state, never a secret:
// a pasted value goes through the input channel to the sign-in and no further.
type login struct {
	id, owner, provider string
	ctx                 context.Context
	cancel              context.CancelFunc
	done                chan struct{}
	first               chan startResult
	input               chan string

	mu         sync.Mutex
	kind       string
	inputTaken bool
	reason     string // set by a cancel; the outcome is then cancelled
	gated      bool   // the credential is being written; a cancel is too late
	finished   bool
	account    string
	outcome    Outcome
}

var loginIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func newLoginID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func isLoginOption(provider, method string) bool {
	for _, o := range LoginOptions() {
		if o.Provider == provider && o.Method == method {
			return true
		}
	}
	return false
}

// reserve takes the one slot of a provider for a new login. The caller must
// call release once. isKey marks a key save, which has no first-notice channel.
func (b *Broker) reserve(owner, id, provider string, isKey bool) (*login, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrBrokerClosed
	}
	if b.released[owner] {
		return nil, ErrLoginNotFound
	}
	if b.active[provider] != nil || b.removing[provider] != nil {
		return nil, ErrProviderBusy
	}
	if old := b.logins[id]; old != nil {
		old.mu.Lock()
		reusable := old.finished && old.owner == owner
		old.mu.Unlock()
		if !reusable {
			return nil, ErrInvalidState
		}
	}
	// A new attempt replaces the finished ones of the same owner and provider.
	for key, l := range b.logins {
		l.mu.Lock()
		stale := l.finished && l.owner == owner && l.provider == provider
		l.mu.Unlock()
		if stale {
			delete(b.logins, key)
		}
	}
	ctx, cancel := context.WithCancel(b.root)
	l := &login{id: id, owner: owner, provider: provider, ctx: ctx, cancel: cancel, done: make(chan struct{}), input: make(chan string, 1)}
	if !isKey {
		l.first = make(chan startResult, 1)
	}
	b.logins[id] = l
	b.active[provider] = l
	b.wg.Add(1)
	return l, nil
}

// release ends a login: it records the outcome, frees the provider slot and
// wakes every waiter. Call it exactly once for each reserve.
func (b *Broker) release(l *login, out Outcome) {
	l.mu.Lock()
	// A value pasted after the sign-in stopped reading input must not stay on the login.
	select {
	case <-l.input:
	default:
	}
	l.outcome = out
	l.finished = true
	l.mu.Unlock()
	l.cancel()
	b.mu.Lock()
	if b.active[l.provider] == l {
		delete(b.active, l.provider)
	}
	b.mu.Unlock()
	close(l.done)
	b.wg.Done()
}

// gate runs right before the credential is written. It is the one point where
// a cancel and a commit are ordered: before it a cancel stops the write, after
// it the write goes on and a cancel reports that the credential is saved.
func (l *login) gate() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.reason != "" || l.ctx.Err() != nil {
		if l.reason == "" {
			l.reason = ReasonCancelled
		}
		return context.Canceled
	}
	l.gated = true
	return nil
}

// requestCancel stops a login that has not reached the gate. It returns true
// when nothing was saved by this login.
func (l *login) requestCancel(reason string) bool {
	l.mu.Lock()
	if l.finished {
		saved := l.outcome.Status == OutcomeSignedIn || l.outcome.Status == OutcomeSaved
		l.mu.Unlock()
		return !saved
	}
	if l.gated {
		l.mu.Unlock()
		return false
	}
	if l.reason == "" {
		l.reason = reason
	}
	l.mu.Unlock()
	l.cancel()
	return true
}

func (b *Broker) lookup(owner, id string) (*login, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	l := b.logins[id]
	if l == nil || l.owner != owner {
		return nil, ErrLoginNotFound
	}
	return l, nil
}

// interactions lists the flows that Start tries, in order, for a method.
func interactions(method string) []string {
	switch method {
	case "anthropic-oauth":
		return []string{"browser", "copy-code"}
	case "openai-chatgpt":
		return []string{"browser"}
	default:
		return []string{""}
	}
}

func noticeKind(method, interaction string) string {
	switch {
	case method == "anthropic-oauth" && interaction == "browser":
		return KindBrowserAddress
	case method == "anthropic-oauth":
		return KindCopyCode
	case method == "openai-chatgpt":
		return KindBrowser
	default:
		return KindDevice
	}
}

// Start begins an interactive sign-in and returns at the first notice or at
// the early failure. The sign-in runs under the broker, not under ctx: it goes
// on after the request that started it ends, and ends by Cancel, a removal,
// ReleaseOwner or Close. A ctx that ends before the first notice cancels it.
func (b *Broker) Start(ctx context.Context, owner, provider, method string) (StartInfo, error) {
	if method == "api-key" || !isLoginOption(provider, method) {
		return StartInfo{}, ErrMethod
	}
	hostID := ""
	if method == "openai-chatgpt" {
		if b.opts.HostID == nil {
			return StartInfo{}, &StartError{Outcome{Status: OutcomeFailed, Reason: ReasonInternal}}
		}
		var err error
		if hostID, err = b.opts.HostID(ctx); err != nil {
			return StartInfo{}, &StartError{Outcome{Status: OutcomeFailed, Reason: ReasonInternal}}
		}
	}
	id, err := newLoginID()
	if err != nil {
		return StartInfo{}, err
	}
	l, err := b.reserve(owner, id, provider, false)
	if err != nil {
		return StartInfo{}, err
	}
	go b.runSignIn(l, method, hostID)
	select {
	case r := <-l.first:
		if r.err != nil {
			b.forget(l)
			return StartInfo{}, r.err
		}
		return r.info, nil
	case <-ctx.Done():
		l.requestCancel(ReasonCancelled)
		<-l.done
		b.forget(l)
		return StartInfo{}, ctx.Err()
	}
}

func (b *Broker) forget(l *login) {
	b.mu.Lock()
	if b.logins[l.id] == l {
		delete(b.logins, l.id)
	}
	b.mu.Unlock()
}

func (b *Broker) runSignIn(l *login, method, hostID string) {
	var err error
	noticed := false
	flows := interactions(method)
	for i, flow := range flows {
		req := LoginRequest{
			Interaction:  flow,
			HostID:       hostID,
			BeforeCommit: l.gate,
			Account: func(name string) {
				l.mu.Lock()
				l.account = name
				l.mu.Unlock()
			},
			Notify: func(ctx context.Context, n LoginNotice) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				kind := noticeKind(method, flow)
				l.mu.Lock()
				l.kind = kind
				l.mu.Unlock()
				if !noticed {
					noticed = true
					l.first <- startResult{info: StartInfo{LoginID: l.id, Kind: kind, URL: n.URL, UserCode: n.UserCode, ExpiresAt: n.ExpiresAt}}
				}
				return nil
			},
		}
		if method == "anthropic-oauth" {
			req.Input = l.inputFunc
		}
		err = b.svc.Login(l.ctx, l.provider, method, req)
		// A busy callback port fails before any notice; the code flow takes over.
		if errors.Is(err, ErrCallbackPort) && !noticed && i+1 < len(flows) {
			continue
		}
		break
	}
	var out Outcome
	if err == nil {
		l.mu.Lock()
		account := l.account
		l.mu.Unlock()
		out = Outcome{Status: OutcomeSignedIn, Account: account}
		out.Generation = b.changed(l.provider, ChangeSaved)
	} else {
		out = outcomeFor(l, err)
	}
	b.release(l, out)
	if !noticed {
		l.first <- startResult{err: &StartError{Outcome: out}}
	}
}

// inputFunc gives the sign-in the one value that Input sends.
func (l *login) inputFunc(ctx context.Context) (string, error) {
	select {
	case v := <-l.input:
		return v, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func outcomeFor(l *login, err error) Outcome {
	l.mu.Lock()
	reason := l.reason
	l.mu.Unlock()
	if reason != "" {
		return Outcome{Status: OutcomeCancelled, Reason: reason}
	}
	var refusal *KeyRefusal
	switch {
	case errors.Is(err, ErrAuthorizationDenied), errors.Is(err, ErrDeviceDenied):
		return Outcome{Status: OutcomeFailed, Reason: ReasonAccessDenied}
	case errors.Is(err, ErrDeviceExpired):
		return Outcome{Status: OutcomeExpired, Reason: ReasonExpired}
	case errors.As(err, &refusal):
		return Outcome{Status: OutcomeRefused, Reason: ReasonInvalidKey, HTTPStatus: refusal.HTTPStatus, Code: refusal.Code}
	case errors.Is(err, ErrKeyCheckUnavailable), errors.Is(err, ErrHTTPExchange), errors.Is(err, context.DeadlineExceeded):
		// A deadline here is the limit of one request or of the whole sign-in.
		return Outcome{Status: OutcomeFailed, Reason: ReasonNetwork}
	case errors.Is(err, context.Canceled):
		return Outcome{Status: OutcomeCancelled, Reason: ReasonCancelled}
	default:
		return Outcome{Status: OutcomeFailed, Reason: ReasonInternal}
	}
}

// Input sends one pasted value (a code or a callback address) to a sign-in
// that waits for it. The value is not stored on the login. A second value is
// refused with ErrInvalidState.
func (b *Broker) Input(owner, id, code string) error {
	l, err := b.lookup(owner, id)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.finished || l.inputTaken || (l.kind != KindBrowserAddress && l.kind != KindCopyCode) {
		return ErrInvalidState
	}
	l.inputTaken = true
	l.input <- code // the channel holds one value and only one is ever sent
	return nil
}

// Wait blocks until the login ends and returns its outcome. When ctx ends first
// it returns the ctx error and the login goes on.
func (b *Broker) Wait(ctx context.Context, owner, id string) (Outcome, error) {
	l, err := b.lookup(owner, id)
	if err != nil {
		return Outcome{}, err
	}
	select {
	case <-l.done:
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.outcome, nil
	case <-ctx.Done():
		return Outcome{}, ctx.Err()
	}
}

// Cancel stops a login of owner. It returns true when nothing was saved by
// that login, and false when the credential was already saved or is being
// written.
func (b *Broker) Cancel(owner, id string) (beforeCommit bool, err error) {
	l, err := b.lookup(owner, id)
	if err != nil {
		return false, err
	}
	return l.requestCancel(ReasonCancelled), nil
}

// ReleaseOwner cancels every login of a client that is gone and forgets them.
func (b *Broker) ReleaseOwner(owner string) {
	b.mu.Lock()
	// A forwarded request can arrive after the client release. Keep the owner
	// closed, so that request cannot start another sign-in or key save.
	b.released[owner] = true
	var gone []*login
	for id, l := range b.logins {
		if l.owner == owner {
			gone = append(gone, l)
			delete(b.logins, id)
		}
	}
	b.mu.Unlock()
	for _, l := range gone {
		l.requestCancel(ReasonCancelled)
	}
}

// Close cancels every login and returns after all of them have ended. It
// must run before the refresh drain of the service, because a login holds a
// refresh slot while it runs.
func (b *Broker) Close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.rootCancel()
	b.wg.Wait()
}

const keyLengthLimit = privateInputLimit

// validKey accepts a printable ASCII token. A key with a space, a control
// character or a non-ASCII byte is never valid and cannot go into a header.
func validKey(key string) bool {
	if key == "" || len(key) > keyLengthLimit {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] <= 0x20 || key[i] >= 0x7f {
			return false
		}
	}
	return true
}

// SaveKey checks an API key (when the provider can be checked) and saves it.
// loginID is made by the client, so Cancel can reach the call. The key lives
// only in this call. The error is set only when the call did not start; every
// other result, a refusal included, is in the Outcome.
func (b *Broker) SaveKey(ctx context.Context, owner, loginID, provider, key string) (Outcome, error) {
	if !loginIDPattern.MatchString(loginID) {
		return Outcome{}, ErrInvalidState
	}
	if !isLoginOption(provider, "api-key") {
		return Outcome{}, ErrMethod
	}
	key = trimKey(key)
	if !validKey(key) {
		return Outcome{Status: OutcomeRefused, Reason: ReasonInvalidKey}, nil
	}
	l, err := b.reserve(owner, loginID, provider, true)
	if err != nil {
		return Outcome{}, err
	}
	stop := context.AfterFunc(ctx, func() { l.requestCancel(ReasonCancelled) })
	defer stop()
	out := b.runKey(l, key)
	b.release(l, out)
	return out, nil
}

func trimKey(key string) string {
	start, end := 0, len(key)
	for start < end && (key[start] == ' ' || key[start] == '\t' || key[start] == '\r' || key[start] == '\n') {
		start++
	}
	for end > start && (key[end-1] == ' ' || key[end-1] == '\t' || key[end-1] == '\r' || key[end-1] == '\n') {
		end--
	}
	return key[start:end]
}

func (b *Broker) runKey(l *login, key string) Outcome {
	checkable := KeyCheckable(l.provider)
	if checkable {
		if b.opts.CheckKey == nil {
			return Outcome{Status: OutcomeFailed, Reason: ReasonInternal}
		}
		if err := b.opts.CheckKey(l.ctx, l.provider, key); err != nil {
			return outcomeFor(l, err)
		}
	}
	// The write runs under a context that a cancel cannot end. The gate, not the
	// context, decides whether it happens, so the result is all or nothing.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(l.ctx), 15*time.Second)
	defer cancel()
	sent := false
	err := b.svc.Login(ctx, l.provider, "api-key", LoginRequest{
		BeforeCommit: l.gate,
		Input: func(context.Context) (string, error) {
			if sent {
				return "", io.EOF
			}
			sent = true
			return key, nil
		},
	})
	if err != nil {
		return outcomeFor(l, err)
	}
	return Outcome{Status: OutcomeSaved, NotChecked: !checkable, Generation: b.changed(l.provider, ChangeSaved)}
}

// Logout removes the saved credential of a provider. It wins over a sign-in or
// key check of that provider by any client: that one is cancelled with reason
// removed and has ended before the credential is deleted. Removals of one
// provider run one after the other.
func (b *Broker) Logout(ctx context.Context, provider, method string) (LogoutInfo, error) {
	known := false
	for _, o := range LoginOptions() {
		known = known || o.Provider == provider
	}
	if !known {
		return LogoutInfo{}, ErrMethod
	}
	var mine chan struct{}
	var running *login
	for mine == nil {
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return LogoutInfo{}, ErrBrokerClosed
		}
		if earlier := b.removing[provider]; earlier != nil {
			b.mu.Unlock()
			select {
			case <-earlier:
			case <-ctx.Done():
				return LogoutInfo{}, ctx.Err()
			}
			continue
		}
		mine = make(chan struct{})
		b.removing[provider] = mine
		running = b.active[provider]
		b.mu.Unlock()
	}
	defer func() {
		b.mu.Lock()
		delete(b.removing, provider)
		b.mu.Unlock()
		close(mine)
	}()
	if running != nil {
		running.requestCancel(ReasonRemoved)
		select {
		case <-running.done:
		case <-ctx.Done():
			return LogoutInfo{}, ctx.Err()
		}
	}
	if err := b.svc.Logout(ctx, provider, method); err != nil {
		return LogoutInfo{}, err
	}
	b.changed(provider, ChangeRemoved)
	env := b.svc.Env
	if env == nil {
		env = os.LookupEnv
	}
	return LogoutInfo{EnvVar: providers.KeyEnvName(provider, env)}, nil
}

// changed counts one credential change and tells OnChange, in generation order.
func (b *Broker) changed(provider string, change Change) uint64 {
	b.changeMu.Lock()
	defer b.changeMu.Unlock()
	b.svc.Forget(provider)
	b.generation++
	if b.opts.OnChange != nil {
		b.opts.OnChange(b.generation, provider, change)
	}
	return b.generation
}
