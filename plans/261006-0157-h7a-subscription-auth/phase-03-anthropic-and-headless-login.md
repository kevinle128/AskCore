---
title: "Anthropic and headless login"
status: todo
---

# Anthropic and headless login

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
Deliver the shared command lifecycle, API-key save, local logout and first usable Anthropic subscription prompt.
Dependencies: phases 1–2 and H3 Messages only.
Phases 4–5 add strategies/records to these services; they do not build another CLI/store/runner.

## Execution record — 2026-10-06

This phase passed its three criteria and was checked through the CLI.
Connected command tests cover key save, explicit method selection, both Anthropic interactions, profile/name round trips, callback cleanup, capture isolation, and local logout.
Ordinary live Anthropic login saved generation 1 with expiry and no pending fence.
The `claude-sonnet-4-6` prompt ran one canonical echo tool, completed two assistant turns, reported usage, and settled once.
Login, prompt, and local logout each returned exit code 0; the record was absent after logout.
All three provider live routes are recorded.
The shared [exact filesystem actor](../reports/tester-261006-0855-h7a-fsync-actor.md) passed all five cases twice with race instrumentation, and the reviewer closed the actor gate.
See the [full progress report](../reports/pm-261006-0823-h7a-progress.md), [live acceptance](../reports/tester-261006-0835-h7a-live-acceptance.md), and [final review](../reports/reviewer-261006-0843-h7a-final-review.md).

The source inventory and TDD steps below retain the accepted creation-time plan.
Use this execution record and the linked reports for implemented paths and executed checks.

## Requirements and architecture

Use `ask auth login --provider <id> --method <id>` and `ask auth logout --provider <id>`.
Omitted method asks the user to choose a supported method; noninteractive omission returns a clear error.
API-key login uses `--method api-key` and private stdin input, never a key argument.
Anthropic subscription uses `--method anthropic-oauth` and explicit `--interaction browser` or `--interaction copy-code`.
Built-in method IDs are `api-key`, `anthropic-oauth`, `openai-chatgpt` and `xai-oauth`; help and compiled registrations must use the same IDs.
Reserve `--new-account` for explicit ChatGPT account replacement; other providers reject that unsupported option.
Dispatch auth before normal parseArgs, readPrompts and startCapture so login text cannot become a model prompt.
Use ordinary shared app constructors for login and inference with no SQLite/gateway dependency.
Preserve runHeadless print/JSON and signal exit behavior.
Login runs on the inference host's ASK_HOME; stdout/stderr show only sanitized progress/result, not credentials or callback codes.
Browser mode binds loopback and uses the native registered `http://localhost:53692/callback`.
Copy-code mode uses `https://platform.claude.com/oauth/code/callback` and no local listener.
Use native PKCE and attempt-bound state; browser/full-redirect input requires exact URI/state and copy-code alone accepts its supported bare code form.
Do not silently choose another fixed callback port, kill its owner or bind on all interfaces.
A port failure is explicit; the user can select supported copy-code interaction.
Bound total login and each HTTP exchange/response size; use no-redirect token clients.
Close listener, spare connections and unused input waiters on every exit.
Do not hold the store lock while waiting for the user; compare captured revision at validated final commit.
A callback success page means received, not durably saved.
Keep saved versus post-commit availability failure distinct so an already-saved credential is not falsely reported absent.
Logout is a locked local delete with revision advance, including absence; it does not revoke remotely or cancel issued snapshots.
Explain that env keys can resolve after deletion.

## Anthropic profile

Bind OAuth explicitly, never by token prefix.
Use bearer only, required CLI identity/header/system block and beta values in adapter-owned versioned profile data.
Keep Token Plan/API-key route behavior separate and compatible.
Use a supported SDK auth-token/client seam; inspect installed fantasy/native SDK before choosing it.
Do not add a dummy key or assume Vertex skip-auth disables ordinary environment auth.
Validate actual final origin/path/body/headers after SDK serialization and optional model headers.
Use request-local reversible tool-name codec for declarations, historical calls, additions, removals and named choice.
Historical names use historical tool snapshots; response names use active declarations and are canonical before first public toolcall_start.
Custom/MCP names remain unchanged; reject collisions before HTTP.
Preserve schema, argument values, results and stored transcript names.
If current StreamOptions has no forced named choice, add only the required typed optional field and update all adapter callers/tests; do not fake it in a helper-only test.

<!-- Updated: Review 2026-10-06 - real CLI test wiring, input bounds and shutdown. -->
## Accepted review contract

