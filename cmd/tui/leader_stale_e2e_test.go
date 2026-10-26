package main

import (
	"context"
	"net"
	"os"
	"syscall"
	"testing"

	"AskCore/internal/leader"

	"github.com/stretchr/testify/require"
)

// A socket file that no process serves is replaced by the leader that wins the
// lock. A regular file at the socket path is never removed, and no leader is
// started for it.
func TestLeaderE2EStaleSocketReplacedByTheLockWinner(t *testing.T) {
	bin := acpBinary(t)
	home := leaderHome(t)
	stopLeaderAtEnd(t, bin, home)
	paths, _ := leader.ResolvePaths(home)
	require.NoError(t, leader.EnsureHome(home))

	// A leader that died without cleanup leaves its socket file behind.
	ln, err := net.Listen("unix", paths.Socket)
	require.NoError(t, err)
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	require.NoError(t, ln.Close())
	stale, err := os.Lstat(paths.Socket)
	require.NoError(t, err)
	require.NotZero(t, stale.Mode()&os.ModeSocket)
	// Nobody serves the stale socket. A freed inode number can be reused for the
	// new socket, so the file identity cannot show the replacement. The refused
	// dial before and the answered handshake after show it.
	dead, derr := net.Dial("unix", paths.Socket)
	if derr == nil {
		_ = dead.Close()
	}
	require.ErrorIs(t, derr, syscall.ECONNREFUSED)

	c, err := leader.ConnectOrSpawn(context.Background(), e2eConnect(home, bin))
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	live, err := os.Lstat(paths.Socket)
	require.NoError(t, err)
	require.NotZero(t, live.Mode()&os.ModeSocket)
	require.NotEmpty(t, c.Info.InstanceID, "a live leader answers on the replaced socket")
	require.Equal(t, os.FileMode(0o600), live.Mode().Perm())
}

func TestLeaderE2ERegularFileAtTheSocketPathIsKept(t *testing.T) {
	bin := acpBinary(t)
	home := leaderHome(t)
	paths, _ := leader.ResolvePaths(home)
	require.NoError(t, leader.EnsureHome(home))
	require.NoError(t, os.WriteFile(paths.Socket, []byte("not a socket"), 0o600))

	_, err := leader.ConnectOrSpawn(context.Background(), e2eConnect(home, bin))
	require.ErrorIs(t, err, leader.ErrUnsafeEndpoint)
	data, rerr := os.ReadFile(paths.Socket)
	require.NoError(t, rerr)
	require.Equal(t, "not a socket", string(data), "the file is untouched")
	require.Empty(t, leaderProcesses(bin), "no leader was started for an unsafe endpoint")
}
