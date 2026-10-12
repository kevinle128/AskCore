package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/leader"
	"AskCore/internal/tui"
	"AskCore/internal/tuiclient/rpc"
	"AskCore/pkg/protocol"
)

// These tests drive the built `ask leader` over its Unix socket, and the
// in-process agent of the TUI. Only the model HTTP service is replaced: the
// leader reaches the provider fixture through a proxy, the in-process agent
// through a scripted transport.

// leaderFrame is one session/update notification.
type leaderFrame struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
}

// frameUpdate is the part of a session update that these tests read.
type frameUpdate struct {
	Kind       string `json:"sessionUpdate"`
	ToolCallID string `json:"toolCallId"`
	Title      string `json:"title"`
	ToolKind   string `json:"kind"`
	Status     string `json:"status"`
	Locations  []struct {
		Path string `json:"path"`
		Line int    `json:"line"`
	} `json:"locations"`
	// Content is an array on a tool frame and an object on a message chunk.
	Content json.RawMessage `json:"content"`
}

// toolContentItem is one entry of the content array of a tool frame.
type toolContentItem struct {
	Type    string `json:"type"`
	Path    string `json:"path"`
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
	Content struct {
		Text string `json:"text"`
	} `json:"content"`
}

// toolContent decodes the content array of a tool frame.
func (u frameUpdate) toolContent(t *testing.T) []toolContentItem {
	t.Helper()
	var items []toolContentItem
	if len(u.Content) > 0 && u.Content[0] == '[' {
		require.NoError(t, json.Unmarshal(u.Content, &items))
	}
	return items
}

// leaderPeer is a client of the leader that keeps the session updates.
type leaderPeer struct {
	t    *testing.T
	conn *rpc.Conn

	mu     sync.Mutex
	frames []leaderFrame
}

func (p *leaderPeer) Request(id json.RawMessage, _ string, _ json.RawMessage) {
	_ = p.conn.RespondError(id, -32601, "not supported")
}

func (p *leaderPeer) Notification(method string, params json.RawMessage) {
	if method != "session/update" {
		return
	}
	var f leaderFrame
	if json.Unmarshal(params, &f) != nil {
		return
	}
	p.mu.Lock()
	p.frames = append(p.frames, f)
	p.mu.Unlock()
}

// updates returns the decoded updates of one session in arrival order.
func (p *leaderPeer) updates(sid string) []frameUpdate {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []frameUpdate
	for _, f := range p.frames {
		if f.SessionID != sid {
			continue
		}
		var u frameUpdate
		require.NoError(p.t, json.Unmarshal(f.Update, &u))
		out = append(out, u)
	}
	return out
}

func (p *leaderPeer) rawFrames() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var b strings.Builder
	for _, f := range p.frames {
		b.Write(f.Update)
		b.WriteByte('\n')
	}
	return b.String()
}

func (p *leaderPeer) call(method string, params any, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := p.conn.Call(ctx, method, params)
	if err != nil {
		return err
	}
	if out != nil {
		return json.Unmarshal(res, out)
	}
	return nil
}

