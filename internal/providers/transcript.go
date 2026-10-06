package providers

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"

	"AskCore/pkg/protocol"
)

// CurrentTools replays the tool fields of every system message in msgs and
// returns the tools the model may call after them, in name order, so the
// order of the declarations never reaches a request (DeepSeek tool-order.spec.ts).
// A removal drops a tool; a later addition of the same name replaces it.
// It is Pi's getCurrentTools, with the name order.
func CurrentTools(msgs []protocol.Message) []protocol.ToolDecl {
	var out []protocol.ToolDecl
	for _, m := range msgs {
		var sys protocol.SystemMessage
		switch v := m.(type) {
		case protocol.SystemMessage:
			sys = v
		case *protocol.SystemMessage:
			if v == nil {
				continue
			}
			sys = *v
		default:
			continue
		}
		for _, r := range sys.ToolsRemoved {
			out = deleteTool(out, r.Name)
		}
		for _, t := range sys.ToolsAdded {
			if i := toolIndex(out, t.Name); i >= 0 {
				out[i] = t
			} else {
				out = append(out, t)
			}
		}
	}
	slices.SortStableFunc(out, func(a, b protocol.ToolDecl) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// ToolChanges compares two complete tool states. A changed declaration is a
// removal plus an addition, so a replay of previous followed by the changes
// gives current. It is Pi's getToolStateChanges.
func ToolChanges(previous, current []protocol.ToolDecl) (added []protocol.ToolDecl, removed []protocol.ToolRef) {
	for _, t := range current {
		if i := toolIndex(previous, t.Name); i < 0 || !SameToolDecl(previous[i], t) {
			added = append(added, t)
		}
	}
	for _, t := range previous {
		if i := toolIndex(current, t.Name); i < 0 || !SameToolDecl(t, current[i]) {
			removed = append(removed, protocol.ToolRef{Name: t.Name})
		}
	}
	return added, removed
}

// SameToolDecl reports whether two declarations show the model the same
// interface. Parameters compare as JSON values, so key order and spacing do
// not count.
func SameToolDecl(a, b protocol.ToolDecl) bool {
	if a.Name != b.Name || a.Description != b.Description {
		return false
	}
	if bytes.Equal(a.Parameters, b.Parameters) {
		return true
	}
	var av, bv any
	if json.Unmarshal(a.Parameters, &av) != nil || json.Unmarshal(b.Parameters, &bv) != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

func toolIndex(decls []protocol.ToolDecl, name string) int {
	for i, t := range decls {
		if t.Name == name {
			return i
		}
	}
	return -1
}

func deleteTool(decls []protocol.ToolDecl, name string) []protocol.ToolDecl {
	if i := toolIndex(decls, name); i >= 0 {
		return append(decls[:i:i], decls[i+1:]...)
	}
	return decls
}
