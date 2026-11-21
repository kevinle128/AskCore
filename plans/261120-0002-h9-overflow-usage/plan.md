---
title: "H9: overflow detection and usage accounting"
description: "Detect context overflow (explicit and silent) with a distinct cycle end and no retry, fix silent zero prices, and give every surface one usage fold, a partial flag and one context estimate."
status: completed
priority: P1
effort: 36h
branch: task/h9-plan
tags: [feature, backend, providers, agent, sessions, acp, tui]
blockedBy: []
blocks: []
created: 2026-11-20
---

# H9: overflow detection and usage accounting

## Outcome

A provider overflow error, and a reply whose reported input is at or over the model window, ends the cycle with reason `error`, code `CONTEXT_WINDOW_EXCEEDED` and cause `context_overflow`. No retry and no model switch occur. A 429, 402 or quota error is never overflow. Every surface (ACP `_ask/session/usage`, `ask -p --mode json`, the TUI footer) reads one fold of `AttemptSettled` entries. That fold gives the known cost sum, a partial flag, and one context estimate that H10 also uses as its trigger.

## Constraints

- Binding decisions 1A-4A (below). No new session entry type; `FormatVersion` stays 1.
- Import rules: `docs/ask-architecture-reference.md`, `.golangci.yml` depguard. `sessions` imports only `pkg/protocol`; `internal/tui` only `pkg/protocol`.
- Tests of user-visible behaviour run the built `ask` against a scripted HTTPS provider (`providerFixture`, `builtAsk`) or the real Agent with faux. No production test seam.
- The TUI change goes through `.claude/skills/ask-tui-design` and waits for the user's approval (phase 6 gate).
- No plan labels, phase numbers or decision codes in code, comments, test names or commit messages. Docs in ASD-STE100.

## Non-goals

