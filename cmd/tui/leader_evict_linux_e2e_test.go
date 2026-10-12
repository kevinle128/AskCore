//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"AskCore/internal/leader"

	"github.com/stretchr/testify/require"
)

// A stopped leader cannot handle SIGTERM, so the eviction ends with SIGKILL.
func TestLeaderE2EFrozenLeaderWithoutSocketIsKilled(t *testing.T) {
	bin := acpBinary(t)
	home := leaderHome(t)
	stopLeaderAtEnd(t, bin, home)
	_, owner, oldID := startLeaderWithoutSocket(t, bin, home)
	require.NoError(t, syscall.Kill(owner.PID, syscall.SIGSTOP))

	cfg := e2eConnect(home, bin)
	cfg.ZombieAfter = time.Nanosecond
	cfg.EvictWait = time.Second
	c, err := leader.ConnectOrSpawn(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	require.NotEqual(t, oldID, c.Info.InstanceID)
	require.Eventually(t, func() bool { return processGone(owner.PID) }, 5*time.Second, 20*time.Millisecond)
}

func TestLeaderE2EEvictionStopsAtTheDeadline(t *testing.T) {
	bin := acpBinary(t)
	home := leaderHome(t)
	stopLeaderAtEnd(t, bin, home)
	_, owner, _ := startLeaderWithoutSocket(t, bin, home)
	require.NoError(t, syscall.Kill(owner.PID, syscall.SIGSTOP))

	cfg := e2eConnect(home, bin)
	cfg.ZombieAfter = time.Nanosecond
	cfg.EvictWait = time.Second
	cfg.Deadline = 200 * time.Millisecond
	_, err := leader.ConnectOrSpawn(context.Background(), cfg)
	require.ErrorIs(t, err, leader.ErrLeaderStarting)
	require.Contains(t, err.Error(), "SIGTERM")
	require.NotContains(t, err.Error(), "SIGKILL", "the deadline must stop escalation")
	require.NoError(t, syscall.Kill(owner.PID, 0), "the old leader must still exist")
}

func TestLeaderE2ECancelledEvictionDoesNotKill(t *testing.T) {
	bin := acpBinary(t)
	home := leaderHome(t)
	stopLeaderAtEnd(t, bin, home)
	_, owner, _ := startLeaderWithoutSocket(t, bin, home)
	require.NoError(t, syscall.Kill(owner.PID, syscall.SIGSTOP))

	cfg := e2eConnect(home, bin)
	cfg.ZombieAfter = time.Nanosecond
	cfg.EvictWait = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		c, err := leader.ConnectOrSpawn(ctx, cfg)
		if c != nil {
			_ = c.Close()
		}
		done <- err
	}()
	defer func() { cancel(); <-done }()
	// A stopped process cannot handle TERM. The kernel records it as pending.
	require.Eventually(t, func() bool {
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", owner.PID))
		if err != nil {
			return false
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if mask, ok := strings.CutPrefix(line, "ShdPnd:"); ok {
				pending, err := strconv.ParseUint(strings.TrimSpace(mask), 16, 64)
				return err == nil && pending&(1<<uint(syscall.SIGTERM-1)) != 0
			}
		}
		return false
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	err := <-done
	done <- err
	require.ErrorIs(t, err, leader.ErrLeaderStarting)
	require.NotContains(t, err.Error(), "SIGKILL")
	require.NoError(t, syscall.Kill(owner.PID, 0), "cancellation must stop escalation")
}
