package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// buildCompletionsBody builds the request document. It reads the effective
// values from pr only. A pr.MaxTokens of zero leaves the output limit out,
// which is how the size estimate for the clamp is made.
func buildCompletionsBody(msgs []protocol.Message, model providers.Model, pr *providers.Prepared) (map[string]any, error) {
	compat, _ := model.Completions()
	extra := map[string]any{
		"model":  model.ID,
		"stream": true,
	}
	if compat.SupportsStore {
		extra["store"] = false
	}
	if compat.SupportsStreamOptions {
		extra["stream_options"] = map[string]any{"include_usage": true}
	}
	messages, err := encodeCompletionsMessages(msgs, model, compat)
	if err != nil {
		return nil, err
	}
	extra["messages"] = messages

	tools, err := encodeCompletionsTools(msgs)
	if err != nil {
		return nil, err
	}
	if len(tools) > 0 {
		extra["tools"] = tools
	}

	if pr.ToolChoice != "" {
		found := false
		for _, tool := range providers.CurrentTools(msgs) {
			if tool.Name == pr.ToolChoice {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: unknown Completions tool choice %q", providers.ErrUnsupportedRequest, pr.ToolChoice)
		}
		extra["tool_choice"] = map[string]any{"type": "function", "function": map[string]any{"name": pr.ToolChoice}}
	}
	applyCompletionsThinking(extra, compat, pr)

	if pr.Temperature != nil {
		extra["temperature"] = *pr.Temperature
	}
	if pr.MaxTokens > 0 {
		field := compat.MaxTokensField
		if field == "" {
			field = "max_tokens"
		}
		extra[field] = pr.MaxTokens
	}
	return extra, nil
}

// effectiveCompletionsThinking is the thinking level and the effort word of a
// request. A model without reasoning has neither.
func effectiveCompletionsThinking(model providers.Model, level protocol.ThinkingLevel) (protocol.ThinkingLevel, string) {
	if !model.Reasoning {
		return "", ""
	}
	if level == "" {
		level = protocol.ThinkingMedium
	}
	if level == protocol.ThinkingOff {
		return level, ""
	}
	word, _ := thinkingEffort(model, level)
	return level, word
}

func applyCompletionsThinking(extra map[string]any, compat providers.CompletionsCompat, pr *providers.Prepared) {
	if pr.Thinking == "" {
		return
	}
	if compat.ThinkingFormat == "qwen" {
		if pr.Thinking == protocol.ThinkingOff {
			extra["enable_thinking"] = false
			return
		}
		extra["enable_thinking"] = true
	}
	if pr.Thinking == protocol.ThinkingOff {
		return
	}
	if pr.Effort != "" {
		extra["reasoning_effort"] = pr.Effort
	}
}

func thinkingEffort(model providers.Model, level protocol.ThinkingLevel) (string, bool) {
	word, ok := model.ThinkingLevelMap[level]
	if !ok || word == nil {
		return "", false
	}
	return *word, true
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

func encodeCompletionsTools(msgs []protocol.Message) ([]map[string]any, error) {
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
		fn := map[string]any{"name": t.Name, "parameters": schema}
		if t.Description != "" {
			fn["description"] = t.Description
		}
		out = append(out, map[string]any{"type": "function", "function": fn})
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

func encodeCompletionsMessages(msgs []protocol.Message, model providers.Model, compat providers.CompletionsCompat) ([]map[string]any, error) {
	var out []map[string]any
	seenSystem := false
	systemRole := "system"
	if model.Reasoning && compat.SupportsDeveloperRole {
		systemRole = "developer"
	}
	for i := 0; i < len(msgs); i++ {
		m := msgs[i]
		if sys, ok := systemValue(m); ok {
			if !seenSystem {
				seenSystem = true
				if text := providers.SystemPromptText(sys); strings.TrimSpace(text) != "" {
					out = append(out, map[string]any{"role": systemRole, "content": text})
				}
				continue
			}
			if text := providers.SystemUpdateText(sys); strings.TrimSpace(text) != "" {
				out = append(out, map[string]any{"role": "user", "content": text})
			}
			continue
		}
		if um, ok := userValue(m); ok {
			content, err := encodeCompletionsUser(um.Content)
			if err != nil {
				return nil, err
			}
			if content == nil {
				continue
			}
			out = append(out, map[string]any{"role": "user", "content": content})
			continue
		}
		if asst, ok := assistantValue(m); ok {
			msg, ok, err := encodeCompletionsAssistant(asst, model, compat)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, msg)
			}
			continue
		}
		if tr, ok := toolResultValue(m); ok {
			var images []protocol.Image
			for {
				toolMessage, toolImages := encodeCompletionsToolResult(tr)
				out = append(out, toolMessage)
				images = append(images, toolImages...)
				if i+1 >= len(msgs) {
					break
				}
				next, isResult := toolResultValue(msgs[i+1])
				if !isResult {
					break
				}
				i++
				tr = next
			}
			if len(images) > 0 {
				content := []map[string]any{{"type": "text", "text": "Attached image(s) from tool result:"}}
				for _, img := range images {
					content = append(content, encodeCompletionsImage(img))
				}
				out = append(out, map[string]any{"role": "user", "content": content})
			}
			continue
		}
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}

func encodeCompletionsUser(blocks []protocol.UserBlock) (any, error) {
	var parts []map[string]any
	var texts []string
	hasImage := false
	for _, b := range blocks {
		switch v := b.(type) {
		case protocol.Text:
			if strings.TrimSpace(v.Text) == "" {
				continue
			}
			texts = append(texts, v.Text)
			parts = append(parts, map[string]any{"type": "text", "text": v.Text})
		case *protocol.Text:
			if v == nil || strings.TrimSpace(v.Text) == "" {
				continue
			}
			texts = append(texts, v.Text)
			parts = append(parts, map[string]any{"type": "text", "text": v.Text})
		case protocol.Image:
			hasImage = true
			parts = append(parts, encodeCompletionsImage(v))
		case *protocol.Image:
			if v == nil {
				continue
			}
			hasImage = true
			parts = append(parts, encodeCompletionsImage(*v))
		}
	}
	if len(parts) == 0 {
		return nil, nil
	}
	if !hasImage && len(texts) == 1 {
		return texts[0], nil
	}
	if !hasImage {
		return strings.Join(texts, "\n"), nil
	}
	return parts, nil
}

func encodeCompletionsImage(img protocol.Image) map[string]any {
	url := img.Data
	if !strings.HasPrefix(url, "data:") {
		url = "data:" + img.MimeType + ";base64," + url
	}
	return map[string]any{
		"type":      "image_url",
		"image_url": map[string]any{"url": url},
	}
}

func encodeCompletionsAssistant(m protocol.AssistantMessage, model providers.Model, compat providers.CompletionsCompat) (map[string]any, bool, error) {
	var texts []string
	var thinks []string
	var field string
	var calls []map[string]any
	for _, b := range m.Content {
		switch v := b.(type) {
		case protocol.Text:
			if strings.TrimSpace(v.Text) != "" {
				texts = append(texts, v.Text)
			}
		case *protocol.Text:
			if v != nil && strings.TrimSpace(v.Text) != "" {
				texts = append(texts, v.Text)
			}
		case protocol.Thinking:
			if strings.TrimSpace(v.Thinking) == "" {
				continue
			}
			thinks = append(thinks, v.Thinking)
			if field == "" && v.ThinkingSignature != nil {
				field = *v.ThinkingSignature
			}
		case *protocol.Thinking:
			if v == nil || strings.TrimSpace(v.Thinking) == "" {
				continue
			}
			thinks = append(thinks, v.Thinking)
			if field == "" && v.ThinkingSignature != nil {
				field = *v.ThinkingSignature
			}
		case protocol.ToolCall:
			call, err := encodeCompletionsToolCall(v)
			if err != nil {
				return nil, false, err
			}
			calls = append(calls, call)
		case *protocol.ToolCall:
			if v == nil {
				continue
			}
			call, err := encodeCompletionsToolCall(*v)
			if err != nil {
				return nil, false, err
			}
			calls = append(calls, call)
		}
	}
	msg := map[string]any{"role": "assistant"}
	if compat.RequiresThinkingAsText && len(thinks) > 0 {
		content := []map[string]any{{"type": "text", "text": strings.Join(thinks, "\n\n")}}
		for _, text := range texts {
			content = append(content, map[string]any{"type": "text", "text": text})
		}
		msg["content"] = content
	} else if len(texts) > 0 {
		msg["content"] = strings.Join(texts, "")
	} else {
		msg["content"] = nil
	}
	if len(calls) > 0 {
		msg["tool_calls"] = calls
	}
	if !compat.RequiresThinkingAsText && (field == "reasoning_content" || field == "reasoning" || field == "reasoning_text") {
		msg[field] = strings.Join(thinks, "\n")
	}
	if compat.RequiresReasoningContentOnAssistantMessages && model.Reasoning {
		if _, ok := msg["reasoning_content"]; !ok {
			msg["reasoning_content"] = ""
		}
	}
	if msg["content"] == nil && len(calls) == 0 {
		return nil, false, nil
	}
	return msg, true, nil
}

func encodeCompletionsToolCall(c protocol.ToolCall) (map[string]any, error) {
	args := bytes.TrimSpace(c.Arguments)
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	if !bytes.HasPrefix(args, []byte("{")) || !json.Valid(args) {
		return nil, fmt.Errorf("tool arguments are not a JSON object")
	}
	return map[string]any{
		"id":   c.ID,
		"type": "function",
		"function": map[string]any{
			"name":      c.Name,
			"arguments": string(args),
		},
	}, nil
}

func encodeCompletionsToolResult(m protocol.ToolResultMessage) (map[string]any, []protocol.Image) {
	var texts []string
	var images []protocol.Image
	for _, b := range m.Content {
		switch v := b.(type) {
		case protocol.Text:
			if strings.TrimSpace(v.Text) != "" {
				texts = append(texts, v.Text)
			}
		case *protocol.Text:
			if v != nil && strings.TrimSpace(v.Text) != "" {
				texts = append(texts, v.Text)
			}
		case protocol.Image:
			images = append(images, v)
		case *protocol.Image:
			if v != nil {
				images = append(images, *v)
			}
		}
	}
	content := strings.Join(texts, "\n")
	if content == "" {
		if len(images) > 0 {
			content = "(see attached image)"
		} else {
			content = "(no tool output)"
		}
	}
	return map[string]any{
		"role":         "tool",
		"tool_call_id": m.ToolCallID,
		"content":      content,
	}, images
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
