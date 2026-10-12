package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// --- helpers that build agent events ---

const baseMs = int64(1_760_000_000_000) // fixed so that times are stable

func at(ms int64) protocol.Envelope { return protocol.Envelope{TS: baseMs + ms} }

func userEvents(text string, ms int64) []protocol.Event {
	m := protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: text}}, Timestamp: baseMs + ms}
	return []protocol.Event{&protocol.MessageStart{Envelope: at(ms), Message: m}, &protocol.MessageEnd{Envelope: at(ms), Message: m}}
}

func pendingAssistant(model string) protocol.AssistantMessage {
	return protocol.AssistantMessage{Content: []protocol.AssistantBlock{}, Provider: "anthropic", Model: model, StopReason: protocol.StopPending}
}

func assistantStart(model string, ms int64) protocol.Event {
	return &protocol.MessageStart{Envelope: at(ms), Message: pendingAssistant(model)}
}

func update(ev protocol.BlockEvent) protocol.Event {
	return &protocol.MessageUpdate{AssistantMessageEvent: ev}
}

// updateAt is an update event with a time.
func updateAt(ms int64, ev protocol.BlockEvent) protocol.Event {
	return &protocol.MessageUpdate{Envelope: at(ms), AssistantMessageEvent: ev}
}

func textParts(idx int, deltas ...string) []protocol.Event {
	evs := []protocol.Event{update(protocol.TextStartEvent{ContentIndex: idx})}
	var all string
	for _, d := range deltas {
		all += d
		evs = append(evs, update(protocol.TextDeltaEvent{ContentIndex: idx, Delta: d}))
	}
	return append(evs, update(protocol.TextEndEvent{ContentIndex: idx, Content: all}))
}

func thinkingParts(idx int, deltas ...string) []protocol.Event {
	evs := []protocol.Event{update(protocol.ThinkingStartEvent{ContentIndex: idx})}
	var all string
	for _, d := range deltas {
		all += d
		evs = append(evs, update(protocol.ThinkingDeltaEvent{ContentIndex: idx, Delta: d}))
	}
	return append(evs, update(protocol.ThinkingEndEvent{ContentIndex: idx, Content: all}))
}

func assistantEnd(model string, stop protocol.StopReason, ms int64, blocks ...protocol.AssistantBlock) protocol.Event {
	return assistantEndUsage(model, stop, ms, protocol.Usage{Input: 12_400, Output: 1_800, Cost: protocol.Cost{Total: 4_000}}, blocks...)
}

func assistantEndUsage(model string, stop protocol.StopReason, ms int64, usage protocol.Usage, blocks ...protocol.AssistantBlock) protocol.Event {
	m := protocol.AssistantMessage{Content: blocks, Provider: "anthropic", Model: model, StopReason: stop, Usage: usage}
	if stop == protocol.StopError {
		msg := "anthropic token expired and refresh failed."
		m.ErrorMessage = &msg
	}
	return &protocol.MessageEnd{Envelope: at(ms), Message: m}
}

func toolStart(id, name, args string, ms int64) protocol.Event {
	return &protocol.ToolExecutionStart{Envelope: at(ms), ToolCallID: id, ToolName: name, Args: json.RawMessage(args)}
}

func toolEnd(id, name string, isErr bool, text string, ms int64) protocol.Event {
	return &protocol.ToolExecutionEnd{
		Envelope: at(ms), ToolCallID: id, ToolName: name, IsError: isErr,
		Result: protocol.ToolExecutionResult{Content: []protocol.UserBlock{protocol.Text{Text: text}}},
	}
}

func flat(parts ...any) []protocol.Event {
	var out []protocol.Event
	for _, p := range parts {
		switch v := p.(type) {
		case protocol.Event:
			out = append(out, v)
		case []protocol.Event:
			out = append(out, v...)
		}
	}
	return out
}

// --- helpers that apply translator output to a real transcript ---

type tx struct {
	t   *testing.T
	tr  *Transcript
	rc  RenderContext
	trl *Translator
}

func newTx(t *testing.T) *tx {
	return &tx{t: t, tr: NewTranscript(), rc: RenderContext{Width: 80, Theme: NewTheme(0)}, trl: NewTranslator(TranslatorOptions{Location: time.UTC})}
}

