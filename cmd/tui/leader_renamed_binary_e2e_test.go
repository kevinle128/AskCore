package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"AskCore/internal/leader"
	"AskCore/internal/tui"

	"github.com/stretchr/testify/require"
)

// renamedAsk copies the built ask binary to a file named tui in a new folder, as
// `go run ./cmd/tui` names its temporary binary. No `ask` is next to it.
func renamedAsk(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(acpBinary(t))
	require.NoError(t, err)
	exe := filepath.Join(t.TempDir(), "tui")
	require.NoError(t, os.WriteFile(exe, data, 0o755))
	return exe
}

// The TUI started from a binary that is not named ask, with no ask on the
// PATH, starts a leader from its own binary. It does not fall back to the
// in-process agent, so the provider setup stays available.
func TestTUIStartsLeaderFromABinaryWithAnotherName(t *testing.T) {
	exe := renamedAsk(t)
	home := leaderHome(t)
	stopLeaderAtEnd(t, exe, home)
	env := leaderEnv(home)

	c, err := openTUIClient(context.Background(), inProcessDeps(home, nil), tuiClientOptions{callerExe: exe, env: env})
	require.NoError(t, err)
	require.NoError(t, c.FallbackReason, "the leader starts; the client does not fall back")
	require.Equal(t, tui.ModeLeader, c.Info().Mode)
	require.NoError(t, c.Close())

	// The leader runs from the renamed binary and records it.
	paths, _ := leader.ResolvePaths(home)
	owner, err := leader.ReadOwner(paths)
	require.NoError(t, err)
	require.Equal(t, exe, owner.Exe)
	require.NoError(t, leader.VerifyLeaderProcess(owner), "the recorded executable identifies the leader")
	require.NotEmpty(t, leaderProcesses(exe))

	// The same binary reports and stops that leader.
	cmd := exec.Command(exe, "leader", "status")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	cmd = exec.Command(exe, "leader", "stop")
	cmd.Env = env
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "leader stopped")
	require.Eventually(t, func() bool { return len(leaderProcesses(exe)) == 0 }, 10*time.Second, 50*time.Millisecond,
		"no leader process is left")
}

// `ask connect` from a renamed binary starts a leader the same way.
func TestConnectStartsLeaderFromABinaryWithAnotherName(t *testing.T) {
	exe := renamedAsk(t)
	home := leaderHome(t)
	stopLeaderAtEnd(t, exe, home)
	env := leaderEnv(home)

	cmd := exec.Command(exe, "connect")
	cmd.Env = env
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "bye")
	require.NotEmpty(t, leaderProcesses(exe), "the leader runs from the renamed binary")
}
