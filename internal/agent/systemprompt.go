package agent

import (
	"AskCore/internal/providers"
	"AskCore/internal/tools"
	"AskCore/pkg/protocol"
)

// initialSystemMessage is Pi's createInitialSystemMessage: one system message
// with timestamp 0 that holds the prompt and declares every tool, or nothing
// when both are empty. The tool set is fixed for the run; tool changes inside
// the log arrive with sessions.
func initialSystemMessage(prompt string, reg *tools.Registry) []protocol.Message {
	var decls []protocol.ToolDecl
	if reg != nil {
		decls = reg.Decls()
	}
	return providers.NormalizeRequest(providers.Request{SystemPrompt: prompt, Tools: decls}).Messages
}
