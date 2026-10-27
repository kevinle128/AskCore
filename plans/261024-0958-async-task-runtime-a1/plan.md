---
title: "Async task runtime A1: registry, delivery port, notice message"
description: "The Agent owns a task registry on a lifetime context; task ends reach the model once through Agent.Notify as a custom message, with no sleep and no polling."
status: completed
priority: P1
effort: "5.5d"
branch: develop
tags: [backend, agent, concurrency, tdd]
blockedBy: []
blocks: []
created: 2026-10-24
---

# Async task runtime A1

## Outcome and boundaries

Mode: `--tdd`, no `--yagni`. Every phase writes its failing tests first.

The Agent owns one `tasks.Registry` on a lifetime context that only Dispose cancels.
A task ends in exactly one terminal state, and the registry gives exactly one notice to `Agent.Notify`.
`Agent.Notify` maps the notice onto the existing claims (steering, follow-up with wake, context) and starts a run on an idle Agent.
The notice is a `protocol.CustomMessage` that the converter turns into a user message for the model.
A1 ships the timer kind as the first body. Process bodies, the `bash` tool, and `ask -p` waiting are phase A3 of the design.

**Design authority:** [async task runtime](../../docs/async-task-runtime.md) (accepted), [ADR 0004](../../docs/adr/0004-agent-owned-async-task-runtime.md).
**Research:** [Go concurrency practices](../reports/researcher-261024-1559-go-goroutine-channel-best-practices.md), [reference harness designs](../reports/researcher-261024-1559-agent-async-task-reference-designs.md).
**Package contracts:** [agent](../../internal/agent/README.md), [tools](../../internal/tools/README.md), [protocol](../../pkg/protocol/README.md), [architecture 7.2](../../docs/ask-architecture-reference.md).

### Fixed decisions (user, 2026-10-24)

| ID | Decision |
|---|---|
| D1 | No cap on wake-started runs |
| D2 | No concurrency limit, no goroutine pool, no per-Agent task cap |
| D3 | `ask -p` waits for background work and takes one more turn per completion (A3) |
| D4 | A foreground command that reaches its timeout moves to the background (A3) |

### Constraints

- No new input queue. Notices use `context`, `steering`, and `followUp` of `queue.go`.
- `registry.mu` and `Agent.mu` are never held together. The registry calls `Deliver` and emit with no lock held.
- No `time.Sleep`, no ticker, and no polling loop in non-test code. Timers use `time.AfterFunc`.
- `internal/tasks` imports only stdlib, `pkg/protocol`, and `internal/tracing`. A strict depguard allow list enforces it.
- Sink files live in `ASK_HOME/tasks/<session>/`, set by `internal/app/agent_native.go`, and are kept 7 days: the first Agent of each process sweeps older session directories once (no timer, no polling); each directory level is checked as `settings.ensureHome` does; files are opened lazily with `O_EXCL|O_NOFOLLOW` and mode 0600.
- Task notices never get `ErrQueueFull`: they do not count toward `MaxQueuedInputs`.
- `WaitForQuiescence` never selects on a channel whose condition already holds.
- Tests: `testing/synctest` for time, `goleak.VerifyTestMain` in every package that starts goroutines, `go test -race`. No sleep for synchronization.
- Code comments, file names, test names, and commit messages carry no plan, phase, or decision labels. Comments use ASD-STE100.

### Non-goals

Process and HTTP bodies, `bash run_in_background`, `task_stop`, move to background, `ask -p` waiting, and the end-to-end proof through `ask -p` (A3, validation answer 1); ACP busy-poll and stdio watchdog replacement (A2); sub-agents (A4); sources such as MCP and extensions (A5); durable tasks (A6); task events in the headless `--mode json` stream (decided in A3).

## Phases

| # | Phase | Effort | Depends on | Status |
|---|---|---|---|---|
| 1 | [Notice message and task events in protocol](./phase-01-protocol-notice-message-and-task-events.md) | 0.5d | none | Completed |
| 2 | [Tasks package: registry, runner, sink, notice, timer](./phase-02-tasks-registry-runner-sink-timer.md) | 2d | 1 | Completed |
| 3 | [Agent integration: lifetime, Notify, lifecycle rules, idle signals](./phase-03-agent-notify-lifecycle-idle.md) | 2.5d | 1, 2 | Completed |
| 4 | [Docs sync and full verification](./phase-04-docs-sync-and-verification.md) | 0.5d | 3 | Completed |

## Acceptance criteria

