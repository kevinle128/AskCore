package tui

import (
	"fmt"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// --- helpers ---

// rowsBlock is a block of fixed rows. draws counts how often it drew.
type rowsBlock struct {
	rows  []string
	draws *int
}

func (b rowsBlock) Lines(RenderContext) []string {
	if b.draws != nil {
		*b.draws++
	}
	return b.rows
}

// labelRows returns n rows "<id>.<i>".
func labelRows(id string, n int) []string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = fmt.Sprintf("%s.%d", id, i)
	}
	return rows
}

// paneRig is a pane over a transcript of blocks "b0", "b1", ... with the
// given row counts.
type paneRig struct {
	t     *testing.T
	tr    *Transcript
	pane  *Pane
	draws map[string]*int
}

func newPaneRig(t *testing.T, width, height int, counts ...int) *paneRig {
	t.Helper()
	r := &paneRig{t: t, tr: NewTranscript(), draws: map[string]*int{}}
	for _, n := range counts {
		r.add(n)
	}
	r.pane = NewPane(r.tr)
	r.pane.SetTheme(NewTheme(colorprofile.TrueColor))
	r.pane.SetSize(width, height)
	return r
}

// add appends a block of n rows and returns its id.
func (r *paneRig) add(n int) string {
	r.t.Helper()
	id := fmt.Sprintf("b%d", r.tr.Len())
	d := new(int)
	r.draws[id] = d
	require.NoError(r.t, r.tr.Append(id, rowsBlock{rows: labelRows(id, n), draws: d}, true))
	return id
}

// grow appends a row to the block and bumps its version.
func (r *paneRig) grow(id string, rows []string) {
	r.t.Helper()
	require.NoError(r.t, r.tr.Replace(id, rowsBlock{rows: rows, draws: r.draws[id]}))
}

// takeDraws returns the draw count of all blocks and sets the counters to zero.
func (r *paneRig) takeDraws() int {
	total := 0
	for _, d := range r.draws {
		total += *d
		*d = 0
	}
	return total
}

// SetFold sets the mode of a block as the user's choice, the way the fold
// commands do. A block that cannot fold keeps the mode as a plain value.
func (p *Pane) SetFold(id string, m FoldMode) error {
	it, err := p.itemByID(id)
	if err != nil {
		return err
	}
	if fb, ok := p.tr.entries[p.tr.index[id]].block.(foldBlock); ok {
		it.setFold(m, fb)
		return nil
	}
	it.fold, it.userSet = m, true
	return nil
}

// --- window math ---

func TestPaneShortContentPadsToHeightWithoutMarker(t *testing.T) {
	r := newPaneRig(t, 40, 6, 2, 1)
	f := r.pane.Frame()
	require.Equal(t, []string{"b0.0", "b0.1", "", "b1.0", "", ""}, f.Rows, "one gap row after each block")
	require.True(t, r.pane.Following())
	require.False(t, f.Marker.Show)
}

func TestPaneFollowShowsTheTail(t *testing.T) {
	r := newPaneRig(t, 40, 5, 3, 3, 3, 3)
	f := r.pane.Frame()
	require.Equal(t, []string{"", "b3.0", "b3.1", "b3.2", ""}, f.Rows, "the window ends with the gap row of the last block")
	require.Equal(t, 2, f.FirstBlock, "the first row is the gap row of block 2")
	require.False(t, f.Marker.Show)
}

func TestPaneScrollUpTurnsFollowOffAndShowsEndMarker(t *testing.T) {
	r := newPaneRig(t, 40, 5, 3, 3, 3, 3)
	r.pane.ScrollBy(-3)
	f := r.pane.Frame()
	require.False(t, r.pane.Following())
	// The last row is the marker slot: the pane draws no content there.
	require.Equal(t, []string{"b2.0", "b2.1", "b2.2", "", ""}, f.Rows)
	require.Equal(t, PaneMarker{Show: true, Text: "▼ End", Pad: (40 - 5) / 2}, f.Marker)
}

func TestPaneScrollClampsAtBothEnds(t *testing.T) {
	r := newPaneRig(t, 40, 5, 3, 3, 3, 3)
	r.pane.ScrollBy(-1000)
	f := r.pane.Frame()
	require.Equal(t, "b0.0", f.Rows[0])
	require.Equal(t, 0, f.FirstBlock)
	require.False(t, r.pane.Following())

	r.pane.ScrollBy(1000)
	f = r.pane.Frame()
	require.True(t, r.pane.Following())
	require.Equal(t, []string{"b3.2", ""}, f.Rows[3:])
	require.False(t, f.Marker.Show)
}

func TestPaneScrollDownToLastRowTurnsFollowOn(t *testing.T) {
	r := newPaneRig(t, 40, 5, 3, 3, 3, 3)
	// The window holds height-1 rows while follow is off, so the last row is in
	// view from top row 12; the end window starts at row 11.
	r.pane.ScrollBy(-4)
	require.False(t, r.pane.Following())
	r.pane.ScrollBy(4)
	require.False(t, r.pane.Following())
	r.pane.ScrollBy(1)
	require.True(t, r.pane.Following())
	require.Equal(t, "b3.2", r.pane.Frame().Rows[3])
}

