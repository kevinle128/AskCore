---
title: "Jobs deliver notices through the Agent notify port"
description: "Route every shell job notice through Agent.Notify, scope job notices and visibility to the conversation that started the job, remove the busy-retry polling, and make ask -p wait for background work."
status: completed
priority: P1
effort: 8d
branch: develop
tags: [backend, refactor, agent, jobs]
blockedBy: []
blocks: []
created: 2026-10-26
---

# Jobs deliver notices through the Agent notify port

## Overview

Two systems deliver background-work notices to the Agent today. `internal/jobs` (the job table behind `bash`, `job_output`, `job_kill`, `job_list`, `monitor`) uses `Agent.FollowUp`, `Agent.Steer` and `Agent.Remove`. `internal/tasks` uses `Agent.Notify`. This plan makes `Agent.Notify` the only delivery port and keeps the H5 tool surface.

The analysis, the verified defects, the reference harnesses (Claude Code, Grok CLI) and every decision are in [the decision report](../reports/brainstorm-261026-1357-jobs-tasks-unification.md). This plan does not repeat them.

## Decisions this plan implements

| ID | Decision (user, 2026-10-26) |
|---|---|
| A | Keep the H5 surface (tools, `<system-reminder>` text, monitor, move to background at the wait budget). Jobs deliver through the Agent notify port as its first outside source. |
| S1 | A completion notice enters at the next turn boundary while a run is active (`Notify` policy `Wake`), as Claude Code. Monitor events use the same path. On Abort, queued notices move to the context queue. |
| S2 | `ask -p` waits for background work by signal, with per-command limits as Claude Code: 10 min default, up to 2 h, plus an idle-wait cap. A command at its limit stops and its notice wakes one more turn. TUI and leader keep no background limit (H5 Q9). |
| S3 | A session reset is a new session for jobs. Old jobs keep running. A job carries the generation of the conversation that started it; its notices are refused after a reset, and the new conversation does not see it in `job_list`, `job_output` or `job_kill`. Old jobs count toward the cap of 10 and end when the session closes. |
| S4 | `origin/master` is merged into `develop` (done, `ee53317`). |
| D-R7 | After Abort, a new notice waits for the next human input and starts no run, as Grok (red-team decision). |
| D-R5b | No total wall-clock ceiling for `ask -p`, as Claude Code (red-team decision). |
| P4-1 | The background limit uses the existing `timeout`, as Claude Code: with `run_in_background: true` in `ask -p`, `timeout` sets the background limit (default 10 min, up to 2 h); a foreground `timeout` stays the wait budget (H5 Q9); a command moved to the background gets the default limit from the move; no limit in the TUI and the leader. No schema change. |
| P4-2 | In `ask -p` every monitor, `persistent` included, has a deadline of at most 10 min (`min(timeout_ms, 10 min)`), as Claude Code; at the deadline it stops with one notice that wakes a turn. The TUI and the leader keep H5 N3. |
| MT | `ask -p --max-turns N` counts every turn of the whole run, woken turns included; at N it stops background work and exits 1 with `Error: Reached max turns (N)`. Claude Code counts per query, which does not bound the wake chain. `--max-budget-usd` is a later plan. |

## Design findings that shape the plan (verified)

- `Agent.Notify` accepts any message except nil and a `CustomMessage` without a type (`internal/agent/tasks.go:149-165`). A job notice keeps its current `UserMessage` with `<system-reminder>` text. The TUI, print mode, ACP and the cassettes see no shape change, and H5 Q14 stays fully intact. The report's earlier idea of a `CustomMessage` wrapper is not needed.
- `tasks.Notice.Origin` exists for outside sources (`internal/tasks/types.go:100-106`).
- `Agent.Remove` removes a queued input by ID from the context, steering and follow-up queues, any origin (`internal/agent/queue.go:99-109`). Monitor coalescing keeps working.
- Each run takes the task registry of the current generation once (`internal/agent/agent.go:607`), and `Reset` needs an idle Agent. So every tool call of a run belongs to one generation, and a port captured per run gives a job the generation of its start.
- `internal/jobs` may import only the standard library, `internal/sandbox` and `pkg/protocol` (`internal/jobs/README.md:79-82`). The port that jobs use is an interface owned by `jobs`; the `bash` and job tools adapt the Agent port to it.

## Phases

| # | Phase | Status | Effort | Depends on |
|---|---|---|---|---|
| 1 | [Agent notice port per generation](./phase-01-agent-notice-port.md) | completed | 1d | — |
| 2 | [Jobs deliver through the port and scope by generation](./phase-02-jobs-through-port.md) | completed | 2d | 1 |
| 3 | [Remove the busy-retry polling](./phase-03-remove-busy-polling.md) | completed | 1d | 2 |
| 4 | [Print mode waits for background work](./phase-04-print-mode-background-wait.md) | completed | 3.5d | 2, 3 |
| 5 | [Design records and docs](./phase-05-design-records-and-docs.md) | completed | 0.5d | 1-4 |

Deep mode: phase 1 is detailed. Phases 2 to 5 list goals, files, known facts and the test matrix; each phase gets its own scout pass before it starts, and its open items are asked one at a time then.

## Success criteria

