package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"AskCore/pkg/protocol"
)

// The /copy command.
//
// /copy puts the markdown text of the last finished assistant message on the
// clipboard. The order of the methods follows Pi: a native clipboard command
// first, then OSC 52 through the terminal. A remote session skips the native
// commands, because they would fill the clipboard of the remote machine. The
// terminal gives no answer to OSC 52, so that route never reports "Copied".

const (
	// osc52Limit is the largest text that /copy sends through OSC 52. Many
	// terminals drop a longer sequence without any message.
	osc52Limit = 75 * 1024
	// clipboardTimeout bounds one native command.
	clipboardTimeout = 5 * time.Second
	// clipboardWaitDelay bounds the wait for the pipes of a command that was
	// stopped. A clipboard tool can leave a child that holds them open.
	clipboardWaitDelay = 500 * time.Millisecond
)

// ClipboardOptions are the machine parts that /copy uses. Every zero field has
// a default for the real machine; tests set all of them.
type ClipboardOptions struct {
	// Run runs one native command with the text on its standard input.
	Run func(ctx context.Context, name string, args []string, input string) error
	// Terminal returns the command that sends the text to the terminal
	// clipboard (OSC 52). The renderer writes it, so it cannot cut into a
	// frame. Nil means tea.SetClipboard.
	Terminal func(text string) tea.Cmd
	// Getenv reads one environment variable. Nil means os.Getenv.
	Getenv func(string) string
	// GOOS is the operating system. Empty means runtime.GOOS.
	GOOS string
	// Timeout bounds one native command. Zero means 5 s.
	Timeout time.Duration
}

// resolved returns the options with the defaults filled in.
func (o ClipboardOptions) resolved() ClipboardOptions {
	if o.Run == nil {
		o.Run = runClipboardCommand
	}
	if o.Terminal == nil {
		o.Terminal = tea.SetClipboard
	}
	if o.Getenv == nil {
		o.Getenv = os.Getenv
	}
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.Timeout <= 0 {
		o.Timeout = clipboardTimeout
	}
	return o
}

// runClipboardCommand runs a command with the text on its standard input. The
// command output is dropped.
func runClipboardCommand(ctx context.Context, name string, args []string, input string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(input)
	cmd.WaitDelay = clipboardWaitDelay
	return cmd.Run()
}

type clipboardCommand struct {
	name string
	args []string
}

// nativeCommands lists the commands that can work in this environment, in the
// order they are tried.
func (o ClipboardOptions) nativeCommands() []clipboardCommand {
	switch o.GOOS {
	case "darwin":
		return []clipboardCommand{{name: "pbcopy"}}
	case "linux":
		var out []clipboardCommand
		if o.Getenv("TERMUX_VERSION") != "" {
			out = append(out, clipboardCommand{name: "termux-clipboard-set"})
		}
		if o.Getenv("WAYLAND_DISPLAY") != "" {
			out = append(out, clipboardCommand{name: "wl-copy"})
		}
		if o.Getenv("DISPLAY") != "" {
			out = append(out,
				clipboardCommand{name: "xclip", args: []string{"-selection", "clipboard"}},
				clipboardCommand{name: "xsel", args: []string{"--clipboard", "--input"}},
			)
		}
		return out
	}
	return nil
}

// remote tells whether the session runs over SSH or mosh.
func (o ClipboardOptions) remote() bool {
	for _, k := range []string{"SSH_TTY", "SSH_CONNECTION", "SSH_CLIENT", "MOSH_CONNECTION"} {
		if o.Getenv(k) != "" {
			return true
		}
	}
	return false
}

// copyMethod is the route that a copy took.
type copyMethod int

const (
	copyNative copyMethod = iota
	copyTerminal
)

// clipboardDoneMsg is the result of one /copy. Seq is the request number; only
// the latest request changes the notice.
type clipboardDoneMsg struct {
	seq    uint64
	method copyMethod
	err    error
	// text is the text to send to the terminal clipboard when the method is
	// copyTerminal and err is nil.
	text string
}

// copyText copies the text through a native command and reports how. When the
// text must go to the terminal, it only checks the limit; the model sends it
// from its update loop, so the renderer writes it. It runs in a command.
func (o ClipboardOptions) copyText(text string) (copyMethod, error) {
	if !o.remote() {
		for _, c := range o.nativeCommands() {
			ctx, cancel := context.WithTimeout(context.Background(), o.Timeout)
			err := o.Run(ctx, c.name, c.args, text)
			cancel()
			if err == nil {
				return copyNative, nil
			}
		}
	}
	if len(text) > osc52Limit {
		return copyTerminal, fmt.Errorf("%s is over the terminal clipboard limit of %s",
			formatSize(int64(len(text))), formatSize(osc52Limit))
	}
	return copyTerminal, nil
}

// copyReply starts /copy.
func (s *sessionView) copyReply() tea.Cmd {
	text := s.trl.LastReply()
	if text == "" {
		s.setNotice("Nothing to copy", false)
		return nil
	}
	return s.copyTextCmd(text)
}

// copyTextCmd starts the copy of a text. /copy and a mouse selection share it,
// so both use the same clipboard order and the same notices.
func (s *sessionView) copyTextCmd(text string) tea.Cmd {
	s.copySeq++
	seq, opts := s.copySeq, s.clip
	return func() tea.Msg {
		method, err := opts.copyText(text)
		return clipboardDoneMsg{seq: seq, method: method, err: err, text: text}
	}
}

// onCopied shows the result of /copy. It returns the command that sends the
// text to the terminal clipboard when the copy took that route.
func (s *sessionView) onCopied(msg clipboardDoneMsg) tea.Cmd {
	switch {
	case msg.seq != s.copySeq:
	case msg.err != nil:
		s.setNotice("Not copied: "+msg.err.Error(), true)
	case msg.method == copyNative:
		s.setResult("Copied")
	default:
		s.setNotice("Sent to the terminal clipboard", false)
		return s.clip.Terminal(msg.text)
	}
	return nil
}

// replyCopyText is the text that /copy takes from an assistant message: the
// text blocks, in order, joined with one empty line. Thinking and tool calls
// are not part of it.
func replyCopyText(am protocol.AssistantMessage) string {
	var parts []string
	for _, blk := range am.Content {
		if t, ok := blk.(protocol.Text); ok && strings.TrimSpace(t.Text) != "" {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// noteReply remembers the text of a finished assistant message for /copy. A
// message with no text keeps the earlier reply.
func (t *Translator) noteReply(am protocol.AssistantMessage) {
	if text := replyCopyText(am); text != "" {
		t.lastReply = text
	}
}

// LastReply returns the raw markdown text of the last finished assistant
// message that had text, or "". A reply that still streams is not part of it.
func (t *Translator) LastReply() string { return t.lastReply }
