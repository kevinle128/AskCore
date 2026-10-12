package tui

import (
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

// entryContext returns the context for one block. The context fold mode wins
// when it is set above FoldCollapsed; otherwise the block shows in its default
// mode, as it does in the block pane before the user changes it.
func entryContext(rc RenderContext, b Block) RenderContext {
	if rc.Fold == FoldCollapsed {
		rc.Fold = blockDefaultFold(b)
	}
	return rc
}

// rowsFrom draws the blocks from index first on, as the block pane does.
func rowsFrom(tr *Transcript, rc RenderContext, first int) []string {
	var rows []string
	for _, e := range tr.entries[first:] {
		rows = append(rows, renderRows(e.block, entryContext(rc, e.block))...)
	}
	return rows
}

// allRows draws every block of the transcript.
func allRows(tr *Transcript, rc RenderContext) []string { return rowsFrom(tr, rc, 0) }

// liveRows draws the blocks behind the frontier: the ones whose content can
// still change or that wait for an earlier block.
func liveRows(tr *Transcript, rc RenderContext) []string { return rowsFrom(tr, rc, tr.Frontier()) }

// finalRows draws the blocks before the frontier.
func finalRows(tr *Transcript, rc RenderContext) []string {
	var rows []string
	for _, e := range tr.entries[:tr.Frontier()] {
		rows = append(rows, renderRows(e.block, entryContext(rc, e.block))...)
	}
	return rows
}

func TestFrontierStopsAtTheFirstMutableBlock(t *testing.T) {
	tr := NewTranscript()
	rc := testContext(40)

	require.NoError(t, tr.Append("a", textBlock{"alpha"}, false))
	require.NoError(t, tr.Append("b", textBlock{"beta"}, true))
	require.NoError(t, tr.Append("c", textBlock{"gamma"}, true))

	require.Equal(t, 0, tr.Frontier(), "a stable block does not pass an earlier mutable block")
	require.Equal(t, []string{"alpha", "beta", "gamma"}, liveRows(tr, rc))
	require.Empty(t, finalRows(tr, rc))

	require.NoError(t, tr.Replace("a", textBlock{"alpha two"}))
	require.NoError(t, tr.Finish("a"))
	require.Equal(t, 3, tr.Frontier())
	require.Equal(t, []string{"alpha two", "beta", "gamma"}, finalRows(tr, rc))
	require.Empty(t, liveRows(tr, rc))

	require.NoError(t, tr.Append("d", textBlock{"delta"}, false))
	require.Equal(t, 3, tr.Frontier())
	require.Equal(t, []string{"delta"}, liveRows(tr, rc))
	require.NoError(t, tr.Finish("d"))
	require.Equal(t, 4, tr.Frontier())
}

func TestTranscriptRejectsInvalidChanges(t *testing.T) {
	tr := NewTranscript()
	require.NoError(t, tr.Append("a", textBlock{"x"}, true))
	require.ErrorIs(t, tr.Append("a", textBlock{"y"}, true), ErrDuplicateBlock)
	require.ErrorIs(t, tr.Replace("missing", textBlock{"y"}), ErrUnknownBlock)
	require.ErrorIs(t, tr.Finish("missing"), ErrUnknownBlock)
	require.ErrorIs(t, tr.Append("", textBlock{"y"}, true), ErrBlockID)
	require.ErrorIs(t, tr.Append("nil", nil, true), ErrBlockContent)
	require.ErrorIs(t, tr.Replace("a", nil), ErrBlockContent)
}

func TestTranscriptVersionGrowsWithEachReplace(t *testing.T) {
	tr := NewTranscript()
	require.NoError(t, tr.Append("b", textBlock{"tail block"}, false))
	v0, ok := tr.Version("b")
	require.True(t, ok)
	require.NoError(t, tr.Replace("b", textBlock{"tail block longer"}))
	v1, _ := tr.Version("b")
	require.Greater(t, v1, v0)
	_, ok = tr.Version("zzz")
	require.False(t, ok)
}

func TestTranscriptRowsWrapAtTheRenderWidth(t *testing.T) {
	tr := NewTranscript()
	require.NoError(t, tr.Append("a", textBlock{"one two three four five six seven eight nine ten"}, true))
	require.Len(t, allRows(tr, testContext(60)), 1)
	require.Greater(t, len(allRows(tr, testContext(20))), 1, "the same block wraps at the new width")
}
