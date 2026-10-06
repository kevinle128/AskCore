# H4 fit report: fantasy v0.45.2 for OpenAI Chat Completions and Responses

Date: 2026-10-05. Scope: research only. No code was changed.

Path shorthand: `F:` = `/Users/dale/Desktop/workspace/go/mobules/pkg/mod/charm.land/fantasy@v0.45.2/`. `SDK:` = `/Users/dale/Desktop/workspace/go/mobules/pkg/mod/github.com/charmbracelet/openai-go@v0.0.0-20260921175203-216db9e71b83/`. `PI:` = `/Users/dale/Desktop/workspace/opensources/pi/packages/ai/src/api/`. `A:` = `/Users/dale/orca/workspaces/AskCore/master-2/`.

## 0. Outcome

1. Chat Completions: fantasy fits. Use `providers/openaicompat`. Ask supplies its own hooks and the whole request body. Effort is medium.
2. Responses: fantasy does not fit for request building. Its prompt builder drops every reasoning item on replay. It exposes no hook for this and no ExtraBody. Fantasy only gives SSE to `StreamPart` parsing, and that parsing loses item ids and the streamed `encrypted_content`.
3. The roadmap line "H4 reuses the same adapter for OpenAI" (roadmap line 197) is true for Chat. It is false for Pi-style stateless Responses replay.
4. H3 code reuse is high, with one bug: `fold.go:131` reads the wrong field for OpenAI tool-argument deltas.
5. Placement: one fantasy-facing package. depguard needs a change in any case.

## 1. Chat Completions in fantasy

SDK pinned: `github.com/charmbracelet/openai-go v0.0.0-20260921175203-216db9e71b83` (`F:go.mod:13`). It is a Charm fork at a pseudo-version. `SDK:internal/version.go` says `3.64.0`.

### 1.1 Request mapping

| Item | Behavior | Source |
|---|---|---|
| Max tokens field | `Call.MaxOutputTokens` goes to `max_tokens` | `F:providers/openai/language_model.go:269-271` |
| Reasoning model switch | For IDs matching `o1/o3/o4/oss/gpt-5`: moves value to `max_completion_tokens`, removes temperature, top_p, penalties. Not configurable. | `language_model.go:285-328`, `:776-782` |
| `ProviderOptions.MaxCompletionTokens` | Exists on `openai` options, not on compat | `provider_options.go:133`, `hooks.go:77-79` |
| Reasoning effort | `none/minimal/low/medium/high/xhigh/max` | `provider_options.go:15-30`, `language_model_hooks.go:119-138` |
| `store`, `user`, `metadata`, `prompt_cache_key`, `safety_identifier`, `service_tier`, `parallel_tool_calls` | Only in `openai.ProviderOptions` | `provider_options.go:126-142`, `language_model_hooks.go:71-117` |
| `service_tier` flex/priority | Dropped by model-ID check | `language_model_hooks.go:167-185` |
| Compat options | Only `User`, `ReasoningEffort`, `ExtraBody` | `openaicompat/provider_options.go:37-41` |
| ExtraBody | `params.SetExtraFields(ExtraBody)`. Exists on compat only. | `openaicompat/language_model_hooks.go:72-74` |
| ExtraBody semantics | `SetExtraFields` overrides any field with the same key. `param.Omit` removes a field. | `SDK:packages/param/param.go:141-148`, `SDK:packages/param/encoder_test.go:182-197` |
| Options key | Compat reads `call.ProviderOptions[model.Provider()]`, the `WithName` value. `NewProviderOptions` writes the constant `"openai-compat"`. A custom `WithName` breaks the match. | `openaicompat/language_model_hooks.go:41`, `provider_options.go:97-102` |
| Plain `openai` options key | Constant `"openai"` | `openai/language_model_hooks.go:52` |
| Headers | Provider-level `WithHeaders`. Per-call `Call.Headers` and `Call.UserAgent`. | `openai.go:103-108`, `model.go:226-230`, `call_useragent.go:33-43` |
| System message | Always `system` role. No developer role. | `language_model_hooks.go:373`, `openaicompat/language_model_hooks.go:243,249` |
| Tool `strict` | Hard-coded `Strict: false`, always present. Not hookable. | `language_model.go:815` |
| Prompt builder | `LanguageModelToPromptFunc` is a hook. `WithLanguageModelToPromptFunc` replaces it. | `language_model_hooks.go:43`, `language_model.go:99-103` |
| Hook order | Defaults first, caller options appended after, so the caller wins | `openaicompat.go:28-36`, `:50-54`, `:136-140` |

