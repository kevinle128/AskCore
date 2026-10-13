package tools

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// globRegexp turns a glob into an anchored regular expression over a
// slash-separated path. "*" and "?" stop at "/", "**" as a whole path segment
// crosses it ("**/" also matches no directory), "[...]" is a class, "{a,b}" an
// alternation, and a backslash escapes the next character.
func globRegexp(glob string) string {
	var b strings.Builder
	b.WriteString("(?s)^")
	depth := 0
	for i := 0; i < len(glob); {
		c := glob[i]
		switch c {
		case '*':
			j := i
			for j < len(glob) && glob[j] == '*' {
				j++
			}
			atStart := i == 0 || strings.IndexByte("/{,", glob[i-1]) >= 0
			atEnd := j == len(glob) || glob[j] == '/' || glob[j] == ',' || glob[j] == '}'
			switch {
			case j-i >= 2 && atStart && atEnd && j < len(glob) && glob[j] == '/':
				b.WriteString("(?:.*/)?")
				j++
			case j-i >= 2 && atStart && atEnd:
				b.WriteString(".*")
			default:
				b.WriteString("[^/]*")
			}
			i = j
		case '?':
			b.WriteString("[^/]")
			i++
		case '[':
			j := i + 1
			if j < len(glob) && (glob[j] == '!' || glob[j] == '^') {
				j++
			}
			if j < len(glob) && glob[j] == ']' {
				j++
			}
			for j < len(glob) && glob[j] != ']' {
				j++
			}
			if j >= len(glob) {
				b.WriteString(`\[`)
				i++
				break
			}
			class := glob[i+1 : j]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i = j + 1
		case '{':
			depth++
			b.WriteString("(?:")
			i++
		case '}':
			if depth > 0 {
				depth--
				b.WriteString(")")
			} else {
				b.WriteString(`\}`)
			}
			i++
		case ',':
			if depth > 0 {
				b.WriteString("|")
			} else {
				b.WriteString(",")
			}
			i++
		case '\\':
			if i+1 < len(glob) {
				_, size := utf8.DecodeRuneInString(glob[i+1:])
				b.WriteString(regexp.QuoteMeta(glob[i+1 : i+1+size]))
				i += 1 + size
			} else {
				b.WriteString(`\\`)
				i++
			}
		default:
			_, size := utf8.DecodeRuneInString(glob[i:])
			b.WriteString(regexp.QuoteMeta(glob[i : i+size]))
			i += size
		}
	}
	for ; depth > 0; depth-- {
		b.WriteString(")")
	}
	b.WriteString("$")
	return b.String()
}

// compileGlob compiles glob. A glob that does not compile matches nothing.
func compileGlob(glob string) *regexp.Regexp {
	re, err := regexp.Compile(globRegexp(glob))
	if err != nil {
		return nil
	}
	return re
}

// globMatch reports whether the slash-separated name matches glob as a whole.
func globMatch(glob, name string) bool {
	re := compileGlob(glob)
	return re != nil && re.MatchString(name)
}

// findMatcher returns the test the find tool applies to a path relative to the
// search root. It follows fd --glob: a pattern without "/" matches the file
// name, and a pattern with "/" matches the whole path at any depth.
func findMatcher(pattern string) func(rel string) bool {
	if !strings.Contains(pattern, "/") {
		re := compileGlob(pattern)
		return func(rel string) bool {
			return re != nil && re.MatchString(path.Base(rel))
		}
	}
	re := compileGlob(fullPathPattern(pattern))
	return func(rel string) bool { return re != nil && re.MatchString(rel) }
}

// fullPathPattern adds the leading "**/" that lets a pattern with a slash
// match below the search root.
func fullPathPattern(pattern string) string {
	if strings.HasPrefix(pattern, "/") || strings.HasPrefix(pattern, "**/") || pattern == "**" {
		return pattern
	}
	return "**/" + pattern
}

type ignoreRule struct {
	re       *regexp.Regexp
	negate   bool
	dirOnly  bool
	anchored bool
}

// ignoreFile holds the rules of one .gitignore. They apply to the paths below
// dir only.
type ignoreFile struct {
	dir   string
	rules []ignoreRule
}

