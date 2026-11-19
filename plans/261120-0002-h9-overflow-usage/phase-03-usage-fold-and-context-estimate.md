---
phase: 3
title: "Usage fold and context estimate"
status: pending
priority: P1
effort: "5h"
dependencies: []
---

# Phase 3: Usage fold and context estimate

## Goal

One pure fold over the session log gives the attempt rows and the totals, and one pure function gives the context estimate. Every reader uses these two functions.

## Context

- `AttemptSettled` (`internal/sessions/entry.go:61-72`) is the single source of usage. The assistant message usage is display only.
- The current fold is private to ACP: `internal/acp/ask_methods.go:497-549` (`usageResult`, `addUsage`, `addOptional`). Phase 5 replaces it with the shared fold.
- `attempt_settled` is a `plainKind`, so an added field is readable by older builds: `internal/sessions/codec.go:109-125,214-232`.
- `MessagesOf` applies context edits: `internal/sessions/entry.go:335-360`.
- `sessions` owns projections and may import only `pkg/protocol`, snowflake and the standard library (`internal/sessions/README.md`).
- Rules: [design.md "Usage fold rules" and "Context estimate rules"](design.md#usage-fold-rules).

## Requirements

1. `pkg/protocol/usage.go`: add `UsageTotals` (`total`, `complete`, `costComplete`, `contextTokens`, `contextWindow`) as in [design.md](design.md#interfaces-names-may-change-in-review).
2. `internal/sessions/entry.go`: add `CostUnknown bool \`json:"costUnknown,omitempty"\`` to `AttemptSettled`, with a doc comment: true when the model of the request had no known price. Update the `CycleClosed.Cause` doc comment: the cause is set for `aborted`, and for an `error` cycle that ended in a context overflow (word `context_overflow`).
3. `internal/sessions/usage.go`: `FoldUsage(entries) ([]AttemptUsage, protocol.UsageTotals)`. Move the add logic from `ask_methods.go` here (the ACP copy is deleted in phase 5). It leaves `ContextTokens` and `ContextWindow` at 0.
4. `internal/sessions/context_estimate.go`: `EstimateContext(entries) (int64, bool)` with the anchor, tail, image and fallback rules of design.md.
5. No new entry type, no wire word change, `FormatVersion` stays 1.

## Files owned

- Modify: `pkg/protocol/usage.go`, `internal/sessions/entry.go`, `internal/sessions/codec_test.go`
- Not here: `internal/sessions/README.md` (phase 7 adds the owner rows)
- Create: `internal/sessions/usage.go`, `internal/sessions/usage_test.go`, `internal/sessions/context_estimate.go`, `internal/sessions/context_estimate_test.go`

## Tests written first

- `TestFoldUsageAddsRetriedAttempts`: a failed attempt with usage, a retry, a completed attempt; total is the sum; two rows.
- `TestFoldUsageMissingUsageIsIncomplete`: one attempt with nil usage gives `Complete` and `CostComplete` false; the known tokens still add.
- `TestFoldUsageRefusedAttemptCountsAsZero`: a failed attempt that streamed no output and has nil usage (an HTTP 429 or 500 before any stream) adds zero and keeps `Complete` and `CostComplete` true. Decide "streamed no output" from the log (no assistant output recorded for that attempt); if the log cannot tell, add the smallest field that records it and note it here.
- `TestFoldUsageUnknownPriceIsCostIncomplete`: `CostUnknown` on one attempt gives `Complete` true and `CostComplete` false.
- `TestFoldUsageIgnoresMessageUsage`: an assistant `MessageEntry` with usage does not change the total.
- `TestFoldUsageAcrossCycles`: rows carry the cycle id of their cycle.
- `TestEstimateUsesLastCompletedAttemptPlusTail`, `TestEstimateSkipsFailedAndAbortedAttempts`, `TestEstimateDropsAnchorBeforeContextEdit`, `TestEstimateWithoutAnchorUsesSnapshotAndMessages`, `TestEstimateCountsImageAsFixedSize`, `TestEstimateEmptyLog`.
- Codec: add `CostUnknown: true` to the `AttemptSettled` sample in `sampleEntries` (`codec_test.go:55`); `TestAttemptSettledWithoutCostUnknownDecodes` decodes a line in the old shape.

## Steps

1. Add `UsageTotals` and the entry field.
2. Write the tests; they fail.
3. Write `FoldUsage` and `EstimateContext`.
4. Run the validation.

## Validation

```
go test ./internal/sessions/... ./pkg/protocol/...
go vet ./internal/sessions/... ./pkg/protocol/...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./internal/sessions/... ./pkg/protocol/...
```

## Risks

| Risk | L x I | Mitigation |
|---|---|---|
| chars/4 is far from the real count for CJK text and JSON | M x L | the anchor limits the error to the tail; documented limit (user decision: no tokenizer) |
| The estimate and the ACP fold diverge later | L x M | both live in `sessions`; ACP, agent and TUI read them, never a copy |
| A `SystemSnapshot` change after the anchor is not re-estimated | M x L | documented limit; H10 may refine it |

## Rollback

Revert the commits. The field is optional, so a log written with it stays readable by the reverted build.

## Code hygiene

No plan labels, phase numbers or decision codes in code, comments, test names or commit messages.
