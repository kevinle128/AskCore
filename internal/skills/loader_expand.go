package skills

import (
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"AskCore/pkg/protocol"
)

// maxBodyBytes caps the text of one SKILL.md body that Expand puts into a prompt.
const maxBodyBytes = 100 << 10

const truncationNote = "\n\n[The skill text is cut here at 100 KB. Read the SKILL.md file at the location above for the rest.]"

// Expand turns a prompt that starts with "/skill:<name>" into the skill block
// followed by the arguments, see protocol.SkillInput. The name ends at the first
// white space and the rest of the text, trimmed, is the arguments. The first
// skill of r with that name wins, so a project skill beats a user skill. It
// reports false, and the caller keeps the text, when the text is no skill
// command, no skill has the name, or the file cannot be read now. A skill with
// disable-model-invocation expands too: asking for it by name is its purpose.
// The body is the text after the frontmatter, cut at 100 KB on a rune boundary
// with a note that says so.
func Expand(text string, r Result) (string, bool) {
	rest, ok := strings.CutPrefix(text, protocol.SkillCommandPrefix)
	if !ok {
		return "", false
	}
	name, args := rest, ""
	if i := strings.IndexFunc(rest, unicode.IsSpace); i >= 0 {
		name, args = rest[:i], strings.TrimSpace(rest[i:])
	}
	if name == "" {
		return "", false
	}
	for _, s := range r.Skills {
		if s.Name != name {
			continue
		}
		body, ok := readBody(s.File)
		if !ok {
			return "", false
		}
		return protocol.SkillInput(s.Name, s.File, body, args), true
	}
	return "", false
}

func readBody(file string) (string, bool) {
	f, err := os.Open(file)
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()
	const limit = maxFrontmatterBytes + maxBodyBytes + 1
	raw, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return "", false
	}
	_, body, reason := cutFrontmatter(raw, len(raw) == limit)
	if reason != "" {
		return "", false
	}
	text := strings.TrimSpace(string(body))
	if len(text) <= maxBodyBytes && len(raw) < limit {
		return text, true
	}
	cut := min(len(text), maxBodyBytes)
	for cut > 0 && cut < len(text) && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + truncationNote, true
}
