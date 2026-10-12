package tui

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"
)

// wrapBlock is a block of one text that wraps at the pane width into rows of
// at most that many bytes, so that its row count depends on the width.
type wrapBlock struct{ n int }

func (b wrapBlock) Lines(rc RenderContext) []string {
	w := max(1, rc.Width)
	rows := make([]string, 0, b.n/w+1)
	for left := b.n; left > 0; left -= w {
		rows = append(rows, fmt.Sprintf("%0*d", min(left, w), 0))
	}
	return rows
}

// settledMarker draws frames until the count is exact and returns the marker.
func settledMarker(t *testing.T, p *Pane) PaneMarker {
	t.Helper()
	for range 200 {
		if m := p.Frame().Marker; !m.Approx {
			return m
		}
	}
	require.FailNow(t, "the marker count does not settle")
	return PaneMarker{}
}

func TestPaneMarkerCountMeasuresTheBlockBelowAFullWindow(t *testing.T) {
	r := newPaneRig(t, 20, 5, 2, 2, 2, 2, 2)
	r.pane.ScrollBy(-1000)
	require.Equal(t, []string{"b0.0", "b0.1", "b1.0", "b1.1", ""}, r.pane.Frame().Rows)

	// The window is full at the end of block b1: block b2 sits below it and
	// has not drawn at its new size.
	r.grow("b2", labelRows("b2", 10))
	m := settledMarker(t, r.pane)
	require.True(t, m.New)
	require.Equal(t, 10+2+2, m.N)
	require.Equal(t, "▼ 14 · End", m.Text)
}

func TestPaneMarkerCountMeasuresTheBlockBelowAFullWindowAfterAResize(t *testing.T) {
	tr := NewTranscript()
	for i := range 6 {
		require.NoError(t, tr.Append(fmt.Sprintf("w%d", i), wrapBlock{n: 31}, true))
	}
	p := NewPane(tr)
	p.SetTheme(NewTheme(colorprofile.TrueColor))
	p.SetSize(40, 5)
	p.Frame()
	p.ScrollBy(-1000)
	p.Frame()
	require.NoError(t, tr.Append("w6", wrapBlock{n: 31}, true))
	p.Frame()

	p.SetSize(10, 5)
	m := settledMarker(t, p)
	require.True(t, m.New)
	// Seven blocks of 31 bytes make four rows each at width 10; the window
	// shows the first four rows, which is the whole first block.
	require.Equal(t, 7*4-4, m.N)
}
