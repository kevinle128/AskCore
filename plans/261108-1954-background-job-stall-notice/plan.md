---
title: "Background job stall notice and interactive background limit"
description: "Send one notice when a background job prints nothing for 5 minutes, and stop interactive background jobs after 2 hours."
status: completed
priority: P2
effort: 1d
branch: develop
tags: [feature, backend, jobs, tui]
mode: fast
blockedBy: []
blocks: []
created: 2026-11-08
---

# Background job stall notice and interactive background limit

## Overview

A background `bash` job that hangs never ends, so no completion notice reaches the model. In the TUI, the leader, and `ask acp` the table has no background limit (`internal/jobs/table.go:121-145`, `cmd/tui/args.go:58-60`), so the session waits silently forever.

This plan adds two things:

1. A stall notice. When a background `bash` job prints no output for `StallAfter` (default 5 min, `ASK_BG_STALL_MS`, `0` off), the table sends one `<system-reminder>` through the existing notice path. The job continues. New output starts a new silence period.
2. A hard limit for interactive modes: `BackgroundLimit{Default: 2h, Max: 2h, BashOnly: true}`, with `ASK_BG_LIMIT_MS` (`0` off). `ask -p` keeps 10 min / 2 h and its monitor cap.

## Outcome and acceptance

- A background `bash` job with no output for N gets exactly one stall notice for each silence period, in all four modes. The job is not stopped.
- No stall notice for: a foreground job inside its wait budget, a job that a `job_output wait` call watches, a monitor, a muted job (reset conversation), a finished or killed job, a closing table, `ASK_DISABLE_BACKGROUND_TASKS=1`, `ASK_BG_STALL_MS=0`.
- Interactive sessions stop a background `bash` job after 2 h with the existing limit notice. Interactive monitors keep their own timeouts.
- The TUI draws a job line for the stall notice (the job stays running) and for the limit notice (the job leaves the running list).
- The performance tests in phase 1 pass (zero allocations on the output path, one pending timer, no timer after the end).
- `go test ./...`, `go vet ./...`, golangci-lint (depguard), and the reference docs drift test pass.

## Phases

| Phase | Name | Status | Effort |
|-------|------|--------|--------|
| 1 | [Stall timer and notice in the job table](./phase-01-job-table-stall-notice.md) | Completed | 4h |
| 2 | [Wire both settings in every mode](./phase-02-mode-wiring-and-tool-text.md) | Pending | 2h |
| 3 | [TUI job lines and user docs](./phase-03-tui-lines-and-docs.md) | Pending | 2h |

## Dependencies

Phase 2 needs `jobs.Options.StallAfter` and `BackgroundLimit.BashOnly` from phase 1. Phase 3 needs the extended `protocol.ParseJobNotice` from phase 1 and the env names from phase 2. The phases run in sequence. No two phases edit the same file.

## Data flow

Process pipe -> `sandbox.Process.ingest` (`internal/sandbox/process.go:249-258`) -> `Spec.Observe` of a bash job -> one atomic store on `Job`. Move to background (`table.go:367`, `:645`, `:679`) -> one stall timer (`clock.AfterFunc`). Timer fire -> check under `t.mu` -> `deliver` -> `Owner.Notify(reminderMessage(text))` (`notice.go:29`, `:95`) -> `Agent.Notify` (`internal/agent/tasks.go:100`) -> a run. The TUI reads the same text with `protocol.ParseJobNotice` (`internal/tui/translate_jobs.go:24`).

## Architecture check

Sources: `docs/ask-architecture-reference.md` (sections 3, 8, 9), `.golangci.yml` depguard, `internal/README.md`, the owning READMEs, `docs/async-task-runtime.md`.

