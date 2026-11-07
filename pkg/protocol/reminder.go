package protocol

import (
	"regexp"
	"strings"
)

// Custom types of the seed items that open a session log. A client can tell
// them apart from human input by CustomType.
const (
	// CustomContextFiles is the seed item that holds the AGENTS.md and
	// CLAUDE.md files.
	CustomContextFiles = "context-file"
	// CustomSkillList is the seed item that holds the skill catalog.
	CustomSkillList = "skill-list"
)

var reminderTag = regexp.MustCompile(`(?i)<(\s*/?\s*system[-_]reminder)`)

// ReminderText builds the body of a system reminder from two kinds of text.
// Frame is trusted text that the harness wrote. Quote is untrusted text, such
// as file content, a path or a skill description: it cannot open or close a
// system reminder. The zero value is ready to use; do not copy it.
type ReminderText struct{ b strings.Builder }

// Frame appends trusted text as it is.
func (r *ReminderText) Frame(s string) { r.b.WriteString(s) }

// Quote appends untrusted text. Every case-insensitive match of a
// system-reminder tag, with any space around the slash and either "-" or "_"
// in the name, loses its "<" to "&lt;". The result holds no match, so quoting
// it again changes nothing.
func (r *ReminderText) Quote(s string) { r.b.WriteString(reminderTag.ReplaceAllString(s, "&lt;$1")) }

// Len is the length in bytes of String, the wrapper included. A budget counts
// with it.
func (r *ReminderText) Len() int {
	return len(systemReminderOpen) + r.b.Len() + len(systemReminderClose)
}

// String returns the text wrapped by SystemReminder.
func (r *ReminderText) String() string { return SystemReminder(r.b.String()) }
