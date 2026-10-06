# H7a scope and assumption review

Date: 2026-10-06.
Lens: Assumption Destroyer and Full tier Scope Auditor.
Verdict: Request changes to the plan before implementation handoff.
The plan must deliver native login, local logout, refresh and subscription inference for all three providers through shared services.
Three findings have source evidence: two High and one Medium.
This review used file reads and source searches only.
No Go command, login, credential read, token exchange or live request was run.
The controller's 131-row runtime gate is a design check, not executed acceptance.
All paths below are relative to the work context unless they start with `/`.

## Findings

### High: Rotation uncertainty has no state that survives restart

Location: `phase-02-auth-resolution-and-request-binding.md:39–42,87,129` and `phase-01-start.md:30,39–42`.
Failure condition: The server rotates a refresh token, but its response is lost or the local replacement fails before rename.
The first request returns a safe error, but the old expired credential remains on disk.
A second process reads the same record and submits the old rotating grant again because no committed state tells it that this grant is uncertain.
The requirement to avoid blind retries applies across requests and processes, not only inside one exchange.
Evidence: `phase-01-start.md:30` lists persisted tokens, expiry, scopes, refresh-not-before and method metadata but defines no uncertainty transition.
Evidence: `phase-01-start.md:85` requires prior bytes to survive pre-rename failure, and `phase-02-auth-resolution-and-request-binding.md:129` requires guidance without another rotating request.
Evidence: `/Users/dale/Desktop/workspace/opensources/pi/packages/ai/src/auth/resolve.ts:122–145` retries refresh from the saved expiry state and reports an exchange error without changing that state.
Evidence: `/Users/dale/Desktop/workspace/opensources/pi/packages/coding-agent/src/core/auth-storage.ts:182–190` writes only a successful callback result, so that reference does not supply a persistent fence.
Fix: Specify a settings-owned durable pending/uncertain rotation state for the provider generation before sending a rotating grant, with auth-owned recovery rules.
For example, a later reader of `rotation_state=pending` must return reauthentication or verified recovery instead of submitting that grant.
Clear that state only with the validated durable replacement or an explicit successful login/logout transaction.
If the fence cannot be saved, do not start the exchange.
Add two-process tests in which process A loses the response or fails the replacement and process B then resolves the same provider with zero additional token requests.
This is a safe local failure state, not a claim that remote rotation and disk commit can be atomic.

### High: Existing signal shutdown can end the process before rotation commit

Location: `phase-02-auth-resolution-and-request-binding.md:41,98` and `phase-03-anthropic-and-headless-login.md:34`.
Failure condition: SIGINT, SIGTERM or SIGHUP arrives after a validated rotation, and local sync/rename takes longer than two seconds.
An independent context keeps the commit alive inside Go, but it cannot keep the operating-system process alive after the CLI returns.
The retained CLI shutdown path exits after `abortGrace` or a second signal, before the required commit can finish.
Evidence: `cmd/tui/headless.go:43–44` sets `abortGrace` to two seconds.
Evidence: `cmd/tui/headless.go:242–255` returns on grace expiry or a second signal without requiring the prompt goroutine to finish.
Evidence: `cmd/tui/main.go:122` exits with the result of `run`.
Evidence: `phase-02-auth-resolution-and-request-binding.md:41` requires commit after validated rotation even if inference was canceled, while phase 3 preserves signal exit behavior without an explicit commit drain.
Fix: Add an app/auth-owned bounded shutdown drain for validated rotation commits and make both auth-command and inference signal paths wait for it before returning the existing signal exit code.
For example, cancellation stops inference immediately, then `DrainCommits(shutdownCtx)` completes or returns explicit uncertainty before process exit.
Define second-signal behavior during this critical section and its relation to the commit deadline.
Add a built-binary test that blocks commit beyond the current two-second grace, sends each signal, then releases commit and checks the replacement from a second process.
Preserve the public exit codes; the current grace policy is an implementation detail that must support the new durability contract.

### Medium: The xAI denial fixture names the wrong second provider error

Location: `plan.md:255`.
Failure condition: An implementation follows the proof matrix's `authorization_declined` fixture and does not recognize the actual `authorization_denied` response.
That response can enter generic transport/protocol handling instead of the required explicit authorization-denial branch.
Evidence: `/Users/dale/Desktop/workspace/opensources/pi/packages/ai/src/auth/oauth/xai.ts:190–191` recognizes `access_denied` and `authorization_denied`.
Evidence: `phase-05-xai-device-authorization.md:33` requires both denial names, but `plan.md:255` names `access_denied` and `authorization_declined`.
Fix: Change the prepared response to `authorization_denied` and name both exact values in phase 5's tests.
Assert terminal denial, no further poll and retention of the prior credential for each value.

## Scope and invariant checks

