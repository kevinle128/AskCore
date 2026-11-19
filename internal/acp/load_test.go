package acp

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

// loadStore keeps the session logs of a test host in memory. It stands for the
// session file store: a log that a live agent holds cannot be opened again.
type loadStore struct {
	mu   sync.Mutex
	logs map[string]*sessions.MemoryLog
	held map[string]bool
	cwd  map[string]string
}

func newLoadStore() *loadStore {
	return &loadStore{logs: map[string]*sessions.MemoryLog{}, held: map[string]bool{}, cwd: map[string]string{}}
}

// release frees the lock of a session, as the end of its holder does.
func (st *loadStore) release(id string) {
	st.mu.Lock()
	st.held[id] = false
	st.mu.Unlock()
}

// config returns an adapter config whose factory saves each new session in the
// store and resumes a stored one on a load.
func (st *loadStore) config(t *testing.T, steps stepsFor, tool *holdTool) Config {
	t.Helper()
	cfg := testConfig(steps, nil)
	cfg.Factory = func(_ context.Context, req SessionRequest) (*agent.Agent, error) {
		st.mu.Lock()
		log, known := st.logs[req.ID]
		switch {
		case req.Resume && (!known || st.cwd[req.ID] != req.Cwd):
			st.mu.Unlock()
			return nil, ErrUnknownSession
		case req.Resume && st.held[req.ID]:
			st.mu.Unlock()
			return nil, ErrSessionInUse
		case !req.Resume:
			log = &sessions.MemoryLog{}
			st.logs[req.ID], st.cwd[req.ID] = log, req.Cwd
		}
		st.held[req.ID] = true
		st.mu.Unlock()
		p, err := faux.New(faux.WithChunk(1000, 1000))
		if err != nil {
			return nil, err
		}
		m, _ := p.Model("faux-1")
		p.Set(steps()...)
		c := agent.Config{
			SessionID: req.ID,
			LoopConfig: agent.LoopConfig{
				Model: m, Stream: p.Stream, Cwd: req.Cwd,
				Options: providers.StreamOptions{APIKey: "test-key"},
				Wait:    func(context.Context, time.Duration) error { return nil },
			},
			NewContext: func() (sessions.Writer, error) { return log, nil },
		}
		if tool != nil {
			c.Tools = holdRegistry(t, tool)
		}
		return agent.New(c)
	}
	return cfg
}

func loadParams(sid, cwd string) map[string]any {
	return map[string]any{"sessionId": sid, "cwd": cwd, "mcpServers": []any{}}
}

// updatesFrom returns the session/update notifications that came after the
// first n frames, as (kind, text, status) rows.
type updateRow struct {
	kind, text, status string
}

func (p *adapterPeer) updateRowsFrom(n int) []updateRow {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []updateRow
	for _, f := range p.frames[n:] {
		if f.Method != "session/update" {
			continue
		}
		var u struct {
			Update struct {
				SessionUpdate string `json:"sessionUpdate"`
				Status        string `json:"status"`
				Content       json.RawMessage
			} `json:"update"`
		}
		if err := json.Unmarshal(f.Params, &u); err != nil {
			continue
		}
		row := updateRow{kind: u.Update.SessionUpdate, status: u.Update.Status}
		var text struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(u.Update.Content, &text) == nil {
			row.text = text.Text
		}
		out = append(out, row)
	}
	return out
}

func (p *adapterPeer) frameCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.frames)
}

func TestInitializeAdvertisesLoadSession(t *testing.T) {
	p := newAdapterPeer(t, testConfig(says("x"), nil), nil)
	var res map[string]any
	p.ok("initialize", map[string]any{"protocolVersion": 1}, &res)
	caps := res["agentCapabilities"].(map[string]any)
	require.Equal(t, true, caps["loadSession"])
}

