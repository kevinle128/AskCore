package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// The session mode is the fold mode that a thinking step takes when it ends.
// It starts as the thinkingFold setting (Options.ThinkingFold), and ctrl+t sets
// it to the result of the toggle.

// twoThoughts are the two lines of thinking of the second example turn.
var twoThoughts = []string{
	"A trailing comma gives one empty part at the end.",
	`The new test wants 2 for "a,b,".`,
}

// secondTurnEvents is the second example turn: two lines of thinking for
// 1.8 s and a one-line reply.
func secondTurnEvents() []protocol.Event {
	text := strings.Join(twoThoughts, "\n")
	reply := "Added TestCountTrailingComma; it passes."
	return flat(
		&protocol.AgentStart{},
		userAt("now add a test for a trailing comma", time.Date(2026, 10, 12, 15, 33, 0, 0, time.UTC)),
		assistantStart("claude-sonnet-5-5", 10_000),
		updateAt(10_000, protocol.ThinkingStartEvent{ContentIndex: 0}),
		updateAt(10_100, protocol.ThinkingDeltaEvent{ContentIndex: 0, Delta: text}),
		updateAt(11_800, protocol.ThinkingEndEvent{ContentIndex: 0, Content: text}),
		textParts(1, reply),
		assistantEndUsage("claude-sonnet-5-5", protocol.StopStop, 15_100, protocol.Usage{Input: 9000, Output: 220},
			protocol.Thinking{Thinking: text}, protocol.Text{Text: reply}),
		&protocol.AgentEnd{}, &protocol.AgentSettled{},
	)
}

// thinkingIDs returns the ids of the thinking steps in transcript order.
func thinkingIDs(m *Model) []string {
	var ids []string
	for _, e := range m.transcript.entries {
		if isThinking(e.block) {
			ids = append(ids, e.id)
		}
	}
	return ids
}

// requireOneRowStep checks that the step with the header is one row with its
// own mark, and that the gap row follows it.
func requireOneRowStep(t *testing.T, rows []string, header string) {
	t.Helper()
	h := rowIndexOf(t, rows, header)
	require.True(t, strings.HasPrefix(rows[h], "  ◆ "), "a collapsed step shows its own mark: %q", rows[h])
	require.Empty(t, rows[h+1], "the gap row follows the one row")
	require.NotEmpty(t, rows[h+2], "the next block follows the gap row")
}

// requireNotInRows checks that no row holds the text.
func requireNotInRows(t *testing.T, rows []string, text string) {
	t.Helper()
	for _, r := range rows {
		require.NotContains(t, r, text, "screen:\n%s", strings.Join(rows, "\n"))
	}
}

func TestThinkingFoldZeroValueIsExpanded(t *testing.T) {
	var zero ThinkingFold
	require.Equal(t, FoldExpanded, zero.Fold())
	require.Equal(t, FoldExpanded, ThinkingFold(200).Fold(), "an unknown value is the default")
	require.Equal(t, FoldTruncated, ThinkingTruncated.Fold())
	require.Equal(t, FoldCollapsed, ThinkingCollapsed.Fold())
}

func TestEndedThinkingShowsAllLinesWithoutASetting(t *testing.T) {
	m := New(Options{Client: newFakeClient(), Profile: colorprofile.NoTTY, Location: time.UTC, TickInterval: -1})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	playEvents(m, NewTranslator(TranslatorOptions{Location: time.UTC}), thinkingTurnEvents(true)...)
	rows := screenOf(m)
	h := requireOpenStep(t, rows, "Thought for", sixThoughts)
	require.Regexp(t, `^  › Thought for 4\.2s$`, rows[h])
	require.Empty(t, rows[h+2+len(sixThoughts)], "one blank row before the reply")
	require.Contains(t, rows[h+3+len(sixThoughts)], "Count now skips empty fields")
	requireNoRail(t, rows)
}

func TestEndedThinkingTakesTheModeOfTheSetting(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fold  ThinkingFold
		check func(t *testing.T, rows []string)
	}{
		{"expanded", ThinkingExpanded, func(t *testing.T, rows []string) {
			requireOpenStep(t, rows, "Thought for", sixThoughts)
		}},
		{"truncated", ThinkingTruncated, func(t *testing.T, rows []string) {
			requireOpenStep(t, rows, "Thought for", append([]string{"…"}, sixThoughts[3:]...))
			requireNotInRows(t, rows, sixThoughts[2])
		}},
		{"collapsed", ThinkingCollapsed, func(t *testing.T, rows []string) {
			requireOneRowStep(t, rows, "Thought for 4.2s")
			requireNotInRows(t, rows, sixThoughts[0])
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := sessionModel(t, colorprofile.NoTTY, 80, 24, tc.fold, thinkingTurnEvents(true)...)
			rows := screenOf(m)
			tc.check(t, rows)
			requireNoRail(t, rows)
		})
	}
}

