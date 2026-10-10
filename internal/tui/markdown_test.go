package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

const streamDoc = "# Title\n" +
	"\n" +
	"First paragraph with **bold** and `code` that is long enough to wrap at a narrow width.\n" +
	"\n" +
	"- item one\n" +
	"- item two\n" +
	"\n" +
	"  continued item paragraph\n" +
	"\n" +
	"```go\n" +
	"func main() {\n" +
	"\n" +
	"\tfmt.Println(\"hi\")\n" +
	"}\n" +
	"```\n" +
	"\n" +
	"| a | b |\n" +
	"|---|---|\n" +
	"| 1 | 2 |\n" +
	"\n" +
	"1. first\n" +
	"2. second\n" +
	"\n" +
	"> quote line\n" +
	"> more\n" +
	"\n" +
	"Last paragraph.\n"

func mdPlain(rows []mdRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = ansi.Strip(r.text)
	}
	return out
}

func noColor() *Theme { return NewTheme(colorprofile.NoTTY) }

func TestSplitMarkdownChunks(t *testing.T) {
	kinds := func(src string) []string {
		var out []string
		for _, c := range splitMarkdown(src) {
			k := "prose"
			if c.code {
				k = "code:" + c.lang
			}
			out = append(out, fmt.Sprintf("%s|%v", k, c.closed))
		}
		return out
	}

	t.Run("paragraphs split at blank lines", func(t *testing.T) {
		require.Equal(t, []string{"prose|true", "prose|false"}, kinds("one\n\ntwo\n"))
	})
	t.Run("blank line inside an open fence does not split", func(t *testing.T) {
		chunks := splitMarkdown("para\n\n```go\nx := 1\n\ny := 2")
		require.Len(t, chunks, 2)
		require.True(t, chunks[1].code)
		require.Equal(t, "go", chunks[1].lang)
		require.False(t, chunks[1].closed)
		require.Equal(t, "x := 1\n\ny := 2", chunks[1].body)
	})
	t.Run("a closed fence is a closed chunk and text after it is a new chunk", func(t *testing.T) {
		require.Equal(t, []string{"code:|true", "prose|false"}, kinds("```\ncode\n```\nafter the fence\n"))
	})
	t.Run("tilde fences and longer closers", func(t *testing.T) {
		chunks := splitMarkdown("~~~py\nx\n```\n~~~~\nnext")
		require.Len(t, chunks, 2)
		require.Equal(t, "x\n```", chunks[0].body)
		require.Equal(t, "py", chunks[0].lang)
	})
	t.Run("a list keeps its loose items and indented paragraphs together", func(t *testing.T) {
		chunks := splitMarkdown("- a\n\n- b\n\n  more of b\n\nplain\n")
		require.Len(t, chunks, 2)
		require.Contains(t, chunks[0].body, "more of b")
		require.Equal(t, "plain\n", chunks[1].body)
	})
	t.Run("a block quote keeps its parts together", func(t *testing.T) {
		require.Len(t, splitMarkdown("> a\n\n> b\n\nz"), 2)
	})
	t.Run("an indented code block keeps its blank lines", func(t *testing.T) {
		require.Len(t, splitMarkdown("text\n\n    a\n\n    b\n\nend"), 3)
	})
	t.Run("an open html comment holds the chunk", func(t *testing.T) {
		require.Len(t, splitMarkdown("<!--\nnote\n\nmore\n-->\n\nend"), 2)
	})
	t.Run("the last chunk is open until a later line is complete", func(t *testing.T) {
		require.Equal(t, []string{"prose|false", "prose|false"}, kinds("one\n\ntw"), "next first line is not complete")
		require.Equal(t, []string{"prose|true", "prose|false"}, kinds("one\n\ntwo\n"))
	})
	t.Run("a closing fence without a line break ends the code but does not close the chunk", func(t *testing.T) {
		chunks := splitMarkdown("```\ncode\n```")
		require.Len(t, chunks, 1)
		require.Equal(t, "code", chunks[0].body)
		require.False(t, chunks[0].closed)
		require.True(t, splitMarkdown("```\ncode\n```\n")[0].closed)
	})
	t.Run("a partial fence line is code", func(t *testing.T) {
		chunks := splitMarkdown("```\ncode\n``")
		require.Equal(t, "code\n``", chunks[0].body)
	})
	t.Run("empty and blank-only text have no chunk", func(t *testing.T) {
		require.Empty(t, splitMarkdown(""))
		require.Empty(t, splitMarkdown("\n\n  \n"))
	})
}

