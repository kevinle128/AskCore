package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// The client was away while the model changed twice. The leader names the end
// of the replay, so the two replayed events add no change line, also when the
// state answer shows the final model before the replay arrives.
func TestRestoreMissedModelChangesDrawNoLine(t *testing.T) {
	fc := providerFake()
	fc.setModels(protocol.ACPModelsResult{Current: deepseekChat, Models: singleFormatModels(), UsableOnly: true})
	r := startView(t, 80, 24, withFake(fc))
	r.firstTurn()
	fc.push(SnapshotMsg{SessionID: testSessionID, Resumed: true, Cursor: protocol.ACPCursor{Epoch: "e1", Seq: 10}})
	r.h.settle()

	fc.setState(protocol.ACPStateResult{SessionID: testSessionID, ModelID: openaiGPT55})
	fc.push(DisconnectedMsg{Err: errNoLeader}, ConnectedMsg{Info: fc.Info()},
		SessionRestoredMsg{Session: SessionInfo{ID: testSessionID, Role: protocol.ACPRoleDriver}, Resumed: true},
		// A resumed follow keeps the cursor of the client; the head is where
		// the replay ends.
		SnapshotMsg{SessionID: testSessionID, Resumed: true,
			Cursor: protocol.ACPCursor{Epoch: "e1", Seq: 10}, Head: protocol.ACPCursor{Epoch: "e1", Seq: 12}},
	)
	// The state answer lands before the replay.
	r.h.waitText("model  openai/gpt-5.5")
	fc.push(
		SessionEventMsg{SessionID: testSessionID, Epoch: "e1", Seq: 11, Event: &protocol.ModelUpdate{Provider: "anthropic", ID: "claude-sonnet-4-6", API: "anthropic-messages"}},
		SessionEventMsg{SessionID: testSessionID, Epoch: "e1", Seq: 12, Event: &protocol.ModelUpdate{Provider: "openai", ID: "gpt-5.5", API: "responses"}},
	)
	r.h.settle()
	require.Zero(t, r.h.count("· was"), "no change line for a change that the state already holds:\n%s", r.screen())
	require.Zero(t, r.h.count("model  anthropic/"), r.screen())
	require.Equal(t, 1, r.h.count("model  openai/gpt-5.5"), r.screen())

	// A change after the head is a real change and draws its line.
	fc.push(SessionEventMsg{SessionID: testSessionID, Epoch: "e1", Seq: 13, Event: &protocol.ModelUpdate{Provider: "anthropic", ID: "claude-sonnet-4-6", API: "anthropic-messages"}})
	r.h.waitText("model  anthropic/claude-sonnet-4-6 · was openai/gpt-5.5")
}
