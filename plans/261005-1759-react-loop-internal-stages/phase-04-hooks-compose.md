# Phase 4: Hook composite (`pipeline.Compose`)

## Context
Today the only place that combines two hook sets is `projectContext` (`internal/agent/context_source.go:21-42`), installed by hand at `internal/agent/agent.go:171`. H11 will need N handlers at each hook point. This phase adds one composite, `pipeline.Compose`, with one written merge rule per hook point, and replaces the manual wrap with it. No behavior change: the JSONL diff of phase 7 and the existing tests guard it.

User decisions (locked, 2026-10-05): add `Compose` now; merge rules derived from Pi; replace the manual wrap at `agent.go:171`; H11 Dispatcher becomes an adapter that produces `pipeline.Hooks`, loop and stages unchanged.

## Three-layer design

| Layer | Where | Role |
|---|---|---|
| Port | `pipeline.Hooks` (`internal/pipeline/hooks.go:112-138`) | One function per hook point. The loop calls each field through exactly one loop method (single seam, phase 2 and 3). |
| Composite | `pipeline.Compose(hs ...Hooks) (Hooks, error)` (new `internal/pipeline/hooks_compose.go`) | Combines N hook sets into one, with the merge rule of each hook point. Strict: the first error stops the chain. |
| Adapter (H11) | a new file in `internal/pipeline` that wraps `hooks.Dispatcher` | Turns each extension event into a `Hooks` field. It owns Pi's per-event error tolerance (fail-open events log and continue; `tool_call` fails closed) before `Compose` sees a result. |

The adapter lives in `internal/pipeline`, not `internal/hooks`: the pipeline README allows `hooks` as an import (`internal/pipeline/README.md:44`), and the hooks README allows only `pkg/protocol`, `store`, `tracing` (`internal/hooks/README.md:29`). Depguard (`.golangci.yml:20-60`) is lax and does not block either direction, so the README rule is the authority.

Note on Pi sources: Pi chains its agent-level hooks by hand in `coding-agent/src/core/agent-session.ts` (the `previous...` wrappers). `extensions/runner.ts` merges the handlers of one extension event inside one hook. `Compose` replaces the first; the H11 adapter maps to the second. Both are cited below.

## Files
- Create `internal/pipeline/hooks_compose.go`: `Compose`.
- Modify `internal/pipeline/hooks.go`: add `func (r *AfterToolCallResult) Apply(res protocol.ToolExecutionResult, isError bool) (protocol.ToolExecutionResult, bool)`. The body moves from `internal/agent/loop_tools.go:314-336`. The type comment at `hooks.go:92-95` already documents this rule, so the code moves next to it. Nil receiver returns the input unchanged.
- Modify `internal/agent/loop_tools.go`: `afterToolCall` calls `r.Apply(o.result, o.isError)`; delete the inline copy (DRY: `Compose` needs the same step to feed the next hook).
- Modify `internal/agent/context_source.go`: `projectContext(src, system)` returns only the projection hook (no `user` parameter, no merge code).
- Modify `internal/agent/agent.go:171`: `hooks, err := pipeline.Compose(pipeline.Hooks{PrepareRequest: projectContext(r.source, system)}, cfg.Hooks)`; on error `execute` returns it before the run starts (no event is emitted), else `cfg.Hooks = hooks`. The projection sets no ConvertToLLM, so this error cannot happen with today's callers. The projection runs first, so the user hook sees the projected context, as today.
- Modify `internal/agent/types.go:24-25` comment: "The Agent composes its own PrepareRequest in front of Hooks.PrepareRequest."
- Modify `internal/pipeline/README.md` (see "README" below).
- Create `internal/pipeline/hooks_compose_test.go` (see "Tests" below). No existing test file changes.

## Compose shape

```go
// Compose returns one Hooks that runs hs in order at each hook point.
func Compose(hs ...Hooks) (Hooks, error)

// ErrMultipleConvertToLLM: more than one input sets ConvertToLLM.
var ErrMultipleConvertToLLM = errors.New("pipeline: more than one ConvertToLLM hook")
```

Per field: collect the non-nil functions in argument order. Zero gives a nil field (the loop keeps its default). One gives that function, assigned as is (identity, not a wrapper). Two or more give a chain with the rule below. A nil field inside a chain is skipped; it never means "use the default" in the middle of a chain. `Compose()` and `Compose(h)` therefore behave exactly as `Hooks{}` and `h`, with a nil error.

