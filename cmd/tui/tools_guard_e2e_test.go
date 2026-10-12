package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrintCommandGuardChecksCommandWrapperOptions(t *testing.T) {
	t.Parallel()
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": "command -p " + guardedCommand}),
		say("run goes on"),
	)

	r := runPrint(t, m, printSpec{env: map[string]string{"ASK_COMMAND_GUARD": "1"}})

	r.requireOK(t)
	res := m.toolResult(1)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "blocked by command-guard")
}