func TestDetachFinishesLastObservedContent(t *testing.T) {
	x := newTx(t)
	x.feed(&protocol.AgentStart{}, assistantStart("faux-1", 0),
		update(protocol.TextStartEvent{ContentIndex: 0}),
		update(protocol.TextDeltaEvent{ContentIndex: 0, Delta: "partial reply"}),
		&protocol.ToolExecutionStart{ToolCallID: "t1", ToolName: "echo"})
	require.Less(t, x.tr.Frontier(), x.tr.Len())
	x.apply(x.trl.Detach())
	require.Equal(t, x.tr.Len(), x.tr.Frontier())
	text := ansi.Strip(strings.Join(finalRows(x.tr, x.rc), "\n"))
	require.Contains(t, text, "partial reply")
	require.Contains(t, text, "◆ echo")
	require.NotContains(t, text, "interrupted")
	require.NotContains(t, text, "Cancelled")
	x.feed(userEvents("next session prompt", 500)...)
	require.Equal(t, x.tr.Len(), x.tr.Frontier(), "the old tail must not block the next session")
}

// feed applies events the way the model does: one message at a time.
func (x *tx) feed(evs ...protocol.Event) {
	x.t.Helper()
	for _, ev := range evs {
		x.apply(x.trl.Event(ev))
	}
}

func (x *tx) apply(msgs []tea.Msg) {
	x.t.Helper()
	for _, msg := range msgs {
		switch m := msg.(type) {
		case AppendBlockMsg:
			require.NoError(x.t, x.tr.Append(m.ID, m.Block, m.Stable))
		case ReplaceBlockMsg:
			require.NoError(x.t, x.tr.Replace(m.ID, m.Block))
		case FinishBlockMsg:
			require.NoError(x.t, x.tr.Finish(m.ID))
		default:
			x.t.Fatalf("unexpected message %T", msg)
		}
	}
}

// rows are all rows so far, final and live, as plain text.
func (x *tx) rows() []string {
	all := allRows(x.tr, x.rc)
	out := make([]string, len(all))
	for i, r := range all {
		out[i] = strings.TrimRight(ansi.Strip(r), " ")
	}
	return out
}

// promptRow is the expected first row of a prompt band at the test width.
func promptRow(text, clock string) string {
	left := "  ❯ " + text
	if clock == "" {
		return left
	}
	return left + strings.Repeat(" ", 80-2-displayWidth(left)-displayWidth(clock)) + clock
}

const promptClock = "08:53" // baseMs in UTC

func TestTranslateReplyStream(t *testing.T) {
	x := newTx(t)
	x.feed(&protocol.AgentStart{})
	x.feed(userEvents("why does it fail?", 0)...)
	x.feed(assistantStart("claude-opus-5-5", 100))
	x.feed(update(protocol.TextStartEvent{ContentIndex: 0}), update(protocol.TextDeltaEvent{ContentIndex: 0, Delta: "The test waits "}), update(protocol.TextDeltaEvent{ContentIndex: 0, Delta: "for the stale socket."}))
	require.Equal(t, []string{
		promptRow("why does it fail?", promptClock),
		"",
		"  The test waits for the stale socket.",
	}, x.rows(), "the reply streams in a block that is not final")
	require.Equal(t, 1, x.tr.Frontier(), "only the prompt is final")

	x.feed(update(protocol.TextEndEvent{ContentIndex: 0, Content: "The test waits for the stale socket."}))
	require.Equal(t, 2, x.tr.Frontier(), "the reply is final at its end event")
	x.feed(assistantEnd("claude-opus-5-5", protocol.StopStop, 4300, protocol.Text{Text: "The test waits for the stale socket."}))
	x.feed(&protocol.AgentEnd{}, &protocol.AgentSettled{})
	require.Equal(t, []string{
		promptRow("why does it fail?", promptClock),
		"",
		"  The test waits for the stale socket.",
		"  claude-opus-5-5 · 4.2s · ↑12.4k ↓1.8k · $0.004",
		"",
	}, x.rows())
	require.Equal(t, x.tr.Len(), x.tr.Frontier(), "every block is final after the run settles")
}