### 1.2 Reasoning from compat servers

| Item | Behavior | Source |
|---|---|---|
| Fields parsed (stream and non-stream) | `reasoning_content` and `reasoning` only. `reasoning_text` is not parsed. `reasoning_details` is not parsed. | `openaicompat/provider_options.go:44-48,64-75` |
| Field provenance | Lost. `GetReasoningContent()` returns whichever field is non-empty. | `provider_options.go:51-56` |
| `null` reasoning field | Ignored (DeepSeek, vLLM, SGLang send `null` on every chunk) | `provider_options.go:58-75` |
| Stream events | `ReasoningStart/Delta/End`, ID = choice index. No metadata. | `openaicompat/language_model_hooks.go:107-184` |
| Replay out | Always writes `reasoning_content`, joined by `\n` | `openaicompat/language_model_hooks.go:489-498` |
| Reasoning-only assistant turn | Dropped on replay | `openaicompat/language_model_hooks.go:593-610` |
| `reasoning_details` | Only in `providers/openrouter`, which imports the `anthropic` and `google` fantasy providers | `F:providers/openrouter/language_model_hooks.go:3-17,95,228` |

### 1.3 Streaming, usage, stop reason, errors, retries

| Item | Behavior | Source |
|---|---|---|
| Tool-call deltas | Keyed by SDK `Index` (Go zero value when absent). Missing ID becomes `tool-call-%d`. Empty name and empty args are skipped. Later ID changes are ignored. Type other than `function` is an error. | `language_model.go:540-592`, `:555-559` |
| Risk | A server with no `index` merges all calls into index 0. Pi treats `index` as optional (`PI:openai-completions.ts:495`). | |
| Risk | Synthetic `tool-call-0` repeats on every turn | `language_model.go:559` |
| Tool-call emission | `ToolInputStart/Delta` live. `ToolInputEnd` and `ToolCall` only after the stream ends, in index order. | `language_model.go:680-706` |
| Truncated turn | `length`, `content_filter`, `error` or bad JSON args: `ToolCall` parts suppressed | `language_model.go:626-678` |
| `stream_options.include_usage` | Always `true` | `language_model.go:461-463` |
| Usage | `input = prompt - cached`. `CacheReadTokens` and `ReasoningTokens` mapped. If `reasoning > output`, reasoning folds into output. | `language_model_hooks.go:210-221,263-309` |
| Usage extras | Unknown usage fields kept in `ProviderMetadata["openai"].ExtraFields` | `language_model_hooks.go:304`, `provider_options.go:87-103` |
| Usage defect | A chunk without usage returns zero usage and nil metadata, and the caller assigns it every chunk. A usage chunk followed by another chunk loses the usage. | `language_model_hooks.go:263-266`, `language_model.go:488` |
| Usage fix | `WithLanguageModelStreamUsageFunc` is a hook | `language_model.go:80-85` |
| Cache write tokens | Not parsed for Chat | `language_model_hooks.go:286-292` |
| Finish reason | Mapped to an enum. Unknown becomes `unknown`. `insufficient_system_resource` becomes `error`. | `language_model_hooks.go:190-208` |
| Raw finish reason | `LanguageModelMapFinishReasonFunc` receives the raw string. A closure can store it. Called once at stream end. | `language_model_hooks.go:19`, `language_model.go:630` |
| Missing finish reason | With complete tool-call JSON: assumed tool turn plus warning. Otherwise: `NewIncompleteStreamError` (cause `io.ErrUnexpectedEOF`). | `language_model.go:640-748` |
| API errors | `*openai.Error` becomes `fantasy.ProviderError` with status, headers, bodies, context-too-large parse | `openai/error.go:26-44,83-112` |
| `ProviderError.RequestBody` | `DumpRequest(true)` uses `httputil.DumpRequestOut`. It includes the `Authorization` header. | `error.go:36`, `SDK:internal/apierror/apierror.go:52-58` |
| Mid-stream SSE error | `TransientError` by error type | `error.go:46-50,68-81`, `F:errors.go:188-202` |
| Transport errors | `WrapTransportError` (unexpected EOF, HTTP/2 resets) | `F:errors.go:204-224` |
| SDK retries | OFF. `option.WithMaxRetries(0)`. SDK default is 2. | `openai.go:168`, `SDK:internal/requestconfig/requestconfig.go:282` |
| Anthropic SDK retries | Also OFF in v0.45.2 | `F:providers/anthropic/anthropic.go:277` |
| Fantasy retry | Only in `agent.go` (lines 520, 1034). `LanguageModel.Stream` never retries. | `F:retry.go`, `F:agent.go:520,1034` |
| Keepalive part | Anthropic only. Chat and Responses emit none. Chunks with no choices are silent. | `anthropic.go:1686`, `language_model.go:489-491` |

