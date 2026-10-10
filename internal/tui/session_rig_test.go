package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"AskCore/pkg/protocol"
)

// fakeClient is a scripted Client. It stands in for the agent link at the
// interface boundary; the model, the Bubble Tea program and the terminal
// emulator around it are the real ones. Calls come from command goroutines, so
// every field is guarded.
type fakeClient struct {
	mu     sync.Mutex
	events chan Message
	info   ConnInfo
	calls  []string

	sessionID string
	cwd       string
	models    protocol.ACPModelsResult
	state     protocol.ACPStateResult
	usage     protocol.ACPUsageResult
	live      []protocol.ACPLiveSession
	listErr   error

	promptErr    error
	setModelErr  map[string]error
	setModelGate map[string]chan struct{}
	removeFails  map[string]bool
	attachEvents []Message // pushed by Attach before it returns
	thinkingCap  protocol.ThinkingLevel

	nextPrompt uint64
	nextInput  int
}

const (
	testLeaderID  = "7c1e5a90b3d24f68"
	testSessionID = "sess_3f2a91c07b5d8e46"
	deepseekModel = "alibaba/deepseek-v4.1-flash@chat"
	qwenModel     = "alibaba/qwen4-coder-plus@chat"
	opusModel     = "anthropic/claude-opus-5-5@messages"
	sonnetModel   = "anthropic/claude-sonnet-5-5@messages"
	codexModel    = "openai/gpt-5.5-codex@responses"
	grokModel     = "xai/grok-5-code@chat"
)

func testCatalog() []protocol.ACPModel {
	row := func(id, provider, name string, window int, reasoning bool) protocol.ACPModel {
		return protocol.ACPModel{ModelID: id, Provider: provider, ID: name, Name: name, ContextWindow: window, Reasoning: reasoning}
	}
	return []protocol.ACPModel{
		row(deepseekModel, "alibaba", "deepseek-v4.1-flash", 128_000, false),
		row(qwenModel, "alibaba", "qwen4-coder-plus", 256_000, false),
		row(opusModel, "anthropic", "claude-opus-5-5", 1_000_000, true),
		row(sonnetModel, "anthropic", "claude-sonnet-5-5", 1_000_000, true),
		row(codexModel, "openai", "gpt-5.5-codex", 400_000, true),
		row(grokModel, "xai", "grok-5-code", 256_000, true),
	}
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		events:      make(chan Message, 256),
		info:        ConnInfo{Mode: ModeLeader, InstanceID: testLeaderID, ClientID: "c1"},
		sessionID:   testSessionID,
		models:      protocol.ACPModelsResult{Current: deepseekModel, Models: testCatalog()},
		state:       protocol.ACPStateResult{SessionID: testSessionID, ModelID: deepseekModel, ThinkingLevel: protocol.ThinkingOff},
		usage:       protocol.ACPUsageResult{Total: protocol.Usage{Cost: protocol.Cost{Total: 40_000}}, Complete: true},
		removeFails: map[string]bool{}, setModelErr: map[string]error{}, setModelGate: map[string]chan struct{}{},
	}
}

func (f *fakeClient) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeClient) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeClient) count(prefix string) int {
	n := 0
	for _, c := range f.callList() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeClient) has(call string) bool {
	for _, c := range f.callList() {
		if c == call {
			return true
		}
	}
	return false
}

func (f *fakeClient) push(msgs ...Message) {
	for _, m := range msgs {
		f.events <- m
	}
}

func (f *fakeClient) Events() <-chan Message { return f.events }

func (f *fakeClient) Info() ConnInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.info
}

func (f *fakeClient) setInfo(info ConnInfo) {
	f.mu.Lock()
	f.info = info
	f.mu.Unlock()
}

func (f *fakeClient) NewSession(_ context.Context, cwd string) (SessionInfo, error) {
	f.record("new_session")
	f.mu.Lock()
	id := f.sessionID
	f.cwd = cwd
	f.mu.Unlock()
	f.push(SnapshotMsg{SessionID: id})
	return SessionInfo{ID: id, Role: protocol.ACPRoleDriver, Cwd: cwd}, nil
}

