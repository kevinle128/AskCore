---
phase: 3
title: "Custom rows and merge order"
status: pending
priority: P1
effort: "1d"
dependencies: []
---

# Phase 3: Custom rows and merge order

## Goal

`internal/providers` turns the provider-owned form of the user `models.json` into model rows with Pi defaults and applies the merge order catalog, overlay, account lists, custom models, overrides on every `ModelSet` build.

<!-- Red Team 2026-11-06: Q2b (Findings 3, 4) -->
There is one config layer, the user file. The project layer, its order rule and its tests are removed (user decision Q2b, 2026-11-06).

## Context

- Decisions: D2, D3, custom defaults, M5, M7, M14; guards U2, U3/P13, P1, P5 ([decisions.md](./decisions.md#guard-earlier-decisions-that-stay)).
- `NewModelSet(base, overlay, accounts)` merges the first two by `Ref`, deletes rows that a known account list does not name (`internal/providers/model_set.go:55-58`), adds fallback rows, and sorts (`internal/providers/model_set.go:46-79`).
- `Validate` checks identity and compat type per API (`internal/providers/model.go:86-111`). Compat types: `internal/providers/compat.go:5-41`.
- `RequestLimits` fills a 0 limit only from a family rule with the same API (`internal/providers/catalog_rules.go:172-188`).
- The account list of a provider belongs to the saved credential at the default host (`internal/app/model_refresh.go:257-269`). A saved credential does not bind to a changed `baseUrl` (M3).
- `providers` must not import `settings` or `auth` (`.golangci.yml:161-170`). This phase defines its own input type; `app` maps to it in phase 5.

## Data flow

<!-- Red Team 2026-11-06: Finding 5 -->
```
ModelConfig{Providers []ProviderConfig}   (no secret: no apiKey and no header field)
  NewModelSet(base, overlay, accounts, cfg ...ModelConfig)
    1. base + overlay (first Ref wins)                               existing
    2. account filter + fallback rows, except for a provider whose
       baseUrl the config changes: its access is unknown, all rows stay
    3. config (the user file):
       a. provider baseUrl on rows of that provider (provider api is only the default of its models[] rows)
       b. models[]: add a row, or replace the row with the same provider+id+api
       c. modelOverrides: patch metadata of rows with that provider+id
    4. sortForDisplay                                                existing
  -> *ModelSet + []ConfigProblem (row-level problems found while mapping)
```

## Requirements

1. Types: `ModelConfig`, `ProviderConfig{ID, BaseURL, API, Compat, Models []ModelSpec, Overrides map[string]ModelPatch}`, `ModelSpec`, `ModelPatch`. These types have no `apiKey` and no header field. A header value from a file can be a secret (a literal or a template), so phase 5 applies file headers per request to a request-local copy of the row, and no snapshot row holds them (M2).
2. New custom row defaults (Pi): `ContextWindow 128000`, `MaxTokens 16384`, `Input ["text"]`, `Reasoning false`, `Cost` zero, `CostUnknown false`, `Name = ID`. `Compat` is the zero value of the type of `API`. A `thinkingLevelMap` in the spec sets `Reasoning true` unless `reasoning: false` is explicit.
3. D3: a `models[]` row needs `api` (on the row or the provider) and needs no family rule. It replaces a base row with the same provider, id and API. An unknown API is a row problem.
4. D3: `modelOverrides` on a row that has a family rule may set `name`, `contextWindow`, `maxTokens`, `cost`, `input`, and may only narrow `thinkingLevelMap` (a level set to `null` or removed). It may not set `api`, `compat`, or an effort word that the row does not have. A refused field is one problem; the other fields still apply.
5. M14: a provider-level `baseUrl` on a built-in provider changes every row of that provider. API, compat and effort words stay from the family rule. (File headers are per request, see requirement 1.)
<!-- Red Team 2026-11-06: Finding 5 -->
6. A provider whose `baseUrl` the config changes gets no account filter and no fallback rows from its account list, because that list describes the account at the default host, not at the new endpoint. Its access is unknown, as for a provider with no list ("a provider with no entry, or with Known false, keeps all its rows", `internal/providers/model_set.go:37`).
<!-- Red Team 2026-11-06: Q2b (Findings 3, 4) -->
7. Overrides apply after custom rows, so an explicit field wins after any refresh (D2): the constructor runs on every snapshot build.
8. `Validate` runs on every produced row; a row that fails is dropped with a problem.
9. `ModelSet` stays an immutable snapshot (P5); `cloneModel` keeps every row independent (`internal/providers/model_set.go:276-292`).
<!-- Red Team 2026-11-06: Q2b (Findings 3, 4) -->
10. `ModelSet.Origin(ref) Origin` (kept beside the rows, not on `Model`) tells `app` whether the user file made or changed a row (`builtin` or `user`). Phase 5 uses it for the key table and for the wire flag of a row that left the config.

## Files

| Action | Path | Notes |
|---|---|---|
| Create | `internal/providers/model_config.go` | Types, defaults, mapping, overrides |
| Create | `internal/providers/model_config_test.go` | Defaults, overrides, refused fields |
| Modify | `internal/providers/model_set.go` | `NewModelSet` takes the config; account rule for a re-pointed provider; origin |
| Modify | `internal/providers/model_set_test.go` | Merge order tests |

Phase 4 also works in `internal/providers` but owns other files only.

## Steps

1. Write the defaults and the spec-to-row mapping with a table test.
2. Write the patch rules for built-in rows.
3. Extend `NewModelSet` with a variadic `...ModelConfig` so the existing callers (`internal/app/model_refresh.go:272-273,295`) compile unchanged.
4. Skip the account filter for a provider that the config re-points.
5. Return row problems through a method `Problems()`, so `NewModelSet` keeps its signature for current callers.

## Tests

- `TestCustomModelGetsPiDefaults`.
- `TestCustomModelNeedsAPI`.
- `TestCustomModelReplacesBaseRowWithSameRef`.
- `TestOverrideOnBuiltInKeepsFamilyAPIAndWords` (override sets `api` and a new effort word: both refused, `contextWindow` applies).
- `TestOverrideWinsOverOverlayAfterRebuild` (build with an overlay row that sets `MaxTokens 32768`, override 4096: result 4096; build again with a changed overlay: still 4096).
- `TestCustomRowSurvivesKnownAccountList` (account list without the custom id keeps the custom row; an account id with no rule still has no row, U2).
- `TestUserBaseURLOnBuiltInProvider`.
- `TestUserBaseURLIgnoresDefaultHostAccountList` (a known account list that names one id: with a user `baseUrl` on that provider, every row of the provider stays). <!-- Red Team 2026-11-06: Finding 5 -->

```
go test ./internal/providers -run 'TestCustomModel|TestOverride|TestCustomRow|TestUserBaseURL|TestModelSet' -count=1
go vet ./internal/providers
```

## Validation

- `go test ./internal/providers -count=1` passes; the catalog snapshot test (`internal/providers/catalog_snapshot_test.go`) is unchanged, because the built-in catalog is unchanged.

## Risk

| Risk | L x I | Mitigation |
|---|---|---|
| A custom row on a built-in provider collides with a generated row | Med x Low | Same `Ref` replaces; a different API adds a second row, which the grammar can address with `@api` |
| An override makes a request shape that the API refuses | Low x Med | Overrides cannot change API, compat or words of built-in rows |
| A re-pointed provider shows rows that its new host does not serve | Med x Low | Access is unknown, as for any provider with no list; a request error names the model |

## Rollback

Revert the commit. The variadic argument means no caller passes a config until phase 5.
