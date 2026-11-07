---
title: Account model discovery plan from omp port
date: 2026-10-28
summary: "Planned per-account /model lists with a models.dev catalog, overlay cache and vendor list APIs; red-team found 15 issues before code."
---

# Account model discovery plan from omp port

## What happened

The user saw only 6 models in `/model`. Ask reads a hand-written catalog (`internal/providers/catalog.go:10`).
The user asked for the list of the signed-in account, as oh-my-pi (omp) does.

We read omp (commit `efbc17154a`) in two passes. The first pass missed the omp rules engine: every row, including rows found at run time, gets thinking efforts and compat flags from family rules (`build.ts:347`, `compat/resolve.ts:1304`).
A live check of `https://models.dev/api.json` showed that models.dev now gives `reasoning_options`, `tool_call`, `type` and cost tiers. This makes a small Go family rule table enough.

## Decisions

- U1-U6: 3 layers (generated models.dev catalog, runtime overlay, account list); ids with no family rule are hidden; unknown context shows `?`; models.dev directly, not `catalog.stencil.so`; ChatGPT list from `api.openai.com/v1/models` (the OpenAI guide forbids `backend-api`); refresh at start, login, logout and by command, TTL 2 h.
- U7: apply all 15 red-team findings. U3 refined: the header marks the session cost incomplete; the selector gets no cost column.

## Red-team results worth remembering

- `providers.Registry` already exists. The new type is `ModelSet`.
- A row with limit 0 sends `max_tokens: 1` on the Anthropic and Completions wires. Family rules carry request defaults.
- `Credential.Generation` grows on every OAuth token refresh. Anthropic and xAI OAuth have no subject. The cache key uses identity, not generation.
- `Broker.OnChange` runs under `changeMu`. It may only schedule a refresh.
- The Anthropic adapter computes no cost today. This is an existing defect, out of scope.

## Next steps

- Run `/ak-cook plans/261028-1310-account-model-discovery/plan.md --auto`.
- Phase 1 spends real tokens on signed-in accounts.
- Phase 6 stops for user approval of the TUI design file.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
