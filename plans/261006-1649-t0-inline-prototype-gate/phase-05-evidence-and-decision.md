---
title: "Phase 5: Evidence and D14 decision"
status: done
---

# Phase 5: Evidence and D14 decision

## Overview

Package reproducible gate results and a bounded renderer recommendation for user review.
This is implementation detail for later execution; no gate pass is claimed.

## Requirements

Use the scenario matrix as the functional contract.
Keep module isolation, output ownership, observable failures, and complete cleanup as required constraints.

## File inventory

Modify scratch README and scenario/test files only when evidence checks need corrections.
Create scratch `evidence/manifest.json` and per-run artifacts, plus a T0 result report under `plans/reports/` during implementation.
No root upgrade, roadmap decision mutation, vendored fork, or SDK contract is performed by this phase.
Exclude personal terminal history and secrets from artifacts.
Archive source, module files, execution commands, and artifact checksums under the plan before removing the temporary module.

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
| `validateEvidence, recordResult, archiveSource` | Run capture to durable report and scratch cleanup | Scenario matrix, failure cases, and prior phase regression checks |

## Architecture and dependency map

Phases 1–4 artifacts → evidence manifest → gate matrix → stock/patch comparison → D14 proposal.
Record tested dependency graph, OS, terminal versions, dimensions, locale, commit, commands, result, and artifact hashes.
Separate automated emulator assertions from real-terminal observations.
Each blocking gate needs both; a missing environment is pending evidence, not a pass.
For G6, an unsupported negotiated-key capability in iTerm2 is recorded separately; the PTY must prove that supported path, and iTerm2 must pass Alt+Enter fallback and paste integrity.

## Scenario matrix

| Case | Required assertion |
|---|---|
| Clean checkout scratch rerun | Documented commands reproduce results with pinned graph |
| G1/G2/G3/G6 | Each has exact criteria, automated artifact, real-terminal artifact, and result |
| G4 Kitty placeholder commit | Image remains in native history when supported; otherwise explicit unsupported/not-run record |
| G5 inline/alternate/inline | Full transcript from block list and final exit transcript; no effect on blocking score |
| Optional capabilities | Pixel/palette/keepalive/modifyOtherKeys support recorded separately |
| Stock versus patches | Stock baseline preserved; each fix class has a failing case and rerun |
| Fallback threshold | User sees raw committer versus fork evidence and chooses if required |
| Root isolation | Root manifest/source unchanged; no root import, replacement, or go.work |

## Implementation steps

1. Read the listed owners and pinned dependency source.
2. Execute Tests Before; save valid RED evidence for new behavior and the initial result for stock baseline controls.
3. Implement the GREEN steps in the isolated module.
4. Execute Tests After and compare every scenario criterion.
5. Save evidence and inspect the risk signal before advancing.

## Tests Before (RED and stock baseline)

Add evidence-manifest validation that fails for a blocking gate without commands or artifacts.
Add nonblocking G4/G5 checks as capability-conditioned tests, with explicit skip reasons rather than successful placeholders.
Add a scratch-boundary check for root imports, replacements, and workspace coupling.
Keep evidence validation distinct from terminal behavior tests.
Run checks before implementing new behavior and retain the failing behavioral assertion.
A stock baseline or existing correct control may PASS immediately; preserve that result and do not manufacture a failure.
Require a reproduced failing terminal assertion before any renderer fix.
A test infrastructure error is not a valid RED result.

## Refactor (GREEN)

Run all blocking scenarios from a clean scratch build.
Run G4/G5 where hardware support exists and record missing support honestly.
Summarize the five-class fix ledger and any unproved terminal assumptions.
Propose D14 stock, bounded patch route, or fallback-needed from measured results.
If fallback is needed, stop for the user choice and amend execution steps only after that choice.
A raw fallback must pre-wrap to width-1, split below terminal height, and preserve one output owner; a fork must retain provenance and pinning.
Raw cursor synchronization remains unproved; validate it instead of assuming `tea.Raw` completes synchronously.
Both routes rerun every blocking gate and partial-write/restoration checks.
Keep the smallest correct implementation and remove only redundant scratch code.
Do not weaken assertions to hide terminal errors.

## Tests After

Run `go test`, race checks, vet, and build in the scratch module.
Verify links, artifact hashes, exact result labels, and replay commands.
Check root `git diff` against the start snapshot, excluding authorized planning files.
Confirm all owned child processes have exited and PTYs are closed.
Mark D14 ready only when the blocking gate evidence is complete; approval and root migration remain separate work.

## Commands

Run these proposed commands from the validated scratch module location with `GOWORK=off`.
The scenario flags are requirements for the new driver, not claims about an existing executable.
Scenario data is explicit terminal test input, not a fake provider or a replacement for real decoding.

