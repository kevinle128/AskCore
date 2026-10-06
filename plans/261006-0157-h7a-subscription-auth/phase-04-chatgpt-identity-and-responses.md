---
title: "ChatGPT identity and Responses"
status: todo
---

# ChatGPT identity and Responses

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
Add verified ChatGPT sign-in, returning client reuse, account discovery and a validated Responses profile.
Dependencies: phase 3 shared services; H4 Responses adapter/fork verification gates wire tests and live inference only.
Identity and login work can start before that wire gate passes.

## Execution record — 2026-10-06

This phase passed its four criteria and was checked through the CLI.
Signed identity, issued-client reuse, one-account replacement, unknown/denied access, stale generation rejection, model/thinking preservation, and serialized Responses checks pass.
Ordinary native ChatGPT login saved generation 1 with expiry and no pending fence.
The live account hid GPT-5.5 and listed GPT-5.6 Sol; readiness rejected the hidden model before inference.
A static `gpt-5.6-sol` catalog row was selected explicitly for successful live acceptance.
The existing GPT-5.5 default was preserved.
The live Sol prompt ran one canonical echo tool, completed two assistant turns, reported usage, and settled once.
Login, prompt, and local logout returned exit code 0; the record was absent after logout.
The signed command regression and race check prove hidden-default refusal and explicit listed-model inference.
The accepted one-record and local-only logout choices do not imply full provider account/session-guidance compliance.
The shared [exact filesystem actor](../reports/tester-261006-0855-h7a-fsync-actor.md) passed all five cases twice with race instrumentation, and the reviewer closed the actor gate.
See the [full progress report](../reports/pm-261006-0823-h7a-progress.md), [live acceptance](../reports/tester-261006-0835-h7a-live-acceptance.md), and [final review](../reports/reviewer-261006-0843-h7a-final-review.md).

The source inventory and TDD steps below retain the accepted creation-time plan.
Use this execution record and the linked reports for implemented paths and executed checks.

## Requirements and architecture

Register ChatGPT under OpenAI with the shared login/logout service and command.
Use phase 3 callback/input/HTTP/store services; do not repeat common command or file logic.
Use PKCE, independent state/nonce, stable host identifier and exact attempt callback URI.
Pi's native reference uses `http://127.0.0.1:1455/auth/callback`; documented supported port variation must retain the exact attempt URI in authorization and exchange.
No arbitrary externally bound listener or silently changed redirect is permitted.
New registration receives the issued client from the validated callback.
Returning sign-in reuses the saved issued client and verified account; reject changed client or subject rather than silently replacing an unrelated account.
`ask auth login --provider openai --method openai-chatgpt --new-account` explicitly starts dynamic registration for a new account, captures the current revision and replaces the one record only after verified commit.
If that attempt fails, the prior record remains usable; logout-first is not a substitute.
After local logout, ordinary login has no saved client and starts fresh registration.
Do not add a registration collection/picker or retain client/account mappings after local logout.
Verify signature, issuer, audience/issued client, expiry and nonce with a maintained OIDC library.
Select and pin the smallest maintained library that meets those checks; do not implement JWT crypto or treat token presence/decoded claims as verification.
Constrain algorithms, issuer/JWKS sources and bounded key refresh; no arbitrary token-provided key URL.
Validate granted inference scopes and complete replacement tokens/metadata before commit.
Retain ID-token hint only inside the saved secret record and redact it from logs/URLs.
Refresh uses saved issued client and shared actual-expiry/earliest-refresh policy.

## Discovery and Responses profile

