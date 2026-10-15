---
title: "Phase 1: SDK conformance and wire contract"
status: completed
---

# Phase 1: SDK conformance and wire contract

## Outcome and requirements

Select a conformance-tested SDK/schema pair and define lossless Ask DTOs without replacing JSON-RPC.
Preserve the complete H13a scope and public Agent semantics.
Use real existing owners; never implement a second execution loop or advertise deferred capabilities.
No implementation is done during planning.

## File inventory

| Action | Absolute path | Change |
|---|---|---|
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/acp/conformance_test.go` | Real SDK pipe-peer tests; selected SDK limitations |
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/pkg/protocol/acp.go` | Typed Ask methods, errors and event metadata |
| CREATE | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/pkg/protocol/acp_test.go` | Typed Ask methods, errors and event metadata |
| MODIFY after gate | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/go.mod` | Only selected exact SDK pin through Go tooling |
| MODIFY after gate | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/go.sum` | Only selected exact SDK pin through Go tooling |
| MODIFY after gate | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/internal/acp/README.md` | Verified dependency and contract navigation |
| MODIFY after gate | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/pkg/protocol/README.md` | Verified dependency and contract navigation |
| MODIFY after gate | `/Users/dale/.paseo/worktrees/09kf6w9r/frail-panther/AGENTS.md` | Verified dependency and contract navigation |

No existing file is deleted unless constructor code moves within its existing owner.
Generated SDK files are inspected, not manually edited.
Only create a helper when its responsibility requires a real boundary.

## Wire contract to implement

The method names below are proposed new Ask contract names, not existing implementation claims.
ACP v1 standard methods are `initialize`, `authenticate`, `session/new`, `session/prompt`, `session/cancel`, and `session/update`.
`session/load` remains explicit unsupported and is not advertised.
Use the stable pinned schema's configuration-option method for model/thinking UI integration if it exists and passes Phase 1 tests.
Do not treat unstable legacy `session/set_model` or generated experimental interfaces as stable capability merely because the SDK contains them.
The required controls remain available through typed Ask methods regardless of editor configuration support.

| Proposed method | Request and result contract | Owner |
|---|---|---|
| `_ask/session/state` | sessionId → copied safe current execution/model/thinking/queue facts | Agent.State projection |
| `_ask/session/get_available_models` | sessionId → copied qualified provider/model/API IDs and declared capabilities | Existing catalog via app callback |
| `_ask/session/set_model` | sessionId, qualified modelId, optional supported authMethodId → accepted model or safe readiness error | Find + existing host auth choice + Agent.SetModel |
| `_ask/session/set_thinking` | sessionId, level → clamped current thinking level or busy/disposed error | Agent.SetThinkingLevel |
| `_ask/session/continue` | sessionId → final stop reason after executed settled/output barrier, or immediate no-event state error | Agent.Continue |
| `_ask/session/reset` | sessionId → same session ID, fresh epoch and safe state | Agent.Reset |
| `_ask/session/steer` | sessionId, supported content → inputId admission result | Agent.Steer |
| `_ask/session/follow_up` | sessionId, supported content → inputId admission result | Agent.FollowUp |
| `_ask/session/remove` | sessionId, inputId → removed bool | Agent.Remove |
| `_ask/session/follow` | sessionId, optional cursor(epoch,seq) → subscriptionId, consistent cut, snapshot if needed, stream baseline, resumed/resync | Agent.Follow |
| `_ask/session/unfollow` | sessionId, subscriptionId → idempotent detached result | Owned follower.Close |
| `_ask/session/usage` | sessionId → safe exact attempt rows and cumulative known usage, with provenance/completeness | Existing committed AttemptEnd/Follow projection |
| `_ask/session/event` | outbound notification with subscriptionId, sessionId, epoch, sequence, run/turn/cycle/attempt identity and existing event body | Existing protocol event envelope |
| `_ask/session/resync` | outbound notification with subscriptionId, cursor and explicit reason; client requests fresh follow | bus.ErrResync |
| `_ask/session/compact`, `fork`, `tree` | typed request → unsupported owner-not-available error | H10/H14 deferred |

<!-- Accepted red-team correction: configured auth method constraint. -->
Optional `authMethodId` constrains the effective existing host credential selection; it does not select, create, or replace credentials.
The app callback compares the requested method with the effective method under existing override/saved/environment precedence.
Reject unsupported or differing methods before SetModel; preserve model, thinking, credentials, and zero inference calls on rejection.
Omitting the field retains existing host selection.
Apply the same configured-method validation to standard authenticate.

Specify DTO fields, JSON names, optional/null semantics and precision encoding in `pkg/protocol/acp.go` before implementation.
The shared wire contract contains no internal SDK, Agent, or credential-bearing AuthSnapshot types.
`StreamBaseline` DTO preserves AttemptID, baseline sequence, partial message, raw open block bytes and block indexes/IDs needed by Builder.ResumeFrom.
Snapshot entries reuse safe existing serialization; private credentials/request-auth context never enter them.
Map unknown session, uninitialized connection, ErrBusy, ErrDisposed, ErrNoAPIKey, ErrQueueFull, invalid model/content, request cancellation, unsupported method and output failure once.
Use standard JSON-RPC invalid params/method-not-found and safe extension error data as permitted by the pinned schema.
Do not put provider error bodies or private credential identity into error data.
A lost stdout write is connection failure, not a successful JSON-RPC response.

## Test baseline and caller protection

Counts are source declarations from the phase scout, not passing test results.
ACP has 0 existing tests; all ACP conformance, adapter, sender, and stdio checks below are missing.
Protocol has 114 tests; Agent 345; bus 13; app 13; auth 25; providers 94; sessions 11; CLI 81.
Read owning package tests before changes and reuse their real fixtures.

| Existing call surface | Production / test lexical sites | Protection |
|---|---:|---|
| Prompt / Continue / Abort | 1/271; 0/8; 1/58 | Preserve busy, state, cancellation and settled behavior |
| WaitForIdle / Dispose / Reset | 0/9; 1/52; 4/16 | Never dispose from sync listeners; preserve writer/epoch lifecycle |
| SetModel / SetThinkingLevel | 0/17; 0/4 | Preserve readiness atomicity, clamp and idle boundary |
| Steer / FollowUp / Remove / Follow | 0/9; 0/9; 2/12; 1/26 | Preserve queue claim, input IDs and consistent snapshot cut |
| NewNativeAuth / BindAuth / AuthWait | 2/0; 1/8; 2/0 | Preserve private auth transport and refresh drain |
| newHeadlessAgentWithAuth / runWithDependencies | 2/0; 1/2 | Move shared construction without changing direct headless behavior |
| parseArgs / runAuth / openProvider | 1/1; 1/0; 1/0 | Dispatch ACP explicitly; no raw auth stdin on protocol connection |

These counts are lexical navigation bounds, not type-resolved call graphs.
Before edits, read every typed caller in `cmd/tui`, `internal/app`, `internal/agent`, and their tests.
Do not alter Agent or bus public APIs unless a failing executable check proves the missing contract.

## Function protection checklist

- [ ] Resolve typed callers and existing fixtures before changing a shared owner.
- [ ] Protect Prompt busy and completion semantics, Abort versus Dispose, and unbounded started-tool drain.
- [ ] Protect queue ID/claim behavior, Follow snapshot/open-stream baseline, and epoch identity.
- [ ] Keep native auth/provider callbacks in app and preserve direct headless behavior.
- [ ] Check cancellation and output errors at their actual physical transport seam.

## Dependency map

Temporary consumer → immutable SDK/schema evidence → real pipe connections → DTO round trips → targeted mitigation proof → production dependency selection.
Start coder `github.com/coder/acp-go-sdk` v0.13.5; compare lx-wnk v1.21.0 and Caelis v1.4.0 only with resolved immutable sources and the same fixture.
An unavailable candidate stays unverified, not failed or passed.

## Scenario matrix

| Scenario | Required assertion |
|---|---|
| Version/schema | wire v1 independent of SDK version; exact stable schema SHA-256 and inputs recorded |
| _meta precision | nested/null/array/unknown fields and integer >2^53 round-trip without numeric loss |
| _ask dispatch | requests, responses, notifications, errors and escaped JSON slash names reach real handler |
| Concurrent request/reverse traffic | hold Prompt while reverse reply and session/cancel reach connection |
| ID cancellation | numeric/string/unknown request IDs and cancellation races documented |
| Inbound pressure | blocked notification callback does not hide control indefinitely; bounded cap/failure |
| Outbound fault | zero-byte/short/error/block writer yields connection failure, no successful lost response |
| Ingress fault | EOF, partial frame, oversized first line, malformed JSON and duplicate IDs handled safely |
| Stable usage/config | DTO fixture validates exact pinned stable schema, unsupported methods explicit |

## Tests Before (RED)

Resolve tag commits, module sum, schema tag/commit/asset digest, generator stable/unstable inputs, and license first.
Run minimal real SDK Agent/client implementations in an external temporary Go module with `GOWORK=off`.
Write failing Ask metadata precision, actual output-failure, control-pressure and first-ingress byte-cap checks before production adapters.
An initially correct SDK baseline remains PASS; infrastructure/DNS failure is not RED.
Run narrow checks first and save the precise failing assertion and command.
A timeout from a broken fixture or inaccessible dependency is not behavioral RED.

## Refactor (GREEN)

Use SDK transport, extension/raw JSON hooks, and private DTO mappers first.
If coder ignores write counts/errors, add the smallest checked io.Writer/error latch at the actual output boundary and signal connection shutdown.
If SDK wraps away relevant failures, require a tested minimal SDK patch rather than claiming the wrapper fixed discarded errors.
If its Scanner cap is unsuitable, enforce a byte cap before any unbounded read/allocation; test split-byte framing.
Represent precision-sensitive Ask seq counters as documented decimal strings or raw JSON when typed metadata cannot preserve them.
Do not implement a second protocol engine or unstable interface just because generated types exist.
Close D17 only after every required check passes or has a small tested owned mitigation.
Reuse standard library and selected SDK facilities before introducing abstractions.
One synchronous Agent listener must never block on wire output or disposal.

## Tests After

Save frame traces, pins/digests, commands, exit codes and selected patch provenance in a conformance report.
Run race and goroutine-leak checks with EOF and blocked peers.
Re-run the same fixtures against the selected production pin.
Conformance is limited to required Ask contracts, not blanket ACP certification.
Every test has an owner, cancellation context, and bounded fixture cleanup.
Ordinary tool drain remains contractually unbounded; the test waits for its deliberate real release rather than inventing a product timeout.

## Regression commands

Commands name proposed tests and files; they do not claim those tests exist today.
Run from the primary worktree with a task-owned cache if the environment needs one.

```sh
go test ./internal/acp ./pkg/protocol -run 'TestACPConformance|TestACPWire' -count=1
go test -race ./internal/acp ./pkg/protocol -count=1
```

## Success criteria

- [x] All listed scenarios have runnable tests and saved results (conformance-261007-h13a-sdk.md).
- [x] New behavior has valid RED-to-GREEN evidence; initially correct controls retain their PASS result.
- [x] Existing Agent/headless/auth public contracts remain intact (no Agent, bus or app file changed).
- [x] Planned external behavior is wired through the real built ask acp boundary in final acceptance; see the [2026-10-08 independent review](../reports/code-review-261008-h13a-plan-compliance.md).
- [x] No silent lifecycle loss, precision loss, secret exposure, or capability overclaim remains (limits listed in the report).
- [x] Narrow tests, applicable race/leak checks, regression gates, and owned-process cleanup pass.

## Risk signal and response

Signal: metadata rounding, response after lost bytes, control starvation, or unknown schema identity.
Response: keep D17 open; test a minimal mitigation or another verified candidate; do not add production pin until evidence passes.

## Rollback

Cancel and join only phase-owned children/readers/SDK connections.
Restore only this phase's reviewed source/dependency diff after checking for user or other agent edits.
Keep failing frame traces and immutable schema/source identities for the next attempt.
Do not weaken existing checks, replay an ambiguously admitted prompt, or modify generated files to conceal a failure.

## Result

D17 closed for coder v0.13.5 on 2026-10-07. Evidence: [conformance report](../reports/conformance-261007-h13a-sdk.md). Guards live in `internal/acp/wire.go`. The built `ask acp` boundary criterion stays open for Phase 4.
