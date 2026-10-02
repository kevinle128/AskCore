package sessions

import (
	"slices"
	"sync"

	"AskCore/pkg/protocol"
)

// MemoryLog is a message log in memory, for a run that keeps no session file.
// The zero value is an empty log. It is safe for concurrent use.
type MemoryLog struct {
	mu   sync.Mutex
	msgs []protocol.Message
}

// Append adds m at the end of the log. It never fails.
func (l *MemoryLog) Append(m protocol.Message) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, m)
	return nil
}

// Messages returns the log in append order. The slice is a new copy; the
// messages share their content with the log.
func (l *MemoryLog) Messages() []protocol.Message {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.msgs)
}
