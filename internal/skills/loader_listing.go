package skills

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"AskCore/pkg/protocol"
)

const (
	// MaxListingBytes caps the text that Listing returns, wrapper included.
	MaxListingBytes = 16 << 10

	maxListedDescription = 500
	listingHeader        = "The skills below give specialized instructions. When a description matches the task, read the SKILL.md file at its location before you act.\n<available_skills>\n"
	listingFooter        = "</available_skills>"
)

// Listing renders the model-invocable skills as one system reminder. It
// returns "" when there is none. Skills that do not fit in MaxListingBytes are
// replaced by one line that gives their number.
func (r Result) Listing() string {
	var invocable []Skill
	for _, s := range r.Skills {
		if s.ModelInvocable {
			invocable = append(invocable, s)
		}
	}
	if len(invocable) == 0 {
		return ""
	}

	var head protocol.ReminderText
	head.Frame(listingHeader)
	used := head.Len() + len(listingFooter)
	listed := 0
	for i, s := range invocable {
		need := used + entryLen(s)
		if rest := len(invocable) - i - 1; rest > 0 && need+len(moreLine(rest)) > MaxListingBytes {
			break
		}
		if need > MaxListingBytes {
			break
		}
		used = need
		listed++
	}

	var out protocol.ReminderText
	out.Frame(listingHeader)
	for _, s := range invocable[:listed] {
		writeEntry(&out, s)
	}
	out.Frame(listingFooter)
	if rest := len(invocable) - listed; rest > 0 {
		out.Frame(moreLine(rest))
	}
	return out.String()
}

func moreLine(n int) string {
	return fmt.Sprintf("\n%d more skills are not listed.", n)
}

func entryLen(s Skill) int {
	var empty, r protocol.ReminderText
	writeEntry(&r, s)
	return r.Len() - empty.Len()
}

func writeEntry(r *protocol.ReminderText, s Skill) {
	r.Frame("<skill>\n<name>")
	r.Quote(s.Name)
	r.Frame("</name>\n<description>")
	r.Quote(listedDescription(s.Description))
	r.Frame("</description>\n<location>")
	r.Quote(s.File)
	r.Frame("</location>\n</skill>\n")
}

func listedDescription(d string) string {
	d = strings.Join(strings.Fields(d), " ")
	if len(d) <= maxListedDescription {
		return d
	}
	n := maxListedDescription
	for n > 0 && !utf8.RuneStart(d[n]) {
		n--
	}
	return d[:n]
}
