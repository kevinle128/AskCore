package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// Theme presentation.
//
// Code names roles, never color codes. The palette is the dark theme of the
// TUI style guide (docs/tui-style-guide.md). A role has a truecolor value, a
// 256-color value and a no-color form. The no-color form uses bold, dim,
// italic or reverse, so that every state stays readable without color.
// Bubble Tea downsamples what a Theme writes to the profile of the terminal;
// the Theme picks the table that matches the profile so that the 256-color
// values of the guide apply and not an automatic approximation.

// Role is a semantic style.
type Role int

// Roles of the style guide.
const (
	RoleFg Role = iota
	RoleMuted
	RoleDim
	RoleBorder
	RolePanel
	RoleCodePanel
	RoleSelected
	RoleAccent
	RoleAssistant
	RoleTool
	RoleThinking
	RoleCode
	RoleSuccess
	RoleWarning
	RoleError
	RoleProv1
	RoleProv2
	RoleProv3
	RoleProv4
	RoleLogo
	roleCount
)

var roleNames = [roleCount]string{
	"fg", "muted", "dim", "border", "panel", "code-panel", "selected",
	"accent", "assistant", "tool", "thinking", "code",
	"success", "warning", "error", "prov-1", "prov-2", "prov-3", "prov-4", "logo",
}

func (r Role) String() string {
	if r < 0 || r >= roleCount {
		return fmt.Sprintf("role(%d)", int(r))
	}
	return roleNames[r]
}

// allRoles lists every role once, in declaration order.
var allRoles = func() []Role {
	roles := make([]Role, roleCount)
	for i := range roles {
		roles[i] = Role(i)
	}
	return roles
}()

// attrs are the text attributes of a role.
type attrs uint8

const (
	attrBold attrs = 1 << iota
	attrFaint
	attrItalic
	attrReverse
)

// roleSpec is one row of the palette table in the style guide. An empty
// color string means "terminal default". fg and bg are truecolor values;
// fg256 and bg256 are 256-color indexes. plain holds the attributes that
// apply without color; both holds the attributes that apply in every mode.
type roleSpec struct {
	fg, bg     string
	fg256      string
	bg256      string
	plain, all attrs
}

var palette = [roleCount]roleSpec{
	RoleFg:        {fg: "#d9dbe8", fg256: "253"},
	RoleMuted:     {fg: "#6f7390", fg256: "244", plain: attrFaint},
	RoleDim:       {fg: "#4c4f66", fg256: "240", plain: attrFaint},
	RoleBorder:    {fg: "#4c4f66", fg256: "240"},
	RolePanel:     {bg: "#1d1f2b", bg256: "235", plain: attrBold},
	RoleCodePanel: {bg: "#252838", bg256: "236"},
	RoleSelected:  {bg: "#2d3150", bg256: "237", plain: attrReverse},
	RoleAccent:    {fg: "#f472b6", fg256: "205", plain: attrBold},
	RoleAssistant: {fg: "#a78bfa", fg256: "141", plain: attrBold},
	RoleTool:      {fg: "#2dd4bf", fg256: "43", plain: attrBold},
	RoleThinking:  {fg: "#8f95b3", fg256: "103", all: attrItalic},
	RoleCode:      {fg: "#fb923c", fg256: "209"},
	RoleSuccess:   {fg: "#4ade80", fg256: "78", plain: attrBold},
	RoleWarning:   {fg: "#facc15", fg256: "220", plain: attrBold},
	RoleError:     {fg: "#f87171", fg256: "203", plain: attrBold},
	RoleProv1:     {fg: "#fb923c", fg256: "209"},
	RoleProv2:     {fg: "#f472b6", fg256: "205"},
	RoleProv3:     {fg: "#2dd4bf", fg256: "43"},
	RoleProv4:     {fg: "#7aa2f7", fg256: "111"},
	RoleLogo:      {fg: "#f472b6", fg256: "205", plain: attrBold, all: attrBold},
}

// logoStops are the gradient colors of the logo: assistant, accent, code.
var logoStops = [3][3]uint8{{0xa7, 0x8b, 0xfa}, {0xf4, 0x72, 0xb6}, {0xfb, 0x92, 0x3c}}

type colorMode int

const (
	modeNone colorMode = iota
	mode256
	modeTrue
)

func modeOf(p colorprofile.Profile) colorMode {
	switch p {
	case colorprofile.TrueColor:
		return modeTrue
	case colorprofile.ANSI256, colorprofile.ANSI:
		return mode256
	default:
		return modeNone
	}
}

