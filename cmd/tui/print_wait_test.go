package main

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/jobs"
)

// A background job that still runs at the final answer keeps ask -p open; its
// notice gets one more turn, and print mode prints that turn too.
func TestPrintWaitsForBackgroundJobAndAnswersAgain(t *testing.T) {
	t.Parallel()
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": "sleep 1; echo late-result", "run_in_background": true}),
		say("first answer"),
		say("saw the result"),
	)

	r := runPrint(t, m, printSpec{})

	assert.Equal(t, "first answer\nsaw the result\n", r.requireOK(t))
	require.Equal(t, 3, m.requestCount())
	assert.Contains(t, m.userText(2), "Background job bash-1")
	assert.Contains(t, m.userText(2), "[status: completed, exit code: 0]")
}

// A monitor that never stops printing cannot hold ask -p past the limit.
func TestPrintMonitorStopsAtBackgroundLimit(t *testing.T) {
	t.Parallel()
	pgid := filepath.Join(t.TempDir(), "pgid")
	m := scriptModel(t,
		call(t, "monitor", map[string]any{"command": recordGroup(pgid) + "while :; do echo tick; sleep 0.2; done", "description": "ticker", "persistent": true}),
		say("watching"),
	)
	// Each event wakes a turn; their number depends on timing.
	m.repeatLast = true

	r := runPrint(t, m, printSpec{env: shortBgLimit()})

	r.requireOK(t)
	requireGroupGone(t, pgid)
}

// With only a watch left and the Agent idle, the idle-wait cap ends the wait.
func TestPrintIdleCeilingEndsWait(t *testing.T) {
	t.Parallel()
	pgid := filepath.Join(t.TempDir(), "pgid")
	m := scriptModel(t,
		call(t, "monitor", map[string]any{"command": recordGroup(pgid) + "sleep 300", "description": "quiet", "persistent": true}),
		say("watching"),
	)
	env := map[string]string{printBgLimitEnv: "60000", printBgCeilingEnv: "300"}

	start := time.Now()
	r := runPrint(t, m, printSpec{env: env})

	assert.Equal(t, "watching\n", r.requireOK(t))
	assert.Less(t, time.Since(start), 30*time.Second, "the cap ended the wait long before the limit")
	requireGroupGone(t, pgid)
}

// A signal during the background wait ends ask -p at once with its code, and
// the process group of the job is gone.
func TestPrintSignalDuringBackgroundWait(t *testing.T) {
	for _, tc := range []struct {
		sig  syscall.Signal
		code int
	}{{syscall.SIGINT, 130}, {syscall.SIGTERM, 143}} {
		t.Run(tc.sig.String(), func(t *testing.T) {
			dir := t.TempDir()
			pgid := filepath.Join(dir, "pgid")
			command := recordGroup(pgid) + "trap '' TERM; sleep 300"
			f := newProviderFixture(t)
			f.inference = func(n int, _ *http.Request, _ []byte) (int, string, string) {
				if n == 0 {
					return toolReplyOf(t, "bash", map[string]any{"command": command, "run_in_background": true})
				}
				return http.StatusOK, "text/event-stream", sseText("first answer")
			}
			cmd := builtAsk(t, f, dir, nil, "-p", "--provider", "anthropic", "--model", "claude-sonnet-4-6", "start it")
			var stdout, stderr lockedBuffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			require.NoError(t, cmd.Start())
			exited := make(chan error, 1)
			go func() { exited <- cmd.Wait() }()
			t.Cleanup(func() { _ = cmd.Process.Kill() })

			group := readPgid(t, pgid)
			require.Eventually(t, func() bool { return strings.Contains(stdout.String(), "first answer") }, e2eWaitFor, 20*time.Millisecond,
				"ask printed its answer and waits for the job")
			require.NoError(t, cmd.Process.Signal(tc.sig))

			select {
			case err := <-exited:
				var ee *exec.ExitError
				require.ErrorAs(t, err, &ee, "stderr: %s", stderr.String())
				assert.Equal(t, tc.code, ee.ExitCode())
			case <-time.After(20 * time.Second):
				t.Fatalf("ask did not exit on %s during the wait\nstderr: %s", tc.sig, stderr.String())
			}
			assert.Eventually(t, func() bool { return groupGone(group) }, 10*time.Second, 20*time.Millisecond)
		})
	}
}

func TestPrintDisableBackgroundTasks(t *testing.T) {
	t.Parallel()
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": "sleep 300", "timeout": 1}),
		call(t, "bash", map[string]any{"command": "sleep 300", "run_in_background": true}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{env: map[string]string{disableBackgroundEnv: "1"}})

	assert.Equal(t, "done\n", r.requireOK(t))
	first := m.toolResult(1)
	assert.NotContains(t, first.Text, "continues as background job", "the command stops at its budget")
	assert.Contains(t, first.Text, "timeout")
	second := m.toolResult(2)
	assert.True(t, second.IsError)
	assert.Contains(t, second.Text, "Background jobs are turned off")
}

// --max-turns counts the whole run: the turns of the prompt and the turn that
// a notice wakes.
func TestPrintMaxTurnsCountsWokenTurns(t *testing.T) {
	t.Parallel()
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": "sleep 0.5", "run_in_background": true}),
		say("first answer"),
	)
	m.repeatLast = true

	r := runPrint(t, m, printSpec{args: []string{"--max-turns", "2"}})

	assert.Equal(t, 1, r.code, "stderr: %s", r.stderr)
	assert.Contains(t, r.stdout, "Error: Reached max turns (2)")
	assert.Equal(t, 2, m.requestCount(), "the woken turn did not reach the model")
}

func TestParseMaxTurns(t *testing.T) {
	o, diags := parseArgs([]string{"-p", "--max-turns", "3", "hi"})
	require.Empty(t, diags)
	assert.Equal(t, 3, o.maxTurns)
	for _, bad := range []string{"0", "-1", "two"} {
		_, diags := parseArgs([]string{"-p", "--max-turns", bad, "hi"})
		require.Len(t, diags, 1, bad)
		assert.True(t, diags[0].err, bad)
	}
}

// A session of the TUI, the leader or ask acp has no background limit.
func TestNativeSessionHasNoBackgroundLimit(t *testing.T) {
	_, jt := newTestSession(t, tokenPlanOptions(&scriptedModel{}), nativeEnv(nil))
	j, err := jt.Start(jobs.StartSpec{Command: "sleep 300", Cwd: t.TempDir(), BackgroundLimit: time.Millisecond})
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, jobs.StatusRunning, j.Status(), "no limit stops it")
}
