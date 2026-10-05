package providers_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

func decl(name, params string) protocol.ToolDecl {
	return protocol.ToolDecl{Name: name, Description: name + " tool", Parameters: json.RawMessage(params)}
}

func sysTools(added []protocol.ToolDecl, removed ...string) protocol.SystemMessage {
	m := protocol.SystemMessage{ToolsAdded: added}
	for _, r := range removed {
		m.ToolsRemoved = append(m.ToolsRemoved, protocol.ToolRef{Name: r})
	}
	return m
}

func TestCurrentToolsReplaysSystemMessages(t *testing.T) {
	a, b, c := decl("a", `{}`), decl("b", `{}`), decl("c", `{}`)
	b2 := decl("b", `{"type":"object"}`)
	msgs := []protocol.Message{
		sysTools([]protocol.ToolDecl{a, b}),
		protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}},
		sysTools([]protocol.ToolDecl{c}, "a"),
		sysTools([]protocol.ToolDecl{b2}, "b"),
	}
	assert.Equal(t, []protocol.ToolDecl{c, b2}, providers.CurrentTools(msgs))
	assert.Empty(t, providers.CurrentTools(nil))
}

func TestToolChanges(t *testing.T) {
	a, b, c := decl("a", `{}`), decl("b", `{"type":"object","properties":{}}`), decl("c", `{}`)
	bSame := decl("b", `{ "properties":{}, "type":"object" }`)
	bNew := decl("b", `{"type":"string"}`)

	added, removed := providers.ToolChanges([]protocol.ToolDecl{a, b}, []protocol.ToolDecl{a, bSame})
	assert.Empty(t, added, "key order and spacing do not count as a change")
	assert.Empty(t, removed)

	added, removed = providers.ToolChanges([]protocol.ToolDecl{a, b}, []protocol.ToolDecl{bNew, c})
	assert.Equal(t, []protocol.ToolDecl{bNew, c}, added)
	assert.Equal(t, []protocol.ToolRef{{Name: "a"}, {Name: "b"}}, removed, "a changed declaration is removed and added")

	// Replaying the changes on top of the previous state gives the current one.
	msgs := []protocol.Message{
		sysTools([]protocol.ToolDecl{a, b}),
		protocol.SystemMessage{ToolsAdded: added, ToolsRemoved: removed},
	}
	assert.Equal(t, []protocol.ToolDecl{bNew, c}, providers.CurrentTools(msgs))
}
