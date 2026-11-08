# H7 decisions, mapping and guards

Back to [plan.md](./plan.md).

## Accepted decisions mapped to phases

Source: [xia report](../reports/xia-261105-1645-h7-model-config-resolution.md), including the section "Superseded (2026-11-06, after red-team)". These are not reopened here.

| # | Decision (short) | Phase |
|---|---|---|
| D1 | `models.json` JSONC at `<ASK_HOME>/models.json`; parser in `internal/settings`. <!-- Red Team 2026-11-06: Q2b (Findings 3, 4) --> The project file is dropped (Q2b). | 1 (read), 5 (wiring) |
| D2 | Merge on each build: catalog, models.dev overlay, account lists, custom models, `modelOverrides` | 3 (merge), 5 (every snapshot build) |
| D3 | A user row that states `api` needs no family rule; an override on a built-in row keeps the rule; account-list ids keep U2 | 3 |
| D4 | Bad file: keep the last valid parsed copy in the process; bad model drops only that row; show problems | 1 (memory, row drop), 5 (diagnostics), 8 (problem on `/model` open) |
| D5 | Unknown keys load and give one problem each (the text names a schema key, never user text) | 1 |
| D6 | `$NAME`, `${NAME}`, whole-value `!command`, `$$`, `$!`; a bare string is a literal | 2 |
| D7 | `!command`: process cache, 30 s backoff after a failure, clear on 401, timeout, output never logged; resolution in `internal/auth` | 2 (cache, grammar), 5 (401 relay, runner wiring) |
| D8 / Q1 | `--api-key` > saved credential > models.json `apiKey` > env | 2 |
| ~~D9 / Q2~~ | <!-- Red Team 2026-11-06: Q2b (Findings 3, 4) --> Superseded by **Q2b** (user, 2026-11-06): Ask reads no `.ask/models.json`, as Pi (`pi/packages/coding-agent/src/config.ts:581`), omp and DSH. The name stays in the gated list (`internal/workspace/project.go:24`); nothing reads it. | none (phase 9 proves the file is not read) |
| D10 | Ambiguous bare id is rejected with the list of choices | 4 |
| D11 | Pi grammar plus `@api`; omp colon rule (user 2026-11-06); parse table test; initial model priority (H-PROV-19) | 4 (grammar), 6 (initial priority; reading M15, user QD) |
| D12 | `--offline` is an alias of `ASK_DISABLE_MODEL_REFRESH` on `ask`, `ask acp` and `ask leader`; `ask auth` keeps the variable only | 6 |
| D13 / Q3 | `--models`, `enabledModels`, TUI cycling key (design approved first) | 4 (globs), 6 (flag, setting, wire), 7 (design gate), 8 (TUI) |
| D14 | Reload by mtime at session start and when `/model` opens; no watcher, no polling. <!-- Red Team 2026-11-06: Finding 8 --> `setModel` is the third rebuild point; the request path never reloads. | 5 |
| Custom defaults | 128000 context, 16384 output, text only, reasoning off, omitted prices 0 | 3 |
| Exit | `--model anthropic/<id>:high` resolves, `auth.json` key used; malformed config keeps the last valid copy in a process that loaded one; refresh keeps the selected model | 9 |
| QG | <!-- Red Team 2026-11-06: QG --> Keep the per-provider key source in `ask settings show`, built on the same source-selection function as the resolver (user, 2026-11-06) | 2, 5 |

## Plan decisions (this plan)

Each one cites the code that forces it.

