package tools_test

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tools"
)

func TestTruncateHeadKeepsTextUnderBothLimits(t *testing.T) {
	got := tools.TruncateHead("a\nb\nc", 0, 0)
	assert.False(t, got.Truncated)
	assert.Equal(t, "a\nb\nc", got.Text)
	assert.Equal(t, 3, got.Truncation.TotalLines)
	assert.Equal(t, 3, got.Truncation.OutputLines)
	assert.Equal(t, 5, got.Truncation.OutputBytes)
	assert.Zero(t, got.NextOffset)
}

func TestTruncateHeadStopsAtLineLimit(t *testing.T) {
	var lines []string
	for i := 1; i <= 3000; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	text := strings.Join(lines, "\n")

	got := tools.TruncateHead(text, 0, 0)
	require.True(t, got.Truncated)
	assert.Equal(t, "lines", got.Truncation.TruncatedBy)
	assert.Equal(t, 3000, got.Truncation.TotalLines)
	assert.Equal(t, len(text), got.Truncation.TotalBytes)
	assert.Equal(t, tools.DefaultMaxLines, got.Truncation.OutputLines)
	assert.Equal(t, strings.Join(lines[:2000], "\n"), got.Text)
	assert.Equal(t, len(got.Text), got.Truncation.OutputBytes)
	assert.Equal(t, 2001, got.NextOffset)
	assert.False(t, got.FirstLineExceedsLimit)
}

func TestTruncateHeadStopsAtByteLimit(t *testing.T) {
	line := strings.Repeat("x", 6*1024)
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = line
	}
	text := strings.Join(lines, "\n")
	require.Greater(t, len(text), 60*1024)

	got := tools.TruncateHead(text, 0, 0)
	require.True(t, got.Truncated)
	assert.Equal(t, "bytes", got.Truncation.TruncatedBy)
	assert.Equal(t, 10, got.Truncation.TotalLines)
	assert.Equal(t, 8, got.Truncation.OutputLines)
	assert.LessOrEqual(t, got.Truncation.OutputBytes, tools.DefaultMaxBytes)
	assert.Equal(t, len(got.Text), got.Truncation.OutputBytes)
	assert.Equal(t, 9, got.NextOffset)
	assert.False(t, strings.HasSuffix(got.Text, "\n"), "kept text does not end inside a separator")
}

func TestTruncateHeadNeverSplitsRune(t *testing.T) {
	// One line of three-byte runes: no byte limit that is not a multiple of
	// three may cut between the bytes of a rune.
	text := strings.Repeat("€", 100)
	for limit := 1; limit < 40; limit++ {
		got := tools.TruncateHead(text, 10, limit)
		require.True(t, got.Truncated)
		assert.True(t, utf8.ValidString(got.Text), "limit %d gave invalid UTF-8", limit)
		assert.LessOrEqual(t, len(got.Text), limit)
		assert.True(t, got.FirstLineExceedsLimit)
		assert.Zero(t, got.Truncation.OutputLines)
		assert.Equal(t, len(got.Text), got.Truncation.OutputBytes)
	}

	// A later line holds the multibyte text; whole lines are kept or dropped.
	got := tools.TruncateHead("ab\n"+strings.Repeat("€", 10), 10, 6)
	assert.Equal(t, "ab", got.Text)
	assert.Equal(t, 1, got.Truncation.OutputLines)
	assert.Equal(t, 2, got.NextOffset)
	assert.False(t, got.FirstLineExceedsLimit)
}
