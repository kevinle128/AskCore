# TUI and UI extension architecture

## Decision and status

Ask uses Bubble Tea for the terminal application.
The TUI has one root update loop and one terminal renderer.
The interactive TUI owns the whole alternate screen. It has no terminal scrollback of its own: the application transcript is the only history, and a block pane draws it.
The choice of the alternate screen is a user decision of 2026-10-12. It reverses the inline model that the T0 gate chose for the interactive TUI (see [Validation and remaining decisions](#validation-and-remaining-decisions)).
Headless `ask -p` is unchanged and does not use the TUI renderer.
Custom UI extensions run as client-local Go subprocesses, consistent with the existing external extension runtime decision.
The new packages are scaffolds only; runtime behavior and public SDK signatures are not implemented.
This design targets Pi-like customization but does not claim full parity before prototype validation.
The [main architecture](ask-architecture-reference.md) owns the process model and general import rules.

## Package boundaries

| Owner | Responsibility |
|---|---|
| [cmd/tui](../cmd/tui/main.go) | CLI entry point and client lifecycle; headless and auth paths remain separate |
| [internal/tui](../internal/tui/README.md) | Root model, local UI state, session projection, components, layout, and renderer |
| [internal/tuiext](../internal/tuiext/README.md) | Client-local UI extension processes, instances, handshake, and reload |
| [pkg/askui](../pkg/askui/README.md) | Public UI extension SDK boundary |
| [pkg/protocol](../pkg/protocol/README.md) | Shared UI request, response, capability, and event data contracts |
| [internal/leader](../internal/leader/README.md) | Client connection and ACP routing |
| [internal/acp](../internal/acp/README.md) | Adapter between the agent Go API and ACP |

The composition root supplies dependencies through constructors.
Agent extensions and UI extensions can belong to one extension package, but they have separate execution locations and lifetimes.
Share build, discovery, and trust mechanics with the external extension runtime when implemented.
Do not put a second tool registry or agent loop in the UI host.

## State ownership

| State | Authority | Lifetime |
|---|---|---|
| Execution, queues, usage, selected model, and lifecycle | Agent and session | Agent session or run |
| Draft, cursor, focus, completion, selector, and terminal size | TUI update loop | Client view |
| Extension business data | Extension, with durable session data when needed | Declared session or run scope |
| Component view, local interaction state, and pending UI request | UI component and host | Client-local component instance |
| Block pane window, follow mode, fold mode and selection of each block | Block pane and block list | Client view |
| Viewer, mouse drag and selected text | Root model, geometry of the last frame | Client view |

The TUI holds a projection of execution state, not a second authority.
Only the root update loop changes retained host UI state.
Commands and subprocesses return messages; they must not mutate retained models concurrently.
Async responses need identifiers that permit rejection of stale session, run, request, or generation results.
Editor replacement must detect draft conflicts instead of overwriting new user input.

## Model, update, and view

Use the Bubble Tea Model–Update–View pattern.
The root model composes concrete components rather than a global component registry.
Input, agent events, and async results enter as messages.
Update applies synchronous state transitions and returns commands.
View composes the current state without network, filesystem, or subprocess waits.
Do not add a second global store, event bus, or actor runtime.

The scaffold file boundaries are listed in the [TUI package README](../internal/tui/README.md#component-owners).
They describe ownership, not implemented APIs.

## Channels and goroutines

Use commands for bounded client operations such as prompt submission and model changes.
Bubble Tea executes commands outside the update loop.
Use context-owned readers for long-lived agent subscriptions and extension subprocess streams.
Do not create one goroutine for each component.

A subscription reader sends events through a bounded channel.
One command waits for the next event or context cancellation and returns a message.
After handling that message, the root schedules the next wait for that subscription.
There must be only one outstanding wait for each ordered stream.
A closed channel returns a stream-closed message and must not rearm a busy loop.
The reader owns channel closure; receivers do not close it.

Lifecycle, tool outcomes, and dialog responses cannot be silently dropped.
Backpressure must be cancellable; cancellation and child-exit control must remain observable when data delivery is blocked.
View updates may be coalesced only when they are complete replacement snapshots.
Do not coalesce message deltas or order-dependent operations.
Channel bounds and delivery policies need measured limits before runtime implementation.

Each reader and child has an owner, cancellation context, and completion signal.
Client shutdown cancels readers, stops owned children, and waits for cleanup.
UI child cleanup does not cancel the shared agent run.
Goroutines return messages and never mutate retained host UI state.
Single-writer state ownership does not authorize terminal writes from Update or commands.
The Bubble Tea renderer remains the terminal output owner.

## Screen composition

The TUI owns every row of the alternate screen.
Top to bottom the rows are the header row, a rule, the block pane, queue rows, the status row, a rule, the prompt pane, a rule and the shortcut bar.
The block pane takes all rows that no other part uses.
Empty parts use no rows.
A selector or dialog takes the prompt pane slot and keeps the draft.
A custom interaction can occupy an assigned bounded region under host focus control.

Layout reserves space in a fixed priority order, so that a small terminal keeps the prompt usable.
The header, the shortcut bar and one prompt row always stay.
The prompt pane grows with the draft up to half of the screen height.
The block pane gives rows away last and keeps at least one row.
The order and the limits are in [layout.go](../internal/tui/layout.go).
Layout owns component geometry and hardware cursor translation, including rules, padding and Unicode column widths.
It also returns the geometry of the frame, so that a mouse position maps to the part that the user saw.

The application transcript is the source of truth.
The block pane draws it again at any size; no terminal output has to be rebuilt after a resize.
A block that the user opens shows its full content in the viewer, which takes the whole screen.
The viewer reads the same application transcript, holds no session state of its own, and returns to the same pane position and selection.
Fullscreen composition reuses transcript data and does not create another session authority.

## Component state and input

The editor owns draft text, cursor, selection, undo, paste, and draft revision.
The transcript owns ordered content blocks and content versions.
The session view owns execution and queue projections; header, status, pending-input, and bar components read those owners instead of storing copies.
Selectors own their filters, selection, and visible item windows.
Completion owns its query generation and selected candidate.
The dialog host owns active and queued interactions and their response lifecycle.
The widget host holds accepted component views; the extension child owns its private interaction state.
The root owns connection state and focus arbitration.
The block pane owns the window, the follow mode and the cached rows of each drawn block; the block list owns selection, and each block owns its content.
The pane keeps one fold mode for each block, and the block draws the rows of that mode.

Required host exit and cancellation handling precedes focused input routing.
The viewer takes every key while it is open.
Active dialogs and custom interactions receive their input before the editor.
Completion consumes only its assigned keys; other input continues to the editor or focused component.
The focused component is the editor or the block list. `Tab` moves focus between them.
The block list owns its selection and fold keys. A printable key that it does not own moves focus to the prompt and is typed there.
Remaining application shortcuts are resolved after component handling.
The mouse acts on the geometry of the last frame and is ignored while a selector or box is open. A selection that a drag makes is copied through the `/copy` clipboard path.
Closing an interaction restores the prior focus only if its target still exists.
Agent events are processed regardless of focus.

## Provider setup, secret input and the model line

The [provider setup design](../plans/261010-1424-t1c-provider-setup-design/design.md) is the UI authority for these parts.
Provider account methods require the leader link; see [the ACP owner](../internal/acp/README.md#provider-methods).
The in-process agent still uses the model line and the no-model state.

**Dialog slot.** `/providers` and its key, login and removal boxes replace the editor slot, as the selectors do, and keep the draft.
They follow the input order above: the active box receives keys before the editor.
A box ignores the history keys and does not touch the draft.

**Secret input.** An API key, a pasted code or a pasted callback address lives only in one buffer of the box ([secret_input.go](../internal/tui/secret_input.go)).
The screen shows the fixed text `(hidden)`.
The text never reaches the editor, the prompt history, the undo or kill ring, a transcript block, a note, the title, the bell or the clipboard.
The buffer is zeroed on every exit of the box.
The leader gets the secret once, in the request that carries it, and does not log or echo it.
Stated exception: `TEA_TRACE=<file>` makes Bubble Tea write raw input, a pasted key included, to that file.
The TUI shows a warning with the file name when the variable is set: once at start, and on the key box and the code input.
Do not set it when you sign in.

**Rail priority.** The status shows one state on the live rail.
The order is in [status.go](../internal/tui/status.go).

**Model line.** The agent is the authority for the model.
The state and `model_update` event carry that model to the TUI.
The [model line renderer](../internal/tui/model_line.go) owns its layout.
The header row and the reply meta line keep the bare model id, as the user decided.

**Default model and fallback.** A choice in `/model` saves the default for new sessions; sign-in keeps the default and the current model.
This is a user decision; see section 3 of the approved design.
The [default model owner](../internal/app/default_model.go) selects the start model and records a fallback.
The [settings owner](../internal/settings/README.md#default-model) stores the choice.

## Request and event reconciliation

Keep client request state distinct from agent execution state.
Sending, accepted, failed, and cancellation-requested are client operation facts, not proof that agent work has started or stopped.
Only the settled lifecycle boundary confirms final completion.
Use the [existing event envelope and lifecycle contracts](../pkg/protocol/events.go) rather than inventing another sequence.
Respect each event's session or run scope instead of rejecting every event from another run without considering its meaning.
Sequence gaps require transport replay or state reconciliation.

Prompt submission captures text and draft revision.
Clear the submitted draft only after acceptance and only if the draft revision still matches.
Preserve later user edits and keep failed submissions recoverable.
Completion results must match both query generation and draft revision.
Extension views must match instance generation and view revision.
Dialog completion must match the active request and resolve only once.

## UI capability and component paths

Tools and extensions request UI changes through typed capabilities.
The first path uses host components for status, notification, widgets, dialogs, and editor operations.
The second path lets a local UI subprocess manage custom component state and return a bounded view for a host-assigned region.
The host sends lifecycle, size, theme, normalized input, and data updates.
The component returns view updates, cursor information, actions, and interaction completion.
Exact schemas and signatures remain pending prototype evidence.

The host owns focus arbitration, layout, global cancellation, and terminal I/O.
A custom component must not start another terminal renderer.
Raw cursor movement, clipboard escapes, and arbitrary terminal control sequences are not component output.
The TUI does not wait for a child while rendering; it uses the latest accepted view.
A slow or failed component must not block agent events or prevent user exit.

UI extension code runs on the client, even when the agent is remote.
Remote requests cannot install or execute client code.
The client advertises supported capabilities and installed component identities.
Missing capabilities return a defined unavailable result rather than waiting for nonexistent UI.
In multi-client sessions, interactive requests route to the driver client; local drafts and focus are never broadcast as shared state.
Reconnection restores display state and reconciles pending request IDs without resending completed actions.

## Component lifecycle

Items and instances have an owner and a declared scope.
An extension may update or remove only its own items.
Replacement and reload dispose old instances, cancel pending interactions, and reject old-generation replies.
A crash releases focus and restores a usable host editor.
Reload must use the existing extension safe-boundary rules; it must not stop a child in the middle of an active tool call.
Persistent business data is separate from ephemeral views and reconstructed from session data when appropriate.
Stopping a TUI detaches the client and stops its UI children; it does not dispose the shared leader agent.

## Transcript and block pane

Keep transcript blocks independent of rendered terminal lines.
A block draws itself for a width, a theme and a fold mode; the pane caches the rows and draws a block again only when one of these changed.
The pane is virtual: it draws only the blocks that meet its window, so the cost of a frame does not grow with the session.
The window top is an anchor (a block and a row in it), not a row number, so a block above the window that grows or folds does not move the text.
Follow mode keeps the window at the end of the transcript. A scroll away from the end turns it off and a bottom marker shows how to return.
The display budget of a frame must not discard source content: the transcript stays whole in application state.
A selection never changes the row count of a block, so the screen does not reflow when the selection moves.
Widgets and prompt-slot dialogs must fit a bounded region and preserve cursor and focus correctness.
The frame budget was measured in a [spike report](../plans/reports/spike-261012-1529-pane-frame-budget.md).

## Validation and remaining decisions

The [T0 gate](../plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#t0-inline-prototype-gate-next-priority) was evaluated in a separate scratch Go module and chose inline output with reconstruction after resize.
The user reversed that choice for the interactive TUI on 2026-10-12: the TUI runs on the alternate screen (decision D15 in the roadmap, plan [t1b-grok-style-altscreen](../plans/261012-1337-t1b-grok-style-altscreen/plan.md)).
The T0 evidence stays valid for what it measured, and the scratch module still holds the inline scenarios. The product has no inline commit or replay path.
D14 selects Bubble Tea v2.0.10 and exact unmodified upstream Ultraviolet.
The root module uses that Bubble Tea v2.0.10 through the copy in `third_party/bubbletea` (see its `PATCH.txt`).
The copy holds one renderer change, a guard against a frame drawn for the old size after a resize.
`third_party/bubbletea/renderer_patch_test.go` guards the change and `internal/tui/deps_pin_test.go` guards the pinned versions.
The [resize plan](../plans/261007-1156-inline-resize-fix/plan.md) records the earlier inline renderer evidence and is a historical record.

The implemented TUI is checked at three levels, and each level uses the real code path:

1. **Golden screens.** [screen_test.go](../internal/tui/screen_test.go) runs a model in a real Bubble Tea program and feeds the renderer output into an `x/vt` terminal emulator. A test compares the plain grid with a file under `internal/tui/testdata`. Refresh the files with `go test ./internal/tui -run <TestName> -update` and review the diff.
2. **Go end-to-end.** [interactive_test.go](../cmd/tui/interactive_test.go) starts the compiled `ask` on a pseudo terminal with an isolated `ASK_HOME`, so the TUI starts a real leader and talks over the real Unix socket. An `x/vt` emulator reads the screen, and the tests read the terminal state before and after the run. The faux provider replaces only the model service.
3. **tui-test.** [e2e/tui/run.py](../e2e/tui/run.py) drives the compiled `ask` in native terminal engines (Alacritty and Ghostty) through the pinned `tui-test` CLI.
Its Ask scenario owners are listed in its README.
The T0 gate scenarios of the scratch module are no longer part of the runner.
See [e2e/tui/README.md](../e2e/tui/README.md).

Headless engines do not certify Terminal.app or iTerm2 rendering; a manual run on a real terminal stays a separate record.
The extension contract stays in the X1 track and extension UI rendering stays in T2 unless the roadmap is explicitly changed.
Mouse capture takes text selection from the terminal. The user keeps native selection with `shift`+drag (`option`+drag on macOS).

Before publishing the custom component SDK, test a live widget, selector, tool renderer, and custom editor with local subprocesses.
Verify input latency, IME, large paste, resize, focus, cancellation, crashes, reload, stale responses, and cleanup.
Test the same UI with a remote agent and with two local clients.
Measure whether subprocess updates are adequate for custom editors and animation.
If they are not, evaluate a client-local embedded runtime as a separate decision.
A declarative component tree is optional, not the only route to custom rendering.
Go dynamic plugins are not the runtime choice because unload, platform support, and build compatibility do not fit the extension lifecycle.

Pi is the reference for component customization, Crush for Go and Bubble Tea composition, and Grok for state/effect boundaries, fold modes and the block pane.
These references are design evidence, not compatibility guarantees.