Note for the roadmap: D22 says the Anthropic provider "leaves the SDK's default retries on". In v0.45.2 that is not true (`anthropic.go:277`). Verified by reading the source.

## 2. Responses API in fantasy

It is present. It lives in `F:providers/openai/responses_language_model.go`. The provider picks it only when `WithUseResponsesAPI()` is set and `IsResponsesModel(modelID)` or a `WithResponsesAPIFunc` says yes (`openai.go:131-144,191-201`). Selection is per provider, not per model record.

| Item | Behavior | Source |
|---|---|---|
| Hooks | The Responses model takes only the header hook. Prompt, usage, finish and stream hooks are ignored. | `openai.go:197-200`, `language_model.go:202-211` |
| ExtraBody | None. A grep for `ExtraBody\|SetExtraFields` in the Responses files returns nothing. | `responses_language_model.go`, `responses_options.go` |
| Per-call body patch | Only headers and UA are per call | `call_useragent.go:16-43` |
| `store` | Defaults to `false`. `ProviderOptions.Store` overrides. | `responses_language_model.go:194-198` |
| `previous_response_id` | Supported. Needs `store=true`. Prompt may hold system and user messages only. | `:200-208`, `:385-395`, `responses_options.go:168-171` |
| `include` | Only if the caller passes `Include`. `reasoning.encrypted_content` is not added by default. | `:214-217,288-290`, `responses_options.go:127-130` |
| Reasoning effort and summary | Sent only when `isReasoningModel` (model-ID heuristic) is true. Otherwise dropped with a warning. | `:292-301`, `:334-351`, `:90-153` |
| System role | `developer` for reasoning IDs, `remove` for `o1-mini/o1-preview`, else `system`. Not overridable. | `:90-153`, `:468-470` |
| Strict tools | `StrictJSONSchema` option. Default `false`. | `:741-757` |
| Other options | `ParallelToolCalls`, `ServiceTier`, `PromptCacheKey`, `SafetyIdentifier`, `User`, `Metadata`, `MaxToolCalls`, `Instructions`, `TextVerbosity` | `responses_options.go:161-184`, `:253-286` |
| Not mapped | `prompt_cache_retention`, `prompt_cache_options` (Pi sends them, `PI:openai-responses.ts:335-336`) | |
| Max tokens | `max_output_tokens` | `:249-251` |

### 2.1 Reasoning items and ids

