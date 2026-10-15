package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"AskCore/internal/agent"
	"AskCore/internal/pipeline"
	"AskCore/internal/providers"
	"AskCore/internal/providers/faux"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// recorder logs every event and every handler call of a run in one ordered list.
// Handlers run on tool goroutines too, so it locks.
type recorder struct {
	mu     sync.Mutex
	log    []string
	events []protocol.Event
	failOn string
}

func (r *recorder) emit(ev protocol.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	l := label(ev)
	r.log = append(r.log, l)
	r.events = append(r.events, ev)
	if r.failOn != "" && l == r.failOn {
		return errEmit
	}
	return nil
}

func (r *recorder) note(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log = append(r.log, s)
}

func (r *recorder) labels() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.log...)
}

// eventLabels drops the hook notes and keeps the events.
func (r *recorder) eventLabels() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.events))
	for _, ev := range r.events {
		out = append(out, label(ev))
	}
	return out
}

func (r *recorder) toolEnds() map[string]*protocol.ToolExecutionEnd {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]*protocol.ToolExecutionEnd{}
	for _, ev := range r.events {
		if e, ok := ev.(*protocol.ToolExecutionEnd); ok {
			out[e.ToolCallID] = e
		}
	}
	return out
}

var errEmit = errors.New("listener failed")

func label(ev protocol.Event) string {
	switch e := ev.(type) {
	case *protocol.MessageStart:
		return "message_start(" + messageLabel(e.Message) + ")"
	case *protocol.MessageEnd:
		return "message_end(" + messageLabel(e.Message) + ")"
	case *protocol.MessageUpdate:
		return "message_update(" + e.AssistantMessageEvent.EventType() + ")"
	case *protocol.ToolExecutionStart:
		return "tool_execution_start(" + e.ToolCallID + ")"
	case *protocol.ToolExecutionUpdate:
		return "tool_execution_update(" + e.ToolCallID + ")"
	case *protocol.ToolExecutionEnd:
		return "tool_execution_end(" + e.ToolCallID + ")"
	default:
		return ev.EventType()
	}
}

func messageLabel(m protocol.Message) string {
	if r, ok := m.(protocol.ToolResultMessage); ok {
		return "toolResult:" + r.ToolCallID
	}
	return m.Role()
}

func newFaux(t testing.TB, opts ...faux.Option) (*faux.Provider, providers.Model) {
	t.Helper()
	p, err := faux.New(append([]faux.Option{faux.WithChunk(1000, 1000)}, opts...)...)
	require.NoError(t, err)
	m, ok := p.Model("faux-1")
	require.True(t, ok)
	return p, m
}

func registry(t testing.TB, ts ...tools.Tool) *tools.Registry {
	t.Helper()
	r := &tools.Registry{}
	for _, tool := range ts {
		require.NoError(t, r.Register(tool, tools.SourceInfo{Kind: "builtin", Name: tool.Decl().Name}))
	}
	return r
}

// Next continuations of the tool points, short for handler signatures.
type (
	nextBefore = pipeline.Next[pipeline.ToolCallInfo, *pipeline.BeforeToolCallResult]
	nextAfter  = pipeline.Next[pipeline.ToolResultInfo, *pipeline.AfterToolCallResult]
)

// registryOf returns the pipeline registry of c and creates it on first use.
func registryOf(c *agent.Config) *pipeline.Registry {
	if c.Pipeline == nil {
		c.Pipeline = pipeline.NewRegistry()
	}
	return c.Pipeline
}

func config(p *faux.Provider, m providers.Model, reg *pipeline.Registry) agent.LoopConfig {
	return agent.LoopConfig{Model: m, Stream: p.Stream, Pipeline: reg}
}

func user(text string) protocol.Message {
	return protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: text}}, Timestamp: 1}
}

func roles(msgs []protocol.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = messageLabel(m)
	}
	return out
}

