# Phase 11 independent documentation review

## Review state

Status: DONE.
Score: 10/10.
Critical findings: 0.
Open warnings: 0.
Required suggestions: 0.
The final frozen documentation is approved.
All nine phase 11 steps and the done criteria are verified.
The review owns only this report and has changed no product document, source, matrix, status, or commit.
The scope is the full accepted phase 11 plan with `--tdd --auto` and no `--yagni` reduction.

## Scope and method

The review read the replacement project instructions, root README, full phase 11 plan, architecture reference, package map, owning runtime READMEs, roadmap sections, and design report status.
The review checked current source and the previously verified regression tests for each changed lifecycle claim.
The final owner report is `plans/reports/docs-261006-phase11.md`.
The package guides use source owner links rather than duplicate machine fields or implementation counts.
The current session log is in memory, while SQLite persistence, leases, resume, and the persistent tree remain planned.
The source is unchanged since the full phase 10 test, race, build, lint, and matrix gates.
No test rerun is required for these documentation changes.

## Verified acceptance mapping

| Plan step | Documentation check | Source evidence |
|---|---|---|
| 1 | The roadmap revision says the lifecycle redesign is implemented and names its plan. | The approved phase 10 review and conformance matrix record the delivered checks. |
| 2 | D19 is revised, and D26 through D29 are marked implemented without declaring the original report accepted. | `internal/agent/tool_coordinator.go:65`, `internal/agent/tool_repair.go:15`, `internal/agent/loop_stream.go:37`, `internal/agent/queue.go:202`, `internal/providers/prepare.go:284`, `internal/agent/agent.go:219`. |
| 3 | H2 documents per-call safety, exclusive barriers, frozen arguments, and truncated tool-call removal. | `internal/agent/tool_coordinator.go:54`, `internal/agent/tool_coordinator.go:116`, `internal/agent/tool_coordinator.go:165`, `internal/agent/loop_stream.go:225`, `internal/agent/loop_stage.go:281`. |
| 4 | H8 keeps SQLite durability, lease ownership, resume, and the persistent context tree; the in-memory entries and rebuild already exist. | `internal/sessions/memory.go:16`, `internal/sessions/entry.go:200`, `internal/agent/request_log.go`. |
| 5 | H9 keeps overflow classification, cumulative usage, estimates, and persistent usage records; queue, retry, and final settlement placement are delivered. | `internal/agent/recover.go`, `internal/agent/retry_events.go:10`, `internal/agent/agent.go:469`, `internal/sessions/entry.go`. |
| 6 | H11 keeps taxonomy mapping, compiled-in handlers, extension adaptation, and fail-closed tool_call. | `internal/pipeline/registry.go`, `internal/pipeline/middleware.go`, `internal/pipeline/decisions.go`, `internal/hooks/doc.go`. |
| 7 | H12 keeps dashboard redaction, spans, OTel integration, and hang detection; trusted follow, cursor, bounds, and resync already exist. | `internal/agent/follow.go:112`, `internal/bus/follow.go:102`, `internal/bus/follow.go:179`, `internal/bus/follow.go:249`. |
| 8 | Architecture 7.2 names the typed points, separates control and observation, and documents DeepSeek input claims without Pi queue modes. | `internal/pipeline/points.go`, `internal/agent/queue.go:229`, `internal/agent/queue.go:245`, `internal/agent/queue.go:275`. |
| 9 | Source and link checks cover every changed surface, including minimal root navigation changes and the historical report status. | The final local link audit and stale-contract scan are recorded below. |

## Source facts checked

Preparation runs before `commitStaged`, while the credential callback and final AuthRunner resolution run after the commit.
A preparation error or cancellation saves neither staged user input nor its error wrapper.
A later credential failure follows the committed-input failure path.
`internal/agent/loop_stream.go:37` and `internal/agent/loop_stream.go:51` establish preparation and commit order.
`internal/agent/loop_stream.go:54` and `internal/app/module_auth.go:19` establish credential timing.
`internal/app/module_auth.go:34` checks the pinned method, profile, and billing hint before adapter dispatch.

The serving registration captures its retry policy in Prepared.
A plain stream registration has PolicyOnly preparation and lets its adapter compute effective materialization at stream start.
`internal/providers/prepare.go:284` and `internal/agent/loop_stream.go:87` preserve that distinction.

