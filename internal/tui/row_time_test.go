package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/require"
)

func TestRowTimeRoom(t *testing.T) {
	cases := []struct {
		width int
		at    string
		want  int
	}{
		{76, "17:30", 76 - 5 - rowTimeGap},
		{76, "", 76},
		{14, "17:30", 7},
		{13, "17:30", 13},
		{3, "17:30", 3},
	}
	for _, c := range cases {
		require.Equal(t, c.want, rowTimeRoom(c.width, c.at), "%d %q", c.width, c.at)
	}
}

func TestWithRowTime(t *testing.T) {
	plain := lipgloss.NewStyle()
	cases := []struct {
		name  string
		row   string
		width int
		at    string
		want  string
		shown bool
	}{
		{"the time ends at the width", "  text", 20, "17:30", "  text" + strings.Repeat(" ", 9) + "17:30", true},
		{"a row that keeps exactly the gap", "  1234567", 16, "17:30", "  1234567  17:30", true},
		{"a row one column too wide", "  12345678", 16, "17:30", "  12345678", false},
		{"no time", "  text", 20, "", "  text", false},
		{"CJK counts two columns", "  日本語", 15, "17:30", "  日本語  17:30", true},
		{"CJK that leaves one column too few", "  日本語", 14, "17:30", "  日本語", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, shown := withRowTime(c.row, c.width, c.at, plain, spaces)
			require.Equal(t, c.shown, shown)
			require.Equal(t, c.want, got)
			if shown {
				require.Equal(t, c.width, displayWidth(got))
			}
		})
	}
}
