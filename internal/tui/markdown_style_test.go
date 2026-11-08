package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// roleSGR is the opening style sequence of a role, without the text.
func roleSGR(th *Theme, r Role) string {
	s := th.Render(r, "x")
	return s[:strings.Index(s, "x")]
}

// colorParams is the foreground color of a role as SGR parameters, such as
// "38;2;45;212;191".
func colorParams(th *Theme, r Role) string {
	var out []string
	for _, p := range sgrParams(th.Render(r, "x")) {
		if p != "1" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ";")
}

func TestMarkdownHeadingsHaveNoHashMarks(t *testing.T) {
	th := NewTheme(colorprofile.TrueColor)
	cases := []struct {
		src  string
		role Role
	}{
		{"# One", RoleH1}, {"## Two", RoleH2}, {"### Three", RoleH3},
		{"#### Four", RoleFg}, {"##### Five", RoleFg}, {"###### Six", RoleFg},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			rows, _ := newMarkdownStream().render(c.src, 40, th)
			require.Len(t, rows, 1)
			text := strings.TrimLeft(c.src, "# ")
			require.Equal(t, text, ansi.Strip(rows[0].text), "no # and no space before the text")
			params := sgrParams(rows[0].text)
			require.True(t, hasParam(params, "1"), "bold: %q", rows[0].text)
			require.Contains(t, strings.Join(params, ";"), colorParams(th, c.role), "role %s: %q", c.role, rows[0].text)
		})
	}
}

func TestMarkdownHeadingsWithoutColorAreBold(t *testing.T) {
	for _, src := range []string{"# One", "## Two", "### Three", "#### Four"} {
		rows, _ := newMarkdownStream().render(src, 40, noColor())
		require.Equal(t, strings.TrimLeft(src, "# "), ansi.Strip(rows[0].text))
		require.True(t, hasParam(sgrParams(rows[0].text), "1"), "%s: %q", src, rows[0].text)
		require.False(t, hasColorParam(sgrParams(rows[0].text)), "%s: %q", src, rows[0].text)
	}
}

func TestMarkdownInlineCode(t *testing.T) {
	th := NewTheme(colorprofile.TrueColor)
	t.Run("blue bold without backticks", func(t *testing.T) {
		rows, _ := newMarkdownStream().render("Set `MaxParallelTools` now.", 60, th)
		require.Equal(t, "Set MaxParallelTools now.", ansi.Strip(rows[0].text))
		require.Contains(t, rows[0].text, "\x1b["+colorParams(th, RoleH2)+";1mMaxParallelTools", "h2 role, bold: %q", rows[0].text)
	})
	t.Run("at the end of a line the span moves whole to the next row", func(t *testing.T) {
		rows, _ := newMarkdownStream().render("Run the tests with `TestCountTrailing`", 24, th)
		require.Equal(t, []string{"Run the tests with", "TestCountTrailing"}, trimRight(mdPlain(rows)))
	})
	t.Run("without color the span is bold", func(t *testing.T) {
		rows, _ := newMarkdownStream().render("Set `x` now.", 60, noColor())
		require.Equal(t, "Set x now.", ansi.Strip(rows[0].text))
		require.Contains(t, rows[0].text, "\x1b[1mx")
	})
}

func TestMarkdownQuoteAndRuleAreMuted(t *testing.T) {
	th := NewTheme(colorprofile.TrueColor)
	rows, _ := newMarkdownStream().render("> quoted\n\n---", 40, th)
	require.Equal(t, []string{markQuote + " quoted", "", markRule}, trimRight(mdPlain(rows)))
	require.Contains(t, rows[0].text, th.Render(RoleMuted, markQuote), "the bar is muted: %q", rows[0].text)
	require.Contains(t, rows[2].text, roleSGR(th, RoleMuted), "the rule is muted: %q", rows[2].text)
}

func TestMarkdownLinkKeepsTheToolRoleAndUnderline(t *testing.T) {
	th := NewTheme(colorprofile.TrueColor)
	rows, _ := newMarkdownStream().render("See [docs](https://example.com).", 60, th)
	joined := rendered(rows)
	require.Contains(t, joined, colorParams(th, RoleTool))
	require.Contains(t, joined, ";4", "the address is underlined: %q", joined)
}
