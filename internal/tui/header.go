package tui

import "strings"

// Header row.
//
// The header is the first row of the screen and holds the logo only. It shows
// no link state, no directory and no model: the footer row owns the status data
// and the status rail owns the link. The row is the same in every state, so a
// lost link, an observer or an in-process session does not change it.

// logoText is the mark in the header row.
const logoText = "◢◤ ask"

// headerText returns the header row for a terminal width. One space of padding
// puts the logo in the column of the prompt glyph and of the bar text.
func (s *sessionView) headerText(w int) string {
	pad := max(0, w-1-displayWidth(logoText))
	return truncateTo(" "+s.m.theme.RenderLogo(logoText)+strings.Repeat(" ", pad), w)
}
