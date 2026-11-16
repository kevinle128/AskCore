package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/auth"
	"AskCore/internal/settings"
)

// pendingChatGPTHome makes an Ask home with a saved openai-chatgpt record
// whose refresh result is uncertain (a pending fence), and a cached account
// model list for that record that does not name gpt-5.5. All values are fake.
func pendingChatGPTHome(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	home := privateHome(t)
	store, err := settings.NewAuthStore(home)
	require.NoError(t, err)
	_, err = store.Replace(ctx, "openai", 0, settings.Credential{Method: "openai-chatgpt", OAuth: &settings.OAuthCredential{
		AccessToken:  "fake-access-value",
		RefreshToken: "fake-refresh-value",
		ExpiresAt:    time.Now().Add(time.Hour),
		Subject:      "fake-subject",
	}})
	require.NoError(t, err)
	saved, _, err := store.Read(ctx, "openai")
	require.NoError(t, err)
	_, err = store.FenceRefresh(ctx, "openai", saved.Generation, "fake-attempt")
	require.NoError(t, err)
	pending, _, err := store.Read(ctx, "openai")
	require.NoError(t, err)
	require.NotNil(t, pending.RefreshState)

	cache, err := settings.NewModelCache(home)
	require.NoError(t, err)
	require.NoError(t, cache.Update(ctx, func(d *settings.ModelCacheData) error {
		key := auth.AccountKey("openai", "openai-chatgpt", pending, d.Salt)
		require.NotEmpty(t, key)
		d.Accounts[key] = settings.CachedAccount{
			Provider:  "openai",
			Method:    "openai-chatgpt",
			FetchedAt: time.Now(),
			State:     settings.AccountModelsKnown,
			Items:     []settings.CachedModel{{ID: "gpt-5.4"}},
		}
		return nil
	}))
	return home
}

func assertNoFakeSecret(t *testing.T, outs ...string) {
	t.Helper()
	for _, out := range outs {
		for _, secret := range []string{"fake-access-value", "fake-refresh-value", "fake-env-value"} {
			assert.NotContains(t, out, secret)
		}
	}
}

// A saved sign-in that waits for recovery blocks the environment key. The
// key source names the saved record and the two recovery commands, not
// "none".
func TestSettingsShowNamesSavedSignInThatWaitsForRecovery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	clearKeyEnv(t)
	t.Setenv("OPENAI_API_KEY", "fake-env-value")
	project, _ := settingsProjectDir(t, `{}`, 0o644)
	home := pendingChatGPTHome(t)

	got := printInProject(t, project, home, "settings", "show", "--json")
	require.Equal(t, 0, got.code, got.stderr)
	sources := map[string]settingsKeySource{}
	for _, k := range decodeSettingsDump(t, got.stdout).KeySources {
		sources[k.Provider] = k
	}
	assert.Equal(t, "saved:openai-chatgpt", sources["openai"].Source)
	assert.Contains(t, sources["openai"].Problem, "refresh result uncertain")
	assert.Contains(t, sources["openai"].Problem, "ask auth login --provider openai --method openai-chatgpt")
	assert.Contains(t, sources["openai"].Problem, "ask auth logout --provider openai")
	assert.Equal(t, "none", sources["anthropic"].Source, "a real absence stays none")

	text := printInProject(t, project, home, "settings", "show")
	require.Equal(t, 0, text.code, text.stderr)
	assert.Regexp(t, `\n  openai +saved:openai-chatgpt \(refresh result uncertain; `, text.stdout)
	assertNoFakeSecret(t, got.stdout, got.stderr, text.stdout, text.stderr)
}

// The cached account list of a record that waits for recovery does not tell
// the current access, so it removes no row: the prompt reaches auth and
// fails with the recovery error, not with "unknown model".
func TestPrintWithSavedSignInThatWaitsForRecoveryAsksToSignInAgain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	clearKeyEnv(t)
	t.Setenv("OPENAI_API_KEY", "fake-env-value")
	project, _ := settingsProjectDir(t, `{}`, 0o644)
	home := pendingChatGPTHome(t)
	before, err := os.ReadFile(filepath.Join(home, "auth.json"))
	require.NoError(t, err)

	got := printInProject(t, project, home, "--provider", "openai", "--model", "gpt-5.5", "-p", "hello")
	assert.Equal(t, 1, got.code)
	assert.Contains(t, got.stderr, "refresh result uncertain; sign in again")
	assert.NotContains(t, got.stderr, "unknown model")
	assertNoFakeSecret(t, got.stdout, got.stderr)

	after, err := os.ReadFile(filepath.Join(home, "auth.json"))
	require.NoError(t, err)
	assert.Equal(t, before, after, "recovery is never automatic: the record stays as it was")
}
