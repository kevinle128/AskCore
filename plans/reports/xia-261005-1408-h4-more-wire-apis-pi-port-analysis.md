# H4 port analysis: more wire APIs and cross-provider replay, Pi to Ask

Date: 2026-10-05. Mode: `xia --port`, stopped after analysis (user request: write the report and stop; no plan, no implementation, no roadmap edits, no live probe).

## 0. Source manifest

| Item | Value |
|---|---|
| Source | Pi monorepo, `/Users/dale/Desktop/workspace/opensources/pi`, checkout `4c6fb7cfe` (v1.0.1). GitNexus repo `pi` is indexed at the same commit. The roadmap pins `2bbfcca4`. |
| Source drift on H4 files | `openai-completions.ts`, `transform-messages.ts`, `models.ts`, `simple-options.ts`, `overflow.ts`, `utils/hash.ts`: no change since the pin. `openai-responses-shared.ts`: one change, `bc2d8dc1c` (2026-10-01, item id prefix rule). `utils/retry.ts`: one pattern, `3874b3e98` (`model is at capacity`). `env-api-keys.ts`: Anthropic federation names only. |
| License | MIT. A port reads behavior; no code is copied. |
| Wire layer (D22) | `charm.land/fantasy` v0.45.2, which pins the Charm fork `github.com/charmbracelet/openai-go v0.0.0-20260921175203-216db9e71b83` (internal version 3.64.0). |
| Local | Ask `master-2` at `8f97953`. H1 and H2 are done. H3 is in progress and not committed: `internal/providers/tokenplan/` (Alibaba Token Plan over the Anthropic wire, fantasy behind it) and `internal/providers/transform.go`. |
| Plan read | `roadmap.md` H4 (lines 222-243) plus D6, D7, D11, D22, H3, H7, H8, H9, H13, H17; inventory rows H-PROV-03, 04, 10, 12, 14, 21, 26, H-AUTH-05. |
| Method | Five research lanes read the files directly. GitNexus was not available to the subagents, so they used `grep`, `git` and file reads. The lead ran GitNexus in the main session for the call graphs (section 9). The decision-changing claims were checked again by the lead against source (marked "verified"). |

Detail reports (every claim there has `file:line`):
- `researcher-261005-1354-h4-pi-openai-completions.md`: Completions end to end, the 26-field compat record, 11 thinking formats, 38 ranked edge cases (20 P0).
- `researcher-261005-1354-h4-pi-openai-responses.md`: Responses end to end, the replay-bug history, providers, 36 ranked edge cases (18 P0).
- `researcher-261005-1354-h4-pi-cross-provider-replay.md`: every `transformMessages` caller and id rule, Go `transform.go` compared to Pi, model switch, 35 test scenarios (S1 to S35).
- `researcher-261005-1354-h4-fantasy-openai-fit.md`: what fantasy can and cannot express for Chat and Responses, H3 code reuse, package layout, depguard.
- `researcher-261005-1354-h4-gap-hunt.md`: the parts around the adapters (auth, model record, switch surface, usage, errors, session id), 17 lane gaps.

Path shorthand: `OC` = Pi `packages/ai/src/api/openai-completions.ts`, `R` = `.../openai-responses.ts`, `S` = `.../openai-responses-shared.ts`, `TM` = `.../transform-messages.ts`, `C:` = `packages/coding-agent/src/`, `F:` = fantasy v0.45.2 module root.

## 1. What H4 must deliver (from the roadmap)

