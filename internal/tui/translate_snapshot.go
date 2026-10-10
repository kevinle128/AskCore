package tui

import (
	tea "charm.land/bubbletea/v2"

	"AskCore/pkg/protocol"
)

// Snapshot translates the history of a session into final blocks, in order,
// and opens the message that streamed at the cut. A SnapshotMsg arrives after
// a new follow and after a resync. A snapshot with Resync set replaces what
// the view holds; the transcript cannot remove committed blocks, so the
// session view decides how to show that (for example a divider).
//
// A tool call that has no result in the snapshot shows "✗ no result": the
// snapshot cannot tell a tool that is still running from one that never
// ended, and a block that stays open would hold the commit frontier.
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
			var block Block
			switch {
			case !ok:
				block = NewToolLineBlock(b.Name, shortArgs(b.Arguments), ToolFailed, "", "no result", t.view)
			default:
				block = toolOutcome(b.Name, shortArgs(b.Arguments), res.IsError, "", firstLine(blocksText(res.Content)), t.view)
			}
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
			emitAppend(out, t.nextID("h"), NewSpacerBlock(), true)
		}
		emitAppend(out, t.nextID("h"), NewErrorBlock(ErrorHeadline("", sentence(msg)), ""), true)
	case protocol.StopAborted:
		emitAppend(out, t.nextID("h"), NewCancelMarker(cancelText("", visible)), true)
	default:
		if visible {
			if text := metaText(am, 0, 0); text != "" {
				emitAppend(out, t.nextID("h"), NewMetaLineBlock(text), true)
			}
		}
	}
}

// historyText adds a final reply text as one block for each chunk of the
// markdown, so that a long reply does not become one tall block.
func (t *Translator) historyText(text string, out *[]tea.Msg) {
	clean := sanitizeMarkdown(text)
	for i, c := range splitMarkdown(clean) {
		emitAppend(out, t.nextID("h"), newAssistantTextBlock(clean[c.start:c.end], newMarkdownStream(), i == 0), true)
	}
}

// historyThinking adds a final thinking text as one block for each paragraph.
func (t *Translator) historyThinking(text string, out *[]tea.Msg) {
	paras := splitParagraphs(sanitizeMarkdown(text))
	if len(paras) == 0 {
		paras = []string{""}
	}
	for i, para := range paras {
		emitAppend(out, t.nextID("h"), newThinkingPart(para, t.view, i == 0, i == len(paras)-1), true)
	}
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
