---
phase: 2
title: "Providers"
status: pending
priority: P1
effort: 6h
dependencies: []
---

# Phase 2: Providers

## Goal

Fix R6, N1, N2, N5, N6, N7, N8, N9, N10 and N11 in the provider adapters: one credential-header rule for both OpenAI paths, an empty reply that is retried, and the small wire and parse defects.

## File ownership

- Modify: `internal/providers/openai/profiles.go`, `completions.go`, `responses.go`, `completions_body.go`
- Modify: `internal/providers/fantasykit/fold.go`
- Modify: `internal/providers/model_config.go`, `normalize.go`, `replay.go`, `catalog.go`
- Modify: `internal/providers/anthropic/prepare.go`, `internal/providers/anthropic/profile.go`
- Modify: `internal/providers/partialjson/partialjson.go`
- Modify: `internal/app/session_models.go` (compat default of N7 only)
- Tests: `internal/app/session_models_test.go` (N7 only), `internal/providers/openai/profiles_test.go`, `completions_test.go`, `internal/providers/fantasykit/fold_test.go`, `internal/providers/model_config_test.go`, `internal/providers/normalize_test.go`, `internal/providers/anthropic/prepare_test.go`, `profile_test.go`, `internal/providers/partialjson/partialjson_test.go`, a new `internal/providers/replay_test.go`

Not owned here: `internal/providers/cassette/` (phase 6) and `internal/settings/models_file.go` (phase 5, base URL trim of N3/N4).

## Constraints

- No test makes a network request. Use the `rtFunc` transports and helpers in `internal/providers/openai/helpers_test.go` and `profiles_test.go:79-81`.
- The six cassettes record Responses requests with no models.json headers. No step changes a request body or URL of those runs, so no re-record is expected. Phase 7 proves it with the replay tests.

## Steps

### 1. R6: one credential-header rule for Completions and Responses (Grok rule, user decision)

Rule: a user `Authorization` header from models.json wins over a static API key (saved key, models.json `apiKey`, environment, `--api-key`; all are `opts.Auth.Method == "api-key"`, `internal/app/module_auth.go:46-58`). A saved OAuth credential wins over the header. The other credential headers (`X-Api-Key`, `Cookie`, `OpenAI-Organization`, `OpenAI-Project`, `ChatGPT-Account-ID`) stay filtered, because the guards refuse them on both paths.

Evidence: with a binding, both paths use `profileClient` (`completions.go:126-129`, `responses.go:135-138`), which drops the user `Authorization` and forces the token (`profiles.go:129-137`). The guards require `"Bearer "+token` (`profiles.go:58`, `:154`). Without a binding, `isolateClient` lets the model header win (`isolate.go:60-62`). The two cases disagree, and neither follows the approved rule for a static key.

Tests first in `profiles_test.go`:

1. `TestUserAuthorizationHeaderWinsOverStaticKey`: for Completions (Token Plan row) and Responses (OpenAI row), `m.Headers = {"Authorization": "Bearer proxy"}` and an `api-key` binding; expect the request to carry `Bearer proxy`, one `Authorization` value, and the guard to pass.
2. `TestOAuthTokenWinsOverUserAuthorizationHeader`: Responses with `openai-chatgpt` and `xai-oauth` bindings and the same header; expect the OAuth token.
3. Change the `api-key` rows of `TestXAIProfilesUseNativeRouteAndConditionalReasoning` (`profiles_test.go:83-110`): with `Authorization: Bearer wrong` in the headers, the `api-key` case now sends `Bearer wrong`. The `xai-oauth` case keeps `Bearer xai-token`. State this intended change in the commit message.

Fix:

1. Give `profileClient` the credential kind (static key or OAuth) instead of only `chatGPT`. Compute the `Authorization` value once: the user header (case-insensitive key) for a static key when present, else `"Bearer " + token`.
2. Pass that value to `completionKeyRT` and `responsesProfileRT` and compare against it, in place of `"Bearer "+t.token` (`profiles.go:58`, `:154`).
3. Keep the filter of the other credential headers as it is (`profiles.go:131-133`).

Command: `go test -run 'Authorization|TestXAIProfiles' ./internal/providers/openai`

### 2. N1 (Medium): an empty reply is not a success

