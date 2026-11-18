package agent_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/providers/faux"
	"AskCore/internal/sessions"
)

// slowSecondLog makes a NewContext whose second call (the one of Reset) waits
// for release, like a log that does slow file I/O.
func slowSecondLog() (newContext func() (sessions.Writer, error), logs func() []*closingLog, entered <-chan struct{}, release func()) {
	var (
		mu   sync.Mutex
		all  []*closingLog
		in   = make(chan struct{})
		gate = make(chan struct{})
		once sync.Once
	)
	newContext = func() (sessions.Writer, error) {
		mu.Lock()
		l := &closingLog{MemoryLog: &sessions.MemoryLog{}}
		all = append(all, l)
		second := len(all) == 2
		mu.Unlock()
		if second {
			close(in)
			<-gate
		}
		return l, nil
	}
	logs = func() []*closingLog {
		mu.Lock()
		defer mu.Unlock()
		return append([]*closingLog(nil), all...)
	}
	return newContext, logs, in, func() { once.Do(func() { close(gate) }) }
}

func TestResetDoesNotHoldStateLockWhileOpeningLog(t *testing.T) {
	p, m := newFaux(t)
	newContext, _, entered, release := slowSecondLog()
	a := newAgent(t, p, m, func(c *agent.Config) { c.NewContext = newContext })
	t.Cleanup(release)

	done := make(chan error, 1)
	go func() { done <- a.Reset() }()
	require.NoError(t, waitOrFail(context.Background(), entered, "the log open"))

	stateRead := make(chan struct{})
	go func() {
		_ = a.State()
		close(stateRead)
	}()
	select {
	case <-stateRead:
	case <-time.After(5 * time.Second):
		t.Fatal("State waited for the log open of Reset")
	}

	release()
	require.NoError(t, <-done)
}

func TestResetClosesNewLogWhenARunStartsDuringOpen(t *testing.T) {
	p, m := newFaux(t, faux.WithChunk(2, 2))
	p.Set(faux.Say(strings.Repeat("slow words ", 50)).Pace(200))
	newContext, logs, entered, release := slowSecondLog()
	a := newAgent(t, p, m, func(c *agent.Config) { c.NewContext = newContext })
	t.Cleanup(release)

	resetDone := make(chan error, 1)
	go func() { resetDone <- a.Reset() }()
	require.NoError(t, waitOrFail(context.Background(), entered, "the log open"))

	promptDone := make(chan error, 1)
	go func() { promptDone <- a.Prompt(context.Background(), user("go")) }()
	require.Eventually(t, func() bool { return a.State().Status == agent.Running }, 5*time.Second, time.Millisecond)

	release()
	require.ErrorIs(t, <-resetDone, agent.ErrBusy)
	all := logs()
	require.Len(t, all, 2)
	assert.EqualValues(t, 1, all[1].closes.Load(), "the log that was never used is closed")
	assert.EqualValues(t, 0, all[0].closes.Load(), "the log of the running conversation stays open")

	a.Abort()
	require.NoError(t, <-promptDone)
}
