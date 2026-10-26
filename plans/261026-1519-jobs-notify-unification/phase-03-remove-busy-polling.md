---
phase: 3
title: "Remove the busy-retry polling"
status: pending
priority: P1
effort: "1d"
dependencies: [2]
---

# Phase 3: Remove the busy-retry polling

## Goal

A prompt that meets a run started by a notice waits on the Agent idle signal and then succeeds, in print mode and over ACP, without sleeping and retrying.

Deep mode: scout both sites, the ACP binding and `Agent.Idle` before you start.

## Known facts (verified on `ee53317`)

- `promptAll` sleeps `busyRetryDelay` (5 ms) after `ErrBusy` and retries (`cmd/tui/headless.go:476-511`).
- ACP has two gates. `admit` waits `WaitForIdle` for at most `busyWindow` (250 ms) and then refuses with `ErrBusy` (`internal/acp/host.go:30,730-734`). `Session.execute` then sleeps `busyPoll` (2 ms) after `ErrBusy` and retries while `b.runID == ""` (`host.go:768-783`).
- `observe` binds the first event that carries a run ID to the open binding, whatever started that run (`host.go:540-543`). A run that a notice starts between `admit` and `Prompt` therefore binds to the caller, sets `runID`, and ends the retry loop with `ErrBusy`; an early client Abort (`abortPending`) is applied to that notice run.
- `Agent.Idle()` returns a channel that closes on the busy-to-idle change (`internal/agent/idle.go:39-46`).
- `docs/async-task-runtime.md` section 12.2 plans this change (phase A2).

## Design

- `promptAll`: on `ErrBusy`, `select` on `ag.Idle()` and `ctx.Done()`, then retry once per idle signal. Remove `busyRetryDelay`.
- ACP binding: bind only the run that the call itself started. The call learns its run ID from the Agent before any event of that run is observed (for example `Prompt`, `Continue` and the queue calls return or report the run ID through a callback that runs before `agent_start`); `observe` binds on that ID only. The scout picks the smallest Agent API change that gives this.
- `Session.execute`: on `ErrBusy`, wait on `s.agent.Idle()` and `ctx.Done()`, then retry once per idle signal, with one overall bound (the existing window value) as a timer, not a poll. Remove `busyPoll`.
- `admit`: stop refusing after 250 ms when the Agent is busy with a run that the call does not own; the retry in `execute` handles it. Keep the refusal for a session that is resetting or already bound.
- A notice run never takes an Abort that the client meant for its own call.

## Files to modify

- `cmd/tui/headless.go`, a focused test file in `cmd/tui`
- `internal/acp/host.go`, `internal/acp/*_test.go`
- `internal/agent/agent.go` or `queue.go` (run-ID hand-back chosen in the scout), `internal/agent/README.md`
- `docs/async-task-runtime.md` section 12.2 (mark the two sites done)

## Test matrix (write first)

| Test | Pass condition |
|---|---|
| `TestPromptAfterNoticeRunWaitsForIdle` (`cmd/tui`) | A notice starts a run; the next prompt is accepted after that run ends; no sleep in the code path |
| `TestPromptDuringNoticeRunSucceedsOverACP` (`internal/acp`) | `Notify` starts a run between `admit` and `Prompt`; the prompt is not bound to the notice run and succeeds after it ends |
| `TestEarlyAbortDoesNotCancelNoticeRun` (`internal/acp`) | A client Abort sent before its own run exists does not cancel a notice run |
| `TestExecuteStopsWaitingWhenOwnRunBinds` (`internal/acp`) | When the call's own run starts, the binding takes its ID and the call proceeds as today |
| Existing ACP busy and sibling-input tests | Pass |

## Verification

```sh
go test -race -count=1 ./cmd/tui/... ./internal/acp/... ./internal/agent/...
rg -n "busyRetryDelay|busyPoll|time\.Sleep" cmd/tui/headless.go internal/acp/host.go
go test -count=1 ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

Pass: all commands exit 0, and the `rg` command prints nothing.

## Rollback

Revert the phase commits; the two sleep loops and the first-event binding return.