func TestPaneScrollOnShortContentKeepsFollow(t *testing.T) {
	r := newPaneRig(t, 40, 6, 2, 1)
	r.pane.ScrollBy(-3)
	require.True(t, r.pane.Following())
	require.False(t, r.pane.Frame().Marker.Show)
}

func TestPaneNewOutputWithFollowOffKeepsTopAndCountsRowsBelow(t *testing.T) {
	r := newPaneRig(t, 40, 5, 3, 3, 3, 3)
	r.pane.ScrollBy(-3)
	before := r.pane.Frame()
	require.Equal(t, "▼ End", before.Marker.Text)

	r.add(4)
	after := r.pane.Frame()
	require.Equal(t, before.Rows, after.Rows)
	require.Equal(t, before.FirstBlock, after.FirstBlock)
	// Below the window: the gap row of b2, b3 and its gap row, b4 and its gap
	// row.
	require.Equal(t, PaneMarker{Show: true, New: true, N: 9, Text: "▼ 9 new lines · End", Pad: (40 - 19) / 2}, after.Marker)

	// A delta of the last block grows N and does not move the window.
	r.grow("b4", labelRows("b4", 6))
	after = r.pane.Frame()
	require.Equal(t, before.Rows, after.Rows)
	require.Equal(t, 11, after.Marker.N)
}

func TestPaneChangeInsideTheWindowKeepsRowsBelowCounted(t *testing.T) {
	r := newPaneRig(t, 40, 5, 3, 3, 3, 3)
	r.pane.ScrollBy(-6)
	// Block 1 is in the window. A change there is output: the marker shows
	// the rows below the window, never zero.
	r.grow("b1", labelRows("b1", 4))
	m := r.pane.Frame().Marker
	require.True(t, m.Show)
	require.True(t, m.New)
	require.Positive(t, m.N)
}

func TestPaneContentThatShrinksIntoTheWindowTurnsFollowOn(t *testing.T) {
	r := newPaneRig(t, 40, 9, 3, 3, 3, 3)
	r.pane.ScrollBy(-3)
	require.False(t, r.pane.Following())
	// Only 8 rows remain, gap rows too: the whole content fits the window.
	for _, id := range []string{"b0", "b1", "b2", "b3"} {
		r.grow(id, labelRows(id, 1))
	}
	f := r.pane.Frame()
	require.True(t, r.pane.Following())
	require.False(t, f.Marker.Show)
	require.Equal(t, []string{"b0.0", "", "b1.0", "", "b2.0", "", "b3.0", "", ""}, f.Rows)
}

func TestPaneNewOutputWithFollowOnStaysAtTheBottom(t *testing.T) {
	r := newPaneRig(t, 40, 5, 3, 3, 3, 3)
	r.pane.Frame()
	r.add(2)
	f := r.pane.Frame()
	require.Equal(t, []string{"b3.2", "", "b4.0", "b4.1", ""}, f.Rows)
	require.False(t, f.Marker.Show)
}

func TestPaneScrollToBottomTurnsFollowOnAndClearsMarker(t *testing.T) {
	r := newPaneRig(t, 40, 5, 3, 3, 3, 3)
	r.pane.ScrollBy(-6)
	r.add(2)
	require.True(t, r.pane.Frame().Marker.New)

	r.pane.ScrollToBottom()
	f := r.pane.Frame()
	require.True(t, r.pane.Following())
	require.False(t, f.Marker.Show)
	require.Equal(t, "b4.1", f.Rows[3])

	// The marker form starts again from "End" after the next scroll up.
	r.pane.ScrollBy(-1)
	require.Equal(t, "▼ End", r.pane.Frame().Marker.Text)
}

func TestPaneMarkerIsShortUnderThirtyColumns(t *testing.T) {
	r := newPaneRig(t, 28, 5, 3, 3, 3, 3)
	r.pane.ScrollBy(-3)
	r.add(14)
	m := r.pane.Frame().Marker
	require.Equal(t, "▼ 19 · End", m.Text)
	require.Equal(t, (28-displayWidth(m.Text))/2, m.Pad)

	// The window at the first row shows 4 rows; 27 rows are below it.
	r.pane.ScrollBy(-1000)
	require.Equal(t, "▼ 27 · End", r.pane.Frame().Marker.Text)
}

func TestPaneRowsFitTheWidthWithWideCharacters(t *testing.T) {
	r := newPaneRig(t, 10, 6)
	// Five wide characters fill ten columns; six need a second row.
	require.NoError(t, r.tr.Append("wide", rowsBlock{rows: []string{strings.Repeat("字", 6)}}, true))
	f := r.pane.Frame()
	require.Equal(t, 6, len(f.Rows))
	require.Equal(t, strings.Repeat("字", 5), f.Rows[0])
	require.Equal(t, "字", f.Rows[1])
	for _, row := range f.Rows {
		require.LessOrEqual(t, displayWidth(row), 10)
	}
}

