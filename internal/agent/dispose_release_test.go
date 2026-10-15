package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

func TestDisposeRunsReleaseAfterToolsDrain(t *testing.T) {
	boom := errors.New("release failed")
	closeErr := errors.New("log close failed")
	p, m := newFaux(t)
	p.Set(faux.Reply(call("t", "a", nil)))
	started := make(chan struct{})
	var toolReturned, releaseSawReturn, logClosedBeforeRelease atomic.Bool
	log := &closingLog{MemoryLog: &sessions.MemoryLog{}, err: closeErr}
	tool := &funcTool{name: "t", run: func(ctx context.Context, _ tools.Context, _ json.RawMessage) (protocol.ToolExecutionResult, error) {
		close(started)
		<-ctx.Done()
		toolReturned.Store(true)
		return textResult("late"), nil
	}}
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Tools = registry(t, tool)
		c.NewContext = func() sessions.Writer { return log }
		c.Release = func() error {
			releaseSawReturn.Store(toolReturned.Load())
			logClosedBeforeRelease.Store(log.closes.Load() > 0)
			return boom
		}
	})
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), user("go")) }()
	<-started

	err := a.Dispose()

	require.ErrorIs(t, err, boom, "the release error joins the dispose error")
	require.ErrorIs(t, err, closeErr, "the log close error is kept")
	assert.True(t, releaseSawReturn.Load(), "release runs after the started tool returned")
	assert.False(t, logClosedBeforeRelease.Load(), "release runs before the log closes")
	<-done
}

func TestDisposeCallsReleaseOnce(t *testing.T) {
	p, m := newFaux(t)
	var calls atomic.Int32
	a := newAgent(t, p, m, func(c *agent.Config) {
		c.Release = func() error { calls.Add(1); return nil }
	})

	require.NoError(t, a.Dispose())
	require.NoError(t, a.Dispose())

	assert.EqualValues(t, 1, calls.Load())
}