// A long reply becomes final paragraph by paragraph. The rows are the same as
// the rows of one block that holds the whole text, and the final blocks never
// shrink.
func TestTranslateLongReplyLeavesTheTailParagraphByParagraph(t *testing.T) {
	paras := []string{
		"First paragraph with some words.",
		"- list item one\n- list item two\n\n  inside the second item",
		"```go\nfunc main() {\n\n\tprintln(1)\n}\n```",
		"| a | b |\n|---|---|\n| 1 | 2 |",
		"Last paragraph that stays open until the reply ends.",
	}
	full := strings.Join(paras, "\n\n")
	whole := plain(renderRows(NewAssistantTextBlock(full), x0().rc))

	x := newTx(t)
	x.feed(assistantStart("m", 0), update(protocol.TextStartEvent{ContentIndex: 0}))
	maxTail, lastFinal := 0, 0
	for i := 0; i < len(full); i += 5 {
		x.feed(update(protocol.TextDeltaEvent{ContentIndex: 0, Delta: full[i:min(i+5, len(full))]}))
		require.GreaterOrEqual(t, x.tr.Frontier(), lastFinal, "final blocks stay final")
		lastFinal = x.tr.Frontier()
		maxTail = max(maxTail, len(liveRows(x.tr, x.rc)))
	}
	require.Greater(t, x.tr.Frontier(), 2, "closed paragraphs were final while the reply streamed")
	require.Less(t, maxTail, len(whole)-3, "the live rows never held the whole reply")

	x.feed(update(protocol.TextEndEvent{ContentIndex: 0, Content: full}))
	require.Equal(t, whole, trimRight(x.rows()), "same rows as one block")
	require.Equal(t, x.tr.Len(), x.tr.Frontier())
}

func x0() *tx { return &tx{rc: RenderContext{Width: 80, Theme: NewTheme(0)}} }

// A thinking text is one block however long it is, so that the user folds it
// as a whole. It is final at its end event and then draws one row.
func TestTranslateThinkingIsOneBlockAndFoldsWhenItEnds(t *testing.T) {
	text := "First thought.\n\nSecond thought\nwith two lines.\n\nThird thought."
	x := newTx(t)
	x.feed(assistantStart("m", 0))
	x.feed(updateAt(1000, protocol.ThinkingStartEvent{ContentIndex: 0}))
	for i := 0; i < len(text); i += 4 {
		x.feed(updateAt(1000+int64(i), protocol.ThinkingDeltaEvent{ContentIndex: 0, Delta: text[i:min(i+4, len(text))]}))
	}
	require.Equal(t, 1, x.tr.Len(), "one block for the whole text")
	require.Equal(t, 0, x.tr.Frontier(), "the block is open while it streams")
	require.Equal(t, FoldTruncated, blockDefaultFold(x.tr.entries[0].block), "a streaming thinking is truncated")

	x.feed(updateAt(3500, protocol.ThinkingEndEvent{ContentIndex: 0, Content: text}))
	require.Equal(t, 1, x.tr.Len())
	require.Equal(t, 1, x.tr.Frontier())
	require.Equal(t, FoldCollapsed, blockDefaultFold(x.tr.entries[0].block), "it folds when it ends")
	require.Equal(t, []string{"  ◆ Thought for 2.5s"}, x.rows())

	x.rc.Fold = FoldExpanded
	require.Equal(t, []string{
		"  ◆ Thought for 2.5s",
		"  ┃ First thought.",
		"  ┃",
		"  ┃ Second thought",
		"  ┃ with two lines.",
		"  ┃",
		"  ┃ Third thought.",
	}, x.rows(), "paragraphs keep one empty row between them")
}

func TestTranslateSnapshotSplitsLongReplyIntoBlocks(t *testing.T) {
	full := "One.\n\n```go\ncode\n```\n\nTwo.\n\n- a\n- b\n"
	am := protocol.AssistantMessage{Content: []protocol.AssistantBlock{protocol.Text{Text: full}}, StopReason: protocol.StopStop}
	x := newTx(t)
	x.apply(x.trl.Snapshot(SnapshotMsg{Entries: []SnapshotEntry{{Kind: "message", Message: am}}}))
	require.Equal(t, plain(renderRows(NewAssistantTextBlock(full), x.rc)), trimRight(x.rows()), "same rows as one block")
	require.Equal(t, 4, x.tr.Len(), "one block for each chunk")
}

