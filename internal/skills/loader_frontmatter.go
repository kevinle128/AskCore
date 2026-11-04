package skills

import (
	"bytes"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxFrontmatterBytes = 4096

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
		return Skill{}, "bad frontmatter"
	}
	invocable := true
	if n := fm.DisableModelInvocation; n.Kind != 0 {
		var disabled bool
		if n.Tag != "!!bool" || n.Decode(&disabled) != nil {
			return Skill{}, "bad boolean disable-model-invocation"
		}
		invocable = !disabled
	}
	name := fm.Name
	if name == "" {
		name = folder
	}
	if len(name) > maxNameBytes || !namePattern.MatchString(name) {
		return Skill{}, "bad name"
	}
	desc := strings.TrimSpace(fm.Description)
	if desc == "" {
		return Skill{}, "missing description"
	}
	if len(desc) > maxDescription {
		return Skill{}, "description over 1024 bytes"
	}
	return Skill{Name: name, Description: desc, File: file, ModelInvocable: invocable}, ""
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
	full := len(buf) == maxFrontmatterBytes
	buf = bytes.ReplaceAll(bytes.TrimPrefix(buf, []byte("\xef\xbb\xbf")), []byte("\r\n"), []byte("\n"))

	rest, ok := bytes.CutPrefix(buf, []byte("---\n"))
	if !ok {
		return nil, "no frontmatter"
	}
	for off := 0; off <= len(rest); {
		end := bytes.IndexByte(rest[off:], '\n')
		line := rest[off:]
		if end >= 0 {
			line = rest[off : off+end]
		}
		if string(line) == "---" {
			return rest[:off], ""
		}
		if end < 0 {
			break
		}
		off += end + 1
	}
	if full {
		return nil, "frontmatter over 4096 bytes"
	}
	return nil, "no frontmatter"
}
