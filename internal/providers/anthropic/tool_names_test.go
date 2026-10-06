package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func TestOAuthToolNameAndChoice(t *testing.T) {
	msgs := providers.NormalizeRequest(providers.Request{Tools: []protocol.ToolDecl{{Name: "read", Parameters: json.RawMessage(`{"type":"object"}`)}}})
	opts := providers.StreamOptions{ToolChoice: "read", Auth: providers.AuthSnapshot{Method: "anthropic-oauth"}}
	doc, _, err := buildDocument(msgs.Messages, Model(), opts, nil)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(doc.body, &body))
	require.Equal(t, "Read", body["tools"].([]any)[0].(map[string]any)["name"])
	require.Equal(t, "Read", body["tool_choice"].(map[string]any)["name"])
	codec, err := newToolNames(msgs.Messages)
	require.NoError(t, err)
	require.Equal(t, "read", codec.decode("Read"))
}

func TestOAuthToolNameCollision(t *testing.T) {
	msgs := providers.NormalizeRequest(providers.Request{Tools: []protocol.ToolDecl{{Name: "read"}, {Name: "Read"}}})
	_, err := newToolNames(msgs.Messages)
	require.ErrorContains(t, err, "collide")
}

func TestOAuthHistoricalToolSnapshot(t *testing.T) {
	msgs := []protocol.Message{
		protocol.SystemMessage{ToolsAdded: []protocol.ToolDecl{{Name: "read"}}},
		protocol.AssistantMessage{Content: []protocol.AssistantBlock{protocol.ToolCall{ID: "one", Name: "read", Arguments: json.RawMessage(`{}`)}}},
		protocol.SystemMessage{ToolsRemoved: []protocol.ToolRef{{Name: "read"}}},
		protocol.AssistantMessage{Content: []protocol.AssistantBlock{protocol.ToolCall{ID: "two", Name: "read", Arguments: json.RawMessage(`{}`)}}},
	}
	blocks, err := encodeMessagesWithNames(msgs, true)
	require.NoError(t, err)
	require.Equal(t, "Read", blocks[0]["content"].([]map[string]any)[0]["name"])
	require.Equal(t, "read", blocks[1]["content"].([]map[string]any)[0]["name"])
}
