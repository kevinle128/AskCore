# Phase 10 independent test report

## Summary

Status: DONE.
The scope is complete phase 10 validation with `--tdd --auto` and no scope reduction.
The build, actual print and JSON CLI commands, agent write-failure fixture with race count 5, and matrix structure dry run passed.
Full ordinary and race suites passed.
All final support checks, new assertion checks, and configured full lint passed after the support and main source freeze notice.
The final matrix all gate passed after all assertion review ticks.
All acceptance gates finished with exit 0 and no independent failures.

## Scope and inputs

The work context is `/Users/dale/orca/workspaces/AskCore/master-2`.
Checks ran on 2026-10-06 with Go 1.27.0 on Darwin arm64.
Only this report is owned by the test agent.
The test agent read AGENTS.md, the full phase 10 plan, the 15 phase 10 matrix rows, the main implementation report, and the matrix script usage and behavior.
The loaded `ak:test` workflow and report instructions apply.
Every independent execution explicitly uses `env ASK_LIVE=0 ASK_LIVE_OPENAI=0 ASK_RECORD=0`.
No paid provider call or cassette recording is enabled.
No source, test, cassette, documentation, project status, matrix, generated file, or commit change is permitted.

## Commands and results

Every command in this table uses the environment prefix above.

| Gate | Exact command | Exit and result |
|---|---|---|
| Full ordinary suite | `go test ./... -count=1` | Exit 0; cmd/tui 35.324 s, agent 3.625 s; session 18221, PID 35351. |
| Full race suite | `go test -race ./... -count=1` | Exit 0; cmd/tui 82.681 s, agent 11.908 s; session 29729, PID 35352. |
| Build | `go build ./...` | Exit 0; no output. |
| CLI help | `go run ./cmd/tui --help` | Exit 0; confirms print, JSON mode, and faux as default provider. |
| Actual print entry | `go run ./cmd/tui -p 'echo hi'` | Exit 0; stdout `hi`. |
| Actual JSON entry | `go run ./cmd/tui -p 'hello' --mode json` | Exit 0; 16 JSON events in sequence, last event `agent_settled`. |
| Write failure fixture | `go test -run '^TestWriteFailureWithUncertainToolOutcomeRunsNoToolAgain$' -race -count=5 ./internal/agent` | Exit 0; initial 9.815 s, final frozen-source repeat 2.476 s. |
| Lifecycle fixture | `go test -run '^TestHeadlessRetryToolsSteeringFollowUpAndSlowListener$' -race -count=5 ./cmd/tui` | Exit 0; 4.731 s. |
| Signal fixture | `go test -run '^TestHeadlessSIGINTDuringToolBatchDisposesAndExits130$' -race -count=5 ./cmd/tui` | Exit 0; 2.394 s. |
| Cassette tool round trip | `go test ./cmd/tui -run '^TestCassette(OneToolCall\|ParallelToolCalls)$' -race -count=1` | Exit 0; 2.937 s; replay only. |
| Tools new and affected tests | `go test ./internal/tools -race -count=5` | Exit 0; 2.141 s. |
| Shipping faux retry composition | `go test ./cmd/tui -run '^TestHeadlessPrintRetriesTransientFailureOnce$' -race -count=5` | Exit 0; 1.779 s. |
| Second append assertion | `go test ./internal/agent -run '^TestWriteFailureStopsFurtherSideEffects/second_append$' -count=1` | Exit 0; 0.682 s. |
| Second append assertion race | `go test ./internal/agent -run '^TestWriteFailureStopsFurtherSideEffects/second_append$' -race -count=5` | Exit 0; 1.782 s. |
| Default Fantasy usage fold | `go test ./internal/providers/fantasykit -run '^TestFoldUsageUsesZeroBucketsAndDerivedTotal$' -count=1` | Exit 0; 0.379 s. |
| Default Fantasy usage fold race | `go test ./internal/providers/fantasykit -run '^TestFoldUsageUsesZeroBucketsAndDerivedTotal$' -race -count=5` | Exit 0; 1.706 s. |
| Final configured lint | `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` | Exit 0; 0 issues. |
| Matrix structure | `bash plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh --dry-run` | Exit 0; structure and review lists consistent. |
| Matrix all | `bash plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh all` | Exit 0; 197 testable assertions ticked, all named tests found and passed, 104 N/A rows valid. |
| N8 wakeup race | `go test -race -count=200 ./internal/agent -run '^(TestSteerRacingRunEndIsDeliveredOrRejected\|TestSteerOnIdleAgentStartsRun)$'` | Exit 0; 2.306 s. |

## Matrix state

