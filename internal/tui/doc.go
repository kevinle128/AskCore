// Package tui owns the terminal UI, client view state, and component composition.
//
// The root Model, the live region layout, the transcript commit path, the
// width helpers, the theme, the editor, the transcript blocks, the event
// translator and the session view are implemented. The session view
// reads and writes an agent only through the Client interface. See README.md
// for the boundaries.
package tui
