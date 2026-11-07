---
phase: 4
title: "Model-id grammar and scope patterns"
status: pending
priority: P1
effort: "1.5d"
dependencies: []
---

# Phase 4: Model-id grammar and scope patterns

## Goal

`internal/providers` has pure functions that resolve a model pattern `[provider/]id[@api][:level]` against rows with the omp colon rule, reuse `findIn` for every exact step, expand a scope list with globs, and have a table-driven test; `acp.parseModelID` uses the same parser for its `provider/id@api` form.

## Context

- Decisions: D10, D11, D13 (globs), M8, M13, M16, M17, M19 ([decisions.md](./decisions.md)).
- Today: `parseModelID` reads `provider/id@api` (`internal/acp/ask_methods.go:121-134`); an ambiguous or unknown ref is refused (`internal/acp/ask_methods.go:244-247`); `findIn` gives the exact match and names both APIs of an ambiguous id (`internal/providers/catalog.go:126-151`); `ModelSet.Find` uses it (`internal/providers/model_set.go:110-120`).
- Alias and dated ids: `splitDate`, `sameModelID` (`internal/providers/model_set.go:166-188`). Display order (newest version first, alias before snapshots): `sortForDisplay` (`internal/providers/model_set.go:202-240`).
- Levels: the seven `protocol.ThinkingLevel` words (`cmd/tui/args.go:97-100`); clamp per model: `ClampThinkingLevel` (`internal/providers/clamp.go:22`).
<!-- Red Team 2026-11-06: Finding 7 -->
- Colon rule (user decision 2026-11-06, "do it as omp does"): omp `parseModelPatternWithContext` (`oh-my-pi/packages/coding-agent/src/config/model-resolver.ts:900-987`); the CLI turns the invalid-suffix fallback off (`model-resolver.ts:2359-2366`, `allowInvalidThinkingSelectorFallback: false`).

## Data flow

<!-- Red Team 2026-11-06: Finding 7 -->
```
ResolvePattern(rows, "local/qwen3-coder:30b", Options{AllowInvalidLevel: false})
  1. whole pattern, exact (findIn after the provider/@api split)  -> qwen3-coder:30b wins
  2. suffix after last ':' is a level word?
       yes: a match whose real id ends in ":<level>" wins; else resolve the text before ':'
            and apply the level ("kimi:high" never matches "kimi-highspeed")
       no:  alias, then fuzzy, on the whole pattern
  3. invalid suffix and no match: resolve the text before ':' with a warning and no level,
     only when AllowInvalidLevel is true (CLI: false -> ErrUnknownModel)
-> Resolved{Model, Level, Warning}
ExpandScope(rows, []string) -> []ScopedModel{Model, Level} + []problem
```

## Requirements

<!-- Red Team 2026-11-06: Finding 7 -->
1. **Ref split** (pure, no rows): `ParseRef(s) (Ref, error)` splits `provider/` at the first `/` and `@api` at the last `@`. `acp.parseModelID` calls it and keeps its error text, so one grammar exists (M13 keeps the exact `provider/id@api` form for saved defaults and `setModel`).
2. **Exact steps reuse `findIn`.** Every exact match calls `findIn` with the `Ref` from `ParseRef`, so the ambiguity text and the two-API rule stay one copy. A bare id (no provider) runs `findIn` per provider that has the id. Rows of more than one provider (or two APIs) give `ErrAmbiguousModel` with the list of `provider/id@api` choices (D10; the user kept D10 and chose omp only for the colon rule). The first segment counts as a provider only when a row has that provider; else the whole text is a bare id (vendor-prefixed ids such as `qwen/qwen3`).
<!-- Red Team 2026-11-06: Finding 6 -->
3. **Case.** Exact steps compare provider and id exactly, as `findIn` does. The alias and fuzzy steps fold case. Two rows whose `provider/id` are equal after case folding are `ErrAmbiguousModel`, never a silent pick.
<!-- Red Team 2026-11-06: Finding 7 -->
4. **Colon rule**, in this order:
   1. The whole pattern as an exact match (requirement 2), so a literal id such as `qwen3-coder:30b` or `x:max` wins.
   2. When the text after the last colon is a level word: a match (exact, alias or fuzzy) whose real id ends in `:<level>` wins, with no level. Otherwise resolve the text before the colon with all steps, and the result carries the level.
   3. When the suffix is not a level word (or there is no colon): alias (`sameModelID`, alias first), then fuzzy on the whole pattern. Fuzzy is a case-insensitive substring of id or name; with a provider, only rows of that provider; the first row in display order wins; matches in more than one provider are `ErrAmbiguousModel`.
   4. When nothing matched and the pattern has a colon with an invalid suffix: with `AllowInvalidLevel` true, resolve the text before the colon and return a warning `invalid thinking level "<suffix>"; the default level is used` and no level. With `AllowInvalidLevel` false (CLI `--model`, session options), return `ErrUnknownModel`.
   5. No match: `ErrUnknownModel` with the pattern.
