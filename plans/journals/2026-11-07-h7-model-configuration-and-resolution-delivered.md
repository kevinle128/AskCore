---
title: H7 model configuration and resolution delivered
date: 2026-11-07
summary: "User models.json, model patterns with :level, --models/enabledModels, --offline and ctrl+p cycling shipped on develop"
---

# H7 model configuration and resolution delivered

## What happened
H7 (H-PROV-17 models.json, H-PROV-19 resolution) was researched from oh-my-pi and deepseek-harness, planned in 9 phases, red-teamed, and implemented on `develop` in commits 53f671d..57d7be0.

- `internal/settings`: reads `<ASK_HOME>/models.json` (JSONC, 0600, no FIFO or link, baseUrl checked), keeps the last valid copy, reports problems by schema path only.
- `internal/auth`: key order `--api-key` > saved credential > models.json `apiKey` > env; Pi value grammar (`$NAME`, `${NAME}`, `!command`); command cache owned by the entry (10 s, 64 KiB output cap, 30 s backoff, cleared on 401).
- `internal/providers`: custom rows with Pi defaults, overrides applied after refresh, `ResolvePattern` with the oh-my-pi colon rule, `ExpandScope` globs.
- `internal/app`, `internal/acp`, `cmd/tui`: one model view rebuilt only at session start, `/model`, `setModel` and cache writes; `(Provider, API, BaseURL)` key tables; `config` error on an edited row; applied-options echo for older leaders.
- `internal/tui`: `ctrl+p` / `ctrl+shift+p` model cycling per the approved design.

## Decisions
- The project `.ask/models.json` was dropped (Q2b). No reference harness has one, and red-team showed it was the source of env exfiltration, provider shadowing and key takeover risks.
- A saved credential is never sent to a changed baseUrl. An apiKey on a built-in provider is used (Pi rule).
- Ambiguous bare ids are rejected (Pi), not ranked (omp).
- 52 meaningless comment lines were removed from the new code at the user's request.

## Lessons
- Ask "is this file needed at all" before asking "what may it do"; the project file question cost a full red-team round.
- Long subagent runs stalled twice on the stream watchdog; smaller tool budgets and early report writes fixed it.
- Test failures under parallel agent load were timing flakes; two of them also fail on clean `97c3052` under `-race`.

## Next steps
- Manual check with a real key: `ask --model anthropic/<id>:high -p "hi"` and `e2e/tui/real_provider_scenarios.py acceptance`.
- Optional: surface "output too large" for an oversize `!command` instead of plain "failed" (needs an `auth` error value).
- Two old plan labels remain in committed code: `internal/tools/coerce.go:183`, `internal/tui/trust_dialog_test.go:480`.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
