package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/jobs"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// gateLoop runs until the test creates the file "gate" in the working
// directory. It ends on SIGTERM, so the table can stop it.
const gateLoop = "while [ ! -e gate ]; do sleep 0.02; done"

// shellClock is a sandbox.Clock whose time moves only when a test calls
// Advance.
type shellClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*shellTimer
}

type shellTimer struct {
	at      time.Time
	f       func()
	stopped bool
	fired   bool
}

func newShellClock() *shellClock { return &shellClock{now: time.Unix(1_700_000_000, 0)} }

func (c *shellClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *shellClock) AfterFunc(d time.Duration, f func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &shellTimer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		if t.stopped || t.fired {
			return false
		}
		t.stopped = true
		return true
	}
}

// Pending returns the number of timers that are armed and have not fired.
func (c *shellClock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.timers {
		if !t.stopped && !t.fired {
			n++
		}
	}
	return n
}

// Advance moves time forward and runs the due timers in order, outside the
// lock.
func (c *shellClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*shellTimer
	for _, t := range c.timers {
		if !t.stopped && !t.fired && !t.at.After(c.now) {
			t.fired = true
			due = append(due, t)
		}
	}
	c.mu.Unlock()
	sort.SliceStable(due, func(i, j int) bool { return due[i].at.Before(due[j].at) })
	for _, t := range due {
		t.f()
	}
}

// shellNotices records the messages that the table sends to the Agent.
type shellNotices struct {
	mu    sync.Mutex
	texts []string
}

func (n *shellNotices) accept(m protocol.Message) (string, error) {
	um, ok := m.(protocol.UserMessage)
	if !ok {
		return "", fmt.Errorf("message is %T, want a user message", m)
	}
	var text string
	for _, b := range um.Content {
		if tb, ok := b.(protocol.Text); ok {
			text += tb.Text
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.texts = append(n.texts, text)
	return fmt.Sprintf("in-%d", len(n.texts)), nil
}

func (n *shellNotices) all() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.texts...)
}

// shellHarness runs the shell tools the way the coordinator does: registry
// preparation, then Execute, on a real job table and real processes.
type shellHarness struct {
	t       *testing.T
	table   *jobs.Table
	set     map[string]tools.Tool
	reg     *tools.Registry
	cwd     string
	clock   *shellClock
	notices *shellNotices

	mu    sync.Mutex
	pgids []int
	errs  []error
}

type shellOption func(*shellConfig)

type shellConfig struct {
	fakeClock bool
	maxActive int
}

func withFakeClock() shellOption { return func(c *shellConfig) { c.fakeClock = true } }

func withMaxActive(n int) shellOption { return func(c *shellConfig) { c.maxActive = n } }

// newShellHarness returns a harness whose cleanup closes the job table and
// checks that every process group it saw through track is gone.
func newShellHarness(t *testing.T, opts ...shellOption) *shellHarness {
	t.Helper()
	var cfg shellConfig
	for _, o := range opts {
		o(&cfg)
	}
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	h := &shellHarness{t: t, cwd: cwd, notices: &shellNotices{}, set: map[string]tools.Tool{}, reg: &tools.Registry{}}
	o := jobs.Options{MaxActive: cfg.maxActive, OnError: func(err error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.errs = append(h.errs, err)
	}}
	if cfg.fakeClock {
		h.clock = newShellClock()
		o.Clock = h.clock
	}
	h.table = jobs.New(o)
	h.table.SetNotifier(jobs.Notifier{FollowUp: h.notices.accept, Steer: h.notices.accept, Remove: func(string) bool { return false }})
	for _, tool := range tools.NewShellTools(h.table) {
		require.NoError(t, h.reg.Register(tool, tools.SourceInfo{Kind: tools.SourceBuiltin}))
		h.set[tool.Decl().Name] = tool
	}
	t.Cleanup(func() {
		assert.NoError(t, h.table.Close())
		h.mu.Lock()
		pgids := append([]int(nil), h.pgids...)
		h.mu.Unlock()
		for _, pgid := range pgids {
			requireShellGroupGone(t, pgid)
		}
	})
	return h
}

