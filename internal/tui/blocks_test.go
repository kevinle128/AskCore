package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
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

func TestStartupHeaderBlock(t *testing.T) {
	b := NewStartupHeader("0.1.0", "leader 7c1e", "~/src/askcore")
	require.Equal(t, []string{"◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore", ""}, plain(linesOf(b, 80, noColor())))
	require.Equal(t, []string{"◢◤ ask   v0.1.0 · in-process agent · ~/src/askcore", ""},
		plain(linesOf(NewStartupHeader("0.1.0", "in-process agent", "~/src/askcore"), 80, noColor())))
	short := plain(linesOf(b, 30, noColor()))
	require.LessOrEqual(t, displayWidth(short[0]), 30)
	require.True(t, strings.HasSuffix(short[0], "…"), "a header that does not fit ends with the cut marker: %q", short[0])
}

func TestUserPromptBlockLayout(t *testing.T) {
	th := noColor()

	t.Run("sketch row", func(t *testing.T) {
		b := NewUserPromptBlock("why does TestLeaderStale fail on CI?", "14:02")
		require.Equal(t, []string{promptRow("why does TestLeaderStale fail on CI?", "14:02"), ""}, plain(linesOf(b, 80, th)))
	})

	t.Run("a long prompt wraps and keeps the time on the first row", func(t *testing.T) {
		text := strings.TrimSpace(strings.Repeat("word ", 30))
		rows := plain(linesOf(NewUserPromptBlock(text, "14:02"), 40, th))
		require.Greater(t, len(rows), 3)
		require.True(t, strings.HasSuffix(rows[0], "14:02"), rows[0])
		require.LessOrEqual(t, displayWidth(rows[0]), 40-2)
		for _, r := range rows[1 : len(rows)-1] {
			require.True(t, strings.HasPrefix(r, "    "), "later rows indent four columns: %q", r)
			require.NotContains(t, r, "14:02")
		}
		words := strings.Fields(strings.Replace(strings.Join(rows[:len(rows)-1], " "), "14:02", "", 1))
		require.Equal(t, append([]string{"❯"}, strings.Fields(text)...), words, "no word is lost")
	})

	t.Run("wide characters never split", func(t *testing.T) {
		rows := plain(linesOf(NewUserPromptBlock("giải thích lỗi\n日本語のテスト", "14:05"), 38, th))
		first := "  ❯ giải thích lỗi"
		require.Equal(t, first+strings.Repeat(" ", 38-2-displayWidth(first)-5)+"14:05", rows[0])
		require.Equal(t, "    日本語のテスト", rows[1])
	})

	t.Run("no time", func(t *testing.T) {
		require.Equal(t, []string{"  ❯ hello", ""}, plain(linesOf(NewUserPromptBlock("hello", ""), 40, th)))
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
			for _, r := range rows[:len(rows)-1] {
				require.Equal(t, 50, displayWidth(r), "%v: %q", p, ansi.Strip(r))
				require.Contains(t, r, "\x1b[48;", "the row sets the panel color")
			}
			require.Equal(t, "", rows[len(rows)-1], "one empty row follows the band")
		}
	})
}

func TestAssistantTextBlockLayout(t *testing.T) {
	th := noColor()
	text := "The test waits for the stale socket check, but CI runs it with a 1 s deadline. The leader removes the stale socket after the lock probe, and the probe can take 1.2 s."
	require.Equal(t, []string{
		"  ◆ The test waits for the stale socket check, but CI runs it with a 1 s",
		"    deadline. The leader removes the stale socket after the lock probe, and the",
		"    probe can take 1.2 s.",
	}, plain(linesOf(NewAssistantTextBlock(text), 75+4, th)))
	require.Equal(t, []string{"  ◆"}, plain(linesOf(NewAssistantTextBlock(""), 80, th)), "an empty reply shows its mark")
}

