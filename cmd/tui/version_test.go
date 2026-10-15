package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"AskCore/internal/leader"
	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func TestVersionJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"version", "--json"}, bytes.NewReader(nil), &out, &errOut)
	require.Zero(t, code, errOut.String())
	var info protocol.VersionInfo
	require.NoError(t, json.Unmarshal(out.Bytes(), &info))
	require.Equal(t, "ask", info.Name)
	require.Equal(t, protocol.LeaderProtocolVersion, info.LeaderProtocol)
	require.Equal(t, 1, info.ACPVersion)
	require.NotEmpty(t, info.Build)
	require.Equal(t, buildIdentity(), info.Build)
	require.Equal(t, 1, bytes.Count(out.Bytes(), []byte("\n")), "one JSON object on one line")
}

func TestVersionText(t *testing.T) {
	var out, errOut bytes.Buffer
	require.Zero(t, run([]string{"version"}, bytes.NewReader(nil), &out, &errOut))
	require.Equal(t, "ask "+buildIdentity()+"\n", out.String())
}

func TestVersionRejectsUnknownArgument(t *testing.T) {
	var out, errOut bytes.Buffer
	require.Equal(t, 1, run([]string{"version", "--bogus"}, bytes.NewReader(nil), &out, &errOut))
	require.Contains(t, errOut.String(), "ask version --help")
	require.Empty(t, out.String())
}

func TestVersionHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	require.Zero(t, run([]string{"version", "--help"}, bytes.NewReader(nil), &out, &errOut))
	require.Contains(t, out.String(), "ask version")
}

// vcsTimeOf reads the commit time that the Go tool stamped into a binary.
func vcsTimeOf(t *testing.T, bin string) string {
	t.Helper()
	out, err := exec.Command("go", "version", "-m", bin).Output()
	require.NoError(t, err)
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "build\tvcs.time="); ok {
			return v
		}
	}
	return ""
}

func TestVersionJSONCarriesCommitTime(t *testing.T) {
	bin := acpBinary(t)
	want := vcsTimeOf(t, bin)
	require.NotEmpty(t, want, "this test needs a binary with a VCS stamp: build inside a git work tree with git on the PATH")
	info := versionOf(t, bin)
	require.Equal(t, want, info.CommitTime)
	_, err := time.Parse(time.RFC3339, info.CommitTime)
	require.NoError(t, err)
	require.Empty(t, info.DevStamp, "a binary with a VCS stamp has no dev stamp")
}

func TestVersionJSONCarriesDevStamp(t *testing.T) {
	oldBin, _ := devBinaries(t)
	info := versionOf(t, oldBin)
	require.Empty(t, info.CommitTime)
	require.Equal(t, expectedDevStamp(t, oldBin), info.DevStamp)
}

// The dev stamp follows the file: the same bytes give the same hash at another
// time, other bytes give another hash.
func TestDevStampFollowsTheFile(t *testing.T) {
	oldBin, newBin := devBinaries(t)
	base := versionOf(t, oldBin).DevStamp
	require.NotEmpty(t, base)

	later := time.Now().Add(time.Hour).Truncate(time.Second)
	cp := devCopy(t, oldBin, "ask-copy", later)
	got := versionOf(t, cp).DevStamp
	require.Equal(t, expectedDevStamp(t, cp), got)
	require.NotEqual(t, base, got, "another time")
	require.Equal(t, strings.SplitN(base, " ", 2)[1], strings.SplitN(got, " ", 2)[1], "the same bytes give the same hash")

	other := versionOf(t, newBin).DevStamp
	require.NotEqual(t, strings.SplitN(base, " ", 2)[1], strings.SplitN(other, " ", 2)[1], "other bytes give another hash")
}

// A leader that a temporary binary started keeps its stamp after that file is
// removed, as `go run` removes its binary when the command ends.
func TestLeaderDevStampSurvivesARemovedBinary(t *testing.T) {
	oldBin, _ := devBinaries(t)
	temp := devCopy(t, oldBin, "ask", time.Now().Add(-time.Hour).Truncate(time.Second))
	want := expectedDevStamp(t, temp)
	home := leaderHome(t)
	stopLeaderAtEnd(t, oldBin, home)

	cfg := e2eConnect(home, temp)
	cfg.CallerIsAsk = true
	c, err := leader.ConnectOrSpawn(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, want, c.Info.DevStamp)
	_ = c.Close()
	require.NoError(t, os.Remove(temp))

	st, err := leader.Status(context.Background(), e2eConnect(home, oldBin))
	require.NoError(t, err)
	require.Equal(t, want, st.DevStamp)
	require.Empty(t, st.CommitTime)
}
