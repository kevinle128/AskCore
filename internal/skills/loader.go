package skills

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

const (
	maxDepth       = 5
	maxNameBytes   = 64
	maxDescription = 1024
	skillFile      = "SKILL.md"
)

var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Scope tells where a skills folder comes from.
type Scope uint8

const (
	ScopeUser Scope = iota + 1
	ScopeProject
)

// Root is one skills folder. Roots are given in precedence order: the first
// skill with a name wins.
type Root struct {
	Dir   string
	Scope Scope
}

// Skill is the metadata of one loaded skill.
type Skill struct {
	// Name defaults to the folder name when the frontmatter has none.
	Name        string
	Description string
	// File is the absolute path of SKILL.md.
	File           string
	Scope          Scope
	ModelInvocable bool
}

// Diagnostic is one skill that did not load. Reason is a fixed text and never
// holds file content.
type Diagnostic struct {
	Path   string
	Scope  Scope
	Reason string
}

// Result is the outcome of Load.
type Result struct {
	// Skills holds the winners, sorted by name.
	Skills []Skill
	// Diagnostics is in walk order.
	Diagnostics []Diagnostic
}

type loader struct {
	visited map[string]struct{}
	winners map[string]string
	res     Result
}

// Load walks each root in sorted order, at most five folder levels deep. A
// folder that holds SKILL.md is a skill and is not entered further. Symbolic
// links are followed; a set of canonical paths breaks loops. A missing root
// is not an error.
func Load(roots []Root) Result {
	l := loader{visited: map[string]struct{}{}, winners: map[string]string{}}
	for _, r := range roots {
		dir, err := filepath.Abs(r.Dir)
		if err != nil {
			continue
		}
		l.walk(dir, r.Scope, 0)
	}
	sort.Slice(l.res.Skills, func(i, j int) bool { return l.res.Skills[i].Name < l.res.Skills[j].Name })
	return l.res
}

func (l *loader) walk(dir string, scope Scope, depth int) {
	canon, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return
	}
	if _, seen := l.visited[canon]; seen {
		return
	}
	l.visited[canon] = struct{}{}

	file := filepath.Join(dir, skillFile)
	if info, err := os.Stat(file); err == nil && info.Mode().IsRegular() {
		l.add(file, filepath.Base(dir), scope)
		return
	}
	if depth == maxDepth {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		child := filepath.Join(dir, e.Name())
		if info, err := os.Stat(child); err == nil && info.IsDir() {
			l.walk(child, scope, depth+1)
		}
	}
}

func (l *loader) add(file, folder string, scope Scope) {
	s, reason := parse(file, folder)
	if reason != "" {
		l.res.Diagnostics = append(l.res.Diagnostics, Diagnostic{Path: file, Scope: scope, Reason: reason})
		return
	}
	if first, ok := l.winners[s.Name]; ok {
		l.res.Diagnostics = append(l.res.Diagnostics, Diagnostic{Path: file, Scope: scope, Reason: "shadowed by " + first})
		return
	}
	s.Scope = scope
	l.winners[s.Name] = file
	l.res.Skills = append(l.res.Skills, s)
}