Keep `run` as the production wrapper and add one constructor-injected `runWithDependencies` boundary used by that wrapper and command tests.
It receives the same argv/stdin/stdout/stderr and executes the real auth parser, dispatch and ordinary app constructors.
Dependency values contain separate external auth and inference HTTP clients, clock/wait functions and owned cleanup; internal auth/store/agent/wire services remain real.
Production supplies normal clients and clocks; expose no endpoint, issuer, TLS or signature bypass flag or environment variable.
For offline checks, a controlled RoundTripper intercepts only the external HTTP boundary after logical origin/path/header validation.
Logical production URLs, callback URI, expected issuer/audience and signed-token verification remain unchanged.
Route auth, JWKS and discovery through the private auth client, and inference through a distinct client that can carry capture.
Use a compiled Go test subprocess helper that calls this same command boundary with the external clients and deterministic barriers injected.
Pass the user argv unchanged to the real parser; the helper is test composition, not an alternate auth route or a product command.
Keep ordinary production-binary help, signals and pipe checks, and verify that the production wrapper calls the same boundary.
Add a phase-3 test helper in `cmd/tui/auth_test.go` and reuse it for ChatGPT, xAI, restart and signal checks.
Install command-owned signal handling before the early auth dispatch; always stop it and close listeners/input on return.
The inference and auth-command paths both use the phase-2 bounded auth wait before returning a signal exit code.
Limit callback request-target/query input and private input lines to 16 KiB each before URL or code parsing.
Use standard HTTP header limits and bounded readers; do not add a parser framework or make the limit a new public setting.
Reject oversize input with sanitized diagnostics, no token exchange and no credential replacement.
An oversized callback must not consume the pending attempt; a later valid callback can still complete it.
A manual reader may retry only after safe bounded line cleanup; otherwise end that input path and preserve the credential without an unbounded drain.
Test oversize loopback requests and stdin separately, including a subsequent valid callback and cancellation during bounded input.
For nonempty named tool choice, each adapter must encode it correctly or reject it before HTTP; silent ignoring is forbidden.
Messages maps the selected name with the same request codec; Responses and Completions preserve canonical names when supported, otherwise return a classified unsupported option.
Faux must copy/record the option without sharing mutable state or exposing auth secrets.
Use the complete contract inventory for every body builder, option copy and test constructor affected by the new fields.

## Runtime Flow

Actor: user running auth login/logout and then `ask -p` or JSON mode.
Entry: cmd/tui run → auth dispatch before prompt/capture; subsequent run → newHeadlessAgent → app-composed agent.
Path: shared auth.Login → native strategy/input/callback → validated revision commit → compiled readiness publication → next prompt → resolver → Messages profile → real HTTP → canonical event.
Prepared state: isolated home, one old key/OAuth record, conflicting env key and external auth/inference httptest servers.
Use a real loopback HTTP callback and real stdin manual interaction separately.
Observe one provider credential, chosen subscription profile at HTTP, tool event, terminal result and local logout.
The first authorized live Anthropic prompt is an early checkpoint; it does not complete H7a.

## Related Code Files

| Action | File | Rough size | Test impact |
| --- | --- | --- | --- |
| New | `cmd/tui/auth.go`, `auth_test.go` | Large. | Built command routing, private input, signals and next prompt. |
| Existing, modify | `cmd/tui/headless.go`, `headless_faux.go`, `args.go`, `args_test.go`, `headless_test.go`, `headless_cassette_test.go` | Medium. | Shared constructor, help, provider/method selection. |
| New | `internal/auth/login.go`, `anthropic.go`, `anthropic_test.go`, `callback.go`, `callback_test.go` | Large. | Common lifecycle and separate native interactions. |
| New | `internal/auth/pkce.go`, `pkce_test.go`, `http.go`, `http_test.go` | Small. | Secure randomness, bounded response/no redirects. |
| Existing, modify | `internal/auth/service.go`, `internal/app/module_auth.go` | Small. | Register key and Anthropic methods, inject interaction. |
| Existing, modify | `internal/providers/api.go`, `catalog.go`, `catalog_test.go`, `keys.go` | Small. | Compiled Anthropic records and env/method data. |
| Existing, modify | `internal/providers/anthropic/provider.go`, `document.go` | Large. | Typed material and final Messages profile. |
| New | `internal/providers/anthropic/profile.go`, `profile_test.go`, `tool_names.go`, `tool_names_test.go` | Medium. | Identity, wire validation and reversible codec. |
| Existing, modify | `internal/providers/anthropic/provider_test.go`, `tool_changes_test.go` | Medium. | Protected key behavior and historical names. |
| Existing, modify | `internal/providers/types.go`, `cmd/tui/capture.go`, `internal/providers/cassette/cassette.go`, `cassette_test.go` | Small. | Named-choice contract if missing; strict auth transport/capture isolation. |
| New | `cmd/tui/auth_live_test.go` | Small. | Explicit opt-in live acceptance, sanitized evidence. |

