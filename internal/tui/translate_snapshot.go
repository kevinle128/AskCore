package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"AskCore/pkg/protocol"
)

// Snapshot translates the history of a session into final blocks, in order,
// and opens the message that streamed at the cut. A SnapshotMsg arrives after
// a new follow and after a resync. A snapshot with Resync set replaces what
// the view holds; the session view decides how to show that (for example a
// divider or a new transcript).
//
// A tool call that has no result in the snapshot shows "✗ no result": the
// snapshot cannot tell a tool that is still running from one that never
// ended, and a block that stays open would hold the frontier.
func (t *Translator) Snapshot(s SnapshotMsg) []tea.Msg {
	var out []tea.Msg
	results := map[string]protocol.ToolResultMessage{}
	for _, e := range s.Entries {
		if e.Kind == "message" {
			if tr, ok := toolResultOf(e.Message); ok {
				results[tr.ToolCallID] = tr
			}
		}
	}
	for _, e := range s.Entries {
		if e.Kind != "message" || e.Message == nil {
			continue
		}
		if um, ok := userOf(e.Message); ok {
			t.userPrompt(um, &out)
		} else if am, ok := assistantOf(e.Message); ok {
			t.historyAssistant(am, results, &out)
		}
	}
	if s.Stream != nil {
		t.resumeStream(s.Stream, &out)
	}
	return out
}

func (t *Translator) historyAssistant(am protocol.AssistantMessage, results map[string]protocol.ToolResultMessage, out *[]tea.Msg) {
	t.noteReply(am)
	visible, lastIsText := false, false
	for _, blk := range am.Content {
		switch b := blk.(type) {
		case protocol.Text:
			if oneLine(b.Text) == "" {
				continue
			}
			t.historyText(b.Text, out)
			visible, lastIsText = true, true
		case protocol.Thinking:
			t.historyThinking(b.Thinking, out)
			visible, lastIsText = true, false
		case protocol.ToolCall:
			res, ok := results[b.ID]
			call := toolCall{name: b.Name, args: shortArgs(b.Arguments), raw: b.Arguments}
			var block Block
			switch {
			case !ok:
				block = newToolCallBlock(call, ToolFailed, "", "no result", toolOutput{}, t.view)
			default:
				block = toolOutcome(call, res.IsError, "", firstLine(blocksText(res.Content)), toolOutputOf(res.Content, res.Details), t.view)
			}
			t.afterStep = true
			emitAppend(out, t.nextID("h"), block, true)
			lastIsText = false
		}
	}
	switch am.StopReason {
	case protocol.StopError:
		msg := "The request failed."
		if am.ErrorMessage != nil && oneLine(*am.ErrorMessage) != "" {
			msg = *am.ErrorMessage
		}
		if lastIsText {
			t.afterStep = false
			emitAppend(out, t.nextID("h"), NewSpacerBlock(), true)
		}
		t.afterStep = false
		emitAppend(out, t.nextID("h"), NewErrorBlock(ErrorHeadline("", sentence(msg)), ""), true)
	case protocol.StopAborted:
		t.afterStep = false
		emitAppend(out, t.nextID("h"), NewCancelMarker(cancelText("", visible)), true)
	default:
		if visible {
			if text := metaText(am, 0, 0); text != "" {
				t.afterStep = false
				emitAppend(out, t.nextID("h"), NewMetaLineBlock(text), true)
			}
		}
	}
}

// historyText adds a final reply text as one block for each chunk of the
// markdown, so that a long reply does not become one tall block.
func (t *Translator) historyText(text string, out *[]tea.Msg) {
	clean := sanitizeMarkdown(text)
	lead := t.afterStep
	t.afterStep = false
	for i, c := range splitMarkdown(clean) {
		emitAppend(out, t.nextID("h"), newAssistantTextBlock(clean[c.start:c.end], newMarkdownStream(), i == 0, lead), true)
	}
}

// historyThinking adds a final thinking text as one block. A snapshot has no
// event times, so the block shows no duration.
func (t *Translator) historyThinking(text string, out *[]tea.Msg) {
	text = sanitizeMarkdown(text)
	if strings.TrimSpace(text) == "" {
		text = ""
	}
	t.afterStep = true
	emitAppend(out, t.nextID("h"), newThinkingBlock(text, t.view, false, 0, 0, false), true)
}

// resumeStream shows the assistant message that was open at the cut. Blocks
// that had ended are final; the open ones continue with the events that follow.
func (t *Translator) resumeStream(st *protocol.ACPStreamBaseline, out *[]tea.Msg) {
	t.closeReply(out)
	r := &replyState{model: st.Message.Model, parts: map[int]*partState{}}
	t.reply = r
	for i, blk := range st.Message.Content {
		var (
			text     string
			thinking bool
		)
		switch b := blk.(type) {
		case protocol.Text:
			text = b.Text
		case protocol.Thinking:
			text, thinking = b.Thinking, true
		default:
			continue
		}
		p := t.part(r, i, thinking, out)
		t.setText(p, text, out)
		if _, open := st.Open[i]; !open {
			t.finishPart(p, out)
		}
	}
}
