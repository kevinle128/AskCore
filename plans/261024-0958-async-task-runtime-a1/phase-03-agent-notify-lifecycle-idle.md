---
phase: 3
title: "Agent integration: lifetime, Notify, lifecycle rules, idle signals"
status: completed
priority: P1
effort: "2.5d"
dependencies: [1, 2]
---

# Phase 3: Agent integration: lifetime, Notify, lifecycle rules, idle signals

## Goal

Each Agent owns one registry on a lifetime context, `Agent.Notify` is the only delivery port, Abort, Reset, and Dispose follow the design rules, and `Idle()` and `WaitForQuiescence` wait by push without a spin or a race.

Design: sections [7.2](../../docs/async-task-runtime.md), [7.3](../../docs/async-task-runtime.md), [7.5](../../docs/async-task-runtime.md), [12.2](../../docs/async-task-runtime.md), [14](../../docs/async-task-runtime.md).
Evidence: [deep scout](../reports/scout-261024-1705-async-task-runtime-a1-deep.md), [red-team](../reports/red-team-261024-1718-async-task-runtime-a1.md).

## Files to Create / Modify

- Create: `internal/agent/tasks.go` (registry ownership, `Notify`, `Tasks()`, generation, event gate, `WaitForQuiescence`, `ErrStaleNotice`)
- Create: `internal/agent/idle.go` (`busy` predicate, idle channel edges, work counter)
- Modify: `internal/agent/types.go` (`Config.TaskDir`; Agent fields `life`, `endLife`, `tasks`, `taskGen`, `taskSeq`, `idle`, `work`)
- Modify: `internal/agent/agent.go` (`New`; `Abort`; `Reset`; `dispose`; `startRunLocked` and `end` call the idle edge helper)
- Modify: `internal/agent/emit.go` (`dispatch` calls the idle edge helper where `dispatching` changes, `emit.go:136-145`)
- Modify: `internal/agent/queue.go` (`input.origin`; notice inputs bypass `MaxQueuedInputs`; closing-path rule for notices; Abort moves notices to `context`)
- Modify: `internal/tools/types.go` (`Context.Tasks tasks.Control`, nil when no registry)
- Modify: `internal/agent/tool_coordinator.go:264`, `loop_run.go` (`LoopConfig.Tasks`), `agent.go` `execute` (fill it per run)
- Modify: `internal/settings/paths.go` (exported `EnsurePrivateDir(path string) error`: the checks of `ensureHome` for each level: no symlink, owner is the user, mode 0700)
- Modify: `internal/app/agent_native.go` (set `TaskDir = <ASK_HOME>/tasks/<session>` with the resolution of `settings/auth.go:66-82`; the first call in the process runs `tasks.Sweep(<ASK_HOME>/tasks, tasks.DefaultRetention, time.Now(), session)` once, guarded by `sync.Once`; a sweep error is logged and does not stop the Agent) <!-- Red team #9, Validation 2 -->
- Create tests: `internal/agent/notify_test.go`, `task_lifecycle_test.go`, `idle_test.go`, `synctest_smoke_test.go`; `internal/settings/paths_test.go` (extend)
- Modify tests: `TestAbortClearsQueuesByDefault`, `TestAbortKeepQueuedKeepsQueues` (`queue_test.go`) get notice cases; `newAgent` (`agent_test.go:30-44`) sets `TaskDir: t.TempDir()`

## Rules to implement

| Case | Behavior | Code anchor |
|---|---|---|
| `Notify`, disposed | `ErrDisposed` | `enqueue` disposed check |
| `Notify`, old generation | `ErrStaleNotice`; no input | `tasks.go` |
| `Notify`, policy `Silent` | No input | `tasks.go` |
| `Notify`, policy `Context` | Append to `in.context`, no wake; the next `claimSteering` takes it (`queue.go:80-85`) | like `addToolContext` (`queue.go:276-285`) |
| `Notify`, policy `Wake`, idle | Steering with `origin = task`; start a run (`queue.go:181-193`) | `enqueue` path |
| `Notify`, policy `Wake`, running | Steering; claimed at the next turn boundary | `enqueue` path |
| `Notify`, policy `Wake`, closing or cancelled | **Steering** (not follow-up) and `wake = true`, so several notices give one cycle input in the next run (`end` checks `in.len()`, which counts steering) <!-- Red team #6 --> | new branch for `origin = task` |
| Queue cap | Notice inputs do not count toward `MaxQueuedInputs` and never get `ErrQueueFull`; human inputs keep the cap <!-- Red team #4 --> | `queue.go:176-178` |
| `Abort()` without `KeepQueued` | Drop human inputs and tool context; move notice inputs to `in.context` | `agent.go:173-189` |
| `Reset()` | Under `a.mu`, after the second idle check: bump `taskGen`, turn off the old registry's event gate, swap in a new registry. After **both** `a.mu` and `emitMu` are released: `old.Close(errReset)` <!-- Red team #5 --> | `agent.go:326-360` |
| `Dispose()` | After `<-r.done`: `tasks.Close(errDisposed)` outside every lock; then `endLife(errDisposed)`; then the existing drain | `agent.go:224-261` |
| Lifetime context | `context.WithCancelCause(context.Background())` in `New` | `agent.go:104-123` |
| `queue_update` text | A task notice shows `task <id> <status>: <title>`, at most 200 bytes (from `Details`), not the content <!-- Red team #12 --> | `messageText`, `queue.go:118-132` |

### Idle and quiescence <!-- Red team #1, #2 -->

