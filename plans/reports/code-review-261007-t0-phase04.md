# T0 Phase 4 code review

Status: DONE.
The frozen implementation has no unresolved source finding in the accepted Phase 4 scope.
It is ready for independent tests.
This reviewer ran no tests and changed no source files.

## Scope and isolation

Reviewed the accepted [Phase 4 contract](../261006-1649-t0-inline-prototype-gate/phase-04-selector-and-resize.md) and frozen [source archive](../261006-1649-t0-inline-prototype-gate/artifacts/phase-04-source/README.md).
Archived Go source, tests, README, and module files match the reviewed scratch files.
The source manifest SHA-256 is `0757f201a34add8b09db37517dfc937086584176a41350fe34c2495a9180883d`.
All 27 protected product hashes match.
No root source, dependency, workspace, or public contract changed.
The full test set retains prior phase regression checks.

## Layout and input

The active editor or selector slot is measured at the final width.
Editor viewport height is reconciled before optional tail and help rows use the remaining space.
View derives content and hardware cursor from the same geometry and has no I/O.
Boundary checks cover widths 1 and 2, height 1, and normal sizes.
An oversized glyph in a one-column display uses a question mark without changing the retained draft bytes.
The layout follows the inspected Crush width-then-height dependency rule without copying a source body.
No generic component framework or dependency was added.

The selector has 12 items and a window of at most three rows.
Selection stays within the item bounds and remains visible after resize.
Global Ctrl+C handling precedes selector routing.
Selector input and paste do not change the retained editor draft or cursor.
Closing the selector restores the prior draft and cursor without stale selector rows.
The PTY assertions check the exact visible item window and selected cursor coordinates.
They also check the restored editor cursor against its visible row.

## Resize and output

Tests change real PTY dimensions and send SIGWINCH rather than injecting size messages.
Hand-written expected cells cover CJK and emoji at widths 13 and 12.
Repeated narrow, wide, and height-only changes check bounded cursor coordinates and one editor.
Four G1 streaming checkpoints include real resize.
All 2,000 committed identifiers retain their order and appear once in captured output.
Stable-height selector open, resize, and close preserve committed history.
Captured bytes reject global screen and history erase as a workaround.
The existing terminal output owner and FD are preserved.
No renderer fix class, fork, custom committer, or alternate screen was introduced.
Owned child and fixture readers retain the existing close and join paths.

## Evidence

The model RED records missing selector routing and a two-row layout overflow.
The PTY RED records missing selector behavior from actual Tab bytes.
The first GREEN compile error is identified separately from behavioral evidence.
The corrected focused checks pass.
The worker normal-suite log passes before the final selector boundary assertion was added.
The final [race log](../261006-1649-t0-inline-prototype-gate/artifacts/phase-04-source/evidence/phase4-final-race.txt) passes all 33 top-level tests, including that assertion.
Saved vet, build, and gofmt logs have no errors or unformatted files.
These are worker checks, not an independent run by this reviewer.

## Remaining limits

Pinned x/vt truncates screen rows during height shrink instead of saving those rows to scrollback.
The saved emulator limitation is kept separate from application replay.
Height-shrink checks prove live geometry and no token re-emission; they do not prove native terminal history preservation or reflow.
Stable-height history assertions remain strict.
Real iTerm2 Unicode placement, selector, resize, history reflow, exit, and shell-input observations remain pending.
Complete G2/G3, G1/G6, T0, and D14 acceptance is not claimed.
Independent tests follow this review under the user's gate waiver.