func lastAssistant(t *testing.T, msgs []protocol.Message) protocol.AssistantMessage {
	t.Helper()
	for i := len(msgs) - 1; i >= 0; i-- {
		if a, ok := msgs[i].(protocol.AssistantMessage); ok {
			return a
		}
	}
	t.Fatal("no assistant message")
	return protocol.AssistantMessage{}
}

func assistantText(m protocol.AssistantMessage) string {
	var b strings.Builder
	for _, c := range m.Content {
		if t, ok := c.(protocol.Text); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

func resultText(blocks []protocol.UserBlock) string {
	var b strings.Builder
	for _, c := range blocks {
		if t, ok := c.(protocol.Text); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

// toolResults maps call id to the result message of each call in msgs.
func toolResults(msgs []protocol.Message) map[string]protocol.ToolResultMessage {
	out := map[string]protocol.ToolResultMessage{}
	for _, m := range msgs {
		if r, ok := m.(protocol.ToolResultMessage); ok {
			out[r.ToolCallID] = r
		}
	}
	return out
}

// funcTool is a test tool built from a schema and an execute function.
type funcTool struct {
	name    string
	schema  string
	run     func(ctx context.Context, tc tools.Context, args json.RawMessage) (protocol.ToolExecutionResult, error)
	prepare func(raw json.RawMessage) (json.RawMessage, error)
}

func (f *funcTool) Decl() protocol.ToolDecl {
	schema := f.schema
	if schema == "" {
		schema = `{"type":"object"}`
	}
	return protocol.ToolDecl{Name: f.name, Description: f.name, Parameters: json.RawMessage(schema)}
}

func (f *funcTool) Execute(ctx context.Context, tc tools.Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
	return f.run(ctx, tc, args)
}

// preparingTool adds PrepareArguments to a funcTool.
type preparingTool struct{ *funcTool }

func (p preparingTool) PrepareArguments(raw json.RawMessage) (json.RawMessage, error) {
	return p.prepare(raw)
}

// exclusiveTool makes the execution mode explicit.
type exclusiveTool struct{ *funcTool }

func (exclusiveTool) ConcurrencySafe(json.RawMessage) bool { return false }

func textResult(s string) protocol.ToolExecutionResult {
	return protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: s}}}
}

func waitOrFail(ctx context.Context, ch <-chan struct{}, what string) error {
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timed out waiting for %s", what)
	}
}

func within(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("did not finish within %s", d)
	}
}

func TestNoGoroutineLeak(t *testing.T) {
	cases := []struct {
		name  string
		setup func(p *faux.Provider) (context.Context, *pipeline.Registry, func())
	}{
		{"normal end", func(p *faux.Provider) (context.Context, *pipeline.Registry, func()) {
			p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"})), faux.Say("done"))
			return context.Background(), nil, func() {}
		}},
		{"abort", func(p *faux.Provider) (context.Context, *pipeline.Registry, func()) {
			ctx, cancel := context.WithCancel(context.Background())
			p.Set(faux.Say(strings.Repeat("slow words ", 50)).Pace(200))
			go func() { time.Sleep(20 * time.Millisecond); cancel() }()
			return ctx, nil, cancel
		}},
		{"handler error", func(p *faux.Provider) (context.Context, *pipeline.Registry, func()) {
			p.Set(faux.Reply(faux.ToolCall("echo", map[string]any{"text": "hi"})), faux.Say("done"))
			reg := pipeline.NewRegistry()
			reg.OnCompleteStep(func(context.Context, pipeline.Turn) (pipeline.TurnDecision, error) {
				return pipeline.Proceed, errors.New("finish failed")
			})
			return context.Background(), reg, func() {}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer goleak.VerifyNone(t)
			p, m := newFaux(t)
			ctx, reg, cancel := tc.setup(p)
			defer cancel()
			rec := &recorder{}
			_, _ = agent.Run(ctx, []protocol.Message{user("hi")},
				pipeline.AgentContext{Tools: registry(t, tools.Echo{})}, config(p, m, reg), rec.emit)
			require.NotEmpty(t, rec.eventLabels())
		})
	}
}
