package tui

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
	"github.com/stretchr/testify/require"
)

// widthSamples mix narrow, wide, combining, emoji, joined emoji, tabs, controls
// and SGR sequences, the inputs that break naive column counting.
var widthSamples = []string{
	"a", "bb", "word", "longerword", " ", "  ", "\t", "\n",
	"日", "本語", "テスト", "한글", "é", "é", "😀", "👍🏽", "👨‍👩‍👧‍👦", "🇯🇵",
	"\x1b[1m", "\x1b[0m", "\x1b[38;2;244;114;182m", "\x1b[7m",
	"x\x07y", "\x7f", "https://example.com/a/very/long/path/without/spaces",
}

func randomText(r *rand.Rand) string {
	var b strings.Builder
	for n := 1 + r.Intn(24); n > 0; n-- {
		b.WriteString(widthSamples[r.Intn(len(widthSamples))])
	}
	return b.String()
}

// graphemes returns the visible graphemes of a styled string without spaces.
func visibleGraphemes(s string) []string {
	var out []string
	g := uniseg.NewGraphemes(ansi.Strip(s))
	for g.Next() {
		if strings.TrimSpace(g.Str()) != "" {
			out = append(out, g.Str())
		}
	}
	return out
}

func TestWidthWrapNeverExceedsColumns(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for width := 1; width <= 200; width++ {
		for i := 0; i < 40; i++ {
			text := randomText(r)
			rows := wrapLines(text, width)
			for _, row := range rows {
				require.LessOrEqual(t, displayWidth(row), width, "width %d row %q from %q", width, row, text)
				require.NotContains(t, row, "\n")
			}
			if width < 2 {
				// A wide character cannot fit one column; the width limit wins.
				continue
			}
			// Nothing is lost, and no grapheme is split between rows.
			want := visibleGraphemes(cleanText(text))
			var got []string
			for _, row := range rows {
				got = append(got, visibleGraphemes(row)...)
			}
			require.Equal(t, want, got, "width %d text %q", width, text)
		}
	}
}

func TestWrapKeepsWideCharactersWhole(t *testing.T) {
	rows := wrapLines("日本語のテスト", 5)
	require.Equal(t, []string{"日本", "語の", "テス", "ト"}, rows)
	rows = wrapLines("ab日", 3)
	require.Equal(t, []string{"ab", "日"}, rows)
}

func TestWrapBreaksLongWordAtColumnLimit(t *testing.T) {
	require.Equal(t, []string{"abcd", "efgh", "ij"}, wrapLines("abcdefghij", 4))
}

func TestWrapKeepsParagraphsAndBlankLines(t *testing.T) {
	require.Equal(t, []string{"one two", "three", "", "four"}, wrapLines("one two three\n\nfour", 7))
}

func TestCleanTextTabsAndControls(t *testing.T) {
	require.Equal(t, "a   b", cleanText("a\tb"), "tab stops are multiples of 4")
	require.Equal(t, "    x", cleanText("\tx"))
	require.Equal(t, "ab  c", cleanText("ab\tc"))
	require.Equal(t, "a?b", cleanText("a\x07b"))
	require.Equal(t, "a?b", cleanText("a\x7fb"))
	require.Equal(t, "a\nb", cleanText("a\nb"), "line breaks stay")
	require.Equal(t, "a?b", cleanText("a\rb"), "carriage return would move the cursor")
	require.Equal(t, "\x1b[1mbold\x1b[0m", cleanText("\x1b[1mbold\x1b[0m"), "SGR passes")
	require.Equal(t, "日  x", cleanText("日\tx"), "tab stop counts wide characters as two columns")
}

func TestCleanTextDropsTerminalControlSequences(t *testing.T) {
	// A cursor move or OSC in model text must not reach the terminal.
	require.Equal(t, "ab", cleanText("a\x1b[2Jb"))
	require.Equal(t, "ab", cleanText("a\x1b]0;title\x07b"))
}

func TestTruncateUsesEllipsis(t *testing.T) {
	require.Equal(t, "abc…", truncateTo("abcdefgh", 4))
	require.Equal(t, "abc", truncateTo("abc", 4))
	require.Equal(t, "日…", truncateTo("日本語", 3))
	require.Equal(t, "…", truncateTo("abc", 1))
	require.Equal(t, "", truncateTo("abc", 0))
	require.LessOrEqual(t, displayWidth(truncateTo("日本語テスト", 6)), 6)
}

func TestPadToFillsColumns(t *testing.T) {
	require.Equal(t, "ab   ", padTo("ab", 5))
	require.Equal(t, 5, displayWidth(padTo("日本", 5)))
	require.Equal(t, "abcdef", padTo("abcdef", 3), "no cut: padTo only adds")
}
