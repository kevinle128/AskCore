package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"AskCore/internal/leader"
	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// leaderEnvWithoutFaux is the leader environment without the demo provider and
// without any credential variable of the developer shell.
func leaderEnvWithoutFaux(home string) []string {
	var env []string
	for _, kv := range withoutCredentialEnv(leaderEnv(home)) {
		if !strings.HasPrefix(kv, "ASK_FAUX=") {
			env = append(env, kv)
		}
	}
	return env
}

// noProviderEnv is appended to the leaderEnv of a started process. It turns the
// demo provider off and blanks every variable that can carry a provider key. A
// later duplicate wins, and a blank key is no key.
func noProviderEnv() []string {
	extra := []string{"ASK_FAUX=0"}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if len(withoutCredentialEnv([]string{kv})) == 0 {
			extra = append(extra, name+"=")
		}
	}
	return extra
}

// openRawLeader connects a line client to the leader of home. env is the
// environment of a leader that the connect starts.
func openRawLeader(t *testing.T, bin, home string, env []string) *lineClient {
	t.Helper()
	cfg := e2eConnect(home, bin)
	cfg.Env = env
	cl, err := leader.ConnectOrSpawn(context.Background(), cfg)
	require.NoError(t, err)
	c := newLineClient(cl, io.Discard)
	go c.readLoop()
	t.Cleanup(c.close)
	_, f := c.call("initialize", map[string]any{"protocolVersion": 1})
	require.Nil(t, f)
	return c
}

func rawSession(t *testing.T, c *lineClient) string {
	t.Helper()
	res, f := c.call("session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}})
	require.Nil(t, f)
	var out struct {
		SessionID string `json:"sessionId"`
	}
	require.NoError(t, json.Unmarshal(res, &out))
	require.NotEmpty(t, out.SessionID)
	return out.SessionID
}

func rawCall[T any](t *testing.T, c *lineClient, method string, params any) T {
	t.Helper()
	res, f := c.call(method, params)
	require.Nil(t, f, "%s: %+v", method, f)
	var out T
	require.NoError(t, json.Unmarshal(res, &out))
	return out
}

func modelIDOf(m providers.Model) string { return fmt.Sprintf("%s/%s@%s", m.Provider, m.ID, m.API) }

func stopLeader(t *testing.T, bin, home string) {
	t.Helper()
	cmd := exec.Command(bin, "leader", "stop")
	cmd.Env = leaderEnv(home)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	paths, err := leader.ResolvePaths(home)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return !leader.Serving(paths) }, 15*time.Second, 25*time.Millisecond, "the leader stops")
}

// With no usable provider a session has no model. `ask connect` shows the
// refused prompt, and the models list is empty. The demo provider is absent.
func TestLeaderStartsWithoutModelWhenNothingUsable(t *testing.T) {
	bin, home := newE2EHome(t)
	env := leaderEnvWithoutFaux(home)

	clean := startConnect(t, bin, home, noProviderEnv())
	clean.waitLine("connected", hasPrefix("connected "))
	sid := clean.sessionID()
	clean.send("hello")
	line := clean.waitLine("a refused prompt", hasPrefix("error session="+sid+" op=prompt"))
	require.Equal(t, "no_model", field(line, "kind"))
	require.Equal(t, fmt.Sprint(protocol.ACPCodeNoModel), field(line, "code"))
	require.Zero(t, clean.count("update "), "no run started")

	c := openRawLeader(t, bin, home, env)
	raw := rawSession(t, c)
	st := rawCall[protocol.ACPStateResult](t, c, protocol.ACPState, map[string]any{"sessionId": raw})
	require.Equal(t, "", st.ModelID)
	models := rawCall[protocol.ACPModelsResult](t, c, protocol.ACPModels, map[string]any{"sessionId": raw, "usableOnly": true})
	require.True(t, models.UsableOnly)
	require.Empty(t, models.Models)
	require.Equal(t, "", models.Current)
	all := rawCall[protocol.ACPModelsResult](t, c, protocol.ACPModels, map[string]any{"sessionId": raw})
	for _, m := range all.Models {
		require.NotEqual(t, "faux", m.Provider, "the demo provider is listed only on request")
	}
	providersList := rawCall[protocol.ACPProvidersResult](t, c, protocol.ACPProvidersList, map[string]any{})
	require.Len(t, providersList.Options, 7)
	for _, o := range providersList.Options {
		require.Equal(t, protocol.ACPLoginStateNone, o.State, o.ProviderID+"/"+o.MethodID)
	}
}

func TestFauxOnlyWithOptIn(t *testing.T) {
	bin := acpBinary(t)
	for name, on := range map[string]bool{"opt in": true, "default": false} {
		t.Run(name, func(t *testing.T) {
			home := leaderHome(t)
			stopLeaderAtEnd(t, bin, home)
			env := leaderEnvWithoutFaux(home)
			if on {
				env = withoutCredentialEnv(leaderEnv(home))
			}
			c := openRawLeader(t, bin, home, env)
			sid := rawSession(t, c)
			st := rawCall[protocol.ACPStateResult](t, c, protocol.ACPState, map[string]any{"sessionId": sid})
			models := rawCall[protocol.ACPModelsResult](t, c, protocol.ACPModels, map[string]any{"sessionId": sid, "usableOnly": true})
			if !on {
				require.Equal(t, "", st.ModelID)
				require.Empty(t, models.Models)
				return
			}
			require.Equal(t, "faux/faux-1@faux", st.ModelID, "the demo model is the initial model")
			require.Len(t, models.Models, 1)
			require.Equal(t, "faux", models.Models[0].Provider)
		})
	}
}

// A model chosen with saveDefault is the start model of a session after the
// leader restarts, and a key save leaves the default alone.
func TestLeaderDefaultModelSurvivesRestart(t *testing.T) {
	bin, home := newE2EHome(t)
	env := withoutCredentialEnv(leaderEnv(home))
	c := openRawLeader(t, bin, home, env)

	// A Token Plan key is saved without a live check, so no request leaves the test.
	out := rawCall[protocol.ACPLoginOutcome](t, c, protocol.ACPProvidersAPIKey,
		map[string]any{"loginId": "k1", "providerId": providers.ProviderTokenPlan, "key": "fake-token-plan-key-0001"})
	require.Equal(t, protocol.ACPLoginStatusSaved, out.Status)
	require.True(t, out.NotChecked)

	sid := rawSession(t, c)
	st := rawCall[protocol.ACPStateResult](t, c, protocol.ACPState, map[string]any{"sessionId": sid})
	require.Equal(t, "faux/faux-1@faux", st.ModelID, "a sign-in does not change the start model; the demo model leads the catalog")

	target := modelIDOf(providers.TokenPlanCompletions())
	set := rawCall[protocol.ACPSetModelResult](t, c, protocol.ACPSetModel, map[string]any{"sessionId": sid, "modelId": target, "saveDefault": true})
	require.True(t, set.DefaultSaved)
	require.Equal(t, target, set.ModelID)
	c.close()

	stopLeader(t, bin, home)
	again := openRawLeader(t, bin, home, env)
	next := rawSession(t, again)
	st = rawCall[protocol.ACPStateResult](t, again, protocol.ACPState, map[string]any{"sessionId": next})
	require.Equal(t, target, st.ModelID, "the saved default is the start model of the new leader")
	require.Nil(t, st.ModelFallback)
	models := rawCall[protocol.ACPModelsResult](t, again, protocol.ACPModels, map[string]any{"sessionId": next, "usableOnly": true})
	require.Equal(t, target, models.Default)
}
