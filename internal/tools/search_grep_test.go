package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"AskCore/pkg/protocol"
)

func TestGrepJSONPreviewKeepsBoundedValidEvents(t *testing.T) {
	for _, text := range []string{
		strings.Repeat("x", 5<<20),
		strings.Repeat("秘密", 5000),
		strings.Repeat(`\"`+"\n", 5000),
	} {
		raw, err := json.Marshal(map[string]any{"type": "match", "data": map[string]any{
			"path":  map[string]string{"text": "/tmp/a.txt"},
			"lines": map[string]string{"text": text}, "line_number": 3,
			"submatches": []any{map[string]any{"match": map[string]string{"text": text}, "start": 1, "end": len(text)}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		var p grepJSONPreview
		var out bytes.Buffer
		for _, b := range raw {
			out.Write(p.write([]byte{b}))
		}
		if out.Len() > 10<<10 {
			t.Fatalf("preview uses %d bytes", out.Len())
		}
		var event rgEvent
		if err := json.Unmarshal(out.Bytes(), &event); err != nil {
			t.Fatalf("invalid preview: %v", err)
		}
		if event.Type != "match" || event.Data.LineNumber != 3 || event.Data.Path.String() != "/tmp/a.txt" {
			t.Fatalf("event metadata changed: %+v", event)
		}
		got, cut := cutLine(event.Data.Lines.String(), grepMaxLineChars)
		want, _ := cutLine(text, grepMaxLineChars)
		if !cut || got != want {
			t.Fatalf("preview does not preserve the first 500 characters")
		}
	}
}

func TestGrepJSONPreviewKeepsEscapedAndBinaryContext(t *testing.T) {
	for _, tc := range []struct{ name, lines, text string }{
		{"unicode escapes", `{"text":"` + strings.Repeat(`\u79d8\u5bc6`, 5000) + `"}`, strings.Repeat("秘密", 5000)},
		{"binary bytes", `{"bytes":"` + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 10000)+"\xff")) + `"}`, strings.Repeat("x", 10000) + "�"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"type":"context","data":{"path":{"text":"/tmp/a.txt"},"lines":` + tc.lines + `,"line_number":2,"submatches":[` +
				strings.Repeat(`{"match":{"text":"[\\\" nested"},"start":0,"end":1},`, 100000) + `{}]}}` + "\n"
			var p grepJSONPreview
			var out bytes.Buffer
			for start := 0; start < len(raw); start += 7 {
				out.Write(p.write([]byte(raw[start:min(start+7, len(raw))])))
			}
			if out.Len() > 10<<10 {
				t.Fatalf("preview uses %d bytes", out.Len())
			}
			var event rgEvent
			if err := json.Unmarshal(out.Bytes(), &event); err != nil {
				t.Fatalf("invalid preview: %v", err)
			}
			if event.Type != "context" || event.Data.LineNumber != 2 {
				t.Fatalf("event metadata changed: %+v", event)
			}
			got, cut := cutLine(event.Data.Lines.String(), grepMaxLineChars)
			want, _ := cutLine(tc.text, grepMaxLineChars)
			if !cut || got != want {
				t.Fatal("preview does not preserve the first 500 characters")
			}
		})
	}
}

// writeSearchTree creates the files of a fixture below root.
func writeSearchTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func searchArgs(m map[string]any) json.RawMessage {
	raw, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return raw
}

func searchText(t *testing.T, res protocol.ToolExecutionResult) string {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("result has %d content blocks, want 1", len(res.Content))
	}
	text, ok := res.Content[0].(protocol.Text)
	if !ok {
		t.Fatalf("content block is %T, want protocol.Text", res.Content[0])
	}
	return text.Text
}

func searchDetails(t *testing.T, res protocol.ToolExecutionResult) protocol.ToolResultDetails {
	t.Helper()
	var d protocol.ToolResultDetails
	if err := json.Unmarshal(res.Details, &d); err != nil {
		t.Fatalf("details %q: %v", res.Details, err)
	}
	return d
}

// realLookPath resolves only the named binaries on the real PATH. A name that
// is not listed is reported as missing, which selects the next fallback.
func realLookPath(t *testing.T, only ...string) func(string) (string, error) {
	t.Helper()
	found := map[string]string{}
	for _, name := range only {
		p, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("%s is not on PATH", name)
		}
		found[name] = p
	}
	return fixedLookPath(found)
}

func fixedLookPath(found map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if p, ok := found[name]; ok {
			return p, nil
		}
		return "", exec.ErrNotFound
	}
}

// writeFakeBinary writes an executable shell script that stands for an
// external search program, and returns its path.
func writeFakeBinary(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func waitForFile(path string) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := os.Stat(path); err == nil && st.Size() > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// assertGroupGone reads the process group id the fake program wrote and
// checks that no process of the group is left.
func assertGroupGone(t *testing.T, pidFile string) {
	t.Helper()
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("pid file %q: %v", raw, err)
	}
	if err := syscall.Kill(-pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("process group %d still exists: %v", pid, err)
	}
}

func runGrep(t *testing.T, g Grep, cwd string, args map[string]any) (string, protocol.ToolResultDetails) {
	t.Helper()
	res, err := g.Execute(context.Background(), Context{Cwd: cwd}, searchArgs(args))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	return searchText(t, res), searchDetails(t, res)
}

func TestGrepFormatsMatchesWithRelativePaths(t *testing.T) {
	g := Grep{lookPath: realLookPath(t, "rg")}
	root := t.TempDir()
	writeSearchTree(t, root, map[string]string{
		"a.txt":     "hello world\nnothing here\n",
		"sub/b.txt": "say hello\n",
	})
	text, details := runGrep(t, g, root, map[string]any{"pattern": "hello"})
	if want := "a.txt:1: hello world\nsub/b.txt:1: say hello"; text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
	if details.Kind != protocol.ToolKindSearch {
		t.Fatalf("kind = %q", details.Kind)
	}
	want := []protocol.ToolLocation{
		{Path: filepath.Join(root, "a.txt"), Line: 1},
		{Path: filepath.Join(root, "sub", "b.txt"), Line: 1},
	}
	if len(details.Locations) != 2 || details.Locations[0] != want[0] || details.Locations[1] != want[1] {
		t.Fatalf("locations = %+v, want %+v", details.Locations, want)
	}
}

func TestGrepSearchesASingleFileByBasename(t *testing.T) {
	g := Grep{lookPath: realLookPath(t, "rg")}
	root := t.TempDir()
	writeSearchTree(t, root, map[string]string{"sub/b.txt": "say hello\n"})
	text, _ := runGrep(t, g, root, map[string]any{"pattern": "hello", "path": "sub/b.txt"})
	if want := "b.txt:1: say hello"; text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
}

func TestGrepKeepsFlagLikePatternLiteral(t *testing.T) {
	g := Grep{lookPath: realLookPath(t, "rg")}
	root := t.TempDir()
	writeSearchTree(t, root, map[string]string{"f.txt": "a -e b\nuse --files here\nplain\n"})
	for pattern, want := range map[string]string{
		"-e":      "f.txt:1: a -e b",
		"--files": "f.txt:2: use --files here",
	} {
		text, _ := runGrep(t, g, root, map[string]any{"pattern": pattern})
		if text != want {
			t.Errorf("pattern %q: text = %q, want %q", pattern, text, want)
		}
	}
}

func TestGrepLiteralAndIgnoreCaseFlags(t *testing.T) {
	g := Grep{lookPath: realLookPath(t, "rg")}
	root := t.TempDir()
	writeSearchTree(t, root, map[string]string{"f.txt": "a.c\nabc\nA.C\n"})
	text, _ := runGrep(t, g, root, map[string]any{"pattern": "a.c", "literal": true})
	if want := "f.txt:1: a.c"; text != want {
		t.Errorf("literal: text = %q, want %q", text, want)
	}
	text, _ = runGrep(t, g, root, map[string]any{"pattern": "a.c", "literal": true, "ignoreCase": true})
	if want := "f.txt:1: a.c\nf.txt:3: A.C"; text != want {
		t.Errorf("literal ignoreCase: text = %q, want %q", text, want)
	}
	text, _ = runGrep(t, g, root, map[string]any{"pattern": "a.c"})
	if want := "f.txt:1: a.c\nf.txt:2: abc"; text != want {
		t.Errorf("regex: text = %q, want %q", text, want)
	}
}

func TestGrepGlobFiltersFiles(t *testing.T) {
	g := Grep{lookPath: realLookPath(t, "rg")}
	root := t.TempDir()
	writeSearchTree(t, root, map[string]string{"a.go": "needle\n", "b.md": "needle\n"})
	text, _ := runGrep(t, g, root, map[string]any{"pattern": "needle", "glob": "*.go"})
	if want := "a.go:1: needle"; text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
}

func TestGrepRespectsGitignoreAndShowsHidden(t *testing.T) {
	g := Grep{lookPath: realLookPath(t, "rg")}
	root := t.TempDir()
	writeSearchTree(t, root, map[string]string{
		".git/HEAD":    "ref: refs/heads/main\n",
		".gitignore":   "ignored.txt\n",
		"ignored.txt":  "needle\n",
		".env.example": "needle=1\n",
		"plain.txt":    "needle\n",
	})
	text, _ := runGrep(t, g, root, map[string]any{"pattern": "needle"})
	if strings.Contains(text, "ignored.txt") {
		t.Errorf("an ignored file was searched:\n%s", text)
	}
	if !strings.Contains(text, ".env.example:1: needle=1") {
		t.Errorf("hidden file missing:\n%s", text)
	}
	if strings.Contains(text, ".git/") {
		t.Errorf("the .git directory was searched:\n%s", text)
	}
}

// fakeRgMatches returns a script that stands for rg: it writes its group id,
// prints n match events for path, and then waits so that only a kill ends it.
func fakeRgMatches(pidFile, path string, n int) string {
	return fmt.Sprintf(`echo $$ > %s
i=1
while [ $i -le %d ]; do
  printf '{"type":"match","data":{"path":{"text":"%s"},"lines":{"text":"hit %%d\\n"},"line_number":%%d}}\n' $i $i
  i=$((i+1))
done
sleep 60
`, pidFile, n, path)
}

func TestGrepStopsEarlyAtLimit(t *testing.T) {
	root := t.TempDir()
	pidFile := filepath.Join(root, "pid")
	rg := writeFakeBinary(t, "rg", fakeRgMatches(pidFile, filepath.Join(root, "big.txt"), 500))
	g := Grep{lookPath: fixedLookPath(map[string]string{"rg": rg})}

	start := time.Now()
	text, details := runGrep(t, g, root, map[string]any{"pattern": "hit", "limit": 10})
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("grep waited %v instead of stopping at the limit", d)
	}
	lines := strings.Split(text, "\n")
	if len(lines) < 10 || lines[0] != "big.txt:1: hit 1" || lines[9] != "big.txt:10: hit 10" {
		t.Fatalf("unexpected output:\n%s", text)
	}
	if strings.Contains(text, "hit 11") {
		t.Fatalf("output goes past the limit:\n%s", text)
	}
	notice := "[Found at least 10 matches; showing the first 10. Refine the pattern or use path/glob.]"
	if !strings.HasSuffix(text, "\n\n"+notice) {
		t.Fatalf("missing limit notice:\n%s", text)
	}
	if len(details.Locations) != 10 {
		t.Fatalf("locations = %d, want 10", len(details.Locations))
	}
	assertGroupGone(t, pidFile)
}

func TestGrepLimitNoticeWithRealRg(t *testing.T) {
	g := Grep{lookPath: realLookPath(t, "rg")}
	root := t.TempDir()
	var b strings.Builder
	for i := range 500 {
		fmt.Fprintf(&b, "hit %d\n", i)
	}
	writeSearchTree(t, root, map[string]string{"big.txt": b.String()})
	text, _ := runGrep(t, g, root, map[string]any{"pattern": "hit", "limit": 10})
	if got := strings.Count(text, "big.txt:"); got != 10 {
		t.Fatalf("match lines = %d, want 10:\n%s", got, text)
	}
	if !strings.Contains(text, "Found at least 10 matches; showing the first 10.") {
		t.Fatalf("missing limit notice:\n%s", text)
	}
}

func TestGrepCutsLongLines(t *testing.T) {
	g := Grep{lookPath: realLookPath(t, "rg")}
	root := t.TempDir()
	long := "needle" + strings.Repeat("x", 2000)
	writeSearchTree(t, root, map[string]string{"long.txt": long + "\n"})
	text, _ := runGrep(t, g, root, map[string]any{"pattern": "needle"})
	wantLine := "long.txt:1: " + long[:500] + "... [truncated]"
	if !strings.HasPrefix(text, wantLine+"\n") {
		t.Fatalf("line not cut at 500 chars:\n%.200s", text)
	}
	if !strings.Contains(text, "[Some lines truncated to 500 chars. Use read tool to see full lines]") {
		t.Fatalf("missing line notice:\n%.300s", text)
	}
}

func TestGrepContextLines(t *testing.T) {
	g := Grep{lookPath: realLookPath(t, "rg")}
	root := t.TempDir()
	writeSearchTree(t, root, map[string]string{"f.txt": "l1\nl2\nl3 needle\nl4\nl5\n"})
	text, details := runGrep(t, g, root, map[string]any{"pattern": "needle", "context": 1})
	want := "f.txt-2- l2\nf.txt:3: l3 needle\nf.txt-4- l4"
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
	if len(details.Locations) != 1 || details.Locations[0].Line != 3 {
		t.Fatalf("locations = %+v, want only the match line", details.Locations)
	}
}

func TestGrepKeepsTrailingContextOfLastMatch(t *testing.T) {
	g := Grep{lookPath: realLookPath(t, "rg")}
	root := t.TempDir()
	writeSearchTree(t, root, map[string]string{"f.txt": "needle 1\nafter 1\nneedle 2\nafter 2\n"})
	text, _ := runGrep(t, g, root, map[string]any{"pattern": "needle", "context": 1, "limit": 1})
	if !strings.HasPrefix(text, "f.txt:1: needle 1\nf.txt-2- after 1") {
		t.Fatalf("trailing context lost:\n%s", text)
	}
}

func TestGrepNoMatchText(t *testing.T) {
	g := Grep{lookPath: realLookPath(t, "rg")}
	root := t.TempDir()
	writeSearchTree(t, root, map[string]string{"f.txt": "nothing\n"})
	text, _ := runGrep(t, g, root, map[string]any{"pattern": "absent"})
	if text != "No matches found" {
		t.Fatalf("text = %q", text)
	}
}

func TestGrepWithoutRgReportsError(t *testing.T) {
	g := Grep{lookPath: fixedLookPath(nil)}
	_, err := g.Execute(context.Background(), Context{Cwd: t.TempDir()}, searchArgs(map[string]any{"pattern": "x"}))
	if err == nil || !strings.Contains(err.Error(), "grep needs ripgrep (rg) on PATH") {
		t.Fatalf("error = %v", err)
	}
}

func TestGrepReportsBadRegexAndMissingPath(t *testing.T) {
	g := Grep{lookPath: realLookPath(t, "rg")}
	root := t.TempDir()
	writeSearchTree(t, root, map[string]string{"f.txt": "x\n"})
	_, err := g.Execute(context.Background(), Context{Cwd: root}, searchArgs(map[string]any{"pattern": "("}))
	if err == nil || !strings.Contains(err.Error(), "regex parse error") {
		t.Errorf("bad regex error = %v", err)
	}
	_, err = g.Execute(context.Background(), Context{Cwd: root}, searchArgs(map[string]any{"pattern": "x", "path": "nope"}))
	if err == nil || !strings.Contains(err.Error(), "path not found") {
		t.Errorf("missing path error = %v", err)
	}
}

func TestGrepTimeoutReturnsPartialMatches(t *testing.T) {
	root := t.TempDir()
	pidFile := filepath.Join(root, "pid")
	body := fmt.Sprintf(`echo $$ > %s
printf '{"type":"match","data":{"path":{"text":"%s"},"lines":{"text":"hit\\n"},"line_number":1}}\n'
sleep 60
`, pidFile, filepath.Join(root, "f.txt"))
	rg := writeFakeBinary(t, "rg", body)
	g := Grep{lookPath: fixedLookPath(map[string]string{"rg": rg}), timeout: 1500 * time.Millisecond}
	text, _ := runGrep(t, g, root, map[string]any{"pattern": "hit"})
	if want := "f.txt:1: hit\n\n[search stopped after 1.5 s]"; text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
	assertGroupGone(t, pidFile)
}

func TestGrepParsesEventsSplitAcrossReads(t *testing.T) {
	root := t.TempDir()
	// The event is longer than one pipe read, so it arrives in several chunks.
	long := strings.Repeat("y", 100*1024)
	body := fmt.Sprintf(`printf '{"type":"match","data":{"path":{"text":"%s"},"lines":{"text":"%s\\n"},"line_number":7}}\n'
`, filepath.Join(root, "f.txt"), long)
	rg := writeFakeBinary(t, "rg", body)
	g := Grep{lookPath: fixedLookPath(map[string]string{"rg": rg})}
	text, _ := runGrep(t, g, root, map[string]any{"pattern": "y"})
	if !strings.HasPrefix(text, "f.txt:7: yyyy") || !strings.Contains(text, "... [truncated]") {
		t.Fatalf("unexpected text: %.120s", text)
	}
}

func TestGrepDecl(t *testing.T) {
	g := Grep{}
	if g.Decl().Name != "grep" {
		t.Fatalf("name = %q", g.Decl().Name)
	}
	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(g.Decl().Parameters, &schema); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pattern", "path", "glob", "ignoreCase", "literal", "context", "limit"} {
		if _, ok := schema.Properties[name]; !ok {
			t.Errorf("missing parameter %q", name)
		}
	}
	if len(schema.Required) != 1 || schema.Required[0] != "pattern" {
		t.Errorf("required = %v", schema.Required)
	}
	if !g.ConcurrencySafe(nil) {
		t.Error("grep must be concurrency safe")
	}
	var _ PromptContributor = g
	if g.PromptSnippet() == "" {
		t.Error("empty prompt snippet")
	}
}
