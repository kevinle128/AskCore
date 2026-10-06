# H7a independent security and public-contract review

Status: DONE_WITH_CONCERNS.
Score: 8.5/10 for the reviewed offline implementation.
Verdict: Comment; keep H7a incomplete until the remaining acceptance gates pass.

## Summary

H7a adds native Anthropic, ChatGPT, and xAI subscription login, durable credential refresh, and request-bound inference without automatic key fallback.
The reviewed implementation has no confirmed open critical security defect.
The remaining concerns are proof gaps and incomplete live acceptance.
The controller is adding the missing connected checks while this review ends.
This report is a review snapshot, not a completion record.

## Scope and evidence

Read the repository instructions, architecture reference, package owner documents, main H7a plan, six phase contracts, runtime matrix, and shared/native/wire/CLI reports.
Reviewed settings transactions and paths, native login and callback flows, identity verification, discovery, app composition, request bindings, wire guards, tool conversion, and CLI signals.
The working tree includes H4 and unrelated edits.
No finding below attributes an unrelated diff to H7a.
No existing user credential home was read.
No live provider request was made.
Only the authorized report file was written by this reviewer.

## Critical issues

None remain confirmed in the reviewed source.

## Major concerns to close before completion

1. The crash/restart matrix needs its stated command actor proof.
`internal/settings/auth_test.go:482` proves that a child publishes a fence before normal process exit.
Its `fence-exit` helper at `internal/settings/auth_test.go:187` does not contact a token server or die after server rotation.
`internal/settings/auth_test.go:451` checks lost response through the store library.
`internal/app/module_auth_test.go:190` checks a second Agent prompt after invalid rotation by reopening the store in the same process.
These checks support the implementation, but do not prove process-A death/lost-response/replacement-failure followed by a process-B CLI prompt with zero grant reuse.
Add the planned compiled subprocess checks with the real command/app/auth/store path and external token counters.
The controller reports that the CLI tester is now adding death and lost-response checks.

2. Native discovery needs the stated connected readiness proof.
`internal/auth/native_test.go:119` calls the native access function directly for allowed, denied, cached, and unknown discovery.
`internal/agent/agent_auth_test.go:13` checks SetModel with an injected test readiness function.
`internal/auth/service.go:199` rereads the committed record after discovery and rejects a changed method, generation, client, subject, or pending fence.
No reviewed test holds a native discovery response, replaces the account, then proves rejection through app-composed Agent.SetModel or Agent.Prompt.
Add native unknown/denied/stale-response cases through those real actor boundaries and verify that failed SetModel preserves the model and thinking level.
The controller has accepted this gap and will add the connected checks.

3. The signal test does not yet cross the old two-second grace.
`cmd/tui/auth_signal_test.go:145` checks all three signals and a second ordinary signal with real compiled command composition.
The reviewed version releases its external exchange barrier after only 150 ms at `cmd/tui/auth_signal_test.go:208`.
It therefore proves second-signal waiting, but not the planned wait beyond the old two-second grace or a commit barrier.
Hold the external exchange for more than two seconds as a minimum connected check.
Keep the independent commit-bound check at `internal/settings/auth_test.go:625`; it injects a 5.1-second sync delay and checks that the fence survives.
Do not label the external exchange barrier as an internal commit barrier.

4. Live acceptance remains pending for all three providers.
The user has the three accounts, so this is an execution gate rather than a missing-account claim.
Run authorized login, prompt, real tool turn, and local logout with isolated homes and sanitized outcome records.
Offline logical-URL transports cannot prove provider approval, account eligibility, current dynamic-client acceptance, or current grouped-tool acceptance.
The numeric-second/RFC3339 interpretation of earliest_refresh_at has inspected client evidence and offline tests; live response evidence remains required.

## Resolved defect

Private stdin originally bounded the retained string rather than all input bytes.
A production binary accepted 20,000 carriage returns followed by a short API key, returned code 0, and saved the credential in an isolated home.
This violated the accepted 16 KiB pre-parse bound because discarded carriage returns did not count.
The controller added total received-byte counting before filtering and editing at `cmd/tui/auth_input.go:53`.
`TestPrivateInputBoundsDiscardedBytes` at `cmd/tui/auth_test.go:50` now covers the discarded-byte path.
The reviewer reran `go test ./cmd/tui -run '^TestPrivateInputBounds' -count=1`; it passed.
This finding is closed.

## Acceptance coverage

