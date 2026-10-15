---
title: "Inline resize fix"
status: completed
created: 2026-10-07
---

# Inline resize fix

## Outcome and boundaries

Fix the stale or duplicate editor rows after real terminal resize in T0.
Keep the editor text, Unicode cells, cursor, selector, and ordered transcript correct.
Keep one renderer output owner and retain the ordered transcript in application state.
On 2026-10-07 the user accepted the Grok-style inline route after the [source review](../reports/researcher-261007-1222-grok-inline-resize.md).
After resize settles for 120 ms, the renderer can purge the screen and native scrollback and replay the committed transcript at the new width.
This intentionally replaces the earlier no-purge and no-replay requirements for resize repair only.
Terminal output before application startup can be removed by this repair.
Retain all T0 transcript entries; a 4000-row truncation is not needed for its bounded 2000-line fixture.
Keep exact-once output checks for ordinary commits and check each resize replay as one complete ordered transcript generation.
Keep root production code, root dependencies, and existing immutable archives unchanged.
Work in an isolated temporary copy of the T0 module and retain the reviewed candidate with source hashes in this plan's artifacts.
Point the E2E runner at that candidate after independent verification.
Do not claim real iTerm2 or D14 acceptance from headless tests.

## Evidence and cause

Fresh G3 reproduction fails on Alacritty and Ghostty.
The reproduction is in `/private/tmp/askcore-resize-before-native-261007`.
The [diagnosis](../reports/debugger-261007-1156-inline-resize-fix.md) traces WindowSize handling into Bubble Tea resize and Ultraviolet repaint.
The old cursor model does not follow native terminal reflow before the renderer clears the live frame.
Changing the model cursor acts after repaint and does not repair the frame origin.
The original candidate is Bubble Tea v2.0.10 with Ultraviolet `f5a850f9c2b7`.

## Approach

The first isolated origin correction remains partial and is retained as failed hypothesis evidence.
Build a separate candidate for the accepted route, without modifying that snapshot.
Store committed transcript entries separately from terminal coordinates and the commit acknowledgement frontier.
Adopt size before commit and never flush an old-sized frame after resize.
Coalesce resize events with a generation-tagged 120 ms delay.
Use the existing renderer output owner to purge and reconstruct the committed transcript plus the current live frame.
Replay must not advance the commit frontier, duplicate acknowledgements, lose pending blocks, or change editor and selector state.
Handle resize during an in-flight write by repairing after that write completes with the latest size and transcript state.
Do not add a second direct stdout writer.
Keep output-error restoration and synchronized-output reset checks.
Update the retained Go tests and E2E oracle only where the user accepted the changed contract.
Continue to reject missing, duplicate, reordered, or partial replay generations and duplicate live frames.
Preserve the existing physical-terminal and extreme-size evidence limits.

### Earlier experiment

First check whether a bounded source correction can restore the renderer's frame origin before erase.
Keep any dependency correction local to the isolated candidate, with its license, provenance, patch, and tests.
Prefer a tested upstream correction when it satisfies the same contract.
A model-only cursor change or raw output from a second owner does not satisfy the contract.
The isolated correction can declare native reflow as an explicit renderer capability.
Alacritty and Ghostty exercise that capability; the retained VT fixture explicitly declares its measured no-reflow behavior.
Keep the fixture capability separate from its control-channel mode and retain its assertions.
Do not infer this capability from a terminal name or claim support for unmeasured reflow behavior.
Record each renderer fix class and its failing evidence.
Classify origin reconciliation with full live redraw as the first measured fix class before applying a patch.
This experiment does not select a raw committer or a product vendored-fork fallback.
Follow the original T0 limit of five fix classes before a fallback decision.
Do not silently change the product renderer route.

## Steps

