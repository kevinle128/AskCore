---
phase: 7
title: "Typed provider failures and the provider Prepare step"
status: done
priority: P1
effort: 13h
dependencies: [phase-06]
---

# Phase 07: Typed provider failures and the provider Prepare step

## Goal

Two provider-side changes, both before retry:

1. Every adapter maps a failed request to one typed failure with a DeepSeek code, the HTTP status and the provider `Retry-After`, so that the retry phase classifies real provider paths and not only faux. Classification only; nothing retries yet, so every request count stays the same. <!-- red-team #1 -->
2. **D28.** Each adapter computes its safe effective request values once, in a `Prepare` step. The Agent logs that result, and `Stream` sends exactly those values. The log then rebuilds the exact wire body, and the tests compare it with the body the real adapter sends.

## Context links

- Today only the OpenAI Responses path produces sentinels: `internal/providers/openai/profile_errors.go:15-55`, called from `responses.go:145,150,155,161`. Anthropic and Completions go through `fantasykit.Fail` with no sentinel (`internal/providers/fantasykit/errors.go:21-38`). No code reads `Retry-After`. <!-- red-team #1 -->
- `fantasy.ProviderError` carries `StatusCode`, `ResponseHeaders`, `ResponseBody`, `URL` (fantasy module `errors.go:34-54`), so the adapter can read `Retry-After` without leaking fantasy types (D22).
- Idle timeout: already present on every path, 5 minutes (`internal/providers/anthropic/provider.go:101,219`, `openai/options.go:51`, `openai/completions.go:113`, `openai/responses.go:124`; added by commit `e991b5c`). Only the classification as `TIMEOUT` is new. DeepSeek default is the same 300 s (`packages/llm/llm-deepseek/src/defaults.ts:4`).
- Clean end vs cut: `fantasykit/errors.go:31-33` folds `io.ErrUnexpectedEOF` and a missing terminal event into one `ErrStreamIncomplete`.
- Effective values computed inside adapters today: `clampMaxTokens` (`internal/providers/anthropic/document.go:130`), thinking effort mapping (`document.go:113-128`); the clamp uses an input-size estimate, so `prepare` takes the request, and binding-dependent shaping for Anthropic OAuth: a system prefix and tool-name mapping (`document.go:22-50`), request profile headers (`anthropic/profile.go:17-20`).
- Binding: `internal/app/module_auth.go:14-42` (`AuthRunner` binds inside the stream function, after the D26 commit point).
- DeepSeek: codes `packages/llm/llm-deepseek/src/transport.ts:22-44` (AUTH, QUOTA, RATE_LIMIT, CONTEXT_WINDOW_EXCEEDED, INVALID_REQUEST, SERVER, `HTTP_<status>` at :35, `Retry-After` in ms at :36-37), `adapter.ts:63,66` (TIMEOUT, TRANSPORT), `translate.ts:149,165` (EMPTY_RESPONSE, STREAM_CLOSED), `packages/llm/llm/src/adapter-failure.ts:19,106` (`UNKNOWN` for a failure with no provider facts), `transport-recovery.spec.ts:127-214`; prepared call `packages/llm/llm/src/index.ts:889-891,930-936`, adapter defaults logged `packages/core/agent-loop/src/agent.ts:587,609-613`.

## Decisions

- **Codes (follow DeepSeek).** `RATE_LIMIT` (429), `SERVER` (5xx, overloaded, and an in-band provider error of an unknown type), `TIMEOUT` (idle timeout), `TRANSPORT` (connection reset, refused, `io.ErrUnexpectedEOF` mid-body), `EMPTY_RESPONSE` (a `stop` completion with no content block and no tool call), `STREAM_CLOSED` (the body ended cleanly with no terminal event), `QUOTA` (exhausted allowance or balance, 402, provider quota codes; maps `ErrAllowanceExhausted`), `AUTH` (401/403, maps `ErrAuthentication`), `INVALID_REQUEST` (400/413), `CONTEXT_WINDOW_EXCEEDED`, `HTTP_<status>` (any other HTTP status, for example `HTTP_418`), `UNKNOWN` (a Go error with no provider facts, for example a faux `fail` step without a typed failure). There is no unconditional `UNKNOWN` fallback for provider responses, because it would change retry eligibility (source audit, row F1). <!-- red-team #11 -->
- **Prepare boundary (D28).** `providers.Registry.Prepare(ctx, model, request, opts) (*Prepared, error)` calls the adapter's `prepare`. `Prepared` holds the safe effective values: provider, API, model ID, endpoint (scheme, host and path; no query, no userinfo), effective max tokens (after the clamp), sampling values, thinking level and effort, tool choice, cache retention, the names of every header, and the values of allowlisted non-secret headers (`anthropic-version`, `anthropic-beta`, `content-type`, `user-agent`, `x-app`). Phase 08 adds the retry policy of the serving registration. `StreamOptions.Prepared` carries it into `Stream`, and the adapter reads effective values only from it. The adapter's `encode(prepared, request, binding)` builds the body; `Stream` and `Registry.Encode` both call it, so the Agent never repeats an adapter calculation.
- **Binding-dependent shaping.** Binding stays inside the stream function, after the D26 commit point, so a missing credential still gives a saved assistant error (D26). The Anthropic OAuth shaping is therefore a pure function of `(Prepared, AuthBinding.Method)` inside `encode`. `AuthRunner` puts the safe projection `providers.AuthBinding{Provider, Method, Profile, BillingHint}` (no token, no account ID) on the returned stream (`Stream.Binding()`); the Agent writes it to `AttemptSettled`. Phase 08 uses the same projection for the billing pin.
- **Not logged (named, D23/D28).** API key, OAuth access token, account ID, and the values of `Authorization`, `x-api-key`, `Cookie`, account headers and every header that is not on the allowlist (only its name is logged). Everything else needed by `encode` is logged.
- **Ordering.** The driver calls `Prepare` after `PrepareRequest` and the freeze, before the stream call. A `Prepare` error is a request-preparation failure: no attempt event (phase 01 boundary), D20 error events; phase 08 makes it commit nothing (D26).