- **Waits on:** D6 (decided B: `openai-completions` with the compat record, and `openai-responses`).
- **Concept:** "compatible" APIs hide real differences. Keep the quirks as data. Replay history across vendors.
- **Owns:** H-PROV-03, H-PROV-04, H-PROV-10 (cross-API part: per-vendor tool-call id rules and replay tests), H-PROV-21 (in memory).
- **Tests:** replay across all chosen APIs (E§24#2, #3); stream completeness for each protocol (E§24#4).
- **Do not rebuild:** `reasoningEffortMap`, `sendSessionIdHeader`, per-level model variants, Gemini CLI, Antigravity.
- **Exit:** one in-memory conversation switches between Anthropic and OpenAI with correct thinking replay.

## 2. How Pi implements it (summary)

1. **Two layers of history conversion.** Layer 1 is the shared `transformMessages` (`TM:64-235`), which Ask already has. Layer 2 is in each adapter: id normalizer, signature format, serializer rules. Pi's replay regressions came from layer 2.
2. **Completions** (`OC`, 1726 lines). `streamSimple` clamps the level (`clampThinkingLevel`, GitNexus), then `buildParams` (`OC:797-1002`) builds the body from the per-model compat record. The record is resolved field by field: model data first, URL and provider detection as fallback (`OC:1585-1726`). There are 11 thinking formats (`OC:875-972`). A thinking block's "signature" holds the reasoning field name (`reasoning_content`, `reasoning`, `reasoning_text`) or a `reasoning_details` JSON array. Replay writes the text back into that field, for the same model only. Cost is computed in `parseChunkUsage`.
3. **Responses** (`R` 415 lines, `S` 809 lines). Always `store:false`. With thinking on, it adds `include:["reasoning.encrypted_content"]` and `reasoning:{effort, summary:"auto"}` (`R:337,361-372`). The whole reasoning item JSON is stored in `thinkingSignature` and replayed as is for the same model (`S:264-268,692`). The text item id and phase are stored as `textSignature` `{v:1,id,phase}` (`S:53-77`). The tool-call id is `call_id|item_id`. Foreign item ids become `fc_<shortHash>` (`S:160-177`). Events are tracked by `output_index`. A tool call that is not finished at completion is an error (`S:763-776`). The shared converter also serves Azure and Codex.
4. **Model switch** (`C:core/agent-session.ts:2430-2450,2565-2637`). Auth check, set the model, append `model_change`, re-clamp the level with `thinkingLevelMap`. History is never rewritten. The next request converts it per target. The loop reads the model per request through `prepareRequest` (`:760-786`).
5. **Stream end.** Each protocol throws a different error text when the terminal event is missing. Completions does this only if `compat.supportsFinishReason` is true, which is the default (cross-provider report, section 8).

## 3. H4 row coverage

| Row | Pi evidence | Fantasy | Gap |
|---|---|---|---|
| H-PROV-03 Completions + compat | `OC`; `types.ts:789-868` | `openaicompat` fits with an Ask-built body in `ExtraBody` (H3 pattern). Limits: `Strict:false` is hard-coded (`F:providers/openai/language_model.go:815`, verified), the role is always `system`, it parses only `reasoning_content` and `reasoning`, the usage hook zeroes usage, and calls without `index` merge. The `openai` model-ID check (`o1/o3/o4/oss/gpt-5`: `max_completion_tokens`, no temperature; `language_model.go:285-328`) cannot be turned off, so `MaxOutputTokens` stays nil and the field goes in `ExtraBody`. | H4-G2, G6, G7, G11 |
| H-PROV-04 Responses | `R`, `S`, `openai-prompt-cache.ts` | Present, but it cannot build a stateless replay. Reasoning parts are always skipped on replay (`F:providers/openai/responses_language_model.go:596-603`, verified). It has no `ExtraBody` (verified by grep). `encrypted_content` is read only at `output_item.added` (`:1067`), not at `done` (`:1149-1162`, verified). Function-call item ids are not exposed. | **H4-G1 (D22)** |
| H-PROV-10 cross-API | `TM`; normalizers `OC:1194-1217`, `S:154-177`, Anthropic `:1290-1292` | n/a (Ask code) | H4-G7 |
| H-PROV-21 in memory | `agent-session.ts:2430-2637` | n/a | H4-G3, G5, G8 |
| H-PROV-26, 12, 14, H-AUTH-05 (OpenAI halves) | `env-api-keys.ts:73-111`; `GM:1060-1081` | n/a | H4-G6 (no owner) |

## 4. Gaps in the plan

Severity: **high** = the exit fails, or a secret or wrong data leaves the process. Lane source in brackets: GH = gap hunt, FF = fantasy fit, RP = replay, OC/OR = adapter lanes, L = lead.

### H4-G1. Fantasy cannot do stateless Responses replay; D22 and roadmap line 197 do not hold for Responses (high, user decision) [FF, verified]

- **The current decision.** D22 (user, 2026-10-01): "One adapter maps Ask messages to `fantasy.Call` and `fantasy.StreamPart` to the H1 `Assembler`. Fantasy types never leave that adapter package." H3 "Wire layer" (`roadmap.md:197`): "H4 reuses the same adapter for OpenAI."
- **The new evidence.** Pi's "correct thinking replay" on Responses needs the stored reasoning item, the message item id and phase, and the `fc_` item id in the next request. Fantasy drops all three. Reasoning is always skipped (`responses_language_model.go:596-603`). Text is replayed with no id or phase (`:565`). A function call is replayed with `call_id` only (`:591`). There is no `ExtraBody` to work around this. The streamed `encrypted_content` is lost because it is read at `added` and real servers send it at `done`. Without the encrypted item, the same-model replay loses the reasoning, and a reasoning item that has no following item gives a 400 (E§13). Chat Completions is not affected: `openaicompat` has `ExtraBody`, so the H3 pattern (Ask builds the whole body) works there.
- **Options** (from the fantasy-fit report, section 4.2):

| Option | What | Trade-off |
|---|---|---|
| A | Fantasy Responses for decoding. A `RoundTripper` rewrites `input`. A wire tap keeps the raw `output_item.done` items. | Keeps D22 on paper. Half of the decoder is rebuilt in a tap. Fragile. |
| B | In the same adapter package, call `openai-go/responses` directly (already a fantasy dependency). Ask owns the body, ids and errors. | Simplest correct path. Changes D22 for one API. openai-go types stay inside the package, so the "types never leave" rule still holds. |
| C | Use fantasy as is, with `store:true` and `previous_response_id`. | No stateless replay. History lives on the server. This conflicts with Ask's local log (H8) and with Pi parity. |
| D | Defer stateless replay. | The H4 exit ("correct thinking replay") is not met for Responses. |

- **What I would pick:** B, because it is the only option that meets the exit without rebuilding the decoder. This changes a decision you made, so it is your call.

### H4-G2. The H4 target provider, model and key are not chosen (high, user decision) [GH]

The roadmap names no OpenAI target. Facts:
- Pi's `openai` provider is Responses only (`AI:providers/openai.ts:1-24`), default `gpt-5.5` (`C:core/model-resolver.ts:22-28`).
- Pi serves Alibaba Token Plan on `openai-completions` at `.../compatible-mode/v1`, on the same host as the H3 Anthropic route (`AI:providers/qwen-token-plan.ts:8-12`; compat `thinkingFormat:"qwen"`, `supportsDeveloperRole:false`, `supportsStore:false`). It is not verified that `deepseek-v4.1-flash` is served there.
- Pi's native DeepSeek record is on Completions with `thinkingFormat:"deepseek"` and `requiresReasoningContentOnAssistantMessages:true` (`GM:3099-3115`).
- The Token Plan Anthropic wire and the Token Plan Completions wire carry the same model id but a different `API`. `sameModel` compares provider, API and id (`transform.go:278-280`), so a switch between them is a real cross-model replay. This is a cheap test that needs only one key.
- Options: A = Token Plan Completions plus OpenAI `gpt-5.5` on Responses (needs an OpenAI key). B = Token Plan Completions, with Responses proven by offline golden tests only. C = DeepSeek native plus OpenAI (two new keys). The gap-hunt report recommends A after one probe of `/compatible-mode/v1`. The probe spends your key, so it waits for you.
- Note: lane OC says OpenAI's own Chat Completions returns no reasoning text. That is the lane's background knowledge, not verified. It does not change the P0 lists here.

### H4-G3. `--api-key` goes to the wrong host after a provider switch (high, security) [GH, verified]

`cmd/tui/headless.go:146` sets one `Options.APIKey` for the whole agent. `internal/agent/loop_stream.go:32-41` copies it on every request. The `GetAPIKey` hook overrides it only when the hook is set and returns a non-empty key. Headless sets no hook. After a switch, the first provider's key goes to the second host. Pi binds `--api-key` to the initial provider only (`C:main.ts:827-834`; `runtime-credentials.ts:12-34`). Fix in H4: resolve keys per provider in a `GetAPIKey` hook. Test with two `httptest` servers that the second server never sees the first key.

### H4-G4. The OpenAI SDK reads `OPENAI_*` env variables on its own (high, security) [GH, verified]

The openai-go fork's `NewClient` always applies `DefaultClientOptions()` (`client.go:112-116`). These read `OPENAI_BASE_URL`, `OPENAI_API_KEY`, `OPENAI_ADMIN_KEY`, `OPENAI_ORG_ID`, `OPENAI_PROJECT_ID`, `OPENAI_WEBHOOK_SECRET` and `OPENAI_CUSTOM_HEADERS` (`client.go:72-98`). Fantasy sets the key and base URL only when they are non-empty (`F:providers/openai/openai.go:170-175`). So an org id, a project id or custom headers go to Token Plan, DeepSeek or OpenRouter. With an empty key, `OPENAI_API_KEY` goes to a third-party host. The fork has an opt-out (`requestconfig.WithEnvironmentDefaultsDisabled`), but it is internal API (`requestconfig.go:168-174`) and no public `option` exposes it. This is the same class as H3-G2 (`ANTHROPIC_AUTH_TOKEN`). Fix: always pass a non-empty key and the record's base URL, override or delete the org, project and custom headers, and test with every `OPENAI_*` variable set.

### H4-G5. Ask has no way to switch the model (high, partly decision) [GH, RP]

`LoopConfig` has one `Stream` and one `Model` (`internal/agent/loop_run.go:22-27`). `Agent` has no `SetModel` and no `SetThinkingLevel` (`agent.go:63-135`). `openProvider` returns one stream function (`cmd/tui/headless_faux.go:70-79`). `set_model` and `/model` are in H13. The loop already has the per-request `PrepareRequest` hook (`loop_run.go:224-253`), which is the same as Pi's `prepareRequest`, so the loop needs no change. Smallest fix: a registry that picks the stream function by `Model.API`, plus `Agent.SetModel` and `SetThinkingLevel` (auth check first, then re-clamp). Show the exit with an in-memory test using two `httptest` servers (scenario S29). Do not add a CLI flag. Open: do you want a visible demo before H13?

### H4-G6. The model record lacks the fields H4 needs, and four inventory rows have no OpenAI owner (high) [GH, RP]

- `internal/providers/model.go:5-17` has no `BaseURL`, `Headers`, `ThinkingLevelMap`, `Cost` (with tiers), `Compat` (26 Completions fields, 10 Responses fields) or `SamplingParams`. The Token Plan base URL is a package constant (`tokenplan/provider.go:25`). `README.md` names `compat.go`, but the file does not exist.
- These rows have no owner for their OpenAI half: H-AUTH-05 (H3 owns Anthropic, and H17 owns "the other vendors", `roadmap.md:570`), H-PROV-26 (H3 owns Anthropic quirk data), H-PROV-14 (H17, but H-PROV-04 in H4 lists `prompt_cache_key` and retention, so the two rows conflict), H-PROV-12 (the OpenAI `thinkingLevelMap` data).
- The roadmap lists `reasoningEffortMap` and `sendSessionIdHeader` under "do not rebuild". It does not name what replaced them, which H4 must build: `thinkingLevelMap` and `compat.sessionAffinityFormat`.
- Fix: add all of these to the H4 "Owns" list. This is not a decision.

### H4-G7. Layer-2 id rules, `shortHash` and collisions (high) [RP, OR]

- Ask has only a private Anthropic normalizer in `tokenplan` (`provider.go:288-300`), and it counts runes, not UTF-16 units. H4 needs three shared pure functions of type `NormalizeToolCallID`. **Completions:** split at the first `|`, join as `call_item`; if the result is over 40, use 31 chars, `_` and an 8-char hash. Provider `openai` also caps plain ids at 40 (`OC:1194-1217`). **Responses:** `normCall|fc_<hash>` for foreign item ids, only for providers in the allowed set; other providers get `|` turned into `_` (`S:154-177`). **Anthropic:** `[a-zA-Z0-9_-]`, 64 chars.
- `shortHash` is missing. It must be deterministic, for cache stability. It does not need to match Pi bit for bit, because no session file is shared.
- Serializer rules after the normalizer, for Responses (`S:258-330`): drop the item id when the model differs, or when its prefix does not match the item type (`fc_` or `ctc_`; `bc2d8dc1c`, after the pin). Keep `namespace` only for the same model. Use the `msg_pi_{idx}[_k]` fallback ids, and hash ids over 64.
- Collisions: only Pi's Mistral normalizer guards against two ids that map to one id. With such a collision, Go `pairMessages` marks only the first pending call done and adds a second synthetic result with the same id (`transform.go:325-330`, verified). Pi's output is also invalid in this case. Decision: add a collision guard in the shared normalizer (recommended), or follow Pi.
- A Completions thinking "signature" holds a field name (`"reasoning_content"`). Only layer 1 stops it from reaching an Anthropic `signature` field. A test must lock this in (S8).

### H4-G8. Thinking level: default, clamp, `off`, and the switch rule (medium, partly decision) [GH, RP]

- Ask has no clamp. `medium` is the default only for Token Plan (`headless.go:130-133`). In Pi, every OpenAI adapter clamps inside `streamSimple` (GitNexus: `clampThinkingLevel` is called by the Completions, Responses, Azure, Codex, Google and Mistral `streamSimple`, but not by the Anthropic one). Ask can clamp once, above the adapters, when it sets the model.
- Responses `off`: Pi sends `reasoning:{effort: map.off ?? "none"}`, and no field at all when `map.off === null` (`R:361-377`).
- Switch rule: Pi takes the settings default, then the current level, then `medium` (`agent-session.ts:2622-2634`). It does not store the requested level. With no settings (H6 comes later), `high`, then a non-reasoning model, then back gives `off` (S30). E§13 says the level must survive this. Decision: follow Pi, or keep a separate requested level (recommended, small deviation).

### H4-G9. Replay storage slots (high) [GH, OR]

The slots exist: `Thinking.ThinkingSignature`, `Text.TextSignature`, `ToolCall.ID` and `AssistantMessage.ResponseID` (`pkg/protocol/content.go:28-55`, `message.go:169`). The adapter must write the reasoning item JSON, the `{v:1,id,phase}` text JSON and the `call|item` id into them, and read them back. Rule from Pi's history (`8fc2b7682`, reverted by `b4e7d5c44`): keep a thinking block that has a signature even when its text is empty. The replay key is the signature. Test: a round trip through a stored and re-read message, then replay to the same model, then to another model (S1, S10, S21, S27).

### H4-G10. Mid-conversation system messages and tool deltas: per-wire rendering has no owner (medium) [L]

GitNexus shows that `renderSystemMessageUpdate` is called by the message converter of every adapter (Anthropic, Completions, Responses-shared, Mistral). `appendSystemToolAdditions` is called by Responses (and uses `shortHash`). H8 pulls H-LOOP-15 for the log entries (`roadmap.md:327`). No phase names the rendering on each of the three wires. Today Ask sends only a leading system message with `ToolsAdded` (`internal/providers/convert.go:30-41`), and fantasy drops later system messages (H3 report, G15). Lane OC marks this "later" (#32). Decision: add golden tests for it in H4, or name H8 as the owner for all three wires.

### H4-G11. Usage, cache read and cost for OpenAI (medium, decision) [GH, OC, FF]

- Completions: `input = prompt - cacheRead - cacheWrite`. There are three cache-read field names (`prompt_tokens_details.cached_tokens`, DeepSeek `prompt_cache_hit_tokens`, Kimi top-level `cached_tokens`). Pi computes `totalTokens` itself (`OC:1511-1552`). Responses uses the provider's `total_tokens` (`S:559-578`).
- Fantasy reads only `cached_tokens` (`language_model_hooks.go:244-259`), so DeepSeek cache hits count as full input. Its usage hook returns zero usage for every chunk without usage, so a later chunk erases the real usage (`:263-266`). Ask must replace the hook (`WithLanguageModelStreamUsageFunc`).
- Service tier multipliers (0.5, 2, 2.5 for `gpt-5.5`) use the tier echoed in the response (`R:389-415`). Fantasy has only the requested tier.
- Decision (same as H3-G9): H4 carries correct token counts and a plain price table. H9 adds tiers, the echoed tier and the response model.

### H4-G12. Error and stream-end data that H9 needs (medium) [GH, RP]

H4 must keep the status and body in the error text, and map a missing terminal event to the H1 `ErrStreamIncomplete`. The Pi texts: Completions `Stream ended without finish_reason` (`OC:697`); Responses `...ended before a terminal response event` and `...completed with an unfinished tool call` (`S:743-775`); `Response incomplete: {reason}`. Only `max_output_tokens` means `length`. A Responses `failed` or `error` event becomes a plain `fantasy.Error` with no status (`responses_language_model.go:1336-1349`). Fixtures needed for H9: `exceeds the context window`, `range of input length should be` (Token Plan), `context_length_exceeded`, `insufficient_quota`, `model is at capacity` (`3874b3e98`). SDK retries are off in fantasy (`openai.go:168`). Add a lock-in test as H3 did.

### H4-G13. No session id in headless, so no prompt cache key (medium) [GH]

`cmd/tui` sets no `SessionID`, so there is no `prompt_cache_key` (`R:334`, clamped to 64, `openai-prompt-cache.ts`) and there are no affinity headers. Pi always has an id (`C:core/sdk.ts:411`). Fix: one UUID per process in headless, replaced by H8. Completions sends the key only for `api.openai.com` or for long retention on a vendor that supports it (`OC:821-823`).

### H4-G14. The H4 test list is two lines (medium) [all lanes]

Pi's only offline cross-provider tests are `transform-messages-copilot-openai-to-anthropic.test.ts` and `openai-responses-foreign-toolcall-id.test.ts`. Its cross-provider handoff test only checks that no error comes back, and it is live. Third-party servers accept invalid input (the H3 probe proved this for orphan tool calls), so live acceptance proves little. The H4 list needs offline golden-payload tests for each compat flag, the 35 replay scenarios (S1 to S35; kinds U, G, I, L), stream end for each protocol (S33), usage fixtures, and unfinished calls without `output_index` (llama.cpp, `1b2aa0ca0`). Live tests stay manual and env-gated, because the Token Plan terms limit it to interactive use.

### H4-G15. Package shape and depguard (medium, user decision) [FF, GH]

`internal/providers/tokenplan/` is a package per provider. D22 says "that adapter package" (one package). `internal/providers/README.md` says flat `<vendor>.go` files. `.golangci.yml:70-79` (verified, in the current tree) exempts only `tokenplan/**` and denies only fantasy and the Anthropic SDK. Options: (1) one fantasy-facing package with one file per wire and shared fold, idle and error code, with Token Plan reduced to record data; (2) one package per API with a fourth shared package; (3) keep `tokenplan` and add `openai` next to it. The fantasy lane ranks (1) first. In every option, add `github.com/charmbracelet/openai-go` to the deny list. You own the dewee layout, so this is your call.

### H4-G16. Smaller items (no phase change)

- Env names (decision): `ASK_OPENAI_API_KEY` first, then `OPENAI_API_KEY`, the same two-name pattern as H3 (`tokenplan/provider.go:30-31`). The `ASK_` name also keeps the SDK-ambient variables away (H4-G4).
- A tool-call delta with no id: Pi leaves `""` (`OC:509`). Fantasy makes `tool-call-N` (`language_model.go:559`), and that repeats every turn. The adapter must pass `""` so the `Assembler` makes a unique id.
- `samplingParams` merge (last wins): keep the field and build the merge in H7 (`models.json`).
- Strict tool schemas are off by default on both APIs (`supportsStrictMode` default false), so H4 needs no strict conversion. `constrained-sampling.ts` converts schemas to strict form; it is not sampling.
- The ChatGPT sign-in heuristic (`R:40-47`): do not copy it (no ChatGPT OAuth in M1).
- `requiresReasoningContentOnAssistantMessages`: P0 only if the target is DeepSeek, Kimi or Xiaomi (H4-G2).
- Lone surrogates: Go `encoding/json` already writes U+FFFD (H3-G15). No sanitizer is needed.
- `ProviderError.RequestBody` contains the `Authorization` header (`F:providers/openai/error.go:36`). Never log or serialize the whole error.

## 5. H3 fix found during this analysis (do now, in the uncommitted H3 code)

`internal/providers/tokenplan/fold.go:131` passes `part.ToolCallInput` to `ToolDelta` (verified). Fantasy's Anthropic provider fills `ToolCallInput`. The OpenAI Chat and Responses providers fill `Delta` (`F:providers/openai/language_model.go:546`, verified; `responses_language_model.go:1172`). If the fold is shared, it must read `cmp.Or(part.Delta, part.ToolCallInput)`. Otherwise OpenAI tool arguments arrive empty.

## 6. Proposed text corrections (not applied)

1. INV H-PROV-03: "never URL heuristics". Pi merges model data with a URL and provider fallback (`OC:1585-1726`). Decide: an explicit record only, or a small fallback for custom endpoints (H7).
2. INV H-PROV-04: the provider list also has `opencode`, `opencode-go` and `cloudflare-ai-gateway`. "Errors when `output_index` is missing" is wrong. The real rule is an error when a tool call is not finished at completion (`S:763-776`); a delta with no index is skipped (`S:448-454`). "Replay bugs fixed 5 times": the git log shows six main fixes (`d327b9c76`, `b2548ce48`+`b21b42d03`, `8fc2b7682`/`b4e7d5c44`, `d1fb34bc8`, `8c9dbffa3`, `bc2d8dc1c`) and several related ones.
3. INV H-PROV-21: "foreign thinking becomes tagged text" should be "plain text". "Cache lost" has no code in Pi. The lines are now `:2430-2450` (set) and `:2472-2553` (cycle).
4. INV H-PROV-10: no `<thinking>` tags (already corrected for H3; it applies to OpenAI replay too).
5. D11 "no jitter": true at the agent level only. The provider-level retry has jitter (`utils/provider-retry.ts:66`), but it is off while provider `maxRetries` is 0.
6. Roadmap line 197, "H4 reuses the same adapter for OpenAI": true for Chat, false for stateless Responses (H4-G1).
7. Roadmap H4 "Do not rebuild": add what replaced those items (`thinkingLevelMap`, `compat.sessionAffinityFormat`), which H4 must build.
8. INV env-key line numbers: `env-api-keys.ts` now has 195 lines (`getApiKeyEnvVars` `:73`, the map `:84-111`).
9. The D22 retry note is stale (already in H3 report section 5, item 5). Fantasy's openai provider also sets `WithMaxRetries(0)`.
10. Pi pin: moving from `2bbfcca4` to `4c6fb7cfe` costs one replay rule (`bc2d8dc1c`) and one retry pattern (`3874b3e98`) for H4.

## 7. Dependency matrix (Pi component to Ask)

| Pi component | Ask target | Status |
|---|---|---|
| `transformMessages` (layer 1) | `internal/providers/transform.go` | EXISTS (matches Pi; collision divergence, H4-G7) |
| Normalizers (Completions, Responses, Anthropic) + `shortHash` | `internal/providers` (pure functions) | NEW (the Anthropic one is private in `tokenplan`) |
| `OpenAICompletionsCompat`, Responses compat, `thinkingLevelMap`, cost tiers | `providers.Model` + `compat.go` | CONFLICT (fields missing) |
| `clampThinkingLevel` | agent or CLI path | NEW |
| Completions `buildParams` + `convertMessages` + `convertTools` | adapter, Ask-built body in `openaicompat` `ExtraBody` | NEW |
| Completions SSE parsing | fantasy `openaicompat` + own usage, finish-reason and stream-extra hooks | EXISTS (fantasy), NEW (hooks) |
| Responses `convertResponsesMessages` + `processResponsesStream` | adapter; fantasy cannot build it (H4-G1) | NEW, path depends on the decision |
| StreamPart to Assembler fold, idle timer, error mapping, witness | `tokenplan/fold.go`, `provider.go`, `witness.go` | EXISTS (generic; fix `fold.go:131`; witness key as a parameter) |
| `setModel`, `prepareRequest` | `Agent.SetModel` + registry; `PrepareRequest` hook | NEW; hook EXISTS |
| Per-provider `--api-key` (`runtime-credentials.ts`) | `GetAPIKey` hook in headless | NEW |
| Session id for the cache key | headless UUID, H8 log later | NEW |
| `renderSystemMessageUpdate`, `appendSystemToolAdditions` | per-wire serializers | NEW, owner open (H4-G10) |
| `calculateCost` in each adapter | adapter + `pkg/protocol/usage.go` (micro-USD) | EXISTS (type), NEW (formula, prices) |
| `charmbracelet/openai-go` | `go.mod` (indirect through fantasy) | NEW at the first import |

## 8. Decision matrix (in the order to answer)

| # | Decision | Pi's way | Ask options | What I would pick |
|---|---|---|---|---|
| 1 | Target provider, model and key (H4-G2) | `openai` = Responses `gpt-5.5`; Token Plan on Completions | A, B or C (section 4) | A, after one probe of `/compatible-mode/v1`, which needs your approval because it spends your key |
| 2 | Responses wire layer (H4-G1, changes D22) | Own HTTP, stateless replay | A tap, B direct openai-go, C `store:true`, D defer | B; your call, because it changes D22 |
| 3 | Env names (H4-G16) | Plain `OPENAI_API_KEY` | `ASK_` first, then plain; or plain only | `ASK_` first, then plain |
| 4 | Thinking level on a switch (H4-G8) | Settings default, then current level | Follow Pi, or a separate requested level | A separate requested level |
| 5 | Package shape (H4-G15) | n/a | (1), (2) or (3) | (1); your call (dewee layout) |
| 6 | Id collision policy (H4-G7) | Only Mistral guards | Guard all, or follow Pi | Guard all |
| 7 | Mid-conversation system rendering owner (H4-G10) | Every adapter | H4 tests or H8 | H4 golden tests, H8 wires the entries |
| 8 | Cost scope (H4-G11) | Full in the adapter | Plain table in H4, tiers in H9 | Plain table in H4 |
| 9 | Switch demo (H4-G5) | `/model` | Test only until H13, or a visible demo | Test only |
| 10 | Pi pin | n/a | Keep `2bbfcca4` or move to `4c6fb7cfe` | Move (small cost) |

## 9. Risk score

**High (8/10).** H3 was 7/10.

- H4-G1 is a verified conflict with a decision you made (D22). Update 2026-10-05: the user chose to patch fantasy, and the fork already fixes the two largest gaps. F1 to F4 remain, and the fork is not wired into Ask yet. With this, the score is 7/10.
- H4-G3 and H4-G4 can send a key, an org id or custom headers to a third-party host.
- Pi's Responses replay regressed at least six times, all in layer 2 (H4-G7, G9).
- The exit needs work in four places that the roadmap does not list: the model record, the switch API, per-provider keys and the normalizers.
- H3 is not committed yet. H4 builds on its fold, idle and error code.

## 10. GitNexus check (lead, main session)

- **Callees of `openai-completions.ts` outside the file:** `transformMessages`, `clampThinkingLevel` (in `streamSimple`), `buildBaseOptions`, `calculateCost` (in `parseChunkUsage`), `shortHash` (in `normalizeToolCallId`), `clampOpenAIPromptCacheKey`, `resolveTranscript` and `renderSystemMessageUpdate` (mid-conversation system messages), constrained-sampling helpers, Copilot header helpers, `retryProviderRequest`, `formatProviderError` and `normalizeProviderError`, and the three SDK hooks in `C:core/sdk.ts` (`transformProviderPayload`, `handleProviderResponse`, `handleProviderStreamEvent`).
- **Callees of `openai-responses*.ts`:** the same set plus `buildForeignResponsesItemId`, `backfillReasoningSignatures`, `appendSystemToolAdditions` (all three use `shortHash`), and `finalizeResponse` calling `resolveCodexServiceTier`, which means the shared file depends on Codex code.
- **Callers of `openai-responses-shared.ts`:** `azure-openai-responses.ts` (`buildParams`, `stream`) and `openai-codex-responses.ts` (`buildRequestBody`, `processWebSocketStream`, `processStream`), plus eight test files (`openai-responses-foreign-toolcall-id`, `-message-id`, `-terminal-event`, `-partial-json-cleanup`, `-namespace`, `-empty-tool-result`, `azure-openai-responses-reasoning-replay`, `constrained-sampling`). A change to the shared converter affects three APIs.
- **Callers of `clampThinkingLevel` in `packages/ai`:** the `streamSimple` of Completions, Responses, Azure, Codex, Google, Vertex and Mistral. The Anthropic adapter is not a caller. Ask can clamp once above the adapters.
- **Callers of `renderSystemMessageUpdate`:** Anthropic, Completions, Responses-shared and Mistral converters. This is the source of H4-G10, which no lane flagged.
- **Callers of `transformMessages`:** six adapters (Anthropic, Bedrock, Completions, Google-shared, Mistral, Responses-shared). This matches the replay lane.

## Decisions recorded

- **H4-G2 target (user, 2026-10-05): option A.** Completions target: Alibaba Token Plan `/compatible-mode/v1` (same key as H3). Responses target: OpenAI `gpt-5.5`. The probe of `/compatible-mode/v1` was approved and run (next section). Still open: the OpenAI key for live Responses tests (in the analysis shell, `OPENAI_API_KEY` is not set).

- **H4-G1 Responses wire layer (user, 2026-10-05): patch fantasy.** This is a new option E, not one of A to D: "sửa fantasy, chúng ta có source mà" (fix fantasy, we have the source). D22 stays as written: all wires go through fantasy, and the gaps are fixed in fantasy itself.

### State of the fantasy fork (checked 2026-10-05)

- **Location:** `/Users/dale/Desktop/workspace/opensources/fantasy`, remote `git@github.com:kevinle128/fantasy.git`, branch `fix/openai-reasoning-replay-stream-completion` (pushed). Base `82d42a7` is upstream `main`, a little after v0.45.2: `providers/openai/*.go` and `providers/anthropic/anthropic.go` are byte-identical to v0.45.2, and `agent.go` differs.
- **Ask does not use the fork yet.** `go.mod:6` is `charm.land/fantasy v0.45.2` with no `replace`.
- **Already fixed in the fork:**

| Commit | Fix | Closes |
|---|---|---|
| `bff4512` (2026-10-01) | With `store:false`, reasoning parts are replayed inline as `reasoning` items (id, summary, `encrypted_content`), once per item id. Streamed reasoning metadata is taken from `output_item.done`, not `added`. An empty summary list is kept. There is a new test `responses_reasoning_replay_test.go`, and the cassettes were re-recorded. | Two of the four Responses gaps: reasoning skipped on replay, and `encrypted_content` lost in the stream |
| `76fdec8` (2026-10-01) | `WithLanguageModelRequireFinishReason()`: a Chat stream that ends without `finish_reason` is an error, even when the tool arguments are valid JSON | Pi's `supportsFinishReason` default (stream completeness for Completions) |
| `9a5405c` (2026-10-02) | Anthropic: large numbers in tool arguments are kept exactly | H3 |

- **Still missing in the fork (verified at fork HEAD):**

| # | Gap | Where (fork HEAD) | Needed for |
|---|---|---|---|
| F1 | Assistant text is replayed with no message item id and no `phase` | `responses_language_model.go:566` | Pi keeps both for the same model (`S:271-289`). Whether OpenAI rejects a replay without them is not verified. It needs a live test with an OpenAI key. |
| F2 | The function-call item id (`fc_...`) is not exposed on the stream and not replayed; `call_id` only | `:592`; stream emits `CallID` only | Pi pairs `rs_` with its `fc_` item for the same model (`S:292-305`, bug `d327b9c76`). Whether it is required is not verified. |
| F3 | Responses has no `ExtraBody` (no general escape hatch) | grep shows none in `providers/openai/responses*.go` | `prompt_cache_retention`, `prompt_cache_options` and future fields |
| F4 | A Responses `failed` or `error` event becomes a plain `fantasy.Error` with no status; the raw `incomplete_details.reason` is lost | `:1336-1349`, `:967-985` | H9 classification (H4-G12) |
| F5 | Chat: `Strict: param.NewOpt(false)` is hard-coded | `language_model.go:833` | Not needed for H4 (strict is off by default). Ask can override it with `ExtraBody` `tools`. |
| F6 | Chat: the default usage hook returns zero usage for chunks without usage | `language_model_hooks.go:263-266` | Ask can replace the hook (`WithLanguageModelStreamUsageFunc`). A fork fix is optional. |
| F7 | Chat: calls without `index` merge into index 0; `reasoning_text` is not parsed | `language_model.go:540-592`; `openaicompat/provider_options.go` | Not needed for the Token Plan target (the probe shows `index` and `reasoning_content`) |
| F8 | Responses: the `service_tier` echoed in the response is not exposed | `responses_language_model.go:275,354-371` (request tier only) | Service tier pricing (cost decision, question 8) |

- **The H4 adapter still needs, on the Ask side:** pass `Store:false` and `Include:["reasoning.encrypted_content"]` (fantasy does not add the include by default), and serialize the reasoning metadata into `Thinking.ThinkingSignature` and back (H4-G9).

## Live probe of the Token Plan Completions route (user-approved, 2026-10-05)

Six `curl` calls to `https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1/chat/completions`. The key came from `ALIBABA_TOKEN_PLAN_API_KEY` through `Authorization: Bearer` and was never printed. The raw outputs are in the session scratchpad (`h4-probe/p1..p6.out`), not in the repo.

| # | Probe | Result |
|---|---|---|
| p1 | Stream, no thinking field, `stream_options.include_usage` | HTTP 200, time to first byte 1.2 s. **`deepseek-v4.1-flash` is served on this route.** The model thinks by default. Thinking arrives in `delta.reasoning_content`, and every chunk also carries `"content":""`. One chunk holds both the last reasoning delta and the first content delta. `finish_reason:"stop"` comes in its own chunk, and then usage comes in a final chunk with `choices:[]`, then `[DONE]`. |
| p1 | Usage shape | `prompt_tokens`, `completion_tokens`, `total_tokens`, `prompt_tokens_details{cached_tokens,text_tokens}`, `completion_tokens_details{reasoning_tokens,text_tokens}`. Reasoning is a subset of completion (17 of 19). Cache hits are in `prompt_tokens_details.cached_tokens`, which fantasy reads. There is no `prompt_cache_hit_tokens`. |
| p2 | `enable_thinking:false` (Pi's `qwen` format) | HTTP 200. No `reasoning_content` field and no `reasoning_tokens`. **The `qwen` thinking format works for `off`.** Small usage inconsistency: `completion_tokens` 1, `text_tokens` 2. |
| p3 | `reasoning_effort:"high"` and two tools, parallel | HTTP 200. Thinking, then two tool calls. Each call has `index` (0, 1) and an id `call_<24 hex>` in its first delta only. The arguments stream in pieces. `finish_reason:"tool_calls"`. `reasoning_effort` is accepted. It is not proven that it changes the thinking depth. |
| p4 | Replay: `system` role, `max_completion_tokens`, assistant `tool_calls` with an Anthropic-style id `toolu_01ABCdef_from_anthropic`, no `reasoning_content`, then a `tool` result | HTTP 200, a correct answer. The foreign id, the missing `reasoning_content` and the `max_completion_tokens` field are all accepted. The model did not think on this turn (no `reasoning_tokens`). |
| p5 | Unknown model id | HTTP 404 with an OpenAI-shaped body `{"error":{"message":"Model not exist.","type":"invalid_request_error","code":"model_not_found"}}`. This is not the shape of the Anthropic route (H3 probe: HTTP 400, `InvalidParameter`). |
| p6 | `developer` role | HTTP 400 `invalid_parameter_error`: "developer is not one of ['system', 'assistant', 'user', 'tool', 'function']". **This confirms Pi's `supportsDeveloperRole:false` for Token Plan.** |

What this changes:
- **H4-G2:** option A works for the Completions half. The record is `provider: alibaba-token-plan` (or a separate id for the Completions route), `api: openai-completions`, base URL `.../compatible-mode/v1`. The compat values match Pi's `qwen-token-plan`: `thinkingFormat:"qwen"`, `supportsDeveloperRole:false`, `supportsStore:false` (not probed). The reasoning field is `reasoning_content`.
- **Real cross-wire test with one key:** the same model id on the Anthropic route (H3) and on this route gives a true cross-model replay. Anthropic thinking has `signature:""` (H3 probe), and Completions stores the field name `"reasoning_content"` as its "signature". Scenario S8 can run live.
- **The server is lenient** (p4 accepts a foreign id and no `reasoning_content`; the H3 probe showed the same on the Anthropic route). Live acceptance proves little. Keep the assertions on the request JSON (H4-G14).
- **H4-G11:** fantasy's single cache-read field is enough for this provider. The usage-zeroing hook defect still matters, because usage comes in a separate final chunk.
- **H4-G12:** error bodies differ by route on the same host. H9 must classify both shapes.
- **H4-G16:** tool ids always arrive with the first delta here. The missing-id case is for other servers.
- Not probed: `supportsStore` (`store:false` was in p6, but p6 failed on the role first), whether `reasoning_effort` changes the output, a 429 or 5xx shape, and an interrupted stream.

## Unresolved questions

Answer one per turn, in this order:

1. **Target (H4-G2):** decided A (above). The Completions half is confirmed by the live probe. Open: does an OpenAI key exist, and may the analysis spend one Token Plan call on `/compatible-mode/v1` with `deepseek-v4.1-flash`?
2. **Responses wire layer (H4-G1):** decided: patch fantasy (above). **Fork use (user, 2026-10-05): option (a)**, `replace charm.land/fantasy => github.com/kevinle128/fantasy <pseudo-version>` in `go.mod`. A local `go.work` is allowed during fork work. Upstream (user, 2026-10-05): option (a), send each fix as a PR to `charmbracelet/fantasy` once it has tests and works in Ask; remove the `replace` when upstream accepts it. Still open: which of F1 to F4 go into the fork before H4.
3. **Env names (H4-G16):** decided (user, 2026-10-05): option (b), plain `OPENAI_API_KEY` only, as Pi does. Note: H3 reads two names (`ASK_ALIBABA_TOKEN_PLAN_API_KEY`, then `ALIBABA_TOKEN_PLAN_API_KEY`), so the two phases differ. H3 is not changed by this decision. The adapter must still block the other `OPENAI_*` variables (H4-G4).
4. **Thinking level on a switch (H4-G8):** decided (user, 2026-10-05): option (a), follow Pi. Store only the clamped level. Known effect until H6 settings exist: `high`, then a non-reasoning model, then back gives `off` (S30). The H4 test for S30 must expect `off`, and the exit demo must not route through a non-reasoning model unless that result is wanted.
5. **Package shape (H4-G15):** decided (user, 2026-10-05): "fantasy là ở tầng infrastructure, các adapter sử dụng cái gì là việc của adapter" (fantasy is infrastructure; what an adapter uses is the adapter's business). The user confirmed this restatement:
   - One adapter for each wire API, named for what it serves: `internal/providers/anthropic/` (`anthropic-messages`) and `internal/providers/openai/` (`openai-completions`, `openai-responses`).
   - Token Plan is data (provider and model records, base URL, key env name), not a package. The current `tokenplan` package becomes the `anthropic` adapter.
   - Each adapter chooses its own libraries. Core files in `internal/providers/*.go` and packages outside providers must not import fantasy. depguard allows fantasy only in the adapter packages under `internal/providers/<api>/`.
   - The shared fantasy plumbing (StreamPart-to-Assembler fold, idle timeout, error mapping, witness) goes in one infrastructure package, for example `internal/providers/fantasykit/`, which the adapters import.
   - Follow-ups: update `internal/providers/README.md` ("File names") and `docs/ask-architecture-reference.md:115` from flat `<vendor>*.go` files to adapter sub-packages; add `github.com/charmbracelet/openai-go` to the depguard deny list for core.
6. **Id collisions (H4-G7):** decided (user, 2026-10-05): option (b), follow Pi. No collision guard. Accepted risk: two long foreign ids with the same 64-char prefix map to one id on Anthropic (and similar on Completions and Responses), and `pairMessages` then adds a second synthetic result with that id (`transform.go:325-330`). Do not write a test that expects distinct ids (S7).
7. **Mid-conversation system rendering (H4-G10):** decided (user, 2026-10-05): option (a). H4 owns the per-wire rendering of mid-conversation system messages and tool deltas (Pi `renderSystemMessageUpdate`, Responses `appendSystemToolAdditions`) for Anthropic, Completions and Responses, with golden tests. H8 only creates the entries (H-LOOP-15). This replaces the current Ask behavior of sending one leading system message (`convert.go:30-41`) and the fantasy drop of later system messages.
8. **Cost (H4-G11):** decided (user, 2026-10-05): "làm giống PI" (do it like Pi), option (c). H4 does the full Pi cost path in the adapters: prices and `tiers` in the model record (OpenAI long-context tier above 272000 input tokens: input x2, output x1.5, cache read x2); usage split (`input = input_tokens - cached - cache_write`); one shared `calculateCost` (tier chosen by `input + cacheRead + cacheWrite`, highest threshold passed; Anthropic 1h cache write at 2x input); and the Responses service tier multiplier (flex 0.5, priority/fast 2, 2.5 for `gpt-5.5`), using the tier echoed in the response first. Follow-ups:
   - The pricing part of H-RETRY-07 moves from H9 to H4. The roadmap text must change.
   - Add fork fix **F8**: expose the response `service_tier` on the Responses finish metadata (fantasy keeps only the requested tier).
   - Token Plan records have cost 0 (subscription), as Pi does for such providers.
   - Prices for `gpt-5.5` must come from OpenAI's price page; the Pi catalog data is git-ignored.
   - Ask stores cost as micro-USD integers (`pkg/protocol/usage.go:13-25`); Pi uses USD floats. Convert the price table.
9. **Switch demo (H4-G5):** decided (user, 2026-10-05): option (a), tests only, as Pi does at the agent level. An offline Go test with two `httptest` servers (Anthropic-shaped and OpenAI-shaped) and one agent that switches model between turns (S29), modeled on Pi's `test/suite/agent-session-model-extension.test.ts` (faux providers: `setModel` event, re-clamp, fallback to the current level). Plus one env-gated live test on Token Plan that switches between the Anthropic route and the Completions route with the same key, modeled on Pi's `packages/ai/test/cross-provider-handoff.test.ts`. No CLI flag or demo command. The user-facing switch stays in H13 (`set_model`, `/model`) and H17 (`cycle_model`).
10. **Pi pin:** decided (user, 2026-10-05): option (a), move the reference to `4c6fb7cfe` (v1.0.1). H4 ports `bc2d8dc1c` (Responses item id prefix rule) and `3874b3e98` (retry pattern `model is at capacity`). Old line numbers in the inventory are fixed when each phase touches them. The roadmap header ("Reference: Pi 0.99.1, commit `2bbfcca4`") must change.
11. **Apply the corrections:** decided (user, 2026-10-05): option (a). Applied: roadmap revision 5 (header, D11, D22, H1, H3, H4, H9, H17, section 8, new section 9), inventory section 19, `plan.md` decision log, `internal/providers/README.md`, `docs/ask-architecture-reference.md` (sections 3.1 and 9).
12. Still unverified: whether `ExtraBody` `messages` fully replaces the stub prompt for OpenAI Chat, and whether `param.Omit` set through `ExtraBody` survives fantasy's later assignments. Both need a golden test before H4 depends on them.
