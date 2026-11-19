package jsonlstore

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/sessions"
	"AskCore/internal/store"
)

// seedSession stores a finished turn of five entries and returns its id.
func seedSession(t *testing.T, s *Store, cwd string) string {
	t.Helper()
	w, err := s.Create(context.Background(), testHeader(cwd, sessions.NewID()), store.CreateNow)
	require.NoError(t, err)
	_, err = w.Append(
		sessions.CycleOpened{CycleID: "c"},
		sessions.TurnOpened{CycleID: "c"},
		sessions.MessageEntry{Message: userText("hi")},
		sessions.MessageEntry{Message: assistantText("hello")},
		sessions.TurnClosed{CycleID: "c"},
	)
	require.NoError(t, err)
	id := w.ID()
	require.NoError(t, w.Close())
	return id
}

func appendRaw(t *testing.T, path string, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	require.NoError(t, err)
	_, err = f.WriteString(text)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func entryLine(t *testing.T, seq int, e sessions.Entry) string {
	t.Helper()
	b, err := sessions.EncodeEntry(seq, e)
	require.NoError(t, err)
	return string(b)
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Size()
}

func TestTornTailIsHiddenFromRead(t *testing.T) {
	s, cwd := mustNew(t, t.TempDir()), t.TempDir()
	id := seedSession(t, s, cwd)
	path := s.logPath(cwd, id)
	good := fileSize(t, path)
	appendRaw(t, path, `{"seq":5,"type":"cycle_op`)

	_, entries, err := s.Read(context.Background(), cwd, id)
	require.NoError(t, err)
	assert.Len(t, entries, 5)
	assert.Equal(t, good+int64(len(`{"seq":5,"type":"cycle_op`)), fileSize(t, path), "Read never writes")
}

func TestTornTailIsCutBeforeNextAppend(t *testing.T) {
	cases := map[string]func(t *testing.T) string{
		"partial line": func(*testing.T) string { return `{"seq":5,"type":"cycle_op` },
		// A line that decodes but has no newline was never reported as stored.
		"complete line without newline": func(t *testing.T) string {
			return entryLine(t, 5, sessions.CycleOpened{CycleID: "late"})
		},
	}
	for name, tail := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s, cwd := mustNew(t, t.TempDir()), t.TempDir()
			id := seedSession(t, s, cwd)
			path := s.logPath(cwd, id)
			good := fileSize(t, path)
			appendRaw(t, path, tail(t))

			w, err := s.Open(ctx, cwd, id)
			require.NoError(t, err)
			assert.Len(t, w.Entries(), 5)
			assert.Equal(t, good, fileSize(t, path))
			ref, err := w.Append(sessions.CycleOpened{CycleID: "next"})
			require.NoError(t, err)
			assert.Equal(t, sessions.CommitRef{Start: 5, End: 6}, ref)
			require.NoError(t, w.Close())

			_, entries, err := s.Read(ctx, cwd, id)
			require.NoError(t, err)
			require.Len(t, entries, 6)
			assert.Equal(t, sessions.CycleOpened{CycleID: "next"}, entries[5])
		})
	}
}

func TestBadLineBeforeTurnEndIsCorrupt(t *testing.T) {
	ctx := context.Background()
	s, cwd := mustNew(t, t.TempDir()), t.TempDir()
	id := seedSession(t, s, cwd)
	path := s.logPath(cwd, id)
	// File line 7 is bad; file line 8 is a valid turn end.
	appendRaw(t, path, entryLine(t, 5, sessions.CycleOpened{CycleID: "d"})+"\n")
	appendRaw(t, path, "not json at all\n")
	appendRaw(t, path, entryLine(t, 6, sessions.TurnClosed{CycleID: "d"})+"\n")
	before := fileSize(t, path)

	_, err := s.Open(ctx, cwd, id)
	require.Error(t, err)
	assert.True(t, errors.Is(err, store.ErrSessionCorrupt), "got %v", err)
	assert.Contains(t, err.Error(), "line 8")
	_, _, err = s.Read(ctx, cwd, id)
	assert.True(t, errors.Is(err, store.ErrSessionCorrupt), "got %v", err)
	assert.Equal(t, before, fileSize(t, path), "a corrupt log is left as it is")

	// The refusal must release the session.
	_, err = s.Open(ctx, cwd, id)
	assert.False(t, errors.Is(err, store.ErrSessionOwned), "got %v", err)
}

