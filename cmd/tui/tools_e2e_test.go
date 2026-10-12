package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/jobs"
	"AskCore/internal/providers"
	"AskCore/internal/providers/anthropic"
)

// The helpers below serve the end-to-end tests of the built-in tools. The
// model is a scripted Anthropic Messages server; everything behind it (the
// adapter, the Agent, the tools, the runner and the job table) is the product
// code.

// e2eWaitFor bounds every wait on a file or a process in these tests.
const e2eWaitFor = 30 * time.Second

// modelStep is one scripted reply of the model.
type modelStep struct {
	// before runs when the request arrives and before the reply is sent. It
	// gets the request body. A step uses it to hold the reply until a job has
	// reached a state, or to look at the world while the Agent waits.
	before func(body []byte)
	reply  string
}

// scriptedAnthropic answers the Anthropic Messages requests in order and keeps
// every request body. A request past the last step fails the test.
type scriptedAnthropic struct {
	t     *testing.T
	steps []modelStep

	mu     sync.Mutex
	bodies [][]byte
}

// scriptModel returns the scripted model for steps. Tool call ids are made
// unique per step.
func scriptModel(t *testing.T, steps ...modelStep) *scriptedAnthropic {
	t.Helper()
	for i := range steps {
		steps[i].reply = strings.Replace(steps[i].reply, `"call_1"`, `"call_`+strconv.Itoa(i+1)+`"`, 1)
	}
	return &scriptedAnthropic{t: t, steps: steps}
}

func (m *scriptedAnthropic) RoundTrip(r *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	n := len(m.bodies)
	m.bodies = append(m.bodies, body)
	m.mu.Unlock()
	if n >= len(m.steps) {
		m.t.Errorf("the model got request %d, but the script has %d steps", n+1, len(m.steps))
		return sseResponse(http.StatusOK, sseText("unexpected request"))(r)
	}
	step := m.steps[n]
	if step.before != nil {
		runBefore(step.before, body)
	}
	return sseResponse(http.StatusOK, step.reply)(r)
}

// runBefore runs the hook of a step in a goroutine of its own. A require in the
// hook ends that goroutine with runtime.Goexit; run on the goroutine of the
// HTTP transport, it would end the model stream without a result and the run
// would wait for ever instead of failing.
func runBefore(before func(body []byte), body []byte) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		before(body)
	}()
	<-done
}

// requestCount is the number of requests that the model has received.
func (m *scriptedAnthropic) requestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.bodies)
}

func (m *scriptedAnthropic) body(i int) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	require.Greater(m.t, len(m.bodies), i, "request %d was not sent", i+1)
	return m.bodies[i]
}

// wireBlock is one content block of an Anthropic request message.
type wireBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
	Source    struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	} `json:"source"`
}

// wireMessage is one message of an Anthropic request.
type wireMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// blocks decodes a content field that holds a string or an array of blocks.
func blocks(t *testing.T, raw json.RawMessage) []wireBlock {
	t.Helper()
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []wireBlock{{Type: "text", Text: s}}
	}
	var out []wireBlock
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

// requestMessages returns the messages of request i.
func (m *scriptedAnthropic) requestMessages(i int) []wireMessage {
	var req struct {
		Messages []wireMessage `json:"messages"`
	}
	require.NoError(m.t, json.Unmarshal(m.body(i), &req))
	return req.Messages
}

// userText joins the text of every user message of request i, tool results
// included. The text is decoded, so "<" is "<".
func (m *scriptedAnthropic) userText(i int) string {
	var b strings.Builder
	for _, msg := range m.requestMessages(i) {
		if msg.Role != "user" {
			continue
		}
		for _, blk := range blocks(m.t, msg.Content) {
			switch blk.Type {
			case "text":
				b.WriteString(blk.Text + "\n")
			case "tool_result":
				for _, inner := range blocks(m.t, blk.Content) {
					b.WriteString(inner.Text + "\n")
				}
			}
		}
	}
	return b.String()
}

// toolOutcome is the result of one tool call as the model reads it.
type toolOutcome struct {
	Text    string
	IsError bool
	Images  []wireBlock
}

// toolResult returns the result of the last tool call in request i.
func (m *scriptedAnthropic) toolResult(i int) toolOutcome {
	msgs := m.requestMessages(i)
	for j := len(msgs) - 1; j >= 0; j-- {
		var out toolOutcome
		found := false
		for _, blk := range blocks(m.t, msgs[j].Content) {
			if blk.Type != "tool_result" {
				continue
			}
			found = true
			out.IsError = blk.IsError
			for _, inner := range blocks(m.t, blk.Content) {
				switch inner.Type {
				case "text":
					out.Text += inner.Text
				case "image":
					out.Images = append(out.Images, inner)
				}
			}
		}
		if found {
			return out
		}
	}
	m.t.Fatalf("request %d has no tool result", i+1)
	return toolOutcome{}
}

// say is a step that answers with text.
func say(text string) modelStep { return modelStep{reply: sseText(text)} }

// call is a step that calls one tool.
func call(t *testing.T, name string, input map[string]any) modelStep {
	t.Helper()
	return modelStep{reply: toolUseStream(t, name, input)}
}

// callAfter is call that runs before first.
func callAfter(t *testing.T, before func(body []byte), name string, input map[string]any) modelStep {
	s := call(t, name, input)
	s.before = before
	return s
}

// sayAfter is say that runs before first.
func sayAfter(before func(body []byte), text string) modelStep {
	s := say(text)
	s.before = before
	return s
}

// printSpec is one run of ask -p through runWithDependencies.
type printSpec struct {
	// provider and model default to the Token Plan provider and its model.
	provider string
	model    string
	json     bool
	// env holds extra values for getenv.
	env map[string]string
	// environ is the base environment of the child processes. Nil means the
	// environment of the test process, as in ask.
	environ []string
	prompts []string
}

// printResult is what ask -p wrote and returned.
type printResult struct {
	code   int
	stdout string
	stderr string
}

