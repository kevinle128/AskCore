---
title: "Phase 3: Ordered scrollback, G1"
status: done
---

# Phase 3: Ordered scrollback, G1

## Overview

Prove ordered stable-prefix output and a usable live editor throughout streaming.
This is implementation detail for later execution; no gate pass is claimed.

## Requirements

Use the scenario matrix as the functional contract.
Keep module isolation, output ownership, observable failures, and complete cleanup as required constraints.

## File inventory

Create `transcript.go`, `transcript_test.go`, `commit_test.go`, and `g1_test.go` in the scratch module.
Modify scratch model, scenario driver, output observer, and oracle.
A custom committer file is not authorized merely by this inventory.

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
| `stablePrefix, scheduleCommit, observeWrite, stopTranscript` | Block schedule to terminal-confirmed prefix or failure | Scenario matrix, failure cases, and prior phase regression checks |

## Architecture and dependency map

Phase 2 editor + Phase 1 oracle → ordered content blocks → stable commit frontier → stock Println → renderer writes → terminal state.
Track scheduled progress separately from confirmed progress.
The observer correlates actual output and frame state with scenario checkpoints; command return is not confirmation.
Inspect renderer insertion/flush scheduling and prove that live frame output cannot overtake a committed prefix.
Research found the event-loop insertion error can be discarded in stock v2.0.10.
Therefore, prove failure observability with injected actual writer errors; do not invent a successful stock acknowledgement API.

## Scenario matrix

| Case | Required assertion |
|---|---|
| 2,000 uniquely numbered committed lines | Exact order, no missing or duplicate lines across visible screen and history |
| Growing live tail | Editor remains visible and cursor stays in the draft |
| One commit taller than terminal | All lines retained and live view remains present |
| Commit while live region shrinks | No stale rows, missing tail, or cursor drift |
| Completion order differs from submission order | Terminal order follows transcript sequence |
| Zero-byte write error | No confirmed progress and controlled shutdown |
| Partial write plus error/short write | Stop transcript/frame output; preserve cleanup; report exact written prefix; no automatic block replay |
| Recoverable failure after mode 2026 enable, before disable | Cleanup resets synchronized output; no content replay |
| Permanent output failure | Failed terminal-byte restoration is explicit; no false restore PASS |
| Resize while commit is pending | One ordered output path preserves prefix and editor |

## Implementation steps

1. Read the listed owners and pinned dependency source.
2. Execute Tests Before; save valid RED evidence for new behavior and the initial result for stock baseline controls.
3. Implement the GREEN steps in the isolated module.
4. Execute Tests After and compare every scenario criterion.
5. Save evidence and inspect the risk signal before advancing.

## Tests Before (RED and stock baseline)

Add stable-prefix unit tests and out-of-order completion tests.
Add PTY G1 assertions before adjusting stock behavior.
Inject zero-byte and partial write failures through the renderer output adapter.
Include an offset after `CSI ?2026 h` and before `CSI ?2026 l`, with a recoverable writer so cleanup can be observed.
Also test permanent writer failure as a distinct restoration limit.
Assert no subsequent transcript/frame attempt and no confirmed frontier advance for an incomplete block.
Allow terminal-mode restoration through the same owned cleanup path; record cleanup writes separately from transcript writes.
Require mode 2026 disable during recoverable cleanup; inspect pinned renderer close behavior rather than assuming stock emits it.
Save stock failure artifacts before any patch.
Run checks before implementing new behavior and retain the failing behavioral assertion.
A stock baseline or existing correct control may PASS immediately; preserve that result and do not manufacture a failure.
Require a reproduced failing terminal assertion before any renderer fix.
A test infrastructure error is not a valid RED result.

## Refactor (GREEN)

Implement ordered scenario blocks and a growing live tail.
Schedule only a stable prefix, with one outstanding ordered insertion operation.
Use stock Println first and inspect writes with the independent oracle.
If stock fails, reproduce the root cause and apply only a measured kiln-class fix within the five-class budget.
Do not disguise pre-wrapping or chunking as unmodified stock evidence.
Record any such adaptation as a separate tested variant.
Keep the smallest correct implementation and remove only redundant scratch code.
Do not weaken assertions to hide terminal errors.

## Tests After

