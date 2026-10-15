---
phase: 8
title: "Model attempt, retry, billing pin and cancel mid-stream"
status: done
priority: P1
effort: 14h
dependencies: [phase-07]
---

# Phase 08: Model attempt, retry, billing pin and cancel mid-stream

## Goal

`ExecuteModel` covers the whole attempt. `RecoverModel` classifies a failed attempt by its typed failure (phase 07) and the driver retries inside the same turn with a new attempt, using DeepSeek's numbers and durable retry entries (Q3, resolved by D25) without jitter (D11). The JSON stream shows Pi's retry events, derived from the one durable retry record. A retry cannot change the billing class. Cancel mid-stream keeps the partial answer, and the model sees it on the next request (Q4, resolved by D25).

## Decisions (D25, DeepSeek source verified)

- **Retry policy (follow DeepSeek `packages/llm/llm/src/retry-policy.ts:14-24`).** 5 retries after the first request; delay `min(500 ms × 2^(n-1), 10 s)`; **no jitter** (D11 exception; DeepSeek uses 0.1, `packages/llm/llm-retry/src/index.ts:59-63`). Retryable codes: `EMPTY_RESPONSE`, `RATE_LIMIT`, `SERVER`, `TIMEOUT`, `TRANSPORT`. <!-- red-team #9 -->
- **Retry-After (follow DeepSeek `packages/llm/llm-retry/src/index.ts:227-235`).** A provider `Retry-After` at or below 10 s is used as the delay. Above 10 s: the normal policy declines the retry (`:230-231`) and the failure ends the turn with its own cleaned text. Ask adds no reset-time text; DeepSeek does not synthesize one either (`llm-deepseek/src/transport.ts:36-42` only stores `providerRetryAfterMs`). <!-- red-team #9 -->
- **Budget (follow DeepSeek `index.ts:79-81,128-132,219-224`).** The budget is keyed by provider and policy, starts again at each turn start and at cycle end, and the policy belongs to the provider registration, never to `LoopConfig` (DeepSeek rejects an executor-level policy, `retry.spec.ts:1050`). <!-- red-team #9 -->
- **Policy capture (follow DeepSeek, source audit finding 5).** `Registry.Prepare` (phase 07) copies the serving registration's policy into `Prepared.RetryPolicy`. `RecoverModel` gets the failure together with that captured policy and the serving provider, as DeepSeek passes `preparedCall.retryPolicy` with the failure (`packages/core/agent-loop/src/agent.ts:494-501`; `packages/llm/llm/src/index.ts:930-936`). There is no policy lookup after the failure. A changed policy key starts a new budget (retry counter 1; `retry.spec.ts:600,649`). Live provider replacement is N/A (matrix SL10b, SB10, SL32), because the registry is fixed at start; the capture rule still decides which policy applies when `PrepareRequest` routes attempts to different providers.
- **Durable retry records (follow DeepSeek `packages/llm/llm-retry/src/types.ts:6-12`, `index.ts:188-190`).** `RetryScheduled{retryId, cycleId, turn, provider, policyKey, retry, maxRetries, delayMs, failure}` is written before the wait; `RetryStarted{retryId, retry}` after it.
- **Retry after visible output (follow DeepSeek `agent.ts:487-509`, `transport-recovery.spec.ts:127-162`).** A failed attempt is retried even when text, thinking or tool-call frames were already published. The failed attempt is log-only: `AttemptSettled{outcome: failed}` holds its cleaned failure and usage; its message is not committed and runs no tools. Pi does the same (`C:core/agent-session.ts:1169-1185` decides by stop reason only). Roadmap H9's line "mid-stream public output cannot be transparently replayed" is updated in phase 11: the retry is visible on the wire, not transparent.
- **Each attempt re-runs `PrepareRequest` and the provider `Prepare`** from committed history and freezes a new request (DeepSeek `agent.ts:408`, `docs/architecture.md:113`: "Retries do not repeat assembly or `agent/pre-step`"). `AdmitStep` does not run again; input is not committed again. <!-- red-team #5 -->
- **Attempt boundary (phase 01, unchanged).** The `ExecuteModel` terminal calls the stream function; the attempt is live, and `attempt_start` is published, only after the stream returned and `ctx.Err() == nil`. A `PrepareRequest`, `Prepare` or `ExecuteModel` handler failure before that point publishes no attempt event and is not retried (matrix SA5, row 30, A6).
- **Pi wire projection of one retry (JSON exception, single source).** For a failed attempt that will retry, in this order: `message_end` of the failed attempt's assistant message with `stopReason: "error"` and the cleaned `errorMessage` (preceded by `message_start` if it was not published yet), `attempt_end{outcome: "failed"}`, `turn_end`, `agent_end{willRetry: true}`, `auto_retry_start{attempt, maxAttempts, delayMs, errorMessage}`. After the wait: `agent_start`, `turn_start`, `attempt_start`, … The cycle stays open (no `cycle_end`/`cycle_start`). After the first successful assistant `message_end`: `auto_retry_end{success: true, attempt}`. When the series ends in failure: `auto_retry_end{success: false, attempt, finalError}`. `agent_end.willRetry` is always present (`false` when no retry follows). Pi evidence: `C:core/agent-session.ts:215-216,1112,1145-1153,1829,3729-3735`. Every `auto_retry_*` event is built in one function from the `RetryScheduled`/`RetryStarted`/`AttemptSettled` entries. <!-- red-team #5, #9 -->
- **Billing pin (red-team #8).** Binding stays in `AuthRunner` (`internal/app/module_auth.go:14-42`); a retry is a new `Stream` call, so it binds again there. Phase 07 already puts the safe projection `providers.AuthBinding{Provider, Method, Profile, BillingHint}` (no token, no account ID) on the returned stream (`Stream.Binding()`). The driver keeps attempt 1's binding for the retry series and passes it as `StreamOptions.RequireBinding`. `AuthRunner` resolves as usual (a token refresh is allowed) and fails with `auth.ErrBindingChanged` (code `AUTH`, not retryable) when `Method`, `Profile` or `BillingHint` differ. <!-- red-team #8 -->
- **Credential failure contract (unchanged).** A credential failure happens inside the stream call, after admission, and stays an assistant error message (`TestTokenPlanMissingKeyIsAnAssistantError`, `cmd/tui/headless_test.go:300`; `internal/app/module_auth_test.go:233-240`). <!-- red-team #4 -->
- **Cancel mid-stream (follow DeepSeek `cancel.spec.ts:571-605,664`, `packages/llm/llm/src/assembler.ts:163-180`).** The interrupted message is committed with `StopAborted`, keeps text and thinking with non-blank content, and drops all tool-call blocks. The next request sends it to the model: `internal/providers/transform.go:313` stops skipping an aborted message that has content. A thinking block without a signature in an aborted message is sent as plain text, because Anthropic rejects unsigned thinking (`transform.go:232-249` keeps unsigned same-model thinking as thinking today). Error messages and the empty D20 wrapper message stay out of replay.
- **Cancel before visible content (diverge, D20 + JSON).** Ask keeps the aborted wrapper message (DeepSeek commits nothing, `cancel.spec.ts:775`).
- **Retry clock (product seam).** `Config.Wait func(ctx context.Context, d time.Duration) error` (nil = a real timer with `select` on `ctx`). Tests pass a recording `Wait` and assert the production delays; no production value is overridden. `cmd/tui` `options` gets the same field, next to `transport`. <!-- red-team #13 -->

## Input commit timing (D26, follow DeepSeek)

Move the commit of admitted input (prompt, steering, follow-up) from cycle admission to right after the first attempt's `PrepareRequest` and provider `Prepare` succeed (DeepSeek `agent.ts:408-423`: `prepareRequest` includes the prepared call, and the user message is appended after it; `contract-regressions.spec.ts:1336,1431`, `system-prompt-admission.spec.ts:372`).
- On a `PrepareRequest` or `Prepare` error or panic: commit nothing. The Agent wrapper publishes the D20 error events (message_start, message_end, turn_end, agent_end) but does not save the error message. `Prompt` returns the error. History is unchanged, so a later `Prompt` or `Continue` runs normally.
- On a cancel during preparation: commit nothing, send no request, end the cycle `aborted`.
- A retried attempt runs `PrepareRequest` again but never commits the input a second time.
- A missing or invalid credential fails inside the stream call, after the commit, so it keeps today's saved assistant error (`TestTokenPlanMissingKeyIsAnAssistantError` does not change).
- Queue claims (phase 06) are acknowledged only after this commit; on a preparation failure the claimed input is reported as failed, not re-offered.
- Tests: `TestPrepareFailureCommitsNothing`, `TestAbortDuringRequestPreparationSendsNoRequest`, `TestDisposeDuringRequestPreparationSendsNoRequest` (D29; no `RequestDelta` entry), `TestRetryDoesNotRecommitUserInput`, `TestPromptAfterRequestPreparationErrorRunsNormally` (matrix SB4: D20 events published, no message saved), and the JSON test `TestJSONPrepareFailureHasNoUserMessageBeforeError` (asserts the one deliberate difference from the Pi stream).

## Context links

- Current stream path: `internal/agent/loop_stream.go:13-75` (`MessageStart` on the provider start event, :44-47)
- Run-failure path: `internal/agent/agent.go:232-295`
- DeepSeek driver: `packages/core/agent-loop/src/agent.ts:400-540`; retry plugin `packages/llm/llm-retry/src/index.ts:120-242`

## Files to Create / Modify

- Create: `internal/agent/attempt.go` (attempt terminal, settle), `internal/agent/recover.go` (classification, budget, backoff), `internal/agent/retry_events.go` (Pi projection), tests `attempt_test.go`, `recover_test.go`, `retry_events_test.go`
- Modify: `internal/agent/loop_stream.go`, `loop_stage.go`, `agent.go` (`agent_settled` after the final attempt, D18), `types.go` (`Wait`)
- Modify: `internal/providers/registry.go` (policy per registration), `prepare.go` (`Prepared.RetryPolicy`), `types.go` (`StreamOptions.RequireBinding`), `transform.go` (aborted replay)
- Modify: `internal/app/module_auth.go` (binding projection, pin check), `internal/auth/service.go` (`ErrBindingChanged`)
- Modify: `pkg/protocol/events.go`, codecs, README (`auto_retry_start`, `auto_retry_end`, `agent_end.willRetry`)
- Modify: `cmd/tui/headless.go` (`options.wait`)
- Modify tests (deliberate): <!-- red-team #13 -->
  - `cmd/tui/headless_fault_test.go`: `TestFaultHTTPStatusIsOneRequestAndExit1` (:49) becomes `TestFaultHTTPStatusRetriesFiveTimesThenExits1` (6 requests, delays 0.5/1/2/4/8 s); `TestFaultConnectionResetMidStream` (:106) expects 6 requests; `TestFaultStreamEndsWithoutMessageStop` (:79) asserts 1 request (`STREAM_CLOSED`); `TestFaultSIGINTMidStreamExits130` unchanged.
  - `cmd/tui/headless_test.go`: `TestJSONHelloLines` (`agent_end` gains `"willRetry":false`), `TestJSONSmallReplyWaitsForFinalFlush` if its byte count changes.
  - `internal/providers/transform_test.go`: aborted-message replay cases.

## Tasks & Steps

1. `ExecuteModel` terminal: call `Stream`; open the attempt only when it returned and `ctx.Err() == nil` (phase 01 boundary); consume all frames, drain the channel, return the outcome. A middleware timeout covers generation.
2. Settle every attempt with `AttemptSettled` (usage nil = unknown, never zero).
3. `RecoverModel` default: apply the captured policy above. Cancellation wins over a retry decision. A handler cannot start an attempt. A handler error ends the cycle `error` with no retry (matrix SB8).
4. Retry loop inside the turn: new attempt ID, `PrepareRequest` and `Prepare` again, new frozen request and `RequestDelta`, billing pin, no new admission. `Dispose` during the backoff wait stops the timer and starts no attempt.
5. Wire projection in `retry_events.go`, as specified.
6. Cancel mid-stream and replay rule, as specified.
7. Move `agent_settled` after the final attempt and queued work (D18).
8. Extend `TestCredentialCanariesNeverReachEntriesEventsOrRebuild` with a retry that fails twice with a canary in the response body.

## Tests

The phase owns every matrix row with Phase `08`; run `check-conformance-matrix.sh 08`.

- `TestRateLimitTwiceThenSuccessIsOneCycleThreeAttempts` (H9 exit through faux), `TestRetryKeepsCycleWithNewAttemptID`, `TestRetryDoesNotRecommitUserInput`, `TestRetryRunsNoToolsFromFailedAttempt`, `TestRetryRerunsPrepareRequestButNotAdmission`.
- `TestRetryDelaysFollowPolicyWithoutJitter`, `TestRetryStopsAfterFiveRetries`, `TestRetryAfterUnderCapIsHonored`, `TestRetryAfterAboveCapIsNotRetried` (one request; the failure text is the provider's cleaned text with no added reset time), `TestRetryBudgetIsPerProvider`, `TestRetryBudgetResetsAtTurnStart`, `TestRetryEntriesAreWrittenBeforeAndAfterWait`, `TestRetryEntryIsLogOnlyAndAddsNoMessage` (matrix SL15).
- Policy capture (matrix SL10): `TestRetryUsesPolicyCapturedAtPrepare` (a `PrepareRequest` handler routes attempt 1 to provider B and attempt 2 to provider A, Agent model A: the first wait uses B's policy delay; when attempt 2 fails, the wait uses A's delay and the counter is 1; counterexample: a policy looked up from the Agent's model gives A's delay for the first wait).
- `TestRetryAfterVisibleOutputClosesFailedMessageOnWire` (JSON: `message_end` error, `turn_end`, `agent_end{willRetry:true}`, `auto_retry_start`, then the retried answer), `TestAutoRetryEventsAgreeWithRetryEntries`, `TestCancelDuringBackoffEndsWithAutoRetryEndFailure`.
- `TestEmptyResponseIsRetriedAndNeverCommitted`, `TestCleanPrematureEndIsNotRetried`, `TestQuotaIsNotRetried`, `TestMiddlewareErrorIsNotRetried`, `TestCancelDuringBackoffEndsWithoutAnotherAttempt`, `TestDisposeDuringBackoffStartsNoAttempt`, `TestRetryDecisionAfterAbortIsIgnored`, `TestRecoveryHandlerFailureEndsCycleWithoutRetry` (matrix SB8; counterexample: the same failure with no handler gives two requests).
- Billing: `TestRetryNeverSwitchesSubscriptionToApiKey`, `TestRetryFailsWhenBillingClassChangedDuringBackoff`, `TestRetryRebindsSameMethodAfterTokenRefresh`, `TestBindingProjectionHoldsNoToken`. <!-- red-team #8 -->
- `TestFailedAttemptUsageCountedOnce`, `TestMissingUsageIsUnknownNotZero`, `TestModelMiddlewareTimeoutCoversStreamConsumption`.
- Cancel: `TestCancelMidStreamKeepsTextAndDropsUnfinishedToolCall`, `TestInterruptedMessageIsSentToModelOnNextRequest`, `TestInterruptedUnsignedThinkingIsReplayedAsText`, `TestCancelBeforeAnyContentCommitsAbortedMessage`, `TestCancelDuringReasoningKeepsThinkingBlockOnly`.
- Headless: `TestFaultHTTP429ThenSuccessRetriesAndExits0`, the updated fault tests above, `TestTokenPlanMissingKeyIsAnAssistantError` unchanged.
- goleak: backoff timers and stream producers after cancel.

```sh
go test ./internal/agent/... ./internal/providers/... ./internal/app/... ./internal/auth/...
go test -run 'Fault|JSON' ./cmd/tui
go test -race ./internal/agent/... ./internal/providers/... ./cmd/tui/...
go test ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
bash plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh 08
```

## DeepSeek conformance rows covered

See the matrix Phase column (rows with Phase `08`).

## Risks & rollback

- More requests per failure, each possibly billed (Med x Med). Mitigation: DeepSeek retryable set only; `QUOTA` and `AUTH` never retry; over-cap `Retry-After` never retries; billing pin.
- JSON readers see two `agent_end` records in one prompt (Low x Med). Mitigation: this is Pi's own sequence (`willRetry: true`); `agent_settled` still comes once, last (D18).
- Anthropic rejects a replayed interrupted message (Med x Med). Mitigation: unsigned thinking replayed as text; replay matrix test per provider.
- Rollback: reset to tag `lifecycle-p08-base`.

## Done criteria

- The H9 retry exit test passes through faux and `TestFaultHTTP429ThenSuccessRetriesAndExits0` passes through the real Anthropic adapter.
- `auto_retry_*` always agrees with the retry entries; `agent_settled` comes after retries; no billing class change on retry.
- Green tests, race, lint, goleak and the phase-08 matrix check.
