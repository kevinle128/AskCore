---
name: verify-tool-outcomes
description: TLA+ model check of the internal/agent tool batch (runToolBatch, repairTools). Use when a change touches tool_coordinator.go, tool_repair.go, loop_tools.go, or the error and repair path of Agent.execute, or to check D19 (one result per tool call), the exclusive-tool barrier, the parallel-tool pool, or the abort drain.
---

# Verify tool outcomes

**Promise.** Every tool call in an assistant message gets exactly one result in the session log, in call order, whatever happens: abort, a blocked call, a failed log write or event publish. A tool that is not concurrency-safe runs alone.

**Harm when broken.** A call without a result makes the next provider request invalid and the session unusable. Two results, or a result that says "not started" for a tool that ran, makes the model repeat a side effect such as a file write or a shell command. An exclusive tool that overlaps another body races on the user's files.

The check is a TLA+ model of one batch, `model/ToolBatch.tla`, run by TLC. It explores every interleaving of the coordinator goroutine, the worker goroutines, an `Abort` from another goroutine, and one failed write, so it reaches orders that the Go tests in `tool_abort_test.go` and `tool_coordinator_test.go` hit only by chance.

## Run

From the repo root:

```sh
python3 .omp/skills/verify-tool-outcomes/verify.py          # model check + drift check, about 25 s
python3 .omp/skills/verify-tool-outcomes/verify.py mutants  # each property fails on its break, about 20 s
```

Needs `java` 11 or newer and `python3`. The first run downloads tla2tools 1.7.4 to `~/.cache/tlaplus/` and checks its sha256. `TLA2TOOLS_JAR` overrides the path.

Exit codes of the default run:

- 0, `PASS` and `IN SYNC`: every property holds and no covered file changed since the sync commit.
- 1, `FAIL: <property>`: TLC found a counterexample. The trace is printed above the line.
- 3, `PASS` then `STALE`: the model passes, but covered code changed since the sync commit. The model may no longer describe the code, so a pass means nothing yet. Go to "When the code changed".
- 2: setup error (no java, wrong jar).

## Read a result with /statechart

Read every TLC result with the pstack `/statechart` skill. On a failure it gives the shortest path to the broken property in plain words. On a pass it draws how a batch moves between states. Tell the counterexample as steps in the code's words, such as "call 1 (safe) starts; call 2 (exclusive) starts while call 1's body runs".

Then decide what the trace is:

- **A real bug.** Every step in the trace maps to a line in the Coverage table, and the Go code can take those steps in that order. Report it with the trace in the code's words. Leave the property and the model as they are, and change product code only when the user asks.
- **A modeling error.** A step in the trace has no counterpart in the code, or merges two steps that another goroutine can separate in the code. Fix the model to match the code, then rerun. Never weaken a property to make it pass.

## When the code changed

Run this whenever `verify.py` says `STALE`, or a diff touches a file in the Coverage table.

1. Read `git diff <sync commit> -- <covered file>` for each listed file.
2. For each hunk, find the model action in the Coverage table that stands for it, and make the same change in the model: a removed guard is a removed conjunct, a moved write is a moved assignment, a new failure point is a new `FailTo` branch. A change that no action covers either is outside the model (say so) or needs a new action.
3. Run `verify.py`. A `FAIL` on a mirrored change is the change breaking the promise: report the trace. A pass covers the change only within the Scope below.
4. Run `verify.py mutants`. Every line must say `caught`. A `BROKEN MUTANT` line means the model text a mutant edits moved; update the `old` string of that entry in `MUTANTS` in `verify.py` to the new model text, keeping the same break.
5. When the change is intended and the model now describes the code at the new commit, set the sync commit below to that commit. Move it forward only after steps 1 to 4 pass for that commit, so the record keeps saying where the model was last confirmed.

## Coverage

Sync commit: `de61a4d9ef0300937131470dd8de3b74a5a7cae7`

