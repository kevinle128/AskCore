package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/acp"
	"AskCore/internal/app"
	"AskCore/internal/providers"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

// canonicalTempDir returns a fresh folder whose path has no symbolic link, so
// the cwd that a child process reports matches the one the test expects.
func canonicalTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

// loggedSession is one session file of a test home, decoded.
type loggedSession struct {
	path    string
	header  sessions.Header
	entries []sessions.Entry
}

// sessionFilesOf returns every session file below home, decoded. It stops the
// test on a file that does not decode, so call it only on a file that no
// process holds open for writing.
func sessionFilesOf(t *testing.T, home string) []loggedSession {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(home, "sessions", "*", "*.jsonl"))
	require.NoError(t, err)
	out := make([]loggedSession, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		header, err := sessions.DecodeHeader([]byte(lines[0]))
		require.NoError(t, err, path)
		s := loggedSession{path: path, header: header}
		for _, line := range lines[1:] {
			_, e, err := sessions.DecodeEntry([]byte(line))
			require.NoError(t, err, "%s: %q", path, line)
			s.entries = append(s.entries, e)
		}
		out = append(out, s)
	}
	return out
}

// onlySession returns the one session file of home.
func onlySession(t *testing.T, home string) loggedSession {
	t.Helper()
	files := sessionFilesOf(t, home)
	require.Len(t, files, 1)
	return files[0]
}

func (s loggedSession) userTexts() []string {
	var out []string
	for _, e := range s.entries {
		me, ok := e.(sessions.MessageEntry)
		if !ok {
			continue
		}
		if u, ok := me.Message.(protocol.UserMessage); ok {
			for _, b := range u.Content {
				if txt, ok := b.(protocol.Text); ok {
					out = append(out, txt.Text)
				}
			}
		}
	}
	return out
}

func (s loggedSession) requestDeltas() []sessions.RequestDelta {
	var out []sessions.RequestDelta
	for _, e := range s.entries {
		if d, ok := e.(sessions.RequestDelta); ok {
			out = append(out, d)
		}
	}
	return out
}

func TestPrintSessionFlagConflicts(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		want string
	}{
		{"continue with session", []string{"-c", "--session", "1", "-p", "x"}, "Error: --continue cannot be used with --session\n"},
		{"long continue with session", []string{"--continue", "--session", "1", "-p", "x"}, "Error: --continue cannot be used with --session\n"},
		{"continue with no-session", []string{"-c", "--no-session", "-p", "x"}, "Error: --no-session cannot be used with --continue\n"},
		{"session with no-session", []string{"--session", "1", "--no-session", "-p", "x"}, "Error: --no-session cannot be used with --session\n"},
		{"session without value", []string{"--session"}, "Error: --session requires a value\n"},
		{"session with empty value", []string{"--session", "", "-p", "x"}, "Error: --session requires a session id\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runCLI("", tc.argv...)

			assert.Equal(t, result{"", tc.want, 1}, got)
		})
	}
}

func TestPrintContinueWithoutSessionStartsNewOne(t *testing.T) {
	dir, home := canonicalTempDir(t), privateHome(t)

	got := printWithDefault(t, dir, home, &scriptedModel{}, nil, "-p", "-c", "hi")

	require.Equal(t, result{"hi\n", "", 0}, got)
	assert.Equal(t, []string{"hi"}, onlySession(t, home).userTexts(), "the new session is saved")
}

// A session that was stored and never answered is not a session to continue.
func TestPrintContinueSkipsSessionsWithoutReply(t *testing.T) {
	dir, home := canonicalTempDir(t), privateHome(t)
	require.Equal(t, 0, printWithDefault(t, dir, home, &scriptedModel{}, nil, "-p", "used").code)
	storeUnusedSession(t, home, dir)
	require.Len(t, sessionFilesOf(t, home), 2)

	got := printWithDefault(t, dir, home, &scriptedModel{}, nil, "-p", "-c", "again")

	require.Equal(t, result{"again\n", "", 0}, got)
	var used []string
	for _, s := range sessionFilesOf(t, home) {
		if len(s.userTexts()) > 0 {
			used = s.userTexts()
		}
	}
	assert.Equal(t, []string{"used", "again"}, used, "the older session with a reply is continued")
	assert.Len(t, sessionFilesOf(t, home), 2, "no new session is made")
}

