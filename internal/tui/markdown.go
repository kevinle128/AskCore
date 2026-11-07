package tui

import (
	"regexp"
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
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
// A boxed row belongs to a table.
type mdRow struct {
	text  string
	panel bool
	boxed bool
}

// textRow reports whether the row is a row of prose: not a row of a code
// panel and not a row of a table. Only a first row of prose shows the time of
// a reply.
func (r mdRow) textRow() bool { return !r.panel && !r.boxed }

// markdownStream caches the rows of the chunks of one reply. The cache is
// dropped when the width or the theme changes. It is safe for concurrent use;
// the mutex also serializes the glamour renderers, which are not.
type markdownStream struct {
	mu         sync.Mutex
	width      int
	firstWidth int
	theme      *Theme
	chunks     []cachedChunk
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
	return s.renderFirstRoom(src, width, width, th)
}

// renderFirstRoom is render, but a first chunk of prose is drawn firstWidth
// columns wide, so that its first row can leave room for the time at its
// right. A first chunk that starts with a table shows no time, so it is drawn
// at the full width. The source tells which width to use, so each chunk is
// drawn once for each change.
func (s *markdownStream) renderFirstRoom(src string, width, firstWidth int, th *Theme) (rows []mdRow, stable int) {
	width = max(1, width)
	firstWidth = min(width, max(1, firstWidth))
	s.mu.Lock()
	defer s.mu.Unlock()
	// A theme never changes after it is built, so the pointer tells whether the
	// style output of the cached rows is still valid.
	if s.width != width || s.firstWidth != firstWidth || s.theme != th {
		s.chunks = nil
	}
	s.width, s.firstWidth, s.theme = width, firstWidth, th

	chunks := splitMarkdown(sanitizeMarkdown(src))
	if len(s.chunks) > len(chunks) {
		s.chunks = s.chunks[:len(chunks)]
	}
	stableChunks := 0
	for i, c := range chunks {
		if i < len(s.chunks) && s.chunks[i].chunk.code == c.code && s.chunks[i].chunk.lang == c.lang && s.chunks[i].chunk.body == c.body {
			s.chunks[i].chunk.closed = c.closed
		} else {
			w := width
			if i == 0 && !c.code && !startsWithTable(c.body) {
				w = firstWidth
			}
			s.chunks = append(s.chunks[:i], cachedChunk{chunk: c, rows: s.draw(c, w, th)})
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
	return proseOrTableRows(c.body, width, th)
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
			return muteListNumbers(glamourRows(out, width), th.mode)
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

// headingRoles are the roles of the heading levels 1 to 6.
var headingRoles = [6]Role{RoleH1, RoleH2, RoleH3, RoleFg, RoleFg, RoleFg}

// Marks of reply markdown. The bullet and the list numbers use the muted
// role; the quote bar and the rule too.
const (
	markBullet    = "• "
	markNumberEnd = ". "
	markQuote     = "│"
	markRule      = "────────"
)

// markdownStyle makes the glamour style of a color mode from the theme roles.
// The reply block draws the margins and the indent, so the document has none.
// Without color roleColor gives no color, so the same rules keep only their
// text attributes: headings and inline code stay bold, and emphasis uses
// attributes instead of the marker characters of the plain style.
func markdownStyle(mode colorMode) ansi.StyleConfig {
	var cfg ansi.StyleConfig
	if mode == modeNone {
		cfg = styles.NoTTYStyleConfig
	} else {
		cfg = styles.DarkStyleConfig
	}
	color := func(r Role) *string { return roleColor(mode, r) }
	cfg.Document = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: color(RoleFg)}, Margin: ptr(uint(0))}

	// Headings show no "#" marks: the level is in the color. A blank row
	// follows a heading, as it follows a heading that ends a chunk.
	cfg.Heading = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{BlockSuffix: "\n", Bold: ptr(true)}}
	for i, h := range []*ansi.StyleBlock{&cfg.H1, &cfg.H2, &cfg.H3, &cfg.H4, &cfg.H5, &cfg.H6} {
		*h = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: color(headingRoles[i]), Bold: ptr(true)}}
	}
	cfg.Strong = ansi.StylePrimitive{Bold: ptr(true)}
	cfg.Emph = ansi.StylePrimitive{Italic: ptr(true)}
	cfg.Strikethrough = ansi.StylePrimitive{CrossedOut: ptr(true)}
	// Inline code shows no backticks.
	cfg.Code = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: color(RoleH2), Bold: ptr(true)}}
	// Glamour styles the Prefix of an item with the item style, but draws the
	// BlockPrefix and the number of an ordered item in the style of the list.
	// muteListNumbers colors the numbers after the render.
	cfg.Item = ansi.StylePrimitive{Color: color(RoleMuted), Prefix: markBullet}
	cfg.Enumeration = ansi.StylePrimitive{BlockPrefix: markNumberEnd}
	cfg.Link = ansi.StylePrimitive{Color: color(RoleTool), Underline: ptr(true)}
	cfg.LinkText = ansi.StylePrimitive{Color: color(RoleTool)}
	cfg.BlockQuote = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{Color: color(RoleMuted)},
		Indent:         ptr(uint(1)),
		IndentToken:    ptr(quoteBar(mode)),
	}
	cfg.HorizontalRule = ansi.StylePrimitive{Color: color(RoleMuted), Format: markRule}
	cfg.CodeBlock = ansi.StyleCodeBlock{StyleBlock: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{Color: color(RoleCode)},
		Margin:         ptr(uint(0)),
	}}
	return cfg
}

// quoteBar is the bar before each row of a block quote, in the muted role.
// Glamour draws the bar in the style of the block around the quote, so the bar
// carries its own color.
func quoteBar(mode colorMode) string {
	c := roleColor(mode, RoleMuted)
	if c == nil {
		return markQuote + " "
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(*c)).Render(markQuote) + " "
}
