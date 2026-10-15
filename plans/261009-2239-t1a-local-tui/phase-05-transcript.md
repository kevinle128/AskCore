---
phase: 5
title: "Transcript content and markdown streaming"
status: completed
priority: P1
effort: "1.5d"
dependencies: [3]
---

# Phase 5: Transcript content and markdown streaming

Outline, expanded by the scout result and the done list below.

## Goal

Every committed block format in design section 3, and a streaming markdown tail that stays stable and cheap.

## Scope

- Block types: startup header, user prompt, assistant markdown, code block with language and `┃ ` gutter, thinking (italic `thinking` role; hidden form `Thinking...`), tool line with `running…` then `✓ <duration>` or `✗`, retry note, error block with next action, cancel marker (committed at settle).
- Markdown: glamour v2.0.1 with `WithWordWrap(width)`, one renderer per width, chroma highlighting; own stable-prefix cache that re-renders only the open tail and drops the cache on width change (T-MD-01, -02, -05).
- Thinking toggle (ctrl+t): hide or show all thinking blocks by rebuilding the transcript through the phase 3 rebuild path; status notice for 3 s.
- Events from `pkg/protocol/events.go` mapped to blocks: `message_update`, `tool_execution_*`, `auto_retry_start/end`, `cycle_end` reasons, `agent_settled`.

## Tests first

| Test | Proves |
|---|---|
| `TestStreamingMarkdownStablePrefix` | Open fence, list and table in the tail do not corrupt the committed prefix; output equals a full render at the end |
| `TestStreamingMarkdownWidthChange` | Cache is dropped and output matches a fresh render |
| `TestBlockGoldens` (S6, S8, S9, S10, S18) | Each block format matches the design sketch at 80 columns |
| `TestThinkingToggleRebuild` (S19) | All thinking blocks switch form; committed order unchanged |
| `TestCancelMarkerAtSettleOnly` | Marker appears after `agent_settled`, not at the key press |

## Verification

`go test ./internal/tui/...`

## Scout result

- Render core (phase 3) gives `Block.Lines(RenderContext{Width, Theme})`, `AppendBlockMsg`/`ReplaceBlockMsg`/`FinishBlockMsg`, `RebuildMsg`. `Transcript.Replace` rejects committed blocks, so the thinking form is read at `Lines` time from a shared view state (no change to `RenderContext` or `model.go`).
- Events: the user prompt arrives as `message_start`/`message_end` of a user message, so the prompt band comes from the event stream and works the same for live events, snapshot replay and observers. Tool lines come from `tool_execution_*`, not from `ToolCall` content blocks. `cycle_end` has only `Code`; the error text comes from the last assistant `message_end` (`ErrorMessage`) or `auto_retry_end.FinalError`.
- Code block form: the phase outline says a `┃ ` gutter. design.md section 3 and the style guide say a `code-panel` band with the language name in `dim` and no gutter; S3 shows the same. Design and style guide win (approved authority).
- Depguard for `internal/tui` does not allow goldmark, so chunk boundaries come from an own line scanner. Prose goes to glamour v2, fenced code to chroma directly (glamour has no language label row).
- `protocol.Builder` is strict about event order and `Snapshot` copies the whole message; the translator keeps its own text accumulators and reads `SnapshotMsg.Stream.Message` directly.
- `go get` of glamour v2.0.1 and chroma v2.27.0 keeps lipgloss v2.0.5 and the Ultraviolet pin (checked with `TestDependencyPins`). goldmark rises to v1.7.8 as a glamour dependency only.
- The tool output expand, tool output truncation display and `/copy` stay out of scope (later track).

## Done

- [x] Block formats: startup header, user prompt band, assistant markdown reply, code block on the code panel (language row, no gutter), thinking (full and `Thinking...` form), tool line (`running` spinner, `✓ <duration>`, `✗ <reason>`), retry note, error block with next action, cancel marker, meta line, divider, detach and reconnect notes (`blocks.go`).
- [x] Markdown: own line scanner cuts a reply into chunks (`markdown_split.go`); prose goes to glamour v2.0.1 with one renderer per width and color mode, fenced code to chroma v2.27.0 (`markdown.go`, `markdown_code.go`); the stream cache draws closed chunks once and drops on width or theme change.
- [x] Long replies and thinking leave the live tail paragraph by paragraph: closed chunks become finished blocks while the rest streams; rows equal the rows of one block with the whole text.
- [x] Thinking toggle: shared `TranscriptView`, `Translator.ToggleThinking`, `ThinkingNotice`; the caller sends `RebuildMsg`.
- [x] Event translator (`translate.go`, `translate_snapshot.go`, `translate_errors.go`): `message_*`, `tool_execution_*`, `auto_retry_*`, `cycle_end` reasons, `agent_settled`, and snapshot entries with a mid-message stream baseline.
- [x] Tests first: `TestStreamingMarkdownStablePrefix`, `TestStreamingMarkdownWidthChange`, `TestBlockGoldens`, `TestThinkingToggleRebuild`, `TestCancelMarkerAtSettleOnly` (plus translator, block and markdown unit tests).
- [x] Pins: glamour v2.0.1 and chroma v2.27.0 added to `deps_pin_test.go`; lipgloss v2.0.5 and the Ultraviolet pin are unchanged.
- [x] Verification: `go test ./internal/tui/... -race`, `go vet`, `golangci-lint run ./internal/tui/...`, `go test ./...` pass.

Not built here (later track): tool output expand, tool output truncation display, `/copy`.

Known limit (open, render core): when one block is taller than the live region and it commits while the program is busy, a stale row of the last live frame can stay on the screen below the committed rows. It reproduces with plain test blocks that take about 1 ms to draw: a growing unstable block of about 14 rows at 60x14, then finish. Splitting a reply at closed paragraphs makes the case rare, but one very long paragraph or code block still triggers it. `TestStreamedReplyCommitsOnceThroughRenderer` uses a many-paragraph reply for this reason and does not cover the single tall block. The fault is in the commit path of `render_inline.go` (outbox chunking against the previous frame height), which this phase does not own.
