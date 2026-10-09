package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// leaderHome returns a short ASK_HOME under /tmp whose parent is a real directory.
func leaderHome(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "askl-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	dir, err = filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	return filepath.Join(dir, "home")
}

func runLeaderCmd(t *testing.T, home string, argv ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	deps := runDependencies{getenv: func(k string) string {
		if k == "ASK_HOME" {
			return home
		}
		return ""
	}}
	code = runWithDependencies(append([]string{"leader"}, argv...), bytes.NewReader(nil), &o, &e, deps)
	return code, o.String(), e.String()
}

func TestParseLeaderArgs(t *testing.T) {
	for argv, want := range map[string]leaderOptions{
		"":                    {},
		"--spawned-by-client": {spawned: true},
		"status":              {sub: "status"},
		"status --json":       {sub: "status", json: true},
		"list --json":         {sub: "list", json: true},
		"stop":                {sub: "stop"},
		"--help":              {help: true},
	} {
		got, err := parseLeaderArgs(strings.Fields(argv))
		require.NoError(t, err, argv)
		require.Equal(t, want, got, argv)
	}
	for _, argv := range []string{"bogus", "status stop", "stop --json", "status --spawned-by-client", "--port 80"} {
		_, err := parseLeaderArgs(strings.Fields(argv))
		require.Error(t, err, argv)
	}
}

func TestLeaderHelp(t *testing.T) {
	code, out, _ := runLeaderCmd(t, leaderHome(t), "--help")
	require.Zero(t, code)
	require.Contains(t, out, "ask leader stop")
	require.Contains(t, out, "<ASK_HOME>/leader.sock")
}

func TestLeaderRejectsBadArguments(t *testing.T) {
	code, out, errOut := runLeaderCmd(t, leaderHome(t), "--bogus")
	require.Equal(t, 1, code)
	require.Empty(t, out)
	require.Contains(t, errOut, "ask leader --help")
}

func TestLeaderRejectsRelativeHome(t *testing.T) {
	code, _, errOut := runLeaderCmd(t, "relative/home", "status")
	require.Equal(t, 1, code)
	require.Contains(t, errOut, "ASK_HOME")
}

func TestLeaderManagementWithoutLeader(t *testing.T) {
	home := leaderHome(t)
	code, _, errOut := runLeaderCmd(t, home, "status")
	require.Equal(t, 1, code)
	require.Contains(t, errOut, "no leader is running")

	code, out, _ := runLeaderCmd(t, home, "list")
	require.Zero(t, code)
	require.Contains(t, out, "no leader is running")

	code, out, _ = runLeaderCmd(t, home, "list", "--json")
	require.Zero(t, code)
	require.JSONEq(t, `[]`, out)

	code, out, _ = runLeaderCmd(t, home, "stop")
	require.Zero(t, code)
	require.Contains(t, out, "no leader is running")
	require.NoFileExists(t, filepath.Join(home, "leader.sock"), "these commands never start a leader")
	require.NoFileExists(t, filepath.Join(home, "leader.lock"))
}

func TestVersionIsNotStarted(t *testing.T) {
	var out bytes.Buffer
	require.Zero(t, run([]string{"version", "--json"}, bytes.NewReader(nil), &out, &bytes.Buffer{}))
	var m map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &m))
}

func TestRunUntilSignalCleanEndAndError(t *testing.T) {
	var errOut bytes.Buffer
	require.Zero(t, runUntilSignal(make(chan os.Signal, 2), &errOut, func(context.Context) error { return nil }))
	require.Empty(t, errOut.String())

	errOut.Reset()
	code := runUntilSignal(make(chan os.Signal, 2), &errOut, func(context.Context) error { return errors.New("link failed") })
	require.Equal(t, 1, code)
	require.Contains(t, errOut.String(), "ask leader: link failed")
}

func TestRunUntilSignalFirstSignalStopsInOrder(t *testing.T) {
	sigs := make(chan os.Signal, 2)
	var errOut bytes.Buffer
	stopped := make(chan struct{})
	go func() {
		time.Sleep(30 * time.Millisecond)
		sigs <- syscall.SIGTERM
	}()
	code := runUntilSignal(sigs, &errOut, func(ctx context.Context) error {
		<-ctx.Done() // the stop sequence begins when the context ends
		close(stopped)
		return context.Canceled
	})
	<-stopped
	require.Equal(t, 143, code)
	require.Empty(t, errOut.String(), "a stop by signal is not an error")
}

func TestRunUntilSignalSecondSignalReportsIncompleteCleanup(t *testing.T) {
	sigs := make(chan os.Signal, 2)
	var errOut bytes.Buffer
	release := make(chan struct{})
	defer close(release)
	go func() {
		time.Sleep(30 * time.Millisecond)
		sigs <- os.Interrupt
		time.Sleep(30 * time.Millisecond)
		sigs <- os.Interrupt
	}()
	code := runUntilSignal(sigs, &errOut, func(ctx context.Context) error {
		<-ctx.Done()
		<-release // the drain of a started tool body never ends
		return nil
	})
	require.Equal(t, 130, code, "a forced exit is never reported as success")
	require.Contains(t, errOut.String(), "cleanup did not drain; forced exit")
}
