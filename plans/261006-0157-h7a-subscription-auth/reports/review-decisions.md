# H7a review decisions

Status: all six changes approved by the user and applied; final design validation complete.
This is a planning record, not evidence that H7a is implemented.
The user selected Pi as the reference for one saved account, local logout and the headless adaptation.
Those decisions are not open again.

## Proposed changes with source evidence

| Finding | Disposition | Source evidence | Concrete plan change |
| --- | --- | --- | --- |
| A later process can retry an uncertain rotating grant. | Accept; applied. | `internal/settings/doc.go:1` has no durable store today; phase 1 proposes only record/revision metadata, and phase 2 stops only the current request. | Add a durable per-record refresh-attempt fence before the exchange; clear it only with a validated durable rotation commit, or require explicit recovery/reauthentication. |
| Signal shutdown can stop a validated rotation commit. | Accept; applied. | `cmd/tui/headless.go:247` starts a two-second abort grace and returns at line 255 even when work is unfinished. | Coordinate shutdown with the bounded auth commit budget and test a signal between response validation and durable save. |
| Built-command offline auth endpoints have no defined test transport. | Accept; applied. | `cmd/tui/args.go:33` supplies an in-process inference RoundTripper; `cmd/tui/headless.go:47` receives no auth transport and dispatch currently parses normal prompts. | Specify a constructor-injected command harness that runs real parsing/dispatch/app services with only external HTTP/time boundaries controlled; use subprocess checks for real binary signal/lock behavior. |
| Callback and private input have no explicit size-limit checks. | Accept; applied. | Pi `packages/ai/src/auth/oauth/callback-server.ts:80` parses the raw URL and `openai-chatgpt.ts:67` parses full input; the Ask architecture report line 517 requires bounded input. | Add a finite limit before parsing and command-boundary oversized-input checks without exchanging or replacing credentials. |
| Changed option and constructor inventory is overstated. | Accept; applied. | `internal/providers/faux/faux.go:252` clones options, and `responses_prompt.go:17` / `completions_body.go:14` consume them; the scout lists only production function calls. | Link the contract reviewer's complete inventory, name immutable copy rules, and require each wire to encode or explicitly reject nonempty named choice. |
| Wrong xAI denial variant. | Accept; applied. | Pinned Pi `packages/ai/src/auth/oauth/xai.ts:190` accepts `authorization_denied`; the plan matrix currently says `authorization_declined`. | Use `access_denied` and `authorization_denied` in the protocol scenarios and matrix. |

The durable fence is part of the existing credential transaction, not a separate journal service.
A process crash after the fence but before an exchange can require reauthentication even if the remote grant did not rotate.
This conservative result prevents a later process from blindly reusing a potentially consumed refresh token.
Network success and disk persistence remain separate transactions.

The auth command test harness must enter the real CLI parsing and dispatch boundary.
It must not call an OAuth helper directly and label that test E2E.
It must not add production flags that disable endpoint, signature or TLS validation.
The ordinary binary still needs subprocess signal and cleanup checks.

## Review and validation status

Four reports checked 96 source claims: 72 VERIFIED, 6 FAILED and 18 UNVERIFIED proposed implementation or live acceptance claims.
The six failed samples map to the fixes above and are resolved in the updated plan.
The user approved all six changes on 2026-10-06; no account/logout decision was reopened.
The 18 future-work samples are not failed current-source facts and are not executed-test evidence.
Nine raw findings deduplicate to six findings: three High and three Medium.
All six pass the file:line evidence filter; none is rejected.
The scope reviewer's method-choice supersession note will reconcile the older scout recommendation with the final Pi-style selection contract.
See [security](./security-review.md), [failure](./failure-review.md), [scope](./scope-review.md) and [contract](./contract-review.md) reports.
The whole-plan consistency scan has zero unresolved design contradictions.
Runtime flow proof passes at design level; no implementation or executed test is claimed.

See the [independent final validation](./final-validation.md) for the corrected shutdown, refresh and command contracts.
The final matrix has 143 planned atomic capabilities; all design rows pass.
