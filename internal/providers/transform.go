package providers

import (
	"slices"
	"strings"
	"time"

	"AskCore/pkg/protocol"
)

const (
	imageOmittedPlaceholder     = "(image omitted: model does not support images)"
	toolImageOmittedPlaceholder = "(tool image omitted: model does not support images)"
	noResultProvided            = "No result provided"
)

// NormalizeToolCallID rewrites one tool-call id. source is the assistant
// message that contained the call. A nil function means the identity.
type NormalizeToolCallID func(id string, model Model, source protocol.AssistantMessage) string

// TransformMessages returns the model-facing transcript.
// now is unix milliseconds for synthetic tool results. A nil now is time.Now.
// The input slice and every message it holds are not mutated.
func TransformMessages(msgs []protocol.Message, model Model, now func() int64, normalize NormalizeToolCallID) []protocol.Message {
	if now == nil {
		now = func() int64 { return time.Now().UnixMilli() }
	}
	if normalize == nil {
		normalize = func(id string, _ Model, _ protocol.AssistantMessage) string { return id }
	}
	vision := slices.Contains(model.Input, "image")
	idMap := map[string]string{}
	prepared := make([]protocol.Message, 0, len(msgs))
	for _, m := range msgs {
		m = emptyNilContent(m)
		m = omitImages(m, vision)
		m = rewriteMessage(m, model, normalize, idMap)
		prepared = append(prepared, m)
	}
	return pairMessages(prepared, now)
}

func emptyNilContent(m protocol.Message) protocol.Message {
	switch v := m.(type) {
	case protocol.SystemMessage:
		if v.Content != nil {
			return m
		}
		v.Content = []protocol.Text{}
		return v
	case *protocol.SystemMessage:
		if v == nil || v.Content != nil {
			return m
		}
		c := *v
		c.Content = []protocol.Text{}
		return &c
	case protocol.UserMessage:
		if v.Content != nil {
			return m
		}
		v.Content = []protocol.UserBlock{}
		return v
	case *protocol.UserMessage:
		if v == nil || v.Content != nil {
			return m
		}
		c := *v
		c.Content = []protocol.UserBlock{}
		return &c
	case protocol.AssistantMessage:
		if v.Content != nil {
			return m
		}
		v.Content = []protocol.AssistantBlock{}
		return v
	case *protocol.AssistantMessage:
		if v == nil || v.Content != nil {
			return m
		}
		c := *v
		c.Content = []protocol.AssistantBlock{}
		return &c
	case protocol.ToolResultMessage:
		if v.Content != nil {
			return m
		}
		v.Content = []protocol.UserBlock{}
		return v
	case *protocol.ToolResultMessage:
		if v == nil || v.Content != nil {
			return m
		}
		c := *v
		c.Content = []protocol.UserBlock{}
		return &c
	default:
		return m
	}
}

func omitImages(m protocol.Message, vision bool) protocol.Message {
	if vision {
		return m
	}
	switch v := m.(type) {
	case protocol.UserMessage:
		if content, ok := collapseImages(v.Content, imageOmittedPlaceholder); ok {
			v.Content = content
			return v
		}
	case *protocol.UserMessage:
		if v == nil {
			return m
		}
		if content, ok := collapseImages(v.Content, imageOmittedPlaceholder); ok {
			c := *v
			c.Content = content
			return &c
		}
	case protocol.ToolResultMessage:
		if content, ok := collapseImages(v.Content, toolImageOmittedPlaceholder); ok {
			v.Content = content
			return v
		}
	case *protocol.ToolResultMessage:
		if v == nil {
			return m
		}
		if content, ok := collapseImages(v.Content, toolImageOmittedPlaceholder); ok {
			c := *v
			c.Content = content
			return &c
		}
	}
	return m
}

