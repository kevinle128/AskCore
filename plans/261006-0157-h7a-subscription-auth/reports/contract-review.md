# H7a contract review

Review date: 2026-10-06.
Lens: Scope and Complexity Critic, Full tier Contract Verifier.
Scope: the main plan, all six phase files, cited H7a requirements, accepted design, architecture, and two scouts.
The requested three-provider scope, verified ChatGPT identity, issued-client reuse, one saved account per provider, and local-only logout are fixed constraints.
The review uses source reads and `rg` only.
No Go command, test, build, lint, login, live request, or credential read was performed.
Only this report was changed.

## Summary and verdict

The plan keeps the required scope and uses the existing wire, stream, hook, and transcript owners.
The separate persisted credential and request snapshot types have different data and meet the architecture rule at `docs/ask-architecture-reference.md:151`.
No supported scope cut or unnecessary new framework was found.
One source inventory claim needs correction before implementation handoff.
Verdict: Comment, with the inventory correction below.
The 131 matrix rows at `plan.md:139` through `plan.md:269` are planned design checks, as stated at `plan.md:125` and `plan.md:271`.
They are not executed acceptance evidence.

## Finding

### Medium: the claimed complete consumer inventory does not exist in the cited scout

Location: `phase-01-start.md:76`, repeated at `phase-02-auth-resolution-and-request-binding.md:77` and narrowed to constructor counts at `phase-04-chatgpt-identity-and-responses.md:91`.
Evidence: `plans/reports/researcher-261006-0157-h7a-local-runtime-scout.md:78` explicitly limits its inventory to production function call sites, and its table starts at line 82.
That table does not list StreamOptions literals, option copies, faux consumers, body builders, or test constructor callers.
Evidence: `internal/providers/faux/stream.go:62` copies options, line 72 records them, and line 240 copies them into the script call.
Evidence: `internal/providers/faux/faux.go:247` copies recorded options, and line 252 owns the copy function.
Evidence: `internal/providers/openai/completions_body.go:14`, `responses_prompt.go:17`, and `internal/providers/anthropic/document.go:18` consume options directly.
Evidence: `internal/agent/agent_model_test.go:112`, line 180, and line 275 call NewResponses, but the scout lists only its one production caller at line 105.
Failure scenario: an implementer uses the promised complete inventory to add an optional auth or named-choice value, misses a copy boundary or adapter consumer, and tests only the listed producer path.
A reference-bearing new value can then share mutable state through faux records, or a shared option can disappear in another wire path without a targeted check.
This is an inventory risk, not a claim that the proposed new fields already have those bugs.
Fix: replace the complete-inventory claim with the scout's actual production-call scope, then attach the direct consumer and literal inventory below to the phase handoff.
For each new field, state value-copy rules and supported-wire behavior, and add a focused real-call check at the affected owner.
Do not create another inventory framework.

## Changed-contract consumer inventory

All paths below are relative to the work context.
Counts describe the source snapshot read during this review, including H4 working-tree files.
New symbols remain planned symbols and their absence is not a defect.

### StreamOptions and the optional auth field

The existing public type is at `internal/providers/types.go:33`, StreamFn at line 48, and Provider.Stream at line 55.
Existing direct production consumers are listed below.

