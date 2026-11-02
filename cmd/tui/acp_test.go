package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/app"
	"AskCore/internal/testsupport"
)

// offlineModelRefresh is the model refresh of a fixture that builds ACPParams
// itself: off, with an HTTP client that fails the test on a request to any
// host but loopback, in case the refresh is turned on by mistake.
func offlineModelRefresh(t *testing.T) app.ModelRefresh {
	return app.ModelRefresh{Enabled: false, HTTP: testsupport.LoopbackOnly(t)}
}

// The product entry point turns the model refresh on in the shared host
// parameters (leader, ask acp, in-process TUI); the environment switch and
// the dependencies of a test turn it off.
func TestACPParamsModelRefreshSwitch(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	p, err := acpParams(runDependencies{getenv: env(nil), modelRefresh: true})
	require.NoError(t, err)
	require.True(t, p.ModelRefresh.Enabled)
	require.NotNil(t, p.ModelRefresh.HTTP)

	p, err = acpParams(runDependencies{getenv: env(map[string]string{disableModelRefreshEnv: "1"}), modelRefresh: true})
	require.NoError(t, err)
	require.False(t, p.ModelRefresh.Enabled)

	p, err = acpParams(runDependencies{getenv: env(nil)})
	require.NoError(t, err)
	require.False(t, p.ModelRefresh.Enabled, "test dependencies stay offline")
}

// bare acp must select the protocol command, not a positional prompt.
func TestACPDispatchHelp(t *testing.T) {
	for _, argv := range [][]string{{"acp", "--help"}, {"acp", "-h"}} {
		var out, errb bytes.Buffer
		code := runWithDependencies(argv, strings.NewReader(""), &out, &errb, runDependencies{getenv: func(string) string { return "" }})
		require.Equal(t, 0, code, errb.String())
		assert.Contains(t, out.String(), "ask acp", "argv %v", argv)
		assert.Contains(t, out.String(), "ACP v1", "argv %v", argv)
		assert.Empty(t, errb.String())
	}
}

func TestACPDispatchRejectsArguments(t *testing.T) {
	for _, argv := range [][]string{{"acp", "extra"}, {"acp", "-p", "hi"}, {"acp", "--provider", "faux"}, {"acp", "--mode", "json"}} {
		var out, errb bytes.Buffer
		code := runWithDependencies(argv, strings.NewReader(""), &out, &errb, runDependencies{getenv: func(string) string { return "" }})
		assert.Equal(t, 1, code, "argv %v", argv)
		assert.Contains(t, errb.String(), "acp does not accept", "argv %v", argv)
		assert.Empty(t, out.String(), "argv %v", argv)
	}
}

// Only the first token selects the command. Everywhere else acp stays a message.
func TestACPWordStaysPromptElsewhere(t *testing.T) {
	for _, argv := range [][]string{{"-p", "acp"}, {"--", "acp"}, {"--mode", "json", "acp"}} {
		o, diags := parseArgs(argv)
		assert.Empty(t, diags, "argv %v", argv)
		assert.Equal(t, []string{"acp"}, o.messages, "argv %v", argv)
	}
	o, _ := parseArgs([]string{"@acp"})
	assert.Equal(t, []string{"acp"}, o.files)
}

// The usage text of the main command names the new command.
func TestUsageNamesACP(t *testing.T) {
	assert.Contains(t, usage, "ask acp")
}
