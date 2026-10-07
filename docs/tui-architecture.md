# TUI and UI extension architecture

## Decision and status

Ask uses Bubble Tea for the terminal application.
The TUI has one root update loop and one terminal renderer.
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
| Terminal commit progress | Inline renderer coordination | Client transcript presentation |

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

## Inline component composition

The terminal scrollback holds the startup header and committed transcript blocks.
The live region contains the transcript tail, pending inputs, run status, widgets above the editor, completion, the editor slot, widgets below the editor, and footer.
A selector or dialog replaces the editor slot without destroying the draft.
A custom interaction can occupy an assigned bounded region under host focus control.

Layout reserves space for the active editor or dialog first.
Optional previews and widgets shrink or truncate when space is limited.
Empty slots consume no rows.
Layout owns component geometry and hardware cursor translation, including borders, padding, and Unicode column widths.
The application transcript is the source of truth in inline mode.
Native scrollback is a view that the renderer can rebuild after resize.
This repair can remove terminal output from before the application started.
Fullscreen composition is a later concern and must reuse transcript data rather than create another session authority.

## Component state and input

The editor owns draft text, cursor, selection, undo, paste, and draft revision.
The transcript owns ordered content blocks and content versions.
The session view owns execution and queue projections; status, pending-input, and footer components read those owners instead of storing copies.
Selectors own their filters, selection, and visible item windows.
Completion owns its query generation and selected candidate.
The dialog host owns active and queued interactions and their response lifecycle.
The widget host holds accepted component views; the extension child owns its private interaction state.
The root owns connection state and focus arbitration.
Render coordination owns output progress and derived caches.

Required host exit and cancellation handling precedes focused input routing.
Active dialogs and custom interactions receive their input before the editor.
Completion consumes only its assigned keys; other input continues to the editor or focused component.
Remaining application shortcuts are resolved after component handling.
Closing an interaction restores the prior focus only if its target still exists.
Agent events are processed regardless of focus.

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

## Inline transcript

Keep transcript blocks independent of rendered terminal lines.
A commit frontier separates the stable ordered prefix from the live tail.
Layout and commit scheduling use the same frontier calculation.
Keep scheduled output distinct from confirmed output; terminal write failure must not silently discard content or retry a partially written block as if nothing was written.
Use one ordered output path for scrollback and live frames.
After resize settles, the output owner can clear the screen and native scrollback and reconstruct committed content at the new width.
Reconstruction does not advance the commit frontier or acknowledge a block again.
Preserve editor and dialog state across reconstruction.
Keep the full transcript in application state; a display budget must not discard source content.
Widgets and editor-slot dialogs must fit a bounded live region and preserve cursor and focus correctness.

## Validation and remaining decisions

The [T0 gate](../plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#t0-inline-prototype-gate-next-priority) was evaluated in a separate scratch Go module.
D14 selects Bubble Tea v2.0.10 with the tested minimal local renderer patch and exact unmodified upstream Ultraviolet.
Go 1.27.0 remains; T1 owns root dependency migration and reproducible product packaging.
The [decision record](../plans/reports/pm-261007-1312-d14-tui-decision.md) separates independent automated evidence from user-reported Terminal.app/iTerm2 acceptance.
The selected experiment uses inline reconstruction after resize, based on the [Grok source review](../plans/reports/researcher-261007-1222-grok-inline-resize.md).
The [resize plan](../plans/261007-1156-inline-resize-fix/plan.md) owns acceptance evidence and dependency corrections; the production scaffold does not implement this renderer yet.
The extension contract stays in the X1 track and full UI rendering stays in T2 unless the roadmap is explicitly changed.

Before publishing the custom component SDK, test a live widget, selector, tool renderer, and custom editor with local subprocesses.
Verify input latency, IME, large paste, resize, focus, cancellation, crashes, reload, stale responses, and cleanup.
Test the same UI with a remote agent and with two local clients.
Measure whether subprocess updates are adequate for custom editors and animation.
If they are not, evaluate a client-local embedded runtime as a separate decision.
A declarative component tree is optional, not the only route to custom rendering.
Go dynamic plugins are not the runtime choice because unload, platform support, and build compatibility do not fit the extension lifecycle.

Pi is the reference for component customization, Crush for Go and Bubble Tea composition, and Grok for state/effect boundaries and commit-frontier design.
These references are design evidence, not compatibility guarantees.
