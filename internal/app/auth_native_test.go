package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/auth"
)

// The value commands of models.json run through the real runner and the real
// shell; only the command cache of auth is between the test and the runner.

func TestValueCommandTrimsStdout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, err := auth.NewValues(runValueCommand, nil, nil).Expand(context.Background(), "local apiKey", "!printf '  key-1\\n'")
	require.NoError(t, err)
	assert.Equal(t, "key-1", got)
}

func TestValueCommandRunsInHomeWithProcessEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ASK_VALUE_TEST_VAR", "from-parent")
	out, err := runValueCommand(context.Background(), `printf '%s|%s' "$ASK_VALUE_TEST_VAR" "$(pwd -P)"`)
	require.NoError(t, err)
	want, err := filepath.EvalSymlinks(home)
	require.NoError(t, err)
	assert.Equal(t, "from-parent|"+want, out)
}

func TestValueCommandExitGivesOnlyTheCode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := runValueCommand(context.Background(), "printf out-secret; printf err-secret >&2; exit 3")
	var exit *auth.ExitError
	require.ErrorAs(t, err, &exit)
	assert.Equal(t, 3, exit.Code)

	_, err = auth.NewValues(runValueCommand, nil, nil).Expand(context.Background(), "local apiKey", "!printf out-secret; printf err-secret >&2; exit 3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed (exit 3)")
	assert.NotContains(t, err.Error(), "secret")
}

func TestValueCommandStderrNeverReachesTheResult(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out, err := runValueCommand(context.Background(), "printf key; printf err-secret >&2")
	require.NoError(t, err)
	assert.Equal(t, "key", out)
}

func TestValueCommandDeadlineStopsTheCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := runValueCommand(ctx, "printf partial; exec sleep 30")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestValueCommandOversizeOutputFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out, err := runValueCommand(context.Background(), "head -c 131072 /dev/zero | tr '\\000' x")
	require.ErrorIs(t, err, errValueOutputTooLarge)
	assert.Empty(t, out)
	assert.NotContains(t, err.Error(), "xxx")

	_, err = runValueCommand(context.Background(), "yes secret-line")
	require.ErrorIs(t, err, errValueOutputTooLarge, "an endless command stops at the limit")
	assert.NotContains(t, err.Error(), "secret")
}