func TestTranslateReplyWithoutMessageStartOrUpdates(t *testing.T) {
	// A follower that joins late sees only the end of a message.
	x := newTx(t)
	x.feed(assistantEnd("m", protocol.StopStop, 0, protocol.Text{Text: "Only the end."}))
	require.Equal(t, []string{"  Only the end.", "  m · ↑12.4k ↓1.8k · $0.004", ""}, x.rows())
}

func TestTranslateUpdatesWithoutStartKeepWorking(t *testing.T) {
	x := newTx(t)
	x.feed(update(protocol.TextDeltaEvent{ContentIndex: 0, Delta: "late "}), update(protocol.TextDeltaEvent{ContentIndex: 0, Delta: "joiner"}))
	require.Equal(t, []string{"  late joiner"}, x.rows())
	x.feed(assistantEnd("m", protocol.StopStop, 0, protocol.Text{Text: "late joiner"}))
	require.Equal(t, 2, x.tr.Len(), "reply and meta line, no extra reply block")
}

func TestTranslateThinkingThenReply(t *testing.T) {
	x := newTx(t)
	x.feed(assistantStart("m", 0))
	x.feed(thinkingParts(0, "The stale check runs first.")...)
	x.feed(textParts(1, "Look at the probe.")...)
	x.feed(assistantEnd("m", protocol.StopStop, 1000,
		protocol.Thinking{Thinking: "The stale check runs first."}, protocol.Text{Text: "Look at the probe."}))
	require.Equal(t, []string{
		"  ◆ Thought",
		"",
		"  Look at the probe.",
		"  m · 1.0s · ↑12.4k ↓1.8k · $0.004",
		"",
	}, x.rows(), "an empty row separates the step from the reply")
}

func TestTranslateReplyAfterToolStartsWithAnEmptyRow(t *testing.T) {
	x := newTx(t)
	x.feed(userEvents("go", 0)...)
	x.feed(toolStart("c1", "bash", `{"command":"seq 1 40"}`, 0), toolEnd("c1", "bash", false, "1\n2", 100))
	x.feed(assistantStart("m", 200))
	x.feed(textParts(0, "Done.")...)
	rows := x.rows()
	require.Equal(t, []string{"  ◆ Ran seq 1 40", "", "  Done."}, rows[2:], "no output row in the collapsed form, one empty row before the reply")
	x.feed(userEvents("again", 300)...)
	x.feed(assistantStart("m", 400))
	x.feed(textParts(0, "Second.")...)
	rows = x.rows()
	require.Equal(t, []string{"  Done.", promptRow("again", promptClock), "", "  Second."}, rows[len(rows)-4:], "a reply right after a prompt has no extra empty row")
}

func TestTranslateToolLineRunningThenDone(t *testing.T) {
	x := newTx(t)
	x.feed(toolStart("c1", "echo", `{"text":"probe code request"}`, 0))
	running := x.rows()
	require.Len(t, running, 1)
	require.True(t, strings.HasPrefix(running[0], "  ◆ echo probe code request"), running[0])
	require.True(t, strings.HasSuffix(running[0], "⠋"), running[0])
	require.Equal(t, 80, displayWidth(ansi.Strip(liveRows(x.tr, x.rc)[0])), "the spinner sits at the right edge, one column in")

	x.feed(toolEnd("c1", "echo", false, "ok", 100))
	require.Equal(t, []string{"  ◆ echo probe code request"}, x.rows(), "a finished step is one row with nothing at the right")
	require.Equal(t, 1, x.tr.Frontier(), "a finished tool line is final")

	x.rc.Fold = FoldTruncated
	require.Equal(t, []string{"  ◆ echo probe code request", "    ok"}, x.rows(), "the truncated form shows the output")
}

func TestTranslateToolLineFailure(t *testing.T) {
	x := newTx(t)
	x.feed(toolStart("c1", "bash", `{"command":"false"}`, 0), toolEnd("c1", "bash", true, "exit status 1\nmore output", 2500))
	row := x.rows()[0]
	require.True(t, strings.HasPrefix(row, "  ◆ Ran false"), row)
	require.True(t, strings.HasSuffix(row, "✗ exit status 1"), row)
}

