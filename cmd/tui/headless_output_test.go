package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/providers/faux"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// eventCounter counts the events of a run by type. It is subscribed before
// runHeadless, so it sees every event.
type eventCounter struct {
	mu     sync.Mutex
	n      map[string]int
	causes []string
}

func countEvents(ag *agent.Agent) *eventCounter {
	c := &eventCounter{n: map[string]int{}}
	ag.Subscribe(func(ev protocol.Event) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.n[ev.EventType()]++
		if e, ok := ev.(*protocol.CycleEnd); ok {
			c.causes = append(c.causes, e.Cause)
		}
		return nil
	})
	return c
}

func (c *eventCounter) cycleCauses() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.causes...)
}

func (c *eventCounter) get(typ string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n[typ]
}

// failingWriter fails every write with err and counts the calls.
type failingWriter struct {
	err   error
	calls atomic.Int32
}

func (f *failingWriter) Write([]byte) (int, error) {
	f.calls.Add(1)
	return 0, f.err
}

// slowWriter takes delay for each write. It keeps up with the run, but not at once.
type slowWriter struct {
	delay time.Duration
	mu    sync.Mutex
	buf   bytes.Buffer
}

func (s *slowWriter) Write(p []byte) (int, error) {
	time.Sleep(s.delay)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *slowWriter) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func TestJSONAbortWithSlowReaderStillWritesAgentSettled(t *testing.T) {
	ag := newAgent(t, "20")
	streaming := make(chan struct{})
	var once sync.Once
	ag.Subscribe(func(ev protocol.Event) error {
		if _, ok := ev.(*protocol.MessageUpdate); ok {
			once.Do(func() { close(streaming) })
		}
		return nil
	})
	w := &slowWriter{delay: 30 * time.Millisecond}
	sigs := make(chan os.Signal, 1)
	var errb bytes.Buffer
	codes := make(chan int, 1)

	go func() { codes <- runHeadless(ag, []string{longPrompt}, modeJSON, w, &errb, sigs) }()
	<-streaming
	sigs <- os.Interrupt

	select {
	case code := <-codes:
		assert.Equal(t, 130, code)
	case <-time.After(abortGrace + 3*time.Second):
		t.Fatal("runHeadless did not return after the signal")
	}
	evs := decodeJSONL(t, w.String())
	assert.Equal(t, "agent_settled", label(evs[len(evs)-1]), "the reader kept up inside the grace")
	assert.Equal(t, "", errb.String())
}

func TestJSONSignalWithFullPipeExitsWithinGrace(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	// Fill the pipe, and never read it.
	require.NoError(t, w.SetWriteDeadline(time.Now().Add(100*time.Millisecond)))
	chunk := bytes.Repeat([]byte("x"), 4096)
	for {
		if _, err := w.Write(chunk); err != nil {
			break
		}
	}
	require.NoError(t, w.SetWriteDeadline(time.Time{}))

	ag := newAgent(t, "")
	sigs := make(chan os.Signal, 1)
	var errb bytes.Buffer
	codes := make(chan int, 1)
	// Larger than the output buffer, so the driver blocks in a write.
	reply := strings.Repeat("a", 256<<10)

	go func() { codes <- runHeadless(ag, []string{reply}, modeJSON, w, &errb, sigs) }()
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, agent.Running, ag.State().Status, "the driver is blocked in a write")
	start := time.Now()
	sigs <- os.Interrupt

	select {
	case code := <-codes:
		assert.Equal(t, 130, code)
	case <-time.After(abortGrace + 2*time.Second):
		t.Fatal("runHeadless did not return within the grace")
	}
	assert.GreaterOrEqual(t, time.Since(start), abortGrace-100*time.Millisecond, "it waited for the grace")
	assert.Equal(t, agent.Running, ag.State().Status, "the driver is still blocked in the write")

	// The test closes the read end, which ends the blocked write.
	require.NoError(t, r.Close())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, ag.WaitForIdle(ctx))
	require.NoError(t, w.Close())
}

