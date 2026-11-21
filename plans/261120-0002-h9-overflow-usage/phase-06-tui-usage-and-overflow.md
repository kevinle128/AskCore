---
phase: 6
title: "TUI: cumulative usage and overflow text (design gate first)"
status: pending
priority: P1
effort: "8h"
dependencies: [5]
---

# Phase 6: TUI: cumulative usage and overflow text

## Goal

The TUI shows the cumulative session cost (marked partial when not all of it is known) and the server context estimate, and it tells overflow, quota exhaustion and a transient retry apart, also for a silent overflow. This is the roadmap TUI exit for H9 (`roadmap.md:125`).

## Gate (blocking)

No Go code of this phase is written before `tui-design.md` in this folder has `design-status: approved`, set only after the user approves it in the conversation (`.claude/skills/ask-tui-design/SKILL.md`, "Hard rule: the approval gate"). The design file is `tui-design.md`, not `design.md`, because `design.md` here is the technical design. Step 1 (the design) may start before phase 5 ends; steps 3-6 wait for phase 5 and for the approval.

## Context

- The footer shows no session cost by current design: `internal/tui/footer.go:10-18`, `docs/tui-architecture.md:93`, `docs/tui-style-guide.md:96`, `internal/tui/README.md:119-120`. Layout owner: `plans/261028-1310-account-model-discovery/design.md`. The new design reverses the "no session cost" rule.
- The context figure is computed on the client from the last reply: `internal/tui/update_session.go:469-498,521-526`, `internal/tui/footer.go:58-65`, field `internal/tui/session.go:182`. It must be replaced by the server estimate, not kept beside it.
- `Client.Usage` exists and no screen calls it: `internal/tui/client.go:157`, `internal/tuiclient/client.go:775-782`. Refresh pattern: `refreshCmd` `internal/tui/update_session.go:829-851`.
- Overflow, quota and rate limit already have their own error kind and next action: `internal/tui/translate_errors.go:17-30`; retry banner `update_session.go:514-520`, `translate.go:608-617`. A silent overflow has no error text, so `writeError` falls back to "The request failed." (`internal/tui/translate.go:619-646`).
- `internal/tui` imports only `pkg/protocol` (`.golangci.yml:28-37`); all data comes from `ACPUsageResult` and events.
- Golden harness: `startView` (`internal/tui/session_rig_test.go`), `requireGolden`; existing footer goldens `TestFooterRowGoldens` (`internal/tui/footer_test.go:78`).

## Requirements

1. Design (skill steps 1-4): `tui-design.md` with sketches at 80x24 (and 120x40 where the row changes, plus a narrow case) for these states: no attempt yet; priced total; partial total; no known price at all; context unknown window; silent overflow error block; quota error block; retry banner (unchanged, for contrast). Include the give-way order of the footer parts and the preview page (`artifacts/preview.html`). Planner default for the sketches: ` ⌂ ~/src/askcore   openai/gpt-5.5 ◐ high  $0.42  ▰▱▱▱▱▱▱▱ 18% `, partial `≥ $0.42`, no known price `↑1.2M ↓34k`, cost in `muted`; under 60 columns the cost goes first.
2. After approval: one source at each point. At `agent_settled` the footer reads `agent_settled.usage` (phase 4), with no extra call. After `turn_end` (mid-run) and after a snapshot (load or attach), the session view calls `Client.Usage`, with the same stale-result guard as `refreshCmd`. Both give the same `UsageTotals` shape. The footer reads cost, the partial flags and `contextTokens` from it. The client-side `snapshotContext`/`usageTokens` figure is removed.
3. A silent overflow (`cycle_end` error, code `CONTEXT_WINDOW_EXCEEDED`, no error reply) shows the `context` error block with the headline text of the design, not "The request failed.".
4. The per-reply meta line (`translate.go:499-521`) stays as it is: display only.

## Files owned

- Create: `plans/261120-0002-h9-overflow-usage/tui-design.md`, `plans/261120-0002-h9-overflow-usage/artifacts/*`, `internal/tui/usage_footer_test.go`, golden files under `internal/tui/testdata/` for the new tests
- Modify: `internal/tui/footer.go`, `internal/tui/footer_test.go`, `internal/tui/update_session.go`, `internal/tui/session.go`, `internal/tui/translate.go`, `internal/tui/translate_errors.go`, `cmd/tui/interactive_test.go`, existing footer goldens that change on purpose
- Not here: `docs/tui-architecture.md`, `docs/tui-style-guide.md`, `internal/tui/README.md` (phase 7 writes the approved rules)

## Tests written first (after approval)

- One golden test for each approved state at each approved size, named by the state (for example `TestFooterShowsSessionCost`, `TestFooterMarksPartialCost`, `TestFooterShowsTokensWithoutPrice`).
- `TestFooterUsesServerContextEstimate`: the fake client returns `contextTokens` 180 000 for a 1 000 000 window; the footer shows 18%.
- `TestSilentOverflowShowsContextBlock`.
- `TestQuotaRetryAndOverflowLookDifferent`: three runs (402, 429 then success, overflow) give three different blocks or banners.
- `cmd/tui/interactive_test.go`: the built `ask` on a pseudo terminal with the faux provider shows a cost or token figure in the footer after one reply.
- Each acceptance criterion of `tui-design.md` maps to one named test.

## Steps

1. Write `tui-design.md` and the preview with the skill; run its self-check; ask the user to review; stop.
2. Wait for `design-status: approved`.
3. Write the failing golden tests from the approved sketches.
4. Wire `Client.Usage` and the footer; remove the client-side figure.
5. Add the silent-overflow headline.
6. Run the validation; compare every golden diff with the approved sketch.

## Validation

```
go test ./internal/tui/... ./cmd/tui/...
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
go build -o ask ./cmd/tui && python3 e2e/tui/run.py --driver ask
```

## Risks

| Risk | L x I | Mitigation |
|---|---|---|
| The user does not approve the footer change | M x M | the phase waits; phases 1-5 ship without it; the roadmap TUI exit stays open |
| An extra RPC after every turn slows a long tool loop | L x L | one local call per turn; results older than the current sequence are dropped |
| The footer is too wide at 80 columns | M x L | give-way order fixed in the design |

## Rollback

Revert the commits. The footer returns to the client-side figure and no cost.

## Code hygiene

No plan labels, phase numbers or decision codes in code, comments, test names or commit messages.