func TestTranslateToolEndWithoutStart(t *testing.T) {
	x := newTx(t)
	x.feed(toolEnd("c9", "read", false, "x", 0))
	require.Equal(t, []string{"  ◆ Read"}, x.rows(), "the call is unknown, so the row has the verb only")
	x.rc.Fold = FoldTruncated
	require.Equal(t, []string{"  ◆ Read", "    x"}, x.rows())
}

func TestTranslateParallelToolsFinishInAnyOrder(t *testing.T) {
	x := newTx(t)
	x.feed(toolStart("a", "one", `{}`, 0), toolStart("b", "two", `{}`, 0))
	x.feed(toolEnd("b", "two", false, "", 100))
	require.Zero(t, x.tr.Frontier(), "the later tool cannot pass the running one")
	x.feed(toolEnd("a", "one", false, "", 200))
	require.Equal(t, 2, x.tr.Frontier())
	rows := x.rows()
	require.True(t, strings.Contains(rows[0], "one") && strings.Contains(rows[1], "two"), "%q", rows)
}

func TestTranslateRetryNote(t *testing.T) {
	x := newTx(t)
	x.feed(&protocol.AutoRetryStart{Attempt: 1, MaxAttempts: 3, DelayMs: 4000, ErrorMessage: "rate limit (HTTP 429)"})
	require.Equal(t, []string{"  ↻ Attempt 1 failed: rate limit (HTTP 429). Retry 1 of 3 scheduled.", ""}, x.rows())
}

func TestTranslateErrorBlockFromCycleEnd(t *testing.T) {
	x := newTx(t)
	x.feed(&protocol.AgentStart{})
	x.feed(userEvents("show me the probe code", 0)...)
	x.feed(assistantStart("m", 0))
	x.feed(textParts(0, "The probe lives in client.go. It dials the socket, then sends an initialize frame and waits.")...)
	x.feed(assistantEnd("m", protocol.StopError, 500, protocol.Text{Text: "The probe lives in client.go. It dials the socket, then sends an initialize frame and waits."}))
	x.feed(&protocol.CycleEnd{Reason: "error", Code: "AUTH"}, &protocol.AgentEnd{}, &protocol.AgentSettled{})
	rows := x.rows()
	require.Equal(t, []string{
		"  ✗ Error (auth): anthropic token expired and refresh failed.",
		"    Run in a shell: ask auth login --provider anthropic. Then send again.",
		"",
	}, rows[len(rows)-3:])
	require.Equal(t, "", rows[len(rows)-4], "a blank row separates the partial reply from the error")
	require.Equal(t, 1, strings.Count(strings.Join(rows, "\n"), "Error ("), "one error block")
}

func TestTranslateErrorAfterRetriesShowsOnce(t *testing.T) {
	x := newTx(t)
	x.feed(assistantStart("m", 0), assistantEnd("m", protocol.StopError, 0))
	x.feed(&protocol.AutoRetryEnd{Success: false, Attempt: 3, FinalError: "rate limit (HTTP 429) after 3 retries"})
	x.feed(&protocol.CycleEnd{Reason: "error", Code: "RATE_LIMIT"}, &protocol.AgentSettled{})
	joined := strings.Join(x.rows(), "\n")
	require.Equal(t, 1, strings.Count(joined, "✗ Error (rate limit)"), joined)
	require.Contains(t, joined, "rate limit (HTTP 429) after 3 retries.")
	require.Contains(t, joined, "Wait a moment, then send again.")
}

func TestTranslateFailedRetryWithoutCycleEndStillShowsAnError(t *testing.T) {
	x := newTx(t)
	x.feed(&protocol.AutoRetryEnd{Success: false, FinalError: "gave up"})
	require.NotContains(t, strings.Join(x.rows(), "\n"), "Error", "the error waits for the cycle end or the settle")
	x.feed(&protocol.AgentSettled{})
	require.Contains(t, strings.Join(x.rows(), "\n"), "✗ Error (error): gave up.")
}

func TestTranslateCycleEndReasons(t *testing.T) {
	for reason, want := range map[string]string{
		"blocked":            "✗ Error (blocked): ",
		"max-tokens":         "✗ Error (max-tokens): ",
		"continuation-limit": "✗ Error (continuation-limit): ",
	} {
		x := newTx(t)
		x.feed(&protocol.CycleEnd{Reason: reason}, &protocol.AgentSettled{})
		require.Contains(t, x.rows()[0], want, reason)
	}
	x := newTx(t)
	x.feed(&protocol.CycleEnd{Reason: "completed"}, &protocol.AgentSettled{})
	require.Empty(t, x.rows(), "a completed cycle adds nothing")
}

