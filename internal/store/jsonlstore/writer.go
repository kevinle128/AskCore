package jsonlstore

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"

	"AskCore/internal/sessions"
	"AskCore/internal/store"
	"AskCore/pkg/protocol"
)

// maxIDAttempts bounds the new ids that a first write tries when stored
// sessions already own the id.
const maxIDAttempts = 8

// writer is the open session. All state is guarded by mu; the Agent appends
// from one goroutine and other goroutines may read.
type writer struct {
	mu sync.Mutex
	st *Store

	hdr     sessions.Header
	entries []sessions.Entry
	// pending holds the encoded lines of a lazy session that is not stored yet.
	pending [][]byte
	stored  bool

	lock *os.File
	log  *os.File
	// size is the size of the log file that this writer knows. A different size
	// on disk means that someone else wrote.
	size int64

	snap    int
	hasSnap bool
	lost    bool
	closed  bool
}

var _ store.SessionWriter = (*writer)(nil)

func (w *writer) ID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.hdr.ID
}

func (w *writer) setEntries(entries []sessions.Entry) {
	w.entries = entries
	for i, e := range entries {
		if _, ok := e.(sessions.SystemSnapshot); ok {
			w.snap, w.hasSnap = i, true
		}
	}
}

// Append stores the entries as one step. It encodes all of them first, so a
// bad entry changes nothing. A failed write is cut off the file again and the
// writer stays lost.
func (w *writer) Append(entries ...sessions.Entry) (sessions.CommitRef, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.lost || w.closed {
		return sessions.CommitRef{}, fmt.Errorf("%w: writer is not usable", sessions.ErrWriterLost)
	}
	start := len(w.entries)
	ref := sessions.CommitRef{Start: start, End: start + len(entries)}
	lines := make([][]byte, len(entries))
	copies := make([]sessions.Entry, len(entries))
	for i, e := range entries {
		line, err := sessions.EncodeEntry(start+i, e)
		if err != nil {
			return sessions.CommitRef{}, err
		}
		lines[i], copies[i] = line, sessions.Clone(e)
	}
	if len(entries) == 0 {
		return ref, nil
	}

	if w.stored {
		if err := w.write(lines, needsSync(copies)); err != nil {
			return sessions.CommitRef{}, err
		}
	} else {
		held := len(w.pending)
		w.pending = append(w.pending, lines...)
		if holdsAssistantMessage(copies) {
			if err := w.firstWrite(); err != nil {
				w.pending = w.pending[:held]
				w.lost = true
				return sessions.CommitRef{}, errors.Join(sessions.ErrWriterLost, err)
			}
		}
	}

	w.entries = append(w.entries, copies...)
	for i, e := range copies {
		if _, ok := e.(sessions.SystemSnapshot); ok {
			w.snap, w.hasSnap = start+i, true
		}
	}
	return ref, nil
}

// write appends encoded lines to the stored file in one write call.
func (w *writer) write(lines [][]byte, sync bool) error {
	info, err := w.log.Stat()
	if err != nil {
		return w.fail(fmt.Errorf("stat log: %w", err))
	}
	if info.Size() != w.size {
		// Someone else changed the file. Its bytes are not ours to cut.
		w.lost = true
		return fmt.Errorf("%w: log is %d bytes, writer expects %d", sessions.ErrWriterLost, info.Size(), w.size)
	}
	buf := joinLines(lines)
	_, err = w.log.Write(buf)
	if err == nil && sync {
		err = w.log.Sync()
	}
	if err != nil {
		return w.fail(err)
	}
	w.size += int64(len(buf))
	return nil
}

// fail cuts the file back to the last size that was reported stored and
// latches the writer. The cut goes by path, because the descriptor may be the
// reason for the failure.
func (w *writer) fail(cause error) error {
	w.lost = true
	truncErr := os.Truncate(w.st.logPath(w.hdr.Cwd, w.hdr.ID), w.size)
	var syncErr error
	if truncErr == nil {
		syncErr = w.log.Sync()
	}
	return errors.Join(sessions.ErrWriterLost, cause, truncErr, syncErr)
}

// firstWrite stores a new session: header and held lines go to a temporary
// file, which is synced and then linked to its final name. link fails when the
// name exists, so a stored session is never replaced. Then the writer takes a
// new id, because the Agent cannot retry an append.
func (w *writer) firstWrite() error {
	dir := w.st.dir(w.hdr.Cwd)
	if err := w.st.makeDirs(dir); err != nil {
		return err
	}
	for attempt := 0; attempt < maxIDAttempts; attempt++ {
		done, err := w.tryStore(dir)
		if err != nil || done {
			return err
		}
		w.hdr.ID = sessions.NewID()
	}
	return fmt.Errorf("jsonlstore: no free session id after %d tries", maxIDAttempts)
}

