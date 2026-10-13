package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

// fakeProviderSetup is a ProviderSetup that records its calls. It holds no
// credentials: each answer comes from a field that the test sets.
type fakeProviderSetup struct {
	mu      sync.Mutex
	calls   []string
	usable  map[string]bool
	err     error
	keySeen string
	// logout runs inside Logout, as the broker change callback does.
	logout func(req protocol.ACPLogoutRequest) protocol.ACPLogoutResult
	// waitDone ends a LoginWait.
	waitCtxDone bool
}

func (f *fakeProviderSetup) note(call string) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
}

func (f *fakeProviderSetup) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeProviderSetup) List(context.Context) (protocol.ACPProvidersResult, error) {
	f.note("list")
	if f.err != nil {
		return protocol.ACPProvidersResult{}, f.err
	}
	return protocol.ACPProvidersResult{Generation: 3, Options: []protocol.ACPLoginOption{{ProviderID: "openai", MethodID: "api-key", Name: "OpenAI API key", State: protocol.ACPLoginStateNone}}}, nil
}

func (f *fakeProviderSetup) SaveKey(_ context.Context, owner string, req protocol.ACPAPIKeyRequest) (protocol.ACPLoginOutcome, error) {
	f.note("save_key:" + owner + ":" + req.LoginID)
	f.mu.Lock()
	f.keySeen = req.Key
	f.mu.Unlock()
	if f.err != nil {
		return protocol.ACPLoginOutcome{}, f.err
	}
	return protocol.ACPLoginOutcome{LoginID: req.LoginID, Status: protocol.ACPLoginStatusSaved, Generation: 4}, nil
}

func (f *fakeProviderSetup) LoginStart(_ context.Context, owner string, req protocol.ACPLoginStartRequest) (protocol.ACPLoginStartResult, error) {
	f.note("start:" + owner + ":" + req.ProviderID + ":" + req.MethodID)
	if f.err != nil {
		return protocol.ACPLoginStartResult{}, f.err
	}
	return protocol.ACPLoginStartResult{LoginID: "login-1", Kind: protocol.ACPLoginKindDevice, URL: "https://example.test/device", UserCode: "ABCD-EFGH"}, nil
}

func (f *fakeProviderSetup) LoginInput(owner string, req protocol.ACPLoginInputRequest) error {
	f.note("input:" + owner + ":" + req.LoginID)
	return f.err
}

func (f *fakeProviderSetup) LoginWait(ctx context.Context, owner, loginID string) (protocol.ACPLoginOutcome, error) {
	f.note("wait:" + owner + ":" + loginID)
	if f.err != nil {
		return protocol.ACPLoginOutcome{}, f.err
	}
	if f.waitCtxDone {
		<-ctx.Done()
		return protocol.ACPLoginOutcome{}, ctx.Err()
	}
	return protocol.ACPLoginOutcome{LoginID: loginID, Status: protocol.ACPLoginStatusCancelled, Reason: protocol.ACPLoginReasonRemoved}, nil
}

func (f *fakeProviderSetup) LoginCancel(owner, loginID string) (bool, error) {
	f.note("cancel:" + owner + ":" + loginID)
	return true, f.err
}

func (f *fakeProviderSetup) Logout(_ context.Context, req protocol.ACPLogoutRequest) (protocol.ACPLogoutResult, error) {
	f.note("logout:" + req.ProviderID)
	if f.err != nil {
		return protocol.ACPLogoutResult{}, f.err
	}
	if f.logout != nil {
		return f.logout(req), nil
	}
	return protocol.ACPLogoutResult{Removed: true, Generation: 5}, nil
}

func (f *fakeProviderSetup) ReleaseClient(owner string) { f.note("release:" + owner) }

func (f *fakeProviderSetup) Usable(_ context.Context, provider string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	ok, set := f.usable[provider]
	return !set || ok
}

func (f *fakeProviderSetup) setUsable(provider string, ok bool) {
	f.mu.Lock()
	if f.usable == nil {
		f.usable = map[string]bool{}
	}
	f.usable[provider] = ok
	f.mu.Unlock()
}

