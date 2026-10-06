package sessions

import "AskCore/pkg/protocol"

// CommitRef says where an Append wrote: the entries of the call have the log
// positions Start up to, but not including, End.
type CommitRef struct {
	Start int
	End   int
}

// Writer is the session log that an Agent runs on. The Agent driver is the
// only caller of Append: the Agent has no append call, State returns copies,
// and listeners run after Append returns. Messages and Entries may be called
// from another goroutine than the one that appends.
type Writer interface {
	// Append writes entries as one atomic step. It rejects the whole call when
	// an entry cannot be encoded, and then the log does not change.
	Append(entries ...Entry) (CommitRef, error)
	// Messages returns the message entries only, as deep copies.
	Messages() []protocol.Message
	// Entries returns every entry, as deep copies.
	Entries() []Entry
	// LastSystemSnapshot returns a copy of the newest SystemSnapshot entry. It
	// lets the Agent decide at each run start whether the system header
	// changed, without a copy of the whole log.
	LastSystemSnapshot() (SystemSnapshot, bool)
	// Close releases what the writer holds. The Agent calls it once, when it
	// is disposed, after the last run has settled. Reads stay valid afterwards.
	Close() error
}
