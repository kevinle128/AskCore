package skills

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	maxNameRunes = 64
	skillFile    = "SKILL.md"
)

// limits bound the walk of one root.
type limits struct {
	depth, dirs, entries int
}

var walkLimits = limits{depth: 6, dirs: 2000, entries: 20000}

// Scope tells where a skills folder comes from.
type Scope uint8

const (
	ScopeUser Scope = iota + 1
	ScopeProject
)

// Root is one skills folder.
type Root struct {
	Dir   string
	Scope Scope
}

// Skill is the metadata of one loaded skill.
type Skill struct {
	// Name is the frontmatter name or the folder name, on one line and cut to
	// 64 runes. Two skills can share a name.
	Name string
	// Description is on one line and has no length cap; Listing caps it.
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
	// Skills is sorted by scope (project first), name and path.
	Skills []Skill
	// Diagnostics is in walk order.
	Diagnostics []Diagnostic
}

type loader struct {
	limits  limits
	visited map[string]struct{}
	res     Result
}

// rootWalk counts the work spent on one root.
type rootWalk struct {
	scope   Scope
	dirs    int
	entries int
	stopped bool
}

// Load walks each root in sorted order, at most six folder levels deep, up to
// 2000 folders and 20000 entries per root; a root that reaches a limit stops
// without an error. A folder that holds SKILL.md is a skill and is not
// entered further. Hidden folders below a root are skipped. Symbolic links
// are followed; a set of canonical folder paths breaks loops and keeps a
// SKILL.md that two roots reach once. A missing root is not an error.
func Load(roots []Root) Result { return load(roots, walkLimits) }

func load(roots []Root, lim limits) Result {
	l := loader{limits: lim, visited: map[string]struct{}{}}
	for _, r := range roots {
		dir, err := filepath.Abs(r.Dir)
		if err != nil {
			continue
		}
		l.walk(dir, &rootWalk{scope: r.Scope}, 0)
	}
	slices.SortStableFunc(l.res.Skills, func(a, b Skill) int {
		if a.Scope != b.Scope {
			return int(b.Scope) - int(a.Scope)
		}
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.File, b.File)
	})
	return l.res
}

func (l *loader) walk(dir string, w *rootWalk, depth int) {
	if w.stopped {
		return
	}
	canon, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return
	}
	if _, seen := l.visited[canon]; seen {
		return
	}
	l.visited[canon] = struct{}{}
	if w.dirs++; w.dirs > l.limits.dirs {
		w.stopped = true
		return
	}

	file := filepath.Join(dir, skillFile)
	if info, err := os.Stat(file); err == nil && info.Mode().IsRegular() {
		l.add(file, filepath.Base(dir), w.scope)
		return
	}
	if depth == l.limits.depth {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if w.entries++; w.entries > l.limits.entries {
			w.stopped = true
			return
		}
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		child := filepath.Join(dir, e.Name())
		if info, err := os.Stat(child); err == nil && info.IsDir() {
			l.walk(child, w, depth+1)
		}
	}
}

func (l *loader) add(file, folder string, scope Scope) {
	s, reason := parse(file, folder)
	if reason != "" {
		l.res.Diagnostics = append(l.res.Diagnostics, Diagnostic{Path: file, Scope: scope, Reason: reason})
		return
	}
	s.Scope = scope
	l.res.Skills = append(l.res.Skills, s)
}
