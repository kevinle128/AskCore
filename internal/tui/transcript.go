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
// The commit frontier is the count of leading blocks that are stable. Stable
// blocks that sit behind an earlier mutable block stay in the live tail, so a
// later block never reaches the scrollback before an earlier one.

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
}

// Errors of transcript changes. A rejected change leaves the transcript as it
// was.
var (
	ErrBlockID        = errors.New("tui: block id is empty")
	ErrBlockContent   = errors.New("tui: block has no content")
	ErrDuplicateBlock = errors.New("tui: block id already exists")
	ErrUnknownBlock   = errors.New("tui: unknown block id")
	ErrCommitted      = errors.New("tui: block is already in the scrollback")
)

type entry struct {
	id        string
	block     Block
	version   uint64
	stable    bool
	committed bool
}

// Transcript holds the blocks of one session view.
type Transcript struct {
	entries   []*entry
	index     map[string]int
	committed int
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

// Replace changes the content of a block that is not in the scrollback yet.
func (t *Transcript) Replace(id string, b Block) error {
	if b == nil {
		return ErrBlockContent
	}
	e, err := t.find(id)
	if err != nil {
		return err
	}
	if e.committed {
		return fmt.Errorf("%w: %q", ErrCommitted, id)
	}
	e.block = b
	e.version++
	return nil
}

// Finish marks a block stable. The block can then pass the frontier when
// every earlier block is stable too.
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

// Committed returns the number of blocks that were handed to the scrollback.
func (t *Transcript) Committed() int { return t.committed }

// Frontier returns the number of leading blocks that are stable. Blocks
// before this index may be committed; later blocks stay in the live tail.
func (t *Transcript) Frontier() int {
	n := 0
	for n < len(t.entries) && t.entries[n].stable {
		n++
	}
	return n
}

// TakeStable returns the rows of the blocks between the committed count and
// the frontier, and marks those blocks committed. A block is returned once.
// The caller must write the rows to the scrollback in the order returned.
func (t *Transcript) TakeStable(rc RenderContext) []string {
	frontier := t.Frontier()
	var rows []string
	for ; t.committed < frontier; t.committed++ {
		e := t.entries[t.committed]
		e.committed = true
		rows = append(rows, renderRows(e.block, rc)...)
	}
	return rows
}

// CommittedLines draws every committed block again, for a rebuild of the
// scrollback after a resize. It does not move the frontier.
func (t *Transcript) CommittedLines(rc RenderContext) []string {
	var rows []string
	for _, e := range t.entries[:t.committed] {
		rows = append(rows, renderRows(e.block, rc)...)
	}
	return rows
}

// TailLines draws the blocks that are not committed yet.
func (t *Transcript) TailLines(rc RenderContext) []string {
	var rows []string
	for _, e := range t.entries[t.committed:] {
		rows = append(rows, renderRows(e.block, rc)...)
	}
	return rows
}

// renderRows draws a block and enforces the row contract: no row holds a line
// break and no row is wider than the terminal, because the terminal would wrap
// it and the row count of the live area would be wrong.
func renderRows(b Block, rc RenderContext) []string {
	var rows []string
	for _, line := range b.Lines(rc) {
		for _, part := range strings.Split(line, "\n") {
			rows = append(rows, hardWrapRows(part, rc.Width)...)
		}
	}
	return rows
}
