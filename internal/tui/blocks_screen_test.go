package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// sessionRig runs a real Bubble Tea program and feeds it the block messages
// that a Translator makes from agent events, as the session view does.
type sessionRig struct {
	h   *screenHarness
	trl *Translator
}

func startSession(t *testing.T, width, height int, src fakeSource) *sessionRig {
	t.Helper()
	m := New(Options{Slots: src, Profile: colorprofile.NoTTY})
	return &sessionRig{h: startScreen(t, m, width, height), trl: NewTranslator(TranslatorOptions{Location: time.UTC})}
}

// play sends the events one at a time and waits until the screen is quiet
// after each group, so a golden compares one exact screen.
func (r *sessionRig) play(evs ...protocol.Event) {
	r.h.t.Helper()
	for _, ev := range evs {
		for _, msg := range r.trl.Event(ev) {
			r.h.send(msg)
		}
	}
	r.h.settle()
}

func (r *sessionRig) header() {
	r.h.send(AppendBlockMsg{ID: "header", Block: NewStartupHeader("0.1.0", "leader 7c1e", "~/src/askcore"), Stable: true})
	r.h.settle()
}

func sketchTime(hour, minute int) time.Time {
	return time.Date(2026, 10, 9, hour, minute, 0, 0, time.UTC)
}

func userAt(text string, when time.Time) []protocol.Event {
	m := protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: text}}, Timestamp: when.UnixMilli()}
	return []protocol.Event{&protocol.MessageStart{Message: m}, &protocol.MessageEnd{Message: m}}
}

const sketchReply = "The test waits for the stale socket check, but CI runs it with a 1 s deadline. The leader removes the stale socket after the lock probe, and the probe can take 1.2 s.\n\n```go\ndeadline := 5 * time.Second\n```"

// firstTurn is the history that the sketches share: one prompt and one reply
// with a code block and a meta line.
func (r *sessionRig) firstTurn() {
	r.header()
	usage := protocol.Usage{Input: 12_400, Output: 1_800, Cost: protocol.Cost{Total: 4_000}}
	r.play(flat(
		&protocol.AgentStart{},
		userAt("why does TestLeaderStale fail on CI?", sketchTime(14, 2)),
		assistantStart("deepseek-v4.1-flash", 0),
		textParts(0, sketchReply[:60], sketchReply[60:]),
		assistantEndUsage("deepseek-v4.1-flash", protocol.StopStop, 4200, usage, protocol.Text{Text: sketchReply}),
		&protocol.AgentEnd{}, &protocol.AgentSettled{},
	)...)
}

var firstTurnRows = []string{
	"◢◤ ask   v0.1.0 · leader 7c1e · ~/src/askcore",
	"",
	promptRow("why does TestLeaderStale fail on CI?", "14:02"),
	"",
	"  ◆ The test waits for the stale socket check, but CI runs it with a 1 s",
	"    deadline. The leader removes the stale socket after the lock probe, and the",
	"    probe can take 1.2 s.",
	"",
	"    go",
	"    deadline := 5 * time.Second",
	"    deepseek-v4.1-flash · 4.2s · ↑12.4k ↓1.8k · $0.004",
	"",
}

// requireRows checks that the screen holds the rows as one run of lines.
func requireRows(t *testing.T, got, want []string) {
	t.Helper()
	trim := func(rows []string) []string {
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = strings.TrimRight(r, " ")
		}
		return out
	}
	got, want = trim(got), trim(want)
	for i := 0; i+len(want) <= len(got); i++ {
		if strings.Join(got[i:i+len(want)], "\n") == strings.Join(want, "\n") {
			return
		}
	}
	t.Fatalf("rows not found.\nwant:\n%s\n\ngot:\n%s", strings.Join(want, "\n"), strings.Join(got, "\n"))
}