| Item | Behavior | Source |
|---|---|---|
| Reasoning metadata type | `ResponsesReasoningMetadata{ItemID, EncryptedContent, Summary[]}` on `ReasoningStart/Delta/End` parts | `responses_options.go:97-102`, `responses_language_model.go:1062-1082,1149-1162` |
| Streamed `encrypted_content` | Read once at `output_item.added`. Not refreshed at `output_item.done`. Real servers send it at `done`. Pi patches this case (`PI:openai-responses-shared.ts:534-548`). | `responses_language_model.go:1067-1069` vs `:1149-1162` |
| Non-stream reasoning | Item with no summary and no encrypted content is dropped. Summary parts joined with `\n`. | `:918-933` |
| Replay of reasoning | Always skipped. Comment says replay is "not supported by the API". Pi replays the stored item. | `responses_language_model.go:596-603`, `PI:openai-responses-shared.ts:265-266` |
| Replay of assistant text | `EasyInputMessage` with text only. No `msg_` id, no `phase`. | `:565` |
| Replay of function call | `function_call` with `call_id`, name, args. No `fc_` item id. | `:591` |
| Item ids surfaced | Reasoning: yes. Text start: message item id. Function call: `CallID` only. | `:1031-1037`, `:1054-1060` |
| Pi tool id | `call_id\|item_id`, with `fc_` normalization | `PI:openai-responses-shared.ts:161-174,292-322,489` |
| `output_index` | Used to key function calls | `:1031,1089,1167` |
| Events handled | created, output_item added/done, function_call_arguments.delta, output_text.delta, annotation.added, reasoning_summary_part.added, reasoning_summary_text.delta, completed, incomplete, failed, error | `:1023-1288` |
| Events dropped | Everything else, including `function_call_arguments.done`, refusal, reasoning text events | `:1022-1288` (no default case) |
| Usage | Cached subtracted from input. Reasoning mapped. | `:409-429` |
| Finish reason | `mapResponsesFinishReason`. Plain function. Raw `incomplete_details.reason` is lost. | `:967-985` |
| Errors | In-band `failed` or `error` becomes plain `fantasy.Error`. No status. Not retryable. HTTP errors share the Chat mapping. | `:1269-1287,1336-1349` |
| No terminal event | `NewIncompleteStreamError` | `:1300-1310` |
| Response id | `ResponsesProviderMetadata.ResponseID` on the finish part | `:397-407,1322-1329` |

Conclusion for Responses: stateless replay (`store:false`, `include: reasoning.encrypted_content`, replay stored items) is what Pi does (`PI:openai-responses.ts:337,372`). Fantasy cannot build that request. Two parts fail: the prompt builder drops reasoning, and the streamed reasoning metadata has no encrypted payload. Function-call item ids are not exposed. Fantasy contributes SSE decode and a partial mapping. Ask must own request construction and keep raw output items.

## 3. Gap table: Pi need to fantasy support

Legend: yes / hook (Ask supplies a fantasy hook) / ExtraBody / RT (RoundTripper body patch) / no.

