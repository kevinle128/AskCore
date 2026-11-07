---
phase: 9
title: "Acceptance, extension trace and docs"
status: pending
priority: P1
effort: "1.5d"
dependencies: [5, 6, 8]
---

# Phase 9: Acceptance, extension trace and docs

## Goal

Prove the H7 exit and the extension trace through the real `ask` entry points, and update the owning docs, the changelog and the generated reference.

## Context

- Exit (roadmap H7): `--model anthropic/<id>:high` resolves and a key saved in `auth.json` is used; malformed config keeps the last valid catalog; a background refresh does not replace the selected model.
<!-- Red Team 2026-11-06: Finding 14 -->
- The last valid copy lives in the memory of one process. A process that never loaded a valid copy (a new leader after a replacement, `ask -p`, `ask settings show`) shows no custom rows and the file problem. A busy leader that a client started can be replaced by a newer build (`internal/leader/README.md:68`).
- Real `ask acp` e2e helpers exist (`cmd/tui/acp_e2e_test.go:434,523`).
- Real entry: `runWithDependencies` (`cmd/tui/headless.go:80`) with `deps.inferenceHTTP` (`cmd/tui/headless.go:155-157`), the seam that the cassette tests use (`cmd/tui/headless_cassette_test.go:63,165`).
- Saved key: write it with `settings.NewAuthStore` in a temp `ASK_HOME` (as the auth command tests do).
- Docs rules: `AGENTS.md` (docs/cli.md by hand, one `CHANGELOG.md` line under `[Unreleased]`, generated `docs/reference/`, `ask-docs-sync` skill, ASD-STE100).

## Requirements

1. Each test runs a real entry point (`runWithDependencies`, a real leader process, or `ask acp`), not a harness that skips it.
2. Each test is offline: `httptest` servers or an injected transport, and `ASK_DISABLE_MODEL_REFRESH=1` in every child process.
3. The docs describe the behavior that the tests prove, in ASD-STE100.

## Steps

1. Write the tests below; run each alone, then the package.
2. Run the full gates (vet, test, lint, drift test).
3. Update the docs in the table, then run the `ask-docs-sync` skill.
4. Do the manual check and record it in this plan.

## Tests (all offline)

| Test | Entry | Proves |
|---|---|---|
| `TestPrintModelPatternWithLevelUsesSavedAnthropicKey` | `runWithDependencies` with `-p --model anthropic/<generated id>:high` | The request goes to the Anthropic Messages URL with the saved key in `x-api-key`, and the body has the effort `high`. No env key is set. |
| `TestPrintLocalProviderFromModelsFile` | `runWithDependencies` with a user `models.json` whose `baseUrl` is an `httptest` OpenAI-compatible server | The extension trace: a custom provider answers with no Go change; the `Authorization` header holds the file key; limits are the Pi defaults (`max_tokens` 16384 in the body). |
| `TestPrintModelsFileEnvTemplateKey` | as above, `apiKey: "$LOCAL_KEY"` | The env value is sent; with the variable unset the run exits 1 and the error names the variable, not a value. |
| `TestLeaderKeepsLastValidModelsFile` | real leader process (existing leader e2e helpers, `ASK_DISABLE_MODEL_REFRESH=1`) | Break the file between two `/model` lists: the custom row stays, the problem is in the result diagnostics. |
| `TestLeaderRefreshKeepsSessionModel` | leader e2e | A cache write (another process writes `model-cache.json`) while a session is on a custom row: the session model and the override value stay. |
| `TestACPKeepsLastValidModelsFile` | `ask acp` e2e | List models, break the file, list again: the custom row stays and the diagnostic is in the result. <!-- Red Team 2026-11-06: Finding 14 --> |
| `TestReplacedLeaderShowsModelsFileProblem` | leader e2e (replacement helpers) | A leader that starts while the file is broken lists no custom row and shows the file problem; a saved default on a custom row falls back with `not in the catalog`. <!-- Red Team 2026-11-06: Finding 14 --> |
| `TestLeaderFileKeyProviderIsUsable` | leader e2e | A `local` provider with only a file key: start choice, saved default, `setModel` and the `usableOnly` list accept it. <!-- Red Team 2026-11-06: Finding 2 --> |
| `TestProjectModelsFileIsNotRead` | `ask acp` e2e with `--approve` | A `.ask/models.json` in a trusted project is not read: no row, no problem. <!-- Red Team 2026-11-06: Q2b (Findings 3, 4) --> |

