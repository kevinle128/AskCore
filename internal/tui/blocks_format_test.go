package tui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFormatTokens(t *testing.T) {
	for in, want := range map[int64]string{
		-5: "0", 0: "0", 999: "999", 1000: "1.0k", 12_400: "12.4k", 99_949: "99.9k",
		99_950: "100k", 100_000: "100k", 250_499: "250k", 999_499: "999k", 999_999: "1.0M", 1_000_000: "1.0M", 2_345_000: "2.3M",
	} {
		require.Equal(t, want, formatTokens(in), "%d", in)
	}
}

func TestFormatCost(t *testing.T) {
	for in, want := range map[int64]string{
		-1: "$0.000", 0: "$0.000", 4_000: "$0.004", 31_000: "$0.031", 999_499: "$0.999", 999_999: "$1.00",
		1_000_000: "$1.00", 12_345_678: "$12.35",
	} {
		require.Equal(t, want, formatCost(in), "%d", in)
	}
}

func TestFormatDuration(t *testing.T) {
	for in, want := range map[time.Duration]string{
		-time.Second: "0.0s", 100 * time.Millisecond: "0.1s", 4200 * time.Millisecond: "4.2s",
		12 * time.Second: "12s", 65 * time.Second: "1m05s", 3725 * time.Second: "62m05s",
	} {
		require.Equal(t, want, formatDuration(in), "%v", in)
	}
}

func TestShortArgs(t *testing.T) {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	require.Equal(t, "", shortArgs(nil))
	require.Equal(t, "", shortArgs(raw(`{}`)))
	require.Equal(t, "ls -la", shortArgs(raw(`{"command":"ls -la","cwd":"/tmp"}`)), "known key first")
	require.Equal(t, "probe code request", shortArgs(raw(`{"zzz":1,"note":"probe\n code   request"}`)), "first string value in key order, white space collapsed")
	require.Equal(t, `{"n":1,"ok":true}`, shortArgs(raw("{ \"n\": 1, \"ok\": true }")), "no string value: compact JSON")
	require.Equal(t, "not json", shortArgs(raw(`not   json`)))
}

func TestClockTime(t *testing.T) {
	at := time.Date(2026, 10, 10, 14, 2, 30, 0, time.UTC).UnixMilli()
	require.Equal(t, "14:02", clockTime(at, time.UTC))
	require.Equal(t, "", clockTime(0, time.UTC))
}
