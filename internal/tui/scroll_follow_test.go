package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"
)

// paneScreen returns the plain rows of the block pane.
func paneScreen(m *Model) []string {
	rows := screenOf(m)
	return rows[m.geo.PaneTop : m.geo.PaneTop+m.geo.PaneRows]
}

// addNotes appends n two-row notes, as new output.
func addNotes(t *testing.T, m *Model, n int) {
	t.Helper()
	base := m.transcript.Len()
	for i := 0; i < n; i++ {
		m.Update(AppendBlockMsg{ID: fmt.Sprintf("note%d", base+i), Block: NewReconnectNote(fmt.Sprintf("new note %d", base+i)), Stable: true})
	}
	m.View()
}

// centered reports the left pad of a marker row and its trimmed text.
func centered(row string) (pad int, text string) {
	text = strings.TrimLeft(row, " ")
	return len(row) - len(text), strings.TrimRight(text, " ")
}

// rowsBelowWindow counts the pane rows after the last content row of the
// window, from the blocks of the transcript.
func rowsBelowWindow(t *testing.T, m *Model) int {
	t.Helper()
	total := 0
	for i := range m.transcript.entries {
		total += m.pane.RowsOf(i)
	}
	block, off, ok := m.pane.BlockAt(0)
	require.True(t, ok)
	above := off
	for i := 0; i < block; i++ {
		above += m.pane.RowsOf(i)
	}
	return total - above - (m.geo.PaneRows - 1)
}

func TestWheelFreeScrollUpShowsTheEndMarkerCentered(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	require.True(t, m.pane.Following())
	m.pane.ScrollBy(-1)
	m.View()
	pane := paneScreen(m)
	pad, text := centered(pane[len(pane)-1])
	require.Equal(t, "▼ End", text)
	require.Equal(t, (80-displayWidth("▼ End"))/2, pad)
}

func TestMarkerShowsTheNewLineCountAndTheTopRowDoesNotMove(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	m.pane.ScrollBy(-5)
	top := paneScreen(m)[0]
	require.NotEmpty(t, top)

	addNotes(t, m, 3)
	pane := paneScreen(m)
	require.Equal(t, top, pane[0], "new output does not move the window")
	want := rowsBelowWindow(t, m)
	pad, text := centered(pane[len(pane)-1])
	require.Equal(t, fmt.Sprintf("▼ %d new lines · End", want), text)
	require.Equal(t, (80-displayWidth(text))/2, pad)

	before := want
	addNotes(t, m, 2)
	require.Equal(t, top, paneScreen(m)[0])
	require.Greater(t, rowsBelowWindow(t, m), before, "N grows with new output")
	_, text = centered(paneScreen(m)[m.geo.PaneRows-1])
	require.Equal(t, fmt.Sprintf("▼ %d new lines · End", rowsBelowWindow(t, m)), text)
}

func TestMarkerUsesTheShortFormUnder30Columns(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 28, 14, longSession(40)...)
	m.pane.ScrollBy(-5)
	m.View()
	pad, text := centered(paneScreen(m)[m.geo.PaneRows-1])
	require.Equal(t, "▼ End", text, "nothing new: the form does not change")
	require.Equal(t, (28-displayWidth(text))/2, pad)

	addNotes(t, m, 3)
	want := rowsBelowWindow(t, m)
	pad, text = centered(paneScreen(m)[m.geo.PaneRows-1])
	require.Equal(t, fmt.Sprintf("▼ %d · End", want), text)
	require.Equal(t, (28-displayWidth(text))/2, pad)
	for _, r := range screenOf(m) {
		require.LessOrEqual(t, displayWidth(r), 28)
	}
}

func TestMarkerStyle(t *testing.T) {
	m := listModelAt(t, colorprofile.TrueColor, 80, 24, longSession(40)...)
	m.pane.ScrollBy(-5)
	rows := strings.Split(m.View().Content, "\n")
	end := rows[m.geo.PaneTop+m.geo.PaneRows-1]
	require.Contains(t, end, "\x1b[1;38;2;45;212;191m▼ End", "tool role, bold")
	require.NotContains(t, end, "48;2;", "no background")

	addNotes(t, m, 2)
	rows = strings.Split(m.View().Content, "\n")
	fresh := rows[m.geo.PaneTop+m.geo.PaneRows-1]
	require.Contains(t, fresh, "\x1b[1;38;2;244;114;182m▼ ", "accent role, bold")
	require.NotContains(t, fresh, "48;2;")
}

func TestFollowOnKeepsTheBottomForNewOutputAndResize(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	require.True(t, m.pane.Following())
	addNotes(t, m, 2)
	require.True(t, m.pane.Following())
	pane := paneScreen(m)
	require.Contains(t, pane[len(pane)-2], "new note", "the last note is at the bottom")
	for _, r := range pane {
		require.NotContains(t, r, "▼")
	}

	m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	m.View()
	pane = paneScreen(m)
	require.True(t, m.pane.Following())
	require.Contains(t, pane[len(pane)-2], "new note")
}