| Owner | All direct consumers and copy boundaries |
| --- | --- |
| Agent configuration | `internal/agent/loop_run.go:27` holds options, `types.go:33` embeds LoopConfig, and `agent.go:139`, line 156, line 168, line 193, line 231, and line 232 read or change option fields. |
| Agent request | `internal/agent/loop_stream.go:24` copies options, line 29 sets APIKey, line 35 dispatches, and line 98 supplies legacy fallback; `loop_run.go:189`, line 191, line 220, and line 223 change or read reasoning. |
| CLI | `cmd/tui/headless.go:177` creates options, and `headless_faux.go:64` forwards them. |
| Registry | `internal/providers/registry.go:37` receives and forwards options. |
| Messages | `internal/providers/anthropic/provider.go:120` and line 162 receive options; `document.go:18` and line 94 build the body and clamp output. |
| Completions | `internal/providers/openai/completions.go:36` and line 81 receive options; `completions_body.go:14` and line 89 build the body and clamp output. |
| Responses | `internal/providers/openai/responses.go:28` and line 73 receive options; `responses_prompt.go:17` builds the call. |
| Faux | `internal/providers/faux/script.go:20` exposes call options; `faux.go:57` stores record options, line 247 copies them, and line 252 owns cloning; `stream.go:57`, line 62, line 72, line 80, line 102, and line 240 receive, copy, record, inspect, or expose them. |

Actual StreamOptions composite literals total 73 across 12 files in this read snapshot.
The count excludes function return-type braces and the unrelated SupportsStreamOptions condition.

| File | Count | Every literal line |
| --- | ---: | --- |
| `cmd/tui/headless.go` | 1 | 177. |
| `internal/agent/agent_model_test.go` | 5 | 63, 115, 190, 300, 374. |
| `internal/agent/loop_run_test.go` | 1 | 406. |
| `internal/providers/registry_test.go` | 2 | 17, 36. |
| `internal/providers/anthropic/provider_test.go` | 7 | 139, 175, 278, 312, 352, 382, 428. |
| `internal/providers/openai/completions_test.go` | 17 | 27, 50, 70, 101, 122, 150, 171, 201, 235, 275, 303, 318, 336, 355, 366, 377, 397. |
| `internal/providers/openai/responses_test.go` | 18 | 25, 66, 97, 134, 191, 212, 252, 288, 323, 373, 405, 422, 436, 448, 462, 478, 489, 509. |
| `internal/providers/openai/isolate_test.go` | 2 | 47, 56. |
| `internal/providers/openai/completions_replay_matrix_test.go` | 1 | 24. |
| `internal/providers/openai/responses_replay_matrix_test.go` | 1 | 20. |
| `internal/providers/faux/faux_test.go` | 6 | 143, 146, 175, 233, 689, 772. |
| `internal/providers/faux/usage_test.go` | 12 | 69, 71, 78, 85, 103, 104, 112, 114, 117, 121, 131, 149. |

Nonliteral test signatures also consume StreamOptions at `internal/agent/agent_test.go:263`, `internal/providers/registry_test.go:30`, and `internal/providers/faux/helpers_test.go:108`, line 110, line 135, and `usage_test.go:53`.
The planned auth field must follow the actual direct consumer and copy boundaries above.
No existing optional auth field was found at `internal/providers/types.go:33`.

### Optional readiness and constructors

Readiness is proposed at `phase-02-auth-resolution-and-request-binding.md:28`; it is not an existing symbol.
Its planned publisher is the ordinary app constructor in `internal/app/module_auth.go`, and its planned consumer is Agent.SetModel.
The existing fallback consumer is `internal/agent/agent.go:142`, with idle checks at line 133 and line 152.
Current SetModel production callers total zero.
Its seven test call sites are `internal/agent/agent_test.go:147` and `agent_model_test.go:81`, line 126, line 229, line 381, line 408, and line 411.