// --- resize, fold, reveal ---

func TestPaneResizeKeepsTheBlockAtTheTop(t *testing.T) {
	r := newPaneRig(t, 40, 5)
	for range 6 {
		id := r.add(1)
		r.grow(id, []string{id + " " + strings.Repeat("word ", 12)})
	}
	r.pane.ScrollBy(-9)
	before := r.pane.Frame()
	require.False(t, r.pane.Following())

	r.pane.SetSize(20, 5)
	after := r.pane.Frame()
	require.Equal(t, before.FirstBlock, after.FirstBlock)
	require.False(t, r.pane.Following())

	r.pane.SetSize(120, 5)
	again := r.pane.Frame()
	require.Equal(t, before.FirstBlock, again.FirstBlock)
}

func TestPaneResizeWithFollowStaysAtTheBottom(t *testing.T) {
	r := newPaneRig(t, 40, 3)
	for range 4 {
		id := r.add(1)
		r.grow(id, []string{id + " " + strings.Repeat("word ", 20)})
	}
	for _, w := range []int{20, 120, 40, 120} {
		r.pane.SetSize(w, 3)
		f := r.pane.Frame()
		require.True(t, r.pane.Following())
		require.NotEmpty(t, f.Rows[1])
		require.Empty(t, f.Rows[2], "the window ends with the gap row of the last block")
		if w == 120 {
			// One row for each block: the window ends with the last block.
			require.True(t, strings.HasPrefix(f.Rows[1], "b3 "), f.Rows[1])
			require.Empty(t, f.Rows[0], "the gap row of b2")
		}
	}
}

func TestPaneHeightChangeKeepsFollowAndTop(t *testing.T) {
	r := newPaneRig(t, 40, 5, 3, 3, 3, 3)
	r.pane.ScrollBy(-4)
	top := r.pane.Frame().Rows[0]
	r.pane.SetSize(40, 8)
	require.Equal(t, top, r.pane.Frame().Rows[0])
	require.False(t, r.pane.Following())
}

func TestPaneFoldChangeRecountsOnlyThatBlock(t *testing.T) {
	r := newPaneRig(t, 40, 5, 3, 3, 3, 3)
	r.pane.ScrollBy(-100)
	r.pane.Frame()
	r.takeDraws()

	require.NoError(t, r.pane.SetFold("b0", FoldTruncated))
	require.Equal(t, FoldTruncated, r.pane.Fold("b0"))
	r.pane.Frame()
	require.Equal(t, 1, r.takeDraws())
	require.ErrorIs(t, r.pane.SetFold("nope", FoldExpanded), ErrUnknownBlock)
}

func TestPaneFoldChangeAboveTheWindowKeepsTheTopRow(t *testing.T) {
	r := newPaneRig(t, 40, 5, 3, 3, 3, 3)
	r.pane.ScrollBy(-3)
	top := r.pane.Frame().Rows[0]

	// The block draws one more row at the next fold; the text at the top stays.
	r.grow("b0", labelRows("b0", 9))
	require.Equal(t, top, r.pane.Frame().Rows[0])
}

func TestPaneRevealMovesTheWindowByTheLeastRows(t *testing.T) {
	r := newPaneRig(t, 40, 7, 3, 3, 3, 3, 3, 3)
	r.pane.ScrollBy(-100)
	require.Equal(t, "b0.0", r.pane.Frame().Rows[0])

	// Block 3 is at rows 12-14, with the gap row of b2 above it and its own
	// gap row below it. The window moves so that its gap row is the last
	// content row. The last pane row is the marker.
	r.pane.Reveal(3)
	f := r.pane.Frame()
	require.False(t, r.pane.Following())
	require.Equal(t, []string{"b2.2", "", "b3.0", "b3.1", "b3.2", "", ""}, f.Rows)

	// A block in view does not move the window.
	r.pane.Reveal(3)
	require.Equal(t, f.Rows, r.pane.Frame().Rows)

	// A block above the window goes to the second row, below the row above it.
	r.pane.Reveal(1)
	require.Equal(t, []string{"", "b1.0"}, r.pane.Frame().Rows[:2])

	// The first block has no row above it.
	r.pane.Reveal(0)
	require.Equal(t, "b0.0", r.pane.Frame().Rows[0])

	// A block taller than the window shows the row above it and its first rows.
	r.add(20)
	r.pane.Reveal(6)
	require.Equal(t, []string{"", "b6.0"}, r.pane.Frame().Rows[:2])
}

func TestPaneRevealKeepsBothGapRowsOfABlockThatFits(t *testing.T) {
	r := newPaneRig(t, 40, 6, 2, 2, 2, 2, 2, 2)
	r.pane.ScrollBy(-100)
	// Block 2 is at rows 6-7, its gap row is row 8 and the gap row of b1 is
	// row 5. The window of 5 content rows moves down by the fewest rows.
	r.pane.Reveal(2)
	require.Equal(t, []string{"b1.1", "", "b2.0", "b2.1", "", ""}, r.pane.Frame().Rows,
		"the gap row above and the gap row below are in view")
	r.pane.Reveal(4)
	require.Equal(t, []string{"b3.1", "", "b4.0", "b4.1", "", ""}, r.pane.Frame().Rows)
}

