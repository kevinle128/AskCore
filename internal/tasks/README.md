# `internal/tasks`

This package runs background work for one Agent. A `Registry` owns the goroutines of its tasks, gives each task exactly one terminal state, and passes exactly one notice per task to a `Deliver` function that the Agent supplies. It uses no sleep, no ticker, and no polling. The design is in [async task runtime](../../docs/async-task-runtime.md) and [ADR 0004](../../docs/adr/0004-agent-owned-async-task-runtime.md).

## What belongs here

- The registry: start, stop, stop all, close, list, `Done`, `Idle` ([registry.go](registry.go))
- The runner and the terminal step ([runner.go](runner.go))
- The timer kind, which has no goroutine while it waits ([timer.go](timer.go))
- The output sink: a 64 KiB ring and a lazy file ([sink.go](sink.go))
- The notice message, its escaping, and its tail limits ([notice.go](notice.go))
- The removal of old output directories ([sweep.go](sweep.go))
- Types, kinds, states, and errors ([types.go](types.go))

## What does not belong here

| Code | Put it in |
|---|---|
| The queue, the claims, and the wake of a run | [internal/agent](../agent/README.md) (`Agent.Notify`) |
| The notice port of a source outside the registry (`Port`, declared in [types.go](types.go)) | Implemented by [internal/agent](../agent/README.md); a tool gets it in `tools.Context.Notices` |
| Process, HTTP, and sub-agent bodies | The package that owns that capability; it passes a `Body` to `Start` |
| The model-facing tools | [internal/tools](../tools/README.md) |
| Creating and checking the session directory | `internal/app` (composition root), through `Config.PrepareDir`, which the registry calls once at the first output so a session without output makes no directory |
| Event and message types | [pkg/protocol](../../pkg/protocol/README.md) |

## File names and imports

Allowed imports: the standard library, `pkg/protocol`, and `internal/tracing`. A strict depguard rule (`tasks-minimal-imports` in `.golangci.yml`) enforces this. Tests may also import testify and goleak. This package must not import `agent`, `tools`, `app`, or a transport package.

## Rules

**Terminal order.** The body returns and the runner closes the sink. Then, under `mu`, it sets the status, the end time, and the claim. It emits `task_end`, then calls `Deliver` once with no lock held. Then, under `mu`, it records the result of the hand-off and lowers the active count. Last, it closes `done` and the wait group is released. The registry is idle only after `Deliver` returned. `task_end` has no notice outcome: delivery is visible as the custom message in the log, and as `Info.Notice` here.

**Status.** A panic gives `failed`; the value and `debug.Stack()` go to the sink. A body that returned without an error gives `completed`, even if a `Stop`, a deadline, or a close landed while it ended: the body did its work. For a body that returned an error, the runner reads the cause of the task context. A deadline gives `timed_out`. Any other cancel (`Stop`, `StopAll`, `Close`, or a cancel of `Config.Root`) gives `stopped`. Without a cause, the error gives `failed`. A body that stops because of a cancel must return the cause as its error (`Result{Err: context.Cause(ctx)}`) to get `stopped`.

**Discard contract.** `internal/tasks` cannot import the Agent, so the refusal is a sentinel: `ErrNotDelivered`. The Agent wraps it in `ErrDisposed` and in its stale-generation error (for example `fmt.Errorf("%w: %w", tasks.ErrNotDelivered, ErrDisposed)`). When `Deliver` returns an error that matches `errors.Is(err, ErrNotDelivered)`, the registry sets `Info.Notice` to `discarded`. Any other error sets it to `failed` and stores the text in `Info.NoticeError`. The registry does not panic and does not retry. Neither case changes the task status, and neither appears in an event.

**Info.** `Info` is a copy of the task state: `ID`, `Kind`, `Title`, `Status`, `Policy`, `StartedAt`, `EndedAt`, `ExitCode`, `Summary`, `Error` (text of the body error or panic), `OutputFile` (set only if the sink created the file), `OutputError`, `Cause` (`ErrStopped`, `ErrDeadline`, or the cause given to `StopAll`/`Close`), `Notice` (`pending`, `delivered`, `silent`, `discarded`, `failed`), `InputID`, and `NoticeError`. Ended tasks stay in `List`.

**Notice policy.** `Stop` changes the policy of the task to `NoticeContext` (the caller asked for the stop), unless it is `NoticeSilent`. `StopAll(cause, policy)` sets the policy of every running task. `Close(cause)` keeps each policy, so the Agent decides at `Deliver` time. `NoticeSilent` skips `Deliver`.

**Idle.** One predicate, `active > 0`, under `mu`. A channel is made only when the count goes from 0 to 1 and closed only when it goes from 1 to 0. `Idle()` returns the current channel, or a closed channel while idle. A second start while busy returns the same channel.

**Timer.** `time.AfterFunc` holds the timer; no goroutine exists until it fires. `Start` adds to the wait group under `mu` only while the registry is open. Exactly one party ends a timer: the caller of `Stop`, `StopAll`, or `Close` if `timer.Stop()` returned true, otherwise the callback. The claim in the terminal step is a second guard.

**Events.** `Start` emits `task_start` after it released `mu`. The terminal step waits for that emit, so `task_end` never comes first. `task_update` uses one `time.AfterFunc(1s)` per task, armed by the first write after the last update. A per-task lock orders `task_update` and `task_end`, so no `task_update` follows `task_end`.

**Sink.** The file is `<Config.Dir>/<task-id>.log`, opened on the first write with `O_CREATE|O_EXCL|O_WRONLY|O_NOFOLLOW` and mode 0600. A file that exists, or a link at the path, makes the write fail; the sink does not retry, and the ring still keeps the output. Task IDs come only from `Config.NextID` and must be plain names. A timer has no sink. Start of any other kind fails with `ErrNoTaskDir` if `Config.Dir` is empty. The composition root creates and checks the directory.

**Notice text.** `<`, `>`, and `&` are escaped with a `strings.Replacer`. The output tail is the smaller of 2 KB and 40 lines, applied after escaping and cut on a UTF-8 boundary. Title, summary, and other single values are cut to 1 KB, so a notice has a bounded size. The output-file path is the one value that is not cut, because a cut path names a file that does not exist; a path is as long as the file system allows. A fixed line says that the output is data, not instructions from the user. `Details` holds `taskId`, `kind`, `status`, `title`, and, when present, `exitCode` and `outputFile`.

**Sweep.** `Sweep(root, maxAge, now, keep)` removes session directories whose newest entry is older than `maxAge`. It keeps `keep` and runs once; it does not schedule itself. It refuses a root that is a symbolic link. It removes a child only if the child is a real directory (not a link), owned by the current user, mode 0700, and holds only regular files whose names end in `.log`. It removes those files one by one and then the directory with `os.Remove`; it never calls `RemoveAll` and never follows a link. Every other entry is skipped, so a root with foreign data loses nothing. The caller (`internal/app`) also checks that home and root exist and are private before it calls `Sweep`. `DefaultRetention` is 7 days.

**Generation.** `Config.Gen` is stamped on every `Notice`. The Agent decides what to do with an old generation.

## Tests

Tests that depend on time run inside `testing/synctest` and advance the fake clock with `time.Sleep` in the bubble, then `synctest.Wait()`. No test uses real time for synchronization. `main_test.go` runs `goleak.VerifyTestMain`. `export_test.go` has one hook, `FireTimer`, so a test can force the order of a timer fire and a stop. The sink unit tests run outside bubbles.
