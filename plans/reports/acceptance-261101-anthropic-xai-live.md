# Acceptance: Anthropic and xAI live check

Date: 2026-11-01 (Asia/Saigon). Plan: [account model discovery](../261028-1310-account-model-discovery/plan.md).
Earlier reports: [acceptance](acceptance-261028-account-model-discovery.md), [spike](spike-261028-model-request-shapes.md).
Build: working tree on `develop` (not committed), run with `go run ./cmd/tui`. No token, key, code or header value is in this report.

## Method

- One temp `ASK_HOME` under `/private/tmp` (mode 0700). It held a copy of `auth.json` and `settings.json` only. `~/.ask` was not used or written. No login, no browser, no `--api-key`.
- Sign-ins under test: `anthropic-oauth` and `xai-oauth`.
- The resolved list was read through `ask acp` stdio (`initialize`, `session/new`, `_ask/session/get_available_models`) with `ASK_DISABLE_MODEL_REFRESH=1`. The list therefore comes from the cache and the built-in catalog.
- Anthropic refresh ran behind a loopback-only logging proxy (`HTTPS_PROXY`) that answered 502 and logged host and path only.
- Live inference: 9 HTTP requests in 8 `ask -p` runs ("Reply with OK", `--max-turns 3`) with `ASK_CAPTURE`. Only field names and status codes were read from the captures. One extra list request was the raw xAI probe, and one was `ask models refresh --provider xai`.
- The temp home, the capture directories and the proxy were deleted at the end.

## Results

### 1. xAI (`xai-oauth`)

| Check | Status | Evidence |
|---|---|---|
| 1a `ask models refresh --provider xai` | verified live | `xai  1 models  known`, `metadata  updated`, exit 0. |
| 1b Raw list endpoint | verified live | `GET https://api.x.ai/v1/models`: HTTP 200. Top-level keys: `data`, `object`. 14 items. Item fields: `aliases`, `context_length`, `created`, `id`, `object`, `owned_by`, and 8 price fields (`prompt_text_token_price`, `prompt_image_token_price`, `completion_text_token_price`, `cached_prompt_text_token_price`, and the `_long_context` variants plus `long_context_threshold`). No `capabilities` field. No `default_reasoning_effort` field. |
| 1b Family counts of the 14 ids | verified live | `grok-4.7`: 1. `grok-4.3`: 1. `grok-4.5` or `grok-4.6`: 2. `grok-build`: 1. `grok-imagine-*`: 6. Other: 3 (`grok-4.20-0309-non-reasoning`, `grok-4.20-0309-reasoning`, `grok-4.20-multi-agent-0309`). |
| 1c Resolved list (ACP) | verified live | 1 `xai` row: `grok-4.7`, API `openai-responses`, reasoning true, input text and image, window 500000, max tokens 500000. This is the hand-written row. All generated xAI rows are hidden, as the spike table says. The list has 52 rows in total. |
| 1d `grok-4.7` thinking off | verified live: 200 | Reply `OK`. |
| 1d `grok-4.7` thinking low | verified live: 200 | Reply `OK`. |
| 1d `grok-4.7` thinking high | verified live: 200 | Reply `OK`. |
| 1d Request fields | verified live | All three bodies hold `model: grok-4.7`, `store: false`, `reasoning: {effort: null, summary: null}`, `max_output_tokens: null`. The effort is `null` for off, low and high. |
| 1d Does off clamp to the lowest effort? | not observable on the wire | By source (`internal/providers/catalog.go` `XAIGrok47`, `internal/providers/clamp.go`), off and minimal are `nil` in the level map, so `ClampThinkingLevel` returns `low` for off. The wire shows no effort word for any level, so the clamp cannot be seen. See finding 1. |

### 2. Anthropic (`anthropic-oauth`)

| Check | Status | Evidence |
|---|---|---|
| 2a `ask models refresh --provider anthropic` | verified live | `anthropic  17 models  unknown`, exit 0. The proxy log held one host: `models.dev:443`. There was no request to `api.anthropic.com`. The metadata line was `failed` because the proxy answered 502 (expected here). |
| 2b Resolved list (ACP) | verified live | 17 `anthropic` rows, all from the built-in and generated catalog (no account list). `claude-opus-4-7` and `claude-haiku-4-5` are present. Reasoning false: the 6 rows of the 4.5 generation (3 aliases and 3 dated ids). Reasoning true: `claude-opus-4-6`, `claude-sonnet-4-6`, `claude-haiku-5-5`, `claude-opus-5`, `claude-sonnet-5`, `claude-opus-4-8`, `claude-opus-4-7`, `claude-opus-5-5`, `claude-sonnet-5-5`, `claude-fable-5-1`, `claude-fable-5`. Windows: 200000 for 4.5, 1000000 for the rest. Max tokens: 64000 for 4.5, 128000 for the rest. No row has window 0. |
| 2c `claude-haiku-4-5`, off | verified live: 200 | Body: `thinking: {type: disabled}`, no `output_config`. Reply `OK`. |
| 2c `claude-sonnet-4-6`, low | verified live: 200 | Body: `thinking: {type: adaptive}`, `output_config: {effort: low}`. The model called a tool in its first turn, so the run made 2 requests; both returned 200. Reply `OK`. |
| 2c `claude-opus-4-7`, low | verified live: 200 | Same shape as above. Reply `OK`. |
| 2c `claude-opus-5-5`, low | verified live: 200 | Same shape as above. Reply `OK`. |
| 2c `claude-opus-5-5`, off | verified live: 200 | The body held `thinking: {type: adaptive}` and `output_config: {effort: low}`. Off clamped up to the lowest effort, and the vendor did not refuse. Reply `OK`. |
| 2d 401, 403, 400 vendor errors | verified live: none | No request returned an error status, so there is no error type to record. |