func TestBadLineAfterLastTurnEndIsCut(t *testing.T) {
	ctx := context.Background()
	s, cwd := mustNew(t, t.TempDir()), t.TempDir()
	id := seedSession(t, s, cwd)
	path := s.logPath(cwd, id)
	good := fileSize(t, path)
	appendRaw(t, path, "not json at all\n")
	appendRaw(t, path, entryLine(t, 6, sessions.CycleOpened{CycleID: "d"})+"\n")

	_, entries, err := s.Read(ctx, cwd, id)
	require.NoError(t, err)
	assert.Len(t, entries, 5)

	w, err := s.Open(ctx, cwd, id)
	require.NoError(t, err)
	defer func() { _ = w.Close() }()
	assert.Len(t, w.Entries(), 5)
	assert.Equal(t, good, fileSize(t, path))
}

func TestSequenceGapIsABadLine(t *testing.T) {
	ctx := context.Background()
	s, cwd := mustNew(t, t.TempDir()), t.TempDir()
	id := seedSession(t, s, cwd)
	path := s.logPath(cwd, id)
	good := fileSize(t, path)
	appendRaw(t, path, entryLine(t, 9, sessions.CycleOpened{CycleID: "d"})+"\n")

	w, err := s.Open(ctx, cwd, id)
	require.NoError(t, err)
	defer func() { _ = w.Close() }()
	assert.Len(t, w.Entries(), 5)
	assert.Equal(t, good, fileSize(t, path))
}

func TestUnknownTypeIsNewerFormat(t *testing.T) {
	ctx := context.Background()
	s, cwd := mustNew(t, t.TempDir()), t.TempDir()
	id := seedSession(t, s, cwd)
	path := s.logPath(cwd, id)
	appendRaw(t, path, `{"seq":5,"type":"future_thing","data":{}}`+"\n")

	_, err := s.Open(ctx, cwd, id)
	require.Error(t, err)
	assert.True(t, errors.Is(err, store.ErrSessionFormatNewer), "got %v", err)
	assert.True(t, errors.Is(err, sessions.ErrUnknownEntryType), "got %v", err)
	assert.False(t, errors.Is(err, store.ErrSessionCorrupt), "got %v", err)
	_, _, err = s.Read(ctx, cwd, id)
	assert.True(t, errors.Is(err, store.ErrSessionFormatNewer), "got %v", err)
}

func TestIgnorableUnknownTypeIsSkippedAndNotCounted(t *testing.T) {
	ctx := context.Background()
	s, cwd := mustNew(t, t.TempDir()), t.TempDir()
	id := seedSession(t, s, cwd)
	path := s.logPath(cwd, id)
	appendRaw(t, path, `{"seq":5,"type":"future_thing","ignorable":true,"data":{}}`+"\n")

	w, err := s.Open(ctx, cwd, id)
	require.NoError(t, err)
	defer func() { _ = w.Close() }()
	assert.Len(t, w.Entries(), 5)
	ref, err := w.Append(sessions.CycleOpened{CycleID: "n"})
	require.NoError(t, err)
	assert.Equal(t, sessions.CommitRef{Start: 5, End: 6}, ref)
}

func TestNewerHeaderIsNewerFormat(t *testing.T) {
	ctx := context.Background()
	s, cwd := mustNew(t, t.TempDir()), t.TempDir()
	id := seedSession(t, s, cwd)
	path := s.logPath(cwd, id)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	newer := []byte(`{"type":"session","version":2,"id":"` + id + `","cwd":"` + cwd + `","createdAt":"2026-11-18T11:00:00Z"}` + "\n")
	rest := data[indexByte(data, '\n')+1:]
	require.NoError(t, os.WriteFile(path, append(newer, rest...), 0o600))

	_, err = s.Open(ctx, cwd, id)
	assert.True(t, errors.Is(err, store.ErrSessionFormatNewer), "got %v", err)
	assert.True(t, errors.Is(err, sessions.ErrFormatNewer), "got %v", err)
	_, _, err = s.Read(ctx, cwd, id)
	assert.True(t, errors.Is(err, store.ErrSessionFormatNewer), "got %v", err)
	list, err := s.List(ctx, cwd)
	require.NoError(t, err)
	assert.Empty(t, list, "List skips a file it cannot read")
}

func TestListSkipsBadHeaderAndOtherFiles(t *testing.T) {
	ctx := context.Background()
	s, cwd := mustNew(t, t.TempDir()), t.TempDir()
	good := seedSession(t, s, cwd)
	dir := s.dir(cwd)
	require.NoError(t, os.WriteFile(dir+"/100000000000000099.jsonl", []byte("garbage\n"), 0o600))
	require.NoError(t, os.WriteFile(dir+"/100000000000000098.jsonl", nil, 0o600))
	require.NoError(t, os.WriteFile(dir+"/notes.txt", []byte("x"), 0o600))
	// The header names another id than the file does.
	other := seedSession(t, s, cwd)
	require.NoError(t, os.Rename(s.logPath(cwd, other), dir+"/100000000000000097.jsonl"))

	list, err := s.List(ctx, cwd)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, good, list[0].ID)
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}