func TestAssistantTextBlockWithCode(t *testing.T) {
	text := "The test waits.\n\n```go\ndeadline := 5 * time.Second\n```\n"
	require.Equal(t, []string{
		"  ◆ The test waits.",
		"",
		"    go",
		"    deadline := 5 * time.Second",
	}, plain(linesOf(NewAssistantTextBlock(text), 80, noColor())))

	t.Run("the code panel fills the row including the indent", func(t *testing.T) {
		rows := linesOf(NewAssistantTextBlock(text), 60, NewTheme(colorprofile.TrueColor))
		for _, r := range rows[2:] {
			require.Equal(t, 60, displayWidth(r))
			require.True(t, strings.HasPrefix(r, "\x1b[48;2;37;40;56m    "), "the indent is on the panel color: %q", r)
		}
	})

	t.Run("a reply that starts with code keeps its mark on its own row", func(t *testing.T) {
		rows := plain(linesOf(NewAssistantTextBlock("```\ncode\n```"), 80, noColor()))
		require.Equal(t, []string{"  ◆", "    code"}, rows)
	})
}

func TestThinkingBlockForms(t *testing.T) {
	th := noColor()
	view := NewTranscriptView()
	b := NewThinkingBlock("The stale check runs before the deadline starts, so the deadline is too short only when the probe is slow.", view)
	require.Equal(t, []string{
		"  ∴ The stale check runs before the deadline starts, so the deadline is too",
		"    short only when the probe is slow.",
		"",
	}, plain(linesOf(b, 80, th)))

	view.SetThinkingHidden(true)
	require.Equal(t, []string{"  ∴ Thinking...", ""}, plain(linesOf(b, 80, th)), "the same block draws the short form")

	view.SetThinkingHidden(false)
	require.Equal(t, []string{"  ∴ Thinking...", ""}, plain(linesOf(NewThinkingBlock("  ", view), 80, th)), "an empty block has no text to show")
	require.NotPanics(t, func() { linesOf(NewThinkingBlock("text", nil), 80, th) }, "a block without a view shows its text")
}

func TestThinkingBlockUsesItalicRole(t *testing.T) {
	rows := linesOf(NewThinkingBlock("pondering", NewTranscriptView()), 60, NewTheme(colorprofile.TrueColor))
	require.Contains(t, rows[0], "\x1b[", "styled")
	require.Contains(t, rows[0], "\x1b[3;", "thinking text is italic")
}

func TestToolLineBlockForms(t *testing.T) {
	th := noColor()
	view := NewTranscriptView()

	t.Run("running", func(t *testing.T) {
		rows := linesOf(NewToolLineBlock("echo", "probe code request", ToolRunning, "", "", view), 80, th)
		require.Len(t, rows, 1, "a running line has no empty row below it")
		require.Equal(t, "  ◇ echo  probe code request"+strings.Repeat(" ", 80-2-displayWidth("◇ echo  probe code request")-displayWidth("running ⠋")-1)+"running ⠋", plain(rows)[0])
		require.Equal(t, 80, displayWidth(rows[0]))
	})

	t.Run("the spinner frame comes from the view", func(t *testing.T) {
		view.SetSpinnerFrame(3)
		require.Contains(t, plain(linesOf(NewToolLineBlock("echo", "", ToolRunning, "", "", view), 40, th))[0], "running ⠸")
		view.SetSpinnerFrame(13)
		require.Contains(t, plain(linesOf(NewToolLineBlock("echo", "", ToolRunning, "", "", view), 40, th))[0], "running ⠸", "the frame wraps")
	})

	t.Run("succeeded", func(t *testing.T) {
		rows := linesOf(NewToolLineBlock("echo", "probe code request", ToolSucceeded, "0.1s", "", view), 80, th)
		require.Equal(t, "", rows[1], "one empty row follows a finished line")
		require.Equal(t, "  ● echo  probe code request"+strings.Repeat(" ", 80-2-displayWidth("● echo  probe code request")-displayWidth("✓ 0.1s")-1)+"✓ 0.1s", plain(rows)[0])
	})

	t.Run("failed", func(t *testing.T) {
		rows := plain(linesOf(NewToolLineBlock("bash", "false", ToolFailed, "", "exit status 1", view), 60, th))
		require.True(t, strings.HasPrefix(rows[0], "  ● bash  false"), rows[0])
		require.True(t, strings.HasSuffix(rows[0], "✗ exit status 1"), rows[0])
		rows = plain(linesOf(NewToolLineBlock("bash", "", ToolFailed, "", "", view), 60, th))
		require.True(t, strings.HasSuffix(rows[0], "✗ failed"), rows[0])
	})

	t.Run("arguments are cut first and the status stays whole", func(t *testing.T) {
		rows := plain(linesOf(NewToolLineBlock("echo", strings.Repeat("long ", 40), ToolSucceeded, "12s", "", view), 50, th))
		require.True(t, strings.HasSuffix(rows[0], "✓ 12s"), rows[0])
		require.Contains(t, rows[0], "…")
		require.LessOrEqual(t, displayWidth(rows[0]), 49)
	})

	t.Run("a long failure reason is cut before the name", func(t *testing.T) {
		rows := plain(linesOf(NewToolLineBlock("bash", "x", ToolFailed, "", strings.Repeat("reason ", 20), view), 40, th))
		require.Contains(t, rows[0], "bash")
		require.Contains(t, rows[0], "✗")
		require.LessOrEqual(t, displayWidth(rows[0]), 40)
	})

	t.Run("one row, whatever the arguments hold", func(t *testing.T) {
		rows := linesOf(NewToolLineBlock("echo", "line one\nline two\ttab", ToolSucceeded, "1s", "", view), 80, th)
		require.Len(t, rows, 2)
		require.Contains(t, plain(rows)[0], "line one line two tab")
	})
}

