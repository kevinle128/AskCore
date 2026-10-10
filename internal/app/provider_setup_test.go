package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"AskCore/internal/acp"
	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// providerSetupFixture builds the provider setup of a runtime over a fresh home. env is
// the process environment of the runtime.
func providerSetupFixture(t *testing.T, env map[string]string, rt http.RoundTripper) (*providerSetup, *ACPRuntime) {
	t.Helper()
	p := testACPParams(t)
	p.Initial = providers.Model{}
	p.NewAgent = func(*auth.Service, string, string, providers.Model) (*agent.Agent, error) {
		return nil, errors.New("not used")
	}
	p.Env = func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	if rt != nil {
		p.AuthHTTP = &http.Client{Transport: rt}
	}
	runtime, err := NewACPRuntime(p)
	require.NoError(t, err)
	acc := newProviderSetup(runtime, func(uint64, string, protocol.ACPProviderChange) {})
	t.Cleanup(func() { acc.Close(); _ = runtime.Cleanup(context.Background()) })
	return acc, runtime
}

func TestProviderSetupListRowsAndModelCounts(t *testing.T) {
	acc, _ := providerSetupFixture(t, map[string]string{"OPENAI_API_KEY": "sk-fake-env"}, nil)
	res, err := acc.List(context.Background())
	require.NoError(t, err)

	require.Len(t, res.Options, 7)
	byKey := map[string]protocol.ACPLoginOption{}
	for _, o := range res.Options {
		byKey[o.ProviderID+"/"+o.MethodID] = o
	}
	require.Equal(t, protocol.ACPLoginStateEnv, byKey["openai/api-key"].State)
	require.Equal(t, "OPENAI_API_KEY", byKey["openai/api-key"].EnvVar)
	require.Equal(t, protocol.ACPLoginStateNone, byKey["anthropic/api-key"].State)
	require.Equal(t, protocol.ACPLoginStateNone, byKey["xai/xai-oauth"].State)
	require.Equal(t, "Claude Pro/Max sign-in", byKey["anthropic/anthropic-oauth"].Name)

	counts := map[string]int{}
	usable := map[string]bool{}
	for _, p := range res.Providers {
		counts[p.ProviderID], usable[p.ProviderID] = p.ModelCount, p.Usable
	}
	want := map[string]int{}
	for _, m := range providers.AvailableModels() {
		want[m.Provider]++
	}
	require.Equal(t, want, counts, "model counts come from the catalog")
	require.Equal(t, map[string]bool{"alibaba-token-plan": false, "openai": true, "anthropic": false, "xai": false}, usable)
	require.NotContains(t, counts, "faux", "no demo provider without the opt-in")
}

func TestProviderSetupLogoutReportsEnv(t *testing.T) {
	acc, _ := providerSetupFixture(t, map[string]string{"ALIBABA_TOKEN_PLAN_API_KEY": "env-fake-key"}, nil)
	ctx := context.Background()
	// A Token Plan key is saved without a live check, so no request is made.
	out, err := acc.SaveKey(ctx, "c1", protocol.ACPAPIKeyRequest{LoginID: "k1", ProviderID: providers.ProviderTokenPlan, Key: "saved-fake-key"})
	require.NoError(t, err)
	require.Equal(t, protocol.ACPLoginStatusSaved, out.Status)
	require.True(t, out.NotChecked)

	list, err := acc.List(ctx)
	require.NoError(t, err)
	for _, o := range list.Options {
		if o.ProviderID == providers.ProviderTokenPlan {
			require.Equal(t, protocol.ACPLoginStateAPIKey, o.State, "a saved key shows before the environment key")
		}
	}

	res, err := acc.Logout(ctx, protocol.ACPLogoutRequest{ProviderID: providers.ProviderTokenPlan, MethodID: "api-key"})
	require.NoError(t, err)
	require.True(t, res.Removed)
	require.Equal(t, "ALIBABA_TOKEN_PLAN_API_KEY", res.EnvVar, "the environment key still applies")
	require.True(t, acc.Usable(ctx, providers.ProviderTokenPlan))
}

