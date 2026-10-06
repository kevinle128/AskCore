# Lifecycle Git preflight: 2026-10-06

Status: ready for the user to select the commit action.
This report proposes a commit scope for the user to review.
No files were staged, committed, reset, reverted, or pushed.
No pull request or remote write was made.

## Initial inspection: historical snapshot

The accepted lifecycle plan covers phases 00–11.
At the initial inspection, the progress report recorded phases 00–10 complete and phase 11 incomplete.
The recorded phase 10 gates passed: full tests, full race, build, CLI fixtures, configured lint, and the full conformance matrix.
This preflight reused those records and did not repeat product tests.
`git diff --check` passed at inspection.
The worktree contained 97 changed tracked files, plus new lifecycle files and other work at the first inspection.
The tracked diff had 5,176 added lines and 2,526 removed lines at that snapshot.
The completion addendum below supersedes the initial pending phase 11 status.

## Proposed commit sequence

1. `feat(agent): add lifecycle control, retry, and tool recovery`
   Include the source, tests, deletions, and approved cassette fixtures listed below.
   Keep the linked package changes together because the old Hooks API is removed and its callers must change together.
   The auth binding changes are part of retry billing identity and provider preparation, so include them with this feature.
2. `docs(agent): describe the completed lifecycle contracts`
   Include the owning documentation and lifecycle evidence listed below; the phase 11 gate has passed.
   Include only the root README and AGENTS project context changes authorized for this lifecycle work.
   Do not edit or include changes to generator managed regions.

The large combined phase 00–07 starting diff is authorized lifecycle work, based on the accepted plan and current progress.
Do not split that diff into guessed historical phase commits.
No separate approved phase snapshots were inspected.
Do not cherry-pick, revert, or discard another session's work to create a sequence.
Use conventional commit subjects below 72 characters.
Do not add an agent name, AI attribution, or agent co-author trailer.

## Exact source and test inclusion list

Paths below are the inspected candidates for the feature commit.
Deleted Hooks files are included as deletions.

