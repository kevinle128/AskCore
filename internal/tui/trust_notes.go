package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"AskCore/pkg/protocol"
)

// Trust notices.
//
// The block pane shows the project trust of a session start: the explanation
// while the startup trust box is open, a warning of the trust check, one note
// about the trust of the session, and the settings problems of the session
// start. The texts come from the approved trust design. A path or a text of
// the host is untrusted: safeText changes each control character to "?", so
// that it cannot write a terminal sequence.

// Source words of the host (protocol.ACPTrustStateResult.Source).
const (
	trustSourceFlag          = "flag"
	trustSourceNoGatedFiles  = "no_gated_files"
	trustSourceStoreError    = "store_error"
	trustSourceSaved         = "saved"
	trustSourceSavedAncestor = "saved_ancestor"
	trustSourceDefaultAlways = "default_always"
	trustSourceDefaultNever  = "default_never"
	trustSourceNoUI          = "no_ui"
)

// diagnosticRowsMax is the number of settings problems that the block shows.
const diagnosticRowsMax = 8

// safeText changes every control character (C0, C1, DEL and tab) to "?".
// The result holds no escape sequence.
func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return '?'
		}
		return r
	}, s)
}

// span is a part of a row with one role.
type span struct {
	text string
	role Role
	bold bool
}

// render draws the spans of one row.
func renderSpans(th *Theme, spans []span) string {
	var b strings.Builder
	for _, sp := range spans {
		if sp.bold {
			b.WriteString(th.Style(sp.role).Bold(true).Render(sp.text))
			continue
		}
		b.WriteString(th.Render(sp.role, sp.text))
	}
	return b.String()
}

// wrapSpans breaks spans into rows at spaces. The first row has first columns
// and the later rows have rest columns. A word that is wider than a row (for
// example a long path) continues on the next row at the column limit; it is
// never cut. A row never ends inside a wide character. Spaces at the start of
// a row are dropped and spaces at the end of a row are removed.
func wrapSpans(spans []span, first, rest int) [][]span {
	var rows [][]span
	var cur []span
	used, limit := 0, max(1, first)
	newRow := func() {
		for len(cur) > 0 && cur[len(cur)-1].text == " " {
			cur = cur[:len(cur)-1]
		}
		rows = append(rows, cur)
		cur, used, limit = nil, 0, max(1, rest)
	}
	for _, sp := range spans {
		for _, word := range splitSpaces(sp.text) {
			if word == " " {
				if used > 0 {
					cur = append(cur, span{" ", sp.role, sp.bold})
					used++
				}
				continue
			}
			w := displayWidth(word)
			if used > 0 && used+w > limit {
				newRow()
			}
			for displayWidth(word) > limit-used {
				head, tail := splitColumns(word, limit-used)
				if head == "" && used == 0 {
					// A grapheme wider than a whole row: show the marker.
					head, tail = truncateMarker, tail[len(firstGrapheme(tail)):]
				}
				if head != "" {
					cur = append(cur, span{head, sp.role, sp.bold})
				}
				newRow()
				word = tail
			}
			if word != "" {
				cur = append(cur, span{word, sp.role, sp.bold})
				used += displayWidth(word)
			}
		}
	}
	for len(cur) > 0 && cur[len(cur)-1].text == " " {
		cur = cur[:len(cur)-1]
	}
	return append(rows, cur)
}