func TestPrintContinueWithOnlyUnusedSessionsStartsNewOne(t *testing.T) {
	dir, home := canonicalTempDir(t), privateHome(t)
	storeUnusedSession(t, home, dir)

	got := printWithDefault(t, dir, home, &scriptedModel{}, nil, "-p", "-c", "hi")

	require.Equal(t, result{"hi\n", "", 0}, got)
	files := sessionFilesOf(t, home)
	require.Len(t, files, 2)
	var withPrompt int
	for _, s := range files {
		if len(s.userTexts()) == 1 && s.userTexts()[0] == "hi" {
			withPrompt++
		}
	}
	assert.Equal(t, 1, withPrompt, "the prompt went to a new session")
}

// storeUnusedSession stores a session that has a header and no reply, as a
// start of the TUI does, and closes it.
func storeUnusedSession(t *testing.T, home, cwd string) {
	t.Helper()
	log, err := app.NewSessionLog(home)
	require.NoError(t, err)
	w, err := log.NewContext(app.SessionLogOptions{CreateNow: true}, sessions.NewID(), cwd)()
	require.NoError(t, err)
	require.NoError(t, w.Close())
}

func TestPrintSessionFlagUnknownIDFails(t *testing.T) {
	dir, home := canonicalTempDir(t), privateHome(t)

	got := printWithDefault(t, dir, home, &scriptedModel{}, nil, "-p", "--session", "123", "hi")

	assert.Equal(t, result{"", "Error: no session 123 in " + dir + "\n", 1}, got)
}

func TestPrintSavesSessionByDefault(t *testing.T) {
	dir, home := canonicalTempDir(t), privateHome(t)

	got := printWithDefault(t, dir, home, &scriptedModel{}, nil, "-p", "hello")

	require.Equal(t, result{"hello\n", "", 0}, got)
	s := onlySession(t, home)
	assert.Equal(t, dir, s.header.Cwd)
	assert.Regexp(t, `^\d{19}$`, s.header.ID)
	assert.Equal(t, []string{"hello"}, s.userTexts())
	assert.Equal(t, s.header.ID+".jsonl", filepath.Base(s.path))
}

func TestPrintNoSessionWritesNoFile(t *testing.T) {
	dir, home := canonicalTempDir(t), privateHome(t)

	got := printWithDefault(t, dir, home, &scriptedModel{}, nil, "-p", "--no-session", "hello")

	assert.Equal(t, result{"hello\n", "", 0}, got)
	var files []string
	require.NoError(t, filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.Contains(path, string(filepath.Separator)+"sessions"+string(filepath.Separator)) {
			files = append(files, path)
		}
		return err
	}))
	assert.Empty(t, files)
}

func TestPrintContinuePicksNewestSession(t *testing.T) {
	dir, home := canonicalTempDir(t), privateHome(t)
	for _, text := range []string{"first", "second"} {
		require.Equal(t, 0, printWithDefault(t, dir, home, &scriptedModel{}, nil, "-p", text).code)
	}
	files := sessionFilesOf(t, home)
	require.Len(t, files, 2)

	got := printWithDefault(t, dir, home, &scriptedModel{}, nil, "-p", "-c", "third")

	require.Equal(t, result{"third\n", "", 0}, got)
	byText := map[string][]string{}
	for _, s := range sessionFilesOf(t, home) {
		byText[s.userTexts()[0]] = s.userTexts()
	}
	assert.Equal(t, []string{"first"}, byText["first"], "the older session stays as it was")
	assert.Equal(t, []string{"second", "third"}, byText["second"], "the newest session gets the new prompt")
}

func TestPrintSessionFlagPicksSession(t *testing.T) {
	dir, home := canonicalTempDir(t), privateHome(t)
	for _, text := range []string{"first", "second"} {
		require.Equal(t, 0, printWithDefault(t, dir, home, &scriptedModel{}, nil, "-p", text).code)
	}
	var firstID string
	for _, s := range sessionFilesOf(t, home) {
		if s.userTexts()[0] == "first" {
			firstID = s.header.ID
		}
	}
	require.NotEmpty(t, firstID)

	got := printWithDefault(t, dir, home, &scriptedModel{}, nil, "-p", "--session", firstID, "again")

	require.Equal(t, result{"again\n", "", 0}, got)
	for _, s := range sessionFilesOf(t, home) {
		if s.header.ID == firstID {
			assert.Equal(t, []string{"first", "again"}, s.userTexts())
		} else {
			assert.Equal(t, []string{"second"}, s.userTexts())
		}
	}
}