func (f *fakeClient) Attach(_ context.Context, id string) (SessionInfo, error) {
	f.record("attach:" + id)
	f.mu.Lock()
	events := f.attachEvents
	f.mu.Unlock()
	f.push(events...)
	return SessionInfo{ID: id, Role: protocol.ACPRoleObserver}, nil
}

func (f *fakeClient) Detach(context.Context) error {
	f.record("detach")
	if f.Info().Mode == ModeInProcess {
		return ErrNeedsLeader
	}
	return nil
}

func (f *fakeClient) Take(context.Context) (protocol.ACPTakeResult, error) {
	f.record("take")
	if f.Info().Mode == ModeInProcess {
		return protocol.ACPTakeResult{}, ErrNeedsLeader
	}
	return protocol.ACPTakeResult{SessionID: f.sessionID}, nil
}

func (f *fakeClient) ListSessions(context.Context) ([]protocol.ACPLiveSession, error) {
	f.record("list_sessions")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.live, f.listErr
}

func (f *fakeClient) Prompt(text string) (PromptID, error) {
	f.record("prompt:" + text)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.promptErr != nil {
		return 0, f.promptErr
	}
	f.nextPrompt++
	return PromptID(f.nextPrompt), nil
}

func (f *fakeClient) Cancel() error { f.record("cancel"); return nil }

func (f *fakeClient) input(kind, text string) (string, error) {
	f.record(kind + ":" + text)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextInput++
	return fmt.Sprintf("in%d", f.nextInput), nil
}

func (f *fakeClient) Steer(_ context.Context, text string) (string, error) {
	return f.input("steer", text)
}

func (f *fakeClient) FollowUp(_ context.Context, text string) (string, error) {
	return f.input("followup", text)
}

func (f *fakeClient) Remove(_ context.Context, id string) (bool, error) {
	f.record("remove:" + id)
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.removeFails[id], nil
}

func (f *fakeClient) State(context.Context) (protocol.ACPStateResult, error) {
	f.record("state")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, nil
}

func (f *fakeClient) Models(context.Context) (protocol.ACPModelsResult, error) {
	f.record("models")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.models, nil
}

func (f *fakeClient) SetModel(_ context.Context, id string) (protocol.ACPSetModelResult, error) {
	f.record("set_model:" + id)
	f.mu.Lock()
	gate, err := f.setModelGate[id], f.setModelErr[id]
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if err != nil {
		return protocol.ACPSetModelResult{}, err
	}
	level := protocol.ThinkingOff
	for _, m := range f.models.Models {
		if m.ModelID == id && m.Reasoning {
			level = protocol.ThinkingMedium
		}
	}
	f.mu.Lock()
	f.state.ModelID, f.state.ThinkingLevel = id, level
	f.models.Current = id
	f.mu.Unlock()
	return protocol.ACPSetModelResult{ModelID: id, ThinkingLevel: level}, nil
}

func (f *fakeClient) SetThinking(_ context.Context, level protocol.ThinkingLevel) (protocol.ThinkingLevel, error) {
	f.record("set_thinking:" + string(level))
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.thinkingCap != "" && thinkingRank(level) > thinkingRank(f.thinkingCap) {
		level = f.thinkingCap
	}
	f.state.ThinkingLevel = level
	return level, nil
}

func thinkingRank(l protocol.ThinkingLevel) int {
	for i, v := range thinkingLevels {
		if v == l {
			return i
		}
	}
	return 0
}

func (f *fakeClient) Usage(context.Context) (protocol.ACPUsageResult, error) {
	f.record("usage")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.usage, nil
}

func (f *fakeClient) Close() error { return nil }

var _ Client = (*fakeClient)(nil)

// testClock is the clock of the model under test. Tests move it by hand, so
// elapsed times in goldens are exact.
type testClock struct{ ms atomic.Int64 }

// The base is a multiple of 800 ms, so the spinner frame of any whole-second
// offset that is a multiple of 4 s is the first frame.
const clockBaseMs = int64(1_760_000_000_000)

func newTestClock() *testClock {
	c := &testClock{}
	c.ms.Store(clockBaseMs)
	return c
}

func (c *testClock) Now() time.Time          { return time.UnixMilli(c.ms.Load()) }
func (c *testClock) advance(d time.Duration) { c.ms.Add(d.Milliseconds()) }

