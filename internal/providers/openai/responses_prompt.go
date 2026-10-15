package openai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/fantasy"
	fopenai "charm.land/fantasy/providers/openai"

	"AskCore/internal/providers"
	"AskCore/pkg/protocol"
)

// buildResponsesCall builds the call of a Responses request. It reads the
// effective values from pr only; method is the login method of the credential
// binding, which shapes the instructions of the ChatGPT profile.
func buildResponsesCall(msgs []protocol.Message, model providers.Model, pr *providers.Prepared, method string) (fantasy.Call, error) {
	prompt, err := encodeResponsesPrompt(msgs, model)
	if err != nil {
		return fantasy.Call{}, err
	}
	tools, err := encodeResponsesTools(msgs)
	if err != nil {
		return fantasy.Call{}, err
	}
	store := false
	ropts := &fopenai.ResponsesProviderOptions{Store: &store}
	if len(model.SamplingParams) > 0 {
		ropts.ExtraBody = make(map[string]any, len(model.SamplingParams))
		for name, value := range model.SamplingParams {
			switch name {
			case "model", "input", "instructions", "stream", "store", "previous_response_id", "tool_choice":
				return fantasy.Call{}, fmt.Errorf("%w: Responses field %q is owned by the request", providers.ErrUnsupportedRequest, name)
			}
			ropts.ExtraBody[name] = value
		}
	}
	if method == "openai-chatgpt" {
		var supported fantasy.Prompt
		for _, msg := range prompt {
			if msg.Role == fantasy.MessageRoleSystem {
				for _, part := range msg.Content {
					if text, ok := part.(fantasy.TextPart); ok {
						value := text.Text
						ropts.Instructions = &value
					}
				}
			} else {
				supported = append(supported, msg)
			}
		}
		prompt = supported
	}
	compat, _ := model.Responses()
	if model.Reasoning {
		ropts.Include = []fopenai.IncludeType{fopenai.IncludeReasoningEncryptedContent}
	}
	if pr.PromptCacheKey != "" {
		key := pr.PromptCacheKey
		ropts.PromptCacheKey = &key
	}
	if pr.CacheRetention == providers.CacheRetentionLong && compat.SupportsLongCacheRetention {
		if ropts.ExtraBody == nil {
			ropts.ExtraBody = map[string]any{}
		}
		ropts.ExtraBody["prompt_cache_retention"] = "24h"
	}
	if pr.Effort != "" {
		effort := fopenai.ReasoningEffort(pr.Effort)
		ropts.ReasoningEffort = &effort
	}
	call := fantasy.Call{
		Prompt:          prompt,
		Tools:           tools,
		ProviderOptions: fopenai.NewResponsesProviderOptions(ropts),
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
			return fantasy.Call{}, fmt.Errorf("unknown Responses tool choice %q", pr.ToolChoice)
		}
		choice := fantasy.SpecificToolChoice(pr.ToolChoice)
		call.ToolChoice = &choice
		if raw, ok := ropts.ExtraBody["tools"]; ok {
			encoded, err := json.Marshal(raw)
			if err != nil {
				return fantasy.Call{}, fmt.Errorf("responses tools: %w", err)
			}
			var groups []map[string]any
			if err := json.Unmarshal(encoded, &groups); err != nil {
				return fantasy.Call{}, fmt.Errorf("responses tools: %w", err)
			}
			matches := 0
			for _, group := range groups {
				if group["name"] == pr.ToolChoice && (group["type"] == "function" || group["type"] == "custom") {
					matches++
				}
				if group["type"] != "namespace" {
					continue
				}
				children, _ := group["tools"].([]any)
				for _, child := range children {
					tool, ok := child.(map[string]any)
					if ok && tool["name"] == pr.ToolChoice {
						matches++
						ropts.ExtraBody["tool_choice"] = map[string]any{"type": tool["type"], "name": pr.ToolChoice, "namespace": group["name"]}
					}
				}
			}
			if matches != 1 {
				return fantasy.Call{}, fmt.Errorf("responses tool choice %q has %d declarations", pr.ToolChoice, matches)
			}
		}
	}
	if pr.MaxTokens > 0 && compat.SupportsMaxOutputTokens {
		limit := int64(pr.MaxTokens)
		call.MaxOutputTokens = &limit
	}
	return call, nil
}

