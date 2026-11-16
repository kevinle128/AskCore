package tui

import (
	"fmt"
	"strings"

	"AskCore/pkg/protocol"
)

// Footer row and shortcut bar.
//
// The footer row is the second-to-last row of the screen, under the prompt
// pane. The left side holds the working directory. The right side names the
// model as "<provider>/<id>" with its thinking level, then the context use.
// The row shows no logo, no link state and no session cost: the connection is
// reported by the status rail and the notices, not by this row. The shortcut
// bar is the last row. Both read the session view; they keep no state of
// their own.

const (
	// warnPercent and errorPercent are the context use at which the bar and the
	// number change role.
	warnPercent  = 70
	errorPercent = 90
	barCells     = 8
)

// thinkingLevels is the order in which shift+tab cycles the thinking level.
// default (no chosen level) is first, so the cycle goes back to it after the
// last level.
var thinkingLevels = []protocol.ThinkingLevel{
	protocol.ThinkingDefault, protocol.ThinkingOff, protocol.ThinkingMinimal, protocol.ThinkingLow, protocol.ThinkingMedium,
	protocol.ThinkingHigh, protocol.ThinkingXHigh, protocol.ThinkingMax,
}

// segment is a piece of text with a role.
type segment struct {
	text string
	role Role
}

func segmentsWidth(segs []segment) int {
	n := 0
	for _, g := range segs {
		n += displayWidth(g.text)
	}
	return n
}

func (s *sessionView) renderSegments(segs []segment) string {
	var b strings.Builder
	for _, g := range segs {
		b.WriteString(s.m.theme.Render(g.role, g.text))
	}
	return b.String()
}

// contextPercent is the share of the context window that the last reply used.
func (s *sessionView) contextPercent() int {
	window := s.contextWindow()
	if window <= 0 || s.ctxTokens <= 0 {
		return 0
	}
	return int((s.ctxTokens*100 + window/2) / window)
}

// contextBar draws the context bar for a percent. The filled cells are the
// share rounded to the nearest cell.
func contextBar(pct int) string {
	filled := min(barCells, max(0, (pct*barCells+50)/100))
	return strings.Repeat(GlyphBarFull, filled) + strings.Repeat(GlyphBarEmpty, barCells-filled)
}

// usageRole is the role of the context use for a percent.
func usageRole(pct int) Role {
	switch {
	case pct >= errorPercent:
		return RoleError
	case pct >= warnPercent:
		return RoleWarning
	}
	return RoleAccent
}

func pick(short bool, shortText, longText string) string {
	if short {
		return shortText
	}
	return longText
}

// windowUnknown reports whether the catalog row of the session model is known
// and has no context window. Before the row is known the meter keeps its bar,
// so that a "?" does not change to a bar a moment after the start.
func (s *sessionView) windowUnknown() bool {
	row, ok := s.modelRow(s.modelID)
	return ok && row.ContextWindow <= 0
}

// meterSegments is the context use at the end of the footer row, with the
// trailing space of the row. A model with an unknown context window shows a
// lone "?": there is no share to draw or to rate.
func (s *sessionView) meterSegments(short bool) []segment {
	if s.windowUnknown() {
		return []segment{{"?", RoleMuted}, {" ", RoleFg}}
	}
	pct := s.contextPercent()
	text := fmt.Sprintf("%d%%", pct)
	if !short {
		text = contextBar(pct) + " " + text
	}
	return []segment{{text, usageRole(pct)}, {" ", RoleFg}}
}

// footerText returns the footer row for a terminal width.
//
// The long form is " ⌂ ~/src/askcore   openai/gpt-5.5 ◐ level  ▰▱▱▱▱▱▱▱ 18% ".
// Under shortWidth columns it is "   openai/gpt-5.5 18% ": no directory and no
// thinking level. When the row is too wide the parts give way in this order:
// the directory shortens to its last element (it never goes from shortWidth
// columns), then the end of the model id is cut (the provider part stays
// whole), and last the rest of the model text.
func (s *sessionView) footerText(w int) string {
	short := w < shortWidth
	// One space of padding puts the directory in the column of the prompt glyph
	// and of the bar text.
	lead := []segment{{" ", RoleFg}}
	cwds := s.cwdOptions(short)
	leftWith := func(cwd []segment) []segment { return append(append([]segment(nil), lead...), cwd...) }
	narrowest := leftWith(cwds[len(cwds)-1])

	if s.noModel() {
		// No model: the model text and the meter give way to one word.
		right := []segment{{"no model ", RoleMuted}}
		for _, cwd := range cwds {
			if l := leftWith(cwd); segmentsWidth(l)+1+segmentsWidth(right) <= w {
				return s.joinFooter(l, right, w)
			}
		}
		return s.joinFooter(narrowest, right, w)
	}

	meter := s.meterSegments(short)
	sep := pick(short, " ", "  ")
	ref := s.currentRef()
	name := ref.id
	if ref.provider != "" {
		name = ref.provider + "/" + ref.id
	}
	level := ""
	if name != "" && s.reasoning() && s.thinking != "" && !short {
		level = " " + GlyphLevel + " " + string(s.thinking)
	}

	for _, cwd := range cwds {
		l := leftWith(cwd)
		if r := modelPart(name, level, sep, meter); segmentsWidth(l)+1+segmentsWidth(r) <= w {
			return s.joinFooter(l, r, w)
		}
	}
	if name == "" {
		return s.joinFooter(narrowest, meter, w)
	}
	// room is the width that the model text may use next to the narrowest left
	// side.
	room := w - segmentsWidth(narrowest) - 1 - displayWidth(level) - displayWidth(sep) - segmentsWidth(meter)
	if text, ok := cutModelText(ref, room); ok {
		return s.joinFooter(narrowest, modelPart(text, level, sep, meter), w)
	}
	// Only a very narrow terminal comes here: the model text is cut from its
	// end, provider included.
	return s.joinFooter(narrowest, modelPart(truncateTo(name, max(1, room)), level, sep, meter), w)
}