// --- cache ---

func TestPaneScrollAndDeltaDrawOnlyWhatChanged(t *testing.T) {
	counts := make([]int, 300)
	for i := range counts {
		counts[i] = 2
	}
	r := newPaneRig(t, 40, 8, counts...)
	r.pane.Frame()
	r.takeDraws()

	// The first frame of a follow session draws only the tail.
	r.pane.ScrollBy(-3)
	r.pane.Frame()
	require.LessOrEqual(t, r.takeDraws(), 4)

	// A wheel step in the middle draws no block twice.
	r.pane.ScrollBy(-200)
	r.pane.Frame()
	r.takeDraws()
	r.pane.ScrollBy(3)
	r.pane.Frame()
	require.Zero(t, r.takeDraws(), "rows of a cached block are reused")

	r.grow("b299", labelRows("b299", 3))
	r.pane.Frame()
	require.Equal(t, 1, r.takeDraws())
}

func TestPaneResizeDrawsAgainOnlyTheBlocksInTheWindow(t *testing.T) {
	counts := make([]int, 500)
	for i := range counts {
		counts[i] = 2
	}
	r := newPaneRig(t, 40, 6, counts...)
	r.pane.Frame()
	r.takeDraws()

	r.pane.SetSize(60, 6)
	require.NotEmpty(t, r.pane.Frame().Rows)
	require.LessOrEqual(t, r.takeDraws(), 4)
}

func TestPaneSpinnerStateRedrawsABlockWithoutANewVersion(t *testing.T) {
	r := newPaneRig(t, 40, 4)
	view := NewTranscriptView()
	require.NoError(t, r.tr.Append("run",
		NewToolLineBlock("bash", "sleep 1", ToolRunning, "", "", view), false))
	first := strings.Join(r.pane.Frame().Rows, "\n")
	view.SetSpinnerFrame(3)
	second := strings.Join(r.pane.Frame().Rows, "\n")
	require.NotEqual(t, first, second)
}

// --- frame budget ---

// frameBudget is the time of one frame at 60 frames per second.
const frameBudget = 16 * time.Millisecond

// foldedBlock draws a real block in the three fold forms of the design: the
// first row, the first and last three rows around a gap row, or all rows.
type foldedBlock struct{ inner Block }

func (b foldedBlock) Lines(rc RenderContext) []string {
	rows := b.inner.Lines(rc)
	switch rc.Fold {
	case FoldCollapsed:
		return rows[:min(1, len(rows))]
	case FoldTruncated:
		if len(rows) > 7 {
			out := slices.Clone(rows[:3])
			out = append(out, "  ⋮ "+fmt.Sprint(len(rows)-6)+" more lines")
			return append(out, rows[len(rows)-3:]...)
		}
	}
	return rows
}

func (b foldedBlock) drawState() uint64 {
	if ds, ok := b.inner.(drawStater); ok {
		return ds.drawState()
	}
	return 0
}

// paneSession is a session of 2000 blocks of the real block types.
type paneSession struct {
	tr     *Transcript
	pane   *Pane
	stream *markdownStream
	last   string
	text   string
	ids    []string
}

const reply = "The parser now skips empty fields. The hook blocked the removal of the build directory.\n\n" +
	"The test still fails because it expects the old value. I read `parser.go` and found the count in `countFields`, which adds one for each separator, so an empty field at the end still counts.\n\n" +
	"- keep the count of separators\n- skip a field that holds only spaces\n- return the count as an int\n\n" +
	"Shall I change the test or the parser? The test name is TestCount and it lives in parser_test.go."

func newPaneSessionRig(tb testing.TB, blocks, width, height int) *paneSession {
	tb.Helper()
	view := NewTranscriptView()
	s := &paneSession{tr: NewTranscript(), stream: newMarkdownStream()}
	var long strings.Builder
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&long, "line %d of the output: ok --- PASS: TestCase%d (0.00s)\n", i, i)
	}
	for i := range blocks {
		id := fmt.Sprintf("blk%04d", i)
		var b Block
		switch i % 5 {
		case 0, 1, 2:
			name := []string{"read", "bash", "edit"}[i%5]
			b = NewToolLineBlock(name, fmt.Sprintf("file%d.go", i), ToolSucceeded, "0.1s", "", view)
		case 3:
			b = NewAssistantTextBlock(fmt.Sprintf("Step %d. ", i) + reply)
		default:
			b = newToolBlock("bash", fmt.Sprintf("go test ./pkg%d", i), ToolFailed, "1.2s", "exit 1",
				newToolOutput(long.String(), protocol.ToolResultDetails{}), view)
		}
		require.NoError(tb, s.tr.Append(id, foldedBlock{inner: b}, true))
		s.ids = append(s.ids, id)
	}
	// The last block is a reply that streams.
	s.last = "stream"
	s.text = "Streaming. " + reply
	require.NoError(tb, s.tr.Append(s.last, foldedBlock{inner: newAssistantTextBlock(s.text, s.stream, true)}, false))
	s.ids = append(s.ids, s.last)

	s.pane = NewPane(s.tr)
	s.pane.SetTheme(NewTheme(colorprofile.TrueColor))
	// Replies draw expanded; tool rows are one row. Every 25th long output is
	// open, so that a window can meet 200 rows of one block.
	for n, id := range s.ids {
		fold := FoldCollapsed
		switch {
		case id == s.last, n%5 == 3, n%5 == 4 && n%25 == 4:
			fold = FoldExpanded
		}
		require.NoError(tb, s.pane.SetFold(id, fold))
	}
	s.pane.SetSize(width, height)
	return s
}

