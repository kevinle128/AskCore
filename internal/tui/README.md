# `internal/tui`

## Status

T1a is implemented. The package holds the root `Model`, the live region layout, the transcript commit path with resize rebuild, the width helpers, the theme, the editor, the transcript blocks and markdown, the event translator, status, queue, footer, selectors and connection states.
The session view talks to an agent through the `Client` interface in [client.go](client.go). The implementation is [`internal/tuiclient`](../tuiclient), and `cmd/tui` supplies it: it connects to the leader and falls back to an agent in process only when the connect fails at startup (see [interactive.go](../../cmd/tui/interactive.go)).
`ask` with no arguments on a terminal runs this package; see [Session view contract](#session-view-contract).
Extension composition (widgets, dialogs, extension views) and key resolution are comment-only scaffolds.
See the [TUI architecture](../../docs/tui-architecture.md) for the accepted boundaries and the [validation gates](../../docs/tui-architecture.md#validation-and-remaining-decisions).

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

| Responsibility | Owner |
|---|---|
| Root composition and Update dispatch | [model.go](model.go), [messages.go](messages.go) |
| Resize settle and rebuild | [update_window.go](update_window.go) |
| Commit and rebuild output path | [render_inline.go](render_inline.go), [commands.go](commands.go) |
| Text width, wrap, truncate | [width.go](width.go) |
| Agent link contract (plain data types) | [client.go](client.go) |
| Session view state and live region slots | [session.go](session.go) |
| Client messages, command results, generations | [update_session.go](update_session.go) |
| Connection states (reconnect, restored, gone) | [reconnect.go](reconnect.go) |
| Quit flow | [quit.go](quit.go) |
| Input, commands and focus | [update_input.go](update_input.go), [focus.go](focus.go), [keys.go](keys.go) (scaffold) |
| Editor keys and pastes | [editor_keys.go](editor_keys.go), [editor_paste.go](editor_paste.go) |
| Editor state, layout and history | [editor.go](editor.go), [editor_layout.go](editor_layout.go), [editor_history.go](editor_history.go) |
| Transcript state and commit frontier | [transcript.go](transcript.go) |
| Transcript blocks and their text formats | [blocks.go](blocks.go), [blocks_format.go](blocks_format.go) |
| Markdown for replies (stable-prefix cache, chunks, code) | [markdown.go](markdown.go), [markdown_split.go](markdown_split.go), [markdown_code.go](markdown_code.go) |
| Event translation (events, errors, history snapshot) | [translate.go](translate.go), [translate_errors.go](translate_errors.go), [translate_snapshot.go](translate_snapshot.go) |
| Queue and execution presentation | [pending.go](pending.go), [status.go](status.go), [footer.go](footer.go) |
| Selection and completion | [selector.go](selector.go), [completion.go](completion.go) |
| Extension composition (scaffold) | [widget.go](widget.go), [dialog.go](dialog.go), [update_extension.go](update_extension.go) |
| Geometry and theme | [layout.go](layout.go), [theme.go](theme.go) |

Files marked "scaffold" are comment-only and have no behavior yet.

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

## Render core contract

- Components give rows to the live region through `SlotSource.Slots(width)`. Layout gives rows in the order of the design (editor, status, footer, pending, menu, tail) and draws the editor box. See [layout.go](layout.go).
- Session code changes the transcript with `AppendBlockMsg`, `ReplaceBlockMsg` and `FinishBlockMsg`. Blocks draw themselves for a `RenderContext` and use `RenderContext.Width`, which is the full terminal width.
- Only stable blocks at the front of the transcript reach the scrollback, one row per write, through `tea.Println`. The live frame never uses the last screen row, so the row that a write scrolls off is always history.
- `RebuildMsg` and `Model.Rebuild` redraw all committed rows with `tea.ReplayInlineTranscript`. A settled resize does the same after 120 ms.
- Tests render the model in a real Bubble Tea program, feed the output to an `x/vt` emulator and compare the plain grid with `testdata/*.golden`. Refresh with `go test ./internal/tui -run TestScreenGoldens -update` and review the diff.

## Session view contract

`New(Options{Client: c, Cwd, Version, Profile, ...})` builds a model whose live region comes from the session view. Use `Options.Open` instead of `Client` to show "Starting leader…" while the link opens.

- Start: `tea.NewProgram(m, tea.WithoutSignalHandler(), ...)`. `Init` creates the first session and starts the single wait on `Client.Events()`. The startup header is added after the first window size; rows are committed only after the first frame had time to reach the screen (`firstFrameDelay`).
- Stop: when `Run` returns, the caller closes the client (a leader client leaves its runs going; the in-process client stops its run). `Model.StartError` returns the error of `Options.Open`; `Model.ClientError` returns a fatal client error. Quit keys (ctrl+c on an empty draft, ctrl+d, `/quit`) call `requestQuit`, the one place that ends the program on a user request. A failed `Open` and a fatal client error end it too.
- Send: the draft stays in the editor until `PromptAcceptedMsg` arrives and is cleared only if the draft revision is unchanged. The prompt band comes from the user message event, never from the acceptance.
- State: the agent is the authority for run, queue, model and usage. Results of client calls carry the generation they started in; an older result changes nothing.
- Clock: `Options.Now` and `Options.TickInterval` exist for tests; a negative interval turns the animation clock off.
- Footer numbers: context use is the token total of the last reply over the context window of the current model; the cost is the session total from `Client.Usage`.