The dry run checked 301 total rows: 157 follow, 31 deliberate divergence, 9 Ask-new, and 104 N/A.
The 197 non-N/A assertions belong to phases 01 through 10.
Phases 01 through 09 were ticked, but phase 10 had 15 rows and 0 ticks at dry-run time.
The dry run proves structural consistency only and does not prove final assertion approval or passing named tests.
The final all gate ran only after the controller reported final hand review and tick completion.
Its parser checked all 301 rows and reported all 197 testable assertions ticked across phases 01 through 10.
Every named package group passed, with no missing test or unticked assertion error.
The matrix CLI group passed in 10.819 s, agent in 2.510 s, app in 0.631 s, bus in 0.205 s, and pipeline in 0.236 s.
The provider groups passed in 0.219 s, Anthropic in 0.405 s, Fantasy in 0.224 s, and OpenAI in 0.345 s.
Sessions passed in 0.224 s, tools in 0.192 s, and protocol in 0.197 s.

## Real and injected paths

The two CLI commands use the actual `go run ./cmd/tui` entry and shipped faux default provider.
They inject no transport, writer, tool, or wait override.
The write-failure fixture uses the real Agent, ordered tool coordinator, log, repair, and later Prompt path with the shipped faux provider.
It injects a counting tool and a commitGate writer that fails the first result append but allows intent and repair writes.
The support lifecycle and signal fixtures use the real headless construction, adapter, run, event, and disposal paths with injected HTTP fixture transport and extra cooperative tools.
The lifecycle fixture also records the production retry wait and adds a slow listener.
Its durable companion uses the real provider registry, Agent, app-composed auth, and MemoryLog to check Ask turn identity separately from Pi retry turn_start projections.
The signal fixture puts `os.Interrupt` on the signal channel passed to runHeadless; it does not send an operating-system signal to a separate process.
The faux retry fixture directly composes shipped registry, Agent, tools, and headless components because the shipping faux constructor installs a fixed demo script on each request.
This one fixture therefore does not claim to inject a transient failure through newHeadlessAgent's fixed faux branch.
The cassette gate uses the real adapter request and tool round trip with recorded responses in replay mode.
No real network inference, paid call, database, crash recovery, or filesystem failure is claimed by these fixtures.

## Process control

All runs use tracked tool sessions; no detached process or background server was started.
The ordinary and race processes above belonged to this task and finished with exit 0.
The build, CLI help, print, JSON, and write-failure fixture sessions finished with exit 0.
The test agent will not stop another session's or user's process.

## Review corrections and final affected gates

The reviewer found that matrix row 67 requires a writer failure on the second append, while the existing named test covered only assistant append failure.
The controller assigned the missing assertion to the source owner.
The broad passing results above precede that test correction and the remaining support fixtures.
The final source changes add assertion coverage and fixtures; no production change occurred after the broad gates.
All support-focused checks, the three exact fixtures with race count 5, and configured full lint now passed on the frozen source.
Matrix all also passed after final hand review.
The controller requested an interim handoff after the N8 wakeup race check to free an agent slot for the correction.
The N8 check passed, and every tool session started by this test agent has finished.
The controller sent the corrected source and support freeze notice, and independent final affected validation resumed.
The second-append case checks the actual second writer Append, returned error, no model or tool invocation, and terminal agent_settled.
The Fantasy fold case checks the real fold and assembler, zero omitted cache buckets, optional nil reasoning, and derived totals despite a supplied provider total.
The SL19 matrix mapping was updated by the controller to this actual fold assertion.
No independent test or lint failure occurred.
The source-owner and reviewer lifecycle runs had an initial logical-turn assertion failure because Pi emits retry turn_start events within one durable Ask turn.
The support owner retained the real-constructor wire fixture and added the durable-log companion to assert turn identity at the correct contract boundary.
The failure and correction are recorded in `implementation-261006-phase10-support.md` and `review-261006-phase10.md`.
The final independent five-run lifecycle race check passed with both paths.

## Completion and limits

The final reviewer approved all 197 testable assertions and all 104 N/A reasons with 0 open findings before the controller ticked phase 10.
The three planned fixtures each passed with race count 5 on the final frozen test source.
Full repository ordinary and race gates, build, real CLI print and JSON entries, cassette tool replay, N8 race count 200, final affected assertions, and full configured lint all passed.
The broad gates preceded final test-only additions; the frozen-source affected checks and matrix all covered those additions.
No production change occurred between those runs.
No cassette body changed or cassette recording was made.
Coverage was not measured because the assignment requires acceptance gates rather than a coverage audit.
All test, lint, build, and matrix sessions owned by this agent finished.
No remaining owned process or further acceptance gate remains.

## Unresolved questions

None.
