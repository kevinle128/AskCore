package tools_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

func numberedLines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

func TestReadNumbersEveryLine(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "a\nb\nc\n")

	res := h.mustRun("read", map[string]any{"path": "a.txt"})

	assert.Equal(t, "1|a\n2|b\n3|c", resultText(t, res))
	d := resultDetails(t, res)
	assert.Equal(t, protocol.ToolKindRead, d.Kind)
	assert.Equal(t, h.path("a.txt"), d.Path)
	assert.Equal(t, []protocol.ToolLocation{{Path: h.path("a.txt"), Line: 1}}, d.Locations)
	assert.Nil(t, d.Truncation)
	assert.Zero(t, d.NextOffset)
}

func TestReadKeepsLinesWithoutFinalNewline(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "a\nb")

	assert.Equal(t, "1|a\n2|b", resultText(t, h.mustRun("read", map[string]any{"path": "a.txt"})))
}

func TestReadOfEmptyFileGivesNoLines(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "")

	assert.Equal(t, "", resultText(t, h.mustRun("read", map[string]any{"path": "a.txt"})))
}

func TestReadTruncatesAndGivesNextOffset(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("big.txt", numberedLines(3000))

	res := h.mustRun("read", map[string]any{"path": "big.txt"})

	text := resultText(t, res)
	assert.True(t, strings.HasPrefix(text, "1|line 1\n2|line 2\n"))
	assert.Contains(t, text, "\n2000|line 2000\n")
	assert.NotContains(t, text, "2001|")
	assert.True(t, strings.HasSuffix(text, "\n\n[Showing lines 1-2000 of 3000. Use offset=2001 to continue.]"), text[len(text)-80:])
	d := resultDetails(t, res)
	require.NotNil(t, d.Truncation)
	assert.Equal(t, "lines", d.Truncation.TruncatedBy)
	assert.Equal(t, 2001, d.NextOffset)

	// The next page starts where the notice says and ends the file.
	res = h.mustRun("read", map[string]any{"path": "big.txt", "offset": 2001})
	text = resultText(t, res)
	assert.True(t, strings.HasPrefix(text, "2001|line 2001\n"))
	assert.True(t, strings.HasSuffix(text, "3000|line 3000"))
	assert.Equal(t, protocol.ToolLocation{Path: h.path("big.txt"), Line: 2001}, resultDetails(t, res).Locations[0])
}

func TestReadTruncatesByBytes(t *testing.T) {
	h := newFileHarness(t, nil)
	line := strings.Repeat("x", 700)
	h.put("wide.txt", strings.Repeat(line+"\n", 100))

	res := h.mustRun("read", map[string]any{"path": "wide.txt"})

	text := resultText(t, res)
	assert.Contains(t, text, "(50.0KB limit). Use offset=")
	d := resultDetails(t, res)
	require.NotNil(t, d.Truncation)
	assert.Equal(t, "bytes", d.Truncation.TruncatedBy)
	assert.Greater(t, d.NextOffset, 1)
}

func TestReadLimitStopsEarlyAndSaysHowToContinue(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", numberedLines(30))

	res := h.mustRun("read", map[string]any{"path": "a.txt", "offset": 5, "limit": 10})

	text := resultText(t, res)
	assert.True(t, strings.HasPrefix(text, "5|line 5\n"))
	assert.Contains(t, text, "\n14|line 14\n")
	assert.NotContains(t, text, "15|line 15")
	assert.True(t, strings.HasSuffix(text, "[16 more lines in file. Use offset=15 to continue.]"))
	assert.Equal(t, 15, resultDetails(t, res).NextOffset)
}

func TestReadOffsetPastEndGivesHint(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", numberedLines(10))

	res, err := h.run("read", map[string]any{"path": "a.txt", "offset": 50})

	require.NoError(t, err, "the hint is a normal result")
	assert.Equal(t, "Offset 50 is beyond end of file (10 lines total)", resultText(t, res))
}

