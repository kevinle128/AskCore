package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
