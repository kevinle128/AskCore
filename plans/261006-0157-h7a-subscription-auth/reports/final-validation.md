# H7a final failure-mode design validation

Design gate: PASSED.
Unresolved design contradictions: 0.
This result applies to the corrected plan, not to implementation or test execution.

I read the complete `plan.md` and all six complete phase files again after the accepted corrections.
The review used the accepted findings in `failure-review.md` and the six changes recorded in the final plan.
The scope remains `--deep --tdd`, without `--yagni`.
No code, tests, build, lint, credentials, live OAuth or inference were read or executed during this validation.
Only this report was changed.

## Accepted fix checks

| Accepted correction | Corrected design evidence | Trigger, owner and failure result | Result |
| --- | --- | --- | --- |
| Durable uncertainty fence. | `phase-01-start.md:52–62`, `phase-02-auth-resolution-and-request-binding.md:49–52`, and `plan.md:282–285`. | A prompt that needs refresh reaches auth.Resolve; settings durably writes an attempt tied to the committed provider generation before the grant can leave; failed fence durability sends no request; restart returns recovery without grant reuse or key fallback. | PASSED. |
| Signal commit drain. | `phase-02-auth-resolution-and-request-binding.md:53–64`, `phase-03-anthropic-and-headless-login.md:79–80`, and `phase-06-cumulative-acceptance-and-documentation.md:50–51`. | Auth registers rotating work before exchange; app exposes its bounded wait; both command paths install signal handling and drain auth work before signal exit; a second ordinary signal cannot cut a validated commit short. | PASSED. |
| Real CLI external test wiring. | `phase-03-anthropic-and-headless-login.md:68–78`, `phase-04-chatgpt-identity-and-responses.md:67–68`, `phase-05-xai-device-authorization.md:51–52`, and `plan.md:140–145,290`. | The production wrapper and compiled test subprocess call runWithDependencies with unchanged user argv; parsing, dispatch and ordinary internal constructors are real; separate injected clients intercept external HTTP after logical validation. | PASSED. |
| Callback and private-input limits. | `phase-03-anthropic-and-headless-login.md:81–86`, `phase-04-chatgpt-identity-and-responses.md:69`, and `plan.md:288–289`. | The auth command's callback and private-input owners enforce 16 KiB before parsing; oversize input causes sanitized rejection without exchange or replacement; a rejected callback leaves the attempt available for a valid callback. | PASSED. |
| Exact xAI denial variants. | `phase-05-xai-device-authorization.md:49–54,93` and `plan.md:266`. | The shared command reaches the xAI strategy; access_denied and authorization_denied each cause terminal denial, no later poll and retention of the prior credential. | PASSED. |
| Complete option and constructor coverage. | `phase-01-start.md:63–66`, `phase-02-auth-resolution-and-request-binding.md:101–102`, `phase-03-anthropic-and-headless-login.md:87–90`, and `phase-04-chatgpt-identity-and-responses.md:71–72`. | Provider option values are immutable or copied at the inventory boundaries; each real wire encodes nonempty named choice or rejects it before HTTP; faux copies the value and redacts secrets when records are rendered. | PASSED. |

## Rotation and shutdown sequence

The corrected sequence is fresh locked read, generation/method/account recheck, durable pending attempt, registered bounded exchange, response validation and one replacement transaction that clears the matching attempt.
Registration occurs before exchange can start, so signal shutdown cannot miss an active exchange between response arrival and commit registration.
The generation and attempt ID prevent a stale completion from clearing another attempt.
The whole-file lock remains held through exchange and commit, so logout and replacement use the same ordering boundary.
An unresolved attempt is not cleared by elapsed time, process death, a lease or an unverified response.
A successful explicit login can replace the fenced record, and local logout can remove it through the normal revision transaction.
These rules retain one saved provider record and do not add server revocation or retained client registrations.

The refresh exchange has a 15-second maximum, and local commit has a separate five-second budget from its own start.
The shutdown wait covers the remaining exchange and commit budget plus cleanup instead of using the two-second inference grace.
SIGINT, SIGTERM and SIGHUP keep their existing exit codes.
A second ordinary signal preserves the commit budget.
The plan states that a context cannot interrupt blocked OS sync, and treats budget exhaustion and forced death as uncertainty limits.
The durable pre-exchange fence protects restart from old-grant reuse when no validated replacement is available.
The planned barrier tests extend past the old grace and check saved state from another process before exit.
Process-death, lost-response, replacement-failure and explicit recovery cases all have a second-process trigger in the matrix.

## Command, input and wire sequence

The test helper is compiled test composition around the real command boundary, not an alternate product auth route.
The production wrapper supplies normal dependencies to that same boundary and has separate ordinary-binary help, signal and pipe checks.
No production endpoint, issuer, TLS or signature bypass is exposed.
Auth, JWKS and discovery use the private auth client; inference uses the separate client that can carry capture.
Logical production URLs, callback URIs and real signed-token verification remain in force before external transport interception.
The shared clock/wait values and subprocess barriers are explicit dependencies for xAI polling and rotation fault checks.
Command-owned signal handling starts before early auth dispatch and is stopped on every return.
Listener, spare-connection and input-waiter cleanup remains required on success, failure and cancellation.

Input bounds precede URL/code parsing and prohibit unbounded manual-line draining.
Named choice is checked by all three body builders, with the Messages codec applied consistently and canonical names retained by supported Responses and Completions paths.
Unsupported named choice cannot silently disappear before HTTP.
The phase file tables and contract inventory assign settings, auth, app, command, adapter and option-copy owners.
Phases 4 and 5 reuse these contracts rather than add separate command, store or shutdown paths.

## Delivery boundary

One saved account per provider, local-only logout, issued-client removal on logout and verified ChatGPT identity remain fixed.
All three native login-to-prompt/tool-to-logout routes remain required for completion.
H4 remains a partial Responses wire dependency and does not block storage, common commands or Anthropic work.
Exactly-once result settlement and public lifecycle ownership remain with the existing Stream and Agent contracts.
Implementation checks, race checks, required quality gates and authorized live acceptance remain pending.
No missing runtime trigger or material owner gap remains in the six corrected design contracts.

Status: DONE.
Summary: All seven corrected plan files pass the final failure-mode design gate.
Concerns: No unresolved design contradictions; implementation and provider acceptance are not yet verified.