// requireContentGolden compares the screen without its empty rows at the top
// and the bottom. Where the content sits on the screen depends on how the
// renderer batched the output, which varies with timing; the rows themselves
// do not.
//
// The rows are the scrollback and the screen together. A frame that is taller
// for a moment (a live reply with its empty row) scrolls history up, and a
// terminal that is exactly as tall as the content loses its first rows from
// the visible grid for good. The scrollback keeps them, in order.
func requireContentGolden(t *testing.T, h *screenHarness) {
	t.Helper()
	rows := h.settleText()
	for len(rows) > 0 && rows[0] == "" {
		rows = rows[1:]
	}
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	golden.RequireEqual(t, strings.Join(rows, "\n")+"\n")
}

func sketchSource(status string) fakeSource {
	return fakeSource{
		placeholder: true, status: status,
		footerL: "  ● leader 7c1e   ⌂ ~/src/askcore",
		footerR: "deepseek-v4.1-flash  ▰▱▱▱▱▱▱▱ 18%  $0.04 ",
	}
}

func TestBlockGoldens(t *testing.T) {
	t.Run("idle_reply_with_code", func(t *testing.T) {
		r := startSession(t, 80, 40, sketchSource(""))
		r.firstTurn()
		requireRows(t, r.h.text(), firstTurnRows)
		requireContentGolden(t, r.h)
	})

	t.Run("streaming_with_running_tool", func(t *testing.T) {
		r := startSession(t, 80, 40, sketchSource("┊ ◌ Working… 4s · esc to interrupt"))
		r.firstTurn()
		r.play(flat(
			&protocol.AgentStart{},
			userAt("show me the probe code", sketchTime(14, 3)),
			toolStart("c1", "echo", `{"text":"probe code request"}`, 0),
			assistantStart("deepseek-v4.1-flash", 10),
			update(protocol.TextStartEvent{ContentIndex: 0}),
			update(protocol.TextDeltaEvent{ContentIndex: 0, Delta: "The probe lives in internal/leader/client.go. It dials the socket, then sends an initialize frame and waits"}),
		)...)
		requireRows(t, r.h.text(), append(append([]string{}, firstTurnRows...),
			promptRow("show me the probe code", "14:03"),
			"",
			"  ◇ echo  probe code request"+strings.Repeat(" ", 80-2-displayWidth("◇ echo  probe code request")-displayWidth("running ⠋")-1)+"running ⠋",
			"  ◆ The probe lives in internal/leader/client.go. It dials the socket, then",
			"    sends an initialize frame and waits",
		))
		requireContentGolden(t, r.h)
	})

	t.Run("tool_committed_reply_streaming", func(t *testing.T) {
		r := startSession(t, 80, 40, sketchSource("┊ ◌ Working… 9s · esc to interrupt"))
		r.firstTurn()
		r.play(flat(
			&protocol.AgentStart{},
			userAt("show me the probe code", sketchTime(14, 3)),
			toolStart("c1", "echo", `{"text":"probe code request"}`, 0),
			toolEnd("c1", "echo", false, "ok", 100),
			assistantStart("deepseek-v4.1-flash", 200),
			update(protocol.TextStartEvent{ContentIndex: 0}),
			update(protocol.TextDeltaEvent{ContentIndex: 0, Delta: "The probe lives in internal/leader/client.go. It dials the socket, then sends an initialize frame and waits"}),
		)...)
		requireRows(t, r.h.text(), append(append([]string{}, firstTurnRows...),
			promptRow("show me the probe code", "14:03"),
			"",
			"  ● echo  probe code request"+strings.Repeat(" ", 80-2-displayWidth("● echo  probe code request")-displayWidth("✓ 0.1s")-1)+"✓ 0.1s",
			"",
			"  ◆ The probe lives in internal/leader/client.go. It dials the socket, then",
			"    sends an initialize frame and waits",
		))
		requireContentGolden(t, r.h)
	})

	t.Run("retry_wait", func(t *testing.T) {
		r := startSession(t, 80, 40, sketchSource("┊ ◌ Retrying (1/5) in 4s · rate limit · esc to cancel"))
		r.firstTurn()
		r.play(flat(
			&protocol.AgentStart{},
			userAt("show me the probe code", sketchTime(14, 3)),
			&protocol.AutoRetryStart{Attempt: 1, MaxAttempts: 5, DelayMs: 4000, ErrorMessage: "rate limit (HTTP 429)"},
		)...)
		requireRows(t, r.h.text(), append(append([]string{}, firstTurnRows...),
			promptRow("show me the probe code", "14:03"),
			"",
			"  ↻ Attempt 1 failed: rate limit (HTTP 429). Retry 1 of 5 scheduled.",
			"",
		))
		requireContentGolden(t, r.h)
	})

	t.Run("error_block", func(t *testing.T) {
		r := startSession(t, 80, 40, sketchSource(""))
		r.firstTurn()
		partial := "The probe lives in internal/leader/client.go. It dials the socket, then sends an initialize frame and waits."
		r.play(flat(
			&protocol.AgentStart{},
			userAt("show me the probe code", sketchTime(14, 3)),
			assistantStart("deepseek-v4.1-flash", 0),
			textParts(0, partial),
			assistantEndUsage("deepseek-v4.1-flash", protocol.StopError, 500, protocol.Usage{}, protocol.Text{Text: partial}),
			&protocol.CycleEnd{Reason: "error", Code: "AUTH"}, &protocol.AgentEnd{}, &protocol.AgentSettled{},
		)...)
		requireRows(t, r.h.text(), append(append([]string{}, firstTurnRows...),
			promptRow("show me the probe code", "14:03"),
			"",
			"  ◆ The probe lives in internal/leader/client.go. It dials the socket, then",
			"    sends an initialize frame and waits.",
			"",
			"  ✗ Error (auth): anthropic token expired and refresh failed.",
			"    Run in a shell: ask auth login --provider anthropic. Then send again.",
			"",
		))
		requireContentGolden(t, r.h)
	})

	t.Run("cancelled", func(t *testing.T) {
		r := startSession(t, 80, 40, sketchSource(""))
		r.firstTurn()
		partial := "The probe lives in internal/leader/client.go. It dials the socket, then sends an initialize frame and waits"
		r.play(flat(
			&protocol.AgentStart{},
			userAt("show me the probe code", sketchTime(14, 3)),
			assistantStart("deepseek-v4.1-flash", 0),
			update(protocol.TextStartEvent{ContentIndex: 0}),
			update(protocol.TextDeltaEvent{ContentIndex: 0, Delta: partial}),
			assistantEndUsage("deepseek-v4.1-flash", protocol.StopAborted, 500, protocol.Usage{}, protocol.Text{Text: partial}),
			&protocol.CycleEnd{Reason: "aborted", Cause: "user"}, &protocol.AgentEnd{}, &protocol.AgentSettled{},
		)...)
		requireRows(t, r.h.text(), append(append([]string{}, firstTurnRows...),
			promptRow("show me the probe code", "14:03"),
			"",
			"  ◆ The probe lives in internal/leader/client.go. It dials the socket, then",
			"    sends an initialize frame and waits",
			"  ⊘ Cancelled by you. Partial reply kept above.",
			"",
		))
		requireContentGolden(t, r.h)
	})

	t.Run("thinking_shown", func(t *testing.T) {
		r := startSession(t, 80, 40, sketchSource(""))
		r.thinkingTurn()
		requireRows(t, r.h.text(), thinkingShownRows)
		requireContentGolden(t, r.h)
	})
}