func TestPrintJSONModeStartsWithSessionHeader(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra []string
		saved bool
	}{
		{"saved session", nil, true},
		{"no session", []string{"--no-session"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, home := canonicalTempDir(t), privateHome(t)

			got := printWithDefault(t, dir, home, &scriptedModel{}, nil, append([]string{"--mode", "json"}, append(tc.extra, "hello")...)...)

			require.Equal(t, 0, got.code, got.stderr)
			lines := strings.Split(strings.TrimSuffix(got.stdout, "\n"), "\n")
			var raw map[string]any
			require.NoError(t, json.Unmarshal([]byte(lines[0]), &raw))
			assert.Equal(t, "session", raw["type"])
			header, err := sessions.DecodeHeader([]byte(lines[0]))
			require.NoError(t, err)
			assert.Equal(t, sessions.FormatVersion, header.Version)
			assert.Equal(t, dir, header.Cwd)
			assert.Regexp(t, `^\d{19}$`, header.ID)
			assert.WithinDuration(t, time.Now(), header.CreatedAt, time.Minute)
			assert.Contains(t, lines[1], `"type":"agent_start"`, "the first event follows the header")
			assert.Contains(t, lines[1], `"sessionId":"`+header.ID+`"`, "events carry the id of the header")
			if tc.saved {
				s := onlySession(t, home)
				assert.Equal(t, header.ID, s.header.ID)
				assert.Equal(t, header.CreatedAt.UTC().Truncate(time.Second), s.header.CreatedAt.UTC().Truncate(time.Second))
			} else {
				assert.Empty(t, sessionFilesOf(t, home))
			}
		})
	}
}

func TestSessionFileHoldsNoCredential(t *testing.T) {
	const key = "sk-ant-sentinel-session-key-8812"
	dir, home := canonicalTempDir(t), privateHome(t)

	got := printWithDefault(t, dir, home, &scriptedModel{answers: []string{sseText("ok")}},
		map[string]string{"ANTHROPIC_API_KEY": key}, "-p", "--provider", "anthropic", "--model", "claude-sonnet-4-6", "hello")
	require.Equal(t, result{"ok\n", "", 0}, got)
	got = printWithDefault(t, dir, home, &scriptedModel{answers: []string{sseText("ok")}}, nil,
		"-p", "-c", "--provider", "anthropic", "--model", "claude-sonnet-4-6", "--api-key", key, "again")
	require.Equal(t, 0, got.code, got.stderr)

	data, err := os.ReadFile(onlySession(t, home).path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), key)
	assert.NotContains(t, got.stdout+got.stderr, key)
}

// logAnthropicTurn runs one print-mode turn on claude-sonnet-4-6 in dir and
// returns the home that holds its session.
func logAnthropicTurn(t *testing.T, dir, home string) {
	t.Helper()
	got := printWithDefault(t, dir, home, &scriptedModel{answers: []string{sseText("ok")}}, anthropicKey,
		"-p", "--provider", "anthropic", "--model", "claude-sonnet-4-6", "--thinking", "high", "first")
	require.Equal(t, result{"ok\n", "", 0}, got)
}

func TestResumeKeepsModelAndThinkingWithoutFlags(t *testing.T) {
	dir, home := canonicalTempDir(t), privateHome(t)
	logAnthropicTurn(t, dir, home)
	model := &scriptedModel{answers: []string{sseText("again")}}

	got := printWithDefault(t, dir, home, model, anthropicKey, "-p", "-c", "second")

	require.Equal(t, result{"again\n", "", 0}, got)
	deltas := onlySession(t, home).requestDeltas()
	require.Len(t, deltas, 2)
	assert.Equal(t, deltas[0].Model, deltas[1].Model)
	assert.Equal(t, protocol.ThinkingHigh, deltas[1].Options.Reasoning)
	assert.Equal(t, "claude-sonnet-4-6", model.body(t, 0)["model"])
}

func TestExplicitModelBeatsRestoredModel(t *testing.T) {
	dir, home := canonicalTempDir(t), privateHome(t)
	logAnthropicTurn(t, dir, home)
	model := &scriptedModel{}

	got := printWithDefault(t, dir, home, model, anthropicKey, "-p", "-c", "--provider", "faux", "second")

	assert.Equal(t, result{"second\n", "", 0}, got)
	assert.Zero(t, model.requests(), "the explicit provider replaced the restored model")
}