func TestTranslateCancelMarkerAtSettleOnly(t *testing.T) {
	x := newTx(t)
	x.feed(&protocol.AgentStart{})
	x.feed(userEvents("show me the probe code", 0)...)
	x.feed(assistantStart("m", 0))
	x.feed(update(protocol.TextStartEvent{ContentIndex: 0}), update(protocol.TextDeltaEvent{ContentIndex: 0, Delta: "The probe lives in client.go. It dials the socket, then"}))

	// The user presses esc: the client sends a cancel request. No event comes
	// from the key press itself.
	require.NotContains(t, strings.Join(x.rows(), "\n"), "Cancelled")

	x.feed(assistantEnd("m", protocol.StopAborted, 900, protocol.Text{Text: "The probe lives in client.go. It dials the socket, then"}))
	x.feed(&protocol.CycleEnd{Reason: "aborted", Cause: "user"}, &protocol.AgentEnd{})
	require.NotContains(t, strings.Join(x.rows(), "\n"), "Cancelled", "the run is not settled yet")

	x.feed(&protocol.AgentSettled{})
	rows := x.rows()
	require.Equal(t, []string{
		"  The probe lives in client.go. It dials the socket, then",
		"  ⊘ Cancelled by you. Partial reply kept above.",
		"",
	}, rows[len(rows)-3:])
	require.Equal(t, x.tr.Len(), x.tr.Frontier())

	// The next run starts clean: no second marker.
	x.feed(&protocol.AgentStart{}, &protocol.AgentSettled{})
	require.Equal(t, 1, strings.Count(strings.Join(x.rows(), "\n"), "Cancelled"))
}

func TestTranslateCancelWithoutPartialReply(t *testing.T) {
	x := newTx(t)
	x.feed(&protocol.CycleEnd{Reason: "aborted", Cause: "user"}, &protocol.AgentSettled{})
	require.Equal(t, []string{"  ⊘ Cancelled by you.", ""}, x.rows())
}

func TestTranslateAbortCauses(t *testing.T) {
	for cause, want := range map[string]string{
		"deadline": "⊘ Stopped: the run reached its time limit.",
		"output":   "⊘ Stopped: the output limit was reached.",
		"disposed": "⊘ Stopped: the session was closed.",
		"canceled": "⊘ Cancelled.",
		"":         "⊘ Cancelled.",
	} {
		x := newTx(t)
		x.feed(&protocol.CycleEnd{Reason: "aborted", Cause: cause}, &protocol.AgentSettled{})
		require.Contains(t, x.rows()[0], want, "cause %q", cause)
	}
}

func TestTranslateSettleClosesWhatIsStillOpen(t *testing.T) {
	x := newTx(t)
	x.feed(assistantStart("m", 0))
	x.feed(update(protocol.TextDeltaEvent{ContentIndex: 0, Delta: "half"}))
	x.feed(toolStart("c1", "bash", `{"command":"sleep 99"}`, 0))
	require.Zero(t, x.tr.Frontier())
	x.feed(&protocol.AgentSettled{})
	require.Equal(t, x.tr.Len(), x.tr.Frontier(), "an open block would hold the frontier forever")
	rows := strings.Join(x.rows(), "\n")
	require.Contains(t, rows, "  half\n")
	require.Contains(t, rows, "✗ interrupted")
}

func TestTranslateUserMessagesShowAsPromptBands(t *testing.T) {
	x := newTx(t)
	m := protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "line one"}, protocol.Image{Data: "x", MimeType: "image/png"}, protocol.Text{Text: "line two"}}, Timestamp: 0}
	x.feed(&protocol.MessageStart{Message: m}, &protocol.MessageEnd{Message: &m})
	require.Equal(t, []string{"  ❯ line one", "    [image]", "    line two", ""}, x.rows(), "a pointer message works; no time shows without a timestamp")
}

