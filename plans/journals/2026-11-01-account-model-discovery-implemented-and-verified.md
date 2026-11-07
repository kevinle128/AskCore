---
title: Account model discovery implemented and verified
date: 2026-11-01
summary: The /model list now comes from the signed-in account with generated metadata; header and footer layout redone; fantasy effort bug fixed upstream.
---

# Account model discovery implemented and verified

## What happened

Cooked the plan `plans/261028-1310-account-model-discovery` from phase 1 to 7.
`/model` no longer shows 6 hand-written rows. It shows the chat models that the account can use, with metadata from a generated models.dev catalog, a cached runtime overlay and the vendor list endpoints.

Built:
- A family rule table and a models.dev mapper in `internal/providers`. A row with no matching rule is hidden. Rules carry request defaults, so a row with unknown limits never sends `max_tokens: 1`.
- `internal/settings/model_cache.go` (cache file, keyed hash, newer `fetchedAt` wins) and `internal/auth` list functions with a GET-only transport.
- `providers.ModelSet` and a refresher owned by `ACPRuntime`: start, after login and logout, and `ask models refresh`; no polling.
- A new TUI layout: slim header with only the logo, a footer row with directory, `provider/model`, thinking and context, no model line in the pane, `?` for an unknown window, `unlisted` and `not in account list`.

## Verification

- Live: OpenAI key (29 of 134 ids shown), ChatGPT sign-in (8 of 10 listed slugs; the hidden 2 are filtered), Anthropic OAuth (17 rows, no list request), xAI OAuth (`grok-4.7`), offline and logout runs.
- Tests: `go vet`, `go test ./...`, lint, drift test, e2e on Alacritty and Ghostty pass. Terminal tests flake under full-suite load and pass alone.
- Secret scans of caches, reports and 241 changed files: 0 hits.

## Decisions and surprises

- ChatGPT list: `api.openai.com/v1/models` per the OpenAI guide, not omp's `chatgpt.com/backend-api`. omp uses the Codex client id, which is a different flow.
- First ChatGPT login failed with `3p_login_workspace_scope_denied`; a later login of the same account worked. Cause not proven.
- The footer layout was reversed twice by the user: model back to a footer, then no cost, no link state and no model line, logo back to a slim header. The design file went `draft` again after each change and was approved at the end.
- A defect in the fantasy fork dropped the reasoning effort for ids it did not know (`gpt-6*`, `grok-*`). Two causes: the id gate in fantasy, and a missing map in the built-in `grok-4.7` row. Fixed in the fork (`e894229`) and proposed upstream (charmbracelet/fantasy#443).
- The stale-token session: a process listing I ran printed a local MCP token. Avoid `ps aux` patterns that print command lines.

## Open items

- Nothing is committed (about 225 changed files).
- `ask -p --thinking off` does not clamp to the lowest effort; the TUI does.
- Anthropic OAuth terms (third-party use) and Token Plan (interactive use only) need a user decision. No Anthropic list request is sent with OAuth.
- Anthropic adapter still computes no cost.
- `grok-*` developer role and `temperature` handling is unverified with xAI.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