func TestRestoredModelBeatsSavedDefault(t *testing.T) {
	dir := canonicalTempDir(t)
	home := homeWithDefault(t, acp.ModelID(providers.AnthropicSonnet46()))
	// Faux is the logged model; a start without a session would use the saved
	// default, which is usable here.
	require.Equal(t, 0, printWithDefault(t, dir, home, &scriptedModel{}, anthropicKey, "-p", "--provider", "faux", "first").code)
	model := &scriptedModel{}

	got := printWithDefault(t, dir, home, model, anthropicKey, "-p", "-c", "second")

	assert.Equal(t, result{"second\n", "", 0}, got)
	assert.Zero(t, model.requests(), "the saved default did not replace the logged model")
}

func TestResumeRefusesChangedAuthMethod(t *testing.T) {
	dir, home := canonicalTempDir(t), privateHome(t)
	logAnthropicTurn(t, dir, home)
	path := onlySession(t, home).path
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), `"method":"api-key"`)
	// The log now says that the old run used a login that this machine lacks.
	changed := strings.ReplaceAll(string(data), `"method":"api-key"`, `"method":"anthropic-oauth"`)
	require.NoError(t, os.WriteFile(path, []byte(changed), 0o600))
	model := &scriptedModel{}

	got := printWithDefault(t, dir, home, model, anthropicKey, "-p", "-c", "second")

	assert.Equal(t, 1, got.code)
	assert.Empty(t, got.stdout)
	assert.Contains(t, got.stderr, "anthropic-oauth")
	assert.Contains(t, got.stderr, "--model")
	assert.Zero(t, model.requests())
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, changed, string(after), "a refused resume writes nothing")

	// The user can still pick a model, which ends the refusal.
	got = printWithDefault(t, dir, home, &scriptedModel{answers: []string{sseText("picked")}}, anthropicKey,
		"-p", "-c", "--model", "anthropic/claude-sonnet-4-6", "third")
	assert.Equal(t, result{"picked\n", "", 0}, got)
}

// ---- built binary ----

// askRun is one running ask process whose stdout the test reads line by line.
type askRun struct {
	cmd    *exec.Cmd
	lines  chan string
	stderr *lockedBuffer
	exited chan error
}

// startAsk starts the built ask against the fixture, in dir, on the Ask home
// home. The caller kills it; cleanup kills it again when the test fails early.
func startAsk(t *testing.T, f *providerFixture, dir, home string, extraEnv []string, args ...string) *askRun {
	t.Helper()
	cmd := builtAsk(t, f, dir, append([]string{"ASK_HOME=" + home}, extraEnv...), args...)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	r := &askRun{cmd: cmd, lines: make(chan string, 4096), stderr: &lockedBuffer{}, exited: make(chan error, 1)}
	cmd.Stderr = r.stderr
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	go func() {
		rd := bufio.NewReaderSize(stdout, 1<<20)
		for {
			line, err := rd.ReadString('\n')
			if line != "" {
				r.lines <- line
			}
			if err != nil {
				close(r.lines)
				return
			}
		}
	}()
	go func() { r.exited <- cmd.Wait() }()
	return r
}

// waitLine returns the first line that holds substr, or fails the test.
func (r *askRun) waitLine(t *testing.T, substr string) string {
	t.Helper()
	timeout := time.After(30 * time.Second)
	for {
		select {
		case line, ok := <-r.lines:
			if !ok {
				t.Fatalf("ask ended before a line with %q\nstderr: %s", substr, r.stderr.String())
			}
			if strings.Contains(line, substr) {
				return line
			}
		case <-timeout:
			t.Fatalf("no line with %q\nstderr: %s", substr, r.stderr.String())
		}
	}
}

// kill ends the process with SIGKILL, the way a crash or the OOM killer does,
// and waits for it.
func (r *askRun) kill(t *testing.T) {
	t.Helper()
	require.NoError(t, r.cmd.Process.Signal(syscall.SIGKILL))
	select {
	case <-r.exited:
	case <-time.After(10 * time.Second):
		t.Fatal("ask did not end after SIGKILL")
	}
}