func TestTranslateIgnoresEventsWithoutBlocks(t *testing.T) {
	x := newTx(t)
	x.feed(
		&protocol.CycleStart{CycleID: "c"}, &protocol.TurnStart{}, &protocol.AttemptStart{}, &protocol.AttemptEnd{},
		&protocol.QueueUpdate{Steering: []string{"s"}}, &protocol.TurnEnd{}, &protocol.AgentEnd{}, &protocol.AgentDisposed{},
		&protocol.RawEvent{Type: "session_info"},
		&protocol.ToolExecutionUpdate{ToolCallID: "x"},
		&protocol.MessageStart{Message: protocol.SystemMessage{}}, &protocol.MessageEnd{Message: protocol.ToolResultMessage{}},
		update(protocol.ToolCallStartEvent{ContentIndex: 0, ID: "c", ToolName: "t"}), update(protocol.ToolCallDeltaEvent{ContentIndex: 0, Delta: "{}"}),
		update(protocol.ToolCallEndEvent{ContentIndex: 0, ToolCall: protocol.ToolCall{ID: "c", Name: "t"}}),
	)
	require.Zero(t, x.tr.Len())
	require.Nil(t, x.trl.Event(nil))
}

func TestTranslateMidMessageSnapshotThenLiveEvents(t *testing.T) {
	x := newTx(t)
	partial := protocol.AssistantMessage{
		Content:    []protocol.AssistantBlock{protocol.Thinking{Thinking: "done thinking"}, protocol.Text{Text: "partial rep"}},
		Model:      "m",
		StopReason: protocol.StopPending,
	}
	x.apply(x.trl.Snapshot(SnapshotMsg{Stream: &protocol.ACPStreamBaseline{Message: partial, Open: map[int][]byte{1: []byte("partial rep")}}}))
	require.Equal(t, []string{"  ◆ Thought", "", "  partial rep"}, x.rows())
	require.Equal(t, 1, x.tr.Frontier(), "the closed thinking block is final; the open reply is live")

	x.feed(update(protocol.TextDeltaEvent{ContentIndex: 1, Delta: "ly"}))
	require.Equal(t, "  partial reply", x.rows()[2])
	x.feed(update(protocol.TextEndEvent{ContentIndex: 1, Content: "partial reply"}), assistantEnd("m", protocol.StopStop, 0, protocol.Thinking{Thinking: "done thinking"}, protocol.Text{Text: "partial reply"}))
	require.Equal(t, "  partial reply", x.rows()[2])
	require.Equal(t, x.tr.Len(), x.tr.Frontier())
	require.Equal(t, 1, strings.Count(strings.Join(x.rows(), "\n"), "◆"))
}

