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

func TestZombieOwnerDecision(t *testing.T) {
	now := time.Unix(10_000, 0)
	old := Owner{PID: 4242, Start: now.Add(-time.Minute).Unix(), Instance: "i", State: OwnerServing}
	young := old
	young.Start = now.Add(-5 * time.Second).Unix()
	stopping := old
	stopping.State = OwnerStopping
	noState := old
	noState.State = ""
	noInstance := old
	noInstance.Instance = ""
	initProc := old
	initProc.PID = 1
	noStart := old
	noStart.Start = 0
	for name, tc := range map[string]struct {
		owner Owner
		want  bool
	}{
		"serving and old":            {old, true},
		"serving and young":          {young, false},
		"stopping":                   {stopping, false},
		"record from an older build": {noState, false},
		"no instance":                {noInstance, false},
		"pid 1":                      {initProc, false},
		"no start time":              {noStart, false},
		"empty record":               {Owner{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, zombieOwner(tc.owner, now, 30*time.Second))
		})
	}
}

// A lock holder without a socket that is not serving yet or any more is never
// signalled, on any platform: it may be draining after a stop.
func TestStoppingLeaderIsNeverEvicted(t *testing.T) {
	p := testPaths(t)
	h := startHelperLeader(t, p, OwnerStopping, helperDefault)
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
	require.True(t, h.alive(), "a stopping leader is never signalled")
	require.Zero(t, starts.Load())
}

func TestConnectWithoutSpawnNeverEvicts(t *testing.T) {
	p := testPaths(t)
	h := startHelperLeader(t, p, OwnerServing, helperDefault)
	cfg := connectConfig(p)
	cfg.Deadline = 300 * time.Millisecond
	cfg.ZombieAfter = time.Nanosecond
	_, err := Connect(context.Background(), cfg)
	require.ErrorIs(t, err, ErrLeaderStarting)
	require.True(t, h.alive())
}

// A record from a leader that does not write the state is never evicted.
func TestLeaderWithoutStateIsNeverEvicted(t *testing.T) {
	p := testPaths(t)
	h := startHelperLeader(t, p, "", helperDefault)
	cfg := connectConfig(p)
	cfg.CallerExe = filepath.Join(askDir(t), "ask")
	cfg.Deadline = 500 * time.Millisecond
	cfg.ZombieAfter = time.Nanosecond
	cfg.StartFn = func(StartConfig) (*Child, error) { return nil, errors.New("must not start") }
	_, err := ConnectOrSpawn(context.Background(), cfg)
	require.ErrorIs(t, err, ErrLeaderStarting)
	require.True(t, h.alive())
}
