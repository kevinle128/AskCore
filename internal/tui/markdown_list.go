package tui

import (
	"regexp"

	"charm.land/lipgloss/v2"
)

// Ordered list numbers.
//
// Glamour draws the number of an ordered list item and the ". " after it in
// the style of the list, which is the text color, and has no style rule for
// them. The style guide shows list numbers in the muted role, as the bullets.
// muteListNumbers gives the numbers that color after the render.

// listNumber matches the start of a row of an ordered list item: the indent,
// then the number and ". " as the two style spans that glamour writes for them.
// Text that only looks like a number ("1\. text") is one span and does not
// match.
var listNumber = regexp.MustCompile(`^((?:\x1b\[[0-9;]*m| )*?)\x1b\[[0-9;]*m(\d{1,9})\x1b\[0?m\x1b\[[0-9;]*m` + regexp.QuoteMeta(markNumberEnd) + `\x1b\[0?m`)

// muteListNumbers draws the list numbers of rows in the muted role. Without
// color the rows do not change.
func muteListNumbers(rows []mdRow, mode colorMode) []mdRow {
	c := roleColor(mode, RoleMuted)
	if c == nil {
		return rows
	}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(*c))
	for i, row := range rows {
		m := listNumber.FindStringSubmatchIndex(row.text)
		if m == nil {
			continue
		}
		indent, number := row.text[m[2]:m[3]], row.text[m[4]:m[5]]
		rows[i].text = indent + muted.Render(number+markNumberEnd) + row.text[m[1]:]
	}
	return rows
}
