# Lifecycle documentation delivery

Status: DONE.

## Scope and authority

This is a docs-only delivery for people and AI collaborators, with --tdd and --auto and no scope reduction.
The accepted documentation plan and fresh implementation evidence define the scope.
The docs skill was used with its content rules, update workflow, and agent-context rules.
Evergreen guides own package decisions and navigation; source owns machine contracts and execution details.
The roadmap and architecture report remain stateful records.
The architecture report status now says implemented by plan 261006-0933-lifecycle-event-pipeline-redesign and does not claim user acceptance.

## Delivery

The roadmap revision records that the lifecycle redesign is implemented.
D19 identifies the revised tool outcome model as implemented and keeps the earlier Pi abort model as history.
D26 through D29 are marked implemented.
H2 records per-call concurrency approval, exclusive barriers, frozen validated arguments, the pre-tool allow/deny/cancel contract, and dropped tool calls on max-tokens completion.
It distinguishes stopped normal continuation from queued steering that continues the same cycle with its max-tokens reason retained.
H8 retains SQLite durability, lease generation, resume, and persistent context projection rather than defining another entry model.
H9 retains overflow detection, cumulative usage, context-usage estimates, and usage entries.
It records visible Pi retry projection and log-only facts about failed attempts.
Their assistant content stays outside model history.
H11 retains the extension taxonomy mapping, compiled-in handlers, and fail-closed tool_call rather than rebuilding typed dispatch.
H12 retains dashboard redaction, tracing, OTel export, hang detection, and transport integration rather than rebuilding follow, cursor, ring, or resync.

The architecture flow now points to current control and execution owners and separates durable Ask turns from Pi wire turns.
The internal package map points to typed handlers and current in-memory storage.
The package guides were pruned to source navigation, durable decisions, and dependency boundaries.
They distinguish safe failed-attempt facts from raw assistant content, which is not saved to model history.
Imaginary current session tree, manager, and protocol frame files were removed from navigation.
Two links to removed hook files in the historical design report became plain historical source paths; the report text was otherwise preserved.
The scheduler guide now labels its lane implementation as planned rather than claiming a current callback implementation.
The root README and AGENTS received only the authorized stale runtime scaffold navigation correction.
No managed instruction region was changed.

## Source evidence

| Changed claim | Verified owner |
|---|---|
| One execution owner and separate disposal | internal/agent/agent.go:104 New; internal/agent/agent.go:219 Dispose; internal/agent/agent.go:224 dispose |
| Preparation before input commit | internal/agent/loop_stream.go:36 preparation; internal/agent/attempt.go:26 commitStaged |
| Input removal and claim shape | internal/agent/queue.go:80 claimSteering; internal/agent/queue.go:89 claimCycle; internal/agent/queue.go:202 Remove |
| Added context belongs to AfterTool | internal/pipeline/points.go:111 AfterToolCallResult; internal/agent/tool_coordinator.go:312 afterTool |
| Per-call safety, bounded bodies, and barriers | internal/agent/tool_coordinator.go:40 runToolBatch; internal/agent/tool_coordinator.go:165 concurrencySafe; internal/tools/types.go:23 ConcurrencySafe |
| Uncertain outcomes never re-execute tools | internal/agent/tool_repair.go:15 repairTools |
| Max-tokens tool calls are dropped | internal/agent/loop_stream.go:227 StopLength branch; internal/agent/loop_stage.go decideStage |
| Retry and visible Pi projection are implemented | internal/agent/recover.go:12 streamAssistantResponse; internal/agent/retry_events.go:10 retryEvents |
| Serving policy and preparation capture | internal/providers/prepare.go:257 RegisterProvider; internal/providers/prepare.go:284 Prepare; internal/providers/retry.go:14 DefaultRetryPolicy |
| Typed failures and settled streams | internal/providers/failure.go:45 Failure; internal/providers/failure.go:176 ClassifyProvider; internal/providers/stream.go:84 Result |
| Method, profile, and billing pin | internal/app/module_auth.go:34 RequireBinding guard; internal/agent/loop_stream.go:27 binding pin |
| Interrupted replay keeps nonblank content | internal/providers/transform.go:216 aborted-content branch; internal/providers/transform.go:329 error omission |
| In-memory entries and sole writer | internal/sessions/entry.go:13 Entry; internal/sessions/writer.go:16 Writer; internal/sessions/memory.go:32 Append |
| Request rebuild already works in memory | internal/sessions/entry.go:171 RequestDelta; internal/agent/request_log.go:188 rebuildRequest |
| Typed scoped dispatch and accepted next lifecycle | internal/pipeline/registry.go:162 Registry; internal/pipeline/registry.go:186 Scoped; internal/pipeline/middleware.go:52 claim; internal/pipeline/middleware.go:64 finish |
| Follow, ring, cursor, and resync are implemented | internal/agent/follow.go:131 Follow; internal/bus/follow.go:102 Add; internal/bus/follow.go:133 Reset; internal/bus/follow.go:179 Resume |
| Local observation differs from nonblocking follow | internal/agent/emit.go:136 dispatch; internal/bus/follow.go:79 Ring |
| Declaration allowlist and frozen preparation | internal/tools/registry.go:165 Decls; internal/tools/registry.go:181 Prepare |
| Pi turn identity and retry flag | pkg/protocol/events.go:96 TurnStart; pkg/protocol/events.go:53 AgentEnd; pkg/protocol/codec.go EncodeEvent |
| Scheduler is still a scaffold | internal/scheduler/doc.go and absence of scheduler implementation files |

## Validation

The initial read found obsolete hook, delivery mode, whole-batch serial, and future-storage claims before the edits.
The current source was read before replacing each claim.
The final local link check verified 199 paths and Markdown heading targets across 15 authorized documents, with no errors.
The final stale-name scan under docs and internal found no pipeline.Hooks, GetSteeringMessages, FinishTurn, Sequential(), one-at-a-time, pipeline.Step, or _step.go references in Markdown.
git diff --check passed.
No repository Markdown validator was found and no docs-only gate was added.
No new operating command or product command changed.
The fresh full tests, race suite, build, configured lint, actual CLI checks, and conformance gate from tester-261006-phase10.md are reused because this delivery changes no code.
Those checks ran on 2026-10-06 against the completed implementation.
No product code, tests, dependencies, generated files, changelog, matrix, task status, or commit was changed by this docs task.
No process was started or remains from this docs task.

## Concerns

None for the authorized documentation scope.
