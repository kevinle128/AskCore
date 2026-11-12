package skills

import (
	"fmt"
	"slices"
	"unicode/utf8"

	"AskCore/pkg/protocol"
)

const (
	maxListedDescription = 1024
	descriptionEllipsis  = "..."
	listingHeader        = "The skills below give specialized instructions. When a description matches the task, read the SKILL.md file at its location before you act.\n<available_skills>\n"
	listingFooter        = "</available_skills>"
)

// Report counts what Listing did with the model-invocable skills.
type Report struct {
	// Total is the number of model-invocable skills.
	Total int
	// Included is the number of skills that have an entry in the listing.
	Included int
	// Omitted is the number of skills that have no entry.
	Omitted int
	// TruncatedDescriptions is the number of skills whose description in the
	// listing is shorter than the loaded one, an omitted skill included.
	TruncatedDescriptions int
}

// Warning is the text for the user when skills were left out, or "".
// Truncated descriptions alone give no warning.
func (r Report) Warning() string {
	if r.Omitted == 0 {
		return ""
	}
	n := fmt.Sprintf("%d additional skill was", r.Omitted)
	if r.Omitted != 1 {
		n = fmt.Sprintf("%d additional skills were", r.Omitted)
	}
	return "Exceeded skills context budget. All skill descriptions were removed and " + n + " not included in the model-visible skills list."
}

// entry is one skill of the listing with the cost of its text.
type entry struct {
	s     Skill
	desc  []rune
	shown int
	// capped is set when the 1024 rune cap cut the description.
	capped bool
	// quoteExtra[n] is how many bytes, and as many runes, that Quote adds to
	// the first n runes of desc: an escaped tag grows by three. It is nil when
	// the description holds no "<", so most entries pay nothing.
	quoteExtra []int
	// fixedBytes and fixedRunes measure the entry text without its
	// description and its closing newline, which cost charges as the line
	// break.
	fixedBytes, fixedRunes int
}

// Listing renders the model-invocable skills as one system reminder within
// the budget, and returns "" when there is none. The three steps: every
// description (capped at 1024 runes) fits; or the names fit and the room that
// is left is shared over the descriptions, one rune at a time round-robin;
// or only the first skills fit with no description, and the rest are left
// out. The line that counts the left-out skills is charged to the budget, and
// the last entries are dropped until it fits.
func (r Result) Listing(b Budget) (string, Report) {
	var es []entry
	for _, s := range r.Skills {
		if s.ModelInvocable {
			es = append(es, newEntry(s))
		}
	}
	rep := Report{Total: len(es)}
	if len(es) == 0 {
		return "", rep
	}

	full, minimum := 0, 0
	for i := range es {
		full += b.cost(&es[i], len(es[i].desc), es[i].descBytes(len(es[i].desc)))
		minimum += b.cost(&es[i], 0, 0)
	}
	included := len(es)
	used := full
	switch {
	case full <= b.limit():
		for i := range es {
			es[i].shown = len(es[i].desc)
		}
	case minimum <= b.limit():
		used = shareDescriptions(es, b)
	default:
		used = 0
		included = 0
		for i := range es {
			c := b.cost(&es[i], 0, 0)
			if used+c > b.limit() {
				break
			}
			used += c
			included++
		}
	}

	omitted := len(es) - included
	for omitted > 0 {
		line := omissionLine(omitted)
		if used+b.lineCost(line) <= b.limit() || included == 0 {
			break
		}
		included--
		omitted++
		used -= b.cost(&es[included], 0, 0)
	}

	var out protocol.ReminderText
	out.Frame(listingHeader)
	for i := range es[:included] {
		writeEntry(&out, es[i].s, string(es[i].desc[:es[i].shown]))
	}
	if omitted > 0 {
		out.Frame(omissionLine(omitted) + "\n")
	}
	out.Frame(listingFooter)

	rep.Included, rep.Omitted = included, omitted
	for i, e := range es {
		if i >= included || e.shown < len(e.desc) || e.capped {
			rep.TruncatedDescriptions++
		}
	}
	return out.String(), rep
}

func omissionLine(n int) string {
	if n == 1 {
		return "- 1 additional skill omitted from this bounded skills list."
	}
	return fmt.Sprintf("- %d additional skills omitted from this bounded skills list.", n)
}

func newEntry(s Skill) entry {
	e := entry{s: s}
	d := []rune(oneLine(s.Description))
	e.desc = d
	if len(d) > maxListedDescription {
		e.desc = []rune(string(d[:maxListedDescription-len(descriptionEllipsis)]) + descriptionEllipsis)
		e.capped = true
	}
	e.quoteExtra = quoteExtraOf(e.desc)
	var empty, r protocol.ReminderText
	writeEntry(&r, s, "")
	text, wrap := r.String(), empty.String()
	e.fixedBytes = len(text) - len(wrap) - 1
	e.fixedRunes = utf8.RuneCountInString(text) - utf8.RuneCountInString(wrap) - 1
	return e
}

func (e *entry) descBytes(n int) int { return len(string(e.desc[:n])) }

// escaped is the growth that Quote causes in the first n runes of the
// description.
func (e *entry) escaped(n int) int {
	if e.quoteExtra == nil {
		return 0
	}
	return e.quoteExtra[n]
}

// quoteExtraOf measures the growth of every prefix of desc through Quote, the
// call that writes the entry. Quote escapes a tag only when the whole tag is
// in the text, and a tag ends in "r", so only a prefix that ends in "r" or "R"
// can differ from the shorter prefix before it.
func quoteExtraOf(desc []rune) []int {
	if !slices.Contains(desc, '<') {
		return nil
	}
	var empty protocol.ReminderText
	wrap := empty.Len()
	extra := make([]int, len(desc)+1)
	cur := 0
	for n := 1; n <= len(desc); n++ {
		if r := desc[n-1]; r == 'r' || r == 'R' {
			raw := string(desc[:n])
			var q protocol.ReminderText
			q.Quote(raw)
			cur = q.Len() - wrap - len(raw)
		}
		extra[n] = cur
	}
	return extra
}

func writeEntry(r *protocol.ReminderText, s Skill, desc string) {
	r.Frame("<skill>\n<name>")
	r.Quote(s.Name)
	r.Frame("</name>\n<description>")
	r.Quote(desc)
	r.Frame("</description>\n<location>")
	r.Quote(s.File)
	r.Frame("</location>\n</skill>\n")
}
