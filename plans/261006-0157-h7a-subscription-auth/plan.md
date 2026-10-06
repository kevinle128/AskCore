---
title: "h7a-subscription-auth"
description: "Native subscription auth, secure persistence and request profiles with six TDD phases."
status: completed
priority: P1
effort: ""
tags: []
created: 2026-10-06
---

# H7a: Subscription auth

## Overview

Deliver native headless login, local logout, refresh and subscription inference for Anthropic, ChatGPT and xAI.
Preserve API-key callers, canonical history and tools, and one saved credential/account per provider.
This document was created as a deep TDD plan.
At creation time, no implementation, executed test, login, or paid request was claimed.
The execution record below supplies current implementation and acceptance evidence.
The first usable Anthropic prompt is a checkpoint in phase 3; all three live routes are required for completion.

## Current execution record

H7a is complete.
The current CLI-derived status is 6/6 complete phases and 22/22 checked phase criteria, or 100%.
All six phases passed their criteria and were checked through `ak plan check`.
The final exact command signal-during-local-commit actor passed all five cases twice with race instrumentation.
The [implementation progress report](../reports/pm-261006-0823-h7a-progress.md) maps all 22 criteria.
The [controller implementation report](../reports/cook-261006-0849-h7a-implementation.md) explains the delivered architecture and maintenance limits.
The full `--auto --advisor --tdd` scope remains active; no `--yagni` scope cut was requested.

All three ordinary production live login, tool prompt, and local logout routes passed.
The [sanitized live acceptance](../reports/tester-261006-0835-h7a-live-acceptance.md) records Anthropic `claude-sonnet-4-6`, explicitly selected ChatGPT `gpt-5.6-sol`, and xAI `grok-4.7`.
Each login returned exit code 0, saved generation 1 with expiry, and had no pending fence.
Each prompt executed one canonical `echo` result with `isError=false`, two assistant messages ending with `toolUse` and `stop`, usage, and one settlement.
Each local logout returned exit code 0 and removed its provider record.
All three isolated provider records were absent after the final logout.
The existing GPT-5.5 default was preserved; Sol is a static catalog addition selected explicitly, not an automatic substitute.

The controller reports passing full tests, vet, compilation, lint with zero issues, required race checks, and final changed app/provider and signed ChatGPT command races.
The latest full test run reported CLI success in 32.725 seconds.
The earlier concurrent test/live callback collision was followed by a clean full rerun.
The [final review](../reports/reviewer-261006-0843-h7a-final-review.md) scores the result at 9.5/10 and reports no confirmed open critical code defect.
Native readiness now verifies prior model and thinking preservation.
The [final actor proof](../reports/tester-261006-0835-h7a-final-proof.md) records passing real replacement-write-failure, SIGKILL, and lost-response restarts with zero extra exchanges.
Faux named-choice script, record, and value-copy assertions passed their targeted race check in 1.561 seconds.