| Item | Owner | Why there | Imports used | Pattern reused |
|------|-------|-----------|--------------|----------------|
| `jobs.Options.StallAfter` (exported field) | `internal/jobs` | "A new job or process rule" goes to `internal/jobs` (arch ref line 395); the table owns notices (line 100). Exported because `cmd/tui` sets it. | stdlib `time` | `Options.BackgroundLimit`: zero = off, value set by the composition |
| `jobs.BackgroundLimit.BashOnly` (exported field) | `internal/jobs` | The monitor cap lives in `monitor.go:66`; the flag must be set by `cmd/tui`. Extends an existing struct; no new type. | none | `BackgroundLimit.Max` |
| `Job` fields `lastOut`, `stall`, `stopStall`, `bgSince`; funcs `noteOutput`, `armBackground`, `armStall`, `stallFire`, `stallText`, `deliver` (unexported) | `internal/jobs` | Per-job state already lives on `Job` (`table.go:200-218`). | stdlib `sync/atomic`, `time`; `internal/sandbox` (`Clock`, `Stream`); `pkg/protocol` (reminder) — the strict depguard list `jobs-minimal-imports` (`.golangci.yml:104-112`) | `armLimit`/`stopLimit` (`table.go:373-385`, `:568-577`); `Spec.Observe` as monitors use it (`table.go:446-449`); `sendNotice` and `refused` (`notice.go:82-110`); monitor events as the precedent for a non-terminal notice |
| Stall and limit text in `protocol.ParseJobNotice`, `JobNotice.Status` value `running` | `pkg/protocol` | The TUI may import only `pkg/protocol` (depguard `tui-client-boundary`, `.golangci.yml:28-37`); the completion notice is built in `jobs` and parsed in `pkg/protocol` today (`job_notice.go:26-54`). No new exported symbol. | stdlib `regexp`, `strconv`, `strings` | `jobNoticeRE` + `ParseJobNotice`; text sync test `internal/jobs/notice_parse_test.go` |
| `running` check in `Translator.reminder` | `internal/tui` | Job lines are drawn in `translate_jobs.go` (`internal/tui/README.md:64`, `:131`). | `pkg/protocol` | the existing `ParseJobNotice` branch |
| Env constants `ASK_BG_STALL_MS`, `ASK_BG_LIMIT_MS`; `envMillis` (rename of `printWaitCeiling`) | `cmd/tui` | `cmd/tui/headless.go` is "the one place all entry points share" (arch ref line 390); only `cmd/*` and `internal/app` read config (line 380); env names already live in `headless_wait.go:19-28`. | stdlib `strconv`, `time` | `printWaitCeiling` body (same rules); `options.bgLimit` (`args.go:58-60`); `DisableBackground` at `headless.go:469` |
| `sessionOptions` (unexported) | `cmd/tui` | Moves the existing `options` build out of `acpParams` (`acp.go:160-166`) so a test can read it. | none new | — |

Removed or avoided second mechanisms: no new notifier (one `deliver`, extracted from `sendNotice`); no new timer service or ticker (`sandbox.Clock.AfterFunc`, as `armLimit`); no goroutine per job (only `own`, which exists); no new limit mechanism (`armLimit` unchanged); no new env parser (`printWaitCeiling` renamed and reused); no new protocol type or parse function (`ParseJobNotice` extended). `internal/app` is not changed: `acpParams` in `cmd/tui` already builds the `NewAgent` closure for the leader, the in-process TUI, and `ask acp`.

Async rules (`docs/async-task-runtime.md`): the stall notice is a non-terminal notice, like a monitor event. It does not count as the one notice at the end of a job (line 17). It uses no polling (line 6, R9) and every timer has one owner (line 20): the job, which stops it in `own`.

## Performance

- Output hot path: `noteOutput` runs under the process write lock. It does one `atomic.Int64.Store` of monotonic nanoseconds since the job start (`clock.Now().Sub(started)`), and one atomic load of `stall` to see that a timer is armed. No lock, no allocation, no timer call. Only the first chunk after a stall notice arms a timer (one `AfterFunc`, at most once for each silence period).
- Timers: at most one stall timer for each running background bash job, guaranteed by a CompareAndSwap state (`off`, `armed`, `quiet`). It is armed at the move to the background and re-armed only when it fires (for the remaining silence), never on output. `own` stops it at the end, after a kill, and in `Close`; `armStall` stops a timer that it armed after the end.
- Goroutines: none new. A fire runs in the clock callback (`time.AfterFunc`), as the limit timer does. `goleak` (`internal/jobs/main_test.go`) checks this.
- `StallAfter` 0: no observe hook for bash jobs and no timer.
- Limit: interactive modes reuse `armLimit`; a job gets one limit timer as in print mode.
- Proof (phase 1): `TestNoteOutputDoesNotAllocate` (`testing.AllocsPerRun == 0`), `BenchmarkNoteOutput`, `TestChattyJobKeepsOneStallTimer` (`fakeClock.Pending() == 1`), `TestStallTimerReleasedAtEnd` (end, kill, close: `Pending() == 0`), `TestNoStallWorkWhenOff`.

## Rollback

Each phase is one commit. Revert phase 3, then 2, then 1. Phase 1 alone changes no behavior: `StallAfter` zero means off, and `BashOnly` false keeps the monitor cap of print mode. A revert of phase 2 alone puts every mode back to the old limits and turns the stall notice off. A revert of phase 3 alone leaves the stall and limit notices hidden in the TUI.

## Decisions

Accepted (orchestrator defaults; the user can override): interactive monitors keep their own timeouts through `BashOnly`; interactive Default and Max are 2 h; `ASK_BG_LIMIT_MS=0` turns the interactive limit off, unlike `ASK_PRINT_BG_LIMIT_MS`, which ignores values <= 0; a job watched by a `job_output wait` call gets no stall notice and the timer re-arms; the TUI fix for the limit notice stays in this plan.

## Open questions

None.