func TestNoteBlocks(t *testing.T) {
	th := noColor()
	require.Equal(t, []string{"  ↻ Attempt 1 failed: rate limit (HTTP 429). Retry 1 of 3 scheduled.", ""},
		plain(linesOf(NewRetryNote("Attempt 1 failed: rate limit (HTTP 429). Retry 1 of 3 scheduled."), 80, th)))
	require.Equal(t, []string{"  ↻ Reconnected. Session 3f2a restored.", ""},
		plain(linesOf(NewReconnectNote("Reconnected. Session 3f2a restored."), 80, th)))
	require.Equal(t, []string{
		"  ✗ Error (auth): anthropic token expired and refresh failed.",
		"    Run in a shell: ask auth login --provider anthropic. Then send again.",
		"",
	}, plain(linesOf(NewErrorBlock(ErrorHeadline("auth", "anthropic token expired and refresh failed."), "Run in a shell: ask auth login --provider anthropic. Then send again."), 80, th)))
	require.Equal(t, []string{
		"  ✗ Session 3f2a is gone: the leader restarted.",
		"    The transcript above is kept. Your next prompt starts a new session.",
		"",
	}, plain(linesOf(NewErrorBlock("Session 3f2a is gone: the leader restarted.", "The transcript above is kept. Your next prompt starts a new session."), 80, th)))
	require.Equal(t, []string{"  ⊘ Cancelled by you. Partial reply kept above.", ""},
		plain(linesOf(NewCancelMarker("Cancelled by you. Partial reply kept above."), 80, th)))
	require.Equal(t, []string{
		"  ⏏ Detached from session 3f2a. The run continues in leader 7c1e.",
		"    Run ask in this directory to see it again.",
		"",
	}, plain(linesOf(NewDetachNote("Detached from session 3f2a. The run continues in leader 7c1e.", "Run ask in this directory to see it again."), 80, th)))
	require.Equal(t, "Error (error): x", ErrorHeadline("", "x"))
}

func TestNoteBlockWrapsWithIndent(t *testing.T) {
	rows := plain(linesOf(NewErrorBlock(ErrorHeadline("auth", strings.Repeat("token ", 10)), "next step"), 30, noColor()))
	require.Greater(t, len(rows), 4)
	require.True(t, strings.HasPrefix(rows[0], "  ✗ Error (auth): token"))
	for _, r := range rows[1 : len(rows)-1] {
		require.True(t, strings.HasPrefix(r, "    "), "%q", r)
	}
	require.Equal(t, "    next step", rows[len(rows)-2])
}

