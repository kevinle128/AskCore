package agent

import (
	"slices"
	"time"

	"AskCore/internal/providers"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// declareToolChanges tells the model about a changed tool set before the
// next request. It is Pi's declareToolChanges.
//
// committed is the context so far; snap is the tool set this turn can run.
// The tools the transcript declares are replayed from the system messages.
// When they differ from snap, the difference becomes ToolsAdded and
// ToolsRemoved on a system message in pending. A pending system message gets
// the difference in place of its own tool fields, so a replay of the
// transcript always gives exactly snap. Otherwise a new system message goes
// before the first non-system pending message. With no difference, pending
// comes back unchanged.
func declareToolChanges(committed []protocol.Message, snap *tools.Snapshot, pending []protocol.Message) []protocol.Message {
	sysAt := -1
	for i := len(pending) - 1; i >= 0; i-- {
		if _, ok := pending[i].(protocol.SystemMessage); ok {
			sysAt = i
			break
		}
	}
	baseline := pending
	var pendingSys protocol.SystemMessage
	if sysAt >= 0 {
		pendingSys = pending[sysAt].(protocol.SystemMessage)
		baseline = slices.Clone(pending)
		baseline[sysAt] = withToolChanges(pendingSys, nil, nil)
	}
	declared := providers.CurrentTools(append(slices.Clip(committed), baseline...))
	added, removed := providers.ToolChanges(declared, snap.Decls())
	unchanged := len(added) == 0 && len(removed) == 0

	if sysAt >= 0 {
		if unchanged && len(pendingSys.ToolsAdded) == 0 && len(pendingSys.ToolsRemoved) == 0 {
			return pending
		}
		baseline[sysAt] = withToolChanges(pendingSys, added, removed)
		return baseline
	}
	if unchanged {
		return pending
	}
	update := protocol.SystemMessage{Content: []protocol.Text{}, ToolsAdded: added, ToolsRemoved: removed, Timestamp: time.Now().UnixMilli()}
	at := slices.IndexFunc(pending, func(m protocol.Message) bool {
		_, ok := m.(protocol.SystemMessage)
		return !ok
	})
	if at < 0 {
		at = len(pending)
	}
	return slices.Insert(slices.Clone(pending), at, protocol.Message(update))
}

// withToolChanges copies m with its tool fields replaced.
func withToolChanges(m protocol.SystemMessage, added []protocol.ToolDecl, removed []protocol.ToolRef) protocol.SystemMessage {
	m.ToolsAdded, m.ToolsRemoved = added, removed
	return m
}