// delta streams one word into the last block.
func (s *paneSession) delta(i int) {
	s.text += fmt.Sprintf(" word%d", i)
	_ = s.tr.Replace(s.last, foldedBlock{inner: newAssistantTextBlock(s.text, s.stream, true)})
}

// frame draws and joins the rows, as the screen does.
func (s *paneSession) frame() int {
	f := s.pane.Frame()
	return len(strings.Join(f.Rows, "\n"))
}

func raceEnabled() bool {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}
	for _, s := range info.Settings {
		if s.Key == "-race" && s.Value == "true" {
			return true
		}
	}
	return false
}

// frameStats are the median, the 99th percentile and the worst of the runs.
type frameStats struct{ med, p99, worst time.Duration }

// median runs op n times, after warm runs, and returns the durations.
func median(n int, op func(i int)) frameStats {
	for i := range 5 {
		op(i)
	}
	ds := make([]time.Duration, n)
	for i := range ds {
		start := time.Now()
		op(i)
		ds[i] = time.Since(start)
	}
	sort.Slice(ds, func(a, b int) bool { return ds[a] < ds[b] })
	return frameStats{med: ds[n/2], p99: ds[n*99/100], worst: ds[n-1]}
}

func checkBudget(t *testing.T, name string, d frameStats) {
	t.Helper()
	t.Logf("%-46s median %v  p99 %v  worst %v", name, d.med, d.p99, d.worst)
	if raceEnabled() {
		return
	}
	require.Less(t, d.med, frameBudget, "%s over the frame budget", name)
}

func TestPaneMarkerCountConvergesAfterResize(t *testing.T) {
	s := newPaneSessionRig(t, 600, 80, 15)
	s.frame()
	s.pane.ScrollBy(-2000)
	s.delta(0)
	require.True(t, s.pane.Frame().Marker.New)

	s.pane.SetSize(120, 15)
	var m PaneMarker
	frames := 0
	for ; frames < 200; frames++ {
		m = s.pane.Frame().Marker
		if !m.Approx {
			break
		}
	}
	require.False(t, m.Approx, "the count settles")
	require.Greater(t, frames, 0, "the first frame after a resize uses estimates")

	// The settled count equals the rows below the window, counted by hand.
	f := s.pane.Frame()
	want := 0
	shown := len(f.Rows) - 1
	for i := range s.pane.items {
		s.pane.measure(i)
		want += s.pane.items[i].h
	}
	ai, ar := s.pane.ai, s.pane.ar
	above := ar
	for i := range ai {
		above += s.pane.items[i].h
	}
	require.Equal(t, want-above-shown, f.Marker.N)
}

