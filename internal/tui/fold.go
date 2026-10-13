package tui

// FoldMode is how much of a block shows in the block pane.
//
// A block that cannot fold ignores the mode and draws the same rows for every
// value. The pane keeps one mode for each block and passes it to the block in
// RenderContext.
type FoldMode uint8

const (
	// FoldCollapsed is the one-row form of a block.
	FoldCollapsed FoldMode = iota
	// FoldTruncated shows the first and the last rows of the output.
	FoldTruncated
	// FoldExpanded shows all rows.
	FoldExpanded
)

// String returns the name of the mode, for tests and notices.
func (m FoldMode) String() string {
	switch m {
	case FoldCollapsed:
		return "collapsed"
	case FoldTruncated:
		return "truncated"
	case FoldExpanded:
		return "expanded"
	}
	return "unknown"
}

// foldBlock is implemented by blocks that the user can fold. A block that does
// not implement it is not foldable and is always drawn in FoldExpanded.
type foldBlock interface {
	// foldable reports whether the block draws more than one form at a width.
	foldable(width int) bool
	// defaultFold is the mode of the block while the user has not set one. The
	// pane asks again when the content of the block changes, so a block can
	// leave a streaming form when it ends.
	defaultFold() FoldMode
	// collapseFold is the lowest mode that the block has now: FoldCollapsed, or
	// a higher mode for a block that stays visible while it runs.
	collapseFold() FoldMode
	// foldGroup tells which fold-all commands move the block.
	foldGroup() foldGroup
}

// foldGroup is the class of a foldable block for the fold-all commands.
type foldGroup uint8

const (
	// groupNone is a block that only a direct fold command moves, such as a
	// long prompt.
	groupNone foldGroup = iota
	// groupTool is a tool step.
	groupTool
	// groupThinking is a thinking step.
	groupThinking
)

// isStep reports whether the fold-all command of steps moves the group.
func (g foldGroup) isStep() bool { return g != groupNone }

// blockDefaultFold returns the first mode of a block.
func blockDefaultFold(b Block) FoldMode {
	if fb, ok := b.(foldBlock); ok {
		return fb.defaultFold()
	}
	return FoldExpanded
}

// nextFold returns the mode that follows cur when the user cycles: collapsed,
// truncated, expanded, then back to the lowest mode of the block.
func nextFold(cur, lowest FoldMode) FoldMode {
	switch cur {
	case FoldCollapsed:
		return FoldTruncated
	case FoldTruncated:
		return FoldExpanded
	}
	return lowest
}
