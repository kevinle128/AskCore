package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/app"
	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/settings"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

type lifecycleBodies struct {
	mu        sync.Mutex
	active    int
	maxActive int
	exclusive bool
	overlaps  bool
	started   chan struct{}
	release   chan struct{}
}

type lifecycleTool struct {
	name   string
	bodies *lifecycleBodies
	waits  bool
}

func (tool lifecycleTool) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{Name: tool.name, Description: "Lifecycle fixture tool", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (tool lifecycleTool) Execute(ctx context.Context, _ tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
	b := tool.bodies
	b.mu.Lock()
	b.overlaps = b.overlaps || b.exclusive || (!tool.waits && b.active != 0)
	b.active++
	b.maxActive = max(b.maxActive, b.active)
	b.exclusive = !tool.waits
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.active--
		b.exclusive = false
		b.mu.Unlock()
	}()
	if tool.waits {
		b.started <- struct{}{}
		select {
		case <-b.release:
		case <-ctx.Done():
			return protocol.ToolExecutionResult{}, ctx.Err()
		}
	}
	return protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: tool.name + " finished"}}}, nil
}

type safeLifecycleTool struct{ lifecycleTool }

func (safeLifecycleTool) ConcurrencySafe(json.RawMessage) bool { return true }

func lifecycleToolStream(names ...string) string {
	stream := sseToolUse(names[0])
	start := strings.Index(stream, "event:content_block_start")
	end := strings.Index(stream, "event:message_delta")
	block := stream[start:end]
	var blocks strings.Builder
	for i, name := range names {
		value := strings.ReplaceAll(block, `"index":0`, `"index":`+strconv.Itoa(i))
		value = strings.ReplaceAll(value, `"call_1"`, `"call_`+strconv.Itoa(i+1)+`"`)
		value = strings.ReplaceAll(value, `"name":"`+names[0]+`"`, `"name":"`+name+`"`)
		blocks.WriteString(value)
	}
	return stream[:start] + blocks.String() + stream[end:]
}

func lifecycleEnv(key string) string {
	if key == "ASK_ALIBABA_TOKEN_PLAN_API_KEY" {
		return "fixture-key"
	}
	return ""
}

func TestHeadlessRetryToolsSteeringFollowUpAndSlowListener(t *testing.T) {
	for _, durable := range []bool{false, true} {
		t.Run(map[bool]string{false: "headless constructor", true: "durable composition"}[durable], func(t *testing.T) {
			runLifecycleFixture(t, durable)
		})
	}
}

