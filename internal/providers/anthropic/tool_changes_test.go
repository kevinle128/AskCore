package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// The request lists the tools that the transcript declares after every tool
// delta, not only the tools of the first system message.
func TestStreamToolsFollowToolDeltas(t *testing.T) {
	params := json.RawMessage(`{"type":"object"}`)
	tool := func(name string) protocol.ToolDecl { return protocol.ToolDecl{Name: name, Parameters: params} }
	_, body, _, err := streamRaw(t, streamOnceOpts{
		tools: []protocol.ToolDecl{tool("a"), tool("b")},
		messages: []protocol.Message{
			protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
			protocol.SystemMessage{ToolsAdded: []protocol.ToolDecl{tool("c")}, ToolsRemoved: []protocol.ToolRef{{Name: "a"}}},
			&protocol.SystemMessage{ToolsAdded: []protocol.ToolDecl{tool("d")}},
		},
		sse: textSSE("end_turn"),
	})
	require.NoError(t, err)
	var names []string
	for _, item := range body["tools"].([]any) {
		names = append(names, item.(map[string]any)["name"].(string))
	}
	assert.Equal(t, []string{"b", "c", "d"}, names)
}
