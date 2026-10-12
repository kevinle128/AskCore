package tools

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"AskCore/pkg/protocol"
)

// findFixture is a small repository with ignore rules at two levels, hidden
// files, and a nested re-include.
func findFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeSearchTree(t, root, map[string]string{
		".git/HEAD":         "ref: refs/heads/main\n",
		".gitignore":        "*.log\nbuild/\n",
		"README.md":         "r",
		".env.example":      "k=v",
		"app.log":           "x",
		"build/out.go":      "x",
		"src/a.go":          "a",
		"src/deep/b.go":     "b",
		"src/deep/c.md":     "c",
		"src/.hidden/d.go":  "d",
		"sub/.gitignore":    "!keep.log\n*.tmp\n",
		"sub/keep.log":      "k",
		"sub/drop.log":      "d",
		"sub/skip.tmp":      "t",
		"sub/inner/e.go":    "e",
		"sub/inner/f.tmp":   "t",
		"sub/inner/g.log":   "l",
		"docs/guide/one.md": "1",
	})
	return root
}

func runFind(t *testing.T, f Find, cwd string, args map[string]any) (string, protocol.ToolResultDetails) {
	t.Helper()
	res, err := f.Execute(context.Background(), Context{Cwd: cwd}, searchArgs(args))
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	return searchText(t, res), searchDetails(t, res)
}

func TestFindUsesFdWhenPresent(t *testing.T) {
	f := Find{lookPath: realLookPath(t, "fd")}
	root := findFixture(t)
	text, details := runFind(t, f, root, map[string]any{"pattern": "*.go"})
	want := "src/.hidden/d.go\nsrc/a.go\nsrc/deep/b.go\nsub/inner/e.go"
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
	if details.Kind != protocol.ToolKindSearch {
		t.Fatalf("kind = %q", details.Kind)
	}
}

func TestFindKeepsUnicodeAndSpacedNamesAcrossSources(t *testing.T) {
	for _, source := range []string{"fd", "rg", "walk"} {
		t.Run(source, func(t *testing.T) {
			f := Find{lookPath: fixedLookPath(nil)}
			if source != "walk" {
				f.lookPath = realLookPath(t, source)
			}
			root := t.TempDir()
			writeSearchTree(t, root, map[string]string{
				"café.txt": "keep", "秘密.txt": "ignore", " file.txt ": "spaced",
				".gitignore": "秘密.txt\n",
			})
			for _, name := range []string{"café.txt", " file.txt "} {
				text, _ := runFind(t, f, root, map[string]any{"pattern": name})
				if text != name {
					t.Errorf("find(%q) = %q", name, text)
				}
			}
			text, _ := runFind(t, f, root, map[string]any{"pattern": "秘密.txt"})
			if text != "No files found matching pattern" {
				t.Errorf("ignored file found: %q", text)
			}
		})
	}
}

func TestFindPrefersFdOverRg(t *testing.T) {
	root := t.TempDir()
	fd := writeFakeBinary(t, "fd", "printf 'from-fd.txt\\n'\n")
	rg := writeFakeBinary(t, "rg", "printf 'from-rg.txt\\n'\n")
	f := Find{lookPath: fixedLookPath(map[string]string{"fd": fd, "rg": rg})}
	text, _ := runFind(t, f, root, map[string]any{"pattern": "*"})
	if text != "from-fd.txt" {
		t.Fatalf("text = %q", text)
	}
	f = Find{lookPath: fixedLookPath(map[string]string{"rg": rg})}
	text, _ = runFind(t, f, root, map[string]any{"pattern": "*"})
	if text != "from-rg.txt" {
		t.Fatalf("text = %q", text)
	}
}

func TestFindFallsBackToRgFiles(t *testing.T) {
	f := Find{lookPath: realLookPath(t, "rg")}
	root := findFixture(t)
	text, _ := runFind(t, f, root, map[string]any{"pattern": "*.go"})
	want := "src/.hidden/d.go\nsrc/a.go\nsrc/deep/b.go\nsub/inner/e.go"
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
}

func TestFindFallsBackToGoWalkWithGitignore(t *testing.T) {
	f := Find{lookPath: fixedLookPath(nil)}
	root := findFixture(t)
	text, _ := runFind(t, f, root, map[string]any{"pattern": "*"})
	want := strings.Join([]string{
		".env.example",
		".gitignore",
		"README.md",
		"docs/guide/one.md",
		"src/.hidden/d.go",
		"src/a.go",
		"src/deep/b.go",
		"src/deep/c.md",
		"sub/.gitignore",
		"sub/inner/e.go",
		"sub/keep.log",
	}, "\n")
	if text != want {
		t.Fatalf("text =\n%s\nwant\n%s", text, want)
	}
}