Authenticated discovery uses the same resolver/account as inference and its documented account models schema.
Keep visible slugs in server order and join compiled metadata; do not invent price/context/reasoning support.
Cache access by provider/method/issued client/verified identity and committed generation; stale responses cannot publish after replacement.
Unknown access permits inference only with valid auth and required metadata/capabilities; known denial blocks readiness/request.
Discovery failure cannot select key billing or a different account.
Profile uses public HTTP Responses, stream=true, store=false, full local input and no previous_response_id.
Encode system instructions as supported instructions/developer input, not explicit system input items.
Verify documented function/custom tool grouping against current endpoint documentation and actual serialized SDK output.
Forbidden fields include background, conversation, max_output_tokens, max_tool_calls, metadata, moderation, multi_agent, prompt, prompt_cache_retention, safety_identifier, temperature, top_logprobs, top_p, truncation and user.
Test each forbidden field independently after SDK serialization, sampling options and extra body.
Omit unsupported generated defaults; reject explicit unsupported options with a clear error.
Final header/URL/body validation must survive injected clients and model headers; no competing credentials or ambient project/org/account leak.
Keep restriction data in the adapter, not auth/CLI.
Classify allowance exhaustion independently from rate limit, missing auth, unsupported request, transport and storage errors.
Expose the class for H9 without adding automatic replay; no replay after public text/tool output.

<!-- Updated: Review 2026-10-06 - command harness, input bounds and consumer contracts. -->
## Accepted review contract

Use the phase-3 real command boundary and subprocess test composition for fixed logical OAuth/JWKS/discovery URLs.
Verify signed ID tokens with the real selected verifier; intercept only external HTTP, with no production endpoint or identity bypass.
Apply the shared 16 KiB callback/private-input limits before parsing, and test oversize full redirect input without save or exchange.
The refresh fence and bounded shutdown wait apply to ChatGPT rotation; returning login and explicit new-account login remain separate operations.
Reference the contract review for one production and six test NewResponses callers, instead of attributing test counts to the production-only scout.
Nonempty named choice is encoded against canonical declarations or rejected before HTTP; it must not disappear when grouped tools are built.

## Runtime Flow

Actor: user chooses shared auth login with OpenAI/ChatGPT method, then prompts/selects the model.
Entry: cmd/tui auth handler → shared auth.Login; next command → app-composed Agent.Prompt/SetModel.
Path: native callback/manual input → token response → maintained OIDC verification → bound revision commit → resolved discovery/readiness → Responses profile/final validation → stream result.
Prepared state: isolated home, signed test ID tokens and local JWKS/auth/discovery/inference servers; returning saved client/identity variant.
Observable result: verified saved account/client, selected visible model, allowed final wire request, canonical tool output and one settlement.
Mocks are only those external endpoint/JWKS boundaries; use real library verifier and internal services.
Live route and grouped-tool acceptance need an authorized eligible account after H4 wire verification.

## Related Code Files

| Action | File | Rough size | Test impact |
| --- | --- | --- | --- |
| New | `internal/auth/chatgpt.go`, `chatgpt_test.go`, `identity.go`, `identity_test.go` | Large. | OIDC and client/account attempt binding. |
| New | `internal/auth/discovery.go`, `discovery_test.go` | Medium. | Account schema, access states and generations. |
| Existing, modify | `internal/auth/service.go`, `resolve.go`, `internal/app/module_auth.go` | Small. | Strategy/data registration and shared readiness. |
| Existing, modify | `internal/providers/api.go`, `catalog.go`, `catalog_test.go` | Small. | ChatGPT method/profile and compiled metadata. |
| Existing, modify | `internal/providers/openai/responses.go`, `responses_prompt.go`, `isolate.go` | Large. | Profile-aware final request/header validation. |
| New | `internal/providers/openai/profiles.go`, `profiles_test.go` | Medium. | ChatGPT/xAI strategy-independent wire policy. |
| Existing, modify | `internal/providers/openai/responses_test.go`, `isolate_test.go`, `internal/providers/errors.go` | Medium. | Actual final JSON, identity headers and error classification. |
| Existing, modify | `cmd/tui/auth_test.go`, `auth_live_test.go`, `internal/agent/agent_model_test.go` | Medium. | Login-to-next-prompt and readiness. |
| Existing, modify | `go.mod`, `go.sum`, `internal/auth/README.md`, `AGENTS.md` | Small. | Approved verifier dependency and owner documentation. |

AGENTS.md changes here are limited to the real dependency/package feature update required by repository rules.
Do not edit generated bindings or changelogs.

## Protected functions and callers