// tryStore reports done=false, with no error, when the id is taken.
func (w *writer) tryStore(dir string) (done bool, err error) {
	id := w.hdr.ID
	logPath := w.st.logPath(w.hdr.Cwd, id)
	lock, err := lockFile(w.st.lockPath(w.hdr.Cwd, id))
	if errors.Is(err, errLockBusy) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	header, err := sessions.EncodeHeader(w.hdr)
	if err != nil {
		_ = unlockFile(lock)
		return false, err
	}
	tmpPath := filepath.Join(dir, fmt.Sprintf(".%s.%016x.tmp", id, rand.Uint64()))
	tmp, err := openFile(tmpPath, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o600)
	if err != nil {
		_ = unlockFile(lock)
		return false, err
	}
	abort := func(cause error) (bool, error) {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		_ = unlockFile(lock)
		return false, cause
	}
	buf := joinLines(append([][]byte{header}, w.pending...))
	if _, err = tmp.Write(buf); err != nil {
		return abort(err)
	}
	if err = tmp.Sync(); err != nil {
		return abort(err)
	}
	if err = os.Link(tmpPath, logPath); err != nil {
		if errors.Is(err, fs.ErrExist) {
			_, _ = abort(nil)
			return false, nil
		}
		return abort(err)
	}
	// The final name exists now. The temporary name is only clutter.
	_ = os.Remove(tmpPath)
	if err = w.st.syncer(dir); err != nil {
		// The Agent is told that the write failed, so the file must not stay:
		// List and a resume would find a session that the caller never saw.
		_ = os.Remove(logPath)
		return abort(err)
	}
	w.lock, w.log, w.size, w.stored = lock, tmp, int64(len(buf)), true
	w.pending = nil
	return true, nil
}

func (w *writer) Messages() []protocol.Message {
	w.mu.Lock()
	defer w.mu.Unlock()
	return sessions.MessagesOf(w.entries)
}

func (w *writer) Entries() []sessions.Entry {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]sessions.Entry, len(w.entries))
	for i, e := range w.entries {
		out[i] = sessions.Clone(e)
	}
	return out
}

func (w *writer) LastSystemSnapshot() (sessions.SystemSnapshot, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.hasSnap {
		return sessions.SystemSnapshot{}, false
	}
	return sessions.Clone(w.entries[w.snap]).(sessions.SystemSnapshot), true
}

// Close syncs the file and releases the lock. The lock file stays. A lazy
// session that never reached the file leaves nothing. Close is safe to repeat.
func (w *writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	var errs []error
	if w.log != nil {
		if !w.lost {
			errs = append(errs, w.log.Sync())
		}
		errs = append(errs, w.log.Close())
	}
	if w.lock != nil {
		errs = append(errs, unlockFile(w.lock))
	}
	return errors.Join(errs...)
}

// discard releases what a writer holds after Create failed.
func (w *writer) discard() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.log != nil {
		_ = w.log.Close()
	}
	if w.lock != nil {
		_ = unlockFile(w.lock)
	}
}

func joinLines(lines [][]byte) []byte {
	var b bytes.Buffer
	for _, l := range lines {
		b.Write(l)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// makeDirs makes dir and the folders above it that are missing. A new folder
// is an entry of its parent, so each parent is synced: a power loss cannot
// drop the folder of a session file that was synced. Only the folders that
// this call made are synced.
func (s *Store) makeDirs(dir string) error {
	var made []string
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil {
			break
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		made = append(made, d)
		if filepath.Dir(d) == d {
			break
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, d := range made {
		if err := s.syncer(filepath.Dir(d)); err != nil {
			return err
		}
	}
	return nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	return errors.Join(err, d.Close())
}

// needsSync reports whether the batch ends a step that must survive a crash:
// before a model call (RequestDelta), before a tool run (ToolCall), at a step
// start (TurnOpened) and at the end of a cycle (CycleClosed). Other entries
// ride along until the next such batch or Close.
func needsSync(entries []sessions.Entry) bool {
	for _, e := range entries {
		switch e.(type) {
		case sessions.RequestDelta, sessions.ToolCall, sessions.TurnOpened, sessions.CycleClosed:
			return true
		}
	}
	return false
}

// holdsAssistantMessage reports whether the batch has a reply of the model.
func holdsAssistantMessage(entries []sessions.Entry) bool {
	for _, e := range entries {
		if m, ok := e.(sessions.MessageEntry); ok {
			switch m.Message.(type) {
			case protocol.AssistantMessage, *protocol.AssistantMessage:
				return true
			}
		}
	}
	return false
}
