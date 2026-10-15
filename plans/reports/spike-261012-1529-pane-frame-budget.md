# Spike: block pane frame budget (2000 blocks, 80x24)

## Result: PASS, with one design decision that needs sign-off

The four required cases (a wheel scroll, b streaming delta with follow on, c fold change, d resize 80 to 120 then first draw) pass as built, each far under 16 ms. Acceptance criterion 38 is met.

One more realistic case, not in the brief, went over: resize while scrolled up with new output pending (the marker needs N, the rows below the window). The first version took **94 ms** per frame. It now takes 6.3 ms median (7.7 ms worst), but only because of a design choice the design doc did not make: a frame spends at most 4 ms (`paneSettleBudget`) drawing blocks below the window for N, the marker sets `Approx`, and the caller must draw more frames until N settles. The test keeps the 16 ms assertion for this case.

**Cause:** N needs the row count of every block below the window; after a resize each of them has to be drawn again at the new width (about 100 µs each, about 1000 blocks).

**Options for N after a resize:**

1. Bounded settle with `Approx` (current). N is exact within a few frames; costs a frame budget constant and a caller tick while `Approx` is true.
2. Show `▼ new output · End` with no number until all blocks below are drawn. Needs the same background drawing, so it adds a text state and saves nothing.
3. Count N from the last known row counts and never settle. No tick and no budget, but N stays wrong until each block is met; after a resize it can be off by a large factor in a long session.
4. Draw below-window blocks only on a scroll or a tick and never inside a frame (a separate `Settle(budget)` the model calls from idle). Same result as 1 with the cost out of the frame path, but a second entry point.

**Recommendation: option 1.** It is the only one that keeps N exact (criterion 27) and keeps every frame bounded, and the caller cost is one `tea.Tick` while `Approx` is true. Option 4 is the same idea if Step 5 prefers an explicit call. Please confirm option 1, or choose another, before Step 5.

Machine: Apple M4 Max, Go test, 101 runs per case (5 warm-up runs), median reported. The test asserts the median under 16 ms; it skips the assertion under `-race` and still logs.

Session: 2001 blocks, 22,893 rows at width 80. Block mix by index (i mod 5): 3 one-row tool lines (`read`, `bash`, `edit`), 1 multi-paragraph markdown reply (about 11 rows), 1 failed `bash` block with 200 output lines. Replies are Expanded, tool blocks Collapsed, every 25th long output Expanded (200 rows). Blocks are the real types (`NewToolLineBlock`, `NewAssistantTextBlock`, `newToolBlock`) wrapped in a test-only fold wrapper that draws the three fold forms. Pane height is 15 rows (80x24 minus header, rules, prompt, status, bar).

| Case | Median | p99 | Worst |
|---|---|---|---|
| Wheel scroll of 3 rows, middle of the session | 5 µs | 37 µs | 43 µs |
| Wheel scroll through 4,500 rows over never-drawn blocks | 5 µs | 710 µs | 1.0 ms |
| One streaming delta in the last block, follow on | 185 µs | 426 µs | 443 µs |
| One streaming delta, follow off in the middle | 187 µs | 403 µs | 498 µs |
| Fold change of one block in the window | 30 µs | 550 µs | 731 µs |
| Resize 80 to 120 (and back), first frame, follow on | 1.5 ms | 1.9 ms | 1.9 ms |
| Resize 80 to 120, first frame, follow off, no new output | 2.1 ms | 2.4 ms | 2.4 ms |
| Resize 80 to 120, first frame, follow off, new output pending (marker shows N) | 6.3 ms | 7.6 ms | 7.7 ms |
| Cold: build 2000 blocks / first frame with follow on | 2.6 ms / 3.7 ms | | |
| Cold: draw all 2000 blocks once at width 80 (reference) | about 0.2 s | | |

`BenchmarkPane*` (benchtime 300x): wheel 4.9 µs/op, delta 396 µs/op, fold change 198 µs/op (the cycle includes the 200-row Expanded form), resize 1.5 ms/op. Each frame includes `strings.Join` of the rows, as the screen does.

## The case that failed first

First version: the marker count N (pane rows below the window) drew every block below the window. After a resize with follow off and new output pending, that drew about 1000 blocks at the new width: 94 ms per frame. A user who drags the window edge in this state would see 94 ms frames.

Cause: a full draw costs about 100 µs per block (markdown and wrapping), and N needs the row count of every block below the window.

Fix (kept in the code): a frame spends at most `paneSettleBudget` (4 ms) on drawing blocks below the window for N. Blocks it does not reach count with their last known row count, and the marker sets `Approx`. Each later frame settles more blocks, so N becomes exact after a few frames (test `TestPaneMarkerCountConvergesAfterResize`). The `▼ End` form has no N and costs nothing. Without a resize all blocks below were drawn once already, so N is exact at once.

