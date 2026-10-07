package tea

type inlineReplayMsg struct {
	Width, Height int
	Generation    uint64
	Entries       []string
}

// InlineReplayResult reports reconstruction without acknowledging a commit.
type InlineReplayResult struct {
	Generation uint64
	Entries    int
	Stale      bool
	Err        error
}

// ReplayInlineTranscript repairs a settled resize through the renderer owner.
// Generation must come from the latest WindowSizeMsg.
func ReplayInlineTranscript(width, height int, generation uint64, entries []string) Cmd {
	snapshot := append([]string(nil), entries...)
	return func() Msg { return inlineReplayMsg{width, height, generation, snapshot} }
}
