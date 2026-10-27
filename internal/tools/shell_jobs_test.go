package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/jobs"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

func TestJobOutputRunningAnswersAtOnceWithNoticeLine(t *testing.T) {
	h := newShellHarness(t)
	h.mustRun("bash", map[string]any{"command": "echo first; " + gateLoop, "run_in_background": true})
	var seen string
	var res protocol.ToolExecutionResult
	var elapsed time.Duration
	require.Eventually(t, func() bool {
		begin := time.Now()
		r, err := h.run("job_output", map[string]any{"job_id": "bash-1"})
		elapsed = time.Since(begin)
		if err != nil {
			return false
		}
		res = r
		seen += shellText(res)
		return strings.Contains(seen, "first")
	}, 5*time.Second, 5*time.Millisecond)

	assert.Less(t, elapsed, 50*time.Millisecond)
	text := shellText(res)
	assert.Contains(t, text, "[status: running]")
	assert.Contains(t, text, "A notice will arrive when it finishes; do not poll.")
	assert.False(t, isError(res))
	d := shellDetails(t, res)
	assert.Equal(t, "bash-1", d.JobID)
	assert.Equal(t, "running", d.Status)
}

func TestJobOutputWaitReturnsFinalStatusAndNoNoticeFollows(t *testing.T) {
	h := newShellHarness(t)
	h.mustRun("bash", map[string]any{"command": gateLoop + "; echo all done; exit 4", "run_in_background": true})
	call := h.start(context.Background(), "job_output", map[string]any{"job_id": "bash-1", "wait": true, "timeout_ms": 10000})
	time.Sleep(50 * time.Millisecond)
	require.False(t, call.finished(), "wait blocks while the job runs")

	h.release()
	res := call.wait(t)

	text := shellText(res)
	assert.Contains(t, text, "all done")
	assert.Contains(t, text, "[status: completed, exit code: 4]")
	assert.NotContains(t, text, "do not poll")
	d := shellDetails(t, res)
	require.NotNil(t, d.ExitCode)
	assert.Equal(t, 4, *d.ExitCode)
	assert.Equal(t, "completed", d.Status)
	assert.Empty(t, h.table.List(h.notices.Gen()), "reading the end removes the job")
	assert.Empty(t, h.notices.all(), "the model saw the end: no notice")
}

func TestJobOutputUnknownID(t *testing.T) {
	h := newShellHarness(t)

	_, err := h.run("job_output", map[string]any{"job_id": "bash-9"})

	require.Error(t, err)
	assert.Equal(t, "No job with id bash-9. Use job_list.", err.Error())
}

