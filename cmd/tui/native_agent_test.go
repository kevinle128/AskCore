package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/jobs"
	"AskCore/internal/providers/anthropic"
	"AskCore/pkg/protocol"
)

// scriptedModel answers the Anthropic Messages requests of an agent in order
// and keeps the request bodies.
type scriptedModel struct {
	mu      sync.Mutex
	answers []string
	bodies  [][]byte
}

func (s *scriptedModel) RoundTrip(r *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	n := len(s.bodies)
	s.bodies = append(s.bodies, body)
	answer := sseText("done")
	if n < len(s.answers) {
		answer = s.answers[n]
	}
	s.mu.Unlock()
	return sseResponse(200, answer)(r)
}

func (s *scriptedModel) body(t *testing.T, i int) map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.Greater(t, len(s.bodies), i, "request %d was not sent", i)
	var m map[string]any
	require.NoError(t, json.Unmarshal(s.bodies[i], &m))
	return m
}

// toolUseStream is an Anthropic stream that calls one tool with input.
func toolUseStream(t *testing.T, name string, input map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	partial, err := json.Marshal(string(raw))
	require.NoError(t, err)
	s := sseText("x")
	start := strings.Index(s, "event:content_block_start")
	end := strings.Index(s, "event:message_delta")
	block := `event:content_block_start
data:{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"` + name + `","input":{}}}

event:content_block_delta
data:{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":` + string(partial) + `}}

event:content_block_stop
data:{"type":"content_block_stop","index":0}

`
	return strings.Replace(s[:start]+block+s[end:], "end_turn", "tool_use", 1)
}

func nativeEnv(extra map[string]string) func(string) string {
	return func(k string) string {
		if k == "ASK_ALIBABA_TOKEN_PLAN_API_KEY" {
			return "fixture-key"
		}
		return extra[k]
	}
}

func tokenPlanOptions(rt http.RoundTripper) options {
	return options{provider: anthropic.ProviderID, model: anthropic.ModelID, transport: rt}
}

// newTestSession builds one native agent on the scripted model and closes it
// at the end of the test.
func newTestSession(t *testing.T, o options, getenv func(string) string) (*agent.Agent, *jobs.Table) {
	t.Helper()
	ag, jt, err := newNativeAgent(o, getenv, nil, newSessionID(), t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = ag.Dispose() })
	return ag, jt
}

func promptOnce(t *testing.T, ag *agent.Agent, text string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, ag.Prompt(ctx, textUser(text)))
	require.NoError(t, ag.WaitForIdle(ctx))
}

func TestNativeAgentRegistersSessionTools(t *testing.T) {
	model := &scriptedModel{}
	ag, _ := newTestSession(t, tokenPlanOptions(model), nativeEnv(nil))

	promptOnce(t, ag, "hi")

	var names []string
	for _, decl := range model.body(t, 0)["tools"].([]any) {
		names = append(names, decl.(map[string]any)["name"].(string))
	}
	slices.Sort(names)
	assert.Equal(t, []string{"bash", "echo", "edit", "find", "grep", "job_kill", "job_list", "job_output", "ls", "monitor", "read", "write"}, names)
}

func TestNativeAgentKeepsExtraTools(t *testing.T) {
	model := &scriptedModel{}
	o := tokenPlanOptions(model)
	extra := &stuckTool{started: make(chan struct{}), release: make(chan struct{})}
	ag, jt, err := newNativeAgent(o, nativeEnv(nil), nil, newSessionID(), t.TempDir(), extra)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ag.Dispose() })
	require.NotNil(t, jt)

	promptOnce(t, ag, "hi")

	var names []string
	for _, decl := range model.body(t, 0)["tools"].([]any) {
		names = append(names, decl.(map[string]any)["name"].(string))
	}
	assert.Contains(t, names, "stuck")
	assert.Contains(t, names, "bash")
}

// systemText returns the text of the system field of an Anthropic request.
func systemText(t *testing.T, body map[string]any) string {
	t.Helper()
	switch v := body["system"].(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, blk := range v {
			if m, ok := blk.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					b.WriteString(s)
					b.WriteString("\n")
				}
			}
		}
		return b.String()
	}
	return ""
}

