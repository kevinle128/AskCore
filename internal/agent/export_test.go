package agent

import (
	"context"

	"AskCore/internal/pipeline"
	"AskCore/internal/sessions"
	"AskCore/internal/tasks"
	"AskCore/pkg/protocol"
)

// LoggedRequest and RebuildRequest give the tests of package agent_test the
// rebuild of a logical request.
type LoggedRequest = loggedRequest

// RebuildRequest is rebuildRequest for tests.
var RebuildRequest = rebuildRequest

// Run runs the loop on its own, with a log in memory and no Agent around it.
// The loop-level tests use it to look at the loop without the run wrapper. The
// log is opened the way the Agent opens it: through openLog, with the snapshot
// of the header that the loop gets. That header is empty, as ac holds none.
func Run(ctx context.Context, prompts []protocol.Message, ac pipeline.AgentContext, cfg LoopConfig, emit Emit) ([]protocol.Message, error) {
	d, err := openLog(&sessions.MemoryLog{}, snapshotOf("", nil))
	if err != nil {
		return nil, err
	}
	return runLoop(ctx, prompts, ac, cfg, d, emit)
}

// Continue is Run for continueLoop.
func Continue(ctx context.Context, ac pipeline.AgentContext, cfg LoopConfig, emit Emit) ([]protocol.Message, error) {
	d, err := openLog(&sessions.MemoryLog{}, snapshotOf("", nil))
	if err != nil {
		return nil, err
	}
	return continueLoop(ctx, ac, cfg, d, emit)
}

// SetQuiescenceProbe sets the function that WaitForQuiescence calls between
// its checks and its second read of the work counter. A test uses it to force
// work into that window.
func (a *Agent) SetQuiescenceProbe(f func()) { a.quiescenceProbe = f }

// NoticesForTest returns the notice port of the current generation. Production
// code gets a port only from the run it belongs to.
func NoticesForTest(a *Agent) tasks.Port {
	a.mu.Lock()
	defer a.mu.Unlock()
	return noticePort{a: a, gen: a.taskGen}
}
