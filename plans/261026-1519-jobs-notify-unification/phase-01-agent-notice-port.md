---
phase: 1
title: "Agent notice port per generation"
status: pending
priority: P1
effort: "1d"
dependencies: []
---

# Phase 1: Agent notice port per generation

## Goal

An outside source delivers notices through `Agent.Notify` with a port bound to one conversation generation, and every tool call receives the port of its run, pinned against handlers.

## Context (verified)

- `Agent.Notify(tasks.Notice)` refuses an old generation with `ErrStaleNotice`, does not count notices toward `MaxQueuedInputs`, and maps policy `Wake` to steering, a new run on an idle Agent, or the wake latch (`internal/agent/tasks.go:85-143`).
- `Notify` never reads `Notice.Origin`; it stores every notice with `originTask` (`internal/agent/tasks.go:120,136`).
- `Notify` has no model check: on an idle Agent it calls `startRunLocked` even when the model ID is empty (`internal/agent/tasks.go:124-126`), while `enqueue` refuses with `ErrNoModel` (`internal/agent/queue.go:320-323`).
- `checkNoticeMessage` accepts a `UserMessage` (`internal/agent/tasks.go:149-165`).
- `Agent.Remove` removes any queued input by ID, any origin (`internal/agent/queue.go:99-109,346-355`).
- A run takes `cfg.Tasks = a.Tasks()` once (`internal/agent/agent.go:607`). `tools.Context` is built at `internal/agent/tool_coordinator.go:264` and `CallID`, `Cwd` and `Tasks` are set again at `:286`, after the ExecuteTool middleware; the pipeline contract freezes them (`internal/pipeline/points.go:195-199`).
- `taskGen` starts at its zero value and changes only in `swapContext` (`internal/agent/agent.go:88,475`), which `Reset` calls on an idle Agent. Generation 0 is a real generation.
- `Abort` clears the wake latch, moves queued task notices to the context queue (unless `KeepQueued`), and cancels the run with `errUserAbort` (`internal/agent/agent.go:224-238`, `queue.go:147-159`). The run stays active while its tool bodies drain; a `Notify` in that window sets the latch again, and `end` starts a run (`internal/agent/tasks.go:124-136`, `agent.go:564`). After the run ends, a `Notify` on the idle Agent starts a run at once.
- Import rules: `tasks` may import the standard library, `pkg/protocol` and `internal/tracing`; `tools` already imports `tasks`.

## Design

`internal/tasks/types.go` gets the port contract (no origin parameter, because `Notify` does not use it):

```go
// Port delivers the notices of an outside source to the Agent of one
// conversation generation. A tool call gets the Port of its run, so work that
// the call starts keeps the generation of that conversation.
type Port interface {
	// Gen is the generation of the Port.
	Gen() uint64
	// Notify queues msg as a notice with policy. It never counts toward the
	// human queue bound. A Port of an older generation is refused with an
	// error that wraps ErrNotDelivered.
	Notify(policy NoticePolicy, msg protocol.Message) (string, error)
	// Remove deletes a queued notice that no run has taken yet. It removes
	// only notices, never human input, and nothing for an older generation.
	Remove(id string) bool
}
```

Fix the `Notice.Message` comment at `internal/tasks/types.go:104` ("a protocol.CustomMessage") to "the message the model gets; a task uses a protocol.CustomMessage".

`internal/agent/tasks.go`:

- `type noticePort struct { a *Agent; gen uint64 }` implements `tasks.Port`. `Notify` calls `a.Notify(tasks.Notice{Gen: p.gen, Policy: policy, Message: msg})`. `Remove` takes `a.mu`, returns false when `p.gen != a.taskGen`, and removes only an input with `originTask` (new `inbox.removeNotice`), posting `queue_update` once.
- `Agent.Notify` with policy `Wake` on an idle Agent that has no model queues the notice in steering and starts no run; the next admission carries it. Test it.
- Interrupt rule (user decision D-R7, 2026-10-26, as Grok): `Abort` sets an `interrupted` flag under `a.mu`. While it is set, `Notify` with policy `Wake` appends to the context queue, starts no run and does not set the wake latch; this covers the abort-drain window and the idle Agent after it. The next human input (`Prompt`, `Continue`, `Steer`, `FollowUp`) and `Reset` clear the flag; the first run of that input claims the context items with it. `KeepQueued` aborts set the flag too, unless the scout finds a caller that needs otherwise.
- No exported `Agent.Notices()`. Tests reach the port through `export_test.go` (`func NoticesForTest(a *Agent) tasks.Port`); production gets it only from the run.

`internal/agent/agent.go` (`execute`): take the registry and the port in one locked snapshot, so the two always share a generation:

```go
a.mu.Lock()
cfg.Tasks, cfg.Notices = a.tasks, noticePort{a: a, gen: a.taskGen}
a.mu.Unlock()
```

`internal/agent/loop_run.go`: `LoopConfig.Notices tasks.Port` beside `Tasks`, set by the Agent for each run.

`internal/agent/tool_coordinator.go`: pass `Notices: b.cfg.Notices` at `:264`, and set it again at `:286` with `CallID`, `Cwd` and `Tasks`, so a handler cannot drop or swap it.

`internal/pipeline/points.go:195-199`: add `Context.Notices` to the frozen list in the `ExecuteToolInput` comment.