The retry wire sequence is derived from retry records and deliberately repeats Pi AgentStart and TurnStart while the durable Ask turn remains open.
Failed attempts retain safe outcome, failure, binding, and usage facts without saving their raw assistant content.
`internal/agent/retry_events.go:10` and `internal/agent/loop_stream.go:116` own that projection and settlement boundary.

The coordinator uses one turn snapshot and copied validated arguments.
No classifier, false, or classifier panic selects exclusive execution.
The default body limit is ten, and exclusive calls wait for earlier bodies and post-control.
The coordinator drains started workers without a timer before repair or settlement.
`internal/agent/tool_coordinator.go:54`, `internal/agent/tool_coordinator.go:65`, `internal/agent/tool_coordinator.go:116`, and `internal/agent/tool_coordinator.go:165` establish these facts.
Repair is limited to this run's open tail, preserves existing results, and writes uncertainty without running a body.
`internal/agent/tool_repair.go:15` and `internal/agent/tool_repair.go:60` establish that boundary.

Remove affects only a pending input.
AfterTool context is copied into a separate non-waking queue and enters later admission.
Disposal closes admission, clears queues, waits for the current run, closes the writer, and publishes AgentDisposed.
`internal/agent/queue.go:202`, `internal/agent/queue.go:275`, and `internal/agent/agent.go:219` establish these facts.
The headless JSON projection omits AgentDisposed and ends with AgentSettled, as the verified disposal fixture requires.

The consistent follow cut uses the published log position and streaming baseline.
A missing cursor, old epoch, oversized event, or slow follower causes explicit resync instead of a partial event or blocked publisher.
`internal/agent/follow.go:112` and `internal/bus/follow.go:249` establish the current trusted observation contract.
The guides correctly retain dashboard redaction as future work.

## Resolved wording and link findings

The original H3 test text incorrectly dropped every aborted assistant message.
Current replay retains nonblank aborted text and thinking, drops unfinished tool calls, and maps unsigned thinking to text at `internal/providers/transform.go:215`.

The original D22 text still placed retry in H9.
The delivered Agent recovery now owns retry, while H9 retains overflow and usage work.

The original architecture layer diagram labeled sessions as an entry tree without marking that tree as planned.
The corrected diagram now distinguishes the typed in-memory log from the planned persistent tree.

Two new guide sentences said failed attempt content was log-only.
The log keeps failure and usage facts rather than raw failed assistant content, and the corrected sentences now use the precise storage distinction.

The H2 max-tokens sentence stopped the cycle without a steering exception.
`internal/agent/loop_stage.go:281` and `TestCycleReasonStaysMaxTokensAfterLaterCompletedTurn` show that queued steering can continue the same cycle with the max-tokens reason retained.
A Continue decision alone does not continue that cycle.

The historical design report had two links to removed pipeline files.
The controller approved rendering those paths as historical plain code references while preserving the report text and its implemented status.
The final report uses those historical references without broken links and does not claim user acceptance.

## Link and stale-contract audit

The first local sweep checked 201 links across all listed surfaces.
Only the two historical removed-file links failed that sweep.
The final independent sweep checked all 199 remaining local paths and Markdown heading targets with zero failures.
The two removed links are now historical plain paths.
The final stale-contract scan again returned zero matches.
`git diff --check` passed on the frozen workspace.
The revised H3 replay text, D22 retry ownership, storage diagram, failed-attempt record distinction, and max-tokens steering exception were all re-read against source after correction.
The evergreen stale-contract scan found no pipeline.Hooks, GetSteeringMessages, FinishTurn, Sequential(), one-at-a-time, pipeline.Step, or _step.go references.
Historical decision text remains clearly separated from the current runtime contract.

## Risks and limits

The review checks changed local documentation links and repository claims.
It does not revalidate external upstream source history or imply that the user accepted the original architecture report.
The docs introduce no dependency, public contract, execution behavior, or new acceptance scope.

## Final conclusion

The changes remain within the listed phase 11 surfaces and the authorized minimal root navigation corrections.
The root managed instruction regions remain outside the documentation edits.
H8, H9, H11, and H12 distinguish delivered behavior from their remaining persistent storage, accounting, extension, and observability work.
The guides contain no removed hook API, pipeline.Step, old step file placement, whole-batch serial rule, or Pi delivery mode claim.
The report status says implemented by the named plan and makes no user acceptance claim.
No further correction or test rerun is required for this docs-only scope.
