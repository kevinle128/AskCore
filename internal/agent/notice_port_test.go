package agent_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/tasks"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// gateTool blocks its call until release closes. It closes entered when the
// body starts. With obey it also returns when the call is cancelled, as a
// cooperative tool does; without it the body ignores the cancel, so the run
// stays active while it drains.
func gateTool(entered, release chan struct{}, obey bool) *funcTool {
	var once sync.Once
	return &funcTool{name: "gate", run: func(ctx context.Context, _ tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		once.Do(func() { close(entered) })
		if obey {
			select {
			case <-release:
			case <-ctx.Done():
			}
		} else {
			<-release
		}
		return textResult("gate done"), nil
	}}
}

func gateCall(id string) faux.Block { return faux.ToolCall("gate", map[string]any{}, faux.ID(id)) }

// promptAsync runs Prompt on its own goroutine and returns its result channel.
func promptAsync(a *agent.Agent, text string) <-chan error {
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user(text)) }()
	return done
}

func TestNoticePortWakesIdleAgent(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("noticed"))
	a := newAgent(t, p, m, nil)
	defer func() { require.NoError(t, a.Dispose()) }()

	_, err := agent.NoticesForTest(a).Notify(tasks.NoticeWake, user("notice-1"))
	require.NoError(t, err)
	require.NoError(t, a.WaitForIdle(context.Background()))

	require.Equal(t, 1, p.Calls(), "the notice started one run")
	assert.Contains(t, requestUsers(p, 0), "notice-1")
}

func TestNoticePortJoinsRunAtNextTurnBoundary(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(gateCall("g1")), faux.Say("seen"))
	entered, release := make(chan struct{}), make(chan struct{})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, gateTool(entered, release, true)) })
	defer func() { require.NoError(t, a.Dispose()) }()

	done := promptAsync(a, "go")
	<-entered
	_, err := agent.NoticesForTest(a).Notify(tasks.NoticeWake, user("notice-2"))
	require.NoError(t, err)
	close(release)
	require.NoError(t, <-done)
	require.NoError(t, a.WaitForIdle(context.Background()))

	require.Equal(t, 2, p.Calls(), "the notice joined the open run and started no other")
	assert.Contains(t, requestUsers(p, 1), "notice-2")
}

func TestNoticePortRefusedAfterReset(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("fresh"))
	a := newAgent(t, p, m, nil)
	defer func() { require.NoError(t, a.Dispose()) }()

	old := agent.NoticesForTest(a)
	require.NoError(t, a.Reset())

	_, err := old.Notify(tasks.NoticeWake, user("stale"))
	require.ErrorIs(t, err, tasks.ErrNotDelivered)
	assert.False(t, old.Remove("any"), "an old port removes nothing")
	assert.True(t, isClosed(a.Idle()), "a refused notice starts no run")

	fresh := agent.NoticesForTest(a)
	require.NotEqual(t, old.Gen(), fresh.Gen())
	_, err = fresh.Notify(tasks.NoticeWake, user("notice-3"))
	require.NoError(t, err)
	require.NoError(t, a.WaitForIdle(context.Background()))
	require.Equal(t, 1, p.Calls())
	assert.NotContains(t, requestUsers(p, 0), "stale")
}

func TestNoticePortBypassesQueueCap(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(gateCall("g1")), faux.Say("after batch"), faux.Say("follow-up cycle"))
	entered, release := make(chan struct{}), make(chan struct{})
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Tools = registry(t, gateTool(entered, release, true))
		c.MaxQueuedInputs = 1
	})
	defer func() { require.NoError(t, a.Dispose()) }()

	done := promptAsync(a, "go")
	<-entered
	_, err := a.FollowUp(user("human-1"))
	require.NoError(t, err)
	_, err = a.FollowUp(user("human-2"))
	require.ErrorIs(t, err, agent.ErrQueueFull, "the human queue is full")
	_, err = agent.NoticesForTest(a).Notify(tasks.NoticeWake, user("notice-4"))
	require.NoError(t, err, "a notice does not count toward the bound")
	close(release)
	require.NoError(t, <-done)
	require.NoError(t, a.WaitForIdle(context.Background()))

	assert.Contains(t, requestUsers(p, 1), "notice-4")
}

