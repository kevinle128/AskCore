# H3 research: can `charm.land/fantasy` v0.45.2 carry Pi's Anthropic behavior?

Date: 2026-10-03. Read-only. `A:` = `charm.land/fantasy@v0.45.2/providers/anthropic/anthropic.go`, `PO:` = `provider_options.go` in that directory, `F:` = fantasy root, `SDK:` = `anthropic-sdk-go@v1.68.0` (pinned in fantasy `go.mod:8`).

Verified again by the lead (2026-10-03): `A:276-277` (`option.WithMaxRetries(0)`), `A:723-745` (`required` read only as `[]string`; tool has name, description, schema, cache only), `A:1302-1318` (finish-reason map), `providers/anthropic/error.go:33` (`RequestBody: apiErr.DumpRequest(true)`) and `SDK internal/apierror/apierror.go:80` (`httputil.DumpRequestOut`).

## Outcome

Fantasy can carry the Anthropic wire for Ask, but not alone. About 70% of the Pi adapter surface maps directly. The rest needs one Ask-owned `http.RoundTripper` (an SSE tap plus body patch) and `ProviderOptions.ExtraBody` patches.

The D22 note "the adapter must turn SDK retries off" is stale: fantasy already sets `option.WithMaxRetries(0)` (`A:277`; SDK default is 2, `SDK internal/requestconfig/requestconfig.go:173`). Keep a lock-in test, because a later fantasy version can change the line.

## 1. Thinking

| Need | Verdict | Evidence |
|---|---|---|
| Budget thinking | via options (`ThinkingProviderOption{BudgetTokens}`) | `PO:108-111`, `A:431-446` |
| Adaptive + effort | via options (`Effort` -> `output_config.effort`, adaptive) | `A:421-430`, `PO:15-26` |
| `off` | partial: no field sent when unset; explicit `{type:"disabled"}` needs `ExtraBody` | `A:420-477`, `A:132-134` |
| Signature round-trip | supported | `A:1651-1662`, `A:1564-1570`, `A:1122-1123` |
| Redacted thinking | supported (receive and replay) | `A:1514-1519`, `A:1572-1578`, `A:1124-1125` |
| Interleaved beta | via `Call.Headers["anthropic-beta"]` | `F:model.go:229-230` |

Silent behaviors: `Effort` forces adaptive for any model; temperature/top_p/top_k removed only in the budget branch (`A:447-470`); budget silently switched to adaptive for models that require it (`A:435-440`); empty-signature thinking dropped with a warning (`A:1126-1132`, breaks `allowEmptySignature`); thinking-only assistant message dropped as empty (`A:1187-1193`, `A:1209-1216`); no `max_tokens > budget_tokens` check.

## 2. Prompt caching

Per-part cache control on system blocks (`A:911-927`), last tool (`A:731,741-743`), last message parts (`A:951-952,1060-1062,1095-1096,1159-1161`). **1h TTL not supported**: `CacheControl` has only `Type`; any value gives 5m ephemeral (`PO:204-207`, `A:926`). Workaround: `ExtraBody` sjson path (`SDK option/requestoption.go:285-289`) or a `RoundTripper` body patch. `ExtraBody` has no fantasy test.

## 3. Stream parts

| Anthropic event | Fantasy part | Evidence |
|---|---|---|
| text/thinking/redacted start, delta, end | `text_*`, `reasoning_*` (ID = block index) | `A:1500-1512,1636-1650,1558-1579` |
| `tool_use` start | `tool_input_start` (tool id, name) | `A:1522-1530` |
| `input_json_delta` | `tool_input_delta` | `A:1663-1678` |
| tool block stop | `tool_input_end`, then `tool_call` (fallback `{}`) | `A:1545-1550,1754-1792` |
| `signature_delta` | `reasoning_delta` with empty `Delta`, signature in metadata | `A:1651-1662` |
| `message_start`, `message_delta`, unknown | `keepalive` | `A:1682-1688` |
| `message_stop` | `finish` (ID, reason, usage), only after stop + stop reason | `A:1704-1738` |