## Protected functions and callers

run in headless.go is called by main.go and command tests; newHeadlessAgent is called by run and the headless test/cassette/record/fault/live suites.
openProvider lives in headless_faux.go and is called by newHeadlessAgent.
registerOpenAIWires must move/reuse shared construction without duplicating a second registry path.
`headless_test.go:TestPrint` currently expects `--provider openai` rejection; replace that assertion when the compiled target becomes supported rather than retain a stale rejection test.
The scout lists production callers; the contract review lists the ten test callers of newHeadlessAgent and the complete constructor inventory.
Preserve extra-tools injection used by headless integration tests.
Anthropic adapter.Stream/produce, prepareModel, buildDocument and TransformMessages are the existing wire path.
CurrentTools/ToolChanges own snapshot semantics; do not replace them with provider-specific transcript state.
startCapture and cassette recording must never receive the token client or callback traffic.

## Test scenario matrix

| Priority | Scenario | Required observation |
| --- | --- | --- |
| Critical | Key→OAuth→key commands and failed login/save. | Exactly one record; prior generation before commit retained. |
| Critical | Browser and copy-code, each then prompt. | Exact redirect/exchange; proper profile and canonical tool event. |
| Critical | Wrong state/full URL, missing token, corrupt expiry/scopes. | No activation; valid pending attempt can continue where safe. |
| Critical | Login versus logout/replacement, including absent start. | Revision conflict; no resurrection. |
| Critical | Bearer/key/env conflict and final model/header override. | Bound auth only; no secret to wrong origin. |
| Critical | Names: declaration/history/add/remove/forced choice/reverse/collision. | Each field independently asserted; no canonical mutation. |
| High | Fixed port occupied; spare callback connection; repeat login. | Explicit error or cleanup; no leaked listener. |
| High | Cancellation/deadline and committed-publication failure. | Signal exit or saved-state diagnosis, no false completion. |
| High | Capture enabled, synthetic secret canaries. | No OAuth HTTP/codes/tokens in cassette or JSON output. |
| High | Omitted method with interactive choice versus noninteractive omission. | Explicit selected method succeeds; unavailable choice fails before any exchange, save or prompt. |
| Medium | Remote copy-code/help/unsupported method. | Clear supported instructions and no accidental prompt. |

## Tests Before

1. Write built-command failing tests for key save, both Anthropic interactions, logout and next prompt.
2. Use real callback listener and private stdin rather than directly invoking the strategy as E2E proof.
3. Protect Token Plan Messages, historical tool changes and headless output/signal/capture suites.
4. Add actual outgoing request probes for every profile field and codec path, including forced choice.

## Refactor

1. Add common command parsing/dispatch/private interaction and shared app construction.
2. Add common login publication/local logout/key-save operations on phase 1 revision contracts.
3. Implement native Anthropic strategy, cancellable callback/input lifecycle and bounded token exchange.
4. Register compiled Anthropic model/method/profile data and integrate typed adapter material.
5. Build the codec once per request and reverse names before Fold/publication; preserve canonical copies.
6. Map incoming `fantasy.StreamPart.ToolCallName` through an adapter-local stream map before fantasykit.Fold calls Assembler.ToolStart; its existing Tool callback rewrites IDs only.
7. Add final transport validation and auth-client/cassette separation without a general body rewrite transport.

## Tests After

1. Pass command-to-next-prompt for API key, browser and copy-code separately.
2. Repeat replacement/race/rotation/provenance cases through the real command subprocess composition with real owners.
3. Scan synthetic outputs/artifacts for secrets and verify all listener/goroutine cleanup.
4. Run authorized live Anthropic login, tool prompt and local logout after offline gates; record no secrets.

## Regression gate

Run `go test ./cmd/tui ./internal/auth ./internal/app ./internal/providers/anthropic ./internal/providers/cassette`.
Then run affected agent/provider tests, race checks, build and required lint/import gates.
Live acceptance must be opt-in and requires the user's authorized account; its absence is not a mocked pass.

## Success Criteria

- [x] Common commands support key save, both native Anthropic modes and local logout.
- [x] First subscription prompt and tool result work through the shared resolver without API-key fallback.
- [x] Profile fields, name round trips, callback cleanup and capture isolation pass.

## Risk Assessment and rollback

If native client/profile is rejected in live use, stop that method and review provider acceptance instead of inventing identity or redirect.
If SDK bearer auth cannot avoid ambient-key behavior, adapt its supported native boundary in the wire owner and prove the final HTTP request.
Disable the Anthropic method registration first for rollback; preserve key callers and credential records.