```text
cmd/tui/args.go
cmd/tui/headless.go
cmd/tui/headless_dispose_test.go
cmd/tui/headless_fault_test.go
cmd/tui/headless_json.go
cmd/tui/headless_lifecycle_test.go
cmd/tui/headless_live_test.go
cmd/tui/headless_output_test.go
cmd/tui/headless_record_test.go
cmd/tui/headless_request_log_test.go
cmd/tui/headless_test.go
cmd/tui/headless_tool_abort_test.go
cmd/tui/output.go
internal/agent/admission_test.go
internal/agent/agent.go
internal/agent/agent_model_test.go
internal/agent/agent_test.go
internal/agent/agent_tool_changes_test.go
internal/agent/attempt.go
internal/agent/attempt_test.go
internal/agent/context_source.go
internal/agent/dispatch_semantics_test.go
internal/agent/dispose_test.go
internal/agent/doc.go
internal/agent/emit.go
internal/agent/export_test.go
internal/agent/failure_code_test.go
internal/agent/failure_fixture_test.go
internal/agent/follow.go
internal/agent/follow_test.go
internal/agent/interrupted_test.go
internal/agent/lifecycle.go
internal/agent/lifecycle_test.go
internal/agent/loop_helpers_test.go
internal/agent/loop_run.go
internal/agent/loop_run_test.go
internal/agent/loop_stage.go
internal/agent/loop_stage_test.go
internal/agent/loop_stream.go
internal/agent/loop_stream_test.go
internal/agent/loop_tool_changes.go
internal/agent/loop_tools.go
internal/agent/loop_tools_test.go
internal/agent/queue.go
internal/agent/queue_test.go
internal/agent/recover.go
internal/agent/recover_test.go
internal/agent/reentry_test.go
internal/agent/request_body_test.go
internal/agent/request_log.go
internal/agent/retry_events.go
internal/agent/retry_events_test.go
internal/agent/session_log_test.go
internal/agent/systemprompt.go
internal/agent/tool_abort_test.go
internal/agent/tool_context_test.go
internal/agent/tool_control_test.go
internal/agent/tool_coordinator.go
internal/agent/tool_coordinator_test.go
internal/agent/tool_preabort_test.go
internal/agent/tool_repair.go
internal/agent/tool_repair_test.go
internal/agent/types.go
internal/app/module_auth.go
internal/app/module_auth_test.go
internal/auth/service.go
internal/bus/follow.go
internal/bus/follow_test.go
internal/bus/main_test.go
internal/pipeline/after_tool_context_test.go
internal/pipeline/decisions.go
internal/pipeline/decisions_test.go
internal/pipeline/doc.go
internal/pipeline/hooks.go
internal/pipeline/hooks_compose.go
internal/pipeline/hooks_compose_test.go
internal/pipeline/middleware.go
internal/pipeline/middleware_lifecycle_test.go
internal/pipeline/middleware_test.go
internal/pipeline/points.go
internal/pipeline/recovery.go
internal/pipeline/registry.go
internal/pipeline/registry_test.go
internal/pipeline/scope_test.go
internal/providers/anthropic/document.go
internal/providers/anthropic/failure_test.go
internal/providers/anthropic/prepare.go
internal/providers/anthropic/prepare_test.go
internal/providers/anthropic/provider.go
internal/providers/anthropic/tool_names_test.go
internal/providers/assembler.go
internal/providers/diagnostic.go
internal/providers/diagnostic_test.go
internal/providers/errors.go
internal/providers/failure.go
internal/providers/failure_test.go
internal/providers/fantasykit/errors.go
internal/providers/fantasykit/fold.go
internal/providers/fantasykit/fold_test.go
internal/providers/fantasykit/idle.go
internal/providers/fantasykit/transport.go
internal/providers/fantasykit/transport_test.go
internal/providers/faux/faux.go
internal/providers/faux/faux_test.go
internal/providers/faux/script.go
internal/providers/faux/stream.go
internal/providers/openai/completions.go
internal/providers/openai/completions_body.go
internal/providers/openai/failure_test.go
internal/providers/openai/prepare.go
internal/providers/openai/prepare_test.go
internal/providers/openai/profile_errors.go
internal/providers/openai/profiles_test.go
internal/providers/openai/remaining_contracts_test.go
internal/providers/openai/responses.go
internal/providers/openai/responses_prompt.go
internal/providers/prepare.go
internal/providers/registry.go
internal/providers/retry.go
internal/providers/retry_test.go
internal/providers/stream.go
internal/providers/transcript.go
internal/providers/transcript_test.go
internal/providers/transform.go
internal/providers/transform_test.go
internal/providers/types.go
internal/sessions/entry.go
internal/sessions/entry_test.go
internal/sessions/memory.go
internal/sessions/memory_test.go
internal/sessions/retry_test.go
internal/sessions/tool_call_test.go
internal/sessions/writer.go
internal/sessions/writer_test.go
internal/tools/doc.go
internal/tools/echo.go
internal/tools/echo_test.go
internal/tools/registry.go
internal/tools/registry_dispose_test.go
internal/tools/registry_test.go
internal/tools/types.go
internal/tools/validate_test.go
pkg/protocol/builder.go
pkg/protocol/builder_test.go
pkg/protocol/codec.go
pkg/protocol/content.go
pkg/protocol/content_test.go
pkg/protocol/events.go
pkg/protocol/events_test.go
pkg/protocol/hardening_test.go
pkg/protocol/lifecycle_events_test.go
pkg/protocol/message_test.go
pkg/protocol/queue_events_test.go
pkg/protocol/retry_events_test.go
```

## Exact fixture inclusion list

These two existing cassette changes update request tool order and paired responses.
They are test fixtures, not binaries or credentials.
Only include the approved lifecycle fixture updates; do not manually modify generated artifacts.

