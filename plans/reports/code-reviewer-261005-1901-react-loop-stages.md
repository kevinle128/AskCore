# Code review: ReAct loop stages + pipeline.Compose

Scope: `git diff -- internal/agent internal/pipeline` + new loop_stage.go, loop_stage_test.go, agent_compose_error_test.go, hooks_compose.go, hooks_compose_test.go. Old code compared with `git show HEAD:internal/agent/loop_run.go` and the HEAD versions of loop_tools.go, loop_stream.go, agent.go, context_source.go.

## Verdict
The loop is a pure refactor. I traced each path against HEAD: event order, poll points, hook order and error returns are the same. I found no Critical or High issue. The findings below are Medium/Low and are mostly about Compose edge behavior and the compose-error path.

## Equivalence trace (verified, no diff)
- First turn / later turn: `turns > 0` (loop_stage.go:71) matches old `lastCompletedTurn != nil`. Both are set before FinishTurn, and a FinishTurn error ends the run on both sides.
- Failed message (error/aborted): old path was finishTurn (decision ignored), then TurnEnd, then AgentEnd. New path: act/observe skip, then decide calls finishTurn, emits TurnEnd, returns flowEndRun, then endRun. The result list is `[]` (reset in steer, loop_stage.go:88). Same.
- End decision: TurnEnd, then AgentEnd, no poll. Same.
- Continue decision: old cleared `explicitContinuation` when tools or steering were pending. New code returns flowNextTurn first (loop_stage.go:181-183), so flowIdleContinue is only reached when nothing is pending. Follow-ups take priority (loop_run.go driver). Same.
- Follow-up / continue re-entry: steer polls again only when `l.pending` is empty. On flowIdle, `l.pending` is always empty (decide checked it). Same as old.
- Tool executor: the truncated/sequential/parallel choice and its order are the same. `ctxView` is now taken in newBatchRun. No mutation happens between the old point and the new point, so the view is the same.
- apiKey: nil hook or empty key falls back to `Options.APIKey`. Same.
- projectContext through chainPrepare: the user hook sees the projected context. The user update fields win, and a nil Context or nil update keeps the projection. Same as the old wrapper. When the user has no PrepareRequest, the projection func passes through as-is.
- AfterToolCallResult.Apply: field order and the StructuredContent clear rule are the same as the removed inline code.

## Medium

M1. concatQueues drops messages it already took when a later source fails. hooks_compose.go:265-275.
Scenario: source A is a dequeue-on-read queue and returns [m1]. Source B then returns an error. The chain returns `nil, err`. m1 is already gone from A, so it is lost and no consumer sees it. With one source, a poll error cannot lose messages that way. The run ends with the error either way (Compose is strict, a locked decision), so the impact is limited to losing the queued input. Options: document it on Compose, or have the chain return the partial `all` with the error (the loop drops it on error, so you would also need the loop to keep it). At minimum, add one line of doc.

M2. The compose-error path skips agent_settled, but the comment on execute says "agent_settled on every path". agent.go:169-180.
On the early `return err`, no AgentSettled is emitted. A listener that waits for agent_settled to mark a run done would hang (the run handle is still ended by `defer a.end(r)`, and the test covers that). In production this path cannot happen: `a.cfg.Hooks` is one Hooks value, so it can hold at most one ConvertToLLM, and projectContext sets none. So the issue is the misleading comment plus a test-only seam. See L1 for a simpler shape.

## Low

L1. The mutable package var `composeHooks` exists only to reach dead code. agent.go:14-17, agent_compose_error_test.go.
Compose runs on every run, but its inputs (fixed projectContext + the fixed cfg.Hooks) cannot fail. A simpler option: call Compose once to validate in `New` (return the error there), or document that the error is unreachable and drop the var and the test. The var also creates a data race if any agent test calls t.Parallel later (none do today, as the comment says). This is a question, not a blocker.

L2. chainBefore does not check ctx between hooks. hooks_compose.go:192-210.
After a cancel, hook 2..n still run, and the loop only checks `b.ctx.Err()` after the whole chain (loop_tools.go:296). The final outcome is still textAborted, so results are correct. The only cost is extra hook side effects (for example, an audit hook logs a call that never runs). Same note for chainAfter.

L3. chainBefore drops Reason/Terminate from a non-Block result. hooks_compose.go:205-212. The loop reads them only when Block is true, so behavior is the same. No action needed beyond knowing it.

L4. allFinish uses `max` and relies on the order Proceed < Continue < End (hooks_compose.go:185, pipeline/hooks.go:26). An out-of-range value (for example `TurnDecision(9)`) wins the max and the loop treats it as Proceed. The single-hook path behaves the same, so this is consistent. Optional: a test that pins the constant order.

L5. The runTurn panic (loop_stage.go:61) can fire only on a programming error. TestTurnStagesOrder pins the order. Acceptable.

## Aliasing / identity / panics
- Identity: when only one input sets a point, that function is returned as-is. hooks_compose_test.go TestComposeIdentity covers it.
- Aliasing: chainPrepare copies `*upd.Context` into the local `r`, and `merged.Context` keeps the hook's pointer. Later hooks get a copy of the struct, but the Messages backing array is shared, as in the old wrapper. No new aliasing.
- chainAfter: `merged.IsError`, `merged.Usage` and `merged.Terminate` alias the hook's pointers. That is harmless unless a hook mutates its own result after it returns (the old code had the same property).
- Panics: tool-hook chains run inside prepare/execute, which recover, so a panic in any chained tool hook becomes an error result. A panic in the request/turn hooks reaches runGuarded. Same as before.

## Concurrency
No new goroutines. The stages run on the loop goroutine. `l.ts` and `l.pending` are touched only there. The executor structs are stateless. No issue.

## Tests
- hooks_compose_test.go covers each merge rule, identity, ErrMultipleConvertToLLM and Apply. It has no test for M1 (partial loss) or for ctx cancel mid-chain.
- loop_stage_test.go checks the stage order, the first-turn steer, executor selection and the decide flow table. These are real assertions, not phantom tests.

## Comments / plan refs
I grepped the new files for phase/finding/plan IDs and found none. "Pi" references are external design citations, which is fine.

## Complexity
The flow enum has 5 values. flowNext is used only inside runTurn, and flowIdleContinue could be a bool, but the enum reads clearly. The stage interface `name()` is used only in the panic message and the order test. This is acceptable and matches the locked decision.

## Unresolved questions
1. M1: should a failed multi-source poll keep the messages it already took, or is "strict, lose them" accepted?
2. L1: is compose-per-run with a test seam preferred over validating once in New?

Status: DONE_WITH_CONCERNS
