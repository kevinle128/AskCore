package tui

// Messages that change the transcript. The session view turns agent events
// into these messages; the root Update applies them in arrival order.

// AppendBlockMsg adds a block at the end of the transcript. A stable block
// has final content.
type AppendBlockMsg struct {
	ID     string
	Block  Block
	Stable bool
}

// ReplaceBlockMsg changes the content of a block.
type ReplaceBlockMsg struct {
	ID    string
	Block Block
}

// FinishBlockMsg marks a block stable.
type FinishBlockMsg struct{ ID string }