func TestTranslateSnapshotHistory(t *testing.T) {
	x := newTx(t)
	user := protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "run echo"}}, Timestamp: baseMs}
	call := protocol.ToolCall{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{"text":"hi"}`)}
	assistant := protocol.AssistantMessage{
		Content:    []protocol.AssistantBlock{protocol.Thinking{Thinking: "plan"}, protocol.Text{Text: "Running it."}, call},
		Model:      "m",
		StopReason: protocol.StopToolUse,
		Usage:      protocol.Usage{Input: 1000, Output: 50, Cost: protocol.Cost{Total: 1600}},
	}
	result := protocol.ToolResultMessage{ToolCallID: "c1", ToolName: "echo", Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}}
	final := protocol.AssistantMessage{Content: []protocol.AssistantBlock{protocol.Text{Text: "Done."}}, Model: "m", StopReason: protocol.StopStop}
	x.apply(x.trl.Snapshot(SnapshotMsg{Entries: []SnapshotEntry{
		{Kind: "message", Message: user},
		{Kind: "message", Message: assistant},
		{Kind: "message", Message: result},
		{Kind: "message", Message: final},
		{Kind: "input_outcome"},
		{Kind: "message"}, // a row that could not be read
	}}))
	rows := x.rows()
	require.Equal(t, promptRow("run echo", promptClock), rows[0])
	joined := strings.Join(rows, "\n")
	for _, want := range []string{"◆ Thought", "  Running it.", "m · ↑1.0k ↓50 · $0.002", "◆ echo hi", "  Done."} {
		require.Contains(t, joined, want)
	}
	require.Equal(t, x.tr.Len(), x.tr.Frontier(), "history is final")
}

func TestTranslateSnapshotUnresolvedToolCall(t *testing.T) {
	x := newTx(t)
	call := protocol.ToolCall{ID: "c1", Name: "bash", Arguments: json.RawMessage(`{"command":"ls"}`)}
	m := protocol.AssistantMessage{Content: []protocol.AssistantBlock{call}, StopReason: protocol.StopToolUse}
	x.apply(x.trl.Snapshot(SnapshotMsg{Entries: []SnapshotEntry{{Kind: "message", Message: m}}}))
	require.Contains(t, strings.Join(x.rows(), "\n"), "✗ no result")
}

func TestTranslateSnapshotErrorsAndAborts(t *testing.T) {
	x := newTx(t)
	msg := "boom happened"
	x.apply(x.trl.Snapshot(SnapshotMsg{Entries: []SnapshotEntry{
		{Kind: "message", Message: protocol.AssistantMessage{StopReason: protocol.StopError, ErrorMessage: &msg}},
		{Kind: "message", Message: protocol.AssistantMessage{StopReason: protocol.StopAborted}},
	}}))
	joined := strings.Join(x.rows(), "\n")
	require.Contains(t, joined, "✗ Error (error): boom happened.")
	require.Contains(t, joined, "⊘ Cancelled.")
}

func TestTranslatorBlockIDsAreUniqueAcrossSessions(t *testing.T) {
	x := newTx(t)
	for i := 0; i < 3; i++ {
		x.trl.Reset()
		x.feed(toolStart("same-call-id", "echo", `{}`, 0), toolEnd("same-call-id", "echo", false, "", 10))
	}
	require.Equal(t, 3, x.tr.Len(), "a tool call id that repeats in a new session still gets a new block")
}

func TestThinkingNotice(t *testing.T) {
	require.Equal(t, "Thinking blocks: expanded · ctrl+t to collapse", ThinkingNotice(true))
	require.Equal(t, "Thinking blocks: collapsed · ctrl+t to expand", ThinkingNotice(false))
}

// A reply whose reasoning summary is empty gets no thinking block: there is
// nothing to show and the short form would be a row without meaning.
func TestTranslateEmptyThinkingMakesNoBlock(t *testing.T) {
	x := newTx(t)
	x.feed(assistantStart("m", 0))
	x.feed(thinkingParts(0)...)
	require.Empty(t, x.rows(), "an empty thinking part draws nothing while it streams")
	x.feed(textParts(1, "ok")...)
	x.feed(assistantEnd("m", protocol.StopStop, 1000, protocol.Thinking{Thinking: ""}, protocol.Text{Text: "ok"}))
	require.Equal(t, []string{
		"  ok",
		"  m · 1.0s · ↑12.4k ↓1.8k · $0.004",
		"",
	}, x.rows())
	require.NotContains(t, strings.Join(plain(finalRows(x.tr, x.rc)), "\n"), "Thought", "no thinking row for an empty summary")
}

func TestTranslateEmptyThinkingOnlyInEndEvent(t *testing.T) {
	x := newTx(t)
	x.feed(assistantStart("m", 0))
	x.feed(assistantEnd("m", protocol.StopStop, 1000, protocol.Thinking{Thinking: " \n"}, protocol.Text{Text: "ok"}))
	require.Equal(t, []string{"  ok", "  m · 1.0s · ↑12.4k ↓1.8k · $0.004", ""}, x.rows())
}

// A block that ends with an empty row already separates the reply from the
// step before it, so the reply adds no second empty row.
func TestTranslateReplyAfterANoteHasOneEmptyRow(t *testing.T) {
	x := newTx(t)
	x.feed(toolStart("c1", "bash", `{"command":"ls"}`, 0), toolEnd("c1", "bash", false, "", 100))
	x.feed(&protocol.AutoRetryStart{Attempt: 1, MaxAttempts: 3, DelayMs: 1000, ErrorMessage: "rate limit"})
	x.feed(assistantStart("m", 200))
	x.feed(textParts(0, "Back.")...)
	rows := x.rows()
	require.Equal(t, "  ◆ Ran ls", rows[0])
	require.Equal(t, []string{"", "  Back."}, rows[len(rows)-2:])
	require.NotEqual(t, "", rows[len(rows)-3], "no second empty row")
}