func TestScrollAboveTheWindowTurnsFollowOffAndKeepsTheSelectionInView(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	press(t, m, "tab")
	require.True(t, m.pane.Following())
	for range 30 {
		press(t, m, "k")
	}
	require.False(t, m.pane.Following())
	f := m.pane.Frame()
	idx := m.transcript.index[m.selectedBlock()]
	require.GreaterOrEqual(t, idx, f.FirstBlock)
	require.LessOrEqual(t, idx, f.LastBlock)
	pane := paneScreen(m)
	require.Contains(t, pane[len(pane)-1], "▼ End")

	press(t, m, "end")
	require.True(t, m.pane.Following())
	for _, r := range paneScreen(m) {
		require.NotContains(t, r, "▼")
	}
}

func TestViewerDrawsNoMarker(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	m.pane.ScrollBy(-3)
	m.View()
	require.False(t, m.pane.Following())
	require.Contains(t, strings.Join(screenOf(m), "\n"), "▼ End")
	id := toolID(t, m, "bash", 0)
	m.openViewer(id)
	require.True(t, m.viewerOpen())
	require.NotContains(t, strings.Join(screenOf(m), "\n"), "▼")
}

func TestMarkerCountSettlesAfterAResizeWithoutBlockingView(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(300)...)
	m.pane.ScrollBy(-5)
	addNotes(t, m, 1)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	for range 400 {
		m.View()
		if !m.markerApprox {
			break
		}
	}
	require.False(t, m.markerApprox, "frames settle the count")
	_, text := centered(paneScreen(m)[m.geo.PaneRows-1])
	require.Equal(t, fmt.Sprintf("▼ %d new lines · End", rowsBelowWindow(t, m)), text)
}

func TestSettleTickIsRequestedForNewOutputAndStopsWhenSettled(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(40)...)
	m.pane.ScrollBy(-5)
	_, cmd := m.Update(AppendBlockMsg{ID: "extra", Block: NewReconnectNote("extra"), Stable: true})
	require.NotNil(t, cmd, "new output below the window asks for a settle tick")
	require.True(t, m.settlePending)
	m.View()
	require.False(t, m.markerApprox)
	_, cmd = m.Update(markerSettleMsg{})
	require.Nil(t, cmd, "the count is exact: no more ticks")
	require.False(t, m.settlePending)
}

// --- screens ---

func TestScrollScreens(t *testing.T) {
	t.Run("scrolled_up_no_new_output_80x14", func(t *testing.T) {
		m := listModel(t, 80, 14)
		m.pane.ScrollBy(-2)
		m.View()
		requireScreenGolden(t, m)
	})
	t.Run("scrolled_up_new_output_80x14", func(t *testing.T) {
		m := listModel(t, 80, 14)
		m.pane.ScrollBy(-2)
		m.View()
		addNotes(t, m, 2)
		requireScreenGolden(t, m)
	})
	t.Run("scrolled_up_new_output_28x14", func(t *testing.T) {
		m := listModel(t, 28, 14)
		m.pane.ScrollBy(-2)
		m.View()
		addNotes(t, m, 2)
		requireScreenGolden(t, m)
	})
	t.Run("drag_copied_80x24", func(t *testing.T) {
		m, _ := listModelWithClipboard(t, 80, 24)
		y := rowIndex(t, m, "Count now skips empty fields.")
		x0 := colOf(t, screenOf(m)[y], "TestCount still fails")
		mouse(m, tea.MouseClickMsg(leftAt(x0, y)))
		mouse(m, tea.MouseMotionMsg(leftAt(x0+20, y)))
		cmd := m.sv.onMouse(tea.MouseReleaseMsg(leftAt(x0+20, y)))
		require.NotNil(t, cmd)
		mouse(m, cmd())
		requireScreenGolden(t, m)
	})
}

func TestMarkerCountSettlesAfterFoldAllWithoutAnEndlessTickChain(t *testing.T) {
	m := listModelAt(t, colorprofile.NoTTY, 80, 24, longSession(2000)...)
	press(t, m, "tab")
	m.pane.ScrollBy(-60)
	addNotes(t, m, 1)
	for range 2000 {
		if !m.markerApprox {
			break
		}
		m.View()
	}
	require.False(t, m.markerApprox)
	m.Update(markerSettleMsg{})
	require.False(t, m.settlePending)
	rows := rowsBelowWindow(t, m)

	_, cmd := m.Update(keyNamed(t, "E"))
	require.NotNil(t, cmd, "a fold command changes row counts below the window")
	require.True(t, m.settlePending)
	m.View()
	require.NotEqual(t, rows, rowsBelowWindow(t, m), "the fold changed the rows below the window")

	ticks := 0
	for ; ticks < 2000 && m.settlePending; ticks++ {
		m.View()
		_, _ = m.Update(markerSettleMsg{})
	}
	require.False(t, m.settlePending, "the tick chain ends")
	require.Positive(t, ticks)
	m.View()
	require.False(t, m.markerApprox)
	_, text := centered(paneScreen(m)[m.geo.PaneRows-1])
	require.Equal(t, fmt.Sprintf("▼ %d new lines · End", rowsBelowWindow(t, m)), text)
}
