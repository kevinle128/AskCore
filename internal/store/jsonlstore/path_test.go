package jsonlstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/sessions"
	"AskCore/internal/store"
)

func TestEncodeCwd(t *testing.T) {
	cases := map[string]string{
		"/Users/dale/proj":   "--Users-dale-proj--",
		"/":                  "----",
		`C:\Users\dale\proj`: "--C--Users-dale-proj--",
		"/a/b-c":             "--a-b-c--",
		"relative/dir":       "--relative-dir--",
	}
	for cwd, want := range cases {
		assert.Equal(t, want, encodeCwd(cwd), cwd)
	}
}

func TestEncodeCwdKeepsNameWithinFileSystemLimit(t *testing.T) {
	long := "/" + strings.Repeat("deep/", 120)
	other := long + "x"
	a, b := encodeCwd(long), encodeCwd(other)
	assert.LessOrEqual(t, len(a), 200)
	assert.True(t, strings.HasPrefix(a, "--") && strings.HasSuffix(a, "--"))
	assert.NotEqual(t, a, b, "a cut name still tells two directories apart")
}

func TestValidID(t *testing.T) {
	for _, id := range []string{"100000000000000001", "abc-DEF_9"} {
		assert.True(t, validID(id), id)
	}
	for _, id := range []string{"", ".", "..", "../x", "a/b", `a\b`, "a b", "a.jsonl", strings.Repeat("1", 65), "a\x00b"} {
		assert.False(t, validID(id), id)
	}
}

func TestInvalidIDIsNotFound(t *testing.T) {
	s := mustNew(t, t.TempDir())
	_, err := s.Open(context.Background(), "/x", "../../etc/passwd")
	assert.True(t, errors.Is(err, store.ErrSessionNotFound), "got %v", err)
	_, _, err = s.Read(context.Background(), "/x", "a/b")
	assert.True(t, errors.Is(err, store.ErrSessionNotFound), "got %v", err)
	_, err = s.Create(context.Background(), testHeader("/x", "a/b"), store.CreateNow)
	require.Error(t, err)
}

func TestSameCwdEncodingDifferentCwdIsRefused(t *testing.T) {
	ctx := context.Background()
	s := mustNew(t, t.TempDir())
	first, second := "/proj/a-b", "/proj/a/b"
	require.Equal(t, encodeCwd(first), encodeCwd(second))

	w, err := s.Create(ctx, testHeader(first, "100000000000000001"), store.CreateNow)
	require.NoError(t, err)
	_, err = w.Append(sessions.MessageEntry{Message: userText("hi")}, sessions.MessageEntry{Message: assistantText("yo")})
	require.NoError(t, err)
	require.NoError(t, w.Close())

	_, err = s.Open(ctx, second, "100000000000000001")
	assert.True(t, errors.Is(err, store.ErrSessionNotFound), "got %v", err)
	_, _, err = s.Read(ctx, second, "100000000000000001")
	assert.True(t, errors.Is(err, store.ErrSessionNotFound), "got %v", err)
	list, err := s.List(ctx, second)
	require.NoError(t, err)
	assert.Empty(t, list)

	// The refused open must not leave the session held.
	w, err = s.Open(ctx, first, "100000000000000001")
	require.NoError(t, err)
	require.NoError(t, w.Close())
}

func TestEncodeCwdCutKeepsWholeRunes(t *testing.T) {
	// Each ü is two bytes, so the cut at an odd byte offset falls inside a
	// rune when it is not backed up.
	long := "/" + strings.Repeat("ü", 150)
	name := encodeCwd(long)
	assert.True(t, utf8.ValidString(name), "the folder name is valid UTF-8")
	assert.LessOrEqual(t, len(name), maxDirName)
}