This does not weaken the test: the case stays in the suite with the same 16 ms assertion. A change inside the window while follow is off counts as new output; N is then at least 1 because follow off means rows exist below (test `TestPaneChangeInsideTheWindowKeepsRowsBelowCounted`). Content that shrinks so that the end enters the window turns follow on and clears the marker (test `TestPaneContentThatShrinksIntoTheWindowTurnsFollowOn`).

## Design of the cache (`internal/tui/pane.go`)

- `Pane` is bound to one `*Transcript`. One `paneItem` for each entry, in entry order (entries are append-only).
- Item holds: fold mode, rows of the last drawing, row count `h`, and what the drawing came from: content version (`Transcript` entry version), `drawState()` value (spinner frame, same `drawStater` interface the transcript view uses), width, theme pointer, fold mode.
- A block draws again only when one of those keys differs, and only when a frame or a scroll meets it (`measure`). A resize therefore costs the blocks of the window, not the session. A fold change costs one block.
- The window top is an anchor `(block index, row in block)`, not a row number. A block above the window that grows or folds does not move the text at the top; a resize keeps the same block at the top. The pane never builds prefix sums over the session.
- Scroll walks from the anchor over adjacent blocks, so a wheel step touches 1 or 2 blocks. The end anchor (for follow and for the "at the last row" test) walks back from the last block and touches only the window.
- `sync()` runs once per call: one version compare for each block (about 2 µs for 2000) to add items and to detect new output.
- Follow on: the window is the last `height` rows. Follow off: the window is `height-1` content rows and the last row is the marker slot (the frame returns `""` there). Scrolling so that the last content row is in view turns follow on and snaps to the end window.
- Marker data: `PaneMarker{Show, New, N, Approx, Text, Pad}`. `Text` is `▼ End`, `▼ N new lines · End`, or `▼ N · End` under 30 columns. `Pad` centers it with `displayWidth`. `New` turns on when the transcript appends or a block at or after the window top changes while follow is off; scroll to bottom clears it.
- API: `NewPane`, `SetSize`, `SetTheme`, `SetFold`/`Fold`, `DefaultFold` hook, `ScrollBy`, `ScrollToTop`, `ScrollToBottom`, `Reveal(block)`, `Following`, `Frame`.

## What Step 5 can keep

All of `pane.go` and `fold.go`, and the tests. The test-only `foldedBlock` wrapper shows the contract that Step 2 blocks must meet: read `rc.Fold`, return fewer rows for a lower fold, and keep `drawState()` if the block depends on external state.

Not done here (Step 3 and later): selection (use `Reveal(i)` and the block index from `Frame.FirstBlock`), hover and mouse hit testing (the frame has no row-to-block map yet; add one next to `Rows` when Step 6 needs it), theming and drawing of the marker row, the thinking and spinner redraw tick.

## Changes outside the owned files

`internal/tui/transcript.go`: one new field `Fold FoldMode` on `RenderContext` (6 lines with the comment). Existing blocks ignore it, so no output changes. No other existing file changed.

## Notes for Step 5

- `DefaultFold == nil` gives `FoldExpanded`. That is not the design default for tools and hooks (Collapsed). Step 5 must set `Pane.DefaultFold` per block type.
- `paneSettleBudget` (4 ms) is a chosen constant with no design basis. It is tunable; the frame cost for this case is about the budget plus 2 ms.
- `RenderContext.Fold` is the only change outside the owned files.

## Risks

1. **Memory.** The pane keeps the rows of every drawn block. The test session holds 22,893 rows (about 2 MB). A session with several 10,000-line outputs, drawn Expanded, would hold much more. Mitigation if needed: drop `rows` of blocks far from the window and keep `h` (a redraw then costs one block when it comes back into the window).
2. **Marker `Approx`.** After a resize in a long session the first frames can show a N that is a little off. The caller must draw another frame while `Marker.Approx` is true (a short `tea.Tick`). Without the tick N stays off until the next frame for another reason.
3. **Block cost.** About 100 µs per block was measured on average with TrueColor and markdown replies of about 11 rows. A single very large block in the window (not measured here) pays its full draw on each fold change, and on each delta until the markdown stream caches finished parts.
4. **Window height while follow is off is `height-1`.** The design sketches show a blank row above the marker, so this matches them, but design section 5 says N counts "pane rows below the window" without saying whether the marker row is part of the window. This code excludes it. Confirm in Step 5.
5. **Plural.** The design text is literally `▼ <N> new lines · End`, so `▼ 1 new lines · End` shows for N = 1. Kept literal.
6. Timing assertions use a 16 ms median bound with at least a 2x margin in every case; the smallest margin is "resize, follow off, new output pending" (6.3 ms median, 7.7 ms worst); that one is dominated by the fixed 4 ms settle budget and will not grow with session size.

## Unresolved questions

- Should the marker row be part of the window for N (risk 4)?
- Should the pane drop rows of far blocks now or only if a real session shows memory pressure (risk 1)?