`internal/tools/types.go`: `Context.Notices tasks.Port`, nil when the caller has no Agent. A tool that needs the port refuses when it is nil (phase 2); it never falls back to generation 0.

No message shape changes. No new queue. `Agent.FollowUp`, `Steer` and `Remove` keep their behavior.

## Files to create / modify

- Modify: `internal/tasks/types.go` (`Port`, comment fix)
- Modify: `internal/agent/tasks.go` (`noticePort`, no-model rule in `Notify`)
- Modify: `internal/agent/queue.go` (`inbox.removeNotice`)
- Modify: `internal/agent/agent.go` (one locked snapshot in `execute`)
- Modify: `internal/agent/loop_run.go` (`LoopConfig.Notices`)
- Modify: `internal/agent/tool_coordinator.go` (pass and re-pin the port)
- Modify: `internal/agent/export_test.go` (`NoticesForTest`)
- Modify: `internal/pipeline/points.go` (frozen-field comment)
- Modify: `internal/tools/types.go` (`Context.Notices`)
- Create: `internal/agent/notice_port_test.go`
- Modify: `internal/agent/README.md`, `internal/tasks/README.md`, `internal/tools/README.md`, `internal/pipeline/README.md` (one line each)

## Test matrix (write first, watch each fail, then implement)

All tests use the real Agent API with the faux provider, as `internal/agent/notify_test.go` does, and wait on Agent signals, not sleeps.

| Test | Scenario | Pass condition |
|---|---|---|
| `TestNoticePortWakesIdleAgent` | Port `Notify(NoticeWake, msg)` on an idle Agent | One run starts; its request contains `msg` as a user message |
| `TestNoticePortJoinsRunAtNextTurnBoundary` | Notify while a tool call of a run is in progress | The next request of the same run contains `msg`; no second run starts |
| `TestNoticePortRefusedAfterReset` | Take a port, `Reset`, then Notify and Remove with the old port | Notify returns an error that wraps `tasks.ErrNotDelivered`; Remove returns false; nothing is queued; a new port works |
| `TestNoticePortBypassesQueueCap` | `MaxQueuedInputs: 1`, one human follow-up queued during a run, then Notify | Notify succeeds; a second human input still gets `ErrQueueFull` |
| `TestNoticePortNoticeSurvivesAbort` | Notify during a run, then `Abort` | The notice moves to the context queue and rides with the next prompt's request |
| `TestNoticePortRemoveDeletesOnlyNotices` | Queue one human follow-up and one notice during a run; port `Remove` both IDs | The notice ID returns true and leaves one `queue_update`; the human ID returns false and stays queued |
| `TestNoticeAfterAbortWaitsForHumanInput` | `Abort` a run and let it end, then port Notify Wake | No run starts; the notice is in the context queue; the next `Prompt` request contains it |
| `TestNoticeDuringAbortDrainDoesNotStartRun` | A tool body blocks after `Abort`; port Notify Wake while it drains; then the body ends | The run ends and no new run starts; the notice rides with the next human input |
| `TestHumanInputClearsInterrupt` | After `Abort` and one `Prompt`, port Notify Wake on the idle Agent | A run starts as usual |
| `TestNoticePortWithoutModelQueuesWithoutRun` | `ClearModel`, then port Notify Wake on an idle Agent | No run starts; after `SetModel` and a prompt, the request contains the notice |
| `TestToolGetsNoticePortOfItsRun` | A test tool records `tc.Notices.Gen()` in two runs separated by `Reset` | Each recorded generation equals the port generation at that time; the two differ |
| `TestExecuteToolHandlerCannotSwapNoticePort` | An ExecuteTool handler passes on a `tools.Context` with `Notices` nil | The tool body still sees the run's port |
| `TestNoticePortKeepsUserMessageShape` | Notify a `UserMessage` with `protocol.SystemReminder(...)` text | The session log keeps role `user` and the same text; the provider request carries the same bytes |

## Tasks & steps

1. Write `internal/agent/notice_port_test.go` with the thirteen tests; run `go test -run 'NoticePort|ToolGetsNoticePort|ExecuteToolHandlerCannotSwap' ./internal/agent` and confirm they fail.
2. Add `tasks.Port` and fix the `Notice.Message` comment.
3. Add `noticePort`, `inbox.removeNotice`, the no-model rule and the interrupt rule in `Notify` (flag set by `Abort`, cleared by human input and `Reset`), and `NoticesForTest`.
4. Add `LoopConfig.Notices`, set it in `execute` in the same locked snapshot as `Tasks`, pass it at `tool_coordinator.go:264` and re-pin it at `:286`; update the frozen-field comment in `points.go`.
5. Run the phase tests with `-race`, then the full suite and lint.
6. Update the four package READMEs.
7. Commit: `feat(agent): give tools a notice port bound to the run's generation`.

## Verification

```sh
go test -race -count=1 -run 'NoticePort|ToolGetsNoticePort|ExecuteToolHandlerCannotSwap|AfterAbort|AbortDrain|ClearsInterrupt' ./internal/agent
go test -race -count=1 ./internal/agent/... ./internal/tasks/... ./internal/tools/... ./internal/pipeline/...
go test -count=1 ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

Pass: all commands exit 0; the thirteen new tests pass. An existing test that expects a notice after Abort to start a run is changed to the D-R7 rule, with the reason in the commit message.

## Rollback

Revert the phase commit. Nothing outside `agent`, `tasks`, `tools` and a `pipeline` comment depends on the port yet.