const thinkingText = "The stale check runs before the deadline starts, so the deadline is too short only when the probe is slow. I should look at the probe timing first."
const probeReply = "The probe lives in internal/leader/client.go. It dials the socket, then sends an initialize frame and waits."

func (r *sessionRig) thinkingTurn() {
	r.h.send(AppendBlockMsg{ID: "p0", Block: NewUserPromptBlock("why does TestLeaderStale fail on CI?", "14:02"), Stable: true})
	r.h.send(AppendBlockMsg{ID: "r0", Block: NewAssistantTextBlock("The test waits for the stale socket check."), Stable: true})
	r.h.send(AppendBlockMsg{ID: "s0", Block: NewSpacerBlock(), Stable: true})
	usage := protocol.Usage{Input: 14_100, Output: 2_600, Cost: protocol.Cost{Total: 31_000}}
	r.play(flat(
		&protocol.AgentStart{},
		userAt("show me the probe code", sketchTime(14, 3)),
		assistantStart("claude-opus-5-5", 0),
		thinkingParts(0, thinkingText[:50], thinkingText[50:]),
		textParts(1, probeReply),
		assistantEndUsage("claude-opus-5-5", protocol.StopStop, 7900, usage,
			protocol.Thinking{Thinking: thinkingText}, protocol.Text{Text: probeReply}),
		&protocol.AgentEnd{}, &protocol.AgentSettled{},
	)...)
}