func TestNoticePortNoticeSurvivesAbort(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(gateCall("g1")), faux.Say("next"))
	entered, release := make(chan struct{}), make(chan struct{})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, gateTool(entered, release, true)) })
	defer func() { require.NoError(t, a.Dispose()) }()

	done := promptAsync(a, "go")
	<-entered
	_, err := agent.NoticesForTest(a).Notify(tasks.NoticeWake, user("notice-5"))
	require.NoError(t, err)
	a.Abort()
	require.NoError(t, <-done)
	require.NoError(t, a.WaitForIdle(context.Background()))
	require.Equal(t, 1, p.Calls(), "Abort started no run for the notice")

	require.NoError(t, a.Prompt(context.Background(), user("next")))
	assert.Contains(t, requestUsers(p, 1), "notice-5", "the notice rides with the next input")
}

func TestNoticePortRemoveDeletesOnlyNotices(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(gateCall("g1")), faux.Say("after batch"), faux.Say("follow-up cycle"))
	entered, release := make(chan struct{}), make(chan struct{})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, gateTool(entered, release, true)) })
	defer func() { require.NoError(t, a.Dispose()) }()
	port := agent.NoticesForTest(a)

	done := promptAsync(a, "go")
	<-entered
	humanID, err := a.FollowUp(user("human-6"))
	require.NoError(t, err)
	noticeID, err := port.Notify(tasks.NoticeWake, user("notice-6"))
	require.NoError(t, err)
	assert.False(t, port.Remove(humanID), "the port never removes human input")
	assert.True(t, port.Remove(noticeID))
	close(release)
	require.NoError(t, <-done)
	require.NoError(t, a.WaitForIdle(context.Background()))

	require.Equal(t, 3, p.Calls())
	for i := range 3 {
		assert.NotContains(t, requestUsers(p, i), "notice-6")
	}
	assert.Contains(t, requestUsers(p, 2), "human-6", "the human input stayed queued")
}

func TestNoticeAfterAbortWaitsForHumanInput(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(gateCall("g1")), faux.Say("next"))
	entered, release := make(chan struct{}), make(chan struct{})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, gateTool(entered, release, true)) })
	defer func() { require.NoError(t, a.Dispose()) }()

	done := promptAsync(a, "go")
	<-entered
	a.Abort()
	require.NoError(t, <-done)
	require.NoError(t, a.WaitForIdle(context.Background()))

	_, err := agent.NoticesForTest(a).Notify(tasks.NoticeWake, user("notice-7"))
	require.NoError(t, err)
	assert.True(t, isClosed(a.Idle()), "a notice after Abort starts no run")
	require.Equal(t, 1, p.Calls())

	require.NoError(t, a.Prompt(context.Background(), user("next")))
	assert.Contains(t, requestUsers(p, 1), "notice-7", "the notice rides with the next human input")
}

func TestNoticeDuringAbortDrainDoesNotStartRun(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(gateCall("g1")), faux.Say("next"))
	entered, release := make(chan struct{}), make(chan struct{})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, gateTool(entered, release, false)) })
	defer func() { require.NoError(t, a.Dispose()) }()

	done := promptAsync(a, "go")
	<-entered
	a.Abort()
	// The body ignores the cancel, so the aborted run is still draining.
	_, err := agent.NoticesForTest(a).Notify(tasks.NoticeWake, user("notice-8"))
	require.NoError(t, err)
	close(release)
	require.NoError(t, <-done)
	require.NoError(t, a.WaitForIdle(context.Background()))

	assert.True(t, isClosed(a.Idle()), "the end of the aborted run starts no run for the notice")
	require.Equal(t, 1, p.Calls())
	require.NoError(t, a.Prompt(context.Background(), user("next")))
	assert.Contains(t, requestUsers(p, 1), "notice-8")
}

func TestHumanInputClearsInterrupt(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(gateCall("g1")), faux.Say("next"), faux.Say("noticed"))
	entered, release := make(chan struct{}), make(chan struct{})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, gateTool(entered, release, true)) })
	defer func() { require.NoError(t, a.Dispose()) }()

	done := promptAsync(a, "go")
	<-entered
	a.Abort()
	require.NoError(t, <-done)
	require.NoError(t, a.WaitForIdle(context.Background()))
	require.NoError(t, a.Prompt(context.Background(), user("next")))

	_, err := agent.NoticesForTest(a).Notify(tasks.NoticeWake, user("notice-9"))
	require.NoError(t, err)
	require.NoError(t, a.WaitForIdle(context.Background()))
	require.Equal(t, 3, p.Calls(), "after human input a notice starts a run again")
	assert.Contains(t, requestUsers(p, 2), "notice-9")
}

