package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// replyTurn plays one turn on the real program: a prompt and a reply whose
// events carry their times, as the agent sends them.
func (r *sessionRig) replyTurn(prompt, reply string, hour, minute int) {
	ts := sketchTime(hour, minute).UnixMilli()
	env := protocol.Envelope{TS: ts}
	upd := func(ev protocol.BlockEvent) protocol.Event {
		return &protocol.MessageUpdate{Envelope: env, AssistantMessageEvent: ev}
	}
	end := protocol.AssistantMessage{
		Content: []protocol.AssistantBlock{protocol.Text{Text: reply}}, Provider: "anthropic", Model: "claude-sonnet-5-5",
		StopReason: protocol.StopStop, Usage: protocol.Usage{Input: 10_200, Output: 1_800}, Timestamp: ts,
	}
	r.play(flat(
		&protocol.AgentStart{},
		userAt(prompt, sketchTime(hour, minute)),
		&protocol.MessageStart{Envelope: env, Message: pendingAssistant("claude-sonnet-5-5")},
		upd(protocol.TextStartEvent{ContentIndex: 0}),
		upd(protocol.TextDeltaEvent{ContentIndex: 0, Delta: reply}),
		upd(protocol.TextEndEvent{ContentIndex: 0, Content: reply}),
		&protocol.MessageEnd{Envelope: protocol.Envelope{TS: ts + 14_000}, Message: end},
		&protocol.AgentEnd{}, &protocol.AgentSettled{},
	)...)
}

// hasRow reports whether a screen row is exactly want after its trailing
// spaces and a reply time at its right end are removed.
func hasRow(rows []string, want string) bool {
	for _, r := range rows {
		r = strings.TrimRight(r, " ")
		if r == want || strings.TrimRight(strings.TrimSuffix(r, "17:30"), " ") == want {
			return true
		}
	}
	return false
}

func TestReplyMarkdownStyleOnScreen(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		rows  []string
		never []string
	}{
		{
			name:  "a heading shows its text at column 2 without hash marks",
			reply: "## Types\n\nThe types of the file.",
			rows:  []string{"  Types"},
			never: []string{"#"},
		},
		{
			name:  "inline code shows without backticks",
			reply: "`MaxParallelTools` (default 10) limits the workers.",
			rows:  []string{"  MaxParallelTools (default 10) limits the workers."},
			never: []string{"`"},
		},
		{
			name:  "a table shows a box with its header",
			reply: "| Type | Job |\n|---|---|\n| toolSlot | State of one call |",
			rows: []string{
				"  ┌──────────┬───────────────────┐",
				"  │ Type     │ Job               │",
				"  ├──────────┼───────────────────┤",
				"  │ toolSlot │ State of one call │",
				"  └──────────┴───────────────────┘",
			},
		},
		{
			name:  "a list item shows a bullet",
			reply: "Safety\n\n- toolGuarded turns a panic into an error.",
			rows:  []string{"  • toolGuarded turns a panic into an error."},
		},
		{
			name:  "a fenced Go block shows its code on the panel without a language row",
			reply: "The call:\n\n```go\nbody, e = p.tool.Execute(bodyCtx, tc, args)\n```",
			rows:  []string{"    body, e = p.tool.Execute(bodyCtx, tc, args)"},
			never: []string{"  go"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := startSession(t, 80, 24, sketchSource(""))
			r.replyTurn("explain tool_coordinator.go", c.reply, 17, 30)
			screen := r.h.text()
			for _, want := range c.rows {
				require.True(t, hasRow(screen, want), "row %q not on the screen:\n%s", want, strings.Join(screen, "\n"))
			}
			for _, bad := range c.never {
				for _, row := range screen {
					if strings.HasPrefix(row, "  ❯") {
						continue
					}
					require.NotEqual(t, bad, strings.TrimSpace(row), "row %q", row)
					if bad != "  go" {
						require.NotContains(t, row, bad, "row %q", row)
					}
				}
			}
		})
	}
}

func TestReplyFirstRowEndsWithItsTime(t *testing.T) {
	r := startSession(t, 80, 24, sketchSource(""))
	r.replyTurn("explain tool_coordinator.go",
		"tool_coordinator.go runs one batch of tool calls: it prepares them, runs them in parallel with a limit, and commits them in order.", 17, 30)
	requireRows(t, r.h.text(), []string{
		promptRow("explain tool_coordinator.go", "17:30"),
		"",
		"  tool_coordinator.go runs one batch of tool calls: it prepares them,    17:30",
		"  runs them in parallel with a limit, and commits them in order.",
		"  claude-sonnet-5-5 · 14s · ↑10.2k ↓1.8k",
	})
	for _, row := range r.h.text() {
		if strings.Contains(row, "prepares them,") {
			row = strings.TrimRight(row, " ")
			require.True(t, strings.HasSuffix(row, "17:30"), "%q", row)
			require.Equal(t, 80-textRightPad, displayWidth(row), "the time ends 2 columns before the edge: %q", row)
		}
	}
}

func TestReplyTimeAtNarrowWidthWrapsTheFirstRow(t *testing.T) {
	r := startSession(t, 40, 14, sketchSource(""))
	r.replyTurn("fix it", "Count now skips empty fields, and go test ./... passes.", 15, 31)
	requireRows(t, r.h.text(), []string{
		"  Count now skips empty fields,  15:31",
		"  and go test ./... passes.",
	})
}

