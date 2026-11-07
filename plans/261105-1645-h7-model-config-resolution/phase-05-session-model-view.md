---
phase: 5
title: "Model view and composition"
status: pending
priority: P1
effort: "2d"
dependencies: [1, 2, 3, 4]
---

# Phase 5: Model view and composition

## Goal

`internal/app` builds every model snapshot from catalog, overlay, accounts and the user `models.json`, rebuilds it only at session start, when `/model` opens and on `setModel`, gives `AuthRunner` a key and header table keyed by `(Provider, API, BaseURL)`, makes a file-only provider usable, and reports file problems through the existing diagnostics.

<!-- Red Team 2026-11-06: Q2b (Findings 3, 4), Finding 8 -->
## Why the view is per process now

The earlier plan made `acp.Config.Models` and `FindModel` take `SettingsScope` only because a trusted project file is per session. The user dropped the project file (Q2b, 2026-11-06). The user file belongs to the process, so the view is the process snapshot, and the `Models` and `FindModel` signatures stay as they are (`internal/acp/agent.go:41-48`). The rebuild points and the triple lookup of Finding 8 still apply.

## Context

- Decisions: D1, D2, D4, D7 (wiring), D14, QG, M1, M2, M3, M4, M6, M12, M17, M18 ([decisions.md](./decisions.md)).
- Process snapshot today: `modelRefresher` (`internal/app/model_refresh.go:99-132`); `Current()` stats the cache file and reloads under `loadMu` (`internal/app/model_refresh.go:185-213`); `buildModelSnapshot` makes two `NewModelSet` values (`internal/app/model_refresh.go:245-275`); `CachedModelSet` makes a third for print mode (`internal/app/model_refresh.go:288-297`).
- `Current()` call sites: start choice (`internal/app/default_model.go:41-58`, through `newStartChoice` at `internal/app/module_acp.go:136`), `models()` (`internal/acp/ask_methods.go:175`), `setModel` (`internal/acp/ask_methods.go:244`), `Authenticate` (`internal/app/module_acp.go:223`).
- Usable checks read only the credential store status: `providerUsable` (`internal/app/module_acp.go:303-314`), the start choice (`internal/app/default_model.go:59-65,147-148`), the `usableOnly` filter (`internal/acp/ask_methods.go:192`), the `setModel` guard (`internal/acp/ask_methods.go:263`), provider setup (`internal/app/provider_setup.go:45,147-149`).
- Key path: `AuthRunner` resolves per request (`internal/app/module_auth.go:14-50`); `NativeAuthReady` (`internal/app/agent_native.go:176-209`).
- Session start diagnostics reach the TUI notice through `sessionTrust` (`internal/app/trust.go:137-148`, read at `internal/tui/update_session.go:848`) and `ask settings show` (`cmd/tui/settings_command.go:258-260`).
- Print mode: `openProvider` switches on provider name (`cmd/tui/headless_faux.go:78-101`); a non-zero `spec.Start` takes the native path (`cmd/tui/headless.go:262-267`).

## Data flow

<!-- Red Team 2026-11-06: Finding 8 -->
```
rebuild points only (session start, models(), setModel's FindModel; and a cache write):
  modelRefresher.Current()
    stat model-cache.json (fileStamp, as today)
    ModelsFiles.Load() -> generation changed?            (one ModelsFiles per process)
    -> snapshot{set, catalog, keys[(Provider,API,BaseURL)], headers[...], problems}
    -> snap.Store (atomic)

request path (AuthRunner):
  r.Snapshot()  = snap.Load(), no stat, no lock, no rebuild
  keys[(m.Provider, m.API, m.BaseURL)]  missing for a row with user origin
      -> fail "model configuration changed; choose the model again"
  service.ResolveWith(ctx, {Model: m + expanded headers, Override, Config})
  relay stream: on a terminal 401 with a command source -> Values.Forget, then forward
```

## Requirements