// runPrint runs ask -p with m as the model HTTP service. Only the model is
// replaced.
func runPrint(t *testing.T, m *scriptedAnthropic, s printSpec) printResult {
	t.Helper()
	provider := s.provider
	if provider == "" {
		provider = anthropic.ProviderID
	}
	args := []string{"-p", "--provider", provider}
	if s.model != "" {
		args = append(args, "--model", s.model)
	}
	if s.json {
		args = append(args, "--mode", "json")
	}
	if len(s.prompts) == 0 {
		s.prompts = []string{"go"}
	}
	args = append(args, s.prompts...)
	home := t.TempDir()
	getenv := func(k string) string {
		if v, ok := s.env[k]; ok {
			return v
		}
		switch k {
		case "ASK_HOME":
			return home
		case "ASK_ALIBABA_TOKEN_PLAN_API_KEY", "ANTHROPIC_API_KEY":
			return "fixture-key"
		}
		return ""
	}
	deps := runDependencies{getenv: getenv, inferenceHTTP: &http.Client{Transport: m}}
	if s.environ != nil {
		env := s.environ
		deps.environ = func() []string { return env }
	}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(args, strings.NewReader(""), &stdout, &stderr, deps)
	return printResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// requireOK requires exit 0 and returns the printed reply.
func (r printResult) requireOK(t *testing.T) string {
	t.Helper()
	require.Equal(t, 0, r.code, "stderr: %s", r.stderr)
	return r.stdout
}

// newToolRepo makes the repository that the tool tests work in: a Go module
// with one failing test, a .gitignore with ignored files, a CRLF file with a
// byte order mark, a generated file, a 3000-line file, a binary file and two
// images.
func newToolRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	files := map[string]string{
		"go.mod":        "module calc\n\ngo 1.21\n",
		"calc.go":       "package calc\n\n// Add returns the sum of a and b.\nfunc Add(a, b int) int {\n\treturn a - b\n}\n",
		"calc_test.go":  "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif got := Add(2, 3); got != 5 {\n\t\tt.Fatalf(\"Add(2, 3) = %d, want 5\", got)\n\t}\n}\n",
		".gitignore":    "*.log\nbuild/\n",
		"app.log":       "ignored log\n",
		"build/out.txt": "ignored build output\n",
		"notes/todo.md": "# todo\n",
		"crlf.txt":      "\xEF\xBB\xBFalpha\r\nbeta\r\ngamma\r\n",
		"generated.go":  "// Code generated by gen; DO NOT EDIT.\n\npackage calc\n",
		"binary.bin":    "\x00\x01\x02\x03binary\x00\x00\xff\xfe",
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	var big strings.Builder
	for i := 1; i <= 3000; i++ {
		fmt.Fprintf(&big, "row %d\n", i)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.txt"), []byte(big.String()), 0o644))
	writePNG(t, filepath.Join(dir, "small.png"), 8, 8)
	writePNG(t, filepath.Join(dir, "large.png"), 4000, 3000)
	out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput()
	require.NoError(t, err, string(out))
	return dir
}

// writePNG writes a gradient image of the given size.
func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: uint8((x + y) % 256), A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
}

// waitFile waits until path exists.
func waitFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(e2eWaitFor)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("timeout: %s was not created", path)
}

// settle is the time that the job table needs to turn the end of a process
// into a notice or an event, once the process has done its last visible act.
const settle = 500 * time.Millisecond

// readPgid returns the process group id that a job script wrote to path.
func readPgid(t *testing.T, path string) int {
	t.Helper()
	waitFile(t, path)
	var pgid int
	deadline := time.Now().Add(e2eWaitFor)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			if n, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil && n > 0 {
				pgid = n
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotZero(t, pgid, "no process group id in %s", path)
	return pgid
}

// groupGone reports whether no process of the group is left.
func groupGone(pgid int) bool {
	return errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH)
}

// requireGroupGone requires that the process group of the job whose script
// wrote its pgid to pgidFile has no process left (kill(-pgid, 0) is ESRCH).
func requireGroupGone(t *testing.T, pgidFile string) {
	t.Helper()
	pgid := readPgid(t, pgidFile)
	require.True(t, groupGone(pgid), "process group %d still has a process", pgid)
}

// requireGroupGoneSoon is requireGroupGone for a state that another process
// reaches: the kill is observed from outside, so the check waits a bounded time.
func requireGroupGoneSoon(t *testing.T, pgidFile string) {
	t.Helper()
	pgid := readPgid(t, pgidFile)
	require.Eventually(t, func() bool { return groupGone(pgid) }, 10*time.Second, 20*time.Millisecond,
		"process group %d still has a process", pgid)
}

// requireGroupAlive requires that the group still has a process.
func requireGroupAlive(t *testing.T, pgidFile string) {
	t.Helper()
	pgid := readPgid(t, pgidFile)
	require.False(t, groupGone(pgid), "process group %d is gone", pgid)
}