func TestStreamingMarkdownStablePrefix(t *testing.T) {
	th := noColor()
	const width = 36
	fresh := newMarkdownStream()
	final, _ := fresh.render(streamDoc, width, th)
	require.NotEmpty(t, final)

	for _, step := range []int{1, 3, 7, 29} {
		t.Run(fmt.Sprintf("step_%d", step), func(t *testing.T) {
			s := newMarkdownStream()
			var seen []mdRow
			var steps int
			for end := step; ; end += step {
				if end > len(streamDoc) {
					end = len(streamDoc)
				}
				rows, stable := s.render(streamDoc[:end], width, th)
				steps++
				require.LessOrEqual(t, stable, len(rows))
				// Rows that were stable stay stable and never change.
				require.GreaterOrEqual(t, stable, len(seen), "the stable prefix never shrinks")
				require.Equal(t, mdPlain(seen), mdPlain(rows[:len(seen)]), "committed rows changed at byte %d", end)
				seen = rows[:stable]
				// Rows that are stable now are the head of the final render.
				require.Equal(t, mdPlain(rows[:stable]), mdPlain(final[:stable]), "stable rows differ from the final render at byte %d:\n%s", end, strings.Join(mdPlain(rows), "\n"))
				if end == len(streamDoc) {
					break
				}
			}
			rows, _ := s.render(streamDoc, width, th)
			require.Equal(t, final, rows, "the streamed result equals a fresh full render")
			chunks := len(splitMarkdown(streamDoc))
			require.LessOrEqual(t, s.renders, steps+chunks, "closed chunks render once; only the tail renders for each step")
		})
	}
}

func TestStreamingMarkdownOpenConstructsDoNotTouchThePrefix(t *testing.T) {
	th := noColor()
	for name, tail := range map[string][]string{
		"fence": {"```go\nx := 1\n\ny :=", "```go\nx := 1\n\ny := 2\n```"},
		"list":  {"- a\n- b", "- a\n- b\n\n- c"},
		"table": {"| h1 | h2 |\n|----|", "| h1 | h2 |\n|----|----|\n| 1 | 2 |"},
		"quote": {"> start\n> mo", "> start\n> more"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newMarkdownStream()
			head := "Intro paragraph that is closed.\n\n"
			first, firstStable := s.render(head+tail[0], 40, th)
			require.Positive(t, firstStable, "the intro paragraph is stable")
			second, _ := s.render(head+tail[1], 40, th)
			require.Equal(t, mdPlain(first[:firstStable]), mdPlain(second[:firstStable]))
			fresh, _ := newMarkdownStream().render(head+tail[1], 40, th)
			require.Equal(t, fresh, second)
		})
	}
}

func TestStreamingMarkdownWidthChange(t *testing.T) {
	th := noColor()
	s := newMarkdownStream()
	wide, _ := s.render(streamDoc, 60, th)
	before := s.renders

	narrow, _ := s.render(streamDoc, 28, th)
	chunks := len(splitMarkdown(streamDoc))
	require.Equal(t, chunks, s.renders-before, "a new width renders every chunk again")
	fresh, _ := newMarkdownStream().render(streamDoc, 28, th)
	require.Equal(t, fresh, narrow, "output equals a fresh render at the new width")
	require.NotEqual(t, mdPlain(wide), mdPlain(narrow))
	for _, r := range narrow {
		require.LessOrEqual(t, displayWidth(r.text), 28, "%q", ansi.Strip(r.text))
	}

	again := s.renders
	_, _ = s.render(streamDoc, 28, th)
	require.Equal(t, again, s.renders, "an unchanged call uses the cache")
}

func TestStreamingMarkdownThemeChangeDropsTheCache(t *testing.T) {
	s := newMarkdownStream()
	th := NewTheme(colorprofile.TrueColor)
	rows, _ := s.render("Some **bold** text\n\nmore", 40, th)
	n := s.renders
	other := th.WithProfile(colorprofile.NoTTY)
	plain, _ := s.render("Some **bold** text\n\nmore", 40, other)
	require.Greater(t, s.renders, n, "a new theme renders again")
	require.NotEqual(t, rows, plain)
	require.NotRegexp(t, `\x1b\[[0-9;]*[34][0-9];`, plain[0].text, "no color means no color sequences")
	require.Contains(t, plain[0].text, "\x1b[1m", "bold stays an attribute")
}

func TestMarkdownCodeBlockShowsLanguageAndCode(t *testing.T) {
	rows, _ := newMarkdownStream().render("Intro text.\n\n```go\ndeadline := 5 * time.Second\n```\n", 40, noColor())
	require.Equal(t, []string{
		"Intro text.",
		"",
		"go",
		"deadline := 5 * time.Second",
	}, trimRight(mdPlain(rows)))
	require.False(t, rows[0].panel)
	require.True(t, rows[2].panel && rows[3].panel, "language and code rows sit on the code panel")
}

func TestMarkdownCodeBlockWithoutLanguageHasNoLabelRow(t *testing.T) {
	rows, _ := newMarkdownStream().render("```\nplain\n```", 40, noColor())
	require.Equal(t, []string{"plain"}, trimRight(mdPlain(rows)))
}

func TestMarkdownCodeBlockWrapsLongLines(t *testing.T) {
	long := strings.Repeat("abcdefghij", 5)
	rows, _ := newMarkdownStream().render("```\n"+long+"\n```", 20, noColor())
	require.Equal(t, []string{long[:20], long[20:40], long[40:]}, trimRight(mdPlain(rows)))
	for _, r := range rows {
		require.LessOrEqual(t, displayWidth(r.text), 20)
	}
}