Evidence: `TextStart` adds a block to `texts` with no content (`fold.go:95-97`); `blocks := len(texts) + len(tools)` plus opened thinking blocks (`fold.go:207-212`) is then above zero, so `finish` skips `CodeEmptyResponse` (`fold.go:298-301`). The retry of `CodeEmptyResponse` exists in `internal/pipeline/recovery.go:15`.

Accepted rule (omp and Grok): a reply has content only with non-whitespace text, a complete tool call, or an image. A content-filter stop is exempt. Fantasy folds have no image output part, so text and tool calls are the two cases. A content-filter stop maps to an error, not `StopStop` (`internal/providers/openai/stop.go:38-39`), so the `StopStop` gate already exempts it. A reply with only thinking counts as empty, as in omp.

Tests first in `fold_test.go`, next to `TestFoldEmptyStopIsEmptyResponseButEmptyLengthIsNot` (`:146`):

1. `TextStart` + `TextEnd` + finish `stop` -> `CodeEmptyResponse`.
2. Text with only spaces and newlines -> `CodeEmptyResponse`.
3. Thinking only + finish `stop` -> `CodeEmptyResponse`.
4. `ToolInputStart` with no `ToolCall` part + finish `stop` -> `CodeEmptyResponse`.
5. One complete tool call, or one non-whitespace text -> success (regression rows).
6. Finish `length` with an empty block -> not empty (keeps the existing row).

Fix: count a text block only when `strings.TrimSpace(b.text) != ""`; count a tool block only after its `ToolCall` part (add `done bool` to `toolBlock`, set at `fold.go:173-174`); stop counting thinking blocks.

Command: `go test -run '^TestFold' ./internal/providers/fantasykit && go test ./internal/pipeline`

### 3. N2: `input: []` changes nothing

Evidence: the contract says "A nil or empty field changes nothing" (`model_config.go:73-74`); the gate is `p.Input != nil` (`:364`) and `validInput` refuses an empty list (`:439-441`).

Test first: in `model_config_test.go`, a patch with `Input: []string{}` gives no problem and keeps the row input.

Fix: gate on `len(p.Input) > 0`.

Command: `go test -run 'Input' ./internal/providers`

### 4. N5: token estimate from bytes, with a margin

Evidence: `clampMaxTokens(tm, opts, utf8.RuneCount(doc.body)/4)` (`anthropic/prepare.go:61`) undercounts CJK; the room is `window - estimate - 4096` (`anthropic/document.go:151`).

Accepted rule (omp): bytes / 4, plus 10 %, plus 64 tokens.

Test first in `prepare_test.go`: for a large CJK prompt and an ASCII prompt of the same byte size, expect the same `max_tokens`, equal to the clamp of `window - est - 4096` with `est := bytes/4; est += est/10 + 64` (the same integer steps as the code). Today the CJK prompt gets a larger value.

Fix: `est := len(doc.body) / 4; est += est/10 + 64`. Remove the `unicode/utf8` import when unused.

Command: `go test -run 'MaxTokens|Estimate' ./internal/providers/anthropic`

### 5. N6: Anthropic profile wrapper closes and does not change the request

Evidence: `io.ReadAll(req.Body)` then `req.Body = io.NopCloser(...)` (`anthropic/profile.go:62-66`): the original body is not closed and the caller's request changes.

Test first in `profile_test.go`: a body that records `Close`; expect it closed after `RoundTrip`, and the caller's `req.Body` unchanged.

Fix: read, close the original, then send `req.Clone` with the new body, `GetBody` and `ContentLength`, as `openai/profiles.go:107-111` does.

### 6. N8: `omitStreamOptionsRT` closes the original body

Evidence: it reads `req.GetBody()` and closes only that copy (`openai/completions.go:215-216`).

Test first in `completions_test.go`: the same close-recording body; expect it closed.

Fix: `if req.Body != nil { defer func() { _ = req.Body.Close() }() }` at the top of `RoundTrip`.

Command for steps 5 and 6: `go test -run 'Close' ./internal/providers/anthropic ./internal/providers/openai`

### 7. N7: wire `SupportsReasoningEffort` (see open question)

Evidence: the field exists (`internal/providers/compat.go:17`) and models.json can set it (`internal/settings/models_file_test.go:37`), but no code reads it; `reasoning_effort` is sent whenever an effort is set (`openai/completions_body.go:100-102`). Removing the field would make a Pi-style key an "unknown compat key" problem (`internal/app/session_models.go:253-256`, `docs/cli.md:554`). Wiring keeps the files valid and follows Pi.

