package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"AskCore/internal/providers"
	"AskCore/internal/settings"
	"AskCore/internal/testsupport"
)

const (
	xaiDeviceURL = "https://auth.x.ai/oauth2/device/code"
	xaiSuccess   = `{"access_token":"access","refresh_token":"refresh","expires_in":3600}`
	devicePage   = `{"device_code":"private-device","user_code":"USER-CODE","verification_uri":"https://auth.x.ai/device","expires_in":600,"interval":1}`
)

type changeEvent struct {
	generation uint64
	provider   string
	change     Change
}

type brokerEnv struct {
	t       *testing.T
	svc     *Service
	store   *settings.AuthStore
	broker  *Broker
	changes chan changeEvent
	ignore  goleak.Option
}

type brokerSetup struct {
	transport func(*http.Request) (*http.Response, error)
	wait      func(context.Context, time.Duration) error
	now       func() time.Time
	checkKey  func(context.Context, string, string) error
	hostID    func(context.Context) (string, error)
	env       map[string]string
}

// blockingWait never returns before the context ends, so a device login stays pending.
func blockingWait(ctx context.Context, _ time.Duration) error { <-ctx.Done(); return ctx.Err() }

func immediateWait(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// deviceTransport answers the device-code request and sends every token poll to poll.
func deviceTransport(t *testing.T, poll func(*http.Request) (*http.Response, error)) func(*http.Request) (*http.Response, error) {
	return func(r *http.Request) (*http.Response, error) {
		switch r.URL.String() {
		case xaiDeviceURL:
			return nativeResponse(200, devicePage), nil
		case xaiToken:
			return poll(r)
		}
		t.Errorf("unexpected destination %s", r.URL)
		return nil, errors.New("unexpected destination")
	}
}

func pendingPoll(*http.Request) (*http.Response, error) {
	return nativeResponse(400, `{"error":"authorization_pending"}`), nil
}

func newBrokerEnv(t *testing.T, setup brokerSetup) *brokerEnv {
	t.Helper()
	ignore := goleak.IgnoreCurrent()
	transport := setup.transport
	if transport == nil {
		transport = func(r *http.Request) (*http.Response, error) {
			t.Errorf("unexpected HTTP request to %s", r.URL)
			return nil, errors.New("unexpected request")
		}
	}
	svc, store := testServiceWith(t, NativeOptions{HTTPClient: &http.Client{Transport: nativeRT(transport)}, Now: setup.now, Wait: setup.wait}, setup.env)
	e := &brokerEnv{t: t, svc: svc, store: store, changes: make(chan changeEvent, 16), ignore: ignore}
	e.broker = NewBroker(svc, BrokerOptions{
		CheckKey: setup.checkKey,
		HostID:   setup.hostID,
		OnChange: func(g uint64, provider string, change Change) { e.changes <- changeEvent{g, provider, change} },
	})
	return e
}

// done closes the broker and proves that no goroutine of the test is left.
func (e *brokerEnv) done() {
	e.t.Helper()
	e.broker.Close()
	goleak.VerifyNone(e.t, e.ignore)
}

func (e *brokerEnv) wait(owner, id string) Outcome {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	o, err := e.broker.Wait(ctx, owner, id)
	if err != nil {
		e.t.Fatalf("wait: %v", err)
	}
	return o
}

func (e *brokerEnv) saved(provider string) settings.Credential {
	e.t.Helper()
	c, _, err := e.store.Read(context.Background(), provider)
	if err != nil {
		e.t.Fatal(err)
	}
	return c
}

func recv[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatalf("timeout waiting for %s", what)
		panic("unreachable")
	}
}

func noEvent[T any](t *testing.T, ch <-chan T, what string) {
	t.Helper()
	select {
	case v := <-ch:
		t.Fatalf("unexpected %s: %v", what, v)
	case <-time.After(50 * time.Millisecond):
	}
}

func startDevice(t *testing.T, e *brokerEnv, owner string) StartInfo {
	t.Helper()
	info, err := e.broker.Start(context.Background(), owner, providers.ProviderXAI, "xai-oauth")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return info
}

func TestBrokerSavesUncheckableKeyWithoutRequest(t *testing.T) {
	e := newBrokerEnv(t, brokerSetup{checkKey: NativeKeyCheck(NativeOptions{HTTPClient: failTransport(t)})})
	defer e.done()
	out, err := e.broker.SaveKey(context.Background(), "o1", "key-1", providers.ProviderTokenPlan, "fake-token-plan-key")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != OutcomeSaved || !out.NotChecked || out.Generation != 1 {
		t.Fatalf("outcome: %+v", out)
	}
	if c := e.saved(providers.ProviderTokenPlan); c.APIKey != "fake-token-plan-key" || c.Method != "api-key" {
		t.Fatalf("record: %+v", c)
	}
}

