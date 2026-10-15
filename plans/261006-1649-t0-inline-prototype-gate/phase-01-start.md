---
title: "Phase 1: Terminal oracle and isolated module"
status: done
---

# Phase 1: Terminal oracle and isolated module

## Overview

Build a trustworthy fixture before testing the renderer.
This is implementation detail for later execution; no gate pass is claimed.

## Requirements

Use the scenario matrix as the functional contract.
Keep module isolation, output ownership, observable failures, and complete cleanup as required constraints.

## File inventory

Create `go.mod`, `go.sum`, `main.go`, `model.go`, `terminal_test.go`, `oracle_test.go`, `README.md`, and `testdata/` under the proposed scratch location after location validation.
Use `go.sum` only through Go tooling.
Create only fixture files that the selected APIs require.
No root file is an implementation target.

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
| `spawnPTY, waitPredicate, captureOutput, assertTerminalState` | PTY child launch to cancel/close/wait | Scenario matrix, failure cases, and prior phase regression checks |

## Architecture and dependency map

Location validation → module pin verification → dependency source read → minimal stock program → PTY driver → output capture → independent screen/scrollback oracle.
Select one licensed PTY dependency and an independent terminal emulator after checking their APIs and history support.
`github.com/charmbracelet/x/vt` is a candidate, not a confirmed pin or API contract.
Prove its exact pinned build, query responses, Unicode rules, and scrollback behavior using hand-written ANSI fixtures.
Do not copy kiln testkit source because its inspected checkout has no license.
Record versions and license provenance.
The oracle must track screen rows, native history, cursor, active buffer, terminal modes, and complete writes.

## Scenario matrix

| Case | Required assertion |
|---|---|
| Plain known terminal byte stream | Exact rows, cursor, and scrollback match a hand-written expected result |
| CR/LF, wrap, erase, cursor motion | Emulator state follows terminal control behavior |
| Main versus alternate buffer | History is tracked separately and buffer transitions are observable |
| Fragmented PTY reads | Result is independent of byte chunk boundaries |
| Negative control | A deliberate missing line or wrong cursor fails the oracle |
| Startup and normal quit | Live editor appears and shell state is restored |
| Observed terminal output FD | Wrapper exposes the real slave FD; ioctl/SIGWINCH reaches model dimensions without injected messages |

## Implementation steps

1. Read the listed owners and pinned dependency source.
2. Execute Tests Before; save valid RED evidence for new behavior and the initial result for stock baseline controls.
3. Implement the GREEN steps in the isolated module.
4. Execute Tests After and compare every scenario criterion.
5. Save evidence and inspect the risk signal before advancing.

## Tests Before (RED and stock baseline)

Write oracle self-tests before the program.
Use known byte sequences and hand-written expectations, not expectations produced by the application layout code.
Add normal-exit restoration and stock live-editor baseline checks; they may already PASS.
Keep the new fixture and editor behavior tests-first without forcing an upstream defect.
Record the failing command and exact assertion.
Run checks before implementing new behavior and retain the failing behavioral assertion.
A stock baseline or existing correct control may PASS immediately; preserve that result and do not manufacture a failure.
Require a reproduced failing terminal assertion before any renderer fix.
A test infrastructure error is not a valid RED result.

## Refactor (GREEN)

Implement the smallest real inline model and deterministic scenario driver.
Use `tea.Println` for a startup block; keep alternate screen and mouse reporting off.
Tee only the renderer-owned output through a synchronized observer that reports actual write results.
The observer must preserve the pinned `term.File` interface and forward the PTY slave `Fd()`; a plain `io.Writer` wrapper can disable terminal size detection and SIGWINCH handling.
Verify this interface against the pinned source, then assert the model receives ioctl/SIGWINCH dimensions without an injected `WindowSizeMsg`.
A capture callback records bytes, not command completion.
Retain the PTY slave descriptor to compare termios before startup, during raw mode, and after exit.
Record initial size and cursor, paste, keyboard, mouse, and synchronized-output modes.
Drive actual input bytes, ioctl resize and SIGWINCH.
Use a separate control/status pipe for scenario checkpoints and bounded predicate waits instead of fixed sleeps.
Do not put test commands into the editor input path.
Test child cancellation and wait for all children and PTYs to close.
Keep the smallest correct implementation and remove only redundant scratch code.
Do not weaken assertions to hide terminal errors.

## Tests After

Run oracle self-tests and the stock smoke scenario.
Confirm the negative control still fails when deliberately enabled.
Run a real terminal smoke test and record iTerm2 name/version, dimensions, locale, OS, dependency graph, and command.
Do not infer real-terminal behavior from the emulator.

## Commands