| # | Decision | Evidence |
|---|---|---|
| M1 | <!-- Red Team 2026-11-06: Q2b, Finding 8 --> **Per-process model view with fixed rebuild points.** After Q2b the only config file belongs to the process, so `acp.Config.Models` and `FindModel` keep their signatures. The snapshot (catalog, overlay, accounts, user file, key and header tables) is rebuilt only at session start, when `/model` opens, in `setModel`, and after a cache write. The request path reads the stored snapshot with an atomic load. | `internal/acp/agent.go:41-48`; `internal/app/model_refresh.go:185-213,245-275`; call sites `internal/app/module_acp.go:136,223`, `internal/acp/ask_methods.go:175,244` |
| M2 | <!-- Red Team 2026-11-06: Finding 8 --> **No secret on a model row.** A `providers.Model` never holds a key or a value template. The snapshot has a key table and a header table keyed by `(Provider, API, BaseURL)`; `AuthRunner` reads them for the session row, and `auth` resolves the values per request. A row of user origin whose triple left the tables fails with `model configuration changed; choose the model again`. | Rows are cloned into snapshots and wire rows (`internal/providers/model_set.go:276-292`, `internal/acp/ask_methods.go:215-221`); `internal/app/module_auth.go:14-50`; `internal/auth/service.go:131-186` |
| M3 | <!-- Red Team 2026-11-06: Finding 1, Finding 13, QG --> **Binding rules.** (a) A saved credential whose method does not match `(m.Provider, m.API, m.BaseURL)` is skipped only when a models.json key is present for that provider and binds to the row endpoint; in every other case the result stays `ErrMethod`, as today. (b) The env key keeps the registered-method rule. (c) A models.json key makes a snapshot through `configSnapshot`, which checks only `ConfigKey.Endpoint == m.BaseURL` and never reads the method table. (d) `--api-key` keeps its provider pin and is also accepted at a config endpoint; a config endpoint exists only when the file entry of a provider that the user file defines or re-points also sets `apiKey`, so without that `apiKey` a re-pointed provider refuses `--api-key` with `ErrMethod` (aligned with code 2026-11-07). (e) One `selectSource` function holds this order for `ResolveWith` and the `KeySource` dry run. User QA (2026-11-05): keep (a). This text matches phase 2 requirements 4-7. | `internal/auth/service.go:121-128,138-140,162-165,247-259`; `internal/providers/keys.go:8-38` |
| M4 | <!-- Red Team 2026-11-06: Finding 12 --> **Wire by API, not by provider name.** Print mode resolves the pattern to a `Model` and passes it as `spec.Start`; the native agent already registers the three wires by API and the transports pin to `m.BaseURL`. The provider-name switch keeps only `faux`; Token Plan goes through the model set too. | `cmd/tui/headless_faux.go:78-101`; `cmd/tui/headless.go:262-267`; `internal/app/agent_native.go:78-82`; `internal/providers/openai/profiles.go:125-127,154`; `internal/providers/anthropic/provider.go:50-59` |
| M5 | **Two schema types.** `settings` parses into its own stdlib types; `app` maps them to a `providers` input type. The import rules allow no shared type. | `.golangci.yml:155-170` |
| M6 | **Command runner injected.** `auth` gets a `func(ctx, command string) (stdout string, err error)` from `app`. `app` builds it on `sandbox.Run` with `Dir` = the user home folder and `Env` = the full process environment, because the default child environment drops credential names that a secret command needs. <!-- Red Team 2026-11-06: Finding 10 --> The cache entry owns the run context (`WithTimeout(WithoutCancel(ctx), 10s)`); a caller cancellation is never cached. | `internal/auth/README.md:31-35`; `internal/sandbox/process.go:45-53`; `internal/sandbox/run.go:11-24` |
| M7 | **Custom rows carry limits.** A new custom row gets 128000 and 16384 when the file omits them, and `CostUnknown` false with zero prices. `RequestLimits` fills limits only from a family rule with the same API, so a row without limits would send 0. | `internal/providers/catalog_rules.go:172-188`; prior red-team Critical 2 (`max_tokens: 1`) |
| M8 | `--thinking` wins over a `:level` suffix. | Pi `packages/coding-agent/src/main.ts:492,523` |
| M9 | `--models` and `enabledModels` choose the start model and the cycle list only. `/model` keeps the full list. | Pi `main.ts:505-516`; omp report Q4 |
| M10 | `--offline` applies to the process that reads it and to a leader that this client starts (the child inherits the environment). A leader that already runs keeps its own setting; `docs/cli.md` says so. | `cmd/tui/acp.go:125-127`; `internal/leader/spawn.go:239-279` |
| M11 | <!-- Red Team 2026-11-06: Finding 9, Q2b --> The user `models.json` follows the user `settings.json` file rules (0600 file, 0700 home) through one shared byte reader; a wrong mode is a file problem. User QB (2026-11-05): 0600 is required. | `internal/settings/settings.go:156-184,346-365` |
| M12 | Print mode and `ask settings show` read the file once. They have no earlier copy, so a bad file gives a warning and no custom rows. | `cmd/tui/headless.go:181-205`; `internal/app/model_refresh.go:288-297` |
| M13 | Saved defaults and `_ask/session/setModel` keep the exact `provider/id@api` name. The pattern grammar serves `--model`, `--models`, `enabledModels` and session options only. <!-- Red Team 2026-11-06: Finding 7 --> `parseModelID` and the grammar share `providers.ParseRef`. | `internal/app/default_model.go:135-151`; `internal/acp/ask_methods.go:121-134,240-247` |
| M14 | <!-- Red Team 2026-11-06: Q2b --> The user file may set `baseUrl` or `headers` on a built-in provider. The family rule still owns API, compat and effort words of built-in rows. <!-- Red Team 2026-11-06: Finding 5 --> A provider whose `baseUrl` the file changes has unknown account access (no account filter). | `internal/providers/catalog_rules.go:32-90`; `internal/providers/model_set.go:55-58`; `internal/app/model_refresh.go:257-269` |
| M15 | **Reading of D11 (user QD, 2026-11-05: confirmed).** When the saved default is inside the scope, Pi starts on the saved default, not on the first scope entry. The plan follows Pi; without a scope the D11 order is unchanged. | Pi `packages/coding-agent/src/main.ts:500-517` (`savedInScope` before `scopedModels[0]`); inventory row H-PROV-19 |
| M16 | <!-- Red Team 2026-11-06: reasoning notice --> **Dropped level notice.** When the clamp of a row drops a requested level (for example a custom row with reasoning off), print mode writes one warning line and the TUI host adds a session notice. | `internal/providers/clamp.go:22`; custom default reasoning off (phase 3) |
| M17 | <!-- Red Team 2026-11-06: Finding 6 --> **Provider ids and "existing provider".** A file provider id must match the store rule (lower case `a-z`, `0-9`, `-`); another id is a provider problem. "Existing provider" has one definition in `app`: built-in ids, providers of registered auth methods, providers of the user file. Two rows equal after case folding are ambiguous. | `internal/settings/auth.go:100-109,362-364`; `internal/app/auth_native.go:16-31`; `internal/providers/catalog.go:126-151` |
| M18 | <!-- Red Team 2026-11-06: Finding 2 --> **One usable rule.** A provider is usable when the store status says so or when the snapshot has a file key for it (dry-run source `config:*`). Every usable site calls this one function. | `internal/app/module_acp.go:303-314`; `internal/app/default_model.go:59-65,147-148`; `internal/acp/ask_methods.go:192,263`; `internal/app/provider_setup.go:45,147-149` |
| M19 | <!-- Red Team 2026-11-06: Finding 7 --> **omp colon rule** (user, 2026-11-06): whole pattern exact first; a valid level suffix is stripped only after that, and a real id that ends in `:<level>` wins; with no valid suffix, alias and fuzzy run on the whole pattern; an invalid suffix falls back to the prefix with a warning, except in CLI and session-option resolution, where it is `ErrUnknownModel`. Exact steps call `findIn`; D10 stays. | omp `packages/coding-agent/src/config/model-resolver.ts:900-987,2359-2366`; `internal/providers/catalog.go:126-151` |

