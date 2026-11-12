---
phase: 1
title: "Agent, pipeline and task registry"
status: pending
priority: P1
effort: 5h
dependencies: []
---

# Phase 1: Agent, pipeline and task registry

## Goal

Fix R1, R8, R9, R10, N18, N19 and N20 so that queued input never waits in an idle Agent, a typed-nil notice is refused, handler edits made in place are kept, and closed or disposed objects fail before they do work.

## File ownership

- Modify: `internal/agent/agent.go`, `internal/agent/tasks.go`, `internal/agent/tool_coordinator.go`, `internal/agent/export_test.go`
- Modify: `internal/pipeline/registry.go`, `internal/pipeline/points.go`
- Modify: `internal/tasks/registry.go`
- Tests: `internal/agent/loop_run_test.go`, `internal/agent/notify_wake_test.go`, `internal/agent/dispose_test.go`, `internal/pipeline/registry_test.go`, `internal/tasks/registry_test.go`

No other phase touches these files.

## Constraints

- Import rules: `internal/pipeline` must not import `internal/agent` (`internal/pipeline/README.md:62`). The clone helper therefore moves from agent to pipeline, not the other way.
- No sleep and no polling in code or tests (`docs/async-task-runtime.md`). Use test seams that the package already uses (`quiescenceProbe`, `internal/agent/agent.go:99-101`, set through `internal/agent/export_test.go:62`).

## Steps

### 1. R1 (Medium): `Continue` must not strand queued input

Evidence: `Continue` takes the slot with `begin` (`internal/agent/agent.go:232`, `startRunLocked` at `:586-592` sets `a.active`), then returns `ErrContinueEmpty` or `ErrContinueFromAssistant` (`:238-243`) with no `execute`. `enqueue` on an active run that is not closing queues the input and does not set `a.wake` (`internal/agent/queue.go:338-351`). `end` starts the next run only when `a.wake` is set and the inbox is not empty (`agent.go:609-614`). Queued input then waits in an idle Agent until the next human input.

Tests first (both must fail before the fix, run with `-race`):