NewResponses has one production and six test callers, enumerated in the contract review; the scout records the production caller only.
Responses Stream/produce → TransformMessages → buildResponsesCall → isolateClient/fantasykit.Client → fantasy.Stream → Fold is the current path.
Preserve all H4 replay, stop, usage, encrypted reasoning, error/status and maxRetries=0 checks.
Isolate currently applies model headers after the SDK headers, which can replace Authorization; protect profile-owned headers in the final selected-auth path.
Common login/logout remains phase 3 code; additions are only ChatGPT strategy, discovery, profile and registrations.

## Test scenario matrix

| Priority | Scenario | Required observation |
| --- | --- | --- |
| Critical | Signature/issuer/audience/expiry/nonce independently wrong. | No save or inference; old record intact. |
| Critical | New registration, returning saved-client login, wrong client/subject. | Correct bound client/account, explicit conflict. |
| Critical | Explicit --new-account replacement and failed attempt. | New registration and one-record commit; old record remains on failure. |
| High | Logout then ordinary sign-in. | New registration with no retained client mapping. |
| Critical | Missing scopes/token rotation and earliest-refresh gate. | No activation or premature exchange. |
| Critical | Every forbidden final field/default/extra-body path. | Rejected explicit field; omitted unsupported default. |
| Critical | Protected credential/identity headers and wrong origin. | No SDK/model-header bypass or key fallback. |
| Critical | Known denial versus unknown access/capability. | Correct readiness/request policy and unchanged selection on failure. |
| High | Stale discovery after account replacement. | Old view cannot publish or authorize. |
| High | Grouped tools and full local reasoning/tool history. | Supported final form and canonical replay. |
| High | HTTP/stream allowance, rate limit, partial output then failure. | Distinct class, one request and settlement. |
| Medium | Callback/manual race, wrong-state error, port/spare connection. | Attempt remains secure and all resources close. |

## Tests Before

1. Add built-command failing tests for new, returning and explicit replacement ChatGPT login, then real next prompt.
2. Issue signed test tokens/JWKS and vary each identity claim/security failure independently with the maintained verifier.
3. Add connected SetModel/Prompt discovery unknown/denied/stale-generation cases.
4. Protect H4 Responses replay/usage/error and OpenAI key-only request suites before profile edits.

## Refactor

1. Select/pin verifier and implement native ChatGPT strategy on the common lifecycle.
2. Add authenticated discovery and generation-keyed readiness using the same resolver.
3. Register compiled method/profile/model data; retain one provider record and local-only logout.
4. Add adapter-owned ChatGPT profile and final serialized validation after SDK/user options.
5. Map allowance/rate/auth errors without duplicate settlement or new retry ownership.

## Tests After

1. Pass all identity/client/scope cases through the common command, not direct callback helper as E2E proof.
2. Probe actual final request for all restrictions, local-history encoding and grouped tools.
3. Verify account replacement invalidation, no retained registration after logout and no secret capture.
4. Run authorized live ChatGPT prompt/tool/logout only after H4 wire prerequisites pass; keep sanitized evidence.

## Regression gate

Run `go test ./internal/auth ./internal/providers/openai ./internal/agent ./cmd/tui`.
Run relevant race checks, `go build ./cmd/tui`, required lint/import checks and H4's documented Responses/fork gate.
Check the currently installed fork version rather than assuming historical analysis means completion.

## Success Criteria

- [x] Verified identity, issued-client reuse and one saved account are proved through the common command.
- [x] Discovery distinguishes unknown/denied/unknown capability and rejects stale generations.
- [x] Actual Responses body/headers meet the supported subscription contract; key callers remain valid.
- [x] Live eligible ChatGPT route/tool/logout acceptance is recorded without secrets.

## Risk Assessment and rollback

If current grouped-tool or client registration is rejected live, stop that profile and update only the verified adapter/strategy contract.
The user chose reduced Pi account/logout scope; do not silently add multi-registration or revocation to address documentation concerns.
Disable ChatGPT registration/profile first for rollback, preserving all saved/unknown data and key-only Responses behavior.
