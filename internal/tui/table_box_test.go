package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"
)

var plainTableStyle = tableStyle{border: RoleDim, header: RoleFg, body: RoleFg}

func TestTextTableDraw(t *testing.T) {
	cases := []struct {
		name  string
		table textTable
		width int
		want  []string
	}{
		{
			name: "columns are as wide as their widest cell",
			table: textTable{
				header: []string{"Type", "Job"},
				rows:   [][]string{{"toolSlot", "State of one call"}, {"toolDone", "A worker is done"}, {"updater", "Progress that never blocks"}},
			},
			width: 76,
			want: []string{
				"┌──────────┬────────────────────────────┐",
				"│ Type     │ Job                        │",
				"├──────────┼────────────────────────────┤",
				"│ toolSlot │ State of one call          │",
				"│ toolDone │ A worker is done           │",
				"│ updater  │ Progress that never blocks │",
				"└──────────┴────────────────────────────┘",
			},
		},
		{
			name: "a table wider than the width wraps the cells of the widest column",
			table: textTable{
				header: []string{"Type", "Job"},
				rows:   [][]string{{"toolSlot", "State of one call"}},
			},
			width: 24,
			want: []string{
				"┌──────────┬───────────┐",
				"│ Type     │ Job       │",
				"├──────────┼───────────┤",
				"│ toolSlot │ State of  │",
				"│          │ one call  │",
				"└──────────┴───────────┘",
			},
		},
		{
			name: "a word longer than its column breaks and is not cut",
			table: textTable{
				header: []string{"k", "v"},
				rows:   [][]string{{"a", "abcdefghij"}},
			},
			width: 12,
			want: []string{
				"┌───┬──────┐",
				"│ k │ v    │",
				"├───┼──────┤",
				"│ a │ abcd │",
				"│   │ efgh │",
				"│   │ ij   │",
				"└───┴──────┘",
			},
		},
		{
			name: "CJK cells count two columns",
			table: textTable{
				header: []string{"名前", "x"},
				rows:   [][]string{{"日本語", "y"}},
			},
			width: 40,
			want: []string{
				"┌────────┬───┐",
				"│ 名前   │ x │",
				"├────────┼───┤",
				"│ 日本語 │ y │",
				"└────────┴───┘",
			},
		},
		{
			name: "a wrapped CJK cell keeps whole graphemes",
			table: textTable{
				header: []string{"a", "b"},
				rows:   [][]string{{"日本語のテスト", "x"}},
			},
			width: 13,
			want: []string{
				"┌───────┬───┐",
				"│ a     │ b │",
				"├───────┼───┤",
				"│ 日本  │ x │",
				"│ 語の  │   │",
				"│ テス  │   │",
				"│ ト    │   │",
				"└───────┴───┘",
			},
		},
		{
			name: "alignment comes from the delimiter",
			table: textTable{
				header: []string{"left", "mid", "right"},
				rows:   [][]string{{"a", "b", "c"}},
				align:  []cellAlign{alignLeft, alignCenter, alignRight},
			},
			width: 40,
			want: []string{
				"┌──────┬─────┬───────┐",
				"│ left │ mid │ right │",
				"├──────┼─────┼───────┤",
				"│ a    │  b  │     c │",
				"└──────┴─────┴───────┘",
			},
		},
		{
			name: "short rows get empty cells",
			table: textTable{
				header: []string{"a", "b"},
				rows:   [][]string{{"1"}},
			},
			width: 40,
			want: []string{
				"┌───┬───┐",
				"│ a │ b │",
				"├───┼───┤",
				"│ 1 │   │",
				"└───┴───┘",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, ok := c.table.draw(c.width, noColor(), plainTableStyle)
			require.True(t, ok)
			require.Equal(t, c.want, stripRows(rows))
			for _, r := range rows {
				require.LessOrEqual(t, displayWidth(r), c.width)
			}
		})
	}
}

func TestTextTableDrawFailsWhenTheBoxCannotFit(t *testing.T) {
	table := textTable{header: []string{"a", "b", "c"}}
	_, ok := table.draw(tableBoxWidth(3)+3*minTableColumn-1, noColor(), plainTableStyle)
	require.False(t, ok, "three columns of the narrowest width do not fit")
	_, ok = table.draw(tableBoxWidth(3)+3*minTableColumn, noColor(), plainTableStyle)
	require.True(t, ok)
	_, ok = textTable{}.draw(80, noColor(), plainTableStyle)
	require.False(t, ok, "a table without columns draws nothing")
}

func TestTextTableStyles(t *testing.T) {
	table := textTable{header: []string{"Head"}, rows: [][]string{{"body"}}}
	t.Run("color: dim box lines, bold header, body in its role", func(t *testing.T) {
		th := NewTheme(colorprofile.TrueColor)
		rows, ok := table.draw(40, th, plainTableStyle)
		require.True(t, ok)
		dim := sgrParams(th.Render(RoleDim, "x"))
		require.Equal(t, th.Render(RoleDim, "┌──────┐"), rows[0], "the rule is one dim span")
		require.True(t, strings.HasPrefix(rows[1], th.Render(RoleDim, "│")), "the bar is dim: %q", rows[1])
		require.Contains(t, rows[1], th.Style(RoleFg).Bold(true).Render("Head"))
		require.Contains(t, rows[3], th.Render(RoleFg, "body"))
		require.NotContains(t, rows[3], "\x1b[1m", "body cells are not bold")
		require.NotEmpty(t, dim)
	})
	t.Run("no color: the header stays bold and nothing sets a color", func(t *testing.T) {
		rows, ok := table.draw(40, noColor(), plainTableStyle)
		require.True(t, ok)
		require.True(t, hasParam(sgrParams(rows[1]), "1"), "header bold: %q", rows[1])
		for _, r := range rows {
			require.False(t, hasColorParam(sgrParams(r)), "no color: %q", r)
		}
	})
}

func TestShrinkColumns(t *testing.T) {
	cases := []struct {
		name   string
		widths []int
		avail  int
		want   []int
	}{
		{"fits as it is", []int{3, 4}, 10, []int{3, 4}},
		{"the widest column gives up columns first", []int{3, 10}, 9, []int{3, 6}},
		{"columns end equal when both are wide", []int{8, 8}, 10, []int{5, 5}},
		{"no column goes below the minimum", []int{5, 5}, 2, []int{minTableColumn, minTableColumn}},
		{"an empty column is one column wide", []int{0, 2}, 10, []int{1, 2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := append([]int(nil), c.widths...)
			shrinkColumns(got, c.avail)
			require.Equal(t, c.want, got)
		})
	}
}
