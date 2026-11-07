package skills

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxFrontmatterBytes = 64 << 10

var frontmatterOverText = "frontmatter over " + strconv.Itoa(maxFrontmatterBytes) + " bytes"

// scalarLine matches a top-level key with a plain scalar value.
var scalarLine = regexp.MustCompile(`^([A-Za-z0-9_-]+):[ \t]+(.*\S)[ \t]*$`)

type frontmatter struct {
	Name                   string    `yaml:"name"`
	Description            string    `yaml:"description"`
	DisableModelInvocation yaml.Node `yaml:"disable-model-invocation"`
}

// parse reads the frontmatter of one SKILL.md. A non-empty reason is a fixed
// text and the skill is not loaded.
func parse(file, folder string) (Skill, string) {
	block, reason := readFrontmatter(file)
	if reason != "" {
		return Skill{}, reason
	}
	var fm frontmatter
	if err := yaml.Unmarshal(block, &fm); err != nil {
		fm = frontmatter{}
		if err := yaml.Unmarshal(repairYAML(block), &fm); err != nil {
			return Skill{}, "bad frontmatter"
		}
	}
	invocable := true
	if n := fm.DisableModelInvocation; n.Kind != 0 {
		var disabled bool
		if n.Tag != "!!bool" || n.Decode(&disabled) != nil {
			return Skill{}, "bad boolean disable-model-invocation"
		}
		invocable = !disabled
	}
	desc := oneLine(fm.Description)
	if desc == "" {
		return Skill{}, "missing description"
	}
	return Skill{Name: skillName(fm.Name, folder), Description: desc, File: file, ModelInvocable: invocable}, ""
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func skillName(name, folder string) string {
	n := oneLine(name)
	if n == "" {
		n = oneLine(folder)
	}
	if n == "" {
		n = "skill"
	}
	if r := []rune(n); len(r) > maxNameRunes {
		n = string(r[:maxNameRunes])
	}
	return n
}

// repairYAML double-quotes each top-level plain scalar that strict YAML reads
// as something else: one that holds ": " or starts with "[", "{", "@" or a
// backtick.
func repairYAML(block []byte) []byte {
	lines := strings.Split(string(block), "\n")
	for i, line := range lines {
		m := scalarLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		v := m[2]
		if !strings.Contains(v, ": ") && !strings.ContainsRune("[{@`", rune(v[0])) {
			continue
		}
		lines[i] = m[1] + ": " + strconv.Quote(v)
	}
	return []byte(strings.Join(lines, "\n"))
}

func readFrontmatter(file string) ([]byte, string) {
	f, err := os.Open(file)
	if err != nil {
		return nil, "unreadable"
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(io.LimitReader(f, maxFrontmatterBytes))
	if err != nil {
		return nil, "unreadable"
	}
	block, _, reason := cutFrontmatter(buf, len(buf) == maxFrontmatterBytes)
	return block, reason
}

// cutFrontmatter splits the text of a SKILL.md into the frontmatter block and
// the body after the closing line. full tells that buf is only the first part
// of the file, so a missing closing line may be a frontmatter that is too long.
func cutFrontmatter(buf []byte, full bool) (block, body []byte, reason string) {
	buf = bytes.ReplaceAll(bytes.TrimPrefix(buf, []byte("\xef\xbb\xbf")), []byte("\r\n"), []byte("\n"))

	rest, ok := bytes.CutPrefix(buf, []byte("---\n"))
	if !ok {
		return nil, nil, "no frontmatter"
	}
	for off := 0; off <= len(rest); {
		end := bytes.IndexByte(rest[off:], '\n')
		line := rest[off:]
		if end >= 0 {
			line = rest[off : off+end]
		}
		if string(line) == "---" {
			if end < 0 {
				return rest[:off], nil, ""
			}
			return rest[:off], rest[off+end+1:], ""
		}
		if end < 0 {
			break
		}
		off += end + 1
	}
	if full {
		return nil, nil, frontmatterOverText
	}
	return nil, nil, "no frontmatter"
}
