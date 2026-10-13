---
phase: 8
title: "ACP tool mapping"
status: completed
priority: P2
effort: "1d"
dependencies: [7]
---

# Phase 8: ACP tool mapping

## Context

- Architecture: [Boundaries & contracts](../architectures/h5-builtin-tools-architecture.md#boundaries--contracts) (results reuse H2's contract; ACP diff with whole-file old and new text plus `locations` for read and edit).
- xia H5: [Dependency matrix](../reports/xia-261010-1727-h5-builtin-tools.md#dependency-matrix) row "ACP tool mapping (kind, locations, diff)" = CONFLICT; ["What to take"](../reports/xia-261010-1727-h5-builtin-tools.md#what-to-take-no-conflict-with-the-contract) row `result` (Grok `acp_conversion.rs`).
- xia T1b: [Surface by surface](../reports/xia-261010-1642-t1b-tui-surfaces.md#surface-by-surface) row 1 (tool output; the TUI renderers that will read these details come with T1b, not here).
- Inventory: H-TOOL-13 result shape ([inventory-harness.md](../260930-2254-pi-feature-inventory-go-roadmap/inventory-harness.md) line 63).

## Overview

Map the H5 tools to the ACP standard `tool_call` frames: a kind per tool at start, and at the end the `locations` and, for `write` and `edit`, a diff content block with whole-file old and new text, decoded from `ToolExecutionResult.Details` (P10). No TUI change.

## Requirements

- Functional:
  - Kind at start (`protocol.ToolCallStartEvent`, `internal/acp/updates.go:58-63`, today `sdk.ToolKindOther`): by tool name, `read` → `read`; `write`, `edit` → `edit`; `ls`, `grep`, `find` → `search`; `bash`, `monitor` → `execute`; every other name (including `echo`, `job_*`, extension and MCP tools) → `other`. The name map lives in `acp` (`acp` imports `tools` in tests only, `internal/acp/README.md` "Imports").
  - End (`protocol.ToolExecutionEnd`, `updates.go:34-42`): decode `e.Result.Details` as `protocol.ToolResultDetails` when it is valid JSON of that shape (unknown or invalid details are ignored, the frame stays as today). Add `sdk.WithUpdateLocations` from `locations` (path, line); when `diff` is present, add `sdk.ToolDiffContent(diff.path, diff.newText, diff.oldText)` (`third_party/acp-go-sdk/helpers.go:55`) before the text content; `sdk.WithUpdateKind` from `kind` when present.
  - The text content stays the model text (no change to `toolContent`).
  - Partial updates (`protocol.ToolExecutionUpdate`, `updates.go:28-33`) are unchanged.
  - Notices: standard ACP clients receive no `user` message frames (`standardUpdates` has no user case), so a `<system-reminder>` notice is not shown to them; a follower (TUI) gets whole events (N1).
- Non-functional: no new dependency; `acp` production code does not import `tools`.

## Architecture

```
Agent event ─▶ standardUpdates
  ToolCallStartEvent → StartToolCall(kind = kindOf(name))
  ToolExecutionEnd   → UpdateToolCall(status, [kind], [locations], [diff, text])
                         ▲ details := protocol.ToolResultDetails (decoded once)
```

## Runtime Flow

- Actor and trigger: an ACP client sends `session/prompt`; the model calls `read`, `edit`, `bash`, `grep`.
- Registered entry point: `ask leader` (`cmd/tui/leader.go`) or `ask acp` → `acp.Adapter` → `Host.NewSession` (`internal/acp/host.go:259`) → `session/update` frames from `standardUpdates` (`internal/acp/updates.go:22`).
- Internal path: Agent events → `standardUpdates` → SDK frames → socket.
- Prepared data and external mocks: a small file in the session cwd; the provider fixture serves Anthropic SSE that calls `read`, then `edit`, then `bash`.
- Observable result: `tool_call` frames with kinds, locations and a diff.
- End-to-end proof: `TestLeaderToolUpdatesCarryKindLocationsAndDiff` (phase 9).

## File inventory

| File | Action | Size | Test impact |
|---|---|---|---|
| `internal/acp/updates.go` | modify (`kindOf`, details decode, end options) | +50 | `internal/acp/updates_test.go` (new cases) |

## Tests Before

`go test ./internal/acp` passes (phase 7 state); record the count, including the conformance tests (`internal/acp/conformance_test.go`).

## Tests After (write first)

| Test | Scenario | Priority |
|---|---|---|
| `TestStandardUpdatesSetsKindByToolName` | table of all 12 tool names plus an unknown name | critical |
| `TestStandardUpdatesAddsDiffAndLocationsForEdit` | details with diff and two locations → diff block first, locations | critical |
| `TestStandardUpdatesAddsLocationsForRead` | read details | high |
| `TestStandardUpdatesIgnoresUnknownDetails` | details `{"foo":1}` and invalid JSON → frame as today | high |
| `TestStandardUpdatesKeepsUserMessagesOut` | a `MessageEnd` with a reminder user message gives no frame | medium |

## Function and interface checklist

- [x] `kindOf(name string) sdk.ToolKind`
- [x] `detailsOf(raw json.RawMessage) (protocol.ToolResultDetails, bool)`
- [x] end-frame options in `standardUpdates`

## Dependency map

Needs phase 1 (`ToolResultDetails`) and phase 7 (tools registered, so the E2E has real details). Blocks phase 9. No parallel phase.

## Related Code Files

- Modify: `internal/acp/updates.go`, `internal/acp/updates_test.go`.

## Implementation Steps

1. Write the tests; they fail.
2. Add `kindOf` and `detailsOf`; extend the two cases of `standardUpdates`.
3. Run the conformance tests to prove the SDK frames still validate against the pinned schema.

## Regression Gate

`go test ./internal/acp -race && go vet ./internal/acp && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./internal/acp/...`

## Success Criteria

- [x] All new tests and all existing `internal/acp` tests (conformance included) pass.
- [x] A frame without valid details is byte-identical to today's frame.

## Risk Assessment

| Risk | L x I | Mitigation |
|---|---|---|
| Whole-file diff of a large file makes a large frame | M x L | The leader carries lines up to 64 MiB (`internal/acp/README.md`, SDK copy); `write` content is bounded by the model output; documented. |
| An ACP client renders `kind` differently | L x L | Kinds are the standard enum values (`third_party/acp-go-sdk/types_gen.go:6615-6624`). |

## Security Considerations

- The diff carries file content that the model already sent or read; no new data leaves the host.
- Notices are not sent to standard ACP clients.
