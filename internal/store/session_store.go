package store

import (
	"context"
	"errors"

	"AskCore/internal/sessions"
)

// CreateMode says when a new session reaches durable storage.
type CreateMode int

const (
	// CreateLazy keeps the entries in memory until the first assistant message
	// is appended, so a session that never gets a reply leaves nothing behind.
	CreateLazy CreateMode = iota
	// CreateNow stores the session at once, so its id can be opened later.
	CreateNow
)

var (
	// ErrSessionOwned means another writer holds the session.
	ErrSessionOwned = errors.New("store: session is held by another writer")
	// ErrSessionNotFound means no stored session has that cwd and id.
	ErrSessionNotFound = errors.New("store: session not found")
	// ErrSessionCorrupt means the stored session cannot be read.
	ErrSessionCorrupt = errors.New("store: session is corrupt")
	// ErrSessionFormatNewer means a newer build wrote the session.
	ErrSessionFormatNewer = errors.New("store: session format is newer than this build")
)

// SessionWriter is the writer of one open session. The store holds the write
// ownership of the session until Close.
type SessionWriter interface {
	sessions.Writer
	// ID returns the session id. It can change once, before the first entry is
	// stored: a lazy writer that meets a stored session with its id takes a
	// new id.
	ID() string
}

// SessionStore keeps sessions. Every call takes the canonical working
// directory and the session id, because the id alone does not name a session
// across directories. Every backend must pass the suite in storetest.
type SessionStore interface {
	// Create starts a session with header h. The header version and time are
	// used as given.
	Create(ctx context.Context, h sessions.Header, mode CreateMode) (SessionWriter, error)
	// Open takes the write ownership of a stored session and loads its entries.
	// It fails at once with ErrSessionOwned when another writer holds it.
	Open(ctx context.Context, cwd, id string) (SessionWriter, error)
	// Read returns the header and the committed entries without taking the
	// write ownership.
	Read(ctx context.Context, cwd, id string) (sessions.Header, []sessions.Entry, error)
	// List returns the headers of the stored sessions of cwd, newest first, with
	// ids compared as strings. It reads no entries.
	List(ctx context.Context, cwd string) ([]sessions.Header, error)
}
