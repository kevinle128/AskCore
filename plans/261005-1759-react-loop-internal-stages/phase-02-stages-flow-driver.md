# Phase 2: Stage abstraction, flow enum, driver

## Files
- Modify `internal/agent/loop_run.go`: replace `(*loop).run` body (lines 105-215) with the driver; add cross-turn fields to `loop` (line 92). Keep `Run`, `Continue`, `LoopConfig`, `poll`, `prepareRequest`, `turn`, `finishTurn`, `context`, `thinkingLevel`, `emit*`, `toolCalls` unchanged.
- Create `internal/agent/loop_stage.go`: `flow`, `stage`, `turnState`, the six stages, `turnStages`.
- Modify `internal/agent/loop_stream.go` only to extract the three hook seams below; `streamAssistantResponse` keeps its order and is otherwise unchanged.
- Do not touch any test file.

## Types (signature may be refined, semantics may not)

```go
type flow uint8
const (
    flowNext         flow = iota // run the next stage of this turn
    flowNextTurn                 // start a new turn: tools or steering are pending
    flowIdle                     // nothing pending: check follow-ups, else end
    flowIdleContinue             // as flowIdle, but FinishTurn asked for one more request
    flowEndRun                   // emit agent_end and stop, no queue poll
)

type stage interface {
    name() string
    run(l *loop) (flow, error)
}

// turnState is reset at the start of each turn.
type turnState struct {
    msg       protocol.AssistantMessage
    results   []protocol.ToolResultMessage // starts as non-nil empty slice
    moreTools bool                         // tool batch ran and did not ask to terminate
}
```

`loop` gains `pending []protocol.Message`, `turns int` (completed turns; replaces `lastCompletedTurn != nil`), and `ts turnState`. Stages are empty structs: all state is on `loop`, typed. No closures, no deps struct.

## Single hook seam (user decision, locked 2026-10-05)

Every hook call in the loop goes through exactly one loop method per hook point. Only that method reads `l.cfg.Hooks.<Field>`; stages and the driver call the method. This gives one place to trace, and the H11 adapter changes nothing in the loop.

| Hook point | Seam method | Today |
|---|---|---|
| `GetSteeringMessages` | new `(*loop).pollSteering()` wrapping `poll` | three reads at `loop_run.go:108,120,190` |
| `GetFollowUpMessages` | new `(*loop).pollFollowUps()` wrapping `poll` | `loop_run.go:198` |
| `PrepareRequest` | `(*loop).prepareRequest()` (exists) | `loop_run.go:227` |
| `FinishTurn` | `(*loop).finishTurn()` (exists) | `loop_run.go:266-269` |
| `TransformContext` | new `(*loop).transformContext(msgs)` | inline `loop_stream.go:17-21` |
| `ConvertToLLM` | new `(*loop).convertToLLM(msgs)` (keeps the `providers.ConvertToLLM` default) | inline `loop_stream.go:22-29` |
| `GetAPIKey` | new `(*loop).apiKey()` (keeps the "empty key falls back" rule) | inline `loop_stream.go:32-41` |
| `BeforeToolCall` | new `(*batchRun).beforeToolCall` | inline `loop_tools.go:239-260`; done in phase 3, which owns `loop_tools.go` |
| `AfterToolCall` | `(*batchRun).afterToolCall` (exists) | `loop_tools.go:290` |

The driver sketch below uses `l.pollSteering()` and `l.pollFollowUps()`, not `l.poll(l.cfg.Hooks...)`.

## Stage rules (copied from current `run`, line numbers in loop_run.go)

| Stage | Behavior | Source |
|---|---|---|
| steer | If `turns > 0`: if `pending` empty, poll steering; emit `turn_start`. Then emit and append each pending message to `ac.Messages` and `newMessages`; clear `pending`; reset `ts` | 117-136 |
| prepare | `l.prepareRequest()` | 138-140 |
| reason | `streamAssistantResponse()`; append to `newMessages`; store in `ts.msg` | 141-145 |
| act | Skip if `ts.msg` failed (StopError/StopAborted). If tool calls exist: pick executor (phase 3; in this phase call the existing `failTruncatedToolCalls` / `executeToolCalls` branch), set `ts.results = batch.messages` (keep nil-vs-empty exactly as today), `ts.moreTools = !batch.terminate` | 159-171 |
| observe | Append `ts.results` to `ac.Messages` and `newMessages` | 172-175 |
| decide | `turns++`; `finishTurn(l.turn(msg, results))`. Failed message: emit `turn_end` with empty results, return `flowEndRun` (decision ignored, as 147-157). Else emit `turn_end`; `End` gives `flowEndRun`; poll steering into `pending`; if `moreTools` or pending non-empty gives `flowNextTurn`; else `Continue` gives `flowIdleContinue`, otherwise `flowIdle` | 177-194 |

Note the error tail emits `turn_end` and then `agent_end` in the same order as today because the driver emits `agent_end` on `flowEndRun`.

## Driver (target shape, about 30 lines)

```go
func (l *loop) run() error {
    var err error
    if l.pending, err = l.pollSteering(); err != nil { return err }
    for {
        f, err := l.runTurn()          // runs turnStages until a non-flowNext flow
        if err != nil { return err }
        switch f {
        case flowNextTurn: continue
        case flowEndRun:   return l.emit(&protocol.AgentEnd{...})
        }
        // idle check: the outer loop of Pi
        followUps, err := l.pollFollowUps()
        if err != nil { return err }
        if len(followUps) > 0 { l.pending = followUps; continue }
        if f == flowIdleContinue { continue }
        return l.emit(&protocol.AgentEnd{...})
    }
}
```

Check: a stage that returns `flowNext` from the last stage is a programming error; `runTurn` should panic with the stage name (cannot happen with decide as last stage).

Edge check against today: with the very first turn, `turns == 0`, so steer neither re-polls nor emits `turn_start` (already emitted by `Run`/`Continue`). A follow-up turn or forced continuation has `turns > 0`, so it emits `turn_start`, and polls steering only when `pending` is empty, identical to lines 118-126.

## Comments
Explain why (for example "poll only when empty so a one-at-a-time queue never delivers two messages in one turn"). Keep the existing comments that carry Pi rules. No plan or phase IDs. Simple technical English.

## Validation
`go test ./internal/agent/... -race -count=1` green; `go vet ./internal/agent/`; lint clean.

## Risk and rollback
Highest-risk phase. Commit alone (`refactor(agent): run one turn as a list of ReAct stages`). Revert that commit to roll back.
