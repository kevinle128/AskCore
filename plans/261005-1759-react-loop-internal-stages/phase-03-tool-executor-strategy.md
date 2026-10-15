# Phase 3: Tool executor strategy

## Files
- Modify `internal/agent/loop_tools.go` only, and the act stage call site in `internal/agent/loop_stage.go` (one line; same owner sequence, phase 2 is done).

## Design

```go
// toolExecutor runs all tool calls of one assistant message.
type toolExecutor interface {
    run(l *loop, assistant protocol.AssistantMessage, calls []protocol.ToolCall) (toolBatch, error)
}
type truncatedExecutor struct{}  // body of failTruncatedToolCalls (loop_tools.go:54)
type sequentialExecutor struct{} // newBatchRun(...).sequential(calls)
type parallelExecutor struct{}   // newBatchRun(...).parallel(calls)

func (l *loop) toolExecutor(assistant protocol.AssistantMessage, calls []protocol.ToolCall) toolExecutor
```

Selection rules, unchanged from today:
1. `assistant.StopReason == protocol.StopLength` gives `truncatedExecutor` (from `loop_run.go:162`).
2. Any call whose tool implements `tools.Sequential` and returns true gives `sequentialExecutor` (from `executeToolCalls`, `loop_tools.go:73-83`).
3. Else `parallelExecutor`.

`batchRun.sequential`, `parallel`, `runJobs`, `prepare`, `execute`, `afterToolCall`, `updater` stay as they are; the executors are thin wrappers. A small `newBatchRun(l, assistant)` builds the `batchRun` with `ctxView: l.context()` and an empty `seen` map, so both executors share it (DRY). `failTruncatedToolCalls` and `executeToolCalls` are removed once the act stage calls `l.toolExecutor(msg, calls).run(l, msg, calls)`.

Important: `ctxView` is taken when the executor runs, after observe of the previous turn, same moment as today. Lookup for selection uses the same registry (`l.ac.Tools`).

## BeforeToolCall seam
Extract the hook block of `batchRun.prepare` (`loop_tools.go:239-260`) into `(*batchRun).beforeToolCall(c, args) (json.RawMessage, *toolOutcome)`, the single place that reads `b.cfg.Hooks.BeforeToolCall`. Keep the abort checks before and after the hook and the block, `Terminate` and `Args` handling exactly as today. It stays inside `prepare`'s deferred recover (`loop_tools.go:222-226`), so a hook panic still becomes an error result.

## Validation
`go test ./internal/agent/... -race -count=1` (`loop_tools_test.go` covers parallel order, sequential, truncated, abort, hooks). Lint clean.

## Rollback
Separate commit (`refactor(agent): select the tool batch runner through a toolExecutor`). Revert alone; phase 2 still works if the act stage call is restored.
