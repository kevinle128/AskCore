package leader

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

var (
	oldCommit = time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC).Format(time.RFC3339)
	newCommit = time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
)

// stampedAsk returns a directory with an `ask` that reports the current
// protocol and the given stamps.
func stampedAsk(t *testing.T, commit, dev string) string {
	t.Helper()
	dir := t.TempDir()
	info := protocol.VersionInfo{Name: "ask", Build: "dev", LeaderProtocol: protocol.LeaderProtocolVersion, ACPVersion: 1, CommitTime: commit, DevStamp: dev}
	raw, err := json.Marshal(info)
	require.NoError(t, err)
	fakeAsk(t, dir, fmt.Sprintf(`if [ "$1" = version ]; then echo '%s'; exit 0; fi; exit 3`, raw))
	return dir
}

// newerRig is a running leader of an older build and the config of a client
// that is newer. Each leader that the client starts gets a stamp from next.
type newerRig struct {
	pl     *procLeader
	paths  Paths
	oldID  string
	cfg    ConnectConfig
	starts atomic.Int32
	hints  []string
}

type leaderStamps struct {
	commit, dev   string
	busy          bool
	supervisedRun bool
}

// startNewerRig starts a leader with the stamps of `old` and returns a client
// config whose hello and binary carry the stamps of `client`. A leader that the
// client starts later gets the stamps of `next`.
func startNewerRig(t *testing.T, old, next leaderStamps, client buildStamp, binary buildStamp) *newerRig {
	t.Helper()
	r := &newerRig{pl: newProcLeader(t), paths: testPaths(t)}
	r.pl.server = func(c *Config) {
		s := next
		if r.pl.servers.Load() == 0 {
			s = old
		}
		c.Server.CommitTime, c.Server.DevStamp = s.commit, s.dev
		c.SpawnedByClient = !s.supervisedRun
		if s.busy {
			c.QuiesceIfIdle = func() bool { return false }
		}
	}
	_, err := r.pl.start(StartConfig{Paths: r.paths})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return socketServing(r.paths) }, 5*time.Second, 10*time.Millisecond)
	c, err := Connect(context.Background(), connectConfig(r.paths))
	require.NoError(t, err)
	r.oldID = c.Info.InstanceID
	_ = c.Close()

	r.cfg = connectConfig(r.paths)
	r.cfg.Hello.CommitTime, r.cfg.Hello.DevStamp = client.CommitTime, client.DevStamp
	r.cfg.CallerExe = filepath.Join(stampedAsk(t, binary.CommitTime, binary.DevStamp), "ask")
	r.cfg.Warn = func(s string) { r.hints = append(r.hints, s) }
	r.cfg.StartFn = func(sc StartConfig) (*Child, error) {
		r.starts.Add(1)
		return r.pl.start(sc)
	}
	return r
}

func TestNewerBuildReplacesABusyLeader(t *testing.T) {
	r := startNewerRig(t,
		leaderStamps{commit: oldCommit, busy: true}, leaderStamps{commit: newCommit},
		buildStamp{CommitTime: newCommit}, buildStamp{CommitTime: newCommit})
	c, err := ConnectOrSpawn(context.Background(), r.cfg)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	require.NotEqual(t, r.oldID, c.Info.InstanceID)
	require.Equal(t, newCommit, c.Info.CommitTime)
	require.EqualValues(t, 1, r.starts.Load())
}

func TestNewerDevBuildReplacesABusyLeader(t *testing.T) {
	oldDev := time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC).Format(time.RFC3339Nano) + " " + hashA
	newDev := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC).Format(time.RFC3339Nano) + " " + hashB
	r := startNewerRig(t,
		leaderStamps{dev: oldDev, busy: true}, leaderStamps{dev: newDev},
		buildStamp{DevStamp: newDev}, buildStamp{DevStamp: newDev})
	c, err := ConnectOrSpawn(context.Background(), r.cfg)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	require.NotEqual(t, r.oldID, c.Info.InstanceID)
	require.Equal(t, newDev, c.Info.DevStamp)
	require.EqualValues(t, 1, r.starts.Load())
}

