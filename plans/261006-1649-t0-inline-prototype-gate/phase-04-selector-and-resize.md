---
title: "Phase 4: Selector and resize, G2 and G3"
status: done
---

# Phase 4: Selector and resize, G2 and G3

## Overview

Prove bounded editor-slot replacement and resize without application replay of history.
This is implementation detail for later execution; no gate pass is claimed.

## Requirements

Use the scenario matrix as the functional contract.
Keep module isolation, output ownership, observable failures, and complete cleanup as required constraints.

## File inventory

Create `layout.go`, `selector.go`, `layout_test.go`, `selector_test.go`, and `g2_g3_test.go` in the scratch module.
Modify scratch model/editor/scenarios and terminal assertions.
Reuse pinned terminal-width facilities; add no generic component framework.

### Inventory summary

| Surface | Action | Ownership |
|---|---|---|
| Scratch module and phase files above | Create or modify | This phase only; no root imports |
| Existing root TUI and CLI | Read only | Protect existing contracts |
| Terminal evidence and testdata | Create or extend | Fixture and artifact owner |
| Root manifests and generated files | No edits | Outside T0 |

## Historical tests and function protection

Confirmed relevant scratch gate tests: 0.
The scratch module does not exist; `internal/tui` is a comment-only scaffold with no implemented gate functions or tests.
The current CLI has 80 tests, but none protect `initialModel` or `runInteractive` for these gates; they do not replace the new oracle.
All tests listed below are missing and must be created in the scratch module.

> - [ ] Read every caller before changing a proposed helper.
> - [ ] Protect root `cmd/tui` behavior by making no edits or imports from the scratch module.
> - [ ] Keep retained model changes in Update; keep View free of I/O.
> - [ ] Keep one renderer-owned terminal output path and one ordered stream wait.
> - [ ] Check failure, cancellation, size boundaries, and cursor state with assertions.
> - [ ] Keep terminal bytes and diagnostic logs on different outputs.

### Proposed function protection

These names are proposed local seams, not existing APIs.
Use existing equivalents if the selected dependencies provide them.

| Functions | Lifetime | Protection |
|---|---|---|
| `measureLayout, reconcileWrap, translateCursor, openSelector, closeSelector` | Size message and selector-open to selector-close | Scenario matrix, failure cases, and prior phase regression checks |

## Architecture and dependency map

G1 ordered frontier → shared geometry → editor or selector slot → final child cursor → renderer output → oracle.
Measure and render from the same final dimensions.
Adapt the Crush two-pass width/height reconciliation rule with a verified license route for any source adaptation.
Clamp selector to available live height and scroll its item window.
Resize changes only the live region; committed block text is not emitted again.

## Scenario matrix

| Case | Required assertion |
|---|---|
| 12-row selector over 3-row live area | Valid visible item window, correct focus and bounded region |
| Selector close | Prior draft/cursor restored; no stale selector rows; history intact |
| Resize while selector open | Selection retained, cursor/geometry valid, no editor duplication |
| Narrow/wide during G1 stream | Live redraw only; committed output tokens appear once |
| CJK/emoji at width-1 | Terminal cell widths and cursor agree with independent oracle |
| Widths 1, 2, normal and height 1 | No negative sizes, wrap loop, or out-of-range cursor |
| Repeated shrink/grow and height-only resize | No stale rows or duplicate input box |
| Native terminal history reflow | Record terminal reflow separately from application replay |

## Implementation steps

1. Read the listed owners and pinned dependency source.
2. Execute Tests Before; save valid RED evidence for new behavior and the initial result for stock baseline controls.
3. Implement the GREEN steps in the isolated module.
4. Execute Tests After and compare every scenario criterion.
5. Save evidence and inspect the risk signal before advancing.

## Tests Before (RED and stock baseline)

Write layout boundary tests and selector draft/focus tests.
Add PTY open/close and resize sequences with committed line sentinels before the new selector behavior.
Confirm the Phase 1 output observer still exposes the terminal FD, and drive real ioctl/SIGWINCH rather than injecting size messages.
Use expected terminal cells independent of application width calculations.
A shared Unicode width dependency in renderer and emulator can hide a defect; verify CJK/emoji placement independently in iTerm2.
Assert captured output does not re-emit committed sentinels on resize.
Do not require identical physical history row wrapping in terminals that reflow history natively.
Run checks before implementing new behavior and retain the failing behavioral assertion.
A stock baseline or existing correct control may PASS immediately; preserve that result and do not manufacture a failure.
Require a reproduced failing terminal assertion before any renderer fix.
A test infrastructure error is not a valid RED result.

## Refactor (GREEN)

Implement one selector editor-slot replacement and bounded item window.
Reserve editor or selector rows first; clamp optional live-tail rows.
Recalculate after width-induced editor wrapping, then translate cursor from final geometry.
Do not use alternate screen, global screen/history erase, or fullscreen overlays to pass G2.
Apply further renderer fixes only with saved failures and within the shared five-class budget.
Keep the smallest correct implementation and remove only redundant scratch code.
Do not weaken assertions to hide terminal errors.

## Tests After

Run focused G2/G3 tests, then G1/G6 regression checks.
Run real-terminal resize and selector scenarios at recorded dimensions.
Inspect physical cursor and scrollback; save screenshots or recordings plus raw bytes.
Separate emulator result, real-terminal result, and unsupported environment facts.

## Commands

Run these proposed commands from the validated scratch module location with `GOWORK=off`.
The scenario flags are requirements for the new driver, not claims about an existing executable.
Scenario data is explicit terminal test input, not a fake provider or a replacement for real decoding.

