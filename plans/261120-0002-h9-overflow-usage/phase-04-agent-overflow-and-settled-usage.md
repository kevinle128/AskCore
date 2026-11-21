---
phase: 4
title: "Agent: overflow cycle end, price stamp, settled usage"
status: pending
priority: P1
effort: "6h"
dependencies: [1, 2, 3]
---

# Phase 4: Agent: overflow cycle end, price stamp, settled usage

## Goal

The Agent ends every overflow (explicit or silent) with one distinct cycle end and no retry, stamps each attempt with the price state of its model, and puts the session totals and the context estimate on `agent_settled`.

## Context

- Attempt settlement: `closeAttempt` `internal/agent/lifecycle.go:159-184`; `openAttempt` `:150-157`, called at `internal/agent/loop_stream.go:124`. The request model is `l.cfg.Model` (`loop_stream.go:60,65`).
- `AttemptSettled` writers (all two): `closeAttempt` `internal/agent/lifecycle.go:168` and `recordFailureStep` `internal/agent/agent.go:871-887`. The second one writes an attempt that ended in a failure step and carries no usage, so it needs no price stamp (a nil usage already makes the totals incomplete).
- Turn flow: `reasonStage` `internal/agent/loop_stage.go:196-209`, `actStage` `:211-232`, `decideStage` `:260-312`, `turnState.failed` `:56-58`, `settleTurn` `lifecycle.go:67-80`, `closeCycle` `lifecycle.go:118-140`, cause words `lifecycle.go:227-239`.
- Recovery: `settleAndRecover` `internal/agent/recover.go:55-104`; overflow is not in the retry list (`internal/pipeline/recovery.go:14-18`), and stays out.
- `finishAttempt` blanks the content of an error reply (`loop_stream.go:171-183`), so a silent overflow must not become a `StopError`.
- `agent_settled` emit: `internal/agent/agent.go:788`; event type `pkg/protocol/events.go:82`; decode `pkg/protocol/codec.go:278-279`.
- Shared functions from phases 1-3: `providers.UsageOverflow`, `Model.PriceKnown`, `sessions.FoldUsage`, `sessions.EstimateContext`.

## Requirements

1. Price stamp: `openAttempt` records `l.cfg.Model.PriceKnown()` in `attemptState`; `closeAttempt` sets `AttemptSettled.CostUnknown` when the price is not known and the attempt has usage.
2. Overflow flag: `turnState` gets `overflow bool`. `reasonStage` sets it when the failure code of the reply is `CONTEXT_WINDOW_EXCEEDED`, or when `providers.UsageOverflow(l.cfg.Model, msg)` is true. This flag is the single overflow signal that H10 reads.
3. Silent overflow keeps the reply: no change to the message, the `MessageEntry` or the attempt outcome. `actStage` runs no tools when `overflow` is set.
4. `decideStage`: when `overflow` is set, the cycle settles as reason `error` with code `CONTEXT_WINDOW_EXCEEDED`, and the run ends as for a failed message (no queue claim, no continue).
5. `closeCycle`: an `error` cycle with code `CONTEXT_WINDOW_EXCEEDED` gets the new cause word `context_overflow` (a constant next to `causeInterrupted`). The ACP update projection already copies the cause (`internal/acp/updates.go:450-451`).
6. `agent_settled` carries `Usage *protocol.UsageTotals` (`json:"usage,omitempty"`): `FoldUsage` of the log, `ContextTokens` from `EstimateContext`, `ContextWindow` from the current model. It is nil when the log has no attempt. Encode and decode it in `pkg/protocol/codec.go`.
7. Do not rewrite the provider error text of an explicit overflow; the surfaces own the user text (phases 5 and 6).

## Files owned

- Modify: `internal/agent/lifecycle.go`, `internal/agent/loop_stage.go`, `internal/agent/agent.go`, `pkg/protocol/events.go`, `pkg/protocol/codec.go`, `pkg/protocol/events_test.go`, `cmd/tui/headless_test.go` (the expected `agent_settled` line at `:339` only)
- Create: `internal/agent/overflow_test.go`, `internal/agent/settled_usage_test.go`

## Tests written first

All tests run the real Agent with the faux provider (`faux.Step.Err`, `faux.Step.WithUsage`, `faux.Step.Stop`).

- `TestExplicitOverflowEndsCycleWithCause`: a reply that fails with `CONTEXT_WINDOW_EXCEEDED` gives `CycleClosed{Reason: error, Code: CONTEXT_WINDOW_EXCEEDED, Cause: context_overflow}`, no `RetryScheduled`, one attempt.
- `TestSilentOverflowKeepsReplyAndEndsCycle`: a `stop` reply with `Input+CacheRead` above the window keeps its text in the log, the attempt is `completed` with usage, the cycle ends with the overflow cause, and a queued follow-up does not start.
- `TestLengthStopAtWindowIsOverflow` and `TestLengthStopBelowWindowIsMaxTokens`.
- `TestRateLimitIsNotOverflow`: a 429 failure is retried as today and the cycle cause is empty.
- `TestAttemptStampsUnknownPrice`: a model with `CostUnknown` writes `CostUnknown: true`; a priced model writes false.
- `TestSettledUsageMatchesFold`: after two runs with a retry, `agent_settled.Usage` equals `sessions.FoldUsage` of the log plus the estimate and the model window.
- `TestEstimateAgreesWithNextRequest`: after a run and a new queued user message, the trailing messages that `EstimateContext` counts are exactly the messages that the rebuilt request of the next attempt adds after the anchor request (`rebuildRequest`, `internal/agent/request_log.go`).
- `TestResumeAfterErrorReplyKeepsLastGoodAnchor`: a preloaded log that ends with a failed attempt gives the estimate of the last completed attempt plus the tail.
- Codec: `agent_settled` with and without `usage` round-trips; the existing exact line at `pkg/protocol/events_test.go:57` stays the same.

## Steps

1. Write the tests; they fail.
2. Add the price stamp.
3. Add the overflow flag, the stage changes and the cause word.
4. Add the `agent_settled` payload and its codec.
5. Update the expected JSON line in `cmd/tui/headless_test.go:339`.
6. Run the validation.

## Validation

```
go test ./internal/agent/... ./pkg/protocol/... ./cmd/tui/...
go test -race ./internal/agent/...
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

## Risks

| Risk | L x I | Mitigation |
|---|---|---|
| The fold runs over the whole log at each settle | L x L | one linear pass of in-memory entries per run; measure with a 10 000-entry log in `settled_usage_test.go` and keep it under 10 ms |
| Silent overflow ends a run that the user wanted to go on | L x M | only stop and length replies; the reply text stays; H10 adds recovery |
| A row whose `ContextWindow` is smaller than the real window turns every large good reply into an overflow (exit 1 in `ask -p`); Pi only compacts there | M x M | `UsageOverflow` trusts the row; the user fixes the row with a `models.json` `contextWindow` override (`docs/cli.md:615-627`); listed beside plan.md default 2 |
| A strict JSON reader rejects the new field | L x M | the field is optional and omitted when nil |

## Rollback

Revert the commits. The cycle end returns to reason `error` with code only; `agent_settled` loses its optional field; logs keep the optional `costUnknown` field, which the reverted reader ignores.

## Code hygiene

No plan labels, phase numbers or decision codes in code, comments, test names or commit messages.
