package agent

import (
	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
	"fmt"
)

type callKey struct {
	assistant int
	id        string
}

// repairTools completes only a still-open turn that this run opened.
func repairTools(log sessions.Writer, start int) error {
	entries := log.Entries()
	open := -1
	for i, e := range entries {
		switch e.(type) {
		case sessions.TurnOpened:
			open = i
		case sessions.TurnClosed:
			open = -1
		}
	}
	if open < start || open < 0 {
		return nil
	}
	recorded := map[callKey]bool{}
	results := map[callKey]int{}
	pending := map[string][]callKey{}
	for i := open + 1; i < len(entries); i++ {
		switch e := entries[i].(type) {
		case sessions.ToolCall:
			recorded[callKey{e.AssistantEntry, e.CallID}] = true
		case sessions.MessageEntry:
			switch m := messageValue(e.Message).(type) {
			case protocol.AssistantMessage:
				for _, c := range toolCalls(m) {
					pending[c.ID] = append(pending[c.ID], callKey{i, c.ID})
				}
			case protocol.ToolResultMessage:
				keys := pending[m.ToolCallID]
				if len(keys) > 0 {
					results[keys[0]]++
					pending[m.ToolCallID] = keys[1:]
				}
			}
		}
	}
	for i := open + 1; i < len(entries); i++ {
		e, ok := entries[i].(sessions.MessageEntry)
		if !ok {
			continue
		}
		a, ok := messageValue(e.Message).(protocol.AssistantMessage)
		if !ok {
			continue
		}
		for _, c := range toolCalls(a) {
			key := callKey{i, c.ID}
			if results[key] > 0 {
				results[key]--
				continue
			}
			text := "not started"
			if recorded[key] {
				text = "outcome unknown"
			}
			if _, err := log.Append(sessions.MessageEntry{Message: toolResultMessage(errorOutcome(c, text))}); err != nil {
				return fmt.Errorf("agent: repair tool results: %w", err)
			}
		}
	}
	return nil
}