func TestNativeAgentSetsToolPromptSection(t *testing.T) {
	model := &scriptedModel{}
	ag, _ := newTestSession(t, tokenPlanOptions(model), nativeEnv(nil))

	promptOnce(t, ag, "hi")

	system := systemText(t, model.body(t, 0))
	assert.Contains(t, system, "Available tools:")
	for _, name := range []string{"read", "write", "edit", "ls", "grep", "find", "bash", "job_output", "job_kill", "job_list", "monitor"} {
		assert.Contains(t, system, "\n- "+name+": ", "snippet of %s", name)
	}
	assert.NotContains(t, system, "\n- echo: ", "a tool without a snippet is not listed")
}

// guardProbe runs one bash call that the guard blocks when it is on and that
// fails harmlessly (the command does not exist) when it is off. It returns the
// text of the tool result in the second request.
func guardProbe(t *testing.T, env map[string]string) string {
	t.Helper()
	model := &scriptedModel{answers: []string{
		toolUseStream(t, "bash", map[string]any{"command": "mkfs.ask-no-such-tool /dev/null"}),
		sseText("done"),
	}}
	ag, _ := newTestSession(t, tokenPlanOptions(model), nativeEnv(env))

	promptOnce(t, ag, "run it")

	raw, err := json.Marshal(model.body(t, 1)["messages"])
	require.NoError(t, err)
	return string(raw)
}

func TestNativeAgentEnablesGuardOnlyWithEnv(t *testing.T) {
	on := guardProbe(t, map[string]string{"ASK_COMMAND_GUARD": "1"})
	assert.Contains(t, on, "blocked by command-guard")

	off := guardProbe(t, nil)
	assert.NotContains(t, off, "blocked by command-guard")
	assert.Contains(t, off, "tool_result", "the command ran and returned a result")

	other := guardProbe(t, map[string]string{"ASK_COMMAND_GUARD": "true"})
	assert.NotContains(t, other, "blocked by command-guard", "only the value 1 enables the guard")
}

// The tests below start jobs on the table directly, with no owner, so the jobs
// belong to generation 0.
func TestNativeAgentSessionsHaveSeparateTables(t *testing.T) {
	o := tokenPlanOptions(&scriptedModel{})
	_, first := newTestSession(t, o, nativeEnv(nil))
	_, second := newTestSession(t, o, nativeEnv(nil))
	require.NotSame(t, first, second)

	job, err := first.Start(jobs.StartSpec{Command: "sleep 30", Cwd: t.TempDir()})
	require.NoError(t, err)

	require.Len(t, first.List(0), 1)
	assert.Equal(t, job.ID(), first.List(0)[0].ID)
	assert.Empty(t, second.List(0), "a job of one session is not listed in another")
}

func TestNativeAgentDisposeClosesTable(t *testing.T) {
	ag, jt := newTestSession(t, tokenPlanOptions(&scriptedModel{}), nativeEnv(nil))
	_, err := jt.Start(jobs.StartSpec{Command: "sleep 30", Cwd: t.TempDir()})
	require.NoError(t, err)

	require.NoError(t, ag.Dispose())

	_, err = jt.Start(jobs.StartSpec{Command: "true", Cwd: t.TempDir()})
	require.ErrorIs(t, err, jobs.ErrClosed)
	list := jt.List(0)
	require.Len(t, list, 1)
	assert.Equal(t, jobs.StatusKilled, list[0].Status, "the dispose killed the job")
}

func TestNativeAgentJobsGetSessionEnvironment(t *testing.T) {
	o := tokenPlanOptions(&scriptedModel{})
	o.thinking = protocol.ThinkingMedium
	o.environ = func() []string {
		return []string{"PATH=" + os.Getenv("PATH"), "FIXTURE_API_KEY=hunter2", "ASK_HOME=/leader/home", "KEEP_ME=1"}
	}
	ag, jt, err := newNativeAgent(o, nativeEnv(nil), nil, "session-env-1", t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = ag.Dispose() })

	job, err := jt.Start(jobs.StartSpec{Command: "env", Cwd: t.TempDir()})
	require.NoError(t, err)
	require.Equal(t, jobs.Done, jt.Wait(context.Background(), job.ID(), 10*time.Second))

	out := job.Snapshot()
	text := string(out.Head) + string(out.Tail)
	assert.Contains(t, text, "KEEP_ME=1")
	assert.Contains(t, text, "ASK_SESSION_ID=session-env-1")
	assert.Contains(t, text, "ASK_PROVIDER="+anthropic.ProviderID)
	assert.Contains(t, text, "ASK_MODEL="+anthropic.ModelID)
	assert.Contains(t, text, "ASK_REASONING_LEVEL=medium")
	assert.NotContains(t, text, "hunter2", "credential-shaped names are dropped")
	assert.NotContains(t, text, "/leader/home", "inherited ASK_* names are dropped")
}