func runLifecycleFixture(t *testing.T, durable bool) {
	t.Helper()
	bodies := &lifecycleBodies{started: make(chan struct{}, 2), release: make(chan struct{})}
	safe := safeLifecycleTool{lifecycleTool{name: "safe_wait", bodies: bodies, waits: true}}
	exclusive := lifecycleTool{name: "exclusive", bodies: bodies}
	var requests [][]byte
	var delays []time.Duration
	rt := rtFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		requests = append(requests, body)
		switch len(requests) {
		case 1:
			return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"1"}}, Body: io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"rate_limit_error","message":"retry once"}}`)), Request: request}, nil
		case 2:
			return sseResponse(200, lifecycleToolStream("safe_wait", "safe_wait", "exclusive"))(request)
		case 3:
			return sseResponse(200, sseText("steering answer"))(request)
		default:
			return sseResponse(200, sseText("follow-up answer"))(request)
		}
	})
	options := options{provider: anthropic.ProviderID, model: anthropic.ModelID, transport: rt, wait: func(ctx context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return ctx.Err()
	}}
	var ag *agent.Agent
	var log *sessions.MemoryLog
	var err error
	if durable {
		log = &sessions.MemoryLog{}
		registry := providers.NewRegistry()
		registry.RegisterProvider(anthropic.New(anthropic.WithHTTPClient(&http.Client{Transport: rt})))
		toolRegistry := &tools.Registry{}
		for _, tool := range append(builtinTools(), safe, exclusive) {
			require.NoError(t, toolRegistry.Register(tool, tools.SourceInfo{Kind: tools.SourceExtension}))
		}
		store, storeErr := settings.NewAuthStore(t.TempDir())
		require.NoError(t, storeErr)
		service := &auth.Service{Store: store}
		cfg := app.BindAuth(agent.Config{
			Registry: registry, Tools: toolRegistry, NewContext: func() sessions.Writer { return log },
			LoopConfig: agent.LoopConfig{Model: anthropic.Model(), Options: providers.StreamOptions{APIKey: "fixture-key", Reasoning: protocol.ThinkingMedium}, Wait: options.wait},
		}, service, registry)
		ag, err = agent.New(cfg)
	} else {
		ag, err = newHeadlessAgent(options, lifecycleEnv, safe, exclusive)
	}
	require.NoError(t, err)
	firstStart := make(chan struct{}, 1)
	var observed []protocol.Event
	ag.Subscribe(func(event protocol.Event) error {
		observed = append(observed, event)
		if _, ok := event.(*protocol.ToolExecutionStart); ok {
			select {
			case firstStart <- struct{}{}:
			default:
			}
		}
		return nil
	})
	ag.Subscribe(func(protocol.Event) error { time.Sleep(time.Millisecond); return nil })
	queued := make(chan error, 1)
	go func() {
		<-firstStart
		_, steerErr := ag.Steer(protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "steer next turn"}}})
		_, followErr := ag.FollowUp(protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "follow next cycle"}}})
		queued <- errors.Join(steerErr, followErr)
		<-bodies.started
		<-bodies.started
		close(bodies.release)
	}()
	var stdout, stderr bytes.Buffer
	code := runHeadless(ag, []string{"run the tool batch"}, modeJSON, &stdout, &stderr, nil)
	require.NoError(t, <-queued)
	require.Equal(t, 0, code, stderr.String())
	require.Empty(t, stderr.String())
	require.Equal(t, []time.Duration{time.Second}, delays)
	require.Len(t, requests, 4)
	assert.JSONEq(t, string(requests[0]), string(requests[1]), "retry reconstructs the same request")
	assert.Contains(t, string(requests[2]), "steer next turn")
	assert.NotContains(t, string(requests[2]), "follow next cycle")
	assert.Contains(t, string(requests[3]), "follow next cycle")
	bodies.mu.Lock()
	assert.Equal(t, 2, bodies.maxActive, "both safe bodies overlap within the default pool limit")
	assert.False(t, bodies.overlaps, "the undeclared tool runs alone")
	assert.Zero(t, bodies.active)
	bodies.mu.Unlock()
	events := decodeJSONL(t, stdout.String())
	require.Len(t, events, len(observed), "the slow listener loses no JSON events")
	for i, event := range observed {
		encoded, encodeErr := protocol.EncodeEvent(event)
		require.NoError(t, encodeErr)
		wire, encodeErr := protocol.EncodeEvent(events[i])
		require.NoError(t, encodeErr)
		assert.JSONEq(t, string(encoded), string(wire), "event %d retains publication order", i)
	}
	assert.Equal(t, protocol.TypeAgentSettled, events[len(events)-1].EventType())
	var attempts []*protocol.AttemptStart
	var attemptTurns []int
	var turn int
	var ends []*protocol.AttemptEnd
	var cycles []*protocol.CycleStart
	var retrySequence []string
	for _, event := range observed {
		switch event := event.(type) {
		case *protocol.TurnStart:
			turn++
		case *protocol.AttemptStart:
			attempts = append(attempts, event)
			attemptTurns = append(attemptTurns, turn)
		case *protocol.AttemptEnd:
			ends = append(ends, event)
		case *protocol.CycleStart:
			cycles = append(cycles, event)
		case *protocol.AgentEnd:
			if event.WillRetry {
				retrySequence = append(retrySequence, "will retry")
			}
		case *protocol.AutoRetryStart:
			retrySequence = append(retrySequence, "retry start")
			assert.EqualValues(t, 1000, event.DelayMs)
		case *protocol.AutoRetryEnd:
			retrySequence = append(retrySequence, "retry end")
			assert.True(t, event.Success)
		}
	}
	require.Len(t, attempts, 4)
	require.Len(t, ends, 4)
	assert.Equal(t, attempts[0].CycleID, attempts[1].CycleID)
	assert.Equal(t, attemptTurns[0]+1, attemptTurns[1], "Pi wire turn_start opens for each attempt")
	assert.Greater(t, attemptTurns[2], attemptTurns[1], "steering enters the next turn")
	assert.Equal(t, "failed", ends[0].Outcome)
	assert.Equal(t, "completed", ends[1].Outcome)
	assert.Equal(t, []string{"will retry", "retry start", "retry end"}, retrySequence)
	require.Len(t, cycles, 2)
	assert.NotEqual(t, cycles[0].CycleID, cycles[1].CycleID)
	if log != nil {
		var settled []sessions.AttemptSettled
		var settledTurns []int
		var scheduled []sessions.RetryScheduled
		var opened []sessions.TurnOpened
		for _, entry := range log.Entries() {
			switch entry := entry.(type) {
			case sessions.AttemptSettled:
				settled = append(settled, entry)
				settledTurns = append(settledTurns, len(opened))
			case sessions.RetryScheduled:
				scheduled = append(scheduled, entry)
			case sessions.TurnOpened:
				opened = append(opened, entry)
			}
		}
		require.Len(t, settled, 4)
		assert.Equal(t, "failed", settled[0].Outcome)
		assert.Equal(t, "completed", settled[1].Outcome)
		assert.Equal(t, 1, settledTurns[0])
		assert.Equal(t, settledTurns[0], settledTurns[1], "no second durable TurnOpened occurs between failed and recovered attempts")
		require.Len(t, scheduled, 1)
		assert.Equal(t, settledTurns[0], scheduled[0].Turn)
		assert.Equal(t, opened[0].CycleID, scheduled[0].CycleID)
		assert.EqualValues(t, 1000, scheduled[0].DelayMs)
		assert.Equal(t, providers.CodeRateLimit, scheduled[0].Failure.Code)
		require.Len(t, opened, 3, "initial turn, steering turn, and follow-up turn")
		assert.Equal(t, opened[0].CycleID, opened[1].CycleID)
		assert.NotEqual(t, opened[1].CycleID, opened[2].CycleID)
	}
}

func TestHeadlessSIGINTDuringToolBatchDisposesAndExits130(t *testing.T) {
	tool := &cooperativeAbortTool{started: make(chan struct{}), returned: make(chan struct{})}
	var requests atomic.Int32
	ag, err := newHeadlessAgent(options{provider: anthropic.ProviderID, model: anthropic.ModelID, transport: counted(sseResponse(200, lifecycleToolStream(tool.Decl().Name, tool.Decl().Name)), &requests)}, lifecycleEnv, tool)
	require.NoError(t, err)
	var observed []protocol.Event
	ag.Subscribe(func(event protocol.Event) error {
		observed = append(observed, event)
		if _, ok := event.(*protocol.AgentDisposed); ok {
			select {
			case <-tool.returned:
			default:
				t.Error("Dispose completed before the invoked tool returned")
			}
		}
		return nil
	})
	signals := make(chan os.Signal, 1)
	var stdout, stderr bytes.Buffer
	exit := make(chan int, 1)
	go func() { exit <- runHeadless(ag, []string{"run tools"}, modeJSON, &stdout, &stderr, signals) }()
	select {
	case <-tool.started:
	case <-time.After(5 * time.Second):
		ag.Abort()
		t.Fatal("cooperative body did not start")
	}
	signals <- os.Interrupt
	select {
	case code := <-exit:
		assert.Equal(t, 130, code)
	case <-time.After(5 * time.Second):
		t.Fatal("SIGINT did not dispose the headless Agent")
	}
	require.Empty(t, stderr.String())
	assert.Equal(t, agent.Idle, ag.State().Status)
	assert.EqualValues(t, 1, requests.Load())
	assert.EqualValues(t, 1, tool.bodies.Load(), "the exclusive second body never starts during disposal")
	wire := decodeJSONL(t, stdout.String())
	assert.Equal(t, protocol.TypeAgentSettled, wire[len(wire)-1].EventType())
	var cycleEnds, toolEnds int
	for _, event := range wire {
		assert.NotEqual(t, protocol.TypeAgentDisposed, event.EventType())
		if _, ok := event.(*protocol.ToolExecutionEnd); ok {
			toolEnds++
		}
		if end, ok := event.(*protocol.CycleEnd); ok {
			cycleEnds++
			assert.Equal(t, "aborted", end.Reason)
			assert.Equal(t, "disposed", end.Cause)
		}
	}
	assert.Equal(t, 1, cycleEnds)
	assert.Equal(t, 2, toolEnds, "disposal gives both requested calls an outcome")
	require.GreaterOrEqual(t, len(observed), 2)
	assert.Equal(t, protocol.TypeAgentSettled, observed[len(observed)-2].EventType())
	assert.Equal(t, protocol.TypeAgentDisposed, observed[len(observed)-1].EventType())
}

func TestHeadlessPrintRetriesTransientFailureOnce(t *testing.T) {
	for _, mode := range []runMode{modePrint, modeJSON} {
		t.Run(map[runMode]string{modePrint: "print", modeJSON: "json"}[mode], func(t *testing.T) {
			provider, err := faux.New()
			require.NoError(t, err)
			provider.Set(faux.Fail("transient").Err(providers.NewFailure(providers.CodeRateLimit, 429, 0, "transient", nil)), faux.Say("recovered reply"))
			registry := providers.NewRegistry()
			registry.RegisterProvider(provider)
			model, ok := provider.Model("faux-1")
			require.True(t, ok)
			toolRegistry := &tools.Registry{}
			for _, tool := range builtinTools() {
				require.NoError(t, toolRegistry.Register(tool, tools.SourceInfo{Kind: tools.SourceBuiltin}))
			}
			log := &sessions.MemoryLog{}
			var delays []time.Duration
			ag, err := agent.New(agent.Config{Registry: registry, Tools: toolRegistry, NewContext: func() sessions.Writer { return log }, LoopConfig: agent.LoopConfig{Model: model, Wait: func(ctx context.Context, delay time.Duration) error { delays = append(delays, delay); return ctx.Err() }}})
			require.NoError(t, err)
			var stdout, stderr bytes.Buffer
			require.Equal(t, 0, runHeadless(ag, []string{"recover"}, mode, &stdout, &stderr, nil), stderr.String())
			require.Empty(t, stderr.String())
			assert.Equal(t, 2, provider.Calls())
			assert.Equal(t, []time.Duration{500 * time.Millisecond}, delays)
			var scheduled []sessions.RetryScheduled
			for _, entry := range log.Entries() {
				if retry, ok := entry.(sessions.RetryScheduled); ok {
					scheduled = append(scheduled, retry)
				}
			}
			require.Len(t, scheduled, 1)
			assert.Equal(t, providers.CodeRateLimit, scheduled[0].Failure.Code)
			assert.Equal(t, providers.DefaultRetryPolicy().Key, scheduled[0].PolicyKey)
			if mode == modePrint {
				assert.Equal(t, "recovered reply\n", stdout.String())
			} else {
				var starts int
				for _, event := range decodeJSONL(t, stdout.String()) {
					if _, ok := event.(*protocol.AutoRetryStart); ok {
						starts++
					}
				}
				assert.Equal(t, 1, starts)
				assert.Contains(t, stdout.String(), "recovered reply")
			}
		})
	}
}
