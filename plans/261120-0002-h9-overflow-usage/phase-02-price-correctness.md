---
phase: 2
title: "Price correctness"
status: pending
priority: P1
effort: "3h"
dependencies: []
---

# Phase 2: Price correctness

## Goal

No priced attempt has a silent zero cost, and every model can say whether its price is known, so the totals can mark a partial cost.

## Context

- `pickTier` gives a zero price when no tier has `Above < tokens`: `internal/providers/cost.go:102-112`. Callers: `CalculateCost` (`cost.go:72-100`), used only by `internal/providers/openai/usage.go:29`.
- The Anthropic adapter prices nothing: `internal/providers/anthropic/provider.go:314-321` passes no `Usage` function; the default token copy is `internal/providers/fantasykit/fold.go:270-280`. The token split is already Anthropic-style (uncached input), see `TestStreamUsageComesFromTheFinalCounters` (`internal/providers/anthropic/provider_test.go:195-207`).
- Hand rows with no `Cost` and `CostUnknown` false: `internal/providers/catalog.go:37,53,157,164`. The hand row wins over the generated row: `internal/providers/generated_catalog.go:16-40`.
- The reference-docs generator already treats "no tiers" as unknown: `cmd/tui/reference_docs_test.go:298-300`.
- The faux model rows have no `Cost` (`internal/providers/faux/faux.go:170-189`), so without requirement 5 every demo session would read cost-incomplete.
- A `models.json` model without `cost` gets one zero-price tier (`internal/providers/model_config.go:258-264`), which is the documented Pi default "a price of 0" (`docs/cli.md:627`). It stays priced (free).
- Scope note: H4 owns pricing (`roadmap.md:162,248`). Requirements 2 and 3 widen the approved `pickTier` fix to the other silent zeros that the scout found. Requirement 2 is necessary for decision 2A: `PriceKnown` reads the model row, not the adapter, so without requirement 2 a priced Anthropic-wire row (for example a generated `claude-opus-*` row) still reports a known price and a cost of $0. There is no fallback that keeps 2A true; the plan asks the user only to confirm the H4 boundary (plan.md, unresolved question 2).

## Requirements

1. `pickTier`: when no tier has `Above < tokens`, use the tier with the lowest `Above`. Empty tiers still give the zero price (the caller marks it unknown).
2. The Anthropic adapter passes a `Usage` function that keeps the default token copy and sets `Cost` with `providers.CalculateCost(m, u, "")`. It lives in a new `internal/providers/anthropic/usage.go`, next to the OpenAI pattern.
3. `func (m Model) PriceKnown() bool` is true when `!m.CostUnknown && len(m.Cost.Tiers) > 0`. The doc comment of `CostUnknown` (`model.go:64-66`) says that a row with no tiers is unknown too. The reference-docs generator uses `PriceKnown` so the rule has one owner; its output does not change.
4. No catalog row changes: the hand rows stay unpriced and now report unknown through `PriceKnown`.
5. The faux provider is a free scripted demo: `buildModel` gives each faux row one zero-price base tier, so a faux session reads cost-complete at $0. A `faux.Step.WithUsage` cost still passes through unchanged.
6. The OpenAI adapters keep their pricing path (`openai/usage.go:12-31`); `internal/providers/acp` holds no adapter code (only `README.md` and `doc.go`), so no other wire needs a change.

## Files owned

- Modify: `internal/providers/cost.go`, `internal/providers/cost_test.go`, `internal/providers/model.go`, `internal/providers/anthropic/provider.go`, `internal/providers/faux/faux.go`, `internal/providers/faux/faux_test.go`, `cmd/tui/reference_docs_test.go` (call site only)
- Create: `internal/providers/anthropic/usage.go`, `internal/providers/anthropic/usage_test.go`

## Tests written first

- `TestPickTierFallsBackToLowestTier`: zero prompt tokens and output 1000 on a row with base `Above 0` give the base output price.
- `TestPriceKnown`: models.dev row with tiers (true), `CostUnknown` (false), hand row with no tiers (false), `models.json` row with a cost (true), free row with zero prices in a tier (true).
- `TestAnthropicUsageHasCost`: a priced Anthropic row and the final counters give the expected micro-USD cost; `AnthropicSonnet46()` gives cost 0 and `PriceKnown` false.
- `TestFauxRowsAreFree`: every faux row has `PriceKnown` true and a reply costs 0.
- `TestReferenceDocs` passes unchanged.

## Steps

1. Write the tests; they fail.
2. Fix `pickTier`; add `PriceKnown`; switch the generator call site.
3. Add the Anthropic `Usage` function.
4. Run the validation.

## Validation

```
go test ./internal/providers/... ./cmd/tui -run 'TestReferenceDocs|TestCatalog'
go vet ./internal/providers/...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./internal/providers/... ./cmd/tui/...
```

## Risks

| Risk | L x I | Mitigation |
|---|---|---|
| Anthropic cost changes a cassette or a golden that holds a zero cost | M x L | `rg -n '"cost"' internal/providers/anthropic cmd/tui/testdata` before the change; update the expected values on purpose |
| A row that has a real price but whose tiers start above 0 is now priced at its lowest tier | L x L | this matches the intent of a base tier; test row covers it |
| H4 owner disagrees with the wider fix | M x M | without requirement 2, decision 2A cannot hold for Anthropic-wire rows; the user confirms the boundary before this phase starts |
| `docs/reference/providers.md` changes when faux rows get a tier | L x L | faux rows are not in the reference table (the table lists `providers.GeneratedCatalog()`, `cmd/tui/reference_docs_test.go:291`; `docs/reference/providers.md:88` says faux is not in it); the drift test proves it |

## Rollback

Revert the commits. Costs return to the old values; `PriceKnown` has no reader until phase 4.

## Code hygiene

No plan labels, phase numbers or decision codes in code, comments, test names or commit messages.