// Theme resolves roles to styles. It does not change after it is built; a
// new color profile makes a new Theme with a higher revision, and caches that
// depend on style output compare revisions.
type Theme struct {
	profile  colorprofile.Profile
	mode     colorMode
	revision uint64
	styles   [roleCount]lipgloss.Style
}

// NewTheme builds the theme for a terminal color profile.
func NewTheme(p colorprofile.Profile) *Theme { return buildTheme(p, 1) }

func buildTheme(p colorprofile.Profile, revision uint64) *Theme {
	t := &Theme{profile: p, mode: modeOf(p), revision: revision}
	for role := Role(0); role < roleCount; role++ {
		t.styles[role] = t.styleFor(palette[role])
	}
	return t
}

func (t *Theme) styleFor(spec roleSpec) lipgloss.Style {
	st := lipgloss.NewStyle()
	set := spec.all
	switch t.mode {
	case modeTrue:
		if spec.fg != "" {
			st = st.Foreground(lipgloss.Color(spec.fg))
		}
		if spec.bg != "" {
			st = st.Background(lipgloss.Color(spec.bg))
		}
	case mode256:
		if spec.fg256 != "" {
			st = st.Foreground(lipgloss.Color(spec.fg256))
		}
		if spec.bg256 != "" {
			st = st.Background(lipgloss.Color(spec.bg256))
		}
	default:
		set |= spec.plain
	}
	return st.
		Bold(set&attrBold != 0).
		Faint(set&attrFaint != 0).
		Italic(set&attrItalic != 0).
		Reverse(set&attrReverse != 0)
}

// Profile returns the color profile that the theme was built for.
func (t *Theme) Profile() colorprofile.Profile { return t.profile }

// Revision grows each time the theme changes.
func (t *Theme) Revision() uint64 { return t.revision }

// WithProfile returns the theme for another profile, or the same theme when
// the profile is the same.
func (t *Theme) WithProfile(p colorprofile.Profile) *Theme {
	if p == t.profile {
		return t
	}
	return buildTheme(p, t.revision+1)
}

// Style returns the style of a role.
func (t *Theme) Style(r Role) lipgloss.Style {
	if r < 0 || r >= roleCount {
		return lipgloss.NewStyle()
	}
	return t.styles[r]
}

// Render styles text with a role.
func (t *Theme) Render(r Role, s string) string {
	if s == "" {
		return s
	}
	return t.Style(r).Render(s)
}

// RenderLogo draws the logo. In truecolor the color sweeps from the assistant
// color through the accent to the code color; other modes use the logo role.
func (t *Theme) RenderLogo(s string) string {
	if t.mode != modeTrue {
		return t.Render(RoleLogo, s)
	}
	runes := []rune(s)
	var visible []int
	for i, r := range runes {
		if r != ' ' {
			visible = append(visible, i)
		}
	}
	if len(visible) < 2 {
		return t.Render(RoleLogo, s)
	}
	var b strings.Builder
	rank := 0
	for _, r := range runes {
		if r == ' ' {
			b.WriteRune(r)
			continue
		}
		pos := float64(rank) / float64(len(visible)-1)
		rank++
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(gradientAt(pos)).Render(string(r)))
	}
	return b.String()
}

// gradientAt returns the color at a position from 0 to 1 along the logo stops.
func gradientAt(pos float64) color.Color {
	segment := pos * float64(len(logoStops)-1)
	i := int(segment)
	if i >= len(logoStops)-1 {
		i = len(logoStops) - 2
	}
	frac := segment - float64(i)
	a, b := logoStops[i], logoStops[i+1]
	mix := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*frac + 0.5) }
	return color.RGBA{R: mix(a[0], b[0]), G: mix(a[1], b[1]), B: mix(a[2], b[2]), A: 0xff}
}

// Glyphs of the style guide. Each glyph has one meaning and one column.
const (
	GlyphUser       = "❯"
	GlyphAssistant  = "◆"
	GlyphThinking   = "∴"
	GlyphToolRun    = "◇"
	GlyphToolDone   = "●"
	GlyphOK         = "✓"
	GlyphFail       = "✗"
	GlyphRetry      = "↻"
	GlyphCancelled  = "⊘"
	GlyphDetached   = "⏏"
	GlyphWorking    = "◌"
	GlyphQueued     = "⧗"
	GlyphRailStatus = "┊"
	GlyphRailQueue  = "┆"
	GlyphObserver   = "◉"
	GlyphInProcess  = "○"
	GlyphCwd        = "⌂"
	GlyphLevel      = "◐"
	GlyphBarFull    = "▰"
	GlyphBarEmpty   = "▱"
	GlyphFilter     = "⌕"
)
