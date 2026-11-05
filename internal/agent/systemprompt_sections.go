package agent

import (
	"fmt"
	"strings"
)

// SectionName names one part of the system prompt. It matches ^[a-z][a-z0-9_-]*$.
type SectionName string

const (
	// SectionPreamble is the default preamble, or the custom prompt.
	SectionPreamble SectionName = "preamble"
	// SectionTools lists the tools of the session.
	SectionTools SectionName = "tools"
	// SectionRules holds the guidelines of the tools and the Ask rules.
	SectionRules SectionName = "rules"
	// SectionDocs points to the Ask docs.
	SectionDocs SectionName = "docs"
	// SectionAddendum holds APPEND_SYSTEM.md and each --append-system-prompt.
	SectionAddendum SectionName = "addendum"
	// SectionCwd is the working directory of the session.
	SectionCwd SectionName = "cwd"
	// SectionDate is the ISO date of the session start. It is the most volatile
	// section, so it is last.
	SectionDate SectionName = "date"
)

// sections lists every section in render order. Orders are unique and at least
// 10 apart, so a later section can go between two of them. baseline marks the
// sections of the default prompt that a custom prompt drops. cwd and date come
// last, so every byte before them is the same for sessions that differ only in
// directory and date, which keeps the provider's prompt cache valid. A test
// pins the order, the gaps and the names.
var sections = [...]struct {
	name     SectionName
	order    int
	baseline bool
}{
	{SectionPreamble, 100, false},
	{SectionTools, 200, true},
	{SectionRules, 300, true},
	{SectionDocs, 400, true},
	{SectionAddendum, 500, false},
	{SectionCwd, 9000, false},
	{SectionDate, 9010, false},
}

const defaultPreamble = "You are Ask, a coding agent harness. You help the user read, change and run code in their project. Use the tools to look at the project before you answer, and to make changes. Be concise."

const docsText = "Ask documentation: docs/cli.md describes the command line and docs/README.md lists the other docs. Read them only when the user asks about Ask itself."

// PromptParts are the inputs of one system prompt. A field that is empty after
// trimming gives no section.
type PromptParts struct {
	// Custom, when not blank, replaces the preamble text and drops every
	// baseline section (tools, rules and docs).
	Custom string
	Tools  string
	Rules  string
	// Addendum entries render in order; blank entries are dropped.
	Addendum []string
	Cwd      string
	// Date is the ISO date "2006-01-02".
	Date string
}

// SystemPrompt renders the non-empty sections in registry order, joined by a
// blank line.
func SystemPrompt(p PromptParts) string {
	custom := strings.TrimSpace(p.Custom) != ""
	var out []string
	for _, s := range sections {
		if custom && s.baseline {
			continue
		}
		if text := textOf(s.name, p); strings.TrimSpace(text) != "" {
			out = append(out, text)
		}
	}
	return strings.Join(out, "\n\n")
}

// textOf maps a section to its text. A section with no text gives "".
func textOf(name SectionName, p PromptParts) string {
	switch name {
	case SectionPreamble:
		if strings.TrimSpace(p.Custom) != "" {
			return p.Custom
		}
		return defaultPreamble
	case SectionTools:
		return p.Tools
	case SectionRules:
		return p.Rules
	case SectionDocs:
		return docsText
	case SectionAddendum:
		var parts []string
		for _, a := range p.Addendum {
			if strings.TrimSpace(a) != "" {
				parts = append(parts, a)
			}
		}
		return strings.Join(parts, "\n\n")
	case SectionCwd:
		if strings.TrimSpace(p.Cwd) == "" {
			return ""
		}
		return "Current working directory: " + p.Cwd
	case SectionDate:
		if strings.TrimSpace(p.Date) == "" {
			return ""
		}
		return "Current date: " + p.Date
	default:
		panic(fmt.Sprintf("agent: no text for system prompt section %q", name))
	}
}
