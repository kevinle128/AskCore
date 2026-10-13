package sandbox

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// outputOf feeds data through a buffer in 7 KB chunks and returns its Output.
func outputOf(data []byte) Output {
	var b buffer
	fillBuffer(&b, data, 7*1024)
	head, tail, total := b.snapshot()
	return Output{
		Head:    head,
		Tail:    tail,
		Total:   total,
		Dropped: total - int64(len(head)) - int64(len(tail)),
	}
}

var elisionMarker = regexp.MustCompile(`\n\[\.\.\. (\d+) bytes elided \.\.\.\]\n`)

func TestViewElidesMiddleAndNamesSpill(t *testing.T) {
	var sb strings.Builder
	for i := 0; sb.Len() < 200_000; i++ {
		fmt.Fprintf(&sb, "%06d %s\n", i, strings.Repeat("x", 92))
	}
	data := []byte(sb.String())
	out := outputOf(data)
	out.SpillPath = "/tmp/ask-jobs-test/bash-1.log"

	text, tr := View(out, DefaultMaxLines, DefaultMaxBytes)

	assert.True(t, strings.HasPrefix(text, string(data[:100])))
	m := elisionMarker.FindStringSubmatch(text)
	require.NotNil(t, m, "marker missing")
	n, err := strconv.ParseInt(m[1], 10, 64)
	require.NoError(t, err)
	assert.Equal(t, out.Dropped, n)
	assert.Contains(t, text, string(data[len(data)-100:]))
	assert.True(t, strings.HasSuffix(text, "[full output: /tmp/ask-jobs-test/bash-1.log]"))
	assert.NotContains(t, text, "spill file capped")
	assert.True(t, tr.Truncated)
	assert.Equal(t, out.Dropped, tr.ElidedBytes)
	assert.Equal(t, int64(len(data)), tr.TotalBytes)
	assert.Equal(t, out.SpillPath, tr.SpillPath)
}

func TestViewNamesCappedSpill(t *testing.T) {
	out := outputOf([]byte(strings.Repeat("y", 100_000)))
	out.SpillPath = "/tmp/full.log"
	out.SpillCapped = true

	text, tr := View(out, DefaultMaxLines, DefaultMaxBytes)

	assert.Contains(t, text, "[full output: /tmp/full.log]")
	assert.True(t, strings.HasSuffix(text, "[spill file capped at 64 MB]"))
	assert.True(t, tr.SpillCapped)
}

func TestViewShortOutputIsUntouched(t *testing.T) {
	text, tr := View(outputOf([]byte("hello\nworld\n")), DefaultMaxLines, DefaultMaxBytes)
	assert.Equal(t, "hello\nworld\n", text)
	assert.False(t, tr.Truncated)
	assert.Zero(t, tr.ElidedBytes)
	assert.Equal(t, 2, tr.ShownLines)
}

func TestViewCutsAt2000Lines(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 5000; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	out := outputOf([]byte(sb.String()))
	require.Zero(t, out.Dropped, "the fixture must fit the buffer")

	text, tr := View(out, DefaultMaxLines, DefaultMaxBytes)

	assert.True(t, tr.Truncated)
	assert.Equal(t, DefaultMaxLines, tr.ShownLines)
	assert.True(t, strings.HasPrefix(text, "line 1\n"))
	assert.Contains(t, text, "line 800\n")
	assert.NotContains(t, text, "line 801\n")
	assert.Contains(t, text, "line 3801\n")
	assert.NotContains(t, text, "line 3800\n")
	assert.True(t, strings.HasSuffix(text, "line 5000\n"))
	assert.NotNil(t, elisionMarker.FindStringSubmatch(text))
}

func TestViewHandlesTailWithoutNewline(t *testing.T) {
	out := outputOf([]byte(strings.Repeat("x", 200_000)))

	text, tr := View(out, DefaultMaxLines, DefaultMaxBytes)

	assert.True(t, tr.Truncated)
	assert.Equal(t, headSize+tailSize, strings.Count(text, "x"))
	assert.NotNil(t, elisionMarker.FindStringSubmatch(text))
	assert.Equal(t, int64(200_000-headSize-tailSize), tr.ElidedBytes)
}

func TestViewStripsAnsiAndCollapsesCarriageReturns(t *testing.T) {
	in := "\x1b[31mred\x1b[0m\n10%\r50%\r100%\n\x1b]0;title\x07done\r\nlast\r"
	text, tr := View(outputOf([]byte(in)), DefaultMaxLines, DefaultMaxBytes)
	assert.Equal(t, "red\n100%\ndone\nlast", text)
	assert.False(t, tr.Truncated)
}

func TestViewDropsIncompleteEscapeAtCut(t *testing.T) {
	in := strings.Repeat("a", headSize-2) + "\x1b[31;1" + strings.Repeat("b", 100_000)
	text, _ := View(outputOf([]byte(in)), DefaultMaxLines, DefaultMaxBytes)
	assert.NotContains(t, text, "\x1b")
}

func TestViewNeverSplitsRune(t *testing.T) {
	// The euro sign has 3 bytes; the buffer cut at both ends falls inside a rune.
	data := []byte(strings.Repeat("€", 70_000) + "ab")
	out := outputOf(data)
	require.False(t, utf8.Valid(out.Head), "fixture must cut the head inside a rune")
	require.False(t, utf8.Valid(out.Tail), "fixture must cut the tail inside a rune")

	text, tr := View(out, DefaultMaxLines, DefaultMaxBytes)

	assert.True(t, utf8.ValidString(text))
	assert.True(t, tr.Truncated)
	assert.True(t, strings.HasPrefix(text, "€"))
	assert.Equal(t, 0, strings.Count(text, "�"))

	// A byte limit inside a rune of a short contiguous output.
	small, _ := View(outputOf([]byte(strings.Repeat("€", 100))), DefaultMaxLines, 10)
	assert.True(t, utf8.ValidString(small))
}
