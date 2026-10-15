---
title: "H13a ACP v1 over stdio"
description: "Expose the current Agent API through a session-owned ACP adapter and built ask acp."
status: completed
priority: P1
effort: ""
tags: [backend, api, auth, tdd]
created: 2026-10-07
---

# H13a ACP v1 over stdio

## Outcome and boundaries

HOLD SCOPE: full H13a, `--deep --tdd`, without `--yagni`.
A built `ask acp` serves ACP v1 over stdio, with one host and one independent Agent per session.
Expose current lifecycle, prompts, continuation, cancellation, model/thinking controls, state, queues, exact attempt usage, and Follow/resync.
Reuse H7a native auth, existing Agent execution, and the in-memory session writer.
No leader/socket, gateway, database, durable load, compaction, session tree, or second Agent loop is implemented here.
Unsupported owner methods return explicit unsupported errors and are not advertised as capabilities.
Root composition must start no TCP/Unix listener; outbound inference and native auth HTTP are permitted.
The planning stage changed no dependency or source.
Implementation and the independent review now have separate execution evidence.

## Decisions and dependencies

The user confirmed ACP v1 and one host with one Agent per session.
D17 closed after executable Phase 1 SDK/schema conformance passed; coder v0.13.5 is the selected dependency.
ACP wire v1, schema tag/commit/SHA-256, and SDK module version/sum are separate identities.
Authentication is decided: use configured credentials and existing host-side `ask auth` login.
Standard authenticate validates configured method/readiness and returns safe CLI guidance; it never starts interactive login or reads raw stdin.
H4/H7a/lifecycle implementations are reused; the parent roadmap is scope authority, not an unimplemented prerequisite.
Phase 1 → 2 → 3 → 4; shared file edits are sequential.
H13b consumes this host later, with client detach separate from shared Agent disposal.

