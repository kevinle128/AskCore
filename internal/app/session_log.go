package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"AskCore/internal/agent"
	"AskCore/internal/sessions"
	"AskCore/internal/settings"
	"AskCore/internal/store"
	"AskCore/internal/store/jsonlstore"
	"AskCore/internal/workspace"
	"AskCore/pkg/protocol"
)

// sessionsDir is the folder of the Ask home that holds the session files.
const sessionsDir = "sessions"

// SessionLogOptions says how one native agent gets its session log.
type SessionLogOptions struct {
	// Resume is a session that the caller opened. The agent continues it and
	// closes it when the agent is disposed.
	Resume store.SessionWriter
	// NoSession keeps the log in memory: nothing is saved.
	NoSession bool
	// CreateNow stores a new session before its first reply, so its id can be
	// opened later. Without it a new session is stored with the first
	// assistant message.
	CreateNow bool
	// CreatedAt is the creation time of a new session. Zero means now.
	CreatedAt time.Time
}

// SessionLog opens and creates the session files of one Ask home. It makes no
// file or folder when it is built: the folders appear with the first stored
// session, so a command that only reads settings leaves the home as it was.
type SessionLog struct {
	store store.SessionStore
	now   func() time.Time
}

// NewSessionLog returns the session log of home. An empty home selects
// ASK_HOME, then ~/.ask.
func NewSessionLog(home string) (*SessionLog, error) {
	resolved, err := settings.ResolveHome(home)
	if err != nil {
		return nil, fmt.Errorf("session log: %w", err)
	}
	st, err := jsonlstore.New(filepath.Join(resolved, sessionsDir))
	if err != nil {
		return nil, fmt.Errorf("session log: %w", err)
	}
	return &SessionLog{store: st, now: time.Now}, nil
}

// RestoredSelection is what the log of a resumed session says about the model
// of its last run.
type RestoredSelection struct {
	Model sessions.ModelRef
	// Method is the login method of the last settled request, or empty when
	// the log has none (the demo provider has no credential).
	Method string
	// Level is the thinking level of the last run. A run that sent no level is
	// ThinkingDefault.
	Level protocol.ThinkingLevel
}

// Resumed is a session that Resume opened for writing.
type Resumed struct {
	Writer store.SessionWriter
	Header sessions.Header
	// Selection is nil when the log names no model, for example a session that
	// was stored and never ran.
	Selection *RestoredSelection
}

// Latest returns the id of the newest session of cwd that holds an assistant
// reply. A session that was stored and never answered (ACP session/new, the
// TUI start and /new store at once) is not worth a continue and is skipped. A
// session that another writer holds still counts: the open then fails with
// the words for the user. A file that cannot be read counts too, so the open
// reports it. It fails with an error that wraps store.ErrSessionNotFound when
// cwd has no such session.
func (l *SessionLog) Latest(ctx context.Context, cwd string) (string, error) {
	canon, err := workspace.CanonicalPath(cwd)
	if err != nil {
		return "", err
	}
	headers, err := l.store.List(ctx, canon)
	if err != nil {
		return "", sessionError(err, canon, "")
	}
	for _, h := range headers {
		_, entries, err := l.store.Read(ctx, canon, h.ID)
		switch {
		case errors.Is(err, store.ErrSessionNotFound):
			// Removed after List.
			continue
		case errors.Is(err, store.ErrSessionCorrupt), errors.Is(err, store.ErrSessionFormatNewer):
			return h.ID, nil
		case err != nil:
			return "", sessionError(err, canon, h.ID)
		case hasAssistantReply(entries):
			return h.ID, nil
		}
	}
	return "", sessionError(store.ErrSessionNotFound, canon, "")
}

// hasAssistantReply tells whether a log holds an assistant message.
func hasAssistantReply(entries []sessions.Entry) bool {
	for _, e := range entries {
		if m, ok := e.(sessions.MessageEntry); ok {
			if _, ok := m.Message.(protocol.AssistantMessage); ok {
				return true
			}
		}
	}
	return false
}

