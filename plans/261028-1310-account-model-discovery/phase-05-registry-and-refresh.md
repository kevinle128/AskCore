---
phase: 5
title: "Model set, composition and refresh"
status: completed
priority: P1
effort: "2.5d"
dependencies: [2, 3, 4]
---

# Phase 5: Model set, composition and refresh

<!-- Updated: Red Team 2026-10-28 - ModelSet name; refresher owned by ACPRuntime at 3 sites; OnChange schedules only; stale-result fence; current model row; test switch; alias matching; fallback order; Authenticate per provider -->

## Goal

Build the `/model` list from three sources: the built-in and generated catalog, the metadata overlay, and the account lists. Use that list for selection and session start. Refresh it at the agreed points, with no polling and no network calls in tests.

## Context

- Plan decisions P5, P8, P9, P11, P12, U2, U3 and U6. Accepted design Q4=A.
- Red-team findings 1, 4-10 and 15.
- Callers to move:
  - `internal/app/module_acp.go`: `rows` at 91-97, `find` at 98-104, the `Authenticate` loop at 143-150, cleanup at 150-161.
  - `internal/acp/ask_methods.go:147-186` (`models()`).
  - `internal/app/default_model.go:11-100` (session start choice, fallback reasons).
  - `cmd/tui/headless_faux.go:72-94` (`ask -p --provider --model`).
  - `internal/app/provider_setup.go:41,78-92` (`/providers` model count).
- Composition sites:
  - `internal/app/module_leader.go:78`;
  - `cmd/tui/acp.go:76`;
  - `cmd/tui/tui_client.go:96`.
- Change hook: `internal/auth/broker.go:676-686` (`OnChange` runs under `changeMu`).
- Other: `cmd/tui/auth.go:86-88` (`ask auth login` and `logout` in a separate process).

## Files

- Create `internal/providers/model_set.go` with `ModelSet`. It is an immutable snapshot that a holder swaps atomically. Its API:
  - `Models(provider) []Model`;
  - `Find(Ref) (Model, error)`;
  - `NewModelSet(builtin []Model, overlay []Model, accounts map[string]AccountView, aliases func(id string) []string)`.

  Merge rules:
  - Start with the built-in and generated rows.
  - Add overlay rows only for ids that the built-in rows lack. An overlay row never replaces a built-in row.
  - Account state `known`:
    - Keep rows whose id or alias is in the list.
    - For a listed id with no row but a matching family rule, add a fallback row. Its metadata is unknown for display, and its request defaults come from the rule.
    - Drop listed ids with no rule (U2).
  - Account state `unknown`: keep all rows of the provider (Q4=A).
  - Display order is fixed by Ask: the rule family order, then the version from newest to oldest, then the id. It never follows the vendor's order.
- Create `internal/providers/model_set_test.go`.
- Create `internal/app/model_refresh.go` with `modelRefresher`, a field of `ACPRuntime`. It holds:
  - a root context and a wait group;
  - an HTTP client;
  - a clock;
  - an on/off switch.

  Its methods:
  - `Load()` builds the `ModelSet` from the cache. It makes no network call.
  - `Schedule(provider)` is a non-blocking request. It is safe to call under any lock.
  - `RefreshIfStale()`.
  - `Refresh(ctx, providers...)`.
  - `Stop()`.

  In-flight rules:
  - One refresh runs at a time for each account key, and one for the overlay.
  - Each refresh records the broker change generation when it starts. The refresher drops the result if that generation moved before commit.
- Create `internal/app/model_refresh_test.go`. It uses a fake list function, a fake overlay fetch, a temp `ASK_HOME`, and goleak.
- Modify `internal/app/module_acp.go`:
  - `ACPParams` gets a `ModelRefresh` setting (HTTP client, clock, enabled). It is enabled in the three product compositions.
  - `rows` and `find` use the `ModelSet`.
  - `Authenticate` checks readiness once per provider and method, not once per row.
  - `Cleanup` calls `refresher.Stop()` before `StopRefresh`.
- Modify `internal/acp/ask_methods.go`:
  - `models()` calls `RefreshIfStale()`, which runs on the refresher's root context, never on the request context. It then answers from the current snapshot.
  - It always adds the current session model row when the snapshot lacks it, marked "not in account list".
- Modify `internal/app/default_model.go`:
  - Add the reason `ACPFallbackNotInAccount`.
  - Check the recommended model against the `ModelSet`.
  - The fallback order is: the saved default, then the recommended model, then the first row in Ask's display order.
- Modify `cmd/tui/headless_faux.go` to resolve through a `ModelSet` built from the cache and the catalog, with no network call.
- Modify `internal/app/provider_setup.go`:
  - `OnChange` calls `refresher.Schedule(provider)` for saves and removals.
  - For a removal, it also deletes the cache entries of that provider.
  - The model count comes from the `ModelSet`.
- Modify `cmd/tui/auth.go`:
  - After `ask auth login`, delete the provider's cache entries and refresh that provider in process.
  - After `ask auth logout`, delete the entries.
  - No leader is needed.
- Modify the test fixtures that build `ACPParams` so they turn the refresher off. The files are `internal/app/*_test.go`, `internal/acp/*_test.go` and `cmd/tui/*_test.go`; list each file in the commit.
- Modify `pkg/protocol/acp.go`, `internal/acp/ask_methods.go:179-183` and `internal/tuiclient/client.go`: add `ACPModel.CostUnknown` (omitempty) and `NotInAccount` (omitempty).

## Steps

1. Write the `ModelSet` tests first:
   - each merge rule;
   - duplicate ids and alias matching;
   - Token Plan with two APIs;
   - an overlay row that tries to replace a built-in row;
   - display order;
   - a fallback row whose request defaults are above 0.
2. Start path (leader, `ask acp`, in-process TUI): `Load()` from the cache, then `RefreshIfStale()` in the background. The start path makes no network call.
3. Reload: before `models()` answers, the refresher compares the cache file's modification time and size with the values it last read. If they changed, it calls `Load()` again. This picks up a write by `ask auth login` or `ask models refresh`.
4. Errors:
   - A failed refresh keeps the last snapshot and cache entry.
   - It sets `retryAfter` to now + 5 min, which the next `RefreshIfStale` checks. There is no timer.
   - It logs the cause.
5. The model of a running session does not change. The current row is always in the `models()` answer (step 2 of `ask_methods.go`).
6. Network guard test. Run the package tests with an `http.RoundTripper` that fails on any host other than `127.0.0.1`. Do this for the app, acp and cmd/tui fixtures, so that a fixture that forgets to turn the refresher off fails.

## Verification

- `go test ./internal/providers/... ./internal/app/... ./internal/acp/... ./cmd/tui/... ./internal/tui/...` passes.
- A test with 10 concurrent callers proves one in-flight refresh per account key.
- A test proves that a result from before a credential change is discarded: logout during an in-flight fetch.
- A test proves that `Stop()` during an in-flight refresh returns, and goleak is clean.
- A TUI test proves that the header keeps the window and the thinking key after a refresh that removes the running model.
- `rg -n "time.Sleep|NewTicker|time.Tick" internal/app/model_refresh.go internal/providers/model_set.go` finds nothing.
- `go vet ./...` and the lint command pass.
