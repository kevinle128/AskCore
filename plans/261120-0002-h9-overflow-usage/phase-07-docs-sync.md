---
phase: 7
title: "Docs sync"
status: pending
priority: P2
effort: "3h"
dependencies: [5, 6]
---

# Phase 7: Docs sync

## Goal

Every user-visible change of H9 has its docs and one CHANGELOG line, the owning READMEs describe the new owners, and the roadmap row records what H9 delivered and the approved wording change.

## Context

- Repository rules: `AGENTS.md` ("Documentation", "Maintenance"); ASD-STE100 Simplified Technical English (`CONTRIBUTING.md`); run the `ask-docs-sync` skill (`.claude/skills/ask-docs-sync`).
- `docs/cli.md:64` (print-mode exit codes), `docs/cli.md:361-383` (`ask acp`), `docs/cli.md:520` (interactive TUI).
- TUI rules that say "no session cost": `docs/tui-architecture.md:93`, `docs/tui-style-guide.md:96`, `internal/tui/README.md:119-120`.
- `docs/reference/` is generated; phase 2 does not change its output, so no regeneration is expected. Run the drift test to prove it.

## Requirements

1. `docs/cli.md`: overflow in print mode (stderr text, exit 1 for both forms), JSON mode `cycle_end.cause` `context_overflow` and `agent_settled.usage` (fields and meaning of `complete`, `costComplete`, `contextTokens`, `contextWindow`), ACP `_ask/session/usage` fields and the `context_overflow` error kind, the TUI footer cost and partial marker.
2. `CHANGELOG.md` under `[Unreleased]`, one line each: overflow detection (more provider texts, 429/402 never overflow, byte-size 413 not overflow, silent overflow); overflow cause word; Anthropic-wire cost and unknown-price marking; `pickTier` fix; totals with partial flags and context estimate on ACP and JSON; TUI footer cost; logs written before this change count as priced.
3. READMEs: `internal/providers/README.md` (overflow owner `overflow.go`, `PriceKnown`), `internal/sessions/README.md` (owner rows for `usage.go` and `context_estimate.go`, the `costUnknown` field), `internal/agent/README.md` (overflow ends the cycle, no retry; settled usage), `internal/acp/README.md` (usage method, new kind), `internal/tui/README.md` (footer numbers from the server estimate, session cost).
4. TUI docs: `docs/tui-architecture.md:93` and `docs/tui-style-guide.md:96` follow the approved `tui-design.md` (and the `≥` glyph entry if the approved marker uses it).
5. `docs/ask-architecture-reference.md`: no change unless an import rule changed (none planned).
6. Roadmap: the H9 row of `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md:400-414` gets a status line and links to this plan; the H-RETRY-10 line records "met by `AttemptSettled` entries and the shared fold; no new entry type (user, 2026-11-20)"; line 379 records that the `context_edit` writer moved to H10.
7. `AGENTS.md`: update only if a command, a structure item or a dependency changed (none planned).

## Files owned

- Modify: `docs/cli.md`, `CHANGELOG.md`, `docs/tui-architecture.md`, `docs/tui-style-guide.md`, `internal/providers/README.md`, `internal/sessions/README.md`, `internal/agent/README.md`, `internal/acp/README.md`, `internal/tui/README.md`, `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md`, `AGENTS.md` only if requirement 7 applies
- Plan status: change it with the `ak plan` commands (`ak plan --help`), not by hand

## Steps

1. Run the `ask-docs-sync` skill on the merged change set.
2. Write each doc change; read each file before the edit.
3. Run the drift test and the link checks.

## Validation

```
go test -run '^TestReferenceDocs$' ./cmd/tui
go test ./...
rg -n "no session cost" docs internal/tui
```

The last command must return no stale rule after the approved TUI change.

## Risks

| Risk | L x I | Mitigation |
|---|---|---|
| A doc states a default that the user changed in review | M x L | write docs from the approved plan.md and `tui-design.md`, not from this phase text |

## Rollback

Revert the commits.

## Code hygiene

Docs may name this plan in the roadmap and in plan files only. No plan labels in code, comments, test names or commit messages.
