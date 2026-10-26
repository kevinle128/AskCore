package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPrintMonitorFractionalTimeoutDoesNotSelectDefault(t *testing.T) {
	m := scriptModel(t,
		call(t, "monitor", map[string]any{"command": "while :; do sleep 0.05; done", "description": "short watch", "timeout_ms": 0.5}),
		sayAfter(func([]byte) { time.Sleep(100 * time.Millisecond) }, "waiting"),
		say("done"),
	)
	r := runPrint(t, m, printSpec{})
	r.requireOK(t)
	assert.Equal(t, 3, m.requestCount(), "the short timeout must start a completion turn")
	if m.requestCount() > 2 {
		assert.True(t, strings.Contains(m.userText(2), "[status: killed, reason: timeout]"))
	}
}

// A foreground timeout above the maximum wait budget is cut and reported. With
// run_in_background, timeout is the background limit instead, which the job
// table caps.
func TestPrintForegroundBashReportsClampedTimeout(t *testing.T) {
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": "true", "timeout": 601}),
		say("done"),
	)
	r := runPrint(t, m, printSpec{})
	r.requireOK(t)
	assert.Contains(t, m.toolResult(1).Text, "[timeout limited to 600 s]")
}

func TestPrintJobOutputReportsSpillCap(t *testing.T) {
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": "head -c 68157440 /dev/zero", "run_in_background": true}),
		call(t, "job_output", map[string]any{"job_id": "bash-1", "wait": true}),
		say("done"),
	)
	r := runPrint(t, m, printSpec{})
	r.requireOK(t)
	assert.True(t, strings.Contains(m.toolResult(2).Text, "[spill file capped at 64 MB]"), "the result must report the spill cap")
}

func TestPrintJobOutputFractionalWaitDoesNotSelectDefault(t *testing.T) {
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": "sleep 0.3", "run_in_background": true}),
		call(t, "job_output", map[string]any{"job_id": "bash-1", "wait": true, "timeout_ms": 0.5}),
		call(t, "job_kill", map[string]any{"job_id": "bash-1"}),
		say("done"),
	)
	r := runPrint(t, m, printSpec{})
	r.requireOK(t)
	assert.Contains(t, m.toolResult(2).Text, "[status: running]")
}