func TestPaneFrameBudget2000Blocks(t *testing.T) {
	const runs = 101

	t.Run("cold first frame", func(t *testing.T) {
		start := time.Now()
		s := newPaneSessionRig(t, 2000, 80, 15)
		build := time.Since(start)
		start = time.Now()
		s.frame()
		first := time.Since(start)
		t.Logf("build %v, first frame with follow on %v", build, first)
		rows := 0
		for i := range s.pane.items {
			s.pane.measure(i)
			rows += s.pane.items[i].h
		}
		t.Logf("session: %d blocks, %d rows at width 80", len(s.pane.items), rows)
		s.pane.ScrollBy(-1)
		require.False(t, s.pane.Following())
		start = time.Now()
		s.pane.ScrollBy(-30)
		s.frame()
		t.Logf("first scroll up from the bottom %v", time.Since(start))
	})

	t.Run("wheel scroll of 3 rows in the middle", func(t *testing.T) {
		s := newPaneSessionRig(t, 2000, 80, 15)
		s.frame()
		s.pane.ScrollBy(-6000)
		s.frame()
		d := median(runs, func(i int) {
			if i%2 == 0 {
				s.pane.ScrollBy(3)
			} else {
				s.pane.ScrollBy(-3)
			}
			s.frame()
		})
		require.False(t, s.pane.Following())
		checkBudget(t, "wheel 3 rows, middle", d)
	})

	t.Run("wheel scroll upward through the whole session", func(t *testing.T) {
		s := newPaneSessionRig(t, 2000, 80, 15)
		s.frame()
		// Cold rows: every block that enters the window is drawn once.
		ds := make([]time.Duration, 0, 1500)
		for range 1500 {
			start := time.Now()
			s.pane.ScrollBy(-3)
			s.frame()
			ds = append(ds, time.Since(start))
		}
		sort.Slice(ds, func(a, b int) bool { return ds[a] < ds[b] })
		t.Logf("scroll up 4500 rows over cold blocks: median %v, p99 %v, worst %v",
			ds[len(ds)/2], ds[len(ds)*99/100], ds[len(ds)-1])
		if !raceEnabled() {
			require.Less(t, ds[len(ds)*99/100], frameBudget)
		}
	})

	t.Run("streaming delta in the last block, follow on", func(t *testing.T) {
		s := newPaneSessionRig(t, 2000, 80, 15)
		s.frame()
		d := median(runs, func(i int) {
			s.delta(i)
			s.frame()
		})
		require.True(t, s.pane.Following())
		checkBudget(t, "delta in last block, follow on", d)
	})

	t.Run("streaming delta, follow off in the middle", func(t *testing.T) {
		s := newPaneSessionRig(t, 2000, 80, 15)
		s.frame()
		s.pane.ScrollBy(-6000)
		s.frame()
		d := median(runs, func(i int) {
			s.delta(i)
			s.frame()
		})
		require.False(t, s.pane.Following())
		checkBudget(t, "delta in last block, follow off", d)
	})

	t.Run("fold change of one block in the window", func(t *testing.T) {
		s := newPaneSessionRig(t, 2000, 80, 15)
		s.frame()
		s.pane.ScrollBy(-6000)
		f := s.pane.Frame()
		id := s.ids[f.FirstBlock+1]
		d := median(runs, func(i int) {
			mode := []FoldMode{FoldExpanded, FoldTruncated, FoldCollapsed}[i%3]
			require.NoError(t, s.pane.SetFold(id, mode))
			s.frame()
		})
		checkBudget(t, "fold change, one block", d)
	})

	t.Run("resize 80 to 120, then the first frame, follow on", func(t *testing.T) {
		s := newPaneSessionRig(t, 2000, 80, 15)
		s.frame()
		d := median(runs, func(i int) {
			s.pane.SetSize([]int{120, 80}[i%2], 15)
			s.frame()
		})
		checkBudget(t, "resize + first frame, follow on", d)
	})

	t.Run("resize 80 to 120, follow off, new output pending", func(t *testing.T) {
		s := newPaneSessionRig(t, 2000, 80, 15)
		s.frame()
		s.pane.ScrollBy(-6000)
		s.frame()
		s.delta(0)
		require.True(t, s.pane.Frame().Marker.New)
		d := median(runs, func(i int) {
			s.pane.SetSize([]int{120, 80}[i%2], 15)
			s.frame()
		})
		checkBudget(t, "resize + frame, follow off, marker N", d)
	})

	t.Run("resize 80 to 120, then the first frame, follow off", func(t *testing.T) {
		s := newPaneSessionRig(t, 2000, 80, 15)
		s.frame()
		s.pane.ScrollBy(-6000)
		s.frame()
		d := median(runs, func(i int) {
			s.pane.SetSize([]int{120, 80}[i%2], 15)
			s.frame()
		})
		checkBudget(t, "resize + first frame, follow off", d)
	})
}

func BenchmarkPaneWheelScroll(b *testing.B) {
	s := newPaneSessionRig(b, 2000, 80, 15)
	s.frame()
	s.pane.ScrollBy(-6000)
	s.frame()
	b.ResetTimer()
	for i := range b.N {
		if i%2 == 0 {
			s.pane.ScrollBy(3)
		} else {
			s.pane.ScrollBy(-3)
		}
		s.frame()
	}
}

func BenchmarkPaneStreamingDelta(b *testing.B) {
	s := newPaneSessionRig(b, 2000, 80, 15)
	s.frame()
	b.ResetTimer()
	for i := range b.N {
		s.delta(i)
		s.frame()
	}
}

func BenchmarkPaneFoldChange(b *testing.B) {
	s := newPaneSessionRig(b, 2000, 80, 15)
	s.frame()
	s.pane.ScrollBy(-6000)
	id := s.ids[s.pane.Frame().FirstBlock+1]
	b.ResetTimer()
	for i := range b.N {
		_ = s.pane.SetFold(id, []FoldMode{FoldExpanded, FoldTruncated, FoldCollapsed}[i%3])
		s.frame()
	}
}

func BenchmarkPaneResize(b *testing.B) {
	s := newPaneSessionRig(b, 2000, 80, 15)
	s.frame()
	b.ResetTimer()
	for i := range b.N {
		s.pane.SetSize([]int{120, 80}[i%2], 15)
		s.frame()
	}
}

// --- hit testing ---