// recordGroup is the start of a job script: it writes the process group id of
// the shell to file.
func recordGroup(file string) string {
	return "ps -o pgid= -p $$ | tr -d ' ' > " + file + "; "
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ---- file tools ----

func TestPrintReadsFileWithLineNumbers(t *testing.T) {
	repo := newToolRepo(t)
	t.Setenv("HOME", repo)
	m := scriptModel(t,
		call(t, "read", map[string]any{"path": filepath.Join(repo, "calc.go")}),
		call(t, "read", map[string]any{"path": filepath.Join(repo, "binary.bin")}),
		call(t, "read", map[string]any{"path": filepath.Join(repo, "calc.go"), "offset": 99}),
		call(t, "read", map[string]any{"path": "@" + filepath.Join(repo, "calc.go"), "limit": 2}),
		call(t, "read", map[string]any{"path": "~/calc.go", "limit": 2}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	assert.Equal(t, "done\n", r.requireOK(t))
	first := m.toolResult(1)
	assert.False(t, first.IsError)
	assert.Contains(t, first.Text, "1|package calc")
	assert.Contains(t, first.Text, "4|func Add(a, b int) int {")
	binary := m.toolResult(2)
	assert.True(t, binary.IsError)
	assert.Contains(t, binary.Text, "binary file")
	pastEnd := m.toolResult(3)
	assert.Contains(t, pastEnd.Text, "Offset 99 is beyond end of file")
	prefixed := m.toolResult(4)
	assert.Contains(t, prefixed.Text, "1|package calc")
	assert.Contains(t, prefixed.Text, "Use offset=3 to continue")
	home := m.toolResult(5)
	assert.False(t, home.IsError, home.Text)
	assert.Contains(t, home.Text, "1|package calc")
	assert.Contains(t, home.Text, "Use offset=3 to continue")
}

func TestPrintReadPagesWithOffset(t *testing.T) {
	t.Parallel()
	repo := newToolRepo(t)
	path := filepath.Join(repo, "big.txt")
	m := scriptModel(t,
		call(t, "read", map[string]any{"path": path}),
		call(t, "read", map[string]any{"path": path, "offset": 2001}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	page1 := m.toolResult(1).Text
	assert.Contains(t, page1, "1|row 1\n")
	assert.Contains(t, page1, "2000|row 2000")
	assert.NotContains(t, page1, "2001|row 2001")
	assert.Contains(t, page1, "Use offset=2001 to continue")
	page2 := m.toolResult(2).Text
	assert.Contains(t, page2, "2001|row 2001")
	assert.Contains(t, page2, "3000|row 3000")
	assert.NotContains(t, page2, "to continue")
}

func TestPrintCoercesStringArguments(t *testing.T) {
	t.Parallel()
	repo := newToolRepo(t)
	m := scriptModel(t,
		call(t, "read", map[string]any{"path": filepath.Join(repo, "big.txt"), "offset": "5", "limit": "2"}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	res := m.toolResult(1)
	assert.False(t, res.IsError, res.Text)
	assert.True(t, strings.HasPrefix(res.Text, "5|row 5\n6|row 6"), res.Text)
}

func TestPrintWritesFileAtomically(t *testing.T) {
	t.Parallel()
	repo := newToolRepo(t)
	script := filepath.Join(repo, "tools", "deep", "run.sh")
	m := scriptModel(t,
		call(t, "write", map[string]any{"path": script, "content": "#!/bin/sh\necho hi\n"}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	assert.False(t, m.toolResult(1).IsError, m.toolResult(1).Text)
	data, err := os.ReadFile(script)
	require.NoError(t, err)
	assert.Equal(t, "#!/bin/sh\necho hi\n", string(data))
	info, err := os.Stat(script)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "a shebang makes the file executable")
	entries, err := os.ReadDir(filepath.Dir(script))
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temporary file stays next to the target")
}

func TestPrintWriteRefusesGeneratedFile(t *testing.T) {
	t.Parallel()
	repo := newToolRepo(t)
	path := filepath.Join(repo, "generated.go")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	m := scriptModel(t,
		call(t, "write", map[string]any{"path": path, "content": "package calc\n"}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	res := m.toolResult(1)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "Refusing to overwrite")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
}

func TestPrintEditsFileAfterRead(t *testing.T) {
	t.Parallel()
	repo := newToolRepo(t)
	path := filepath.Join(repo, "crlf.txt")
	m := scriptModel(t,
		call(t, "read", map[string]any{"path": path}),
		call(t, "edit", map[string]any{"path": path, "edits": []map[string]any{{"oldText": "beta", "newText": "BETA"}}}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	res := m.toolResult(2)
	assert.False(t, res.IsError, res.Text)
	assert.Contains(t, res.Text, "Successfully replaced 1 block(s)")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "\xEF\xBB\xBFalpha\r\nBETA\r\ngamma\r\n", string(data), "BOM and CRLF stay")
}

func TestPrintEditRejectsStaleFile(t *testing.T) {
	t.Parallel()
	repo := newToolRepo(t)
	path := filepath.Join(repo, "calc.go")
	outside := "package calc\n\n// changed outside\nfunc Add(a, b int) int { return a + b }\n"
	m := scriptModel(t,
		call(t, "read", map[string]any{"path": path}),
		callAfter(t, func([]byte) {
			// The file changes after the read result and before the edit call.
			time.Sleep(20 * time.Millisecond)
			assert.NoError(t, os.WriteFile(path, []byte(outside), 0o644))
		}, "edit", map[string]any{"path": path, "edits": []map[string]any{{"oldText": "a - b", "newText": "a + b"}}}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	res := m.toolResult(2)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "changed on disk since you last read it")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, outside, string(data), "the stale edit changed nothing")
}

func TestPrintEditAcceptsLegacyShape(t *testing.T) {
	t.Parallel()
	repo := newToolRepo(t)
	path := filepath.Join(repo, "calc.go")
	m := scriptModel(t,
		call(t, "read", map[string]any{"path": path}),
		call(t, "edit", map[string]any{"path": path, "oldText": "a - b", "newText": "a + b"}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	assert.False(t, m.toolResult(2).IsError, m.toolResult(2).Text)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "return a + b")
}

func TestPrintListsDirectory(t *testing.T) {
	t.Parallel()
	repo := newToolRepo(t)
	m := scriptModel(t,
		call(t, "ls", map[string]any{"path": repo}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	lines := strings.Split(m.toolResult(1).Text, "\n")
	assert.Contains(t, lines, ".gitignore", "dotfiles are listed")
	assert.Contains(t, lines, "build/", "directories have a slash")
	assert.Contains(t, lines, "calc.go")
	var folded []string
	for _, l := range lines {
		folded = append(folded, strings.ToLower(l))
	}
	assert.True(t, sort.StringsAreSorted(folded), "entries are sorted without case: %v", lines)
}

// requireRipgrep skips the test on a machine without rg, which grep needs.
func requireRipgrep(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("ripgrep is not installed")
	}
	return path
}

func TestPrintGrepFindsMatches(t *testing.T) {
	requireRipgrep(t)
	t.Parallel()
	repo := newToolRepo(t)
	m := scriptModel(t,
		call(t, "grep", map[string]any{"pattern": "func Add", "path": repo}),
		call(t, "grep", map[string]any{"pattern": "row", "path": filepath.Join(repo, "big.txt"), "limit": 10}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	assert.Contains(t, m.toolResult(1).Text, "calc.go:4: func Add(a, b int) int {")
	capped := m.toolResult(2)
	assert.False(t, capped.IsError, capped.Text)
	assert.Equal(t, 10, strings.Count(capped.Text, "big.txt:"))
	assert.Contains(t, capped.Text, "big.txt:10: row 10")
	assert.NotContains(t, capped.Text, "big.txt:11:")
	assert.Contains(t, capped.Text, "Found at least 10 matches; showing the first 10.")
}

// findExpected is the list that every find chain returns for "*" in the
// repository: the .gitignore rules hide app.log and build/.
var findExpected = []string{
	".gitignore", "binary.bin", "big.txt", "calc.go", "calc_test.go", "crlf.txt", "generated.go",
	"go.mod", "large.png", "notes/todo.md", "small.png",
}

func findResult(t *testing.T, text string) []string {
	t.Helper()
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(text), "\n") {
		if l != "" && !strings.HasPrefix(l, "[") {
			out = append(out, l)
		}
	}
	sort.Strings(out)
	return out
}

func TestPrintFindWithoutFdUsesFallback(t *testing.T) {
	rg := requireRipgrep(t)
	repo := newToolRepo(t)
	bin := t.TempDir()
	require.NoError(t, os.Symlink(rg, filepath.Join(bin, "rg")))
	t.Setenv("PATH", bin)
	m := scriptModel(t,
		call(t, "find", map[string]any{"pattern": "*", "path": repo}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	expected := append([]string(nil), findExpected...)
	sort.Strings(expected)
	assert.Equal(t, expected, findResult(t, m.toolResult(1).Text))
}

func TestPrintFindWithoutFdAndRgUsesWalk(t *testing.T) {
	repo := newToolRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "notes", ".gitignore"), []byte("todo.md\n"), 0o644))
	t.Setenv("PATH", t.TempDir())
	m := scriptModel(t,
		call(t, "find", map[string]any{"pattern": "*", "path": repo}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	var expected []string
	for _, path := range findExpected {
		if path != "notes/todo.md" {
			expected = append(expected, path)
		}
	}
	expected = append(expected, "notes/.gitignore")
	sort.Strings(expected)
	assert.Equal(t, expected, findResult(t, m.toolResult(1).Text))
}

// ---- images ----

func TestPrintReadImageReturnsImageBlock(t *testing.T) {
	t.Parallel()
	repo := newToolRepo(t)
	m := scriptModel(t,
		call(t, "read", map[string]any{"path": filepath.Join(repo, "small.png")}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{provider: "anthropic", model: "claude-sonnet-4-6"})

	r.requireOK(t)
	res := m.toolResult(1)
	assert.Contains(t, res.Text, "Read image file [image/png]")
	require.Len(t, res.Images, 1)
	assert.Equal(t, "image/png", res.Images[0].Source.MediaType)
	raw, err := base64.StdEncoding.DecodeString(res.Images[0].Source.Data)
	require.NoError(t, err)
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	require.NoError(t, err)
	assert.Equal(t, 8, cfg.Width)
}

func TestPrintReadLargeImageIsResized(t *testing.T) {
	t.Parallel()
	repo := newToolRepo(t)
	m := scriptModel(t,
		call(t, "read", map[string]any{"path": filepath.Join(repo, "large.png")}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{provider: "anthropic", model: "claude-sonnet-4-6"})

	r.requireOK(t)
	res := m.toolResult(1)
	require.Len(t, res.Images, 1)
	assert.LessOrEqual(t, len(res.Images[0].Source.Data), 4_718_592, "base64 text within 4.5 MiB")
	raw, err := base64.StdEncoding.DecodeString(res.Images[0].Source.Data)
	require.NoError(t, err)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	require.NoError(t, err)
	assert.LessOrEqual(t, cfg.Width, 2000)
	assert.LessOrEqual(t, cfg.Height, 2000)
	assert.Contains(t, res.Text, "[Image: original 4000x3000, displayed at")
}

// TestPrintReadImageForNonVisionModelAddsNote uses the agent composition of
// ask -p with a model row that has no image input. The catalog has no such
// model, so --model cannot select it and the row comes from the caller, as it
// does for the ACP session factory.
func TestPrintReadImageForNonVisionModelAddsNote(t *testing.T) {
	t.Parallel()
	repo := newToolRepo(t)
	m := scriptModel(t,
		call(t, "read", map[string]any{"path": filepath.Join(repo, "small.png")}),
		say("done"),
	)
	row := anthropic.ModelFor(anthropic.ModelID)
	row.Input = []string{"text"}
	client := &http.Client{Transport: m}
	stream := anthropic.New(
		anthropic.WithEnv(func(string) (string, bool) { return "fixture-key", true }),
		anthropic.WithHTTPClient(client),
	).Stream
	getenv := func(k string) string {
		if k == "ASK_ALIBABA_TOKEN_PLAN_API_KEY" {
			return "fixture-key"
		}
		return ""
	}
	o := options{provider: anthropic.ProviderID, model: anthropic.ModelID, transport: m}
	ag, jt, err := newNativeAgentOn(o, getenv, nil, newSessionID(), repo, stream, row)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	code := runHeadless(ag, []string{"go"}, modePrint, &stdout, &stderr, nil)
	jt.KillRemaining()

	require.Equal(t, 0, code, stderr.String())
	res := m.toolResult(1)
	assert.Empty(t, res.Images)
	assert.Contains(t, res.Text, "Read image file [image/png]")
	assert.Contains(t, res.Text, "[Current model does not support images.")
}

// ---- tool prompt section ----

func TestPrintRequestCarriesToolPromptSection(t *testing.T) {
	t.Parallel()
	m := scriptModel(t, say("done"))

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	var req map[string]any
	require.NoError(t, json.Unmarshal(m.body(0), &req))
	system := systemText(t, req)
	assert.Contains(t, system, "Available tools:")
	for _, name := range []string{"read", "write", "edit", "ls", "grep", "find", "bash", "job_output", "job_kill", "job_list", "monitor"} {
		assert.Contains(t, system, "\n- "+name+": ", name)
	}
}

// ---- bash ----

func TestPrintBashRunsTestsAndReportsExit(t *testing.T) {
	t.Parallel()
	repo := newToolRepo(t)
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": "cd " + shellQuote(repo) + " && go test ./...", "timeout": 100}),
		call(t, "bash", map[string]any{"command": "cd " + shellQuote(repo) + " && sed -i.bak 's/a - b/a + b/' calc.go && go test ./...", "timeout": 100}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	failed := m.toolResult(1)
	assert.True(t, failed.IsError)
	assert.True(t, strings.HasPrefix(failed.Text, "exit: 1\n"), failed.Text)
	assert.Contains(t, failed.Text, "--- FAIL: TestAdd")
	passed := m.toolResult(2)
	assert.False(t, passed.IsError, passed.Text)
	assert.True(t, strings.HasPrefix(passed.Text, "exit: 0\n"), passed.Text)
	assert.Contains(t, passed.Text, "ok")
}

// spillPathOf returns the spill file that a result text names.
func spillPathOf(t *testing.T, text string) string {
	t.Helper()
	m := regexp.MustCompile(`\[full output: ([^\]]+)\]`).FindStringSubmatch(text)
	require.NotNil(t, m, "no spill file in: %.300s", text)
	return m[1]
}

// toolResultOf returns the result text of the last tool call in a request body.
func toolResultOf(t *testing.T, body []byte) toolOutcome {
	t.Helper()
	m := &scriptedAnthropic{t: t, bodies: [][]byte{body}}
	return m.toolResult(0)
}

func TestPrintBashBigOutputKeepsHeadAndTail(t *testing.T) {
	t.Parallel()
	var spill struct {
		path string
		mode os.FileMode
		data []byte
	}
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": "seq 1 60000"}),
		sayAfter(func(body []byte) {
			// The spill file is removed when the session closes, so it is
			// read while the Agent waits for this reply.
			res := toolResultOf(t, body)
			spill.path = spillPathOf(t, res.Text)
			info, err := os.Stat(spill.path)
			if assert.NoError(t, err) {
				spill.mode = info.Mode().Perm()
			}
			spill.data, _ = os.ReadFile(spill.path)
		}, "done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	res := m.toolResult(1)
	assert.True(t, strings.HasPrefix(res.Text, "exit: 0\n1\n2\n"), "the head stays: %.60s", res.Text)
	assert.Contains(t, res.Text, "bytes elided")
	assert.Contains(t, res.Text, "60000", "the tail stays")
	assert.NotContains(t, res.Text, "\n30000\n", "the middle is left out")
	assert.Equal(t, os.FileMode(0o600), spill.mode)
	assert.True(t, bytes.HasSuffix(spill.data, []byte("\n60000\n")), "the spill file holds the whole output")
	assert.True(t, bytes.HasPrefix(spill.data, []byte("1\n2\n")))
	_, err := os.Stat(spill.path)
	assert.True(t, os.IsNotExist(err), "the spill file is removed with the session")
}

func TestPrintBashSpillStopsAtCap(t *testing.T) {
	t.Parallel()
	var size int64
	var mode os.FileMode
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": "head -c 70000000 /dev/zero | tr '\\0' 'a'; echo; echo finished >&2"}),
		sayAfter(func(body []byte) {
			info, err := os.Stat(spillPathOf(t, toolResultOf(t, body).Text))
			if assert.NoError(t, err) {
				size, mode = info.Size(), info.Mode().Perm()
			}
		}, "done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	res := m.toolResult(1)
	assert.True(t, strings.HasPrefix(res.Text, "exit: 0\n"), "the job was not killed: %.80s", res.Text)
	assert.Contains(t, res.Text, "[spill file capped at 64 MB]")
	assert.EqualValues(t, 64<<20, size)
	assert.Equal(t, os.FileMode(0o600), mode)
}

func TestPrintJSONStreamCarriesBashPartialUpdates(t *testing.T) {
	t.Parallel()
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": "for i in 1 2 3 4 5 6 7 8; do echo tick$i; sleep 0.1; done"}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{json: true})

	require.Equal(t, 0, r.code, r.stderr)
	var updates, endAt, lastUpdate int
	for i, line := range strings.Split(strings.TrimSpace(r.stdout), "\n") {
		var ev jsonEvent
		require.NoError(t, json.Unmarshal([]byte(line), &ev))
		switch ev.Type {
		case "tool_execution_update":
			updates++
			lastUpdate = i
		case "tool_execution_end":
			endAt = i
		}
	}
	assert.GreaterOrEqual(t, updates, 2, "the output streams while the command runs")
	assert.Less(t, lastUpdate, endAt)
}

func TestPrintBashMovesToBackgroundAfterTimeout(t *testing.T) {
	t.Parallel()
	pgid := filepath.Join(t.TempDir(), "pgid")
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": recordGroup(pgid) + "echo started; sleep 300", "timeout": 1}),
		sayAfter(func([]byte) { requireGroupAlive(t, pgid) }, "done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	res := m.toolResult(1)
	assert.False(t, res.IsError)
	assert.Contains(t, res.Text, "continues as background job bash-1")
	assert.Contains(t, res.Text, "started")
	requireGroupGone(t, pgid)
}

func TestPrintBashRunInBackgroundReturnsJobID(t *testing.T) {
	t.Parallel()
	pgid := filepath.Join(t.TempDir(), "pgid")
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": recordGroup(pgid) + "sleep 300", "run_in_background": true}),
		sayAfter(func([]byte) { requireGroupAlive(t, pgid) }, "done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	assert.Contains(t, m.toolResult(1).Text, "Started background job bash-1.")
	requireGroupGone(t, pgid)
}

func TestPrintBashRejectsTrailingAmpersand(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "ran")
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": "touch " + marker + " &"}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	res := m.toolResult(1)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "run_in_background")
	assert.NoFileExists(t, marker, "the command did not start")
}

func TestPrintBashEnvironmentHidesSecrets(t *testing.T) {
	t.Parallel()
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": "env | sort"}),
		say("done"),
	)
	environ := []string{
		"PATH=" + os.Getenv("PATH"),
		"FAKE_API_KEY=hunter2-fake-key",
		"GH_TOKEN=ghp_fakefakefake",
		"ASK_HOME=/leader/home",
		"KEEP_VISIBLE=yes",
	}

	r := runPrint(t, m, printSpec{environ: environ})

	r.requireOK(t)
	text := m.toolResult(1).Text
	assert.Contains(t, text, "KEEP_VISIBLE=yes")
	assert.Contains(t, text, "TERM=dumb")
	assert.Contains(t, text, "ASK_SESSION_ID=")
	assert.Contains(t, text, "ASK_MODEL="+anthropic.ModelID)
	assert.NotContains(t, text, "hunter2")
	assert.NotContains(t, text, "FAKE_API_KEY")
	assert.NotContains(t, text, "GH_TOKEN")
	assert.NotContains(t, text, "/leader/home")
}

// ---- jobs ----

// gatedJob is a background command that records its group, prints one line
// and waits until the gate file exists.
func gatedJob(pgid, gate string) string {
	return recordGroup(pgid) + "echo job-output; while [ ! -f " + gate + " ]; do sleep 0.05; done; echo job-end"
}

func TestPrintJobOutputAnswersRunningJobAtOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pgid, gate := filepath.Join(dir, "pgid"), filepath.Join(dir, "gate")
	var asked time.Time
	var elapsed time.Duration
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": gatedJob(pgid, gate), "run_in_background": true}),
		callAfter(t, func([]byte) {
			readPgid(t, pgid)
			time.Sleep(200 * time.Millisecond)
			asked = time.Now()
		}, "job_output", map[string]any{"job_id": "bash-1"}),
		sayAfter(func([]byte) { elapsed = time.Since(asked) }, "done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	res := m.toolResult(2)
	assert.Contains(t, res.Text, "job-output")
	assert.Contains(t, res.Text, "[status: running]")
	assert.Contains(t, res.Text, "do not poll")
	assert.Less(t, elapsed, 5*time.Second, "a running job answers at once, not after the 30 s wait")
}

func TestPrintJobOutputWaitReturnsFinalStatus(t *testing.T) {
	t.Parallel()
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": "sleep 1; echo final-line", "run_in_background": true}),
		call(t, "job_output", map[string]any{"job_id": "bash-1", "wait": true, "timeout_ms": 20000}),
		sayAfter(func([]byte) { time.Sleep(settle) }, "done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	res := m.toolResult(2)
	assert.Contains(t, res.Text, "final-line")
	assert.Contains(t, res.Text, "[status: completed, exit code: 0]")
	for i := range 3 {
		assert.NotContains(t, m.userText(i), "<system-reminder>", "no notice follows a read of the end")
	}
}

func TestPrintJobKillStopsJobWithoutNotice(t *testing.T) {
	t.Parallel()
	pgid := filepath.Join(t.TempDir(), "pgid")
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": recordGroup(pgid) + "echo up; sleep 300", "run_in_background": true}),
		callAfter(t, func([]byte) { readPgid(t, pgid) }, "job_kill", map[string]any{"job_id": "bash-1"}),
		sayAfter(func([]byte) { requireGroupGoneSoon(t, pgid); time.Sleep(settle) }, "done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	res := m.toolResult(2)
	assert.Contains(t, res.Text, "Job bash-1 killed.")
	assert.Contains(t, res.Text, "[status: killed")
	assert.Equal(t, 3, m.requestCount(), "no notice started another request")
}

func TestPrintJobListShowsJobs(t *testing.T) {
	t.Parallel()
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": "sleep 300", "run_in_background": true}),
		call(t, "bash", map[string]any{"command": "sleep 301", "run_in_background": true}),
		call(t, "job_list", map[string]any{}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	text := m.toolResult(3).Text
	assert.Regexp(t, `bash-1 \[bash\] running \S+ - sleep 300`, text)
	assert.Regexp(t, `bash-2 \[bash\] running \S+ - sleep 301`, text)
}

func TestPrintEleventhJobIsRefused(t *testing.T) {
	t.Parallel()
	steps := make([]modelStep, 0, 12)
	for range 9 {
		steps = append(steps, call(t, "bash", map[string]any{"command": "sleep 300", "run_in_background": true}))
	}
	steps = append(steps,
		call(t, "monitor", map[string]any{"command": "sleep 300", "description": "idle watch"}),
		call(t, "bash", map[string]any{"command": "sleep 300", "run_in_background": true}),
		say("done"),
	)
	m := scriptModel(t, steps...)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	assert.False(t, m.toolResult(10).IsError, "the monitor is the tenth job")
	res := m.toolResult(11)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "Too many active jobs (10)")
	assert.Contains(t, res.Text, "job_kill")
}

// ---- completion notice ----

// noticeScript is a model script in which a background job ends while the
// Agent waits for the model: the second reply is held until the job is done.
// The answer to the notice is the last step.
func noticeScript(t *testing.T, pgid, gate, done string) []modelStep {
	t.Helper()
	command := recordGroup(pgid) + "while [ ! -f " + gate + " ]; do sleep 0.05; done; echo job-result; touch " + done
	return []modelStep{
		call(t, "bash", map[string]any{"command": command, "run_in_background": true}),
		sayAfter(func([]byte) {
			readPgid(t, pgid)
			assert.NoError(t, os.WriteFile(gate, nil, 0o644))
			waitFile(t, done)
			time.Sleep(settle)
		}, "job started"),
		say("notice answered"),
	}
}

func TestPrintCompletionNoticeStartsFollowUpCycle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pgid, gate, done := filepath.Join(dir, "pgid"), filepath.Join(dir, "gate"), filepath.Join(dir, "done")
	command := recordGroup(pgid) + "while [ ! -f " + gate + " ]; do sleep 0.05; done; echo job-result; touch " + done
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": command, "run_in_background": true}),
		say("first answered"),
		sayAfter(func([]byte) {
			// The job ends while the second prompt waits for the model.
			readPgid(t, pgid)
			assert.NoError(t, os.WriteFile(gate, nil, 0o644))
			waitFile(t, done)
			time.Sleep(settle)
		}, "second answered"),
		say("notice answered"),
	)

	r := runPrint(t, m, printSpec{prompts: []string{"start the job", "next question"}})

	assert.Equal(t, "notice answered\n", r.requireOK(t))
	require.Equal(t, 4, m.requestCount())
	notice := m.userText(3)
	assert.Contains(t, notice, "<system-reminder>")
	assert.Contains(t, notice, "Background job bash-1")
	assert.Contains(t, notice, "[status: completed, exit code: 0]")
	assert.NotContains(t, m.userText(2), "<system-reminder>", "the notice waits for the next cycle")
}

func TestPrintJSONStreamCarriesNotice(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m := scriptModel(t, noticeScript(t, filepath.Join(dir, "pgid"), filepath.Join(dir, "gate"), filepath.Join(dir, "done"))...)

	r := runPrint(t, m, printSpec{json: true})

	require.Equal(t, 0, r.code, r.stderr)
	var notices int
	for _, line := range strings.Split(strings.TrimSpace(r.stdout), "\n") {
		var ev jsonEvent
		require.NoError(t, json.Unmarshal([]byte(line), &ev))
		if ev.Type == "message_end" && ev.Message.Role == "user" && strings.Contains(contentText(ev, "text"), "<system-reminder>") {
			notices++
			assert.Contains(t, contentText(ev, "text"), "Background job bash-1")
		}
	}
	assert.Equal(t, 1, notices, "the notice is kept in the log and in the stream")
}

func TestPrintNoticeAfterLastPromptIsAnswered(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m := scriptModel(t, noticeScript(t, filepath.Join(dir, "pgid"), filepath.Join(dir, "gate"), filepath.Join(dir, "done"))...)

	r := runPrint(t, m, printSpec{})

	assert.Equal(t, "notice answered\n", r.requireOK(t), "the reply to the notice is printed")
	assert.Contains(t, m.userText(2), "[status: completed, exit code: 0]")
}

// ---- command guard ----

// guardedCommand is blocked by the command guard and does nothing harmful
// when it runs: no such program exists.
const guardedCommand = "mkfs.ask-no-such-tool /dev/null"

func TestPrintCommandGuardBlocksWhenEnabled(t *testing.T) {
	t.Parallel()
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": guardedCommand}),
		say("run goes on"),
	)

	r := runPrint(t, m, printSpec{env: map[string]string{"ASK_COMMAND_GUARD": "1"}})

	assert.Equal(t, "run goes on\n", r.requireOK(t))
	res := m.toolResult(1)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "blocked by command-guard")
}

func TestPrintCommandGuardOffByDefault(t *testing.T) {
	t.Parallel()
	m := scriptModel(t,
		call(t, "bash", map[string]any{"command": guardedCommand}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	res := m.toolResult(1)
	assert.NotContains(t, res.Text, "blocked by command-guard")
	assert.True(t, strings.HasPrefix(res.Text, "exit: 127"), "the command reached the shell: %s", res.Text)
}

// ---- monitor ----

func TestPrintMonitorDeliversEveryStdoutLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pgid, started := filepath.Join(dir, "pgid"), filepath.Join(dir, "started")
	command := recordGroup(pgid) + "printf 'line-one\\nline-two\\nDONE build\\n'; touch " + started + "; sleep 300"
	m := scriptModel(t,
		call(t, "monitor", map[string]any{"command": command, "description": "build watch"}),
		callAfter(t, func([]byte) {
			waitFile(t, started)
			time.Sleep(settle)
		}, "job_kill", map[string]any{"job_id": "monitor-1"}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	assert.Contains(t, m.toolResult(1).Text, "Started monitor-1 (build watch)")
	events := m.userText(2)
	assert.Contains(t, events, "<system-reminder>")
	assert.Contains(t, events, `<monitor-event job="monitor-1" description="build watch">`)
	for _, line := range []string{"line-one", "line-two", "DONE build"} {
		assert.Contains(t, events, line)
	}
	requireGroupGone(t, pgid)
}

func TestPrintMonitorFloodGuardSuppressesEvents(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pgid, finished := filepath.Join(dir, "pgid"), filepath.Join(dir, "finished")
	command := recordGroup(pgid) + "for i in $(seq 1 40); do echo \"tick $i\"; sleep 0.25; done; touch " + finished + "; sleep 300"
	var started time.Time
	var elapsed time.Duration
	m := scriptModel(t,
		callAfter(t, func([]byte) { started = time.Now() }, "monitor", map[string]any{"command": command, "description": "steady printer"}),
		callAfter(t, func([]byte) {
			waitFile(t, finished)
			time.Sleep(settle)
			elapsed = time.Since(started)
		}, "job_kill", map[string]any{"job_id": "monitor-1"}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	events := m.userText(2)
	// Each tick is outside the debounce window. Count the output lines,
	// because pending events share one monitor-event element.
	var delivered strings.Builder
	for _, event := range regexp.MustCompile(`(?s)<monitor-event [^>]*>\n(.*?)\n</monitor-event>`).FindAllStringSubmatch(events, -1) {
		delivered.WriteString(event[1] + "\n")
	}
	ticks := regexp.MustCompile(`(?m)^tick [0-9]+$`).FindAllString(delivered.String(), -1)
	require.GreaterOrEqual(t, len(ticks), 10, "the initial burst passes")
	for i := range 10 {
		assert.Equal(t, fmt.Sprintf("tick %d", i+1), ticks[i])
	}
	assert.LessOrEqual(t, len(ticks), 10+int(elapsed/(2*time.Second)), "only the initial burst and refilled tokens pass")
	assert.Less(t, len(ticks), 40, "the flood guard removes output events")
	assert.Regexp(t, `\[\d+ events suppressed by the flood guard\]`, events)
	requireGroupGone(t, pgid)
}

func TestPrintMonitorOverloadKillsProcess(t *testing.T) {
	t.Parallel()
	pgid := filepath.Join(t.TempDir(), "pgid")
	command := recordGroup(pgid) + "while :; do echo flood; sleep 0.01; done"
	m := scriptModel(t,
		call(t, "monitor", map[string]any{"command": command, "description": "endless printer"}),
		sayAfter(func([]byte) {
			// About 30 s of continuous overload end in a kill of the group.
			pg := readPgid(t, pgid)
			deadline := time.Now().Add(60 * time.Second)
			for !groupGone(pg) && time.Now().Before(deadline) {
				time.Sleep(50 * time.Millisecond)
			}
			time.Sleep(settle)
		}, "done"),
		say("saw the stop"),
		say("noticed"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	requireGroupGone(t, pgid)
	assert.Contains(t, m.userText(2), "monitor-1 stopped: output too fast for the monitor")
	assert.Contains(t, m.userText(3), "[status: killed, reason: overload]")
}

func TestPrintMonitorStopsAtTimeout(t *testing.T) {
	t.Parallel()
	pgid := filepath.Join(t.TempDir(), "pgid")
	m := scriptModel(t,
		call(t, "monitor", map[string]any{"command": recordGroup(pgid) + "sleep 300", "description": "quiet watch", "timeout_ms": 500}),
		sayAfter(func([]byte) {
			pg := readPgid(t, pgid)
			require.Eventually(t, func() bool { return groupGone(pg) }, 20*time.Second, 20*time.Millisecond)
			time.Sleep(settle)
		}, "waiting"),
		say("noticed"),
	)

	r := runPrint(t, m, printSpec{})

	assert.Equal(t, "noticed\n", r.requireOK(t))
	notice := m.userText(2)
	assert.Contains(t, notice, "Background job monitor-1")
	assert.Contains(t, notice, "[status: killed, reason: timeout]")
}

func TestPrintMonitorRejectsTimeoutAboveMax(t *testing.T) {
	t.Parallel()
	m := scriptModel(t,
		call(t, "monitor", map[string]any{"command": "sleep 300", "description": "too long", "timeout_ms": 36000001}),
		call(t, "job_list", map[string]any{}),
		say("done"),
	)

	r := runPrint(t, m, printSpec{})

	r.requireOK(t)
	res := m.toolResult(1)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Text, "persistent must be true when timeout_ms exceeds 36000000")
	assert.Equal(t, "No jobs.", m.toolResult(2).Text, "no job was started")
}

func TestPrintMonitorPersistentRunsUntilJobKill(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pgid, started := filepath.Join(dir, "pgid"), filepath.Join(dir, "started")
	command := recordGroup(pgid) + "touch " + started + "; sleep 300"
	m := scriptModel(t,
		call(t, "monitor", map[string]any{"command": command, "description": "session watch", "persistent": true}),
		callAfter(t, func([]byte) {
			waitFile(t, started)
			time.Sleep(2 * time.Second)
			requireGroupAlive(t, pgid)
		}, "job_kill", map[string]any{"job_id": "monitor-1"}),
		sayAfter(func([]byte) {
			requireGroupGoneSoon(t, pgid)
			time.Sleep(settle)
		}, "done"),
	)

	r := runPrint(t, m, printSpec{json: true})

	require.Equal(t, 0, r.code, r.stderr)
	assert.Equal(t, 3, m.requestCount(), "job_kill leaves no notice")
	assert.Contains(t, m.toolResult(2).Text, "[status: killed")
	var details struct {
		TimeoutMs  *float64 `json:"timeoutMs"`
		Persistent bool     `json:"persistent"`
	}
	found := false
	for _, line := range strings.Split(strings.TrimSpace(r.stdout), "\n") {
		var ev struct {
			Type     string `json:"type"`
			ToolName string `json:"toolName"`
			Result   struct {
				Details json.RawMessage `json:"details"`
			} `json:"result"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &ev))
		if ev.Type == "tool_execution_end" && ev.ToolName == "monitor" {
			require.NoError(t, json.Unmarshal(ev.Result.Details, &details))
			found = true
		}
	}
	require.True(t, found, "the stream holds the monitor result")
	assert.True(t, details.Persistent)
	require.NotNil(t, details.TimeoutMs)
	assert.Zero(t, *details.TimeoutMs)
}

// ---- built binary ----

// builtAsk runs the built ask against the provider fixture. dir is its working
// directory. The environment holds no credential of the developer shell.
func builtAsk(t *testing.T, f *providerFixture, dir string, extraEnv []string, args ...string) *exec.Cmd {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "ask-home")
	require.NoError(t, os.MkdirAll(home, 0o700))
	cmd := exec.Command(acpBinary(t), args...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + root, "ASK_HOME=" + home, "ANTHROPIC_API_KEY=fixture-anthropic-key", "TZ=UTC"}
	for k, v := range f.env() {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Env = append(cmd.Env, extraEnv...)
	return cmd
}

// toolReplyOf returns a fixture answer that calls one tool.
func toolReplyOf(t *testing.T, name string, input map[string]any) (int, string, string) {
	t.Helper()
	return http.StatusOK, "text/event-stream", toolUseStream(t, name, input)
}

func TestPrintSIGINTDuringBashKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pgid := filepath.Join(dir, "pgid")
	child, grandchild := filepath.Join(dir, "child"), filepath.Join(dir, "grandchild")
	// The shell and a grandchild both ignore SIGTERM; only the group kill ends them.
	command := recordGroup(pgid) +
		"sh -c 'trap \"\" TERM; echo $$ > " + grandchild + "; sleep 300' & " +
		"trap '' TERM; echo $$ > " + child + "; sleep 300"
	f := newProviderFixture(t)
	f.inference = func(n int, _ *http.Request, _ []byte) (int, string, string) {
		if n == 0 {
			return toolReplyOf(t, "bash", map[string]any{"command": command})
		}
		return http.StatusOK, "text/event-stream", sseText("unreachable")
	}
	cmd := builtAsk(t, f, dir, nil, "-p", "--provider", "anthropic", "--model", "claude-sonnet-4-6", "run it")
	var stdout, stderr lockedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	require.NoError(t, cmd.Start())
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	waitFile(t, child)
	waitFile(t, grandchild)
	group := readPgid(t, pgid)
	require.False(t, groupGone(group))
	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))

	select {
	case err := <-exited:
		var ee *exec.ExitError
		require.ErrorAs(t, err, &ee, "stderr: %s", stderr.String())
		assert.Equal(t, 130, ee.ExitCode())
	case <-time.After(20 * time.Second):
		t.Fatalf("ask did not exit on SIGINT\nstderr: %s", stderr.String())
	}
	// ask has exited, so init reaps the killed processes; the check allows that.
	assert.Eventually(t, func() bool { return groupGone(group) }, 10*time.Second, 20*time.Millisecond,
		"no process of the group survives, although each ignores SIGTERM")
	assert.Empty(t, stdout.String(), "a cancelled run prints no reply")
}

func TestPrintExitKillsBackgroundJobs(t *testing.T) {
	dir := t.TempDir()
	pgid := filepath.Join(dir, "pgid")
	command := recordGroup(pgid) + "trap '' TERM; sleep 300"
	f := newProviderFixture(t)
	f.inference = func(n int, _ *http.Request, _ []byte) (int, string, string) {
		switch n {
		case 0:
			return toolReplyOf(t, "bash", map[string]any{"command": command, "run_in_background": true})
		default:
			readPgid(t, pgid)
			return http.StatusOK, "text/event-stream", sseText("all done")
		}
	}
	cmd := builtAsk(t, f, dir, nil, "-p", "--provider", "anthropic", "--model", "claude-sonnet-4-6", "start it")
	var stdout, stderr lockedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()

	require.NoError(t, err, "stderr: %s", stderr.String())
	assert.Equal(t, "all done\n", stdout.String())
	requireGroupGoneSoon(t, pgid)
}

// ---- job table diagnostics ----

// refusedNoticeAgent builds a session that has no model, so the Agent refuses
// the completion notice of a job, and runs one short job to its end.
func refusedNoticeAgent(t *testing.T, o options) {
	t.Helper()
	getenv := func(string) string { return "" }
	ag, jt, err := newNativeAgentOn(o, getenv, nil, newSessionID(), t.TempDir(), nil, providers.Model{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ag.Dispose() })
	// Nobody waits for the job, so its end produces a notice.
	_, err = jt.Start(jobs.StartSpec{Command: "true", Cwd: t.TempDir()})
	require.NoError(t, err)
}

func TestJobTableErrorsReachTheConfiguredSink(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var got []error
	refusedNoticeAgent(t, options{onJobError: func(err error) {
		mu.Lock()
		got = append(got, err)
		mu.Unlock()
	}})

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) > 0
	}, 10*time.Second, 10*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.ErrorContains(t, got[0], "notice for job bash-1")
}

// TestJobTableErrorsDefaultToStderr replaces os.Stderr, so it runs alone.
func TestJobTableErrorsDefaultToStderr(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "stderr")
	require.NoError(t, err)
	saved := os.Stderr
	os.Stderr = file
	t.Cleanup(func() { os.Stderr = saved })

	refusedNoticeAgent(t, options{})

	require.Eventually(t, func() bool {
		data, readErr := os.ReadFile(file.Name())
		return readErr == nil && strings.Contains(string(data), "jobs: notice for job bash-1")
	}, 10*time.Second, 10*time.Millisecond)
}

// ---- stdout guard ----

func TestGuardStdoutKeepsOsStdoutForNonFileStderr(t *testing.T) {
	before := os.Stdout
	restore := guardStdout(&bytes.Buffer{})
	assert.Same(t, before, os.Stdout)
	restore()
	assert.Same(t, before, os.Stdout)
}

// ---- tests ----
