---
phase: 1
title: "Request-shape and endpoint spike"
status: completed
priority: P1
effort: "1.5d"
dependencies: []
---

# Phase 1: Request-shape and endpoint spike

<!-- Updated: Red Team 2026-10-28 - spike runs as a gated adapter test, per family generation, in a git-ignored directory -->

## Goal

Use vendor docs and opt-in live calls to prove which model families Ask's adapters can drive, and what each model-list endpoint returns. The family rules in phase 2 and the list clients in phase 4 then rest on evidence.

## Context

- [xia report](../reports/xia-261028-1957-omp-model-discovery-gaps.md): gaps G1-G4 and unresolved questions 1-3.
- [Red-team adjudication](../reports/red-team-261028-account-model-discovery.md): findings 2, 3, 10, 11 and 15.
- Adapter request shapes:
  - `internal/providers/anthropic/document.go:65-74` (thinking) and `:133-153` (`max_tokens` clamp).
  - `internal/providers/openai/prepare.go:52,98-108`.
  - `internal/providers/openai/completions_body.go:105-131`.
- `ask -p` resolves models only through `providers.Find` over the 6 built-in rows (`cmd/tui/headless_faux.go:78-80`). It cannot test new ids.
- Credentials:
  - Keys come from the environment: `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `XAI_API_KEY`, `ALIBABA_TOKEN_PLAN_API_KEY`.
  - OAuth tokens come from a copy of an OAuth home.
  - Never print a key or a token. Never ask the user for a key.

## Files

- Temporary, reverted at the end of the phase: `cmd/tui/headless_faux.go` reads `ASK_SPIKE_MODEL_ROW`, a JSON `providers.Model`, when `--model` is not in the catalog. The run then goes through the real `ask -p` path: native auth, `BindAuth`, the provider registry and the adapter. This follows the project rule that tests use the real entry point. The variable is never committed.
- Create: `plans/reports/spike-261028-model-request-shapes.md`. It names families, id patterns and outcomes only. It contains no account-specific id (for example a fine-tuned id) and no header.
- Credentials:
  - Run with a temp `ASK_HOME`.
  - Copy into it only `auth.json` and `settings.json` from the real home; never use the source home.
  - Read keys from the environment.
  - Delete the temp home at the end.
- At exit, `git diff` shows no product change.

## Steps

1. **Anthropic.** Test one model of each family generation that models.dev lists: Opus, Sonnet and Haiku families, and each version line from 4.5 upward. Run every non-dated id once. For each:
   - thinking off: expect `thinking: disabled`;
   - thinking `low` and `high`: expect `output_config.effort` plus adaptive.

   Record pass, or the error type and code. This decides P2 for each family.
2. **OpenAI Responses.** Test one model per family: `gpt-4.1`, `gpt-4o`, `gpt-5`, `gpt-5.1`-`5.6`, `gpt-6`, `o3`, and the `-pro` and `-codex` variants. Run thinking off plus each effort word that the row lists. Record which words the API accepts.
3. **xAI.** Test `grok-4.20-0309-reasoning` (reasoning, no effort list), `grok-4.3`, `grok-4.7` and `grok-build-0.1`. Run thinking off plus each effort word.
4. **Token Plan completions.** Test one model per family listed in models.dev `alibaba-token-plan` with a chat type: deepseek, qwen, glm, kimi, MiniMax. Run thinking off and on. Record the `ThinkingFormat` and `CompletionsCompat` flags that work.
5. **Request defaults.** For each family, record a safe output limit and context window to use when models.dev has no number. Use the smallest documented value in the family.
6. **Model lists.** For each source, record: URL, status, JSON field names, paging fields, item count, and whether ids come as aliases, dated snapshots or both. Save field names and family patterns only.
   - `GET https://api.openai.com/v1/models` with an API key.
   - The same URL with a ChatGPT OAuth token. Confirm `models[].slug`, `display_name` and `visibility`.
   - `GET https://api.anthropic.com/v1/models?limit=1000` with a key, and with an OAuth token. Confirm the paging fields. Check the vendor terms for listing with a subscription token before you send it; if the terms forbid it, record "not used".
   - `GET https://api.x.ai/v1/models` with a key, and with an xAI OAuth token.
   - `GET <TokenPlanCompletionsURL>/models` with the Token Plan key.
7. **401 and 403 meaning.** For each method, record whether a list refusal also means an inference refusal. A revoked or wrong-scope credential is enough; do not try to break a real account. This decides P12.
8. Write the family rule table. Per row: provider, id pattern, API, base URL, request shape that works, effort words, request defaults, compat flags, and "show" or "hide".
9. Revert the `ASK_SPIKE_MODEL_ROW` change and delete the temp home.

## Verification

- The report has a row for every family that models.dev lists for anthropic, openai, xai and alibaba-token-plan and that passes P3.
- Every row cites a doc URL or a live result: date, model family, outcome.
- `git status` shows no product file change. The only new file is the report.

## Exit

This phase needs no approval and does not stop the run. A family marked "unknown" or without live evidence is hidden in phase 2, which decides automatically. The user can read the rule table at any time.