1. **Mapping.** One function in `app` maps `settings.ModelsFile` to `providers.ModelConfig`, a key table and a header table. Both tables are keyed by `(Provider, API, BaseURL)` of every row that the user file defines or re-points. A table entry holds the raw value templates, never a resolved secret.
<!-- Red Team 2026-11-06: Finding 9 -->
2. **One loader, one generation.** `modelRefresher` owns the one `settings.ModelsFiles` of the process. `Current()` keeps `fileStamp` for `model-cache.json` only and compares the `generation` that `ModelsFiles.Load` returns for the user file. Either change rebuilds the snapshot. When the generation changes, `Values.ForgetAll` runs, so a removed or changed command is not used again.
<!-- Red Team 2026-11-06: Finding 8 -->
3. **Rebuild points (D14).** A rebuild happens only in `Current()`, which runs at session start, in `models()` (when `/model` opens), and in `setModel` through `FindModel`, plus the existing reload after a cache write. The request path reads the stored snapshot with a new `Snapshot()` (an atomic load). It never stats a file, never takes `loadMu` and never builds a `ModelSet`.
<!-- Red Team 2026-11-06: Finding 14 -->
4. **Every build passes the config.** Both builds in `buildModelSnapshot` (`set` and `catalog`, `internal/app/model_refresh.go:271-274`) and the build in `CachedModelSet` (`internal/app/model_refresh.go:291-297`) pass the user config. If `catalog` lacks it, `catalog.Contains` misses a custom row and a custom saved default falls back with the wrong reason (`internal/app/default_model.go:140-146`). Overrides then win after each refresh (D2).
<!-- Red Team 2026-11-06: Finding 6 -->
5. **Existing provider, one definition.** `app` defines it once: the built-in provider ids, the providers of registered auth methods (`internal/app/auth_native.go:16-31`), and the providers of the user file. The phase 1 cross-field rule and the key table use this definition.
<!-- Red Team 2026-11-06: Finding 2 -->
6. **Usable.** `usable(ctx, provider)` is true when the store status says so, or when the current snapshot has a key-table entry for that provider whose source (`auth.KeySource`, dry run) is `config:*`. The dry run never runs a command; a command source counts as usable, and a failed run is reported at the request. Every site in the context list uses this one function: `providerUsable`, the start choice, the saved default, `usableOnly`, the `setModel` guard through `ProviderSetup.Usable`.
<!-- Red Team 2026-11-06: Finding 8 -->
7. **Key source per request (M2).** `AuthRunner` and `NativeAuthReady` look up the key and header tables by `(m.Provider, m.API, m.BaseURL)` of the session row. They call `ResolveWith` with that config key and a request-local copy of `m` whose `Headers` hold the expanded file headers. The agent state and the snapshot never hold them. When the row has user origin and the triple is no longer in the tables (the user edited the file), the request fails with the fixed text `model configuration changed; choose the model again` and sends nothing.
<!-- Red Team 2026-11-06: Finding 8 -->
8. **Wire flag.** `ACPModel.NotInConfig bool` (`omitempty`) marks the current row of `models()` when its triple is no longer in the snapshot because the user file changed. Such a row gets `NotInConfig`, not `NotInAccount` (`internal/acp/ask_methods.go:201-210`).
<!-- Red Team 2026-11-06: Finding 10 -->
9. **401 hook without a goroutine.** When the winning source of a request is a config command (or a header value is a command), `AuthRunner` wraps the registry stream in a relay (`providers.NewStream` with a body that forwards each item). On a terminal `*providers.Failure` with `Status 401`, the relay calls `Values.Forget(command)` and then forwards the terminal item, so the next request after the consumer sees the end runs the command again. The only goroutine is the relay producer that `NewStream` owns; it ends with the stream (`internal/providers/stream.go:33-58`). No detached waiter exists.
10. **Command runner (M6).** `NewNativeAuth` gets the runner: `sandbox.Run` with `Argv sandbox.ShellArgv(cmd)`, `Dir` the user home, `Env os.Environ()`, stdout only. The context comes from the cache entry (phase 2).
<!-- Red Team 2026-11-06: Finding 12 -->
11. **Print mode (M4, M12).** `openProvider` keeps only the `faux` branch. Every other provider, Token Plan included, resolves through `CachedModelSet` with the user config and returns a zero stream, so `newPrintAgent` takes the `spec.Start` branch and the native wires serve the row by API. The Token Plan default level `medium` (`cmd/tui/headless.go:389-392`) stays. The run reads the file once; a bad file is a warning line. Phase 6 adds the pattern and its print-mode tests.
12. **Diagnostics.** The user file problems join `SessionStart.Settings.Diagnostics` (layer user, the file path), so the TUI start notice and `ask settings show` show them with no TUI change. Problems found later, when `/model` opens, go to phase 6 (wire) and phase 8 (TUI).
<!-- Red Team 2026-11-06: QG -->
13. **settings show.** `loadSettingsDump` lists the user models.json (path, `loaded` or `problem`), its problems, and per provider the key source from `auth.KeySource` (the shared dry-run function of phase 2; never a value, never a run).
<!-- Red Team 2026-11-06: Q2b (Findings 3, 4) -->
14. **Removed.** The session-scoped `Models`/`FindModel` signatures, the per-project `ModelsFiles`, the project trust wiring for models, the project limits of D9 and the project tests are gone with Q2b.

## Files