func TestFindAllChainsGiveSameResult(t *testing.T) {
	root := findFixture(t)
	type chain struct {
		name string
		look func(string) (string, error)
	}
	chains := []chain{{"walk", fixedLookPath(nil)}}
	for _, name := range []string{"fd", "rg"} {
		if p, err := exec.LookPath(name); err == nil {
			chains = append(chains, chain{name, fixedLookPath(map[string]string{name: p})})
		}
	}
	if len(chains) < 2 {
		t.Skip("neither fd nor rg is on PATH")
	}
	for _, pattern := range []string{"*", "*.go", "*.{go,md}", "src/**", "src/**/*.go", "**/*.md", "*.log", "inner/*.go"} {
		var first []string
		for i, c := range chains {
			text, _ := runFind(t, Find{lookPath: c.look}, root, map[string]any{"pattern": pattern})
			lines := strings.Split(text, "\n")
			if !slices.IsSorted(lines) {
				t.Errorf("%s %q: output is not sorted: %v", c.name, pattern, lines)
			}
			if i == 0 {
				first = lines
				continue
			}
			if !slices.Equal(lines, first) {
				t.Errorf("pattern %q: %s gives %v, walk gives %v", pattern, c.name, lines, first)
			}
		}
	}
}

func TestFindLimitNotice(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{}
	for i := range 1500 {
		files[fmt.Sprintf("d%02d/f%04d.txt", i%10, i)] = "x"
	}
	writeSearchTree(t, root, files)
	chains := map[string]func(string) (string, error){"walk": fixedLookPath(nil)}
	if p, err := exec.LookPath("rg"); err == nil {
		chains["rg"] = fixedLookPath(map[string]string{"rg": p})
	}
	for name, look := range chains {
		text, details := runFind(t, Find{lookPath: look}, root, map[string]any{"pattern": "*.txt"})
		lines := strings.Split(text, "\n")
		notice := "[1000 results limit reached. Use limit=2000 for more, or refine pattern]"
		if len(lines) != 1002 || lines[1000] != "" || lines[1001] != notice {
			t.Errorf("%s: %d lines, tail %q", name, len(lines), lines[len(lines)-2:])
		}
		if details.Kind != protocol.ToolKindSearch {
			t.Errorf("%s: kind = %q", name, details.Kind)
		}
	}
}

func TestFindHonoursExplicitLimitWithoutNoticeAtExactCount(t *testing.T) {
	root := t.TempDir()
	writeSearchTree(t, root, map[string]string{"a.txt": "", "b.txt": "", "c.txt": ""})
	f := Find{lookPath: fixedLookPath(nil)}
	text, _ := runFind(t, f, root, map[string]any{"pattern": "*.txt", "limit": 3})
	if text != "a.txt\nb.txt\nc.txt" {
		t.Fatalf("text = %q", text)
	}
	text, _ = runFind(t, f, root, map[string]any{"pattern": "*.txt", "limit": 2})
	if want := "a.txt\nb.txt\n\n[2 results limit reached. Use limit=4 for more, or refine pattern]"; text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
}

func TestFindNoMatchText(t *testing.T) {
	root := t.TempDir()
	writeSearchTree(t, root, map[string]string{"a.txt": ""})
	text, _ := runFind(t, Find{lookPath: fixedLookPath(nil)}, root, map[string]any{"pattern": "*.zzz"})
	if text != "No files found matching pattern" {
		t.Fatalf("text = %q", text)
	}
}

func TestFindSearchesGivenPathAndReportsMissingPath(t *testing.T) {
	root := findFixture(t)
	f := Find{lookPath: fixedLookPath(nil)}
	text, details := runFind(t, f, root, map[string]any{"pattern": "*.go", "path": "src/deep"})
	if text != "b.go" {
		t.Fatalf("text = %q", text)
	}
	if want := filepath.Join(root, "src", "deep"); details.Path != want {
		t.Fatalf("details path = %q, want %q", details.Path, want)
	}
	_, err := f.Execute(context.Background(), Context{Cwd: root}, searchArgs(map[string]any{"pattern": "*", "path": "nope"}))
	if err == nil || !strings.Contains(err.Error(), "path not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestFindTimeoutReturnsPartialResult(t *testing.T) {
	root := t.TempDir()
	pidFile := filepath.Join(root, "pid")
	fd := writeFakeBinary(t, "fd", "echo $$ > "+pidFile+"\nprintf 'found.txt\\n'\nsleep 60\n")
	f := Find{lookPath: fixedLookPath(map[string]string{"fd": fd}), timeout: 1500 * time.Millisecond}
	text, _ := runFind(t, f, root, map[string]any{"pattern": "*"})
	if want := "found.txt\n\n[search stopped after 1.5 s]"; text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
	assertGroupGone(t, pidFile)
}

func TestFindReportsExternalFailure(t *testing.T) {
	root := t.TempDir()
	fd := writeFakeBinary(t, "fd", "echo 'bad pattern' >&2\nexit 1\n")
	f := Find{lookPath: fixedLookPath(map[string]string{"fd": fd})}
	_, err := f.Execute(context.Background(), Context{Cwd: root}, searchArgs(map[string]any{"pattern": "*"}))
	if err == nil || !strings.Contains(err.Error(), "bad pattern") {
		t.Fatalf("error = %v", err)
	}
}

func TestFindDecl(t *testing.T) {
	f := Find{}
	if f.Decl().Name != "find" {
		t.Fatalf("name = %q", f.Decl().Name)
	}
	for _, name := range []string{"pattern", "path", "limit"} {
		if !strings.Contains(string(f.Decl().Parameters), `"`+name+`"`) {
			t.Errorf("missing parameter %q", name)
		}
	}
	if !f.ConcurrencySafe(nil) {
		t.Error("find must be concurrency safe")
	}
	var _ PromptContributor = f
	if f.PromptSnippet() == "" {
		t.Error("empty prompt snippet")
	}
}