func TestJSONWriteFailureAbortsRun(t *testing.T) {
	ag := newAgent(t, "")
	c := countEvents(ag)
	w := &failingWriter{err: syscall.EPIPE}
	var errb bytes.Buffer

	// The first record is larger than the output buffer, so the failing write is
	// a direct one. Two prompts: the second must not start.
	code := runHeadless(ag, []string{bigPrompt, "second"}, modeJSON, w, &errb, nil)

	assert.Equal(t, 1, code)
	assert.Equal(t, "", errb.String(), "a closed pipe ends the run quietly")
	assert.Equal(t, 1, c.get("agent_start"), "the second prompt did not start")
	assert.Equal(t, 0, c.get("attempt_start"), "no model call after the write failed")
	assert.Equal(t, int32(1), w.calls.Load(), "the sticky error stops later writes")
	assert.Equal(t, []string{"output"}, c.cycleCauses(), "the cycle ended because the output failed, not by the user")
}

func TestJSONTimerFlushEPIPEAbortsRun(t *testing.T) {
	// A paced reply keeps the run alive for a while. Every event is small, so
	// the buffer never fills and the only write that reaches the target is the
	// timer flush.
	ag := newAgent(t, "50")
	c := countEvents(ag)
	w := &failingWriter{err: syscall.EPIPE}
	var errb bytes.Buffer

	code := runHeadless(ag, []string{"echo hello"}, modeJSON, w, &errb, nil)

	assert.Equal(t, 1, code)
	assert.Equal(t, "", errb.String())
	assert.LessOrEqual(t, c.get("attempt_start"), 1, "no second model call")
	assert.Equal(t, 0, c.get("tool_execution_start"), "the run stopped before the tool ran")
	assert.Equal(t, []string{"output"}, c.cycleCauses())
	assert.Equal(t, int32(1), w.calls.Load())
	st := ag.State()
	assert.Equal(t, agent.Idle, st.Status)
}

func TestProtocolOutReportsFirstErrorOnce(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(o *protocolOut)
	}{
		{"direct write", func(o *protocolOut) { _, _ = o.Write(bytes.Repeat([]byte("a"), 128<<10)) }},
		{"timer flush", func(o *protocolOut) {
			_, _ = o.Write([]byte("a\n"))
			time.Sleep(10 * flushDelay)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := newProtocolOut(&failingWriter{err: syscall.EPIPE})
			var got []error
			var mu sync.Mutex
			o.setOnError(func(err error) {
				mu.Lock()
				defer mu.Unlock()
				got = append(got, err)
			})

			tc.write(o)
			_, again := o.Write([]byte("b\n"))
			_ = o.flush()

			mu.Lock()
			defer mu.Unlock()
			assert.ErrorIs(t, again, syscall.EPIPE)
			require.Len(t, got, 1)
			assert.ErrorIs(t, got[0], syscall.EPIPE)
		})
	}
}

// badDetailsTool returns a result whose details are not JSON, so the event
// that carries it cannot be encoded.
type badDetailsTool struct{}

func (badDetailsTool) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{Name: "bad", Description: "bad details", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (badDetailsTool) Execute(context.Context, tools.Context, json.RawMessage) (protocol.ToolExecutionResult, error) {
	return protocol.ToolExecutionResult{
		Content: []protocol.UserBlock{protocol.Text{Text: "ok"}},
		Details: json.RawMessage("{not json"),
	}, nil
}

func TestJSONEncodeFailureAbortsRunWithExit1(t *testing.T) {
	p, err := faux.New()
	require.NoError(t, err)
	m, ok := p.Model("faux-1")
	require.True(t, ok)
	p.Set(faux.Reply(faux.ToolCall("bad", map[string]any{})), faux.Say("never"))
	reg := &tools.Registry{}
	require.NoError(t, reg.Register(badDetailsTool{}, tools.SourceInfo{Kind: tools.SourceExtension, Name: "test"}))
	ag, err := agent.New(agent.Config{
		LoopConfig: agent.LoopConfig{Model: m, Stream: p.Stream},
		Tools:      reg,
		SessionID:  "s1",
	})
	require.NoError(t, err)
	var out, errb bytes.Buffer

	code := runHeadless(ag, []string{"go", "second"}, modeJSON, &out, &errb, nil)

	assert.Equal(t, 1, code)
	assert.Equal(t, 1, p.Calls(), "no model call after the failed event")
	assert.Contains(t, errb.String(), "ToolExecutionResult", "stderr names the failure")
	assert.NotContains(t, out.String(), "second")
	assert.Equal(t, agent.Idle, ag.State().Status)
}