func TestBrokerCheckedKeyReportsChecked(t *testing.T) {
	var calls atomic.Int32
	e := newBrokerEnv(t, brokerSetup{checkKey: func(_ context.Context, provider, key string) error {
		calls.Add(1)
		if provider != providers.ProviderOpenAI || key != "fake-openai-key" {
			t.Errorf("probe arguments: %s", provider)
		}
		return nil
	}})
	defer e.done()
	out, err := e.broker.SaveKey(context.Background(), "o1", "key-1", providers.ProviderOpenAI, "  fake-openai-key\n")
	if err != nil || out.Status != OutcomeSaved || out.NotChecked || calls.Load() != 1 {
		t.Fatalf("outcome %+v err %v calls %d", out, err, calls.Load())
	}
}

func TestBrokerKeyRefusalCarriesCodeOnly(t *testing.T) {
	const key = "fake-secret-key-0123456789"
	body := `{"error":{"code":"invalid_api_key","message":"Incorrect API key provided: ` + key + `"}}`
	e := newBrokerEnv(t, brokerSetup{checkKey: NativeKeyCheck(NativeOptions{HTTPClient: &http.Client{Transport: nativeRT(func(*http.Request) (*http.Response, error) {
		return nativeResponse(401, body), nil
	})}})})
	defer e.done()
	out, err := e.broker.SaveKey(context.Background(), "o1", "key-1", providers.ProviderOpenAI, key)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != OutcomeRefused || out.HTTPStatus != 401 || out.Code != "invalid_api_key" || out.Reason != ReasonInvalidKey {
		t.Fatalf("outcome: %+v", out)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", out, out), key) {
		t.Fatal("key reached the outcome")
	}
	if c := e.saved(providers.ProviderOpenAI); c.Method != "" {
		t.Fatalf("refused key stored: %+v", c)
	}
	noEvent(t, e.changes, "change")
}

func TestBrokerKeyShapeRefusedWithoutProbe(t *testing.T) {
	e := newBrokerEnv(t, brokerSetup{checkKey: func(context.Context, string, string) error {
		t.Error("probe ran")
		return nil
	}})
	defer e.done()
	for _, key := range []string{"", "   ", "two words", "line\nbreak", "café", strings.Repeat("k", 16385)} {
		out, err := e.broker.SaveKey(context.Background(), "o1", "key-1", providers.ProviderOpenAI, key)
		if err != nil || out.Status != OutcomeRefused || out.Reason != ReasonInvalidKey {
			t.Fatalf("key %q: %+v %v", key, out, err)
		}
	}
}

func TestBrokerOneLoginPerProvider(t *testing.T) {
	e := newBrokerEnv(t, brokerSetup{transport: deviceTransport(t, pendingPoll), wait: blockingWait, checkKey: func(context.Context, string, string) error { return nil }})
	defer e.done()
	info := startDevice(t, e, "o1")
	if info.Kind != KindDevice || info.UserCode != "USER-CODE" || info.URL != "https://auth.x.ai/device" || info.LoginID == "" || info.ExpiresAt.IsZero() {
		t.Fatalf("start: %+v", info)
	}
	if _, err := e.broker.Start(context.Background(), "o2", providers.ProviderXAI, "xai-oauth"); !errors.Is(err, ErrProviderBusy) {
		t.Fatalf("second start: %v", err)
	}
	if _, err := e.broker.SaveKey(context.Background(), "o2", "key-2", providers.ProviderXAI, "fake-key"); !errors.Is(err, ErrProviderBusy) {
		t.Fatalf("key save: %v", err)
	}
	out, err := e.broker.SaveKey(context.Background(), "o2", "key-3", providers.ProviderOpenAI, "fake-key")
	if err != nil || out.Status != OutcomeSaved {
		t.Fatalf("other provider: %+v %v", out, err)
	}
	if before, err := e.broker.Cancel("o1", info.LoginID); err != nil || !before {
		t.Fatalf("cancel: %v %v", before, err)
	}
	if o := e.wait("o1", info.LoginID); o.Status != OutcomeCancelled || o.Reason != ReasonCancelled {
		t.Fatalf("outcome: %+v", o)
	}
	// The slot is free again.
	again := startDevice(t, e, "o2")
	if _, err = e.broker.Cancel("o2", again.LoginID); err != nil {
		t.Fatal(err)
	}
}

