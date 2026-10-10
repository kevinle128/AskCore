package tui

// Focus names the component that receives input after the host shortcuts.
// The root model owns it. Showing a selector or a dialog moves focus there
// without touching the draft; closing it gives focus back only when the
// target still exists.
type Focus int

// Focus targets.
const (
	FocusEditor Focus = iota
	FocusSelector
	FocusDialog
)

func (f Focus) String() string {
	switch f {
	case FocusEditor:
		return "editor"
	case FocusSelector:
		return "selector"
	case FocusDialog:
		return "dialog"
	}
	return "unknown"
}
