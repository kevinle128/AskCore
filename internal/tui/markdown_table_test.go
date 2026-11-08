package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"
)

func TestSplitCells(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{"| a | b |", []string{"a", "b"}},
		{"a | b", []string{"a", "b"}},
		{"| a |", []string{"a"}},
		{`| a \| b | c |`, []string{"a | b", "c"}},
		{"|  | x |", []string{"", "x"}},
		{"| `code` | **bold** |", []string{"`code`", "**bold**"}},
	}
	for _, c := range cases {
		require.Equal(t, c.want, splitCells(c.line), "%q", c.line)
	}
}

func TestDelimiterRow(t *testing.T) {
	cases := []struct {
		line  string
		want  []cellAlign
		valid bool
	}{
		{"|---|---|", []cellAlign{alignLeft, alignLeft}, true},
		{"| :-- | :-: | --: |", []cellAlign{alignLeft, alignCenter, alignRight}, true},
		{"--- | ---", []cellAlign{alignLeft, alignLeft}, true},
		{"---", nil, false},
		{"| a | b |", nil, false},
		{"    |---|---|", nil, false},
	}
	for _, c := range cases {
		got, ok := delimiterRow(c.line)
		require.Equal(t, c.valid, ok, "%q", c.line)
		if ok {
			require.Equal(t, c.want, got, "%q", c.line)
		}
	}
}

func TestFindTable(t *testing.T) {
	cases := []struct {
		name          string
		body          string
		found         bool
		before, after string
		table         textTable
	}{
		{
			name:  "a table alone",
			body:  "| Type | Job |\n|---|---|\n| `toolSlot` | State of **one** call |",
			found: true,
			table: textTable{header: []string{"Type", "Job"}, rows: [][]string{{"toolSlot", "State of one call"}}, align: []cellAlign{alignLeft, alignLeft}},
		},
		{
			name:  "text before and a heading after",
			body:  "Types\n| a | b |\n|---|---|\n| 1 | 2 |\n## Next\nmore",
			found: true, before: "Types", after: "## Next\nmore",
			table: textTable{header: []string{"a", "b"}, rows: [][]string{{"1", "2"}}, align: []cellAlign{alignLeft, alignLeft}},
		},
		{
			name:  "a row with too many or too few cells",
			body:  "| a | b |\n|---|---|\n| 1 | 2 | 3 |\n| 4 |",
			found: true,
			table: textTable{header: []string{"a", "b"}, rows: [][]string{{"1", "2"}, {"4", ""}}, align: []cellAlign{alignLeft, alignLeft}},
		},
		{name: "a header without a delimiter row is text", body: "| a | b |\n| 1 | 2 |"},
		{name: "a delimiter with another column count is text", body: "| a | b |\n|---|\n| 1 | 2 |"},
		{name: "a table in a list stays with glamour", body: "- item\n  | a | b |\n  |---|---|"},
		{name: "a table in a quote stays with glamour", body: "> | a | b |\n> |---|---|"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before, table, after, ok := findTable(c.body)
			require.Equal(t, c.found, ok)
			if !ok {
				return
			}
			require.Equal(t, c.before, before)
			require.Equal(t, c.after, after)
			require.Equal(t, c.table, table)
		})
	}
}

func TestMarkdownTableDrawsABox(t *testing.T) {
	src := "| Type | Job |\n|---|---|\n| toolSlot | State of one call |\n| toolDone | A worker is done |"
	rows, _ := newMarkdownStream().render(src, 76, noColor())
	require.Equal(t, []string{
		"┌──────────┬───────────────────┐",
		"│ Type     │ Job               │",
		"├──────────┼───────────────────┤",
		"│ toolSlot │ State of one call │",
		"│ toolDone │ A worker is done  │",
		"└──────────┴───────────────────┘",
	}, trimRight(mdPlain(rows)))
}

func TestMarkdownTableWithTextAround(t *testing.T) {
	src := "Types are here.\n| a | b |\n|---|---|\n| 1 | 2 |\n## Next"
	rows, _ := newMarkdownStream().render(src, 40, noColor())
	require.Equal(t, []string{
		"Types are here.",
		"",
		"┌───┬───┐",
		"│ a │ b │",
		"├───┼───┤",
		"│ 1 │ 2 │",
		"└───┴───┘",
		"",
		"Next",
	}, trimRight(mdPlain(rows)))
}

func TestMarkdownTableWiderThanTheWidthWrapsItsCells(t *testing.T) {
	src := "| Name | Description |\n|---|---|\n| updater | Progress that never blocks the worker loop |"
	for _, width := range []int{20, 30, 44} {
		rows, _ := newMarkdownStream().render(src, width, NewTheme(colorprofile.TrueColor))
		plain := mdPlain(rows)
		require.True(t, strings.HasPrefix(plain[0], "┌"), "width %d: %q", width, plain[0])
		require.True(t, strings.HasPrefix(plain[len(plain)-1], "└"), "width %d", width)
		for _, r := range rows {
			require.LessOrEqual(t, displayWidth(r.text), width, "width %d: %q", width, r.text)
		}
		// Read the body cells column by column: the text of each cell stays
		// whole, only its line breaks move.
		body := false
		cols := make([]string, 2)
		for _, p := range plain[1 : len(plain)-1] {
			if strings.HasPrefix(p, "├") {
				body = true
				continue
			}
			if !body {
				continue
			}
			parts := strings.Split(p, "│")
			require.Len(t, parts, 4, "width %d: %q", width, p)
			for c := range cols {
				cols[c] += strings.TrimSpace(parts[c+1]) + " "
			}
		}
		require.Equal(t, "updater", strings.ReplaceAll(cols[0], " ", ""), "width %d", width)
		require.Equal(t, "Progressthatneverblockstheworkerloop", strings.ReplaceAll(cols[1], " ", ""), "width %d", width)
	}
}

func TestMarkdownTableTooNarrowForTheBoxFallsBackToGlamour(t *testing.T) {
	src := "| a | b | c | d |\n|---|---|---|---|\n| 1 | 2 | 3 | 4 |"
	rows, _ := newMarkdownStream().render(src, 12, noColor())
	require.NotEmpty(t, rows)
	for _, r := range rows {
		require.NotContains(t, r.text, "┌", "no box at this width")
		require.LessOrEqual(t, displayWidth(r.text), 12)
	}
}

func TestMarkdownTableWithCJKCells(t *testing.T) {
	src := "| 名前 | 説明 |\n|---|---|\n| 日本語 | テスト |"
	rows, _ := newMarkdownStream().render(src, 40, noColor())
	require.Equal(t, []string{
		"┌────────┬────────┐",
		"│ 名前   │ 説明   │",
		"├────────┼────────┤",
		"│ 日本語 │ テスト │",
		"└────────┴────────┘",
	}, trimRight(mdPlain(rows)))
}

func TestMarkdownTableNoColorKeepsTheHeaderBold(t *testing.T) {
	src := "| Head |\n|---|\n| body |"
	rows, _ := newMarkdownStream().render(src, 40, noColor())
	require.True(t, hasParam(sgrParams(rows[1].text), "1"), "header bold: %q", rows[1].text)
	for _, r := range rows {
		require.False(t, hasColorParam(sgrParams(r.text)), "no color: %q", r.text)
	}
}
