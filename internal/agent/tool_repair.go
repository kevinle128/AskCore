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

// openTurn returns the position of the TurnOpened that no TurnClosed follows,
// or -1 when no turn is open.
func openTurn(entries []sessions.Entry) int {
	open := -1
	for i, e := range entries {
		switch e.(type) {
		case sessions.TurnOpened:
			open = i
		case sessions.TurnClosed:
			open = -1
		}
	}
	return open
}

// openCycle returns the ID of the cycle that no CycleClosed follows, or "".
func openCycle(entries []sessions.Entry) string {
	id := ""
	for _, e := range entries {
		switch v := e.(type) {
		case sessions.CycleOpened:
			id = v.CycleID
		case sessions.CycleClosed:
			if v.CycleID == id {
				id = ""
			}
		}
	}
	return id
}

// repairTools completes only a still-open turn that this run opened.
func repairTools(log sessions.Writer, start int) error {
	entries := log.Entries()
	open := openTurn(entries)
	if open < start || open < 0 {
		return nil
	}
	results := unfinishedToolResults(entries, open)
	if len(results) == 0 {
		return nil
	}
	if _, err := log.Append(results...); err != nil {
		return fmt.Errorf("agent: repair tool results: %w", err)
	}
	return nil
}

// closeOpenScopes ends the turn and the cycle that a process left open in log,
// as one commit: a result for each tool call of the turn that has none, then
// the end of the turn, then the end of the cycle. A log that no process left
// open is not changed.
func closeOpenScopes(log sessions.Writer) error {
	entries := log.Entries()
	var repair []sessions.Entry
	if open := openTurn(entries); open >= 0 {
		repair = append(repair, unfinishedToolResults(entries, open)...)
		repair = append(repair, sessions.TurnClosed{CycleID: entries[open].(sessions.TurnOpened).CycleID})
	}
	if id := openCycle(entries); id != "" {
		repair = append(repair, sessions.CycleClosed{CycleID: id, Reason: string(ReasonAborted), Cause: causeInterrupted})
	}
	if len(repair) == 0 {
		return nil
	}
	if _, err := log.Append(repair...); err != nil {
		return logWriteError(err)
	}
	return nil
}

// unfinishedToolResults returns an error result for each tool call after the
// turn that opened at position open and has no result: "outcome unknown" when
// the log holds the intent to run it, else "not started".
func unfinishedToolResults(entries []sessions.Entry, open int) []sessions.Entry {
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
	var out []sessions.Entry
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
			out = append(out, sessions.MessageEntry{Message: toolResultMessage(errorOutcome(c, text))})
		}
	}
	return out
}