// parseIgnore reads the .gitignore subset the walk supports: comments, "!"
// negation, a trailing "/" for directories, a leading or inner "/" that
// anchors the pattern to dir, and "**".
func parseIgnore(dir, text string) *ignoreFile {
	f := &ignoreFile{dir: dir}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var r ignoreRule
		if strings.HasPrefix(line, "!") {
			r.negate = true
			line = line[1:]
		}
		if strings.HasPrefix(line, `\#`) || strings.HasPrefix(line, `\!`) {
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			r.dirOnly = true
			line = strings.TrimRight(line, "/")
		}
		if line == "" {
			continue
		}
		r.anchored = strings.Contains(line, "/")
		line = strings.TrimPrefix(line, "/")
		if r.re = compileGlob(line); r.re == nil {
			continue
		}
		f.rules = append(f.rules, r)
	}
	return f
}

// loadIgnore reads dir/.gitignore. It returns nil when the file is absent,
// unreadable or holds no rule.
func loadIgnore(dir string) *ignoreFile {
	raw, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return nil
	}
	f := parseIgnore(dir, string(raw))
	if len(f.rules) == 0 {
		return nil
	}
	return f
}

// verdict applies the rules to the absolute path abs below f.dir. The last
// matching rule wins. decided is false when no rule matched.
func (f *ignoreFile) verdict(abs string, isDir bool) (ignored, decided bool) {
	rel, err := filepath.Rel(f.dir, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false, false
	}
	rel = filepath.ToSlash(rel)
	base := path.Base(rel)
	for _, r := range f.rules {
		if r.dirOnly && !isDir {
			continue
		}
		target := base
		if r.anchored {
			target = rel
		}
		if r.re.MatchString(target) {
			ignored, decided = !r.negate, true
		}
	}
	return ignored, decided
}

// gitRepoRoot returns the nearest directory at or above dir that holds a .git
// entry, or "" when there is none.
func gitRepoRoot(dir string) string {
	for cur := dir; ; {
		if _, err := os.Lstat(filepath.Join(cur, ".git")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

// walker lists the regular files below root that no .gitignore excludes and
// match returns true for.
type walker struct {
	root  string
	match func(rel string) bool
	// limit stops the walk once more than limit paths matched, so the caller
	// can tell that the list is cut.
	limit int
	// onDir is called before a directory is read.
	onDir func(dir string)

	files   []string
	ignores []*ignoreFile
}

// walkFind runs a walker over root and returns the matches as slash-separated
// paths relative to root. The result is in walk order, not sorted. On a context
// error it returns the paths found so far together with the error.
func walkFind(ctx context.Context, root string, match func(string) bool, limit int) ([]string, error) {
	w := &walker{root: root, match: match, limit: limit}
	return w.run(ctx)
}

func (w *walker) run(ctx context.Context) ([]string, error) {
	w.files, w.ignores = nil, nil
	// Inside a repository the rules of the directories above root still apply.
	if top := gitRepoRoot(w.root); top != "" && top != w.root {
		var chain []string
		for cur := filepath.Dir(w.root); ; cur = filepath.Dir(cur) {
			chain = append(chain, cur)
			if cur == top {
				break
			}
		}
		for i := len(chain) - 1; i >= 0; i-- {
			if f := loadIgnore(chain[i]); f != nil {
				w.ignores = append(w.ignores, f)
			}
		}
	}
	err := w.walk(ctx, w.root)
	return w.files, err
}

func (w *walker) ignored(abs string, isDir bool) bool {
	result := false
	for _, f := range w.ignores {
		if ig, decided := f.verdict(abs, isDir); decided {
			result = ig
		}
	}
	return result
}

func (w *walker) walk(ctx context.Context, dir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w.onDir != nil {
		w.onDir(dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		// An unreadable directory is left out, as fd and rg do.
		return nil
	}
	if f := loadIgnore(dir); f != nil {
		w.ignores = append(w.ignores, f)
		defer func() { w.ignores = w.ignores[:len(w.ignores)-1] }()
	}
	for _, e := range entries {
		if len(w.files) > w.limit {
			return nil
		}
		name := e.Name()
		if name == ".git" {
			continue
		}
		abs := filepath.Join(dir, name)
		switch {
		case e.Type()&os.ModeSymlink != 0:
			continue
		case e.IsDir():
			if w.ignored(abs, true) {
				continue
			}
			if err := w.walk(ctx, abs); err != nil {
				return err
			}
		case e.Type().IsRegular():
			if w.ignored(abs, false) {
				continue
			}
			rel, err := filepath.Rel(w.root, abs)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			if w.match(rel) {
				w.files = append(w.files, rel)
			}
		}
	}
	return nil
}
