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

// openSource opens a log, closes the scopes that a dead process left open in
// it, and commits the seed items that it lacks. It returns the log with the
// error, so the caller can close it; the log is nil when it did not open. The
// selection is what the log holds of the model and the thinking level.
func (a *Agent) openSource() (*trackedLog, loggedSelection, error) {
	w, err := a.newContext()
	if err != nil {
		return nil, loggedSelection{}, fmt.Errorf("agent: open session log: %w", err)
	}
	log := newTrackedLog(w)
	if err := closeOpenScopes(log); err != nil {
		return log, loggedSelection{}, err
	}
	if err := seedLog(log, a.cfg.Seed); err != nil {
		return log, loggedSelection{}, err
	}
	return log, selectionOf(log.Entries()), nil
}

// seedLog appends the seed items that w lacks, in one commit. A seed item
// counts as held when the newest logged item of its type has the same content.
// An item that changed since it was logged is appended after the log: the
// logged items and the cache prefix of the history stay, and the model gets
// the new text.
func seedLog(w sessions.Writer, seed []protocol.CustomMessage) error {
	latest := latestCustom(w.Entries())
	var entries []sessions.Entry
	for _, m := range seed {
		if held, ok := latest[m.CustomType]; ok && sameSeedContent(held, m) {
			continue
		}
		entries = append(entries, sessions.MessageEntry{Message: protocol.CloneMessage(m)})
	}
	if len(entries) == 0 {
		return nil
	}
	if _, err := w.Append(entries...); err != nil {
		return logWriteError(err)
	}
	return nil
}

// latestCustom returns the newest logged custom message of each type.
func latestCustom(entries []sessions.Entry) map[string]protocol.CustomMessage {
	latest := map[string]protocol.CustomMessage{}
	for _, e := range entries {
		if me, ok := e.(sessions.MessageEntry); ok {
			if c, ok := customOf(me.Message); ok {
				latest[c.CustomType] = c
			}
		}
	}
	return latest
}

// sameSeedContent compares two seed items without their timestamp, which the
// composition sets at each start.
func sameSeedContent(a, b protocol.CustomMessage) bool {
	a.Timestamp, b.Timestamp = 0, 0
	return sameMessage(a, b)
}

// logWriteError names a failed log write. A lost writer gets a text that tells
// the user what to do, because the session file cannot take any more entries.
func logWriteError(err error) error {
	if errors.Is(err, sessions.ErrWriterLost) {
		return fmt.Errorf("agent: the session file can no longer be written and must be reopened; run with `--no-session` to run without saving: %w", err)
	}
	return fmt.Errorf("agent: write session log: %w", err)
}

func customOf(m protocol.Message) (protocol.CustomMessage, bool) {
	switch v := m.(type) {
	case protocol.CustomMessage:
		return v, true
	case *protocol.CustomMessage:
		if v != nil {
			return *v, true
		}
	}
	return protocol.CustomMessage{}, false
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
