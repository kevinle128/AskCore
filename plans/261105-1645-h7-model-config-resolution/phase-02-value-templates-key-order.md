---
phase: 2
title: "Value templates and key order"
status: pending
priority: P1
effort: "2d"
dependencies: []
---

# Phase 2: Value templates and key order

## Goal

`internal/auth` resolves a models.json value (`apiKey`, header value) with the Pi grammar and a command cache, and `Service` uses the models.json key as the third source, after `--api-key` and the saved credential and before env, through one source-selection function that the resolver and the `ask settings show` display share.

## Context

- Decisions: D6, D7, D8/Q1, QG, M2, M3, M6 ([decisions.md](./decisions.md)).
- Today's order: override, saved credential, env (`internal/auth/service.go:131-186`). Saved credentials bind to a registered method by provider, API and endpoint (`internal/auth/service.go:121-128`).
- `keySnapshot` accepts a key only when a registered `api-key` method binds the row or the row is one of three fixed OpenAI and Token Plan endpoints (`internal/auth/service.go:247-259`). The override path also calls it (`internal/auth/service.go:138-140`). A provider that only the file defines has no registered method, so `keySnapshot` refuses its key.
- The env lookup reads `s.Env`, else `os.LookupEnv` (`internal/auth/service.go:146-148`).
- `auth` may import stdlib, OIDC, `settings` and provider core types (`internal/auth/README.md:31-35`). The process runner arrives as a function (M6); this phase does not import `sandbox`. `sandbox.Run` kills the group and returns `ctx.Err()` when its context ends (`internal/sandbox/run.go:11-24`).
- This phase takes plain strings, so it does not wait for phase 1.

## Data flow

<!-- Red Team 2026-11-06: Finding 1 -->
```
ConfigKey{Value: "$OLLAMA_KEY" | "!op read ..." | "literal", Endpoint}   (only for providers in the user file)
  -> Values.Resolve(ctx, raw) -> template expand (env) | command cache -> secret

selectSource(ctx, req, dryRun) -> one order for ResolveWith and KeySource:
  --api-key (provider pin)                           -> keySnapshot, or configSnapshot when the
                                                        provider has a ConfigKey with Endpoint == m.BaseURL
  saved credential, only if it binds to m endpoint   -> OAuth or api-key snapshot
  models.json key, only if Endpoint == m.BaseURL     -> configSnapshot (no method table)
  env key, only if a registered api-key method binds -> keySnapshot(source env)
  else ErrNoCredential
```

## Requirements

1. Grammar (`Values.Expand`), Pi rules:
   - A whole value that starts with `!` is a command (the rest of the string). `$!` at the start is a literal `!`.
   - `$NAME` and `${NAME}` read the environment (`[A-Za-z_][A-Za-z0-9_]*`). `$$` is a literal `$`. Any other `$` form is an error.
   - A bare string is a literal. A variable that is unset or empty is an error that names the variable (never its value), so a variable name is never sent as a key (D6).
<!-- Red Team 2026-11-06: Finding 10 -->
2. Command cache (`Values`, one per `Service`, process lifetime):
   - Key: the command string. The cache entry owns the run: it starts the runner on `context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)`, so no caller's cancellation ends the run.
   - Each caller (the first one and each waiter) waits with `select` on the entry `done` channel and on its own `ctx.Done()`. A caller whose context ends returns `ctx.Err()` and leaves the run alone.
   - Success: trimmed stdout is cached. Empty stdout, a non-zero exit and the 10 s timeout are failures. A caller cancellation is never cached as a failure.
   - Failure: no new run for 30 s from the failure time (`Now` of the service), then one new run is allowed. The backoff is a time compare on the next call, not a timer.
   - `Forget(command)` and `ForgetAll()` clear entries. A run that is still going when `Forget` is called does not store its result.
   - Stdout and stderr never go to a log or an error text; an error says `command for <provider> apiKey failed (exit 1)` or `timed out`.
<!-- Red Team 2026-11-06: Q2b (Findings 3, 4) -->
   (The earlier `AllowCommand` guard for project values is removed: Ask reads no project models file.)