| Plan criteria and matrix family | Implementation and nearest proof | Review result |
| --- | --- | --- |
| Phase 1: legacy keys, unknown data, one record, absent logout revision | settings decode/marshal/Replace/Logout; `auth_test.go:17`, `:289`, `:326`, `:587`, `:671` | Implemented; real store boundary tests. |
| Phase 1: durable replace, pre/post-rename errors, owner-only paths, corrupt/schema refusal | settings commit/path/no-follow/sidecar flow; `auth_test.go:66`, `:103`, `:243`, `:537` | Implemented; real filesystem and process checks. |
| Phase 1: lock death and canceled waiting | OS flock; `auth_test.go:202`, `:386` | Implemented; subprocess and cancellation checks. |
| Phase 1: immutable runtime auth and redaction | value-only AuthSnapshot; `internal/providers/auth_test.go:10` | Implemented; no refresh/ID token or store handle in snapshot. |
| Phase 2: override precedence, no billed fallback, final request binding | Resolve/ValidateOverride/AuthRunner; service precedence test and app runner tests at `module_auth_test.go:18`, `:52`, `:151` | Implemented; tool-loop re-resolution is connected. |
| Phase 2: refresh timing, one grant, response validation, independent commit | auth Resolve plus settings Refresh; refresh tests at `:15`, `:56`, `:134`, `:219`, `:245`; store tests at `:407`, `:625` | Implemented; process-death and CLI restart actor gaps listed above. |
| Phase 2: readiness and stale discovery | app BindAuth, Agent.SetModel, post-discovery reread | Implemented; native connected readiness/stale-response proof remains open. |
| Phase 2: stop registration and signal drain | beginRefresh/StopRefresh/DrainRefresh/AuthWait and both CLI signal paths | Implemented; old-grace/budget/death actor proof limits listed above. |
| Phase 3: auth dispatch, explicit method, key save, browser/copy-code, logout | auth parser/private interaction/native strategy/store; command tests at `auth_command_test.go:91`, `:114`; OAuth test at `auth_oauth_command_test.go:229` | Both Anthropic interactions and next real tool prompt are connected. |
| Phase 3: callback/input bounds, listener cleanup, signal exit codes | callback guard/private reader; native browser tests and CLI signal tests | Implemented; discarded-byte defect fixed during review. |
| Phase 3: Anthropic identity profile and reversible names | final oauthTransport and historical tool codec; profile tests at `profile_test.go:18`, `:47`, `:61`, `:96`, `:117`; tool-name tests | Implemented; canonical names are restored before public tool events. |
| Phase 4: verified identity, issued-client reuse, new-account replacement, logout reset | maintained OIDC verification, fixed native sources, ChatGPT strategy; `identity_test.go:18`, `native_test.go:219`, CLI OAuth test at `:347` | Implemented; signed-token and common-command evidence. |
| Phase 4: scope, actual expiry, earliest refresh | native token decoder and shared resolver; signed native/CLI gate tests | Implemented; live gate shape remains pending. |
| Phase 4: final Responses restrictions, groups, named choice, local replay, error classes | final profile transport after isolation/SDK serialization; `profiles_test.go:51`, `:115`, `:162`, `:190`, `:230` | Implemented; adapter proof is strong, connected discovery proof remains open. |
| Phase 5: device timing, pending, slow-down, both denials, expiry, cancel, retention | xAI strategy; native tests at `:57`, `:98`, `:162`, `:207`, `:407`; CLI OAuth test at `:521` | Implemented; native and next-prompt command evidence. |
| Phase 5: key/OAuth xAI route and conditional reasoning | typed route/profile plus compiled model; `profiles_test.go:83` | Implemented; live route remains pending. |
| Phase 6: legacy key/tool/replay/stream regression | existing H3/H4 suites plus connected real echo turns and Fold argument regression | No confirmed new regression; full root gates remain controller-owned. |
| Phase 6: every matrix actor row, three live flows, completion metadata | source/test inventory and plan | Incomplete for the gaps above; do not mark complete. |
| Phase 6: owner documents and safe operations | root README/auth/settings/provider/app documents | Documentation updates arrived during review; controller must finish link and claim verification. |

## Public contracts and architecture

Provider.Stream and StreamFn signatures remain unchanged.
Stream.Result and Assembler settlement ownership remain unchanged.
The new StreamOptions Auth and ToolChoice fields are optional values.
BoundKey limits CLI key use to its selected provider while the legacy empty-hook fallback remains compatible.
Settings uses only standard library imports.
Providers do not import auth or settings.
Auth does not import agent, wire adapters, config, or gateway.
App owns composition and injects the same resolver for request and readiness paths.
No generated protocol schema, database migration, or configuration rewrite is required by this auth implementation.
No new production endpoint override, TLS bypass, issuer bypass, or signature bypass was found.

## Minor concerns and suggestions

Faux preserves Auth and ToolChoice as value fields in Call and Record copies at `internal/providers/faux/stream.go:62`, `:72`, and `:102`.
No explicit faux named-choice copy/record/script check was found.
Add a small check for that matrix sibling rather than infer coverage from the wire adapters.
This is a coverage concern, not a confirmed wire defect.

At `internal/auth/service.go:184`, all settings.Refresh errors become ErrRecovery.
This is safe for grant reuse, but loses errors.Is classification for a pre-exchange store failure or post-rename ErrIndeterminate.
Consider preserving the cause with `errors.Join(ErrRecovery, err)` so future H9 consumers can distinguish storage uncertainty while still requiring safe recovery.
This suggestion does not require a new error framework.

## Positive findings

The pending fence is durable before any rotating network exchange.
Validated rotation commits before model discovery, so model denial cannot discard the new refresh grant.
The snapshot is a value with redacted formatting and JSON output.
Final wire guards run after SDK serialization and isolate ambient organization, project, account, cookie, and credential headers.
Token/JWKS clients use fixed trusted native sources, bounded response reads, and no redirects.
The same compiled command boundary is used by production and offline native login-to-prompt tests.
The shared Fold fix preserves accumulated arguments when metadata changes the tool ID and keeps the assembler's authoritative-final contract.

## Executed review checks

The focused review command passed for settings, auth, app, agent, Anthropic, OpenAI, fantasykit, and cmd/tui.
It selected auth, bound-agent, refresh, OAuth, typed-key, profile, and tool-ID checks with `-count=1`.
`go build -o /tmp/ask-h7a-review-01a10ecf ./cmd/tui` passed.
The discarded-byte input reproduction used that ordinary binary with an isolated temporary home and no external request.
The fixed private-input focused tests passed separately.
Worker reports record focused race, vet, and lint passes.
The controller owns the final full test/race/build/lint runs; this reviewer does not claim those unobserved final gates.
The review started no background server and left no owned child process running.

## Unresolved questions

No product-scope question remains.
Remaining execution evidence: connected discovery/readiness, command death/lost-response/replacement-failure and drain limits, faux named-choice coverage, final full gates, and three authorized live routes.