1. A run ends when the model ends its turn while a task still runs (agent test).
2. A task that ends during a run reaches the next model request of that run once, after the whole tool batch (agent test with a recording provider).
3. A task that ends while the Agent is idle starts exactly one run (agent test).
4. Notices that arrive before the first claim of a run, or while a run closes, share one cycle input (agent test).
5. Abort keeps running tasks and moves queued notices to the `context` queue; Reset stops tasks and no notice or event of the old generation reaches the new epoch; Dispose stops and waits for every task. No goroutine leaks (`goleak`).
5a. A full input queue never drops a notice, and notices never take the human's queue slots.
6. A notice reaches the model as a user message with escaped fields, kept line breaks, and the framing line; `queue_update` shows a preview of at most 200 bytes; the session log stores it as a `MessageEntry`.
7. `go test -race ./...` and `golangci-lint run ./...` pass. `grep` finds no new `time.Sleep` or `NewTicker` in non-test code of the changed packages.

## Risks

| Risk | Mitigation |
|---|---|
| A late notice from before Reset lands in the new conversation | Registry generation: Reset swaps the registry under `a.mu`; `Notify` drops a notice of an old generation (dewee pattern) |
| A notice is lost between the terminal transition and `Deliver` | The runner closes `done`, turns the registry idle, and calls `wg.Done` only after `Deliver` returns; `Close` waits on `wg` |
| Idle channel closed twice or a waiter orphaned | One `busy` predicate and edge-only channel changes at four sites (phase 3) |
| `WaitForQuiescence` spins or returns early | Waits only on open conditions; a work counter read before and after (phase 3) |
| `ConvertToLLM` change alters replay of existing logs | Existing logs hold no custom messages; a test rebuilds a log that has one through `request_log.go:114` |
| Notice text forges a block or reads as a user instruction | `<`, `>`, `&` escaped with a replacer; a fixed framing line says the content is data; a test puts `</task-notification>` in the output |

## Red Team Review

Session 2026-10-24. Report: [red-team](../reports/red-team-261024-1718-async-task-runtime-a1.md). Scout: [deep scout](../reports/scout-261024-1705-async-task-runtime-a1-deep.md).
Reviewers: Failure Mode Analyst, Assumption Destroyer, Security Adversary. 27 raw findings, 18 after deduplication: 15 accepted (user approved all, 2026-10-24), 2 rejected, 2 moved to validation.

| # | Severity | Accepted finding | Applied in |
|---|---|---|---|
| 1 | Critical | Idle channel orphaned on the wake chain or closed twice | Phase 2 idle rule, phase 3 idle section |
| 2 | Critical | `WaitForQuiescence` spin, race, undefined empty inbox | Phase 3 idle section |
| 3 | High | `task_end` cannot carry the notice outcome | Phase 1 types, phase 2 terminal order, design 13 |
| 4 | High | `ErrQueueFull` drops notices; notices starve human input | Phase 3 rules, phase 2 discard rule |
| 5 | High | Reset closes the old registry under `emitMu`; stale events in the new epoch | Phase 3 rules |
| 6 | Medium | Notices during a closing run give N cycles | Phase 3 rules, criterion 4 |
| 7 | High | No time control in agent tests; race test cannot force order | Phase 2 and 3 test notes |
| 8 | Medium | Throttle event after `task_end` | Phase 2 rules |
| 9 | High | Sink directory in `/tmp`, unchecked, never set in production | Constraints, phase 2 sink rule, phase 3 files |
| 10 | Medium | Lax depguard does not enforce "imports only" | Phase 2 files |
| 11 | High | `xml.EscapeText` breaks line structure; cap before escaping | Phase 2 notice rule |
| 12 | Medium | `queue_update` can pass the bus event cap | Phase 1 step 6 |
| 13 | Medium | Sink file opened for every task, also timers | Phase 2 sink rule |
| 14 | High | Two existing tests use `"custom"` as an unknown role | Phase 1 files and tests |
| 15 | Medium | Task output reads as a user instruction | Phase 2 notice rule (framing line) |

Rejected: a bound on the `context` queue (user decisions D1 and D2: no limits; each notice is bounded); exposing the run origin to hooks in A1 (no hook consumer yet; A3).

### Whole-Plan Consistency Sweep

- Searched all plan files for `NoticeInputID`, `Discarded` in events, `os.TempDir`, `xml.EscapeText`, `lax depguard`, `NumGoroutine`, `<kind-prefix>`: none remain except as "not this" notes.
- Effort updated (phase 2: 2d, phase 3: 2.5d, total 5.5d).
- Design doc sections 7.1, 7.2, 9.3, 13, and 14 updated to match.
- The two open items (end-to-end proof, sink retention) are resolved in the Validation Log below.

