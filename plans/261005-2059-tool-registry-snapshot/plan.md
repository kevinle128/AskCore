---
title: "Tool registry snapshot and tool deltas"
status: done
created: 2026-10-05
branch: master-2
---

# Tool registry snapshot and tool deltas

## Problem

`tools.Registry` is live and shared. The run declares tools to the model once (`agent.go` `initialSystemMessage` → `reg.Decls()` at run start), but executes against the live registry (`loop_tools.go` `b.ac.Tools.Lookup` / `Prepare`). A `Register` during a run makes a tool the model was never told about executable; a future `Unregister` would leave a declared tool that fails as not found. Declared set and executable set can differ.

## Outcome

- Each turn takes one immutable `tools.Snapshot` of the registry. Declared tools and executed tools of that turn come from the same snapshot.
- Before each request the loop compares the tools the transcript declares with the snapshot and, when they differ, adds a system message with `ToolsAdded` / `ToolsRemoved` (Pi `declareToolChanges`, `agent/src/agent-loop.ts:333-380`; `getCurrentTools` / `getToolStateChanges`, `ai/src/utils/transcript.ts:58,150`).
- `Registry.Unregister` exists and is safe during a run.

User decision 2026-10-05: do snapshot, `Unregister` and the tool delta now. Storing the delta in the session log stays in H8.

## Non-goals

- Session log persistence of deltas (H8). Per-wire rendering beyond what adapters already do (H4). Changing the `pipeline.AgentContext.Tools` type.

## Steps

1. `internal/tools`: split the lookup table out of `Registry`; add `Snapshot` (immutable, nil-safe `Lookup`, `Decls`, `Prepare`), `Registry.Snapshot()`, `Registry.Unregister(name) bool`. Tests.
2. `internal/providers/transcript.go`: `CurrentTools(msgs)` and `ToolChanges(prev, cur)`, ported from Pi. Tests.
3. `internal/agent`: steer stage takes the snapshot, declares changes into pending messages (merge into a pending system message, else insert before the first non-system message); `Run` applies the same to prompts; act stage and executor selection use the turn snapshot. Tests through `agent.New` / `Prompt`.
4. READMEs (`tools`, `providers`, `agent`).

## Acceptance

- [x] A tool registered mid-run is declared by a system message before the next request and runs; it does not run before it is declared.
- [x] A tool unregistered mid-run still runs in the current turn, is declared removed before the next request, and then fails as not found.
- [x] No tool change means no system message: the JSONL baseline of `ask -p` is unchanged. Loop tests that call `Run` with tools but no declaring system message now expect one declaration before the first prompt (Pi parity).
- [x] `go test ./... -race`, lint 0 issues.

## Rollback

Revert the commit; no data or wire format changes (`SystemMessage.ToolsAdded/ToolsRemoved` already exist).