func TestProviderSetupMapBrokerErrorsToHostSentinels(t *testing.T) {
	acc, _ := providerSetupFixture(t, nil, nil)
	_, err := acc.LoginWait(context.Background(), "c1", "missing")
	require.ErrorIs(t, err, acp.ErrLoginNotFound)
	require.ErrorIs(t, acc.LoginInput("c1", protocol.ACPLoginInputRequest{LoginID: "missing", Code: "x"}), acp.ErrLoginNotFound)
	_, err = acc.LoginStart(context.Background(), "c1", protocol.ACPLoginStartRequest{ProviderID: "openai", MethodID: "api-key"})
	require.ErrorIs(t, err, acp.ErrLoginMethod)
	_, err = acc.Logout(context.Background(), protocol.ACPLogoutRequest{ProviderID: "nope", MethodID: "api-key"})
	require.ErrorIs(t, err, acp.ErrLoginMethod)
}

func TestProviderSetupKeyRefusalDoesNotEchoKey(t *testing.T) {
	const key = "fake_secret_key"
	acc, _ := providerSetupFixture(t, nil, readinessRT(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"` + key + `","type":"` + key + `"}}`)), Header: make(http.Header)}, nil
	}))
	out, err := acc.SaveKey(context.Background(), "c1", protocol.ACPAPIKeyRequest{LoginID: "k1", ProviderID: providers.ProviderOpenAI, Key: key})
	require.NoError(t, err)
	require.Equal(t, protocol.ACPLoginStatusRefused, out.Status)
	require.Empty(t, out.ProviderCode, "a provider can reflect a key in its code or type")
}

func TestProviderSetupListMatchesItsGeneration(t *testing.T) {
	acc, runtime := providerSetupFixture(t, nil, nil)
	read, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	runtime.Service.Env = func(string) (string, bool) {
		once.Do(func() { close(read); <-resume })
		return "", false
	}
	result := make(chan protocol.ACPProvidersResult, 1)
	errResult := make(chan error, 1)
	go func() {
		res, err := acc.List(context.Background())
		result <- res
		errResult <- err
	}()
	<-read
	out, err := acc.SaveKey(context.Background(), "c1", protocol.ACPAPIKeyRequest{LoginID: "k1", ProviderID: providers.ProviderTokenPlan, Key: "fake-key"})
	require.NoError(t, err)
	require.Equal(t, protocol.ACPLoginStatusSaved, out.Status)
	close(resume)
	res := <-result
	require.NoError(t, <-errResult)
	require.Equal(t, out.Generation, res.Generation)
	for _, option := range res.Options {
		if option.ProviderID == providers.ProviderTokenPlan {
			require.Equal(t, protocol.ACPLoginStateAPIKey, option.State, "the newer generation must not label stale rows")
		}
	}
}

func TestProviderSetupSignInSurvivesAnotherProviderSave(t *testing.T) {
	resume := make(chan struct{})
	l, _, _ := providerSetupLeader(t, func(p *LeaderParams) {
		p.Wait = func(ctx context.Context, _ time.Duration) error {
			select {
			case <-resume:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		p.AuthHTTP = &http.Client{Transport: readinessRT(func(req *http.Request) (*http.Response, error) {
			body := `{"device_code":"private-device","user_code":"USER-CODE","verification_uri":"https://auth.x.ai/device","expires_in":600,"interval":1}`
			if strings.HasSuffix(req.URL.Path, "/token") {
				body = `{"access_token":"fake-access","refresh_token":"fake-refresh","expires_in":3600}`
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})}
	})
	a, b := l.client(nil), l.client(nil)
	start := decodeInto[protocol.ACPLoginStartResult](t, a.must(protocol.ACPLoginStart, map[string]any{"providerId": "xai", "methodId": "xai-oauth"}))
	out := decodeInto[protocol.ACPLoginOutcome](t, b.must(protocol.ACPProvidersAPIKey, map[string]any{"loginId": "k1", "providerId": providers.ProviderTokenPlan, "key": "fake-key"}))
	require.Equal(t, protocol.ACPLoginStatusSaved, out.Status)
	close(resume)
	finish := decodeInto[protocol.ACPLoginOutcome](t, a.must(protocol.ACPLoginWait, map[string]any{"loginId": start.LoginID}))
	require.Equal(t, protocol.ACPLoginStatusSignedIn, finish.Status, "saving another provider must not invalidate this sign-in")
}
