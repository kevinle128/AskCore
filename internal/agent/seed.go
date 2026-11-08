package agent

import (
	"errors"
	"fmt"

	"AskCore/internal/sessions"
	"AskCore/pkg/protocol"
)

var errSeedType = errors.New("agent: seed item needs a distinct, non-empty CustomType")

func validateSeed(seed []protocol.CustomMessage) error {
	seen := make(map[string]struct{}, len(seed))
	for _, m := range seed {
		if _, dup := seen[m.CustomType]; dup || m.CustomType == "" {
			return fmt.Errorf("%w: %q", errSeedType, m.CustomType)
		}
		seen[m.CustomType] = struct{}{}
	}
	return nil
}

// openSource opens the log of a fresh conversation and commits the seed to it.
// It returns the log with the error, so the caller can close it.
func (a *Agent) openSource() (*trackedLog, error) {
	log := newTrackedLog(a.newContext())
	return log, seedLog(log, a.cfg.Seed)
}

// seedLog appends the seed items that w does not hold yet, in one commit.
func seedLog(w sessions.Writer, seed []protocol.CustomMessage) error {
	held := customTypes(w.Entries())
	var entries []sessions.Entry
	for _, m := range seed {
		if _, ok := held[m.CustomType]; ok {
			continue
		}
		entries = append(entries, sessions.MessageEntry{Message: protocol.CloneMessage(m)})
	}
	if len(entries) == 0 {
		return nil
	}
	if _, err := w.Append(entries...); err != nil {
		return fmt.Errorf("agent: write session log: %w", err)
	}
	return nil
}

func customTypes(entries []sessions.Entry) map[string]struct{} {
	held := map[string]struct{}{}
	for _, e := range entries {
		if me, ok := e.(sessions.MessageEntry); ok {
			if t, ok := customTypeOf(me.Message); ok {
				held[t] = struct{}{}
			}
		}
	}
	return held
}

func customTypeOf(m protocol.Message) (string, bool) {
	switch v := m.(type) {
	case protocol.CustomMessage:
		return v.CustomType, true
	case *protocol.CustomMessage:
		return v.CustomType, true
	}
	return "", false
}

// hasConversation reports whether msgs hold a user or assistant message. A log
// with only seed items, or only other custom messages, has none.
func hasConversation(msgs []protocol.Message) bool {
	for _, m := range msgs {
		if r := m.Role(); r == protocol.RoleUser || r == protocol.RoleAssistant {
			return true
		}
	}
	return false
}
