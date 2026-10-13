package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"AskCore/internal/leader"

	"github.com/stretchr/testify/require"
)

// busyLeaderWithJob starts a leader from bin, opens a session whose model starts
// a background job that ignores SIGTERM, and returns the leader owner, the file
// that records the process group of the job, and the home.
func busyLeaderWithJob(t *testing.T, f *providerFixture, bin string) (owner leader.Owner, pgid, home string) {
	t.Helper()
	pgid = filepath.Join(t.TempDir(), "pgid")
	scriptFixture(t, f,
		call(t, "bash", map[string]any{"command": recordGroup(pgid) + "trap '' TERM; sleep 300", "run_in_background": true}),
		say("job started"),
	)
	p, _, home := startToolLeaderFrom(t, f, bin)
	sid := p.newSession(newToolRepo(t))
	p.prompt(sid, "start the job")
	requireGroupAlive(t, pgid)
	paths, _ := leader.ResolvePaths(home)
	owner, err := leader.ReadOwner(paths)
	require.NoError(t, err)
	require.Equal(t, leader.OwnerServing, owner.State)
	return owner, pgid, home
}

// versionWrapper writes an `ask` that reports the version JSON of bin with a
// commit time one hour later, and runs bin for every other command. It stands
// in for a newer build of the same program.
func versionWrapper(t *testing.T, bin string) string {
	t.Helper()
	info := versionOf(t, bin)
	require.NotEmpty(t, info.CommitTime, "this test needs a binary with a VCS stamp")
	commit, err := time.Parse(time.RFC3339, info.CommitTime)
	require.NoError(t, err)
	info.CommitTime = commit.Add(time.Hour).Format(time.RFC3339)
	raw, err := json.Marshal(info)
	require.NoError(t, err)
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = version ]; then echo '%s'; exit 0; fi\nexec %q \"$@\"\n", raw, bin)
	path := filepath.Join(dir, "ask")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}

// A client of a newer build stops an older leader that a client started, also
// when it runs a job, and the jobs end with it.
func TestLeaderE2ENewerBuildReplacesBusyOlderLeader(t *testing.T) {
	bin := acpBinary(t)
	f := newProviderFixture(t)
	owner, pgid, home := busyLeaderWithJob(t, f, bin)
	wrapper := versionWrapper(t, bin)
	hello := versionOf(t, wrapper)

	cfg := e2eConnect(home, wrapper)
	cfg.CallerIsAsk = true
	cfg.Env = toolLeaderEnv(home, f)
	cfg.Hello.CommitTime = hello.CommitTime
	c, err := leader.ConnectOrSpawn(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	require.NotEqual(t, owner.Instance, c.Info.InstanceID)
	require.Eventually(t, func() bool { return processGone(owner.PID) }, 10*time.Second, 20*time.Millisecond)
	requireGroupGoneSoon(t, pgid)
	require.Len(t, leaderProcesses(bin), 1)
}
