# Grok inline resize

Read-only source review through GitNexus and direct source reads.
Repository: `/Users/dale/Desktop/workspace/opensources/grok-build`.
Commit: `2bdd1d6a6369de0e8c68132ea4539e9abd9e14a8`.
The GitNexus index matches this commit.
No Grok test was run for this review.

## Result

Grok minimal mode uses native scrollback between resize repairs.
It keeps committed entries in application state and can print them again after a layout change.
This is a third design option beside fullscreen and inline with restricted resize support.
It does not meet the current T0 rules that forbid global erase and committed transcript replay.

## Source path

GitNexus query returned the `Draw → Appearance` process and `maybe_reprint`.
GitNexus context confirmed `draw → maybe_reprint → reprint_history`, with layout observation and success marking.
Paths below are relative to the Grok repository.

- `crates/codegen/xai-grok-pager-minimal/src/lib.rs:52`: adopt terminal size before synchronized output, then reprint, size the live viewport, commit entries, and draw the live frame.
- `crates/codegen/xai-ratatui-inline/src/terminal.rs:438`: resize the viewport using a cursor offset estimate and a backend cursor-position query.
- `crates/codegen/xai-ratatui-inline/src/terminal.rs:1257`: estimate extra wrapped rows from the previous frame when width decreases.
  The source explicitly accepts an estimate that can leave a stale row above the redraw to avoid clearing a committed row.
- `crates/codegen/xai-grok-pager/src/minimal/reprint.rs:11`: wait 120 ms after a width or compact-layout change.
  Track uniform and mixed printed layouts so returning to an earlier width does not hide mixed history.
- `crates/codegen/xai-grok-pager-minimal/src/reprint.rs:62`: reset attributes, move to the origin, clear the screen and scrollback, print the welcome card, and print committed entries again.
- `crates/codegen/xai-grok-pager-minimal/src/reprint.rs:31`: limit the reprint to the newest complete blocks within a 4000-row budget.
  Older blocks remain available through `/transcript`.
  The module documents that clearing also removes terminal output from before Grok started and from earlier `/new` sessions.

## Test evidence limits

`minimal_resize_reanchors_live_region.rs:79` waits for history to be reprinted and for each committed and live token to occur once in the resulting terminal content.
Its mid-stream case checks that cursor queries do not occur inside synchronized output.
The harness uses `alacritty_terminal`; this source review does not certify real Alacritty, Ghostty, Terminal.app, or iTerm2.
These PTY tests are marked `#[ignore]` and require explicit execution.

`minimal_resize_preserves_committed_scrollback.rs` still has a comment that forbids history replay.
Its assertions check content survival and continued interaction, but do not check raw output for replay or purge commands.
The current implementation and the newer reanchor test explicitly reprint history.
The old comment is not evidence of an exactly-once output contract.

## Implication for Ask

Reuse size adoption before commit, cursor-query ordering, resize debounce, and an application-owned transcript.
Do not copy the cursor estimate alone and claim it repairs rows already moved into native history.
For an inline design like Grok, the application transcript must be the source of truth and terminal scrollback must be a rebuildable view.
Adopting this route requires an explicit change to T0's no-purge and no-replay rules.
The history loss before application startup and the reprint budget also require a deliberate product decision.
The current partial Ask candidate remains unaccepted and is not promoted to the E2E runner.
