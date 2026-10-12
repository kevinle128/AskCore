//go:build darwin

package leader

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// This platform has no stable process handle, so a leader without a socket is
// waited for and never signalled.
func TestZombieLeaderIsNotSignalledWithoutAStableHandle(t *testing.T) {
	p := testPaths(t)
	h := startHelperLeader(t, p, OwnerServing, helperDefault)
	var starts atomic.Int32
	cfg := connectConfig(p)
	cfg.CallerExe = filepath.Join(askDir(t), "ask")
	cfg.Deadline = time.Second
	cfg.ZombieAfter = time.Nanosecond
	cfg.StartFn = func(StartConfig) (*Child, error) {
		starts.Add(1)
		return nil, errors.New("must not start")
	}
	_, err := ConnectOrSpawn(context.Background(), cfg)
	require.ErrorIs(t, err, ErrLeaderStarting)
	require.True(t, h.alive())
	require.Zero(t, starts.Load())
}
