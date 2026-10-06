package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

type document struct {
	body []byte
}

// buildDocument builds the request document. It reads the effective values
// from pr only; oauth adds the binding-dependent shaping of the subscription
// profile. A pr.MaxTokens of zero leaves the output limit out, which is how
// the size estimate for the clamp is made.
func buildDocument(msgs []protocol.Message, model providers.Model, pr *providers.Prepared, oauth bool) (document, []protocol.Diagnostic, error) {
	extra := map[string]any{}
	var diags []protocol.Diagnostic
	codec := toolNames{forward: map[string]string{}, reverse: map[string]string{}}
	if oauth {
		var err error
		codec, err = newToolNames(msgs)
		if err != nil {
			return document{}, nil, err
		}
	}

	sys := encodeSystem(msgs)
	if oauth {
		sys = append([]map[string]any{{"type": "text", "text": "You are Claude Code, Anthropic's official CLI for Claude."}}, sys...)
	}
	if len(sys) > 0 {
		extra["system"] = sys
	}
	tools, err := foldTools(msgs)
	if err != nil {
		return document{}, nil, err
	}
	for _, tool := range tools {
		tool["name"] = codec.encode(tool["name"].(string))
	}
	messages, err := encodeMessagesWithNames(msgs, oauth)
	if err != nil {
		return document{}, nil, err
	}
	extra["messages"] = messages
	if pr.ToolChoice != "" {
		if !oauth {
			for _, t := range providers.CurrentTools(msgs) {
				codec.forward[t.Name] = t.Name
			}
		}
		wire, ok := codec.forward[pr.ToolChoice]
		if !ok {
			return document{}, nil, fmt.Errorf("tool choice %q is not declared", pr.ToolChoice)
		}
		extra["tool_choice"] = map[string]any{"type": "tool", "name": wire}
	}

	if pr.Thinking == protocol.ThinkingOff {
		extra["thinking"] = map[string]any{"type": "disabled"}
		if pr.Temperature != nil {
			extra["temperature"] = *pr.Temperature
		}
	} else {
		extra["output_config"] = map[string]any{"effort": pr.Effort}
		if model.Provider == providers.ProviderAnthropic && model.Reasoning {
			extra["thinking"] = map[string]any{"type": "adaptive"}
		}
		if pr.Temperature != nil {
			diags = append(diags, protocol.Diagnostic{
				Type:  "unsupported_setting",
				Error: &protocol.DiagnosticError{Message: "temperature"},
			})
		}
	}

	if pr.CacheRetention == providers.CacheRetentionShort {
		applyCacheControl(tools, messages)
	}
	if len(tools) > 0 {
		extra["tools"] = tools
	}
	if pr.MaxTokens > 0 {
		extra["max_tokens"] = pr.MaxTokens
	}
	body, err := json.Marshal(extra)
	if err != nil {
		return document{}, diags, err
	}
	return document{body: body}, diags, nil
}

// effectiveThinking is the thinking level that a request uses: the requested
// level, medium when none was asked, and off when the model is an Anthropic
// model and the request forces a tool.
func effectiveThinking(model providers.Model, opts providers.StreamOptions) (protocol.ThinkingLevel, error) {
	level := opts.Reasoning
	if model.Provider == providers.ProviderAnthropic && opts.ToolChoice != "" {
		if level != "" && level != protocol.ThinkingOff {
			return "", fmt.Errorf("%w: forced tool choice cannot use Anthropic thinking", providers.ErrUnsupportedRequest)
		}
		level = protocol.ThinkingOff
	}
	if level == "" {
		level = protocol.ThinkingMedium
	}
	return level, nil
}

func effortOf(level protocol.ThinkingLevel) string {
	switch level {
	case protocol.ThinkingMinimal, protocol.ThinkingLow:
		return "low"
	case protocol.ThinkingMedium:
		return "medium"
	case protocol.ThinkingHigh:
		return "high"
	case protocol.ThinkingXHigh:
		return "xhigh"
	case protocol.ThinkingMax:
		return "max"
	default:
		return "medium"
	}
}

