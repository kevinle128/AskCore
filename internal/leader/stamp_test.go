package leader

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	hashA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func commitStamp(t time.Time) buildStamp {
	return buildStamp{CommitTime: t.UTC().Format(time.RFC3339)}
}

func devStamp(t time.Time, hash string) buildStamp {
	return buildStamp{DevStamp: t.UTC().Format(time.RFC3339Nano) + " " + hash}
}

func TestNewerBuildDecision(t *testing.T) {
	base := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		client, leader buildStamp
		want           bool
	}{
		"later":               {commitStamp(base.Add(time.Hour)), commitStamp(base), true},
		"equal":               {commitStamp(base), commitStamp(base), false},
		"earlier":             {commitStamp(base.Add(-time.Hour)), commitStamp(base), false},
		"client has no stamp": {buildStamp{}, commitStamp(base), false},
		"leader has no stamp": {commitStamp(base), buildStamp{}, false},
		"both empty":          {buildStamp{}, buildStamp{}, false},
		"client not RFC 3339": {buildStamp{CommitTime: "yesterday"}, commitStamp(base), false},
		"leader not RFC 3339": {commitStamp(base), buildStamp{CommitTime: "yesterday"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, newerBuild(tc.client, tc.leader))
		})
	}
}

func TestSameDevHashKeepsTheLeader(t *testing.T) {
	base := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	require.False(t, newerBuild(devStamp(base.Add(time.Hour), hashA), devStamp(base, hashA)))
}

func TestNewerDevBuildWithAnotherHashReplaces(t *testing.T) {
	base := time.Date(2026, 10, 11, 9, 0, 0, 123, time.UTC)
	require.True(t, newerBuild(devStamp(base.Add(time.Nanosecond), hashB), devStamp(base, hashA)))
}

func TestOlderDevBuildKeepsTheLeader(t *testing.T) {
	base := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	require.False(t, newerBuild(devStamp(base.Add(-time.Hour), hashB), devStamp(base, hashA)))
	require.False(t, newerBuild(devStamp(base, hashB), devStamp(base, hashA)), "the same time keeps the leader")
}

func TestDevAndVCSBuildsAreNeverCompared(t *testing.T) {
	base := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	for _, later := range []time.Duration{time.Hour, -time.Hour, 0} {
		require.False(t, newerBuild(devStamp(base.Add(later), hashB), commitStamp(base)))
		require.False(t, newerBuild(commitStamp(base.Add(later)), devStamp(base, hashA)))
	}
}

func TestBadDevStampKeepsTheLeader(t *testing.T) {
	base := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	good := devStamp(base, hashA)
	later := base.Add(time.Hour).Format(time.RFC3339Nano)
	for name, bad := range map[string]string{
		"no hash prefix": later + " " + strings.TrimPrefix(hashB, "sha256:"),
		"short hash":     later + " sha256:abcd",
		"bad time":       "noon " + hashB,
		"no hash":        later,
		"upper hex":      later + " sha256:" + strings.Repeat("B", 64),
	} {
		t.Run(name, func(t *testing.T) {
			require.False(t, newerBuild(buildStamp{DevStamp: bad}, good))
			require.False(t, newerBuild(good, buildStamp{DevStamp: bad}))
		})
	}
}

func TestParseDevStamp(t *testing.T) {
	mod, hash, ok := parseDevStamp("2026-10-11T16:42:48.797541Z " + hashA)
	require.True(t, ok)
	require.Equal(t, time.Date(2026, 10, 11, 16, 42, 48, 797541000, time.UTC), mod)
	require.Equal(t, hashA, "sha256:"+hash)
}
