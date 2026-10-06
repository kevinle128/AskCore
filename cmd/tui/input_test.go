package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func TestInputJoinsStdinFileAndMessage(t *testing.T) {
	p := writeFile(t, "notes.txt", "x\ny")
	stdin, err := readPipedStdin(strings.NewReader("  piped\n\n"))
	require.NoError(t, err)
	files, err := fileText([]string{p})
	require.NoError(t, err)

	got := buildPrompts(stdin, files, []string{"question", "second", "third"})

	assert.Equal(t, []string{
		"piped<file name=\"" + p + "\">\nx\ny\n</file>\nquestion",
		"second",
		"third",
	}, got)
}

func TestInputFileWrapper(t *testing.T) {
	a := writeFile(t, "a.md", "\uFEFFalpha")
	b := writeFile(t, "b.md", "beta\n")

	got, err := fileText([]string{a, b})

	require.NoError(t, err)
	assert.Equal(t, "<file name=\""+a+"\">\nalpha\n</file>\n<file name=\""+b+"\">\nbeta\n\n</file>\n", got)
}

func TestInputEmptyFileSkipped(t *testing.T) {
	empty := writeFile(t, "empty.txt", "")

	files, err := fileText([]string{empty})

	require.NoError(t, err)
	assert.Equal(t, []string{"hi"}, buildPrompts("", files, []string{"hi"}))
}

func TestInputMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.txt")

	_, err := fileText([]string{missing})

	require.Error(t, err)
	assert.Equal(t, "File not found: "+missing, err.Error())
}

func TestInputRelativeFileIsAbsolute(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rel.txt"), []byte("r"), 0o600))
	t.Chdir(dir)
	abs, err := filepath.Abs("rel.txt")
	require.NoError(t, err)

	got, err := fileText([]string{"rel.txt"})

	require.NoError(t, err)
	assert.Equal(t, "<file name=\""+abs+"\">\nr\n</file>\n", got)
}

func TestInputPrompts(t *testing.T) {
	tests := []struct {
		name     string
		stdin    string
		messages []string
		want     []string
	}{
		{"stdin and message join with no separator", "a", []string{"b"}, []string{"ab"}},
		{"stdin alone", "a", nil, []string{"a"}},
		{"nothing", "", nil, nil},
		{"an empty first message is dropped", "", []string{"", "b"}, []string{"b"}},
		{"a leading slash stays text", "", []string{"/help"}, []string{"/help"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildPrompts(tt.stdin, "", tt.messages))
		})
	}
}

func TestInputWhitespaceStdinIsEmpty(t *testing.T) {
	got, err := readPipedStdin(strings.NewReader(" \n\t"))

	require.NoError(t, err)
	assert.Equal(t, "", got)
}