| Model action or variable | Code |
|---|---|
| `Collect`, `AcceptHead` | `internal/agent/tool_coordinator.go`: `runToolBatch`, the non-blocking `select` on `done` and `accept` |
| `CommitNext`, `CommitDone` | `runToolBatch`, the `for committed < next && slots[committed].ready` loop: `b.progress`, `b.afterTool`, `l.emitToolEnd`, `l.emitMessage`; then `if committed == len(calls) { break }` |
| `Prepare` | `b.prepare`: `b.commit(sessions.ToolCall{...})`, `emitToolStart`, the `b.ctx.Err()` checks, `Pipeline.BeforeTool` (allow, block, cancel) |
| `Resume`, `NoStart`, `Dispatch` | the start block of `runToolBatch`: `next < len(calls) && !exclusive && active < limit`, `l.ctx.Err()`, `concurrencySafe`, `!s.exclusive \|\| (active == 0 && committed == next)` |
| `WaitDone`, `WaitNotify` | the blocking `select` at the end of the loop body; `b.progress` publish failure |
| `Drain`, `Return`, `FailTo` | the deferred `cancel()` and `for active > 0 { <-done }`; every `return batch, err` |
| `WorkerCheck`, `WorkerReturn`, `WorkerProgress` | `b.execute`: the `once.Do` ctx check, `p.tool.Execute`, the `textAborted` swap; `updater.send` and `b.notify` |
| `afterTool` swap inside `CommitNext` | `b.afterTool`: `b.ctx.Err() != nil && !o.isError` becomes `textAborted` |
| result texts | `internal/agent/loop_tools.go`: `textAborted`, `textBlocked`, `textBeforeDispatch`, `errorOutcome`, `toolResultMessage` |
| `Repair` | `internal/agent/tool_repair.go`: `repairTools`, "not started" or "outcome unknown" per `(assistant entry, call ID)` |
| `pc' = IF failed THEN "repair"` | `internal/agent/agent.go`: `Agent.execute` calls `repairTools(r.source, repairStart)` when the run returns an error |
| a run error from the batch | `internal/agent/loop_stage.go`: `actStage.run` returns `flowEndRun, err` |
| `Limit` | `Config.MaxParallelTools` (default 10) |

## Properties

| Property | Plain sentence |
|---|---|
| `PoolBound` | At most `MaxParallelTools` tool bodies run at once. |
| `ExclusiveAlone` | A tool that is not concurrency-safe runs with no other body. |
| `ExclusiveBarrier` | An exclusive call starts after every earlier call is committed, `AfterTool` included, and no later body starts before it is committed. |
| `RecordBeforeBody` | The `ToolCall` entry is in the log before the body runs (matrix B2). |
| `AtMostOneResult` | No call gets two results. |
| `ResultsInSourceOrder` | Results are in the log in call order. |
| `TruthfulOutcome` | "aborted before dispatch" and "not started" mean the body never ran; "not started" means no `ToolCall` entry; "outcome unknown" only for a recorded call. |
| `NoBodyAfterAbort` | Once `l.ctx` is cancelled, no tool body starts; so an abort before the batch runs no body (matrix 50). |
| `DrainedOnReturn` | When `runToolBatch` returns, every started body has returned (D19, no time bound). |
| `EveryCallAnswered` | After the batch, and after repair on error, every call has a result. With `AtMostOneResult`: exactly one. |
| `Terminates` | The batch always ends, given that every tool body returns. TLC also checks that no state is stuck. |

## Scope

- **Bounds.** 4 calls, `MaxParallelTools` 2, every split of the calls into safe and exclusive, an abort at any point, at most one failed write or publish. About 1.5 million states. `verify.py mutants` uses 3 calls.
- **Atomic steps.** Each driver step in the model is one stretch of the driver goroutine with no shared-state read that another goroutine can change in between, except where noted. `Prepare` merges the two `b.ctx.Err()` checks around `BeforeTool` into one; an abort between them gives the same "aborted before dispatch" text, so the properties see no difference. `WorkerCheck` makes the worker's ctx check and the start of `p.tool.Execute` one step. In Go an abort can land after the `bodyCtx.Err()` check and before `Execute`; that body starts with a context that is already cancelled. `NoBodyAfterAbort` holds for the model, and for Go it means "no body starts after the worker saw the abort".
- **Assumptions.** Call IDs are unique in the session (duplicate and reused IDs get an error result in `prepare` and are covered by Go tests, not here). The turn of the batch is open and was opened by this run, so `repairTools` acts. Repair's own appends succeed; when the writer keeps failing, repair fails too and the run returns the joined error (matrix 51), which this model leaves out. Every tool body returns eventually (the D19 tool duty); `Terminates` depends on it.
- **Not modeled.** Argument validation and `ArgumentPreparer` (they behave as a block), the `ExecuteTool` middleware short circuit, `AfterTool` result rewrites and added context, progress payloads, `shouldTerminate`, the aborted assistant message after the batch, and everything outside one batch: queues, retries, the next turn.
- **Guards that the model shows to be redundant.** At the sync commit, removing only `active == 0` or only `committed == next` from the exclusive start condition keeps every property (checked at 4 calls): between the collect and the start block, no body active means every earlier call is ready, and the commit loop has committed it. The `l.ctx.Err()` check in the start block is also covered by the worker's own check (checked at 3 calls). A pass after such a removal is a correct result, not a model gap. Removing both exclusive guards, or the worker's check, fails.