func TestReplyFirstRowWithoutRoomShowsNoTime(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
		reply         string
		first         string
	}{
		{"a code panel fills the first row", 80, 24, "```go\nx := 1\n```\n\nDone.", "    x := 1"},
		{"a terminal too narrow for text and time", 12, 14, "Done now.", "  Done"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := startSession(t, c.width, c.height, sketchSource(""))
			r.replyTurn("go", c.reply, 17, 30)
			screen := r.h.text()
			require.True(t, hasRow(screen, c.first), "the first row keeps its text:\n%s", strings.Join(screen, "\n"))
			for _, row := range screen {
				if strings.HasPrefix(row, "  ❯") {
					continue
				}
				require.NotContains(t, row, "17:30", "no room: no reply row shows the time: %q", row)
			}
		})
	}
}

func TestReplyThatStartsWithATableOrCodeShowsNoTime(t *testing.T) {
	// The table is 72 columns wide: it fits the 76 text columns of an 80
	// column reply, but not the 69 that are left beside a time.
	wide := strings.Repeat("w", 33)
	cases := []struct {
		name  string
		reply string
		first string
	}{
		{"a short table", "| Type | Job |\n|---|---|\n| toolSlot | State of one call |\n\nDone.", "  ┌──────────┬───────────────────┐"},
		{"a table that uses the full width", "| A | B |\n|---|---|\n| " + wide + " | " + wide + " |", "  ┌" + strings.Repeat("─", 35) + "┬" + strings.Repeat("─", 35) + "┐"},
		{"a short code block", "```go\nx := 1\n```\n\nDone.", "    x := 1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := startSession(t, 80, 24, sketchSource(""))
			r.replyTurn("go", c.reply, 17, 30)
			screen := r.h.text()
			require.True(t, hasRow(screen, c.first), "the first row of the reply:\n%s", strings.Join(screen, "\n"))
			for _, row := range screen {
				if strings.HasPrefix(row, "  ❯") {
					continue
				}
				require.NotContains(t, row, "17:30", "no reply row shows the time: %q", row)
			}
		})
	}
}

func TestReplyTimeComesFromTheMessageTimes(t *testing.T) {
	reply := "Count now skips empty fields."
	want := "  " + reply + strings.Repeat(" ", 80-2-2-len(reply)-len(promptClock)) + promptClock
	t.Run("a live reply shows the time of its first text event", func(t *testing.T) {
		x := newTx(t)
		x.feed(&protocol.AgentStart{})
		x.feed(userEvents("fix it", 0)...)
		x.feed(assistantStart("m", 0),
			updateAt(0, protocol.TextStartEvent{ContentIndex: 0}),
			updateAt(10, protocol.TextDeltaEvent{ContentIndex: 0, Delta: reply}),
			updateAt(20, protocol.TextEndEvent{ContentIndex: 0, Content: reply}),
			assistantEnd("m", protocol.StopStop, 900, protocol.Text{Text: reply}))
		require.Contains(t, x.rows(), want)
	})
	t.Run("a reply from the history shows the time of its message", func(t *testing.T) {
		x := newTx(t)
		am := protocol.AssistantMessage{Content: []protocol.AssistantBlock{protocol.Text{Text: reply + "\n\nMore text."}}, Model: "m", StopReason: protocol.StopStop, Timestamp: baseMs}
		x.apply(x.trl.Snapshot(SnapshotMsg{Entries: []SnapshotEntry{{Kind: "message", Message: am}}}))
		rows := x.rows()
		require.Contains(t, rows, want)
		require.Contains(t, rows, "  More text.", "only the first block of the reply shows the time")
	})
	t.Run("events without a time show no time", func(t *testing.T) {
		x := newTx(t)
		x.feed(assistantStart("m", 0))
		x.feed(textParts(0, reply)...)
		require.Contains(t, x.rows(), "  "+reply)
	})
}

// m1Reply is the reply of the style A sketch: a paragraph, headings, a table,
// lists and a code block.
const m1Reply = "`tool_coordinator.go` runs **one batch of tool calls**: it prepares them, runs them in parallel with a limit, and commits them in order.\n\n" +
	"## Types\n\n" +
	"| Type | Job |\n|---|---|\n| toolSlot | State of one call |\n| toolDone | A worker is done |\n| updater | Progress that never blocks |\n\n" +
	"## The batch loop\n\n" +
	"1. Find the last assistant entry in the log.\n2. `MaxParallelTools` (default 10) limits the workers.\n3. Commit in order: `progress`, `afterTool`, `emitToolEnd`.\n\n" +
	"### Safety\n\n" +
	"- `toolGuarded` turns a panic into an error.\n- `cloneToolResult` deep-copies before emit.\n\n" +
	"```go\nbody, e = p.tool.Execute(bodyCtx, tc, args)\n```"

func TestReplyMarkdownStyleGolden(t *testing.T) {
	r := startSession(t, 120, 40, sketchSource(""))
	r.replyTurn("explain tool_coordinator.go", m1Reply, 17, 30)
	requireContentGolden(t, r.h)
}
