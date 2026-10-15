//go:build linux

package leader

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func evictConfig(t *testing.T, p Paths, start func(StartConfig) (*Child, error)) ConnectConfig {
	t.Helper()
	cfg := connectConfig(p)
	cfg.CallerExe = filepath.Join(askDir(t), "ask")
	cfg.ZombieAfter = time.Nanosecond
	cfg.StartFn = start
	return cfg
}

func TestZombieLeaderIsEvictedWithSIGTERM(t *testing.T) {
	p := testPaths(t)
	h := startHelperLeader(t, p, OwnerServing, helperDefault)
	pl := newProcLeader(t)
	c, err := ConnectOrSpawn(context.Background(), evictConfig(t, p, pl.start))
	require.NoError(t, err)
	_ = c.Close()
	<-h.done
	require.True(t, h.status.Signaled())
	require.Equal(t, syscall.SIGTERM, h.status.Signal())
	require.EqualValues(t, 1, pl.starts.Load())
}

func TestZombieLeaderIsKilledWhenItIgnoresSIGTERM(t *testing.T) {
	p := testPaths(t)
	h := startHelperLeader(t, p, OwnerServing, helperIgnoreTerm)
	pl := newProcLeader(t)
	cfg := evictConfig(t, p, pl.start)
	cfg.EvictWait = 500 * time.Millisecond
	c, err := ConnectOrSpawn(context.Background(), cfg)
	require.NoError(t, err)
	_ = c.Close()
	<-h.done
	require.True(t, h.status.Signaled())
	require.Equal(t, syscall.SIGKILL, h.status.Signal())
}

// A process that the record names but that is not a leader is not signalled.
func TestZombieWithUnverifiedOwnerIsNotSignalled(t *testing.T) {
	p := testPaths(t)
	l, err := Acquire(p)
	require.NoError(t, err)
	defer func() { _ = l.Release() }()
	sleeper := exec.Command("sleep", "60")
	require.NoError(t, sleeper.Start())
	t.Cleanup(func() { _ = sleeper.Process.Kill(); _ = sleeper.Wait() })
	require.NoError(t, l.WriteOwner(Owner{PID: sleeper.Process.Pid, Start: time.Now().Unix(), Instance: "x", State: OwnerServing}))
	var starts atomic.Int32
	cfg := evictConfig(t, p, func(StartConfig) (*Child, error) {
		starts.Add(1)
		return nil, errors.New("must not start")
	})
	cfg.Deadline = time.Second
	_, err = ConnectOrSpawn(context.Background(), cfg)
	require.ErrorIs(t, err, ErrLeaderStarting)
	require.NoError(t, sleeper.Process.Signal(syscall.Signal(0)), "the process was not signalled")
	require.Zero(t, starts.Load())

}

// A leader that turns to stopping after SIGTERM drains; it is not killed.
func TestZombieThatTurnsStoppingIsNotKilled(t *testing.T) {
	p := testPaths(t)
	h := startHelperLeader(t, p, OwnerServing, helperStopsOnTerm)
	var starts atomic.Int32
	cfg := evictConfig(t, p, func(StartConfig) (*Child, error) {
		starts.Add(1)
		return nil, errors.New("must not start")
	})
	cfg.EvictWait = 300 * time.Millisecond
	cfg.Deadline = 2 * time.Second
	_, err := ConnectOrSpawn(context.Background(), cfg)
	require.ErrorIs(t, err, ErrLeaderStarting)
	require.Contains(t, err.Error(), "SIGTERM")
	require.NotContains(t, err.Error(), "SIGKILL")
	require.True(t, h.alive(), "a leader that began to stop is not killed")
	require.Zero(t, starts.Load())
}
