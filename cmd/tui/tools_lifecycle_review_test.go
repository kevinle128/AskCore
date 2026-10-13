package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintCommandGuardAllowsQuotedForkBombText(t *testing.T) {
	t.Parallel()
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": `printf '%s\n' ':(){ :|:& };:'`}),
		say("done"),
	)

	runPrint(t, m, printSpec{env: map[string]string{"ASK_COMMAND_GUARD": "1"}}).requireOK(t)
	res := m.toolResult(1)
	assert.False(t, res.IsError)
	assert.Equal(t, "exit: 0\n:(){ :|:& };:", res.Text)
}

func TestPrintSignalRemovesSpilledJobOutput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		signal syscall.Signal
		code   int
	}{{"SIGINT", syscall.SIGINT, 130}, {"SIGTERM", syscall.SIGTERM, 143}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			spillDir := t.TempDir()
			pgid, ready := filepath.Join(dir, "pgid"), filepath.Join(dir, "ready")
			command := recordGroup(pgid) + "trap '' TERM; head -c 100000 /dev/zero; touch " + ready + "; sleep 300"
			f := newProviderFixture(t)
			f.inference = func(n int, _ *http.Request, _ []byte) (int, string, string) {
				if n == 0 {
					return toolReplyOf(t, "bash", map[string]any{"command": command})
				}
				return http.StatusOK, "text/event-stream", sseText("unreachable")
			}
			cmd := builtAsk(t, f, dir, []string{"TMPDIR=" + spillDir}, "-p", "--provider", "anthropic", "--model", "claude-sonnet-4-6", "run it")
			var stdout, stderr lockedBuffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			require.NoError(t, cmd.Start())
			exited := make(chan error, 1)
			go func() { exited <- cmd.Wait() }()
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			waitFile(t, ready)
			group := readPgid(t, pgid)
			t.Cleanup(func() { _ = syscall.Kill(-group, syscall.SIGKILL) })
			require.Eventually(t, func() bool {
				entries, err := os.ReadDir(spillDir)
				return err == nil && len(entries) > 0
			}, 5*time.Second, 10*time.Millisecond, "the command must spill output before the signal")
			require.NoError(t, cmd.Process.Signal(tc.signal))
			select {
			case err := <-exited:
				var ee *exec.ExitError
				require.ErrorAs(t, err, &ee)
				assert.Equal(t, tc.code, ee.ExitCode())
			case <-time.After(20 * time.Second):
				t.Fatal("ask did not exit after the signal")
			}
			assert.Eventually(t, func() bool { return groupGone(group) }, 10*time.Second, 20*time.Millisecond)
			entries, err := os.ReadDir(spillDir)
			require.NoError(t, err)
			assert.Empty(t, entries, "exit must remove the private output directory")
			assert.Empty(t, stdout.String())
		})
	}
}
