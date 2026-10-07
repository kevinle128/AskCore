# `pkg/askui`

## Status

This package is a scaffold, not a usable or stable SDK.
No component interfaces or wire schemas are published yet.
The module distribution question in the [external extension roadmap](../../plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#x1-external-extensions-loaded-at-run-time-no-ask-rebuild) remains open.

## Ownership

This package is the public author-facing boundary for UI components and UI capability requests.
It will hide subprocess framing and expose component lifecycle, data, input, view, and completion operations.
Shared serializable requests, responses, capabilities, and errors belong in `pkg/protocol`.
Do not duplicate wire types here.
Agent-side tools and hooks belong to the planned agent extension SDK, not this package.

## Contract boundaries

Support host components and a custom component path without exposing internal Go types.
Keep component state separate from agent session state.
Expose theme tokens, available size, normalized input, and cursor information through versioned data contracts.
Component output is confined to its assigned region; it cannot send terminal control operations.
The host retains layout, focus arbitration, cancellation, and terminal ownership.
Do not make Bubble Tea models, commands, or renderer internals part of the public SDK.
The custom component and editor contracts require a prototype before publication.
See the [TUI architecture](../../docs/tui-architecture.md).

## File convention and imports

Use capability file names such as `component.go`, `client.go`, and `lifecycle.go` only when their contracts are verified.
Allowed: the standard library and `pkg/protocol`.
Denied: all `internal/*` packages and terminal framework dependencies.
