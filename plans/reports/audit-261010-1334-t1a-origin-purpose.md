# T1a origin-purpose audit

Date: 2026-10-10.
Base: `7ea97b4`; working tree contains audit repairs and concurrent renderer edits.

## Decision

T1a implements the main terminal workflow required by the [parent roadmap](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#t1-tui-core).
The no-argument `ask` uses the real local leader, Unix socket, and ACP.
Startup fallback, streaming, queue input, retry and error display, cancellation, model selection, observer attachment, reconnect, session loss, detach, thinking display, resize, and terminal restoration have automated evidence.
The [plan](../261009-2239-t1a-local-tui/plan.md) must remain `in-progress`.
Native Terminal.app or iTerm2 acceptance is missing, and exact early prompt acceptance under concurrent clients is not proven.

## Context checked

- The approved design, all phase records, and the criterion-to-test map in the T1a plan.
- The parent T1 roadmap, TUI architecture, owning package README, and terminal runner setup.
- The [real-provider acceptance report](real-provider-acceptance-261010-t1a-tui.md), which records 21 calls with OpenAI API key, ChatGPT OAuth, and Anthropic OAuth.
- The actual client, session model, translator, entry point, and their tests.

The accepted startup-only fallback stays intact.
A lost leader after a successful connection does not trigger in-process fallback or prompt resend.
Tool expansion, truncation display, and `/copy` belong to T1b by explicit decision.
They are not missing T1a features.
The real-provider report is historical evidence; this audit did not repeat provider calls.
A real 429 cannot be forced, so deterministic provider-fixture evidence remains appropriate.

## Defects repaired

| Defect | Cause and repair | Regression evidence |
|---|---|---|
| Watching another running session freezes or splits its reply | Reset abandoned unstable transcript blocks, and the old prompt completion affected the newly watched run. Detach now freezes last observed content without claiming cancellation or completion. | `TestInteractiveSwitchFromRunningSession`, `TestDetachCommitsLastObservedContent` |
| Late request result closes a new selector | The result handler closed selectors before checking their generation. Only the selector owned by that generation is closed. | `TestStaleResultsKeepCurrentSelector` |
| A refused prompt can lose its draft after another client starts a run | `AgentStart` lacks submitting request identity. Admission refusal now takes precedence over tentative acceptance; the original submission is retained until the correlated answer and restored with later edits. | `TestRefusalAfterUnrelatedAcceptanceRestoresDraft`, `TestPromptRefusalOverridesUnrelatedRunStart` |
| Reconnect or replacement-session ordering can leave sending blocked or lose acceptance | Request ownership was tied only to connection generation. Still-current submission revision is reconciled, and replacement snapshots preserve pending submission state. | `TestPendingPromptSurvivesConnectionAndSessionUpdates`, `TestInteractiveSessionGone` |
| Previous prompt completion stops a queued run in the same session | Buffered completion was used to synthesize a settled lifecycle event after a new run started. Only followed lifecycle events and state now determine completion. | `TestEarlyPromptDoneDoesNotSettleNextRun` |
| Late queue, session, or cancel results modify a newer state | Missing generation or run guards. | Existing session and queue tests, plus the final package checks below |

The active-session switch was reproduced through the built `ask`, a PTY, real leader, and actual socket before repair.
The same-session early-completion test failed before the final repair.
The direct refusal test reproduced draft loss; the two-client PTY scenario passed but did not force that race.
Reviewer feedback found regressions in the first repair, which were corrected before final verification.

## Remaining acceptance gaps

1. Native Terminal.app or iTerm2 checks of S3, S6, S7, S11, and resize have no recorded result.
   Computer-use access to iTerm2 was refused by the tool for safety reasons.
   Headless terminal engines cannot certify these native terminals.
2. `AgentStart` supplies provisional early acceptance with no prompt request identity.
   Correlated refusals now preserve data, but a draft can disappear briefly before restoration.
   The strict continuous draft-retention requirement therefore remains unproven under concurrent clients.
   An authoritative early acceptance signal needs request correlation at the server/client boundary.
3. Another session applied the full-width/right-margin renderer changes during this audit.
   Those changes were preserved and are included in the working-tree checks.
   Their native terminal acceptance is still pending; they are not attributed to this audit.

## Documentation corrections

Phase 8 no longer calls itself an unexpanded outline.
Resolved duplicate-model, provider-guidance, and model-access findings are no longer marked open.
The plan keeps the native terminal and acceptance-signal gaps visible.
The approved generated design was not edited.

## Verification

- `go test ./...`: all packages passed (including `cmd/tui`, `internal/tui`, `internal/tuiclient`).
- `go vet ./...`: clean (exit code 0).
- `golangci-lint run ./...`: 0 issues.
- `python3 e2e/tui/run.py --driver ask`: 6 of 6 cases passed across Alacritty and Ghostty (`ask-idle`, `ask-selector`, `ask-resize`).