func collapseImages(blocks []protocol.UserBlock, placeholder string) ([]protocol.UserBlock, bool) {
	changed := false
	out := make([]protocol.UserBlock, 0, len(blocks))
	lastPlaceholder := false
	for _, b := range blocks {
		switch b.(type) {
		case protocol.Image, *protocol.Image:
			changed = true
			if lastPlaceholder {
				continue
			}
			out = append(out, protocol.Text{Text: placeholder})
			lastPlaceholder = true
		default:
			text, isText := textOf(b)
			out = append(out, b)
			lastPlaceholder = isText && text.Text == placeholder
		}
	}
	if !changed {
		return nil, false
	}
	return out, true
}

func rewriteMessage(m protocol.Message, model Model, normalize NormalizeToolCallID, idMap map[string]string) protocol.Message {
	switch v := m.(type) {
	case protocol.AssistantMessage:
		if next, ok := rewriteAssistant(v, model, normalize, idMap); ok {
			return next
		}
	case *protocol.AssistantMessage:
		if v == nil {
			return m
		}
		if next, ok := rewriteAssistant(*v, model, normalize, idMap); ok {
			return &next
		}
	case protocol.ToolResultMessage:
		if next, ok := rekeyResult(v, idMap); ok {
			return next
		}
	case *protocol.ToolResultMessage:
		if v == nil {
			return m
		}
		if next, ok := rekeyResult(*v, idMap); ok {
			c := next
			return &c
		}
	}
	return m
}

func rewriteAssistant(m protocol.AssistantMessage, model Model, normalize NormalizeToolCallID, idMap map[string]string) (protocol.AssistantMessage, bool) {
	same := sameModel(m, model)
	changed := false
	out := make([]protocol.AssistantBlock, 0, len(m.Content))
	for _, b := range m.Content {
		nb, keep, did := rewriteAssistantBlock(b, same, model, m, normalize, idMap)
		if did {
			changed = true
		}
		if !keep {
			changed = true
			continue
		}
		out = append(out, nb)
	}
	if !changed {
		return m, false
	}
	m.Content = out
	return m, true
}

func rewriteAssistantBlock(b protocol.AssistantBlock, same bool, model Model, source protocol.AssistantMessage, normalize NormalizeToolCallID, idMap map[string]string) (protocol.AssistantBlock, bool, bool) {
	if t, ok := thinkingOf(b); ok {
		return rewriteThinking(t, same)
	}
	if t, ok := textOfAssistant(b); ok {
		if same || t.TextSignature == nil {
			return b, true, false
		}
		t.TextSignature = nil
		return t, true, true
	}
	if c, ok := toolCallOf(b); ok {
		return rewriteToolCall(c, same, model, source, normalize, idMap)
	}
	return b, true, false
}

func rewriteThinking(t protocol.Thinking, same bool) (protocol.AssistantBlock, bool, bool) {
	if t.Redacted != nil && *t.Redacted {
		if same {
			return t, true, false
		}
		return nil, false, true
	}
	if same && t.ThinkingSignature != nil && *t.ThinkingSignature != "" {
		return t, true, false
	}
	if strings.TrimSpace(t.Thinking) == "" {
		return nil, false, true
	}
	if same {
		return t, true, false
	}
	return protocol.Text{Text: t.Thinking}, true, true
}

func rewriteToolCall(c protocol.ToolCall, same bool, model Model, source protocol.AssistantMessage, normalize NormalizeToolCallID, idMap map[string]string) (protocol.AssistantBlock, bool, bool) {
	if same {
		return c, true, false
	}
	changed := false
	if c.ThoughtSignature != nil {
		c.ThoughtSignature = nil
		changed = true
	}
	old := c.ID
	if next := normalize(old, model, source); next != old {
		c.ID = next
		idMap[old] = next
		changed = true
	}
	return c, true, changed
}

func rekeyResult(m protocol.ToolResultMessage, idMap map[string]string) (protocol.ToolResultMessage, bool) {
	next, ok := idMap[m.ToolCallID]
	if !ok || next == m.ToolCallID {
		return m, false
	}
	m.ToolCallID = next
	return m, true
}

func sameModel(m protocol.AssistantMessage, model Model) bool {
	return m.API == string(model.API) && m.Provider == model.Provider && m.Model == model.ID
}

type pendingCall struct {
	id, name string
	done     bool
}