Run these proposed commands from the validated scratch module location with `GOWORK=off`.
The scenario flags are requirements for the new driver, not claims about an existing executable.
Scenario data is explicit terminal test input, not a fake provider or a replacement for real decoding.

```sh
export GOWORK=off
export GOCACHE=/private/tmp/askcore-t0-09kf6w9r-go-cache
go mod init askcore-t0-inline
go get charm.land/bubbletea/v2@v2.0.10
go mod tidy
go test -run 'TestOracle|TestStartup|TestNormalExit' -count=1 ./...
go test -race -run 'TestOracle|TestStartup|TestNormalExit' -count=1 ./...
go run . --scenario smoke
```

## Historical success criteria

> - [ ] Oracle negative controls fail; known ANSI cases pass; raw/restore snapshots and clean module build are saved.
> - [ ] Every scenario has a result with a command and saved evidence.
> - [ ] New behavior has valid RED-to-GREEN evidence; already-correct stock controls retain their initial PASS results.
> - [ ] Focused checks and prior phase regressions pass.
> - [ ] Real-terminal observations are separate from emulator assertions.
> - [ ] No root source, manifest, workspace, or runtime dependency changed.
> - [ ] All owned child processes and PTYs are closed after each run.

## Risk signal and response

The oracle accepts an intentionally wrong cursor or history.
Stop all gate scoring, correct the independent oracle, and rerun its controls.

## Rollback

Stop and wait for only the child processes started by this phase.
Restore the terminal before collecting shell evidence.
Keep the stock baseline, failing captures, and the tested graph.
Revert only this phase's scratch changes to its recorded prior commit or saved diff after checking for user edits.
Do not replay a partially written block or erase native history to recover a failed run.

## Execution checkpoint: 2026-10-07

Phase 1 automated implementation is available for review.
Phase 1 is not complete.
Final code review accepted the two fixes.
The user approved independent tests and continuation to Phase 2.
Independent tests are in progress; the real iTerm2 smoke check remains pending.
The accepted requirements and unchecked completion items above remain in force.

| Execution surface | Current evidence |
|---|---|
| Isolated source and commands | [Archived source README](artifacts/phase-01-source/README.md) |
| Exact pins | Bubble Tea v2.0.10; creack/pty v1.1.24; x/vt v0.0.0-20261004011457-ad85c59fdf4e |
| Graph and MIT licenses | [Resolved graph](artifacts/phase-01-source/evidence/module-graph.txt), [module manifest](artifacts/phase-01-source/go.mod), notices in archived evidence/licenses |
| Six automated tests and race check | [Final race log](artifacts/phase-01-source/evidence/race-green.txt); all six tests PASS |
| Exact final terminal assertions | [Focused check log](artifacts/phase-01-source/evidence/review-green.txt); rows, history, cursor, and main buffer PASS |
| Vet and build | [Vet log](artifacts/phase-01-source/evidence/vet.txt) and [build log](artifacts/phase-01-source/evidence/build.txt); commands PASS |
| Capture and integrity | [ANSI output](artifacts/phase-01-source/evidence/smoke-output.ansi) and [archive hashes](artifacts/phase-01-source/archive-sha256.json) |
| TDD evidence | [Initial oracle PASS](artifacts/phase-01-source/evidence/oracle-initial.txt), [smoke RED replay](artifacts/phase-01-source/evidence/smoke-red.txt), [review negative controls](artifacts/phase-01-source/evidence/review-controls-before.txt) |

The PTY driver uses unwrapped terminal files and separate control, status, and diagnostic outputs.
Real ioctl and SIGWINCH reach the model without an injected size message.
Retained-slave termios is compared before raw mode, during raw mode, and after `tea.Run` returns while the child is still alive.
On macOS, the slave returns ENOTTY after the session leader exits; this is a fixture limit.
The final terminal-state assertion follows child exit and output drain.
All owned children and PTYs were closed and waited for; no background process remains.

The first missing-editor behavioral RED also had a cleanup fault.
The saved RED log is a replay against the saved baseline after the fixture cleanup was corrected.
It is not evidence of a clean original RED run or an upstream renderer defect.
Initially correct oracle controls retain their PASS result.
No renderer fix was applied.

The controller found iTerm2 3.7.3 from its local plist.
Computer-use app access was denied, so no real iTerm2 result is claimed.
ASCII fragmentation, split UTF-8 for `é界`, and CPR are tested.
Combining characters, ZWJ fragmentation, and keyboard negotiation remain unproved.
No G1, G2, G3, G6, or D14 acceptance is claimed.
See the [full-plan progress report](../reports/pm-261007-t0-phase01-progress.md).

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