## Validation Log

### Session 1 (2026-10-24)

Verification: skipped as allowed, because the red-team review already re-checked every cited `file:line` (Standard tier, Fact Checker and Contract Verifier).

| # | Question | Answer (user) | Propagated to |
|---|---|---|---|
| 1 | Where is the end-to-end proof through `ask -p`? | **B: in A3.** A1 proves every criterion through the real Agent API with a test fixture tool. A3 adds `bash run_in_background`, the `ask -p` wait, a faux `bg` command, and runs all criteria again through `ask -p`. This is a recorded deviation from design section 16 for A1 only | Non-goals, design sections 16 and 17 |
| 2 | How long are task output files kept? | **B: keep, remove by age.** 7 days (`tasks.DefaultRetention`); the first Agent of each process sweeps older session directories once. A settings key can override it when `settings.json` loading exists | Phase 2 `sweep.go`, phase 3 `agent_native.go`, design section 9.3 |

### Whole-Plan Consistency Sweep

- Searched plan files for "retention", "Dispose" plus files, and "cleanup": every phase now agrees that Dispose does not delete task files and that the startup sweep removes them after 7 days.
- Non-goals and design sections 9.3, 16, and 17 match both answers.
- No unresolved contradictions. The plan is ready for implementation.

## Implementation Review

Completed 2026-10-24 with `/ak:cook --auto` in TDD order. Nothing is committed.

### Evidence

- `go test -count=1 ./...`: all packages pass.
- `go test -race`: `internal/agent`, `internal/tasks`, `internal/settings`, `internal/app` (count 2 to 5), `cmd/tui`, `internal/acp`, `pkg/protocol` pass.
- `go vet ./...` clean; golangci-lint `0 issues`; the strict depguard rule rejects a probe import of testify in non-test code.
- No `time.Sleep` or `NewTicker` in non-test code of `internal/agent` and `internal/tasks`.
- `~/.ask/tasks` is not created by `go test ./...` or by a session without task output.
- `ASK_FAUX_TPS=0 go run ./cmd/tui -p "hello"` prints the answer and exits 0.
- Independent code review: [report](../reports/code-review-261024-1848-async-task-runtime-a1.md). Two High and four Medium findings were fixed test-first; mutation checks confirm that the new tests fail without each fix.

### Deviations from the phase files, with reasons

| Change | Reason |
|---|---|
| The task directory is prepared lazily through `Config.PrepareDir` at the first output, not in `NewNativeAgent` | The eager version created one empty directory per session (265 in one test run) |
| `settings.EnsurePrivateDir` corrects a wrong mode of an owned directory instead of a refusal | Same rule as `ensureHome`; a refusal disabled task output for a `~/.ask` with mode 0755 |
| `settings.ResolveHome` is shared by the auth store and the task directory | One home resolution instead of two copies |
| A Wake notice during any active run also sets `wake` | A turn that ends with an error, an End decision, or a blocked admission claims nothing; the notice then gets exactly one further run, with no loop |
| A `Context` notice is claimed only with other input or after a tool turn, never alone at the end of a run | Otherwise the notice caused an extra model request, against the meaning of the policy |
| `Abort(KeepQueued)` with a queued Wake notice starts one run after the abort | `KeepQueued` keeps the wake flag, as it did before for input that arrived while a run closed |
| `Dispose` waits for a `Reset` that still closes its old registry | Dispose must not return while a task body runs |
| `tasks.Sweep` refuses a symbolic-link root and removes only session-shaped directories file by file; the app checks home and root before it sweeps | A linked or user-set tasks root could lose user data |
| `internal/acp` and `cmd/tui` tests run with an isolated `ASK_HOME` | Tests must not read or sweep the real home |

### Carried to later phases

- A2 and A3: ACP binds the first event with a run ID to the client call (`internal/acp/host.go` `admit`); a run that a notice starts must not be attributed to a client call.
- A3: each task event on an idle Agent runs a listener dispatch, during which `Prompt` and `Reset` return `ErrBusy`; `task_update` once a second makes this more frequent (ACP replacement of the busy poll in A2 addresses the caller side).
- A3: task IDs restart for a reloaded session ID; with a reused directory, `O_EXCL` would refuse an old `<id>.log`.
- `NativeAgentConfig.Env` is not authoritative for `ASK_HOME` in the auth store and the task directory alike; change both together if needed.
