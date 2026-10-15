# H7a: Subscription auth and request profiles

Creation-time status: planned; no implementation was claimed.
Schedule: priority M1 lane after H3, alongside the required H4 Responses wire work; it does not wait for full H5-H7.
The user requested the split on 2026-10-06 because H4 cannot deliver the complete provider design.
This replaces the earlier proposal to deliver native login inside H4.
The later priority decision (2026-10-06) advances this lane because Alibaba Token Plan expires soon.

## Current execution record

H7a is complete.
The [six-phase execution plan](../261006-0157-h7a-subscription-auth/plan.md) reports completed, 6/6 phases and 22/22 criteria through the installed plan CLI.
The [live acceptance](../reports/tester-261006-0835-h7a-live-acceptance.md) records native Anthropic, ChatGPT, and xAI login, canonical echo tool loops, one settlement, and local logout.
The [exact local-commit actor](../reports/tester-261006-0855-h7a-fsync-actor.md) passed all five cases twice with race instrumentation in 38.186 seconds.
The [controller report](../reports/cook-261006-0849-h7a-implementation.md) records full quality gates, final tagged lint, architecture limits, and owned-resource cleanup.
The [final review](../reports/reviewer-261006-0843-h7a-final-review.md) closed the actor gate with no critical defect.
The requirements and steps below retain the accepted creation-time scope.
This H7a completion does not mark the whole parent roadmap or other phases complete.

## Outcome and dependencies

Deliver usable Anthropic, OpenAI ChatGPT and xAI subscription inference with native headless login/logout.
H3 supplies Messages and H2 supplies tool/result contracts; OpenAI and xAI inference additionally needs H4's Responses adapter.
This phase now supplies the shared locked credential store and auth precedence; H7 integrates them later.
Use compiled provider/model records and existing tool snapshots for initial inference.
Remote catalog, user model configuration, full builtin implementations and project settings are not login/inference prerequisites.
No TUI, leader or network gateway is required for this exit.
Login executes on the inference host and saves to that runtime's Ask home.
Remote users run the command on that host, with provider-native interaction or documented callback forwarding.
Local-client credential transfer remains outside this phase.
Use the [deep TDD implementation plan](../261006-0157-h7a-subscription-auth/plan.md) for the six execution steps and runtime proof.
Read the [accepted design](../261005-2139-provider-auth-design/plan.md), [H4 report](../reports/xia-261005-1408-h4-more-wire-apis-pi-port-analysis.md) and [subscription audit](../reports/researcher-261005-2139-h4-subscription-source-audit.md).

## Requirements and ownership

- One provider receives injected supported auth methods; one tagged credential/account is saved per provider.
- Successful login replaces that record; failed login or a save failure before commit preserves the prior record.
- Report a save failure after replacement as an uncertain commit; do not claim that the old record is still present.
- The user confirmed Pi semantics: one saved account per provider and local-only logout.
- Logout deletes the whole provider record, including issued-client metadata; it does not revoke remote tokens.
- Use `ask auth login --provider <id>` and `ask auth logout --provider <id>` as the Ask headless adaptation of Pi interactive operations.
- Explicit supported API-key overrides win; otherwise use the stored type, then ambient key resolution when no record exists.
- OAuth failure never falls back to API-key billing.
- Resolve after the final model is selected; bind credential material to provider, method, account and allowed endpoint.
- Anthropic uses its native browser flow and Claude Code request profile: bearer/header/system/beta shaping and request-local tool-name conversion.
- Name conversion covers declarations, historical calls, additions/removals and named tool choice; response names are restored before public events.
- OpenAI uses native ChatGPT sign-in, verified identity/account binding, issued-client reuse, Responses restrictions and supported account discovery.
- xAI uses device authorization, server polling intervals, slow-down/cancellation handling and safe refresh-token replacement.
- Unknown account access permits inference with valid auth/capabilities; known denial blocks selection.
- Refresh uses this phase's shared cross-process read/recheck/commit path; persist rotated credentials even if inference is canceled after rotation.
- Durably fence a rotating grant before exchange; an unresolved attempt blocks automatic reuse after error or restart.
- Signal shutdown waits through the bounded auth commit path; forced death remains protected by the persisted fence.
- Use bounded exchanges, validated token responses, secret redaction and durable atomic writes.

H-AUTH-01/02/03 precedence and secure persistence are pulled here from H7; that phase must reuse them.
H-AUTH-06 is owned here for Anthropic, ChatGPT and xAI; additional providers remain H17.
H-AUTH-07 shared mechanics for these methods and H-AUTH-11 identity shaping are pulled into this phase.
H-AUTH-10 is split: shared operations and minimal headless commands here, gateway transport in H17 and full TUI dialogs in T2.
H3's H-AUTH-08 per-request hook remains the foundation; this phase adds typed method/profile resolution without breaking key-only callers.

## Files and steps

Expected owners: `internal/auth`, `internal/settings`, provider core/wire adapters, `internal/app` and headless command handlers.
Follow actual package import rules; SDK types stay inside wire infrastructure boundaries.
Use the design plan's proposed boundaries; validate import enforcement before adding a package.

1. Specify typed auth snapshots, profile bindings and one-record store contracts against existing provider interfaces; keep refresh tokens outside inference options.
2. Implement secure file persistence, atomic replacement, cross-process locking and Pi precedence without depending on the full H6 settings loader.
3. Implement native Anthropic login/profile first using Messages, then connect OpenAI and xAI strategies as Responses becomes available.
   The three strategies can be developed independently after the shared contracts are fixed; this scheduling permission does not itself start parallel workers.
4. Implement adapter-local profiles and final body/header validation.
5. Connect headless login/logout and inference to one resolver; document command errors and callback constraints.
6. Validate replacement, concurrent refresh, discovery, request shaping and cross-method provenance.
7. Run authorized live acceptance per provider as it becomes ready; record evidence without secrets, and do not wait for the other two to deliver the first usable subscription prompt.

## Validation and exit

- Store checks cover two-process refresh, logout/replacement races, canceled lock waits, cancellation after rotation and failed save.
- Wire checks cover conflicting environment credentials, prohibited fields, tool-name round trips and forced tool choice.
- Provider checks cover ChatGPT identity/scopes/client reuse and allowance errors, plus all xAI polling states and missing replacement refresh tokens.
- Explicit key/subscription login replacement leaves exactly one saved record; failed login preserves the prior record.
- Failed selection neither changes the target nor submits the intended prompt through another credential.
- Canonical tool arguments and stored transcript names remain unchanged across wire profiles.
- Headless login/logout and a subscription prompt work for all three providers on the inference host.
- A usable live prompt for the first ready subscription is an early checkpoint, not completion of the three-provider scope.
- H9 later integrates retry classification; this phase already exposes distinguishable allowance errors without silently retrying a spent allowance.

Run focused checks, affected packages and required quality gates.
Live account acceptance proves endpoint behavior; public catalog access does not prove entitlement.

## Risk and rollback

Auth-server success and local persistence cannot be one atomic transaction.
Surface ambiguous rotation/save failures; request reauthentication where recovery is unsafe.
Respect registered callbacks on remote hosts; document supported interaction/forwarding instead of inventing redirect URIs.
Disable new method registrations to roll back; preserve credential records, unknown fields and key-only registrations.
Do not rewrite files into an older schema or delete unrelated provider records.