func TestJobOutputCanceledWaitKeepsUnreadOutput(t *testing.T) {
	h := newShellHarness(t, withFakeClock())
	h.mustRun("bash", map[string]any{"command": "echo first; touch ready; " + gateLoop, "run_in_background": true})
	ctx, cancel := context.WithCancel(context.Background())
	call := h.start(ctx, "job_output", map[string]any{"job_id": "bash-1", "wait": true})
	require.Eventually(t, func() bool { return h.clock.Pending() > 0 }, 5*time.Second, time.Millisecond)
	require.Eventually(t, func() bool { _, err := os.Stat(h.path("ready")); return err == nil }, 5*time.Second, time.Millisecond)
	cancel()
	select {
	case <-call.done:
		require.ErrorIs(t, call.err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("canceled job_output did not return")
	}
	assert.Contains(t, shellText(h.mustRun("job_output", map[string]any{"job_id": "bash-1"})), "first")
}

func TestJobOutputLargeTimeoutUsesMaximumWait(t *testing.T) {
	h := newShellHarness(t, withFakeClock())
	h.mustRun("bash", map[string]any{"command": gateLoop, "run_in_background": true})
	call := h.start(context.Background(), "job_output", map[string]any{"job_id": "bash-1", "wait": true, "timeout_ms": 1e30})
	require.Eventually(t, func() bool { return h.clock.Pending() > 0 }, 5*time.Second, time.Millisecond)
	h.clock.Advance(30 * time.Second)
	assert.Never(t, call.finished, 50*time.Millisecond, time.Millisecond, "a large timeout must not become the 30-second default")
	h.clock.Advance(570 * time.Second)
	res := call.wait(t)
	assert.Contains(t, shellText(res), "[status: running]")
	h.release()
}

func TestJobOutputOverTheLimitsIsShownAsHeadAndTail(t *testing.T) {
	h := newShellHarness(t)
	h.mustRun("bash", map[string]any{"command": "seq 1 60000", "run_in_background": true})

	res := h.mustRun("job_output", map[string]any{"job_id": "bash-1", "wait": true, "timeout_ms": 10000})

	text := shellText(res)
	assert.Contains(t, text, "bytes elided")
	assert.True(t, strings.HasSuffix(text, "[status: completed, exit code: 0]"), text[len(text)-60:])
	assert.Less(t, len(text), 60*1024)
	assert.Less(t, strings.Count(text, "\n"), 2100)
}

func TestJobKillReturnsFinalStatus(t *testing.T) {
	h := newShellHarness(t)
	h.mustRun("bash", map[string]any{"command": "echo working; " + gateLoop, "run_in_background": true})
	time.Sleep(100 * time.Millisecond)

	res := h.mustRun("job_kill", map[string]any{"job_id": "bash-1", "reason": "not needed"})

	text := shellText(res)
	assert.True(t, strings.HasPrefix(text, "Job bash-1 killed.\n"), text)
	assert.Contains(t, text, "working")
	assert.Contains(t, text, "[status: killed, reason: not needed]")
	assert.False(t, isError(res))
	d := shellDetails(t, res)
	assert.Equal(t, "bash-1", d.JobID)
	assert.Equal(t, "killed", d.Status)
	assert.Empty(t, h.table.List(h.notices.Gen()))
	assert.Empty(t, h.notices.all(), "a kill by the model sends no notice")
}

func TestJobKillOfFinishedJobSaysSo(t *testing.T) {
	h := newShellHarness(t)
	h.mustRun("bash", map[string]any{"command": "echo hi", "run_in_background": true})
	require.Eventually(t, func() bool { return h.table.List(h.notices.Gen())[0].Status == jobs.StatusCompleted }, 5*time.Second, 5*time.Millisecond)

	res := h.mustRun("job_kill", map[string]any{"job_id": "bash-1"})

	text := shellText(res)
	assert.True(t, strings.HasPrefix(text, "Job bash-1 had already finished.\n"), text)
	assert.Contains(t, text, "[status: completed, exit code: 0]")
}

func TestJobKillUnknownID(t *testing.T) {
	h := newShellHarness(t)
	_, err := h.run("job_kill", map[string]any{"job_id": "monitor-3"})
	require.Error(t, err)
	assert.Equal(t, "No job with id monitor-3. Use job_list.", err.Error())
}

func TestJobListFormatsLines(t *testing.T) {
	h := newShellHarness(t)
	assert.Equal(t, "No jobs.", shellText(h.mustRun("job_list", map[string]any{})))
	h.mustRun("bash", map[string]any{"command": ": one; " + gateLoop, "run_in_background": true})
	h.mustRun("bash", map[string]any{"command": ": two\n" + gateLoop, "run_in_background": true})

	res := h.mustRun("job_list", map[string]any{})

	lines := strings.Split(shellText(res), "\n")
	require.Len(t, lines, 2)
	assert.Regexp(t, regexp.MustCompile(`^bash-1 \[bash\] running \d+s - : one; while \[ ! -e gate \]`), lines[0])
	assert.Regexp(t, regexp.MustCompile(`^bash-2 \[bash\] running \d+s - : two$`), lines[1])
}

func TestShellToolConcurrencyFlags(t *testing.T) {
	set := tools.NewShellTools(jobs.New(jobs.Options{}))
	byName := map[string]tools.Tool{}
	for _, tool := range set {
		byName[tool.Decl().Name] = tool
	}
	require.Len(t, byName, 5)

	safe := func(name, args string) bool {
		cs, ok := byName[name].(tools.ConcurrencySafe)
		return ok && cs.ConcurrencySafe(json.RawMessage(args))
	}
	assert.True(t, safe("job_list", `{}`))
	assert.True(t, safe("job_output", `{"job_id":"bash-1"}`))
	assert.True(t, safe("job_output", `{"job_id":"bash-1","wait":false}`))
	assert.False(t, safe("job_output", `{"job_id":"bash-1","wait":true}`))
	assert.False(t, safe("job_output", `not json`))
	assert.False(t, safe("bash", `{"command":"ls"}`))
	assert.False(t, safe("monitor", `{"command":"ls","description":"d"}`))
	assert.False(t, safe("job_kill", `{"job_id":"bash-1"}`))
	for name, tool := range byName {
		pc, ok := tool.(tools.PromptContributor)
		require.True(t, ok, name)
		assert.NotEmpty(t, pc.PromptSnippet(), name)
	}
}

func TestJobToolsRefuseWithoutPort(t *testing.T) {
	h := newShellHarness(t)
	calls := map[string]any{
		"bash":       map[string]any{"command": "true"},
		"monitor":    map[string]any{"command": "true", "description": "x"},
		"job_output": map[string]any{"job_id": "bash-1"},
		"job_kill":   map[string]any{"job_id": "bash-1"},
		"job_list":   map[string]any{},
	}
	for name, args := range calls {
		t.Run(name, func(t *testing.T) {
			_, err := h.set[name].Execute(context.Background(), tools.Context{CallID: "c1", Cwd: h.cwd}, h.prepare(name, args))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "needs an agent conversation")
		})
	}
	assert.Empty(t, h.table.List(h.notices.Gen()), "a refused call starts no job")
}