## Files to Create / Modify

- Create: `internal/providers/failure.go` (`Failure{Code, Status, RetryAfter time.Duration, Message}`, code constants, `Unwrap` to the existing sentinel), `failure_test.go`
- Create: `internal/providers/prepare.go` (`Prepared`, `Registry.Prepare`, `Registry.Encode`), `prepare_test.go`
- Modify: `internal/providers/errors.go` (doc: sentinels stay; `Failure` wraps them), `fantasykit/errors.go` (`Fail` builds a `Failure`), `anthropic/provider.go`, `anthropic/document.go` (`prepare`, `encode`), `openai/completions.go`, `openai/completions_body.go`, `openai/responses.go`, `openai/profile_errors.go` (map to codes; `prepare`, `encode`), `faux/script.go` (`Step.Err(*providers.Failure)`), `types.go` (`StreamOptions.Prepared`), `stream.go` (`Binding()`), `auth.go` (`AuthBinding`)
- Modify: `internal/app/module_auth.go` (`authFailure` gives code `AUTH`; `AuthRunner` sets the binding projection)
- Modify: `internal/agent/loop_stream.go` (call `Prepare`), `internal/agent/request_log.go` (`RequestDelta` gets the `Prepared` safe fields; `AttemptSettled` gets `AuthBinding`; `rebuildRequest` returns them), `internal/sessions/entry.go`
- Create tests: `internal/providers/anthropic/failure_test.go`, `internal/providers/openai/failure_test.go`, `internal/providers/faux/faux_test.go` (new case), `cmd/tui/headless_request_log_test.go` (exact-body tests through `newHeadlessAgent` with an injected `httptest` transport)

## Tasks & Steps

1. Codes, as in "Decisions".
2. **Retry-After.** Read `Retry-After` from the response headers: seconds or HTTP date; keep it only when it is finite and above zero.
3. **Split the stream-end cases.** A body that ends with `io.EOF` before the terminal event is `STREAM_CLOSED` and keeps the text "stream ended without a terminal event". A read error (`io.ErrUnexpectedEOF`, `ECONNRESET`) is `TRANSPORT`. <!-- red-team #11 -->
4. **Empty response.** The adapter turns a content-less `stop` completion into an `EMPTY_RESPONSE` failure instead of an empty success message. A content-less `length` completion stays a success (phase 01 rule). The retry of it is phase 08 (matrix SL14b).
5. **Usage.** The final message usage replaces the last streamed sample; when the final message has no usage, the last sample stays (matrix SL18).
6. **Faux.** `Step.Err(f *providers.Failure)` lets a script return a typed failure through `Stream.Result`. A faux `fail` step without a typed failure stays `UNKNOWN` (not retryable in phase 08).
7. Error text of a `Failure` goes through `CleanDiagnostic` (phase 04).
8. **Prepare (D28).** Implement `prepare` and `encode` per adapter as in "Decisions"; remove the inline effective-value calculations from the stream paths so that `Stream` uses `opts.Prepared` only.
9. **Binding projection.** `AuthBinding` and `Stream.Binding()`; `AuthRunner` sets it; the Agent writes it to `AttemptSettled`.
10. **Log and rebuild.** `RequestDelta` stores the `Prepared` safe fields; `rebuildRequest` returns `{logical request, Prepared, AuthBinding}`; `Registry.Encode` of that result gives the body.

