# `internal/tuiext`

## Status

This package is a scaffold.
It does not start processes or load extensions yet.
See the [TUI architecture](../../docs/tui-architecture.md) for the accepted boundaries and open prototype questions.

## Ownership

This package owns client-local UI extension discovery, process lifecycle, handshake, instance routing, and reload.
It converts subprocess messages to typed host events and sends typed input and lifecycle events to children.
The TUI owns layout, focus, state application, and terminal output.
Agent-side tool registration and hook execution remain outside this package.

## Lifecycle and routing rules

A UI extension runs on the client machine, including when the agent runs remotely.
Do not download or execute client code because a remote agent requests it.
Use the shared extension build and trust mechanics when they exist; do not create a second build cache or trust store here.
Each component instance has an owner, client scope, generation, revision, and lifetime.
Drop stale replies and cancel pending interactions when an instance stops.
A child must not own stdin, the terminal renderer, or the host's stdout.
Use framed protocol output and a separate diagnostic stream.
Readers send host events rather than mutate TUI state.
Use cancellable bounded delivery; keep lifecycle and response events lossless.
See the [channel and goroutine rules](../../docs/tui-architecture.md#channels-and-goroutines).
A failed component must release focus and permit restoration of the default editor.
Stop owned children and reap them when the client exits.
A client disconnect must not cancel the shared agent run.

## File convention

Use `host.go`, `process.go`, `instance.go`, and `reload.go` when those responsibilities are implemented.
Keep rendering and Bubble Tea state out of this package.

## Imports

Allowed: `pkg/protocol`, `pkg/askui`, `internal/settings`, `internal/workspace`, and standard-library process and I/O packages.
Denied: `internal/tui`, agent and transport adapters, providers, tools, storage implementations, and `internal/config`.
Do not import Bubble Tea, Lip Gloss, or Ultraviolet.