func requireShellGroupGone(t *testing.T, pgid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := syscall.Kill(-pgid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process group %d still exists: kill(-pgid, 0) = %v", pgid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (h *shellHarness) path(rel string) string { return filepath.Join(h.cwd, rel) }

// release lets every gateLoop of the test end.
func (h *shellHarness) release() {
	h.t.Helper()
	require.NoError(h.t, os.WriteFile(h.path("gate"), nil, 0o600))
}

// pgid waits for the pid file that a command wrote with "echo $$ > name" and
// returns the process group id. The shell is the leader of its group.
func (h *shellHarness) pgid(name string) int {
	h.t.Helper()
	var pid int
	require.Eventually(h.t, func() bool {
		raw, err := os.ReadFile(h.path(name))
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(raw)))
		return err == nil
	}, 5*time.Second, 5*time.Millisecond, "pid file %s", name)
	h.mu.Lock()
	h.pgids = append(h.pgids, pid)
	h.mu.Unlock()
	return pid
}

func (h *shellHarness) prepare(name string, args any) json.RawMessage {
	h.t.Helper()
	var raw json.RawMessage
	switch v := args.(type) {
	case string:
		raw = json.RawMessage(v)
	default:
		b, err := json.Marshal(v)
		require.NoError(h.t, err)
		raw = b
	}
	require.Contains(h.t, h.set, name)
	prepared, err := h.reg.Prepare(name, raw)
	require.NoError(h.t, err)
	return prepared
}

// runWith calls the named tool with a context and an update callback.
func (h *shellHarness) runWith(ctx context.Context, update func(protocol.ToolExecutionResult), name string, args any) (protocol.ToolExecutionResult, error) {
	h.t.Helper()
	prepared := h.prepare(name, args)
	return h.set[name].Execute(ctx, tools.Context{CallID: "c1", Cwd: h.cwd, Update: update}, prepared)
}

func (h *shellHarness) run(name string, args any) (protocol.ToolExecutionResult, error) {
	h.t.Helper()
	return h.runWith(context.Background(), nil, name, args)
}

func (h *shellHarness) mustRun(name string, args any) protocol.ToolExecutionResult {
	h.t.Helper()
	res, err := h.run(name, args)
	require.NoError(h.t, err)
	return res
}

// shellCall is a tool call that runs on its own goroutine.
type shellCall struct {
	done chan struct{}
	res  protocol.ToolExecutionResult
	err  error
}

func (h *shellHarness) start(ctx context.Context, name string, args any) *shellCall {
	h.t.Helper()
	prepared := h.prepare(name, args)
	c := &shellCall{done: make(chan struct{})}
	go func() {
		defer close(c.done)
		c.res, c.err = h.set[name].Execute(ctx, tools.Context{CallID: "c1", Cwd: h.cwd}, prepared)
	}()
	return c
}

func (c *shellCall) wait(t *testing.T) protocol.ToolExecutionResult {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(15 * time.Second):
		t.Fatal("tool call did not return")
	}
	require.NoError(t, c.err)
	return c.res
}

func (c *shellCall) finished() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// awaitTimer waits until the fake clock holds an armed timer, which is the
// wait budget of the call under test.
func (h *shellHarness) awaitTimer() {
	h.t.Helper()
	require.Eventually(h.t, func() bool { return h.clock.Pending() > 0 }, 5*time.Second, time.Millisecond)
}

