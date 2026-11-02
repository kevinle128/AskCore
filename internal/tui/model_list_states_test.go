package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// screenRowWith returns the first row of the selector box that holds a text,
// or "".
func screenRowWith(rows []string, text string) string {
	for _, row := range rows {
		if strings.HasPrefix(row, "│") && strings.Contains(row, text) {
			return row
		}
	}
	return ""
}

// A model with no metadata shows "?" in the window column of /model.
func TestModelSelectorUnknownWindow(t *testing.T) {
	t.Run("80x24", func(t *testing.T) {
		r := idleSketch(t, 80, 24)
		r.key("ctrl+l")
		r.h.waitText("Select model")
		r.key("up", "up")
		r.h.waitText("❯   claude-opus-4-9")
		requireContentGolden(t, r.h)
		rows := r.rows()
		requireRows(t, rows, []string{
			"╭─ Select model ───────────────────────────────────────────────────────────────╮",
			"│  ⌕ type to filter                                                            │",
			"│   ● anthropic                                                                │",
			"│     claude-opus-4-7                                           1M  reasoning  │",
			"│ ❯   claude-opus-4-9                                            ?  reasoning  │",
			"│     claude-sonnet-4-6                                         1M  reasoning  │",
			"│   ● openai                                                                   │",
			"│     gpt-5.4-mini                                            400k  current    │",
			"│     gpt-5.5                                                   1M  reasoning  │",
			"╰──────────────────────────────────────────────────────────────────────────────╯",
			"   enter select  ·  esc close  ·  your draft is kept",
			ruleRow(80),
			" ⌂ ~/src/askcore                      openai/gpt-5.4-mini ◐ medium  ▱▱▱▱▱▱▱▱ 4%",
			sketchBar,
		})
		for _, row := range rows {
			if strings.HasPrefix(row, "│") {
				require.NotContains(t, row, "$", "the selector has no cost column")
			}
		}
	})
	t.Run("40x14", func(t *testing.T) {
		r := idleSketch(t, 40, 14)
		r.key("ctrl+l")
		r.h.waitText("Select model")
		r.key("up", "up")
		r.h.waitText("❯   claude-opus-4-9")
		requireContentGolden(t, r.h)
		require.Equal(t, "│ ❯   claude-opus-4-9    ?  reasoning  │", screenRowWith(r.rows(), "claude-opus-4-9"))
	})
}

// After a choice of a model with an unknown window, the footer row shows "?"
// in place of the bar and the percent, and the pane gets no row.
func TestFooterUnknownWindowAfterChoice(t *testing.T) {
	choose := func(t *testing.T, w, h int) *viewRig {
		r := idleSketch(t, w, h)
		before := r.content()
		r.key("ctrl+l")
		r.h.waitText("Select model")
		r.key("up", "up", "enter")
		r.waitCall("set_model:" + opus49)
		r.waitGone("Select model")
		r.h.waitFor("the new model in the footer row", func(*screenHarness) bool { return strings.Contains(r.footerOf(), "anthropic/claude-opus") })
		after := r.content()
		requireNoModelLine(t, after)
		// The status row confirms the saved default and takes one pane row, so
		// the pane may scroll; it must not show any new text.
		for _, row := range after[:len(after)-5] {
			if row != "" && !strings.HasPrefix(row, "┊ ") {
				require.Contains(t, before, row, "the pane draws no row for the change")
			}
		}
		return r
	}
	t.Run("80x24", func(t *testing.T) {
		r := choose(t, 80, 24)
		r.h.waitText("┊ ✓ Default model: anthropic/claude-opus-4-9")
		requireContentGolden(t, r.h)
		footer := r.footerOf()
		require.True(t, strings.HasSuffix(footer, "⌂ ~/src/askcore                          anthropic/claude-opus-4-9 ◐ medium  ?"), footer)
		require.NotContains(t, footer, GlyphBarFull)
		require.NotContains(t, footer, GlyphBarEmpty)
		require.NotContains(t, footer, "$")
	})
	t.Run("40x14", func(t *testing.T) {
		r := choose(t, 40, 14)
		requireContentGolden(t, r.h)
		require.Equal(t, "            anthropic/claude-opus-4-9 ?", r.footerOf())
	})
	t.Run("a model with a known window brings the bar back", func(t *testing.T) {
		r := choose(t, 80, 24)
		r.key("ctrl+l")
		r.h.waitText("Select model")
		r.typeText("gpt-5.5")
		r.key("enter")
		r.waitCall("set_model:" + openaiGPT55)
		r.h.waitFor("the bar in the footer row", func(*screenHarness) bool { return strings.HasSuffix(r.footerOf(), "▱▱▱▱▱▱▱▱ 2%") })
	})
}

