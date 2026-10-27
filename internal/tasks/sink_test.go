package tasks

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSinkTailAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.log")
	sink := newSink(path, nil, nil)

	all := make([]byte, 0, 200_000)
	for i := range 150_000 {
		all = append(all, byte('a'+i%26))
	}
	// Uneven chunks, and one chunk that is larger than the whole ring.
	for off := 0; off < 70_000; {
		n := min(777, 70_000-off)
		written, err := sink.Write(all[off : off+n])
		require.NoError(t, err)
		require.Equal(t, n, written)
		off += n
	}
	_, err := sink.Write(all[70_000:])
	require.NoError(t, err)
	require.NoError(t, sink.Close())

	assert.Equal(t, all[len(all)-ringSize:], sink.Tail(), "the ring keeps the last 64 KiB")
	assert.True(t, sink.Truncated())
	size, _ := sink.Stats()
	assert.EqualValues(t, len(all), size)
	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, all, onDisk, "the file keeps everything")
	assert.Equal(t, path, sink.FilePath())
}

func TestSinkSmallOutputIsNotTruncated(t *testing.T) {
	sink := newSink(filepath.Join(t.TempDir(), "task.log"), nil, nil)
	_, err := sink.Write([]byte("one\ntwo"))
	require.NoError(t, err)
	assert.False(t, sink.Truncated())
	assert.Equal(t, "one\ntwo", string(sink.Tail()))
	size, lines := sink.Stats()
	assert.EqualValues(t, 7, size)
	assert.EqualValues(t, 2, lines, "a last line without a newline counts")
	_, err = sink.Write([]byte("\n"))
	require.NoError(t, err)
	_, lines = sink.Stats()
	assert.EqualValues(t, 2, lines)
	require.NoError(t, sink.Close())
}

func TestSinkTailStartsOnRuneBoundary(t *testing.T) {
	sink := newSink(filepath.Join(t.TempDir(), "task.log"), nil, nil)
	// Three-byte runes never fit the ring size without a split.
	chunk := []byte("€€€€€€€€€€€€€€€€€€€€€€€€€€€€€€€€")
	for range 2000 {
		_, err := sink.Write(chunk)
		require.NoError(t, err)
	}
	require.NoError(t, sink.Close())
	assert.True(t, sink.Truncated())
	assert.True(t, utf8.Valid(sink.Tail()))
}

func TestSinkOpensLazilyAndSafely(t *testing.T) {
	dir := t.TempDir()

	t.Run("no file before the first write", func(t *testing.T) {
		path := filepath.Join(dir, "lazy.log")
		sink := newSink(path, nil, nil)
		assert.NoFileExists(t, path)
		assert.Empty(t, sink.FilePath())
		require.NoError(t, sink.Close())
		assert.NoFileExists(t, path)

		sink = newSink(path, nil, nil)
		_, err := sink.Write([]byte("x"))
		require.NoError(t, err)
		require.NoError(t, sink.Close())
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})

	t.Run("existing file", func(t *testing.T) {
		path := filepath.Join(dir, "exists.log")
		require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))
		sink := newSink(path, nil, nil)
		_, err := sink.Write([]byte("new"))
		assert.ErrorIs(t, err, os.ErrExist)
		assert.Error(t, sink.Err())
		assert.Empty(t, sink.FilePath(), "a file that the sink did not create is not an output file")
		data, _ := os.ReadFile(path)
		assert.Equal(t, "old", string(data))
		// The ring still holds the output, so the notice has a tail.
		assert.Equal(t, "new", string(sink.Tail()))
		_ = sink.Close()
	})

	t.Run("symlink at the path", func(t *testing.T) {
		target := filepath.Join(dir, "target.txt")
		require.NoError(t, os.WriteFile(target, []byte("secret"), 0o600))
		path := filepath.Join(dir, "link.log")
		require.NoError(t, os.Symlink(target, path))
		sink := newSink(path, nil, nil)
		_, err := sink.Write([]byte("overwrite"))
		assert.Error(t, err)
		data, _ := os.ReadFile(target)
		assert.Equal(t, "secret", string(data))
		_ = sink.Close()
	})

	t.Run("dangling symlink at the path", func(t *testing.T) {
		path := filepath.Join(dir, "dangling.log")
		target := filepath.Join(dir, "not-created.txt")
		require.NoError(t, os.Symlink(target, path))
		sink := newSink(path, nil, nil)
		_, err := sink.Write([]byte("x"))
		assert.Error(t, err)
		assert.NoFileExists(t, target)
		_ = sink.Close()
	})
}

func TestSinkCallsOnWriteWithoutLock(t *testing.T) {
	calls := 0
	var sink *Sink
	sink = newSink(filepath.Join(t.TempDir(), "task.log"), nil, func() {
		calls++
		// The callback may read the sink. This deadlocks if Write holds its
		// lock while it calls back.
		sink.Stats()
	})
	_, err := sink.Write([]byte("a"))
	require.NoError(t, err)
	_, err = sink.Write([]byte("b"))
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	require.NoError(t, sink.Close())
}

func TestSinkPreparesDirectoryAtFirstWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "session")
	calls := 0
	prepare := func() error {
		calls++
		return os.Mkdir(dir, 0o700)
	}
	sink := newSink(filepath.Join(dir, "task.log"), prepare, nil)
	require.Equal(t, 0, calls, "no directory before output")
	_, err := sink.Write([]byte("a\n"))
	require.NoError(t, err)
	_, err = sink.Write([]byte("b\n"))
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.NoError(t, sink.Close())
	data, err := os.ReadFile(filepath.Join(dir, "task.log"))
	require.NoError(t, err)
	require.Equal(t, "a\nb\n", string(data))
}

func TestSinkPrepareFailureKeepsRing(t *testing.T) {
	refused := errors.New("refused")
	sink := newSink(filepath.Join(t.TempDir(), "missing", "task.log"), func() error { return refused }, nil)
	_, err := sink.Write([]byte("out\n"))
	require.ErrorIs(t, err, refused)
	require.Equal(t, "out\n", string(sink.Tail()))
	require.NoError(t, sink.Close())
}