```text
cmd/tui/testdata/cassettes/TestCassetteOneToolCall.yaml
cmd/tui/testdata/cassettes/TestCassetteParallelToolCalls.yaml
```

## Documentation inclusion list

Use the following path list for the final lifecycle documentation changes.
Some phase 11 paths were unchanged at the initial snapshot and changed during the completed documentation work.
Include each path only if its final diff belongs to this lifecycle work.

```text
README.md
AGENTS.md
docs/ask-architecture-reference.md
docs/testing-llm-cassettes.md
internal/README.md
internal/agent/README.md
internal/pipeline/README.md
internal/sessions/README.md
internal/bus/README.md
internal/scheduler/README.md
internal/tools/README.md
internal/providers/README.md
pkg/protocol/README.md
plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md
```

The inspected roadmap changes add lifecycle decisions D19 and D23–D29 and their revision notes.
Those changes belong to this lifecycle work.
The roadmap is an owning phase 11 surface, even though it is in the earlier roadmap plan directory.
Do not stage that whole earlier plan directory.

## Exact lifecycle evidence inclusion list

Include these inspected lifecycle plan, review, test, and progress files when the user selects evidence for the documentation commit.
The final plan and progress records now mark all twelve phases complete.

```text
plans/261006-0933-lifecycle-event-pipeline-redesign/check-conformance-matrix.sh
plans/261006-0933-lifecycle-event-pipeline-redesign/conformance-matrix.md
plans/261006-0933-lifecycle-event-pipeline-redesign/phase-00-design-correction-and-conformance-matrix.md
plans/261006-0933-lifecycle-event-pipeline-redesign/phase-01-lifecycle-vocabulary-and-wire-events.md
plans/261006-0933-lifecycle-event-pipeline-redesign/phase-02-typed-control-dispatch-port.md
plans/261006-0933-lifecycle-event-pipeline-redesign/phase-03-typed-control-dispatch-semantics.md
plans/261006-0933-lifecycle-event-pipeline-redesign/phase-04-session-entry-writer.md
plans/261006-0933-lifecycle-event-pipeline-redesign/phase-05-observation-and-follow-path.md
plans/261006-0933-lifecycle-event-pipeline-redesign/phase-06-input-admission-and-queues.md
plans/261006-0933-lifecycle-event-pipeline-redesign/phase-07-typed-provider-failures.md
plans/261006-0933-lifecycle-event-pipeline-redesign/phase-08-model-attempt-and-retry.md
plans/261006-0933-lifecycle-event-pipeline-redesign/phase-09-tool-coordinator-and-repair.md
plans/261006-0933-lifecycle-event-pipeline-redesign/phase-10-e2e-fixtures-and-conformance-sweep.md
plans/261006-0933-lifecycle-event-pipeline-redesign/phase-11-roadmap-and-docs.md
plans/261006-0933-lifecycle-event-pipeline-redesign/plan.md
plans/261006-0933-lifecycle-event-pipeline-redesign/red-team-findings.md
plans/journals/2026-10-06-complete-the-lifecycle-and-event-pipeline-redesign.md
plans/reports/architecture-261006-ask-lifecycle-event-pipeline.md
plans/reports/docs-261006-phase11.md
plans/reports/git-261006-lifecycle-preflight.md
plans/reports/implementation-261006-1753-phase08-support.md
plans/reports/implementation-261006-1753-phase08.md
plans/reports/implementation-261006-phase09-support.md
plans/reports/implementation-261006-phase09.md
plans/reports/implementation-261006-phase10-support.md
plans/reports/implementation-261006-phase10.md
plans/reports/pm-261006-1753-lifecycle-progress.md
plans/reports/research-261006-0836-deepseek-tool-outcome-recovery.md
plans/reports/review-261006-0848-lifecycle-pipeline-report-audit.md
plans/reports/review-261006-1104-deepseek-lifecycle-source-audit.md
plans/reports/review-261006-1806-phase08.md
plans/reports/review-261006-phase09-support.md
plans/reports/review-261006-phase09.md
plans/reports/review-261006-phase10.md
plans/reports/review-261006-phase11.md
plans/reports/tester-261006-1753-phase07.md
plans/reports/tester-261006-1806-phase08.md
plans/reports/tester-261006-phase09.md
plans/reports/tester-261006-phase10.md
plans/reports/xia-261006-0247-deepseek-lifecycle-event-pipeline.md
tasks/lessons.md
tasks/todo.md
```

