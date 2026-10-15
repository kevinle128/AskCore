# `internal/tui`

## Status

T1a, T1b (tool output and the Grok-style alternate screen) and T1c are implemented. The package holds the root `Model`, the alternate screen layout (header row, block pane, status, queue, prompt pane, shortcut bar), the block pane with three fold modes per block, the block list and its keys, the one-block viewer, the mouse and the text selection, hook blocks, the width helpers, the theme, the editor, the transcript blocks and markdown, the event translator, status, queue, header, bar, selectors and connection states.
Hook blocks are built from fixtures only: the agent has no hook runtime, so no event produces one yet.
The session view talks to an agent through the `Client` interface in [client.go](client.go). The implementation is [`internal/tuiclient`](../tuiclient), and `cmd/tui` supplies it: it connects to the leader and falls back to an agent in process only when the connect fails at startup (see [interactive.go](../../cmd/tui/interactive.go)).
`ask` with no arguments on a terminal runs this package; see [Session view contract](#session-view-contract).
Extension composition (widgets and extension views) and key resolution are comment-only scaffolds. The provider boxes, the key and login boxes and the removal box are implemented dialogs.
T1c adds `/providers`, the model line (start and change lines), the first run with no model, and a `/model` list of usable models only; see [Provider setup contract](#provider-setup-contract). The plan is [t1c-provider-setup](../../plans/261010-1424-t1c-provider-setup/plan.md) and its [design](../../plans/261010-1424-t1c-provider-setup-design/design.md) is the UI authority.
See the [TUI architecture](../../docs/tui-architecture.md) for the accepted boundaries and the [validation gates](../../docs/tui-architecture.md#validation-and-remaining-decisions).

## Ownership

This package owns the root Bubble Tea model, session projections, local drafts, focus, layout, and components.
It owns the editor, transcript, block pane and block list, viewer, selectors, completion, status, themes, and the screen layout.
It applies extension view updates through the same root update loop.
It does not own agent execution, transport framing, or extension process management.

## State and rendering rules

Only the root update loop may change retained UI state.
Run I/O in commands and return results as messages with the relevant session, run, request, and generation identifiers.
Components must not call providers, sockets, or stores.
The agent is the authority for execution state and the settled boundary.
Keep transcript data separate from rendered terminal lines.
The block pane draws only the blocks that meet its window and keeps the rows of each drawn block.
Keep the application transcript as the source of truth across resize; the pane draws it again at the new size.
A resize does not change the editor state or the follow state of the pane.

## Component owners

| Responsibility | Owner |
|---|---|
| Root composition and Update dispatch | [model.go](model.go), [messages.go](messages.go) |
| Window size | [update_window.go](update_window.go) |
| Block pane (window anchor, follow mode, row cache, bottom marker data) | [pane.go](pane.go) |
| Fold modes and the fold interface of blocks | [fold.go](fold.go) |
| Block list (selection units, reveal, select by turn) and its keys | [block_list.go](block_list.go), [keys.go](keys.go) |
| Selection marks and expand indicators (shade or `┃` rail, `›`, `⌄`) | [blocks_mark.go](blocks_mark.go) |
| Mouse (wheel, click, hover, drag, scroll at the edge) | [mouse.go](mouse.go) |
| Text selection and what a drag copies | [selection.go](selection.go) |
| Text width, wrap, truncate | [width.go](width.go) |
| Agent link contract (plain data types) | [client.go](client.go) |
| Session view state and screen slots | [session.go](session.go) |
| Client messages, command results, generations | [update_session.go](update_session.go) |
| Connection states (reconnect, restored, gone) | [reconnect.go](reconnect.go) |
| Quit flow | [quit.go](quit.go) |
| `/copy` (clipboard methods, OSC 52 limit, result notice) | [clipboard.go](clipboard.go) |
| Input, commands and focus | [update_input.go](update_input.go), [focus.go](focus.go) |
| Editor keys and pastes | [editor_keys.go](editor_keys.go), [editor_paste.go](editor_paste.go) |
| Editor state, layout and history | [editor.go](editor.go), [editor_layout.go](editor_layout.go), [editor_history.go](editor_history.go) |
| Transcript state (ordered blocks and content versions) | [transcript.go](transcript.go) |
| Transcript blocks and their text formats | [blocks.go](blocks.go), [blocks_format.go](blocks_format.go) |
| Step blocks (step row, verbs, thinking, tool forms: collapsed, truncated, expanded) | [blocks_step.go](blocks_step.go) |
| Hook blocks (hook row, description row, detail fields; built from `HookRun` fixtures) | [blocks_hook.go](blocks_hook.go) |
| Tool output (truncated rows, hint, truncation rows) | [blocks_preview.go](blocks_preview.go) |
| Line diff of a file change (counts and unified diff rows) | [blocks_diff.go](blocks_diff.go) |
| Viewer (one block on the whole screen: title row, body, key row) | [transcript_view.go](transcript_view.go) |
| Markdown for replies (stable-prefix cache, chunks, code) | [markdown.go](markdown.go), [markdown_split.go](markdown_split.go), [markdown_code.go](markdown_code.go) |
| Event translation (events, errors, history snapshot) | [translate.go](translate.go), [translate_errors.go](translate_errors.go), [translate_snapshot.go](translate_snapshot.go) |
| Background jobs (job lines, running jobs) | [translate_jobs.go](translate_jobs.go) |
| Queue and execution presentation | [pending.go](pending.go), [status.go](status.go) |
| Header row and shortcut bar | [header.go](header.go) |
| Selection and completion | [selector.go](selector.go), [completion.go](completion.go) |
| Boxes (provider list, key, login and removal) | [dialog.go](dialog.go), [providers_box.go](providers_box.go), [login_box.go](login_box.go), [update_providers.go](update_providers.go) |
| Secret input (key, code, callback address) | [secret_input.go](secret_input.go) |
| Model line (start and change lines) | [model_line.go](model_line.go) |
| Extension composition (scaffold) | [widget.go](widget.go), [update_extension.go](update_extension.go) |
| Geometry and theme | [layout.go](layout.go), [theme.go](theme.go) |

Files marked "scaffold" are comment-only and have no behavior yet.
[blocks_step.go](blocks_step.go) and [blocks_hook.go](blocks_hook.go) draw the step rows. The [style guide](../../docs/tui-style-guide.md) owns how they look.

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

- Components give rows to the screen through `SlotSource.Slots(width)`. Layout gives rows in a fixed order (prompt row, bar, header, one pane row, the rules, status, prompt growth up to half the height, queue, menu) and the block pane takes the rest. See [layout.go](layout.go).
- Session code changes the transcript with `AppendBlockMsg`, `ReplaceBlockMsg` and `FinishBlockMsg`. Blocks draw themselves for a `RenderContext` and use `RenderContext.Width`, which is the full terminal width.
- The whole program runs on the alternate screen (`View.AltScreen`) with `MouseModeAllMotion`. The model writes nothing outside the frame.
- There is no commit path: the model never writes rows to the terminal outside the frame, and a resize needs no replay. The only renderer change in `third_party/bubbletea` is a guard against a frame drawn for the old size (see its `PATCH.txt`).
- The mouse is handled in [mouse.go](mouse.go) and [selection.go](selection.go) against the geometry of the last frame (`Frame.Geo`). The wheel scrolls 3 rows. A press on text acts on the release, and only on the cell of the press: it selects a block, and on the first row of a step it also opens the viewer. The bottom marker, `← back` and the prompt pane act on the press (the prompt pane places the cursor). A drag selects cells and copies their plain text through the `/copy` clipboard path on release; a drag at the first or last row of the pane scrolls it. Motion without a button sets the hover indicator. A selector or box ignores the mouse. Terminals keep their own selection behind `shift`+drag (or `option`+drag on macOS).
- The block pane follows the end of the transcript; a scroll away from the end turns follow off and the last pane row shows the bottom marker. The marker count may be approximate for a frame or two after a resize: the pane settles the rows below the window in a few frames. A block that depends on display state reports it in `drawState`, so the pane draws it again when the state changes.
- Fold modes are Collapsed, Truncated and Expanded. The pane keeps the mode of each block, and the block draws its rows for that mode. `e` cycles the mode, `→`/`l` expands, `←`/`h` collapses, and `E` moves all steps. The block drawing is the owner of what each mode shows; the [style guide](../../docs/tui-style-guide.md#patterns) names the forms.
- Block list keys are in [keys.go](keys.go): `Tab` moves focus between the prompt and the list; in the list, `j`/`k` and the arrows select, `e` `E` `→` `←` `l` `h` fold, `Enter` opens the viewer, `Esc` returns to the prompt, `g` `G` `Home` `End` and `PgUp` `PgDn` move, `shift+←`/`shift+→` (or `H`/`L`) select the previous or next prompt. `ctrl+t` or `ctrl+e` in the list toggles all thinking, and `ctrl+End` jumps to the end in any focus. A printable key that the list does not own moves focus to the prompt and is typed there.
- The viewer takes every key while it is open: `Esc` closes it, and the scroll keys move its window. `ctrl+c` closes it first, then follows the quit rule.
- Tests render the model in a real Bubble Tea program, feed the output to an `x/vt` emulator and compare the plain grid with `testdata/*.golden`. Refresh with `go test ./internal/tui -run TestScreenGoldens -update` and review the diff.

## Session view contract

`New(Options{Client: c, Cwd, Version, Profile, ...})` builds a model whose screen parts come from the session view. Use `Options.Open` instead of `Client` to show "Starting leader…" while the link opens.

- Start: `tea.NewProgram(m, tea.WithoutSignalHandler(), ...)`. `Init` creates the first session and starts the single wait on `Client.Events()`. The start line (the model line) is added after the first window size.
- Stop: when `Run` returns, the caller closes the client (a leader client leaves its runs going; the in-process client stops its run). `Model.StartError` returns the error of `Options.Open`; `Model.ClientError` returns a fatal client error. Quit keys (ctrl+c on an empty draft, ctrl+d, `/quit`) call `requestQuit`, the one place that ends the program on a user request. A failed `Open` and a fatal client error end it too.
- Send: the draft stays in the editor until `PromptAcceptedMsg` arrives and is cleared only if the draft revision is unchanged. The prompt band comes from the user message event, never from the acceptance.
- State: the agent is the authority for run, queue, model and usage. Results of client calls carry the generation they started in; an older result changes nothing.
- Clock: `Options.Now` and `Options.TickInterval` exist for tests; a negative interval turns the animation clock off.
- Header row: it shows the link, the working directory, the model with its thinking level, the context use and the cost. `Options.Version` is carried but the header row does not show it. The shortcut bar changes with the focus.
- Header numbers: context use is the token total of the last reply over the context window of the current model; the cost is the session total from `Client.Usage`.

## Provider setup contract

- `/copy` copies the raw markdown of the text blocks of the last finished assistant message, joined with one empty line; thinking, tool calls and tool output are not copied, and a reply in progress is not used. A native command runs first (`pbcopy`; on Linux `termux-clipboard-set`, `wl-copy`, `xclip`, `xsel` by environment), each with a 5 s timeout; a remote session (`SSH_TTY`, `SSH_CONNECTION`, `SSH_CLIENT`, `MOSH_CONNECTION`) skips them. Then OSC 52 goes to the terminal, up to 75 KB. Notices: `✓ Copied` (native command exit 0), `Sent to the terminal clipboard` (OSC 52; the terminal gives no answer), `Nothing to copy`, and `Not copied: <reason>` in the error role. The command runner and the terminal writer come from `Options.Clipboard`. The `c` key of the login box keeps its own path.
- `ctrl+o` has no binding. The viewer ([transcript_view.go](transcript_view.go)) shows one block and opens with `Enter` or a click in the block list.
- `/providers` (any letter case) opens the provider box in the prompt pane slot and keeps the draft. It has seven rows in the catalog order and a search row. The rows come from `Client.Providers`; every provider call needs the leader. With the in-process agent, `Client` returns `ErrNeedsLeader` and the TUI shows `Needs the leader; this session runs in process.` and the shell command `ask auth login`.
- A key, a code or a pasted callback address lives only in the buffer of [secret_input.go](secret_input.go). The screen shows the fixed text `(hidden)`. The text never reaches the editor, the prompt history, the undo or kill ring, a block, a note, the title, the bell or the clipboard, and the buffer is zeroed on every exit of the box. A paste with a line break or a space inside is refused without an echo.
- The call that carries a secret goes out once and the box does not keep it afterwards. A late result of a cancelled sign-in is dropped by its login id and request generation.
- `TEA_TRACE` is the one stated exception: Bubble Tea writes raw input to that file. When the variable is set, the TUI shows a warning with the file name once at start, and on the key box and the code input ([model_line.go](model_line.go)).
- A system reminder (`protocol.IsSystemReminder`) is for the model only. It is never a prompt band or a queue row: a job or monitor notice becomes one job line (`◆ job <id> <label>` with `completed · exit 0`, `✗ failed · exit 2`, `✗ killed · <reason>` or `<n> events`), and any other reminder draws nothing. The running jobs come from tool result details (`jobId`, `status`); the rail shows them when nothing else uses it (`┊ ◌ 1 background job · <id> <label>`, `┊ ◌ N jobs` under 60 columns) and `Working…` adds ` · N job(s)`. A client that attaches after a job started learns of it only from its notice. Design: [plans/261012-0155-t1b-job-notices/design.md](../../plans/261012-0155-t1b-job-notices/design.md).
- With no model (`ACPStateResult.ModelID` empty) there is no model line, the header shows `no model`, and the rail shows a notice ([status.go](status.go)). A prompt is refused with `Not sent: no model`, and the draft stays.
- A model line `  model  <provider>/<model>` starts the pane, follows a divider that starts an attach and a restore note; a change line follows a model event of the leader with the current epoch. The header and the reply meta line keep the bare model id. A start under a fallback shows the reason under the pair.
- A choice in `/model` switches the session and the leader saves it as the default (`setResult("Default model: …")` when `defaultSaved` is true). Signing in does not change the default or the current model.