```
go test ./cmd/tui -run 'TestPrintModelPattern|TestPrintLocalProvider|TestPrintModelsFile|TestLeaderKeepsLastValid|TestLeaderRefreshKeeps|TestACPKeepsLastValid|TestReplacedLeader|TestLeaderFileKeyProvider|TestProjectModelsFileIsNotRead' -count=1
go vet ./...
go test ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
ASK_UPDATE_DOCS=1 go test -run '^TestReferenceDocs$' ./cmd/tui
go test -run '^TestReferenceDocs$' ./cmd/tui
```

A test name describes the scenario. No test name or code comment uses a plan label (D1-D14, Q1-Q3, Q2b, M1-M19, finding numbers, phase numbers).

## Docs

| File | Change |
|---|---|
| `docs/cli.md` | `--model` grammar and `:level`, `--models`, `--offline` (scope of M10), `enabledModels`, the models.json section (user file only, fields, defaults, value grammar, key order, reload points, problems; `.ask/models.json` is not read) <!-- Red Team 2026-11-06: Q2b (Findings 3, 4) -->, the cycling key |
| `CHANGELOG.md` | One line under `[Unreleased]` for each user-visible change |
| `README.md` | Native auth section: the key order with the models.json key |
| `internal/settings/README.md`, `internal/auth/README.md`, `internal/providers/README.md`, `internal/app/README.md`, `internal/acp/README.md` | Owner text for the new files and contracts |
| `docs/ask-architecture-reference.md` | Only if "where to put new code" needs the model view and its rebuild points |
| `docs/reference/` | Regenerate with the drift test; never edit by hand |
| `AGENTS.md` | The `ask` options summary (models.json, `--offline`) |

Run the `ask-docs-sync` skill (`.claude/skills/ask-docs-sync/SKILL.md`) after the code phases.

## Manual check (not in CI)

- `python3 e2e/tui/real_provider_scenarios.py acceptance` with a real key, once, by hand, as the repository describes; plus `go build -o ask ./cmd/tui` and `ask --model anthropic/<id>:high -p "hi"` with a saved key. Record the result in the plan, not in code.

## Validation

- Every acceptance criterion in [plan.md](./plan.md#acceptance-criteria) has a test above or in phases 1-8.
- `git diff master -- '*.go' | rg '^\+' | rg '(//|func Test).*(\b[DQMUP][0-9]{1,2}b?\b|[Ff]inding [0-9]+|[Rr]ed [Tt]eam)'` finds no plan label in a new comment or test name. Commit messages follow the same rule.

## Risk

| Risk | L x I | Mitigation |
|---|---|---|
| A rollback leaves a saved default on a custom row | Low x Low | The rollback section of plan.md documents the manual fix |
| Leader e2e flaky under full-suite load | Med x Low | Run a failing one alone first (known flakes in the earlier plan); do not raise a time limit before that |
| A test touches the network | Low x High | `httptest` and injected transports only; `ASK_DISABLE_MODEL_REFRESH=1` in every e2e |

## Rollback

Revert the doc commit with the code phases it describes. Tests go with their code phase.

## Implementation checklist (2026-11-07)

- [x] Print tests in `cmd/tui/print_models_file_test.go`: `TestPrintModelPatternWithLevelUsesSavedAnthropicKey`, `TestPrintLocalProviderFromModelsFile` (httptest server, `Authorization` holds the file key, `max_tokens` 16384), `TestPrintModelsFileEnvTemplateKey`.
- [x] Leader process and `ask acp` tests in `cmd/tui/models_file_e2e_test.go`: `TestLeaderFileKeyProviderIsUsable`, `TestLeaderKeepsLastValidModelsFile`, `TestReplacedLeaderShowsModelsFileProblem` (a newer-build wrapper replaces the leader that loaded the file), `TestACPKeepsLastValidModelsFile`, `TestProjectModelsFileIsNotRead`.
- [x] `TestLeaderRefreshKeepsSessionModel` is not a new test: `TestModelRefreshKeepsOverrideAndSessionModel` (`internal/app`, real `LeaderRuntime` on a Unix socket) proves it.
- [x] e2e case `ask-model-cycle` in `e2e/tui/run.py`; a three-entry scope tells `ctrl+shift+p` from `ctrl+p`. Run 2026-11-07 on Alacritty and Ghostty: pass (faux and a file-only provider, no prompt); after the case got `ASK_DISABLE_MODEL_REFRESH=1`, run again on Ghostty: pass.
- [x] Docs: `docs/cli.md`, `CHANGELOG.md`, `README.md` (key order link), `AGENTS.md` (`ask acp`, `ask settings show`, model flags), `e2e/tui/README.md`. Package READMEs were already current. `docs/reference/` had no drift.
- [ ] Manual check with a real key (`real_provider_scenarios.py acceptance`, `ask --model anthropic/<id>:high -p "hi"`): not done in this run; it needs a real key and is by hand only.
