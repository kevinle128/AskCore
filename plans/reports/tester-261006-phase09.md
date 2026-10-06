# Phase 09 independent test report

## Summary

Status: DONE.
The scope is the complete phase 09 plan with `--tdd --auto`.
The controller confirmed source completion and simplifier completion before final gates started.
All independent focused gates, build, and configured lint passed.
The full ordinary and affected race suites passed.
The reviewer then found an explicit Cancel-and-Block precedence regression, followed by pointer-copy, repair, and asynchronous middleware drain defects.
The source owners corrected those defects, and all fresh independent affected tests, race, configured lint, and build gates passed.
Matrix09 passed after final hand review and the controller's tick notice.
All final acceptance gates have 0 failures.

## Scope and inputs

The work context is `/Users/dale/orca/workspaces/AskCore/master-2`.
The source baseline is `/private/tmp/askcore-lifecycle-phase08-baseline`.
Checks ran on 2026-10-06 with Go 1.27.0 on Darwin arm64.
The test agent read AGENTS.md, the complete phase 09 plan, all 34 phase 09 matrix rows, and the support implementation and review reports.
The test workflow uses the previously loaded `ak:test` skill, its test execution and report references, and the project organization path rules.
Only this report is owned by the test agent.
No source, test, cassette, generated file, manifest, changelog, matrix, or project status edit is permitted.
All provider checks must disable paid live calls and cassette recording.

## Commands and results

Each independent command below used the prefix `env ASK_LIVE=0 ASK_LIVE_OPENAI=0 ASK_RECORD=0`.
The controller's source completion notice arrived before any final gate ran.
The test agent waited for the reviewer to verify all 34 rows and the controller to tick them before running Matrix09.

| Gate | Exact command | Status |
|---|---|---|
| Focused coordinator, abort, repair, and context | `go test ./internal/agent/... ./internal/tools/... -run 'Tool\|Abort\|Repair\|Pairing\|Concurrency\|Context\|Cancel\|Execute' -count=1` | Exit 0; agent 1.793 s, tools 0.395 s. |
| Focused support copy contracts | `go test ./internal/pipeline ./internal/sessions ./pkg/protocol -run 'AfterTool\|ToolCall\|Clone.*Pointer' -count=1` | Exit 0; pipeline 0.461 s, sessions 1.199 s, protocol 0.830 s. |
| CLI abort and tool cassettes | `go test ./cmd/tui -run 'PrintExitsOneAfterAbortDuringToolBatch\|HeadlessMathToolsDeclareConcurrencySafe\|Cassette.*ToolCall' -count=1` | Exit 0; cmd/tui 1.065 s. |
| Repeated race | `go test -race -count=20 -timeout=90s ./internal/agent -run 'Tool\|Abort\|Cancel'` | Source-owner run passed before final review in 23.237 s with exit 0; `/tmp/askcore-phase09-race-final.txt`. |
| Full ordinary suite | `go test ./... -count=1` | Exit 0; cmd/tui 33.978 s, agent 4.128 s; session 84603. |
| Full affected race | `go test -race ./internal/... ./cmd/tui ./pkg/... -count=1` | Exit 0; cmd/tui 83.559 s, agent 9.187 s; session 94815. |
| Build | `go build ./...` | Exit 0; no output. |
| Configured full lint | `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` | Exit 0; 0 issues. |
| Final correction regressions | `go test ./internal/agent ./internal/pipeline -run 'ExplicitToolCancel\|Signature\|Pointer\|PersistentWriter\|Inflight\|AsyncNext\|HandlerFailureCancels\|AcceptedNext\|HandlerError' -count=1` | Exit 0; agent 1.362 s, pipeline 1.671 s. |
| Final corrected agent and pipeline | `go test ./internal/agent ./internal/pipeline -count=1` | Exit 0; agent 3.457 s, pipeline 0.515 s. |
| Final corrected agent and pipeline race | `go test -race ./internal/agent ./internal/pipeline -count=1` | Exit 0; agent 5.719 s, pipeline 3.020 s. |
| Final corrected full lint | `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` | Exit 0; 0 issues. |
| Final corrected build | `go build ./...` | Exit 0; no output. |
| Matrix09 | `bash plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh 09` | Exit 0; 34 rows, 34 ticked, every named test found and passed. |

## Test inventory