## Guard: earlier decisions that stay

| Earlier decision | How this plan keeps it |
|---|---|
| Account rule "unknown access keeps all rows" (provider auth design, Q4=A in the account plan) | Unchanged for every provider; a re-pointed provider is treated as unknown (M14), which is the same rule (`internal/providers/model_set.go:37-40`). |
| U2: an account-list id with no family rule is hidden | `fallbackRow` is unchanged (`internal/providers/model_set.go:141-164`). Custom rows enter after the account filter (D2), so they are user rows, not account rows. |
| U3/P13: `?` for unknown context | Custom rows always have limits (M7). An override with `contextWindow: 0` is refused as a bad value, so it cannot create a false unknown. |
| U6 and criteria: a refresh never changes the selected model | The snapshot swap never calls `Agent.SetModel`, which runs only on a client request (`internal/acp/ask_methods.go:226-285`). Phase 5 test. |
| P1: family rule owns request shape of generated rows | Overrides on built-in rows cannot set API, compat or new effort words (D3, phase 3). |
| P5: `ModelSet` is an immutable snapshot | Phase 3 adds inputs to the constructor; no setter. |
| P7: the cache holds no secret | models.json data never enters `model-cache.json`; values stay in memory. |
| P9: no timer, no polling | Reload happens only at the rebuild points (D14, M1). <!-- Red Team 2026-11-06: Finding 10 --> The 401 hook is a relay stream that forgets the command before it forwards the terminal item; no detached waiter. |
| P11: cache key per account identity | Custom providers have no list function, so their access is unknown and no cache entry exists. |
| Import rules (`providers` imports neither `settings` nor `auth`) | M5, M6. |