- Compaction and the compact-and-retry (H10). H9 leaves the seam described in [design.md](design.md#extension-trace-h10-compact-and-retry).
- A writer of `context_edit` entries (moves to H10).
- Credential switching and quota wait (M2), forks (H14), a tokenizer, a model switch on overflow.
- Usage of model calls outside the transcript (compaction, titles): none exist yet.

## Acceptance criteria

1. Built `ask -p` against a 400 overflow body makes one request, exits 1, and writes `Context window exceeded:` on stderr. A 429 with token wording retries. A 402 and a byte-size 413 are not overflow.
2. A 200 reply with `input_tokens` above the window prints the reply, writes the silent-overflow text and exits 1. In JSON mode, `cycle_end` has `cause:"context_overflow"`.
3. `agent_settled.usage` and `_ask/session/usage` agree with `sessions.FoldUsage` of the log. A retried run counts both attempts. A missing usage gives `complete:false`, and an unpriced model gives `costComplete:false`. Totals survive `-c`.
4. `contextTokens` is the last completed attempt usage plus chars/4 of the later messages, and agrees with the rebuilt next request.
5. After the approved design, the TUI footer shows the session cost (partial marker when incomplete) and the server estimate, and overflow, quota and retry look different.
6. `go test ./...`, `go vet ./...`, golangci-lint, the reference-docs drift test and the touched `e2e/tui` cases pass.

## Phases

| # | Phase | Effort | Depends on | Status |
|---|-------|--------|------------|--------|
| 1 | [Overflow classification](./phase-01-overflow-classification.md) | 5h | - | Done |
| 2 | [Price correctness](./phase-02-price-correctness.md) | 3h | - | Done |
| 3 | [Usage fold and context estimate](./phase-03-usage-fold-and-context-estimate.md) | 5h | - | Done |
| 4 | [Agent: overflow cycle end, price stamp, settled usage](./phase-04-agent-overflow-and-settled-usage.md) | 6h | 1, 2, 3 | Done |
| 5 | [ACP and headless surfaces](./phase-05-acp-and-headless-surfaces.md) | 6h | 4 | Done |
| 6 | [TUI: cumulative usage and overflow text (design gate)](./phase-06-tui-usage-and-overflow.md) | 8h | 5 | Done |
| 7 | [Docs sync](./phase-07-docs-sync.md) | 3h | 5, 6 | Done |

Phases 1, 2 and 3 can run in parallel; their file sets do not overlap. The design step of phase 6 writes only files in this folder, so it can start at once. Its code waits for phase 5 and the approval. Facts, data flow, interfaces, test matrix, rollback and risks are in [design.md](design.md).

## Roadmap H9 walk (`roadmap.md:400-414`, plus related lines)

| Roadmap line | Result |
|---|---|
| H-RETRY-06 overflow detection | Kept: Pi patterns with attribution, the 429/402 veto (also over the SDK flag), the 413 split and silent overflow (phases 1, 4) |
| H-RETRY-07 totals across turns and usage entries; H4 owns pricing | Kept: shared fold (phase 3). Pricing: `pickTier` fix plus the wider silent-zero fix, which needs approval (phase 2) |
| H-RETRY-08 totals | Kept: ACP, JSON, TUI read one fold. Compaction, branch and tool-call usage do not exist yet |
| H-RETRY-09 context estimate | Kept: last valid usage plus chars/4, `context_edit` drops the anchor, images 4800 chars (phase 3) |
| H-RETRY-10 persistent usage entries | Changed on purpose (user, 2026-11-20): met by existing `AttemptSettled` entries plus tests; no new entry type |
| Retained retry decision (auto_retry on the wire; failed attempt log-only) | Kept, unchanged |
| Test: a 429 is not overflow | Kept (phases 1, 5) |
| Test: subscription quota exhaustion is not transient | Kept: 402 and usage-limit 429 stay `QUOTA` and are not retried (phases 1, 5) |
| Test: totals include usage across attempts and turns | Kept (phases 3, 5) |
| Test: the estimate agrees with the selected context | Kept: pinned against `rebuildRequest` (phase 4) |
| Exit: classification and cumulative cost agree with the recorded requests and usage entries | Kept: built-binary checks (phase 5) |
| `roadmap.md:379` `context_edit` writer belongs to H9 and H10 | Moved: H10 only (1A: no compaction, no retry) |
| `roadmap.md:260` no other text-pattern retry classifier | Kept: the overflow patterns classify; the retry list is unchanged |
| `roadmap.md:63` D11 no jitter | Kept, untouched |
| `roadmap.md:125` TUI exit: cumulative usage; overflow, quota, retry distinct | Kept (phase 6, after approval) |
| Inventory note "patterns as configurable data" | Not taken: compiled table (default needing approval) |

## Approved decisions (user, 2026-11-20; source [xia report](../reports/xia-261120-0002-h9-overflow-usage-improve.md#4-decision-matrix-user-approved-2026-11-20-1a-2a-3a-4a))

- **1A** Better detection (Pi patterns, omp 429/402 veto and 413 split, silent overflow with Pi thresholds), a distinct cycle cause and clear user text, no retry, no model switch. Leave the H10 seam.
- **2A** Totals show the known sum and a partial flag when an attempt lacks usage or a model has no price. Fix the silent zero of `pickTier`. Totals honor `CostUnknown`. Refined by the user (2026-11-20): a failed attempt that streamed no output and reported no usage (for example an HTTP 429 or 500 before any stream) counts as zero tokens and zero cost and keeps the total complete, because the provider does not bill a refused request. Any other attempt with no usage still makes the total partial.
- **3A** No new entry type. `AttemptSettled.Usage` is the single source. One shared fold. Message usage is display only. H-RETRY-10 is met by the existing entries plus tests; this roadmap wording change is deliberate.
- **4A** Context estimate = last valid usage + chars/4 of later messages; one number for the footer and the H10 trigger; no tokenizer.

## Planner defaults (approved by the user, 2026-11-20)

1. Cause word `context_overflow` on reason `error`. Rejected alternative: a new reason, which changes `stopReason` and every reason switch. The cause is redundant with `Code` for an explicit overflow (verified `lifecycle.go:133-135`).
2. Silent overflow keeps the reply in the log and in model history, and the attempt stays `completed`. It does not become a `StopError`, because `finishAttempt` would blank the text (`loop_stream.go:171-174`). Risk: a row whose `contextWindow` is below the real window turns large good replies into overflow errors. The user corrects the row with a `models.json` override.
3. `ask -p`: exit 1 for both forms. Silent overflow prints the reply on stdout and the text on stderr. Texts: see phase 5. JSON mode keeps exit 0.
4. Silent-overflow prompt sum = `Input + CacheRead + CacheWrite` (omp sum). Pi leaves out cache writes, so it misses cache-write-heavy prompts. Thresholds are Pi's.
5. 413: byte wording or no body = `INVALID_REQUEST`; only token wording = overflow. The Cerebras bare-413 rule is not taken.
6. Price scope (H4 owns pricing): also price the Anthropic wire, treat a row with no tiers as unknown (`PriceKnown`), and give faux rows a free tier. Without the Anthropic part, 2A cannot hold: a priced Anthropic-wire row would report $0 as complete.
7. `AttemptSettled.costUnknown` (optional field, no new type) is stamped at settle time. Logs written before H9 count as priced.
8. Shared fold in `internal/sessions` (`usage.go`, `context_estimate.go`); totals type `protocol.UsageTotals`.
9. JSON totals: an optional `usage` object on `agent_settled` (`total`, `complete`, `costComplete`, `contextTokens`, `contextWindow`); the stream still ends with `agent_settled`.
10. ACP: `ACPUsageResult` embeds `UsageTotals` (keeps `total`, `complete`). Rows get `costUnknown`. New error kind `context_overflow`. For a silent overflow, `session/prompt` then returns that error after the reply has streamed.
11. Partial marker `≥ $0.42` (the example in the approved matrix; the `≥` glyph needs a style-guide line). Footer ` openai/gpt-5.5 ◐ high  $0.42  ▰▱▱▱▱▱▱▱ 18% `. With no known price, `↑1.2M ↓34k`. Final form: the phase 6 design review.
12. TUI design file name `tui-design.md`, because the task asks for `design.md` as the technical design.
13. Overflow patterns stay compiled Go data, not user configuration.

## Answered questions (user, 2026-11-20)

1. A failed attempt with no output and no usage counts as zero and keeps the total complete (see 2A above; phase 3).
2. H9 may price the Anthropic wire, mark a row with no tiers as unknown, and give faux rows a free tier (default 6), although H4 owns pricing.
3. The footer shows only the cost when the price is known, and tokens (`↑in ↓out`) only when no price is known. The phase 6 design review confirms the final form.

## Known limits after delivery

- A stream that dies after the provider started the reply (for example after Anthropic `message_start`) but before any content reaches the agent with no usage, because the fantasy fork emits the accumulated usage only in the finish part (`fantasykit/fold.go:283`; fork `providers/anthropic/anthropic.go:1736-1742`). The attempt then counts as a refused attempt (zero tokens, total complete), although the provider may bill the input tokens. The fix belongs in the fork: emit the accumulated usage on error parts. Source: review `plans/reports/code-reviewer-261120-0227-h9-overflow-usage-review.md`, finding 5.
- Silent overflow trusts the model row's context window (approved default 2). A row whose window is below the real limit turns a good reply into an overflow; the user corrects the row in `models.json`.

## Unresolved questions

None.
