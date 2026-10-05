---
title: ReAct loop split into internal stages
date: 2026-10-05
summary: "loop.run became six unexported ReAct stages with a flow enum, a tool executor strategy, single hook seams and pipeline.Compose; behavior unchanged"
---

# ReAct loop split into internal stages

## What happened
- The agent loop (`internal/agent/loop_run.go`) was a direct port of Pi `runLoop`: two nested loops and three flags. It was hard to read and hard to extend for H9 (retry), H10 (compaction) and H11 (extensions).
- We studied the stage pipeline of dewee (commit 815cd9ea, `internal/pipeline/stage.go`, `pipeline.go`) and took its structure, but not its 53-closure `PipelineDeps` or its large `RunState`.
- New `loop_stage.go`: `stage` interface, `flow` enum (next, nextTurn, idle, idleContinue, endRun), `turnState`, six stages (steer, prepare, reason, act, observe, decide). The driver `run()` is about 30 lines.
- `toolExecutor` strategy: truncated, sequential, parallel, over the existing `batchRun`.
- Each hook point is read in exactly one loop method (9 seams).
- New `pipeline.Compose(hs ...Hooks) (Hooks, error)`, with merge rules from Pi (`agent-session.ts`, `runner.ts`). More than one `ConvertToLLM` is rejected. `AfterToolCallResult.Apply` is the only copy of the override rule.

## Verification
- Full `go test ./...` and `-race` on agent/pipeline pass; lint 0 issues; existing tests not edited.
- JSONL of `ask -p --mode json` for `echo hi`, `hello`, `fail boom` is identical to the baseline (without ts, runId, timestamp, sessionId); exit codes unchanged. Steering, follow-ups, Continue, StopLength, parallel and abort are covered by existing unit tests, not by the JSONL diff.
- A FinishTurn short-circuit mutation made `TestComposeFinishTurn` fail.

## Review fix
- A `Compose` error skipped `agent_settled`. It now takes the same failure path as a loop error. The test-only package variable `composeHooks` and its test were removed.
- Documented that a failed multi-source queue poll drops messages already taken in that poll; the run ends with the error anyway.

## Decision
- Stages stay unexported in `internal/agent` (user choice A). Public extension is `pipeline.Hooks` plus `Compose`. Later the H11 dispatcher becomes an adapter file in `internal/pipeline`.

## Next steps
- Commit in plan order (no commits made; the tree has unrelated H3/H4 work).
- Deferred: MaxTurns (H9), compaction stage (H10), dispatcher adapter (H11).

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