func TestPaneBlockAtMapsRowsToBlocksWithFollowOn(t *testing.T) {
	r := newPaneRig(t, 40, 5, 3, 3, 3, 3)
	r.pane.Frame()
	// Follow on: the window is the gap row of b2, b3.0 .. b3.2, and the gap
	// row of b3. A gap row belongs to the block above it.
	for row, want := range []struct{ block, offset int }{{2, 3}, {3, 0}, {3, 1}, {3, 2}, {3, 3}} {
		b, off, ok := r.pane.BlockAt(row)
		require.True(t, ok, "row %d", row)
		require.Equal(t, want.block, b, "row %d", row)
		require.Equal(t, want.offset, off, "row %d", row)
	}
	_, _, ok := r.pane.BlockAt(5)
	require.False(t, ok, "a row outside the pane has no block")
	_, _, ok = r.pane.BlockAt(-1)
	require.False(t, ok)
}

func TestPaneBlockAtAfterScrollWithFollowOffLeavesTheMarkerRowEmpty(t *testing.T) {
	r := newPaneRig(t, 40, 5, 3, 3, 3, 3)
	r.pane.ScrollBy(-3)
	// Window: b2.0 b2.1 b2.2 and the gap row of b2, then the marker row.
	b, off, ok := r.pane.BlockAt(0)
	require.True(t, ok)
	require.Equal(t, [2]int{2, 0}, [2]int{b, off})
	b, off, ok = r.pane.BlockAt(2)
	require.True(t, ok)
	require.Equal(t, [2]int{2, 2}, [2]int{b, off})
	b, off, ok = r.pane.BlockAt(3)
	require.True(t, ok)
	require.Equal(t, [2]int{2, 3}, [2]int{b, off}, "the gap row belongs to the block above it")
	_, _, ok = r.pane.BlockAt(4)
	require.False(t, ok, "the marker row holds no block")
	row, shown := r.pane.MarkerRow()
	require.True(t, shown)
	require.Equal(t, 4, row)
}

func TestPaneBlockAtBelowShortContentIsEmpty(t *testing.T) {
	r := newPaneRig(t, 40, 6, 2, 1)
	b, off, ok := r.pane.BlockAt(3)
	require.True(t, ok)
	require.Equal(t, [2]int{1, 0}, [2]int{b, off})
	b, off, ok = r.pane.BlockAt(4)
	require.True(t, ok)
	require.Equal(t, [2]int{1, 1}, [2]int{b, off}, "the gap row after the last block")
	_, _, ok = r.pane.BlockAt(5)
	require.False(t, ok)
	_, shown := r.pane.MarkerRow()
	require.False(t, shown, "follow is on")
}

func TestPaneBlockAtSkipsBlocksWithoutRows(t *testing.T) {
	r := newPaneRig(t, 40, 5, 2, 0, 2)
	r.pane.ScrollBy(-100)
	b, _, ok := r.pane.BlockAt(3)
	require.True(t, ok)
	require.Equal(t, 2, b, "a block without rows has no gap row")
}

func TestPaneBlockAtFollowsABlockThatGrewAboveTheWindow(t *testing.T) {
	r := newPaneRig(t, 40, 5, 3, 3, 3, 3)
	r.pane.ScrollBy(-3)
	r.pane.Frame()
	r.grow("b0", labelRows("b0", 9))
	b, off, ok := r.pane.BlockAt(0)
	require.True(t, ok)
	require.Equal(t, [2]int{2, 0}, [2]int{b, off}, "the top row does not move")
}

func TestPaneNewOutputIsSetOnlyWhileFollowIsOff(t *testing.T) {
	r := newPaneRig(t, 40, 5, 3, 3, 3, 3)
	r.add(2)
	require.False(t, r.pane.NewOutput())
	r.pane.ScrollBy(-3)
	require.False(t, r.pane.NewOutput())
	r.add(2)
	require.True(t, r.pane.NewOutput())
	r.pane.ScrollToBottom()
	require.False(t, r.pane.NewOutput())
}

func TestPaneHoverMoveDrawsAtMostTwoBlocksAndStaysUnderTheFrameBudget(t *testing.T) {
	s := newPaneSessionRig(t, 2000, 80, 15)
	s.frame()
	s.pane.ScrollToTop()
	f := s.pane.Frame()
	var ids []string
	for i := f.FirstBlock; i <= f.LastBlock; i++ {
		ids = append(ids, s.pane.items[i].id)
	}
	require.GreaterOrEqual(t, len(ids), 3)
	before := s.pane.Draws()
	s.pane.SetHover(ids[0])
	s.frame()
	s.pane.SetHover(ids[1])
	s.frame()
	require.LessOrEqual(t, s.pane.Draws()-before, 3, "a hover move draws the block it leaves and the block it enters")

	d := median(101, func(i int) {
		s.pane.SetHover(ids[i%len(ids)])
		s.frame()
	})
	checkBudget(t, "hover over many rows", d)
}

// --- gap rows ---

// blockPane returns a pane at a size over the blocks, with ids "k0", "k1", ...
func blockPane(t *testing.T, width, height int, blocks ...Block) *Pane {
	t.Helper()
	tr := NewTranscript()
	for i, b := range blocks {
		require.NoError(t, tr.Append(fmt.Sprintf("k%d", i), b, true))
	}
	p := NewPane(tr)
	p.SetTheme(NewTheme(colorprofile.NoTTY))
	p.SetSize(width, height)
	return p
}