// sleepingTool returns the bash command that a fixture answer calls to hold a
// turn open, and a wait that returns once the command runs. The command records
// its pid and the test kills it when it ends, so no sleep outlives the test.
// The pid file is written whole, so the wait and the cleanup never read half
// of it.
func sleepingTool(t *testing.T) (string, func()) {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "pid")
	readPid := func() (int, bool) {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return 0, false
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		return pid, err == nil
	}
	t.Cleanup(func() {
		if pid, ok := readPid(); ok {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	wait := func() {
		t.Helper()
		require.Eventually(t, func() bool { _, ok := readPid(); return ok }, 30*time.Second, 10*time.Millisecond, "the tool command did not start")
	}
	return "echo $$ > " + pidFile + ".tmp && mv " + pidFile + ".tmp " + pidFile + "; exec sleep 100", wait
}

type bodyLog struct {
	mu     sync.Mutex
	bodies [][]byte
}

func (b *bodyLog) add(body []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.bodies = append(b.bodies, bytes.Clone(body))
}

func (b *bodyLog) get(t *testing.T, i int) map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	require.Greater(t, len(b.bodies), i)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b.bodies[i], &m))
	return m
}

// TestPrintResumeAfterKillKeepsModelAndThinking is the end-to-end check of
// the session log: a real ask process is killed inside an open turn, and a new
// process continues the session on the same model and thinking level.
func TestPrintResumeAfterKillKeepsModelAndThinking(t *testing.T) {
	dir, home := canonicalTempDir(t), privateHome(t)
	command, toolStarted := sleepingTool(t)
	var seen bodyLog
	f := newProviderFixture(t)
	f.inference = func(n int, _ *http.Request, body []byte) (int, string, string) {
		seen.add(body)
		switch n {
		case 0:
			return http.StatusOK, "text/event-stream", sseText("one")
		case 1:
			return toolReplyOf(t, "bash", map[string]any{"command": command})
		default:
			return http.StatusOK, "text/event-stream", sseText("resumed")
		}
	}
	first := startAsk(t, f, dir, home, nil, "--mode", "json", "--provider", "anthropic", "--model", "claude-sonnet-4-6", "--thinking", "high", "first", "second")
	var header sessions.Header
	require.NoError(t, json.Unmarshal([]byte(first.waitLine(t, `"type":"session"`)), &header))
	first.waitLine(t, `"type":"tool_execution_start"`)
	toolStarted()
	first.kill(t)

	second := builtAsk(t, f, dir, []string{"ASK_HOME=" + home}, "-p", "-c", "next")
	var stdout, stderr bytes.Buffer
	second.Stdout, second.Stderr = &stdout, &stderr
	require.NoError(t, second.Run(), "stderr: %s", stderr.String())

	assert.Equal(t, "resumed\n", stdout.String())
	s := onlySession(t, home)
	assert.Equal(t, header.ID, s.header.ID, "the new process continued the killed session")
	deltas := s.requestDeltas()
	require.Len(t, deltas, 3)
	assert.Equal(t, "claude-sonnet-4-6", deltas[0].Model.ID)
	assert.Equal(t, protocol.ThinkingHigh, deltas[0].Options.Reasoning)
	assert.Equal(t, deltas[0].Model, deltas[2].Model)
	assert.Equal(t, deltas[0].Options.Reasoning, deltas[2].Options.Reasoning)
	assert.Equal(t, seen.get(t, 0)["model"], seen.get(t, 2)["model"])
	assert.NotNil(t, seen.get(t, 2)["thinking"])
	assert.Equal(t, seen.get(t, 0)["thinking"], seen.get(t, 2)["thinking"])
	assert.Contains(t, s.entries, sessions.Entry(sessions.CycleClosed{CycleID: cycleOfOpenTurn(t, s), Reason: "aborted", Cause: "interrupted"}),
		"the repair closed the cut turn")
	assert.Equal(t, []string{"first", "second", "next"}, s.userTexts())
}

// cycleOfOpenTurn returns the id of the second cycle of s, the one that the
// kill cut.
func cycleOfOpenTurn(t *testing.T, s loggedSession) string {
	t.Helper()
	var ids []string
	for _, e := range s.entries {
		if c, ok := e.(sessions.CycleOpened); ok {
			ids = append(ids, c.CycleID)
		}
	}
	require.GreaterOrEqual(t, len(ids), 2)
	return ids[1]
}