| Existing constructor or setup | Production callers | Test callers |
| --- | --- | --- |
| `agent.New` | 1 at `cmd/tui/headless.go:172`. | 7 at `internal/agent/agent_test.go:39`, line 462, and `agent_model_test.go:58`, line 113, line 188, line 369, line 393. |
| `newHeadlessAgent` | 1 at `cmd/tui/headless.go:76`. | 10 at `headless_test.go:301`, line 325, `headless_live_test.go:80`, `headless_record_test.go:43`, line 99, line 178, `headless_fault_test.go:29`, `headless_cassette_test.go:113`, line 146, line 160. |
| `openProvider` | 1 at `cmd/tui/headless.go:138`. | 0. |
| `registerOpenAIWires` | 1 at `cmd/tui/headless.go:165`. | 0 direct calls. |
| `openai.NewResponses` | 1 at `cmd/tui/headless.go:198`. | 6 at `internal/agent/agent_model_test.go:112`, line 180, line 275, `internal/providers/openai/helpers_test.go:117`, `isolate_test.go:36`, `responses_test.go:130`. |
| `openai.NewCompletions` | 1 at `cmd/tui/headless.go:197`. | 3 at `internal/agent/agent_model_test.go:53`, `internal/providers/openai/helpers_test.go:103`, `isolate_test.go:26`. |
| Anthropic adapter New | 1 at `cmd/tui/headless_faux.go:94`. | 6 at `internal/agent/agent_model_test.go:49`, line 111, line 179, line 274, and `internal/providers/anthropic/provider_test.go:129`, line 460. |

Current Config/LoopConfig literal sites are `internal/agent/agent_test.go:30`, line 31, line 462, `agent_model_test.go:58`, line 60, line 113, line 188, line 298, line 369, line 370, line 393, line 394, `loop_helpers_test.go:129`, `loop_run_test.go:405`, `loop_stage_test.go:76`, line 164, line 329, and `cmd/tui/headless.go:172`, line 173.
The shared auth constructor, method constructors, discovery constructor, and focused fx graph are proposed additions in phases 1–4.
Their future callers are the auth command handler, newHeadlessAgent, connected command/request tests, and focused app composition tests.
Later leader/TUI/gateway consumers are explicitly outside H7a.

### Existing hooks

GetAPIKey has two execution consumers: `internal/agent/agent.go:138` and `loop_stream.go:98`.
Both reach ResolveKey at `internal/providers/keys.go:21`, whose direct production caller count is two.
PrepareRequest has one execution consumer at `internal/agent/loop_run.go:167`, triggered by `loop_stage.go:114`.
The project-context hook producer is `internal/agent/context_source.go:26`.
The sole production hook composition caller is `internal/agent/agent.go:238`.
Composition reads both hooks at `internal/pipeline/hooks_compose.go:52` and line 55, assigns or chains them at line 81, line 83, line 86, and line 88, and preserves key and prepare semantics at line 134 and line 149.
Direct hook test consumers are `internal/pipeline/hooks_compose_test.go:41`, line 42, line 53, line 54, line 102, line 105, line 108, line 113, line 118, line 119, line 128, line 136, line 161, and line 162; `internal/agent/agent_test.go:233`, line 243, line 253, line 323, and line 359; `agent_model_test.go:65`, line 116, line 191, line 282, and line 291; `loop_stream_test.go:111`; and `loop_run_test.go:70`, line 330, line 331, line 373, and line 408.
The plan adds no auth hook to Compose, so no additional hook-chain contract is required.

### Named tool choice

Named choice has no existing public field or consumer in `internal/providers/types.go:33`.
Its planned public field and adapter checks are stated at `phase-03-anthropic-and-headless-login.md:61`.
The existing option transport is LoopConfig → loop_stream → Registry.Stream → selected adapter Stream, as listed above.
The planned Messages encoder/codec owners are `internal/providers/anthropic/document.go:18` and the proposed `tool_names.go`.
The existing sibling body consumers are `internal/providers/openai/completions_body.go:14`, `responses_prompt.go:17`, and faux Call/Record consumers.
The implementation must define encode or reject behavior for a nonempty named choice at those sibling boundaries rather than silently ignore it.
The current response-name publication consumer is `internal/providers/fantasykit/fold.go:153`.
The plan correctly maps names before that boundary at `phase-03-anthropic-and-headless-login.md:128`.
This is a planned contract obligation, not a defect based on an absent new symbol.

## Full-tier contract sample

