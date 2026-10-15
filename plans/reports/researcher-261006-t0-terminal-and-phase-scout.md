# T0 terminal and phase scout

## Scope and evidence

This is read-only research for `ak:plan --deep --tdd`.
No gate tests ran and no gate passed.
The [roadmap](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#t0-inline-prototype-gate-next-priority) owns T0 scope.
G1, G2, G3, and G6 block D14.
G4 and G5 are M2 checks.
The [Crush report](xia-261006-2342-crush-t0-port.md) supplies algorithm references, not inline terminal proof.

The root manifest has Go 1.27.0, Bubble Tea v1.3.10, and Lip Gloss v1.1.0.
The [candidate manifest](https://raw.githubusercontent.com/charmbracelet/bubbletea/v2.0.10/go.mod) has module path `charm.land/bubbletea/v2` and Go floor 1.26.0.
Candidate evaluation must use a separate module with `GOWORK=off`.
Do not change the root manifest for T0.

## Terminal test seams

Launch the built fixture on a real PTY slave.
Send keyboard and bracketed-paste bytes through the master.
Use a separate control and status pipe, so checkpoints cannot pollute terminal output.
Resize with the PTY ioctl and SIGWINCH.
Parse output continuously and wait for bounded screen, history, cursor, and status predicates.
Fixed sleeps must not decide success.

The [candidate event loop](https://raw.githubusercontent.com/charmbracelet/bubbletea/v2.0.10/tea.go) calls insertion, discards its error, and then updates the model and view.
Command completion therefore does not confirm successful output.
The [renderer](https://raw.githubusercontent.com/charmbracelet/bubbletea/v2.0.10/cursed_renderer.go) needs output-failure experiments before a reliable commit route can be selected.
Keep insertion acceptance, writer success, and observed terminal correctness separate.
Partial output must not cause automatic whole-block replay.

`github.com/charmbracelet/x/vt` is an emulator candidate, not a selected or pinned dependency.
Its [screen](https://raw.githubusercontent.com/charmbracelet/x/main/vt/screen.go) and [emulator](https://raw.githubusercontent.com/charmbracelet/x/main/vt/emulator.go) expose cells, history, cursor, and resize behavior.
Select an immutable version after a compile check and ANSI control fixtures prove the required semantics.
Check query responses and history behavior explicitly.
A renderer and emulator with shared dependencies can share a width error; use independent real-terminal evidence for Unicode and modified keys.

Retain the slave FD to compare termios before launch, during raw mode, and after exit.
Also check cursor visibility, bracketed paste, keyboard flags, mouse flags, and synchronized output.
Exercise normal quit, cancellation, errors, SIGINT, SIGTERM, SIGHUP, model panic, and command panic in separate child processes.
The inspected Bubble Tea signal handler handles SIGINT and SIGTERM, but not SIGHUP.
Add a fixture-owned SIGHUP cleanup path.
Always cancel, close, and wait for test-owned processes and PTYs.

## Per-phase baseline

`internal/tui` has 22 comment-only Go files and no tests or executable functions.
`cmd/tui` has 80 test functions excluding `TestMain`; none reference `initialModel` or `runInteractive`.
There are zero existing gate-relevant tests and zero existing gate-protected functions.

| Phase | Existing owners to read | New proof and function boundaries | Dependency risk |
|---|---|---|---|
| 1: scratch and oracle | Root manifest, roadmap, inventory, TUI architecture | PTY lifecycle, ANSI oracle controls, bounded waits, artifact capture | Exact PTY and emulator pins, query responses |
| 2: editor and G6 | Editor, input, keys, focus, layout scaffolds | Input normalization, newline policy, atomic paste, cursor translation | Negotiation, large payload integrity, terminal restoration |
| 3: commit and G1 | Transcript, command, inline renderer scaffolds | Stable prefix, ordered submission, observed output, partial-write stop | Tall insertion and hidden insertion errors |
| 4: selector and G2/G3 | Selector, focus, layout scaffolds | Slot replacement, bounded geometry, wrap reconciliation | Stale rows, Unicode width, native terminal reflow |
| 5: evidence and D14 | All gate tests, roadmap decision rules | Real-terminal checklist, source archive, result ledger | Unavailable terminals, unproved fallback coordination |

No unfinished execution plan blocks T0.
H1 and H2 have old `ready` metadata, provider design is `proposed`, and lifecycle and H7a are completed.
These are context, not terminal-gate prerequisites.
T0 supplies evidence for D14 and T1; no standalone T1 execution plan exists to add a bidirectional plan dependency.

## Remaining evidence

Exact emulator and PTY pins, query responses, image support, real-terminal access, and raw-output coordination remain untested.
Stock `tea.Println` is the baseline.
Apply the inventory failure threshold before presenting raw-committer or fork choices.
Archive scratch source, manifests, commands, and checksums in plan-owned evidence before scratch cleanup.
