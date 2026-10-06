package sessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"AskCore/pkg/protocol"
)

// MemoryLog is a session log in memory, for a run that keeps no session file.
// The zero value is an empty log. It is safe for concurrent use. It keeps its
// own deep copy of every entry, and it gives out deep copies, so no caller
// shares memory with the log.
type MemoryLog struct {
	mu      sync.Mutex
	entries []Entry
	// snapshot is the index of the newest SystemSnapshot in entries; it counts
	// only while hasSnap is true.
	snapshot int
	hasSnap  bool
}

var _ Writer = (*MemoryLog)(nil)

// ErrUnencodableEntry is returned by Append for an entry that encoding/json
// cannot encode, or that holds no value.
var ErrUnencodableEntry = errors.New("sessions: entry cannot be encoded")

// Append adds entries at the end of the log, or none of them.
func (l *MemoryLog) Append(entries ...Entry) (CommitRef, error) {
	copies := make([]Entry, len(entries))
	for i, e := range entries {
		if err := checkEncodable(e); err != nil {
			return CommitRef{}, err
		}
		copies[i] = Clone(e)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ref := CommitRef{Start: len(l.entries), End: len(l.entries) + len(copies)}
	l.entries = append(l.entries, copies...)
	for i, e := range copies {
		if _, ok := e.(SystemSnapshot); ok {
			l.snapshot, l.hasSnap = ref.Start+i, true
		}
	}
	return ref, nil
}

// Messages returns the messages of the log in append order, as deep copies.
func (l *MemoryLog) Messages() []protocol.Message {
	l.mu.Lock()
	defer l.mu.Unlock()
	return MessagesOf(l.entries)
}

// Entries returns every entry in append order, as deep copies.
func (l *MemoryLog) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Entry, len(l.entries))
	for i, e := range l.entries {
		out[i] = Clone(e)
	}
	return out
}

// LastSystemSnapshot returns a copy of the newest SystemSnapshot entry.
func (l *MemoryLog) LastSystemSnapshot() (SystemSnapshot, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.hasSnap {
		return SystemSnapshot{}, false
	}
	return Clone(l.entries[l.snapshot]).(SystemSnapshot), true
}

// Close does nothing: a log in memory holds no resource. Reads stay valid.
func (l *MemoryLog) Close() error { return nil }

func checkEncodable(e Entry) error {
	if e == nil {
		return fmt.Errorf("%w: nil entry", ErrUnencodableEntry)
	}
	if m, ok := e.(MessageEntry); ok && m.Message == nil {
		return fmt.Errorf("%w: nil message", ErrUnencodableEntry)
	}
	if _, err := json.Marshal(e); err != nil {
		return fmt.Errorf("%w: %w", ErrUnencodableEntry, err)
	}
	return nil
}
