package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"AskCore/pkg/protocol"
)

// clipRun is one call of the native clipboard command runner.
type clipRun struct {
	name  string
	args  []string
	input string
}

// fakeClipboard stands in for the machine: the native command runner, the
// terminal output and the environment. It runs no process and writes no byte
// to the real terminal.
type fakeClipboard struct {
	mu   sync.Mutex
	runs []clipRun
	// fail makes the named commands fail. "*" fails every command.
	fail map[string]error
	// block makes a command wait for its context.
	block bool
	// sent holds the texts that went to the terminal clipboard.
	sent []string
	env  map[string]string
	goos string
}

func newFakeClipboard(goos string, env map[string]string) *fakeClipboard {
	return &fakeClipboard{goos: goos, env: env, fail: map[string]error{}}
}

func (f *fakeClipboard) options(timeout time.Duration) ClipboardOptions {
	return ClipboardOptions{
		Run: func(ctx context.Context, name string, args []string, input string) error {
			f.mu.Lock()
			f.runs = append(f.runs, clipRun{name: name, args: args, input: input})
			err, ok := f.fail[name]
			if !ok {
				err = f.fail["*"]
			}
			block := f.block
			f.mu.Unlock()
			if block {
				<-ctx.Done()
				return ctx.Err()
			}
			return err
		},
		Terminal: func(text string) tea.Cmd {
			f.mu.Lock()
			f.sent = append(f.sent, text)
			f.mu.Unlock()
			return nil
		},
		Getenv:  func(k string) string { return f.env[k] },
		GOOS:    f.goos,
		Timeout: timeout,
	}
}

func (f *fakeClipboard) calls() []clipRun {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]clipRun(nil), f.runs...)
}

func (f *fakeClipboard) terminalTexts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

// startCopyRig starts the session view with the fake machine.
func startCopyRig(t *testing.T, width, height int, f *fakeClipboard) *viewRig {
	t.Helper()
	return startView(t, width, height, withOptions(func(o *Options) { o.Clipboard = f.options(0) }))
}

func (r *viewRig) runCopy() {
	r.t.Helper()
	r.typeText("/copy")
	r.key("enter")
}

// replyEvents is one finished assistant message with the given blocks.
func replyEvents(ms int64, blocks ...protocol.AssistantBlock) []protocol.Event {
	return flat(
		&protocol.AgentStart{},
		assistantStart("deepseek-v4.1-flash", ms),
		assistantEnd("deepseek-v4.1-flash", protocol.StopStop, ms+10, blocks...),
		&protocol.AgentEnd{}, &protocol.AgentSettled{},
	)
}

func TestCopyNativeCommandShowsCopiedNotice(t *testing.T) {
	f := newFakeClipboard("darwin", nil)
	r := startCopyRig(t, 80, 24, f)
	r.firstTurn()

	r.runCopy()
	r.h.waitText("┊ ✓ Copied")

	require.Equal(t, []clipRun{{name: "pbcopy", input: sketchReply}}, f.calls())
	require.Empty(t, f.terminalTexts(), "a copy through a native command sends nothing to the terminal")
}

func TestCopyOverSSHSendsOneOSC52Sequence(t *testing.T) {
	f := newFakeClipboard("darwin", map[string]string{"SSH_TTY": "/dev/pts/3"})
	r := startCopyRig(t, 80, 24, f)
	r.firstTurn()

	r.runCopy()
	r.h.waitText("┊ Sent to the terminal clipboard")

	require.Empty(t, f.calls(), "a remote session has no useful native clipboard")
	require.Equal(t, []string{sketchReply}, f.terminalTexts(), "the text goes to the terminal once")
	require.Zero(t, r.h.count("✓ Copied"), "the terminal gives no answer, so the notice must not say Copied")
}

func TestCopyNativeFailureFallsBackToTerminalClipboard(t *testing.T) {
	f := newFakeClipboard("linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"})
	f.fail["wl-copy"] = errors.New("exit status 1")
	f.fail["xclip"] = errors.New("exit status 1")
	f.fail["xsel"] = errors.New("exit status 1")
	r := startCopyRig(t, 80, 24, f)
	r.firstTurn()

	r.runCopy()
	r.h.waitText("┊ Sent to the terminal clipboard")

	var names []string
	for _, c := range f.calls() {
		names = append(names, c.name)
	}
	require.Equal(t, []string{"wl-copy", "xclip", "xsel"}, names, "native commands run first, in order")
	require.Len(t, f.terminalTexts(), 1)
}

func TestCopyStopsAtFirstNativeCommandThatWorks(t *testing.T) {
	f := newFakeClipboard("linux", map[string]string{"DISPLAY": ":0"})
	f.fail["xclip"] = errors.New("exit status 1")
	r := startCopyRig(t, 80, 24, f)
	r.firstTurn()

	r.runCopy()
	r.h.waitText("┊ ✓ Copied")

	calls := f.calls()
	require.Len(t, calls, 2)
	require.Equal(t, "xclip", calls[0].name)
	require.Equal(t, []string{"-selection", "clipboard"}, calls[0].args)
	require.Equal(t, "xsel", calls[1].name)
	require.Equal(t, []string{"--clipboard", "--input"}, calls[1].args)
	require.Empty(t, f.terminalTexts())
}

