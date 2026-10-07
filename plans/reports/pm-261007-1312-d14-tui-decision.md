# D14 TUI decision and T0 acceptance

## Decision

The user stated: “Terminal.app/iTerm2 done, tiếp D14 đi”.
T0 is accepted and D14 is closed based on retained independent automated evidence and this user-reported physical-terminal acceptance.
No exact tested terminal versions, screenshots, recordings, or per-scenario manual results were supplied with this statement.
The earlier requirement for independently retained physical-terminal artifacts is superseded for this closure, not retrospectively proved.
Historical checklists remain unchecked in quoted blocks; current accepted criteria are separate.

Select `charm.land/bubbletea/v2` v2.0.10 with the tested minimal local Bubble Tea renderer patch.
Retain exact unmodified upstream Ultraviolet `v0.0.0-20260703014108-f5a850f9c2b7` and Go 1.27.0.
The renderer uses one output owner, ordinary exact-once commits, and complete ordered resize replay after a generation-tagged 120 ms settle delay.
The full application transcript is retained; resize repair can remove pre-startup terminal history.
Root dependency migration belongs to T1.
T1 must retain reproducible patch provenance or an immutable product fork pin; no remote immutable fork release exists yet.
Root `go.mod` still pins Bubble Tea v1.3.10 and is unchanged by this decision.

## Evidence and full-phase reconciliation

The [independent tester](tester-261007-1230-inline-replay.md) passed 43 Go tests, race, vet, build, the local renderer regression, and 18 default Alacritty/Ghostty cases.
The [promoted run](../261007-1156-inline-resize-fix/artifacts/replay-verification/promoted-default/results.json) independently selected the new archive and passed the same Go and native case set.
The [source integrity proof](../261007-1156-inline-resize-fix/artifacts/replay-verification/source-integrity.json) verifies 987 manifest entries, 34 Bubble Tea vendor files, and 54 exact upstream Ultraviolet files.
The [candidate provenance](../261007-1156-inline-resize-fix/artifacts/replay-candidate/PROVENANCE.md) owns the tested patch and graph.

| T0 phase | Reconciled acceptance |
|---|---|
| 1: Oracle and module | Retained independent fixture/source checks plus user-reported physical smoke acceptance |
| 2: Editor and G6 | Retained input, paste, and cleanup checks plus user-reported physical acceptance |
| 3: Ordered scrollback and G1 | Retained order, full transcript, pending-write, and failure checks plus user-reported physical acceptance |
| 4: Selector and G2/G3 | Retained native resize/draft/selector checks plus user acceptance of physical behavior and resize replay |
| 5: Evidence and D14 | Frozen artifacts and explicit nonblocking not-run limits retained; D14 route selected |

All five phases are accepted under the current recorded decision.
This is not a claim that all 65 historical criteria have independently captured evidence.
The 65 historical checklist entries remain unchecked; only the 15 current reconciliation criteria are checked through the plan CLI.
The original T0 plan is completed through the plan CLI.
Historical checkpoints and immutable manifests remain unchanged in meaning, including earlier `d14_ready: false` results.
No tests were rerun, process started, root dependency changed, commit created, or push performed for this closure.

## Remaining limits

Ghostty 52×15 native history capacity remains limited to the measured 1,096 markers in its plain-process control.
Alacritty 1×1 extreme-size behavior remains unaccepted.
G4/G5 and optional capability checks retain their explicit nonblocking not-run reasons.
No universal terminal capacity, extreme-size support, or independent physical evidence is claimed.
Unresolved questions: none for D14 selection.

## Metadata reconciliation

The CLI closed all five phases from their current checklists and returned `done`, but left their YAML `status: todo` and the earlier table unchanged.
The live phase-update command rejects file-owned status changes and directs file edits plus reindex.
The controller authorized that direct-file fallback for this mismatch.
The phase YAML now says `done`; the current phase table says `Accepted`.
Reindex reconciles these canonical fields without changing historical quoted checklists.
The version selection is recorded as decided after T0 acceptance, not as an explicit user version vote.