func TestNoticePortWithoutModelQueuesWithoutRun(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("hello"))
	// SetModel checks the key of the model it switches to.
	a := newAgent(t, p, m, func(c *agent.Config) { c.Options.APIKey = "test-key" })
	defer func() { require.NoError(t, a.Dispose()) }()

	require.True(t, a.ClearModel())
	_, err := agent.NoticesForTest(a).Notify(tasks.NoticeWake, user("notice-10"))
	require.NoError(t, err)
	assert.True(t, isClosed(a.Idle()), "an Agent with no model starts no run")
	require.Equal(t, 0, p.Calls())

	require.NoError(t, a.SetModel(context.Background(), m))
	require.NoError(t, a.Prompt(context.Background(), user("go")))
	assert.Contains(t, requestUsers(p, 0), "notice-10")
}

func TestToolGetsNoticePortOfItsRun(t *testing.T) {
	p, m := newFaux(t)
	p.Set(
		faux.Reply(faux.ToolCall("gen", map[string]any{}, faux.ID("g1"))), faux.Say("first"),
		faux.Reply(faux.ToolCall("gen", map[string]any{}, faux.ID("g2"))), faux.Say("second"),
	)
	var seen []uint64
	tool := &funcTool{name: "gen", run: func(_ context.Context, tc tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		require.NotNil(t, tc.Notices)
		seen = append(seen, tc.Notices.Gen())
		return textResult("ok"), nil
	}}
	a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, tool) })
	defer func() { require.NoError(t, a.Dispose()) }()

	first := agent.NoticesForTest(a).Gen()
	require.NoError(t, a.Prompt(context.Background(), user("one")))
	require.NoError(t, a.Reset())
	second := agent.NoticesForTest(a).Gen()
	require.NoError(t, a.Prompt(context.Background(), user("two")))

	require.NotEqual(t, first, second)
	assert.Equal(t, []uint64{first, second}, seen)
}

func TestExecuteToolHandlerCannotSwapNoticePort(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Reply(faux.ToolCall("probe", map[string]any{}, faux.ID("p1"))), faux.Say("done"))
	var got tasks.Port
	tool := &funcTool{name: "probe", run: func(_ context.Context, tc tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		got = tc.Notices
		return textResult("ok"), nil
	}}
	h := pipeline.NewRegistry()
	h.OnExecuteTool(func(ctx context.Context, in pipeline.ExecuteToolInput, next pipeline.Next[pipeline.ExecuteToolInput, protocol.ToolExecutionResult]) (protocol.ToolExecutionResult, error) {
		in.Context.Notices = nil
		return next(ctx, in)
	})
	a := newAgent(t, p, m, func(c *agent.Config) { c.Tools = registry(t, tool); c.Pipeline = h })
	defer func() { require.NoError(t, a.Dispose()) }()

	require.NoError(t, a.Prompt(context.Background(), user("go")))
	require.NotNil(t, got, "the body still sees the port of its run")
	assert.Equal(t, agent.NoticesForTest(a).Gen(), got.Gen())
}

func TestNoticePortKeepsUserMessageShape(t *testing.T) {
	p, m := newFaux(t)
	p.Set(faux.Say("noticed"))
	log := &sessions.MemoryLog{}
	a := newAgent(t, p, m, func(c *agent.Config) { c.NewContext = func() sessions.Writer { return log } })
	defer func() { require.NoError(t, a.Dispose()) }()

	text := protocol.SystemReminder("Background job bash-1 finished")
	msg := protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: text}}, Timestamp: fixedMillis}
	_, err := agent.NoticesForTest(a).Notify(tasks.NoticeWake, msg)
	require.NoError(t, err)
	require.NoError(t, a.WaitForIdle(context.Background()))

	var logged []protocol.UserMessage
	for _, m := range log.Messages() {
		if u, ok := m.(protocol.UserMessage); ok {
			logged = append(logged, u)
		}
	}
	require.Len(t, logged, 1, "the log keeps the notice as a user message")
	assert.Equal(t, msg, logged[0])
	assert.Contains(t, requestUsers(p, 0), text, "the model gets the same text")
}
