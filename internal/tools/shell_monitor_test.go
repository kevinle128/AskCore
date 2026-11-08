package tools_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/jobs"
)

// quietWatch is a command that prints nothing and runs until it is stopped.
const quietWatch = "while :; do sleep 0.05; done"

func monitorDetails(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	return m
}

func TestMonitorRejectsTimeoutAboveMax(t *testing.T) {
	h := newShellHarness(t)

	_, err := h.run("monitor", map[string]any{"command": quietWatch, "description": "tail", "timeout_ms": 36_000_001})

	require.Error(t, err)
	assert.Equal(t, "persistent must be true when timeout_ms exceeds 36000000 ms", err.Error())
	assert.Empty(t, h.table.List(h.notices.Gen()), "no job starts")

	res := h.mustRun("monitor", map[string]any{"command": quietWatch, "description": "tail", "timeout_ms": 36_000_001, "persistent": true})

	assert.False(t, isError(res))
	assert.Equal(t, "monitor-1", monitorDetails(t, res.Details)["jobId"])
	require.Len(t, h.table.List(h.notices.Gen()), 1)
}

func TestMonitorRejectsNonPositiveTimeout(t *testing.T) {
	h := newShellHarness(t)
	for _, v := range []float64{0, -1} {
		_, err := h.run("monitor", map[string]any{"command": quietWatch, "description": "tail", "timeout_ms": v})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout_ms")
	}
	assert.Empty(t, h.table.List(h.notices.Gen()))
}

func TestMonitorFractionalTimeoutUsesOneMillisecond(t *testing.T) {
	h := newShellHarness(t, withFakeClock())
	res := h.mustRun("monitor", map[string]any{"command": quietWatch, "description": "short", "timeout_ms": 0.5})
	assert.EqualValues(t, 1, monitorDetails(t, res.Details)["timeoutMs"])
	h.clock.Advance(time.Millisecond)
	require.Eventually(t, func() bool {
		list := h.table.List(h.notices.Gen())
		return len(list) == 1 && list[0].KillReason == "timeout"
	}, 5*time.Second, 10*time.Millisecond)
}

func TestMonitorResultCarriesResolvedTimeout(t *testing.T) {
	h := newShellHarness(t)

	res := h.mustRun("monitor", map[string]any{"command": quietWatch, "description": "build watch"})

	assert.Equal(t, "Started monitor-1 (build watch). Each stdout line arrives as a notice. Stop it with job_kill.", shellText(res))
	assert.False(t, isError(res))
	m := monitorDetails(t, res.Details)
	assert.Equal(t, "monitor-1", m["jobId"])
	assert.EqualValues(t, 36_000_000, m["timeoutMs"])
	assert.Equal(t, false, m["persistent"])
	assert.Equal(t, "execute", m["kind"])

	res = h.mustRun("monitor", map[string]any{"command": quietWatch, "description": "log tail", "persistent": true, "timeout_ms": 5000})

	m = monitorDetails(t, res.Details)
	assert.Equal(t, "monitor-2", m["jobId"])
	assert.EqualValues(t, 0, m["timeoutMs"])
	assert.Equal(t, true, m["persistent"])

	res = h.mustRun("monitor", map[string]any{"command": quietWatch, "description": "short", "timeout_ms": 5000})

	assert.EqualValues(t, 5000, monitorDetails(t, res.Details)["timeoutMs"])
}

func TestMonitorStartsJobCountedByCap(t *testing.T) {
	h := newShellHarness(t, withMaxActive(2))
	h.mustRun("monitor", map[string]any{"command": quietWatch, "description": "tail"})
	h.mustRun("bash", map[string]any{"command": gateLoop, "run_in_background": true})

	list := shellText(h.mustRun("job_list", map[string]any{}))

	lines := strings.Split(list, "\n")
	require.Len(t, lines, 2)
	assert.Regexp(t, `^monitor-1 \[monitor\] running \d+s - `, lines[0])
	assert.Regexp(t, `^bash-1 \[bash\] running `, lines[1])
	_, err := h.run("monitor", map[string]any{"command": quietWatch, "description": "third"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "Too many active jobs")
	assert.Equal(t, jobs.KindMonitor, h.table.List(h.notices.Gen())[0].Kind)
}

func TestMonitorDeliversStdoutLinesAsNotices(t *testing.T) {
	h := newShellHarness(t)

	h.mustRun("monitor", map[string]any{"command": "echo DONE build", "description": "build"})

	require.Eventually(t, func() bool {
		for _, text := range h.notices.all() {
			if strings.Contains(text, "DONE build") {
				return true
			}
		}
		return false
	}, 5*time.Second, 10*time.Millisecond)
}

func TestMonitorDeclaresItsContract(t *testing.T) {
	h := newShellHarness(t)
	decl := h.set["monitor"].Decl()

	assert.Equal(t, "monitor", decl.Name)
	for _, want := range []string{"grep --line-buffered", "persistent", "job_kill", "10 h"} {
		assert.Contains(t, decl.Description, want)
	}
	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(decl.Parameters, &schema))
	assert.ElementsMatch(t, []string{"command", "description"}, schema.Required)
	assert.NotContains(t, schema.Properties, "pattern")
	for _, name := range []string{"command", "description", "timeout_ms", "persistent"} {
		assert.Contains(t, schema.Properties, name)
	}
}
