---
phase: 2
title: "Generated catalog and family rules"
status: completed
priority: P1
effort: "2d"
dependencies: [1]
---

# Phase 2: Generated catalog and family rules

<!-- Updated: Red Team 2026-10-28 - family rule required for every row; GeneratedCatalog() keeps catalog() unchanged; filter, cost tiers, full effort map, request defaults, sanitizing -->

## Goal

Map an embedded models.dev snapshot into `providers.Model` rows. Every row must match a local family rule. Give the rows through a new function `GeneratedCatalog()`, and leave `catalog()` unchanged. Phase 3 reuses the same mapper at run time.

## Context

- Plan decisions P1-P4 and P13. Phase 1 rule table.
- Model contract:
  - `internal/providers/model.go:9-15` (`ThinkingLevelMap`), `:17-35` (micro-USD `Price`, `Tier.Above`), `:49-64`, `:82-89` (`Validate`).
  - `internal/providers/compat.go:1-42`.
  - `internal/providers/clamp.go:19-55`.
- The Responses wire sends an effort only for an explicit map word (`internal/providers/openai/prepare.go:102-104`).
- Docs generation: `cmd/tui/reference_docs_test.go:221-267`. It prints `strconv.Itoa(m.ContextWindow)`.
- Update pattern to copy: `ASK_UPDATE_DOCS=1 go test -run '^TestReferenceDocs$' ./cmd/tui`.

## Files

- Create `internal/providers/catalog_rules.go`. It holds one table of family rules. Each rule has these fields:
  - provider and id pattern;
  - API and base URL, as constants from `api.go`;
  - a compat value;
  - request defaults: output limit and context window;
  - allowed effort words;
  - a "thinking off only" flag (P2).

  It also has the keep predicate (P3) and the id and name check: printable ASCII, no `@`, at most 128 bytes.
- Create `internal/providers/modelsdev.go`. It has models.dev payload types (only the fields used) and `MapModelsDev(payload []byte) ([]Model, error)`. The mapper reads only these models.dev fields: `id`, `name`, `limit`, `cost`, `reasoning`, `reasoning_options`, `modalities`, `tool_call`, `type`, `status`. It never reads `api`, `npm`, `provider` or headers.
- Create `internal/providers/catalog_snapshot.json`: the filtered models.dev subset for anthropic, openai, xai and alibaba-token-plan, embedded with `go:embed`.
- Create `internal/providers/catalog_snapshot_test.go` with `TestCatalogSnapshot`:
  - With `ASK_UPDATE_CATALOG=1`, it fetches `https://models.dev/api.json`, keeps the 4 providers and the used fields, and writes the snapshot.
  - Without that variable, it checks that the snapshot parses and maps. It sends no network request.
- Create `internal/providers/generated_catalog.go` with the exported `GeneratedCatalog() []Model` (exported because `cmd/tui/reference_docs_test.go` reads it). It returns the mapped snapshot rows merged with the hand-written rows; a hand-written row wins on provider + id + API. The function is not used by `catalog()` until phase 5.
- Create `internal/providers/modelsdev_test.go` and `catalog_rules_test.go`.
- Modify `internal/providers/model.go`: add `CostUnknown bool` (P13). A context window or output limit of 0 keeps its meaning: "unknown for display".

## Steps

1. Write table tests first for `MapModelsDev`. Use a small fixture with these rows:
   - a chat row;
   - a realtime row, an image row and an embedding row (all dropped);
   - a deprecated row (dropped);
   - a row with an unknown `reasoning_options` type (dropped);
   - a row with `api`, `npm` and `provider` override fields (ignored);
   - a row whose id contains an escape sequence (dropped).
2. Rule match: a row that matches no family rule is dropped (U2). The rule sets API, base URL and compat. models.dev data never sets them.
3. Thinking map. Write a full map, with one explicit word or `nil` for each of the 7 levels (off, minimal, low, medium, high, xhigh, max).
   - The words are the intersection of the rule's allowed words and the models.dev `effort` values.
   - `none` in the list means that off is supported.
   - A "thinking off only" rule sets `Reasoning: false`.
4. Limits. Map `limit.context` and `limit.output`. When either is missing, the row keeps 0 for display, and the request default comes from the rule (see step 6).
5. Cost. Map base prices and `cost.tiers` (`tier.type == "context"`) to micro-USD with `math.Round`, as `Tier{Above: size}`. Ignore `context_over_200k`. Missing cost sets `CostUnknown`.
6. Request defaults. Add a `RequestLimits()` helper that returns the effective output limit and context window. It uses the model value when it is above 0, else the rule default. Change `clampMaxTokens` in `anthropic/document.go` and `openai/completions_body.go` to use it.
7. Parity test: map the models.dev rows of `gpt-5.5`, `gpt-5.6-sol`, `claude-sonnet-4-6` and `grok-4.7`. Compare them with the hand-written rows (prices, tiers, limits). Record each difference in the test with its reason.
8. Effort test: walk each generated Responses and Completions row through `Prepare`. For every level that `ClampThinkingLevel` accepts, check that `Prepared.Effort` is set.
9. Generate the snapshot with `ASK_UPDATE_CATALOG=1` and commit it. Record its fetch date in a top-level field of the file.
10. Reference doc: extend `renderProvidersDoc` with a second table, "Generated rows (active from phase 5)", built from `GeneratedCatalog()`. Show 0 as `?`. Regenerate the doc.

## Verification

- `go test ./internal/providers/...` passes. This includes the mapper, rule, parity and effort tests.
- `go test ./...` passes. `catalog()` is unchanged, so existing row-count tests (`internal/providers/catalog_test.go:157-160`) still pass.
- `ASK_UPDATE_DOCS=1 go test -run '^TestReferenceDocs$' ./cmd/tui`, then `go test -run '^TestReferenceDocs$' ./cmd/tui`, passes.
- `go vet ./...` and the lint command pass.