func TestMarkdownCodeBlockHighlightsInColor(t *testing.T) {
	for _, p := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256} {
		rows, _ := newMarkdownStream().render("```go\nfunc main() {}\n```", 40, NewTheme(p))
		joined := rendered(rows)
		require.Contains(t, joined, "\x1b[", "%v: code has style sequences", p)
		require.Greater(t, len(regexp.MustCompile(`\x1b\[38;[0-9;]+m`).FindAllString(joined, -1)), 1, "%v: tokens have more than one color", p)
		require.Equal(t, "go", strings.TrimSpace(ansi.Strip(rows[0].text)))
		require.Equal(t, "func main() {}", strings.TrimSpace(ansi.Strip(rows[1].text)))
		for _, r := range rows {
			require.Equal(t, 40, displayWidth(r.text), "panel rows fill the width")
		}
	}
}

func TestMarkdownUnknownLanguageStillRenders(t *testing.T) {
	rows, _ := newMarkdownStream().render("```nosuchlang\nsome code\n```", 40, NewTheme(colorprofile.TrueColor))
	require.Equal(t, []string{"nosuchlang", "some code"}, trimRight(mdPlain(rows)))
}

func TestMarkdownStripsTerminalControlSequences(t *testing.T) {
	src := "safe \x1b[31mred\x1b[0m text \x1b]0;title\x07 and\x00 bell\x07\n\n```go\nx := \"\x1b[2J\"\n```"
	rows, _ := newMarkdownStream().render(src, 60, NewTheme(colorprofile.TrueColor))
	for _, r := range rows {
		stripped := ansi.Strip(r.text)
		require.NotContains(t, r.text, "\x1b[2J")
		require.NotContains(t, r.text, "\x1b]")
		require.NotContains(t, stripped, "\x07")
		require.NotContains(t, stripped, "\x00")
	}
}

func TestMarkdownProseRowsFitTheWidth(t *testing.T) {
	for _, width := range []int{1, 2, 5, 12, 40} {
		rows, _ := newMarkdownStream().render(streamDoc+"\n日本語のテスト日本語のテスト\n", width, noColor())
		for _, r := range rows {
			require.LessOrEqual(t, displayWidth(r.text), max(width, 1), "width %d: %q", width, ansi.Strip(r.text))
		}
	}
}

func TestMarkdownChunksJoinWithOneBlankRow(t *testing.T) {
	rows, _ := newMarkdownStream().render("one\n\ntwo\n\n- a\n- b\n\n# head\n\nend", 40, noColor())
	require.Equal(t, []string{"one", "", "two", "", "• a", "• b", "", "# head", "", "end"}, trimRight(mdPlain(rows)))
}

// A glamour render of the whole document is the reference for prose: chunked
// rendering must not change how prose looks.
func TestMarkdownChunkedProseMatchesWholeDocument(t *testing.T) {
	src := "# Title\n\nFirst paragraph with **bold** text.\n\n- one\n- two\n\n  inner paragraph\n\n1. first\n2. second\n\n> quoted\n\nLast.\n"
	rows, _ := newMarkdownStream().render(src, 50, noColor())
	whole := wholeDocument(t, src, 50, noColor())
	require.Equal(t, whole, trimRight(mdPlain(rows)))
}

func trimRight(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = strings.TrimRight(r, " ")
	}
	return out
}

func rendered(rows []mdRow) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r.text)
		b.WriteByte('\n')
	}
	return b.String()
}

func wholeDocument(t *testing.T, src string, width int, th *Theme) []string {
	t.Helper()
	r, err := glamourRenderer(width, th)
	require.NoError(t, err)
	out, err := r.Render(src)
	require.NoError(t, err)
	rows := trimRight(strings.Split(ansi.Strip(out), "\n"))
	for len(rows) > 0 && rows[0] == "" {
		rows = rows[1:]
	}
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

func TestMarkdownLinkKeepsTheAddressWithoutHyperlinkSequences(t *testing.T) {
	for _, p := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.NoTTY} {
		rows, _ := newMarkdownStream().render("See [the docs](https://example.com/a) now.", 80, NewTheme(p))
		joined := rendered(rows)
		require.NotContains(t, joined, "\x1b]8", "%v", p)
		require.Contains(t, ansi.Strip(joined), "the docs")
		require.Contains(t, ansi.Strip(joined), "https://example.com/a")
	}
}

func TestMarkdownRowsCarryNoEmptyStylePairs(t *testing.T) {
	rows, _ := newMarkdownStream().render("- one\n- two\n\nplain text", 60, NewTheme(colorprofile.TrueColor))
	require.NotRegexp(t, `\x1b\[[0-9;]*m\x1b\[0?m`, rendered(rows))
}

func TestSplitMarkdownOffsets(t *testing.T) {
	src := "one\ntwo\n\n```go\ncode\n\nmore\n```\n\n  - item\n\n  - item 2\n\nlast"
	chunks := splitMarkdown(src)
	var got []string
	for _, c := range chunks {
		got = append(got, src[c.start:c.end])
	}
	require.Equal(t, []string{"one\ntwo\n", "```go\ncode\n\nmore\n```\n", "  - item\n\n  - item 2\n", "last"}, got)
}