var thinkingShownRows = []string{
	promptRow("show me the probe code", "14:03"),
	"",
	"  ∴ The stale check runs before the deadline starts, so the deadline is too",
	"    short only when the probe is slow. I should look at the probe timing first.",
	"",
	"  ◆ The probe lives in internal/leader/client.go. It dials the socket, then",
	"    sends an initialize frame and waits.",
	"    claude-opus-5-5 · 7.9s · ↑14.1k ↓2.6k · $0.031",
	"",
}

func TestThinkingToggleRebuild(t *testing.T) {
	r := startSession(t, 80, 40, sketchSource(""))
	r.thinkingTurn()
	requireRows(t, r.h.text(), thinkingShownRows)
	before := r.h.text()
	require.Equal(t, 0, strings.Count(r.h.out.all(), purgeSequence), "no rebuild yet")

	// ctrl+t: the view changes, then the committed blocks are drawn again.
	require.True(t, r.trl.ToggleThinking())
	r.h.send(RebuildMsg{})
	r.h.waitFor("rebuild", func(h *screenHarness) bool { return strings.Count(h.out.all(), purgeSequence) == 1 })
	r.h.waitText("Thinking...")
	r.h.settle()
	hidden := r.h.text()
	requireRows(t, hidden, []string{
		promptRow("show me the probe code", "14:03"),
		"",
		"  ∴ Thinking...",
		"",
		"  ◆ The probe lives in internal/leader/client.go. It dials the socket, then",
		"    sends an initialize frame and waits.",
		"    claude-opus-5-5 · 7.9s · ↑14.1k ↓2.6k · $0.031",
		"",
	})
	require.Zero(t, r.h.count("stale check runs"), "the thinking text is gone")
	require.Equal(t, 1, r.h.count("Thinking..."))
	requireContentGolden(t, r.h)

	// The order of the committed blocks did not change.
	order := func(rows []string) []int {
		var at []int
		for _, token := range []string{"why does TestLeaderStale", "show me the probe code", "∴", "◆ The probe lives", "claude-opus-5-5"} {
			for i, row := range rows {
				if strings.Contains(row, token) {
					at = append(at, i)
					break
				}
			}
		}
		return at
	}
	for _, rows := range [][]string{before, hidden} {
		idx := order(rows)
		require.Len(t, idx, 5)
		for i := 1; i < len(idx); i++ {
			require.Greater(t, idx[i], idx[i-1])
		}
	}

	// ctrl+t again: the full text is back.
	require.False(t, r.trl.ToggleThinking())
	r.h.send(RebuildMsg{})
	r.h.waitFor("second rebuild", func(h *screenHarness) bool { return strings.Count(h.out.all(), purgeSequence) == 2 })
	r.h.waitText("short only when the probe is slow")
	r.h.settle()
	requireRows(t, r.h.text(), thinkingShownRows)
	require.Zero(t, r.h.count("Thinking..."))

	final := r.h.stop().(*Model)
	require.NoError(t, final.Err())
}

