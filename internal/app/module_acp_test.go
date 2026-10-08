package app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"AskCore/internal/agent"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/settings"

	sdk "github.com/coder/acp-go-sdk"
)

func testACPParams(t *testing.T) ACPParams {
	t.Helper()
	return ACPParams{
		Home:    t.TempDir(),
		Initial: providers.Model{ID: "faux-1", Name: "faux-1", Provider: "faux", API: "faux"},
		Info:    sdk.Implementation{Name: "ask", Version: "test"},
		NewAgent: func(*auth.Service, string, string) (*agent.Agent, error) {
			return nil, errors.New("not used")
		},
	}
}

// The editor composition needs no configuration, database, gateway or leader:
// the graph validates with the parameters alone.
func TestACPModuleValidatesWithoutServers(t *testing.T) {
	p := testACPParams(t)
	require.NoError(t, fx.ValidateApp(ACPModule, fx.Supply(p), fx.Invoke(func(*ACPRuntime) {})))
}

func TestACPModuleRequiresAgentConstructor(t *testing.T) {
	p := testACPParams(t)
	p.NewAgent = nil
	_, err := NewACPRuntime(p)
	require.Error(t, err)
}

func TestACPRuntimeAuthMethodsAndReadiness(t *testing.T) {
	p := testACPParams(t)
	rt, err := NewACPRuntime(p)
	require.NoError(t, err)
	ids := map[string]bool{}
	for _, m := range rt.Config.AuthMethods {
		require.NotNil(t, m.Agent)
		require.NotNil(t, m.Agent.Description)
		require.Contains(t, *m.Agent.Description, "ask auth login")
		ids[m.Agent.Id] = true
	}
	for _, want := range []string{"api-key", "anthropic-oauth", "openai-chatgpt", "xai-oauth"} {
		require.True(t, ids[want], want)
	}
	ctx := context.Background()
	require.Error(t, rt.Config.Authenticate(ctx, "api-key"), "no credential is saved yet")

	store, err := settings.NewAuthStore(p.Home)
	require.NoError(t, err)
	_, err = store.Replace(ctx, providers.ProviderOpenAI, 0, settings.Credential{Method: "api-key", APIKey: "saved-secret"})
	require.NoError(t, err)
	require.NoError(t, rt.Config.Authenticate(ctx, "api-key"))
	require.Error(t, rt.Config.Authenticate(ctx, "xai-oauth"), "only the configured method is ready")
	require.NoError(t, rt.Cleanup(ctx))
}

func TestACPRuntimeCatalogListsInitialModelFirst(t *testing.T) {
	p := testACPParams(t)
	rt, err := NewACPRuntime(p)
	require.NoError(t, err)
	rows := rt.Config.Models()
	require.Equal(t, "faux", rows[0].Provider)
	require.Greater(t, len(rows), 1)
	m, err := rt.Config.FindModel(providers.Ref{Provider: "faux", ID: "faux-1"})
	require.NoError(t, err)
	require.Equal(t, p.Initial, m)
	_, err = rt.Config.FindModel(providers.Ref{Provider: "openai", ID: providers.ModelGPT55})
	require.NoError(t, err)

	err = rt.Config.ModelAuth(context.Background(), providers.OpenAIGPT55(), "subscription")
	require.Error(t, err)
}