func TestLoadSessionReplaysHistoryBeforeResult(t *testing.T) {
	st := newLoadStore()
	released := make(chan struct{})
	close(released)
	tool := &holdTool{started: make(chan struct{}), release: released}
	steps := func() []faux.Step {
		return []faux.Step{
			faux.Reply(faux.Thinking("pondering"), faux.Text("calling"), faux.ToolCall("hold", nil, faux.ID("call-1"))),
			faux.Say("done"),
			faux.Say("and again"),
		}
	}
	p := newAdapterPeer(t, st.config(t, steps, tool), nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	cwd := t.TempDir()
	var created struct {
		SessionID string `json:"sessionId"`
	}
	p.ok("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}}, &created)
	sid := created.SessionID
	p.ok("session/prompt", promptParams(sid, "go"), nil)
	p.ok("session/close", map[string]any{"sessionId": sid}, nil)
	st.release(sid)

	before := p.frameCount()
	id := p.send("session/load", loadParams(sid, cwd))
	f := p.await(id)
	require.Nil(t, f.Error, "%+v", f.Error)
	rows := p.updateRowsFrom(before)
	require.Equal(t, []updateRow{
		{kind: "user_message_chunk", text: "go"},
		{kind: "agent_thought_chunk", text: "pondering"},
		{kind: "agent_message_chunk", text: "calling"},
		{kind: "tool_call", status: "completed"},
		{kind: "tool_call_update", status: "completed"},
		{kind: "agent_message_chunk", text: "done"},
	}, rows)
	require.Greater(t, p.responseIndex(id), p.lastNoteIndex("session/update"), "the history comes before the answer")

	// The loaded session is live and continues on the same log.
	var loaded, after protocol.ACPStateResult
	p.ok(protocol.ACPState, map[string]any{"sessionId": sid}, &loaded)
	require.Equal(t, 4, loaded.MessageCount, "the earlier conversation is the context")
	p.ok("session/prompt", promptParams(sid, "next"), nil)
	p.ok(protocol.ACPState, map[string]any{"sessionId": sid}, &after)
	require.Greater(t, after.MessageCount, loaded.MessageCount, "the new turn is appended to the same log")
}

func TestLoadSessionRefusesRelativeCwd(t *testing.T) {
	p := newAdapterPeer(t, newLoadStore().config(t, says("x"), nil), nil)
	sid := p.start()
	f := p.call("session/load", loadParams(sid, "relative/dir"))
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
}

func TestLoadSessionRefusesOtherCwd(t *testing.T) {
	st := newLoadStore()
	p := newAdapterPeer(t, st.config(t, says("x"), nil), nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	cwd := t.TempDir()
	var created struct {
		SessionID string `json:"sessionId"`
	}
	p.ok("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}}, &created)
	p.ok("session/close", map[string]any{"sessionId": created.SessionID}, nil)
	st.release(created.SessionID)
	f := p.call("session/load", loadParams(created.SessionID, t.TempDir()))
	require.Equal(t, protocol.ACPErrUnknownSession, f.errKind(t))
}

func TestLoadSessionUnknownID(t *testing.T) {
	p := newAdapterPeer(t, newLoadStore().config(t, says("x"), nil), nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	f := p.call("session/load", loadParams("1234567890123456789", t.TempDir()))
	require.Equal(t, protocol.ACPErrUnknownSession, f.errKind(t))
	require.Equal(t, -32002, f.Error.Code)
	f = p.call("session/load", loadParams("", t.TempDir()))
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
}

func TestLoadSessionLiveIDInUse(t *testing.T) {
	st := newLoadStore()
	p := newAdapterPeer(t, st.config(t, says("x"), nil), nil)
	p.ok("initialize", map[string]any{"protocolVersion": 1}, nil)
	cwd := t.TempDir()
	var created struct {
		SessionID string `json:"sessionId"`
	}
	p.ok("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}}, &created)
	before := p.frameCount()
	f := p.call("session/load", loadParams(created.SessionID, t.TempDir()))
	require.Equal(t, protocol.ACPErrUnknownSession, f.errKind(t), "a session belongs to its folder, live or stored")
	f = p.call("session/load", loadParams(created.SessionID, cwd))
	require.Equal(t, protocol.ACPErrSessionInUse, f.errKind(t))
	require.Equal(t, protocol.ACPCodeSessionInUse, f.Error.Code)
	require.Equal(t, protocol.ACPErrorMessage(protocol.ACPErrSessionInUse), f.Error.Message)
	require.Empty(t, p.updateRowsFrom(before), "a refused load replays nothing")

	// A session that another holder owns gives the same answer.
	p.ok("session/close", map[string]any{"sessionId": created.SessionID}, nil)
	f = p.call("session/load", loadParams(created.SessionID, cwd))
	require.Equal(t, protocol.ACPErrSessionInUse, f.errKind(t), "the store still holds the log")
	st.release(created.SessionID)
	p.ok("session/load", loadParams(created.SessionID, cwd), nil)
}

func TestLoadSessionRefusesMcpServersAndNoSession(t *testing.T) {
	st := newLoadStore()
	p := newAdapterPeer(t, st.config(t, says("x"), nil), nil)
	sid := p.start()
	params := loadParams(sid, t.TempDir())
	params["mcpServers"] = []any{map[string]any{"name": "m", "command": "x", "args": []any{}, "env": []any{}}}
	f := p.call("session/load", params)
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))

	params = loadParams("1234567890123456789", t.TempDir())
	params["_meta"] = map[string]any{protocol.ACPSessionOptionsMetaKey: map[string]any{"noSession": true}}
	f = p.call("session/load", params)
	require.Equal(t, protocol.ACPErrInvalidParams, f.errKind(t))
	require.False(t, strings.Contains(f.Error.Message, "1234567890123456789"))
}

func TestSessionOptionsNoSessionRoundTrip(t *testing.T) {
	flags, err := flagsFromMeta(map[string]any{protocol.ACPSessionOptionsMetaKey: map[string]any{"noSession": true}})
	require.NoError(t, err)
	require.True(t, flags.NoSession)
	require.False(t, flags.IsZero(), "the flag alone must still reach the host and be acknowledged")
}

func TestErrorKindSessionInUseHasFixedText(t *testing.T) {
	re := requestError(ErrSessionInUse)
	require.Equal(t, protocol.ACPCodeSessionInUse, re.Code)
	require.Equal(t, protocol.ACPErrorData{Kind: protocol.ACPErrSessionInUse}, re.Data)
	require.Contains(t, re.Message, "ask leader stop")
	require.Equal(t, protocol.ACPErrUnknownSession, classify(ErrUnknownSession))
	var _ sdk.AgentLoader = (*Adapter)(nil)
}

func TestHistoryUpdatesSkipsRemindersAndShowsSkillCommands(t *testing.T) {
	entries := []sessions.Entry{
		sessions.MessageEntry{Message: protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: protocol.SystemReminder("job 1 finished")}}}},
		sessions.MessageEntry{Message: protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: protocol.SkillInput("review", "/abs/skills/review/SKILL.md", "body", "now")}}}},
	}
	updates := historyUpdates(entries)
	require.Len(t, updates, 1)
	require.NotNil(t, updates[0].UserMessageChunk)
	text := updates[0].UserMessageChunk.Content.Text
	require.NotNil(t, text)
	require.Equal(t, "/skill:review now", text.Text)
}

func TestHistoryUpdatesPairsRepeatedCallIDsInLogOrder(t *testing.T) {
	call := protocol.ToolCall{ID: "c1", Name: "bash", Arguments: json.RawMessage(`{}`)}
	entries := []sessions.Entry{
		sessions.MessageEntry{Message: protocol.AssistantMessage{Content: []protocol.AssistantBlock{call}}},
		sessions.MessageEntry{Message: protocol.ToolResultMessage{ToolCallID: "c1", ToolName: "bash"}},
		sessions.MessageEntry{Message: protocol.AssistantMessage{Content: []protocol.AssistantBlock{call}}},
	}
	var starts []*sdk.SessionUpdateToolCall
	for _, u := range historyUpdates(entries) {
		if u.ToolCall != nil {
			starts = append(starts, u.ToolCall)
		}
	}
	require.Len(t, starts, 2)
	require.Equal(t, sdk.ToolCallStatusCompleted, starts[0].Status)
	// The second call with the same ID has no result yet.
	require.Equal(t, sdk.ToolCallStatusInProgress, starts[1].Status)
}