// splitSpaces splits text into words and single spaces, in order.
func splitSpaces(text string) []string {
	var out []string
	for i, w := range strings.Split(text, " ") {
		if i > 0 {
			out = append(out, " ")
		}
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

// splitColumns returns the longest head of s that uses at most cols columns,
// and the rest. It never splits a grapheme.
func splitColumns(s string, cols int) (head, tail string) {
	w := 0
	n := 0
	for _, t := range tokenize(s) {
		if w+t.width > cols {
			break
		}
		w += t.width
		n += len(t.text)
	}
	return s[:n], s[n:]
}

// firstGrapheme returns the first grapheme of s.
func firstGrapheme(s string) string {
	if toks := tokenize(s); len(toks) > 0 {
		return toks[0].text
	}
	return s
}

// wrapPath breaks a path by columns: it never cuts the path and never moves a
// part to the next row at a space.
func wrapPath(path string, width int) []string {
	var rows []string
	for displayWidth(path) > width {
		head, tail := splitColumns(path, width)
		if head == "" {
			head, tail = truncateMarker, tail[len(firstGrapheme(tail)):]
		}
		rows = append(rows, head)
		path = tail
	}
	return append(rows, path)
}

// spansBlock is a block of rows of spans, each row with its own indent.
type spansRow struct {
	indent int
	spans  []span
}

// indentRows adds the indent to wrapped rows: the first row gets first and the
// later rows get cont.
func indentRows(rows [][]span, first, cont int) []spansRow {
	out := make([]spansRow, len(rows))
	for i, r := range rows {
		ind := cont
		if i == 0 {
			ind = first
		}
		out[i] = spansRow{indent: ind, spans: r}
	}
	return out
}

func drawSpansRows(th *Theme, rows []spansRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = strings.Repeat(" ", r.indent) + renderSpans(th, r.spans)
	}
	return out
}

// textRows wraps spans for the pane at an indent: the first row starts at
// column first and the later rows at column cont. Each row ends textRightPad
// columns before the right edge.
func textRows(rc RenderContext, spans []span, first, cont int) []spansRow {
	return indentRows(wrapSpans(spans, textWidth(rc, first), textWidth(rc, cont)), first, cont)
}

// ---- explanation ----

// trustExplainBlock is the explanation of the startup trust box: the folder,
// its gated entries, what trust loads and what trust does not do.
type trustExplainBlock struct {
	root string
	// rel are the gated entries in the root; abs are the entries of the
	// ancestors, with the home as "~".
	rel, abs []string
}

func newTrustExplain(res protocol.ACPTrustStateResult, cwd, home string) trustExplainBlock {
	b := trustExplainBlock{root: safeText(tildePath(trustRoot(res, cwd), home))}
	for _, g := range res.Gated {
		if filepath.IsAbs(g) {
			b.abs = append(b.abs, safeText(tildePath(g, home)))
			continue
		}
		b.rel = append(b.rel, safeText(g))
	}
	return b
}

func (b trustExplainBlock) Lines(rc RenderContext) []string {
	rows := textRows(rc, []span{{b.root, RoleFg, true}, {" has project files that Ask has not read or run:", RoleFg, false}}, glyphIndent, glyphIndent)
	type fileRow struct {
		row     spansRow
		entries int
	}
	var files []fileRow
	if len(b.rel) > 0 {
		for _, r := range textRows(rc, []span{{strings.Join(b.rel, "  "), RoleCode, false}}, textIndent, textIndent) {
			n := 0
			for _, sp := range r.spans {
				if strings.TrimSpace(sp.text) != "" {
					n++
				}
			}
			files = append(files, fileRow{r, n})
		}
	}
	for _, p := range b.abs {
		for i, part := range wrapPath(p, textWidth(rc, textIndent)) {
			n := 0
			if i == 0 {
				n = 1
			}
			files = append(files, fileRow{spansRow{textIndent, []span{{part, RoleCode, false}}}, n})
		}
	}
	if len(files) > 4 {
		hidden := 0
		for _, f := range files[3:] {
			hidden += f.entries
		}
		files = append(files[:3], fileRow{row: spansRow{textIndent, []span{{fmt.Sprintf("… %d more", hidden), RoleMuted, false}}}})
	}
	for _, f := range files {
		rows = append(rows, f.row)
	}
	rows = append(rows, textRows(rc, []span{{"If you trust this folder, Ask loads its project settings, system prompt files, " +
		"skills, prompts, extensions and MCP config.", RoleFg, false}}, glyphIndent, glyphIndent)...)
	rows = append(rows, textRows(rc, []span{{"Trust does not give permission to run shell commands or tools. " +
		"Trust does not apply to AGENTS.md or CLAUDE.md.", RoleFg, false}}, glyphIndent, glyphIndent)...)
	return drawSpansRows(rc.Theme, rows)
}

// trustRoot is the canonical root of a trust answer, or the cwd when the
// answer has none.
func trustRoot(res protocol.ACPTrustStateResult, cwd string) string {
	if res.Root != "" {
		return res.Root
	}
	return cwd
}

// ---- notes ----

// trustOKNote is a result note with the success glyph.
type trustOKNote struct{ text string }

func (b trustOKNote) Lines(rc RenderContext) []string {
	th := rc.Theme
	body := styleRows(th, RoleFg, wrapLines(b.text, textWidth(rc, textIndent)))
	return leadRows(th.Render(RoleSuccess, GlyphOK), body)
}

// ---- settings problems ----

// diagnosticsBlock is a list of settings problems, grouped by file in the
// order of the list. The heading is "Settings problems" unless it is set. A
// problem of a command line flag has no file, so it has no path row.
type diagnosticsBlock struct {
	heading string
	items   []protocol.ACPDiagnostic
	home    string
}

func (b diagnosticsBlock) Lines(rc RenderContext) []string {
	heading := b.heading
	if heading == "" {
		heading = "Settings problems"
	}
	rows := textRows(rc, []span{{heading, RoleWarning, true}}, glyphIndent, glyphIndent)
	shown := b.items
	if len(shown) > diagnosticRowsMax {
		shown = shown[:diagnosticRowsMax]
	}
	path := ""
	for i, d := range shown {
		if (i == 0 || d.Path != path) && d.Path != "" {
			path = d.Path
			for _, part := range wrapPath(safeText(tildePath(d.Path, b.home)), textWidth(rc, textIndent)) {
				rows = append(rows, spansRow{textIndent, []span{{part, RoleMuted, false}}})
			}
		}
		var spans []span
		if d.Key != "" {
			spans = append(spans, span{safeText(d.Key) + ": ", RoleFg, true})
		}
		spans = append(spans, span{safeText(d.Text), RoleMuted, false})
		rows = append(rows, textRows(rc, spans, 6, 8)...)
	}
	if hidden := len(b.items) - len(shown); hidden > 0 {
		rows = append(rows, spansRow{textIndent, []span{{fmt.Sprintf("… %d more · ask settings show", hidden), RoleMuted, false}}})
	}
	return drawSpansRows(rc.Theme, rows)
}
