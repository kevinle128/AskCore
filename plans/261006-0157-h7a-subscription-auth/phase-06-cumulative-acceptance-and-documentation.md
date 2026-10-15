---
title: "Cumulative acceptance and documentation"
status: todo
---

# Cumulative acceptance and documentation

## Context and baseline

Read the [main contract and proof matrix](./plan.md), [local scout](../reports/researcher-261006-0157-h7a-local-runtime-scout.md) and [Pi runtime proof](../reports/researcher-261006-0157-h7a-pi-runtime-proof.md).
The scout supplies baseline test counts and production function callers.
The [contract review](./reports/contract-review.md) supplies option copies/literals and test constructor consumers.
These are source counts, not executed-test claims.
Use the current working tree, including unfinished H4 work, rather than HEAD alone.
All source paths below are relative to `/Users/dale/orca/workspaces/AskCore/master-2`.
New paths are proposed; existing paths are verified in source or the linked scout.
Use existing Go testing, httptest and subprocess patterns; mock only external provider endpoints and JWKS.

## Overview

Priority: P1.
Close the complete H7a scope with connected cross-provider checks, required quality gates and authorized live acceptance.
Dependencies: phases 1–5 and H4 Responses wire verification for ChatGPT/xAI.
Do not mark complete after only the first Anthropic prompt or mocked provider success.

## Execution record — 2026-10-06

