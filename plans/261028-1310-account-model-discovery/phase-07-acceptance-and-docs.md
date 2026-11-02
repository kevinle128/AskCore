---
phase: 7
title: "Acceptance and docs"
status: completed
priority: P2
effort: "1d"
dependencies: [6]
---

# Phase 7: Acceptance and docs

<!-- Updated: Red Team 2026-10-28 - logout step with env keys; e2e model picker; no account ids in committed reports -->

## Goal

Prove the outcome on the real binary with real accounts, and update the docs that own the changed behavior.

## Context

- The success criteria in `plan.md`.
- `e2e/tui/README.md` describes the faux cases.
- `e2e/tui/real_provider_scenarios.py` runs real providers by hand. The child gets no credential variable. `pick_model` at `:81-89` types a substring and presses Enter on the first match.
- Docs rules: `AGENTS.md` section "Documentation", `.claude/skills/ask-docs-sync`, and ASD-STE100.

## Files

- Modify `e2e/tui/real_provider_scenarios.py`. `pick_model` must select an exact id, not the first substring match. Check its callers at `:241,257,288,308,312,316,327,347`.
- Modify `e2e/tui/run.py` only if a faux case depends on the row count. Check `ask-selector` at `:842-865`.
- Create `plans/reports/acceptance-261028-account-model-discovery.md`. It names providers, families and counts. It contains no account-specific id.
- Modify these docs:
  - `docs/cli.md` (`ask models refresh`);
  - `internal/providers/README.md` (catalog sources, family rules, the snapshot update command);
  - `internal/auth/README.md`;
  - `internal/settings/README.md`;
  - `docs/reference/providers.md` (generated);
  - `CHANGELOG.md` (`[Unreleased]`);
  - `AGENTS.md` (command list).

## Steps

1. Get a named `ask` binary for the leader flows the way the e2e runners do. At the start of each run, `python3 e2e/tui/real_provider_scenarios.py` compiles `./cmd/tui` into `/private/tmp/ask-rp-bin/ask`. Reuse that binary with `ASK_BIN=/private/tmp/ask-rp-bin/ask`. Do not run the compile command directly: a local hook refuses commands that contain it.
2. Real runs: do one run for each sign-in method the user has (OpenAI key, ChatGPT, Anthropic OAuth or key, xAI, Token Plan).
   1. Run `ask models refresh`.
   2. Open `/model` and record the row count for each provider and family.
   3. Send one prompt to one newly listed model per provider, with thinking off and with thinking on.
   4. Record pass, or the error.
3. Offline run: block the network with a wrong proxy variable. Start `ask` and open `/model`. The list must come from the cache and the built-in catalog.
4. Logout run: run `ask auth logout <provider>` and open `/model`.
   - With no env key for that provider, its rows are gone.
   - With an env key, the rows switch to the list of the env key.
5. Secret scan: scan `<ASK_HOME>/model-cache.json` and the acceptance artifacts for the credential values. Nothing may match.
6. Run `ask-docs-sync`, and write the changelog line.
7. Full checks: `go vet ./...`, `go test ./...`, the lint command, `python3 e2e/tui/run.py`.

## Verification

- The acceptance report has one row per success criterion in `plan.md`, each with evidence.
- Every check in step 7 passes.
