package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"
)

func TestMuteListNumbers(t *testing.T) {
	th := NewTheme(colorprofile.TrueColor)
	fg := func(s string) string { return th.Render(RoleFg, s) }
	muted := th.Render(RoleMuted, "12"+markNumberEnd)
	cases := []struct {
		name string
		row  string
		want string
	}{
		{"an item number", fg("12") + fg(". ") + fg("text"), muted + fg("text")},
		{"a nested item keeps its indent", fg(" ") + fg(" ") + fg("12") + fg(". ") + fg("x"), fg(" ") + fg(" ") + muted + fg("x")},
		{"text that only looks like a number", fg("12. text"), fg("12. text")},
		{"a number later in the row", fg("see ") + fg("12") + fg(". "), fg("see ") + fg("12") + fg(". ")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := muteListNumbers([]mdRow{{text: c.row}}, th.mode)
			require.Equal(t, c.want, got[0].text)
		})
	}
	t.Run("without color the rows do not change", func(t *testing.T) {
		row := "12. text"
		require.Equal(t, row, muteListNumbers([]mdRow{{text: row}}, modeNone)[0].text)
	})
}

func TestMarkdownListMarksAreMuted(t *testing.T) {
	th := NewTheme(colorprofile.TrueColor)
	sample := th.Render(RoleMuted, "x")
	muted := sample[:strings.Index(sample, "x")]
	rows, _ := newMarkdownStream().render("1. first\n2. second\n\n- item\n  - nested", 60, th)
	require.Equal(t, []string{"1. first", "2. second", "", "• item", "  • nested"}, trimRight(mdPlain(rows)))
	for _, i := range []int{0, 1, 3, 4} {
		require.Contains(t, rows[i].text, muted, "row %d has a muted mark: %q", i, rows[i].text)
	}
	require.Contains(t, rows[0].text, th.Render(RoleMuted, "1"+markNumberEnd))
	require.Contains(t, rows[3].text, th.Render(RoleMuted, markBullet))
}
