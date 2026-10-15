---
title: "Auth resolution and request binding"
status: todo
---

# Auth resolution and request binding

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
Deliver one constructor-injected resolver, composed request runner and readiness contract.
Dependency: phase 1; H4 key-only selection tests are protected behavior, not a whole-phase blocker.
Phase 3 supplies the final command-to-runtime proof.

## Execution record — 2026-10-06

The shared resolver/readiness seam, precedence, request binding, refresh races, native model/thinking preservation, and one-settlement behavior pass their recorded checks.
The command restart matrix passes for actual SIGKILL, lost response, and real replacement-write failure.
The second process sends no extra refresh or inference exchange and preserves the pending fence.
All three provider live routes pass through ordinary app composition.
The [exact filesystem actor](../reports/tester-261006-0855-h7a-fsync-actor.md) now blocks the real replacement-file sync after the validated response and temp-file write.
It passed SIGINT, SIGTERM, SIGHUP, commit-budget expiry, and forced death once with race instrumentation in 19.50 seconds.
The separate process observes durable replacement bytes before ordinary signal exit; both uncertainty cases retain the fence and forbid grant reuse.
The observer reads the owned backing directory because Linux serializes a mounted temp-file read behind its active sync.
The CLI uses the real FUSE mount for all store operations, with no production hook.
The earlier repeated run encountered disk exhaustion.
After space recovery and removal of the damaged owned build cache, a cold rebuild passed all five cases twice with race instrumentation in 38.186 seconds.
The reviewer closed the exact actor gate with score 9.5/10.
The final tagged lint check passed with zero issues.
This phase passed its three criteria and was checked through the CLI.
See the [full progress report](../reports/pm-261006-0823-h7a-progress.md), [live acceptance](../reports/tester-261006-0835-h7a-live-acceptance.md), and [final review](../reports/reviewer-261006-0843-h7a-final-review.md).

The source inventory and TDD steps below retain the accepted creation-time plan.
Use this execution record and the linked reports for implemented paths and executed checks.

## Requirements and architecture

Auth owns supported method registration, credential precedence, actual-expiry scheduling, refresh and structured errors.
Providers import neither auth nor settings; auth imports no agent, wire adapter, config or transport handler.
App owns wiring of a resolver around the existing registry StreamFn and injects the same readiness function into Agent.SetModel.
Use a typed constructor function seam in LoopConfig/Config; do not add vendor logic to agent.
Do not place orchestration in provider core merely to avoid the app seam.
Readiness and request resolution share binding/access checks; readiness success does not claim inference success.
Resolve after PrepareRequest and the existing GetAPIKey hook, including every subsequent tool-loop request.
A supported explicit provider-bound key wins; a saved tagged credential wins over env; env is eligible only without a saved record.
Reject competing nonempty typed and legacy overrides at the caller boundary.
Preserve nonempty GetAPIKey as a supported key override and empty hook as normal fallback.
Keep BoundKey isolation when the final provider differs from the CLI provider.
Refresh uses one method-specific lead time on actual expiry: Anthropic/xAI ten minutes and ChatGPT eight minutes.
Respect earliest_refresh_at; use an otherwise valid token before that gate or return bounded retry-after/auth error when required lifetime cannot be met.
Lock, reread and recheck revision/method/account/deletion/expiry before a bounded exchange.
Use the newest compatible committed credential if another process refreshed it; do not rotate stale account state.
Validate token response before merging; after validated rotation commit with an independent bounded local context even if inference was canceled.
No OAuth failure falls through to key billing, and ambiguous rotating grants are never blindly retried.

<!-- Updated: Review 2026-10-06 - restart fence and bounded shutdown drain. -->
## Accepted review contract

Under the held store lock, commit the pending refresh fence before sending the rotating grant.
A restart or another caller that observes an unresolved fence must require explicit recovery/reauthentication without another exchange.
After validating the response, commit all replacement metadata and clear the matching attempt fence in one durable transaction.
A lost response or replacement failure leaves the fence unresolved; error handling in the first process alone is insufficient.
Register each potentially rotating auth operation with the auth service before the exchange can start, and unregister it only after commit or a classified failure.
Expose one bounded wait function from the shared app constructor; do not add a second shutdown framework or an interface for one implementation.
Use a 15-second maximum refresh exchange and a 5-second local commit budget, each measured from its own start.
The shutdown wait must cover the remaining exchange plus commit budget and cleanup; it cannot reuse the two-second inference grace as the auth deadline.
Check cancellation between local I/O steps, but do not claim that a context can interrupt a blocked OS sync call.
On the first signal, stop new work and cancel inference, then wait for registered auth work through its own bound before process exit.
A second ordinary signal does not shorten a validated commit's budget.
If the budget ends or the process is forcibly killed, retain the durable fence and report uncertainty where output is still possible.
Preserve exit codes 130, 143 and 129; the extra wait changes cleanup behavior, not the signal code.
Test each signal with a commit barrier beyond two seconds, release it before the commit deadline, and check new bytes from another process before exit.
Also test budget exhaustion and forced death, then prove the next process sends no refresh request from the fenced record.

## Runtime Flow

Actor: headless agent/request caller or idle SetModel caller.
Entry: Agent.Prompt or Agent.SetModel with app-composed real services.
Path: final-model hook → legacy override → composed runner → resolver/store/strategy → immutable binding → registry → wire → Stream/Agent settlement.
Readiness path: SetModel → injected same auth/binding readiness → idle recheck → accepted model.
Prepared state: real temp store, expired/valid credentials, conflicting env keys, two request subprocesses and external token/inference servers.
Observable result: selected credential at HTTP, correct provenance, unchanged model on failure and exactly one terminal result.
Phase 3 adds the built-command version of the same connected path.