1. Reproduce both G3 failures, trace the installed renderer graph, and review this plan.
2. Add a cause-aligned regression check and implement the isolated renderer correction.
3. Run focused G3, all G1/G2/G3/G6 cases on both engines, retained Go checks, race, vet, and build.
4. Complete independent source review and tests, retain the candidate and evidence, and update the runner and owning documentation.

## Acceptance

- Both engines pass G3 with one editor frame, correct CJK/emoji cells, and cursor positions after width and height changes.
- Native regression checks include a cursor inside the draft, explicit multiline text, wide glyphs at widths 12 and 13, shrink and grow, selector resize, and resize during transcript insertion.
- All default E2E cases selected by `e2e/tui/run.py` pass with exact-once ordinary commits and complete ordered resize replay generations.
- Stronger native resize cases pass on both engines, including pending-write resize with all 2000 markers retained.
- Purge and replay occur only for resize repair; alternate-screen use remains forbidden.
- Existing Go checks, output-failure cleanup, and owned-process cleanup remain correct.
- The regression check fails against the old candidate and passes against the corrected candidate.
- The candidate has reproducible dependency pins and local patch provenance, with no dependency-cache mutation.
- Existing archive and root product hashes remain equal before and after verification.
- Independent reports state any remaining extreme-size or real-terminal limitations.

### Native history capacity

Plain-process Ghostty controls retain all 2000 markers at 13×6 and 40×12.
At 52×15 they retain only 1096 markers, with or without purge and with an explicit large scrollback profile.
The reviewer accepted the default pending-write test at the measured 13×6 → 40×12 sizes, with the full 2000-marker assertion unchanged.
Keep 52×15 as an explicit failed engine-capacity probe, not a supported history guarantee.
Application transcript retention and ordered raw output remain required at every tested size.

## Risk and rollback

Native engines can reflow cursor positions differently from the old VT fixture.
Do not assume one engine's geometry is a universal terminal contract.
Keep failed hypotheses and raw captures, and test the correction on both engines.
Rollback only this candidate and the E2E target selection; retain historical evidence.

## Independent verification checkpoint

The [implementation review](../reports/code-review-261007-1230-inline-replay.md) returned DONE with no blocking finding.
Independent verification passed 43 Go checks, race, vet, build, and the local renderer regression.
All 18 default native cases passed across Alacritty and Ghostty, including strong draft and pending-write resize cases.
Ordinary output retained all 2,000 markers exactly once; replay generations retained their complete expected ordered content.
All 36 recorded owned PIDs were absent after cleanup.
All 426 protected before/after hashes matched.
See the [independent tester report](../reports/tester-261007-1230-inline-replay.md).

The [frozen candidate](artifacts/replay-candidate/README.md) has 987 verified source files.
Its archive manifest SHA-256 is `788dee6e7aadb89d076ec4c4f9348f27f067539f0e3efae16df4125c6a1f2aa2`.
The [source integrity proof](artifacts/replay-verification/source-integrity.json) confirms 34 Bubble Tea vendor files match the reviewed local candidate and 54 Ultraviolet files match the exact upstream source.
The earlier origin-mapping hypothesis is not part of this promoted replay candidate.
The runner now selects this frozen source archive.
The promoted default run exited 0 with all 43 selected Go tests and all 18 native cases passing.
Five assertion negative controls also passed.
See [promoted results](artifacts/replay-verification/promoted-default/results.json), [boundaries](artifacts/replay-verification/promoted-default/boundaries.json), [Go checks](artifacts/replay-verification/promoted-default/go-tests.txt), and [build](artifacts/replay-verification/promoted-default/go-build.txt).
The fresh run verified 988 candidate archive files including the manifest, 190 prior archive files including its manifest, and unchanged 426 protected file hashes.
The plan is complete for the accepted headless resize repair scope.
Full T0 physical-terminal acceptance remains separate and pending.

The separate Ghostty 52×15 capacity probe remains failed.
Real iTerm2 and D14 remain pending.
Root production code, root dependency pins, and earlier archives remain unchanged.