func TestMetaLineDividerSpacer(t *testing.T) {
	th := noColor()
	require.Equal(t, []string{"    claude-opus-5-5 · 7.9s · ↑14.1k ↓2.6k · $0.031", ""},
		plain(linesOf(NewMetaLineBlock("claude-opus-5-5 · 7.9s · ↑14.1k ↓2.6k · $0.031"), 80, th)))
	cut := plain(linesOf(NewMetaLineBlock(strings.Repeat("meta ", 20)), 30, th))
	require.LessOrEqual(t, displayWidth(cut[0]), 30)
	require.Equal(t, []string{"  ── session 8d07 ──", ""}, plain(linesOf(NewDividerBlock("session 8d07"), 80, th)))
	require.Equal(t, []string{""}, linesOf(NewSpacerBlock(), 80, th))
}

func TestRoleStylesAreApplied(t *testing.T) {
	th := NewTheme(colorprofile.TrueColor)
	for name, tc := range map[string]struct {
		block Block
		want  string // a sequence that the row must hold
	}{
		"error is red":      {NewErrorBlock("Error", ""), "38;2;248;113;113"},
		"retry is yellow":   {NewRetryNote("x"), "38;2;250;204;21"},
		"tool name is teal": {NewToolLineBlock("echo", "", ToolSucceeded, "", "", nil), "38;2;45;212;191"},
		"success mark":      {NewToolLineBlock("echo", "", ToolSucceeded, "1s", "", nil), "38;2;74;222;128"},
		"assistant mark":    {NewAssistantTextBlock("hi"), "38;2;167;139;250"},
		"cancel is muted":   {NewCancelMarker("x"), "38;2;111;115;144"},
		"meta is dim":       {NewMetaLineBlock("m"), "38;2;76;79;102"},
	} {
		require.Contains(t, strings.Join(linesOf(tc.block, 60, th), "\n"), tc.want, name)
	}
}

// Every block, in every color mode, fits any width. A row that is too wide
// would break the row count of the live region.
func TestBlocksFitAnyWidth(t *testing.T) {
	view := NewTranscriptView()
	long := strings.Repeat("日本語 words ", 12)
	blocks := map[string]Block{
		"header":    NewStartupHeader("0.1.0", "leader 7c1e", "~/src/"+long),
		"prompt":    NewUserPromptBlock(long, "14:02"),
		"reply":     NewAssistantTextBlock(streamDoc + "\n" + long),
		"thinking":  NewThinkingBlock(long, view),
		"tool":      NewToolLineBlock("echo"+long, long, ToolSucceeded, "12s", "", view),
		"toolfail":  NewToolLineBlock("echo", long, ToolFailed, "", long, view),
		"toolrun":   NewToolLineBlock("echo", long, ToolRunning, "", "", view),
		"retry":     NewRetryNote(long),
		"error":     NewErrorBlock(long, long),
		"cancel":    NewCancelMarker(long),
		"meta":      NewMetaLineBlock(long),
		"divider":   NewDividerBlock(long),
		"detach":    NewDetachNote(long, long),
		"reconnect": NewReconnectNote(long),
	}
	for _, p := range []colorprofile.Profile{colorprofile.NoTTY, colorprofile.ANSI256, colorprofile.TrueColor} {
		th := NewTheme(p)
		for name, b := range blocks {
			for _, width := range []int{1, 2, 3, 5, 8, 13, 21, 40, 79} {
				for _, r := range linesOf(b, width, th) {
					require.LessOrEqual(t, displayWidth(r), width, "%s at %d (%v): %q", name, width, p, ansi.Strip(r))
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
		"meta":     NewMetaLineBlock("m\x1b[31model"),
	} {
		joined := strings.Join(linesOf(b, 60, th), "")
		for _, bad := range []string{"\x1b[31m", "\x1b[32m", "\x1b[33m", "\x1b[2J"} {
			require.NotContains(t, joined, bad, name)
		}
	}
}