| # | Pi need (Pi ref) | Chat (`openaicompat`) | Responses |
|---|---|---|---|
| 1 | Per-model compat record, not model-ID or URL heuristics | Partly. `isReasoningModel` switch cannot be turned off (`language_model.go:285-328`). Work-around: leave `MaxOutputTokens`/temperature nil on the `Call`, put all fields in ExtraBody. | no. Heuristics at `responses_language_model.go:90-153` and `:292`. Needs RT. |
| 2 | Body fully built by Ask (H3 style) | ExtraBody (`messages`, `tools`, ...), verify override of the stub prompt with a test | RT (no ExtraBody) |
| 3 | Replay encrypted reasoning items and item ids (`PI:openai-responses-shared.ts:265,292,534`) | n/a | no via fantasy (`:596-603`). RT rewriting `input`, plus raw item capture. |
| 4 | Read thinking from compat servers: `reasoning`, `reasoning_content`, `reasoning_text` (`PI:openai-completions.ts:268,606`) | `reasoning`, `reasoning_content`: yes. `reasoning_text`: no, hook (custom `StreamExtraFunc` on `Delta.RawJSON()`). | n/a |
| 5 | Know which field the server used (`PI:...:1331-1336`) | no, hook (custom `StreamExtraFunc`, pass name via `ProviderMetadata`) | n/a |
| 6 | Send thinking back as server expects (`PI:...:1305-1383`) | Only `reasoning_content` (`openaicompat/...hooks.go:494-498`). Other names, `reasoning_details`, text-as-thinking: hook or ExtraBody `messages`. | n/a |
| 7 | `reasoning_details` parse and replay (`PI:...:665,1376`) | no in openai/compat. Hook, or copy the openrouter pattern. | n/a |
| 8 | Developer vs system role (`PI:...:1225`) | hook (own `ToPromptFunc`) or ExtraBody `messages` | no. Heuristic only. RT. |
| 9 | `max_tokens` vs `max_completion_tokens` (`PI:...:838-842`) | ExtraBody (set the field, leave `MaxOutputTokens` nil). `param.Omit` for the other. Verify by test. | `max_output_tokens`: yes |
| 10 | `store` flag (`PI:...:833`) | ExtraBody. Compat has no `Store`. | yes (`:194-198`), default false |
| 11 | Strict tool schema omit/false/true (`PI:...:1497-1505`) | RT or ExtraBody `tools`. Hard-coded `Strict:false` (`language_model.go:815`) is overridden if `tools` is in ExtraBody. | yes (`StrictJSONSchema`, `:741-757`) |
| 12 | `stream_options.include_usage` off (`PI:...:829`) | ExtraBody `stream_options: param.Omit`. Verify by test. | n/a |
| 13 | Reasoning effort off/none, per-model map (`PI:openai-responses.ts:360-372`) | yes (compat `ReasoningEffort`), or ExtraBody | Only if heuristic says reasoning model. Else RT. |
| 14 | Reasoning summary `auto` | n/a | yes (`ReasoningSummary`), same heuristic gate |
| 15 | `include: reasoning.encrypted_content` | n/a | yes (`Include`) |
| 16 | `prompt_cache_key` | ExtraBody (compat), yes (plain `openai` options) | yes |
| 17 | `prompt_cache_retention`, `prompt_cache_options` (`PI:openai-responses.ts:335-336`) | ExtraBody | RT |
| 18 | `service_tier`, `user`, `metadata`, `parallel_tool_calls` | ExtraBody (compat has `User` only) | yes |
| 19 | Tool-call id control | yes (`openaicompat/...hooks.go:463`, verbatim). Missing ids get `tool-call-N` (`language_model.go:559`). | `call_id` only. `call_id\|item_id` no. |
| 20 | Streaming tool deltas: index, missing id | yes (`language_model.go:540-592`). No-index servers: no. | yes, keyed by `output_index` |
| 21 | Usage incl. cached and reasoning tokens | yes. Replace usage hook to fix the zeroing defect. Cache write: no. | yes. Cache write: no. |
| 22 | Provider-specific usage fields (DeepSeek, Moonshot) | yes via `ProviderMetadata.ExtraFields` | no |
| 23 | Raw stop reason | hook (`MapFinishReasonFunc` closure) | witness (H3 mechanism, key `incomplete_details`) or accept enum |
| 24 | Idle timeout with custom `http.Client` | yes: `WithHTTPClient` (`openai.go:111`). H3 wrapper reusable. Must tap bytes: silent chunks yield nothing. | yes, same |
| 25 | Abort | yes: ctx cancel. `failStream` checks `reqCtx.Err()` first (`fold.go:186-190`). | yes |
| 26 | SDK retries off | yes (`openai.go:168`) | yes |
| 27 | Mid-stream failure classification | yes (`error.go:46-81`) | Plain `fantasy.Error`, no status (`:1336-1349`) |
| 28 | Headers (Azure `Api-Key`, Copilot) | yes (`WithHeaders`, `WithSDKOptions`). Azure: `azure/azure.go:48-58`. | yes |

Reading the table: Chat needs ExtraBody and four hooks (stream-extra, to-prompt or ExtraBody messages, usage, finish-reason). Responses needs RT for rows 1, 3, 8, 13 (non-heuristic models), 17, and raw-item capture for row 3.

## 4. Reuse from `internal/providers/tokenplan/`

Key fact: H3 does not use fantasy to build the request. `buildDocument` builds the full JSON body from Ask types (`document.go:15-70`). It passes the body as `ExtraBody` with a stub prompt `"."` (`provider.go:181-244`). A test checks the stub is replaced (`provider_test.go:106`). Fantasy is used for HTTP, SSE decode and `StreamPart`. This is the right model for Chat. It cannot work for Responses (no ExtraBody).

