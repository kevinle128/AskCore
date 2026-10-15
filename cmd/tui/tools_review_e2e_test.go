package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintReadLargePageNumbers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "text.txt")
	require.NoError(t, os.WriteFile(path, []byte("a\nb\nc\n"), 0o600))
	m := scriptModel(t,
		call(t, "read", map[string]any{"path": path, "offset": 2, "limit": 1e30}),
		call(t, "read", map[string]any{"path": path, "offset": 1e30}),
		say("done"),
	)
	runPrint(t, m, printSpec{}).requireOK(t)
	assert.Equal(t, "2|b\n3|c", m.toolResult(1).Text)
	assert.False(t, m.toolResult(1).IsError)
	assert.Contains(t, m.toolResult(2).Text, "beyond end of file")
	assert.False(t, m.toolResult(2).IsError)
}

func TestPrintEditRejectsOverlappingOccurrences(t *testing.T) {
	for _, tc := range []struct{ name, content, old string }{
		{"exact", "aaa\n", "aa"},
		{"normalized", "–––\n", "--"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "text.txt")
			require.NoError(t, os.WriteFile(path, []byte(tc.content), 0o600))
			m := scriptModel(t, call(t, "edit", map[string]any{"path": path,
				"edits": []map[string]any{{"oldText": tc.old, "newText": "changed"}}}), say("done"))
			runPrint(t, m, printSpec{}).requireOK(t)
			assert.True(t, m.toolResult(1).IsError)
			assert.Contains(t, m.toolResult(1).Text, "occurrences")
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tc.content, string(data))
		})
	}
}

func TestPrintFileAndSearchToolsTryPathVariants(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Screenshot 2026-10-11 at 9.00.00 AM.dir")
	require.NoError(t, os.Mkdir(dir, 0o700))
	path := filepath.Join(dir, "note.txt")
	require.NoError(t, os.WriteFile(path, []byte("before\n"), 0o600))
	given := filepath.Join(filepath.Dir(dir), "Screenshot 2026-10-11 at 9.00.00 AM.dir", "note.txt")
	m := scriptModel(t,
		call(t, "grep", map[string]any{"path": given, "pattern": "before"}),
		call(t, "find", map[string]any{"path": filepath.Dir(given), "pattern": "*.txt"}),
		call(t, "write", map[string]any{"path": given, "content": "after\n"}),
		say("done"),
	)
	runPrint(t, m, printSpec{}).requireOK(t)
	for i := 1; i <= 3; i++ {
		assert.False(t, m.toolResult(i).IsError, m.toolResult(i).Text)
	}
	assert.Contains(t, m.toolResult(1).Text, "note.txt:1: before")
	assert.Equal(t, "note.txt", m.toolResult(2).Text)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "after\n", string(data))
	_, err = os.Stat(given)
	assert.True(t, os.IsNotExist(err), "the alternate spelling must not create a second file")
}

func TestPrintFileToolsRejectNamedPipes(t *testing.T) {
	for _, name := range []string{"read", "edit"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pipe")
			require.NoError(t, syscall.Mkfifo(path, 0o600))
			writerDone := make(chan error, 1)
			args := map[string]any{"path": path}
			if name == "edit" {
				args["edits"] = []map[string]any{{"oldText": "before", "newText": "after"}}
			}
			step := callAfter(t, func([]byte) {
				// Release a reader if the tool incorrectly opens the pipe.
				go func() {
					time.Sleep(150 * time.Millisecond)
					f, err := os.OpenFile(path, os.O_RDWR, 0)
					if err == nil {
						_, err = f.WriteString("before\n")
						closeErr := f.Close()
						if err == nil {
							err = closeErr
						}
					}
					writerDone <- err
				}()
			}, name, args)
			m := scriptModel(t, step, say("done"))
			runPrint(t, m, printSpec{}).requireOK(t)
			assert.True(t, m.toolResult(1).IsError)
			assert.Contains(t, m.toolResult(1).Text, "not a regular file")
			require.NoError(t, <-writerDone)
			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.NotZero(t, info.Mode()&os.ModeNamedPipe)
		})
	}
}

func TestPrintFindMatchesUnicodeNamesAndIgnoreRules(t *testing.T) {
	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repo, "café.txt"), []byte("keep"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "秘密.txt"), []byte("ignore"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("秘密.txt\n"), 0o600))
	t.Setenv("PATH", t.TempDir())
	m := scriptModel(t,
		call(t, "find", map[string]any{"path": repo, "pattern": "café.*"}),
		call(t, "find", map[string]any{"path": repo, "pattern": "*.txt"}),
		say("done"),
	)
	runPrint(t, m, printSpec{}).requireOK(t)
	assert.Equal(t, "café.txt", m.toolResult(1).Text)
	assert.Equal(t, "café.txt", m.toolResult(2).Text)
}

func TestPrintFindKeepsFilenameSpaces(t *testing.T) {
	rg := requireRipgrep(t)
	bin := t.TempDir()
	require.NoError(t, os.Symlink(rg, filepath.Join(bin, "rg")))
	t.Setenv("PATH", bin)
	repo := t.TempDir()
	const name = " file.txt "
	require.NoError(t, os.WriteFile(filepath.Join(repo, name), []byte("content"), 0o600))
	m := scriptModel(t, call(t, "find", map[string]any{"path": repo, "pattern": "*"}), say("done"))
	runPrint(t, m, printSpec{}).requireOK(t)
	assert.Equal(t, name, m.toolResult(1).Text)
}

func TestPrintWriteRefusesGeneratorMarkerAfterLongHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.go")
	content := "// " + strings.Repeat("x", 5000) + "\n// Code generated by protoc-gen-go. DO NOT EDIT.\npackage generated\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	m := scriptModel(t, call(t, "write", map[string]any{"path": path, "content": "package replacement\n"}), say("done"))
	runPrint(t, m, printSpec{}).requireOK(t)
	assert.True(t, m.toolResult(1).IsError)
	assert.Contains(t, m.toolResult(1).Text, "generator marker")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
}

func TestPrintGrepKeepsMatchesOnLargeLines(t *testing.T) {
	requireRipgrep(t)
	path := filepath.Join(t.TempDir(), "minified.txt")
	content := strings.Repeat("x", 5<<20) + "MATCH\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	m := scriptModel(t, call(t, "grep", map[string]any{"path": path, "pattern": "MATCH"}), say("done"))
	runPrint(t, m, printSpec{}).requireOK(t)
	assert.False(t, m.toolResult(1).IsError)
	assert.Contains(t, m.toolResult(1).Text, "minified.txt:1: "+strings.Repeat("x", 500)+"... [truncated]")
	assert.Contains(t, m.toolResult(1).Text, "Some lines truncated to 500 chars")
}
