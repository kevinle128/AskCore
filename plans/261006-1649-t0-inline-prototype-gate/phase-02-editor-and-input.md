---
title: "Phase 2: Editor and input, G6"
status: done
---

# Phase 2: Editor and input, G6

## Overview

Prove real input decoding, intact paste, and complete terminal restoration.
This is implementation detail for later execution; no gate pass is claimed.

## Requirements

Use the scenario matrix as the functional contract.
Keep module isolation, output ownership, observable failures, and complete cleanup as required constraints.

## File inventory

Create `editor.go`, `input.go`, `editor_test.go`, `input_test.go`, and `lifecycle_test.go` only in the scratch module.
Modify scratch `model.go`, `main.go`, terminal fixture, and README.
No full editor history, kill ring, attachments, or SDK implementation is required.

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
| `normalizePaste, routeInput, applyEdit, restoreTerminal` | Draft lifetime and program startup to exit | Scenario matrix, failure cases, and prior phase regression checks |

## Architecture and dependency map

Phase 1 oracle → negotiated capability messages → input route → draft mutation → measured editor view/cursor.
Use terminal stack negotiation; do not infer support from TERM alone.
Inspect the pinned v2 keyboard and paste APIs before implementing bindings.
Global exit handling precedes selector/editor input.
Paste normalization has one owner and a specified CRLF-to-LF rule; payload checks compare against that rule.

## Scenario matrix

| Case | Required assertion |
|---|---|
| Kitty negotiation supported | Shift+Enter and Ctrl+Enter insert newline from actual encoded input |
| Negotiation absent/declined | Alt+Enter inserts newline; help describes the fallback |
| Ordinary Enter | Submit is distinct from newline |
| Bracketed paste, 2,000 lines | Exactly one PasteMsg before normalization; full normalized draft matches expected bytes/hash and line count |
| Paste contains CSI-u-like text | Text remains paste content and causes no submission or key action |
| CRLF, tabs, CJK, emoji paste | Documented normalization preserves all other content |
| Normal/error/panic exits | Raw mode, cursor, bracketed paste, keyboard flags, mouse state, and synchronized output restored |
| SIGINT/SIGTERM/SIGHUP | Child exits within a recorded bounded fixture deadline; terminal state restored |
| Exit while output/input is pending | Cleanup completes without a blocked wait or lost shell input |

## Implementation steps

1. Read the listed owners and pinned dependency source.
2. Execute Tests Before; save valid RED evidence for new behavior and the initial result for stock baseline controls.
3. Implement the GREEN steps in the isolated module.
4. Execute Tests After and compare every scenario criterion.
5. Save evidence and inspect the risk signal before advancing.

## Tests Before (RED and stock baseline)

Add table tests for newline and paste transformations.
Add PTY tests that send negotiation responses and the real encoded keys.
Send the 2,000-line paste as one bracketed payload with fragmented transport reads.
Add one child-process case for each exit route; a model-only quit test is insufficient.
Run checks before implementing new behavior and retain the failing behavioral assertion.
A stock baseline or existing correct control may PASS immediately; preserve that result and do not manufacture a failure.
Require a reproduced failing terminal assertion before any renderer fix.
A test infrastructure error is not a valid RED result.

## Refactor (GREEN)

Implement a real minimal multiline draft and cursor model.
Keep large paste as editor input, without attachment conversion or placeholder substitution.
Bound only the visible editor rows; retain all pasted text.
Use context-owned shutdown and the program cleanup path verified against the pinned implementation.
Keep model-panic and command-panic tests in disposable children.
Test context cancellation as well as normal quit and output error.
The inspected Bubble Tea version catches SIGINT and SIGTERM, not SIGHUP; explicitly handle SIGHUP and verify cleanup.
Do not repair a missing upstream guarantee by declaring success from process exit alone.
Keep the smallest correct implementation and remove only redundant scratch code.
Do not weaken assertions to hide terminal errors.

## Tests After

Run input and lifecycle tests, then all Phase 1 checks.
In the selected real terminal, iTerm2, record negotiated support and each key result separately.
If its installed version cannot negotiate the requested modified keys, prove the supported response path in the PTY fixture and record the real capability as unsupported.
Alt+Enter fallback and paste integrity must still pass in iTerm2.
Check shell input, cursor visibility, and mode 2026 disabled after each exit path.
For a recoverable output failure, permit cleanup through the same output owner and verify its bytes.
For permanent output failure, restore termios where possible and report terminal-byte restoration as failed or unavailable, never PASS.
Record pixel-size, palette forwarding, progress-bar keepalive, and modifyOtherKeys probes as optional evidence, not M1 blockers.

## Commands

Run these proposed commands from the validated scratch module location with `GOWORK=off`.
The scenario flags are requirements for the new driver, not claims about an existing executable.
Scenario data is explicit terminal test input, not a fake provider or a replacement for real decoding.

```sh
export GOWORK=off
export GOCACHE=/private/tmp/askcore-t0-09kf6w9r-go-cache
go test -run 'TestEditor|TestInput|TestG6|TestRestore' -count=1 ./...
go test -race -run 'TestEditor|TestInput|TestG6|TestRestore' -count=1 ./...
go run . --scenario g6 --keyboard auto
go run . --scenario g6 --keyboard off
```

