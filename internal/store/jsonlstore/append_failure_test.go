package jsonlstore

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/sessions"
	"AskCore/internal/store"
)

func openStored(t *testing.T) (*Store, string, string, *writer) {
	t.Helper()
	s, cwd := mustNew(t, t.TempDir()), t.TempDir()
	id := seedSession(t, s, cwd)
	w, err := s.Open(context.Background(), cwd, id)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })
	return s, cwd, id, w.(*writer)
}

// The file size limit of the process makes the kernel accept only the first
// bytes of a write, which is the failure that the rollback exists for.
func TestFailedAppendTruncatesToOldSize(t *testing.T) {
	s, cwd, id, w := openStored(t)
	path := s.logPath(cwd, id)
	old := fileSize(t, path)

	var lim syscall.Rlimit
	require.NoError(t, syscall.Getrlimit(syscall.RLIMIT_FSIZE, &lim))
	restore := lim
	lim.Cur = uint64(old) + 64
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &lim); err != nil {
		t.Skipf("cannot set the file size limit: %v", err)
	}
	signal.Ignore(syscall.SIGXFSZ)
	defer func() {
		_ = syscall.Setrlimit(syscall.RLIMIT_FSIZE, &restore)
		signal.Reset(syscall.SIGXFSZ)
	}()

	big := sessions.MessageEntry{Message: userText(strings.Repeat("x", 4096))}
	_, err := w.Append(sessions.CycleOpened{CycleID: "small"}, big)
	require.Error(t, err)
	assert.True(t, errors.Is(err, sessions.ErrWriterLost), "got %v", err)
	assert.Equal(t, old, fileSize(t, path), "the partial write is cut off")
	assert.Len(t, w.Entries(), 5, "the memory copy does not move either")

	_, err = w.Append(sessions.CycleOpened{CycleID: "again"})
	assert.True(t, errors.Is(err, sessions.ErrWriterLost), "the writer stays lost: %v", err)
	require.NoError(t, w.Close())

	require.NoError(t, syscall.Setrlimit(syscall.RLIMIT_FSIZE, &restore))
	_, entries, err := s.Read(context.Background(), cwd, id)
	require.NoError(t, err)
	assert.Len(t, entries, 5)
}

func TestAppendOnBrokenDescriptorLosesWriter(t *testing.T) {
	s, cwd, id, w := openStored(t)
	path := s.logPath(cwd, id)
	old := fileSize(t, path)
	require.NoError(t, w.log.Close())

	_, err := w.Append(sessions.CycleOpened{CycleID: "x"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, sessions.ErrWriterLost), "got %v", err)
	assert.Equal(t, old, fileSize(t, path))
	assert.Len(t, w.Entries(), 5)
	_ = w.Close()

	// The lock is released, so the session opens again with its old entries.
	w2, err := s.Open(context.Background(), cwd, id)
	require.NoError(t, err)
	defer func() { _ = w2.Close() }()
	assert.Len(t, w2.Entries(), 5)
}

func TestAppendOnReadOnlyDescriptorLosesWriter(t *testing.T) {
	s, cwd, id, w := openStored(t)
	ro, err := os.Open(s.logPath(cwd, id))
	require.NoError(t, err)
	require.NoError(t, w.log.Close())
	w.log = ro

	_, err = w.Append(sessions.CycleOpened{CycleID: "x"})
	assert.True(t, errors.Is(err, sessions.ErrWriterLost), "got %v", err)
}

func TestAppendRefusesAfterForeignChange(t *testing.T) {
	s, cwd, id, w := openStored(t)
	path := s.logPath(cwd, id)
	appendRaw(t, path, "foreign\n")
	size := fileSize(t, path)

	_, err := w.Append(sessions.CycleOpened{CycleID: "x"})
	assert.True(t, errors.Is(err, sessions.ErrWriterLost), "got %v", err)
	assert.Equal(t, size, fileSize(t, path), "data of another writer is not removed")
}

func TestClosedWriterRefusesAppendButKeepsReads(t *testing.T) {
	_, _, _, w := openStored(t)
	require.NoError(t, w.Close())
	_, err := w.Append(sessions.CycleOpened{CycleID: "x"})
	assert.True(t, errors.Is(err, sessions.ErrWriterLost), "got %v", err)
	assert.Len(t, w.Entries(), 5)
	assert.Len(t, w.Messages(), 2)
}

func TestOpenMissingSessionLeavesNoLockFile(t *testing.T) {
	s, cwd := mustNew(t, t.TempDir()), t.TempDir()
	_, err := s.Open(context.Background(), cwd, "100000000000000001")
	assert.True(t, errors.Is(err, store.ErrSessionNotFound), "got %v", err)
	_, statErr := os.Stat(s.lockPath(cwd, "100000000000000001"))
	assert.True(t, errors.Is(statErr, os.ErrNotExist))
}
