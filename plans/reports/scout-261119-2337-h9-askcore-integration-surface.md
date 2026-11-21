# H9 integration surface in AskCore (overflow detection and usage accounting)

Date: 2026-11-19. Branch `task/h9-plan` at `7f1b4a0` (latest `develop`). Read-only scout. Every claim cites `file:line`.

## 1. Provider failure classification

- Codes: `internal/providers/failure.go:13-35` has AUTH, QUOTA, RATE_LIMIT, CONTEXT_WINDOW_EXCEEDED (line 21), INVALID_REQUEST, SERVER, TIMEOUT, TRANSPORT, EMPTY_RESPONSE, STREAM_CLOSED, MALFORMED_RESPONSE, MODEL_CONFIG, UNKNOWN; an unmapped status gives `HTTP_<status>` (line 39). Sentinels: `internal/providers/errors.go:24-39`.
- `ClassifyProvider` (`failure.go:180-199`) order: auth (401/403, `authentication_error`, `permission_error`), quota (402, subscription code, quota text), rate limit (429), context window (line 191), invalid request (400/413), server (5xx, `api_error`, `overloaded_error`, status 0). Quota and context use regex lists after the classifier.
- `internal/providers/fantasykit/errors.go`: `classifyProviderError` (~line 89) sets CONTEXT_WINDOW at line 103 when the SDK flag `pe.ContextTooLargeErr` is true, then calls `ClassifyProvider` (line 109). The fantasy fork sets that flag for Anthropic (`providers/anthropic/error.go:140-146`) and OpenAI (`providers/openai/error.go:85-98`).
- Stop length: Anthropic `max_tokens` → `StopLength` (`internal/providers/anthropic/stop.go:18`); OpenAI Completions `length` (`openai/stop.go:20`), Responses `length`/`max_tokens`/`max_output_tokens` (line 34). `internal/agent/loop_stream.go:227-228` drops tool calls on `StopLength`; `lifecycle.go:215-224` maps it to `ReasonMaxTokens`; `loop_stage.go:310` does not continue; ACP maps to `StopReasonMaxTokens` (`internal/acp/agent.go:428-429`).
- No xAI-specific mapper was found (it appears to use the OpenAI Responses path; not verified). Faux scripts the stop reason (`faux/script.go:44,89`) and does not classify errors.
- **Overflow today is classified but not acted on.** CONTEXT_WINDOW_EXCEEDED is in no retry list (`internal/pipeline/recovery.go:15`) and is used nowhere outside `failure.go` and `fantasykit/errors.go`. It ends the cycle as an assistant error.

## 2. Retry

- Policy `internal/providers/retry.go:6-15`: 5 retries, base 500 ms, max 10 s, captured per registration (`registry.go:25-38`, `prepare.go:257-312`).
- Decision `internal/pipeline/recovery.go:9-33`: retries only EMPTY_RESPONSE, RATE_LIMIT, SERVER, TIMEOUT, TRANSPORT; `Retry-After` above MaxDelay stops; doubling delay; no jitter (roadmap D11).
- Budget resets per model answer (`internal/agent/recover.go:12-16`, `:55-58`), not per cycle.
- Order: `RetryScheduled` before the wait (`recover.go:86-88`), `RetryStarted` after (`:27-31`), `AttemptSettled` per attempt (`lifecycle.go:162-183`). Entry shapes in `internal/sessions/entry.go` (`Failure` :56, `AttemptSettled` :63, `RetryScheduled` :83, `RetryPolicy` :113, `RequestDelta` :171). Failure text capped at 512 bytes.
- Wire: `internal/agent/retry_events.go:10-49` projects durable records to TurnEnd, AgentEnd(WillRetry), `AutoRetryStart`, AgentStart, TurnStart, `AutoRetryEnd` (`pkg/protocol/events.go:9-10,63-64`).
- Billing pin: `recover.go:16`, `loop_stream.go:27,112-114`; `AttemptSettled.Binding` (`entry.go:63-70`).
- Tests: `internal/agent/recover_test.go` (21, 79, 91, 97), `internal/sessions/retry_test.go:12`.

## 3. Usage today

- `pkg/protocol/usage.go:8-19`: Input, Output, CacheRead, CacheWrite, CacheWrite1h (ptr), Reasoning (ptr), TotalTokens, Cost; nil = not reported. `Cost` (:22-28) micro-USD: Input, Output, CacheRead, CacheWrite, Total.
- `internal/providers/cost.go:21-57` `NormalizeUsage` (input = input − cached − cache write); `CalculateCost` :72-99; `pickTier` :102-112 returns zero price when no tier matches (silent zero cost).
- Adapters: `openai/usage.go:12-30`, `openai/completions.go:137`, `fantasykit/fold.go:37-38,271-274`, `faux/usage.go:18`.
- Attachment: assistant message `Usage` (`pkg/protocol/message.go:528`); `AttemptSettled.Usage` only when reported (`lifecycle.go:189-192`).
- Prices: `internal/providers/model.go:18-35,51,66` (`Price` micro-USD per 1M, `Tier`, `Cost`, `CostUnknown`); hand rows `catalog.go:76-116`; models.dev `modelsdev.go:50-60,231,254`; `models.json` `internal/settings/models_file.go:70-74` via `internal/app/session_models.go:214`; model cache `CachedModel` has no price (`internal/settings/model_cache.go:100-103`).
- Totals: ACP `_ask/session/usage` (`pkg/protocol/acp.go:22`, `internal/acp/ask_methods.go:85,478-487,499-523,525`) reads the whole log and sums attempts; `Complete=false` only when an attempt lacks usage; it ignores `CostUnknown`.
- TUI: per-message usage and cost (`internal/tui/translate.go:510-516`); no session cost in the footer (`footer.go:14-15`).