The [exact filesystem actor](../reports/tester-261006-0855-h7a-fsync-actor.md) now supplies the literal command barrier inside the real local replacement-file sync.
The corrected five-case matrix passed once with race instrumentation in 19.50 seconds.
It covers all three ordinary signals and a second signal, durable replacement bytes observed by another process before exit, budget expiry, and actual SIGKILL with unchanged-fence restarts and no grant reuse.
The observer reads the owned backing directory; all product operations still use the real FUSE mount.
The earlier repeated run encountered disk exhaustion and is not a passing count-two result.
After host space recovery and removal of the damaged owned cache, a cold rebuild passed all five cases twice in 38.186 seconds with exit code 0.
No race or cleanup diagnostic was reported.
The reviewer closed the exact actor gate, and the final tagged lint run passed with zero issues.
The controller removed the owned test VM and its cache, confirmed no owned process remains, and preserved the original Podman default connection and user Docker processes.
The [test setup](../../internal/testsupport/README.md#filesystem-fault-tests) links the test-only fixture and Linux runner; no production I/O hook or auth bypass was added.
The [implementation report](../reports/cook-261006-0849-h7a-implementation.md#maintainability-and-scale-limits) records single-host storage, provider-source adaptation, and one-account/local-logout limits.
The owning parent H7a phase records this completion; the whole parent roadmap and other phases were not marked complete.

## Authority and scope

- [Roadmap](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md) and [H7a requirements](../260930-2254-pi-feature-inventory-go-roadmap/phase-h7a-subscription-auth.md) own scope.
- [Provider design](../261005-2139-provider-auth-design/plan.md) owns accepted product choices.
- [Architecture](../../docs/ask-architecture-reference.md) owns package/import rules.
- [Architecture evidence](../reports/xia-261006-0143-h7a-subscription-auth-architecture.md) supplies concurrency, identity and wire gaps.
- [Pi runtime proof](../reports/researcher-261006-0157-h7a-pi-runtime-proof.md) supplies actual triggers and pinned source behavior.
- [Local runtime scout](../reports/researcher-261006-0157-h7a-local-runtime-scout.md) supplies callers and baseline test inventory.

Hold the full H7a scope.
Reuse Stream, Assembler, NormalizeRequest, CurrentTools, TransformMessages, registry dispatch, API-key hooks and adapter boundaries.
The shared store/lifecycle and three protocol strategies need separate owners because file transactions, auth protocols and wire conversion have different contracts.
Do not add saved method collections, account pickers, remote revocation, retained client registrations after logout, SQL storage or token transfer.
Full settings layering, remote catalogs, custom provider configuration, leader/TUI/gateway login and selection controls remain with their roadmap owners.
Verified ChatGPT identity and returning issued-client reuse while a record exists remain required.
The user selected Pi's one-record and local-only logout behavior after the documentation conflict was presented.
Do not claim full OpenAI account/session-guidance compliance from that product choice.

## Phases

The table is creation-time metadata.
Use the current CLI-derived execution record above for progress.
The installed CLI changes phase checkboxes but does not rewrite this table or phase status frontmatter.

| # | Phase | Status |
| --- | --- | --- |
| 1 | [Secure credential store and contracts](./phase-01-start.md) | Pending |
| 2 | [Auth resolution and request binding](./phase-02-auth-resolution-and-request-binding.md) | Pending |
| 3 | [Anthropic and headless login](./phase-03-anthropic-and-headless-login.md) | Pending |
| 4 | [ChatGPT identity and Responses](./phase-04-chatgpt-identity-and-responses.md) | Pending |
| 5 | [xAI device authorization](./phase-05-xai-device-authorization.md) | Pending |
| 6 | [Cumulative acceptance and documentation](./phase-06-cumulative-acceptance-and-documentation.md) | Pending |

## Execution dependency map

| Phase | Deliverable | Inputs | Relative change size |
| --- | --- | --- | --- |
| [1](./phase-01-start.md) | Secure store and contracts. | H2/H3 interfaces. | Large, isolated new storage behavior. |
| [2](./phase-02-auth-resolution-and-request-binding.md) | Resolution, binding and shared readiness. | Phase 1. | Large, shared contract changes. |
| [3](./phase-03-anthropic-and-headless-login.md) | Common commands, key save and first Anthropic prompt. | Phases 1–2 and H3 Messages. | Largest, first full actor path. |
| [4](./phase-04-chatgpt-identity-and-responses.md) | Verified ChatGPT sign-in, discovery and Responses profile. | Phase 3; H4 Responses for wire tests only. | Large, identity and profile. |
| [5](./phase-05-xai-device-authorization.md) | xAI device flow and Responses profile. | Phase 3; phase 4 shared Responses validation; H4 wire gate. | Medium, strategy and data. |
| [6](./phase-06-cumulative-acceptance-and-documentation.md) | Cumulative live acceptance and owning docs. | Phases 1–5. | Medium, connected acceptance. |

H4 is a partial dependency, not a whole-plan blocker.
Verify its actual working-tree Responses adapter and fork features before phases 4–5 wire acceptance.
Store, resolver, commands and Anthropic delivery can proceed without full H4, H5, H6 or H7 completion.
H7 must reuse the store/resolver, H9 consumes error classes, and H13/T2/H17 consume shared operations later.
The controller owns cross-plan metadata; the table lists the six CLI-created phase files.

## Runtime contract

Settings uses stdlib-only file I/O and platform locks; it imports no other internal package.
Auth owns login/refresh/discovery and can import settings and provider core types; it imports no agent, wire adapter, config, gateway or transport handler.
Providers receive immutable resolved auth and import neither auth nor settings.
App injects a composed StreamFn and one readiness function into each runtime.
Resolve after PrepareRequest and the legacy per-request key hook, once for each model request, including the next tool-loop call.
The agent has no vendor switch or credential-file access.
A caller with competing nonempty typed/legacy overrides is rejected.
A supported nonempty key override wins, a saved tagged credential wins over env, and only absence permits ambient key resolution.
A saved OAuth failure never changes the billing route.

A resolved snapshot binds provider, method, profile, account/generation, source, endpoint and access material.
It carries no refresh token, ID token, callback code or store handle, and is never persisted in history/events.
Use one real composed StreamFn around registry dispatch.
Early resolution errors return NewStream with Assembler.Fail and the selected model seed; they do not escape as a second loop error.
There is one settled Result and at most one terminal stream event, with the existing full-buffer cancellation exception.
The Agent remains the sole owner of public MessageStart/MessageEnd and run settlement.
Retain the legacy hook-error path so the Agent wrapper emits the failure sequence once.

Use one stable sidecar OS lock, fresh read/merge and a durable store-wide revision even when a provider is absent.
Write a same-directory 0600 temporary file, sync and close it, rename it, then sync its 0700 owner directory.
Corrupt or unsafe files fail closed.
Pre-rename failure preserves old bytes; post-rename failure is indeterminate and never reported as success.
Browser interaction is outside the lock; its final commit compares the captured revision.
Refresh holds the bounded lock across recheck, exchange, validation and commit.
After validated token rotation, finish the bounded local commit without caller cancellation, then report cancellation.
Network rotation and disk commit cannot be atomic; uncertain rotation needs safe recovery or explicit reauthentication.
Before a rotating exchange, durably fence the current provider generation with a pending attempt under the same lock.
An unresolved fence survives restart and forbids automatic grant reuse or key fallback; only a validated durable replacement or explicit login/logout clears it.
Register auth work before the exchange; signal shutdown waits for its remaining bounded exchange/commit budget instead of only the two-second inference grace.
A second ordinary signal cannot shorten a validated commit, and forced death remains protected by the persisted fence.
Use a 15-second refresh exchange maximum and a 5-second commit budget; blocked OS sync is an explicit uncertainty limit.
The CLI wrapper and injected command test composition use the same real parsing/dispatch/app path.
External auth/inference clients stay separate; no production endpoint, TLS, issuer or signature bypass is introduced.
Callback and private input have a 16 KiB limit before parsing; each wire encodes or explicitly rejects nonempty named choice.

## Command surface and source adaptation

Pi production login/logout are interactive /login and /logout routes.
Pi's separate ai development login CLI bypasses production persistence and is not the Ask reference.
The accepted headless adaptation is `ask auth login --provider <id> --method <id>` and `ask auth logout --provider <id>`.
API-key save selects `--method api-key` and reads private stdin.
Anthropic OAuth selects `--method anthropic-oauth --interaction browser` or `--method anthropic-oauth --interaction copy-code`; OpenAI selects `--method openai-chatgpt` and xAI selects `--method xai-oauth`.
Omitted method asks the user to choose a supported method; noninteractive omission returns a clear error.
API-key save requires explicit `--method api-key`.
For a saved ChatGPT record, ordinary login reuses its issued client/account.
`ask auth login --provider openai --method openai-chatgpt --new-account` explicitly starts dynamic registration for replacement and preserves the old record until verified durable commit.
Pi headless auth only checks/prints credentials; this native headless login surface is an Ask addition.
The final explicit-method selection above supersedes the earlier scout recommendation to default to OAuth; it follows the actual Pi production selector.
The owner is cmd/tui/run dispatch before parseArgs, readPrompts and startCapture, backed by shared app constructors.
Input for keys and manual callbacks uses stdin/private interaction, never command arguments.
Each operation accepts explicit provider/method selection; Anthropic browser and copy-code are independently selected modes.
Login runs on the inference host and its ASK_HOME.
Document copy-code, ChatGPT loopback forwarding and xAI device use without an externally bound callback listener.

## Success criteria

These overview checkboxes retain the creation-time contract.
The CLI-owned phase checkboxes and current execution record above supply completion status.

- [ ] All three command-to-next-prompt paths work with real internal services and authorized live accounts.
- [ ] API-key and OAuth replacement retain exactly one provider record; failed validation or pre-commit save retains the prior generation.
- [ ] Refresh, revision conflicts, endpoint/header isolation, verified identity and request-local profiles pass their connected checks.
- [ ] Auth failures settle once without billed fallback or replay after public output.
- [ ] Focused tests, race checks, package tests, build and required lint/import gates pass.
- [ ] Owning docs state supported command, host, logout, callback and storage behavior without secrets.

## Validation Log

### Runtime Flow Proof Matrix

PASSED means the design path and same-boundary planned test are complete.
It does not mean a test was run or a provider accepted a request.
All new segments are explicit phase tasks.
The command surface is recorded above from the controller's Pi-trigger decision.
For offline command rows, use the compiled test subprocess composition around the same real runWithDependencies parsing/dispatch boundary with an isolated owner-only home.
Keep app/auth/settings/agent/adapter services real and intercept only external HTTP/clock boundaries.
The production run wrapper supplies normal dependencies to the same boundary; ordinary binary help/signal/pipe checks and authorized live runs verify that wrapper separately.
For request rows, invoke Agent.Prompt or Agent.SetModel with the app-composed services; add the CLI-to-next-prompt case in phase 3 and reuse it in phases 4–6.
For store-only library rows, invoke the planned exported settings credential transaction boundary; phases 2–3 supply its real consumer proof.
All tests mock external provider HTTP/JWKS/discovery only; filesystem, locks, auth services, runner, agent and wire conversion remain real.
Use deterministic barriers for concurrency and crash/fault cases.

| Feature | Actor | Runtime trigger | Entry point | Internal path | Observable result | End-to-end test | External mocks | Prepared data | Status |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Command dispatch and help | User | ask auth login --provider <id> / ask auth logout --provider <id> | cmd/tui run → planned auth dispatch | dispatch → app auth constructor | No prompt, capture, DB or gateway; clear help/errors | CLI auth routing | Provider HTTP | Extra words; capture enabled; unsupported method | PASSED |
| Interactive method selection | User | ask auth login --provider <id> without method, with interactive input | Real command parser/private interaction | supported method list → explicit choice → shared auth.Login → native strategy | Chosen method only; no accidental prompt or billed fallback | CLI method selector then native login/prompt | External provider HTTP | Provider with API-key and OAuth methods; selected OAuth input | PASSED |
| Noninteractive missing method | User | Same auth command without method and without interactive selection | Real command parser/dispatch | selection requirement → sanitized command error before auth exchange | Clear error; no record change, capture or model request | CLI noninteractive omission | External HTTP counter | Existing saved credential; no selection available | PASSED |
| API-key login/save | User | ask auth login --provider <id> --method api-key | cmd/tui planned auth handler | app → auth.Login → strategy → revision commit → next ask -p | One saved tagged record; chosen subscription/key prompt succeeds | CLI API key then prompt | Provider HTTP | Existing OAuth record; new key on private stdin | PASSED |
| Anthropic browser login | User | ask auth login --provider anthropic --method anthropic-oauth --interaction browser | cmd/tui planned auth handler | app → auth.Login → strategy → revision commit → next ask -p | One saved tagged record; chosen subscription/key prompt succeeds | CLI browser then prompt | Provider HTTP | Existing key; real loopback callback and token response | PASSED |
| Anthropic copy-code login | User | ask auth login --provider anthropic --method anthropic-oauth --interaction copy-code | cmd/tui planned auth handler | app → auth.Login → strategy → revision commit → next ask -p | One saved tagged record; chosen subscription/key prompt succeeds | CLI copy-code then prompt | Provider HTTP | Existing key; hosted redirect; manual code | PASSED |
| ChatGPT new sign-in | User | ask auth login --provider openai --method openai-chatgpt | cmd/tui planned auth handler | app → auth.Login → strategy → revision commit → next ask -p | One saved tagged record; chosen subscription/key prompt succeeds | CLI ChatGPT then prompt | Provider HTTP | No saved record; callback; signed test ID token | PASSED |
| Returning ChatGPT sign-in | User | ask auth login --provider openai --method openai-chatgpt | cmd/tui planned auth handler | app → auth.Login → strategy → revision commit → next ask -p | One saved tagged record; chosen subscription/key prompt succeeds | CLI ChatGPT then prompt | Provider HTTP | Saved issued client and verified subject | PASSED |
| ChatGPT explicit new-account replacement | User | ask auth login --provider openai --method openai-chatgpt --new-account | cmd/tui planned auth handler | shared login → dynamic registration → verified new identity → revision commit → next prompt | New account replaces one record only after success; failed attempt retains old account | CLI new-account replacement and failed attempt | Auth HTTP/JWKS/inference | Saved client/account A; authorized signed identity B; failure variant | PASSED |
| ChatGPT new registration after logout | User | ask auth logout --provider openai then ask auth login --provider openai --method openai-chatgpt | cmd/tui planned auth handler | local delete → shared login → new registration → verified commit | No retained client/account mapping; new saved record works | CLI logout then fresh registration | Auth HTTP/JWKS/inference | Saved ChatGPT record; no registration after logout | PASSED |
| xAI device login | User | ask auth login --provider xai --method xai-oauth | cmd/tui planned auth handler | app → auth.Login → strategy → revision commit → next ask -p | One saved tagged record; chosen subscription/key prompt succeeds | CLI xAI then prompt | Provider HTTP | Existing key; device response and timed polls | PASSED |
| Local-only logout | User | ask auth logout --provider <id> provider | cmd/tui planned auth handler | app → auth.Logout → locked revision delete → next prompt | Record removed; no revocation; env remains eligible and result explains it | CLI local logout | Provider HTTP | Saved OAuth plus env key; unrelated provider | PASSED |
| Logout method mismatch | User | ask auth logout --provider <id> with wrong method | cmd/tui planned auth handler | auth.Logout checks saved tag before delete | Current credential retained; clear error | CLI mismatched logout | Provider HTTP | Saved OAuth; selected key method | PASSED |
| Owner-only paths | Auth service | Credential transaction or login commit | planned settings public credential boundary | settings lock → fresh decode/revision → merge → atomic replace | 0700 directory; 0600 data/temp/lock metadata | Store subprocess Owner-only paths | None | New home | PASSED |
| Unsafe path rejection | Auth service | Credential transaction or login commit | planned settings public credential boundary | settings lock → fresh decode/revision → merge → atomic replace | No overwrite or secret send | Store subprocess Unsafe path rejection | None | Symlink or unexpected file type | PASSED |
| Corrupt-file rejection | Auth service | Credential transaction or login commit | planned settings public credential boundary | settings lock → fresh decode/revision → merge → atomic replace | Corrupt bytes retained; classified failure | Store subprocess Corrupt-file rejection | None | Malformed JSON | PASSED |
| Unknown-field merge | Auth service | Credential transaction or login commit | planned settings public credential boundary | settings lock → fresh decode/revision → merge → atomic replace | Unrelated/unknown values preserved | Store subprocess Unknown-field merge | None | Two provider records and unknown fields | PASSED |
| API-key legacy readability | Auth service | Credential transaction or login commit | planned settings public credential boundary | settings lock → fresh decode/revision → merge → atomic replace | Key request succeeds without method collection | Store subprocess API-key legacy readability | None | Pi-style key record | PASSED |
| Ambiguous OAuth import | Auth service | Credential transaction or login commit | planned settings public credential boundary | settings lock → fresh decode/revision → merge → atomic replace | Request fails before HTTP | Store subprocess Ambiguous OAuth import | None | OAuth record missing explicit method | PASSED |
| Atomic visibility | Auth service | Credential transaction or login commit | planned settings public credential boundary | settings lock → fresh decode/revision → merge → atomic replace | Every read sees complete old or new JSON | Store subprocess Atomic visibility | None | Concurrent reader during replacement | PASSED |
| Failure before rename | Auth service | Credential transaction or login commit | planned settings public credential boundary | settings lock → fresh decode/revision → merge → atomic replace | Prior bytes remain; no success | Store subprocess Failure before rename | None | Write/sync/close/rename fault | PASSED |
| Failure after rename | Auth service | Credential transaction or login commit | planned settings public credential boundary | settings lock → fresh decode/revision → merge → atomic replace | Indeterminate result; no success | Store subprocess Failure after rename | None | Directory sync fault | PASSED |
| Absent-record ABA | Auth service | Credential transaction or login commit | planned settings public credential boundary | settings lock → fresh decode/revision → merge → atomic replace | Late login conflicts instead of recreating credential | Store subprocess Absent-record ABA | None | Login starts absent; logout while waiting | PASSED |
| Concurrent replacement | Auth service | Credential transaction or login commit | planned settings public credential boundary | settings lock → fresh decode/revision → merge → atomic replace | Late commit retains newer record | Store subprocess Concurrent replacement | None | Waiting login; newer account/key login | PASSED |
| Explicit API-key override | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | Explicit key route/profile used | Connected runner Explicit API-key override | Provider HTTP | Saved OAuth and env; supported provider-bound key | PASSED |
| Stored OAuth precedence | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | OAuth request, no env key | Connected runner Stored OAuth precedence | Provider HTTP | Valid saved OAuth plus conflicting env | PASSED |
| Stored API-key precedence | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | Saved key used | Connected runner Stored API-key precedence | Provider HTTP | Saved key plus conflicting env | PASSED |
| Absent-record ambient key | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | Ambient key used | Connected runner Absent-record ambient key | Provider HTTP | No saved record; supported env | PASSED |
| OAuth failure forbids fallback | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | No inference on any API-key route | Connected runner OAuth failure forbids fallback | Provider HTTP | Invalid refresh and valid env key | PASSED |
| Empty key hook fallback | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | Stored method resolves normally | Connected runner Empty key hook fallback | Provider HTTP | Hook returns empty; stored OAuth | PASSED |
| Nonempty legacy hook | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | Key method only for final provider | Connected runner Nonempty legacy hook | Provider HTTP | Hook returns supported key; stored OAuth | PASSED |
| Conflicting typed/legacy overrides | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | Error before HTTP | Connected runner Conflicting typed/legacy overrides | Provider HTTP | Two nonempty overrides | PASSED |
| PrepareRequest final model | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | Only final provider credential sent | Connected runner PrepareRequest final model | Provider HTTP | Hook changes provider after startup | PASSED |
| Next tool-loop resolution | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | Next call resolves current generation | Connected runner Next tool-loop resolution | Provider HTTP | Tool response; credential generation changes | PASSED |
| Bound CLI key isolation | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | A key never reaches B | Connected runner Bound CLI key isolation | Provider HTTP | CLI key for A; PrepareRequest selects B | PASSED |
| Endpoint URL binding | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | Rejected before credential send | Connected runner Endpoint URL binding | Provider HTTP | Wrong HTTPS origin/path, userinfo or redirect | PASSED |
| Protected final headers | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | Only selected credential and required identity headers | Connected runner Protected final headers | Provider HTTP | Model headers and SDK env conflict | PASSED |
| Concurrent request isolation | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | No credential, profile or decoder leak | Connected runner Concurrent request isolation | Provider HTTP | Two providers/accounts and shared transcript | PASSED |
| Readiness success | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | SetModel succeeds and prompt uses same rules | Connected runner Readiness success | Provider HTTP | Saved subscription; valid binding | PASSED |
| Readiness failure | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | SetModel retains prior model and reasoning | Connected runner Readiness failure | Provider HTTP | Method mismatch or known denial | PASSED |
| Early auth stream failure | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | One error Result and terminal event; one public sequence | Connected runner Early auth stream failure | Provider HTTP | Resolver returns error | PASSED |
| Canceled full stream | User | Prompt or idle model selection | Agent.Prompt / Agent.SetModel | PrepareRequest/key hook → app runner/readiness → auth.Resolve → registry → wire | Result settles; producer exits; no leak | Connected runner Canceled full stream | Provider HTTP | Canceled consumer with full buffer | PASSED |
| Valid token avoids auth HTTP | Request caller | Prompt or authenticated discovery | Agent.Prompt / planned auth discovery | auth.Resolve → lock/recheck → bounded strategy exchange → validated durable commit | Inference without token call | Refresh process Valid token avoids auth HTTP | Provider HTTP | Unexpired OAuth | PASSED |
| Concurrent refresh | Request caller | Prompt or authenticated discovery | Agent.Prompt / planned auth discovery | auth.Resolve → lock/recheck → bounded strategy exchange → validated durable commit | One rotation; both use committed generation | Refresh process Concurrent refresh | Provider HTTP | Two processes; same expired record | PASSED |
| Canceled lock wait | Request caller | Prompt or authenticated discovery | Agent.Prompt / planned auth discovery | auth.Resolve → lock/recheck → bounded strategy exchange → validated durable commit | Canceled caller makes no later exchange | Refresh process Canceled lock wait | Provider HTTP | Another process holds lock | PASSED |
| Fresh recheck after refresh | Request caller | Prompt or authenticated discovery | Agent.Prompt / planned auth discovery | auth.Resolve → lock/recheck → bounded strategy exchange → validated durable commit | Uses new record, no duplicate exchange | Refresh process Fresh recheck after refresh | Provider HTTP | Another writer rotated while waiting | PASSED |
| Refresh after logout | Request caller | Prompt or authenticated discovery | Agent.Prompt / planned auth discovery | auth.Resolve → lock/recheck → bounded strategy exchange → validated durable commit | No resurrection or stale refresh | Refresh process Refresh after logout | Provider HTTP | Record deleted before locked recheck | PASSED |
| Refresh after account replacement | Request caller | Prompt or authenticated discovery | Agent.Prompt / planned auth discovery | auth.Resolve → lock/recheck → bounded strategy exchange → validated durable commit | No stale account rotation | Refresh process Refresh after account replacement | Provider HTTP | Current account differs from first read | PASSED |
| Actual expiry lead times | Request caller | Prompt or authenticated discovery | Agent.Prompt / planned auth discovery | auth.Resolve → lock/recheck → bounded strategy exchange → validated durable commit | One shared refresh threshold | Refresh process Actual expiry lead times | Provider HTTP | Anthropic/xAI 10m; ChatGPT 8m | PASSED |
| Server earliest refresh | Request caller | Prompt or authenticated discovery | Agent.Prompt / planned auth discovery | auth.Resolve → lock/recheck → bounded strategy exchange → validated durable commit | No early exchange; use valid token | Refresh process Server earliest refresh | Provider HTTP | Valid token; future earliest_refresh_at | PASSED |
| Validity before permitted refresh | Request caller | Prompt or authenticated discovery | Agent.Prompt / planned auth discovery | auth.Resolve → lock/recheck → bounded strategy exchange → validated durable commit | Bounded retry-after/auth result, no loop | Refresh process Validity before permitted refresh | Provider HTTP | Insufficient request lifetime; future refresh gate | PASSED |
| Cancel after valid rotation | Request caller | Prompt or authenticated discovery | Agent.Prompt / planned auth discovery | auth.Resolve → lock/recheck → bounded strategy exchange → validated durable commit | Replacement durably saved then cancellation returned | Refresh process Cancel after valid rotation | Provider HTTP | Cancel after token validation; before save | PASSED |
| Rotation save failure | Request caller | Prompt or authenticated discovery | Agent.Prompt / planned auth discovery | auth.Resolve → lock/recheck → bounded strategy exchange → validated durable commit | No inference, success or old-token fallback | Refresh process Rotation save failure | Provider HTTP | Validated rotation; injected disk fault | PASSED |
| Ambiguous rotation response | Request caller | Prompt or authenticated discovery | Agent.Prompt / planned auth discovery | auth.Resolve → lock/recheck → bounded strategy exchange → validated durable commit | No blind refresh retry; recovery error | Refresh process Ambiguous rotation response | Provider HTTP | Server may rotate; response lost | PASSED |
| Callback state and URI | User | ask auth login --provider <id> interaction | cmd/tui planned auth handler | auth attempt → external response/input → validation → commit/publication | Wrong callback cannot complete attempt | CLI login fault Callback state and URI | Provider HTTP | Wrong-state/error callback then correct callback | PASSED |
| Attempt state/PKCE isolation | User | ask auth login --provider <id> interaction | cmd/tui planned auth handler | auth attempt → external response/input → validation → commit/publication | Only matching single-use callback exchanges | CLI login fault Attempt state/PKCE isolation | Provider HTTP | Concurrent login attempts | PASSED |
| Manual input validation | User | ask auth login --provider <id> interaction | cmd/tui planned auth handler | auth attempt → external response/input → validation → commit/publication | No commit; supported copy-code bare code only | CLI login fault Manual input validation | Provider HTTP | Wrong full redirect or browser-mode bare code | PASSED |
| Listener/input cleanup | User | ask auth login --provider <id> interaction | cmd/tui planned auth handler | auth attempt → external response/input → validation → commit/publication | No listener/waiter remains; repeat attempt works | CLI login fault Listener/input cleanup | Provider HTTP | Success, failure, cancellation; spare connection | PASSED |
| Login deadlines | User | ask auth login --provider <id> interaction | cmd/tui planned auth handler | auth attempt → external response/input → validation → commit/publication | Bounded exit with signal convention | CLI login fault Login deadlines | Provider HTTP | Hanging callback/exchange; signal | PASSED |
| Token response validation | User | ask auth login --provider <id> interaction | cmd/tui planned auth handler | auth attempt → external response/input → validation → commit/publication | No credential activation | CLI login fault Token response validation | Provider HTTP | Empty/wrong tokens, invalid expiry/scopes, oversize body | PASSED |
| Committed publication failure | User | ask auth login --provider <id> interaction | cmd/tui planned auth handler | auth attempt → external response/input → validation → commit/publication | Error states credential saved; next process can use it | CLI login fault Committed publication failure | Provider HTTP | Commit succeeds; readiness refresh fails | PASSED |
| Anthropic bearer header | User | Subscription prompt/tool turn | Agent.Prompt via app-composed Messages | runner → resolved profile → document/codec → actual HTTP → decoder → Assembler | Bearer only; no x-api-key | Messages connected Anthropic bearer header | Provider HTTP | Saved OAuth; env key conflict | PASSED |
| Anthropic CLI identity header | User | Subscription prompt/tool turn | Agent.Prompt via app-composed Messages | runner → resolved profile → document/codec → actual HTTP → decoder → Assembler | Pinned supported identity header on actual HTTP | Messages connected Anthropic CLI identity header | Provider HTTP | OAuth request | PASSED |
| Anthropic beta headers | User | Subscription prompt/tool turn | Agent.Prompt via app-composed Messages | runner → resolved profile → document/codec → actual HTTP → decoder → Assembler | Required beta values survive final isolation | Messages connected Anthropic beta headers | Provider HTTP | OAuth request and tool changes | PASSED |
| Anthropic identity system block | User | Subscription prompt/tool turn | Agent.Prompt via app-composed Messages | runner → resolved profile → document/codec → actual HTTP → decoder → Assembler | Identity added to wire copy; canonical prompt retained | Messages connected Anthropic identity system block | Provider HTTP | Canonical system prompt | PASSED |
| Anthropic declaration names | User | Subscription prompt/tool turn | Agent.Prompt via app-composed Messages | runner → resolved profile → document/codec → actual HTTP → decoder → Assembler | Known names mapped; unknown names unchanged | Messages connected Anthropic declaration names | Provider HTTP | Builtin and custom/MCP declarations | PASSED |
| Anthropic historical calls | User | Subscription prompt/tool turn | Agent.Prompt via app-composed Messages | runner → resolved profile → document/codec → actual HTTP → decoder → Assembler | Names use matching historical declarations | Messages connected Anthropic historical calls | Provider HTTP | Removed/readded tool snapshots | PASSED |
| Anthropic tool additions | User | Subscription prompt/tool turn | Agent.Prompt via app-composed Messages | runner → resolved profile → document/codec → actual HTTP → decoder → Assembler | New wire name maps; original snapshot unchanged | Messages connected Anthropic tool additions | Provider HTTP | Delta adds tool | PASSED |
| Anthropic tool removals | User | Subscription prompt/tool turn | Agent.Prompt via app-composed Messages | runner → resolved profile → document/codec → actual HTTP → decoder → Assembler | Removal has matching wire name | Messages connected Anthropic tool removals | Provider HTTP | Delta removes tool | PASSED |
| Anthropic forced tool choice | User | Subscription prompt/tool turn | Agent.Prompt via app-composed Messages | runner → resolved profile → document/codec → actual HTTP → decoder → Assembler | Choice matches encoded declaration | Messages connected Anthropic forced tool choice | Provider HTTP | Named tool selection | PASSED |
| Anthropic response names | User | Subscription prompt/tool turn | Agent.Prompt via app-composed Messages | runner → resolved profile → document/codec → actual HTTP → decoder → Assembler | Canonical name before first public tool event | Messages connected Anthropic response names | Provider HTTP | Streamed tool call | PASSED |
| Anthropic codec collision | User | Subscription prompt/tool turn | Agent.Prompt via app-composed Messages | runner → resolved profile → document/codec → actual HTTP → decoder → Assembler | Failure before HTTP | Messages connected Anthropic codec collision | Provider HTTP | Two names map to one wire name | PASSED |
| Canonical tool values/results | User | Subscription prompt/tool turn | Agent.Prompt via app-composed Messages | runner → resolved profile → document/codec → actual HTTP → decoder → Assembler | Schema, argument values, result content and stored names unchanged | Messages connected Canonical tool values/results | Provider HTTP | Tool call/result and parallel profile request | PASSED |
| ChatGPT signature | User | ask auth login --provider <id> ChatGPT or logout | cmd/tui planned auth handler | auth ChatGPT strategy → maintained OIDC verifier → bound commit | Rejected before save | CLI ChatGPT identity ChatGPT signature | Auth HTTP/JWKS | Forged ID token | PASSED |
| ChatGPT issuer | User | ask auth login --provider <id> ChatGPT or logout | cmd/tui planned auth handler | auth ChatGPT strategy → maintained OIDC verifier → bound commit | Rejected before save | CLI ChatGPT identity ChatGPT issuer | Auth HTTP/JWKS | Valid signature; wrong issuer | PASSED |
| ChatGPT audience/client | User | ask auth login --provider <id> ChatGPT or logout | cmd/tui planned auth handler | auth ChatGPT strategy → maintained OIDC verifier → bound commit | Rejected before save | CLI ChatGPT identity ChatGPT audience/client | Auth HTTP/JWKS | Wrong audience or issued client | PASSED |
| ChatGPT token expiry | User | ask auth login --provider <id> ChatGPT or logout | cmd/tui planned auth handler | auth ChatGPT strategy → maintained OIDC verifier → bound commit | Rejected before save | CLI ChatGPT identity ChatGPT token expiry | Auth HTTP/JWKS | Expired verified token | PASSED |
| ChatGPT nonce | User | ask auth login --provider <id> ChatGPT or logout | cmd/tui planned auth handler | auth ChatGPT strategy → maintained OIDC verifier → bound commit | Rejected before save | CLI ChatGPT identity ChatGPT nonce | Auth HTTP/JWKS | Wrong attempt nonce | PASSED |
| ChatGPT verified subject | User | ask auth login --provider <id> ChatGPT or logout | cmd/tui planned auth handler | auth ChatGPT strategy → maintained OIDC verifier → bound commit | Prior account retained | CLI ChatGPT identity ChatGPT verified subject | Auth HTTP/JWKS | Returning sign-in different subject | PASSED |
| ChatGPT granted scopes | User | ask auth login --provider <id> ChatGPT or logout | cmd/tui planned auth handler | auth ChatGPT strategy → maintained OIDC verifier → bound commit | Rejected before save | CLI ChatGPT identity ChatGPT granted scopes | Auth HTTP/JWKS | Missing inference scope | PASSED |
| Stable host identifier | User | ask auth login --provider <id> ChatGPT or logout | cmd/tui planned auth handler | auth ChatGPT strategy → maintained OIDC verifier → bound commit | Same owner-scoped host ID | CLI ChatGPT identity Stable host identifier | Auth HTTP/JWKS | Two logins in same home | PASSED |
| Issued-client reuse | User | ask auth login --provider <id> ChatGPT or logout | cmd/tui planned auth handler | auth ChatGPT strategy → maintained OIDC verifier → bound commit | Authorization/refresh use saved issued client | CLI ChatGPT identity Issued-client reuse | Auth HTTP/JWKS | Existing saved client; returning login | PASSED |
| Logout removes registration | User | ask auth login --provider <id> ChatGPT or logout | cmd/tui planned auth handler | auth ChatGPT strategy → maintained OIDC verifier → bound commit | New registration; no retained account/client mapping | CLI ChatGPT identity Logout removes registration | Auth HTTP/JWKS | Saved ChatGPT record; logout; new login | PASSED |
| Discovery account schema/order | User | Account readiness or prompt | Agent.SetModel / Agent.Prompt → planned auth discovery | resolver → authenticated discovery → generation-keyed access view → readiness/runner | Only visible slugs in server order; compiled metadata joined | Connected discovery Discovery account schema/order | Auth/discovery HTTP/JWKS | Visible and hidden model slugs | PASSED |
| Discovery unknown access | User | Account readiness or prompt | Agent.SetModel / Agent.Prompt → planned auth discovery | resolver → authenticated discovery → generation-keyed access view → readiness/runner | Inference allowed with unknown access | Connected discovery Discovery unknown access | Auth/discovery HTTP/JWKS | Discovery network failure; valid auth/capabilities | PASSED |
| Discovery known denial | User | Account readiness or prompt | Agent.SetModel / Agent.Prompt → planned auth discovery | resolver → authenticated discovery → generation-keyed access view → readiness/runner | Selection/request blocked | Connected discovery Discovery known denial | Auth/discovery HTTP/JWKS | Confirmed denied model | PASSED |
| Unknown metadata capability | User | Account readiness or prompt | Agent.SetModel / Agent.Prompt → planned auth discovery | resolver → authenticated discovery → generation-keyed access view → readiness/runner | No invented capability support | Connected discovery Unknown metadata capability | Auth/discovery HTTP/JWKS | Discovered slug lacks required compiled capability | PASSED |
| Discovery generation invalidation | User | Account readiness or prompt | Agent.SetModel / Agent.Prompt → planned auth discovery | resolver → authenticated discovery → generation-keyed access view → readiness/runner | Old result not published/used | Connected discovery Discovery generation invalidation | Auth/discovery HTTP/JWKS | Old discovery returns after account replacement | PASSED |
| Discovery refresh parity | User | Account readiness or prompt | Agent.SetModel / Agent.Prompt → planned auth discovery | resolver → authenticated discovery → generation-keyed access view → readiness/runner | Same scheduling/resolver and committed account | Connected discovery Discovery refresh parity | Auth/discovery HTTP/JWKS | Expiring credential; discovery then inference | PASSED |
| ChatGPT streaming | User | ChatGPT prompt | Agent.Prompt through Responses | runner → Responses profile/buildResponsesCall → final serialized validation → Fold | HTTP stream=true; no background run | Responses connected ChatGPT streaming | Provider HTTP | Subscription Responses call | PASSED |
| ChatGPT local history/store | User | ChatGPT prompt | Agent.Prompt through Responses | runner → Responses profile/buildResponsesCall → final serialized validation → Fold | store=false; full local input; canonical history unchanged | Responses connected ChatGPT local history/store | Provider HTTP | Tool/thinking transcript | PASSED |
| ChatGPT previous response ID | User | ChatGPT prompt | Agent.Prompt through Responses | runner → Responses profile/buildResponsesCall → final serialized validation → Fold | No HTTP previous_response_id | Responses connected ChatGPT previous response ID | Provider HTTP | Previous output exists | PASSED |
| ChatGPT system conversion | User | ChatGPT prompt | Agent.Prompt through Responses | runner → Responses profile/buildResponsesCall → final serialized validation → Fold | Instructions/developer encoding; no explicit system input item | Responses connected ChatGPT system conversion | Provider HTTP | Canonical system and developer content | PASSED |
| ChatGPT grouped tools | User | ChatGPT prompt | Agent.Prompt through Responses | runner → Responses profile/buildResponsesCall → final serialized validation → Fold | Current documented grouping accepted by actual serialized request | Responses connected ChatGPT grouped tools | Provider HTTP | Function/custom tools | PASSED |
| ChatGPT forbidden background | User | ChatGPT prompt with explicit background | Agent.Prompt through Responses | runner → profile → final SDK JSON validation | Explicit background rejected before HTTP; generated default absent | Responses final background | Inference HTTP | Saved ChatGPT record; explicit option/extra-body value and default case | PASSED |
| ChatGPT forbidden conversation | User | ChatGPT prompt with explicit conversation | Agent.Prompt through Responses | runner → profile → final SDK JSON validation | Explicit conversation rejected before HTTP; generated default absent | Responses final conversation | Inference HTTP | Saved ChatGPT record; explicit option/extra-body value and default case | PASSED |
| ChatGPT forbidden max_output_tokens | User | ChatGPT prompt with explicit max_output_tokens | Agent.Prompt through Responses | runner → profile → final SDK JSON validation | Explicit max_output_tokens rejected before HTTP; generated default absent | Responses final max_output_tokens | Inference HTTP | Saved ChatGPT record; explicit option/extra-body value and default case | PASSED |
| ChatGPT forbidden max_tool_calls | User | ChatGPT prompt with explicit max_tool_calls | Agent.Prompt through Responses | runner → profile → final SDK JSON validation | Explicit max_tool_calls rejected before HTTP; generated default absent | Responses final max_tool_calls | Inference HTTP | Saved ChatGPT record; explicit option/extra-body value and default case | PASSED |
| ChatGPT forbidden metadata | User | ChatGPT prompt with explicit metadata | Agent.Prompt through Responses | runner → profile → final SDK JSON validation | Explicit metadata rejected before HTTP; generated default absent | Responses final metadata | Inference HTTP | Saved ChatGPT record; explicit option/extra-body value and default case | PASSED |
| ChatGPT forbidden moderation | User | ChatGPT prompt with explicit moderation | Agent.Prompt through Responses | runner → profile → final SDK JSON validation | Explicit moderation rejected before HTTP; generated default absent | Responses final moderation | Inference HTTP | Saved ChatGPT record; explicit option/extra-body value and default case | PASSED |
| ChatGPT forbidden multi_agent | User | ChatGPT prompt with explicit multi_agent | Agent.Prompt through Responses | runner → profile → final SDK JSON validation | Explicit multi_agent rejected before HTTP; generated default absent | Responses final multi_agent | Inference HTTP | Saved ChatGPT record; explicit option/extra-body value and default case | PASSED |
| ChatGPT forbidden prompt | User | ChatGPT prompt with explicit prompt | Agent.Prompt through Responses | runner → profile → final SDK JSON validation | Explicit prompt rejected before HTTP; generated default absent | Responses final prompt | Inference HTTP | Saved ChatGPT record; explicit option/extra-body value and default case | PASSED |
| ChatGPT forbidden prompt_cache_retention | User | ChatGPT prompt with explicit prompt_cache_retention | Agent.Prompt through Responses | runner → profile → final SDK JSON validation | Explicit prompt_cache_retention rejected before HTTP; generated default absent | Responses final prompt_cache_retention | Inference HTTP | Saved ChatGPT record; explicit option/extra-body value and default case | PASSED |
| ChatGPT forbidden safety_identifier | User | ChatGPT prompt with explicit safety_identifier | Agent.Prompt through Responses | runner → profile → final SDK JSON validation | Explicit safety_identifier rejected before HTTP; generated default absent | Responses final safety_identifier | Inference HTTP | Saved ChatGPT record; explicit option/extra-body value and default case | PASSED |
| ChatGPT forbidden temperature | User | ChatGPT prompt with explicit temperature | Agent.Prompt through Responses | runner → profile → final SDK JSON validation | Explicit temperature rejected before HTTP; generated default absent | Responses final temperature | Inference HTTP | Saved ChatGPT record; explicit option/extra-body value and default case | PASSED |
| ChatGPT forbidden top_logprobs | User | ChatGPT prompt with explicit top_logprobs | Agent.Prompt through Responses | runner → profile → final SDK JSON validation | Explicit top_logprobs rejected before HTTP; generated default absent | Responses final top_logprobs | Inference HTTP | Saved ChatGPT record; explicit option/extra-body value and default case | PASSED |
| ChatGPT forbidden top_p | User | ChatGPT prompt with explicit top_p | Agent.Prompt through Responses | runner → profile → final SDK JSON validation | Explicit top_p rejected before HTTP; generated default absent | Responses final top_p | Inference HTTP | Saved ChatGPT record; explicit option/extra-body value and default case | PASSED |
| ChatGPT forbidden truncation | User | ChatGPT prompt with explicit truncation | Agent.Prompt through Responses | runner → profile → final SDK JSON validation | Explicit truncation rejected before HTTP; generated default absent | Responses final truncation | Inference HTTP | Saved ChatGPT record; explicit option/extra-body value and default case | PASSED |
| ChatGPT forbidden user | User | ChatGPT prompt with explicit user | Agent.Prompt through Responses | runner → profile → final SDK JSON validation | Explicit user rejected before HTTP; generated default absent | Responses final user | Inference HTTP | Saved ChatGPT record; explicit option/extra-body value and default case | PASSED |
| ChatGPT generated defaults (also individually checked above) | User | ChatGPT prompt | Agent.Prompt through Responses | runner → Responses profile/buildResponsesCall → final serialized validation → Fold | Unsupported defaults omitted; final body valid | Responses connected ChatGPT generated defaults | Provider HTTP | SDK output/max defaults | PASSED |
| ChatGPT profile bypass attempt | User | ChatGPT prompt | Agent.Prompt through Responses | runner → Responses profile/buildResponsesCall → final serialized validation → Fold | Final binding and profile still enforced | Responses connected ChatGPT profile bypass attempt | Provider HTTP | Injected client; custom headers/body | PASSED |
| Subscription exhausted allowance | User | ChatGPT prompt | Agent.Prompt through Responses | runner → Responses profile/buildResponsesCall → final serialized validation → Fold | Distinct terminal class; no 429 loop | Responses connected Subscription exhausted allowance | Provider HTTP | HTTP and stream allowance error | PASSED |
| Transient rate limit | User | ChatGPT prompt | Agent.Prompt through Responses | runner → Responses profile/buildResponsesCall → final serialized validation → Fold | Distinct classification for H9 | Responses connected Transient rate limit | Provider HTTP | Retryable HTTP/stream rate limit | PASSED |
| No replay after public output | User | ChatGPT prompt | Agent.Prompt through Responses | runner → Responses profile/buildResponsesCall → final serialized validation → Fold | One request; partial output retained; one settlement | Responses connected No replay after public output | Provider HTTP | Text/tool event then failure | PASSED |
| xAI code and URL validation | User | ask auth login --provider <id> xAI or refreshed prompt | cmd/tui auth / Agent.Prompt | device strategy → bounded wait/poll → validation → store; shared resolver on refresh | No unsafe display/activation | xAI connected xAI code and URL validation | Provider HTTP | Empty codes or non-HTTPS URL | PASSED |
| xAI first poll interval | User | ask auth login --provider <id> xAI or refreshed prompt | cmd/tui auth / Agent.Prompt | device strategy → bounded wait/poll → validation → store; shared resolver on refresh | First poll waits at least interval | xAI connected xAI first poll interval | Provider HTTP | Device response sets interval | PASSED |
| xAI authorization pending | User | ask auth login --provider <id> xAI or refreshed prompt | cmd/tui auth / Agent.Prompt | device strategy → bounded wait/poll → validation → store; shared resolver on refresh | Waits again; one commit | xAI connected xAI authorization pending | Provider HTTP | Pending then success | PASSED |
| xAI slow-down | User | ask auth login --provider <id> xAI or refreshed prompt | cmd/tui auth / Agent.Prompt | device strategy → bounded wait/poll → validation → store; shared resolver on refresh | Polling never speeds up; absent interval adds required delay | xAI connected xAI slow-down | Provider HTTP | Slow-down interval lower or absent | PASSED |
| xAI denial variants | User | ask auth login --provider <id> xAI or refreshed prompt | cmd/tui auth / Agent.Prompt | device strategy → bounded wait/poll → validation → store; shared resolver on refresh | Prior credential retained; terminal error | xAI connected xAI denial variants | Provider HTTP | access_denied or authorization_denied | PASSED |
| xAI expiry | User | ask auth login --provider <id> xAI or refreshed prompt | cmd/tui auth / Agent.Prompt | device strategy → bounded wait/poll → validation → store; shared resolver on refresh | No later token request | xAI connected xAI expiry | Provider HTTP | Code expires while waiting | PASSED |
| xAI stalled poll deadline | User | ask auth login --provider <id> xAI or refreshed prompt | cmd/tui auth / Agent.Prompt | device strategy → bounded wait/poll → validation → store; shared resolver on refresh | Call ends within remaining device lifetime | xAI connected xAI stalled poll deadline | Provider HTTP | HTTP never completes | PASSED |
| xAI cancellation | User | ask auth login --provider <id> xAI or refreshed prompt | cmd/tui auth / Agent.Prompt | device strategy → bounded wait/poll → validation → store; shared resolver on refresh | No later request; signal exit; no leak | xAI connected xAI cancellation | Provider HTTP | Cancel wait or poll | PASSED |
| xAI refresh token retention | User | ask auth login --provider <id> xAI or refreshed prompt | cmd/tui auth / Agent.Prompt | device strategy → bounded wait/poll → validation → store; shared resolver on refresh | Prior token retained only for this strategy | xAI connected xAI refresh token retention | Provider HTTP | Refresh omits replacement token | PASSED |
| xAI refresh token replacement | User | ask auth login --provider <id> xAI or refreshed prompt | cmd/tui auth / Agent.Prompt | device strategy → bounded wait/poll → validation → store; shared resolver on refresh | New record commits as one unit | xAI connected xAI refresh token replacement | Provider HTTP | Refresh returns new token | PASSED |
| xAI opaque identity | User | ask auth login --provider <id> xAI or refreshed prompt | cmd/tui auth / Agent.Prompt | device strategy → bounded wait/poll → validation → store; shared resolver on refresh | Generation binding; no invented account ID | xAI connected xAI opaque identity | Provider HTTP | Opaque token without identity claim | PASSED |
| xAI key Responses route | User | Key prompt | Agent.Prompt | runner → xAI profile → Responses | api.x.ai/v1/responses; correct key billing provenance | xAI key inference | Provider HTTP | Saved xAI key; OpenAI env conflict | PASSED |
| xAI OAuth Responses route | User | Subscription prompt | Agent.Prompt | runner → xAI profile → Responses | Same xAI endpoint with OAuth provenance; no Anthropic codec | xAI OAuth inference | Provider HTTP | Saved xAI OAuth; OpenAI env conflict | PASSED |
| xAI reasoning model | User | Reasoning prompt | Agent.Prompt | Responses profile → final request | Encrypted reasoning included only with verified support | xAI reasoning conditional | Provider HTTP | Reasoning and nonreasoning compiled models | PASSED |
| Secret redaction and no auth capture | User | Login, refresh, inference capture | cmd/tui run / Agent.Prompt | dispatch → private auth client; inference redaction → cassette | No code/token/JWT/identity headers in output or artifacts | CLI and cassette secret scan | Provider HTTP | Synthetic canary secrets; capture enabled | PASSED |
| Legacy API-key regression | User | Existing Token Plan/OpenAI prompt and SetModel | cmd/tui run / Agent.SetModel | Existing key hooks/compat → unchanged key adapter route | Existing request/replay and key behavior retained | Existing key suites plus connected switch | Provider HTTP | Token Plan Messages/Completions; OpenAI Responses | PASSED |
| Anthropic live route | User | Authorized login → prompt/tool → logout | Built ask command and prompt | Real app/auth/store/provider → real provider endpoints | Correct account/method/profile; tool event; terminal success; local removal | Opt-in live Anthropic | None | Authorized eligible account; isolated 0700 home | PASSED |
| ChatGPT live route | User | Authorized login → prompt/tool → logout | Built ask command and prompt | Real app/auth/store/provider → real provider endpoints | Correct account/method/profile; tool event; terminal success; local removal | Opt-in live ChatGPT | None | Authorized eligible account; isolated 0700 home | PASSED |
| xAI live route | User | Authorized login → prompt/tool → logout | Built ask command and prompt | Real app/auth/store/provider → real provider endpoints | Correct account/method/profile; tool event; terminal success; local removal | Opt-in live xAI | None | Authorized eligible account; isolated 0700 home | PASSED |

| Durable pending refresh fence | Request caller | Prompt needs refresh | Agent.Prompt → app runner → auth.Resolve | settings durable pending-attempt transaction → bounded exchange → validated replacement/clear | Fence exists before first token request; one-record invariant retained | Connected fenced rotation | Token/inference HTTP | Expired record; attempt and network barriers | PASSED |
| Pending fence save failure | Request caller | Prompt needs refresh | Agent.Prompt through app | resolver → failed fence durability → classified stream failure | Zero token/inference requests; no false success | Command fence-write fault | External HTTP only; real filesystem fault boundary | Failed temp/rename/directory sync before exchange | PASSED |
| Restart after uncertain rotation | User | New prompt in process B | Real command test subprocess → app → auth.Resolve | fresh read sees unresolved fence → reauthentication result | No second rotating request and no API-key fallback | Two-process lost-response/death/save-failure checks | External token/inference HTTP | Process A fenced grant; server rotates; A dies or loses response | PASSED |
| Explicit recovery of fenced record | User | Auth login or local logout | Real auth command dispatch | verified login/revision replace or local delete → next resolution | Fence removed only by explicit safe transaction | Command login/logout recovery | External HTTP/JWKS | Saved pending attempt; successful and failed recovery variants | PASSED |
| Signal commit drain | User/OS | SIGINT, SIGTERM or SIGHUP during rotation | Command signal handler → app auth wait | stop new work → cancel inference → bounded registered auth completion | Replacement confirmed before signal exit; existing exit code retained | Subprocess commit barrier beyond two seconds | External HTTP; real I/O barrier | Valid response; pending commit; release before five-second budget | PASSED |
| Second signal and forced death | User/OS | Second ordinary signal or forced process death | Same command signal/drain boundary | second signal retains commit bound; forced death leaves durable fence | No ordinary early exit; next process cannot reuse uncertain grant | Subprocess second-signal/budget/death checks | External HTTP | Commit in progress, then second signal or kill | PASSED |
| Callback input size bound | Browser/local actor | Oversize loopback request then valid callback | Real registered loopback listener from auth command | 16 KiB pre-parse bound → reject without consuming attempt → valid callback | No exchange for oversize request; valid pending attempt remains usable | CLI oversized callback then valid request | External auth HTTP | Real pending listener and oversized request target/query | PASSED |
| Private input size bound | User | Oversize callback/code/key stdin | Real command private-input boundary | 16 KiB bounded reader → sanitized reject/cleanup | No exchange or replacement; bounded cleanup and no secret echo | Command oversized stdin and cancel | External auth HTTP | Pending login and over-limit private line | PASSED |
| Shared CLI test transport wiring | Test actor at real user boundary | User auth argv and next prompt | Compiled test subprocess → runWithDependencies | real dispatch/app/owners → logical URL guard → separate external RoundTrippers | Fixed origin/identity contract and no captured token traffic | Native command-to-next-prompt for all methods | Only external auth/JWKS/discovery/inference HTTP | Isolated home, signed test tokens and deterministic clocks | PASSED |
| Named-choice sibling contract | Request caller | Prompt with nonempty named choice | Agent.Prompt → registry → each selected wire | immutable option copy → body/profile encode or pre-HTTP reject | No silent ignore; no shared mutation or canonical name change | Connected Messages/Completions/Responses/faux choice cases | External inference HTTP | Named declaration plus unsupported pairing variant | PASSED |

- **Gate status:** PASSED after applying all six user-approved corrections and rechecking the full planned path.
This matrix records creation-time design coverage only.
It does not update implementation or live acceptance status; use the current execution record and linked progress report.

### Supporting verification and consistency

Use Full tier verification because there are six phases.
At plan creation, the controller was required to perform the red-team and validation gates after the draft.
Existing symbols and caller paths are grounded in the local scout and direct reads; new filenames/functions are proposed work.
Each phase names its source inventory, protected callers, tests-first sequence and regression gate.
Re-read all seven files after review changes and check the same-record, local-logout, partial-H4 and exactly-once contracts across them.
The creation-time planning pass did not mark implementation checkboxes or phase status complete.

## Risks and rollback

Provider approval, account eligibility and current ChatGPT grouped-tool acceptance need live evidence.
If a live route rejects the native client/profile, stop that route and replan its registration/profile; do not substitute an API key.
Select and pin a maintained OIDC verifier during phase 4 and keep it inside auth.
The file store supports one authoritative local host/home with reliable OS locks and rename, not independent replicas.
Disable new method registrations first for rollback and preserve credential records/unknown fields.
Stop every command-owned listener/test process before removing a test home.

## Unresolved questions

None for product scope or command surface.
The user approved all six source-backed corrections on 2026-10-06; they are applied across the affected phases.
The two account-storage and logout scope decisions are resolved.
All three live routes passed; their evidence is linked in the current execution record.
None for H7a acceptance.
The exact internal-commit actor, final review, and final tagged lint checks passed.

<!-- slug: h7a-subscription-auth -->

## Red Team Review

### Session — 2026-10-06

Four lenses produced nine raw findings, deduplicated to six accepted changes.
Severity: three High and three Medium.
All six have source file:line evidence; none is rejected.
The user approved all six changes; the controller applied them to the phase contracts and runtime matrix.
See the [decision record](./reports/review-decisions.md) and linked source evidence.

| Finding | Severity | Disposition | Applied to |
| --- | --- | --- | --- |
| Durable uncertainty fence for rotating grants. | High | Accept; applied. | Phases 1–2, 6 and runtime contract. |
| Auth commit drain during signal shutdown. | High | Accept; applied. | Phases 2–3, 6 and runtime contract. |
| Explicit CLI external HTTP/time test harness. | High | Accept; applied. | Phases 3–6 and proof notes. |
| Callback/private input bounds and oversize checks. | Medium | Accept; applied. | Phases 3–4, 6 and proof rows. |
| Exact native xAI denial variants. | Medium | Accept; applied. | Phase 5 and proof matrix. |
| Complete option/constructor consumer inventory. | Medium | Accept; applied. | Phases 1–4 and contract inventory link. |

### Verification Results

- Tier: Full; four roles and 16 sampled claims per phase, 96 total.
- Initial source review: Verified 72; Failed 6; Unverified 18 explicitly planned implementation/live claims.
- All six failed samples are resolved by the accepted corrections; remaining failed design claims: 0.
- The 18 future-work samples remain implementation/acceptance obligations, not claims about existing code or blocking unknown product decisions.
- No current-source dependency is left unverified by this planning review.
- At plan creation, no product test, build, lint, login, or inference was executed.

### Validation decisions

The earlier questions resolved one saved account/provider, local-only logout and the Pi-based headless adaptation.
The final review question approved all six technical corrections.
No additional product question was found by the four reviewers.
The exact command methods, new-account replacement, auth input limits and signal codes are recorded above and in the owning phases.
At creation time, implementation was a separate step and the request was planning only.
Implementation execution is now recorded above.

### Whole-Plan Consistency Sweep

All seven plan files were re-read after applying the six decision deltas.
The sweep checked pending fences, cancellation/drain, command test composition, size bounds, xAI errors and consumer/copy ownership across overview, phases, risks and acceptance.
The final matrix covers 143 atomic capabilities, including explicit interactive/noninteractive method selection and the accepted recovery, shutdown and input checks.
The [independent final validation](./reports/final-validation.md) confirms the six corrected contracts and zero unresolved design contradictions.
Historical reviewer reports retain their initial findings and source counts; the decision record supplies their final disposition.
Unresolved design contradictions: 0.
At the creation-time sweep, implementation checkboxes and phase status remained pending/todo.
The current CLI status is completed with 6/6 complete phases and 22/22 checked phase criteria.
The exact internal-commit actor and final review gates are closed.