Errors: the first error stops the chain and is returned unchanged; later functions do not run. This keeps the D20 contract of `hooks.go:108-111` for every chain. `Compose` does not recover panics: a panic in `BeforeToolCall` or `AfterToolCall` still reaches the recover in `batchRun.prepare` (`loop_tools.go:222-226`) or `afterToolCall` (`loop_tools.go:294-298`) and becomes an error result; a panic elsewhere reaches `runGuarded` (`agent.go:190-197`).

## Merge rules

| Hook point | Rule | Pi source | Matches the draft? |
|---|---|---|---|
| `TransformContext` | Chain: the output of one is the input of the next. | `agent-session.ts:1721-1723,1741-1743` (`previousTransformContext` runs, its output feeds the next); `runner.ts:1298-1358` (`currentMessages` flows through every `context` handler) | Yes |
| `ConvertToLLM` | At most one. If two or more inputs set it, `Compose` returns `ErrMultipleConvertToLLM` and no hooks. One converter is kept as is. | Pi has one function (`agent/src/agent.ts:116,234`). | User decision 2026-10-05: reject, not chain. A second converter would get model messages, not the log, which is an easy silent bug. Open it later only for a real need. |
| `GetAPIKey` | First non-empty key wins; later functions do not run. Empty means "no opinion"; all empty gives "" and the loop falls back to the configured key (`loop_stream.go:33-41`). | Pi has one function (`agent/src/agent.ts:119,237,495`); no chain exists. | Yes (Ask choice; Pi has no equivalent) |
| `PrepareRequest` | Chain with accumulation: each function sees the `Request` with all earlier updates applied (`Context`, `Model`, `ThinkingLevel`). Merged update is field-wise, later non-nil (non-empty for `ThinkingLevel`) wins. All nil gives nil. | `agent-session.ts:760-788`: the projection builds the request the previous hook sees (778-782); `previous?.context ?? canonicalContext` (783); `previous?.model ?? state.model` (785-786) | Yes. Note: in Pi the order of a wrapper and `previous` differs per hook; `Compose` always runs argument order, so the caller orders the arguments. |
| `FinishTurn` | Run every function in order (no short-circuit on `End`), then combine `End > Continue > Proceed`. | `agent-session.ts:859-866`: both the extension boundary and `previousFinishTurn` run before the decision; `end` wins, then any `continue` | Partly: priority matches; Pi adds "call all, no short-circuit". |
| `BeforeToolCall` | Chain. A non-nil `Args` replaces `ToolCallInfo.Args` for the next function. The first result with `Block` wins and stops the chain (with its `Reason` and `Terminate`). Without a block: nil if no `Args` changed, else `&BeforeToolCallResult{Args: last}`. | `runner.ts:1242-1259` (first `block` returns at 1252-1253, later handlers skipped); `types.ts:1211-1214` (handlers mutate `event.input` in place, later handlers see it); errors are not caught (`runner.ts:1246-1257`), so they stop the chain; `agent-session.ts:639-652` rethrows | Yes |
| `AfterToolCall` | Chain. Each function sees `Result`/`IsError` with all earlier overrides applied (`AfterToolCallResult.Apply`). Merged result is field-wise, later non-nil wins; a later `Content` without `StructuredContent` clears the merged `StructuredContent`, so the loop drops it. All nil gives nil. | `runner.ts:1183-1240` (`currentEvent` is updated in place per handler; content without structured content deletes it at 1196-1197) | Yes. `Terminate` has no Pi `tool_result` field; it follows the same later-non-nil rule (`agent/src/types.ts:84`: "replaces the early-termination hint"). |
| `GetSteeringMessages` | Call every function in order, concatenate the results. | Pi has one queue drain (`agent/src/agent.ts:496-502`). | Yes (Ask choice). Each source keeps its own one-at-a-time rule; two sources can give two messages in one poll. That is a per-source property, not a `Compose` property. |
| `GetFollowUpMessages` | Same as steering. | `agent/src/agent.ts:503` | Yes (Ask choice), same caveat |