func pairMessages(msgs []protocol.Message, now func() int64) []protocol.Message {
	out := make([]protocol.Message, 0, len(msgs))
	var pending []pendingCall
	var held []protocol.Message

	closePending := func() {
		for _, p := range pending {
			if p.done {
				continue
			}
			out = append(out, protocol.ToolResultMessage{
				ToolCallID: p.id,
				ToolName:   p.name,
				Content:    []protocol.UserBlock{protocol.Text{Text: noResultProvided}},
				IsError:    true,
				Timestamp:  now(),
			})
		}
		pending = nil
		out = append(out, held...)
		held = nil
	}

	for _, m := range msgs {
		if asst, ok := assistantValue(m); ok {
			closePending()
			if asst.StopReason == protocol.StopError || asst.StopReason == protocol.StopAborted {
				continue
			}
			for _, b := range asst.Content {
				if c, ok := toolCallOf(b); ok {
					pending = append(pending, pendingCall{id: c.ID, name: c.Name})
				}
			}
			out = append(out, m)
			continue
		}
		if res, ok := toolResultValue(m); ok {
			matched := false
			for i := range pending {
				if pending[i].id == res.ToolCallID && !pending[i].done {
					pending[i].done = true
					matched = true
					break
				}
			}
			if matched {
				out = append(out, m)
			}
			continue
		}
		if isSystem(m) {
			if len(pending) > 0 {
				held = append(held, m)
			} else {
				out = append(out, m)
			}
			continue
		}
		if isUser(m) {
			closePending()
			out = append(out, m)
			continue
		}
		out = append(out, m)
	}
	closePending()
	return out
}

func assistantValue(m protocol.Message) (protocol.AssistantMessage, bool) {
	switch v := m.(type) {
	case protocol.AssistantMessage:
		return v, true
	case *protocol.AssistantMessage:
		if v == nil {
			return protocol.AssistantMessage{}, false
		}
		return *v, true
	default:
		return protocol.AssistantMessage{}, false
	}
}

func toolResultValue(m protocol.Message) (protocol.ToolResultMessage, bool) {
	switch v := m.(type) {
	case protocol.ToolResultMessage:
		return v, true
	case *protocol.ToolResultMessage:
		if v == nil {
			return protocol.ToolResultMessage{}, false
		}
		return *v, true
	default:
		return protocol.ToolResultMessage{}, false
	}
}

func isSystem(m protocol.Message) bool {
	switch m.(type) {
	case protocol.SystemMessage, *protocol.SystemMessage:
		return true
	default:
		return false
	}
}

func isUser(m protocol.Message) bool {
	switch m.(type) {
	case protocol.UserMessage, *protocol.UserMessage:
		return true
	default:
		return false
	}
}

func thinkingOf(b protocol.AssistantBlock) (protocol.Thinking, bool) {
	switch v := b.(type) {
	case protocol.Thinking:
		return v, true
	case *protocol.Thinking:
		if v == nil {
			return protocol.Thinking{}, false
		}
		return *v, true
	default:
		return protocol.Thinking{}, false
	}
}

func textOfAssistant(b protocol.AssistantBlock) (protocol.Text, bool) {
	switch v := b.(type) {
	case protocol.Text:
		return v, true
	case *protocol.Text:
		if v == nil {
			return protocol.Text{}, false
		}
		return *v, true
	default:
		return protocol.Text{}, false
	}
}

func toolCallOf(b protocol.AssistantBlock) (protocol.ToolCall, bool) {
	switch v := b.(type) {
	case protocol.ToolCall:
		return v, true
	case *protocol.ToolCall:
		if v == nil {
			return protocol.ToolCall{}, false
		}
		return *v, true
	default:
		return protocol.ToolCall{}, false
	}
}

func textOf(b protocol.UserBlock) (protocol.Text, bool) {
	switch v := b.(type) {
	case protocol.Text:
		return v, true
	case *protocol.Text:
		if v == nil {
			return protocol.Text{}, false
		}
		return *v, true
	default:
		return protocol.Text{}, false
	}
}
