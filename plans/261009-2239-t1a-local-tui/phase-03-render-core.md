---
phase: 3
title: "Inline render core and layout"
status: completed
priority: P1
effort: "1.5d"
dependencies: [1]
---

# Phase 3: Inline render core and layout

Scout pass done and implemented; see "Scout result" and "Implementation notes" below.

## Goal

The root model, live-region layout and transcript commit path from the architecture doc, rendered through the patched renderer, with screen goldens read from a virtual terminal.

## Scope

- Root model, update loop, message types (`internal/tui/model.go`, `update_*.go`, `commands.go`).
- Transcript blocks separate from rendered lines; commit frontier; one ordered output path (`transcript.go`, `render_inline.go`).
- Layout slot order and limits from design section 7: editor slot first (1 row to 30% of height), status, footer rows, pending (3 rows then `+N more`), completion and selector (up to 8 rows), transcript tail last; footer hidden under 8 rows; only editor and status under 4 rows (`layout.go`).
- Width helpers: grapheme width, wrap, truncate with `…`, tab expansion, control characters as `?` (design section 8; inventory T-WID-01..03).
- Theme roles from design section 9 with lipgloss v2 styles and a no-color fallback (`theme.go`).
- Resize: rebuild after the 120 ms settle through the patch API; reused by the thinking toggle in phase 5.
- Test helper: render a model at w×h into an `x/vt` emulator and compare the plain grid with a golden file (`internal/tui/screen_test.go`), refreshed with `-update`.

## Tests first

| Test | Proves |
|---|---|
| `TestLayoutSlotPriority` (table over heights 3..40) | Rows go to slots in the design order; empty slots use no rows |
| `TestWidthWrapNeverExceedsColumns` (property test, widths 1..200, CJK, emoji, ZWJ, tabs, SGR) | No rendered row exceeds the width; wide characters are not split |
| `TestCommitFrontierOrderedOnce` | Stable blocks reach scrollback once, in order |
| `TestResizeRebuildKeepsDraftAndFocus` | After a resize, committed content is redrawn once, draft and cursor unchanged |
| `TestNoColorFallbackReadable` | Every role renders without color |

## Verification

`go test ./internal/tui/...`, goldens reviewed in the PR.

## Scout result

- `internal/tui` held comment-only scaffolds plus the phase 1 guards (`deps_pin_test.go`, `renderer_patch_test.go`). `internal/tuiext` and `pkg/askui` are scaffolds and are not touched.
- Commit path: `tea.Println` calls `insertAbove` in the patched renderer. It scrolls the screen by the rows of one write, goes to the top of the last frame and inserts rows there. `tea.ReplayInlineTranscript(width, height, generation, entries)` clears screen and scrollback and writes every entry plus the live view; it is stale when the generation is old.
- Findings that shaped the design (all verified with the x/vt emulator in `internal/tui` tests):
  - `insertAbove` ends each row with erase-to-end-of-line. A row that fills every column loses its last character, so transcript rows use `ContentWidth(width) = width - 1`.
  - A write that is longer than the room above the live frame scrolls rows of the frame itself into the scrollback. Commits write one row at a time, and layout never lets the frame fill the last screen row. A cap from the model's own frame height was unsound because the frame on screen lags the model.
  - Two commit commands started in one batch can reach the loop in any order. Commits are one `tea.Sequence(Println, done message)` at a time.
  - Output that arrives before the first frame is drawn is scrolled against the full screen and leaves the live frame at the top. Tests wait for the first frame; phase 7 must not commit before the first frame.
  - After a commit, the renderer does not put the hardware cursor back when the next frame is identical. A frame that changes (every streaming frame) fixes it.
- Libraries: `charm.land/lipgloss/v2` v2.0.5, `x/exp/golden` v0.1.0, `x/ansi` v0.11.7, `colorprofile`, `uniseg` are direct requirements now. `ansi.Wrap` loses characters and exceeds the limit in some inputs (verified), so wrapping is an own grapheme-level function. teatest v2 is not used: golden tests need the renderer bytes in an emulator, which the own harness (`screen_test.go`) gives; `go mod tidy` removes an unused requirement.
- Pending slot reading: "3 rows then `+N more`" is implemented as 3 rows in total, the last one `┆ +N more` when rows are cut.

## Implementation notes

| File | Role |
|---|---|
| `width.go` | `cleanText` (tabs to 4-column stops, controls to `?`, only SGR sequences pass), grapheme-safe `wrapLines`, `truncateTo` with `…`, `padTo` |
| `theme.go` | Roles and palette from the style guide for truecolor, 256 and no color; logo gradient; glyph constants |
| `layout.go` | `allocate` (slot priority), `composeFrame` (order, editor box, scroll inside the box, `+N more` markers, cursor) |
| `transcript.go` | Blocks, versions, commit frontier, `TakeStable`, `CommittedLines`, `TailLines` |
| `model.go`, `messages.go`, `update_window.go`, `render_inline.go`, `commands.go`, `focus.go` | Root model, block messages, settle and rebuild, serial output path |
| `screen_test.go` | Real program plus x/vt emulator harness; `requireGolden` writes `testdata/` with `-update` |

## Done

- [x] `TestLayoutSlotPriority`, `TestWidthWrapNeverExceedsColumns`, `TestCommitFrontierOrderedOnce`, `TestResizeRebuildKeepsDraftAndFocus`, `TestNoColorFallbackReadable`, plus golden, ordering and commit-size tests
- [x] `go test ./internal/tui/...`, `go vet ./internal/tui/...`, golangci-lint on `internal/tui`
- [x] Goldens: idle 80x24 and 120x40, streaming, queue, selector, narrow 40x14 (review the files in `internal/tui/testdata/TestScreenGoldens/`)