## Tests

The phase owns every matrix row with Phase `07`; run `check-conformance-matrix.sh 07`. Adapter tests use real HTTP bodies through an injected transport (the `cmd/tui/headless_fault_test.go:29-37` pattern), never a faux sentinel. <!-- red-team #1 -->

- Anthropic: `TestAnthropic429WithRetryAfterIsRateLimit`, `TestAnthropic529OverloadedIsServer`, `TestAnthropic500IsServer`, `TestAnthropicUsageLimitIsQuota`, `TestAnthropicResetMidBodyIsTransport`, `TestAnthropicCleanEOFIsStreamClosed`, `TestAnthropicContentlessStopIsEmptyResponse`.
- OpenAI Completions: `TestCompletions429IsRateLimit`, `TestCompletions503IsServer`, `TestCompletionsStalledBodyIsTimeout`, `TestCompletionsResetMidBodyIsTransport`.
- OpenAI Responses: `TestResponsesAllowanceExhaustedIsQuota`, `TestResponsesRateLimitKeepsRetryAfter`.
- Fallbacks: `TestUnmappedHTTPStatusIsHTTPStatusCode` (418 gives `HTTP_418`; counterexample: `UNKNOWN` fails), `TestUnknownInBandErrorIsServer`.
- `TestRetryAfterSecondsAndHTTPDateAreParsed`, `TestFailureUnwrapsToSentinel`, `TestFauxStepErrReturnsTypedFailure`, `TestAuthFailureHasAuthCode`, `TestFinalUsageReplacesLastSampleAndMissingFinalKeepsSample`.
- Prepare and rebuild (matrix 25b, SB9): `TestRebuiltRequestEqualsAdapterBodyWithDefaults` (per adapter, `Options.MaxTokens == 0`: the `httptest` body equals `Registry.Encode(rebuildRequest(...))` byte for byte, and the logged max tokens is the clamped nonzero value; counterexample: logging `Options.MaxTokens` fails), `TestRebuiltRequestEqualsAdapterBodyAfterModelSwitch` (a `PrepareRequest` switch from Anthropic to OpenAI Completions), `TestRebuiltRequestEqualsAdapterBodyForOAuthBinding` (Anthropic subscription binding: the system prefix and mapped tool names are rebuilt from the logged `AuthBinding`), `TestRequestLogRecordsPreparedMaxTokensAndThinking`, `TestStreamUsesPreparedValuesOnly` (a `Prepared` with max tokens 123 gives 123 in the body).
- Canary: `TestCredentialCanariesNeverReachEntriesEventsOrRebuild` is extended with a canary in one non-allowlisted `Model.Headers` value and in the bound access token: neither is in `RequestDelta`, `AttemptSettled` or the rebuild.
- Unchanged on purpose (one request each, same text): `TestFaultHTTPStatusIsOneRequestAndExit1`, `TestFaultStreamEndsWithoutMessageStop`, `TestFaultBadJSONEvent`, `TestFaultConnectionResetMidStream`. The retry phase changes the first and the last.
- Cassette tests: request bodies do not change (the refactor moves calculations, it does not change values).

```sh
go test ./internal/providers/... ./internal/app/... ./internal/agent/...
go test -run 'Fault|Cassette|RequestLog|Rebuilt' ./cmd/tui
go test -race ./internal/providers/... ./internal/agent/...
go test ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
bash plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh 07
```

## DeepSeek conformance rows covered

See the matrix Phase column (rows with Phase `07`).

## Risks & rollback

- A provider sends an unusual quota body that maps to `RATE_LIMIT`, so phase 08 retries a billed quota error (Low x High). Mitigation: quota and rate-limit tests per adapter with recorded bodies; `QUOTA` is never retryable.
- A content-less `stop` was a success before (Low x Med). Mitigation: phase 08 retries it; until then it is an error message, which is the DeepSeek outcome without the retry.
- The `Prepare` refactor changes a request body (Med x High). Mitigation: cassette replay compares bodies; `TestStreamUsesPreparedValuesOnly`; exact-body tests per adapter.
- A secret header value reaches the log (Low x High). Mitigation: header values are logged only from the allowlist; the canary test covers a `Model.Headers` value.
- Rollback: reset to tag `lifecycle-p07-base`.

## Done criteria

- Every adapter path returns a `*providers.Failure` for a failed request; `errors.As` finds it from `Stream.Result`; no provider response maps to `UNKNOWN`.
- Every adapter has `prepare` and `encode`; `Stream` computes no effective value itself; the exact-body tests pass for defaults, a model switch and the OAuth binding.
- Fault tests keep one request each; cassettes unchanged; green tests, race, lint and the phase-07 matrix check.
