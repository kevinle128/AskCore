package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// --- helpers ---

func linesOf(b Block, width int, th *Theme) []string {
	return renderRows(b, RenderContext{Width: width, Theme: th})
}

func plain(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = strings.TrimRight(ansi.Strip(r), " ")
	}
	return out
}

// --- block unit tests ---

func TestUserPromptBlockLayout(t *testing.T) {
	th := noColor()

	t.Run("sketch row", func(t *testing.T) {
		b := NewUserPromptBlock("why does TestLeaderStale fail on CI?", "14:02")
		require.Equal(t, []string{promptRow("why does TestLeaderStale fail on CI?", "14:02")}, plain(linesOf(b, 80, th)))
	})

	t.Run("a long prompt wraps and keeps the time on the first row", func(t *testing.T) {
		text := strings.TrimSpace(strings.Repeat("word ", 30))
		rows := plain(renderRows(NewUserPromptBlock(text, "14:02"), RenderContext{Width: 40, Theme: th, Fold: FoldExpanded}))
		require.Greater(t, len(rows), 3)
		require.True(t, strings.HasSuffix(rows[0], "14:02"), rows[0])
		require.LessOrEqual(t, displayWidth(rows[0]), 40-2)
		for _, r := range rows[1:] {
			require.True(t, strings.HasPrefix(r, "    "), "later rows indent four columns: %q", r)
			require.NotContains(t, r, "14:02")
		}
		words := strings.Fields(strings.Replace(strings.Join(rows, " "), "14:02", "", 1))
		require.Equal(t, append([]string{"❯"}, strings.Fields(text)...), words, "no word is lost")
	})

	t.Run("wide characters never split", func(t *testing.T) {
		rows := plain(linesOf(NewUserPromptBlock("giải thích lỗi\n日本語のテスト", "14:05"), 38, th))
		first := "  ❯ giải thích lỗi"
		require.Equal(t, first+strings.Repeat(" ", 38-2-displayWidth(first)-5)+"14:05", rows[0])
		require.Equal(t, "    日本語のテスト", rows[1])
	})

	t.Run("no time", func(t *testing.T) {
		require.Equal(t, []string{"  ❯ hello"}, plain(linesOf(NewUserPromptBlock("hello", ""), 40, th)))
	})

	t.Run("control characters cannot move the cursor", func(t *testing.T) {
		joined := strings.Join(linesOf(NewUserPromptBlock("a\x1b[2Jb\x07c\x1b[31mred", ""), 40, th), "")
		require.NotContains(t, joined, "\x1b[2J")
		require.NotContains(t, joined, "\x1b[31m", "text from outside cannot set colors")
		require.NotContains(t, joined, "\x07")
	})

	t.Run("the band fills the width in color", func(t *testing.T) {
		for _, p := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256} {
			rows := linesOf(NewUserPromptBlock(strings.Repeat("band ", 20), "14:02"), 50, NewTheme(p))
			for _, r := range rows {
				require.Equal(t, 50, displayWidth(r), "%v: %q", p, ansi.Strip(r))
				require.Contains(t, r, "\x1b[48;", "the row sets the panel color")
			}
		}
	})
}

func TestAssistantTextBlockLayout(t *testing.T) {
	th := noColor()
	text := "The test waits for the stale socket check, but CI runs it with a 1 s deadline. The leader removes the stale socket after the lock probe, and the probe can take 1.2 s."
	require.Equal(t, []string{
		"  The test waits for the stale socket check, but CI runs it with a 1 s",
		"  deadline. The leader removes the stale socket after the lock probe, and the",
		"  probe can take 1.2 s.",
	}, plain(linesOf(NewAssistantTextBlock(text), 75+2+textRightPad, th)), "the reply has no glyph, its text starts at column 2 and ends 2 columns before the edge")
	require.Empty(t, linesOf(NewAssistantTextBlock(""), 80, th), "an empty reply draws nothing")
}

