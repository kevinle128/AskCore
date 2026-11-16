---
phase: 1
title: "Stall timer and notice in the job table"
status: completed
effort: 4h
---

# Phase 1: Stall timer and notice in the job table

## Context

- `Options` and `BackgroundLimit`: `internal/jobs/table.go:102-145`. `BackgroundLimit` zero means off (`table.go:121-123`).
- The three sites where a bash job becomes a background job. Each calls `armLimit`: `Start` with `!spec.Foreground` (`table.go:367`), `Wait` with budget <= 0 (`table.go:645`), `Wait` when the budget expires (`table.go:679`). `StartMonitor` never goes through these sites.
- `armLimit` (arm, then store the stop func under `t.mu`, stop at once if the job finished): `table.go:373-385`. `own` stops the limit timer after the end: `table.go:568-577`. `own` runs after `proc.Done()`, which closes only after the pipe readers stopped (`internal/sandbox/README.md:29`), so no `Observe` call runs after `own` starts.
- Write path: `sandbox.Process.ingest` holds `writeMu` and calls `Spec.Observe` after the buffer write (`internal/sandbox/process.go:249-258`). `Observe` must not block (`process.go:60-62`). Today only monitors set it (`table.go:446-449`).
- `job_output wait` registers a waiter (`internal/jobs/read.go:60-66`).
- Notice path: `reminderMessage` (`notice.go:29`), `sendNotice` (`notice.go:82-98`), `refused` mutes on an expected drop (`notice.go:102-110`). Limit notice text: `notice.go:72-75`.
- Monitor cap from the background limit: `internal/jobs/monitor.go:66-71`.
- Protocol parser: `pkg/protocol/job_notice.go:26-54`; text sync test: `internal/jobs/notice_parse_test.go:14-34`.
- Test support: `fakeclock_test.go` (`Advance`, `Pending`), rig options (`helpers_test.go:178-186`, `limit_test.go:15-17`), fifo for output on demand (`helpers_test.go:275-295`), `goleak.VerifyTestMain` (`main_test.go:9-11`).

## Requirements

1. `Options.StallAfter time.Duration`. Zero or less turns the stall notice off. The jobs layer has no default, as `BackgroundLimit` has none; the composition sets 5 min (phase 2). Existing fakeClock tests that count `Pending()` stay unchanged.
2. `BackgroundLimit.BashOnly bool`. When true, `Default` does not cap monitors (`monitor.go:66`). False keeps the print-mode behavior, so this phase changes nothing on its own.
3. Only a bash job in the background gets a stall timer: armed at the three sites above, never by `StartMonitor`.
4. Silence counts from the later of the move to the background and the last output chunk. Output during the foreground wait does not count, so the text "printed no output for N" is always true.
5. One notice for each silence period. After a notice, no timer is armed until new output arrives.
6. No notice when the job finished, `killedByModel`, `muted`, `t.closing`, or `j.waiters > 0` (a `job_output wait` caller watches it). In the last case the timer re-arms for a full N.
7. The stall notice is a non-terminal notice, like a monitor event. It does not set `noticeStarted` or `awaited` and does not change `busy`/`idle`. The completion notice still follows, so "exactly one notice when the task ends" (`docs/async-task-runtime.md:17`) still holds.
8. Text, exact:
   `Background command "<label>" (job <id>) printed no output for <dur>. It still runs. Read it with job_output, stop it with job_kill, or leave it running.`
   `<label>` uses `%q`, as the limit notice does. `<dur>`: whole minutes as `1 minute` / `N minutes`; else whole seconds as `1 second` / `N seconds`; else `time.Duration.String()` (for example `200ms`, used by tests).

## Design

One lazy timer for each background bash job. No timer reset for each chunk. No new goroutine.

State on `Job` (no new type):

- `lastOut atomic.Int64`: monotonic nanoseconds since `j.started` of the last chunk (`t.clock.Now().Sub(j.started)`; `Sub` uses the monotonic reading of the system clock).
- `stall atomic.Int32`: `stallOff` (not in the background), `stallArmed`, `stallQuiet` (notice sent, no timer).
- `stopStall atomic.Pointer[func() bool]`: the stop func of the one armed timer.
- `bgSince int64` (under `t.mu`): monotonic nanoseconds since start of the move to the background.