func TestStreamingThinkingShowsTheLastLinesForEverySetting(t *testing.T) {
	for _, fold := range []ThinkingFold{ThinkingExpanded, ThinkingTruncated, ThinkingCollapsed} {
		t.Run(fold.Fold().String(), func(t *testing.T) {
			m, _ := sessionModel(t, colorprofile.NoTTY, 80, 24, fold, thinkingTurnEvents(false)...)
			require.Equal(t, FoldTruncated, m.pane.Fold(thinkingIDs(m)[0]))
			rows := screenOf(m)
			requireOpenStep(t, rows, "Thinking…", append([]string{"…"}, sixThoughts[4:]...))
			requireNoRail(t, rows)
		})
	}
}

func TestEndedThinkingShowsAllLinesAt40x14(t *testing.T) {
	m, _ := sessionModel(t, colorprofile.NoTTY, 40, 14, ThinkingExpanded, thinkingTurnEvents(true)...)
	rows := screenOf(m)
	i := rowIndexOf(t, rows, "I will edit Count and run the")
	require.Equal(t, "    I will edit Count and run the", rows[i], "the thinking wraps at column 4")
	require.Equal(t, "    tests again.", rows[i+1])
	require.Empty(t, rows[i+2])
	require.True(t, strings.HasPrefix(rows[i+3], "  Count now skips empty fields"), "the reply follows: %q", rows[i+3])
}

func TestCtrlTCollapseMakesTheNextThinkingEndCollapsed(t *testing.T) {
	m, trl := sessionModel(t, colorprofile.NoTTY, 80, 24, ThinkingExpanded, thinkingTurnEvents(true)...)
	press(t, m, "ctrl+t")
	first := thinkingIDs(m)[0]
	require.Equal(t, FoldCollapsed, m.pane.Fold(first))

	playEvents(m, trl, secondTurnEvents()...)
	second := thinkingIDs(m)[1]
	require.Equal(t, FoldCollapsed, m.pane.Fold(second))
	rows := screenOf(m)
	requireOneRowStep(t, rows, "Thought for 1.8s")
	requireNotInRows(t, rows, twoThoughts[0])
	require.Contains(t, strings.Join(rows, "\n"), "┊ "+ThinkingNotice(false))
}

func TestCtrlTExpandMakesTheNextThinkingEndExpanded(t *testing.T) {
	m, trl := sessionModel(t, colorprofile.NoTTY, 80, 24, ThinkingCollapsed, thinkingTurnEvents(true)...)
	press(t, m, "ctrl+t")
	require.Equal(t, FoldExpanded, m.pane.Fold(thinkingIDs(m)[0]))

	playEvents(m, trl, secondTurnEvents()...)
	require.Equal(t, FoldExpanded, m.pane.Fold(thinkingIDs(m)[1]))
	rows := screenOf(m)
	requireOpenStep(t, rows, "Thought for 1.8s", twoThoughts)
	require.Contains(t, strings.Join(rows, "\n"), "┊ "+ThinkingNotice(true))
}

func TestCtrlEInTheBlockListSetsTheSessionMode(t *testing.T) {
	m, trl := sessionModel(t, colorprofile.NoTTY, 80, 24, ThinkingExpanded, thinkingTurnEvents(true)...)
	press(t, m, "tab", "ctrl+e", "esc")
	playEvents(m, trl, secondTurnEvents()...)
	require.Equal(t, FoldCollapsed, m.pane.Fold(thinkingIDs(m)[1]))
}

func TestCtrlTExpandsWhenOneThinkingStepIsTruncated(t *testing.T) {
	m, trl := sessionModel(t, colorprofile.NoTTY, 80, 24, ThinkingTruncated, thinkingTurnEvents(true)...)
	playEvents(m, trl, secondTurnEvents()...)
	ids := thinkingIDs(m)
	require.NoError(t, m.pane.ExpandBlock(ids[0]))
	require.Equal(t, FoldTruncated, m.pane.Fold(ids[1]))

	press(t, m, "ctrl+t")
	for _, id := range ids {
		require.Equal(t, FoldExpanded, m.pane.Fold(id), "a step that is not expanded makes ctrl+t expand all")
	}
	press(t, m, "ctrl+t")
	for _, id := range ids {
		require.Equal(t, FoldCollapsed, m.pane.Fold(id), "all expanded, so ctrl+t collapses all")
	}
}

func TestCtrlTWithoutThinkingStepsSetsTheSessionModeToCollapsed(t *testing.T) {
	m, trl := sessionModel(t, colorprofile.NoTTY, 80, 24, ThinkingExpanded, &protocol.AgentStart{}, &protocol.AgentEnd{}, &protocol.AgentSettled{})
	press(t, m, "ctrl+t")
	require.Contains(t, strings.Join(screenOf(m), "\n"), ThinkingNotice(false))
	playEvents(m, trl, thinkingTurnEvents(true)...)
	require.Equal(t, FoldCollapsed, m.pane.Fold(thinkingIDs(m)[0]))
}