## Findings

1. **xAI effort never reaches the wire.** Family pattern: every `grok-*` id on `https://api.x.ai/v1`, here the row `grok-4.7`. Result: the three requests (off, low, high) all returned 200 and all sent `reasoning.effort: null`. This is the known defect of the spike (unresolved question 2: the fantasy fork marks a reasoning model by id and drops the effort for `grok-*`). It is not a new contradiction. The effect for the user is that the thinking control of `grok-4.7` does nothing, and the clamp of off to low is not visible. Smallest fix: widen the id rule of the fork (`getResponsesModelConfig`) or add an Ask-side `ResponsesCompat` flag that forces the reasoning path. Both are outside the plan file list.
2. **The xAI list has less than the docs said.** The live item has no `capabilities` and no `default_reasoning_effort` field (the spike took these from the docs). Phase 4 cannot narrow an xAI family rule with capabilities for an `xai-oauth` account. Smallest fix: none for correctness (the family rules decide, as P1 says); update the spike note to say that the `xai-oauth` list gives `id`, `aliases`, `context_length` and prices only.
3. **xAI shows 1 of 14 listed ids.** Family patterns `grok-4.3`, `grok-4.5` or `grok-4.6`, `grok-build-0.1` and `grok-4.20-*` are listed by the vendor but hidden by the rules (no live evidence). This matches the spike table and is not a defect. The built-in `grok-4.7` row works live (200 on all three levels). If the user wants more xAI rows, one live request each on `grok-4.3` and `grok-4.5` or `grok-4.6` is the evidence to ask for first.
4. **Anthropic: no contradiction.** All 6 runs returned 200. The 4.5 off path (`disabled`, no effort) and the always-on off clamp (`opus-5-5` off sends adaptive with `low`) work live. The earlier spike risks for these two groups are closed by this evidence. Not covered: off or low on `claude-fable-5`, `claude-fable-5-1` and `claude-sonnet-5-5` (same family rule as `claude-opus-5-5`, so only the family pattern `claude-fable-5(-1)?|claude-opus-5-5|claude-sonnet-5-5` is proven by one member).
5. **Policy reminder.** The `anthropic-oauth` method still falls under policy finding 1 of the spike. This check did not change that. No list request was sent with the OAuth token.

## Secret scan

The scan looked for 8 values (the access, refresh and id tokens and similar secret fields of all sign-ins in the copied `auth.json`) in 35 files: the temp home without the copied `auth.json` (cache, settings, lock files), the capture directories, the command output files, the helper scripts, the proxy log and this report. Result: 0 matches. The temp home, the capture directories and the proxy process were then deleted.

## Unresolved questions

- None for the code. Finding 1 needs a user decision on where to fix the `grok-*` effort drop (fork or Ask compat flag).

## Fork fix follow-up (2026-11-01)

The `charm.land/fantasy` replace now points to `github.com/kevinle128/fantasy` at `v0.0.0-20261101125124-e894229ccae5`. The fork sends an explicit effort for ids outside the known non-reasoning families. Ask also gave the built-in `grok-4.7` row explicit words for `low`, `medium` and `high`: the row had no entry for them, so no word was sent even with the fork fix.

Live results (`ask -p`, prompt "Reply with OK", 6 requests, temp `ASK_HOME`, status and `reasoning.effort` of the request body only):

| Model | `--thinking` | Status | Effort on the wire |
|---|---|---|---|
| `grok-4.7` (xAI) | off | 200 | null (see note) |
| `grok-4.7` (xAI) | low | 200 | low |
| `grok-4.7` (xAI) | high | 200 | high |
| `gpt-6-luna` (OpenAI) | low | 200 | low |
| `gpt-6-luna` (OpenAI) | high | 200 | high |
| `gpt-6.1-sol` (OpenAI) | low | 200 | low |

Note: `ask -p --thinking off` passes the level to the provider without the clamp that the TUI applies in `SetModel` and `SetThinkingLevel`. For a family that cannot turn thinking off, the print-mode request therefore sends no effort (the vendor default), not the lowest effort. The word "none" is never sent to `gpt-6-astra` or `gpt-6.1-sol`. The xAI and Token Plan family hides stay: only `grok-4.7` has live evidence, and it is a hand-written row.
