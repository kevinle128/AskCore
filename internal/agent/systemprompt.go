package agent

import (
	"slices"
	"strings"

	"AskCore/internal/providers"
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

// systemMessages is Pi's createInitialSystemMessage: one system message with
// timestamp 0 that holds the prompt and declares every tool of the snapshot,
// or nothing when both are empty. Tool changes inside the log arrive as
// messages of their own.
func systemMessages(snap sessions.SystemSnapshot) []protocol.Message {
	return providers.NormalizeRequest(providers.Request{SystemPrompt: snap.SystemPrompt, Tools: snap.Tools}).Messages
}

// sortedDecls sorts decls by tool name and returns them. The order of the
// registration never reaches a request, so two Agents that register the same
// tools in a different order send the same request.
func sortedDecls(decls []protocol.ToolDecl) []protocol.ToolDecl {
	slices.SortStableFunc(decls, func(a, b protocol.ToolDecl) int { return strings.Compare(a.Name, b.Name) })
	return decls
}