Error tolerance of `runner.ts` (handlers of `context`, `tool_result`, `message_end` are caught and logged, `runner.ts:1104,1167,1216,1316`) is not a `Compose` rule. It belongs to the H11 adapter, which catches per handler before it returns to `Compose`.

## Steps
1. Add `AfterToolCallResult.Apply` in `hooks.go`; switch `loop_tools.go` to it; run `go test ./internal/agent/... -race -count=1` (the `loop_tools_test.go` AfterToolCall cases guard the move).
2. Write `hooks_compose.go`. One small unexported helper per hook point; no reflection, no generic "fold" that hides the rule. Comments state each rule and why (for example "every FinishTurn runs, because a hook may record the turn even when another ends the run").
3. Write `hooks_compose_test.go`.
4. Replace `agent.go:171` and simplify `projectContext`. Check the four old cases against the composed result: user hook nil (projection update), user error (error), user returns nil (projection update), user update without `Context` (projected context plus user `Model`/`ThinkingLevel`). Guards: `agent_test.go:226-245` (error and panic), `agent_test.go:318`, `agent_test.go:354`, `loop_run_test.go:366`.
5. Update the pipeline README.

## Tests (`internal/pipeline/hooks_compose_test.go`, package `pipeline`, table-driven, one test per rule)
- `TestComposeIdentity`: `Compose()` gives all-nil fields; `Compose(h)` gives the same function pointers as `h` (compare with `reflect.ValueOf(f).Pointer()`); nil fields in a list are skipped.
- `TestComposeTransformContext`: order (a then b), output feeds input, first error stops (b not called).
- `TestComposeConvertToLLM`: one converter is kept; two converters return `ErrMultipleConvertToLLM`.
- `TestComposeGetAPIKey`: first non-empty wins and later not called; all empty gives ""; error stops.
- `TestComposePrepareRequest`: second sees the first's `Context`/`Model`/`ThinkingLevel`; merged is later-non-nil-wins per field; all nil gives nil; a nil from the second keeps the first's update; error stops.
- `TestComposeFinishTurn`: table over decision pairs; all functions run even after `End`; error stops.
- `TestComposeBeforeToolCall`: first `Block` wins with its `Reason`/`Terminate` and later not called; `Args` from the first reaches the second; no block and no args gives nil; error stops.
- `TestComposeAfterToolCall`: the second sees the first's overrides; field-wise merge; later `Content` alone clears earlier `StructuredContent`; `IsError` and `Terminate` later-wins; error stops.
- `TestComposeQueues`: steering and follow-up concatenate in order; nil and empty results add nothing; error stops.
- `TestAfterToolCallResultApply`: the rule table from `hooks.go:92-95` (nil receiver, empty non-nil `Content`, content without structured content).

## README (`internal/pipeline/README.md`)
- "What belongs here": add `hooks_compose.go` (`Compose`) and `AfterToolCallResult.Apply`; reword the "later" bullet to name the H11 Dispatcher adapter as one file here.
- Add the three-layer table and the merge-rule table (rule column only, short).
- "Rules": replace line 50 ("Ordered multi-handler steps come only when H11 ...") with "One function per hook point in `Hooks`. Several handlers at one point are combined with `Compose`, never by a hand-written wrapper."
- "File names": add `hooks_compose.go`.

## Validation
- `go test ./internal/pipeline/... ./internal/agent/... -race -count=1` green.
- `grep -n 'projectContext' internal/agent/*.go` shows one definition and one call in `agent.go`, with `pipeline.Compose` on that line.
- Lint clean.

## Risks
| Risk | L x I | Mitigation |
|---|---|---|
| Composed `PrepareRequest` differs from `projectContext` in one of its four cases | L x H | step 4 case check; existing agent tests; JSONL diff in phase 7 |
| `Apply` move changes the AfterToolCall result | L x M | pure move; `loop_tools_test.go` hook cases unchanged and green |
| `Compose(h)` wraps instead of returning the function | L x L | `TestComposeIdentity` compares pointers |
| Future readers expect runner.ts fail-open in `Compose` | M x M | README states the layer split; adapter owns tolerance |

## Rollback
Two commits: `refactor(pipeline): apply AfterToolCall overrides in one place` and `feat(pipeline): combine hook sets with Compose`. Revert the second to restore the manual wrap; the first stands alone.
