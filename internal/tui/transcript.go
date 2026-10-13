package tui

import (
	"errors"
	"fmt"
	"strings"
)

// Transcript component.
//
// The transcript owns ordered blocks and their content versions. It does not
// own rendered lines: a block draws itself for a width when asked, so a
// resize or a theme change can draw the same content again.
//
// A block is stable when its content is final. The frontier is the count of
// leading stable blocks.

// Block is one unit of transcript content: a prompt, a reply, a tool line or a
// note. Lines returns terminal rows without line breaks. Rows should fit the
// width; the transcript wraps any row that does not.
type Block interface {
	Lines(rc RenderContext) []string
}

// RenderContext is what a block needs to draw itself.
type RenderContext struct {
	Width int
	Theme *Theme
	// Fold is the fold mode that the block pane keeps for the block. Blocks
	// that cannot fold ignore it. The viewer of one block draws with
	// FoldExpanded.
	Fold FoldMode
	// Pointed is true while the block is selected or under the mouse pointer. A
	// foldable step then shows its expand indicator in place of its glyph.
	Pointed bool
}

// Errors of transcript changes. A rejected change leaves the transcript as it
// was.
var (
	ErrBlockID        = errors.New("tui: block id is empty")
	ErrBlockContent   = errors.New("tui: block has no content")
	ErrDuplicateBlock = errors.New("tui: block id already exists")
	ErrUnknownBlock   = errors.New("tui: unknown block id")
)

type entry struct {
	id      string
	block   Block
	version uint64
	stable  bool
}

// Transcript holds the blocks of one session view.
type Transcript struct {
	entries []*entry
	index   map[string]int
}

// NewTranscript returns an empty transcript.
func NewTranscript() *Transcript {
	return &Transcript{index: make(map[string]int)}
}

// Append adds a block at the end. A stable block has final content.
func (t *Transcript) Append(id string, b Block, stable bool) error {
	switch {
	case id == "":
		return ErrBlockID
	case b == nil:
		return ErrBlockContent
	}
	if _, ok := t.index[id]; ok {
		return fmt.Errorf("%w: %q", ErrDuplicateBlock, id)
	}
	t.index[id] = len(t.entries)
	t.entries = append(t.entries, &entry{id: id, block: b, version: 1, stable: stable})
	return nil
}

// Replace changes the content of a block.
func (t *Transcript) Replace(id string, b Block) error {
	if b == nil {
		return ErrBlockContent
	}
	e, err := t.find(id)
	if err != nil {
		return err
	}
	e.block = b
	e.version++
	return nil
}

// Finish marks a block stable. The block counts for the frontier when every
// earlier block is stable too.
func (t *Transcript) Finish(id string) error {
	e, err := t.find(id)
	if err != nil {
		return err
	}
	e.stable = true
	return nil
}

func (t *Transcript) find(id string) (*entry, error) {
	i, ok := t.index[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownBlock, id)
	}
	return t.entries[i], nil
}

// Version returns the content version of a block. It grows on each Replace.
func (t *Transcript) Version(id string) (uint64, bool) {
	e, err := t.find(id)
	if err != nil {
		return 0, false
	}
	return e.version, true
}

// Len returns the number of blocks.
func (t *Transcript) Len() int { return len(t.entries) }

// Frontier returns the number of leading blocks that are stable.
func (t *Transcript) Frontier() int {
	n := 0
	for n < len(t.entries) && t.entries[n].stable {
		n++
	}
	return n
}

// renderRows draws a block and enforces the row contract: no row holds a line
// break and no row is wider than the terminal, because the terminal would wrap
// it and the row count of the pane would be wrong.
func renderRows(b Block, rc RenderContext) []string {
	var rows []string
	for _, line := range b.Lines(rc) {
		for _, part := range strings.Split(line, "\n") {
			rows = append(rows, hardWrapRows(part, rc.Width)...)
		}
	}
	return rows
}
