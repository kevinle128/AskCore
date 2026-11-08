---
title: "H7 model configuration and resolution"
description: "Add the user models.json, Pi value templates with a command cache, the models.json key as the third credential source, the Pi model-id grammar with the omp colon rule, --models, enabledModels, --offline and a TUI model-cycling key."
status: completed
priority: P1
effort: 13.5d
branch: develop
tags: [feature, backend, providers, auth, settings, cli, tui]
blockedBy: []
blocks: []
created: 2026-11-05
mode: hard
---

# H7 model configuration and resolution

## Outcome

<!-- Red Team 2026-11-06: Q2b (Findings 3, 4) -->
A user adds providers and models in `<ASK_HOME>/models.json` without a rebuild, and explicit fields win after every model list refresh. Ask does not read a project `.ask/models.json` (user decision Q2b, 2026-11-06).
`ask --model anthropic/<id>:high` resolves the model and the thinking level, and a key saved in `auth.json` is used.
A malformed file keeps the last valid copy in a process that loaded one, and shows each problem. A background refresh never replaces the selected model.
`--models`, `enabledModels` and a TUI key that cycles through the scoped models work.

## Why

H-PROV-17 (models.json) is missing: only the trust gate names the file (`internal/workspace/project.go:24`).
H-PROV-19 is partial: `parseModelID` reads `provider/id@api` only (`internal/acp/ask_methods.go:121-134`), and `--model` is a plain string (`cmd/tui/args.go:105`).
H-PROV-16 is done except `--offline` ([account model discovery](../261028-1310-account-model-discovery/plan.md)).

## Authority

