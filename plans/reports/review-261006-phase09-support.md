# Phase 09 support review

## Verdict

The final support review score is 10/10, with zero critical findings, zero open warnings, and no required suggestions.
The support scope is approved after independent verification of the accepted copy fix.
The coordinator and all 34 phase 09 matrix assertions require a separate integration review.

## Scope and method

This review uses `/private/tmp/askcore-lifecycle-phase08-baseline` as the source baseline.
The scope is the phase 09 changes in `internal/tools`, `internal/sessions`, `internal/pipeline`, and `cmd/tui/headless_live_test.go`, plus the approved fix to shared protocol block copies.
The accepted phase 09 plan, all 34 phase 09 matrix rows, project instructions, and the owning package READMEs were read before source review.
The review applies the code review skill with specification checks before code quality checks.
The flags are `--tdd --auto`, with the full requested support scope and no `--yagni` reduction.
No production file, matrix checkbox, or phase status was changed by this reviewer.

## Stage 1: Specification checks

| Support requirement | Verdict | Source and assertion evidence |
|---|---|---|
| Replace `Sequential` with argument-sensitive `ConcurrencySafe` | Pass | `internal/tools/types.go:20` defines the optional capability and documents default exclusivity, false answers, and classifier panics. |
| Declare pure Echo and test math tools safe | Pass | `internal/tools/echo.go:15` and `cmd/tui/headless_live_test.go:29` return true; both tests first prepare coerced arguments, and the math test also checks actual body results. |
| State unbounded drain and prompt cancellation duties | Pass | `internal/tools/types.go:10`, package godoc, and the owning README state the tool duty, Agent busy state, and process group duty. |
| Record call intent with assistant position and call ID | Pass | `internal/sessions/entry.go:77` adds the closed entry type with scalar fields and no body-start claim. |
| Keep call intent outside model history | Pass | `TestToolCallEntryIsLogOnlyAndKeepsAssistantScope` checks two identical IDs at distinct assistant positions, append range, empty message projection, value isolation, and exact JSON fields. |
| Add user context without changing the `Apply` contract | Pass | `internal/pipeline/points.go:111` adds the field; `Apply` keeps its signature and existing override rules. |
| Preserve the waterfall rule | Pass | `TestAfterToolAddedContextKeepsWaterfallOuterPrecedence` checks that an outer returned result replaces inner context. |
| Copy terminal, downstream, and handler return values | Pass | `internal/pipeline/registry.go:350` copies each return boundary, and the repaired shared protocol helper also copies pointer user blocks and their signature fields. |
| Preserve nil and deliberate empty overrides | Pass | `TestAfterToolCloneKeepsEmptyOverridesAndNilContext` checks nil results, nil context, non-nil empty content, and non-nil empty context. |

The new DTOs provide the support for rows 47b and 48, rows B2, 63, and N4, and rows 19, 53, and SA26.
These DTO checks do not prove coordinator scheduling, call-record timing, repair, abort, or queue delivery behavior.
The `Sequential` removal is an intentional public contract change in the accepted plan.
The current CLI math test compiles after the main worker migrated the old caller references.

## Accepted finding

### Resolved: Pointer content blocks crossed the copy boundary

`internal/pipeline/points.go:129` and `internal/pipeline/points.go:147` use `protocol.CloneMessage` for result content and added context.
At review time, `pkg/protocol/content.go:425` copies the user-block slice and value `Text` signatures, but keeps `*Text` and `*Image` pointers.
Those pointer types implement the public `UserBlock` interface and encode through the existing JSON contract.
An outer `AfterTool` handler can therefore change an inner handler's text, signature, or image through a returned copy.
The caller can also change a retained handler result through the same pointers.
The actual side effect is mutation of another handler's result and queued input content across a boundary documented as a deep copy.
This is a data ownership defect, with no demonstrated credential exposure or hostile-input exploit.

The temporary overlay `/private/tmp/askcore-phase09-support-review/overlay.json` adds `TestReviewAfterToolPointerBlockIsolation` without changing workspace source.
The test first encodes a pointer-text user message, then changes the downstream pointer text and signature in an outer handler.
The test fails because the retained inner text becomes `outer` instead of remaining `inner`.
The controller accepted the finding and assigned a root fix in the shared protocol copy helper.
The final `pkg/protocol/content.go:425` copies pointer text and image blocks, while `CloneAssistantBlock` also copies pointer text, thinking, and tool-call blocks.
The helper preserves pointer types and typed nil values, and uses the existing field cloners for signatures and raw argument bytes.
The unchanged original overlay now passes on the final source.
`TestAfterToolPointerBlocksArePrivateAtHandlerAndCallerBoundaries` verifies pointer text, signatures, image data, and image MIME types across both handler and caller boundaries.
`TestCloneMessageCopiesPointerUserAndToolResultBlocks` checks user and tool-result messages in value and pointer forms.
`TestClonePointerAssistantBlocksSharesNothing` checks text, thinking, and tool-call pointer fields through both the block helper and assistant message copy.
`TestClonePointerBlocksPreservesTypedNil` checks the legal nil pointer forms without changing their type.

## Stage 2: Quality and caller checks

The specification and quality stages pass for the final support scope.
The remaining source review found no additional defect.
The capability interface follows the existing optional tool capability pattern and adds no model-facing field.
Echo and the arithmetic fixtures use immutable tool state and local execution values.
The `ToolCall` entry adds only scalar identity facts and uses the existing closed entry and message projection rules.
The new pipeline clone copies raw JSON, usage, boolean pointers, and value-text signatures, while preserving deliberate empty slices.
The registry keeps registration order, outer-result precedence, and the existing `Apply` call shape.
Sibling callers retain the existing override rules for content, structured content, details, error status, usage, and termination.
The main coordinator also uses the new clone helper for tool execution results, so the shared fix protects that caller without a separate pipeline workaround.
No unrequested compatibility shim, tool execution mode, or new approval flow was added.

## Fresh verification

`go test ./internal/tools/... ./internal/sessions/... ./internal/pipeline/... -count=1` passed.
`go test -race ./internal/tools/... ./internal/sessions/... ./internal/pipeline/... -count=1` passed.
`go vet ./internal/tools/... ./internal/sessions/... ./internal/pipeline/...` passed.
Scoped `golangci-lint run` for these three packages reported zero issues.
`go test ./cmd/tui -run '^TestHeadlessMathToolsDeclareConcurrencySafe$' -count=1` passed without a live provider call.
The original pointer isolation overlay failed before the fix and passed after the fix.
Focused permanent pipeline and protocol copy regression tests passed on the final source.
Focused race tests for the final pipeline and protocol copy contracts passed.
Final `go vet ./internal/pipeline ./pkg/protocol` passed.

## Status

Status: DONE.
Summary: The support contracts match the accepted scope, and the reproduced copy defect is fixed and independently verified.
Concerns/Blockers: None in the support scope; the coordinator and all 34 matrix assertions still need integration review.