The one-record/provider rule, local-only logout and no retained account/client mapping are consistent with parent H7a requirements at `plans/260930-2254-pi-feature-inventory-go-roadmap/phase-h7a-subscription-auth.md:27–29`.
Verified OIDC, returning issued-client reuse and explicit new-account replacement remain in phase 4; no change to those accepted choices is needed.
The first Anthropic prompt is an early checkpoint, and all three live routes remain required by `plan.md:18,116` and phase 6's success criteria.
H4 is a partial wire dependency at `plan.md:59–62`; it does not block settings, resolution, commands or Anthropic work.
Remote catalogs, custom configuration, leader/gateway login, full TUI and public session selection have named later owners, so their exclusion is explicit rather than silent deferral.
No required H7a capability was silently deferred in the six phase files.

The final method-choice contract is consistent between `plan.md:103` and `phase-03-anthropic-and-headless-login.md:27`.
The cited local scout still says OAuth is the default at `plans/reports/researcher-261006-0157-h7a-local-runtime-scout.md:18`.
The Pi proof also recommends direct OAuth selection at `plans/reports/researcher-261006-0157-h7a-pi-runtime-proof.md:182`, while its production trigger evidence describes a selector when several methods match.
Add a short supersession note in the plan's source-adaptation section so an implementer does not treat those earlier recommendations as the current command contract.
Keep the accepted explicit method-choice behavior; this is an evidence reconciliation, not a request for a new product decision.

## Four sampled claims per phase

VERIFIED means the cited source supports the baseline or reference claim.
FAILED means the cited source contradicts the claim.
UNVERIFIED means a new owner or behavior is correctly proposed but does not exist in the baseline.
None of these labels means a test passed.

| Phase | Sampled claim | Result | Source evidence |
| --- | --- | --- | --- |
| 1 | Settings is the credential-file owner. | VERIFIED. | `internal/settings/doc.go:1` and `docs/ask-architecture-reference.md:77`. |
| 1 | StreamOptions currently has legacy APIKey. | VERIFIED. | `internal/providers/types.go:33–44`. |
| 1 | StreamFn and Provider.Stream can retain their current signatures. | VERIFIED. | `internal/providers/types.go:48–55`. |
| 1 | The durable schema, sidecar and typed snapshot are new work. | UNVERIFIED, explicitly proposed. | `phase-01-start.md:61–65`; baseline settings has only `doc.go` and `README.md`, as recorded in the local scout at `:56`. |
| 2 | ResolveKey has two production callers. | VERIFIED. | `internal/agent/agent.go:142` and `internal/agent/loop_stream.go:98`. |
| 2 | Each model request reads the key immediately before Stream. | VERIFIED. | `internal/agent/loop_stream.go:24–35`. |
| 2 | SetModel checks idle state again after resolution. | VERIFIED. | `internal/agent/agent.go:132–155`. |
| 2 | Stream.Result settles independently and wins over canceled context. | VERIFIED. | `internal/providers/stream.go:62–80,88–93`. |
| 3 | Auth dispatch must precede prompt parsing and capture. | VERIFIED as a necessary change. | `cmd/tui/headless.go:47–48,72–81` and `cmd/tui/args.go:89–94`. |
| 3 | Current Anthropic auth fallback is Token Plan. | VERIFIED. | `internal/providers/anthropic/provider.go:162–168`. |
| 3 | Fold publishes incoming tool names before any name callback can restore them. | VERIFIED. | `internal/providers/fantasykit/fold.go:153`. |
| 3 | Forced named tool choice needs a new typed option. | VERIFIED. | `internal/providers/types.go:33–44` has no choice field. |
| 4 | Responses already disables server storage. | VERIFIED. | `internal/providers/openai/responses_prompt.go:26–27`. |
| 4 | Current tool serialization is flat function tools. | VERIFIED. | `internal/providers/openai/responses_prompt.go:71–90`. |
| 4 | Model headers can currently replace Authorization. | VERIFIED. | `internal/providers/openai/isolate.go:20–25,60–64`. |
| 4 | Verified ChatGPT identity and generation-bound discovery are new owners. | UNVERIFIED, explicitly proposed. | `phase-04-chatgpt-identity-and-responses.md:76–77`; local scout `:72` records their baseline absence. |
| 5 | xAI waits before its first poll. | VERIFIED. | Pi `packages/ai/src/auth/oauth/xai.ts:161–166` and `auth/oauth/device-code.ts:57–64`. |
| 5 | xAI can retain an omitted replacement refresh token. | VERIFIED. | Pi `packages/ai/src/auth/oauth/xai.ts:128–134`. |
| 5 | The second xAI denial name is authorization_declined. | FAILED. | `plan.md:255` conflicts with Pi `packages/ai/src/auth/oauth/xai.ts:190`. |
| 5 | Ask must add device-lifetime bounds to stalled HTTP. | VERIFIED as a necessary addition. | Pi `packages/ai/src/auth/oauth/device-code.ts:64–71` waits for `poll()` before checking expiry again. |
| 6 | Ordinary constructors must avoid the current full server Module. | VERIFIED. | `internal/app/app.go:24–44` supplies DB/server construction and startup. |
| 6 | Existing live OpenAI testing does not establish subscription acceptance. | VERIFIED. | `internal/providers/openai/responses_test.go:120` is the key-only Responses replay test; local scout `:66–67` records its limit. |
| 6 | Three native live routes must pass separately from offline proof. | UNVERIFIED, explicitly planned acceptance. | `phase-06-cumulative-acceptance-and-documentation.md:79,114,120` and `plan.md:127–128`. |
| 6 | The shared auth/fx composition tests are future integration work. | UNVERIFIED, explicitly proposed. | `phase-06-cumulative-acceptance-and-documentation.md:57,63`; baseline `internal/app/app.go:24` has only the server Module. |