func TestAssistantTextBlockContinuationStartsWithTheParagraphBreak(t *testing.T) {
	th := noColor()
	require.Equal(t, []string{"  first"}, plain(linesOf(newAssistantTextBlock("first", newMarkdownStream(), true), 40, th)),
		"the first block of a reply has no empty row: the pane owns the gap above it")
	require.Equal(t, []string{"", "  more"}, plain(linesOf(newAssistantTextBlock("more", newMarkdownStream(), false), 40, th)),
		"a continuation block starts with the empty row between two paragraphs")
}

func TestAssistantTextBlockWithCode(t *testing.T) {
	text := "The test waits.\n\n```go\ndeadline := 5 * time.Second\n```\n"
	require.Equal(t, []string{
		"  The test waits.",
		"",
		"    deadline := 5 * time.Second",
	}, plain(linesOf(NewAssistantTextBlock(text), 80, noColor())), "no language row; the code is at column 4")

	t.Run("the code panel starts at the text column and ends 2 columns before the edge", func(t *testing.T) {
		rows := linesOf(NewAssistantTextBlock(text), 60, NewTheme(colorprofile.TrueColor))
		for _, r := range rows[2:] {
			require.Equal(t, 60-textRightPad, displayWidth(r))
			require.True(t, strings.HasPrefix(r, "  \x1b[48;2;37;40;56m  "), "columns 0 and 1 are blank, then 2 panel columns before the code: %q", r)
		}
	})
}

func TestUserPromptFoldsAboveThreeRows(t *testing.T) {
	th := noColor()
	b := NewUserPromptBlock(strings.TrimSpace(strings.Repeat("word ", 40)), "14:02")
	fb := b.(foldBlock)
	require.True(t, fb.foldable(40))
	require.False(t, fb.foldable(300), "one row at a very wide terminal")
	require.Equal(t, FoldTruncated, fb.defaultFold(ThinkingExpanded.defaults()))

	all := plain(renderRows(b, RenderContext{Width: 40, Theme: th, Fold: FoldExpanded}))
	for _, mode := range []FoldMode{FoldTruncated, FoldCollapsed} {
		rows := plain(renderRows(b, RenderContext{Width: 40, Theme: th, Fold: mode}))
		require.Len(t, rows, 3+1, "%v: three rows and the count row", mode)
		require.Equal(t, all[:3], rows[:3])
		hidden := len(all) - 3
		require.Equal(t, fmt.Sprintf("    … +%d lines · e to expand", hidden), rows[3], mode.String())
	}
}

func TestUserPromptThreeRowsOrFewerShowsEveryRowInEveryMode(t *testing.T) {
	b := NewUserPromptBlock("one\ntwo\nthree", "")
	for _, mode := range []FoldMode{FoldCollapsed, FoldTruncated, FoldExpanded} {
		require.Equal(t, []string{"  ❯ one", "    two", "    three"},
			plain(renderRows(b, RenderContext{Width: 40, Theme: noColor(), Fold: mode})), mode.String())
	}
	require.False(t, b.(foldBlock).foldable(40))
}

