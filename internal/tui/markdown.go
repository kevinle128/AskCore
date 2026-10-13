package tui

import (
	"regexp"
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	xansi "github.com/charmbracelet/x/ansi"
)

// Markdown rendering for assistant replies.
//
// Glamour draws a whole document and has no incremental form, and it draws a
// half-written construct (an open fence, a list, a table) badly. A reply is
// therefore cut into chunks (see markdown_split.go). Each chunk is drawn alone
// and the rows are cached by chunk text. While a reply streams, only chunks
// whose text changed are drawn again, which is the open tail. The result is a
// function of the text, the width and the theme only, so the streamed rows
// and the rows of a fresh render of the same text are equal.

// mdRow is one row of a rendered reply. A panel row belongs to a code block:
// the block that places the rows draws the panel color under the indent too.
type mdRow struct {
	text  string
	panel bool
}

// markdownStream caches the rows of the chunks of one reply. The cache is
// dropped when the width or the theme changes. It is safe for concurrent use;
// the mutex also serializes the glamour renderers, which are not.
type markdownStream struct {
	mu     sync.Mutex
	width  int
	theme  *Theme
	chunks []cachedChunk
	// renders counts drawn chunks. Tests use it to prove that closed chunks
	// are drawn once.
	renders int
}

type cachedChunk struct {
	chunk mdChunk
	rows  []mdRow
}

func newMarkdownStream() *markdownStream { return &markdownStream{} }

// render draws src for a width. stable is the number of leading rows that
// cannot change when more text arrives: the rows of closed chunks and the blank
// rows between them.
func (s *markdownStream) render(src string, width int, th *Theme) (rows []mdRow, stable int) {
	width = max(1, width)
	s.mu.Lock()
	defer s.mu.Unlock()
	// A theme never changes after it is built, so the pointer tells whether the
	// style output of the cached rows is still valid.
	if s.width != width || s.theme != th {
		s.chunks = nil
	}
	s.width, s.theme = width, th

	chunks := splitMarkdown(sanitizeMarkdown(src))
	if len(s.chunks) > len(chunks) {
		s.chunks = s.chunks[:len(chunks)]
	}
	stableChunks := 0
	for i, c := range chunks {
		if i < len(s.chunks) && s.chunks[i].chunk.code == c.code && s.chunks[i].chunk.lang == c.lang && s.chunks[i].chunk.body == c.body {
			s.chunks[i].chunk.closed = c.closed
		} else {
			s.chunks = append(s.chunks[:i], cachedChunk{chunk: c, rows: s.draw(c, width, th)})
		}
		if c.closed && stableChunks == i {
			stableChunks = i + 1
		}
		if i > 0 {
			rows = append(rows, mdRow{})
		}
		rows = append(rows, s.chunks[i].rows...)
		if stableChunks == i+1 {
			stable = len(rows)
		}
	}
	return rows, stable
}

// sanitizeMarkdown removes escape sequences and control characters. Model text
// must not move the cursor or restyle the terminal.
func sanitizeMarkdown(src string) string {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	return cleanText(xansi.Strip(src))
}

// draw renders one chunk. Callers hold s.mu.
func (s *markdownStream) draw(c mdChunk, width int, th *Theme) []mdRow {
	s.renders++
	if c.code {
		return codeRows(c.lang, c.body, width, th)
	}
	return proseRows(c.body, width, th)
}

// proseRows draws prose with glamour. If glamour fails, the text is shown as
// plain wrapped rows, so a reply is never lost.
func proseRows(body string, width int, th *Theme) []mdRow {
	r, err := glamourRenderer(width, th)
	if err == nil {
		var out string
		glamourMu.Lock()
		out, err = r.Render(body)
		glamourMu.Unlock()
		if err == nil {
			return glamourRows(out, width)
		}
	}
	var rows []mdRow
	for _, row := range wrapLines(body, width) {
		rows = append(rows, mdRow{text: row})
	}
	return trimBlankRows(rows)
}

// glamourRows splits glamour output into rows. Blank rows at both ends and
// spaces at the end of a row are removed; a row wider than the width (a wide
// table) is wrapped so that no row can overflow the terminal.
func glamourRows(out string, width int) []mdRow {
	var rows []mdRow
	for _, line := range strings.Split(out, "\n") {
		line = dropNoiseSequences(trimRowEnd(hyperlinkSequence.ReplaceAllString(line, "")))
		for _, part := range hardWrapRows(line, width) {
			rows = append(rows, mdRow{text: part})
		}
	}
	return trimBlankRows(rows)
}

var (
	// hyperlinkSequence is an OSC 8 link. Glamour also prints the address after
	// the link text, so the information stays, and a wrapped link row cannot
	// leave a link open.
	hyperlinkSequence = regexp.MustCompile(`\x1b\]8;[^\x07\x1b]*(?:\x07|\x1b\\)`)
	// emptyStyle is a style that is set and reset with no text between.
	emptyStyle = regexp.MustCompile(`\x1b\[[0-9;]*m\x1b\[0?m`)
)

// dropNoiseSequences removes the link sequences and the empty style pairs
// that glamour writes for padding.
func dropNoiseSequences(row string) string {
	row = hyperlinkSequence.ReplaceAllString(row, "")
	for {
		next := emptyStyle.ReplaceAllString(row, "")
		if next == row {
			return row
		}
		row = next
	}
}