// Resume takes the write ownership of a stored session of cwd. An empty id
// means the newest session of cwd. It fails at once when another writer holds
// the session. The caller owns the returned writer until it gives it to an
// agent through SessionLogOptions.Resume.
func (l *SessionLog) Resume(ctx context.Context, cwd, id string) (Resumed, error) {
	canon, err := workspace.CanonicalPath(cwd)
	if err != nil {
		return Resumed{}, err
	}
	if id == "" {
		if id, err = l.Latest(ctx, canon); err != nil {
			return Resumed{}, err
		}
	}
	w, err := l.store.Open(ctx, canon, id)
	if err != nil {
		return Resumed{}, sessionError(err, canon, id)
	}
	header, _, err := l.store.Read(ctx, canon, id)
	if err != nil {
		return Resumed{}, errors.Join(sessionError(err, canon, id), w.Close())
	}
	out := Resumed{Writer: w, Header: header}
	if model, method, level, ok := agent.LoggedSelection(w.Entries()); ok {
		if level == "" {
			level = protocol.ThinkingDefault
		}
		out.Selection = &RestoredSelection{Model: model, Method: method, Level: level}
	}
	return out, nil
}

// NewContext returns the agent.Config.NewContext of one agent. The first call
// gives the resumed session, or creates the session of spec; each later call
// (a reset) creates a new session in the same directory with a new id.
func (l *SessionLog) NewContext(opts SessionLogOptions, id, cwd string) func() (sessions.Writer, error) {
	var (
		mu    sync.Mutex
		first = true
	)
	return func() (sessions.Writer, error) {
		mu.Lock()
		defer mu.Unlock()
		if first {
			first = false
			if opts.Resume != nil {
				return opts.Resume, nil
			}
			mode := store.CreateLazy
			if opts.CreateNow {
				mode = store.CreateNow
			}
			created := opts.CreatedAt
			if created.IsZero() {
				created = l.now()
			}
			return l.create(id, cwd, created, mode)
		}
		return l.create(sessions.NewID(), cwd, l.now(), store.CreateNow)
	}
}

func (l *SessionLog) create(id, cwd string, created time.Time, mode store.CreateMode) (sessions.Writer, error) {
	w, err := l.store.Create(context.Background(), sessions.Header{
		Version: sessions.FormatVersion, ID: id, Cwd: cwd, CreatedAt: created,
	}, mode)
	if err != nil {
		return nil, sessionError(err, cwd, id)
	}
	return w, nil
}

// StoreError is a session store failure in words for the user. It wraps the
// store error, so errors.Is still finds it.
type StoreError struct {
	msg string
	err error
}

func (e *StoreError) Error() string { return e.msg }
func (e *StoreError) Unwrap() error { return e.err }

// sessionError words a store error for the user. id is empty when the call
// named no session yet.
func sessionError(err error, cwd, id string) error {
	switch {
	case errors.Is(err, store.ErrSessionNotFound):
		if id == "" {
			return &StoreError{msg: "no session to continue in " + cwd, err: err}
		}
		return &StoreError{msg: fmt.Sprintf("no session %s in %s", id, cwd), err: err}
	case errors.Is(err, store.ErrSessionOwned):
		// A leader keeps a session after its TUI quits, so the usual cause is a
		// leader and not a second terminal.
		return &StoreError{msg: fmt.Sprintf("session %s is open in another ask process (a leader keeps it after the TUI quits); open it in the TUI with `ask -c`, or stop the leader with `ask leader stop`", id), err: err}
	case errors.Is(err, store.ErrSessionFormatNewer):
		return &StoreError{msg: fmt.Sprintf("session %s was written by a newer ask; update ask to open it", id), err: err}
	case errors.Is(err, store.ErrSessionCorrupt):
		return &StoreError{msg: fmt.Sprintf("session %s cannot be read: %v", id, err), err: err}
	}
	if id == "" {
		return fmt.Errorf("sessions in %s: %w", cwd, err)
	}
	return fmt.Errorf("session %s: %w", id, err)
}