// promptRecorder is a promptTarget that records the order of its calls.
type promptRecorder struct {
	events []string
	busy   int
	idle   chan struct{} // nil: the idle signal is already given
}

func (p *promptRecorder) Idle() <-chan struct{} {
	p.events = append(p.events, "signal")
	if p.idle != nil {
		return p.idle
	}
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (p *promptRecorder) WaitForIdle(context.Context) error {
	p.events = append(p.events, "idle")
	return nil
}

func (p *promptRecorder) Prompt(_ context.Context, msgs ...protocol.Message) error {
	p.events = append(p.events, "prompt:"+blockText(msgs[0].(protocol.UserMessage).Content))
	if p.busy > 0 {
		p.busy--
		return agent.ErrBusy
	}
	return nil
}

func TestPromptAllRetriesWhenNoticeRunIsActive(t *testing.T) {
	rec := &promptRecorder{busy: 2}

	err := promptAll(context.Background(), rec, []string{"first", "second"})

	require.NoError(t, err)
	assert.Equal(t, []string{
		"idle", "prompt:first", "signal", // a notice run started: busy
		"idle", "prompt:first", "signal", // busy again
		"idle", "prompt:first",
		"idle", "prompt:second",
		"idle", // the notice of the last prompt gets its answer
	}, rec.events)
}

func TestPromptAllStopsOnOtherErrors(t *testing.T) {
	boom := errors.New("boom")
	failing := promptFunc(func(context.Context, ...protocol.Message) error { return boom })

	err := promptAll(context.Background(), failing, []string{"first", "second"})

	require.ErrorIs(t, err, boom)
}

type promptFunc func(context.Context, ...protocol.Message) error

func (promptFunc) WaitForIdle(context.Context) error { return nil }

func (promptFunc) Idle() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (f promptFunc) Prompt(ctx context.Context, msgs ...protocol.Message) error {
	return f(ctx, msgs...)
}

func TestPromptAllWaitsForNoticeRunOfRealAgent(t *testing.T) {
	model := &scriptedModel{answers: []string{sseText("notice answer"), sseText("prompt answer")}}
	ag, _ := newTestSession(t, tokenPlanOptions(model), nativeEnv(nil))
	// A completion notice on an idle agent starts a run of its own.
	_, err := ag.FollowUp(textUser(protocol.SystemReminder("Background job bash-1 finished.")))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, promptAll(ctx, ag, []string{"next"}))

	msgs := ag.State().Messages
	require.Len(t, msgs, 4, "notice, its answer, the prompt, its answer")
	assert.Equal(t, agent.Idle, ag.State().Status)
}

func TestPrintEndDisposesAgentAndKillsBackgroundJobs(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	command := "echo $$ > " + pidFile + "; trap '' TERM; sleep 300"
	model := &scriptedModel{answers: []string{
		toolUseStream(t, "bash", map[string]any{"command": command, "timeout": 1}),
		sseText("the final answer"),
	}}
	var stdout, stderr bytes.Buffer
	deps := runDependencies{
		getenv:        nativeEnv(map[string]string{"ASK_HOME": t.TempDir()}),
		inferenceHTTP: &http.Client{Transport: model},
	}

	code := runWithDependencies([]string{"-p", "--provider", anthropic.ProviderID, "start a job"}, strings.NewReader(""), &stdout, &stderr, deps)

	require.Equal(t, 0, code, stderr.String())
	assert.Equal(t, "the final answer\n", stdout.String())
	raw, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	require.NoError(t, err)
	assert.ErrorIs(t, syscall.Kill(-pid, 0), syscall.ESRCH, "the process group is gone when ask -p returns")
}

// After a busy refusal the retry waits for the idle signal of the Agent, not
// for a timer.
func TestPromptAllRetryWaitsForIdleSignal(t *testing.T) {
	rec := &promptRecorder{busy: 1, idle: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- promptAll(context.Background(), rec, []string{"only"}) }()

	select {
	case err := <-done:
		t.Fatalf("promptAll returned before the idle signal: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(rec.idle)
	require.NoError(t, <-done)
	assert.Equal(t, []string{"idle", "prompt:only", "signal", "idle", "prompt:only", "idle"}, rec.events)
}