- [x] No production code calls `Agent.FollowUp`, `Agent.Steer` or `Agent.Remove` for a job notice; `jobs.Notifier` and `Table.SetNotifier` are gone.
- [x] A job notice is delivered when the human queue is full, and survives Abort in the context queue.
- [x] After a reset, a job of the old conversation sends no notice and is invisible to `job_list`, `job_output` and `job_kill`.
- [x] `cmd/tui/headless.go` and `internal/acp/host.go` have no sleep or retry loop on `ErrBusy`.
- [x] `ask -p` waits for running background jobs without polling, answers once per completion, stops a command at its limit and wakes one more turn.
- [x] `ask -p --max-turns N` ends the whole run after N turns, woken turns included, with exit code 1.
- [x] Monitor event text is escaped, so job output cannot close the `<system-reminder>` or `<monitor-event>` structure.
- [x] After phases 1 to 3 the cassettes replay unchanged (completion notice bytes unchanged). Phase 4 changes the `bash` declaration and re-records the six cassettes once.
- [x] SIGINT and SIGTERM end `ask -p` at once with 130 and 143 at any point, the background wait included.
- [x] `go test ./...`, `go test -race` on the touched packages, and golangci-lint pass locally and in the GitHub CI of `develop`. Local: passes (load flakes noted in the phase results). CI: passed on draft PR #5 (run 40382072420, `ubuntu-latest`, 2026-10-27).
- [x] ADR, design doc, H5 decision record, package READMEs and `CHANGELOG.md` match the code.

## Test commands (every phase)

```sh
go test -race -count=1 ./internal/agent/... ./internal/tasks/... ./internal/jobs/... ./internal/tools/...
go test -count=1 ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

Tests run the real code path (the Agent API, `ask -p`, the leader); no test-only harness bypasses the entry point. No test uses `time.Sleep` to synchronize.

## Risks

| Risk | Mitigation |
|---|---|
| S1 changes when a completion notice arrives during a run | Phase 2 expects no test to change and records the reason if one does |
| Job output reaches the model as a user message (prompt injection) | Monitor body escaped (phase 2); completion notice text is built by the table, not by job output |
| `ask -p` exits before a completion's extra turn (flaky) | Hand-off invariant and a gated-owner test (phase 4) |
| Old jobs fill the cap after a reset | Generation-aware refusal and removal of finished old jobs (phase 2) |
| A port captured by a long-lived job outlives its generation | `Notify` and `Remove` check the generation; a stale port is refused (phase 1 tests) |
| Hidden old jobs keep running with no way to stop them before the session closes | Accepted in S3; `Release` kills them at session close; they count toward the cap |
| The print-mode wait hangs CI | Print limit on every job kind, idle-wait cap, signals honored during the wait, kill switch (phase 4) |
| Two open items (limit parameter name, persistent monitors in `ask -p`) | Asked at the start of phase 4, one at a time |

## Rollback

Each phase is one or more focused commits on `develop`. Revert the commits of a phase in reverse order. Phase 1 is additive; phases 2 to 4 revert to the `FollowUp`/`Steer` path and the N2 kill-at-end behavior.

## Red-team review

Three reviewers (Security Adversary, Assumption Destroyer, Failure Mode Analyst) raised 24 findings, deduplicated to 15. The user approved the 13 accepted findings on 2026-10-26; they are applied in the phase files. One was rejected (new UI at reset). See [the red-team report](../reports/red-team-261026-1537-jobs-notify-plan.md).

## Open items

- None.

## Validation Log

### Session 1 (2026-10-26)

Questions were asked one at a time during planning, after each piece of evidence, not as a separate batch.

| Topic | Decision | Source |
|---|---|---|
| Unification option | A: H5 surface, one delivery port | User |
| Completion timing while busy | S1 = A: next turn boundary, as Claude Code | User, after the Claude Code and Grok comparison |
| `ask -p` end | S2 = B: wait, per-command limits, as Claude Code | User, after the comparison |
| Session reset | S3: old jobs run on, their notices are refused, they are hidden from the new conversation | User |
| Order of work | S4 = A: merge `origin/master` first (done, `ee53317`) | User |
| Notices after Abort | D-R7 = A: wait for the next human input, as Grok | User, from the red team |
| Total ceiling | D-R5b: none, as Claude Code | User, from the red team |
| `--max-turns` | Counts the whole run, woken turns included | User, after Claude Code was found to count per query |

Deferred on purpose (deep mode, asked at phase start after the scout): the background-limit parameter and its mode visibility, and `persistent` monitors in `ask -p`.

### Verification Results

- The red team checked the plan's file:line claims against `ee53317` (most matched; the exceptions are the findings applied above). The orchestrator re-read the key lines of every accepted finding.
- Extra check in this session: `ErrStaleNotice` and the disposed-notice error both wrap `tasks.ErrNotDelivered` (`internal/agent/tasks.go:17,21`), which phases 1 and 2 rely on.
- Failed: 0 open.

### Whole-Plan Consistency Sweep

- Searched all plan files for superseded terms (`Agent.Notices()` as public API, the `origin` port parameter, generation 0 as a default, the `CustomMessage` wrapper, "without re-recording", old efforts). Only current-state descriptions remain.
- Efforts reconciled: 1d + 2d + 1d + 3.5d + 0.5d = 8d (front matter and phase table).
- No unresolved contradiction.

Recommendation: proceed to phase 1.