```sh
export GOWORK=off
export GOCACHE=/private/tmp/askcore-t0-09kf6w9r-go-cache
go test -run 'TestLayout|TestSelector|TestG2|TestG3' -count=1 ./...
go test -race -count=1 ./...
go run . --scenario g2
go run . --scenario g3
```

## Historical success criteria

> - [ ] G2 opens 12 items over a 3-row live area and closes without stale rows or history loss; G3 width-1 CJK/emoji and narrow/wide resize preserve cursor and one editor with no application history replay.
> - [ ] Every scenario has a result with a command and saved evidence.
> - [ ] New behavior has valid RED-to-GREEN evidence; already-correct stock controls retain their initial PASS results.
> - [ ] Focused checks and prior phase regressions pass.
> - [ ] Real-terminal observations are separate from emulator assertions.
> - [ ] No root source, manifest, workspace, or runtime dependency changed.
> - [ ] All owned child processes and PTYs are closed after each run.

## Risk signal and response

A duplicate editor appears, closing leaves rows, or application output repeats committed tokens.
Mark the related gate failed, preserve size/event/byte timelines, and repair shared geometry or renderer ownership.
Do not clear native history as a workaround.

## Rollback

Stop and wait for only the child processes started by this phase.
Restore the terminal before collecting shell evidence.
Keep the stock baseline, failing captures, and the tested graph.
Revert only this phase's scratch changes to its recorded prior commit or saved diff after checking for user edits.
Do not replay a partially written block or erase native history to recover a failed run.

## Execution checkpoint: 2026-10-07

The [Phase 4 source archive](artifacts/phase-04-source/README.md) is frozen with 148 file hashes.
The final worker race run passes 33 top-level tests; vet, build, and formatting pass.
The regular suite passes 32 tests before the final selector boundary assertion was added; the final race run includes that assertion.
Final source review and independent tests are pending at this checkpoint.
The user waived intermediate human approval pauses; required review and independent verification continue.
Phase 4 and full G2/G3 acceptance remain incomplete.

Actual PTY input proves the bounded selector window, exact cursor, draft restoration, and stable-height history.
Real ioctl/SIGWINCH resize, hand-written Unicode cell checks, widths 1 and 2 with height 1, repeated size changes, and stream resize exact-once output pass.
No kiln-class renderer fix, fork, or custom committer is used.
The local layout follows the pinned Crush width-then-height rule without copying a source body.
Pinned x/vt truncates visible rows during height shrink without preserving them in history.
The saved failure is an emulator limit, not a renderer defect.
Height-shrink checks prove live geometry and no application replay or global erase; native history reflow remains unproved.
Real iTerm2 3.7.3 checks remain pending because app access was denied.

The controller verified protected product hashes and prior archives unchanged.
Phase 3's archive now has 130 hashes after independent logs were added; its previous 123 hashes match.
All owned children were waited; no known owned background process remains.
The five-phase sweep retains 0 of 65 checklist items checked and 0 of 5 phases complete.
Phase 5 preparation is read-only; implementation has not started.
See the [progress report](../reports/pm-261007-t0-phase04-progress.md).

## Independent verification update: 2026-10-07

Final review returned DONE with no findings.
Independent narrow checks pass 6 top-level tests; the full race run passes 33 top-level tests and 30 subtests.
Vet, build, and formatting exit 0.
The tester verified 148 archive hashes, 20 frozen source hashes, all 27 protected product hashes, and unchanged prior archives with 34, 79, and 130 files.
Phase 5 implementation is authorized.
Real-terminal evidence remains pending and completion checkboxes remain unchanged.

## Accepted resize route checkpoint: 2026-10-07

The user accepted the [inline resize repair](../261007-1156-inline-resize-fix/plan.md) with a generation-tagged 120 ms settle delay.
For resize repair only, the output owner can purge the screen and native scrollback and replay committed content plus the live frame at the latest size.
This supersedes earlier no-purge and no-replay requirements for resize in this record.
It does not permit replay after a partial output failure or duplicate ordinary commits.
The application retains all 2,000 fixture entries; native history capacity remains a measured terminal limit.
The reviewed frozen candidate is selected by the [E2E runner](../../e2e/tui/run.py).
Independent checks passed 43 Go tests, race, vet, build, the local renderer test, and all 18 default Alacritty/Ghostty cases.
See the [verification report](../reports/tester-261007-1230-inline-replay.md) and [plan sync](../reports/pm-261007-1230-inline-replay.md).
The 52×15 Ghostty capacity probe remains failed and is outside default acceptance.
Real iTerm2 observations remain pending, so this phase and full T0 acceptance are not complete.
Earlier archive evidence remains immutable and records its original contract.

## Current acceptance: D14 closure

The user reported: “Terminal.app/iTerm2 done, tiếp D14 đi”.
This is user-reported physical-terminal acceptance, not independent capture.
No exact tested versions, screenshots, recordings, or per-scenario manual results were supplied with this statement.
For closing T0, this acceptance replaces the earlier requirement for independently retained physical-terminal artifacts.
Earlier unchecked criteria are preserved as quoted historical checklists; they must not be read as missing current work or as independently verified passes.
The accepted 120 ms resize purge/replay route also replaces the earlier resize no-purge/no-replay rule.

- [x] Independent automated evidence and promoted verification have been reconciled for this phase.
- [x] User-reported Terminal.app/iTerm2 acceptance is recorded with its evidence limits.
- [x] D14 selects the tested local Bubble Tea patch with unchanged upstream Ultraviolet; T1 owns root migration and reproducible product packaging.

See the [D14 decision and full-phase reconciliation](../reports/pm-261007-1312-d14-tui-decision.md).
Known Ghostty 52×15 capacity and Alacritty 1×1 limits remain.
Historical machine manifests remain unchanged, including their earlier `d14_ready: false` values.