func TestOlderOrUnknownBuildKeepsTheLeader(t *testing.T) {
	devAt := func(h int, hash string) string {
		return time.Date(2026, 10, 11, h, 0, 0, 0, time.UTC).Format(time.RFC3339Nano) + " " + hash
	}
	for name, tc := range map[string]struct {
		leader, client buildStamp
	}{
		"older client":        {buildStamp{CommitTime: newCommit}, buildStamp{CommitTime: oldCommit}},
		"equal":               {buildStamp{CommitTime: oldCommit}, buildStamp{CommitTime: oldCommit}},
		"client has no stamp": {buildStamp{CommitTime: oldCommit}, buildStamp{}},
		"leader has no stamp": {buildStamp{}, buildStamp{CommitTime: newCommit}},
		"same dev hash":       {buildStamp{DevStamp: devAt(8, hashA)}, buildStamp{DevStamp: devAt(9, hashA)}},
		"dev client, vcs leader": {buildStamp{CommitTime: oldCommit},
			buildStamp{DevStamp: devAt(9, hashB)}},
	} {
		t.Run(name, func(t *testing.T) {
			r := startNewerRig(t, leaderStamps{commit: tc.leader.CommitTime, dev: tc.leader.DevStamp, busy: true}, leaderStamps{},
				tc.client, tc.client)
			c, err := ConnectOrSpawn(context.Background(), r.cfg)
			require.NoError(t, err)
			defer func() { _ = c.Close() }()
			require.Equal(t, r.oldID, c.Info.InstanceID)
			require.Zero(t, r.starts.Load())
		})
	}
}

func TestNewerClientKeepsTheLeaderWhenTheBinaryIsNotNewer(t *testing.T) {
	r := startNewerRig(t,
		leaderStamps{commit: newCommit, busy: true}, leaderStamps{},
		buildStamp{CommitTime: time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC).Format(time.RFC3339)}, // client is newer
		buildStamp{CommitTime: oldCommit}) // the binary that would start is older
	c, err := ConnectOrSpawn(context.Background(), r.cfg)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	require.Equal(t, r.oldID, c.Info.InstanceID)
	require.Zero(t, r.starts.Load())
	require.Len(t, r.hints, 1)
	require.Contains(t, r.hints[0], "binary")
}

func TestNewerBuildKeepsASupervisedLeader(t *testing.T) {
	r := startNewerRig(t,
		leaderStamps{commit: oldCommit, supervisedRun: true}, leaderStamps{},
		buildStamp{CommitTime: newCommit}, buildStamp{CommitTime: newCommit})
	c, err := ConnectOrSpawn(context.Background(), r.cfg)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	require.Equal(t, r.oldID, c.Info.InstanceID)
	require.Zero(t, r.starts.Load())
	require.Len(t, r.hints, 1)
	require.Contains(t, r.hints[0], "not started by a client")
}

// The leader that replaces the old one may again be older than the client. The
// call uses it and does not stop it a second time.
func TestNewerBuildReplacesOncePerCall(t *testing.T) {
	r := startNewerRig(t,
		leaderStamps{commit: oldCommit}, leaderStamps{commit: oldCommit},
		buildStamp{CommitTime: newCommit}, buildStamp{CommitTime: newCommit})
	c, err := ConnectOrSpawn(context.Background(), r.cfg)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	require.NotEqual(t, r.oldID, c.Info.InstanceID)
	require.EqualValues(t, 1, r.starts.Load())
	require.EqualValues(t, 2, r.pl.servers.Load(), "one old leader and one replacement")
}

func TestConnectNeverReplaces(t *testing.T) {
	r := startNewerRig(t,
		leaderStamps{commit: oldCommit}, leaderStamps{commit: newCommit},
		buildStamp{CommitTime: newCommit}, buildStamp{CommitTime: newCommit})
	c, err := Connect(context.Background(), r.cfg)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	require.Equal(t, r.oldID, c.Info.InstanceID)
	require.Zero(t, r.starts.Load())
}

// A newer binary that cannot start does not cost the old leader.
func TestNewerBuildProbesTheBinaryBeforeItStopsTheLeader(t *testing.T) {
	r := startNewerRig(t,
		leaderStamps{commit: oldCommit, busy: true}, leaderStamps{},
		buildStamp{CommitTime: newCommit}, buildStamp{CommitTime: newCommit})
	dir := t.TempDir()
	fakeAsk(t, dir, "exit 5")
	r.cfg.CallerExe = filepath.Join(dir, "ask")
	c, err := ConnectOrSpawn(context.Background(), r.cfg)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	require.Equal(t, r.oldID, c.Info.InstanceID)
	require.Zero(t, r.starts.Load())
	_ = os.Remove(filepath.Join(dir, "ask"))
}