func shellText(res protocol.ToolExecutionResult) string {
	var parts []string
	for _, b := range res.Content {
		if tb, ok := b.(protocol.Text); ok {
			parts = append(parts, tb.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func shellDetails(t *testing.T, res protocol.ToolExecutionResult) protocol.ToolResultDetails {
	t.Helper()
	var d protocol.ToolResultDetails
	require.NotEmpty(t, res.Details, "result has no details")
	require.NoError(t, json.Unmarshal(res.Details, &d))
	return d
}

func isError(res protocol.ToolExecutionResult) bool { return res.IsError != nil && *res.IsError }

func TestBashForegroundReportsExitAndOutput(t *testing.T) {
	h := newShellHarness(t)

	res := h.mustRun("bash", map[string]any{"command": "echo hi; exit 2"})

	assert.Equal(t, "exit: 2\nhi", shellText(res))
	assert.True(t, isError(res))
}

func TestBashExitZeroIsNotAnErrorAndKeepsStderr(t *testing.T) {
	h := newShellHarness(t)

	res := h.mustRun("bash", map[string]any{"command": "echo out; echo err >&2"})

	assert.False(t, isError(res))
	text := shellText(res)
	assert.True(t, strings.HasPrefix(text, "exit: 0\n"), text)
	assert.Contains(t, text, "out")
	assert.Contains(t, text, "err")
}

func TestBashEmptyOutputSaysSo(t *testing.T) {
	h := newShellHarness(t)

	res := h.mustRun("bash", map[string]any{"command": "true"})

	assert.Equal(t, "exit: 0\n(no output)", shellText(res))
	assert.False(t, isError(res))
}

func TestBashTimeoutMovesToBackground(t *testing.T) {
	h := newShellHarness(t, withFakeClock())
	call := h.start(context.Background(), "bash", map[string]any{"command": "echo started; : > ready; " + gateLoop + "; echo finished", "timeout": 1})
	h.awaitTimer()
	require.Eventually(t, func() bool { _, err := os.Stat(h.path("ready")); return err == nil }, 5*time.Second, 5*time.Millisecond)
	time.Sleep(100 * time.Millisecond) // the reader of the output pipe sees the line

	h.clock.Advance(time.Second)
	res := call.wait(t)

	text := shellText(res)
	assert.Contains(t, text, "Command still running after 1 s. It continues as background job bash-1. "+
		"A notice will arrive when it finishes; do not poll job_output. Use job_kill to stop it.")
	assert.Contains(t, text, "started")
	assert.False(t, isError(res))
	d := shellDetails(t, res)
	assert.Equal(t, "bash-1", d.JobID)
	assert.Equal(t, "running", d.Status)
	list := h.table.List()
	require.Len(t, list, 1)
	assert.Equal(t, jobs.StatusRunning, list[0].Status)

	// The job goes on: it ends once the gate opens, and the wait reports it.
	h.release()
	out := h.mustRun("job_output", map[string]any{"job_id": "bash-1", "wait": true, "timeout_ms": 5000})
	assert.Contains(t, shellText(out), "finished")
	assert.Contains(t, shellText(out), "[status: completed, exit code: 0]")
}

func TestBashTimeoutAbove600IsClamped(t *testing.T) {
	h := newShellHarness(t, withFakeClock())
	call := h.start(context.Background(), "bash", map[string]any{"command": gateLoop, "timeout": 900})
	h.awaitTimer()

	h.clock.Advance(599 * time.Second)
	time.Sleep(50 * time.Millisecond)
	require.False(t, call.finished(), "the budget is 600 s, not less")
	h.clock.Advance(time.Second)
	res := call.wait(t)

	text := shellText(res)
	assert.Contains(t, text, "Command still running after 600 s.")
	assert.Contains(t, text, "[timeout limited to 600 s]")
}

func TestBashRejectsNonPositiveTimeout(t *testing.T) {
	h := newShellHarness(t)
	for _, v := range []float64{0, -5} {
		_, err := h.run("bash", map[string]any{"command": "echo hi", "timeout": v})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")
	}
	assert.Empty(t, h.table.List())
}

func TestBashRunInBackgroundReturnsAtOnce(t *testing.T) {
	h := newShellHarness(t)

	begin := time.Now()
	res := h.mustRun("bash", map[string]any{"command": gateLoop, "run_in_background": true})
	elapsed := time.Since(begin)

	assert.Less(t, elapsed, 100*time.Millisecond)
	assert.Equal(t, "Started background job bash-1. A notice will arrive when it finishes; do not poll job_output.", shellText(res))
	assert.False(t, isError(res))
	d := shellDetails(t, res)
	assert.Equal(t, "bash-1", d.JobID)
	assert.Equal(t, "running", d.Status)
	list := h.table.List()
	require.Len(t, list, 1)
	assert.Equal(t, jobs.StatusRunning, list[0].Status)
}

func TestBashRejectsTrailingAmpersand(t *testing.T) {
	h := newShellHarness(t)
	for _, cmd := range []string{"sleep 5 &", "sleep 5 &  ", "echo a; sleep 5 &\n"} {
		_, err := h.run("bash", map[string]any{"command": cmd})
		require.Error(t, err, cmd)
		assert.Equal(t, "Do not end the command with &. Set run_in_background to true to run it in the background.", err.Error())
	}
	assert.Empty(t, h.table.List(), "a refused command starts no job")

	for command, want := range map[string]string{
		"echo a && echo b":    "a\nb",
		`echo "fish &"`:       "fish &",
		`echo 'fish &'`:       "fish &",
		`echo a\&`:            "a&",
		"echo a # run & wait": "a",
		"echo a 1>&2":         "a",
	} {
		res := h.mustRun("bash", map[string]any{"command": command})
		assert.False(t, isError(res), command)
		assert.Equal(t, "exit: 0\n"+want, shellText(res), command)
	}
}

func TestBashCancelledKillsJob(t *testing.T) {
	h := newShellHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	call := h.start(ctx, "bash", map[string]any{"command": "echo $$ > pid; echo partial; : > ready; while :; do sleep 0.05; done"})
	pgid := h.pgid("pid")
	require.Eventually(t, func() bool { _, err := os.Stat(h.path("ready")); return err == nil }, 5*time.Second, 5*time.Millisecond)

	cancel()
	res := call.wait(t)

	assert.True(t, isError(res))
	text := shellText(res)
	assert.True(t, strings.HasPrefix(text, "Command aborted"), text)
	assert.Contains(t, text, "partial")
	assert.ErrorIs(t, syscall.Kill(-pgid, 0), syscall.ESRCH, "the process group is gone when the body returns")
	assert.Empty(t, h.table.List())
}

func TestBashSendsThrottledPartialUpdates(t *testing.T) {
	h := newShellHarness(t)
	var mu sync.Mutex
	var updates []protocol.ToolExecutionResult
	update := func(r protocol.ToolExecutionResult) {
		mu.Lock()
		defer mu.Unlock()
		updates = append(updates, r)
	}
	command := "while [ ! -e stop ]; do echo tick; sleep 0.005; done"
	call := make(chan protocol.ToolExecutionResult, 1)
	begin := time.Now()
	go func() {
		res, err := h.runWith(context.Background(), update, "bash", map[string]any{"command": command})
		assert.NoError(t, err)
		call <- res
	}()
	time.Sleep(400 * time.Millisecond)
	require.NoError(t, os.WriteFile(h.path("stop"), nil, 0o600))
	var final protocol.ToolExecutionResult
	select {
	case final = <-call:
	case <-time.After(10 * time.Second):
		t.Fatal("bash did not return")
	}
	elapsed := time.Since(begin)

	mu.Lock()
	defer mu.Unlock()
	assert.GreaterOrEqual(t, len(updates), 3, "output that goes on produces updates")
	assert.LessOrEqual(t, len(updates), int(elapsed/(50*time.Millisecond))+1, "at most one update per 50 ms")
	for _, u := range updates {
		assert.Contains(t, shellText(u), "tick")
		assert.False(t, isError(u))
	}
	assert.True(t, strings.HasPrefix(shellText(final), "exit: 0\n"))
}

func TestBashSendsNoUpdateAfterMoveToBackground(t *testing.T) {
	h := newShellHarness(t, withFakeClock())
	var mu sync.Mutex
	var count int
	update := func(protocol.ToolExecutionResult) {
		mu.Lock()
		defer mu.Unlock()
		count++
	}
	command := "while :; do echo tick; sleep 0.01; done"
	done := make(chan protocol.ToolExecutionResult, 1)
	go func() {
		res, err := h.runWith(context.Background(), update, "bash", map[string]any{"command": command, "timeout": 1})
		assert.NoError(t, err)
		done <- res
	}()
	h.awaitTimer()
	time.Sleep(150 * time.Millisecond)
	h.clock.Advance(time.Second)
	res := <-done
	assert.Contains(t, shellText(res), "continues as background job bash-1")

	mu.Lock()
	after := count
	mu.Unlock()
	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, after, count, "no update after the call returned")
	assert.Positive(t, after)
}

func TestBashBigOutputKeepsHeadTailAndSpill(t *testing.T) {
	h := newShellHarness(t)
	var want strings.Builder
	for i := 1; i <= 60000; i++ {
		fmt.Fprintf(&want, "%d\n", i)
	}

	res := h.mustRun("bash", map[string]any{"command": "seq 1 60000"})

	text := shellText(res)
	assert.True(t, strings.HasPrefix(text, "exit: 0\n1\n2\n3\n"), text[:40])
	assert.Contains(t, text, "bytes elided")
	assert.Contains(t, text, "\n60000\n[full output: ")
	assert.Less(t, len(text), 60*1024)
	d := shellDetails(t, res)
	require.NotEmpty(t, d.SpillPath)
	assert.Contains(t, text, "[full output: "+d.SpillPath+"]")
	require.NotNil(t, d.Truncation)
	assert.Equal(t, want.Len(), d.Truncation.TotalBytes)
	assert.Positive(t, d.Truncation.OutputBytes)
	info, err := os.Stat(d.SpillPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	spilled, err := os.ReadFile(d.SpillPath)
	require.NoError(t, err)
	assert.Equal(t, want.String(), string(spilled))
}

func TestBashDetailsCarryJobAndExit(t *testing.T) {
	h := newShellHarness(t)

	res := h.mustRun("bash", map[string]any{"command": "echo hi; exit 3"})

	var raw map[string]any
	require.NoError(t, json.Unmarshal(res.Details, &raw))
	assert.Equal(t, "execute", raw["kind"])
	assert.EqualValues(t, 3, raw["exitCode"])
	assert.Equal(t, "bash-1", raw["jobId"])
	assert.Equal(t, "completed", raw["status"])
	assert.NotContains(t, raw, "signal")
	assert.NotContains(t, raw, "spillPath")
}

func TestBashSignalIsReportedInHeaderAndDetails(t *testing.T) {
	h := newShellHarness(t)

	res := h.mustRun("bash", map[string]any{"command": "echo before; kill -KILL $$"})

	assert.True(t, strings.HasPrefix(shellText(res), "signal: SIGKILL\n"), shellText(res))
	assert.True(t, isError(res))
	d := shellDetails(t, res)
	assert.Equal(t, "SIGKILL", d.Signal)
	assert.Nil(t, d.ExitCode)
}

func TestBashLeadingCdIsNotedInDetailsOnly(t *testing.T) {
	h := newShellHarness(t)
	require.NoError(t, os.Mkdir(h.path("sub"), 0o700))

	res := h.mustRun("bash", map[string]any{"command": "cd sub && pwd"})

	assert.True(t, strings.HasSuffix(shellText(res), "/sub"), shellText(res))
	assert.Equal(t, "sub", shellDetails(t, res).Cwd)
}

func TestBashRefusesWhenTooManyJobs(t *testing.T) {
	h := newShellHarness(t, withMaxActive(1))
	h.mustRun("bash", map[string]any{"command": gateLoop, "run_in_background": true})

	_, err := h.run("bash", map[string]any{"command": "echo hi"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Too many active jobs")
	assert.Contains(t, err.Error(), "job_kill")
}

func TestBashRejectsEmptyCommand(t *testing.T) {
	h := newShellHarness(t)
	_, err := h.run("bash", map[string]any{"command": "  \n"})
	require.Error(t, err)
	assert.Empty(t, h.table.List())
}

func TestBashDeclaresItsContract(t *testing.T) {
	h := newShellHarness(t)
	decl := h.set["bash"].Decl()

	assert.Equal(t, "bash", decl.Name)
	for _, want := range []string{"120", "600", "run_in_background", "do not poll", "job_kill", "session"} {
		assert.Contains(t, decl.Description, want)
	}
	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(decl.Parameters, &schema))
	assert.Equal(t, []string{"command"}, schema.Required)
	assert.Contains(t, schema.Properties, "timeout")
	assert.Contains(t, schema.Properties, "run_in_background")
	assert.Regexp(t, regexp.MustCompile(`\S`), h.set["bash"].(tools.PromptContributor).PromptSnippet())
}