| Action | Path | Notes |
|---|---|---|
| Create | `internal/app/session_models.go` | Mapping, key and header tables, existing-provider set, usable rule, header expansion |
| Create | `internal/app/session_models_test.go` | Real `NewModelSet` and real files in a temp home |
| Modify | `internal/app/model_refresh.go` | `ModelsFiles` generation, config in every build, `Snapshot()` |
| Modify | `internal/app/module_acp.go` | Usable rule, `Authenticate` on the snapshot |
| Modify | `internal/app/default_model.go` | Start choice and `ChooseSavedModel` with the config and the usable rule |
| Modify | `internal/app/module_auth.go` | `AuthRunner` with the triple lookup and the 401 relay |
| Modify | `internal/app/agent_native.go` | `NativeAuthReady` with the config key |
| Modify | `internal/app/auth_native.go` | Command runner into `auth.Service` |
| Modify | `internal/app/session_start.go` | Model problems into the settings diagnostics |
| Modify | `internal/app/provider_setup.go` | `Usable` through the shared rule |
| Modify | `internal/acp/ask_methods.go`, `pkg/protocol/acp.go` | `NotInConfig` on the current row |
| Modify | `cmd/tui/headless_faux.go`, `cmd/tui/headless.go`, `cmd/tui/settings_command.go` | Print path, settings show |
| Modify | `internal/app/README.md`, `internal/acp/README.md` | Model view, rebuild points |

## Steps

1. Write the mapping, the tables and the existing-provider set with tests over real temp files.
2. Change the refresher: generation compare, config in all three builds, `Snapshot()`.
3. Put the usable rule behind one function and switch every listed site to it.
4. Wire the triple lookup and the relay into `AuthRunner` and `NativeAuthReady`.
5. Move print mode to the `spec.Start` path, Token Plan included.
6. Add `NotInConfig` and the settings show rows.

## Tests

All through real code: real `settings.ModelsFiles`, real `NewModelSet`, the real `ACPRuntime` from `NewACPRuntime` (`internal/app/module_acp.go:101`), real `auth.Service` with a temp `ASK_HOME`. HTTP goes to `httptest` servers only.

- `TestUserModelsFileReloadsWhenListIsRead` (edit the file, call `models()`: new row; no timer).
- `TestRequestPathDoesNotReloadModelsFile` (edit the file, send a request: the request uses the stored snapshot and does not stat the file). <!-- Red Team 2026-11-06: Finding 8 -->
- `TestEditedProviderAsksForModelChoice` (a session on a custom row; edit its `baseUrl` and headers; open `/model`; the next request fails with `model configuration changed; choose the model again` and sends no new header to the old host; the current row has `notInConfig`). <!-- Red Team 2026-11-06: Finding 8 -->
- `TestBadUserModelsFileKeepsLastValidRows`.
- `TestModelRefreshKeepsOverrideAndSessionModel` (a session on a custom row; a cache write; the session model is the same and the override value holds).
- `TestCustomSavedDefaultKeepsCatalogReason` (a saved default on a custom row: the start uses it; a removed custom row gives `not in the catalog`, not `not in account list`). <!-- Red Team 2026-11-06: Finding 14 -->
- `TestFileKeyProviderIsUsable` (a `local` provider with only a file key: start choice, saved default, `setModel` and `usableOnly` accept it). <!-- Red Team 2026-11-06: Finding 2 -->
- `TestConfigKeyReachesCustomEndpoint` (httptest OpenAI-compatible server checks `Authorization: Bearer <file key>`).
- `TestConfigCommandForgottenBefore401IsSeen` (the server answers 401 once; when the consumer sees the failure, the cache entry is already gone; the next request runs the command again). <!-- Red Team 2026-11-06: Finding 10 -->
- `TestPrintTokenPlanUsesModelSet` (in `cmd/tui`). <!-- Red Team 2026-11-06: Finding 12 -->
- `TestSettingsShowNamesKeySourceNotValue` (in `cmd/tui`).

```
go test ./internal/app -run 'TestUserModelsFile|TestRequestPath|TestEditedProvider|TestBadUserModelsFile|TestModelRefreshKeeps|TestCustomSavedDefault|TestFileKeyProvider|TestConfigKey|TestConfigCommand' -count=1
go test ./internal/acp -count=1
go test ./cmd/tui -run 'TestSettingsShow|TestPrint' -count=1
go test -race ./internal/app -run 'TestRequestPath|TestConfigCommand|TestEditedProvider' -count=1
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

## Validation

- `go test ./...` passes and sends no request outside `httptest` (existing `ASK_DISABLE_MODEL_REFRESH=1` in e2e tests stays).
- `rg -n 'time\.Sleep|NewTicker|fsnotify|go func' internal/app/session_models.go internal/app/module_auth.go` finds nothing new (the relay uses `providers.NewStream`).

## Risk

| Risk | L x I | Mitigation |
|---|---|---|
| A rebuild point is missed and a session lists stale rows | Low x Med | The three call sites are the only `Current()` callers; a test per point |
| The usable rule misses one site | Med x High | One function; `rg -n 'providerUsable\|\.Usable(' internal` before the commit; the leader test covers all sites |
| A row and its key come from two different files | Low x High | Triple lookup and the fixed failure text (test above) |

## Rollback

Revert this phase together with phases 6, 8 and 9. Phases 1-4 can stay: they have no caller then. A saved default that points at a custom row then falls back (see plan.md "Rollback").