// notInAccountFake is a session that runs on claude-opus-4-7,
// which the account list no longer has, so the leader marks its row.
func notInAccountFake() *fakeClient {
	rows := accountRows()
	rows[0].NotInAccount = true
	return accountFake(opus47, rows)
}

func notInAccountSketch(t *testing.T, w, h int) *viewRig {
	r := startView(t, w, h, withFake(notInAccountFake()))
	r.sketchTurn("claude-opus-4-7", 120_000)
	r.h.waitText("12%")
	r.key("ctrl+l")
	r.h.waitText("Select model")
	return r
}

// The running model that is not in the account list keeps its row with a
// note, or with the tag "unlisted" in a short terminal.
func TestModelSelectorNotInAccount(t *testing.T) {
	const noteRow = "│ ❯   claude-opus-4-7 · not in account list                     1M  current    │"
	t.Run("80x24", func(t *testing.T) {
		r := notInAccountSketch(t, 80, 24)
		r.h.waitText("not in account list")
		requireContentGolden(t, r.h)
		require.Equal(t, noteRow, screenRowWith(r.rows(), "claude-opus-4-7"))
		require.True(t, strings.HasSuffix(r.footerOf(), "anthropic/claude-opus-4-7 ◐ medium  ▰▱▱▱▱▱▱▱ 12%"), r.footerOf())
	})
	t.Run("40x14", func(t *testing.T) {
		r := notInAccountSketch(t, 40, 14)
		r.h.waitText("unlisted")
		requireContentGolden(t, r.h)
		require.Equal(t, "│ ❯   claude-opus-4-7   1M  unlisted   │", screenRowWith(r.rows(), "claude-opus-4-7"))
		r.requireNotOnScreen("not in account list", "current")
		require.Equal(t, "          anthropic/claude-opus-4-7 12%", r.footerOf())
	})
	t.Run("resize to 40 columns and back", func(t *testing.T) {
		r := notInAccountSketch(t, 80, 24)
		r.h.waitText("not in account list")
		r.h.resize(40, 14)
		r.h.waitText("unlisted")
		r.requireNotOnScreen("not in account list")
		r.h.resize(80, 24)
		r.h.waitText("not in account list")
		require.Equal(t, noteRow, screenRowWith(r.rows(), "claude-opus-4-7"))
		r.requireNotOnScreen("unlisted")
	})
	t.Run("another model clears the note", func(t *testing.T) {
		r := notInAccountSketch(t, 80, 24)
		r.h.waitText("not in account list")
		r.key("down", "enter")
		r.waitCall("set_model:" + opus49)
		r.waitGone("Select model")
		r.key("ctrl+l")
		r.h.waitText("Select model")
		r.h.waitText("❯   claude-opus-4-9")
		r.requireNotOnScreen("not in account list", "unlisted")
	})
	t.Run("a note that does not fit gives way", func(t *testing.T) {
		long := accountRows()
		long[0].ID = "claude-opus-4-7-with-a-long-preview-name-2026"
		long[0].NotInAccount = true
		sl := newModelSelectorFor(protocol.ACPModelsResult{Models: long, UsableOnly: true}, opus47)
		th := NewTheme(colorprofile.NoTTY)
		var got string
		for _, row := range sl.slot(70, 24, th).Rows {
			if strings.Contains(row, "preview") {
				got = row
			}
		}
		require.NotContains(t, got, "not in account list", "the id is never cut for the note")
		require.Contains(t, got, "claude-opus-4-7-with-a-long-preview-name-2026")
		require.Contains(t, got, "unlisted")
	})
	t.Run("an older leader sends no mark", func(t *testing.T) {
		r := startView(t, 80, 24, withFake(accountFake(opus47, accountRows())))
		r.key("ctrl+l")
		r.h.waitText("Select model")
		r.requireNotOnScreen("not in account list", "unlisted")
	})
}