| Part | Where | Verdict |
|---|---|---|
| Fold loop shape (`StreamPart` to `Assembler`) | `fold.go:35-147` | Generic. Keep. |
| Tool-delta field | `fold.go:131` reads `part.ToolCallInput` | Bug for OpenAI. Anthropic sets `ToolCallInput` (`anthropic.go:1675`). Chat and Responses set `Delta` (`language_model.go:546,586`, `responses_language_model.go:1172`). Google and kronk also use `Delta`. Shared fold must use `cmp.Or(part.Delta, part.ToolCallInput)`. |
| Synthetic tool id | `fold.go:127` passes `part.ID` | Chat gives `tool-call-N` (non-empty), so `Assembler` does not generate a unique id. Adapter must pass `""` for synthetic ids. |
| Reasoning metadata | `reasoningMeta` `fold.go:213-223` | Anthropic-typed. Chat has no metadata. Responses has `ResponsesReasoningMetadata`. Needs per-API function. |
| Thinking block rules | `fold.go:76-125` (delay start, skip empty) | Generic idea. Signature handling is per API. |
| Stop reason switch | `finishStream` `fold.go:149-184` | Anthropic-specific values. Per-API table. Usage sum and metadata code is generic. |
| Usage mapping | `fold.go:150-156` | Generic. Fantasy `InputTokens` excludes cached, so the sum is right. |
| Error mapping | `failStream`, `httpMessage`, `extractJSONBody`, `isIncompleteStream` `fold.go:186-255` | Generic. Parses `ResponseBody` only. Never print `RequestBody` (holds the API key). |
| Idle timer, `touchBody`, `wrapClient`, `roundTripFunc` | `provider.go:187-222,261-286` | Generic. Keep. |
| Witness | `witness.go` | Mechanism generic. Key `"stop_reason"` is hard-coded (`witness.go:67`). Make the key a parameter. For Chat use the `MapFinishReasonFunc` closure instead. |
| Key lookup | `provider.go:143-157` | Generic shape. Env names are provider data and belong to the catalog, not the adapter. |
| Cache retention check | `provider.go:158-167` | Per API. |
| Constants (`ProviderID`, `BaseURL`, `ModelID`, key envs) | `provider.go:23-34` | Provider data. Move to the catalog and the compat record. |
| `document.go` | all | Anthropic-specific. Chat and Responses each need an own document builder. |
| `normalizeAnthropicToolCallID` | `provider.go:288-300` | Anthropic rule. Chat and Responses need own rules (`transform.go` takes a function). |
| `LanguageModel` per call | `provider.go:224-238` | Keep. Per-call construction allows per-call RT and per-call hook closures (raw stop reason, field name). |

### 4.1 Layout recommendation

Ranked:

1. **One fantasy-facing package** (rename `tokenplan` to a name for what it isolates, for example `fantasyadapter`). Files: `anthropic.go`, `openai_completions.go`, `openai_responses.go`, plus shared `fold.go`, `witness.go`, `idle.go`, `errors.go`. Reasons: D22 says "that adapter package" (singular) and "one Ask adapter". depguard already allows exactly one path. Shared fold, idle and error code stay in one copy (DRY). Cost: one package grows to about eight files. Token Plan becomes catalog data plus a base URL and key env, not a package.
2. One package per API (`.../anthropic`, `.../openaicompat`). Cost: three exceptions in depguard, and the fold, idle and error code is copied or moved to a fourth shared package that also imports fantasy. Rejected.
3. Keep `tokenplan` and add `openai` beside it. Rejected: duplicates the fold and keeps a provider name on an API-level adapter.

Tension to decide, not hide: `internal/providers/README.md` ("File names") says `<vendor>.go` flat files in `internal/providers`. That predates the depguard isolation. A subpackage is needed because the depguard rule works on paths.

### 4.2 Responses: options (decision for the user, D22 touched)

| Option | Description | Verdict |
|---|---|---|
| A | Fantasy Responses for decode only, RT to rewrite `input`, wire tap to capture raw `output_item.done` items | Works, but re-implements half of the decoder in a tap. Fragile. |
| B | In the same package, call `github.com/charmbracelet/openai-go/responses` directly. Typed events and `RawJSON()` of each item give exact replay. Ask owns body, ids, errors. | Simplest correct path. Deviates from D22 for this API only. openai-go is already a fantasy dependency. |
| C | Use fantasy Responses as is. `store:true` plus `previous_response_id` only. | Fails for stateless backends and Pi parity (encrypted replay). Ask history is local (H8 log). |
| D | Defer Responses stateless replay. | Violates the H4 exit ("correct thinking replay" across vendors). |