1. Add an unexported seam `continueProbe func(afterDecision bool)` on `Agent`, nil in production, with a setter in `export_test.go` next to `SetQuiescenceProbe`. `Continue` calls it once before the decision (`false`) and once after the decision, before it returns (`true`).
2. `TestContinueRunsInputQueuedBeforeTheCheck`: an Agent with an empty conversation; the probe (`false`) calls `FollowUp` with a user message. Expect `Continue` to return nil, the follow-up to reach the model once (faux or the recorder stream of `agent_test.go`), and `WaitForQuiescence` to return.
3. `TestContinueWakesForInputQueuedAfterTheCheck`: the probe (`true`) calls `Steer`. Expect `Continue` to return `ErrContinueEmpty`, and a second run to start and take the steer message; `WaitForQuiescence` returns without a context deadline.
4. `TestContinueQueuedRunWithoutModelDoesNotRespawn`: the probe (`true`) calls `ClearModel` (`internal/agent/agent.go:423-431`, deferred while a run is active) and then `FollowUp`, so the next run fails preparation (`drive` sets `preparationFailure`, `agent.go:631-637`). Expect exactly one settled event for it and no further run, because `end` clears `wake` (`agent.go:616`). This guards against the omp retry loop (omp #6344).

Fix (follow omp, as accepted):

1. In `Continue`, after the message checks fail, take `a.mu` once. In that one critical section: when `a.in.len() > 0`, keep the run and execute it with the queued-input loop; otherwise set `r.closing = true` and return the existing error. Release `a.mu` before `execute`.
2. `r.closing` is the existing flag that makes `enqueue` set `a.wake` (`queue.go:344-348`), so a message that arrives after the decision starts the next run in `end`. Add no new flag.
3. Extract the loop closure of `drive` (`agent.go:631-641`, the `runQueued` call) into one helper that `drive` and `Continue` share, so both apply the same no-model rule.
4. Keep `notifyRunStart(ctx, r.id)` before `execute` on the queued path.
5. Update the doc comment of `Continue` (`internal/agent/agent.go:228-230`): it no longer always fails on an empty context or an assistant tail; queued input runs instead.

Command: `go test -race -run '^TestContinue' ./internal/agent`

### 2. R8: refuse every typed-nil notice

Evidence: `checkNoticeMessage` refuses a typed nil only for `*protocol.CustomMessage` (`internal/agent/tasks.go:159-176`), while the doc says every typed nil pointer is refused (`tasks.go:95-96`). `(*protocol.UserMessage)(nil).Role()` panics because the method has a value receiver (`pkg/protocol/message.go:203-212`). The one injection point is `Notify` (`tasks.go:114`).

Test first: add rows `(*protocol.UserMessage)(nil)` and `(*protocol.AssistantMessage)(nil)` to `TestNotifyRejectsMalformedMessage` (`internal/agent/notify_wake_test.go:118-127`); expect `ErrNoInput` and no panic.

Fix: at the top of `checkNoticeMessage`, return `ErrNoInput` when `m == nil` or when `reflect.ValueOf(m)` is a pointer and `IsNil()`. Keep the `CustomType` checks.

Command: `go test -run '^TestNotifyRejectsMalformedMessage$' ./internal/agent`

### 3. R9: keep in-place edits of `Context.Messages` in `PrepareRequest`

Evidence: `orig := in` shares the message slice (`internal/pipeline/registry.go:276`). `sameMessages` compares only the slice view (`:297-300`), so a handler that sets `in.Context.Messages[i]` changes `orig` too and the edit is lost (`:280`).

Test first: `TestPrepareRequestKeepsInPlaceMessageEdit` in `registry_test.go`: a handler replaces element 0 in place and calls `next`; expect a non-nil `RequestUpdate` with the new element, and the caller's slice unchanged.

Fix (omp shape: copy before dispatch, compare against the original):

1. When the handler list is not empty, dispatch `in` with `in.Context.Messages = slices.Clone(orig.Context.Messages)`. With no handler, keep today's path (no copy on each model request).
2. In the terminal, a change is `!reflect.DeepEqual(in.Context.Messages, orig.Context.Messages)` or a different `Tools`. Do not compare elements with `==`: `protocol.Message` has value types with slices (`pkg/protocol/message.go:890-892`), and `==` on them panics. `reflect.DeepEqual` returns early for identical pointers and slice headers, so the cost stays linear in the message count.
3. Update the comment of `sameMessages` or remove the function when nothing else uses it.

Command: `go test -run '^TestPrepareRequest' ./internal/pipeline`

### 4. R10: keep in-place edits of the `AfterTool` result

Evidence: `orig := in` (`registry.go:363`) shares `in.Result.Content` with the dispatched value, so `reflect.DeepEqual(in.Result, orig.Result)` (`:365`) is true after an in-place edit and the terminal returns nil. The deep clone exists only in agent (`cloneToolResult`, `internal/agent/tool_coordinator.go:376-379`), built on `pipeline.AfterToolCallResult.Clone`.

Test first: `TestAfterToolKeepsInPlaceResultEdit`: a handler sets `in.Result.Content[0]` to a new text block and calls `next`; expect a non-nil result with that block.

Fix:

1. Move `cloneToolResult` to `internal/pipeline/points.go` as exported `CloneToolResult(protocol.ToolExecutionResult) protocol.ToolExecutionResult`. Make agent call it at its five call sites (`tool_coordinator.go:296`, `:298`, `:305`, `:316`, `:363`) and delete the agent copy at `:376-379` (DRY).
2. In `AfterTool`, set `orig.Result = CloneToolResult(in.Result)` before `runAround`, so the comparison base cannot change.

Command: `go test -run '^TestAfterTool' ./internal/pipeline && go test ./internal/agent`

### 5. N18: `SetThinkingLevel` on a disposed Agent

Evidence: no `a.disposed` check (`internal/agent/agent.go:446-454`); `SetModel` has one (`:372`, `:405`).

Test first: `TestSetThinkingLevelAfterDispose` in `dispose_test.go`: expect `ErrDisposed` and no config change.

Fix: `if a.disposed { return ErrDisposed }` after `a.mu.Lock()`.

### 6. N19: `SetModel` checks disposed first

Evidence: `providers.Validate` runs before the first disposed check (`agent.go:368-375`).

Test first: `TestSetModelAfterDisposeReturnsDisposed`: a disposed Agent and an invalid model; expect `ErrDisposed`, not the validation error, and no key resolver call.

Fix: read `a.disposed` under `a.mu` at the top of `SetModel`; keep the commit-time check (`agent.go:405`).

Command for steps 5 and 6: `go test -run 'AfterDispose' ./internal/agent`

### 7. N20: `Registry.Start` checks closed before `NextID`

Evidence: `r.cfg.NextID` runs at `internal/tasks/registry.go:133`, the closed check at `:148-151`.

Test first: `TestStartOnClosedRegistryTakesNoID`: a `NextID` that counts calls; close the registry; expect `ErrClosed` and zero calls.

Fix: lock, read `r.closed`, unlock, and return `ErrClosed` before `NextID`. Keep the locked check at `:149`, because `Close` can run between the two.

Command: `go test -race -run '^TestStart' ./internal/tasks`

## Verification

- `go test -race ./internal/agent ./internal/pipeline ./internal/tasks`
- `go vet ./internal/agent ./internal/pipeline ./internal/tasks`
- golangci-lint on the three packages (depguard: pipeline does not import agent).

## Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| R1 queued path runs input that a caller expected to fail | Low | Medium | Same behavior as `drive`; `ErrContinueEmpty` stays when the inbox is empty. ACP callers (`internal/acp/host.go:908`, `ask_methods.go:383`) treat nil as a normal run end. |
| R1 respawn loop | Low | High | Test 4; `end` clears `wake` after each run. |
| R9 copy cost on each request | Low | Low | Copy only when handlers exist. |

## Rollback

Revert the phase commits. Step 4 moves one helper; reverting restores the agent copy.

## Docs impact

Phase 7 updates `internal/agent/README.md` (Continue with queued input) and `internal/pipeline/README.md:24-28` (in-place edits count).