All three ordinary production live login, echo tool prompt, and local logout routes pass with sanitized evidence.
Full tests, vet, compilation, lint with zero issues, required race suites, and final changed app/provider and signed ChatGPT command races pass.
The final review reports no confirmed open critical defect.
Real replacement-write-failure, SIGKILL, and lost-response restarts pass with zero extra exchanges.
Owning docs and local links were checked; all isolated provider records were removed by logout.
Faux named-choice script, record, and value-copy assertions passed their targeted race check in 1.561 seconds.
The [exact filesystem actor](../reports/tester-261006-0855-h7a-fsync-actor.md) passed all five cases once with race instrumentation in 19.50 seconds.
After host disk-space recovery and an owned cache rebuild, its final repeated race run passed all ten cases in 38.186 seconds with exit code 0 and no race or cleanup diagnostic.
The [test setup](../../internal/testsupport/README.md#filesystem-fault-tests) links the tagged actor and machine-owned Linux runner.
Missing fixture setup fails explicitly; unselected or partially passed tests do not close the acceptance gate.
The final review closed the actor gate with score 9.5/10, and final tagged lint passed with zero issues.
This phase passed its five criteria and was checked through the CLI.
H7a is complete with 6/6 phases and 22/22 criteria.
The controller removed the owned test VM and its cache; no owned test process remains.
The user's existing Docker processes and Podman default connection are unchanged.
See the [full progress report](../reports/pm-261006-0823-h7a-progress.md), [live acceptance](../reports/tester-261006-0835-h7a-live-acceptance.md), and [final review](../reports/reviewer-261006-0843-h7a-final-review.md).

The source inventory and TDD steps below retain the accepted creation-time plan.
Use this execution record and the linked reports for implemented paths and executed checks.

## Requirements and architecture

Run the same ordinary constructors in command tests, headless inference and later fx composition checks.
Keep test auth/inference/JWKS servers external to the real internal flow.
Use isolated 0700 homes, 0600 credentials and deterministic barriers for two-process refresh/logout/login races.
Verify key→OAuth→key replacement and local-only logout for each provider without a method collection.
Test multi-provider concurrent requests with shared canonical transcript/tool snapshots and independent immutable auth/name state.
Verify each independently selectable interaction/override/profile field in the main matrix.
Run print and JSON error/signal checks without changing existing public exit conventions.
Prove one settled Result and at most one terminal stream event and public lifecycle sequence for every early/late auth/stream failure.
No automatic billed fallback, spent-allowance retry or transparent replay after public text/tool output is allowed.
Protect existing H3/H4 key-only behavior, replay, tool arguments/results, stream pairing and fork retry invariants.
Record live evidence only from authorized eligible accounts on the inference host.
Each live route records sanitized command/provider/method/model/profile, terminal outcome, tool event/usage and local logout result.
Do not capture token exchanges, credentials, auth codes, ID-token hints or sensitive account headers.
A live request cannot prove concurrency/identity fault coverage; retain offline deterministic checks too.

<!-- Updated: Review 2026-10-06 - cumulative restart, shutdown and command proof. -->
## Accepted review contract

Reuse the real phase-3 command test composition for all offline native login-to-next-prompt cases.
Ordinary production binaries keep separate help/signal/pipe smoke checks; no helper-level OAuth call replaces command parsing/dispatch proof.
Add process-A death/lost-response/replacement-failure cases, then invoke a process-B prompt and assert zero reuse of the fenced rotating grant.
Test pending-fence publication failure before network I/O and explicit login/logout recovery under the same revision contract.
Hold a commit barrier past the old two-second grace, deliver SIGINT/SIGTERM/SIGHUP and a second ordinary signal, then verify commit or classified bounded uncertainty before exit.
Forced death and blocked OS sync remain unavoidable limits; the durable fence must make later behavior safe.
Repeat oversize callback/stdin, exact xAI denial variants, auth capture isolation and every changed option copy/encode-or-reject path.
Recheck the contract review inventory against current source after prior phases, including faux records and all three body builders.

## Runtime Flow

Actor: user invoking each built auth command, prompt and local logout; request callers exercising shared library APIs.
Entry: real command parsing/dispatch and Agent.Prompt/SetModel through the same constructors.
Offline tests use the compiled command test composition; live checks use the ordinary ask binary.
Path: native interaction → validated durable save → shared readiness/resolution → actual adapter HTTP → canonical events → local logout → next resolution.
Prepared state: separate homes, canonical tool/thinking transcript, expired records and controlled external failure endpoints; real eligible accounts for live gates only.
Observable result: all matrix capabilities with complete real-owner path, three provider-live successes, no secret artifact or owned process leak.
Tests start at the actor boundary; a seeded record/helper test cannot replace a native login command.

## Related Code Files

| Action | File | Rough size | Test impact |
| --- | --- | --- | --- |
| Existing, modify | `cmd/tui/auth_test.go`, `auth_live_test.go`, `headless_test.go`, `headless_cassette_test.go` | Medium. | Cumulative built-command/output/capture acceptance. |
| Existing, modify | `internal/auth/refresh_test.go`, `discovery_test.go` | Small. | Cross-method races and generation invalidation. |
| Existing, modify | `internal/agent/agent_model_test.go`, `loop_stream_test.go` | Small. | Shared readiness/provenance/terminal lifecycle. |
| Existing, modify | `internal/app/module_auth_test.go`, `app_test.go` | Small. | Ordinary/fx composition without DB/servers for headless. |
| Existing, modify | `README.md`, `internal/auth/README.md`, `internal/settings/README.md`, `internal/providers/README.md` | Small. | Command, schema, owner and user behavior. |
| Existing, modify | `docs/ask-architecture-reference.md`, `docs/testing-llm-cassettes.md`, `internal/README.md`, `internal/app/README.md`, `AGENTS.md` | Small. | Actual new package/constructor/import and capture contracts only. |
| Existing, update when authorized | Parent H7a/roadmap completion record. | Small. | Accurate state and evidence links; CLI owns plan status. |

README/doc targets are verified owners from the root README and package navigation; read each before editing.
Auth and module files listed here are created by earlier phases and exist at phase entry.
No new evergreen audit/report framework is needed.
Never edit generated changelogs.

## Protected functions and callers

Preserve cmd/tui run, runHeadless, newHeadlessAgent/ordinary app constructor and signalExitCodes.
Preserve Agent.SetModel idle semantics, hooks and canonical message/tool event ownership.
Preserve Provider.Stream/StreamFn and Stream.Result/Assembler.Fail one-settlement contract.
Protect actual SDK final request isolation, H4 Responses replay/status/usage and maxRetries=0 tests.
Use the production-call and baseline-test scout together with the complete contract review inventory, refreshed for prior-phase changes.

## Test scenario matrix

| Priority | Scenario | Required observation |
| --- | --- | --- |
| Critical | Three native login→prompt/tool→logout live flows. | Correct method/profile/account route and no fallback. |
| Critical | Two processes refresh plus logout/replacement/crash points. | One rotation; revision guards; explicit uncertainty. |
| Critical | All override/identity/endpoint/final profile cases. | No wrong account, billing route or credential leak. |
| Critical | Early/late stream error, cancellation/full buffer. | One settled result; no duplicated public lifecycle or producer leak. |
| High | Parallel providers with shared transcript and tool changes. | Canonical history/arguments/results unchanged. |
| High | API-key compatibility and Messages↔Responses replay. | Existing route contracts preserved. |
| High | Capture, logs, print/JSON and committed-save failure. | No canary secret; accurate sanitized result. |
| Medium | Remote-host instructions, callbacks and local logout help. | Supported operation is clear and executable. |

## Tests Before

1. Write cumulative failing command scenarios for any matrix row not already connected through its nearest real caller.
2. Add external server faults after public text/tool output and assert one HTTP request, partial content and one terminal lifecycle.
3. Prepare subprocess lifecycle checks that retain command/PID/port/worktree ownership and deterministic cleanup barriers.

## Refactor

1. Repair integration gaps at their shared owner, without duplicate services or weakened assertions.
2. Update only owning docs for actual user behavior, commands, package boundaries and dependency changes.
3. Describe pre-rename preservation, post-rename uncertainty, remote/disk non-atomicity, local-host scope and provider-specific login limits accurately.
4. State Pi production trigger adaptation and user-selected one-record/local-logout policy without claiming provider approval from source parity.

## Tests After

1. Run all focused connected suites and inspect actual output/help as an end user.
2. Run relevant race/subprocess suites and confirm all owned listeners/processes stop cleanly.
3. Run required package/full tests, build and lint/import gates; fix failures instead of hiding them.
4. Run authorized live routes as ready, preserving the first-ready early checkpoint and requiring all three for completion.
5. Review all proof rows and docs links/claims against test artifacts/current source without storing secret evidence.

## Regression gate

Run `go test ./internal/settings ./internal/auth ./internal/app ./internal/agent ./internal/providers/... ./cmd/tui` first.
Then run `go test -race ./internal/settings ./internal/auth ./internal/app ./internal/agent ./internal/providers/... ./cmd/tui`.
Run `go test ./...`, `go build ./...` and `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` for changed public/import contracts.
Run opt-in live auth checks separately; no fixture or public model list substitutes for eligibility.
If unrelated existing failures appear, diagnose and repair them under repository rules without concealing their origin.

## Success Criteria

- [x] Every main proof row is implemented and tested through its stated actor boundary.
- [x] Authorized live Anthropic, ChatGPT and xAI routes and local logout pass with sanitized evidence.
- [x] Existing key/replay/tool/stream contracts and all required quality gates pass.
- [x] Owning docs are accurate and links valid; no secrets/generated files or owned background processes remain.
- [x] CLI-managed status changes occur only after all required work and acceptance pass.

## Risk Assessment and rollback

Missing authorized accounts or provider acceptance blocks live completion, not the usefulness of offline design/tests.
If any live route is rejected, report its exact sanitized failure and keep the plan incomplete until the route is repaired/approved scope changes.
Rollback new registrations before runtime/store types; retain credentials, unknown fields and legacy API-key registrations.
Stop only owned processes and close callback/poll resources before removing isolated homes.