// selectThinking moves the block list selection to the first thinking step.
func selectThinking(t *testing.T, m *Model) {
	t.Helper()
	press(t, m, "tab")
	id := thinkingIDs(m)[0]
	for i := 0; m.selectedBlock() != id; i++ {
		require.Less(t, i, 20, "the selection does not reach the thinking step")
		press(t, m, "k")
	}
}

func TestCollapsingAStreamingStepKeepsItOneRowWhenItEnds(t *testing.T) {
	text := strings.Join(sixThoughts, "\n")
	m, trl := sessionModel(t, colorprofile.NoTTY, 80, 24, ThinkingExpanded, thinkingTurnEvents(false)...)
	selectThinking(t, m)
	press(t, m, "left", "esc")
	id := thinkingIDs(m)[0]
	require.Equal(t, FoldTruncated, m.pane.Fold(id), "a streaming step stays open")

	playEvents(m, trl,
		updateAt(4200, protocol.ThinkingEndEvent{ContentIndex: 0, Content: text}),
		assistantEndUsage("claude-sonnet-5-5", protocol.StopStop, 9400, protocol.Usage{}, protocol.Thinking{Thinking: text}),
		&protocol.AgentEnd{}, &protocol.AgentSettled{})
	require.Equal(t, FoldCollapsed, m.pane.Fold(id))
	rows := screenOf(m)
	require.True(t, strings.HasPrefix(rowWith(t, m, "Thought for"), "  ◆ Thought for"), "screen:\n%s", strings.Join(rows, "\n"))
	requireNotInRows(t, rows, sixThoughts[5])
}

func TestFoldKeysDoNotChangeTheSessionMode(t *testing.T) {
	for _, keys := range [][]string{{"left"}, {"h"}, {"e"}, {"right"}, {"l"}, {"E"}} {
		t.Run(keys[0], func(t *testing.T) {
			m, trl := sessionModel(t, colorprofile.NoTTY, 80, 24, ThinkingTruncated, thinkingTurnEvents(true)...)
			selectThinking(t, m)
			press(t, m, keys...)
			press(t, m, "esc")
			playEvents(m, trl, secondTurnEvents()...)
			require.Equal(t, FoldTruncated, m.pane.Fold(thinkingIDs(m)[1]), "the next step takes the session mode")
		})
	}
}

// historySnapshot is a resync with one ended turn that has a thinking step, as
// the client sends it when the TUI attaches to a session or resumes one.
func historySnapshot() SnapshotMsg {
	return SnapshotMsg{SessionID: testSessionID, Resync: true, Entries: []SnapshotEntry{
		{Kind: "message", Message: protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "run the tests"}}, Timestamp: baseMs}},
		{Kind: "message", Message: protocol.AssistantMessage{
			Content:    []protocol.AssistantBlock{protocol.Thinking{Thinking: strings.Join(sixThoughts, "\n")}, protocol.Text{Text: "Done."}},
			Model:      "m",
			StopReason: protocol.StopStop,
		}},
	}}
}

func TestLoadedHistoryTakesTheSessionMode(t *testing.T) {
	for _, tc := range []struct {
		name string
		fold ThinkingFold
		want FoldMode
	}{
		{"expanded", ThinkingExpanded, FoldExpanded},
		{"truncated", ThinkingTruncated, FoldTruncated},
		{"collapsed", ThinkingCollapsed, FoldCollapsed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := sessionModel(t, colorprofile.NoTTY, 80, 24, tc.fold, &protocol.AgentStart{}, &protocol.AgentEnd{}, &protocol.AgentSettled{})
			m.Update(clientEventMsg{msg: historySnapshot()})
			m.View()
			ids := thinkingIDs(m)
			require.Len(t, ids, 1)
			require.Equal(t, tc.want, m.pane.Fold(ids[0]))
		})
	}
	t.Run("collapsed history is one row", func(t *testing.T) {
		m, _ := sessionModel(t, colorprofile.NoTTY, 80, 24, ThinkingCollapsed, &protocol.AgentStart{}, &protocol.AgentEnd{}, &protocol.AgentSettled{})
		m.Update(clientEventMsg{msg: historySnapshot()})
		rows := screenOf(m)
		requireOneRowStep(t, rows, "◆ Thought")
		requireNotInRows(t, rows, sixThoughts[0])
	})
}

func TestLoadedHistoryTakesTheSessionModeThatCtrlTSet(t *testing.T) {
	m, trl := sessionModel(t, colorprofile.NoTTY, 80, 24, ThinkingExpanded, &protocol.AgentStart{}, &protocol.AgentEnd{}, &protocol.AgentSettled{})
	// Follow the session first, so that the resync below replaces the
	// transcript and its pane.
	m.Update(clientEventMsg{msg: SnapshotMsg{SessionID: testSessionID}})
	playEvents(m, trl, thinkingTurnEvents(true)...)
	press(t, m, "ctrl+t")
	m.Update(clientEventMsg{msg: historySnapshot()})
	m.View()
	ids := thinkingIDs(m)
	require.Len(t, ids, 1, "the resync replaced the transcript")
	require.Equal(t, FoldCollapsed, m.pane.Fold(ids[0]))
}