func TestReadHugeFirstLineGivesSedHint(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("big.txt", strings.Repeat("x", 60*1024)+"\nsecond\n")

	res := h.mustRun("read", map[string]any{"path": "big.txt"})

	assert.Equal(t, "[Line 1 is 60.0KB, exceeds 50.0KB limit. Use bash: sed -n '1p' big.txt | head -c 51200]", resultText(t, res))
}

func TestReadCutsLongLineInModelViewOnly(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", strings.Repeat("x", 2000)+"\nshort\n")

	res := h.mustRun("read", map[string]any{"path": "a.txt"})

	assert.Equal(t, "1|"+strings.Repeat("x", 768)+" … [line cut]\n2|short", resultText(t, res))
	d := resultDetails(t, res)
	assert.Nil(t, d.Truncation, "a cut line is not a truncated read")
}

func TestReadKeepsDetailCountsOfUncutText(t *testing.T) {
	h := newFileHarness(t, nil)
	// 100 lines of 700 bytes are over 50 KB before any line is cut.
	h.put("wide.txt", strings.Repeat(strings.Repeat("y", 700)+"\n", 100))

	res := h.mustRun("read", map[string]any{"path": "wide.txt"})

	d := resultDetails(t, res)
	require.NotNil(t, d.Truncation)
	assert.Equal(t, 100, d.Truncation.TotalLines)
	assert.Greater(t, d.Truncation.TotalBytes, 50*1024)
}

func TestReadRejectsBinaryByContent(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("nul.bin", "text\x00more")
	ctrl := strings.Repeat("\x01\x02\x03\x04\x05", 40) + strings.Repeat("ab", 150) // 40% control bytes
	h.put("ctrl.bin", ctrl)

	for _, name := range []string{"nul.bin", "ctrl.bin"} {
		_, err := h.run("read", map[string]any{"path": name})
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), name)
		assert.Contains(t, err.Error(), "binary")
	}
}

func TestReadAcceptsTextWithTabsAndUnicode(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "col1\tcol2\r\nvalue é中\n")

	res := h.mustRun("read", map[string]any{"path": "a.txt"})

	assert.Equal(t, "1|col1\tcol2\n2|value é中", resultText(t, res))
}

func TestReadTriesScreenshotVariant(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("Screenshot 2024-01-01 at 10.00.00 AM.txt", "from a screenshot name\n")

	res := h.mustRun("read", map[string]any{"path": "Screenshot 2024-01-01 at 10.00.00 AM.txt"})

	assert.Equal(t, "1|from a screenshot name", resultText(t, res))
}

func TestReadAcceptsNullAndStringNumbers(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", numberedLines(5))

	res := h.mustRun("read", `{"path":"a.txt","offset":null,"limit":"2"}`)

	assert.True(t, strings.HasPrefix(resultText(t, res), "1|line 1\n2|line 2\n"))
	res = h.mustRun("read", `{"path":"a.txt","offset":"4"}`)
	assert.True(t, strings.HasPrefix(resultText(t, res), "4|line 4\n"))
}

func TestReadMissingFileNamesPath(t *testing.T) {
	h := newFileHarness(t, nil)

	_, err := h.run("read", map[string]any{"path": "nope.txt"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nope.txt")
}

func TestReadDirectoryGivesError(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("d/a.txt", "x")

	_, err := h.run("read", map[string]any{"path": "d"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "directory")
}

func TestReadAcceptsAtPrefixAndFileURL(t *testing.T) {
	h := newFileHarness(t, nil)
	h.put("a.txt", "hi\n")

	assert.Equal(t, "1|hi", resultText(t, h.mustRun("read", map[string]any{"path": "@a.txt"})))
	assert.Equal(t, "1|hi", resultText(t, h.mustRun("read", map[string]any{"path": "file://" + h.path("a.txt")})))
}
