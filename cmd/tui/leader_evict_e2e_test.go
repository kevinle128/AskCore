package main

import (
	"context"
	"errors"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"AskCore/internal/leader"

	"github.com/stretchr/testify/require"
)

// processGone tells whether the process no longer exists.
func processGone(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// startLeaderWithoutSocket starts a real leader and removes its socket file, so
// it holds the lock and answers nobody. It returns the leader and its owner.
func startLeaderWithoutSocket(t *testing.T, bin, home string) (leader.Paths, leader.Owner, string) {
	t.Helper()
	c, err := leader.ConnectOrSpawn(context.Background(), e2eConnect(home, bin))
	require.NoError(t, err)
	id := c.Info.InstanceID
	_ = c.Close()
	paths, _ := leader.ResolvePaths(home)
	owner, err := leader.ReadOwner(paths)
	require.NoError(t, err)
	require.Equal(t, leader.OwnerServing, owner.State)
	require.Equal(t, id, owner.Instance)
	require.NoError(t, os.Remove(paths.Socket))
	t.Cleanup(func() {
		_ = syscall.Kill(owner.PID, syscall.SIGCONT)
		_ = syscall.Kill(owner.PID, syscall.SIGKILL)
	})
	return paths, owner, id
}

// A leader that holds the lock without a socket is stopped on Linux, and a new
// leader serves. Other platforms have no stable process handle: the call waits
// and reports that a leader is starting, and the old process is not touched.
func TestLeaderE2ELeaderWithoutSocketIsEvicted(t *testing.T) {
	bin := acpBinary(t)
	home := leaderHome(t)
	stopLeaderAtEnd(t, bin, home)
	paths, owner, oldID := startLeaderWithoutSocket(t, bin, home)

	cfg := e2eConnect(home, bin)
	cfg.ZombieAfter = time.Nanosecond
	if runtime.GOOS != "linux" {
		cfg.Deadline = 2 * time.Second
		_, err := leader.ConnectOrSpawn(context.Background(), cfg)
		require.ErrorIs(t, err, leader.ErrLeaderStarting)
		require.False(t, processGone(owner.PID), "the old leader is not signalled")
		held, _ := leader.LockHeld(paths)
		require.True(t, held)
		return
	}
	c, err := leader.ConnectOrSpawn(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	require.NotEqual(t, oldID, c.Info.InstanceID)
	require.Eventually(t, func() bool { return processGone(owner.PID) }, 5*time.Second, 20*time.Millisecond)
	require.Len(t, leaderProcesses(bin), 1)
}