// viewRig runs the session view in a real Bubble Tea program.
type viewRig struct {
	t     *testing.T
	h     *screenHarness
	fc    *fakeClient
	m     *Model
	clock *testClock
}

type rigConfig struct {
	opts func(*Options)
	fc   *fakeClient
}

type rigOption func(*rigConfig)

func withFake(fc *fakeClient) rigOption { return func(c *rigConfig) { c.fc = fc } }
func withOptions(f func(*Options)) rigOption {
	return func(c *rigConfig) { c.opts = f }
}

// startView starts the model at a size and waits until the editor shows.
func startView(t *testing.T, width, height int, ro ...rigOption) *viewRig {
	t.Helper()
	cfg := &rigConfig{fc: newFakeClient()}
	for _, o := range ro {
		o(cfg)
	}
	clock := newTestClock()
	opts := Options{
		Client:       cfg.fc,
		Profile:      colorprofile.NoTTY,
		Cwd:          "/home/u/src/askcore",
		Home:         "/home/u",
		Version:      "0.1.0",
		Location:     time.UTC,
		Now:          clock.Now,
		TickInterval: -1,
		SettleDelay:  30 * time.Millisecond,
	}
	if cfg.opts != nil {
		cfg.opts(&opts)
	}
	m := New(opts)
	r := &viewRig{t: t, fc: cfg.fc, m: m, clock: clock}
	r.h = startScreen(t, m, width, height)
	t.Cleanup(func() { close(cfg.fc.events) })
	if opts.Client != nil {
		r.h.waitText("Ask anything")
		r.h.waitText("ask   v0.1.0")
	}
	return r
}

func (r *viewRig) key(names ...string) {
	r.t.Helper()
	for _, n := range names {
		r.h.send(keyNamed(r.t, n))
	}
}

func (r *viewRig) typeText(s string) {
	for _, c := range s {
		r.h.send(tea.KeyPressMsg{Code: c, Text: string(c)})
	}
}

// event delivers an agent event of the followed session.
func (r *viewRig) event(evs ...protocol.Event) {
	r.t.Helper()
	for _, ev := range evs {
		r.fc.push(SessionEventMsg{SessionID: r.fc.sessionID, Event: ev})
	}
	r.h.settle()
}

func (r *viewRig) waitCall(call string) {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if r.fc.has(call) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.t.Fatalf("call %q not made; calls: %v", call, r.fc.callList())
}

func (r *viewRig) waitGone(text string) {
	r.t.Helper()
	r.h.waitFor("no "+text, func(h *screenHarness) bool { return h.count(text) == 0 })
}

// exited waits until the program ends by itself and returns its result. The
// result is put back, so the cleanup of the harness does not wait for it.
func (r *viewRig) exited() runResult {
	r.t.Helper()
	select {
	case res := <-r.h.done:
		r.h.done <- res
		return res
	case <-time.After(10 * time.Second):
		r.t.Fatal("the program did not end")
		return runResult{}
	}
}

// stopped ends the program and returns the model, safe to read.
func (r *viewRig) stopped() *Model { return r.h.stop().(*Model) }

// ---- shared history ----

// sketchUsage is the usage of the first reply: the meta line shows the input
// and output counts; the footer shows the share of the context window that the
// whole message used (23,040 of 128,000 is 18%).
func sketchUsage() protocol.Usage {
	return protocol.Usage{Input: 12_400, Output: 1_800, TotalTokens: 23_040, Cost: protocol.Cost{Total: 4_000}}
}

func firstTurnEvents() []protocol.Event {
	return flat(
		&protocol.AgentStart{},
		userAt("why does TestLeaderStale fail on CI?", sketchTime(14, 2)),
		assistantStart("deepseek-v4.1-flash", 0),
		textParts(0, sketchReply[:60], sketchReply[60:]),
		assistantEndUsage("deepseek-v4.1-flash", protocol.StopStop, 4200, sketchUsage(), protocol.Text{Text: sketchReply}),
		&protocol.AgentEnd{}, &protocol.AgentSettled{},
	)
}

// firstTurn plays the history that the sketches share and waits until the
// footer shows its usage.
func (r *viewRig) firstTurn() {
	r.t.Helper()
	r.event(firstTurnEvents()...)
	r.h.waitText("18%")
}

var errNoLeader = errors.New("no leader")
