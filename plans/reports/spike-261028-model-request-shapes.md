# Spike: model request shapes and model-list endpoints

Date: 2026-10-28 (Asia/Saigon). Plan: [account model discovery](../261028-1310-account-model-discovery/plan.md), [phase 1](../261028-1310-account-model-discovery/phase-01-request-shape-spike.md).
Input catalog: models.dev `api.json`, fetched 2026-10-28 (anthropic 17 rows, openai 52, xai 13, alibaba-token-plan 29).

## Summary

- **Anthropic.** Haiku, Sonnet and Opus 4.5 refuse adaptive thinking; thinking off works. Every id from 4.6 upward accepts adaptive thinking with `output_config.effort`. The vendor docs say that Fable 5, Fable 5.1, Opus 5.5 and Sonnet 5.5 refuse `thinking: disabled`, which Ask sends for thinking off. P2: show 4.6+ with thinking. Show 4.5 with `Reasoning: false`, but only after the adapter sends no effort for such a row. The always-on group needs an `off` rule. Both items are question 1.
- **OpenAI.** Every family up to `gpt-5.6` works on the Responses wire with the effort words that models.dev lists. The vendor refuses other words (HTTP 400, `param: reasoning.effort`). **Ask sends no effort for `gpt-6` and `gpt-6.1` ids:** the fantasy fork marks a reasoning model by its id, so it drops `reasoning.effort` and sends a `system` role. The request returns 200, but the thinking control has no effect.
- **xAI.** No credential, so no live result. The same id rule drops the effort from every `grok-*` request, including the built-in `grok-4.7` row.
- **Token Plan.** The key has no purchased plan. Every model on both wires returns HTTP 403 `AccessDenied.Unpurchased`, also the built-in `deepseek-v4.1-flash` row. No completions family has live evidence. The `/models` list returns ids that inference refuses, so the list is a plan catalog, not an entitlement list.
- **ChatGPT OAuth.** The access token in the copied home had expired, and the vendor rejected the refresh exchange. There is no live ChatGPT list or inference result. A live ChatGPT check needs a new sign-in (`ask auth login`).
- **Policy.** Two vendor rules affect this plan; see [Policy findings](#policy-findings). Anthropic does not permit a third-party app to send requests with a Free, Pro or Max subscription credential. Token Plan permits interactive use only.

## Method

- Every inference ran through the real `ask -p` path: native auth, `BindAuth`, the provider registry and the adapter.
  A temporary `ASK_SPIKE_MODEL_ROW` hook in `cmd/tui/headless_faux.go` gave the `providers.Model` row for an id that the built-in catalog does not have. The hook is reverted, and `git diff` shows no product change.
- `ASK_CAPTURE` recorded each run. The request bodies (thinking fields, effort, role, output limit) and the vendor answers below come from those recordings. The recordings and the temp homes are deleted.
- Credentials: two temp `ASK_HOME` directories (mode 0700). The OAuth home held a copy of `auth.json` and `settings.json`. The key home held only `settings.json`, so the resolver used the environment keys. The source home was never written.
- Anthropic used the `anthropic-oauth` method only, because no Anthropic API key was available. OpenAI used the `api-key` method only. Token Plan used its API key.
- Prompt: "Reply with OK". Two Token Plan runs used a prompt with one tool call. `--max-turns 3` capped every run.
- Count: 52 `ask -p` runs; 51 reached the vendor, and the ChatGPT run stopped at the token refresh. The 51 runs sent 53 inference HTTP requests (2 runs made one tool call). The raw list probes sent 14 requests; 10 of them used a deliberately invalid credential.
- "Echoed model" is the `model` field of the first stream event. It shows whether the vendor resolved an alias to a dated snapshot.

## Live inference results

All results are from 2026-10-28, through `ask -p`, with the HTTP status from the recording.

### Anthropic Messages (`anthropic-oauth`)

| Id | off | low | high | Other | Echoed model |
|---|---|---|---|---|---|
| claude-haiku-4-5 | 200 | 400 `invalid_request_error` "adaptive thinking is not supported on this model" | not run | | `claude-haiku-4-5-20251001` |
| claude-sonnet-4-5 | 200 | 400, same message | not run | | `claude-sonnet-4-5-20250929` |
| claude-opus-4-5 | 200 | 400, same message | not run | | `claude-opus-4-5-20251101` |
| claude-sonnet-4-6 | 200 | 200 | 200 | `xhigh`: 400 "does not support effort level 'xhigh'. Supported levels: high, low, max, medium." | alias |
| claude-opus-4-6 | not run | 200 | not run | | alias |
| claude-opus-4-7 | 200 | 200 | 200 | | alias |
| claude-opus-4-8 | not run | 200 | not run | | alias |
| claude-opus-5 | not run | 200 | not run | | alias |
| claude-sonnet-5 | not run | 200 | not run | | alias |
| claude-fable-5 | not run | 200 | not run | | alias |
| claude-fable-5-1 | not run | 200 | not run | | alias |
| claude-haiku-5-5 | 200 | 200 | 200 | `max`: 200 | alias |
| claude-opus-5-5 | not run | 200 | not run | | alias |
| claude-sonnet-5-5 | not run | 200 | 200 | | alias |

- Request shapes in the recordings: off sends `thinking: {type: disabled}`. Low, high, xhigh and max send `thinking: {type: adaptive}` and `output_config: {effort: <word>}`.
- `max_tokens` was the row's `MaxTokens` (64 000 for 4.5, 128 000 for 4.6+). No vendor refused it.
- The dated ids of 4.5 (`-20251001`, `-20250929`, `-20251101`) were not sent. The vendor echoes them for the aliases, so they share the alias rule. The docs say that from 4.6 on, the dateless id is itself a pinned snapshot ([model ids](https://platform.claude.com/docs/en/about-claude/models/overview)).
- Off was not sent to Fable 5, Fable 5.1, Opus 5.5 or Sonnet 5.5. The [thinking troubleshooting doc](https://platform.claude.com/docs/en/build-with-claude/thinking-troubleshooting) says these models reject `disabled`, and Sonnet 5.5 wants `between_tools` instead. The doc gives the answer, so no live call was spent.

### OpenAI Responses (`api-key`)

Every row sent `store: false`. A row with `Reasoning: true` also sent `include: [reasoning.encrypted_content]`.

| Family (id run) | Word sent | Result | Role sent | Echoed model |
|---|---|---|---|---|
| gpt-4.1 (`gpt-4.1-mini`) | none (`Reasoning: false`) | 200 | system | dated `-2025-04-14` |
| gpt-4o (`gpt-4o-mini`) | none (`Reasoning: false`) | 200 | system | dated `-2024-07-18` |
| gpt-5 (`gpt-5-nano`) | omitted (off) | 200. The model still reasons at its default level. | developer | dated `-2025-08-07` |
| gpt-5 (`gpt-5-nano`) | `minimal` | 200 | developer | dated |
| gpt-5 (`gpt-5-nano`) | `none` | 400 `invalid_request_error`, `param: reasoning.effort`, "Supported values are: 'minimal', 'low', 'medium', and 'high'" | developer | |
| gpt-5.1 | `none` | 200 | developer | dated `-2025-11-13` |
| gpt-5.2 | `xhigh` | 200 | developer | dated `-2025-12-11` |
| gpt-5.3-codex | `none`, `xhigh` | 200, 200 | developer | alias |
| gpt-5.4 (`gpt-5.4-nano`) | `none`, `xhigh` | 200, 200 | developer | dated `-2026-03-17` |
| gpt-5.5 | `max` | 400, "Supported values are: 'none', 'low', 'medium', 'high', and 'xhigh'" | developer | |
| gpt-5.6 (`gpt-5.6-luna`) | `none`, `max` | 200, 200 | developer | alias |
| gpt-5-pro | `high` | 200 | developer | dated `-2025-10-06` |
| o3 | `low` | 200 | developer | dated `-2025-04-16` |
| gpt-6 (`gpt-6-astra`) | asked `none`, then `max`: **nothing sent** | 200, 200 | system | alias |
| gpt-6 (`gpt-6-luna`) | asked `none`: **nothing sent** | 200 | system | alias |
| gpt-6.1 (`gpt-6.1-sol`) | asked `max`: **nothing sent** | 200 | system | alias |

Cause of the gpt-6 result (verified in code):

- `go.mod:415` replaces `charm.land/fantasy` with the fork `github.com/kevinle128/fantasy`.
- In the fork, `getResponsesModelConfig` (`providers/openai/responses_language_model.go:92-150`) marks a reasoning model only when the id has `o1`, `o3`, `o4`, `oss`, `gpt-5`, `codex-` or `computer-use`.
- For any other id it drops `ReasoningEffort` with a warning (`:300-309`, `:341-349`) and uses the `system` role. Ask sets `pr.Effort` (`internal/providers/openai/responses_prompt.go:71-73`), but the value never reaches the wire.
- The same rule applies to every `grok-*` id on `https://api.x.ai/v1`. The built-in `grok-4.7` row (`internal/providers/catalog.go:158-163`) therefore never sends its `xhigh` word today. This defect exists already; this plan did not cause it.

The docs disagree with one live result. The `gpt-5.3-codex` page lists low, medium, high and xhigh, but the API accepted `none` on 2026-10-28. The live result decides.

### Token Plan (API key)

| Request | Result |
|---|---|
| Completions `deepseek-v4-pro`, off (`enable_thinking: false`) | 403 `AccessDenied.Unpurchased` |
| Completions `deepseek-v4-pro`, high (`enable_thinking: true`, `reasoning_effort: high`) | 403 `AccessDenied.Unpurchased` |
| Completions `deepseek-v4.1-flash`, high | 403 `AccessDenied.Unpurchased` |
| Completions `qwen3.6-flash`, off | 403 `AccessDenied.Unpurchased` |
| Messages, built-in row `deepseek-v4.1-flash`, off (no spike row) | 403 `AccessDenied.Unpurchased` |

The built-in row fails too, so the cause is the account and not the request shape. The runs stopped there. No Token Plan completions family has live evidence on 2026-10-28.

## Model-list endpoints

| Source | Request | Result | Fields | Paging | Id forms |
|---|---|---|---|---|---|
| OpenAI, API key | `GET https://api.openai.com/v1/models` | live 200, 134 items | top: `object` (`"list"`), `data`; item: `id`, `object`, `created`, `owned_by`, `shutdown_date` | none (live, and none in the [reference](https://developers.openai.com/api/reference/resources/models/methods/list)) | Both. Most families list an alias and a dated id (`gpt-5.4-nano`, `gpt-5.4-nano-2026-03-17`). `gpt-5.6-*`, `gpt-6*`, `gpt-6.1-*` and `gpt-5.3-codex` have an alias only. This key has no fine-tuned (`ft:`) id. |
| OpenAI, ChatGPT OAuth | same URL, `Authorization: Bearer <access token>` | live 401 `token_expired` (expired token; refresh rejected) | docs: `models[]` with `slug`, `display_name`, `visibility`; keep `visibility == "list"` and the server order ([guide](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference)). Other `visibility` values are not documented. | not documented | not observed |
| Anthropic, API key | `GET https://api.anthropic.com/v1/models?limit=1000` | not run (no key) | docs: `data[]`, `first_id`, `last_id`, `has_more`; item: `type`, `id`, `display_name`, `created_at`, `capabilities` (`effort.<word>.supported`, `thinking.types.<type>.supported`), `max_input_tokens`, `max_tokens`, `line`, `lifecycle`, `deprecated_at`, `retires_at` ([list reference](https://platform.claude.com/docs/en/api/models/list)) | `limit` 1-1000 (default 20), `after_id`, `before_id`, `has_more`, `first_id`, `last_id` | docs: dateless ids from 4.6 on; dated ids before 4.6, with aliases. Whether the list returns the dated or the alias form for 4.5 is not stated. |
| Anthropic, OAuth | same | **not used** (see [Policy findings](#policy-findings)) | | | |
| xAI, API key | `GET https://api.x.ai/v1/models` | not run (no credential) | docs: `object`, `data[]`; item: `id`, `aliases[]`, `created`, `object`, `owned_by`, `context_length`, prices, `capabilities.reasoning_effort[]`, `default_reasoning_effort`. It lists image models too ([reference](https://docs.x.ai/developers/rest-api-reference/inference/models)). | none documented | docs: `<name>` is an alias of the latest stable version; `<name>-<date>` is one release ([models](https://docs.x.ai/developers/models)) |
| xAI, OAuth | same | not run | no official doc for third-party OAuth | | |
| Token Plan, API key | `GET https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1/models` | live 200, 20 items (not documented by the vendor) | top: `object`, `data`, `first_id`, `last_id`, `has_more` (false); item: `id`, `object`, `created`, `owned_by` | `has_more`, `first_id`, `last_id` | Both: aliases (`deepseek-v4-pro`) and `-MMDD` snapshots (`deepseek-v4-pro-0813`, `deepseek-v4.1-flash-1017`). It also has non-chat ids (audio, image, video) and `auto`. |

Notes for phases 2, 4 and 5:

- Two list sources give capabilities, so phase 4 can read them later: Anthropic `capabilities.effort` and `capabilities.thinking.types`, and xAI `capabilities.reasoning_effort`. Under P1 they can only narrow a family rule.
- The OpenAI key list has non-chat ids that phase 2 must exclude by pattern, because many have no models.dev row: `-tts`, `-transcribe`, `-audio`, `-realtime`, `-search-`, `-image`, `-live`, `whisper-`, `tts-`, `sora-`, `omni-moderation-`, `text-embedding-`, `davinci-`, `babbage-`, `chat-latest` and `-chat-latest`.
- Some chat ids in the key list have no models.dev row:
  - every dated snapshot (phase 5 matches it to its alias);
  - `gpt-5-codex`, `gpt-5.1-codex`, `gpt-5.1-codex-max`, `gpt-5.1-codex-mini` and `gpt-5.2-codex` (the docs say these are shut down);
  - the `-chat-latest` ids.
- models.dev P3 rows that the OpenAI key list does not have: `gpt-5.3-codex-spark`, `gpt-5.6`, `gpt-daybreak-blue-latest`, `gpt-daybreak-red-latest` and `o3-pro`.
- The Token Plan list equals the Personal edition model list in the [vendor overview](https://www.alibabacloud.com/help/en/model-studio/token-plan-personal-overview), plus non-chat ids. The Team edition adds `qwen3.6-plus`, `kimi-k2.5`, `kimi-k2.6`, `kimi-k2.7-code`, `glm-5`, `glm-5.1` and `MiniMax-M2.5` ([Team overview](https://www.alibabacloud.com/help/en/model-studio/token-plan-team-overview)). These are the models.dev P3 rows that the live list does not have.

## 401 and 403 meaning per method (P12)

A deliberately invalid credential was sent to the list endpoint and to the inference endpoint of each vendor. No real account was changed.

| Method | List refusal | Inference refusal | Same meaning? |
|---|---|---|---|
| OpenAI `api-key` | 401 `invalid_request_error` / `invalid_api_key` | 401, same type and code | Yes for a bad key (live). The docs give 403 for an unsupported country or region, which is account-wide. |
| OpenAI `openai-chatgpt` | malformed token: 401 `invalid_jwt`; expired token: 401 `token_expired` | malformed token on `/v1/responses`: 401 `invalid_jwt` | Yes for a bad or expired token (live). The docs give admission 403 for a policy or region check and coded 403s such as `subscription_sharing_user_not_eligible` ([errors](https://developers.openai.com/siwc/token-sharing-open-source/errors-and-recovery)). Their meaning for one model is not documented. |
| Anthropic `api-key` | 401 `authentication_error` | 401 `authentication_error` | Yes for a bad key (live). The docs say 403 `permission_error` means that the key has no permission for the resource ([errors](https://platform.claude.com/docs/en/api/errors)). It can be specific to one model. |
| Anthropic `anthropic-oauth` | not sent | not tested | unknown |
| xAI `api-key` | **400**, top-level `code: "invalid-argument"` and an `error` string | 400, same shape | Same status (live). A bad key is not a 401 on xAI. |
| xAI `xai-oauth` | not tested | not tested | unknown |
| Token Plan `api-key`, bad key | 401, top-level `code: "InvalidApiKey"` (DashScope shape) | 401 `invalid_request_error` / `invalid_api_key` (OpenAI shape) | Same status, different body shape (live). |
| Token Plan `api-key`, valid key without a plan | **200**, and the list has the refused ids | 403 `AccessDenied.Unpurchased` | **No** (live). A list success does not mean access. |

Decision input for P12:

- For every tested method, a list 401 means a bad or expired credential, and inference refuses the same credential with a 401 (xAI: 400).
- No method shows that a list 403 means "this model is denied". Keep model access `unknown` after a list 401 or 403, as P12 says.
- Treat the xAI 400 `invalid-argument` as `unknown`.
- Token Plan needs no list for access: its list is a catalog, and only inference shows a denial (403 `AccessDenied.Unpurchased`).

## Policy findings

1. **Anthropic subscription OAuth.**
   The [Claude Code legal and compliance page](https://code.claude.com/docs/en/legal-and-compliance) (read 2026-10-28) says: "Anthropic does not permit third-party developers to offer Claude.ai login into their own applications, or to route requests through Free, Pro, or Max plan credentials on behalf of their users. Moreover, developers may not collect, store, or intermediate Claude.ai credentials or session tokens".
   It also says that OAuth "is designed to support ordinary use of Claude Code and other native Anthropic applications".
   Ask's `anthropic-oauth` method sends requests with such a credential. Therefore:
   - this spike did not send `GET /v1/models` with the OAuth token ("not used");
   - an account list for `anthropic-oauth` in phase 4 has the same policy problem as inference.
   The 26 Anthropic runs above used this method, because the task said to use it. Whether to keep the method is a user decision outside this phase.
2. **Token Plan usage scope.**
   The [Personal overview](https://www.alibabacloud.com/help/en/model-studio/token-plan-personal-overview) says: "This plan is intended for interactive use only within coding tools and agent tools… It must not be used for automation scripts, custom application backends, or any non-interactive batch call scenarios". The vendor can suspend the subscription or ban the key.
   The Team overview has the same rule.
   Headless `ask -p` runs and an automatic background list refresh can conflict with this rule.

## Family rule table

Columns:

- **Shape** is the request that works.
- **Words** are the effort words that the rule allows. models.dev can only narrow them (P1).
- **Defaults** are the output limit and the context window to use when models.dev has no number: the smallest documented value in the family.
- **Show** follows the phase exit rule: no live evidence means hide.

All Anthropic rows: API `anthropic-messages`, base URL `https://api.anthropic.com`, compat `AnthropicCompat{}`.

| Id pattern | Shape | Words | Defaults (out / ctx) | Show | Evidence |
|---|---|---|---|---|---|
| `claude-(haiku\|sonnet\|opus)-4-5(-\d{8})?` | `thinking: disabled` only | none: `Reasoning: false` | 64 000 / 200 000 | **show**, `Reasoning: false`, but only after the off path is fixed (question 1) | live 2026-10-28: off 200 on all 3, low 400 on all 3; [troubleshooting](https://platform.claude.com/docs/en/build-with-claude/thinking-troubleshooting): adaptive rejected |
| `claude-(sonnet\|opus)-4-6` | off: `disabled`; on: `adaptive` + `output_config.effort` | low, medium, high, max | 128 000 / 1 000 000 | **show** | live: sonnet off, low, high 200; xhigh 400; opus low 200; [effort](https://platform.claude.com/docs/en/build-with-claude/effort) |
| `claude-opus-4-[78]` | same | low, medium, high, xhigh, max | 128 000 / 1 000 000 | **show** | live: opus-4-7 off, low, high 200; opus-4-8 low 200; effort doc |
| `claude-opus-5`, `claude-sonnet-5`, `claude-haiku-5-5` | same. The docs reject `disabled` on Opus 5 and Haiku 5.5 only together with xhigh or max, which Ask never sends; Sonnet 5 accepts it. | low, medium, high, xhigh, max | 128 000 / 1 000 000 | **show** | live: low 200 on all 3; haiku-5-5 off, high, max 200; troubleshooting doc |
| `claude-fable-5(-1)?`, `claude-opus-5-5`, `claude-sonnet-5-5` | on: `adaptive` + effort works. Off: `disabled` is rejected (Sonnet 5.5 wants `between_tools`). | low, medium, high, xhigh, max | 128 000 / 1 000 000 | **show only with an always-on rule** (question 1), else hide | live: low 200 on all 4, sonnet-5-5 high 200; troubleshooting doc |

All OpenAI rows: API `openai-responses`, base URL `https://api.openai.com/v1`, compat `ResponsesCompat{Store: false, SupportsMaxOutputTokens: true}` as in the built-in `gpt-5.5` row. `SupportsLongCacheRetention` stays on the built-in rows only, because the spike did not test it.
"off: omit" means a `nil` map entry for off: Ask sends no effort, and the vendor uses its default level. "off: none" means an explicit `none` word.

| Id pattern | Shape | Words | Defaults (out / ctx) | Show | Evidence |
|---|---|---|---|---|---|
| `gpt-4\.1(-mini)?(-\d{4}-\d{2}-\d{2})?` | no reasoning | none: `Reasoning: false` | 32 768 / 1 047 576 | **show** | live: gpt-4.1-mini 200 |
| `gpt-4o(-mini)?(-\d{4}-\d{2}-\d{2})?` | no reasoning | none: `Reasoning: false` | 16 384 / 128 000 | **show** | live: gpt-4o-mini 200 |
| `gpt-5(-mini\|-nano)?(-\d{4}-\d{2}-\d{2})?` | off: omit (the model still reasons) | minimal, low, medium, high | 128 000 / 400 000 | **show** | live: off 200, minimal 200, none 400 |
| `gpt-5-pro(-\d{4}-\d{2}-\d{2})?` | off: omit | high | 272 000 / 400 000 | **show** | live: high 200; [model page](https://developers.openai.com/api/docs/models/gpt-5-pro) "only supports" high |
| `gpt-5\.1(-\d{4}-\d{2}-\d{2})?` | off: none | none, low, medium, high | 128 000 / 400 000 | **show** | live: none 200 |
| `gpt-5\.[245](-mini\|-nano)?(-\d{4}-\d{2}-\d{2})?` | off: none | none, low, medium, high, xhigh | 128 000 / 400 000 | **show** | live: 5.2 xhigh 200, 5.4-nano none and xhigh 200, 5.5 max 400 |
| `gpt-5\.[245]-pro(-\d{4}-\d{2}-\d{2})?` | off: omit | medium, high, xhigh | 128 000 / 400 000 | **show** | live for the `-pro` family (gpt-5-pro); words from the [gpt-5.5-pro page](https://developers.openai.com/api/docs/models/gpt-5.5-pro) |
| `gpt-5\.3-codex(-spark)?` | off: none | none, low, medium, high, xhigh | 32 000 / 128 000 | **show** | live: none 200, xhigh 200 |
| `gpt-5\.6(-luna\|-sol\|-terra)?` | off: none | none, low, medium, high, xhigh, max | 128 000 / 1 050 000 | **show** | live: luna none 200, max 200 |
| `gpt-6(\.1)?-(astra\|luna\|sol\|terra)` | 200, but Ask sends no effort and a `system` role | none until the adapter rule covers `gpt-6`; then per the [model pages](https://developers.openai.com/api/docs/models/gpt-6-sol) (`astra` and `6.1-sol`: low to max, `none` gives 400; `sol` and `luna`: none to max) | 128 000 / 1 050 000 | **show with `Reasoning: false`** until question 2 is fixed | live: astra, luna, 6.1-sol 200 with no effort on the wire |
| `o3(-pro)?(-\d{4}-\d{2}-\d{2})?` | off: omit | low, medium, high | 100 000 / 200 000 | **show** | live: o3 low 200 |
| `gpt-daybreak-.*` | none | | | **hide** | no doc, no live result, not in the key list |

All xAI rows: API `openai-responses`, base URL `https://api.x.ai/v1`, compat `ResponsesCompat{Store: false, SupportsMaxOutputTokens: true}` as in the built-in `grok-4.7` row. **Live: not run.** Until the adapter rule covers `grok-*` (question 2), Ask cannot send an effort word to xAI.

| Id pattern | Shape (docs) | Words (docs) | Defaults (out / ctx) | Show | Evidence |
|---|---|---|---|---|---|
| `grok-4\.20-0309-non-reasoning` | no reasoning | none: `Reasoning: false` | 30 000 / 1 000 000 | **hide** | [model page](https://docs.x.ai/developers/models); no live result |
| `grok-4\.20-0309-reasoning` | reasoning always on; no effort list | none | 30 000 / 1 000 000 | **hide** | model page; no live result |
| `grok-4\.3` | effort accepted | none, low, medium, high, xhigh | 30 000 / 1 000 000 | **hide** | [grok-4.3 page](https://docs.x.ai/developers/models/grok-4.3); no live result |
| `grok-4\.[5-7]` | reasoning cannot be disabled; effort accepted | low, medium, high, xhigh. For 4.5, the [reasoning guide](https://docs.x.ai/developers/model-capabilities/text/reasoning) says `xhigh` is treated as `high`. | 128 000 / 500 000 | **hide** (the built-in `grok-4.7` row stays as an id override, P4) | [grok-4.7 page](https://docs.x.ai/developers/models/grok-4.7); the Responses reference gives a default `max_output_tokens` of 128 000; no live result |
| `grok-build-0\.1` | no effort list | none | 128 000 / 256 000 | **hide** | model page; no live result |

All Token Plan rows: API `openai-completions`, base URL `https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1`. **Live: no evidence** (403 `AccessDenied.Unpurchased` on every model), so every generated row is **hide**. The two built-in deepseek rows stay (P4).

Shared compat from the docs:

- `ThinkingFormat: "qwen"` (`enable_thinking`).
- `SupportsDeveloperRole: false`: the docs list only the system, user, assistant and tool roles.
- `SupportsStore: false`, `SupportsFinishReason: true`.
- `SupportsStreamOptions: true`: `include_usage` is documented.

The [Chat Completions reference](https://www.alibabacloud.com/help/en/model-studio/qwen-api-via-openai-chat-completions) marks `max_tokens` "to be deprecated" in favor of `max_completion_tokens`. The built-in row uses `max_tokens`.

| Id pattern | Thinking (docs) | Words (docs) | Extra compat (docs) | Defaults (out / ctx) | Show |
|---|---|---|---|---|---|
| `deepseek-v4(\.1)?-(flash\|pro)(-\d{4})?` | `enable_thinking` | v4-pro: high, max; v4.1-flash, -0731, -0813: low, high, max | `RequiresReasoningContentOnAssistantMessages: true`. The [upstream DeepSeek doc](https://api-docs.deepseek.com/guides/thinking_mode) gives a 400 without it in tool loops; Alibaba does not say. | 384 000 / 1 000 000 | **hide** |
| `qwen3\.[67]-(flash\|plus\|max)` | `enable_thinking`, `thinking_budget` | none | | 65 536 / 1 000 000 | **hide** |
| `qwen3\.8-(flash\|max)` | `reasoning_effort`; `none` maps to thinking off | low, medium, xhigh | `preserve_thinking` defaults to true: send back all `reasoning_content` | 131 072 / 1 000 000 | **hide** |
| `glm-5(\.1)?` (Team only) | `enable_thinking` | high, max | GLM `clear_thinking` defaults to false: send back `reasoning_content` | 16 384 / 202 752 | **hide** |
| `glm-5\.[23]` | `enable_thinking`. For glm-5.3, `false` fails, so off is not allowed. | 5.2: high, max; 5.3: low, high, max | as GLM above | 131 072 / 1 000 000 | **hide** |
| `kimi-k2\.[5-7](-code)?` (Team only) | `enable_thinking`; k2.7-code is thinking-only | none | send back `reasoning_content` ([Kimi doc](https://platform.kimi.ai/docs/guide/use-kimi-k2-thinking-model)) | 98 304 / 262 144 | **hide** |
| `MiniMax-M2\.5` (Team only) | uses a `thinking` parameter, not `enable_thinking` ([deep thinking](https://www.alibabacloud.com/help/en/model-studio/deep-thinking)); Ask has no such format | | | 32 768 / 196 608 | **hide** |

## Unresolved questions

1. **Anthropic thinking off.** The Anthropic adapter does not decide thinking off correctly for two groups of rows.
   - **Always-on models.** `ClampThinkingLevel` always allows off (`internal/providers/clamp.go:19-44`), and the adapter then sends `thinking: disabled` (`internal/providers/anthropic/document.go:65-67`). Fable 5, Fable 5.1, Opus 5.5 and Sonnet 5.5 reject that.
     Phase 2 must choose:
     - a rule flag that removes off from their levels, with the lowest effort word sent in its place;
     - or hide them.
     With no change, a user who selects thinking off gets HTTP 400.
   - **`Reasoning: false` rows (the 4.5 generation).** No `Reasoning: false` Anthropic row exists today, so this path was not live-tested.
     - The agent clamps the level only in `SetModel` and `SetThinkingLevel` (`internal/agent/agent.go:404,443`), not in `New`.
     - The adapter turns an empty level into `medium` (`internal/providers/anthropic/document.go:102-115`, `prepare.go:38`). For a `Reasoning: false` row it then sends `output_config.effort` with no `thinking` key.
     - The docs say Haiku 4.5 and Sonnet 4.5 do not take `effort`. Before phase 2 shows the 4.5 rows, the adapter must send `thinking: disabled` and no effort for a `Reasoning: false` row. Clamping in `effectiveThinking` is one way to do that.
2. **Effort on gpt-6 and grok ids.** The fantasy fork rule (`responses_language_model.go:92-150`) drops the effort for any id outside `gpt-5`, `o1`, `o3`, `o4`, `oss` and `codex-`. Fixing it (a wider id rule in the fork, or an Ask-side `ResponsesCompat` flag that forces the reasoning path) is outside this plan's file list. It also affects the built-in `grok-4.7` row today.
3. **Anthropic OAuth policy.** See [Policy findings](#policy-findings) item 1. This is a user decision; it affects inference and the phase 4 list client.
4. **Token Plan policy and evidence.** See Policy findings item 2. A purchased plan is needed for live completions evidence. Until then, every generated Token Plan row is hidden.
5. **ChatGPT list fields and access.** No valid token was available, so the `models[]` fields, `visibility` values and slugs are from the docs only. A new sign-in is needed for a live check.
6. **The `off` meaning for gpt-5, `-pro` and o3.** These models have no `none` word, so "off" sends no effort and the model still reasons. Phase 2 can keep "off: omit" (as the built-in rows do) or map off to the lowest word.
7. **Budget-token thinking (plan "Out of scope").** The 4.5 generation can think only with `thinking: {type: enabled, budget_tokens}`. Opus 4.5 also takes `effort` with it ([effort](https://platform.claude.com/docs/en/build-with-claude/effort)); Haiku and Sonnet 4.5 do not. Without budget-token support, those 3 models stay at thinking off. Budget-token support is a separate plan, and it is needed only if the user wants thinking on the 4.5 models.