func TestThinkingToggleAppliesToThinkingStillStreaming(t *testing.T) {
	r := startSession(t, 80, 40, sketchSource(""))
	r.play(flat(assistantStart("m", 0), update(protocol.ThinkingStartEvent{ContentIndex: 0}), update(protocol.ThinkingDeltaEvent{ContentIndex: 0, Delta: "still thinking"}))...)
	r.h.waitText("still thinking")
	require.True(t, r.trl.ToggleThinking())
	// A live block draws from the view at the next frame; any message does it.
	r.play(update(protocol.ThinkingDeltaEvent{ContentIndex: 0, Delta: " more"}))
	r.h.waitText("Thinking...")
	require.Zero(t, r.h.count("still thinking"))
}

func TestCancelMarkerAtSettleOnly(t *testing.T) {
	r := startSession(t, 80, 40, sketchSource("┊ ◌ Working… 4s · esc to interrupt"))
	partial := "The probe lives in internal/leader/client.go. It dials the socket, then"
	r.play(flat(
		&protocol.AgentStart{},
		userAt("show me the probe code", sketchTime(14, 3)),
		assistantStart("m", 0),
		update(protocol.TextStartEvent{ContentIndex: 0}),
		update(protocol.TextDeltaEvent{ContentIndex: 0, Delta: partial}),
	)...)
	r.h.waitText("It dials the socket")

	// esc: the client asks the agent to cancel. The screen shows nothing new
	// until events come back.
	time.Sleep(200 * time.Millisecond)
	require.Zero(t, r.h.count("Cancelled"), "the key press alone writes no marker")

	r.play(flat(
		assistantEndUsage("m", protocol.StopAborted, 900, protocol.Usage{}, protocol.Text{Text: partial}),
		&protocol.CycleEnd{Reason: "aborted", Cause: "user"}, &protocol.AgentEnd{},
	)...)
	time.Sleep(200 * time.Millisecond)
	require.Zero(t, r.h.count("Cancelled"), "the run is aborted but not settled")

	r.play(&protocol.AgentSettled{})
	r.h.waitText("⊘ Cancelled by you. Partial reply kept above.")
	requireRows(t, r.h.text(), []string{
		"  ◆ The probe lives in internal/leader/client.go. It dials the socket, then",
		"  ⊘ Cancelled by you. Partial reply kept above.",
		"",
	})
	require.Equal(t, 1, r.h.count("Cancelled"))
}

// A reply that streams in many small pieces ends up on the screen exactly
// once, and the commit path never rewrites a block that is in the scrollback.
// The screen is small, so the reply is far taller than the live region: it can
// only fit because closed paragraphs leave the tail while the reply streams.
func TestStreamedReplyCommitsOnceThroughRenderer(t *testing.T) {
	r := startSession(t, 60, 14, sketchSource(""))
	var paras []string
	for i := 0; i < 30; i++ {
		paras = append(paras, "alpha beta gamma delta epsilon "+strings.Repeat("filler ", 3))
	}
	all := strings.Join(paras, "\n\n")
	evs := flat(&protocol.AgentStart{}, userAt("go", sketchTime(14, 3)), assistantStart("m", 0), update(protocol.TextStartEvent{ContentIndex: 0}))
	for i := 0; i < len(all); i += 9 {
		evs = append(evs, update(protocol.TextDeltaEvent{ContentIndex: 0, Delta: all[i:min(i+9, len(all))]}))
	}
	evs = append(evs, update(protocol.TextEndEvent{ContentIndex: 0, Content: all}),
		assistantEndUsage("m", protocol.StopStop, 100, protocol.Usage{}, protocol.Text{Text: all}), &protocol.AgentEnd{}, &protocol.AgentSettled{})
	r.play(evs...)
	r.h.waitText("m · 0.1s")
	r.h.settle()
	text := strings.Join(r.h.text(), " ")
	require.Equal(t, 30, strings.Count(text, "alpha"), "every paragraph shows once:\n%s", strings.Join(r.h.text(), "\n"))
	require.Equal(t, 1, r.h.count("◆"))
	final := r.h.stop().(*Model)
	require.NoError(t, final.Err(), "no change hit a block that was already committed")
}
