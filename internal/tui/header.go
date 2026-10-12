package tui

import (
	"fmt"
	"strings"

	"AskCore/pkg/protocol"
)

// Header row and shortcut bar.
//
// The header is the first row of the screen. The left side holds the logo and
// names the link (leader id, observer, in-process or lost); the working
// directory follows. The right side names the model with its thinking level,
// the context use and the session cost. The shortcut bar is the last row. Both
// read the session view; they keep no state of their own.

const (
	// warnPercent and errorPercent are the context use at which the bar and the
	// number change role.
	warnPercent  = 70
	errorPercent = 90
	barCells     = 8
)

// thinkingLevels is the order in which shift+tab cycles the thinking level.
var thinkingLevels = []protocol.ThinkingLevel{
	protocol.ThinkingOff, protocol.ThinkingMinimal, protocol.ThinkingLow, protocol.ThinkingMedium,
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
		if g.role == RoleLogo {
			b.WriteString(s.m.theme.RenderLogo(g.text))
			continue
		}
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

// headerLink returns the dot and the label that name the link.
func (s *sessionView) headerLink(short bool) (dot string, dotRole Role, label string) {
	id := ShortID(s.info.InstanceID)
	switch {
	case s.opening:
		return GlyphWorking, RoleMuted, pick(short, "starting", "leader starting")
	case s.conn == connDown:
		return GlyphToolDone, RoleError, pick(short, "lost", "leader lost")
	case s.info.Mode == ModeInProcess:
		return GlyphInProcess, RoleMuted, "in-process"
	case s.isObserver():
		return GlyphObserver, RoleWarning, pick(short, ShortID(s.sess.ID), "observer "+ShortID(s.sess.ID))
	}
	return GlyphToolDone, RoleSuccess, pick(short, id, "leader "+id)
}

func pick(short bool, shortText, longText string) string {
	if short {
		return shortText
	}
	return longText
}

// logoText is the logo at the start of the header row.
const logoText = "◢◤ ask"

// headerText returns the header row for a terminal width.
//
// The long form is "◢◤ ask  ● leader 7c1e  ⌂ ~/src/askcore   model ◐ level  ▰▱▱▱▱▱▱▱ 18%  $0.04 ".
// Under shortWidth columns it is "◢◤ ask ● 7c1e   model 18% ". When the row is
// too wide the directory gives way first (its last element, then nothing),
// then the model name is cut.
func (s *sessionView) headerText(w int) string {
	short := w < shortWidth
	dot, dotRole, label := s.headerLink(short)
	left := []segment{{logoText, RoleLogo}, {pick(short, " ", "  "), RoleFg}, {dot, dotRole}, {" " + label, RoleMuted}}

	pct := s.contextPercent()
	var meter []segment
	sep := "  "
	if short {
		meter = []segment{{fmt.Sprintf("%d%%", pct), usageRole(pct)}, {" ", RoleFg}}
		sep = " "
	} else {
		meter = []segment{
			{contextBar(pct) + fmt.Sprintf(" %d%%", pct), usageRole(pct)},
			{fmt.Sprintf("  $%.2f ", float64(s.costMicro)/1e6), RoleMuted},
		}
	}

	if s.noModel() {
		// No model: the model name, the bar and the cost give way to one word.
		right := []segment{{"no model ", RoleMuted}}
		for _, cwd := range s.cwdOptions(short) {
			l := append(append([]segment(nil), left...), cwd...)
			if segmentsWidth(l)+1+segmentsWidth(right) <= w {
				return s.joinHeader(l, right, w)
			}
		}
		return s.joinHeader(left, right, w)
	}

	name := s.modelName()
	level := ""
	if name != "" && s.reasoning() && s.thinking != "" && !short {
		level = " " + GlyphLevel + " " + string(s.thinking)
	}

	for _, cwd := range s.cwdOptions(short) {
		l := append(append([]segment(nil), left...), cwd...)
		if segmentsWidth(l)+1+segmentsWidth(modelPart(name, level, sep, meter)) <= w {
			return s.joinHeader(l, modelPart(name, level, sep, meter), w)
		}
	}
	// Cut the model name to the room that is left.
	room := w - segmentsWidth(left) - 1 - segmentsWidth(modelPart("", level, sep, meter)) - displayWidth(level) - displayWidth(sep)
	return s.joinHeader(left, modelPart(truncateTo(name, max(1, room)), level, sep, meter), w)
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

// cwdOptions lists the forms of the directory part, from the longest to none.
func (s *sessionView) cwdOptions(short bool) [][]segment {
	if short || s.cwd == "" {
		return [][]segment{nil}
	}
	path := tildePath(s.cwd, s.home)
	opts := [][]segment{{{"  " + GlyphCwd + " " + path, RoleMuted}}}
	if last := lastElementPath(path); last != path {
		opts = append(opts, []segment{{"  " + GlyphCwd + " " + last, RoleMuted}})
	}
	return append(opts, nil)
}

// joinHeader puts the left side at the start and the right side at the end.
func (s *sessionView) joinHeader(l, r []segment, w int) string {
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