Pi paths in this table are under `/Users/dale/Desktop/workspace/opensources/pi/`.

## State lifetime and owner audit

The following includes existing fields that gain a new role.
New paths are proposed owners, not implemented files.

| State | Lifetime and owner | Disposition |
| --- | --- | --- |
| Tagged credential, refresh token, ID-token hint and verified client/account. | Store lifetime; proposed `internal/settings/auth.go`. | One record is explicit, and local logout deletes the complete mapping. |
| Store revision, including absence. | Store lifetime; proposed `internal/settings/auth.go`. | Defined across login, refresh and logout under the same lock. |
| Stable host identity. | Installation lifetime; proposed settings metadata. | Explicitly separate from an account/client registration and retained as nonsecret host data. |
| Pending/uncertain rotation. | Must survive process lifetime; settings persistence plus auth transition rules. | Missing; first High finding. |
| Sidecar lock and current file handles. | Transaction lifetime; proposed settings lock files. | Stable lock inode and bounded wait are explicit, and streaming holds no lock. |
| Temporary replacement file. | Transaction lifetime; proposed settings auth writer. | Sync/close/rename and pre/post-rename errors are explicit, but cleanup must remain part of the transaction tests. |
| PKCE verifier, state, nonce, callback URI and captured revision. | Login-attempt lifetime; proposed auth login/strategy/callback files. | Attempt binding, final revision comparison and exit cleanup are explicit. |
| Browser listener, spare connection and manual input waiter. | Login-attempt lifetime; proposed auth callback plus injected command interaction. | Bounded closure and losing-waiter cancellation are explicit. |
| Device code, interval and expiry deadline. | Login-attempt lifetime; proposed `internal/auth/xai.go`. | Wait, poll and HTTP are bounded by the device and operation deadlines. |
| Resolved access material and selected endpoint/profile. | One request lifetime; proposed providers snapshot passed through StreamOptions. | No refresh/ID token or store handle, and no history/event persistence. |
| Existing Options.APIKey and BoundKey. | Agent configuration lifetime; existing agent config and providers keys. | Preserve provider pinning and prevent a model switch from using another provider's fallback. |
| Existing SessionID and Options.SessionID. | Session/agent lifetime; existing agent and CLI owners. | Reused for event/cache identity, not as proof of OAuth account identity; `internal/providers/openai/responses_prompt.go:35–38` uses it for prompt caching. |
| Existing Model.Headers and compatibility options. | Model snapshot lifetime; existing provider model and wire owners. | Final typed-auth validation must prevent header replacement and profile bypass. |
| Name codec and decoded tool names. | Request lifetime; proposed Anthropic codec and adapter stream mapping. | Historical/active snapshot use and canonical restoration before publication are explicit. |
| Account access cache and in-flight discovery result. | Auth-service process lifetime; proposed `internal/auth/discovery.go`. | Identity/generation invalidation is explicit; choose and document freshness and bounded cache policy during phase 4 rather than storing it in credentials. |
| OIDC verifier and JWKS cache. | Auth-service process lifetime; proposed identity owner and maintained verifier. | Algorithm/source restrictions and bounded key refresh are explicit. |
| Independent rotation commit context. | May outlive request cancellation; proposed auth operation with app shutdown ownership. | Process-drain interaction is missing; second High finding. |
| Ordinary app constructors, shared runner and readiness function. | Runtime/process lifetime; proposed `internal/app/module_auth.go`. | One composition owner and no headless DB/gateway startup are explicit. |
| Existing cassette recorder and transport. | Capture/process lifetime; existing CLI and cassette owners. | Auth endpoint transport is separate, and inference secret redaction is explicit. |

## Positive checks and questions

The plan uses one shared resolver and keeps auth protocols out of wire adapters and the agent.
The durable revision handles an absent-record login/logout race instead of relying on record presence.
The plan keeps real internal owners in connected checks and treats only external endpoint servers as mocks.
There are no unresolved product questions in this review.
The controller must resolve the two rotation lifetime contracts and the xAI fixture before handoff.

Status: DONE_WITH_CONCERNS.
Summary: Full scope and 24 source claims were checked, with three material plan findings.
Concerns: Restart-safe rotation uncertainty and signal commit drain need explicit contracts; the xAI denial fixture needs correction.