Tests first in `completions_test.go`: a Completions row with `SupportsReasoningEffort: false` and an effort sends no `reasoning_effort`; the Token Plan row (`catalog.go:64-69`) still sends it; a models.json model without the key still sends it.

Fix:

1. Gate `extra["reasoning_effort"]` on `compat.SupportsReasoningEffort`.
2. Set `SupportsReasoningEffort: true` on the Token Plan row (`catalog.go:64-69`).
3. Default it to true for new Completions rows: in `zeroCompat` (`model_config.go:426-432`) and in the compat mapping, which starts from `&providers.CompletionsCompat{}` (`internal/app/session_models.go:244`). A key in the file then overrides the default.

Put the models.json case in `internal/app/session_models_test.go`.

Command: `go test -run 'ReasoningEffort' ./internal/providers/openai ./internal/app`

### 8. N9: partial JSON checks the colon

Evidence: the byte after a key is skipped with no check (`partialjson/partialjson.go:260-261`); `{"a" 12}` gives `{"a":2}`.

Accepted rule (omp): check the colon; when it is missing, return the object built so far (the existing repair path).

Test first in `partialjson_test.go`: `{"a" 12}` -> `{}`; `{"a":1,"b" 2}` -> `{"a":1}`; `{"a"` (truncated) -> `{}`.

Fix: after `skipBlank`, `if p.i >= len(p.s) || p.s[p.i] != ':' { return obj, nil }`, then `p.i++`. Delete the comment at `:260`.

Command: `go test ./internal/providers/partialjson`

### 9. N10: compare provider, API and model id before keeping an item id

Evidence: `NormalizeResponsesToolCallID` compares only `source.Model != model.ID` (`normalize.go:74`), while the caller's same-model test uses provider, API and id (`transform.go:194`, `:277`).

Test first in `normalize_test.go`: the same model id from `openai` replayed to another native provider keeps the call id and gets `fc_<hash>` as item id.

Fix: `if source.Model != model.ID || source.Provider != model.Provider || source.API != string(model.API)`.

### 10. N11: refuse a thinking signature without type and id

Evidence: `DecodeThinkingSignature` accepts `null` and `{}` (`replay.go:57-63`); the caller then builds a reasoning item with an empty `ItemID` (`openai/responses_prompt.go:309-320`) instead of the text fallback (`:310-315`).

Accepted rule (omp): valid only with `Type == "reasoning"` (the value the encoder writes, `openai/stop.go:61`) and a non-empty `ID`.

Test first: `null`, `{}`, `{"type":"reasoning"}` and `{"id":"rs_1"}` return false; a full signature returns true.

Fix: return false when `r.Type != "reasoning" || r.ID == ""`.

Command for steps 9 and 10: `go test -run 'Normalize|ThinkingSignature' ./internal/providers`

## Verification

- `go test ./internal/providers/... ./internal/app ./internal/pipeline`
- `go vet ./internal/providers/... ./internal/app`
- `go test -run '^TestCassette|^TestCapturedSessionReplays' ./cmd/tui` (replays pass with no re-record).

## Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| R6 sends a proxy header where a user expected the key | Low | Medium | The rule is the approved one; phase 7 documents it at `docs/cli.md:553`. |
| N1 retries a reply that a model meant as empty | Low | Low | The retry budget of the recovery handler applies; the end state is an error that names the cause. |
| N7 default true is missed on one creation path | Medium | Medium | Test 3 of step 7 covers a models.json model without the key. |
| N1 treats a reply with only thinking as empty | Medium | Low | This is the approved omp rule. A reasoning model that stops after thinking now gets the empty-reply retry and then an error. The changelog line says so. |

## Rollback

Revert the phase commits. No stored data changes.

## Docs impact

Phase 7: `docs/cli.md:553` (header rule), `docs/cli.md:554` (compat key `supportsReasoningEffort`), `CHANGELOG.md` Fixed lines for R6, N1, N7.

## Open question

N7: the triage tables offer "wire" or "remove". This plan wires it, because removal turns a valid Pi-style key into a settings problem. Confirm, or choose removal (then delete the field and accept the new problem line).
