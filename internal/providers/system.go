package providers

import (
	"strings"

	"AskCore/pkg/protocol"
)

// SystemPromptText is the leading system message as one string: content
// blocks, then section values, joined by blank lines. Empty parts drop out.
func SystemPromptText(m protocol.SystemMessage) string {
	parts := make([]string, 0, 1+len(m.Sections))
	if t := joinSystemContent(m.Content); t != "" {
		parts = append(parts, t)
	}
	for _, sec := range m.Sections {
		if sec.Value == nil || *sec.Value == "" {
			continue
		}
		parts = append(parts, *sec.Value)
	}
	return strings.Join(parts, "\n\n")
}

// SystemUpdateText is a later system message for APIs that accept mid-
// conversation updates. Section changes are named so the model can relate
// them to the leading prompt.
func SystemUpdateText(m protocol.SystemMessage) string {
	parts := make([]string, 0, 1+len(m.Sections))
	if t := joinSystemContent(m.Content); t != "" {
		parts = append(parts, t)
	}
	for _, sec := range m.Sections {
		if sec.Value == nil {
			parts = append(parts, `Removed system prompt section "`+sec.Name+`".`)
			continue
		}
		parts = append(parts, `Updated system prompt section "`+sec.Name+`":`+"\n\n"+*sec.Value)
	}
	return strings.Join(parts, "\n\n")
}

func joinSystemContent(blocks []protocol.Text) string {
	parts := make([]string, 0, len(blocks))
	for _, t := range blocks {
		if strings.TrimSpace(t.Text) == "" {
			continue
		}
		parts = append(parts, t.Text)
	}
	return strings.Join(parts, "\n")
}