## Extension trace: a local OpenAI-compatible provider

The next variant is a local Ollama or vLLM server. It needs data only:

```jsonc
// <ASK_HOME>/models.json
{
  "providers": {
    "local": {
      "baseUrl": "http://127.0.0.1:11434/v1",
      "api": "openai-completions",
      "apiKey": "ollama",                     // literal; "$OLLAMA_KEY" reads env
      "models": [{ "id": "qwen3-coder:30b" }], // Pi defaults fill the rest
    },
  },
}
```

<!-- Red Team 2026-11-06: Finding 1, Finding 2, Finding 7 -->
| Step | Owner | What happens |
|---|---|---|
| Read | `internal/settings` (phase 1) | Shared byte reader, JSONC strip, typed rows, id rule, problems |
| Map | `internal/app` to `internal/providers` (phases 3, 5) | Row `{Provider: local, API: openai-completions, BaseURL: http://127.0.0.1:11434/v1, CompletionsCompat{}, 128000, 16384}`; key table entry for that triple |
| Usable | `internal/app` (phase 5) | The dry-run source is `config:literal`, so the provider is usable |
| Resolve | `internal/providers` (phase 4) | `local/qwen3-coder:30b` matches exactly as a whole pattern; no level |
| Key | `internal/auth` (phase 2) | No saved credential or env binds to this endpoint; `configSnapshot` binds the file key `ollama` |
| Wire | existing | `openai.NewCompletions` serves the API; the transport pins to `m.BaseURL` |

**Stable contract:** a row's `(Provider, API, BaseURL)` selects the wire by `API`, binds the credential, and keys the file key and headers. A new provider on one of the three wire APIs (`anthropic-messages`, `openai-completions`, `openai-responses`) is data only. A new wire API is the only change that needs Go code. Phase 9 proves the trace with a test.

## Unresolved questions

- **QA:** answered 2026-11-05. Keep M3 (a).
- **QB:** answered 2026-11-05. Keep M11: the user models.json must be 0600.
- **QD:** answered 2026-11-05. Follow Pi (M15).
- **QE, QF:** moot after Q2b (2026-11-06).
- **QG:** answered 2026-11-06. Keep the key source display on the shared function.
- **QC (cycling key).** The key and its feedback are decided in the phase 7 design file. Only the user approves it.

## Carry-over notes from phases 3 and 4 (2026-11-07)

- A glob `*` in `--models` and `enabledModels` does not match `/` (`path.Match`). `openrouter/*/*` reaches `openrouter/qwen/qwen3:free`. Phase 9 docs state this.
- Cost of an override or a custom row: phase 5 maps each omitted price field from the base row (per-field merge, as Pi does) and keeps the price tiers of a built-in row when the file sets no cost. A file price never zeroes a field that the file omits.
- An override that names no row gives no problem, because an account list can hide the row until a later refresh. For a provider that the user file defines, phase 5 reports an override for an unknown id as a problem.
- On a row from `models[]`, an override may change `compat` and add effort words; `api` is refused on every row. A `models[]` row without `baseUrl` takes the URL of the provider entry, else of another row of that provider with the same API; else it is a problem.

## Carry-over notes from phase 5 (2026-11-07)

- A `models.json` provider entry that sets `apiKey` or `headers` configures every row of that provider, also a built-in provider with no `baseUrl` change. This follows Pi (the models.json key is the third source for any provider, Q1) and adds no exposure, because a registered endpoint already accepts `--api-key`. The narrower rule in phase 2 came from the project file, which Q2b removed.
- "Model configuration changed" is decided by endpoints: a request fails only when its endpoint was in an earlier table and is in no current row. A row with only a name override keeps working.
- Phase 8 must not skip `setModel` when the chosen id equals the current id: choosing the same id again is how a `notInConfig` row takes its new endpoint.
- `TestLeaderMaximumFrameAndOversizeClientIsolation` and `TestPrintMonitorFractionalTimeoutDoesNotSelectDefault` fail under `-race` on a clean `97c3052` worktree too (verified 2026-11-07). They are timing-bound and not caused by this plan.
