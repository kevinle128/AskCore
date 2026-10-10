package tui

// Messages that change the transcript. The session view turns agent events
// into these messages; the root Update applies them in arrival order.

// AppendBlockMsg adds a block at the end of the transcript. A stable block
// has final content and may be committed when every earlier block is stable.
type AppendBlockMsg struct {
	ID     string
	Block  Block
	Stable bool
}

// ReplaceBlockMsg changes the content of a block that is still in the live
// tail. A block that is already in the scrollback cannot change.
type ReplaceBlockMsg struct {
	ID    string
	Block Block
}

// FinishBlockMsg marks a block stable.
type FinishBlockMsg struct{ ID string }

// RebuildMsg asks for the committed rows to be drawn again at the current
// size, for example after the thinking blocks are hidden or shown.
type RebuildMsg struct{}