A static scan found 34 phase 09 rows with their named tests in coordinator, control, abort, pre-abort, repair, context, existing tool, provider transform, and support test files.
The initial incomplete source lacked `TestPrintExitsOneAfterAbortDuringToolBatch`, which row 50b requires.
The support owner added that real headless acceptance test before the completion notice.
The independent CLI focused gate ran and passed it with the math and tool cassette tests.
All named row tests are now present.
This inventory proves name presence only; it does not prove assertion coverage or passing execution.
The controller received the missing test finding before final gates.

## Process control

No test, server, watcher, or detached process was started during preparation.
Final runs use tracked tool sessions and report command exits.
The ordinary process was PID 28454, session 84603, and the full affected race process was PID 28453, session 94815.
The first lint process was PID 28455, session 22367, and completed with exit 0.
The build session 94303 completed with exit 0.
The controller supplied the completed source-owner repeated-race result from session 91872.
The test agent did not duplicate that gate.
The initial repeated-race result predates the assistant-copy and pointer-tool-call correction.
The later required group passed in 23.237 s after those changes.
After all review corrections, the source owner's expanded relevant race group with count 20 passed in 24.680 s, with output at `/tmp/askcore-phase09-review-correction-race.txt`.
The independent full agent and pipeline race run verified the final corrected source.
Only processes started by this test agent may be stopped by it.
The full ordinary suite ran once, with affected repeats after the source corrections created a concrete need.

## Pre-final failure evidence

The controller reported that the later repeated-race run in session 23946 stalled in `TestCancelDuringAfterToolTakesEffectAfterHandlerReturns`.
The source owner captured a stack from the owned process.
The fixture waited for sibling pre-control rather than actual body invocation, so Abort could correctly skip that body while the fixture waited for a signal it could no longer receive.
The source owner changed the fixture to wait for actual body start before blocking AfterTool and repeated the required race check.
The exact stalled command was `go test -race -count=20 ./internal/agent -run 'Tool|Abort|Cancel'`.
The stack and failure output are at `/tmp/askcore-phase09-race-latest.txt`.
The source owner stopped its test process PID 97060 with SIGQUIT to capture the stack, and the owning go process PID 96646 exited with status 1.
The source owner then passed the single post-control fixture with race count 50 in 2.020 s and the required race count 20 group in 23.237 s.
These outcomes are recorded in `implementation-261006-phase09.md`.
The controller confirmed all source-owner processes completed.
No independent final gate ran on that intermediate source.

The later integration reviewer reproduced a `BeforeTool{Cancel:true, Block:true}` regression with `TestReviewExplicitCancelWinsOverBlock`.
The accepted existing contract gives explicit Cancel priority over Block.
The controller assigned a minimal decision-order correction while preserving a policy denial that settles after root Abort.
The completed full suite, full affected race, build, and first full lint evidence above precede that correction.
The controller then expanded the correction scope to call signature deep copies, legal pointer assistant/result-ID/repair reads, repair panic handling, and asynchronous-next body drain.
The support owner corrected the shared pipeline middleware next claim and completion join across normal return, handler error, and handler panic.
Fresh full agent and pipeline ordinary and race checks, focused new regressions, configured full lint, and build verified the changed surface after source completion.
The controller will require fresh full repository gates again in phase 10.

## Final correction evidence

The source owners promoted the review failures to permanent runnable tests before each correction.
The main implementation report records red output at `/tmp/askcore-phase09-review-correction-red.txt`, `/tmp/askcore-phase09-inflight-next-red.txt`, and `/tmp/askcore-phase09-inflight-failure-red.txt`.
The support implementation report records the accepted-next completion and failure-cancellation red cases.
The independent final focused gate and complete agent and pipeline suites passed after the controller confirmed the corrected source was frozen.
The complete agent and pipeline race suites, full configured lint, and full build also passed on that final source.
The full repository ordinary and affected race results above precede these final corrections, as the controller directed.
No independent gate failure occurred.

## Matrix and cleanup

The controller reported final review approval with 0 findings and all 34 assertions verified, then ticked only the phase 09 assertion list.
The test agent ran Matrix09 only after that explicit notice.
The script validated 301 total rows and reported `phase 09: 34 rows, 34 ticked`.
The exact named groups passed in cmd/tui (0.587 s), agent (0.865 s), and providers (0.201 s).
The test agent changed only this report.
All owned test, build, lint, and matrix sessions finished.
The final process check found no remaining test or lint child process.
No background server or detached process was started, and no paid provider call or cassette recording was enabled.
Coverage was not measured because this assignment requires acceptance gates rather than a coverage audit.
No further acceptance gate remains for this test assignment.

## Unresolved questions

None.