Run all G1 cases against stock and each changed variant.
Use one-byte/randomized output fragmentation to check observation independence.
Run the full scratch suite with the race detector.
In a real terminal, inspect native scrollback and the live editor after oversized output and shrink.
Writer-confirmed bytes support progress tracking but do not alone prove correct terminal placement.

## Commands

Run these proposed commands from the validated scratch module location with `GOWORK=off`.
The scenario flags are requirements for the new driver, not claims about an existing executable.
Scenario data is explicit terminal test input, not a fake provider or a replacement for real decoding.

```sh
export GOWORK=off
export GOCACHE=/private/tmp/askcore-t0-09kf6w9r-go-cache
go test -run 'TestTranscript|TestCommit|TestG1' -count=1 ./...
go test -race -count=1 ./...
go run . --scenario g1 --lines 2000
```

## Historical success criteria

> - [ ] G1 retains all 2,000 ordered lines exactly once; oversized commit and shrink retain the live editor and exact cursor; partial writes stop transcript output without replay.
> - [ ] Every scenario has a result with a command and saved evidence.
> - [ ] New behavior has valid RED-to-GREEN evidence; already-correct stock controls retain their initial PASS results.
> - [ ] Focused checks and prior phase regressions pass.
> - [ ] Real-terminal observations are separate from emulator assertions.
> - [ ] No root source, manifest, workspace, or runtime dependency changed.
> - [ ] All owned child processes and PTYs are closed after each run.

## Risk signal and response

History order differs, the live editor disappears, or output continues after a partial failure.
Mark G1 failed, preserve the stock run and exact patch diff, and stop replay or further writes.
Exceeding five fix classes requires a user fallback decision.

## Rollback

Stop and wait for only the child processes started by this phase.
Restore the terminal before collecting shell evidence.
Keep the stock baseline, failing captures, and the tested graph.
Revert only this phase's scratch changes to its recorded prior commit or saved diff after checking for user edits.
Do not replay a partially written block or erase native history to recover a failed run.

## Execution checkpoint: 2026-10-07

The [Phase 3 source archive](artifacts/phase-03-source/README.md) is frozen with 123 archived file hashes.
Worker checks pass: 27 top-level tests, race, vet, build, and formatting.
Final review and independent verification are pending at this checkpoint.
The user removed intermediate human approval pauses; required source review and independent tests remain in force.
Real iTerm2 3.7.3 observations remain pending because computer-use app access was denied.
Phase 3, full G1, T0, and D14 acceptance are incomplete.

Stock Println passes ordered 2,000-line output, oversized insertion, and live-region shrink in the PTY oracle.
No kiln-class renderer fix, fork, or custom committer was used.
The output owner distinguishes insertion WriteString calls from renderer frames, confirms complete writes only, and stops after errors or short writes.
Failure tests record the exact actual prefix and reject later content without replay.
Cleanup uses the same owner after the renderer stops; permanent output failure reports terminal-byte restoration as unavailable.
Termios is observed after tea.Run returns while the child is alive; final ANSI state is observed after child exit and output drain.
Real ioctl/SIGWINCH resize during a held insertion and one-byte/random output fragmentation pass.
The manual startup pre-check used an invalid timeout flag and is infrastructure evidence, not a behavioral RED.
The earlier stock timeout was an observer matching fault, not a renderer defect.
See the archived README for the valid RED evidence, exact commands, and these limits.

The controller verified all 27 protected product hashes and the unchanged 34-file Phase 1 and 79-file Phase 2 archives.
All owned test children were waited; no known owned background process remains.
A system-wide process listing was denied by the sandbox.
The five-phase sweep retains 0 of 65 checklist items checked and 0 of 5 phases complete.
Phase 4 preparation is read-only; Phase 4 implementation and Phase 5 have not started.
See the [progress report](../reports/pm-261007-t0-phase03-progress.md).

## Independent verification update: 2026-10-07

Final source review returned DONE with no findings.
Independent checks pass: the narrow run has 14 top-level tests and 13 subtests; the full race run has 27 top-level tests and 30 subtests.
Vet, build, and formatting exit 0.
The tester verified 123 archive hashes, 16 frozen source hashes, all 27 protected product hashes, and unchanged 34-file Phase 1 and 79-file Phase 2 archives.
Phase 4 implementation is now authorized after this preparation checkpoint.
Real iTerm2 evidence and full gate acceptance remain pending; completion checkboxes are unchanged.

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