Read [architecture 7.3](../../docs/ask-architecture-reference.md), [ACP owner](../../internal/acp/README.md), [roadmap H13](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#h13-leader-acp-adapter-and-gateway-pis-rpc-mode-multi-client), [Grok port](../reports/xia-261007-1328-grok-acp-h13a-port.md), [source evidence](../reports/researcher-261007-1328-grok-acp-source.md), [SDK research](../reports/researcher-261007-1358-h13a-sdk.md), and [phase scout](../reports/scout-261007-1358-h13a-phases.md).
The table below is file-owned fallback because CLI add-phase created four files but left a one-row index; no live table-sync command exists.

## Phases

| # | Phase | Status |
|---|---|---|
| 1 | [SDK conformance and wire contract](phase-01-start.md) | Completed |
| 2 | [Independent session adapter](phase-02-session-adapter.md) | Completed |
| 3 | [Ordered events and controls](phase-03-ordered-events-and-control.md) | Completed |
| 4 | [Stdio, native auth, and full E2E](phase-04-stdio-auth-and-e2e.md) | Completed |

## Runtime Flow Proof

These statuses assess planned flow completeness, not executed product results.
Every public operation has planned production `ask acp` subprocess coverage.
The user approved supplemental tests through the same real stdio composition for controlled started-tool drain and faulting-log no-start failure.
Those fixtures use real Ask owners and injected tool/writer implementations; they are not production-binary certification of those faults.
External mocks are restricted to provider HTTP/OAuth services; all Ask owners and stdin/stdout transport remain real.
Prepared data uses real temporary homes/cwds and native credentials produced through supported auth flows, not MockAgent or test-only CLI flags.

| Feature | Actor | Runtime trigger | Entry point | Internal path | Observable result | End-to-end test | External mocks | Prepared data | Status |
|---|---|---|---|---|---|---|---|---|---|
| Initialize/framing | ACP editor | initialize/raw bytes | ask acp stdin | SDK + checked writer/ingress | v1/capabilities/errors | TestACPE2EInitialize | None | NDJSON peer | PASSED |
| New sessions | ACP editor | session/new twice | ask acp | host factory → app → Agent.New | distinct session/cwd/log | TestACPE2ESessionIsolation | Provider HTTP | two cwd/home paths | PASSED |
| Prompt/busy/repeat | ACP editor | session/prompt | ask acp | Follow registration → Agent.Prompt → sender barrier | settled result after updates; busy error | TestACPE2EPrompt | Provider HTTP | held stream then second turn | PASSED |
| Continue | ACP editor | _ask/session/continue | ask acp | Agent.Continue → call result or settled/write barrier | continuation or immediate no-event state error; next prompt unaffected | TestACPE2EContinue | Provider HTTP | empty, assistant-tail and valid real session logs | PASSED |
| Inference cancel | ACP editor | session/cancel | built ask acp | Agent.Abort → settled → barrier | cancelled prompt after pending updates | TestACPE2ECancel | Provider HTTP | held inference | PASSED |
| Started-tool drain | ACP peer | session/cancel after body start | production stdio composition on real pipes | SDK → host → real Agent/executor → held tool → settled → barrier | no result until tool release and completed writes | TestACPStdioToolDrain | Provider HTTP | registered controlled tool; approved supplemental fixture | PASSED |
| No-start execution failure | ACP peer | session/prompt with log append fault | production stdio composition on real pipes | SDK → real Agent/faulting writer → failure envelopes → sender | mapped error after failure writes; no start wait or sibling binding | TestACPStdioNoStartFailure | Provider HTTP | real writer implementation with append fault | PASSED |
| Request cancel | ACP editor | $/cancel_request | ask acp | SDK request IDs + adapter context | documented request/run distinction | TestACPE2ERequestCancel | Provider HTTP | string/numeric IDs | PASSED |
| State | ACP editor | _ask/session/state | ask acp | Agent.State projection | copied current state | TestACPE2EState | Provider HTTP | running and idle session | PASSED |
| Reset | ACP editor | _ask/session/reset | ask acp | Agent.Reset → fence → replace mandatory observer | same session, old cursor resync, immediate next Prompt completes | TestACPE2EReset | Provider HTTP | settled session then old cursor | PASSED |
| Catalog | ACP editor | _ask/session/get_available_models | ask acp | app callback → existing catalog | qualified reversible IDs/current model | TestACPE2ECatalog | None | initial faux plus real rows | PASSED |
| Model switch | ACP editor | _ask/session/set_model | ask acp | app callback → providers.Find → Agent.SetModel | atomic ready switch; auth mismatch rejects without inference or mutation | TestACPE2EModels | Provider HTTP | ambiguous/ready/unready model | PASSED |
| Thinking | ACP editor | _ask/session/set_thinking | ask acp | Agent.SetThinkingLevel | clamp, busy/disposed errors | TestACPE2EThinking | Provider HTTP | capability-bound model | PASSED |
| Steer | ACP editor | _ask/session/steer | ask acp | real inbox steering claim → queue events | admission ID distinct from prompt result | TestACPE2ESteer | Provider HTTP | held inference plus steering text | PASSED |
| Follow-up | ACP editor | _ask/session/follow_up | ask acp | real inbox next-cycle wake → queue events | admission ID and retained queue policy | TestACPE2EFollowUp | Provider HTTP | busy and idle session | PASSED |
| Remove | ACP editor | _ask/session/remove | ask acp | Agent.Remove | true only before claim | TestACPE2ERemove | Provider HTTP | captured pending input ID | PASSED |
| Follow/resync | ACP editor | _ask/session/follow | ask acp | Agent.Follow → bus.Follower | cut/baseline then ordered events, resync | TestACPE2EFollow | Provider HTTP | open block and stale cursor | PASSED |
| Unfollow | ACP editor | _ask/session/unfollow | ask acp | owned follower detach | subscription stops; prompt barrier unaffected | TestACPE2EUnfollow | Provider HTTP | active prompt and subscription | PASSED |
| Retry/usage/errors | ACP editor | prompt, _ask/session/usage | built ask acp with external HTTPS provider fixture (`TestACPE2ERetryUsageProvider`) | existing retry/AttemptEnd projection | safe retry facts, exact attempt usage | TestACPE2ERetryUsage | Provider HTTP | retryable then terminal response | PASSED |
| Deferred methods | ACP editor | load/compact/fork/tree | ask acp | capability/error mapper | explicit unsupported, no fake results | TestACPE2EUnsupported | None | unsupported requests | PASSED |
| H7a auth/readiness | ACP editor/operator | authenticate + prompt/model | built ask acp / ask auth with external HTTPS OAuth fixture (`TestACPE2EAuthRefresh`) | NewNativeAuth/BindAuth callbacks | safe readiness or login guidance | TestACPE2EAuth | OAuth + inference HTTP | expired native credential | PASSED |
| EOF/signals/output failure | ACP editor/OS | close pipe or signal | ask acp | close admission → owned host cleanup | no success after lost output; drained exit | TestACPE2EShutdown | Provider HTTP | held stream/output pipe | PASSED |

- **Gate status:** PASSED. Every row ran on 2026-10-07; see the execution evidence in [phase 4](phase-04-stdio-auth-and-e2e.md#execution-evidence-2026-10-07).
The initial provider/OAuth rows used a re-executed test binary and did not satisfy built-binary acceptance.
The independent review replaced those fixtures with a CONNECT proxy and temporary CA, so both rows now use the production binary and its production HTTP clients.
See [independent review](../reports/code-review-261008-h13a-plan-compliance.md) for the current verification evidence.

## Acceptance

- [x] Phase 1 records immutable SDK and stable schema identities and every required conformance result before closing D17.
- [x] All public wire operations use real existing owners through built-process stdio E2E.
- [x] Executed prompt responses follow settled execution and actual completed outbound writes; no-event admission/state errors return directly without waiting for start or settled.
- [x] Follow preserves session/run/epoch/seq, open-stream baseline and incomplete tool arguments; no automatic prompt resend.
- [x] Auth uses shared native composition, no raw token DTOs, private stdin competition, billed fallback, or secret capture.
- [x] EOF/fault/signal cleanup owns host disposal and AuthWait; ordinary cancellation preserves unbounded started-tool drain.
- [x] Focused tests, race/leak checks, lint, build, and existing headless regressions pass.

## Validation and open question

The implementation and all tests ran on 2026-10-07; results are in the phase files and the [conformance report](../reports/conformance-261007-h13a-sdk.md).
No effort estimate is invented.
The four accepted findings are applied and tested.
The three scope decisions are recorded in Phase 4; no authentication scope question remains.

## Red Team Review

Three review lenses checked 40 source claims each.
The security lens was performed by the draft author and is not an independent review.
Two external reviewers covered assumptions/contracts and failure modes.
No product tests or SDK conformance checks ran.

| Finding | Severity | Proposed disposition | Required correction |
|---|---|---|---|
| No-start completion | High | Accepted and applied | Handle returned Agent errors and failure envelopes without waiting for agent_start |
| Reset observer replacement | Medium | Accepted and applied | Establish a new-epoch mandatory observer before returning Reset or admitting another prompt |
| Production blocking-tool fixture | High | Accepted and applied | Use a disclosed supplemental stdio composition test with a controlled real tool; retain production binary cancellation E2E |
| Requested auth method mismatch | Medium | Accepted and applied | Treat authMethodId as a configured-method constraint; reject mismatch without mutation or inference |

Evidence: [assumptions and contract review](../reports/red-team-261007-1358-h13a-assumptions.md), [failure review](../reports/red-team-261007-1358-h13a-failures.md), and [security review](../reports/red-team-261007-1358-h13a-security.md).
The duplicate Reset finding was merged.
The user approved all four corrections on 2026-10-07.
Inline corrections are marked in the affected phases.

### Whole-Plan Consistency Sweep

All five plan files were reread after edits.
Four decision deltas were checked across contracts, implementation steps, tests, acceptance, and the runtime matrix.
The production binary and supplemental composition proof boundaries are explicit.
No unresolved contradiction or user scope question remains.
CLI format validation is separate from runtime conformance; no implementation test result is claimed.
Runtime task tools are unavailable; the four unchecked phase records and CLI file index remain the progress source.

<!-- slug: h13a-acp-stdio -->
