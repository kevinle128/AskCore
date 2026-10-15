package faux

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"AskCore/pkg/protocol"
)

// promptEstimate is the part of the usage that depends on the request only.
type promptEstimate struct {
	input, cacheRead, cacheWrite int64
}

func (e promptEstimate) usage(output int64) protocol.Usage {
	return protocol.Usage{
		Input:       e.input,
		Output:      output,
		CacheRead:   e.cacheRead,
		CacheWrite:  e.cacheWrite,
		TotalTokens: e.input + output + e.cacheRead + e.cacheWrite,
	}
}

// estimateTokens counts runes, not bytes or UTF-16 units: ceil(runes/4).
func estimateTokens(s string) int64 {
	return int64((utf8.RuneCountInString(s) + 3) / 4)
}

// estimatePrompt gives the prompt part of the usage and, when the session
// caches, updates the cached prompt. The caller holds the provider lock.
func (p *Provider) estimatePrompt(sessionID, retention string, prompt string, update bool) promptEstimate {
	tokens := estimateTokens(prompt)
	e := promptEstimate{input: tokens}
	if !update || sessionID == "" || retention == "none" {
		return e
	}
	if prev, ok := p.cache[sessionID]; ok && prev != "" {
		n := commonPrefix(prev, prompt)
		e.cacheRead = estimateTokens(prev[:n])
		e.cacheWrite = estimateTokens(prompt[n:])
		e.input = max(0, tokens-e.cacheRead)
	} else {
		e.cacheWrite = tokens
	}
	p.cache[sessionID] = prompt
	return e
}

// commonPrefix returns the byte length of the common prefix of a and b, cut
// back to a rune boundary.
func commonPrefix(a, b string) int {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	for i > 0 && i < len(a) && !utf8.RuneStart(a[i]) {
		i--
	}
	return i
}

// serializeTranscript writes the transcript as role: lines separated by a
// blank line. It is the text that the usage estimate counts.
func serializeTranscript(msgs []protocol.Message) string {
	parts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		role, text := messageText(m)
		parts = append(parts, role+":"+text)
	}
	return strings.Join(parts, "\n\n")
}

func messageText(m protocol.Message) (role, text string) {
	switch v := m.(type) {
	case protocol.SystemMessage:
		return protocol.RoleSystem, systemText(v)
	case *protocol.SystemMessage:
		return protocol.RoleSystem, systemText(*v)
	case protocol.UserMessage:
		return protocol.RoleUser, userText(v.Content)
	case *protocol.UserMessage:
		return protocol.RoleUser, userText(v.Content)
	case protocol.AssistantMessage:
		return protocol.RoleAssistant, assistantText(v.Content)
	case *protocol.AssistantMessage:
		return protocol.RoleAssistant, assistantText(v.Content)
	case protocol.ToolResultMessage:
		return protocol.RoleToolResult, toolResultText(v)
	case *protocol.ToolResultMessage:
		return protocol.RoleToolResult, toolResultText(*v)
	case nil:
		return "", ""
	default:
		return m.Role(), ""
	}
}

func systemText(m protocol.SystemMessage) string {
	var texts []string
	for _, t := range m.Content {
		texts = append(texts, t.Text)
	}
	parts := []string{strings.Join(texts, "\n")}
	for _, s := range m.Sections {
		if s.Value != nil {
			parts = append(parts, *s.Value)
		}
	}
	lines := []string{joinNonEmpty(parts, "\n\n")}
	for _, r := range m.ToolsRemoved {
		lines = append(lines, "tool-:"+jsonText(r))
	}
	for _, d := range m.ToolsAdded {
		lines = append(lines, "tool+:"+jsonText(d))
	}
	return joinNonEmpty(lines, "\n")
}

func userText(blocks []protocol.UserBlock) string {
	lines := make([]string, 0, len(blocks))
	for _, b := range blocks {
		lines = append(lines, userBlockText(b))
	}
	return strings.Join(lines, "\n")
}

func userBlockText(b protocol.UserBlock) string {
	switch v := b.(type) {
	case protocol.Text:
		return v.Text
	case protocol.Image:
		return fmt.Sprintf("[image:%s:%d]", v.MimeType, len(v.Data))
	}
	return ""
}

func assistantText(blocks []protocol.AssistantBlock) string {
	lines := make([]string, 0, len(blocks))
	for _, b := range blocks {
		switch v := b.(type) {
		case protocol.Text:
			lines = append(lines, v.Text)
		case protocol.Thinking:
			lines = append(lines, v.Thinking)
		case protocol.ToolCall:
			lines = append(lines, v.Name+":"+compactJSON(v.Arguments))
		}
	}
	return strings.Join(lines, "\n")
}

func toolResultText(m protocol.ToolResultMessage) string {
	lines := []string{m.ToolName}
	for _, b := range m.Content {
		lines = append(lines, userBlockText(b))
	}
	return strings.Join(lines, "\n")
}

func joinNonEmpty(parts []string, sep string) string {
	kept := parts[:0:0]
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}

func jsonText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// compactJSON compacts valid JSON and gives {} for empty arguments. Invalid
// text is kept as it is.
func compactJSON(raw []byte) string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "{}"
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}