func trimBlankRows(rows []mdRow) []mdRow {
	for len(rows) > 0 && xansi.Strip(rows[0].text) == "" {
		rows = rows[1:]
	}
	for len(rows) > 0 && xansi.Strip(rows[len(rows)-1].text) == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

// trimRowEnd removes spaces at the end of a row, also when style sequences
// follow them.
func trimRowEnd(row string) string {
	plain := xansi.Strip(row)
	trimmed := strings.TrimRight(plain, " ")
	if len(trimmed) == len(plain) {
		return row
	}
	// Cut the row at the column where the trailing spaces start and keep the
	// style reset that closes the text.
	cut := xansi.Truncate(row, displayWidth(trimmed), "")
	if strings.Contains(row, "\x1b[") && !strings.HasSuffix(cut, "\x1b[m") && !strings.HasSuffix(cut, "\x1b[0m") {
		cut += "\x1b[m"
	}
	return cut
}

// glamourMu serializes calls into glamour renderers, which keep state while
// they render.
var glamourMu sync.Mutex

type glamourKey struct {
	width int
	mode  colorMode
}

var (
	glamourCacheMu sync.Mutex
	glamourCache   = map[glamourKey]*glamour.TermRenderer{}
)

// glamourRenderer returns the renderer for a width and color mode. There is
// one renderer for each key: building one parses the style tables.
func glamourRenderer(width int, th *Theme) (*glamour.TermRenderer, error) {
	key := glamourKey{width: max(1, width), mode: th.mode}
	glamourCacheMu.Lock()
	defer glamourCacheMu.Unlock()
	if r, ok := glamourCache[key]; ok {
		return r, nil
	}
	r, err := glamour.NewTermRenderer(glamour.WithStyles(markdownStyle(th.mode)), glamour.WithWordWrap(key.width))
	if err != nil {
		return nil, err
	}
	if len(glamourCache) > 64 {
		// Terminal resizes make many widths; keep the table small.
		clear(glamourCache)
	}
	glamourCache[key] = r
	return r, nil
}

func ptr[T any](v T) *T { return &v }

// roleColor returns the glamour color string of a role: a hex value in
// truecolor, an index in 256 colors, and nil without color.
func roleColor(mode colorMode, r Role) *string {
	spec := palette[r]
	switch mode {
	case modeTrue:
		if spec.fg != "" {
			return ptr(spec.fg)
		}
	case mode256:
		if spec.fg256 != "" {
			return ptr(spec.fg256)
		}
	}
	return nil
}

// markdownStyle makes the glamour style of a color mode from the theme roles.
// The reply block draws the margins and the indent, so the document has none.
func markdownStyle(mode colorMode) ansi.StyleConfig {
	var cfg ansi.StyleConfig
	if mode == modeNone {
		cfg = styles.NoTTYStyleConfig
	} else {
		cfg = styles.DarkStyleConfig
	}
	cfg.Document = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: roleColor(mode, RoleFg)}, Margin: ptr(uint(0))}
	if mode == modeNone {
		// Without color, emphasis uses text attributes, which a terminal that
		// has no color can still show, instead of the marker characters that
		// the plain style uses.
		cfg.Strong = ansi.StylePrimitive{Bold: ptr(true)}
		cfg.Emph = ansi.StylePrimitive{Italic: ptr(true)}
		cfg.Strikethrough = ansi.StylePrimitive{CrossedOut: ptr(true)}
		cfg.Link = ansi.StylePrimitive{Underline: ptr(true)}
		cfg.Code = ansi.StyleBlock{}
		cfg.CodeBlock = ansi.StyleCodeBlock{StyleBlock: ansi.StyleBlock{Margin: ptr(uint(0))}}
		return cfg
	}
	heading := ansi.StylePrimitive{Color: roleColor(mode, RoleAssistant), Bold: ptr(true)}
	cfg.Heading = ansi.StyleBlock{StylePrimitive: heading}
	for i, h := range []*ansi.StyleBlock{&cfg.H1, &cfg.H2, &cfg.H3, &cfg.H4, &cfg.H5, &cfg.H6} {
		*h = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: strings.Repeat("#", i+1) + " ", Color: roleColor(mode, RoleAssistant), Bold: ptr(true)}}
	}
	cfg.Strong = ansi.StylePrimitive{Bold: ptr(true)}
	cfg.Emph = ansi.StylePrimitive{Italic: ptr(true)}
	cfg.Code = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: roleColor(mode, RoleCode)}}
	cfg.Link = ansi.StylePrimitive{Color: roleColor(mode, RoleTool), Underline: ptr(true)}
	cfg.LinkText = ansi.StylePrimitive{Color: roleColor(mode, RoleTool)}
	cfg.BlockQuote = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{Color: roleColor(mode, RoleMuted)},
		Indent:         ptr(uint(1)),
		IndentToken:    ptr("│ "),
	}
	cfg.HorizontalRule = ansi.StylePrimitive{Color: roleColor(mode, RoleDim), Format: "--------"}
	cfg.CodeBlock = ansi.StyleCodeBlock{StyleBlock: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{Color: roleColor(mode, RoleCode)},
		Margin:         ptr(uint(0)),
	}}
	return cfg
}