func plainFrame(p *Pane) []string { return plain(p.Frame().Rows) }

func TestPaneGapRowsSeparateUnitsAndFollowTheLast(t *testing.T) {
	p := blockPane(t, 40, 8,
		NewUserPromptBlock("go", ""),
		NewToolLineBlock("bash", "ls", ToolSucceeded, "", "", NewTranscriptView()),
		NewAssistantTextBlock("done"),
	)
	require.Equal(t, []string{"  ❯ go", "", "  ◆ Ran ls", "", "  done", "", "", ""}, plainFrame(p),
		"one empty row between any two blocks and one after the last, none above the first")
}

func TestPaneReplyParagraphsAndUsageLineAreOneUnit(t *testing.T) {
	stream := newMarkdownStream()
	p := blockPane(t, 40, 7,
		newAssistantTextBlock("one", stream, true),
		newAssistantTextBlock("two", newMarkdownStream(), false),
		NewMetaLineBlock("m · 1.0s"),
	)
	require.Equal(t, []string{"  one", "", "  two", "", "  m · 1.0s", "", ""}, plainFrame(p),
		"the break between paragraphs is part of the reply; one blank row of the reply comes before the usage row")
}

func TestPaneGapRowFollowsAReplyThatEndsWithAnEmptyParagraph(t *testing.T) {
	// A streaming reply opens its next paragraph before it has text. The gap
	// row stays after the last row that the reply drew.
	p := blockPane(t, 40, 5,
		newAssistantTextBlock("partial", newMarkdownStream(), true),
		newAssistantTextBlock("", newMarkdownStream(), false),
		NewCancelMarker("Cancelled by you."),
	)
	require.Equal(t, []string{"  partial", "", "  ⊘ Cancelled by you.", "", ""}, plainFrame(p))
}

func TestPaneBlockWithoutRowsHasNoGapRow(t *testing.T) {
	p := blockPane(t, 40, 5, noRows{}, NewRetryNote("again"), noRows{}, NewRetryNote("more"))
	require.Equal(t, []string{"  ↻ again", "", "  ↻ more", "", ""}, plainFrame(p),
		"no empty row above the first block, one between the two notes")
}

// --- selection box ---

// boxRig is a pane over five blocks of two rows each, with the text at the
// columns that blocks use: column 0 and the last column are blank.
func boxRig(t *testing.T, height int) *paneRig {
	t.Helper()
	r := newPaneRig(t, 20, height)
	for range 5 {
		id := r.add(0)
		r.grow(id, []string{"  " + id + " one", "  " + id + " two"})
	}
	return r
}

func TestPaneSelectionBoxUsesTheGapRowsAndKeepsTheRowCount(t *testing.T) {
	r := boxRig(t, 15)
	r.pane.ScrollToTop()
	before := plainFrame(r.pane)
	r.pane.SetSelected("b2")
	f := r.pane.Frame()
	got := plain(f.Rows)
	require.Len(t, got, len(before))
	require.Equal(t, []string{
		"┌                  ┐",
		"│ b2 one           │",
		"│ b2 two           │",
		"└                  ┘",
	}, got[5:9])
	for i, row := range got {
		if i < 5 || i > 8 {
			require.Equal(t, before[i], row, "row %d outside the box does not change", i)
		}
	}
	for _, row := range f.Rows[5:9] {
		require.Equal(t, 20, displayWidth(row))
	}
}

func TestPaneSelectionBoxShowsTheClipMarkAtTheWindowEdges(t *testing.T) {
	r := boxRig(t, 6)
	r.pane.ScrollToTop()
	r.pane.ScrollBy(7) // The window starts at the second row of b2.
	r.pane.SetSelected("b2", "b3")
	got := plainFrame(r.pane)
	require.Equal(t, []string{
		"┆ b2 two           ┆",
		"│                  │",
		"│ b3 one           │",
		"│ b3 two           │",
		"└                  ┘",
		"",
	}, got, "the top edge is above the window; the marker row stays empty")

	r.pane.ScrollToTop()
	r.pane.SetSelected("b1")
	got = plainFrame(r.pane)
	require.Equal(t, "┌                  ┐", got[2])
	require.Equal(t, "┆ b1 two           ┆", got[4], "the bottom edge is below the window")

	r.pane.SetSelected("b0")
	got = plainFrame(r.pane)
	require.Equal(t, "┆ b0 one           ┆", got[0], "the first block has no row above it")
	require.Equal(t, "└                  ┘", got[2])
}

func TestPaneSelectionBelowTheWindowDrawsNoBox(t *testing.T) {
	r := boxRig(t, 7)
	r.pane.ScrollToTop()
	// The window shows b0 and b1 with their gap rows. b2 is between them and
	// the selection, so no edge of the box is in the window.
	r.pane.SetSelected("b3")
	for _, row := range plainFrame(r.pane) {
		require.NotContains(t, row, "┆")
		require.NotContains(t, row, "┌")
	}
}
