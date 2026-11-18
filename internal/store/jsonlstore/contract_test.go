package jsonlstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/sessions"
	"AskCore/internal/store"
	"AskCore/internal/store/storetest"
	"AskCore/pkg/protocol"
)

var _ store.SessionStore = (*Store)(nil)

var testCreated = time.Date(2026, 11, 18, 11, 0, 0, 0, time.UTC)

func mustNew(t testing.TB, root string) *Store {
	t.Helper()
	s, err := New(root)
	require.NoError(t, err)
	return s
}

func testHeader(cwd, id string) sessions.Header {
	return sessions.Header{Version: sessions.FormatVersion, ID: id, Cwd: cwd, CreatedAt: testCreated}
}

func userText(s string) protocol.Message {
	return protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: s}}, Timestamp: 1}
}

func assistantText(s string) protocol.Message {
	return protocol.AssistantMessage{
		Content:    []protocol.AssistantBlock{protocol.Text{Text: s}},
		StopReason: protocol.StopReason("stop"),
	}
}

func TestJSONLWriterContract(t *testing.T) {
	for _, mode := range []struct {
		name string
		mode store.CreateMode
	}{{"CreateNow", store.CreateNow}, {"CreateLazy", store.CreateLazy}} {
		t.Run(mode.name, func(t *testing.T) {
			storetest.RunSessionWriter(t, func(t *testing.T) sessions.Writer {
				s := mustNew(t, t.TempDir())
				w, err := s.Create(context.Background(), testHeader(t.TempDir(), sessions.NewID()), mode.mode)
				require.NoError(t, err)
				return w
			})
		})
	}
}

func TestJSONLStoreContract(t *testing.T) {
	// Two calls of the factory in one test must see the same data, so the root
	// is chosen once for each test name.
	var mu sync.Mutex
	roots := map[string]string{}
	storetest.RunSessionStore(t, func(t *testing.T) store.SessionStore {
		mu.Lock()
		defer mu.Unlock()
		root, ok := roots[t.Name()]
		if !ok {
			root = t.TempDir()
			roots[t.Name()] = root
		}
		return mustNew(t, root)
	})
}

func TestLazySessionLeavesNoFileUntilAssistantMessage(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "sessions")
	s, cwd := mustNew(t, root), t.TempDir()
	w, err := s.Create(ctx, testHeader(cwd, sessions.NewID()), store.CreateLazy)
	require.NoError(t, err)
	defer func() { _ = w.Close() }()

	_, err = w.Append(sessions.CycleOpened{CycleID: "c"}, sessions.MessageEntry{Message: userText("hi")})
	require.NoError(t, err)
	_, err = os.Stat(root)
	assert.True(t, errors.Is(err, os.ErrNotExist), "nothing is written before the first reply: %v", err)

	_, err = w.Append(sessions.MessageEntry{Message: assistantText("hello")})
	require.NoError(t, err)
	id := w.ID()

	dirInfo, err := os.Stat(s.dir(cwd))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
	for _, p := range []string{s.logPath(cwd, id), s.lockPath(cwd, id)} {
		info, err := os.Stat(p)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), p)
	}
	names, err := os.ReadDir(s.dir(cwd))
	require.NoError(t, err)
	assert.Len(t, names, 2, "no temporary file stays behind")
}

func TestLazyCloseLeavesNoFileAndNoDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	s := mustNew(t, root)
	w, err := s.Create(context.Background(), testHeader(t.TempDir(), sessions.NewID()), store.CreateLazy)
	require.NoError(t, err)
	_, err = w.Append(sessions.MessageEntry{Message: userText("hi")})
	require.NoError(t, err)
	require.NoError(t, w.Close())
	_, err = os.Stat(root)
	assert.True(t, errors.Is(err, os.ErrNotExist), "got %v", err)
	require.NoError(t, w.Close(), "Close is safe to repeat")
}

// A stored session that owns the id makes the writer take a new id, because
// the Agent cannot retry an append.
func TestFirstWriteCollisionReportsExist(t *testing.T) {
	for _, mode := range []store.CreateMode{store.CreateLazy, store.CreateNow} {
		ctx := context.Background()
		s, cwd := mustNew(t, t.TempDir()), t.TempDir()
		taken := seedSession(t, s, cwd)
		takenBytes, err := os.ReadFile(s.logPath(cwd, taken))
		require.NoError(t, err)

		w, err := s.Create(ctx, testHeader(cwd, taken), mode)
		require.NoError(t, err)
		_, err = w.Append(sessions.MessageEntry{Message: userText("hi")}, sessions.MessageEntry{Message: assistantText("yo")})
		require.NoError(t, err)
		id := w.ID()
		require.NoError(t, w.Close())

		assert.NotEqual(t, taken, id)
		after, err := os.ReadFile(s.logPath(cwd, taken))
		require.NoError(t, err)
		assert.Equal(t, takenBytes, after, "the stored session is not touched")
		h, entries, err := s.Read(ctx, cwd, id)
		require.NoError(t, err)
		assert.Equal(t, id, h.ID, "the header carries the new id")
		assert.Len(t, entries, 2)
	}
}

func TestCreateRejectsHeaderWithoutCwd(t *testing.T) {
	s := mustNew(t, t.TempDir())
	_, err := s.Create(context.Background(), testHeader("", sessions.NewID()), store.CreateNow)
	require.Error(t, err)
}
