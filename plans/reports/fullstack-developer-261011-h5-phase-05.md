# H5 phase 5: job table, notices and monitor

Status: completed. Package `internal/jobs` (unix only, imports `sandbox` and `pkg/protocol`).

## Verification

- `go test ./internal/jobs ./internal/sandbox -race -count=3`: pass. `go test ./internal/jobs -race -count=5`: pass.
- `go vet ./internal/jobs/...` clean. golangci-lint on `./internal/jobs/...`: 0 issues.
- `ps` after the runs: no stray `sleep` or `cat` processes. Every process test checks `kill(-pgid, 0) == ESRCH` in cleanup. `goleak.VerifyTestMain` is on.
- Every test of the phase "Tests After" table exists. Notice tests live in `table_test.go`; there is no separate `notice_test.go`.

## Files (all new, in `internal/jobs`)

`doc.go`, `table.go`, `read.go`, `notice.go`, `monitor.go`, and tests `main_test.go`, `fakeclock_test.go`, `helpers_test.go`, `table_test.go`, `read_test.go`, `monitor_test.go`. README.md is untouched (phase 10).

## Exported API for phases 6 and 7

- `New(Options{Clock, Env, MaxActive, KillGrace, OnError}) *Table`; the zero value is valid.
- `Table`: `SetNotifier(Notifier)`, `Start(StartSpec{Kind, Command, Cwd}) (*Job, error)`, `StartMonitor(MonitorSpec{Command, Cwd, Description, Timeout, Persistent}) (*Job, error)`, `Wait(ctx, id, budget) WaitOutcome`, `Read(ctx, id, wait, timeout) (ReadResult, error)`, `Kill(id, reason) (*Job, error)`, `List() []JobInfo`, `Close() error`, `KillRemaining()`.
- `Notifier{FollowUp, Steer func(protocol.Message) (string, error); Remove func(string) bool}`.
- `WaitOutcome`: `Done`, `Background`, `Cancelled`, `NotFound`.
- Errors: `ErrClosed`, `ErrTooManyJobs` (wrapped; text is exactly `Too many active jobs (10). Stop one with job_kill or wait for one to finish.`), `ErrUnknownJob`, `ErrNoNotifier`.
- `Job` (stays valid after the table removed it): `ID()`, `Kind()`, `Label()`, `Status()`, `StatusLine()`, `KillReason()`, `Done()`, `Snapshot() sandbox.Output`, `Result() sandbox.Result`, `Info() JobInfo`.
- `ReadResult{JobID, Output, Skipped, SpillPath, Status, Running, ExitCode, Signal, KillReason, StatusLine, Text}`; `Text` is the full model text (note at the gap, status line, "do not poll" line for a running job). `Output` is the raw bytes, so phase 6 can apply `sandbox.View` itself.
- `JobInfo{ID, Kind, Status, Label, Elapsed, ExitCode, Signal, KillReason}`; `Kind` = `KindBash`/`KindMonitor`; `Status` = `StatusRunning|Stopping|Completed|Killed|Failed`.

## Deviations from the phase text (additive)

- `Read` and `Kill` return an error (`ErrUnknownJob`) because the phase had no way to signal an unknown id. Phase 6 maps it to `No job with id <id>. Use job_list.`
- `Wait` has a fourth outcome `NotFound`. Output and result after `Wait` come from the `*Job` that `Start` returned, not from the outcome.
- `Kill` removes the job and returns it; with no reason the reason is `job_kill`. Kill on an already finished job only removes it.
- `Options.KillGrace` cannot reach `sandbox` (its grace is an unexported 3 s constant). The table uses it for the shared `Close` grace: SIGTERM to all at once, then `KillNow` for what is left after `KillGrace`.
- `KillRemaining` also sets the closing state (no notice, `Start` returns `ErrClosed`). Otherwise the owner goroutines would send notices to an Agent that is already disposed.
- A non-persistent monitor with `Timeout <= 0` gets the 10 h default in the table (the tool in phase 6 still validates).
- Blank stdout lines are not events. The last unterminated stdout line at exit is an event. Events are not sent after `job_kill` or while closing.

## Behavior notes

- Waiter rule: waiters register under the table mutex; the owner goroutine sets `awaited` under the same mutex; the budget path re-checks for the end under the mutex, so a job that ends at the budget is reported once (200-run race test).
- Status: exit code present means `completed` (also non-zero); a table or model kill reason means `killed`; an unsent signal or wait error means `failed`.
- Flood guard: lazy refill on the injected clock. With one line per 250 ms the bucket gives 11 delivered events, not 10, because the refill at 2 s falls before event 8. The test asserts 11 (events 0 to 10), then 4 suppressed, then the note after +2 s. The violation counts as continuous while drops come less than one refill interval apart, so a sustained flood reaches the 30 s kill although a refill delivers one event every 2 s.
- Events batch with `html.EscapeString` for the description attribute; line content is not escaped.
- The mutex is never held while calling the Notifier or waiting on a process. The spill directory has its own lock because the `SpillFile` callback runs inside the process write lock.

## Unresolved questions

- Phase 6 `monitor` description says 10 h default; the table default matches (`monitorDefaultTimeout`).
- The flood-guard constants are in one block in `monitor.go` (risk row of the phase).