Lost: response model (never read), raw stop reason and `stop_details` (`A:1302-1318,1737`), response ID except on `finish`. Finish map: `end_turn`/`pause_turn`/`stop_sequence` -> stop; `max_tokens`/`model_context_window_exceeded` -> length; `tool_use` -> tool-calls; `refusal` -> content-filter; else unknown. The SDK drops `ping` before fantasy (`SDK packages/ssestream/ssestream.go:208-209`), so the idle timer must sit at the body level.

## 4. Usage

Input, output, cache read, total cache write supported (`A:1730-1736`). **No 5m/1h split** (SDK `CacheCreation.Ephemeral1hInputTokens`, `SDK message.go:1561-1565`, ignored). Reasoning tokens not mapped. Usage only on `finish`, so lost on abort and error. Fantasy `TotalTokens` excludes cache tokens (`A:1732`).

## 5. Errors

- HTTP errors during setup arrive as an `error` part with `*fantasy.ProviderError` (`StatusCode`, `TransientError`, `ContextTooLargeErr`), not as the `Stream()` error (`A:1467-1469,1692-1699`). `Stream()` errors only for prepare errors (`A:379,433`).
- `overloaded_error` -> `TransientError=true`.
- Abort -> `error` part with `ctx.Err()` (`A:1704-1712`).
- No `message_stop` or stop reason -> `NewIncompleteStreamError()` (`A:1704-1714`, `F:errors.go:125-131`).
- Unverified: a mid-stream error may carry HTTP 200 and title "ok"; retry code must read `TransientError`, not `StatusCode`.
- **Secret leak:** `ProviderError.RequestBody = apiErr.DumpRequest(true)` = `httputil.DumpRequestOut`, which includes the `X-Api-Key` header and the full prompt. Never log or persist it.
- Fantasy never calls `Close` on the SDK stream; the adapter must cancel a derived context. Use `context.WithCancelCause` to tell idle timeout from user abort.

## 6. HTTP control