## Related Code Files

| Action | File | Rough size | Test impact |
| --- | --- | --- | --- |
| New | `internal/auth/types.go`, `service.go`, `resolve.go`, `resolve_test.go` | Large. | Real store/resolver and precedence. |
| New | `internal/auth/refresh_test.go`, `errors.go` | Medium. | Two-process rotation and classes. |
| New | `internal/app/module_auth.go`, `module_auth_test.go` | Medium. | Shared constructors, runner/readiness; no DB startup. |
| Existing, modify | `internal/agent/agent.go`, `loop_run.go`, `types.go` | Small. | Optional readiness function and error compatibility. |
| Existing, modify | `internal/agent/loop_stream.go`, `agent_model_test.go`, `loop_stream_test.go` | Medium. | Final-provider resolution and settlement. |
| Existing, modify | `internal/providers/keys.go`, `keys_test.go`, `errors.go` | Small. | Bound/legacy key contracts and structured classes. |
| Existing, modify | `.golangci.yml` | Small. | Auth capability import bans. |
| New | `internal/auth/README.md` | Small. | Auth ownership and constructor contracts. |

## Protected functions and callers

ResolveKey has two production callers: Agent.SetModel in agent.go and loop.apiKey in loop_stream.go; keys_test.go is its direct unit consumer.
SetModel production/public runtime use is its exported Go API; current direct tests are agent_test.go and agent_model_test.go.
The latter includes Messages→Completions/Responses switch, key pinning, replay, readiness failure and thinking clamp checks.
Preserve prepareRequest in loop_run.go, called by the prepare stage in loop_stage.go before streamAssistantResponse.
Preserve streamAssistantResponse's ownership of MessageStart/MessageEnd and reading Stream.Result after Events closes.
Early resolver failure must create a failing Stream through NewStream/Assembler.Fail, not return both a stream failure and a loop error.
The existing GetAPIKey error still propagates to Agent's wrapper, which owns one failure lifecycle sequence.
Reuse existing SetModel idle/second-idle checks so asynchronous readiness cannot apply a model after a run starts.
Production function counts and baseline test counts are in the scout; option literals, copy boundaries and test constructors are in the contract review.
Recheck the affected inventory if H4 changes the working tree.

## Test scenario matrix

| Priority | Scenario | Required observation |
| --- | --- | --- |
| Critical | Every precedence branch and competing overrides. | Correct selected credential; zero billed fallback. |
| Critical | PrepareRequest changes provider; next tool request. | Fresh final-provider generation; pinned key cannot leak. |
| Critical | Two processes refresh one expired record. | One token rotation and preserved unrelated providers. |
| Critical | Logout/replacement while caller waits. | No stale refresh or credential resurrection. |
| Critical | Cancel after validated rotation, disk failure, lost response. | Durable commit before cancel; safe error on uncertainty. |
| Critical | Early resolver error and canceled full stream. | One Result; at most one terminal; one public lifecycle sequence. |
| High | Readiness valid/denied/mismatched/busy. | Same binding rules; prior state retained on failure. |
| High | Actual expiry, refresh lead and earliest-refresh. | No timing drift or repeated early exchange. |
| High | Wrong origin/path/redirect/header override. | No secret reaches wrong destination. |
| Medium | Parallel request snapshots. | Independent credentials/decoder and no shared mutation. |

## Tests Before

1. Add failing connected Prompt/SetModel tests against real app/auth/store with external HTTP servers.
2. Protect existing ResolveKey and H4 SetModel, stream lifecycle and hook-error cases before changing signatures/fields.
3. Add deterministic subprocess barriers for one rotation, canceled waiter, deletion/replacement and cancel-after-validation.

## Refactor

1. Add method functions and supported binding registry to auth; validate provider/method/profile/API/origin combinations at composition.
2. Implement Resolve and refresh transaction on phase 1 store contracts.
3. Compose StreamFn and readiness once through ordinary app constructors that later fx modules can reuse.
4. Add optional readiness injection without importing auth into agent.
5. Route each early auth failure through one settled failing stream and preserve the legacy hook-error contract.

## Tests After

1. Pass the matrix through real nearest callers and verify actual outgoing credential headers.
2. Assert every early/late error emits exactly one assistant end and one agent settlement; canceled full buffer still settles Result.
3. Assert no refresh lock is held during model streaming and no internal service was mocked in E2E cases.

## Regression gate

Run `go test ./internal/auth ./internal/app ./internal/agent ./internal/providers`.
Then run race checks for these packages and the required import/lint gate.
Phase 3 must repeat these scenarios through the real command subprocess composition where a user command exists.

## Success Criteria

- [x] One shared resolver/readiness seam is used without dependency cycles or vendor branches in agent.
- [x] Precedence, refresh races, cancellation and endpoint binding are covered through real callers.
- [x] Existing key-only hooks, model switching and exactly-once stream settlement remain valid.

## Risk Assessment and rollback

If H4 changes SetModel or stream fields, integrate its protected tests rather than overwrite that work.
If token exchange may have succeeded but response/storage is uncertain, report recovery guidance without another rotating request.
Rollback disables composed subscription registrations and retains legacy key seams and credential data.