| Link | Read it for |
|---|---|
| [Xia decision matrix](../reports/xia-261105-1645-h7-model-config-resolution.md) | D1-D14, user decisions Q1-Q3, and "Superseded (2026-11-06)": Q2b, QF, QG |
| [Red-team adjudication](./reports/red-team-adjudication-261106-1401.md) | 15 merged findings and the binding user review log |
| [omp report](../reports/researcher-261105-1631-omp-model-config-resolution.md), [DSH report](../reports/researcher-261105-1631-deepseek-model-config-resolution.md) | Source anatomy with citations |
| [Roadmap H7](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#h7-model-catalog-and-resolution), inventory rows H-PROV-16/17/19, H-AUTH-01 | Scope, Pi defaults, exit test |
| [Account model discovery](../261028-1310-account-model-discovery/plan.md) | U1-U9 and P1-P18: do not reverse |
| `docs/ask-architecture-reference.md` section 3, `.golangci.yml:155-170` | `settings` is stdlib only (tests too); `providers` imports neither `settings` nor `auth` |
| `docs/async-task-runtime.md` | No sleep, no polling, no watcher |

## Decisions

The accepted decisions D1-D14, Q1-Q3 and Q2b are in the [xia report](../reports/xia-261105-1645-h7-model-config-resolution.md#decision-matrix). The plan decisions M1-M19 and the guard against reversal of U1-U9/P1-P18 are in [decisions.md](./decisions.md). Every decision maps to a phase there.

## Phases

<!-- Red Team 2026-11-06: effort and dependencies after Findings 1-15 -->
| # | Phase | Effort | Depends on | Status |
|---|---|---|---|---|
| 1 | [models.json loader](./phase-01-models-file-loader.md) | 1.5d | none | Pending |
| 2 | [Value templates and key order](./phase-02-value-templates-key-order.md) | 2d | none | Pending |
| 3 | [Custom rows and merge order](./phase-03-custom-rows-merge.md) | 1d | none | Pending |
| 4 | [Model-id grammar and scope patterns](./phase-04-model-id-grammar.md) | 1.5d | none | Pending |
| 5 | [Model view and composition](./phase-05-session-model-view.md) | 2d | 1, 2, 3, 4 | Pending |
| 6 | [CLI flags, enabledModels and wire](./phase-06-cli-flags-wire.md) | 2.5d | 1, 4, 5 | Pending |
| 7 | [TUI design gate (user approval)](./phase-07-tui-design-gate.md) | 0.5d | 4 | Pending |
| 8 | [TUI model cycling and reload problems](./phase-08-tui-cycling.md) | 1d | 6, 7 approved | Pending |
| 9 | [Acceptance, extension trace and docs](./phase-09-acceptance-docs.md) | 1.5d | 5, 6, 8 | Pending |

Phases 1-4 run in parallel: each owns other files. Phase 4 changes `parseModelID` in `internal/acp/ask_methods.go`; phases 5 and 6 change other parts of that file later. Phases 1 and 6 both change `internal/settings/settings.go`; phase 6 starts after phase 5, which waits for phase 1. Phase 7 is design only and can run beside phases 5 and 6.

## Acceptance criteria

- [x] `ask -p --model anthropic/<id>:high "hi"` with only an `auth.json` Anthropic key sends that key and thinking level `high` (real `runWithDependencies` path, injected inference transport).
- [x] A models.json provider on an OpenAI-compatible local server (httptest) answers `ask -p --model local/<id>` with no Go change for that provider, and the provider is usable in the leader (start choice, saved default, `setModel`, `usableOnly`). <!-- Red Team 2026-11-06: Finding 1, Finding 2 -->
- [x] `modelOverrides` values win after a cache write that changes the same row.
<!-- Red Team 2026-11-06: Finding 14 -->
- [x] In a leader or an `ask acp` process that loaded a valid copy before the file broke, the custom rows stay; each problem shows in the session diagnostics and the models result. A process that never loaded a valid copy shows the problem and no custom rows. `ask settings show` shows the problem in both cases.
- [x] A `.ask/models.json` in a trusted project is not read. <!-- Red Team 2026-11-06: Q2b (Findings 3, 4) -->
- [x] Key order is `--api-key`, saved credential, models.json `apiKey`, env, with the binding rules of M3; `ask settings show` names the winning source from the same function. <!-- Red Team 2026-11-06: QG -->
- [x] After an edit of the user file, a session on a changed custom row gets `model configuration changed; choose the model again`, and no request mixes an old row with a new key or header. <!-- Red Team 2026-11-06: Finding 8 -->
- [x] The id-grammar table test passes: `provider/id`, exact id, alias and dated ids, fuzzy in one provider, globs in scope lists, `@api`, the omp colon rule (`qwen3-coder:30b` and `x:max` as literal ids, `kimi:high` not `kimi-highspeed`), an invalid level on the command line is an unknown model, an ambiguous bare id lists its choices. <!-- Red Team 2026-11-06: Finding 7 -->
- [x] A new TUI against a leader that drops `--model` fails the session start with a clear message. <!-- Red Team 2026-11-06: Finding 11 -->
- [x] `--offline` equals `ASK_DISABLE_MODEL_REFRESH=1` for the process that reads it.
- [x] The TUI cycling key follows the approved `design.md`.
- [x] `go vet ./...`, `go test ./...` (no network request), lint, and `ASK_UPDATE_DOCS=1 go test -run '^TestReferenceDocs$' ./cmd/tui` pass. No `time.Sleep`, ticker or file watcher in product code of this change. (2026-11-07: the full run had two load failures outside this change, `TestNewerBuildReplacesABusyLeader` and `TestUntrustedNoticeShowsSource/no_ui`; both pass alone and in a package rerun.)

Evidence for each criterion (test names): [phase 9 checklist](./phase-09-acceptance-docs.md#implementation-checklist-2026-11-07) and the phase 9 completion message.

## Rollback

Each phase is one commit or more. Phases 1-4 add code with no caller (phase 1 also splits the settings reader with no behavior change, and phase 4 moves `parseModelID` onto the shared parser), so a revert of one changes no behavior. Phase 5 switches the runtime to the config-aware snapshot; revert phases 9, 8, 6 and 5 together to return to today's behavior (models.json is then ignored again, and `auth.json`/env keys keep working). Phase 6 wire fields are `omitempty`, so an older peer ignores them. Ask never writes the user's models.json, so no data migration exists.

<!-- Red Team 2026-11-06: Finding 14 -->
A saved default that points at a custom row (for example `local/qwen3-coder@openai-completions`) stays in `settings.json` after a rollback. The older build cannot find that row, so a new session falls back with the note `not in the catalog`. The manual fix: choose a model with `/model` and save it as the default, or remove the `defaultModel` key from `<ASK_HOME>/settings.json`. `docs/cli.md` states this in the models.json section.

## Risks

| Risk | L x I | Mitigation |
|---|---|---|
| A file-only provider is refused at one usable site | Med x High | One usable rule for every site (M18); a leader test per site (phase 9) <!-- Red Team 2026-11-06: Finding 2 --> |
| A row and its key or headers come from two versions of the file | Low x High | Triple lookup and the fixed failure text (M2, phase 5) <!-- Red Team 2026-11-06: Finding 8 --> |
| `!command` hangs, prints a secret, or one cancellation blocks all sessions | Med x Med | Entry-owned 10 s run, output never logged, caller cancellation never cached, 30 s backoff (phase 2) <!-- Red Team 2026-11-06: Finding 10 --> |
| A custom row sends `max_tokens: 1` | Low x High | Custom rows always carry limits (M7, phase 3) |
| Silent model change after refresh or reload | Low x High | Snapshot swap never touches a session model (phase 5 test) |
| A grammar change breaks saved defaults | Low x Med | Saved defaults and `_ask/session/setModel` keep exact `provider/id@api` (M13) |
| An older leader drops the new session options | Med x Med | Host echo and a client check (phase 6) <!-- Red Team 2026-11-06: Finding 11 --> |

## Unresolved questions

QA, QB, QD (2026-11-05) and QG (2026-11-06) are answered; QE and QF are moot after Q2b (see [decisions.md](./decisions.md#unresolved-questions)). Only QC remains: the user chooses the cycling key at the phase 7 design approval.

## Red Team Review

### Session — 2026-11-06
**Findings:** 15 (13 accepted and applied, 2 closed by user decision Q2b, 0 rejected)
**Severity breakdown:** 3 Critical, 9 High, 3 Medium

Source: [adjudication](./reports/red-team-adjudication-261106-1401.md) and its user review log (binding).

| # | Finding | Severity | Disposition | Applied To |
|---|---------|----------|-------------|------------|
| 1 | A models.json key fails `keySnapshot` for every custom provider | Critical | Accept: `configSnapshot` binds only `Endpoint == m.BaseURL` | Phase 2, decisions M3, trace |
| 2 | A provider with only a file key is never usable | Critical | Accept: one usable rule over the snapshot | Phase 5, Phase 9, M18 |
| 3 | Project `$NAME` sends any env var to a project host | Critical | Closed by Q2b (no project file) | Phases 1, 2, 5 (project parts removed) |
| 4 | Project `apiKey` on an existing provider replaces the user's key | High | Closed by Q2b | Phase 5 (project limits removed) |
| 5 | Project rows replace built-in rows; account filter runs before a user `baseUrl` change | High | Accept, user-layer part only: a re-pointed provider has unknown account access | Phase 3, M14 |
| 6 | "Existing provider" defined three ways; ids outside the store rule | High | Accept: store id rule, one definition, case-folded duplicates ambiguous | Phases 1, 4, 5, M17 |
| 7 | Colon-tagged ids fail at `parseArgs`; grammar repeats `findIn` | High | Accept as omp `parseModelPatternWithContext`; CLI without fallback; exact steps on `findIn`; D10 stays; level check after resolution | Phases 4, 6, M13, M19 |
| 8 | View rebuilt per request; key and headers by provider name only | High | Accept: rebuild at session start, `/model`, `setModel`; triple tables; fixed failure text; `notInConfig` | Phases 5, 7, 8, M1, M2, D14 |
| 9 | Phase 1 copies hardened readers; false lock claim; two stamps; stamp before open | High | Accept: shared byte reader, stamp from the opened file, read outside the lock, one `ModelsFiles` with a generation, FIFO and symlink tests | Phases 1, 5, M11 |
| 10 | Cancelled first caller poisons the command cache; 401 forget races | High | Accept: entry-owned run, caller cancellation not cached, relay forgets before the terminal item | Phases 2, 5, M6 |
| 11 | Older leader drops `--model`; no bounds; cycling needs two RPCs | High | Accept: host echo and client check, bounds, `ThinkingLevel` on `ACPModelRequest`, presses ignored while switching | Phases 6, 7, 8 |
| 12 | `ask -p` Token Plan branch skips grammar, model set and models.json | High | Accept: only `faux` stays on the switch | Phases 5, 6, M4 |
| 13 | `--api-key` accepted at any config endpoint | Medium | Accept (merged into Finding 1 after Q2b): only for a provider the user file defines or re-points | Phase 2, M3 |
| 14 | Project row saved as user default; last valid copy is memory only; no `ask acp` test | Medium | Accept (project part moot): narrowed acceptance, rollback manual fix, `TestACPKeepsLastValidModelsFile`, all three model set builds get the config | Plan, Phases 5, 9 |
| 15 | Diagnostics print user key names; project symlink out of root | Medium | Accept (symlink part moot): schema segments and positions only | Phase 1 |
| — | Merged: `KeySource` repeats the resolver order (QG) | — | Accept: one `selectSource` with a dry-run flag; M3 text matches phase 2 | Phases 2, 5, M3 |
| — | Merged: a requested level dropped with no notice | — | Accept: warning line and session notice | Phases 4, 6, 7, M16 |

### Whole-Plan Consistency Sweep
- Files reread: plan.md, decisions.md, phase-01-models-file-loader.md, phase-02-value-templates-key-order.md, phase-03-custom-rows-merge.md, phase-04-model-id-grammar.md, phase-05-session-model-view.md, phase-06-cli-flags-wire.md, phase-07-tui-design-gate.md, phase-08-tui-cycling.md, phase-09-acceptance-docs.md.
- Decision deltas checked: 12 (Q2b project file removed; per-process view without `SettingsScope`; rebuild points; triple key and header tables; `NotInConfig`; `configSnapshot` and the override rule; shared `selectSource`; command cache ownership and the relay; shared byte reader and generation; provider id rule and one existing-provider definition; omp colon rule with `findIn`; host echo, bounds and one-call model and level).
- Reconciled stale references: 9 (the plan-label range in phase 9 to M1-M19; the decision lists of phases 1, 2, 4 and 5; a wrong U1 label in decisions.md and phase 3, now the account rule of `internal/providers/model_set.go:37`; the "no goroutine" claim of the 401 relay in phase 5, now "only the producer that `NewStream` owns"; the D9 row and the project limits, now superseded by Q2b in every file).
- Searches with no stale hit left: `SettingsScope` (only in the phase 5 explanation of why it was dropped), `AllowCommand` (only the removal note), `ErrUnknownLevel`, `Provenance`, `layers`, `TestParseModelPattern`, `readProjectSettings`, `two-session`, `Token Plan demo`.
- Kept on purpose: `enabledModels` may sit in the project layer of `settings.json` (that file keeps its H6a trust rules; only `models.json` lost its project layer). The phase 5 file name stays `phase-05-session-model-view.md` for link stability; its title is now "Model view and composition".
- Unresolved contradictions: 0. Only QC (the cycling key, a user design approval) is open, and it gates phase 8 only.