The exact completion journal and both phase 11 reports are now included above.
The local journal was validated through the CLI, as recorded by the controller.
Do not stage the entire reports or journals directory.

## Exact exclusions

```text
.cursor/skills/verify-ask-server/features/posts.md
tui
cmd/tui/tui
plans/261006-0130-h4-verification/
plans/reports/tester-261006-0130-h4-verification.md
plans/reports/researcher-261005-2151-pi-provider-model-catalog.md
plans/reports/xia-261005-1408-h4-more-wire-apis-pi-port-analysis.md
```

The posts skill edit changes unrelated REST verification behavior.
`tui` and `cmd/tui/tui` are Mach-O arm64 build outputs.
The H4 verification directory and report belong to earlier provider verification.
The earlier provider catalog report changes credential policy wording.
The earlier H4 report contains 479 added lines and 4 removed lines of provider/auth/tool design discussion.
Their ownership and commit intent are not settled by the lifecycle authorization, so exclude them from this lifecycle commit proposal.
Do not delete these files or discard their changes.
Do not include `.env` files, tokens, real credentials, private keys, personal data, CHANGELOG changes, or marked generated artifacts.

## Secret review and limits

The read-only scan covered 306 changed or untracked text files at inspection.
No changed `.env`, private-key file, credential file, or marked generated header was found by that scan.
No private-key marker, AWS access-key pattern, credential URL, GitHub token pattern, or bounded provider-key pattern identified a verified secret.
An initial unbounded key pattern matched parts of Ask document paths; the word-boundary check removed those false positives.
Credential assignment matches were checked by source location and test context.
Live test credentials come from environment reads, not checked-in values.
Auth, provider request, and session-log tests use short fixture tokens and credential canaries.
Examples include `TestCredentialCanariesNeverReachEntriesEventsOrRebuild`, `testAdapterCredentialCanaries`, and `TestPreparedHoldsNoCredentialValue`.
Do not classify those test canaries as actual credentials without new evidence.
The cassette headers inspected contain protocol headers rather than private credentials.
This is a bounded content review, not a claim that any possible secret can be detected by patterns.

## Completion addendum

The final phase 11 documentation review is approved with score 10/10 and zero open findings.
The independent review checked 199 local links with zero failures and found zero stale API references.
The documentation delivery and review reports name the verified source owners.
The plan status is `completed`, and the progress report records 12/12 completed phases.
The completion journal exists at the exact path listed above.
The current `git diff --check` passed during this completion update.
Product tests were not repeated because the documentation work changed no product code and the final review reused the completed phase 10 gates.
No files were staged or committed, and no push, pull request, or remote write was made.

## Final gate before any commit
- Refresh `git status` and review the exact inclusion list because other agents still work in this workspace.
- Keep all exclusions out of the staged diff.
- Inspect the final selected diff for secrets and whitespace before creating commits.
- The controller can now ask the user whether to commit the reviewed lifecycle scope.

No verified secret or Git whitespace error blocks this proposed scope.
The phase 11 acceptance gate is closed.
This commit proposal is ready for the user's commit choice.

## Unresolved questions

The earlier provider reports and H4 evidence remain excluded unless their owner confirms a separate commit scope.
The user's final commit choice remains with the controller.
