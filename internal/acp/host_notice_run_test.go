package acp

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/agent"
	"AskCore/internal/tasks"
	"AskCore/pkg/protocol"
)

// runStarts returns a write function that records the run ID of every
// agent_start, and a function that reads them.
func runStarts() (WriteEvent, func() []string) {
	var mu sync.Mutex
	var ids []string
	write := func(_ context.Context, _, _ string, ev protocol.Event) error {
		if ev.EventType() == protocol.TypeAgentStart {
			mu.Lock()
			ids = append(ids, ev.Env().RunID)
			mu.Unlock()
		}
		return nil
	}
	return write, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), ids...)
	}
}

// wakeNotice starts a run on an idle Agent, as a job notice does.
func wakeNotice(t *testing.T, s *Session) {
	t.Helper()
	_, err := s.agent.Notify(tasks.Notice{Policy: tasks.NoticeWake, Message: hostUser("notice")})
	require.NoError(t, err)
}

// A run that a notice starts between admission and the call must not be taken
// for the run of the call: the call waits for it to end and then runs its own.
func TestPromptDuringNoticeRunBindsOwnRun(t *testing.T) {
	write, starts := runStarts()
	h := hostFixture(t, nil, write)
	s, err := h.NewSession(hostDeadline(t), t.TempDir())
	require.NoError(t, err)

	calls := 0
	result, err := s.execute(hostDeadline(t), func(runCtx context.Context) error {
		calls++
		if calls == 1 {
			wakeNotice(t, s)
			return agent.ErrBusy
		}
		return s.agent.Prompt(runCtx, hostUser("hi"))
	})
	require.NoError(t, err)
	require.NoError(t, s.agent.WaitForIdle(hostDeadline(t)))

	ids := starts()
	require.Len(t, ids, 2, "the notice run, then the run of the call")
	assert.Equal(t, ids[1], result.RunID, "the call reports its own run")
	assert.NotEqual(t, ids[0], result.RunID, "the notice run is not the run of the call")
}

// A cancel that comes before the run of the call exists stops that run, even
// when a notice run ran in between.
func TestEarlyAbortStopsOwnRunNotNoticeRun(t *testing.T) {
	write, starts := runStarts()
	h := hostFixture(t, nil, write)
	s, err := h.NewSession(hostDeadline(t), t.TempDir())
	require.NoError(t, err)

	result, _ := s.execute(hostDeadline(t), func(runCtx context.Context) error {
		wakeNotice(t, s)
		require.NoError(t, s.agent.WaitForIdle(hostDeadline(t)))
		s.Abort()
		return s.agent.Prompt(runCtx, hostUser("hi"))
	})

	ids := starts()
	require.Len(t, ids, 2)
	assert.Equal(t, ids[1], result.RunID)
	assert.Equal(t, "aborted", result.Reason, "the early cancel reached the run of the call")
}