func TestCopyWithoutReplyShowsNothingToCopy(t *testing.T) {
	f := newFakeClipboard("darwin", nil)
	r := startCopyRig(t, 80, 24, f)

	r.runCopy()
	r.h.waitText("┊ Nothing to copy")

	require.Empty(t, f.calls())
	require.Empty(t, f.terminalTexts())
}

func TestCopyOversizedReplyIsRefusedWithoutOSC52(t *testing.T) {
	f := newFakeClipboard("plan9", nil)
	r := startCopyRig(t, 80, 24, f)
	para := strings.Repeat("word ", 19) + "end"
	var b strings.Builder
	for b.Len() < 180*1024 {
		b.WriteString(para + "\n\n")
	}
	big := strings.TrimSpace(b.String())[:180*1024]
	r.event(replyEvents(0, protocol.Text{Text: big})...)

	r.runCopy()
	r.h.waitText("┊ Not copied: 180 KB is over the terminal clipboard limit of 75 KB")

	require.Empty(t, f.terminalTexts())
	m := r.stopped()
	require.True(t, m.sv.notice.err, "the refusal uses the error role")
}

func TestCopyNativeFailureAndOversizedReplyIsRefused(t *testing.T) {
	f := newFakeClipboard("darwin", nil)
	f.fail["pbcopy"] = errors.New("exit status 1")
	r := startCopyRig(t, 80, 24, f)
	big := strings.Repeat("0123456789", 12*1024) // 120 KB
	r.event(replyEvents(0, protocol.Text{Text: big})...)

	r.runCopy()
	r.h.waitText("┊ Not copied: 120 KB is over the terminal clipboard limit of 75 KB")

	require.Len(t, f.calls(), 1, "the native command ran first")
	require.Empty(t, f.terminalTexts())
}

func TestCopyNoticeIsCutAtNarrowWidth(t *testing.T) {
	f := newFakeClipboard("plan9", nil)
	r := startCopyRig(t, 40, 14, f)
	r.event(replyEvents(0, protocol.Text{Text: strings.Repeat("a", 180*1024)})...)

	r.runCopy()
	r.h.waitText("┊ Not copied: 180 KB is over the termin…")

	require.Zero(t, r.h.count("clipboard limit of 75 KB"))
}

func TestCopyUsesFinishedReplyAndSkipsThinkingAndToolOutput(t *testing.T) {
	f := newFakeClipboard("darwin", nil)
	r := startCopyRig(t, 80, 24, f)
	r.event(replyEvents(0, protocol.Text{Text: "older reply"})...)
	r.event(flat(
		&protocol.AgentStart{},
		assistantStart("deepseek-v4.1-flash", 100),
		assistantEnd("deepseek-v4.1-flash", protocol.StopStop, 110,
			protocol.Thinking{Thinking: "private reasoning"},
			protocol.Text{Text: "# Heading\n\nfirst **part**"},
			protocol.ToolCall{ID: "c1", Name: "bash", Arguments: []byte(`{"command":"echo hi"}`)},
			protocol.Text{Text: "second part"},
		),
		toolStart("c1", "bash", `{"command":"echo hi"}`, 120),
		toolEnd("c1", "bash", false, "TOOLOUTPUT", 130),
		&protocol.AgentEnd{}, &protocol.AgentSettled{},
	)...)
	// A reply in progress is not used.
	r.event(flat(
		&protocol.AgentStart{},
		assistantStart("deepseek-v4.1-flash", 200),
		textParts(0, "streaming reply"),
	)...)

	r.runCopy()
	r.h.waitText("┊ ✓ Copied")

	calls := f.calls()
	require.Len(t, calls, 1)
	require.Equal(t, "# Heading\n\nfirst **part**\n\nsecond part", calls[0].input)
	require.NotContains(t, calls[0].input, "private reasoning")
	require.NotContains(t, calls[0].input, "TOOLOUTPUT")
}

func TestCopyUsesReplyOfRestoredHistory(t *testing.T) {
	f := newFakeClipboard("darwin", nil)
	fc := newFakeClient()
	r := startView(t, 80, 24, withFake(fc), withOptions(func(o *Options) { o.Clipboard = f.options(0) }))
	r.fc.push(SnapshotMsg{SessionID: fc.sessionID, Entries: []SnapshotEntry{
		{Kind: "message", Message: protocol.UserMessage{Content: []protocol.UserBlock{protocol.Text{Text: "hi"}}}},
		{Kind: "message", Message: protocol.AssistantMessage{
			Content:    []protocol.AssistantBlock{protocol.Thinking{Thinking: "hmm"}, protocol.Text{Text: "restored reply"}},
			StopReason: protocol.StopStop,
		}},
	}, Resync: true})
	r.h.waitText("restored reply")

	r.runCopy()
	r.h.waitText("┊ ✓ Copied")

	require.Equal(t, []clipRun{{name: "pbcopy", input: "restored reply"}}, f.calls())
}

func TestCopyCommandStopsAfterTimeout(t *testing.T) {
	f := newFakeClipboard("darwin", nil)
	f.block = true
	r := startView(t, 80, 24, withOptions(func(o *Options) { o.Clipboard = f.options(30 * time.Millisecond) }))
	r.firstTurn()

	r.runCopy()
	r.h.waitText("┊ Sent to the terminal clipboard")

	require.Len(t, f.calls(), 1)
}

func TestCopyIsListedInCompletion(t *testing.T) {
	r := startView(t, 80, 24)
	r.typeText("/co")
	r.h.waitText("/copy  Copy the last reply")
}
