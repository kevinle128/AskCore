package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"AskCore/internal/leader"

	"github.com/stretchr/testify/require"
)

// retryWrapper writes an `ask` next to the spawn caller. It runs the real
// binary for `version`, counts each `leader` run in the count file, fails the
// first failures runs of `leader` with exit status 1 and a numbered line on
// stderr, and then runs the real binary.
func retryWrapper(t *testing.T, dir, bin string, failures int) (countFile string) {
	t.Helper()
	countFile = filepath.Join(dir, "leader-runs")
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
version) exec %[1]q "$@" ;;
leader)
	echo run >> %[2]q
	n=$(wc -l < %[2]q | tr -d ' ')
	if [ "$n" -le %[3]d ]; then
		echo "wrapper start $n failed" >&2
		exit 1
	fi
	exec %[1]q "$@" ;;
esac
exec %[1]q "$@"
`, bin, countFile, failures)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ask"), []byte(script), 0o755))
	return countFile
}

func countLines(t *testing.T, file string) int {
	t.Helper()
	b, err := os.ReadFile(file)
	require.NoError(t, err)
	return strings.Count(string(b), "\n")
}

// A leader that ends with an error before it serves is started again, and the
// call returns the leader of the third start.
func TestLeaderE2ESpawnRetriesAfterEarlyExit(t *testing.T) {
	bin := acpBinary(t)
	dir := t.TempDir()
	caller := compileSpawnCaller(t, dir)
	countFile := retryWrapper(t, dir, bin, 2)
	home := leaderHome(t)
	stopLeaderAtEnd(t, bin, home)

	out, errOut, err := runCaller(t, caller, t.TempDir(), leaderEnv(home))
	require.NoError(t, err, errOut)
	require.NotEmpty(t, out, "the caller prints the instance id")
	require.Equal(t, 3, countLines(t, countFile), "two failed starts and one that serves")

	st, err := leader.Status(t.Context(), e2eConnect(home, bin))
	require.NoError(t, err)
	require.Equal(t, out, st.InstanceID)
}

// Three failed starts end the call with the log of the last one.
func TestLeaderE2ESpawnGivesUpAfterThreeStarts(t *testing.T) {
	bin := acpBinary(t)
	dir := t.TempDir()
	caller := compileSpawnCaller(t, dir)
	countFile := retryWrapper(t, dir, bin, 100)
	home := leaderHome(t)

	_, errOut, err := runCaller(t, caller, t.TempDir(), leaderEnv(home))
	require.Error(t, err)
	require.Contains(t, errOut, "the leader did not start")
	require.Contains(t, errOut, "after 3 starts")
	require.Contains(t, errOut, "wrapper start 3 failed")
	require.Equal(t, 3, countLines(t, countFile))
	require.Empty(t, leaderProcesses(bin))
	paths, _ := leader.ResolvePaths(home)
	held, _ := leader.LockHeld(paths)
	require.False(t, held)
}
