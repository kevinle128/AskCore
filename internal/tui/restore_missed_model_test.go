package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// The client was away while the model changed twice. The leader names the end
// of the replay, so the two replayed events leave the footer row on the final
// model, also when the state answer shows it before the replay arrives. The
// pane draws no line for any change.
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
	r.h.waitFor("the final model in the footer row", func(*screenHarness) bool { return strings.Contains(r.footerOf(), "openai/gpt-5.5") })
	fc.push(
		SessionEventMsg{SessionID: testSessionID, Epoch: "e1", Seq: 11, Event: &protocol.ModelUpdate{Provider: "anthropic", ID: "claude-sonnet-4-6", API: "anthropic-messages"}},
		SessionEventMsg{SessionID: testSessionID, Epoch: "e1", Seq: 12, Event: &protocol.ModelUpdate{Provider: "openai", ID: "gpt-5.5", API: "responses"}},
	)
	r.h.settle()
	requireNoModelLine(t, r.content())
	require.Contains(t, r.footerOf(), "openai/gpt-5.5", r.screen())
	require.NotContains(t, r.footerOf(), "anthropic/", r.screen())

	// A change after the head is a real change: the footer row follows it, the
	// pane still draws no line.
	fc.push(SessionEventMsg{SessionID: testSessionID, Epoch: "e1", Seq: 13, Event: &protocol.ModelUpdate{Provider: "anthropic", ID: "claude-sonnet-4-6", API: "anthropic-messages"}})
	r.h.waitFor("the new model in the footer row", func(*screenHarness) bool { return strings.Contains(r.footerOf(), "anthropic/claude-sonnet-4-6") })
	requireNoModelLine(t, r.content())
}
