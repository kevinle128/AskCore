# Phase 5: New tests

## Files
Create `internal/agent/loop_stage_test.go` (package `agent`, internal test, so it can reach unexported types). Existing test files are read-only. `loop_helpers_test.go` is package `agent_test` (verified line 1), so its helpers are not reachable; write the minimal helpers needed locally and keep them small.

## Tests
1. `TestDecideFlow`: table over (failed message, `moreTools`, steering returns a message, FinishTurn decision in Proceed/Continue/End). Expected flow per the phase 2 table, and expected events (`turn_end` always once). Includes: End with pending tools gives `flowEndRun` and no steering poll; Continue with `moreTools` gives `flowNextTurn` (continuation satisfied by tool results).
2. `TestSteerStageFirstTurn`: with `turns == 0`, no poll and no `turn_start`; with `turns > 0` and pending set, no poll but `turn_start`.
3. `TestToolExecutorSelection`: StopLength gives truncated (even with a sequential tool); a registry with a `tools.Sequential` tool gives sequential; else parallel.
4. `TestTurnStagesOrder`: names of `turnStages` equal `steer, prepare, reason, act, observe, decide`, so a reordering is a visible test change.

Names describe behavior; no plan IDs. Run with `-race`. `TestMain` with `goleak.VerifyTestMain` (`loop_helpers_test.go:24`) runs for the whole test binary, internal tests included, so no extra leak check is needed. Do not add a second `TestMain`.

Pipeline merge-rule tests live in phase 4 (`internal/pipeline/hooks_compose_test.go`); this phase adds only agent tests.

## Validation
`go test ./internal/agent/... -race -count=1 -run 'Decide|SteerStage|ToolExecutor|TurnStages'` green. Then confirm each new test fails if its rule is broken (temporarily flip one condition locally, then restore; do not commit the flip).

## Rollback
Delete the file.
