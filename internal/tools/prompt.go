package tools

import (
	"slices"
	"strings"
)

// PromptContributor is implemented by a tool that adds text to the system
// prompt. PromptSnippet is one line that says what the tool does; a tool with
// an empty snippet is not listed. PromptGuidelines are rules for using it.
type PromptContributor interface {
	PromptSnippet() string
	PromptGuidelines() []string
}

// PromptSection renders the tool list and the guidelines of the tools in snap
// that contribute a snippet. Tools are sorted by name, so the registration
// order never reaches the prompt, and a repeated guideline appears once. It
// returns "" when no tool contributes.
func PromptSection(snap *Snapshot) string {
	if snap == nil {
		return ""
	}
	type item struct {
		name, snippet string
		guidelines    []string
	}
	var items []item
	for _, e := range snap.entries {
		pc, ok := e.tool.(PromptContributor)
		if !ok {
			continue
		}
		snippet := strings.TrimSpace(pc.PromptSnippet())
		if snippet == "" {
			continue
		}
		items = append(items, item{name: e.decl.Name, snippet: snippet, guidelines: pc.PromptGuidelines()})
	}
	if len(items) == 0 {
		return ""
	}
	slices.SortFunc(items, func(a, b item) int { return strings.Compare(a.name, b.name) })

	var b strings.Builder
	b.WriteString("Available tools:")
	var guidelines []string
	for _, it := range items {
		b.WriteString("\n- " + it.name + ": " + it.snippet)
		for _, g := range it.guidelines {
			if g = strings.TrimSpace(g); g != "" && !slices.Contains(guidelines, g) {
				guidelines = append(guidelines, g)
			}
		}
	}
	if len(guidelines) > 0 {
		b.WriteString("\n\nGuidelines:")
		for _, g := range guidelines {
			b.WriteString("\n- " + g)
		}
	}
	return b.String()
}