```sh
export GOWORK=off
export GOCACHE=/private/tmp/askcore-t0-09kf6w9r-go-cache
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build ./...
go run . --scenario all --evidence evidence
```

## Historical success criteria

> - [ ] Every blocking gate has emulator and iTerm2 evidence; G4/G5 and optional probes have explicit results; fix ledger and D14 recommendation match artifacts; source archive checksum verifies.
> - [ ] Every scenario has a result with a command and saved evidence.
> - [ ] New behavior has valid RED-to-GREEN evidence; already-correct stock controls retain their initial PASS results.
> - [ ] Focused checks and prior phase regressions pass.
> - [ ] Real-terminal observations are separate from emulator assertions.
> - [ ] No root source, manifest, workspace, or runtime dependency changed.
> - [ ] All owned child processes and PTYs are closed after each run.

## Risk signal and response

A report claims pass without an artifact, a terminal check is unavailable, or a fallback exceeds approved scope.
Keep D14 pending, state the missing evidence, and preserve runnable artifacts.
Rollback only the scratch variant under test, never user files or the stock baseline.

## Rollback

Stop and wait for only the child processes started by this phase.
Restore the terminal before collecting shell evidence.
Keep the stock baseline, failing captures, and the tested graph.
Revert only this phase's scratch changes to its recorded prior commit or saved diff after checking for user edits.
Do not replay a partially written block or erase native history to recover a failed run.

## Automated execution checkpoint: 2026-10-07

The [Phase 5 archive](artifacts/phase-05-source/README.md) contains 179 hashed files at this checkpoint.
The worker clean regular suite and final clean race run each pass 39 top-level tests.
The regular run preceded the clean-rerun metadata addition; the final race and focused validation cover that addition.
Vet, build, formatting, and final validation pass.
All 10 manifest artifact references match after terminal captures stopped changing.
Final source review and independent Phase 5 checks are pending at this checkpoint.

The [machine manifest](artifacts/phase-05-source/evidence/manifest.json) records automated G1/G2/G3/G6 PASS and separate real-terminal pending results.
D14Ready is false.
G4/G5 and optional probes have explicit not-run reasons, not successful placeholders.
The [result report](../reports/t0-261007-inline-prototype-result.md) and archived README retain exact commands and the public iTerm2 checklist.
Stock Println is the measured candidate with zero kiln-class renderer fixes.
Required adaptations are explicit WriteString insertion observation preserving term.File, complete-write frontier/error latch, and owned post-renderer reset/filtering of restoration bytes.
No fork, custom committer, root dependency migration, roadmap decision, commit, or push was performed.

Real iTerm2 3.7.3 observations remain pending because app access was denied.
Pinned x/vt height-shrink history truncation remains an emulator limit; native reflow is unproved.
Termios is checked after tea.Run while the child remains alive; final ANSI state follows child exit and drain.
All owned children were waited and no known owned background process remains.
The controller verified all 27 protected product hashes and unchanged prior archives of 34, 79, 130, and 155 files.

The full five-phase sweep retains 0 of 65 acceptance checklist items checked and 0 of 5 phases complete.
The plan remains in progress because all blocking gates still lack required real-terminal observations.
Only prototype README, plan records, and reports own the changed setup and commands; evergreen product docs need no change.
See the [final checkpoint report](../reports/pm-261007-t0-final-checkpoint.md).

## Final independent verification: 2026-10-07

Final Phase 5 source review returned DONE with no findings.
Independent fresh-copy checks pass: the focused run has 6 top-level tests and 14 subtests; the full race run has 39 top-level tests and 44 subtests, with 6 explicit capability skips.
Vet, compilation, formatting, post-run packaging, and final manifest validation all exit 0.
The tester verified the original 179 archive files, 24 source hashes, all 27 protected product hashes, all 10 manifest artifact references, and unchanged prior archives with 34, 79, 130, and 155 files.
The controller's final Phase 5 archive contains 189 hashed files after independent logs were added; all previous 179 hashes still match.
The result report includes the independent verification and its local links resolve.

Automated implementation, review, and independent verification are complete.
Required real iTerm2 observations remain pending, including native history reflow and physical Unicode placement.
G4/G5 and optional probes retain explicit not-run reasons.
D14Ready remains false; zero kiln renderer fixes and the disclosed output-owner adaptations remain the measured route.
The final five-phase sweep retains 0 of 65 acceptance checkboxes checked and 0 of 5 phases complete.
The plan remains in progress with no claim of full T0 or D14 acceptance.
No source, archive, journal, commit, or push change is part of this append-only update.

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