func clampMaxTokens(model providers.Model, opts providers.StreamOptions, estimate int) int {
	limit := model.MaxTokens
	if opts.MaxTokens > 0 && (limit == 0 || opts.MaxTokens < limit) {
		limit = opts.MaxTokens
	}
	if model.ContextWindow > 0 {
		room := model.ContextWindow - estimate - 4096
		if room < 1 {
			room = 1
		}
		if limit == 0 || room < limit {
			limit = room
		}
	}
	if limit < 1 {
		limit = 1
	}
	if model.MaxTokens > 0 && limit > model.MaxTokens {
		limit = model.MaxTokens
	}
	return limit
}

func encodeSystem(msgs []protocol.Message) []map[string]any {
	for _, m := range msgs {
		sys, ok := systemValue(m)
		if !ok {
			continue
		}
		text := providers.SystemPromptText(sys)
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []map[string]any{{"type": "text", "text": text}}
	}
	return nil
}

func foldTools(msgs []protocol.Message) ([]map[string]any, error) {
	decls := providers.CurrentTools(msgs)
	if len(decls) == 0 {
		return nil, nil
	}
	out := make([]map[string]any, 0, len(decls))
	for _, t := range decls {
		schema, err := toolSchema(t.Parameters)
		if err != nil {
			return nil, err
		}
		item := map[string]any{
			"name":         t.Name,
			"input_schema": schema,
		}
		if t.Description != "" {
			item["description"] = t.Description
		}
		out = append(out, item)
	}
	return out, nil
}

func toolSchema(raw json.RawMessage) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{"type": "object"}, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("tool parameters: %w", err)
	}
	return v, nil
}

func encodeMessagesWithNames(msgs []protocol.Message, oauth bool) ([]map[string]any, error) {
	var out []map[string]any
	var user []map[string]any
	history := map[string]string{}
	flushUser := func() {
		if len(user) == 0 {
			return
		}
		out = append(out, map[string]any{"role": "user", "content": user})
		user = nil
	}
	seenSystem := false
	for _, m := range msgs {
		if sys, ok := systemValue(m); ok {
			if oauth {
				for _, removed := range sys.ToolsRemoved {
					delete(history, removed.Name)
				}
				for _, added := range sys.ToolsAdded {
					history[added.Name] = canonicalToolName(added.Name)
				}
			}
			if !seenSystem {
				seenSystem = true
				continue
			}
			if text := providers.SystemUpdateText(sys); strings.TrimSpace(text) != "" {
				user = append(user, map[string]any{"type": "text", "text": text})
			}
			continue
		}
		if um, ok := userValue(m); ok {
			blocks, err := encodeUserBlocks(um.Content)
			if err != nil {
				return nil, err
			}
			user = append(user, blocks...)
			continue
		}
		if tr, ok := toolResultValue(m); ok {
			block, err := encodeToolResult(tr)
			if err != nil {
				return nil, err
			}
			user = append(user, block)
			continue
		}
		if asst, ok := assistantValue(m); ok {
			flushUser()
			blocks, err := encodeAssistant(asst)
			if err != nil {
				return nil, err
			}
			if oauth {
				for _, block := range blocks {
					if block["type"] == "tool_use" {
						if wire, ok := history[block["name"].(string)]; ok {
							block["name"] = wire
						}
					}
				}
			}
			if len(blocks) == 0 {
				continue
			}
			out = append(out, map[string]any{"role": "assistant", "content": blocks})
			continue
		}
	}
	flushUser()
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}

func encodeUserBlocks(blocks []protocol.UserBlock) ([]map[string]any, error) {
	var out []map[string]any
	for _, b := range blocks {
		switch v := b.(type) {
		case protocol.Text:
			if strings.TrimSpace(v.Text) == "" {
				continue
			}
			out = append(out, map[string]any{"type": "text", "text": v.Text})
		case *protocol.Text:
			if v == nil || strings.TrimSpace(v.Text) == "" {
				continue
			}
			out = append(out, map[string]any{"type": "text", "text": v.Text})
		case protocol.Image:
			img, err := encodeImage(v)
			if err != nil {
				return nil, err
			}
			out = append(out, img)
		case *protocol.Image:
			if v == nil {
				continue
			}
			img, err := encodeImage(*v)
			if err != nil {
				return nil, err
			}
			out = append(out, img)
		}
	}
	return out, nil
}

