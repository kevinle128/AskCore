# T0 Phase 2 partial execution progress

Date: 2026-10-07.
Work context: `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther`.
Execution flags: `--tdd`; no `--auto` and no `--yagni`.
Environment: macOS arm64, user dale, Go 1.27.0, Asia/Ho_Chi_Minh, C.UTF-8.

## Status and full-plan reconciliation

The [T0 plan](../261006-1649-t0-inline-prototype-gate/plan.md) remains in progress.
Phase 2 automated source is frozen and final source review returned DONE.
Worker logs show 12 top-level tests, race, vet, and build passing.
Independent Phase 2 tests await the next user approval.
Real iTerm2 3.7.3 observations remain pending because computer-use app access was denied.
Full G6, T0, and D14 acceptance are incomplete.

| Phase swept | Current delivered evidence | Completion state |
|---|---|---|
| [1: Oracle and module](../261006-1649-t0-inline-prototype-gate/phase-01-start.md) | Six independently verified tests and immutable 34-file archive | Real-terminal check pending |
| [2: Editor and input](../261006-1649-t0-inline-prototype-gate/phase-02-editor-and-input.md) | Multiline draft, real PTY input, lifecycle cases, 12 worker tests, final source review | Independent tests and real-terminal check pending |
| [3: Ordered scrollback](../261006-1649-t0-inline-prototype-gate/phase-03-ordered-scrollback.md) | None | Not started |
| [4: Selector and resize](../261006-1649-t0-inline-prototype-gate/phase-04-selector-and-resize.md) | None | Not started |
| [5: Evidence and D14](../261006-1649-t0-inline-prototype-gate/phase-05-evidence-and-decision.md) | None; per-phase archives do not complete this phase | Not started |

All five phase files and the index were read for this sync-back.
The CLI status command reports 0 of 65 phase checklist items checked and 0 of 5 phases complete, or 0% checklist completion.
This percentage describes accepted checklist completion, not the amount of implemented work.
No later-phase behavior requires backfill.
Existing completion checkboxes are preserved because real-terminal requirements remain unmet.
The append-only checkpoint keeps the accepted phase table and phase frontmatter unchanged.
The installed CLI phase-update help states that status is file-owned and cannot be changed through that command.
Its checkbox commands mark all items and cannot safely represent this partial execution.
Index notes and evidence were updated for Phase 2 with `AGENTKIT_HOME=/private/tmp/ask-t0-agentkit-plan-index`.
No new runtime task surface is available.

## Durable evidence and limits

The [Phase 2 README](../261006-1649-t0-inline-prototype-gate/artifacts/phase-02-source/README.md) contains exact rerun and manual smoke commands.
The [archive manifest](../261006-1649-t0-inline-prototype-gate/artifacts/phase-02-source/archive-sha256.json) contains 72 file hashes.
The controller verified that Phase 1's 34-file archive remains unchanged and that all 27 product-boundary hashes match.
No root source, dependency, or configuration change is part of this checkpoint.
No evergreen product documentation update is required.

The [final tests](../261006-1649-t0-inline-prototype-gate/artifacts/phase-02-source/evidence/phase2-tests-final.txt) and [race log](../261006-1649-t0-inline-prototype-gate/artifacts/phase-02-source/evidence/phase2-race-final.txt) pass.
Worker vet and build logs also pass.
PTY tests verify actual key bytes, separate Enter submission, absent and supported capability paths, exactly one 2,000-line PasteMsg, complete draft integrity, and visible cell cursor positions.
The explicit terminal responder handles actual query bytes and preserves sibling emulator responses.
It does not claim that x/vt implements Kitty negotiation.

The [input RED](../261006-1649-t0-inline-prototype-gate/artifacts/phase-02-source/evidence/phase2-input-red.txt) preceded the missing paste and Alt+Enter behavior changes.
The [partial-frame RED](../261006-1649-t0-inline-prototype-gate/artifacts/phase-02-source/evidence/phase2-partial-red.txt) proves that stock cleanup left synchronized output enabled after an actual eight-byte prefix write and error.
The [GREEN log](../261006-1649-t0-inline-prototype-gate/artifacts/phase-02-source/evidence/phase2-partial-green.txt) proves reset through the same output owner after the renderer stops.
Permanent failure reports terminal-byte restoration as unavailable while termios restoration is checked separately.
Full G1 ordered-frontier and transcript replay acceptance remain Phase 3 work.

Normal quit, context cancellation, actual output failure, SIGINT, SIGTERM, SIGHUP, model panic, and command panic use separate children.
The termios comparison follows Bubble Tea cleanup while the session leader remains alive.
On macOS, a slave ioctl after session-leader exit returns ENOTTY.
Final terminal observations follow child exit and output drain.
All owned children and PTYs were closed and waited for; no background process remains.
Artifact-path, deadline-support, query-replacement, and cursor-expectation faults are fixture evidence, not upstream behavioral RED claims.

## Remaining work

Obtain the next approval for independent Phase 2 tests and preserve their results.
Run the required iTerm2 observations through an authorized manual path.
Keep full G6 and both phase completion records pending until required evidence is available.
Do not start Phase 3 from this checkpoint.
Optional pixel, palette, keepalive, and modifyOtherKeys probes remain unverified nonblocking Phase 5 work.

Unresolved questions: no new product decision; independent-test approval and real-terminal evidence remain pending.