func TestThinkingBlockForms(t *testing.T) {
	th := noColor()
	view := NewTranscriptView()
	text := "The stale check runs before the deadline starts, so the deadline is too short only when the probe is slow."
	b := newThinkingBlock(text, view, false, 1000, 500, true)

	t.Run("collapsed is one row with the duration", func(t *testing.T) {
		require.Equal(t, []string{"  ◆ Thought for 0.5s"}, plain(renderRows(b, RenderContext{Width: 80, Theme: th, Fold: FoldCollapsed})))
	})
	t.Run("a history block has no duration", func(t *testing.T) {
		rows := plain(renderRows(NewThinkingBlock(text, view), RenderContext{Width: 80, Theme: th, Fold: FoldCollapsed}))
		require.Equal(t, []string{"  ◆ Thought"}, rows)
	})
	t.Run("expanded shows every line at the text column after a blank row", func(t *testing.T) {
		require.Equal(t, []string{
			"  › Thought for 0.5s",
			"",
			"    The stale check runs before the deadline starts, so the deadline is too",
			"    short only when the probe is slow.",
		}, plain(renderRows(b, RenderContext{Width: 80, Theme: th, Fold: FoldExpanded})))
	})
	t.Run("truncated shows the last three lines after an ellipsis row", func(t *testing.T) {
		many := newThinkingBlock("one\ntwo\n\nthree\nfour\nfive", view, false, 0, 0, false)
		require.Equal(t, []string{"  › Thought", "", "    …", "    three", "    four", "    five"},
			plain(renderRows(many, RenderContext{Width: 40, Theme: th, Fold: FoldTruncated})))
		few := newThinkingBlock("one\ntwo", view, false, 0, 0, false)
		require.Equal(t, []string{"  › Thought", "", "    one", "    two"},
			plain(renderRows(few, RenderContext{Width: 40, Theme: th, Fold: FoldTruncated})), "no ellipsis row when nothing is hidden")
	})
	t.Run("a block without a view still draws", func(t *testing.T) {
		require.NotPanics(t, func() { linesOf(NewThinkingBlock("text", nil), 80, th) })
	})
}

func TestStreamingThinkingShowsTheLastTwoLinesAndASpinner(t *testing.T) {
	view := NewTranscriptView()
	view.SetClock(10_000)
	view.SetSpinnerFrame(0)
	b := newThinkingBlock("first\nsecond\nthird", view, true, 8_000, 0, false)
	fb := b.(foldBlock)
	for _, mode := range []ThinkingFold{ThinkingExpanded, ThinkingTruncated, ThinkingCollapsed} {
		require.Equal(t, FoldTruncated, fb.defaultFold(mode.defaults()), "a streaming thinking is truncated in every session mode")
	}
	require.Equal(t, FoldTruncated, fb.collapseFold(), "and its lowest mode stays truncated")
	rows := plain(renderRows(b, RenderContext{Width: 40, Theme: noColor(), Fold: FoldTruncated}))
	require.Equal(t, "  › Thinking… 2s"+strings.Repeat(" ", 40-displayWidth("  › Thinking… 2s")-1-textRightPad)+"⠋", rows[0])
	require.Equal(t, []string{"", "    …", "    second", "    third"}, rows[1:])

	ended := newThinkingBlock("first\nsecond\nthird", view, false, 8_000, 2_000, true)
	for _, mode := range []ThinkingFold{ThinkingExpanded, ThinkingTruncated, ThinkingCollapsed} {
		require.Equal(t, mode.Fold(), ended.(foldBlock).defaultFold(mode.defaults()), "the block takes the session mode when it ends")
	}
}

