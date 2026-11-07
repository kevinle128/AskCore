package protocol

import (
	"html"
	"path/filepath"
	"strings"
)

const (
	// SkillCommandPrefix starts a prompt that loads a skill: "/skill:<name>
	// <arguments>".
	SkillCommandPrefix = "/skill:"

	skillOpen  = `<skill name="`
	skillClose = "\n</skill>"
)

// SkillInput builds the text that replaces "/skill:<name> <args>" in the
// session. body is the SKILL.md text after its frontmatter. name and location
// go into attributes, so the quote and the angle brackets are escaped. body
// and args go in as they are: the user asked for them.
func SkillInput(name, location, body, args string) string {
	var b strings.Builder
	b.WriteString(skillOpen + html.EscapeString(name) + `" location="` + html.EscapeString(location) + "\">\n")
	b.WriteString("References are relative to " + filepath.Dir(location) + ".\n\n")
	b.WriteString(body)
	b.WriteString(skillClose)
	if args != "" {
		b.WriteString("\n\n" + args)
	}
	return b.String()
}

// SkillInputCommand reads a text made by SkillInput back into what the user
// typed, "/skill:<name>" or "/skill:<name> <args>". It reports false for any
// other text. A TUI shows this form in place of the whole skill body.
func SkillInputCommand(text string) (string, bool) {
	rest, ok := strings.CutPrefix(text, skillOpen)
	if !ok {
		return "", false
	}
	name, _, ok := strings.Cut(rest, `" location="`)
	if !ok {
		return "", false
	}
	cmd := SkillCommandPrefix + html.UnescapeString(name)
	if strings.HasSuffix(text, skillClose) {
		return cmd, true
	}
	if i := strings.LastIndex(text, skillClose+"\n\n"); i >= 0 {
		return cmd + " " + text[i+len(skillClose)+2:], true
	}
	return "", false
}
