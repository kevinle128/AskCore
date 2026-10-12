package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"AskCore/internal/sandbox"
	"AskCore/internal/workspace"
	"AskCore/pkg/protocol"
)

const (
	grepDefaultLimit = 100
	grepTimeout      = 20 * time.Second
	// grepMaxLineChars is the length at which a match or context line is cut.
	grepMaxLineChars  = 500
	grepLocationLimit = 20
	// searchLineBound caps one unfinished output line of a search program. A
	// longer line (a minified file) is dropped instead of held in memory.
	searchLineBound = 4 << 20
	// searchStderrKeep is the part of the error output kept for an error text.
	searchStderrKeep = 2048
)

// Grep searches file contents with ripgrep.
type Grep struct {
	// lookPath finds a program on PATH. Nil means exec.LookPath.
	lookPath func(string) (string, error)
	// timeout bounds one search. Zero means the default.
	timeout time.Duration
}

var _ ConcurrencySafe = Grep{}
var _ PromptContributor = Grep{}

func (g Grep) look(name string) (string, error) {
	if g.lookPath != nil {
		return g.lookPath(name)
	}
	return exec.LookPath(name)
}

// ConcurrencySafe returns true because a search only reads.
func (Grep) ConcurrencySafe(json.RawMessage) bool { return true }

func (Grep) PromptSnippet() string {
	return "Search file contents for patterns (respects .gitignore)"
}

func (Grep) PromptGuidelines() []string { return nil }

