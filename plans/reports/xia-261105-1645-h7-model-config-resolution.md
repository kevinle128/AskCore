# Xia (--improve): H7 model configuration and resolution

## Source manifest

| Source | Path | Commit | Report |
|---|---|---|---|
| oh-my-pi (omp) | `/Users/dale/Desktop/workspace/opensources/oh-my-pi` | `efbc171` | [omp report](./researcher-261105-1631-omp-model-config-resolution.md) |
| deepseek-harness (DSH) | `/Users/dale/Desktop/workspace/opensources/deepseek-harness` | `5badb15` | [DSH report](./researcher-261105-1631-deepseek-model-config-resolution.md) |
| Pi (reference) | `/Users/dale/Desktop/workspace/opensources/pi` | `4c6fb7c` | cited inside the omp report |

Both sources are read as data only. No code is copied; Ask rewrites the design in Go.

## Scope of H7 that is still open

The [roadmap H7 entry](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#h7-model-catalog-and-resolution) owns H-PROV-16, H-PROV-17 and H-PROV-19.
The [account model discovery plan](../261028-1310-account-model-discovery/plan.md) delivered H-PROV-16 (generated models.dev catalog, overlay, ETag cache, account lists).

| Item | State in Ask | Evidence |
|---|---|---|
| H-PROV-16 catalog, cache, ETag | Done | `internal/providers/modelsdev_fetch.go:37-90`, `internal/settings/model_cache.go:80` |
| H-PROV-16 offline switch | Env only (`ASK_DISABLE_MODEL_REFRESH`), no `--offline` | `cmd/tui/acp.go` |
| H-PROV-17 `models.json` | Missing. Only the trust gate lists `.ask/models.json` | `internal/workspace/project.go:24` |
| H-PROV-19 resolution | Partial: `provider/id@api` only; no `:level` suffix, no fuzzy, no globs, no parse table test | `internal/acp/ask_methods.go:115-134` |
| Exit test | `--model anthropic/<id>:high` fails today | `cmd/tui/args.go:105` |

## Anatomy compared

| Concern | Pi | omp | DSH | Ask today |
|---|---|---|---|---|
| File | `~/.pi/agent/models.json`, JSON with comments | `~/.omp/agent/models.yml`, one-time JSON migration | `providers` block in profile `cordis.patch.yml` | none |
| Project layer | no | no | no | `.ask/models.json` is gated by trust (user decision 2026-11-04) |
| Schema | typebox | omptype | strict check before write | settings: stdlib only |
| Merge | built-ins, then custom by id, then overrides | catalog, then discovered, then custom, then overrides, on each rebuild | pinned pi-ai catalog plus overrides | built-in, models.dev, accounts (`model_set.go:72`) |
| Defaults for new model | 128000 ctx, 16384 out, text, no reasoning, price 0 | same | guessed context | family rule defaults |
| Reasoning map | `thinkingLevelMap` | same | `reasoningEfforts` map per level | `ThinkingLevelMap` on `Model` |
| Value syntax | `$NAME`, `${NAME}`, `!cmd`, `$$`, `$!`; bare string is literal | `!cmd` or env name, else literal | env name only | n/a |
| Command cache | none (per request); a failed command is cached forever | cached per process, 30 s backoff, 401 clears | no commands | n/a |
| Key order | runtime, auth.json, models.json, env | runtime, models file, OAuth, `/login` key, env | env only | explicit, saved, env (H7a) |
| Bad file | error shown, custom models lost | one error drops every custom provider | refused before write; per-model errors | n/a |
| Reload | when `/model` opens | when picker opens and mtime changed | on write | n/a |
| Id grammar | `provider/id`, exact, fuzzy, globs, `:level` (last colon); ambiguous bare id rejected | same plus `@upstream`; ambiguous id picked by ranking | none (struct selection) | `provider/id@api`; ambiguous rejected (`ask_methods.go:246`) |
| Scope list | `--models`, `enabledModels`, ctrl+P cycling | same | none | none |

## Dependency matrix (source part to Ask)

| Part | Ask owner | Status |
|---|---|---|
| File read, JSONC strip, schema check, mtime reload | `internal/settings` (stdlib only) | NEW |
| Value templates (`$NAME`, `!cmd`) | `internal/auth` (resolves secrets per request) | NEW |
| Custom row to `providers.Model` mapping, merge order | `internal/providers` (`ModelSet`), fed by `internal/app` | EXISTS, extend |
| Credential order with a file key | `internal/auth` resolver (H7a) | EXISTS, CONFLICT on order |
| Id grammar, fuzzy, globs, `:level` | `internal/providers` (pure functions) | NEW |
| `--model`, `--thinking`, `--models`, `--offline` | `cmd/tui/args.go` | EXISTS, extend |
| Trust gate for `.ask/models.json` | `internal/workspace` | EXISTS |
| Problems shown to user | `ask settings show`, TUI settings problems | EXISTS, extend |

## Decision matrix

| # | Decision | Pi | omp | DSH | Recommendation |
|---|---|---|---|---|---|
| D1 | File name and format | `models.json` JSONC | YAML | YAML block | `models.json` JSONC. The name is already in the gated list. |
| D2 | Merge order | custom, overrides | catalog, discovered, custom, overrides each rebuild | overrides | omp order on each `ModelSet` build, so overrides win after a refresh. |
| D3 | Custom rows and family rules | n/a | n/a | n/a | A row that the user defines with `api` needs no family rule (the user states the wire). An override on a built-in row keeps the family rule. Account-list ids keep rule U2. |
| D4 | Bad file | custom lost | all custom lost | refuse at write | Keep the last valid parsed copy in memory, show the problem in `ask settings show` and the TUI. Bad single model: drop only that row (DSH per-model error). |
| D5 | Unknown keys | typebox rejects | rejects | ignores silently | Load, and report each unknown key as a problem (typo help, no hard fail). |
| D6 | Value syntax | Pi templates | env-name-else-literal | env name | Pi templates. A bare string is a literal. Never send a variable name as a key. |
| D7 | `!command` cache | per request; failure cached forever | process cache, 30 s backoff, 401 clears | none | omp cache rules (process cache, backoff, clear on 401). Run with a timeout, no shell output in logs. |
| D8 | Key order | stored beats file | file beats stored OAuth | env only | Pi order (user Q1). |
| D9 | Project file powers | n/a | n/a | n/a | Limited (user Q2). |
| D10 | Ambiguous bare id | reject | pick by ranking | n/a | Reject and list the choices. Ask already does this, and the inventory row says so. |
| D11 | Id grammar | `provider/id`, exact, fuzzy, globs, `:level` | adds `@upstream` | none | Pi grammar plus Ask's existing `@api`. Split `:level` at the last colon; an unknown level is an error. Fuzzy only after exact and alias fail, never across providers silently. |
| D12 | Offline | `--offline`, `PI_OFFLINE` | same | n/a | Add `--offline` as an alias of `ASK_DISABLE_MODEL_REFRESH`. |
| D13 | `--models`, `enabledModels`, cycling | yes | yes | no | All three in H7 (user Q3). |
| D14 | Reload | `/model` opens | `/model` opens, mtime changed | write | omp: check mtime when `/model` opens and when a session starts. No watcher, no polling. |

## Risks

| Risk | Score | Mitigation |
|---|---|---|
| A project `models.json` sets `baseUrl` on a built-in provider and sends the user's stored OAuth token to another host | High | Q2. Trust gate exists, but trust today means "load instructions", not "send my token elsewhere". |
| `!command` in a project file runs code at model selection | Medium | Q2. Trust gate; the bash tool already runs code after trust. |
| File key versus stored subscription order surprises users | Medium | Q1, plus `ask settings show` prints which source gives the key (never the value). |
| Override after refresh lost | Low | Merge on each build, test after a simulated refresh. |
| Import rules | Low | `settings` parses (stdlib), `auth` resolves values, `providers` maps rows; `app` wires. |

Overall risk: medium. The parser and grammar are simple. The credential order and the project file powers are the parts that can hurt users.

## User decisions (2026-11-05)

| # | Decision |
|---|---|
| Q1 | Pi key order: `--api-key`, then the saved credential (auth.json or OAuth), then the `models.json` key, then env. The H7a order stays; the file key is the third source. |
| Q2 | A trusted project `.ask/models.json` is limited. It may add new providers and models and override metadata fields. It may not change `baseUrl` or `headers` of a provider that already exists, and it may not use `!command`. A saved credential is never sent to a host that the project chose. |
| Q3 | `--models`, the `enabledModels` setting and a TUI model-cycling key are in H7. |

### Superseded (2026-11-06, after red-team)

| # | Decision |
|---|---|
| Q2b | The user decided to drop the project `.ask/models.json` from H7. Ask reads only `<ASK_HOME>/models.json`, as Pi (`pi/packages/coding-agent/src/config.ts:581`), omp and DSH do. This replaces Q2. `.ask/models.json` stays in the gated list of `internal/workspace/project.go:24`, but no code reads it. Reason: no reference harness has a project models file, and the red-team found that it is the source of the env exfiltration, provider shadowing and key takeover risks. |
| QF | Answered before Q2b ("allow project apiKey on existing provider"); moot after Q2b. |
| QG | Keep the per-provider key source in `ask settings show`, built on the same source-selection function as the resolver. |

## Unresolved questions

None.