func clampPromptCacheKey(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) > 64 {
		return string(runes[:64])
	}
	return s
}

func encodeResponsesTools(msgs []protocol.Message) ([]fantasy.Tool, error) {
	decls := providers.CurrentTools(msgs)
	if len(decls) == 0 {
		return nil, nil
	}
	out := make([]fantasy.Tool, 0, len(decls))
	for _, t := range decls {
		schema, err := toolSchema(t.Parameters)
		if err != nil {
			return nil, err
		}
		m, _ := schema.(map[string]any)
		if m == nil {
			m = map[string]any{"type": "object"}
		}
		out = append(out, fantasy.FunctionTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: m,
		})
	}
	return out, nil
}

func encodeResponsesPrompt(msgs []protocol.Message, model providers.Model) (fantasy.Prompt, error) {
	var prompt fantasy.Prompt
	seenSystem := false
	for messageIndex, m := range msgs {
		if sys, ok := systemValue(m); ok {
			if !seenSystem {
				seenSystem = true
				if text := providers.SystemPromptText(sys); strings.TrimSpace(text) != "" {
					prompt = append(prompt, fantasy.NewSystemMessage(text))
				}
				continue
			}
			if text := providers.SystemUpdateText(sys); strings.TrimSpace(text) != "" {
				prompt = append(prompt, fantasy.NewUserMessage(text))
			}
			continue
		}
		if um, ok := userValue(m); ok {
			msg, ok, err := encodeResponsesUser(um)
			if err != nil {
				return nil, err
			}
			if ok {
				prompt = append(prompt, msg)
			}
			continue
		}
		if asst, ok := assistantValue(m); ok {
			msg, ok := encodeResponsesAssistant(asst, model, messageIndex)
			if ok {
				prompt = append(prompt, msg)
			}
			continue
		}
		if tr, ok := toolResultValue(m); ok {
			prompt = append(prompt, encodeResponsesToolResult(tr))
			continue
		}
	}
	return prompt, nil
}

func encodeResponsesUser(m protocol.UserMessage) (fantasy.Message, bool, error) {
	var parts []fantasy.MessagePart
	for _, b := range m.Content {
		switch v := b.(type) {
		case protocol.Text:
			if strings.TrimSpace(v.Text) != "" {
				parts = append(parts, fantasy.TextPart{Text: v.Text})
			}
		case *protocol.Text:
			if v != nil && strings.TrimSpace(v.Text) != "" {
				parts = append(parts, fantasy.TextPart{Text: v.Text})
			}
		case protocol.Image:
			parts = append(parts, filePart(v))
		case *protocol.Image:
			if v != nil {
				parts = append(parts, filePart(*v))
			}
		}
	}
	if len(parts) == 0 {
		return fantasy.Message{}, false, nil
	}
	return fantasy.Message{Role: fantasy.MessageRoleUser, Content: parts}, true, nil
}

func filePart(img protocol.Image) fantasy.FilePart {
	data := img.Data
	if i := strings.IndexByte(data, ','); strings.HasPrefix(data, "data:") && i >= 0 {
		data = data[i+1:]
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		raw = []byte(data)
	}
	return fantasy.FilePart{Data: raw, MediaType: img.MimeType}
}