Recommendation: B for Responses, fantasy `openaicompat` for Chat. This changes the D22 text ("fantasy types never leave the adapter" still holds; openai-go types would also stay inside it). User decision needed before H4 starts. Do not apply silently.

## 5. depguard (`A:.golangci.yml:70-79`)

| Item | Current | Needed |
|---|---|---|
| Rule `providers-no-fantasy` | `files: **/internal/providers/**` minus `!**/internal/providers/tokenplan/**` | Change the exclusion to the new package path (layout option 1) |
| Denied imports | `charm.land/fantasy`, `github.com/anthropics/anthropic-sdk-go` | Add `github.com/charmbracelet/openai-go`. depguard `pkg` matches by prefix, so `charm.land/fantasy/providers/openai` is covered. |
| Other rules | `providers-no-tools`, `core-no-adapters` (`.golangci.yml:55-68`) | No change |
| `go.mod` | fantasy v0.45.2 (`go.mod:6`), anthropic-sdk-go indirect (`go.mod:91`). openai-go not listed. | `go mod tidy` adds openai-go (indirect) at first import. |

## 6. Adoption risk

| Risk | Detail |
|---|---|
| Fork | `charmbracelet/openai-go` is a Charm fork at a pseudo-version with no tag. A fantasy minor release can move it. Pin through the fantasy version. |
| Heuristics | Model-ID switches in fantasy change with releases. Ask must not depend on them. Send all model-sensitive fields from the compat record. |
| Hook surface | Hook types use SDK structs (`openai.ChatCompletionChunk`). Our hooks import the fork. Keep them in the adapter package. |
| Usage defect | Chat usage zeroing (section 1.3) needs a custom hook in Ask. |
| Error body | `ProviderError.RequestBody` contains the API key. Do not log or serialize whole `ProviderError`. |
| Fantasy maturity | Active. Many defect fixes in comments. API is pre-1.0 (v0.45.2). |

## 7. Limitations

- No live call to any server. All claims come from source reading.
- The `param.Omit` and ExtraBody-overrides-stub-prompt claims come from SDK docs and tests (`param.go:141-148`, `encoder_test.go:182-197`). No Chat-specific test was run. Add a golden test before H4 relies on them.
- Pi source was read only for the cited lines. The full Pi compat behavior (`detectCompat`, `PI:openai-completions.ts:1585-1714`) was not re-derived.
- `openrouter`, `azure`, Google and kronk providers were read only where cited.
- Alibaba, DeepSeek and Qwen wire quirks were not tested.

## Unresolved questions

1. Does H4 require stateless encrypted-reasoning replay for Responses? This decides fantasy or direct SDK (section 4.2). It is a D22 scope question for the user.
2. Does `param.Omit` set through `ExtraBody` survive the assignments after `prepareCallFunc` (`language_model.go:351-352`, `:461`)? Needs a test.
3. Does ExtraBody `messages` fully replace the stub prompt for OpenAI Chat? H3 proved it for Anthropic only (`provider_test.go:106`).
4. Where does the compat record live? README names `compat.go`. It is absent (`A:internal/providers/compat.go` does not exist, and `providers.Model` has no compat field, `model.go:5-16`).
5. Should the package be renamed before H4 (layout option 1), and who updates `.golangci.yml`?
6. Servers with no tool-call `index`: does Ask accept the merge risk, or write an own accumulator (hook cannot replace the accumulator)?

Status: DONE_WITH_CONCERNS
Summary: Fantasy fits Chat Completions through `openaicompat` with Ask-owned body and hooks. It does not fit stateless Responses replay: the prompt builder drops reasoning items, there is no ExtraBody, streamed encrypted content and function-call item ids are lost. H3 reuse is high except one tool-delta field bug. Layout and depguard changes are needed.