3. `ResolveRequest{Model, Override, Config *ConfigKey}` and `Service.ResolveWith(ctx, req)`. `Resolve(ctx, m, key)` stays as `ResolveWith` with no config key, so existing callers (`internal/app/agent_native.go:201`, `internal/app/module_auth.go:16,28`) keep working until phase 5 moves them.
<!-- Red Team 2026-11-06: Finding 1 -->
4. **`configSnapshot`.** A key from a `ConfigKey` makes a snapshot through a new function `configSnapshot(m, key, ck)`. It checks only `ck.Endpoint == m.BaseURL` and never reads the method table. It has `Source: "config"`, `Method: "api-key"`, `Profile: "api-key"`. `AuthSnapshot.String` and `MarshalJSON` already redact the token (`internal/providers/auth.go:22-36`).
<!-- Red Team 2026-11-06: Finding 8 (merged), QG -->
5. **Binding (M3; user QA 2026-11-05).** A saved credential whose method does not match `(m.Provider, m.API, m.BaseURL)` is skipped only when a models.json key is present for that provider and binds to the row endpoint. In every other case the result stays `ErrMethod`, as today (`internal/auth/service.go:162-165`), so a caller that passes no config key sees no change. The env key keeps the registered-method rule.
<!-- Red Team 2026-11-06: Finding 13 -->
6. **`--api-key`.** The override keeps its provider pin. It goes through `keySnapshot` as today. It is also accepted through `configSnapshot` when the provider has a `ConfigKey` whose endpoint equals `m.BaseURL`, which means only for a provider that the user `models.json` defines or re-points. No other endpoint receives it.
<!-- Red Team 2026-11-06: QG, Finding 8 (merged) -->
7. **One order.** `selectSource(ctx, req, dryRun bool) (source Source, snapshot providers.AuthSnapshot, err error)` holds the whole order. `ResolveWith` calls it with `dryRun` false. `KeySource(ctx, req)` calls it with `dryRun` true and returns the source and a safe name (`override`, `saved:<method>`, `config:$NAME`, `config:command`, `config:literal`, `env:<NAME>`). In a dry run the function never runs a command, never refreshes a token and never returns a secret. `ask settings show` uses `KeySource` in phase 5 (user QG 2026-11-06: keep the display on a shared function).
8. Header values use the same `Values.Expand` (phase 5 calls it per request).

## Files

| Action | Path | Notes |
|---|---|---|
| Create | `internal/auth/config_value.go` | Grammar, `Values` cache, `ConfigKey` |
| Create | `internal/auth/config_value_test.go` | Grammar table, cache, backoff, cancellation, concurrency |
| Modify | `internal/auth/service.go` | `ResolveRequest`, `selectSource`, `ResolveWith`, `KeySource`, `configSnapshot`, `Values` field |
| Modify | `internal/auth/service_test.go` | Order tests |
| Modify | `internal/auth/README.md` | Key order and value grammar |

## Steps

1. Write the grammar as a pure function with an injected env lookup; table test first.
2. Write `Values` with a mutex, a map of entries `{value, err, failedAt, done chan struct{}}`, and an injected `Run func(ctx, command string) (string, error)`.
3. Move the body of `Resolve` into `selectSource` with the order above; add the dry-run branches. Keep the existing OAuth path as it is.
4. Add `configSnapshot`, `ResolveWith` and `KeySource` as thin calls of `selectSource`.

## Tests

- `TestConfigValueGrammar`: `literal`, `$HOME_KEY`, `${A}b`, `$$`, `$!x`, `!cmd`, `$1` error, unset variable error (the error text holds the name, not a value).
- `TestConfigValueCommandRunsOnce` (two goroutines, one run).
- `TestConfigValueCallerCancelDoesNotPoisonCache`: the first caller cancels; a second caller gets the value of the same run; no backoff starts. <!-- Red Team 2026-11-06: Finding 10 -->
- `TestConfigValueCommandBackoff` (failure, call at +10 s gives the cached error with no run, call at +31 s runs again; the test clock is the service `Now`).
- `TestConfigValueForgetRunsAgain`, `TestConfigValueForgetDuringRunDropsResult`. <!-- Red Team 2026-11-06: Finding 10 -->
- `TestConfigValueOutputNotInError` (the command prints a marker; no error text has it).
- `TestResolveOrderOverrideSavedConfigEnv` (four sources set, remove them one at a time from the top).
- `TestResolveConfigKeyForFileDefinedProvider` (a provider with no registered method and a literal key: the snapshot has the key and source `config`). <!-- Red Team 2026-11-06: Finding 1 -->
- `TestResolveOverrideKeyOnlyForUserFileEndpoint` (an override for a file provider binds; an override for a row whose endpoint no `ConfigKey` names gives `ErrMethod`). <!-- Red Team 2026-11-06: Finding 13 -->
- `TestResolveSavedCredentialSkippedOnOtherEndpoint` (saved OpenAI key, row with a user base URL and a config key: the config key wins; with no config key: `ErrMethod`, as today).
- `TestResolveConfigKeyNeedsSameEndpoint`.
- `TestKeySourceMatchesResolveWith`: for each input of the order test, `KeySource` names the source that `ResolveWith` uses. <!-- Red Team 2026-11-06: Finding 8 (merged), QG -->
- `TestKeySourceNeverRunsCommand`.

```
go test ./internal/auth -run 'TestConfigValue|TestResolve|TestKeySource' -count=1
go test -race ./internal/auth -run 'TestConfigValue' -count=1
go test ./internal/auth -count=1
go vet ./internal/auth
```

## Validation

- Existing auth tests pass unchanged: `go test ./internal/auth -count=1`.
- No `time.Sleep` in product code; the backoff is a time compare.

## Risk

| Risk | L x I | Mitigation |
|---|---|---|
| Order change breaks H7a tests | Low x High | `Resolve` keeps its behavior when no config key exists; the full auth suite runs |
| A hung command blocks a request | Med x Med | The entry owns a 10 s run; each caller can leave on its own context; the runner kills the process group |
| A cancelled request blocks all sessions for 30 s | Low x Med | A caller cancellation is never cached (test above) |
| `KeySource` and `ResolveWith` drift | Low x Med | One function, one comparison test |

## Rollback

Revert the commit. `Resolve` is unchanged for callers that pass no config key, and no caller passes one until phase 5.
