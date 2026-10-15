package tools_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func TestLsSortsAndMarksDirectories(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("beta.txt", "")
	h.put("Alpha.txt", "")
	h.put("zeta.txt", "")
	h.put(".hidden", "")
	h.put("Docs/readme.md", "")
	require.NoError(t, os.Mkdir(h.path("assets"), 0o755))

	res := h.mustRun("ls", map[string]any{})

	assert.Equal(t, ".hidden\nAlpha.txt\nassets/\nbeta.txt\nDocs/\nzeta.txt", resultText(t, res))
	d := resultDetails(t, res)
	assert.Equal(t, protocol.ToolKindSearch, d.Kind)
	assert.Equal(t, h.cwd, d.Path)
}

func TestLsListsNamedDirectory(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("sub/a.txt", "")
	h.put("sub/b.txt", "")

	assert.Equal(t, "a.txt\nb.txt", resultText(t, h.mustRun("ls", map[string]any{"path": "sub"})))
}

func TestLsEmptyDirectory(t *testing.T) {
	h := newFileHarness(t, nil)
	require.NoError(t, os.Mkdir(h.path("empty"), 0o755))

	assert.Equal(t, "(empty directory)", resultText(t, h.mustRun("ls", map[string]any{"path": "empty"})))
}

func TestLsErrors(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("f.txt", "")

	_, err := h.run("ls", map[string]any{"path": "nope"})
	require.Error(t, err)
	assert.EqualError(t, err, "Path not found: "+h.path("nope"))

	_, err = h.run("ls", map[string]any{"path": "f.txt"})
	require.Error(t, err)
	assert.EqualError(t, err, "Not a directory: "+h.path("f.txt"))
}

func TestLsLimitNotice(t *testing.T) {
	h := newFileHarness(t, nil)
	for i := 0; i < 600; i++ {
		h.put(fmt.Sprintf("f%03d.txt", i), "")
	}

	res := h.mustRun("ls", map[string]any{})
	lines := strings.Split(resultText(t, res), "\n")
	require.Len(t, lines, 502)
	assert.Equal(t, "f000.txt", lines[0])
	assert.Equal(t, "f499.txt", lines[499])
	assert.Equal(t, "", lines[500])
	assert.Equal(t, "[500 entries limit reached. Use limit=1000 for more]", lines[501])

	res = h.mustRun("ls", map[string]any{"limit": 3})
	assert.Equal(t, "f000.txt\nf001.txt\nf002.txt\n\n[3 entries limit reached. Use limit=6 for more]", resultText(t, res))
}

func TestLsFollowsSymlinkToDirectory(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("real/a.txt", "")
	require.NoError(t, os.Symlink(h.path("real"), h.path("link")))
	require.NoError(t, os.Symlink(h.path("missing"), h.path("broken")))

	assert.Equal(t, "link/\nreal/", resultText(t, h.mustRun("ls", map[string]any{})))
}

func TestLsByteCapNotice(t *testing.T) {
	h := newFileHarness(t, nil)
	name := strings.Repeat("n", 200)
	for i := 0; i < 400; i++ {
		h.put(fmt.Sprintf("%s%03d", name, i), "")
	}

	res := h.mustRun("ls", map[string]any{"limit": 1000})

	text := resultText(t, res)
	assert.True(t, strings.HasSuffix(text, "[50.0KB limit reached]"), text[len(text)-60:])
	d := resultDetails(t, res)
	require.NotNil(t, d.Truncation)
	assert.Equal(t, "bytes", d.Truncation.TruncatedBy)
}
