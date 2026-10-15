package anthropic

import (
	"fmt"
	"strings"

	"AskCore/pkg/protocol"
)

var claudeToolNames = []string{"Read", "Write", "Edit", "Bash", "Grep", "Glob", "AskUserQuestion", "EnterPlanMode", "ExitPlanMode", "KillShell", "NotebookEdit", "Skill", "Task", "TaskOutput", "TodoWrite", "WebFetch", "WebSearch"}

type toolNames struct{ forward, reverse map[string]string }

func newToolNames(msgs []protocol.Message) (toolNames, error) {
	c := toolNames{map[string]string{}, map[string]string{}}
	for _, msg := range msgs {
		sys, ok := systemValue(msg)
		if !ok {
			continue
		}
		for _, removed := range sys.ToolsRemoved {
			wire, ok := c.forward[removed.Name]
			if ok {
				delete(c.reverse, wire)
				delete(c.forward, removed.Name)
			}
		}
		for _, t := range sys.ToolsAdded {
			wire := canonicalToolName(t.Name)
			if prior, ok := c.reverse[wire]; ok && prior != t.Name {
				return c, fmt.Errorf("tool names %q and %q collide on Anthropic wire", prior, t.Name)
			}
			c.forward[t.Name], c.reverse[wire] = wire, t.Name
		}
	}
	return c, nil
}

func canonicalToolName(name string) string {
	for _, known := range claudeToolNames {
		if strings.EqualFold(known, name) {
			return known
		}
	}
	return name
}

func (c toolNames) encode(name string) string {
	if wire, ok := c.forward[name]; ok {
		return wire
	}
	return name
}

func (c toolNames) decode(name string) string {
	if original, ok := c.reverse[name]; ok {
		return original
	}
	return name
}