// cutModelText fits "<provider>/<id>" into room columns by cutting the end of
// the id. The provider part stays whole and at least the marker of the id
// stays; ok is false when that does not fit.
func cutModelText(ref modelRef, room int) (string, bool) {
	if ref.provider == "" {
		if room < 1 {
			return "", false
		}
		return truncateTo(ref.id, room), true
	}
	prefix := ref.provider + "/"
	if displayWidth(prefix)+displayWidth(ref.id) <= room {
		return prefix + ref.id, true
	}
	if room < displayWidth(prefix)+1 {
		return "", false
	}
	return prefix + truncateTo(ref.id, room-displayWidth(prefix)), true
}

// Shortcut bar texts. The short forms are used under shortWidth columns.
const (
	barText      = "Tab blocks · ctrl+t thinking · ctrl+l model · / commands"
	barTextShort = "Tab blocks · / commands"
)

// barRow returns the shortcut bar for a terminal width.
func (s *sessionView) barRow(w int) string {
	text := pick(w < shortWidth, barTextShort, barText)
	if s.listFocused() {
		text = s.listBarText(w < shortWidth)
	}
	const nativeSelection = " · shift/option+drag select"
	if displayWidth(text+nativeSelection) < w {
		text += nativeSelection
	}
	return " " + s.m.theme.Render(RoleDim, truncateTo(text, max(0, w-1)))
}

// listFocused reports whether the block list has the input and shows its keys:
// no selector or box has taken the prompt slot.
func (s *sessionView) listFocused() bool {
	return s.m.listActive() && !s.boxOpen() && s.sel == nil
}

// listBarText is the bar of the block list. The fold key always shows; the
// mode of the selected block follows it only when that block can fold. The
// short form drops the mode and the words that the keys explain by themselves.
func (s *sessionView) listBarText(short bool) string {
	id := s.m.list.selected
	if short {
		return "j/k · e fold · Enter open"
	}
	fold := "e fold"
	if s.m.pane.Foldable(id) {
		fold += ": " + s.m.pane.Fold(id).String()
	}
	return strings.Join([]string{"j/k select", fold, "E all", "Enter open", "Tab prompt"}, " · ")
}

// modelPart is the right side: the model with its level, then the meter.
func modelPart(name, level, sep string, meter []segment) []segment {
	var r []segment
	if name != "" {
		r = append(r, segment{name, RoleAssistant})
		if level != "" {
			r = append(r, segment{level, RoleAssistant})
		}
		r = append(r, segment{sep, RoleFg})
	}
	return append(r, meter...)
}

// cwdOptions lists the forms of the directory part, from the longest to the
// narrowest. The narrowest form is the last element of the path; only a short
// row or an unknown directory has no directory part.
func (s *sessionView) cwdOptions(short bool) [][]segment {
	if short || s.cwd == "" {
		return [][]segment{nil}
	}
	path := tildePath(s.cwd, s.home)
	opts := [][]segment{{{GlyphCwd + " " + path, RoleMuted}}}
	if last := lastElementPath(path); last != path {
		opts = append(opts, []segment{{GlyphCwd + " " + last, RoleMuted}})
	}
	return opts
}

// joinFooter puts the left side at the start and the right side at the end.
func (s *sessionView) joinFooter(l, r []segment, w int) string {
	pad := max(1, w-segmentsWidth(l)-segmentsWidth(r))
	return truncateTo(s.renderSegments(l)+strings.Repeat(" ", pad)+s.renderSegments(r), w)
}

// lastElementPath shortens a path to "~/…/name" or "/…/name" and keeps the
// last element. A path with no directory between the root and the last element
// stays as it is.
func lastElementPath(path string) string {
	p := strings.TrimRight(path, "/")
	root := "/"
	if strings.HasPrefix(p, "~/") {
		root = "~/"
	}
	i := strings.LastIndex(p, "/")
	if i < len(root) {
		return path
	}
	return root + "…" + p[i:]
}