func (Grep) Decl() protocol.ToolDecl {
	return protocol.ToolDecl{
		Name: "grep",
		Description: fmt.Sprintf("Search file contents for a pattern. Returns matching lines with file paths and line numbers. "+
			"Respects .gitignore. Output is truncated to %d matches or %dKB (whichever is hit first). "+
			"Long lines are truncated to %d chars.", grepDefaultLimit, DefaultMaxBytes/1024, grepMaxLineChars),
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"pattern":{"type":"string","description":"Search pattern (regex or literal string)"},` +
			`"path":{"type":"string","description":"Directory or file to search (default: current directory)"},` +
			`"glob":{"type":"string","description":"Filter files by glob pattern, e.g. '*.ts' or '**/*.spec.ts'"},` +
			`"ignoreCase":{"type":"boolean","description":"Case-insensitive search (default: false)"},` +
			`"literal":{"type":"boolean","description":"Treat pattern as literal string instead of regex (default: false)"},` +
			`"context":{"type":"number","description":"Number of lines to show before and after each match (default: 0)"},` +
			`"limit":{"type":"number","description":"Maximum number of matches to return (default: 100)"}` +
			`},"required":["pattern"]}`),
	}
}

type grepArgs struct {
	Pattern    string   `json:"pattern"`
	Path       string   `json:"path"`
	Glob       string   `json:"glob"`
	IgnoreCase bool     `json:"ignoreCase"`
	Literal    bool     `json:"literal"`
	Context    float64  `json:"context"`
	Limit      *float64 `json:"limit"`
}

func (g Grep) Execute(ctx context.Context, tc Context, args json.RawMessage) (protocol.ToolExecutionResult, error) {
	var in grepArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("grep: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return protocol.ToolExecutionResult{}, err
	}
	rg, err := g.look("rg")
	if err != nil {
		return protocol.ToolExecutionResult{}, errors.New("grep needs ripgrep (rg) on PATH")
	}
	target, err := workspace.ResolvePath(tc.Cwd, searchPathArg(in.Path))
	if err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("grep: %w", err)
	}
	target, st, err := locateExisting(target)
	if err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("path not found: %s", target)
	}
	root := target
	if !st.IsDir() {
		root = filepath.Dir(target)
	}
	limit := searchLimit(in.Limit, grepDefaultLimit)
	ctxLines := int(min(max(in.Context, 0), 1<<20))

	argv := []string{rg, "--json", "--line-number", "--color=never", "--hidden", "--no-config",
		"--max-count", strconv.Itoa(limit), "--glob", "!.git"}
	if in.IgnoreCase {
		argv = append(argv, "--ignore-case")
	}
	if in.Literal {
		argv = append(argv, "--fixed-strings")
	}
	if in.Glob != "" {
		argv = append(argv, "--glob", in.Glob)
	}
	if ctxLines > 0 {
		argv = append(argv, "--context", strconv.Itoa(ctxLines))
	}
	// The path follows "--" with the pattern, so a pattern that starts with
	// "-" is never read as a flag.
	argv = append(argv, "--", in.Pattern, target)

	col := newGrepCollector(limit, ctxLines)
	var preview grepJSONPreview
	var stderr bytes.Buffer
	spec := sandbox.Spec{Argv: argv, Dir: root, Observe: func(stream sandbox.Stream, chunk []byte) {
		if stream == sandbox.StreamStderr {
			if room := searchStderrKeep - stderr.Len(); room > 0 {
				stderr.Write(chunk[:min(room, len(chunk))])
			}
			return
		}
		col.lines.write(preview.write(chunk))
	}}

	timeout := g.timeout
	if timeout <= 0 {
		timeout = grepTimeout
	}
	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, end, err := runSearch(sctx, spec, col.stop)
	if err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("grep: %w", err)
	}
	if ctx.Err() != nil {
		return protocol.ToolExecutionResult{}, ctx.Err()
	}
	col.lines.flush()

	// Exit 1 is "no match". Exit 2 with no match is an error such as a bad
	// regex; with matches it is a partial failure, such as an unreadable file.
	if end == endExited && col.matches == 0 && res.ExitCode != nil && *res.ExitCode > 1 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = fmt.Sprintf("ripgrep exited with code %d", *res.ExitCode)
		}
		return protocol.ToolExecutionResult{}, errors.New(msg)
	}

	body, locations, linesCut := col.format(root)
	var notices []string
	if col.matches == 0 {
		body = ""
	}
	if col.full {
		notices = append(notices, fmt.Sprintf("Found at least %d matches; showing the first %d. Refine the pattern or use path/glob.", limit, limit))
	}
	capped := TruncateHead(body, 1<<30, DefaultMaxBytes)
	details := protocol.ToolResultDetails{Path: root, Kind: protocol.ToolKindSearch, Locations: locations}
	if capped.Truncated {
		notices = append(notices, fmt.Sprintf("%dKB limit reached", DefaultMaxBytes/1024))
		details.Truncation = &capped.Truncation
	}
	if linesCut {
		notices = append(notices, fmt.Sprintf("Some lines truncated to %d chars. Use read tool to see full lines", grepMaxLineChars))
	}
	if end == endContext {
		notices = append(notices, "search stopped after "+secondsText(timeout)+" s")
	}
	text := capped.Text
	if col.matches == 0 && end != endContext {
		text = "No matches found"
	}
	return searchResult(text, notices, details)
}

// searchPathArg defaults an empty path argument to the working directory.
func searchPathArg(p string) string {
	if p == "" {
		return "."
	}
	return p
}

// searchLimit turns an optional number argument into a limit of at least one.
func searchLimit(v *float64, def int) int {
	if v == nil || math.IsNaN(*v) {
		return def
	}
	return int(min(max(*v, 1), math.MaxInt32))
}

func secondsText(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', -1, 64)
}

// searchResult joins the text and the notices, each notice in brackets on its
// own line after a blank line, and encodes the details.
func searchResult(text string, notices []string, details protocol.ToolResultDetails) (protocol.ToolExecutionResult, error) {
	var b strings.Builder
	b.WriteString(text)
	for i, n := range notices {
		switch {
		case i > 0:
			b.WriteString("\n")
		case text != "":
			b.WriteString("\n\n")
		}
		b.WriteString("[" + n + "]")
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return protocol.ToolExecutionResult{}, fmt.Errorf("search: details: %w", err)
	}
	return protocol.ToolExecutionResult{
		Content: []protocol.UserBlock{protocol.Text{Text: b.String()}},
		Details: raw,
	}, nil
}

// searchEnd tells how runSearch ended.
type searchEnd int

const (
	// endExited: the program ended on its own.
	endExited searchEnd = iota
	// endStopped: the caller closed stop and the group was killed.
	endStopped
	// endContext: ctx ended and the group was killed.
	endContext
)

// runSearch runs spec and waits until the program ends, stop closes or ctx
// ends. In the last two cases it kills the process group and waits for it, so
// nothing is left running when it returns.
func runSearch(ctx context.Context, spec sandbox.Spec, stop <-chan struct{}) (sandbox.Result, searchEnd, error) {
	p, err := sandbox.Start(spec)
	if err != nil {
		return sandbox.Result{}, endExited, err
	}
	end := endExited
	select {
	case <-p.Done():
	case <-stop:
		end = endStopped
		p.Kill()
		<-p.Done()
	case <-ctx.Done():
		end = endContext
		p.Kill()
		<-p.Done()
	}
	return p.Result(), end, nil
}

// lineSplitter hands each complete line of a byte stream to handle. The line
// slice is only valid during the call.
type lineSplitter struct {
	handle   func(line []byte)
	pending  []byte
	skipping bool
}

// grepJSONPreview bounds the rg JSON stream before event decoding. The
// collector uses no arrays (rg's submatches can be large), so arrays become
// empty. Strings retain more than 500 characters for the preview and its cut
// marker, including JSON Unicode escapes or base64 bytes. Escape sequences
// and UTF-8 runes are kept whole, even across input chunks.
type grepJSONPreview struct {
	inString         bool
	escaped          bool
	unicodeRemaining int
	stringBytes      int
	clipped          bool
	arrayDepth       int
}

func (p *grepJSONPreview) write(chunk []byte) []byte {
	const maxStringBytes = 8 * 1024
	out := make([]byte, 0, len(chunk))
	for _, b := range chunk {
		if p.inString {
			if !p.escaped && p.unicodeRemaining == 0 && b == '"' {
				p.inString = false
				if p.arrayDepth == 0 {
					out = append(out, b)
				}
				continue
			}
			if p.arrayDepth == 0 {
				if !p.escaped && p.unicodeRemaining == 0 && utf8.RuneStart(b) && p.stringBytes >= maxStringBytes {
					p.clipped = true
				}
				if !p.clipped {
					out = append(out, b)
					p.stringBytes++
				}
			}
			switch {
			case p.unicodeRemaining > 0:
				p.unicodeRemaining--
			case p.escaped:
				p.escaped = false
				if b == 'u' {
					p.unicodeRemaining = 4
				}
			case b == '\\':
				p.escaped = true
			}
			continue
		}
		switch b {
		case '"':
			p.inString, p.clipped, p.stringBytes = true, false, 0
			if p.arrayDepth == 0 {
				out = append(out, b)
			}
		case '[':
			p.arrayDepth++
			if p.arrayDepth == 1 {
				out = append(out, b)
			}
		case ']':
			p.arrayDepth--
			if p.arrayDepth == 0 {
				out = append(out, b)
			}
		default:
			if p.arrayDepth == 0 {
				out = append(out, b)
			}
		}
	}
	return out
}

func (s *lineSplitter) write(chunk []byte) {
	for len(chunk) > 0 {
		i := bytes.IndexByte(chunk, '\n')
		if i < 0 {
			if !s.skipping {
				s.pending = append(s.pending, chunk...)
				if len(s.pending) > searchLineBound {
					s.pending, s.skipping = s.pending[:0], true
				}
			}
			return
		}
		if s.skipping {
			s.skipping = false
		} else {
			s.pending = append(s.pending, chunk[:i]...)
			s.handle(s.pending)
		}
		s.pending = s.pending[:0]
		chunk = chunk[i+1:]
	}
}

// flush hands over a last line that has no newline.
func (s *lineSplitter) flush() {
	if !s.skipping && len(s.pending) > 0 {
		s.handle(s.pending)
	}
	s.pending, s.skipping = s.pending[:0], false
}

// rgText is a ripgrep JSON text field: UTF-8 text, or base64 bytes when the
// data is not UTF-8.
type rgText struct {
	Text  *string `json:"text"`
	Bytes *string `json:"bytes"`
}

func (t rgText) String() string {
	switch {
	case t.Text != nil:
		return *t.Text
	case t.Bytes != nil:
		raw, err := base64.StdEncoding.DecodeString(*t.Bytes)
		if err != nil {
			return ""
		}
		return strings.ToValidUTF8(string(raw), "�")
	}
	return ""
}

// rgEvent holds the fields of a ripgrep JSON event that grep reads. Unknown
// fields and event types are ignored.
type rgEvent struct {
	Type string `json:"type"`
	Data struct {
		Path       rgText `json:"path"`
		Lines      rgText `json:"lines"`
		LineNumber int    `json:"line_number"`
	} `json:"data"`
}

type grepLine struct {
	number int
	text   string
	match  bool
}

// grepCollector gathers the match and context lines of the JSON event stream.
type grepCollector struct {
	limit   int
	context int
	lines   lineSplitter
	files   map[string][]grepLine

	matches int
	// full is true once the match budget is used up.
	full bool
	// stop closes when the search can end: the budget is used up and the
	// context of the last match is complete.
	stop     chan struct{}
	stopOnce sync.Once
}

func newGrepCollector(limit, ctxLines int) *grepCollector {
	c := &grepCollector{limit: limit, context: ctxLines, files: map[string][]grepLine{}, stop: make(chan struct{})}
	c.lines.handle = c.event
	return c
}

func (c *grepCollector) signalStop() { c.stopOnce.Do(func() { close(c.stop) }) }

func (c *grepCollector) event(raw []byte) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return
	}
	var ev rgEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return
	}
	switch ev.Type {
	case "match":
		if c.full {
			// A match after the budget: the context of the last one is complete.
			c.signalStop()
			return
		}
		c.add(ev, true)
		c.matches++
		if c.matches >= c.limit {
			c.full = true
			if c.context == 0 {
				c.signalStop()
			}
		}
	case "context":
		c.add(ev, false)
	case "begin", "end":
		if c.full {
			c.signalStop()
		}
	}
}

func (c *grepCollector) add(ev rgEvent, match bool) {
	p := ev.Data.Path.String()
	if p == "" || ev.Data.LineNumber <= 0 {
		return
	}
	text := strings.TrimSuffix(ev.Data.Lines.String(), "\n")
	text = strings.ReplaceAll(text, "\r", "")
	c.files[p] = append(c.files[p], grepLine{number: ev.Data.LineNumber, text: text, match: match})
}

// format renders the collected lines, files in path order, as "path:N: text"
// for a match and "path-N- text" for a context line, with paths relative to
// root. It also returns the locations of the first matches and whether a line
// was cut.
func (c *grepCollector) format(root string) (text string, locations []protocol.ToolLocation, cut bool) {
	paths := make([]string, 0, len(c.files))
	for p := range c.files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	var out []string
	for _, p := range paths {
		rel, err := filepath.Rel(root, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			rel = filepath.Base(p)
		}
		rel = filepath.ToSlash(rel)
		lines := c.files[p]
		slices.SortStableFunc(lines, func(a, b grepLine) int { return a.number - b.number })
		for _, l := range lines {
			t, wasCut := cutLine(l.text, grepMaxLineChars)
			cut = cut || wasCut
			if l.match {
				out = append(out, fmt.Sprintf("%s:%d: %s", rel, l.number, t))
				if len(locations) < grepLocationLimit {
					locations = append(locations, protocol.ToolLocation{Path: p, Line: l.number})
				}
			} else {
				out = append(out, fmt.Sprintf("%s-%d- %s", rel, l.number, t))
			}
		}
	}
	return strings.Join(out, "\n"), locations, cut
}

// cutLine keeps the first chars characters of s and marks the cut.
func cutLine(s string, chars int) (string, bool) {
	if len(s) <= chars {
		return s, false
	}
	n, pos := 0, 0
	for pos < len(s) && n < chars {
		_, size := utf8.DecodeRuneInString(s[pos:])
		pos += size
		n++
	}
	if pos >= len(s) {
		return s, false
	}
	return s[:pos] + "... [truncated]", true
}
