package tui

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
)

// Fenced code blocks.
//
// The block is drawn on the code panel. The first row is the language name in
// the dim role (it is left out when the fence names no language), then the
// code. Rows fill the width so that the panel color is a clean band. Without
// color the panel has no color, and the rows are plain text.

var (
	codeStyleOnce sync.Once
	codeStyle     *chroma.Style
)

// askCodeStyle is a chroma style made of the theme roles, so that code uses the
// same few colors as the rest of the UI. Names, which are most tokens, keep the
// code role.
func askCodeStyle() *chroma.Style {
	codeStyleOnce.Do(func() {
		hex := func(r Role) string { return palette[r].fg }
		codeStyle = chroma.MustNewStyle("ask", chroma.StyleEntries{
			chroma.Text:            hex(RoleCode),
			chroma.Error:           hex(RoleError),
			chroma.Comment:         "italic " + hex(RoleMuted),
			chroma.Keyword:         hex(RoleAssistant),
			chroma.KeywordType:     hex(RoleTool),
			chroma.NameFunction:    hex(RoleTool),
			chroma.NameClass:       hex(RoleTool),
			chroma.NameBuiltin:     hex(RoleTool),
			chroma.LiteralString:   hex(RoleSuccess),
			chroma.LiteralNumber:   hex(RoleWarning),
			chroma.Operator:        hex(RoleFg),
			chroma.Punctuation:     hex(RoleFg),
			chroma.GenericDeleted:  hex(RoleError),
			chroma.GenericInserted: hex(RoleSuccess),
		})
	})
	return codeStyle
}

// codeRows draws the code of a fenced block for a width.
func codeRows(lang, body string, width int, th *Theme) []mdRow {
	var rows []mdRow
	panel := func(text string) {
		rows = append(rows, mdRow{text: panelRow(text, width, th), panel: true})
	}
	if lang != "" {
		panel(th.Render(RoleDim, truncateTo(cleanText(lang), width)))
	}
	if body != "" {
		for _, line := range highlightLines(lang, body, width, th) {
			panel(line)
		}
	}
	return rows
}

// panelRow puts a row on the code panel: it keeps the panel color after every
// style reset in the row and fills the row to the width. Without color it
// returns the row as it is.
func panelRow(text string, width int, th *Theme) string {
	bg := bgSequence(th, RoleCodePanel)
	if bg == "" {
		return text
	}
	text = strings.ReplaceAll(text, "\x1b[0m", "\x1b[0m"+bg)
	text = strings.ReplaceAll(text, "\x1b[m", "\x1b[m"+bg)
	return bg + padTo(text, width) + "\x1b[0m"
}

// bgSequence is the escape sequence that sets the background of a role, or an
// empty string when the theme has no color.
func bgSequence(th *Theme, r Role) string {
	spec := palette[r]
	switch th.mode {
	case modeTrue:
		if len(spec.bg) == 7 && spec.bg[0] == '#' {
			rr, e1 := strconv.ParseUint(spec.bg[1:3], 16, 8)
			gg, e2 := strconv.ParseUint(spec.bg[3:5], 16, 8)
			bb, e3 := strconv.ParseUint(spec.bg[5:7], 16, 8)
			if e1 == nil && e2 == nil && e3 == nil {
				return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", rr, gg, bb)
			}
		}
	case mode256:
		if spec.bg256 != "" {
			return "\x1b[48;5;" + spec.bg256 + "m"
		}
	}
	return ""
}

// highlightLines returns the rows of the code, wrapped to the width and
// highlighted when the theme has color and the language is known. A line that
// does not fit is wrapped as plain text: wrapping highlighted text would split
// style sequences from the text they style.
func highlightLines(lang, body string, width int, th *Theme) []string {
	code := cleanText(body)
	plain := strings.Split(code, "\n")

	var styled []string
	if th.mode != modeNone && lang != "" {
		styled = highlight(lang, code, th)
	}
	var out []string
	for i, line := range plain {
		if styled != nil && i < len(styled) && displayWidth(line) <= width {
			out = append(out, styled[i])
			continue
		}
		for _, row := range hardWrapRows(line, width) {
			out = append(out, th.Render(RoleCode, row))
		}
	}
	return out
}

// highlight colors code with chroma and returns one string for each line, or
// nil when the language is unknown or the lexer fails.
func highlight(lang, code string, th *Theme) []string {
	lexer := lexers.Get(lang)
	if lexer == nil {
		return nil
	}
	lexer = chroma.Coalesce(lexer)
	it, err := lexer.Tokenise(nil, code)
	if err != nil {
		return nil
	}
	formatter := formatters.TTY16m
	if th.mode == mode256 {
		formatter = formatters.TTY256
	}
	lines := chroma.SplitTokensIntoLines(it.Tokens())
	out := make([]string, 0, len(lines))
	for _, tokens := range lines {
		var buf bytes.Buffer
		if err := formatter.Format(&buf, askCodeStyle(), chroma.Literator(tokens...)); err != nil {
			return nil
		}
		out = append(out, strings.TrimRight(buf.String(), "\r\n"))
	}
	// The lexer reports the same lines as the text, with at most one more row
	// after a final line break.
	if want := strings.Count(code, "\n") + 1; len(out) < want {
		return nil
	}
	return out
}
