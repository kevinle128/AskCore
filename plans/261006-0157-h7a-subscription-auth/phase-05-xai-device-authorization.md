---
title: "xAI device authorization"
status: todo
---

# xAI device authorization

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
Add the native xAI device strategy and xAI Responses data/profile through existing shared services.
Dependencies: phase 3 lifecycle/commands; phase 4 final Responses validator; H4 wire gate for inference.
Device strategy tests can proceed before the Responses live gate.

## Execution record — 2026-10-06

This phase passed its four criteria and was checked through the CLI.
Device timing, pending/slow-down/denial/expiry/cancellation, refresh retention/replacement, no fallback, and xAI request profiles pass their recorded checks.
Ordinary live device authorization saved generation 1 with expiry and no pending fence.
The `grok-4.7` prompt ran one canonical echo tool, completed two assistant turns, reported usage, and settled once.
Login, prompt, and local logout returned exit code 0; the record was absent after logout.
All three provider records were absent after the final logout.
Opaque xAI tokens bind to credential generation; the implementation does not invent a verified account identity.
The shared [exact filesystem actor](../reports/tester-261006-0855-h7a-fsync-actor.md) passed all five cases twice with race instrumentation, and the reviewer closed the actor gate.
See the [full progress report](../reports/pm-261006-0823-h7a-progress.md), [live acceptance](../reports/tester-261006-0835-h7a-live-acceptance.md), and [final review](../reports/reviewer-261006-0843-h7a-final-review.md).

The source inventory and TDD steps below retain the accepted creation-time plan.
Use this execution record and the linked reports for implemented paths and executed checks.

## Requirements and architecture

The shared command selects xAI and its OAuth method; display device code/verification URL through private injected interaction.
Use the native `https://auth.x.ai/oauth2/device/code` and `https://auth.x.ai/oauth2/token` endpoints and required Grok/API scopes/client data verified from pinned source.
Do not invent a verified xAI account identity from an opaque token.
Bind replacement/discovery invalidation to credential generation when verified identity is unavailable.
Validate code strings, positive finite expiry, interval, HTTPS verification URLs and bounded token JSON.
Wait the server interval before the first poll; pending waits again.
Slow-down must never reduce the current interval; use the valid longer server interval or required additive delay when absent.
Handle both denial names, device expiry, operation deadline, cancellation and transport failures explicitly.
Bound every poll HTTP call by remaining device lifetime as well as total operation deadline.
Do not report success on callback/token receipt; commit the validated tagged credential under the shared revision contract.
Refresh retains the old refresh token only when this method explicitly omits a replacement; a supplied replacement commits with all metadata.
Do not copy that retention rule to ChatGPT rotation.
Use the same local logout and no-billed-fallback rules as other strategies.
Both xAI key and OAuth use `https://api.x.ai/v1/responses` with distinct provenance/billing hints.
Apply encrypted reasoning inclusion only for compiled models with verified reasoning support.
No Anthropic tool-name codec applies.
Final header/body/origin checks remain in the shared Responses wire owner; method protocol/polling stays in auth.

<!-- Updated: Review 2026-10-06 - native denial names and command test wiring. -->
## Accepted review contract

Recognize `access_denied` and `authorization_denied` as the two native denial values from pinned Pi.
Test each value through the real command boundary and assert terminal denial, no further poll and retention of the prior credential.
Use the phase-3 test subprocess composition, private auth HTTP client and injected clock/wait functions for first-poll and slow-down timing.
Keep the logical `auth.x.ai` URLs intact through final binding validation and intercept only their external HTTP boundary.
Use the shared refresh fence and shutdown wait for xAI refresh, including omitted replacement-token behavior.
Its retention rule does not permit a later automatic retry from an unresolved fence.

## Runtime Flow

Actor: user invokes shared login for xAI then a prompt.
Entry: cmd/tui auth handler → shared auth.Login; subsequent app-composed Agent.Prompt.
Path: device request → display validated code/URL → cancellation-aware wait/poll → validated revision commit → resolver → xAI Responses profile → real HTTP → canonical stream result.
Prepared state: old key/OAuth record, controlled clock barriers and external device/token/inference server scripts.
Observable result: polling request timestamps and states, exactly one saved record, xAI endpoint/provenance and tool/terminal output.
Keep auth/store/runner/agent/adapters real; mock external HTTP only.
The live device interaction does not need callback forwarding and runs on the inference host.

## Related Code Files

