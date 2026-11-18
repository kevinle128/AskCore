package jsonlstore

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"AskCore/internal/sessions"
	"AskCore/internal/store"
)

// maxHeaderLine bounds the first line that List reads from each file.
const maxHeaderLine = 64 << 10

// Store keeps sessions in files below one root folder. It holds no state, so
// two Store values on the same root, also in two processes, work together
// through the file system and the file locks.
type Store struct {
	root string
	// syncer syncs a folder. Tests replace it to make the sync fail.
	syncer func(dir string) error
}

var _ store.SessionStore = (*Store)(nil)

// New returns a store below root. It makes no folder: the folder of a working
// directory, and the root if needed, appear with the first stored session.
func New(root string) (*Store, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("jsonlstore: root %q must be an absolute path", root)
	}
	return &Store{root: filepath.Clean(root), syncer: syncDir}, nil
}

// Create starts a session. With CreateNow the session is stored before Create
// returns, so the header alone is in the file. With CreateLazy nothing is
// stored until the first assistant message is appended.
func (s *Store) Create(ctx context.Context, h sessions.Header, mode store.CreateMode) (store.SessionWriter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validID(h.ID) {
		return nil, fmt.Errorf("jsonlstore: invalid session id %q", h.ID)
	}
	if h.Cwd == "" {
		return nil, errors.New("jsonlstore: session header has no cwd")
	}
	if h.Version == 0 {
		h.Version = sessions.FormatVersion
	}
	w := &writer{st: s, hdr: h}
	if mode == store.CreateNow {
		if err := w.firstWrite(); err != nil {
			w.discard()
			return nil, err
		}
	}
	return w, nil
}

// Open takes the write ownership of a stored session. It cuts a torn or
// crashed tail before it returns, so the first append lands on a clean end.
func (s *Store) Open(ctx context.Context, cwd, id string) (store.SessionWriter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validID(id) {
		return nil, fmt.Errorf("%w: invalid session id %q", store.ErrSessionNotFound, id)
	}
	logPath := s.logPath(cwd, id)
	// Check first, so that opening a missing session makes no lock file.
	if _, err := os.Stat(logPath); err != nil {
		return nil, notFound(err, id)
	}
	lockPath := s.lockPath(cwd, id)
	lock, err := lockFile(lockPath)
	if errors.Is(err, errLockBusy) {
		return nil, fmt.Errorf("%w: session %s (lock %s)", store.ErrSessionOwned, id, lockPath)
	}
	if err != nil {
		return nil, err
	}
	w, err := s.openLocked(cwd, id, logPath, lock)
	if err != nil {
		_ = unlockFile(lock)
		return nil, err
	}
	return w, nil
}

func (s *Store) openLocked(cwd, id, logPath string, lock *os.File) (*writer, error) {
	log, err := openFile(logPath, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		return nil, notFound(err, id)
	}
	data, err := io.ReadAll(log)
	if err != nil {
		_ = log.Close()
		return nil, fmt.Errorf("read session %s: %w", id, err)
	}
	res, err := checkScan(data, cwd, id)
	if err == nil && res.valid < len(data) {
		if err = log.Truncate(int64(res.valid)); err == nil {
			err = log.Sync()
		}
		if err != nil {
			err = fmt.Errorf("cut the tail of session %s: %w", id, err)
		}
	}
	if err != nil {
		_ = log.Close()
		return nil, err
	}
	w := &writer{
		st: s, hdr: res.header, stored: true,
		lock: lock, log: log, size: int64(res.valid),
	}
	w.setEntries(res.entries)
	return w, nil
}

// Read returns the committed entries without the write ownership. It never
// writes, so a torn tail stays in the file and is not shown.
func (s *Store) Read(ctx context.Context, cwd, id string) (sessions.Header, []sessions.Entry, error) {
	if err := ctx.Err(); err != nil {
		return sessions.Header{}, nil, err
	}
	if !validID(id) {
		return sessions.Header{}, nil, fmt.Errorf("%w: invalid session id %q", store.ErrSessionNotFound, id)
	}
	data, err := os.ReadFile(s.logPath(cwd, id))
	if err != nil {
		return sessions.Header{}, nil, notFound(err, id)
	}
	res, err := checkScan(data, cwd, id)
	if err != nil {
		return sessions.Header{}, nil, err
	}
	return res.header, res.entries, nil
}

// List returns the headers of the sessions of cwd, newest first. It reads the
// first line of each file only and skips a file that has no usable header.
func (s *Store) List(ctx context.Context, cwd string) ([]sessions.Header, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	items, err := os.ReadDir(s.dir(cwd))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []sessions.Header
	for _, item := range items {
		name := item.Name()
		id, ok := strings.CutSuffix(name, logExt)
		if !ok || !validID(id) || item.IsDir() {
			continue
		}
		h, err := readHeader(filepath.Join(s.dir(cwd), name))
		if err != nil || h.Cwd != cwd || h.ID != id {
			continue
		}
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func readHeader(path string) (sessions.Header, error) {
	f, err := openFile(path, os.O_RDONLY, 0)
	if err != nil {
		return sessions.Header{}, err
	}
	defer func() { _ = f.Close() }()
	line, err := bufio.NewReaderSize(f, maxHeaderLine).ReadSlice('\n')
	if err != nil {
		return sessions.Header{}, err
	}
	return sessions.DecodeHeader(line)
}

// checkScan scans a file and checks that it is the session that the caller
// asked for. A file under the folder of this cwd can still belong to another
// directory with the same encoded name, or to another id after a rename.
func checkScan(data []byte, cwd, id string) (scanResult, error) {
	res, err := scan(data)
	if err != nil {
		return scanResult{}, fmt.Errorf("session %s: %w", id, err)
	}
	if res.header.Cwd != cwd {
		return scanResult{}, fmt.Errorf("%w: session %s belongs to %s", store.ErrSessionNotFound, id, res.header.Cwd)
	}
	if res.header.ID != id {
		return scanResult{}, fmt.Errorf("%w: session %s: header names id %s", store.ErrSessionCorrupt, id, res.header.ID)
	}
	return res, nil
}

func notFound(err error, id string) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s", store.ErrSessionNotFound, id)
	}
	return err
}
