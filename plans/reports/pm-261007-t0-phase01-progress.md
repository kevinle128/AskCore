# T0 Phase 1 partial execution progress

Date: 2026-10-07.
Work context: `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther`.
Execution flags: `--tdd`; no `--auto` and no `--yagni`.
Environment: macOS arm64, user dale, Go 1.27.0, Asia/Ho_Chi_Minh, C.UTF-8.

## Status and reconciliation

The [T0 plan](../261006-1649-t0-inline-prototype-gate/plan.md) is in progress.
Final code review accepted the two fixes.
The user approved independent tests and continuation to Phase 2.
Independent tests are in progress against frozen source.
Phase 1 still lacks real iTerm2 evidence.
Phase 1 is not complete.
No new runtime task-management surface is available.
The installed CLI status command reports 0 of 5 phases complete and 0 of 65 phase checklist items checked, or 0% checklist completion.
This count measures accepted checklist completion, not the amount of implementation work.

| Phase swept | Delivered work | Completion state |
|---|---|---|
| [1: Oracle and module](../261006-1649-t0-inline-prototype-gate/phase-01-start.md) | Isolated program, PTY driver, oracle, six tests, evidence | Partial; checks remain unchecked |
| [2: Editor and input](../261006-1649-t0-inline-prototype-gate/phase-02-editor-and-input.md) | None | Not started |
| [3: Ordered scrollback](../261006-1649-t0-inline-prototype-gate/phase-03-ordered-scrollback.md) | None | Not started |
| [4: Selector and resize](../261006-1649-t0-inline-prototype-gate/phase-04-selector-and-resize.md) | None | Not started |
| [5: Evidence and D14](../261006-1649-t0-inline-prototype-gate/phase-05-evidence-and-decision.md) | None; the Phase 1 archive does not complete this phase | Not started |

All five phase files and the index were read during this sync-back.
No later-phase delivered behavior needs backfill.
The CLI checkbox operation marks every checkbox in a phase and cannot safely record this partial checkpoint.
All existing completion checkboxes are preserved.
The plan frontmatter is already `in-progress`.
The accepted phase table and phase frontmatter remain unchanged under the append-only assignment.
The appended checkpoint records that Phase 1 work has started.
The controller reports that the git-pointer operation was blocked by the sandbox and used an explicit plan-file fallback with a local CLI index at `/private/tmp/ask-t0-agentkit-plan-index`.
The direct CLI status read succeeded during this sync-back.

## Evidence and boundaries

The [archived README](../261006-1649-t0-inline-prototype-gate/artifacts/phase-01-source/README.md) owns rerun commands and fixture limits.
Pins are Bubble Tea v2.0.10, creack/pty v1.1.24, and x/vt v0.0.0-20261004011457-ad85c59fdf4e.
All three licenses are MIT, with notices retained in the archive.
The [exact graph](../261006-1649-t0-inline-prototype-gate/artifacts/phase-01-source/evidence/module-graph.txt) and [archive hashes](../261006-1649-t0-inline-prototype-gate/artifacts/phase-01-source/archive-sha256.json) are saved.
The [final race log](../261006-1649-t0-inline-prototype-gate/artifacts/phase-01-source/evidence/race-green.txt) records all six tests passing.
The [focused log](../261006-1649-t0-inline-prototype-gate/artifacts/phase-01-source/evidence/review-green.txt) records exact final terminal assertions.
Vet and build pass in worker logs.
Separate stderr artifacts keep diagnostics outside renderer output.
Real ioctl and SIGWINCH report resized model dimensions without test message injection.

The restored termios snapshot is taken after Bubble Tea cleanup and before the child session leader exits.
A post-process-exit slave ioctl returns ENOTTY on macOS.
The final emulator assertion follows process exit and output drain.
Cancellation proves owned-child termination and reaping, not the full signal-restoration gate.
No owned background process remains.

The [saved RED](../261006-1649-t0-inline-prototype-gate/artifacts/phase-01-source/evidence/smoke-red.txt) is a corrected-fixture replay against a retained baseline.
The first missing-editor behavioral failure also had a cleanup fault.
No clean first-run RED or upstream defect is claimed.
Known ANSI controls passed initially.
Negative controls reject missing startup, wrong history, and wrong cursor expectations.

The controller found iTerm2 3.7.3 from a local plist, but computer-use app access was denied.
No real-terminal smoke result is available.
Combining characters, ZWJ fragmentation, and negotiated keyboard capabilities remain unproved.
G1, G2, G3, G6, and D14 remain pending.
Root product behavior, dependencies, and configuration are unchanged.
No evergreen product documentation update is required; the scratch README records fixture use.

## Remaining work

Complete the authorized independent tests of the frozen Phase 1 source.
The controller can start Phase 2 after those tests pass; no Phase 2 work is delivered at this checkpoint.
Run and record the real iTerm2 smoke check through an authorized manual path.
Reconcile Phase 1 completion items only after all required evidence is available.
Keep phases 2–5 pending until their preceding acceptance requirements are met.

Unresolved questions: no new product decision; real-terminal access and remaining verification are pending.