func encodeResponsesAssistant(m protocol.AssistantMessage, target providers.Model, messageIndex int) (fantasy.Message, bool) {
	source := providers.Model{ID: m.Model, Provider: m.Provider, API: providers.API(m.API)}
	var parts []fantasy.MessagePart
	for blockIndex, b := range m.Content {
		fallbackID := fmt.Sprintf("msg_pi_%d_%d", messageIndex, blockIndex)
		switch v := b.(type) {
		case protocol.Text:
			if strings.TrimSpace(v.Text) != "" {
				parts = append(parts, responsesTextPart(v, fallbackID))
			}
		case *protocol.Text:
			if v != nil && strings.TrimSpace(v.Text) != "" {
				parts = append(parts, responsesTextPart(*v, fallbackID))
			}
		case protocol.Thinking:
			if p, ok := responsesReasoningPart(v); ok {
				parts = append(parts, p)
			}
		case *protocol.Thinking:
			if v != nil {
				if p, ok := responsesReasoningPart(*v); ok {
					parts = append(parts, p)
				}
			}
		case protocol.ToolCall:
			parts = append(parts, responsesToolPart(v, source, target))
		case *protocol.ToolCall:
			if v != nil {
				parts = append(parts, responsesToolPart(*v, source, target))
			}
		}
	}
	if len(parts) == 0 {
		return fantasy.Message{}, false
	}
	return fantasy.Message{Role: fantasy.MessageRoleAssistant, Content: parts}, true
}

func responsesTextPart(t protocol.Text, fallbackID string) fantasy.TextPart {
	p := fantasy.TextPart{Text: t.Text}
	replay := providers.TextReplay{ID: fallbackID}
	if t.TextSignature != nil && *t.TextSignature != "" {
		if decoded, ok := providers.DecodeTextSignature(*t.TextSignature); ok && decoded.ID != "" {
			replay = decoded
		}
	}
	if len([]rune(replay.ID)) > 64 {
		replay.ID = "msg_" + providers.ShortHash(replay.ID)
	}
	p.ProviderOptions = fantasy.ProviderOptions{
		fopenai.Name: &fopenai.ResponsesTextMetadata{ItemID: replay.ID, Phase: replay.Phase},
	}
	return p
}

func responsesReasoningPart(t protocol.Thinking) (fantasy.ReasoningPart, bool) {
	if t.ThinkingSignature == nil || *t.ThinkingSignature == "" {
		if strings.TrimSpace(t.Thinking) == "" {
			return fantasy.ReasoningPart{}, false
		}
		return fantasy.ReasoningPart{Text: t.Thinking}, true
	}
	replay, ok := providers.DecodeThinkingSignature(*t.ThinkingSignature)
	if !ok {
		if strings.TrimSpace(t.Thinking) == "" {
			return fantasy.ReasoningPart{}, false
		}
		return fantasy.ReasoningPart{Text: t.Thinking}, true
	}
	meta := &fopenai.ResponsesReasoningMetadata{
		ItemID:    replay.ID,
		Finalized: true,
	}
	if replay.EncryptedContent != "" {
		enc := replay.EncryptedContent
		meta.EncryptedContent = &enc
	}
	for _, s := range replay.Summary {
		meta.Summary = append(meta.Summary, s.Text)
	}
	return fantasy.ReasoningPart{
		Text:            t.Thinking,
		ProviderOptions: fantasy.ProviderOptions{fopenai.Name: meta},
	}, true
}

func responsesToolPart(c protocol.ToolCall, source, target providers.Model) fantasy.ToolCallPart {
	callID, itemID := providers.SplitToolCallID(c.ID)
	args := bytes.TrimSpace(c.Arguments)
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	part := fantasy.ToolCallPart{ToolCallID: callID, ToolName: c.Name, Input: string(args)}
	if itemID == "" || providers.DropResponsesItemID(itemID, "function_call", source, target) {
		return part
	}
	part.ProviderOptions = fantasy.ProviderOptions{
		fopenai.Name: &fopenai.ResponsesToolCallMetadata{ItemID: itemID},
	}
	return part
}

func encodeResponsesToolResult(m protocol.ToolResultMessage) fantasy.Message {
	callID, _ := providers.SplitToolCallID(m.ToolCallID)
	var texts []string
	hasImage := false
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
			hasImage = true
		case *protocol.Image:
			hasImage = hasImage || v != nil
		}
	}
	content := strings.Join(texts, "\n")
	if content == "" {
		if hasImage {
			content = "(see attached image)"
		} else {
			content = "(no tool output)"
		}
	}
	return fantasy.Message{
		Role: fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{
			fantasy.ToolResultPart{
				ToolCallID: callID,
				Output:     fantasy.ToolResultOutputContentText{Text: content},
			},
		},
	}
}
