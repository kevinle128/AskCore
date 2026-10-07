# `internal/tui`

## Status

This package is a scaffold.
It has no runtime implementation.
The current interactive entry point remains in `cmd/tui`.
See the [TUI architecture](../../docs/tui-architecture.md) for the accepted boundaries and validation gates.

## Ownership

This package owns the root Bubble Tea model, session projections, local drafts, focus, layout, and components.
It owns the editor, transcript, selectors, completion, status, themes, and inline rendering.
It applies extension view updates through the same root update loop.
It does not own agent execution, transport framing, or extension process management.

## State and rendering rules

Only the root update loop may change retained UI state.
Run I/O in commands and return results as messages with the relevant session, run, request, and generation identifiers.
Components must not call providers, sockets, or stores.
The agent is the authority for execution state and the settled boundary.
Keep transcript data separate from rendered terminal lines.
Commit only a stable ordered prefix; keep the remaining blocks in the live area.
Use one ordered terminal output path for scrollback and live rendering.
Keep the application transcript as the source of truth across resize.
The output owner can rebuild native scrollback after resize settles, without advancing the commit frontier or changing editor state.
See the [inline transcript contract](../../docs/tui-architecture.md#inline-transcript) for the accepted repair boundary.

## Component owners

These files are comment-only scaffolds, not working components.
No public component API or Bubble Tea version-specific model is defined yet.

| Responsibility | Scaffold owner |
|---|---|
| Root composition | [model.go](model.go) |
| Session projection | [session.go](session.go), [update_session.go](update_session.go) |
| Client I/O and stream waits | [commands.go](commands.go) |
| Input and focus | [update_input.go](update_input.go), [focus.go](focus.go), [keys.go](keys.go) |
| Editor and history | [editor.go](editor.go), [editor_history.go](editor_history.go) |
| Transcript blocks | [transcript.go](transcript.go) |
| Queue and execution presentation | [pending.go](pending.go), [status.go](status.go), [footer.go](footer.go) |
| Selection and completion | [selector.go](selector.go), [completion.go](completion.go) |
| Extension composition | [widget.go](widget.go), [dialog.go](dialog.go), [update_extension.go](update_extension.go) |
| Geometry, theme, and output | [layout.go](layout.go), [theme.go](theme.go), [render_inline.go](render_inline.go) |

The [concurrency design](../../docs/tui-architecture.md#channels-and-goroutines) owns channel and goroutine rules.

## File convention

Use `model.go`, `state.go`, `update_*.go`, `commands.go`, and `layout.go` for coordination.
Use capability prefixes such as `editor_`, `transcript_`, and `render_` for component files.
Create files only when implementation requires them.

## Imports

Allowed: `pkg/protocol`, `pkg/askui`, `internal/tuiext`, and terminal UI dependencies.
Denied: `internal/agent`, `internal/acp`, `internal/leader`, `internal/providers`, `internal/tools`, storage implementations, and `internal/config`.
The composition root supplies the client and extension host through constructors.
Core packages must not import this package.