func TestBrokerOwnerIsolation(t *testing.T) {
	e := newBrokerEnv(t, brokerSetup{transport: deviceTransport(t, pendingPoll), wait: blockingWait})
	defer e.done()
	info := startDevice(t, e, "o1")
	if err := e.broker.Input("o2", info.LoginID, "x"); !errors.Is(err, ErrLoginNotFound) {
		t.Fatalf("input: %v", err)
	}
	if _, err := e.broker.Cancel("o2", info.LoginID); !errors.Is(err, ErrLoginNotFound) {
		t.Fatalf("cancel: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := e.broker.Wait(ctx, "o2", info.LoginID); !errors.Is(err, ErrLoginNotFound) {
		t.Fatalf("wait: %v", err)
	}
	if err := e.broker.Input("o1", info.LoginID, "x"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("device login takes no input: %v", err)
	}
}

func TestBrokerCancelDropsLateResult(t *testing.T) {
	polling := make(chan struct{})
	release := make(chan struct{})
	e := newBrokerEnv(t, brokerSetup{wait: immediateWait, transport: deviceTransport(t, func(*http.Request) (*http.Response, error) {
		close(polling)
		<-release
		return nativeResponse(200, xaiSuccess), nil
	})})
	defer e.done()
	info := startDevice(t, e, "o1")
	recv(t, polling, "token poll")
	before, err := e.broker.Cancel("o1", info.LoginID)
	if err != nil || !before {
		t.Fatalf("cancel: %v %v", before, err)
	}
	close(release)
	if o := e.wait("o1", info.LoginID); o.Status != OutcomeCancelled {
		t.Fatalf("outcome: %+v", o)
	}
	if c := e.saved(providers.ProviderXAI); c.Method != "" {
		t.Fatalf("late token stored: %+v", c)
	}
	noEvent(t, e.changes, "change")
}

// callbackRoundTrip sends one request to a browser callback address.
func callbackRoundTrip(t *testing.T, address string) int {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	resp, err := client.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}

func anthropicTransport(t *testing.T, tokenGate chan struct{}) func(*http.Request) (*http.Response, error) {
	return func(r *http.Request) (*http.Response, error) {
		switch r.URL.String() {
		case anthropicToken:
			if tokenGate != nil {
				<-tokenGate
			}
			return nativeResponse(200, `{"access_token":"access","refresh_token":"refresh","expires_in":3600,"account":{"email_address":"token@example.com"}}`), nil
		case anthropicProfileURL:
			return nativeResponse(200, `{"account":{"email":"profile@example.com"}}`), nil
		}
		t.Errorf("unexpected destination %s", r.URL)
		return nil, errors.New("unexpected destination")
	}
}

func authorizeParams(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func TestBrokerBrowserDenied(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	e := newBrokerEnv(t, brokerSetup{transport: anthropicTransport(t, nil)})
	defer e.done()
	info, err := e.broker.Start(context.Background(), "o1", providers.ProviderAnthropic, "anthropic-oauth")
	if err != nil || info.Kind != KindBrowserAddress {
		t.Fatalf("start: %+v %v", info, err)
	}
	q := authorizeParams(t, info.URL)
	if code := callbackRoundTrip(t, q.Get("redirect_uri")+"?error=access_denied&state="+url.QueryEscape(q.Get("state"))); code != http.StatusBadRequest {
		t.Fatalf("callback status %d", code)
	}
	if o := e.wait("o1", info.LoginID); o.Status != OutcomeFailed || o.Reason != ReasonAccessDenied {
		t.Fatalf("outcome: %+v", o)
	}
	noEvent(t, e.changes, "change")
}

func TestBrokerAnthropicBrowserAcceptsPastedAddress(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	e := newBrokerEnv(t, brokerSetup{transport: anthropicTransport(t, nil)})
	defer e.done()
	info, err := e.broker.Start(context.Background(), "o1", providers.ProviderAnthropic, "anthropic-oauth")
	if err != nil || info.Kind != KindBrowserAddress || !strings.HasPrefix(info.URL, "https://claude.ai/oauth/authorize?") {
		t.Fatalf("start: %+v %v", info, err)
	}
	q := authorizeParams(t, info.URL)
	if q.Get("redirect_uri") != "http://localhost:53692/callback" {
		t.Fatalf("redirect: %s", q.Get("redirect_uri"))
	}
	const code = "pasted-secret-code"
	if err = e.broker.Input("o1", info.LoginID, q.Get("redirect_uri")+"?code="+code+"&state="+url.QueryEscape(q.Get("state"))); err != nil {
		t.Fatal(err)
	}
	o := e.wait("o1", info.LoginID)
	if o.Status != OutcomeSignedIn || o.Account != "profile@example.com" || o.NotChecked {
		t.Fatalf("outcome: %+v", o)
	}
	if c := e.saved(providers.ProviderAnthropic); c.Method != "anthropic-oauth" || c.OAuth == nil {
		t.Fatalf("record: %+v", c)
	}
	if ev := recv(t, e.changes, "change"); ev.provider != providers.ProviderAnthropic || ev.change != ChangeSaved || ev.generation != 1 {
		t.Fatalf("change: %+v", ev)
	}
	e.broker.mu.Lock()
	dump := fmt.Sprintf("%+v", e.broker.logins)
	e.broker.mu.Unlock()
	if strings.Contains(dump, code) {
		t.Fatal("pasted address kept on the login")
	}
	e.broker.mu.Lock()
	for _, l := range e.broker.logins {
		if len(l.input) != 0 {
			t.Error("a pasted value is still buffered on the login")
		}
	}
	e.broker.mu.Unlock()
}

func TestBrokerAnthropicCallbackFinishesFirst(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	e := newBrokerEnv(t, brokerSetup{transport: anthropicTransport(t, nil)})
	defer e.done()
	info, err := e.broker.Start(context.Background(), "o1", providers.ProviderAnthropic, "anthropic-oauth")
	if err != nil {
		t.Fatal(err)
	}
	q := authorizeParams(t, info.URL)
	if code := callbackRoundTrip(t, q.Get("redirect_uri")+"?code=private&state="+url.QueryEscape(q.Get("state"))); code != 200 {
		t.Fatalf("callback status %d", code)
	}
	if o := e.wait("o1", info.LoginID); o.Status != OutcomeSignedIn {
		t.Fatalf("outcome: %+v", o)
	}
}

func busyCallbackPort(t *testing.T) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:53692")
	if err != nil {
		t.Skipf("callback port in use: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
}

func TestBrokerAnthropicFallsBackToCopyCode(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	busyCallbackPort(t)
	e := newBrokerEnv(t, brokerSetup{transport: anthropicTransport(t, nil)})
	defer e.done()
	info, err := e.broker.Start(context.Background(), "o1", providers.ProviderAnthropic, "anthropic-oauth")
	if err != nil || info.Kind != KindCopyCode {
		t.Fatalf("start: %+v %v", info, err)
	}
	if got := authorizeParams(t, info.URL).Get("redirect_uri"); got != "https://platform.claude.com/oauth/code/callback" {
		t.Fatalf("redirect: %s", got)
	}
	if _, err = e.broker.Cancel("o1", info.LoginID); err != nil {
		t.Fatal(err)
	}
}

func TestBrokerCopyCodeInput(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	busyCallbackPort(t)
	gate := make(chan struct{})
	e := newBrokerEnv(t, brokerSetup{transport: anthropicTransport(t, gate)})
	defer e.done()
	info, err := e.broker.Start(context.Background(), "o1", providers.ProviderAnthropic, "anthropic-oauth")
	if err != nil || info.Kind != KindCopyCode {
		t.Fatalf("start: %+v %v", info, err)
	}
	state := authorizeParams(t, info.URL).Get("state")
	if err = e.broker.Input("o1", info.LoginID, "private-code#"+state); err != nil {
		t.Fatal(err)
	}
	// The first value is being checked against the token endpoint.
	deadline := time.Now().Add(5 * time.Second)
	for {
		err = e.broker.Input("o1", info.LoginID, "second-value")
		if errors.Is(err, ErrInvalidState) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("second input: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(gate)
	if o := e.wait("o1", info.LoginID); o.Status != OutcomeSignedIn {
		t.Fatalf("outcome: %+v", o)
	}
}

func TestBrokerDeviceExpired(t *testing.T) {
	var mu sync.Mutex
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	e := newBrokerEnv(t, brokerSetup{
		now: clock,
		wait: func(ctx context.Context, d time.Duration) error {
			mu.Lock()
			now = now.Add(d)
			mu.Unlock()
			return ctx.Err()
		},
		transport: deviceTransport(t, pendingPoll),
	})
	defer e.done()
	start := clock()
	info := startDevice(t, e, "o1")
	if !info.ExpiresAt.Equal(start.Add(600 * time.Second)) {
		t.Fatalf("expiry: %v", info.ExpiresAt)
	}
	if o := e.wait("o1", info.LoginID); o.Status != OutcomeExpired || o.Reason != ReasonExpired {
		t.Fatalf("outcome: %+v", o)
	}
}

func TestBrokerReleaseOwnerCancelsLogins(t *testing.T) {
	blocked := make(chan struct{})
	e := newBrokerEnv(t, brokerSetup{transport: deviceTransport(t, pendingPoll), wait: blockingWait, checkKey: func(ctx context.Context, _, _ string) error {
		close(blocked)
		<-ctx.Done()
		return ctx.Err()
	}})
	defer e.done()
	first := startDevice(t, e, "o1")
	keyDone := make(chan Outcome, 1)
	go func() {
		out, _ := e.broker.SaveKey(context.Background(), "o2", "key-1", providers.ProviderOpenAI, "fake-key")
		keyDone <- out
	}()
	recv(t, blocked, "key check")
	waited := make(chan Outcome, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		o, _ := e.broker.Wait(ctx, "o1", first.LoginID)
		waited <- o
	}()
	time.Sleep(20 * time.Millisecond)
	e.broker.ReleaseOwner("o1")
	if o := recv(t, waited, "wait"); o.Status != OutcomeCancelled {
		t.Fatalf("released login: %+v", o)
	}
	noEvent(t, keyDone, "end of the other owner's key check")
	// The other owner is untouched: cancelling it ends it.
	if before, err := e.broker.Cancel("o2", "key-1"); err != nil || !before {
		t.Fatalf("cancel: %v %v", before, err)
	}
	if o := recv(t, keyDone, "key outcome"); o.Status != OutcomeCancelled {
		t.Fatalf("outcome: %+v", o)
	}
	// The slot of the released login is free.
	again := startDevice(t, e, "o3")
	_, _ = e.broker.Cancel("o3", again.LoginID)
}

func TestBrokerLogoutReportsEnvAndForgetsCache(t *testing.T) {
	e := newBrokerEnv(t, brokerSetup{env: map[string]string{"ANTHROPIC_API_KEY": "fake-env-key"}})
	defer e.done()
	counts := map[string]int{}
	for i := range e.svc.Methods {
		provider := e.svc.Methods[i].Provider
		e.svc.Methods[i].Forget = func() { counts[provider]++ }
	}
	ctx := context.Background()
	_, rev, _ := e.store.Read(ctx, providers.ProviderAnthropic)
	if _, err := e.store.Replace(ctx, providers.ProviderAnthropic, rev, settings.Credential{Method: "api-key", APIKey: "saved"}); err != nil {
		t.Fatal(err)
	}
	info, err := e.broker.Logout(ctx, providers.ProviderAnthropic, "")
	if err != nil {
		t.Fatal(err)
	}
	if info.EnvVar != "ANTHROPIC_API_KEY" {
		t.Fatalf("info: %+v", info)
	}
	if c := e.saved(providers.ProviderAnthropic); c.Method != "" {
		t.Fatalf("record kept: %+v", c)
	}
	if counts[providers.ProviderAnthropic] == 0 || counts[providers.ProviderOpenAI] != 0 || counts[providers.ProviderXAI] != 0 {
		t.Fatalf("forget calls: %v", counts)
	}
	// A provider with no env key reports none.
	if info, err = e.broker.Logout(ctx, providers.ProviderOpenAI, ""); err != nil || info.EnvVar != "" {
		t.Fatalf("openai: %+v %v", info, err)
	}
	if _, err = e.broker.Logout(ctx, "other", ""); !errors.Is(err, ErrMethod) {
		t.Fatalf("unknown provider: %v", err)
	}
}

func TestBrokerGenerationAndChangeCallback(t *testing.T) {
	e := newBrokerEnv(t, brokerSetup{checkKey: func(_ context.Context, _, key string) error {
		if key == "bad-key" {
			return &KeyRefusal{HTTPStatus: 401}
		}
		return nil
	}})
	defer e.done()
	ctx := context.Background()
	if out, _ := e.broker.SaveKey(ctx, "o1", "k1", providers.ProviderOpenAI, "bad-key"); out.Status != OutcomeRefused {
		t.Fatalf("outcome: %+v", out)
	}
	noEvent(t, e.changes, "change after a refusal")
	if out, _ := e.broker.SaveKey(ctx, "o1", "k2", providers.ProviderOpenAI, "good-key"); out.Generation != 1 {
		t.Fatalf("outcome: %+v", out)
	}
	if ev := recv(t, e.changes, "save change"); ev != (changeEvent{1, providers.ProviderOpenAI, ChangeSaved}) {
		t.Fatalf("change: %+v", ev)
	}
	// A removal with a method that does not match fails and changes nothing.
	if _, err := e.broker.Logout(ctx, providers.ProviderOpenAI, "openai-chatgpt"); !errors.Is(err, ErrMethod) {
		t.Fatalf("mismatch: %v", err)
	}
	noEvent(t, e.changes, "change after a failed removal")
	if _, err := e.broker.Logout(ctx, providers.ProviderOpenAI, ""); err != nil {
		t.Fatal(err)
	}
	if ev := recv(t, e.changes, "removal change"); ev != (changeEvent{2, providers.ProviderOpenAI, ChangeRemoved}) {
		t.Fatalf("change: %+v", ev)
	}
	if g := e.broker.Generation(); g != 2 {
		t.Fatalf("generation: %d", g)
	}
}

func TestBrokerKeyCancelBeforeCommitSavesNothing(t *testing.T) {
	blocked := make(chan struct{})
	e := newBrokerEnv(t, brokerSetup{checkKey: func(ctx context.Context, _, _ string) error {
		close(blocked)
		<-ctx.Done()
		return ctx.Err()
	}})
	defer e.done()
	result := make(chan Outcome, 1)
	go func() {
		out, _ := e.broker.SaveKey(context.Background(), "o1", "key-1", providers.ProviderOpenAI, "fake-key")
		result <- out
	}()
	recv(t, blocked, "key check")
	before, err := e.broker.Cancel("o1", "key-1")
	if err != nil || !before {
		t.Fatalf("cancel: %v %v", before, err)
	}
	if out := recv(t, result, "outcome"); out.Status != OutcomeCancelled || out.Reason != ReasonCancelled {
		t.Fatalf("outcome: %+v", out)
	}
	if c := e.saved(providers.ProviderOpenAI); c.Method != "" {
		t.Fatalf("stored: %+v", c)
	}
	// After a commit a cancel reports that the key was saved.
	e2 := newBrokerEnv(t, brokerSetup{checkKey: func(context.Context, string, string) error { return nil }})
	defer e2.done()
	if out, _ := e2.broker.SaveKey(context.Background(), "o1", "key-2", providers.ProviderOpenAI, "fake-key"); out.Status != OutcomeSaved {
		t.Fatalf("outcome: %+v", out)
	}
	if before, err = e2.broker.Cancel("o1", "key-2"); err != nil || before {
		t.Fatalf("cancel after commit: %v %v", before, err)
	}
	if c := e2.saved(providers.ProviderOpenAI); c.APIKey != "fake-key" {
		t.Fatalf("saved key lost: %+v", c)
	}
}

func TestBrokerCancelAfterCommitGateKeepsKey(t *testing.T) {
	// The gate sits between the probe and the store write. A cancel that
	// arrives after the gate must not stop the write.
	e := newBrokerEnv(t, brokerSetup{})
	defer e.done()
	l, err := e.broker.reserve("o1", "key-1", providers.ProviderOpenAI, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.gate(); err != nil {
		t.Fatal(err)
	}
	if before, _ := e.broker.Cancel("o1", "key-1"); before {
		t.Fatal("cancel after the gate reported before commit")
	}
	if l.ctx.Err() != nil {
		t.Fatal("cancel after the gate stopped the write")
	}
	e.broker.release(l, Outcome{Status: OutcomeSignedIn})
}

func TestBrokerLoginOutlivesStartRequest(t *testing.T) {
	var polls atomic.Int32
	proceed := make(chan struct{})
	e := newBrokerEnv(t, brokerSetup{wait: immediateWait, transport: deviceTransport(t, func(*http.Request) (*http.Response, error) {
		if polls.Add(1) < 3 {
			return nativeResponse(400, `{"error":"authorization_pending"}`), nil
		}
		<-proceed
		return nativeResponse(200, xaiSuccess), nil
	})})
	defer e.done()
	ctx, cancel := context.WithCancel(context.Background())
	info, err := e.broker.Start(ctx, "o1", providers.ProviderXAI, "xai-oauth")
	if err != nil {
		t.Fatal(err)
	}
	cancel() // the request that started the login ends
	time.Sleep(30 * time.Millisecond)
	close(proceed)
	if o := e.wait("o1", info.LoginID); o.Status != OutcomeSignedIn {
		t.Fatalf("outcome: %+v", o)
	}
}

func TestBrokerWaitContextDoesNotCancelLogin(t *testing.T) {
	e := newBrokerEnv(t, brokerSetup{transport: deviceTransport(t, pendingPoll), wait: blockingWait})
	defer e.done()
	info := startDevice(t, e, "o1")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := e.broker.Wait(ctx, "o1", info.LoginID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait: %v", err)
	}
	if before, err := e.broker.Cancel("o1", info.LoginID); err != nil || !before {
		t.Fatalf("login ended with the wait: %v %v", before, err)
	}
}

func TestBrokerLogoutCancelsPendingSignIn(t *testing.T) {
	e := newBrokerEnv(t, brokerSetup{transport: deviceTransport(t, pendingPoll), wait: blockingWait})
	defer e.done()
	ctx := context.Background()
	_, rev, _ := e.store.Read(ctx, providers.ProviderXAI)
	if _, err := e.store.Replace(ctx, providers.ProviderXAI, rev, settings.Credential{Method: "api-key", APIKey: "old-key"}); err != nil {
		t.Fatal(err)
	}
	info := startDevice(t, e, "owner-a")
	if _, err := e.broker.Logout(ctx, providers.ProviderXAI, ""); err != nil {
		t.Fatal(err)
	}
	if o := e.wait("owner-a", info.LoginID); o.Status != OutcomeCancelled || o.Reason != ReasonRemoved {
		t.Fatalf("outcome: %+v", o)
	}
	if c := e.saved(providers.ProviderXAI); c.Method != "" {
		t.Fatalf("record kept: %+v", c)
	}
	// Only the removal raised the generation.
	if ev := recv(t, e.changes, "removal change"); ev.change != ChangeRemoved {
		t.Fatalf("change: %+v", ev)
	}
	noEvent(t, e.changes, "second change")
}

func TestBrokerLogoutCancelsKeyCheckBeforeCommit(t *testing.T) {
	blocked := make(chan struct{})
	e := newBrokerEnv(t, brokerSetup{checkKey: func(ctx context.Context, _, _ string) error {
		close(blocked)
		<-ctx.Done()
		return ctx.Err()
	}})
	defer e.done()
	result := make(chan Outcome, 1)
	go func() {
		out, _ := e.broker.SaveKey(context.Background(), "owner-a", "key-1", providers.ProviderOpenAI, "fake-key")
		result <- out
	}()
	recv(t, blocked, "key check")
	if _, err := e.broker.Logout(context.Background(), providers.ProviderOpenAI, ""); err != nil {
		t.Fatal(err)
	}
	if out := recv(t, result, "outcome"); out.Status != OutcomeCancelled || out.Reason != ReasonRemoved {
		t.Fatalf("outcome: %+v", out)
	}
	if c := e.saved(providers.ProviderOpenAI); c.Method != "" {
		t.Fatalf("stored: %+v", c)
	}
}

func TestBrokerRemovalsSerializedPerProvider(t *testing.T) {
	polling := make(chan struct{})
	release := make(chan struct{})
	e := newBrokerEnv(t, brokerSetup{wait: immediateWait, transport: deviceTransport(t, func(*http.Request) (*http.Response, error) {
		close(polling)
		<-release // ignores the cancel, so the sign-in ends late
		return nativeResponse(400, `{"error":"access_denied"}`), nil
	})})
	defer e.done()
	info := startDevice(t, e, "owner-a")
	recv(t, polling, "token poll")
	var order []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	remove := func(name, provider string) {
		defer wg.Done()
		if _, err := e.broker.Logout(context.Background(), provider, ""); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		mu.Lock()
		order = append(order, name)
		mu.Unlock()
	}
	wg.Add(1)
	go remove("first", providers.ProviderXAI)
	time.Sleep(30 * time.Millisecond)
	wg.Add(1)
	go remove("second", providers.ProviderXAI)
	time.Sleep(30 * time.Millisecond)
	// A removal of another provider does not wait.
	wg.Add(1)
	remove("other", providers.ProviderOpenAI)
	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "other" {
		t.Fatalf("removals before release: %v", got)
	}
	close(release)
	wg.Wait()
	if len(order) != 3 || order[1] != "first" || order[2] != "second" {
		t.Fatalf("order: %v", order)
	}
	if o := e.wait("owner-a", info.LoginID); o.Status != OutcomeCancelled || o.Reason != ReasonRemoved {
		t.Fatalf("outcome: %+v", o)
	}
}

func TestBrokerCloseCancelsAndJoins(t *testing.T) {
	var exited atomic.Int32
	blocked := make(chan struct{})
	e := newBrokerEnv(t, brokerSetup{wait: blockingWait, transport: deviceTransport(t, pendingPoll), checkKey: func(ctx context.Context, _, _ string) error {
		close(blocked)
		<-ctx.Done()
		exited.Add(1)
		return ctx.Err()
	}})
	startDevice(t, e, "o1")
	go func() {
		_, _ = e.broker.SaveKey(context.Background(), "o2", "key-1", providers.ProviderOpenAI, "fake-key")
	}()
	recv(t, blocked, "key check")
	e.broker.Close()
	if exited.Load() != 1 {
		t.Fatal("Close returned before the key check ended")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	if err := e.svc.DrainRefresh(ctx); err != nil || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("drain: %v after %v", err, time.Since(started))
	}
	if _, err := e.broker.Start(context.Background(), "o1", providers.ProviderXAI, "xai-oauth"); !errors.Is(err, ErrBrokerClosed) {
		t.Fatalf("start after close: %v", err)
	}
	e.broker.Close() // a second Close is harmless
	goleak.VerifyNone(t, e.ignore)
}

func TestBrokerNoGoroutineLeak(t *testing.T) {
	e := newBrokerEnv(t, brokerSetup{wait: blockingWait, transport: deviceTransport(t, pendingPoll), checkKey: func(ctx context.Context, _, _ string) error {
		<-ctx.Done()
		return ctx.Err()
	}})
	a := startDevice(t, e, "o1")
	if _, err := e.broker.Cancel("o1", a.LoginID); err != nil {
		t.Fatal(err)
	}
	e.wait("o1", a.LoginID)
	b := startDevice(t, e, "o2")
	e.broker.ReleaseOwner("o2")
	_ = b
	go func() {
		_, _ = e.broker.SaveKey(context.Background(), "o3", "key-1", providers.ProviderOpenAI, "fake-key")
	}()
	time.Sleep(30 * time.Millisecond)
	e.done()
}

func TestBrokerChatGPTNeedsHostIDAndUsesBrowserKind(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	e := newBrokerEnv(t, brokerSetup{})
	_, err := e.broker.Start(context.Background(), "o1", providers.ProviderOpenAI, "openai-chatgpt")
	var failed *StartError
	if !errors.As(err, &failed) || failed.Outcome.Status != OutcomeFailed || failed.Outcome.Reason != ReasonInternal {
		t.Fatalf("no host id: %v", err)
	}
	e.done()
	e = newBrokerEnv(t, brokerSetup{hostID: func(context.Context) (string, error) { return "12345678-1234-1234-1234-123456789abc", nil }})
	defer e.done()
	info, err := e.broker.Start(context.Background(), "o1", providers.ProviderOpenAI, "openai-chatgpt")
	if err != nil || info.Kind != KindBrowser || !strings.HasPrefix(info.URL, "https://auth.openai.com/") {
		t.Fatalf("start: %+v %v", info, err)
	}
	if _, err = e.broker.Cancel("o1", info.LoginID); err != nil {
		t.Fatal(err)
	}
}

func TestBrokerKeySavedPrivately(t *testing.T) {
	home := t.TempDir()
	store, err := settings.NewAuthStore(home)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: store, Methods: []Method{{Provider: providers.ProviderOpenAI, ID: "api-key", API: providers.APIOpenAIResponses}}}
	var probes atomic.Int32
	b := NewBroker(svc, BrokerOptions{CheckKey: func(context.Context, string, string) error { probes.Add(1); return nil }})
	defer b.Close()
	if out, err := b.SaveKey(context.Background(), "o1", "k1", providers.ProviderOpenAI, "fake-key"); err != nil || out.Status != OutcomeSaved {
		t.Fatalf("save: %+v %v", out, err)
	}
	if c, _, err := store.Read(context.Background(), providers.ProviderOpenAI); err != nil || c.APIKey != "fake-key" || c.Generation != 1 || probes.Load() != 1 {
		t.Fatalf("record: %+v %v probes %d", c, err, probes.Load())
	}
	info, err := os.Stat(filepath.Join(home, "auth.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("auth.json: %v %v", info, err)
	}
}

func TestBrokerKeyCancelAfterProbeBeforeWriteSavesNothing(t *testing.T) {
	probing := make(chan struct{})
	release := make(chan struct{})
	e := newBrokerEnv(t, brokerSetup{checkKey: func(context.Context, string, string) error {
		close(probing)
		<-release // the probe ignores the cancel and says the key is good
		return nil
	}})
	defer e.done()
	result := make(chan Outcome, 1)
	go func() {
		out, _ := e.broker.SaveKey(context.Background(), "o1", "key-1", providers.ProviderOpenAI, "fake-key")
		result <- out
	}()
	recv(t, probing, "key check")
	if before, err := e.broker.Cancel("o1", "key-1"); err != nil || !before {
		t.Fatalf("cancel: %v %v", before, err)
	}
	close(release)
	if out := recv(t, result, "outcome"); out.Status != OutcomeCancelled {
		t.Fatalf("outcome: %+v", out)
	}
	if c := e.saved(providers.ProviderOpenAI); c.Method != "" {
		t.Fatalf("stored after cancel: %+v", c)
	}
	noEvent(t, e.changes, "change")
}

func TestBrokerLateInputIsNotKept(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	gate := make(chan struct{})
	e := newBrokerEnv(t, brokerSetup{transport: anthropicTransport(t, gate)})
	defer e.done()
	info, err := e.broker.Start(context.Background(), "o1", providers.ProviderAnthropic, "anthropic-oauth")
	if err != nil {
		t.Fatal(err)
	}
	q := authorizeParams(t, info.URL)
	callbackRoundTrip(t, q.Get("redirect_uri")+"?code=private&state="+url.QueryEscape(q.Get("state")))
	// The callback won and the token exchange waits on the gate; the client pastes anyway.
	deadline := time.Now().Add(5 * time.Second)
	for {
		err = e.broker.Input("o1", info.LoginID, "late-secret-value")
		if err != nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(gate)
	if o := e.wait("o1", info.LoginID); o.Status != OutcomeSignedIn {
		t.Fatalf("outcome: %+v", o)
	}
	e.broker.mu.Lock()
	defer e.broker.mu.Unlock()
	for _, l := range e.broker.logins {
		if len(l.input) != 0 {
			t.Fatal("late value kept on the login")
		}
	}
}

func TestBrokerHungRequestIsNetworkNotExpired(t *testing.T) {
	e := newBrokerEnv(t, brokerSetup{checkKey: func(context.Context, string, string) error { return context.DeadlineExceeded }})
	defer e.done()
	out, err := e.broker.SaveKey(context.Background(), "o1", "key-1", providers.ProviderOpenAI, "fake-key")
	if err != nil || out.Status != OutcomeFailed || out.Reason != ReasonNetwork {
		t.Fatalf("outcome: %+v %v", out, err)
	}
}

func TestBrokerReleaseOwnerStopsLaterAdmission(t *testing.T) {
	e := newBrokerEnv(t, brokerSetup{})
	defer e.done()
	e.broker.ReleaseOwner("gone-owner")
	_, err := e.broker.SaveKey(context.Background(), "gone-owner", "late-key", providers.ProviderTokenPlan, "fake-key")
	if !errors.Is(err, ErrLoginNotFound) {
		t.Fatalf("late key save: %v", err)
	}
	if c := e.saved(providers.ProviderTokenPlan); c.Method != "" {
		t.Fatal("a gone client saved a key")
	}
}

func TestBrokerReleaseOwnerStopsStartBeforeReservation(t *testing.T) {
	testsupport.LockOAuthPorts(t)
	entered, proceed := make(chan struct{}), make(chan struct{})
	e := newBrokerEnv(t, brokerSetup{hostID: func(context.Context) (string, error) {
		close(entered)
		<-proceed
		return "12345678-1234-1234-1234-123456789abc", nil
	}})
	defer e.done()
	result := make(chan error, 1)
	go func() {
		_, err := e.broker.Start(context.Background(), "gone-owner", providers.ProviderOpenAI, "openai-chatgpt")
		result <- err
	}()
	recv(t, entered, "host id read")
	e.broker.ReleaseOwner("gone-owner")
	close(proceed)
	if err := recv(t, result, "start result"); !errors.Is(err, ErrLoginNotFound) {
		t.Fatalf("late start: %v", err)
	}
}

func TestBrokerSavedKeyForgetsProviderCache(t *testing.T) {
	e := newBrokerEnv(t, brokerSetup{checkKey: func(context.Context, string, string) error { return nil }})
	defer e.done()
	counts := map[string]int{}
	for i := range e.svc.Methods {
		provider := e.svc.Methods[i].Provider
		e.svc.Methods[i].Forget = func() { counts[provider]++ }
	}
	out, err := e.broker.SaveKey(context.Background(), "o1", "key-1", providers.ProviderOpenAI, "fake-key")
	if err != nil || out.Status != OutcomeSaved {
		t.Fatalf("save: %+v %v", out, err)
	}
	if counts[providers.ProviderOpenAI] == 0 || counts[providers.ProviderAnthropic] != 0 || counts[providers.ProviderXAI] != 0 {
		t.Fatalf("forget calls: %v", counts)
	}
}
