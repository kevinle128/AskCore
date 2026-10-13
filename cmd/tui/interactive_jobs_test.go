package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// jobScript makes the fixture model start one background job, say that it did,
// and answer the completion notice.
func jobScript(t *testing.T, f *providerFixture) {
	t.Helper()
	scriptFixture(t, f,
		call(t, "bash", map[string]any{"command": "sleep 3; echo built", "run_in_background": true}),
		say("started the build"),
		say("the build is done"),
	)
}

// withDefaultModel writes the fixture model as the default of the home.
func withDefaultModel(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"defaultModel":"`+anthropicModel+`"}`+"\n"), 0o600))
}

func fixtureEnv(f *providerFixture) []string {
	env := []string{"ANTHROPIC_API_KEY=fixture-anthropic-key"}
	for k, v := range f.env() {
		env = append(env, k+"="+v)
	}
	return env
}

// requireJobScreens drives one background job through the TUI and checks what
// the screen shows while the job runs and after its notice.
func requireJobScreens(t *testing.T, a *askTerm) {
	t.Helper()
	a.write("run the build in the background\r")
	a.waitText("started the build")
	a.waitText("┊ ◌ 1 background job · bash-1 sleep 3; echo built") // the status row, not the step row
	a.waitFor("the job line in the block pane", func(rows []string) bool {
		return hasRowPrefix(rows, "  ◆ job bash-1 ") && hasRow(rows, "◆ job bash-1", "completed · exit 0")
	})
	a.waitText("the build is done")
	a.waitFor("the job count to leave the rail", func(rows []string) bool {
		for _, r := range rows {
			if strings.HasPrefix(strings.TrimSpace(r), "┊") && strings.Contains(r, "background job") {
				return false
			}
		}
		return true
	})
	for _, needle := range []string{"system-reminder", "Background job", "❯ Background"} {
		require.NotContains(t, a.rawOutput(), needle, "the notice reached the screen")
	}
	require.False(t, strings.Contains(a.screen(), "⧗"), "the notice had a queue row")
}

func TestInteractiveBackgroundJobThroughTheLeader(t *testing.T) {
	bin, home := newE2EHome(t)
	f := newProviderFixture(t)
	jobScript(t, f)
	withDefaultModel(t, home)
	a := startAskTerm(t, bin, home, askTermOptions{}, fixtureEnv(f)...)
	a.waitText("Ask anything")
	requireLeaderHeader(t, a, bin, home)
	requireJobScreens(t, a)
	a.write("\x04")
	require.Equal(t, 0, a.waitExit(), a.stderr.String())
}

func TestInteractiveBackgroundJobInProcess(t *testing.T) {
	bin := acpBinary(t)
	// A socket path longer than the system limit: the leader cannot listen, so
	// the TUI starts its agent in process. The home itself stays safe.
	home := filepath.Join(leaderHome(t), strings.Repeat("h", 110))
	f := newProviderFixture(t)
	jobScript(t, f)
	withDefaultModel(t, home)
	a := startAskTerm(t, bin, home, askTermOptions{}, fixtureEnv(f)...)
	a.waitText("○ in-process")
	requireJobScreens(t, a)
	require.Empty(t, leaderProcesses(bin))
	a.write("\x03")
	a.write("\x03")
	require.Equal(t, 0, a.waitExit(), a.stderr.String())
}
