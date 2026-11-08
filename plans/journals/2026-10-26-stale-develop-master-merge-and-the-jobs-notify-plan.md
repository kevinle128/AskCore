---
title: "Stale develop, master merge, and the jobs notify plan"
date: 2026-10-26
summary: "Local develop lagged origin by the whole TUI work; rebased and merged master, then planned routing job notices through Agent.Notify"
---

# Stale develop, master merge, and the jobs notify plan

## What happened

- Local `develop` had not pulled since 2026-10-08. PR #2 (TUI, leader, provider setup, 12 session tools) was merged on origin on 2026-10-13. A session on 2026-10-24 built the async task runtime (`internal/tasks`, `Agent.Notify`) on the stale branch. Answers about "only one tool" and "no TUI" in this session were wrong for that reason.
- Rebased the 8 local commits onto `origin/develop`; conflicts in `.golangci.yml`, `internal/agent/{agent,types}.go`, `internal/app/agent_native.go`, docs. `dispose()` order is now: close the task registry, then `Release` (job table), then the session log.
- Merged `origin/master` (v0.1.0, CI workflow, Linux fixes) into `develop` as `ee53317`. Only doc conflicts; `CLAUDE.md` is now `@AGENTS.md`.
- Found two background-work systems: `internal/jobs` (H5) delivers through `FollowUp`/`Steer`/`Remove`; `internal/tasks` (ADR 0004) through `Agent.Notify`. ADR 0004 was written without knowing H5 existed.
- Verified defects: job notices lost when the human queue is full (`queue.go:316`) and on Abort (`agent.go:231`). The reset case turned out to match Claude Code `/clear`, so it became a decision, not a defect.
- `internal/jobs` already meets the no-polling goal (`cmd.Wait`, channel waits). The real polling is in the callers: `promptAll` (5 ms) and ACP `execute` (2 ms) on `ErrBusy`.
- Compared Claude Code and Grok with GitNexus. Claude Code injects shell completions at the next model request (`query.ts:1550-1570`); Grok defers them to the end of the turn. `claude -p` waits for background work with a `sleep(100)` loop; Grok waits by events with a 600 s ceiling. Claude Code `--max-turns` resets per query (`query.ts:276`), so it does not bound the wake chain.

## Decision

Recorded in `plans/reports/brainstorm-261026-1357-jobs-tasks-unification.md` and the plan: option A (H5 surface, one port), S1 next-turn delivery, S2 print-mode wait with per-command limits, S3 reset scoping, S4 master first, D-R7 defer notices after Abort, D-R5b no total ceiling, `--max-turns` over the whole run.

## Lessons

- Check branch freshness against origin before claiming a feature is missing (saved to memory).
- Subagent reports needed checking: two of them made wrong claims (coalescing "automatic", cassettes "must be re-recorded" for the notice shape) that would have skewed the options.
- `Agent.Notify` accepts a `UserMessage`, so job notices keep their exact bytes; no `CustomMessage` wrapper is needed.

## Next steps

- Push `develop` (`a48d59d`) so CI runs on Ubuntu.
- Execute `plans/261026-1519-jobs-notify-unification/plan.md` phase by phase; ask the two phase-4 open items after its scout.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