5. **Scope lists** (`--models`, `enabledModels`): each entry is a pattern or a glob (`*`, `?`, `[`). A glob matches the lower-case `provider/id` or `id` with `path.Match`; it may carry `:level` (a level word only). A plain entry uses `ResolvePattern` with `AllowInvalidLevel` true, as omp does for scopes. The result keeps entry order, then display order inside one entry, with duplicates removed. An entry that matches nothing, or a bad glob, is a problem, not an error.
6. The level is returned as given. The caller applies `--thinking` first (M8), then the clamp of the model, and reports a dropped level (M16, phase 6).
7. The functions read rows only; they do not know `ModelSet` internals beyond `All()`.

## Files

| Action | Path | Notes |
|---|---|---|
| Create | `internal/providers/model_pattern.go` | `ParseRef`, `ResolvePattern`, `ExpandScope`, errors |
| Create | `internal/providers/model_pattern_test.go` | Table tests |
| Modify | `internal/acp/ask_methods.go` | `parseModelID` calls `providers.ParseRef` <!-- Red Team 2026-11-06: Finding 7 --> |

Phases 1-3 do not touch `internal/acp`; phase 5 changes other parts of `ask_methods.go` after this phase.

## Steps

1. Write the `ParseRef` table and move `parseModelID` onto it; run `go test ./internal/acp`.
2. Write the resolve table over fixed rows, then `ResolvePattern` in the order of requirement 4, with every exact step on `findIn`.
3. Write the scope test, then `ExpandScope` with `path.Match`.
4. Give each error a type (`ErrUnknownModel`, `ErrAmbiguousModel` with the choices) so callers print one clear line.

## Tests

`TestParseModelRef` table: `anthropic/claude-opus-4-5`, `openai/gpt-5.5@openai-responses`, `a/b@x`, `/x`, `x/`, `a/@api` (errors keep the existing text).

<!-- Red Team 2026-11-06: Finding 7 -->
`TestResolveModelPattern` rows: two Token Plan rows of one id on two APIs, an Anthropic alias and its dated snapshot, the same bare id in two providers, `local/qwen3-coder:30b`, `router/x:max`, `kimi` and `kimi-highspeed`, `openrouter/qwen/qwen3:free`, `gpt-5.5`.

| Pattern | AllowInvalidLevel | Result |
|---|---|---|
| `local/qwen3-coder:30b` | false | row `qwen3-coder:30b`, no level |
| `router/x:max` | false | row `x:max` (literal id wins) |
| `kimi:high` | false | row `kimi`, level high (not `kimi-highspeed`) |
| `anthropic/claude-opus-4-5:high` | false | alias row, level high |
| `gpt-5.5:hgih` | false | `ErrUnknownModel` |
| `gpt-5.5:hgih` | true | row `gpt-5.5`, warning, no level |
| `openrouter/qwen/qwen3:free` | false | row `qwen/qwen3:free` |
| bare id in two providers | false | `ErrAmbiguousModel` listing both |
| `openai/GPT-5.5` | false | fuzzy, one row |
| two rows equal after case folding | false | `ErrAmbiguousModel` |
| `alibaba-token-plan/<id>@openai-completions` | false | the completions row |

`TestExpandModelScope`: `anthropic/*`, `*sonnet*:low`, a pattern that matches nothing (problem), a bad glob `[`, duplicate removal, order, an entry with an invalid level (warning).

```
go test ./internal/providers -run 'TestParseModelRef|TestResolveModelPattern|TestExpandModelScope' -count=1
go test ./internal/acp -count=1
go vet ./internal/providers ./internal/acp
```

## Validation

- `go test ./internal/providers ./internal/acp -count=1` passes; `parseModelID` gives the same results and texts as before.

## Risk

| Risk | L x I | Mitigation |
|---|---|---|
| Fuzzy match picks a model the user did not mean | Med x Med | Fuzzy runs after exact and alias; cross-provider fuzzy is an error; the resolved `provider/id@api` is printed in print-mode warnings and shown in the footer |
| A typo in a level on the command line is reported as an unknown model | Med x Low | The error text lists the valid level words when the text before the colon resolves |

## Rollback

Revert the commit. `parseModelID` returns to its own split; no other caller exists until phases 5 and 6.