VERIFIED means a current source fact or accepted requirement is supported by the cited lines.
FAILED means the cited source does not support the asserted source fact.
UNVERIFIED means implementation or provider acceptance evidence is still required.
Each phase has exactly four sampled claims, for 24 total.

| Phase | Claim and plan location | Result | Evidence and actual observation |
| --- | --- | --- | --- |
| 1 | Settings uses stdlib only at `phase-01-start.md:27`. | VERIFIED | `internal/settings/README.md:30` permits stdlib and line 31 denies all other internal packages. |
| 1 | StreamFn and Provider.Stream remain unchanged at `phase-01-start.md:73`. | VERIFIED | `internal/providers/types.go:48` and line 55 show the two existing signatures. |
| 1 | Complete StreamOptions literal/caller inventory is in the scout at `phase-01-start.md:76`. | FAILED | Scout line 78 limits its inventory to production function calls; this review finds 73 actual literals in 12 files plus the direct copy/body consumers above. |
| 1 | Persisted and runtime material need different types at `phase-01-start.md:44`. | VERIFIED | `docs/ask-architecture-reference.md:151` allows different data types, and accepted design `plans/261005-2139-provider-auth-design/plan.md:118` states the runtime snapshot has only inference material. |
| 2 | ResolveKey has two production callers at `phase-02-auth-resolution-and-request-binding.md:69`. | VERIFIED | `internal/agent/agent.go:142` and `loop_stream.go:98` are both callers. |
| 2 | SetModel needs its second idle check at `phase-02-auth-resolution-and-request-binding.md:76`. | VERIFIED | `internal/agent/agent.go:133` and line 152 show two checks around resolution. |
| 2 | Empty/nonempty/error key hooks keep their behavior at `phase-02-auth-resolution-and-request-binding.md:35`. | VERIFIED | `internal/providers/keys.go:25`, line 27, line 30, and line 37 implement hook error, nonempty override, and fallback. |
| 2 | Lead times are ten minutes and eight minutes at `phase-02-auth-resolution-and-request-binding.md:37`. | VERIFIED | `plans/reports/researcher-261006-0157-h7a-pi-runtime-proof.md:109` records those exact source-derived margins. |
| 3 | Dispatch must precede parse, prompt reads, and capture at `phase-03-anthropic-and-headless-login.md:32`. | VERIFIED | Current `cmd/tui/headless.go:48`, line 72, line 76, and line 81 identify the real boundaries that the new branch must precede. |
| 3 | Existing CLI rejects OpenAI at `phase-03-anthropic-and-headless-login.md:94`. | VERIFIED | `cmd/tui/headless_test.go:64` tests that rejection, and `headless_faux.go:77` implements it. |
| 3 | Response names must be canonical before Fold at `phase-03-anthropic-and-headless-login.md:58`. | VERIFIED | `internal/providers/fantasykit/fold.go:153` passes ToolCallName directly to Assembler.ToolStart. |
| 3 | A supported bearer seam will deliver native Anthropic inference at `phase-03-anthropic-and-headless-login.md:54`. | UNVERIFIED | `plans/reports/xia-261006-0143-h7a-subscription-auth-architecture.md:257` proves header/client injection, but line 599 still requires provider acceptance; no implemented profile or live proof is claimed. |
| 4 | Verified identity and returning-client reuse remain required at `phase-04-chatgpt-identity-and-responses.md:32` and line 38. | VERIFIED | Accepted design `plans/261005-2139-provider-auth-design/plan.md:477` requires reuse; architecture report line 155 requires verification and line 158 rejects changed binding. |
| 4 | The scout records exact NewResponses caller counts, including listed test owners, at `phase-04-chatgpt-identity-and-responses.md:91`. | FAILED | Scout line 105 records one production caller only; current source has one production plus six test callers, enumerated above. |
| 4 | Current Isolate can replace Authorization at `phase-04-chatgpt-identity-and-responses.md:94`. | VERIFIED | `internal/providers/openai/isolate.go:60` assigns extra headers after SDK headers, including an extra Authorization value. |
| 4 | Current subscription endpoint accepts the final grouped-tool form at `phase-04-chatgpt-identity-and-responses.md:53`. | UNVERIFIED | `internal/providers/openai/responses_prompt.go:71` encodes current tools; architecture report line 188 explicitly requires a compatibility check; the plan keeps that acceptance gate. |
| 5 | Device/token endpoints match the pinned flow at `phase-05-xai-device-authorization.md:27`. | VERIFIED | Pi proof line 92 identifies the device URL and line 95 identifies the token URL. |
| 5 | Polling waits before the first request at `phase-05-xai-device-authorization.md:31`. | VERIFIED | Pi proof line 96 records that source behavior and line 97 records the shared wait policy. |
| 5 | Missing xAI replacement refresh token retains the prior token at `phase-05-xai-device-authorization.md:36`. | VERIFIED | Pi proof line 101 records this xAI rule; line 86 records ChatGPT's required replacement instead. |
| 5 | Encrypted reasoning is conditional at `phase-05-xai-device-authorization.md:40`. | VERIFIED | `internal/providers/openai/responses_prompt.go:32` gates encrypted reasoning on model.Reasoning and line 33 sets its include value. |
| 6 | Existing ordinary construction needs a focused app path at `phase-06-cumulative-acceptance-and-documentation.md:26`. | VERIFIED | `internal/app/app.go:28` provides the DB, line 36 provides HTTP, line 38 provides gRPC, and line 44 invokes migrations/servers; `cmd/tui/headless.go:172` is the current ordinary agent constructor. |
| 6 | Public settlement keeps the cancellation exception at `phase-06-cumulative-acceptance-and-documentation.md:33`. | VERIFIED | `internal/providers/stream.go:90` uses once-only result storage; line 104 states terminal delivery may fail after cancellation; `internal/agent/loop_stream.go:74` owns MessageEnd. |
| 6 | Existing subprocess checks can protect public signal/pipe output at `phase-06-cumulative-acceptance-and-documentation.md:32`. | VERIFIED | `cmd/tui/headless_test.go:469` owns TestSignal and line 511 owns TestEPIPE; existing signal exit values are at `cmd/tui/headless.go:37`. |
| 6 | All three live command-to-tool/logout paths pass at `phase-06-cumulative-acceptance-and-documentation.md:36`. | UNVERIFIED | Main plan `plan.md:267`, line 268, and line 269 require the three routes; line 271 explicitly says their live checks remain planned. |