Functions (all unexported):

- `noteOutput(Stream, []byte)`, set as `spec.Observe` in `launch` (`table.go:446`) only for a bash job and only when `t.stallAfter > 0`. Hot path: one `lastOut.Store`. Rare path: when `stall.CompareAndSwap(stallQuiet, stallArmed)` succeeds (the first chunk after a notice), it calls `armStall(j, t.stallAfter)`. `clock.AfterFunc` does not block, so it is safe under `writeMu`. It takes no lock and keeps no chunk.
- `armStall(j, d)`: `stop := t.clock.AfterFunc(d, func() { t.stallFire(j) })`; `j.stopStall.Store(&stop)`; then if `j.done` is closed, call `stop()`. With the `close(done)`-then-load order in `own`, either `own` sees the new stop func or `armStall` sees `done` closed. No timer outlives the job.
- `armBackground(j, limit)` replaces the three `armLimit` calls: `armLimit(j, limit)` unchanged, then, for a bash job with `t.stallAfter > 0`, set `bgSince` under `t.mu` and, if `stall.CompareAndSwap(stallOff, stallArmed)`, `armStall(j, t.stallAfter)`. A job that already has a limit timer gets no extra limit work.
- `stallFire(j)`: under `t.mu`, return when finished, `killedByModel`, `muted`, or `closing`. `quiet = now - max(bgSince, lastOut)`. Only the caller that wins a `stall` CompareAndSwap arms a timer, so one job never has two stall timers. If `waiters > 0`, re-arm N. If `quiet < N`, re-arm `N - quiet`. Else `stall.Store(stallQuiet)`, read `lastOut` once more, and if it moved, `CompareAndSwap(stallQuiet, stallArmed)` and re-arm instead of sending (no lost re-arm). Else build the text, unlock, and send through `deliver`.
- `deliver(j, what, text)`: extracted from `sendNotice` (`notice.go:91-97`): owner nil check, `Owner.Notify(t.reminderMessage(text))`, `refused` on error. `sendNotice` and `stallFire` both call it. No second send path.
- `own`: after `close(j.done)`, load `stopStall` and call it, next to `stopLimit` (`table.go:575-577`).
- `stallText(j)` in `notice.go`, next to `noticeLocked`.

## Files

- Modify: `internal/jobs/table.go` (two option fields, `Table.stallAfter`, `Job` fields, `launch`, `Start`, `Wait`, `own`, `armBackground`, `armStall`, `stallFire`).
- Modify: `internal/jobs/notice.go` (`stallText`, `deliver`).
- Modify: `internal/jobs/monitor.go` (line 66: `if limit := t.limit.Default; limit > 0 && !t.limit.BashOnly`).
- Modify: `pkg/protocol/job_notice.go`: extend `ParseJobNotice` and `JobNotice` (no new exported symbol). The stall text gives `JobNotice{JobID, Kind: "bash", Label, Status: "running", Detail: "no output for <dur>"}`. The limit text (`notice.go:73`) gives `JobNotice{JobID, Kind: "bash", Label, Status: "killed", Detail: "background time limit"}`. Both texts write the label with `%q`, so both read it with `strconv.Unquote`. Update the `Status` comment (`job_notice.go:32`) to add `running`.
- Modify: `pkg/protocol/job_notice_test.go`, `internal/jobs/notice_parse_test.go`, `internal/jobs/limit_test.go`.
- Create: `internal/jobs/stall_test.go`.
- Modify: `internal/jobs/README.md` (section "Background limit and quiescence", lines 35-40; notices, lines 48-53): stall notice, `StallAfter`, `BashOnly`.
- Modify: `pkg/protocol/README.md`: add the row for `job_notice.go` if it has none (the file list at line 16 names `reminder.go` only).

## Steps