func TestToolLineBlockForms(t *testing.T) {
	th := noColor()
	view := NewTranscriptView()

	t.Run("running shows the spinner and no empty row", func(t *testing.T) {
		rows := linesOf(NewToolLineBlock("echo", "probe code request", ToolRunning, "", "", view), 80, th)
		require.Len(t, rows, 1)
		require.Equal(t, "  ◆ echo probe code request"+strings.Repeat(" ", 80-displayWidth("  ◆ echo probe code request")-displayWidth("⠋")-textRightPad)+"⠋", plain(rows)[0])
		require.Equal(t, 80-textRightPad, displayWidth(rows[0]), "the spinner ends textRightPad columns before the edge")
	})

	t.Run("a running step shows the whole seconds once the clock is set", func(t *testing.T) {
		v := NewTranscriptView()
		v.SetClock(5_000)
		b := newToolCallBlock(toolCall{name: "bash", args: "sleep 9", startMs: 1_000}, ToolRunning, "", "", toolOutput{}, v)
		rows := plain(linesOf(b, 60, th))
		require.True(t, strings.HasPrefix(rows[0], "  ◆ Running sleep 9"), rows[0])
		require.True(t, strings.HasSuffix(rows[0], "⠋ 4s"), rows[0])
		v.SetClock(66_000)
		require.True(t, strings.HasSuffix(plain(linesOf(b, 60, th))[0], "⠋ 1m05s"))
	})

	t.Run("the spinner frame comes from the view", func(t *testing.T) {
		view.SetSpinnerFrame(3)
		require.Contains(t, plain(linesOf(NewToolLineBlock("echo", "", ToolRunning, "", "", view), 40, th))[0], "⠸")
		view.SetSpinnerFrame(13)
		require.Contains(t, plain(linesOf(NewToolLineBlock("echo", "", ToolRunning, "", "", view), 40, th))[0], "⠸", "the frame wraps")
	})

	t.Run("succeeded has nothing at the right and no empty row", func(t *testing.T) {
		rows := linesOf(NewToolLineBlock("echo", "probe code request", ToolSucceeded, "0.1s", "", view), 80, th)
		require.Equal(t, []string{"  ◆ echo probe code request"}, plain(rows))
	})

	t.Run("failed shows the reason at the right edge", func(t *testing.T) {
		rows := plain(linesOf(NewToolLineBlock("bash", "false", ToolFailed, "", "exit status 1", view), 60, th))
		require.True(t, strings.HasPrefix(rows[0], "  ◆ Ran false"), rows[0])
		require.True(t, strings.HasSuffix(rows[0], "✗ exit status 1"), rows[0])
		rows = plain(linesOf(NewToolLineBlock("bash", "", ToolFailed, "", "", view), 60, th))
		require.True(t, strings.HasSuffix(rows[0], "✗ failed"), rows[0])
		rows = plain(linesOf(NewToolLineBlock("bash", "go test", ToolFailed, "", "exit: 1", view), 60, th))
		require.True(t, strings.HasSuffix(rows[0], "✗ exit 1"), "the exit header becomes the short form: %q", rows[0])
	})

	t.Run("the description is cut first and the right column stays whole", func(t *testing.T) {
		rows := plain(linesOf(NewToolLineBlock("bash", strings.Repeat("long ", 40), ToolFailed, "", "exit: 2", view), 50, th))
		require.True(t, strings.HasSuffix(rows[0], "✗ exit 2"), rows[0])
		require.Contains(t, rows[0], "…")
		require.True(t, strings.HasPrefix(rows[0], "  ◆ Ran long long"), rows[0])
		require.LessOrEqual(t, displayWidth(rows[0]), 50)
	})

	t.Run("a long failure reason does not remove the verb", func(t *testing.T) {
		rows := plain(linesOf(NewToolLineBlock("bash", "x", ToolFailed, "", strings.Repeat("reason ", 20), view), 40, th))
		require.Contains(t, rows[0], "Ran")
		require.Contains(t, rows[0], "✗")
		require.LessOrEqual(t, displayWidth(rows[0]), 40)
	})

	t.Run("one row, whatever the arguments hold", func(t *testing.T) {
		rows := linesOf(NewToolLineBlock("echo", "line one\nline two\ttab", ToolSucceeded, "1s", "", view), 80, th)
		require.Len(t, rows, 1)
		require.Contains(t, plain(rows)[0], "line one line two tab")
	})
}

