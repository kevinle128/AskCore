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

type promptItem struct {
	name, snippet string
	guidelines    []string
}

// promptItems returns the tools of snap that contribute a snippet, sorted by
// name, so the registration order never reaches the prompt.
func promptItems(snap *Snapshot) []promptItem {
	if snap == nil {
		return nil
	}
	var items []promptItem
	for _, e := range snap.entries {
		pc, ok := e.tool.(PromptContributor)
		if !ok {
			continue
		}
		snippet := strings.TrimSpace(pc.PromptSnippet())
		if snippet == "" {
			continue
		}
		items = append(items, promptItem{name: e.decl.Name, snippet: snippet, guidelines: pc.PromptGuidelines()})
	}
	slices.SortFunc(items, func(a, b promptItem) int { return strings.Compare(a.name, b.name) })
	return items
}

// PromptList renders the "Available tools:" list of the tools in snap that
// contribute a snippet, sorted by name. It returns "" when no tool contributes.
func PromptList(snap *Snapshot) string {
	items := promptItems(snap)
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Available tools:")
	for _, it := range items {
		b.WriteString("\n- " + it.name + ": " + it.snippet)
	}
	return b.String()
}

// PromptGuidelines renders the "Guidelines:" block of the tools in snap that
// contribute a snippet. Tools are sorted by name and a repeated guideline
// appears once. It returns "" when there is no guideline.
func PromptGuidelines(snap *Snapshot) string {
	var guidelines []string
	for _, it := range promptItems(snap) {
		for _, g := range it.guidelines {
			if g = strings.TrimSpace(g); g != "" && !slices.Contains(guidelines, g) {
				guidelines = append(guidelines, g)
			}
		}
	}
	if len(guidelines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Guidelines:")
	for _, g := range guidelines {
		b.WriteString("\n- " + g)
	}
	return b.String()
}
