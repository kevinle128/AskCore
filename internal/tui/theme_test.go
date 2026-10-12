package tui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

var sgrPattern = regexp.MustCompile("\x1b\\[([0-9;:]*)m")

// sgrParams returns every SGR parameter of a styled string, in order.
func sgrParams(s string) []string {
	var out []string
	for _, m := range sgrPattern.FindAllStringSubmatch(s, -1) {
		out = append(out, strings.FieldsFunc(m[1], func(r rune) bool { return r == ';' || r == ':' })...)
	}
	return out
}

func hasColorParam(params []string) bool {
	for _, p := range params {
		n, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		if (n >= 30 && n <= 38) || (n >= 40 && n <= 48) || (n >= 90 && n <= 97) || (n >= 100 && n <= 107) {
			return true
		}
	}
	return false
}

func hasParam(params []string, want string) bool {
	for _, p := range params {
		if p == want {
			return true
		}
	}
	return false
}

// Every role stays readable without color. Roles that carry meaning by color
// alone in a color theme must use bold, dim, italic or reverse instead.
func TestNoColorFallbackReadable(t *testing.T) {
	const (
		bold    = "1"
		faint   = "2"
		italic  = "3"
		reverse = "7"
	)
	want := map[Role]string{
		RoleFg: "", RoleBorder: "", RoleCodePanel: "", RoleCode: "",
		RoleProv1: "", RoleProv2: "", RoleProv3: "", RoleProv4: "",
		RoleMuted: faint, RoleDim: faint,
		RolePanel: bold, RoleSelected: reverse,
		RoleAccent: bold, RoleAssistant: bold, RoleTool: bold, RoleHook: bold,
		RoleSuccess: bold, RoleWarning: bold, RoleError: bold,
		RoleThinking: italic, RoleLogo: bold,
	}
	require.Len(t, want, len(allRoles), "the table covers every role")

	for _, profile := range []colorprofile.Profile{colorprofile.ASCII, colorprofile.NoTTY} {
		th := NewTheme(profile)
		for _, role := range allRoles {
			out := th.Render(role, "sample")
			require.Equal(t, "sample", ansi.Strip(out), "role %s keeps its text", role)
			params := sgrParams(out)
			require.False(t, hasColorParam(params), "role %s must not set a color without color support: %q", role, out)
			if attr := want[role]; attr != "" {
				require.True(t, hasParam(params, attr), "role %s needs attribute %s: %q", role, attr, out)
			}
		}
		logo := th.RenderLogo("◢◤ ask")
		require.Equal(t, "◢◤ ask", ansi.Strip(logo))
		require.False(t, hasColorParam(sgrParams(logo)))
		require.True(t, hasParam(sgrParams(logo), bold))
	}
}

func TestThemePaletteTruecolor(t *testing.T) {
	th := NewTheme(colorprofile.TrueColor)
	cases := []struct {
		role Role
		want string
	}{
		{RoleFg, "38;2;217;219;232"},
		{RoleMuted, "38;2;111;115;144"},
		{RoleDim, "38;2;76;79;102"},
		{RoleBorder, "38;2;76;79;102"},
		{RolePanel, "48;2;29;31;43"},
		{RoleCodePanel, "48;2;37;40;56"},
		{RoleSelected, "48;2;45;49;80"},
		{RoleAccent, "38;2;244;114;182"},
		{RoleAssistant, "38;2;167;139;250"},
		{RoleTool, "38;2;45;212;191"},
		{RoleHook, "38;2;20;184;166"},
		{RoleThinking, "38;2;143;149;179"},
		{RoleCode, "38;2;251;146;60"},
		{RoleSuccess, "38;2;74;222;128"},
		{RoleWarning, "38;2;250;204;21"},
		{RoleError, "38;2;248;113;113"},
		{RoleProv1, "38;2;251;146;60"},
		{RoleProv2, "38;2;244;114;182"},
		{RoleProv3, "38;2;45;212;191"},
		{RoleProv4, "38;2;122;162;247"},
	}
	for _, c := range cases {
		out := th.Render(c.role, "x")
		require.Contains(t, strings.Join(sgrParams(out), ";"), c.want, "role %s: %q", c.role, out)
	}
	require.True(t, hasParam(sgrParams(th.Render(RoleThinking, "x")), "3"), "thinking is italic in every profile")
}

func TestThemePalette256(t *testing.T) {
	th := NewTheme(colorprofile.ANSI256)
	cases := []struct {
		role Role
		want string
	}{
		{RoleFg, "38;5;253"}, {RoleMuted, "38;5;244"}, {RoleDim, "38;5;240"},
		{RolePanel, "48;5;235"}, {RoleCodePanel, "48;5;236"}, {RoleSelected, "48;5;237"},
		{RoleAccent, "38;5;205"}, {RoleAssistant, "38;5;141"}, {RoleTool, "38;5;43"},
		{RoleHook, "38;5;37"},
		{RoleThinking, "38;5;103"}, {RoleCode, "38;5;209"}, {RoleSuccess, "38;5;78"},
		{RoleWarning, "38;5;220"}, {RoleError, "38;5;203"},
		{RoleProv4, "38;5;111"},
	}
	for _, c := range cases {
		out := th.Render(c.role, "x")
		require.Contains(t, strings.Join(sgrParams(out), ";"), c.want, "role %s: %q", c.role, out)
	}
	require.True(t, hasParam(sgrParams(th.Render(RoleThinking, "x")), "3"))
}

func TestThemeLogoGradient(t *testing.T) {
	tc := NewTheme(colorprofile.TrueColor)
	logo := tc.RenderLogo("◢◤ ask")
	require.Equal(t, "◢◤ ask", ansi.Strip(logo))
	colors := map[string]bool{}
	for _, m := range regexp.MustCompile(`38;2;\d+;\d+;\d+`).FindAllString(logo, -1) {
		colors[m] = true
	}
	require.GreaterOrEqual(t, len(colors), 3, "the logo sweeps from assistant through accent to code: %q", logo)

	c256 := NewTheme(colorprofile.ANSI256).RenderLogo("◢◤ ask")
	require.Contains(t, strings.Join(sgrParams(c256), ";"), "38;5;205", "256-color logo uses the accent")
}

func TestThemeRevisionChangesWithProfile(t *testing.T) {
	th := NewTheme(colorprofile.TrueColor)
	require.Equal(t, colorprofile.TrueColor, th.Profile())
	next := th.WithProfile(colorprofile.NoTTY)
	require.Equal(t, colorprofile.NoTTY, next.Profile())
	require.Greater(t, next.Revision(), th.Revision(), "caches keyed by revision must drop")
	same := next.WithProfile(colorprofile.NoTTY)
	require.Equal(t, next.Revision(), same.Revision(), "an unchanged profile keeps the revision")
}