| Action | File | Rough size | Test impact |
| --- | --- | --- | --- |
| New | `internal/auth/xai.go`, `xai_test.go` | Medium. | Device state machine and refresh contract. |
| Existing, modify | `internal/auth/service.go`, `internal/app/module_auth.go` | Small. | Add strategy/data only. |
| Existing, modify | `internal/providers/api.go`, `catalog.go`, `catalog_test.go`, `keys.go`, `keys_test.go` | Small. | Compiled xAI model/method/env bindings. |
| Existing, modify | `internal/providers/openai/profiles.go`, `profiles_test.go`, `responses_test.go` | Small. | xAI route/reasoning and provenance. |
| Existing, modify | `cmd/tui/auth_test.go`, `auth_live_test.go` | Medium. | Device command-to-prompt and live route. |
| Existing, modify | `internal/auth/README.md` | Small. | Device/polling/identity constraints. |

The phase 4 profile files and phase 3 CLI/auth test files must exist first; they are existing-at-phase-entry paths, not baseline files today.
Do not build a second device polling framework for one strategy.

## Protected functions and callers

Shared auth.Login, auth.Resolve, store transactions and CLI dispatch are phase 1–3 contracts.
Responses buildResponsesCall/produce/final validation/Fold are phase 4/H4 contracts.
Keep API registry dispatch by wire API, not by vendor in agent.
Reuse phase 4 endpoint/profile protection and phase 3 external test transport seam.
Keep no-replay and exactly-once settlement contracts from phase 2.
Current baseline test counts/callers are in the scout; Pi xAI case counts and device waits are in the Pi report.

## Test scenario matrix

| Priority | Scenario | Required observation |
| --- | --- | --- |
| Critical | Invalid/unsafe URL, empty code, invalid expiry. | No display/activation or unsafe navigation. |
| Critical | First poll, pending, slow-down lower/higher/absent. | Measured schedule never polls too early or speeds up. |
| Critical | access_denied, authorization_denied, expiry and cancellation. | Terminal failure; old record retained; no later poll. |
| Critical | Hung poll during short remaining lifetime. | HTTP ends before device expiry/deadline. |
| Critical | Refresh omits/replaces token. | Correct method-specific retention/atomic replacement. |
| High | Login/logout/replacement and canceled rotation. | Shared revision/commit rules hold. |
| High | OAuth/key route with conflicting OpenAI env/headers. | xAI-bound credential and billing provenance only. |
| High | Reasoning/nonreasoning models and custom tools. | Conditional encrypted reasoning; no name remap. |
| Medium | Progress text and auth capture enabled. | Useful sanitized output and no credentials/artifacts. |

## Tests Before

1. Add failing built-command device cases using external server and deterministic time/request barriers.
2. Record first poll and every subsequent poll interval as observable HTTP evidence.
3. Protect shared ChatGPT/OpenAI Responses and Anthropic/key paths before adding xAI data/profile.

## Refactor

1. Implement only the xAI device/refresh strategy on the common HTTP and login lifecycle.
2. Register compiled xAI records, supported auth methods and Responses profile binding.
3. Add model-capability-based reasoning policy to the existing final Responses profile owner.
4. Reuse common command, store, resolver, logout and errors without vendor changes to agent.

## Tests After

1. Pass each device state through command → real strategy → real commit → next prompt.
2. Repeat one shared canceled-rotation/replacement case for this method's retention rule.
3. Verify actual outgoing route, credential isolation, reasoning condition and canonical tools.
4. Run authorized live xAI device login, tool prompt and local logout after offline/wire gates.

## Regression gate

Run `go test ./internal/auth ./internal/providers/openai ./internal/providers ./cmd/tui`.
Run relevant race tests, build and required lint/import checks.
Keep the H4 Responses gate explicit; device success alone is not subscription inference acceptance.

## Success Criteria

- [x] Every device state has a bounded command-to-result check and no orphan polling.
- [x] Method-specific refresh retention/replacement and common no-fallback rules pass.
- [x] Key/OAuth xAI Responses profiles work without changing canonical tools or agent vendor logic.
- [x] Authorized live xAI prompt/tool/logout evidence is recorded without secrets.

## Risk Assessment and rollback

If granted token/model access differs from pinned source, classify the denial and recheck native contract rather than infer identity/support.
If poll timing is violated, use deterministic elapsed/request barriers to fix the shared deadline path.
Disable xAI method/profile registration for rollback; preserve saved data and other routes.