## 4. Context window data

- `Model.ContextWindow`/`MaxTokens` (`internal/providers/model.go:51-60`) from hand rows (`catalog.go:45-46,94-95,159`), models.dev (`modelsdev.go:50-52,219-221`), `models.json` (`models_file.go:54-55`, `session_models.go:196-200`). Zero = unknown (`model.go:49-50`, footer `footer.go:97,100-112`). `RequestLimits` falls back to family defaults (`catalog_rules.go:175-180`).
- No tokenizer and no pre-send estimate in agent, pipeline or providers. Only faux estimates (rune-based, `faux/usage.go:18`).
- TUI context percent = total of the last assistant usage (`internal/tui/update_session.go:479-498,515-524`, `footer.go:58-65`, `session.go:182`): post-hoc, not an estimate of the next request.

## 5. Session log

- 15 entry types in `internal/sessions/entry.go` (methods :232-250); wire words `codec.go:108-125` (never changed), `FormatVersion` 1 (:17); `Ignorable` envelope flag (:95, decode skip :186) is never set by an encoder.
- Adding a type: struct + `entry()`; codec kind; `Clone` case if it holds pointers or slices; `sampleEntries` (`codec_test.go:55`) for `TestCodecRoundTripsEveryEntryType` (:85) and `TestEveryEntryTypeHasWireWord` (:113); `entry_test.go:14,50`; projections `internal/acp/updates.go:457-465`.
- Resume: `internal/app/session_log.go:130-155` → `agent.LoggedSelection` (`internal/agent/selection_log.go:76-136`).
- **Usage totals already rebuild on resume** because ACP usage reads the full log (`ask_methods.go:478-487`) and `AttemptSettled` persists per-attempt usage. A new entry would only be needed for a cost-complete flag or a stored cycle total.

## 6. Surfaces

- TUI footer: context bar only (`footer.go:58-112`); retry banner `update_session.go:516-518`.
- ACP: `_ask/session/usage`; attempt rows with usage (`updates.go:457`); retry rows with `failureCode` (`:465`); stop `max-tokens`; `ACPModel` has `ContextWindow`, `CostUnknown` (`pkg/protocol/acp.go:232-236`).
- Headless JSON: `cmd/tui/headless.go:227`, `headless_json.go:16-30` stream per-event usage, no totals.
- `ask -p` exit: `cmd/tui/headless_print.go:32-39` exits 1 on error/aborted (overflow exits 1); a length stop exits 0 with partial text.

## 7. Tests and cassettes

- `internal/providers/failure_test.go` (18, 43, 86, 125); `anthropic/failure_test.go` (58-176, `TestAnthropicUsageLimitIsQuota` :90, `TestAnthropicContextOverflowIsContextWindow` :104); `openai/failure_test.go` (63-176).
- `fantasykit/fold_test.go` (111, 146, 168); `cost_test.go` (11, 80); `faux/usage_test.go` (15-146).
- `internal/agent/recover_test.go` (21-168), `failure_code_test.go` (19, 121), `retry_events_test.go` (18, 33).
- `internal/sessions/codec_test.go` (85, 113, 141, 148), `entry_test.go` (14, 50).
- Cassettes `cmd/tui/testdata/cassettes/*.yaml` (six hold usage); `TestCassetteTextReply.yaml` has a real OpenAI Responses usage object (input 2119, cached 1536, output 5).

## 8. Constraints

- `docs/adr/0006-session-logs-as-jsonl-files.md` (log, durability); no ADR on usage, overflow or pricing.
- `docs/ask-architecture-reference.md:199,213-215,235`; `internal/agent/README.md:32-41`; `internal/providers/README.md:84,90,96`; `internal/sessions/README.md:14,20,36`.
- Roadmap `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md:400-414` (H9), :162 (micro-USD, H4 owns pricing), :248 (H9 keeps totals).

## Risks

- Silent zero cost (`cost.go:102-112`) and totals that ignore `CostUnknown` (`ask_methods.go:499-523`).
- A length stop exits 0 (`headless_print.go:32`); H9 must decide whether max-tokens relates to overflow.
- Regex misclassification: unrecognised overflow wording on a 400 becomes INVALID_REQUEST; quota regexes scan the whole detail.
- The retry budget resets per answer, so a multi-step cycle can retry more than five times.
- `Ignorable` is never set by an encoder, so a new entry type breaks older readers.
- `AttemptSettled` already persists usage; H-RETRY-10 may be partly met already.

## Unresolved questions

- Does `ModelCacheData` (`internal/settings/model_cache.go:69-90`) hold price or limit fields?
- Does the alibaba token-plan provider have its own error mapper?
- Should overflow get its own cycle reason or exit code for `ask -p`? (`docs/cli.md` not read.)
- Is `--mode json` exit 0 on an assistant error confirmed in code?
- Do the store contract suites enumerate entry types?
- Does the TUI context figure stay right after a resume whose last reply was an error?