- `busy := a.active != nil || a.dispatching > 0`, evaluated under `a.mu` by one helper called at four sites: `startRunLocked`, `end`, `dispatching++`, `dispatching--`.
- The helper makes a new channel only when `busy` changes from false to true and closes the current one only when it changes from true to false. The wake chain in `end` (old run ends, next run starts in the same critical section) keeps `busy` true, so no channel changes and no waiter is orphaned.
- `work` is a counter under `a.mu`, increased on every busy edge, every enqueue, and every registry start and end (through `Config.Work`).
- `WaitForQuiescence(ctx)`:
  1. Read `w1 := work`.
  2. If the registry is not idle, wait on `tasks.Idle()` or `ctx.Done()`, then go to 1.
  3. If the Agent is busy or `in.len() > 0`, wait on `a.Idle()` or `ctx.Done()`, then go to 1.
  4. Read `w2 := work`. If `w2 != w1`, go to 1. Otherwise return nil.
  It never selects on a channel whose condition already holds, so it never spins. `in.context` items do not block quiescence; a `Context` notice that is still there at exit is not delivered, which is the meaning of that policy.

## Tests first

Every test that depends on time builds the Agent, the faux provider, and the registry inside `synctest.Test`. `TestSynctestSmoke` proves that this works before the other tests use it. A fixture tool calls `tools.Context.Tasks.Start` with a timer spec or with a body that blocks on a test channel. <!-- Red team #7 -->

| Test | Tool | Proves (criterion) |
|---|---|---|
| `TestSynctestSmoke` | synctest | `newAgent` plus faux plus one prompt settle inside a bubble |
| `TestRunEndsWhileTaskRuns` | synctest | The run settles while the timer still waits (1) |
| `TestNoticeDuringRunJoinsNextRequest` | synctest | A notice delivered during a tool batch is in the next request after all tool results, once (2) |
| `TestIdleNoticeStartsOneRun` | synctest | One notice on an idle Agent gives one run (3) |
| `TestNoticesBeforeClaimShareCycle` | synctest | Two notices before the first claim give one cycle input (4) |
| `TestNoticesDuringClosingShareCycle` | synctest | Three notices after `claimCycle(ending)` give one cycle input in the next run (4) |
| `TestNoticeBypassesQueueCap` | plain | With `MaxQueuedInputs: 1`, one human input and two notices: both notices are queued; a second human `Steer` still gets `ErrQueueFull` |
| `TestAbortKeepsTasksAndMovesNotices` | synctest | After Abort the task still runs; a queued notice moves to `context`, rides with the next prompt, no wake (5) |
| `TestResetDropsLateNoticesAndEvents` | synctest | After Reset, no notice and no `task_*` event of the old generation appears after the first event of the new epoch (5) |
| `TestDisposeStopsAndWaitsTasks` | synctest | Dispose returns after every runner; later `Start` returns `ErrClosed`; `goleak` clean (5) |
| `TestNoticeIsUserMessageForModel` | synctest | The provider request holds a user message with the escaped block and the framing line; the log holds a `CustomMessage` (6) |
| `TestQueueUpdateShowsNoticePreview` | plain | `queue_update` shows the preview, at most 200 bytes |
| `TestIdleNoOrphanOnWakeChain` | synctest | A waiter on `Idle()` during a wake-chain handoff wakes once, when the last run ends |
| `TestIdleCoversPublisherDispatch` | synctest | `end` during a publisher dispatch does not close the channel; it closes once when the dispatch ends; no double close |
| `TestWaitForQuiescenceBlocksWithoutSpin` | synctest | Idle Agent plus running task: the waiter is durably blocked (`synctest.Wait` returns); it returns after the notice run ends |
| `TestWaitForQuiescenceRace` | synctest | A task that fires between the two reads makes the call wait for the notice run |
| `TestWaitForQuiescenceIgnoresContextQueue` | synctest | A `Context` notice left in `in.context` does not block |
| `TestNoNestedLocks` | synctest | A fixture tool and a listener that call `Tasks().List()` and `State()` while tasks deliver do not deadlock (`-race`) |
| `TestEnsurePrivateDir` (settings) | plain | A symlink, a foreign owner, or a wrong mode at any level is refused |
| `TestNativeAgentSweepsOnce` (app) | plain | Two `NewNativeAgent` calls in one process run the sweep once; an old session directory is gone, the new session's is kept |

## Steps

1. Write `TestSynctestSmoke` and make it pass with the current code. Then write the other tests and see them fail.
2. `settings.EnsurePrivateDir` and its test. `app.NewNativeAgent` sets `TaskDir`.
3. Agent fields, lifetime context, registry construction in `New` (`Deliver = a.Notify`, `Emit` through the generation gate and `postLocked`, `Dir = Config.TaskDir`, `Gen`, `NextID` from `taskSeq`, `Work`).
4. `idle.go`: the `busy` helper at the four sites, the work counter.
5. `input.origin`; `Notify` with the rules table; queue cap bypass; closing-path rule; `messageText` preview.
6. `Abort`, then `Reset`, then `dispose`; run the lifecycle tests after each.
7. `LoopConfig.Tasks` into `tools.Context`.
8. `WaitForQuiescence` as specified above.
9. Run the package tests with `-race` and `goleak`.

## Verification

- `go test -race ./internal/agent/... ./internal/tools/... ./internal/tasks/... ./internal/settings/... ./internal/app/...`
- `go test -race ./cmd/tui/... ./internal/acp/...` (behavior with no tasks stays the same)
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...`