## Historical success criteria

> - [ ] G6 counts exactly one PasteMsg containing all 2,000 normalized lines; negotiated keys and Alt+Enter fallback pass; all exit modes restore termios and terminal flags.
> - [ ] Every scenario has a result with a command and saved evidence.
> - [ ] New behavior has valid RED-to-GREEN evidence; already-correct stock controls retain their initial PASS results.
> - [ ] Focused checks and prior phase regressions pass.
> - [ ] Real-terminal observations are separate from emulator assertions.
> - [ ] No root source, manifest, workspace, or runtime dependency changed.
> - [ ] All owned child processes and PTYs are closed after each run.

## Risk signal and response

Paste arrives as multiple semantic messages, text is changed, or a signal leaves terminal modes active.
Keep G6 failed, save bytes and mode snapshots, and fix the input/lifecycle owner before proceeding.

## Rollback

Stop and wait for only the child processes started by this phase.
Restore the terminal before collecting shell evidence.
Keep the stock baseline, failing captures, and the tested graph.
Revert only this phase's scratch changes to its recorded prior commit or saved diff after checking for user edits.
Do not replay a partially written block or erase native history to recover a failed run.

## Execution checkpoint: 2026-10-07

Phase 2 automated source is frozen and ready for independent tests.
Final source review returned DONE.
The worker's 12 top-level tests, race check, vet, and build pass.
Independent tests await the next user approval.
Real iTerm2 3.7.3 checks remain pending because computer-use app access was denied.
Phase 2 and full G6 acceptance are incomplete.
The unchecked requirements and success criteria above remain in force.

| Evidence | Durable location |
|---|---|
| Source, commands, and limits | [Phase 2 README](artifacts/phase-02-source/README.md) |
| Final tests and race check | [Tests](artifacts/phase-02-source/evidence/phase2-tests-final.txt), [race](artifacts/phase-02-source/evidence/phase2-race-final.txt) |
| Vet and build | [Vet](artifacts/phase-02-source/evidence/phase2-vet.txt), [build](artifacts/phase-02-source/evidence/phase2-build.txt) |
| Initial input RED | [Paste and Alt+Enter failure](artifacts/phase-02-source/evidence/phase2-input-red.txt) |
| Actual partial-frame failure and cleanup | [Stock RED](artifacts/phase-02-source/evidence/phase2-partial-red.txt), [cleanup GREEN](artifacts/phase-02-source/evidence/phase2-partial-green.txt) |
| Integrity and graph | [72 archived file hashes](artifacts/phase-02-source/archive-sha256.json), [exact graph](artifacts/phase-02-source/evidence/phase2-module-graph.txt) |

The automated PTY cases decode actual Alt+Enter, negotiated Shift+Enter and Ctrl+Enter, and ordinary Enter bytes.
Both capability cases retain the full normalized 2,000-line paste, with exactly one PasteMsg and matching draft hash, bytes, and lines.
The draft retains CSI-u-like text, tabs, CJK, and emoji.
The viewport is bounded and cursor positions use terminal cells and grapheme boundaries.
The test terminal responder answers actual emitted queries; it does not claim that x/vt implements Kitty negotiation.

Normal quit, context cancellation, SIGINT, SIGTERM, SIGHUP, model panic, command panic, recoverable partial output failure, and permanent output failure have separate child cases.
The recoverable case writes the real eight-byte mode 2026 enable prefix before returning an error.
Stock cleanup left that mode enabled; fixture cleanup now resets it after the renderer stops through the same ordered output owner.
Permanent output failure restores termios but reports terminal-byte restoration as unavailable.
The termios snapshot is taken after `tea.Run` returns and before the child session leader exits.
Final terminal-state observations follow process exit and master drain.
No full G1 frontier, transcript-order, or replay acceptance is claimed by these cleanup tests.

Infrastructure failures and the corrected cursor expectation are identified separately in the archived README.
No infrastructure failure is counted as a behavioral RED.
All owned children and PTYs were closed and waited for; no background process remains.
Phase 1's 34-file archive remains unchanged, and the controller verified all 27 product-boundary hashes.
See the [full-plan progress report](../reports/pm-261007-t0-phase02-progress.md).

## Independent verification checkpoint: 2026-10-07

The [independent tester](../reports/tester-261007-t0-phase02.md) reports passing automated checks.
The narrow run passes 6 top-level tests and 12 subtests; the full race run passes 12 top-level tests and 17 subtests.
Vet, build, and formatting checks exit 0.
The immutable [archive manifest](artifacts/phase-02-source/archive-sha256.json) covers 79 files after independent evidence was added.
All 79 archive hashes and all 27 protected product hashes match.
Permanent output failure still reports terminal-byte restoration as unavailable; that expected result is not a restoration PASS.

The user removed intermediate human code-review approval pauses and authorized continued execution.
Mandatory review and independent verification remain in force.
Phase 3 started after these checks passed.
Real iTerm2 evidence remains pending, so Phase 2 and complete G6 acceptance remain incomplete.
The existing acceptance checkboxes are unchanged.
See the [full-plan checkpoint report](../reports/pm-261007-t0-phase02-independent.md).

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
