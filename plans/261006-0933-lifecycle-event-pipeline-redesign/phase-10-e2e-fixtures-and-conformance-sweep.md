---
phase: 10
title: "End-to-end fixtures and final conformance sweep"
status: done
priority: P1
effort: 10h
dependencies: [phase-09]
---

# Phase 10: End-to-end fixtures and final conformance sweep

## Goal

Prove the whole design through the real entry point, and prove that every conformance-matrix row has a named Ask test that exists, passes, and checks what its "Ask assertion" cell says.

## Context links

- Design section 15; `tasks/lessons.md` (real code path, no overridden production values)
- Real entry: `cmd/tui/headless.go` `newHeadlessAgent` (:205), `runHeadless` (:295); injected transport: `cmd/tui/headless_fault_test.go:24-37`; extra tools: `newHeadlessAgent(o, getenv, extra...)`; retry wait: `options.wait` (phase 08)
- Matrix: `conformance-matrix.md`; script: `check-conformance-matrix.sh` (phase 00, run at the end of every phase)

## Files to Create / Modify

- Create: `cmd/tui/headless_lifecycle_test.go` (end-to-end fixture), `internal/agent/failure_fixture_test.go`
- Modify: `conformance-matrix.md` (final status and the ticked "Assertion review" list), `docs/testing-llm-cassettes.md` (new test layers)

## Tasks & Steps

1. **End-to-end fixture** `TestHeadlessRetryToolsSteeringFollowUpAndSlowListener`: `newHeadlessAgent` with the Token Plan provider, an injected transport that answers 429 (with `Retry-After: 1`), then a tool-call response with two calls of an injected slow tool that declares itself concurrency-safe and one undeclared (exclusive) tool, then final answers; a recording `wait`. Run `runHeadless` in JSON mode. A goroutine calls `ag.Steer` when it reads the first `tool_execution_start`, and `ag.FollowUp` before the first cycle ends. A second listener sleeps on every event. Assert: one failed and one successful attempt in one turn; the Pi retry sequence (`agent_end{willRetry:true}`, `auto_retry_start`, `auto_retry_end{success:true}`); the recorded delay is 1 s; safe bodies overlap within the limit; the exclusive tool runs alone; steering is in the next turn's request; the follow-up opens a second cycle; the JSON output has every event in order and ends with `agent_settled`; exit 0.
2. **Failure fixture** `TestWriteFailureWithUncertainToolOutcomeRunsNoToolAgain`: `agent.New` with the faux provider, a counting tool, and `Config.NewContext` returning a writer that fails when the first tool result is appended (the `ToolCall` entry before it succeeds). Assert: the run returns the write error; repair marks the recorded call `outcome unknown`; the tool counter is 1; a later `Prompt` does not run the tool again.
3. **Signal fixture** `TestHeadlessSIGINTDuringToolBatchDisposesAndExits130`: SIGINT while a cooperative tool runs: the JSON stream has `cycle_end{aborted, cause: disposed}` and ends with `agent_settled` (D18; the JSON writer does not write `agent_disposed`); a second Go listener gets `agent_disposed` after `agent_settled`; exit 130; the tool body returned before `Dispose` returned (D19, D29).
4. **Real provider path:** one cassette test through `runHeadless` with a tool round trip. Re-record only if a request body changed on purpose, and name the phase that changed it.
5. **Final sweep:** `check-conformance-matrix.sh all`: every name exists (`go test -list`), every named test passes, and every row in "Assertion review" is ticked.
   Tick a row only after reading the Go test and confirming that it checks the assertion sentence. <!-- red-team #12 -->
6. PR text: list which parts are real and which are injected (transport, writer, extra tools, retry wait).

## Tests

```sh
go test -run 'TestHeadlessRetryToolsSteeringFollowUpAndSlowListener' -race -count=5 ./cmd/tui
go test -run 'TestWriteFailureWithUncertainToolOutcomeRunsNoToolAgain' -race -count=5 ./internal/agent
go test -run 'TestHeadlessSIGINTDuringToolBatchDisposesAndExits130' -race -count=5 ./cmd/tui
bash plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh all
go test -race ./...
go test ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
go run ./cmd/tui -p "echo hi"          # real binary path, exit 0
go run ./cmd/tui -p "hello" --mode json # real binary path, ends with agent_settled
```

## DeepSeek conformance rows covered

See the matrix Phase column (rows with Phase `10`); the script checks all rows.

## Risks & rollback

- Flaky timing in the fixture. Mitigation: synchronise on events, not sleeps; the retry wait is recorded, not slept; bound only the slow-listener delay; `-count=5`.
- Matrix drift after later changes. Mitigation: run the script before each later change to `internal/agent` until H8 closes.
- Rollback: tests and documents only; reset to tag `lifecycle-p10-base`.

## Done criteria

- The three fixtures pass five times in a row under `-race`.
- The script passes with zero missing names and zero unticked assertion rows; every N/A row has a reason.
