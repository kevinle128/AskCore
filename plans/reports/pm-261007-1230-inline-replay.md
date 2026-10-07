# Inline resize documentation and plan sync

## Result

The accepted inline resize fix is complete for its headless execution scope.
The [review](code-review-261007-1230-inline-replay.md) returned DONE with no blocking finding.
The [independent tester](tester-261007-1230-inline-replay.md) passed 43 Go tests, race, vet, build, the local renderer regression, and all 18 Alacritty/Ghostty default cases.
Ordinary commits retained 2,000 ordered markers exactly once; pending-write replay snapshots retained complete ordered generations of 1 and 2,000 markers.
All 36 owned process PIDs were absent after cleanup and all 426 protected file hashes matched.

The promoted default runner then exited 0 with 43 selected Go tests and 18 native cases passing.
Five negative controls passed.
See [results](../261007-1156-inline-resize-fix/artifacts/replay-verification/promoted-default/results.json), [boundaries](../261007-1156-inline-resize-fix/artifacts/replay-verification/promoted-default/boundaries.json), [tests](../261007-1156-inline-resize-fix/artifacts/replay-verification/promoted-default/go-tests.txt), and [build](../261007-1156-inline-resize-fix/artifacts/replay-verification/promoted-default/go-build.txt).

## Frozen candidate and boundaries

The [candidate source](../261007-1156-inline-resize-fix/artifacts/replay-candidate/README.md) has 987 verified manifest entries, or 988 files including the manifest.
The manifest SHA-256 is `788dee6e7aadb89d076ec4c4f9348f27f067539f0e3efae16df4125c6a1f2aa2`.
The [source integrity proof](../261007-1156-inline-resize-fix/artifacts/replay-verification/source-integrity.json) verifies 34 Bubble Tea files against the reviewed candidate and 54 Ultraviolet files against exact upstream source.
The abandoned origin correction is not part of this candidate.
The promoted run also retained all 190 files of the earlier archive, including its manifest.
Root production source and dependencies remain unchanged.

## Full-plan sync

| Record | Final state |
|---|---|
| [Resize fix](../261007-1156-inline-resize-fix/plan.md) | Completed through the plan CLI after independent and promoted checks |
| [Original T0 plan](../261006-1649-t0-inline-prototype-gate/plan.md) | In progress; all five phase records swept |
| Original five phase checklists | 0 of 65 checked; physical-terminal acceptance remains incomplete |
| [Runner README](../../e2e/tui/README.md) | Current archive, accepted resize contract, focused commands, and capacity limits |
| [Roadmap T0](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#t0-inline-prototype-gate-next-priority) | Measured resize route linked; D14 open |

All five historical phase records now point to the accepted resize route and independent evidence.
The earlier no-purge/no-replay requirement is superseded for resize repair only.
A partial output failure still stops content and must not trigger whole-block replay.
Application state retains all 2,000 entries; terminal scrollback is a rebuildable view.

CLI commands used `AGENTKIT_HOME=/private/tmp/ask-t0-agentkit-plan-index`.
`ak plan reindex --apply --json` exited 0 and recognized the resize plan as `AskCore/261007-0603-13`.
`ak plan update AskCore/261007-0603-13 --status completed --json` exited 0 and returned `status: completed`.
No status field was edited by hand.
No commit or push was performed.

## Remaining limits

The default pending-write native history assertion retains all 2,000 markers at the measured 13×6 → 40×12 sizes.
The opt-in `g3-capacity` probe retains 52×15 and its Ghostty failure.
The promoted command was verified separately and exited 1 with `retained=1097, expected=2000`.
Its [result](../261007-1156-inline-resize-fix/artifacts/replay-verification/capacity-probe/results.json) and [boundary hashes](../261007-1156-inline-resize-fix/artifacts/replay-verification/capacity-probe/boundaries.json) are retained.
Plain-process controls there retain only 1,096 markers, with or without purge and with the larger scrollback profile.
No universal native history capacity guarantee is claimed.
Real iTerm2, extreme-size acceptance, and D14 remain pending.
Unresolved questions: none for this documentation sync.