Sample totals: VERIFIED 19, FAILED 2, UNVERIFIED 3.
Both failed samples are covered by the single inventory finding.
No sample treats a proposed new symbol as a current-source defect.
No runtime execution count or provider success is inferred from a source test count.

## Scope and complexity decisions

Keep the separate store transaction, auth strategy, and wire profile owners because they protect different contracts.
Reuse NewStream/Assembler, current hook composition, registry dispatch, and current tool snapshots.
A whole-file lock meets the selected single-host requirement; a per-provider lock is not required.
Do not add account collections, remote revocation, retained client registrations, SQL storage, or a second provider hierarchy.
H4 gates only ChatGPT/xAI wire acceptance; store, resolver, CLI, identity, device flow, and Anthropic work can proceed as planned.
The already reported durable rotation fence, signal grace period, subprocess HTTP seam, and xAI denial-name issues are not repeated here.

## Unresolved questions

No additional product-scope question was found.
The planned verifier selection, bearer integration, current grouped-tool encoding, and three live routes still require implementation or acceptance evidence.
Those checks do not justify a scope cut.

Status: DONE_WITH_CONCERNS.
Summary: Reviewed all six phases and sampled 24 contracts, with one concrete inventory finding and a complete affected-consumer inventory.
Concerns: Correct the overstated scout inventory before handoff, and keep the planned implementation/live gates explicit.