// newSession creates a session in cwd on the Anthropic model that the fixture
// serves.
func (p *leaderPeer) newSession(cwd string) string {
	p.t.Helper()
	var res struct {
		SessionID string `json:"sessionId"`
	}
	require.NoError(p.t, p.call("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}}, &res))
	require.NotEmpty(p.t, res.SessionID)
	require.NoError(p.t, p.call(protocol.ACPSetModel, map[string]any{"sessionId": res.SessionID, "modelId": anthropicModel, "authMethodId": "api-key"}, nil))
	return res.SessionID
}

func (p *leaderPeer) prompt(sid, text string) {
	p.t.Helper()
	require.NoError(p.t, p.call("session/prompt", e2ePrompt(sid, text), nil))
}

// scriptFixture makes f answer its inference requests with steps, in order.
// A request past the last step fails the test.
func scriptFixture(t *testing.T, f *providerFixture, steps ...modelStep) {
	t.Helper()
	f.inference = func(n int, _ *http.Request, body []byte) (int, string, string) {
		if n >= len(steps) {
			t.Errorf("the fixture got request %d, but the script has %d steps", n+1, len(steps))
			return http.StatusOK, "text/event-stream", sseText("unexpected request")
		}
		if steps[n].before != nil {
			runBefore(steps[n].before, body)
		}
		reply := strings.Replace(steps[n].reply, `"call_1"`, `"call_`+strconv.Itoa(n+1)+`"`, 1)
		return http.StatusOK, "text/event-stream", reply
	}
}

// toolLeaderEnv is the environment of a leader that reaches the fixture.
func toolLeaderEnv(home string, f *providerFixture) []string {
	env := append(leaderEnvWithoutFaux(home), "ANTHROPIC_API_KEY=fixture-anthropic-key")
	for k, v := range f.env() {
		env = append(env, k+"="+v)
	}
	return env
}

// startToolLeader starts a leader of a new home that reaches the fixture, and
// connects a client. The leader is stopped when the test ends.
func startToolLeader(t *testing.T, f *providerFixture) (*leaderPeer, string, string) {
	t.Helper()
	return startToolLeaderFrom(t, f, acpBinary(t))
}

// startToolLeaderFrom is startToolLeader for a leader that bin starts. bin must
// be named ask: the call finds the program next to its caller.
func startToolLeaderFrom(t *testing.T, f *providerFixture, bin string) (*leaderPeer, string, string) {
	t.Helper()
	home := leaderHome(t)
	stopLeaderAtEnd(t, bin, home)
	cfg := e2eConnect(home, bin)
	cfg.Env = toolLeaderEnv(home, f)
	cl, err := leader.ConnectOrSpawn(context.Background(), cfg)
	require.NoError(t, err)
	p := &leaderPeer{t: t}
	p.conn = rpc.NewConn(rpc.NewLeaderTransport(cl), p)
	go p.conn.Run()
	t.Cleanup(func() { _ = p.conn.Close() })
	require.NoError(t, p.call("initialize", map[string]any{"protocolVersion": 1}, nil))
	return p, bin, home
}

// stopToolLeader runs `ask leader stop` and returns how long it took.
func stopToolLeader(t *testing.T, bin, home string) time.Duration {
	t.Helper()
	cmd := exec.Command(bin, "leader", "stop")
	cmd.Env = leaderEnv(home)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return time.Since(start)
}

func TestLeaderToolUpdatesCarryKindLocationsAndDiff(t *testing.T) {
	repo := newToolRepo(t)
	path := filepath.Join(repo, "calc.go")
	f := newProviderFixture(t)
	scriptFixture(t, f,
		call(t, "read", map[string]any{"path": path}),
		call(t, "edit", map[string]any{"path": path, "edits": []map[string]any{{"oldText": "a - b", "newText": "a + b"}}}),
		call(t, "ls", map[string]any{"path": repo}),
		call(t, "bash", map[string]any{"command": "echo from-bash"}),
		say("all done"),
	)
	p, _, _ := startToolLeader(t, f)
	sid := p.newSession(repo)

	p.prompt(sid, "fix it")

	ups := p.updates(sid)
	startedKind := map[string]string{}
	finished := map[string]frameUpdate{}
	for _, u := range ups {
		switch u.Kind {
		case "tool_call":
			startedKind[u.Title] = u.ToolKind
		case "tool_call_update":
			if u.Status == "completed" || u.Status == "failed" {
				finished[u.ToolCallID] = u
			}
		}
	}
	assert.Equal(t, "read", startedKind["read"])
	assert.Equal(t, "edit", startedKind["edit"])
	assert.Equal(t, "search", startedKind["ls"])
	assert.Equal(t, "execute", startedKind["bash"])

	byKind := map[string]frameUpdate{}
	for _, u := range finished {
		byKind[u.ToolKind] = u
	}
	read := byKind["read"]
	require.NotEmpty(t, read.Locations, "the read frame names the file")
	assert.Equal(t, path, read.Locations[0].Path)
	assert.Equal(t, 1, read.Locations[0].Line)
	edit := byKind["edit"]
	assert.Equal(t, "completed", edit.Status)
	require.NotEmpty(t, edit.Locations)
	assert.Equal(t, path, edit.Locations[0].Path)
	var diff bool
	for _, c := range edit.toolContent(t) {
		if c.Type == "diff" {
			diff = true
			assert.Equal(t, path, c.Path)
			assert.Contains(t, c.OldText, "return a - b")
			assert.Contains(t, c.NewText, "return a + b")
		}
	}
	assert.True(t, diff, "the edit frame carries a diff")
	execute := byKind["execute"]
	items := execute.toolContent(t)
	require.NotEmpty(t, items)
	assert.Contains(t, items[0].Content.Text, "from-bash")
}

func TestLeaderNoticeIsNotSentAsUserFrame(t *testing.T) {
	repo := newToolRepo(t)
	dir := t.TempDir()
	pgid, gate, done := filepath.Join(dir, "pgid"), filepath.Join(dir, "gate"), filepath.Join(dir, "done")
	command := recordGroup(pgid) + "while [ ! -f " + gate + " ]; do sleep 0.05; done; echo job-result; touch " + done
	var mu sync.Mutex
	var noticeBody string
	f := newProviderFixture(t)
	scriptFixture(t, f,
		call(t, "bash", map[string]any{"command": command, "run_in_background": true}),
		say("job started"),
		modelStep{before: func(body []byte) {
			mu.Lock()
			noticeBody = string(body)
			mu.Unlock()
		}, reply: sseText("notice answered")},
	)
	p, _, _ := startToolLeader(t, f)
	sid := p.newSession(repo)

	p.prompt(sid, "start the job")
	readPgid(t, pgid)
	require.NoError(t, os.WriteFile(gate, nil, 0o644))
	waitFile(t, done)

	require.Eventually(t, func() bool {
		return strings.Contains(p.rawFrames(), "notice answered")
	}, e2eWaitFor, 20*time.Millisecond, "the answer to the notice reaches the client")
	mu.Lock()
	body := noticeBody
	mu.Unlock()
	assert.Contains(t, body, "Background job bash-1", "the model got the notice")
	for _, u := range p.updates(sid) {
		assert.NotEqual(t, "user_message_chunk", u.Kind, "no standard frame carries the notice")
	}
	assert.NotContains(t, p.rawFrames(), "system-reminder")
	assert.NotContains(t, p.rawFrames(), "Background job bash-1")
}

func TestLeaderSessionCloseKillsBackgroundJobs(t *testing.T) {
	repo := newToolRepo(t)
	pgid := filepath.Join(t.TempDir(), "pgid")
	f := newProviderFixture(t)
	scriptFixture(t, f,
		call(t, "bash", map[string]any{"command": recordGroup(pgid) + "trap '' TERM; sleep 300", "run_in_background": true}),
		say("job started"),
	)
	p, _, _ := startToolLeader(t, f)
	sid := p.newSession(repo)
	p.prompt(sid, "start the job")
	requireGroupAlive(t, pgid)

	require.NoError(t, p.call("session/close", e2eSession(sid), nil))
	requireGroupGoneSoon(t, pgid)
	var fault *rpc.Fault
	require.ErrorAs(t, p.call(protocol.ACPState, e2eSession(sid), nil), &fault, "the leader forgets a closed session")
	require.Equal(t, "unknown_session", fault.Kind)
}

func TestLeaderShutdownKillsBackgroundJobs(t *testing.T) {
	dir := t.TempDir()
	var pgids []string
	var steps []modelStep
	for i := range 3 {
		pgid := filepath.Join(dir, "pgid"+strconv.Itoa(i))
		pgids = append(pgids, pgid)
		steps = append(steps,
			call(t, "bash", map[string]any{"command": recordGroup(pgid) + "trap '' TERM; sleep 300", "run_in_background": true}),
			say("job started"),
		)
	}
	f := newProviderFixture(t)
	scriptFixture(t, f, steps...)
	p, bin, home := startToolLeader(t, f)
	for range pgids {
		sid := p.newSession(t.TempDir())
		p.prompt(sid, "start the job")
	}
	for _, pgid := range pgids {
		requireGroupAlive(t, pgid)
	}

	took := stopToolLeader(t, bin, home)

	assert.Less(t, took, 8*time.Second, "the jobs of three sessions close in parallel")
	for _, pgid := range pgids {
		requireGroupGone(t, pgid)
	}
}

func TestInProcessClientCloseKillsBackgroundJobs(t *testing.T) {
	repo := newToolRepo(t)
	pgid := filepath.Join(t.TempDir(), "pgid")
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": recordGroup(pgid) + "trap '' TERM; sleep 300", "run_in_background": true}),
		say("job started"),
	)
	home := leaderHome(t)
	deps := runDependencies{
		getenv: func(k string) string {
			switch k {
			case "ASK_HOME":
				return home
			case "ANTHROPIC_API_KEY":
				return "fixture-anthropic-key"
			}
			return ""
		},
		inferenceHTTP: &http.Client{Transport: m},
	}
	c, err := openInProcessClient(context.Background(), deps)
	require.NoError(t, err)
	col := collect(t, c)
	ctx, cancel := context.WithTimeout(context.Background(), clientWait)
	defer cancel()
	_, err = c.NewSession(ctx, repo)
	require.NoError(t, err)
	_, err = c.SetModel(ctx, anthropicModel, false)
	require.NoError(t, err)

	_, err = c.Prompt("start the job")
	require.NoError(t, err)
	col.waitFor("the run settles", isType[tui.PromptDoneMsg])
	requireGroupAlive(t, pgid)

	start := time.Now()
	require.NoError(t, c.Close())

	assert.Less(t, time.Since(start), inProcessStopWait, "the close fits the in-process stop bound")
	requireGroupGone(t, pgid)
}