func TestNoteBlocks(t *testing.T) {
	th := noColor()
	require.Equal(t, []string{"  ↻ Attempt 1 failed: rate limit (HTTP 429). Retry 1 of 3 scheduled."},
		plain(linesOf(NewRetryNote("Attempt 1 failed: rate limit (HTTP 429). Retry 1 of 3 scheduled."), 80, th)))
	require.Equal(t, []string{"  ↻ Reconnected. Session 3f2a restored."},
		plain(linesOf(NewReconnectNote("Reconnected. Session 3f2a restored."), 80, th)))
	require.Equal(t, []string{
		"  ✗ Error (auth): anthropic token expired and refresh failed.",
		"    Run in a shell: ask auth login --provider anthropic. Then send again.",
	}, plain(linesOf(NewErrorBlock(ErrorHeadline("auth", "anthropic token expired and refresh failed."), "Run in a shell: ask auth login --provider anthropic. Then send again."), 80, th)))
	require.Equal(t, []string{
		"  ✗ Session 3f2a is gone: the leader restarted.",
		"    The transcript above is kept. Your next prompt starts a new session.",
	}, plain(linesOf(NewErrorBlock("Session 3f2a is gone: the leader restarted.", "The transcript above is kept. Your next prompt starts a new session."), 80, th)))
	require.Equal(t, []string{"  ⊘ Cancelled by you. Partial reply kept above."},
		plain(linesOf(NewCancelMarker("Cancelled by you. Partial reply kept above."), 80, th)))
	require.Equal(t, []string{
		"  ⏏ Detached from session 3f2a. The run continues in leader 7c1e.",
		"    Run ask in this directory to see it again.",
	}, plain(linesOf(NewDetachNote("Detached from session 3f2a. The run continues in leader 7c1e.", "Run ask in this directory to see it again."), 80, th)))
	require.Equal(t, "Error (error): x", ErrorHeadline("", "x"))
}

func TestNoteBlockWrapsWithIndent(t *testing.T) {
	rows := plain(linesOf(NewErrorBlock(ErrorHeadline("auth", strings.Repeat("token ", 10)), "next step"), 30, noColor()))
	require.Greater(t, len(rows), 3)
	require.True(t, strings.HasPrefix(rows[0], "  ✗ Error (auth): token"))
	for _, r := range rows[1:] {
		require.True(t, strings.HasPrefix(r, "    "), "%q", r)
	}
	require.Equal(t, "    next step", rows[len(rows)-1])
}

func TestMetaLineAndDividerHaveNoGapRow(t *testing.T) {
	th := noColor()
	require.Equal(t, []string{"  claude-opus-5-5 · 7.9s · ↑14.1k ↓2.6k · $0.031"},
		plain(linesOf(NewMetaLineBlock("claude-opus-5-5 · 7.9s · ↑14.1k ↓2.6k · $0.031"), 80, th)))
	cut := plain(linesOf(NewMetaLineBlock(strings.Repeat("meta ", 20)), 30, th))
	require.LessOrEqual(t, displayWidth(cut[0]), 30)
	require.Equal(t, []string{"  ── session 8d07 ──"}, plain(linesOf(NewDividerBlock("session 8d07"), 80, th)))
}

func TestRoleStylesAreApplied(t *testing.T) {
	th := NewTheme(colorprofile.TrueColor)
	for name, tc := range map[string]struct {
		block Block
		want  string // a sequence that the row must hold
	}{
		"error is red":       {NewErrorBlock("Error", ""), "38;2;248;113;113"},
		"retry is yellow":    {NewRetryNote("x"), "38;2;250;204;21"},
		"step glyph is teal": {NewToolLineBlock("echo", "", ToolSucceeded, "", "", nil), "38;2;45;212;191"},
		"running is yellow":  {NewToolLineBlock("echo", "", ToolRunning, "", "", nil), "38;2;250;204;21"},
		"failed step is red": {NewToolLineBlock("echo", "", ToolFailed, "", "x", nil), "38;2;248;113;113"},
		"thinking glyph":     {NewThinkingBlock("x", nil), "38;2;143;149;179"},
		"cancel is muted":    {NewCancelMarker("x"), "38;2;111;115;144"},
		"meta is dim":        {NewMetaLineBlock("m"), "38;2;76;79;102"},
	} {
		require.Contains(t, strings.Join(linesOf(tc.block, 60, th), "\n"), tc.want, name)
	}
}

