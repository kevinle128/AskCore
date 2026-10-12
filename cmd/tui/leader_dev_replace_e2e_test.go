package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"AskCore/internal/leader"

	"github.com/stretchr/testify/require"
)

var connectedInstance = regexp.MustCompile(`connected instance=(\S+)`)

// runConnectOnce runs `<bin> connect` with an empty stdin and returns the
// instance id that it printed.
func runConnectOnce(t *testing.T, bin string, env []string) string {
	t.Helper()
	cmd := exec.Command(bin, "connect")
	cmd.Env = env
	cmd.Dir = t.TempDir()
	cmd.Stdin = bytes.NewReader(nil)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	m := connectedInstance.FindStringSubmatch(string(out))
	require.NotNil(t, m, string(out))
	return m[1]
}

// A rebuilt dev binary (other bytes, written later) replaces an older busy dev
// leader. Both binaries compute their stamps with the production code.
func TestLeaderE2ENewerDevBuildReplacesBusyOlderDevLeader(t *testing.T) {
	oldSrc, newSrc := devBinaries(t)
	oldBin := devCopy(t, oldSrc, "ask", time.Now().Add(-time.Hour).Truncate(time.Second))
	newBin := devCopy(t, newSrc, "ask", time.Now().Truncate(time.Second))
	f := newProviderFixture(t)
	owner, pgid, home := busyLeaderWithJob(t, f, oldBin)

	id := runConnectOnce(t, newBin, toolLeaderEnv(home, f))

	require.NotEqual(t, owner.Instance, id)
	require.Eventually(t, func() bool { return processGone(owner.PID) }, 10*time.Second, 20*time.Millisecond)
	requireGroupGoneSoon(t, pgid)
	st, err := leader.Status(context.Background(), e2eConnect(home, newBin))
	require.NoError(t, err)
	require.Equal(t, id, st.InstanceID)
	require.Equal(t, versionOf(t, newBin).DevStamp, st.DevStamp)
	stopLeaderAtEnd(t, newBin, home)
}

// Two terminals that run the same bytes never replace each other, whatever the
// modification time of their files.
func TestLeaderE2ESameDevBuildKeepsTheLeader(t *testing.T) {
	oldSrc, _ := devBinaries(t)
	oldBin := devCopy(t, oldSrc, "ask", time.Now().Add(-time.Hour).Truncate(time.Second))
	sameBytes := devCopy(t, oldSrc, "ask", time.Now().Truncate(time.Second))
	f := newProviderFixture(t)
	owner, _, home := busyLeaderWithJob(t, f, oldBin)

	id := runConnectOnce(t, sameBytes, toolLeaderEnv(home, f))

	require.Equal(t, owner.Instance, id)
	require.False(t, processGone(owner.PID), "the leader was not stopped")
}

func TestLeaderE2ENewerBuildKeepsAConcurrentReplacement(t *testing.T) {
	oldSrc, newSrc := devBinaries(t)
	oldBin := devCopy(t, oldSrc, "ask", time.Now().Add(-time.Hour).Truncate(time.Second))
	newBin := devCopy(t, newSrc, "ask", time.Now().Truncate(time.Second))
	home := leaderHome(t)
	stopLeaderAtEnd(t, newBin, home)
	initial := e2eConnect(home, oldBin)
	initial.CallerIsAsk = true
	c, err := leader.ConnectOrSpawn(context.Background(), initial)
	require.NoError(t, err)
	_ = c.Close()

	// Pause the binary probe after registration, while another client replaces
	// the old leader with the new build.
	dir := t.TempDir()
	entered, release := filepath.Join(dir, "entered"), filepath.Join(dir, "release")
	wrapper := filepath.Join(dir, "ask")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = version ]; then\n: > %q\nwhile [ ! -f %q ]; do sleep 0.01; done\nfi\nexec %q \"$@\"\n", entered, release, newBin)
	require.NoError(t, os.WriteFile(wrapper, []byte(script), 0o755))
	cfg := e2eConnect(home, wrapper)
	cfg.CallerIsAsk = true
	cfg.Hello.DevStamp = versionOf(t, newBin).DevStamp
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	type result struct {
		client *leader.Client
		err    error
	}
	done := make(chan result, 1)
	go func() {
		client, err := leader.ConnectOrSpawn(ctx, cfg)
		done <- result{client, err}
	}()
	defer func() {
		cancel()
		if r := <-done; r.client != nil {
			_ = r.client.Close()
		}
	}()
	require.Eventually(t, func() bool {
		_, err := os.Stat(entered)
		return err == nil
	}, 10*time.Second, 10*time.Millisecond)

	_, err = leader.Stop(ctx, e2eConnect(home, oldBin), 5*time.Second)
	require.NoError(t, err)
	fresh := e2eConnect(home, newBin)
	fresh.CallerIsAsk = true
	winner, err := leader.ConnectOrSpawn(ctx, fresh)
	require.NoError(t, err)
	defer func() { _ = winner.Close() }()
	winnerID := winner.Info.InstanceID
	require.NoError(t, os.WriteFile(release, nil, 0o600))
	r := <-done
	done <- r // Keep the result for cleanup, including on a failed assertion.
	require.NoError(t, r.err)
	require.Equal(t, winnerID, r.client.Info.InstanceID, "the concurrent replacement must be adopted")
	st, err := leader.Status(ctx, fresh)
	require.NoError(t, err)
	require.Equal(t, winnerID, st.InstanceID)
}
