# `internal/tui`

## Status

T1a and T1c are implemented. The package holds the root `Model`, the live region layout, the transcript commit path with resize rebuild, the width helpers, the theme, the editor, the transcript blocks and markdown, the event translator, status, queue, footer, selectors and connection states.
The session view talks to an agent through the `Client` interface in [client.go](client.go). The implementation is [`internal/tuiclient`](../tuiclient), and `cmd/tui` supplies it: it connects to the leader and falls back to an agent in process only when the connect fails at startup (see [interactive.go](../../cmd/tui/interactive.go)).
`ask` with no arguments on a terminal runs this package; see [Session view contract](#session-view-contract).
Extension composition (widgets and extension views) and key resolution are comment-only scaffolds. The provider boxes, the key and login boxes and the removal box are implemented dialogs.
T1c adds `/providers`, the model line (start and change lines), the first run with no model, and a `/model` list of usable models only; see [Provider setup contract](#provider-setup-contract). The plan is [t1c-provider-setup](../../plans/261010-1424-t1c-provider-setup/plan.md) and its [design](../../plans/261010-1424-t1c-provider-setup-design/design.md) is the UI authority.
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
| Boxes (provider list, key, login and removal) | [dialog.go](dialog.go), [providers_box.go](providers_box.go), [login_box.go](login_box.go), [update_providers.go](update_providers.go) |
| Secret input (key, code, callback address) | [secret_input.go](secret_input.go) |
| Model line (start and change lines) | [model_line.go](model_line.go) |
| Extension composition (scaffold) | [widget.go](widget.go), [update_extension.go](update_extension.go) |
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

## Provider setup contract

- `/providers` (any letter case) opens the provider box in the editor slot and keeps the draft. It has seven rows in the catalog order and a search row. The rows come from `Client.Providers`; every provider call needs the leader. With the in-process agent, `Client` returns `ErrNeedsLeader` and the TUI shows `Needs the leader; this session runs in process.` and the shell command `ask auth login`.
- A key, a code or a pasted callback address lives only in the buffer of [secret_input.go](secret_input.go). The screen shows the fixed text `(hidden)`. The text never reaches the editor, the prompt history, the undo or kill ring, a block, a note, the title, the bell or the clipboard, and the buffer is zeroed on every exit of the box. A paste with a line break or a space inside is refused without an echo.
- The call that carries a secret goes out once and the box does not keep it afterwards. A late result of a cancelled sign-in is dropped by its login id and request generation.
- `TEA_TRACE` is the one stated exception: Bubble Tea writes raw input to that file. When the variable is set, the TUI shows a warning with the file name once at start, and on the key box and the code input ([model_line.go](model_line.go)).
- With no model (`ACPStateResult.ModelID` empty) there is no model line, the footer shows `no model`, and the rail shows a notice ([status.go](status.go)). A prompt is refused with `Not sent: no model`, and the draft stays.
- A model line `  model  <provider>/<model>` follows the header, a divider that starts an attach, and a restore note; a change line follows a model event of the leader with the current epoch. The footer and the reply meta line keep the bare model id. A start under a fallback shows the reason under the pair.
- A choice in `/model` switches the session and the leader saves it as the default (`setResult("Default model: …")` when `defaultSaved` is true). Signing in does not change the default or the current model.
