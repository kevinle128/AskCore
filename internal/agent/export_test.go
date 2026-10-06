package agent

import (
	"context"

	"AskCore/internal/pipeline"
	"AskCore/internal/sessions"
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