func TestPrintSecondWriterFailsAtOnce(t *testing.T) {
	dir, home := canonicalTempDir(t), privateHome(t)
	command, toolStarted := sleepingTool(t)
	f := newProviderFixture(t)
	f.inference = func(_ int, _ *http.Request, _ []byte) (int, string, string) {
		return toolReplyOf(t, "bash", map[string]any{"command": command})
	}
	holder := startAsk(t, f, dir, home, nil, "--mode", "json", "--provider", "anthropic", "--model", "claude-sonnet-4-6", "hold")
	var header sessions.Header
	require.NoError(t, json.Unmarshal([]byte(holder.waitLine(t, `"type":"session"`)), &header))
	holder.waitLine(t, `"type":"tool_execution_start"`)
	toolStarted()

	other := builtAsk(t, f, dir, []string{"ASK_HOME=" + home}, "-p", "--session", header.ID, "me too")
	var stdout, stderr bytes.Buffer
	other.Stdout, other.Stderr = &stdout, &stderr
	started := time.Now()
	err := other.Run()

	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee)
	assert.Equal(t, 1, ee.ExitCode())
	assert.Less(t, time.Since(started), 10*time.Second, "a held session fails at once")
	assert.Regexp(t, regexp.MustCompile(`session `+regexp.QuoteMeta(header.ID)+` is open in another ask process`), stderr.String())
	assert.Contains(t, stderr.String(), "a leader keeps it after the TUI quits")
	assert.Contains(t, stderr.String(), "`ask -c`")
	assert.Contains(t, stderr.String(), "`ask leader stop`")
	assert.Empty(t, stdout.String())

	holder.kill(t)
	// The kernel drops the lock with the process, even while the tool child of
	// the holder lives on, so the session opens at once.
	again := builtAsk(t, f, dir, []string{"ASK_HOME=" + home}, "-p", "--session", header.ID, "--provider", "faux", "after the crash")
	out, err := again.Output()
	require.NoError(t, err)
	assert.Equal(t, "after the crash\n", string(out))
}

func TestHeadlessCredentialReplacementReachesNextRequest(t *testing.T) {
	dir, home := canonicalTempDir(t), privateHome(t)
	f := newProviderFixture(t)
	f.inference = func(int, *http.Request, []byte) (int, string, string) {
		return http.StatusOK, "text/event-stream", sseText("fine")
	}
	run := func(key string, args ...string) {
		cmd := builtAsk(t, f, dir, []string{"ASK_HOME=" + home, "ANTHROPIC_API_KEY=" + key}, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		cmd.Stdout = io.Discard
		require.NoError(t, cmd.Run(), "stderr: %s", stderr.String())
	}
	run("key-one-4410", "-p", "--provider", "anthropic", "--model", "claude-sonnet-4-6", "first")

	run("key-two-4410", "-p", "-c", "second")

	f.mu.Lock()
	defer f.mu.Unlock()
	require.Len(t, f.authHeaders, 2)
	assert.Contains(t, f.authHeaders[0], "key-one-4410")
	assert.Contains(t, f.authHeaders[1], "key-two-4410", "the resumed session uses the credential of the day, not a logged one")
	data, err := os.ReadFile(onlySession(t, home).path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "key-one-4410")
	assert.NotContains(t, string(data), "key-two-4410")
}

func TestInteractiveLoadIDResolvesContinue(t *testing.T) {
	ctx := context.Background()
	dir, home := canonicalTempDir(t), privateHome(t)

	id, err := interactiveLoadID(ctx, options{continueLast: true}, home, dir)
	require.NoError(t, err)
	assert.Empty(t, id, "an empty folder starts a new session")

	require.Equal(t, 0, printWithDefault(t, dir, home, &scriptedModel{}, nil, "-p", "used").code)
	used := onlySession(t, home).header.ID
	storeUnusedSession(t, home, dir)

	id, err = interactiveLoadID(ctx, options{continueLast: true}, home, dir)
	require.NoError(t, err)
	assert.Equal(t, used, id, "a newer session with no reply is skipped")

	id, err = interactiveLoadID(ctx, options{sessionID: "42"}, home, dir)
	require.NoError(t, err)
	assert.Equal(t, "42", id, "an id that the user named is not checked here")

	id, err = interactiveLoadID(ctx, options{}, home, dir)
	require.NoError(t, err)
	assert.Empty(t, id)
}