// providerSetupPeer is an adapter peer on the leader link with a fake provider setup.
func providerSetupPeer(t *testing.T, steps stepsFor, edit func(*Config)) (*adapterPeer, *fakeProviderSetup) {
	t.Helper()
	acc := &fakeProviderSetup{}
	cfg := testConfig(steps, nil)
	cfg.RequireRoute = true
	cfg.ProviderSetup = acc
	if edit != nil {
		edit(&cfg)
	}
	p := newAdapterPeer(t, cfg, nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	return p, acc
}

// rstate reads the state of a session on the leader link.
func (p *adapterPeer) rstate(sid string) protocol.ACPStateResult {
	p.t.Helper()
	var st protocol.ACPStateResult
	p.ok(protocol.ACPState, withRoute(map[string]any{"sessionId": sid}, route("c1", 1, true, "")), &st)
	return st
}

// rfollow follows a session on the leader link.
func (p *adapterPeer) rfollow(sid string, cursor *protocol.ACPCursor) protocol.ACPFollowResult {
	p.t.Helper()
	params := map[string]any{"sessionId": sid}
	if cursor != nil {
		params["cursor"] = cursor
	}
	var res protocol.ACPFollowResult
	p.ok(protocol.ACPFollow, withRoute(params, route("c1", 1, true, "")), &res)
	require.NotEmpty(p.t, res.SubscriptionID)
	return res
}

// providerSetupParams is a provider call as the router sends it: a client id, no session.
func providerSetupParams(client string, params map[string]any) map[string]any {
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = route(client, 0, false, "")
	return params
}

func TestProviderMethodsNeedLeaderRoute(t *testing.T) {
	p, acc := providerSetupPeer(t, says("x"), nil)

	var list protocol.ACPProvidersResult
	p.ok(protocol.ACPProvidersList, providerSetupParams("c1", nil), &list)
	require.Equal(t, uint64(3), list.Generation)
	require.Len(t, list.Options, 1)

	var saved protocol.ACPLoginOutcome
	p.ok(protocol.ACPProvidersAPIKey, providerSetupParams("c1", map[string]any{"loginId": "k1", "providerId": "openai", "key": "sk-fake-1"}), &saved)
	require.Equal(t, protocol.ACPLoginStatusSaved, saved.Status)
	require.Equal(t, "k1", saved.LoginID)

	var started protocol.ACPLoginStartResult
	p.ok(protocol.ACPLoginStart, providerSetupParams("c2", map[string]any{"providerId": "xai", "methodId": "xai-oauth"}), &started)
	require.Equal(t, protocol.ACPLoginKindDevice, started.Kind)
	p.ok(protocol.ACPLoginInput, providerSetupParams("c2", map[string]any{"loginId": "login-1", "code": "fake-code"}), nil)
	var out protocol.ACPLoginOutcome
	p.ok(protocol.ACPLoginWait, providerSetupParams("c2", map[string]any{"loginId": "login-1"}), &out)
	require.Equal(t, protocol.ACPLoginReasonRemoved, out.Reason)
	var cancel protocol.ACPLoginCancelResult
	p.ok(protocol.ACPLoginCancel, providerSetupParams("c2", map[string]any{"loginId": "login-1"}), &cancel)
	require.True(t, cancel.Cancelled)
	var logout protocol.ACPLogoutResult
	p.ok(protocol.ACPProvidersLogout, providerSetupParams("c1", map[string]any{"providerId": "openai", "methodId": "api-key"}), &logout)
	require.True(t, logout.Removed)
	p.ok(protocol.ACPProvidersReleaseClient, providerSetupParams("c2", nil), nil)

	require.Equal(t, []string{
		"list", "save_key:c1:k1", "start:c2:xai:xai-oauth", "input:c2:login-1", "wait:c2:login-1",
		"cancel:c2:login-1", "logout:openai", "release:c2",
	}, acc.seen(), "the route client id is the owner of every call")

	// A call with no route, or with a route that has no client, is refused.
	f := p.call(protocol.ACPProvidersList, map[string]any{})
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
	f = p.call(protocol.ACPProvidersList, map[string]any{"_meta": map[string]any{protocol.ACPRouteMetaKey: map[string]any{"driverGen": "0"}}})
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
	require.Len(t, acc.seen(), 8, "a refused call reaches no provider setup")
}

func TestProviderMethodsRefuseForgedRouteOnStdio(t *testing.T) {
	// The stdio adapter has neither RequireRoute nor ProviderSetup. A forged route
	// must not reach a broker: there is none, and the answer is the same.
	p := newAdapterPeer(t, testConfig(says("x"), nil), nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	for _, m := range []string{
		protocol.ACPProvidersList, protocol.ACPProvidersAPIKey, protocol.ACPLoginStart, protocol.ACPLoginInput,
		protocol.ACPLoginWait, protocol.ACPLoginCancel, protocol.ACPProvidersLogout, protocol.ACPProvidersReleaseClient,
	} {
		f := p.call(m, providerSetupParams("forged", map[string]any{"loginId": "x", "providerId": "openai", "key": "sk-fake-2"}))
		require.Equal(t, protocol.ACPErrUnsupported, f.errKind(t), m)
		f = p.call(m, map[string]any{"loginId": "x"})
		require.Equal(t, protocol.ACPErrUnsupported, f.errKind(t), m)
	}

	// A leader link without provider setup answers the same.
	cfg := testConfig(says("x"), nil)
	cfg.RequireRoute = true
	leaderNoProviderSetup := newAdapterPeer(t, cfg, nil)
	leaderNoProviderSetup.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	f := leaderNoProviderSetup.call(protocol.ACPProvidersList, providerSetupParams("c1", nil))
	require.Equal(t, protocol.ACPErrUnsupported, f.errKind(t))

	// Provider setup without the leader link is not served either.
	acc := &fakeProviderSetup{}
	cfg2 := testConfig(says("x"), nil)
	cfg2.ProviderSetup = acc
	editorWithProviderSetup := newAdapterPeer(t, cfg2, nil)
	editorWithProviderSetup.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	f = editorWithProviderSetup.call(protocol.ACPProvidersAPIKey, providerSetupParams("forged", map[string]any{"loginId": "k", "providerId": "openai", "key": "sk-fake-3"}))
	require.Equal(t, protocol.ACPErrUnsupported, f.errKind(t))
	require.Empty(t, acc.seen())
	require.Empty(t, acc.keySeen)
}

func TestProviderMethodsMapErrorsWithoutText(t *testing.T) {
	secret := "sk-fake-secret-value"
	cases := []struct {
		name string
		err  error
		kind protocol.ACPErrorKind
	}{
		{"busy", fmt.Errorf("%w: %s", ErrProviderBusy, secret), protocol.ACPErrProviderBusy},
		{"unknown login", fmt.Errorf("%w: %s", ErrLoginNotFound, secret), protocol.ACPErrInvalidParams},
		{"login state", fmt.Errorf("%w: %s", ErrLoginState, secret), protocol.ACPErrInvalidState},
		{"unknown option", fmt.Errorf("%w: %s", ErrLoginMethod, secret), protocol.ACPErrInvalidParams},
		{"closed", fmt.Errorf("%w: %s", ErrHostClosed, secret), protocol.ACPErrDisposed},
		{"other", errors.New("provider said " + secret), protocol.ACPErrInternal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, acc := providerSetupPeer(t, says("x"), nil)
			acc.err = c.err
			calls := map[string]map[string]any{
				protocol.ACPProvidersList:   nil,
				protocol.ACPProvidersAPIKey: {"loginId": "k", "providerId": "openai", "key": secret},
				protocol.ACPLoginStart:      {"providerId": "xai", "methodId": "xai-oauth"},
				protocol.ACPLoginInput:      {"loginId": "l", "code": secret},
				protocol.ACPLoginWait:       {"loginId": "l"},
				protocol.ACPLoginCancel:     {"loginId": "l"},
				protocol.ACPProvidersLogout: {"providerId": "openai", "methodId": "api-key"},
			}
			for method, params := range calls {
				f := p.call(method, providerSetupParams("c1", params))
				require.Equal(t, c.kind, f.errKind(t), method)
				require.NotContains(t, string(f.Error.Message), secret, method)
				require.NotContains(t, string(f.Error.Data), secret, method)
				require.Equal(t, protocol.ACPErrorCode(c.kind), f.Error.Code, method)
			}
			for _, line := range p.snapshotLines() {
				require.NotContains(t, line, secret, "no frame repeats a secret")
			}
		})
	}
}

func TestProviderWaitEndsWithRequestContext(t *testing.T) {
	p, acc := providerSetupPeer(t, says("x"), nil)
	acc.waitCtxDone = true
	id := p.send(protocol.ACPLoginWait, providerSetupParams("c1", map[string]any{"loginId": "l"}))
	p.waitFor("wait started", func() bool { return len(acc.seen()) == 1 })
	p.notify("$/cancel_request", map[string]any{"requestId": id})
	f := p.await(id)
	require.Equal(t, protocol.ACPErrCancelled, f.errKind(t))
}

func TestModelsUsableOnly(t *testing.T) {
	p, acc := providerSetupPeer(t, says("x"), nil)
	acc.setUsable("openai", false)
	acc.setUsable("anthropic", true)
	acc.setUsable("xai", false)
	acc.setUsable("alibaba-token-plan", false)
	sid := p.newRouted("c1", "")

	var all protocol.ACPModelsResult
	p.ok(protocol.ACPModels, withRoute(map[string]any{"sessionId": sid}, route("c1", 1, true, "")), &all)
	require.False(t, all.UsableOnly, "no flag gives the full catalog and no echo")
	require.Len(t, all.Models, len(providers.AvailableModels()))

	var usable protocol.ACPModelsResult
	p.ok(protocol.ACPModels, withRoute(map[string]any{"sessionId": sid, "usableOnly": true}, route("c1", 1, true, "")), &usable)
	require.True(t, usable.UsableOnly)
	require.NotEmpty(t, usable.Models)
	require.Less(t, len(usable.Models), len(all.Models))
	for _, m := range usable.Models {
		require.Equal(t, "anthropic", m.Provider)
	}

	// Without provider setup the flag cannot filter and is not echoed.
	plain := newAdapterPeer(t, testConfig(says("x"), nil), nil)
	psid := plain.start()
	var res protocol.ACPModelsResult
	plain.ok(protocol.ACPModels, map[string]any{"sessionId": psid, "usableOnly": true}, &res)
	require.False(t, res.UsableOnly)
	require.Len(t, res.Models, len(providers.AvailableModels()))
}

// noModelConfig starts every session with no model.
func noModelConfig(t *testing.T) Config {
	cfg := testConfig(says("x"), func(c *agent.Config) { c.Model = providers.Model{} })
	return cfg
}

func TestModelsAndStateEmptyWhenNoModel(t *testing.T) {
	p := newAdapterPeer(t, noModelConfig(t), nil)
	sid := p.start()
	st := p.state(sid)
	require.Equal(t, "", st.ModelID)
	var models protocol.ACPModelsResult
	p.ok(protocol.ACPModels, map[string]any{"sessionId": sid}, &models)
	require.Equal(t, "", models.Current)
	raw := p.call(protocol.ACPState, map[string]any{"sessionId": sid})
	require.NotContains(t, string(raw.Result), "/@")
}

func TestPromptWithoutModelAnswersNoModel(t *testing.T) {
	p := newAdapterPeer(t, noModelConfig(t), nil)
	sid := p.start()
	f := p.call("session/prompt", promptParams(sid, "hello"))
	require.Equal(t, protocol.ACPErrNoModel, f.errKind(t))
	require.Equal(t, protocol.ACPCodeNoModel, f.Error.Code)
	f = p.call(protocol.ACPContinue, map[string]any{"sessionId": sid})
	require.Equal(t, protocol.ACPErrNoModel, f.errKind(t))
	for _, line := range p.snapshotLines() {
		require.NotContains(t, line, "agent_start", "a refused prompt starts no run")
	}
	require.Empty(t, p.updateKinds())
	require.Equal(t, 0, p.state(sid).MessageCount)
}

func TestSteerFollowUpWithoutModelAnswerNoModel(t *testing.T) {
	p := newAdapterPeer(t, noModelConfig(t), nil)
	sid := p.start()
	for _, m := range []string{protocol.ACPSteer, protocol.ACPFollowUp} {
		f := p.call(m, textParams(sid, "later"))
		require.Equal(t, protocol.ACPErrNoModel, f.errKind(t), m)
	}
	st := p.state(sid)
	require.Empty(t, st.Steering)
	require.Empty(t, st.FollowUp)
	require.False(t, st.Running)
}

func TestLogoutReportsClearedModelOfCallerSession(t *testing.T) {
	release := make(chan struct{})
	rel := closeOnce(release)
	t.Cleanup(rel)
	p, acc := providerSetupPeer(t, func() []faux.Step { return []faux.Step{heldStep(release, "x")} }, nil)
	acc.logout = func(req protocol.ACPLogoutRequest) protocol.ACPLogoutResult {
		acc.setUsable(req.ProviderID, false)
		p.a.ProvidersChanged(6, req.ProviderID, protocol.ACPProviderChangeRemoved)
		return protocol.ACPLogoutResult{Removed: true, Generation: 6}
	}
	logout := func(sid string) protocol.ACPLogoutResult {
		var res protocol.ACPLogoutResult
		p.ok(protocol.ACPProvidersLogout, providerSetupParams("c1", map[string]any{"providerId": "faux", "methodId": "api-key", "sessionId": sid}), &res)
		return res
	}

	// Idle caller: the model is cleared before the answer.
	idle := p.newRouted("c1", "")
	modelBefore := p.rstate(idle).ModelID
	require.NotEmpty(t, modelBefore)
	res := logout(idle)
	require.True(t, res.Removed)
	require.Equal(t, modelBefore, res.ClearedModel)
	require.False(t, res.ClearPending)
	require.Equal(t, "", p.rstate(idle).ModelID)

	// Running caller: the run keeps its model and the clear is pending.
	acc.setUsable("faux", true)
	busy := p.newRouted("c1", "")
	runID := p.send("session/prompt", withRoute(promptParams(busy, "go"), route("c1", 1, true, "")))
	p.running(busy)
	res = logout(busy)
	require.Equal(t, modelBefore, res.ClearedModel)
	require.True(t, res.ClearPending)
	require.Equal(t, modelBefore, p.rstate(busy).ModelID, "the active run keeps its model")
	rel()
	p.await(runID)
	require.Eventually(t, func() bool { return p.rstate(busy).ModelID == "" }, peerWait, 5*time.Millisecond)

	// A caller whose model is on another provider has nothing cleared.
	acc.setUsable("faux", true)
	other := p.newRouted("c1", "")
	var otherRes protocol.ACPLogoutResult
	p.ok(protocol.ACPProvidersLogout, providerSetupParams("c1", map[string]any{"providerId": "openai", "methodId": "api-key", "sessionId": other}), &otherRes)
	require.Empty(t, otherRes.ClearedModel)
	require.False(t, otherRes.ClearPending)
	require.NotEmpty(t, p.rstate(other).ModelID)
}

func TestHostLogoutClearsUnusableModels(t *testing.T) {
	p, acc := providerSetupPeer(t, says("x"), nil)
	target := providers.OpenAIGPT55()
	targetID := fmt.Sprintf("%s/%s@%s", target.Provider, target.ID, target.API)
	onOpenAI := p.newRouted("c1", "")
	p.ok(protocol.ACPSetModel, withRoute(map[string]any{"sessionId": onOpenAI, "modelId": targetID}, route("c1", 1, true, "")), nil)
	onFaux := p.newRouted("c1", "")
	fauxBefore := p.rstate(onFaux).ModelID
	require.Contains(t, fauxBefore, "faux/")

	sub := p.rfollow(onOpenAI, nil)

	acc.setUsable("openai", false)
	p.a.ProvidersChanged(7, "openai", protocol.ACPProviderChangeRemoved)

	require.Equal(t, "", p.rstate(onOpenAI).ModelID)
	require.Equal(t, fauxBefore, p.rstate(onFaux).ModelID, "a session on another provider keeps its model")
	p.waitFor("model_update to the follower", func() bool {
		for _, ev := range p.followEvents(sub.SubscriptionID) {
			if eventType(t, ev) == protocol.TypeModelUpdate {
				return true
			}
		}
		return false
	})
	notes := p.notes(protocol.ACPProvidersChanged)
	require.Len(t, notes, 1)
	var n protocol.ACPProvidersChangedNotification
	require.NoError(t, json.Unmarshal(notes[0].Params, &n))
	require.Equal(t, protocol.ACPProvidersChangedNotification{Generation: 7, ProviderID: "openai", Change: protocol.ACPProviderChangeRemoved}, n)

	// A removal that leaves the provider usable (an environment key) clears nothing.
	again := p.newRouted("c1", "")
	acc.setUsable("openai", true)
	p.ok(protocol.ACPSetModel, withRoute(map[string]any{"sessionId": again, "modelId": targetID}, route("c1", 1, true, "")), nil)
	p.a.ProvidersChanged(8, "openai", protocol.ACPProviderChangeRemoved)
	require.Equal(t, targetID, p.rstate(again).ModelID)

	// A saved credential never clears a model.
	acc.setUsable("openai", false)
	p.a.ProvidersChanged(9, "openai", protocol.ACPProviderChangeSaved)
	require.Equal(t, targetID, p.rstate(again).ModelID)
}

func TestFollowResumeCarriesModelUpdate(t *testing.T) {
	p, acc := providerSetupPeer(t, says("x"), nil)
	sid := p.newRouted("c1", "")
	first := p.rfollow(sid, nil)

	acc.setUsable("faux", false)
	p.a.ProvidersChanged(2, "faux", protocol.ACPProviderChangeRemoved)
	require.Eventually(t, func() bool { return p.rstate(sid).ModelID == "" }, peerWait, 5*time.Millisecond)

	// A client that resumes from the cursor it held gets the missed event once.
	resumed := p.rfollow(sid, &first.Cursor)
	require.True(t, resumed.Resumed)
	p.waitFor("replayed model_update", func() bool { return len(p.followEvents(resumed.SubscriptionID)) > 0 })
	var types []string
	for _, ev := range p.followEvents(resumed.SubscriptionID) {
		types = append(types, eventType(t, ev))
	}
	require.Equal(t, []string{protocol.TypeModelUpdate}, types)
	var ev protocol.ModelUpdate
	raw := p.followEvents(resumed.SubscriptionID)[0].Event
	require.NoError(t, json.Unmarshal(raw, &ev))
	require.Empty(t, ev.ID)
	require.Empty(t, ev.Provider)
}

func TestSetModelSavesDefaultOnlyWhenAsked(t *testing.T) {
	var saved []string
	var mu sync.Mutex
	failSave := false
	cfg := testConfig(says("x"), nil)
	cfg.SaveDefault = func(_ context.Context, id string) error {
		mu.Lock()
		defer mu.Unlock()
		if failSave {
			return errors.New("disk full at /secret/path")
		}
		saved = append(saved, id)
		return nil
	}
	cfg.DefaultModel = func(context.Context) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if len(saved) == 0 {
			return "", nil
		}
		return saved[len(saved)-1], nil
	}
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	target := providers.OpenAIGPT55()
	targetID := fmt.Sprintf("%s/%s@%s", target.Provider, target.ID, target.API)

	var models protocol.ACPModelsResult
	p.ok(protocol.ACPModels, map[string]any{"sessionId": sid}, &models)
	require.Empty(t, models.Default)

	var set protocol.ACPSetModelResult
	p.ok(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": targetID}, &set)
	require.False(t, set.DefaultSaved)
	require.Empty(t, saved, "no flag, nothing saved")

	f := p.call(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": "nope/none@x", "saveDefault": true})
	require.Equal(t, protocol.ACPErrInvalidModel, f.errKind(t))
	require.Empty(t, saved, "a refused switch saves nothing")

	p.ok(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": targetID, "saveDefault": true}, &set)
	require.True(t, set.DefaultSaved)
	require.Equal(t, []string{targetID}, saved)
	p.ok(protocol.ACPModels, map[string]any{"sessionId": sid}, &models)
	require.Equal(t, targetID, models.Default)

	mu.Lock()
	failSave = true
	mu.Unlock()
	other := providers.AnthropicSonnet46()
	otherID := fmt.Sprintf("%s/%s@%s", other.Provider, other.ID, other.API)
	f = p.call(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": otherID, "saveDefault": true})
	require.Nil(t, f.Error, "a failed save does not undo the switch")
	set = protocol.ACPSetModelResult{}
	require.NoError(t, json.Unmarshal(f.Result, &set))
	require.False(t, set.DefaultSaved)
	require.Equal(t, otherID, set.ModelID)
	require.NotContains(t, string(f.Result), "/secret/path")
}

func TestStateReportsModelFallbackUntilModelChanges(t *testing.T) {
	fb := &protocol.ACPModelFallback{SavedDefault: "openai/gpt-5.5@openai-responses", Reason: protocol.ACPFallbackNoCredential}
	cfg := testConfig(says("x"), nil)
	cfg.StartFallback = func(string) *protocol.ACPModelFallback { return fb }
	p := newAdapterPeer(t, cfg, nil)
	sid := p.start()
	st := p.state(sid)
	require.NotNil(t, st.ModelFallback)
	require.Equal(t, *fb, *st.ModelFallback)

	target := providers.OpenAIGPT55()
	p.ok(protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": fmt.Sprintf("%s/%s@%s", target.Provider, target.ID, target.API)}, nil)
	require.Nil(t, p.state(sid).ModelFallback)
}

func TestProviderCallsNeedInitialize(t *testing.T) {
	cfg := testConfig(says("x"), nil)
	cfg.RequireRoute = true
	cfg.ProviderSetup = &fakeProviderSetup{}
	p := newAdapterPeer(t, cfg, nil)
	f := p.call(protocol.ACPProvidersList, providerSetupParams("c1", nil))
	require.Equal(t, protocol.ACPErrNotInitialized, f.errKind(t))
	require.True(t, strings.Contains(f.Error.Message, "initialize"))
}

func TestModelSwitchAfterRemovalRefusesOldReadiness(t *testing.T) {
	checking, release := make(chan struct{}), make(chan struct{})
	rel := closeOnce(release)
	t.Cleanup(rel)
	cfg := testConfig(says("x"), func(c *agent.Config) {
		c.Ready = func(context.Context, providers.Model, string) error {
			close(checking)
			<-release
			return nil
		}
	})
	acc := &fakeProviderSetup{}
	cfg.RequireRoute, cfg.ProviderSetup = true, acc
	p := newAdapterPeer(t, cfg, nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	sid := p.newRouted("c1", "")
	before := p.rstate(sid).ModelID
	acc.logout = func(req protocol.ACPLogoutRequest) protocol.ACPLogoutResult {
		acc.setUsable(req.ProviderID, false)
		p.a.ProvidersChanged(1, req.ProviderID, protocol.ACPProviderChangeRemoved)
		return protocol.ACPLogoutResult{Removed: true, Generation: 1}
	}
	id := p.send(protocol.ACPSetModel, withRoute(map[string]any{
		"sessionId": sid, "modelId": ModelID(providers.OpenAIGPT55()), "saveDefault": true,
	}, route("c1", 1, true, "")))
	select {
	case <-checking:
	case <-time.After(peerWait):
		t.Fatal("the model readiness check did not start")
	}
	p.ok(protocol.ACPProvidersLogout, providerSetupParams("c2", map[string]any{
		"providerId": "openai", "methodId": "api-key",
	}), nil)
	rel()
	f := p.await(id)
	require.Equal(t, protocol.ACPErrNoAPIKey, f.errKind(t))
	require.Equal(t, before, p.rstate(sid).ModelID, "the usable model stays selected")
}

func TestNewSessionDoesNotKeepProviderRemovedDuringBuild(t *testing.T) {
	built, release := make(chan struct{}), make(chan struct{})
	rel := closeOnce(release)
	t.Cleanup(rel)
	cfg := testConfig(says("x"), func(c *agent.Config) {
		c.Model = providers.OpenAIGPT55()
	})
	factory := cfg.Factory
	cfg.Factory = func(ctx context.Context, id, cwd string) (*agent.Agent, error) {
		a, err := factory(ctx, id, cwd)
		close(built)
		<-release
		return a, err
	}
	acc := &fakeProviderSetup{}
	cfg.RequireRoute, cfg.ProviderSetup = true, acc
	p := newAdapterPeer(t, cfg, nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	acc.logout = func(req protocol.ACPLogoutRequest) protocol.ACPLogoutResult {
		acc.setUsable(req.ProviderID, false)
		p.a.ProvidersChanged(1, req.ProviderID, protocol.ACPProviderChangeRemoved)
		return protocol.ACPLogoutResult{Removed: true, Generation: 1}
	}
	id := p.send("session/new", withRoute(map[string]any{
		"cwd": t.TempDir(), "mcpServers": []any{},
	}, route("c1", 0, false, "")))
	select {
	case <-built:
	case <-time.After(peerWait):
		t.Fatal("the session factory did not start")
	}
	p.ok(protocol.ACPProvidersLogout, providerSetupParams("c2", map[string]any{
		"providerId": "openai", "methodId": "api-key",
	}), nil)
	rel()
	f := p.await(id)
	require.Nil(t, f.Error)
	var res struct {
		SessionID string `json:"sessionId"`
	}
	require.NoError(t, json.Unmarshal(f.Result, &res))
	require.Empty(t, p.rstate(res.SessionID).ModelID)
}