// Every block, in every color mode and every fold mode, fits any width. A row
// that is too wide would break the row count of the pane.
func TestBlocksFitAnyWidth(t *testing.T) {
	view := NewTranscriptView()
	view.SetClock(9_000)
	long := strings.Repeat("日本語 words ", 12)
	out := newToolOutput(strings.Repeat(long+"\n", 9), protocol.ToolResultDetails{})
	edit := newToolOutput("", protocol.ToolResultDetails{Diff: &protocol.ToolDiff{OldText: long + "\nb\n", NewText: "a\n" + long + "\n"}})
	blocks := map[string]Block{
		"prompt":       NewUserPromptBlock(long, "14:02"),
		"reply":        NewAssistantTextBlock(streamDoc + "\n" + long),
		"thinking":     NewThinkingBlock(long+"\n"+long+"\n"+long+"\n"+long, view),
		"thinkinglive": newThinkingBlock(long+"\n"+long+"\n"+long, view, true, 1_000, 0, false),
		"tool":         newToolBlock("bash"+long, long, ToolSucceeded, "12s", "", out, view),
		"toolfail":     newToolBlock("bash", long, ToolFailed, "", long, out, view),
		"toolrun":      newToolBlock("grep", long, ToolRunning, "", "", out, view),
		"edit":         newToolBlock("edit", long, ToolSucceeded, "1s", "", edit, view),
		"write":        newToolBlock("write", long, ToolSucceeded, "1s", "", edit, view),
		"jobnotice":    newJobNotice("job j1", long, ToolNoted, long, "", view),
		"retry":        NewRetryNote(long),
		"error":        NewErrorBlock(long, long),
		"cancel":       NewCancelMarker(long),
		"meta":         NewMetaLineBlock(long),
		"divider":      NewDividerBlock(long),
		"detach":       NewDetachNote(long, long),
		"reconnect":    NewReconnectNote(long),
	}
	for _, p := range []colorprofile.Profile{colorprofile.NoTTY, colorprofile.ANSI256, colorprofile.TrueColor} {
		th := NewTheme(p)
		for name, b := range blocks {
			for _, mode := range []FoldMode{FoldCollapsed, FoldTruncated, FoldExpanded} {
				for _, width := range []int{1, 2, 3, 5, 8, 13, 21, 29, 30, 40, 59, 60, 79} {
					for _, r := range renderRows(b, RenderContext{Width: width, Theme: th, Fold: mode}) {
						require.LessOrEqual(t, displayWidth(r), width, "%s %v at %d (%v): %q", name, mode, width, p, ansi.Strip(r))
					}
				}
			}
		}
	}
}

func TestNotesDropEscapeSequencesFromOutsideText(t *testing.T) {
	th := NewTheme(colorprofile.TrueColor)
	for name, b := range map[string]Block{
		"error":    NewErrorBlock("bad \x1b[31mred\x1b[2J", "next \x1b[32mgreen"),
		"thinking": NewThinkingBlock("think \x1b[33mamber", NewTranscriptView()),
		"tool":     NewToolLineBlock("na\x1b[31mme", "ar\x1b[32mgs", ToolSucceeded, "", "", nil),
		"command": newToolCallBlock(toolCall{name: "bash", args: "x", raw: json.RawMessage(`{"command":"echo \u001b[31mred\u001b[2J"}`)},
			ToolSucceeded, "", "", toolOutput{}, nil),
		"meta": NewMetaLineBlock("m\x1b[31model"),
	} {
		joined := strings.Join(renderRows(b, RenderContext{Width: 60, Theme: th, Fold: FoldExpanded}), "")
		for _, bad := range []string{"\x1b[31m", "\x1b[32m", "\x1b[33m", "\x1b[2J"} {
			require.NotContains(t, joined, bad, name)
		}
	}
}