1. Add the two option fields with doc comments; copy `StallAfter` into `Table` in `New`.
2. Add the `Job` fields and `noteOutput`; set `Observe` in `launch` only for a bash job with `stallAfter > 0`.
3. Extract `deliver` from `sendNotice`.
4. Add `armBackground`, `armStall`, `stallFire`, `stallText`; replace the three `armLimit` calls.
5. Stop the stall timer in `own`.
6. Gate the monitor cap with `BashOnly`.
7. Extend `ParseJobNotice`; add the text sync cases.
8. Update `internal/jobs/README.md` and `pkg/protocol/README.md`.

## Tests

Use `newRig(t, withFakeClock(), withOptions(func(o *Options) { o.StallAfter = time.Minute }))` and a fifo command so the process stays alive and prints on demand.

Behavior:

- `TestStallNoticeAfterSilence`: background start, `Advance(time.Minute)`, one follow-up with the exact text; job still running.
- `TestStallNoticeOncePerSilencePeriod`: after the notice, `Advance(10*time.Minute)` gives no second notice and `Pending() == 0`.
- `TestOutputStartsNewSilencePeriod`: a fifo line before N gives no notice at N, a notice at last output + N. After a notice, a new line and N more give a second notice.
- `TestForegroundJobInBudgetGetsNoStallNotice`: foreground `Wait` with budget 5 min, N = 1 min; `Advance(4*time.Minute)` gives no notice; the notice comes N after the move.
- `TestNoStallNoticeWhileJobOutputWaits`: a `Read` with `wait` runs at N; no notice; the notice comes N after the read returns.
- `TestMonitorGetsNoStallNotice`, `TestNoStallNoticeAfterKill`, `TestDisabledBackgroundGetsNoStallNotice`.
- `TestStallRefusalMutesJob`: `generation_test.go:125` style; an expected drop mutes and is not reported.
- `TestStallNoticeThenCompletionNotice`: exactly one completion notice follows a stall notice.
- `TestNoticeTextParsesInTheProtocol`: add the stall and limit cases. `pkg/protocol` table tests with a label that contains `"` and `)`.
- `TestMonitorDeadlineCappedByBackgroundLimit` (`limit_test.go:145`): keep the case; add `BashOnly: true` with no cap.

Performance and lifetime (see "Performance" in `plan.md`):

- `TestNoteOutputDoesNotAllocate`: `testing.AllocsPerRun(1000, func() { j.noteOutput(sandbox.Stdout, chunk) }) == 0` on an armed job.
- `BenchmarkNoteOutput` with `b.ReportAllocs()`.
- `TestChattyJobKeepsOneStallTimer`: 100 fifo lines with `Advance(time.Second)` between them; `Pending() == 1` after each line and after each fire that re-arms.
- `TestStallTimerReleasedAtEnd`: the job ends; `Pending() == 0`. Same after `Kill` and after `Close`.
- `TestNoStallWorkWhenOff`: `StallAfter` 0; `Pending() == 0` after the move; after fifo output `j.lastOut.Load() == 0`, which proves that no observe hook ran (same-package test, no new export).
- goleak in `TestMain` already proves that no goroutine stays after the tests.

```bash
go test -race -run 'Stall|NoteOutput|NoticeText|MonitorDeadline' ./internal/jobs/
go test -run '^$' -bench 'NoteOutput' -benchmem ./internal/jobs/
go test -race ./internal/jobs/ ./pkg/protocol/ ./internal/sandbox/
go vet ./internal/jobs/ ./pkg/protocol/
```

## Risks

| Risk | L x I | Mitigation |
|------|-------|-----------|
| Lock order inversion if `Observe` takes `t.mu` | M x H | `noteOutput` takes no lock. All tests run with `-race`. |
| A lost re-arm leaves a job without later notices | M x M | Second read of `lastOut` after `stallQuiet`; `TestOutputStartsNewSilencePeriod`. |
| A timer outlives the job | L x L | Double check of `done` in `armStall`, stop in `own`; `TestStallTimerReleasedAtEnd`. |

## Rollback

Revert the commit. No data or file format changes.