func encodeImage(img protocol.Image) (map[string]any, error) {
	data := stripDataURL(img.Data)
	if data == "" {
		return nil, fmt.Errorf("image data is empty")
	}
	return map[string]any{
		"type": "image",
		"source": map[string]any{
			"type":       "base64",
			"media_type": img.MimeType,
			"data":       data,
		},
	}, nil
}

func stripDataURL(s string) string {
	if strings.HasPrefix(s, "data:") {
		if i := strings.IndexByte(s, ','); i >= 0 {
			return s[i+1:]
		}
	}
	return s
}

func encodeToolResult(m protocol.ToolResultMessage) (map[string]any, error) {
	content, err := encodeUserBlocks(m.Content)
	if err != nil {
		return nil, err
	}
	if content == nil {
		content = []map[string]any{}
	}
	block := map[string]any{
		"type":        "tool_result",
		"tool_use_id": m.ToolCallID,
		"content":     content,
	}
	if m.IsError {
		block["is_error"] = true
	}
	return block, nil
}

func encodeAssistant(m protocol.AssistantMessage) ([]map[string]any, error) {
	var out []map[string]any
	for _, b := range m.Content {
		switch v := b.(type) {
		case protocol.Text:
			if strings.TrimSpace(v.Text) == "" {
				continue
			}
			out = append(out, map[string]any{"type": "text", "text": v.Text})
		case *protocol.Text:
			if v == nil || strings.TrimSpace(v.Text) == "" {
				continue
			}
			out = append(out, map[string]any{"type": "text", "text": v.Text})
		case protocol.Thinking:
			out = append(out, encodeThinking(v))
		case *protocol.Thinking:
			if v == nil {
				continue
			}
			out = append(out, encodeThinking(*v))
		case protocol.ToolCall:
			block, err := encodeToolCall(v)
			if err != nil {
				return nil, err
			}
			out = append(out, block)
		case *protocol.ToolCall:
			if v == nil {
				continue
			}
			block, err := encodeToolCall(*v)
			if err != nil {
				return nil, err
			}
			out = append(out, block)
		}
	}
	return out, nil
}

func encodeThinking(t protocol.Thinking) map[string]any {
	if t.Redacted != nil && *t.Redacted {
		data := ""
		if t.ThinkingSignature != nil {
			data = *t.ThinkingSignature
		}
		return map[string]any{"type": "redacted_thinking", "data": data}
	}
	sig := ""
	if t.ThinkingSignature != nil {
		sig = *t.ThinkingSignature
	}
	return map[string]any{"type": "thinking", "thinking": t.Thinking, "signature": sig}
}

func encodeToolCall(c protocol.ToolCall) (map[string]any, error) {
	args := bytes.TrimSpace(c.Arguments)
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	if !bytes.HasPrefix(args, []byte("{")) || !json.Valid(args) {
		return nil, fmt.Errorf("tool arguments are not a JSON object")
	}
	var input any
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	return map[string]any{
		"type":  "tool_use",
		"id":    c.ID,
		"name":  c.Name,
		"input": input,
	}, nil
}

func applyCacheControl(tools []map[string]any, messages []map[string]any) {
	ctl := map[string]any{"type": "ephemeral"}
	if n := len(tools); n > 0 {
		tools[n-1]["cache_control"] = ctl
	}
	if len(messages) == 0 {
		return
	}
	last := messages[len(messages)-1]
	content, ok := last["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		return
	}
	content[len(content)-1]["cache_control"] = ctl
}

func systemValue(m protocol.Message) (protocol.SystemMessage, bool) {
	switch v := m.(type) {
	case protocol.SystemMessage:
		return v, true
	case *protocol.SystemMessage:
		if v == nil {
			return protocol.SystemMessage{}, false
		}
		return *v, true
	default:
		return protocol.SystemMessage{}, false
	}
}

func userValue(m protocol.Message) (protocol.UserMessage, bool) {
	switch v := m.(type) {
	case protocol.UserMessage:
		return v, true
	case *protocol.UserMessage:
		if v == nil {
			return protocol.UserMessage{}, false
		}
		return *v, true
	default:
		return protocol.UserMessage{}, false
	}
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
