package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestGlobMatchDoubleStar(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"**/*.go", "a.go", true},
		{"**/*.go", "src/a.go", true},
		{"**/*.go", "src/x/a.go", true},
		{"**/*.go", "a.md", false},
		{"src/**", "src/a.go", true},
		{"src/**", "src/x/a.go", true},
		{"src/**", "other/a.go", false},
		{"src/**/*.go", "src/a.go", true},
		{"src/**/*.go", "src/x/y/a.go", true},
		{"*.{go,md}", "a.go", true},
		{"*.{go,md}", "a.md", true},
		{"*.{go,md}", "a.txt", false},
		{"*.{go,md}", "src/a.go", false},
		{"*", "a/b", false},
		{"a?c", "abc", true},
		{"a?c", "a/c", false},
		{"[a-c]x", "bx", true},
		{"[a-c]x", "dx", false},
		{"[!a]x", "bx", true},
		{"[!a]x", "ax", false},
		{`a\*b`, "a*b", true},
		{`a\*b`, "axb", false},
		{"a.b", "axb", false},
		{"café.*", "café.txt", true},
		{"秘密.*", "秘密.txt", true},
		{`\é.*`, "é.txt", true},
	}
	for _, c := range cases {
		if got := globMatch(c.pattern, c.name); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

func TestFindMatcherUsesBasenameWithoutSlash(t *testing.T) {
	cases := []struct {
		pattern, rel string
		want         bool
	}{
		{"*.go", "a.go", true},
		{"*.go", "src/deep/a.go", true},
		{"*.go", "src/a.md", false},
		{"src/*.go", "src/a.go", true},
		{"src/*.go", "x/src/a.go", true},
		{"src/*.go", "src/x/a.go", false},
		{"src/**/*.go", "lib/a.go", false},
		{"**", "a/b/c", true},
	}
	for _, c := range cases {
		if got := findMatcher(c.pattern)(c.rel); got != c.want {
			t.Errorf("findMatcher(%q)(%q) = %v, want %v", c.pattern, c.rel, got, c.want)
		}
	}
}

func TestGitignoreRules(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "repo")
	f := parseIgnore(base, "# comment\n\n*.log\n!keep.log\nbuild/\n/root-only.txt\ndocs/*.tmp\n**/cache\n\\#hash.txt\n")
	cases := []struct {
		rel   string
		isDir bool
		want  bool
	}{
		{"a.log", false, true},
		{"x/y/a.log", false, true},
		{"keep.log", false, false},
		{"x/keep.log", false, false},
		{"build", true, true},
		{"build", false, false},
		{"x/build", true, true},
		{"root-only.txt", false, true},
		{"sub/root-only.txt", false, false},
		{"docs/x.tmp", false, true},
		{"a/docs/x.tmp", false, false},
		{"x/y/cache", true, true},
		{"cache", true, true},
		{"#hash.txt", false, true},
		{"src/main.go", false, false},
	}
	for _, c := range cases {
		got, _ := f.verdict(filepath.Join(base, filepath.FromSlash(c.rel)), c.isDir)
		if got != c.want {
			t.Errorf("ignored(%q, dir=%v) = %v, want %v", c.rel, c.isDir, got, c.want)
		}
	}
	if _, decided := f.verdict(filepath.Join(base, "src", "main.go"), false); decided {
		t.Error("a path no rule matches must be undecided")
	}
}

func TestWalkSkipsGitDirectoryAndSymlinks(t *testing.T) {
	root := t.TempDir()
	writeSearchTree(t, root, map[string]string{
		".git/config": "x",
		"a.txt":       "a",
		"sub/b.txt":   "b",
	})
	if err := os.Symlink(filepath.Join(root, "a.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlink: %v", err)
	}
	got, err := walkFind(context.Background(), root, findMatcher("*"), 100)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	if want := []string{"a.txt", "sub/b.txt"}; !slices.Equal(got, want) {
		t.Fatalf("walk = %v, want %v", got, want)
	}
}

func TestWalkAppliesParentGitignoreInsideRepository(t *testing.T) {
	root := t.TempDir()
	writeSearchTree(t, root, map[string]string{
		".git/HEAD":     "ref: refs/heads/main",
		".gitignore":    "*.tmp\n",
		"src/a.go":      "a",
		"src/skip.tmp":  "x",
		"src/deep/b.go": "b",
	})
	got, err := walkFind(context.Background(), filepath.Join(root, "src"), findMatcher("*"), 100)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	if want := []string{"a.go", "deep/b.go"}; !slices.Equal(got, want) {
		t.Fatalf("walk = %v, want %v", got, want)
	}
}

func TestWalkStopsAfterLimitPlusOne(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{}
	for i := range 30 {
		files[fmt.Sprintf("f%02d.txt", i)] = "x"
	}
	writeSearchTree(t, root, files)
	got, err := walkFind(context.Background(), root, findMatcher("*"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 6 {
		t.Fatalf("walk returned %d paths, want limit+1 = 6", len(got))
	}
}

func TestSearchToolsReturnOnCancelledContext(t *testing.T) {
	t.Run("walk", func(t *testing.T) {
		root := t.TempDir()
		files := map[string]string{}
		for i := range 200 {
			files[fmt.Sprintf("d%03d/f.txt", i)] = "x"
		}
		writeSearchTree(t, root, files)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		dirs := 0
		w := &walker{root: root, match: findMatcher("*"), limit: 1000, onDir: func(string) {
			if dirs++; dirs == 3 {
				cancel()
			}
		}}
		_, err := w.run(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("walk error = %v, want context.Canceled", err)
		}
		if dirs > 4 {
			t.Fatalf("walk visited %d directories after the cancel", dirs)
		}
	})

	t.Run("grep", func(t *testing.T) {
		root := t.TempDir()
		pidFile := filepath.Join(root, "pid")
		rg := writeFakeBinary(t, "rg", "echo $$ > "+pidFile+"\nsleep 60\n")
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			waitForFile(pidFile)
			cancel()
		}()
		start := time.Now()
		_, err := Grep{lookPath: fixedLookPath(map[string]string{"rg": rg})}.Execute(ctx, Context{Cwd: root}, searchArgs(map[string]any{"pattern": "x"}))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("grep error = %v, want context.Canceled", err)
		}
		if d := time.Since(start); d > 10*time.Second {
			t.Fatalf("grep returned after %v", d)
		}
		assertGroupGone(t, pidFile)
	})

	t.Run("find", func(t *testing.T) {
		root := t.TempDir()
		pidFile := filepath.Join(root, "pid")
		fd := writeFakeBinary(t, "fd", "echo $$ > "+pidFile+"\nsleep 60\n")
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			waitForFile(pidFile)
			cancel()
		}()
		start := time.Now()
		_, err := Find{lookPath: fixedLookPath(map[string]string{"fd": fd})}.Execute(ctx, Context{Cwd: root}, searchArgs(map[string]any{"pattern": "*"}))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("find error = %v, want context.Canceled", err)
		}
		if d := time.Since(start); d > 10*time.Second {
			t.Fatalf("find returned after %v", d)
		}
		assertGroupGone(t, pidFile)
	})
}