| Need | Verdict |
|---|---|
| Custom `*http.Client` | `WithHTTPClient` (`A:249-254,290-292`); it replaces the SDK default (which has `ResponseHeaderTimeout` 10 min, `SDK default_http_client.go:14,23-27`) |
| Idle timeout | custom client, body-idle wrapper; never `Client.Timeout` |
| Base URL | `WithBaseURL` (`A:193-197,282-284`) |
| Extra headers | `WithHeaders` (`A:243-247`), `Call.Headers` |
| Header deletion | not supported; delete in `RoundTripper`. Default UA `Charm-Fantasy/...` unless `WithUserAgent` (`A:256-262,285`) |
| SDK retries | already off (`A:277`) |
| Per-request key | a new SDK client per `LanguageModel()` call (`A:275-348`) |
| Empty key | SDK falls back to `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, profile file (`SDK client.go:54-80`); always pass a non-empty key |
| `onPayload`, `onResponse` | not exposed; `RoundTripper` |

## 7. Tools

- Schema: only `properties` and `required` copied; `required` only if `[]string`, so a JSON-decoded `[]any` is silently dropped; `additionalProperties`, `$defs` dropped (`A:723-740`).
- `tool_choice` auto/any/none/specific via `Call.ToolChoice` (`A:863-891`); `DisableParallelToolUse` (`A:480-482`).
- `eager_input_streaming`, `strict`, `defer_loading` not supported (SDK has them, `SDK message.go:9393,9396,9405`). Workaround: `ExtraBody["tools"]` override (applied after fantasy's own tools, `A:125-134`) or body patch. Ask `ToolDecl` has no such fields (`pkg/protocol/tool.go:7-11`).

## 8. Images

User images supported (fantasy base64-encodes bytes; Ask stores base64, so decode first) (`A:961-973`). Tool-result images partial: one image plus optional text, image first (`A:1028-1045`, `F:content.go:313-317`); multi-image or ordered results need a patch.

## 9. Request fields

Temperature/top_p/top_k supported (`A:410-418`). **`max_tokens` nil becomes 4096** (`A:404-408`); always set the clamped value. Stop sequences and `metadata.user_id` only via `ExtraBody`. Warnings arrive as a `warnings` part (`A:1470-1477`); map them to `Metadata.Diagnostics`.

## 10. Silent parity breakers

1. Empty text sent as an empty block (`A:948-950,1092-1094,1023-1025`); API rejects it.
2. Consecutive same-role messages merged (`A:540-586`).
3. System messages after the first non-system block skipped silently (`A:904-909`).
4. Messages without "visible" content dropped (`A:1069-1075,1187-1193`), including thinking-only assistants.
5. Malformed tool input becomes `{}` (`A:1224-1241`); ids not rewritten.
6. Model-name substring heuristics for effort/adaptive (`A:53-112`).
7. PDF/text files become document blocks (`A:974-992`).

## StreamPart -> H1 Assembler mapping

| Fantasy part | Assembler call | Note |
|---|---|---|
| `warnings` | `SetMetadata{Diagnostics}` | build records in Ask |
| `keepalive` | none | no pings |
| `text_start/delta/end` | `TextStart`, `TextDelta`, `TextEnd(i, accumulated, nil)` | end part has no text; accumulate (`assembler.go:105-109`) |
| `reasoning_start` | `ThinkingStart("", nil, redacted?)` | redacted data -> `ThinkingSignature`, `Redacted=true` (`pkg/protocol/content.go:33-39`) |
| `reasoning_delta` with text | `ThinkingDelta` | |
| `reasoning_delta` empty + signature | none; remember signature | |
| `reasoning_end` | `ThinkingEnd(i, text, sig, redacted)` | |
| `tool_input_start` | `ToolStart(id, name, ...)` | keyed by tool id |
| `tool_input_delta` | `ToolDelta` | |
| `tool_call` | `ToolEnd(i, nil)` | assembler repairs JSON |
| `finish` | `SetUsage`, `SetMetadata{ResponseID}`, `Done(reason)` | raw reason, model, `CacheWrite1h` from the SSE tap |
| `error` | `Fail(StopError, msg, err)` | ctx cancel handled (`assembler.go:276-279`) |

## Gap table

| # | Item | Status | Workaround |
|---|---|---|---|
| 1 | Budget/adaptive/redacted thinking, signatures | supported | compat data picks mode |
| 2 | `thinking: disabled` | partial | `ExtraBody` |
| 3 | Interleaved beta | headers | `Call.Headers` |
| 4 | Cache control placement | supported | per-part options |
| 5 | 1h TTL | no | `ExtraBody` / patch |
| 6 | Raw stop reason, `stop_details`, `pause_turn` | no | SSE tap |
| 7 | Response model | no | SSE tap |
| 8 | 1h write split | no | SSE tap |
| 9 | Partial usage / early id on abort | no | SSE tap |
| 10 | strict / eager / defer / full schema | no | tools override / patch |
| 11 | `required` as `[]any` | silent loss | convert to `[]string` |
| 12 | Multi-image tool results | no | patch |
| 13 | Idle timeout | no | custom client |
| 14 | Header delete, payload/response hooks | no | `RoundTripper` |
| 15 | SDK retries | already off | lock-in test |
| 16 | Stop sequences, `metadata.user_id` | no | `ExtraBody` |
| 17 | `max_tokens` default 4096 | silent | always set |
| 18 | Empty text blocks | rejected | adapter filter |
| 19 | Empty-signature / thinking-only | dropped | adapter policy |
| 20 | Mid-conversation system | dropped | do not rely on it |

## Risks

1. Key and prompt leak through `ProviderError.RequestBody` (high).
2. Silent field loss (11, 17-20); mitigate with golden wire-JSON tests on a fake server.
3. `ExtraBody` sjson paths untested upstream; pin fantasy.
4. Model-name heuristics go stale; Ask compat data decides.
5. SSE tap duplicates parsing; reuse the H1 SSE reader; it must never block or fail the stream.
6. Mid-stream error classification unverified.
7. Leaked body on early stop; always cancel.

Status: DONE_WITH_CONCERNS. No code run; `ExtraBody` live behavior untested.
