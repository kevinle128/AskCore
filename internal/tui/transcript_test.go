package tui

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"
)

// textBlock is a plain block: its text wraps at the render width.
type textBlock struct{ text string }

func (b textBlock) Lines(rc RenderContext) []string { return wrapLines(b.text, rc.Width) }

func testContext(width int) RenderContext {
	return RenderContext{Width: width, Theme: NewTheme(colorprofile.NoTTY)}
}

func TestCommitFrontierOrderedOnce(t *testing.T) {
	tr := NewTranscript()
	rc := testContext(40)

	require.NoError(t, tr.Append("a", textBlock{"alpha"}, false))
	require.NoError(t, tr.Append("b", textBlock{"beta"}, true))
	require.NoError(t, tr.Append("c", textBlock{"gamma"}, true))

	require.Equal(t, 0, tr.Frontier(), "a stable block cannot pass an earlier mutable block")
	require.Empty(t, tr.TakeStable(rc))
	require.Equal(t, []string{"alpha", "beta", "gamma"}, tr.TailLines(rc), "uncommitted blocks stay in the tail")

	require.NoError(t, tr.Replace("a", textBlock{"alpha two"}))
	require.NoError(t, tr.Finish("a"))
	require.Equal(t, 3, tr.Frontier())

	require.Equal(t, []string{"alpha two", "beta", "gamma"}, tr.TakeStable(rc), "all blocks leave in order")
	require.Empty(t, tr.TakeStable(rc), "no block leaves twice")
	require.Equal(t, 3, tr.Committed())
	require.Empty(t, tr.TailLines(rc))

	require.NoError(t, tr.Append("d", textBlock{"delta"}, false))
	require.Equal(t, []string{"delta"}, tr.TailLines(rc))
	require.Empty(t, tr.TakeStable(rc))
	require.NoError(t, tr.Finish("d"))
	require.Equal(t, []string{"delta"}, tr.TakeStable(rc))
}

func TestTranscriptRejectsInvalidChanges(t *testing.T) {
	tr := NewTranscript()
	rc := testContext(40)
	require.NoError(t, tr.Append("a", textBlock{"x"}, true))
	require.ErrorIs(t, tr.Append("a", textBlock{"y"}, true), ErrDuplicateBlock)
	require.ErrorIs(t, tr.Replace("missing", textBlock{"y"}), ErrUnknownBlock)
	require.ErrorIs(t, tr.Finish("missing"), ErrUnknownBlock)
	require.ErrorIs(t, tr.Append("", textBlock{"y"}, true), ErrBlockID)
	require.ErrorIs(t, tr.Append("nil", nil, true), ErrBlockContent)

	tr.TakeStable(rc)
	require.ErrorIs(t, tr.Replace("a", textBlock{"changed"}), ErrCommitted, "committed output is in the scrollback and cannot change")
	require.NoError(t, tr.Finish("a"), "finishing a committed block is harmless")
}

func TestTranscriptVersionAndRebuild(t *testing.T) {
	tr := NewTranscript()
	require.NoError(t, tr.Append("a", textBlock{"one two three four five six seven eight nine ten"}, true))
	require.NoError(t, tr.Append("b", textBlock{"tail block"}, false))
	v0, ok := tr.Version("b")
	require.True(t, ok)
	require.NoError(t, tr.Replace("b", textBlock{"tail block longer"}))
	v1, _ := tr.Version("b")
	require.Greater(t, v1, v0)
	_, ok = tr.Version("zzz")
	require.False(t, ok)

	wide := testContext(60)
	narrow := testContext(20)
	require.Len(t, tr.TakeStable(wide), 1)
	require.Equal(t, 1, tr.Committed())

	before := tr.Committed()
	rebuilt := tr.CommittedLines(narrow)
	require.Greater(t, len(rebuilt), 1, "committed content wraps at the new width")
	require.Equal(t, before, tr.Committed(), "a rebuild does not move the frontier")
	require.Empty(t, tr.TakeStable(narrow), "and does not commit again")
}

func TestTranscriptCommitsManyBlocksInOrder(t *testing.T) {
	tr := NewTranscript()
	rc := testContext(30)
	var want []string
	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("b%02d", i)
		require.NoError(t, tr.Append(id, textBlock{id}, true))
		want = append(want, id)
	}
	require.Equal(t, want, tr.TakeStable(rc))
}
