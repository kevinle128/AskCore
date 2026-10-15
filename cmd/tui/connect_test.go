package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseConnectArgs(t *testing.T) {
	for argv, want := range map[string]connectOptions{
		"":                {},
		"--cwd /tmp/x":    {cwd: "/tmp/x"},
		"--attach sess_1": {attach: "sess_1"},
		"--help":          {help: true},
	} {
		got, err := parseConnectArgs(strings.Fields(argv))
		require.NoError(t, err, argv)
		require.Equal(t, want, got, argv)
	}
	for _, argv := range []string{"--cwd", "--attach", "--cwd /a --attach s", "extra", "--bogus"} {
		_, err := parseConnectArgs(strings.Fields(argv))
		require.Error(t, err, argv)
	}
}

func TestParseInputLine(t *testing.T) {
	cases := map[string]inputCommand{
		"hello there":          {kind: inputPrompt, text: "hello there"},
		"  spaced prompt  ":    {kind: inputPrompt, text: "spaced prompt"},
		"/new":                 {kind: inputNew},
		"/new /tmp/work":       {kind: inputNew, args: []string{"/tmp/work"}},
		"/sessions":            {kind: inputSessions},
		"/attach sess_1":       {kind: inputAttach, args: []string{"sess_1"}},
		"/detach":              {kind: inputDetach},
		"/take":                {kind: inputTake},
		"/cancel":              {kind: inputCancel},
		"/follow":              {kind: inputFollow},
		"/follow e1 42":        {kind: inputFollow, args: []string{"e1", "42"}},
		"/unfollow":            {kind: inputUnfollow},
		"/answer q1 allow":     {kind: inputAnswer, args: []string{"q1", "allow"}},
		"/quit":                {kind: inputQuit},
		"//literal slash text": {kind: inputPrompt, text: "/literal slash text"},
	}
	for line, want := range cases {
		got, err := parseInputLine(line)
		require.NoError(t, err, line)
		require.Equal(t, want, got, line)
	}
	for _, line := range []string{"/bogus", "/attach", "/attach a b", "/follow e1", "/follow e1 x", "/answer q1", "/take now", "/quit now"} {
		_, err := parseInputLine(line)
		require.Error(t, err, line)
	}
	_, err := parseInputLine("")
	require.ErrorIs(t, err, errBlankLine)
}

func TestConnectHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	deps := runDependencies{getenv: func(string) string { return "" }}
	code := runWithDependencies([]string{"connect", "--help"}, bytes.NewReader(nil), &out, &errOut, deps)
	require.Zero(t, code)
	require.Contains(t, out.String(), "/attach ID")
	require.Contains(t, out.String(), "back to an agent of its own")
}

func TestConnectFailsVisiblyWithoutAsk(t *testing.T) {
	// No ask binary next to the caller and none on the PATH: the client says so
	// and does not fall back to an agent of its own.
	home := leaderHome(t)
	t.Setenv("PATH", t.TempDir())
	var out, errOut bytes.Buffer
	deps := runDependencies{getenv: func(k string) string {
		if k == "ASK_HOME" {
			return home
		}
		return ""
	}}
	code := runConnect(nil, bytes.NewReader(nil), &out, &errOut, deps, nil, "/nonexistent/ask-test-caller")
	require.Equal(t, 1, code)
	require.Contains(t, errOut.String(), "ask binary was not found")
	require.Empty(t, out.String())
}
