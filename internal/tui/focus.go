package tui

// Focus names the component that receives input after the host shortcuts.
// The root model owns it. Showing a selector or a dialog moves focus there
// without touching the draft; closing it gives focus back to the editor, the
// prompt, because the selector took the place of the prompt pane. The block
// list has focus only after the user moved there with Tab.
type Focus int

// Focus targets.
const (
	FocusEditor Focus = iota
	FocusSelector
	FocusDialog
	// FocusBlocks is the block list: the keys select and fold blocks.
	FocusBlocks
)

func (f Focus) String() string {
	switch f {
	case FocusEditor:
		return "editor"
	case FocusSelector:
		return "selector"
	case FocusDialog:
		return "dialog"
	case FocusBlocks:
		return "blocks"
	}
	return "unknown"
}
