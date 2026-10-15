# H13a phase scout

## Result

Use four phases with one ACP SDK connection, one session host, the existing Agent API, and the existing bounded follow path.
The host owns many independent Agents; each Agent still owns one conversation.
Do not implement the leader, durable loading, compaction, or a second execution loop.
This report is source evidence for a deep TDD plan, not implementation or test execution.

Read sources: root instructions and README, architecture section 7.3, the H13 roadmap and D17, package README files for acp, agent, bus, app, auth, providers, sessions and protocol, command dispatch and headless composition, and the Grok port report.

## Phase 1: SDK conformance and wire contract

Create `internal/acp/conformance_test.go` and `pkg/protocol/acp.go` with tests for the actual Ask extension DTOs.
Add the selected pinned SDK to `go.mod` and `go.sum` after its executable check passes.
Modify `internal/acp/README.md`, `pkg/protocol/README.md`, and AGENTS.md only for the selected dependency and actual contract.
Delete no existing files.

Red checks must use a real SDK client and agent connection over pipes: initialize/version negotiation, standard prompt completion, concurrent cancellation while prompt waits, custom request/notification dispatch, `_meta` retention, escaped method names, malformed JSON, numeric/string IDs, ordered notifications before the terminal response, reader EOF, output failure, and the first-ingress byte limit.
Record ACP wire version, schema revision, and SDK module version separately.
Keep SDK validation errors distinct from Ask runtime errors.
D17 closes only on this evidence; the Grok report does not select an SDK.

The v1 completion contract is already the accepted default.
Draft v2 would change the roadmap and needs a user decision.
Use SDK transport before adding another JSON-RPC engine.

## Phase 2: Session host and current Go controls

Create `internal/acp/agent.go`, `internal/acp/agent_test.go`, and the minimal host owner file only if host state makes agent.go too complex.
Create reusable native Agent construction in `internal/app`, with a test.
Modify `cmd/tui/headless.go` to reuse that construction while preserving its direct Go path.
Modify `internal/providers/catalog.go` and its existing tests to expose copied compiled model rows if needed for available models.
Delete only composition code that moves to app; delete no files.

The injected factory must accept the session ID, requested cwd, and initial model/settings.
Existing `newHeadlessAgentWithAuth` gets os.Getwd and creates its ID internally, so it is not a valid session/new factory unchanged.
Each session/new allocates an independent Agent and writer.
Do not use Reset on another session's Agent.
Keep initialize capabilities per connection, not in shared Agent state.
Use a mutex for the session map; release it before factory calls, readiness checks, prompts, waits, and disposal.
Publish only successfully constructed sessions.

Red checks: use before initialize, unknown session, separate cwd/history/model across two sessions, concurrent session creation, failed factory without a leaked entry, prompt completion, busy prompt rejection, model lookup ambiguity, failed model readiness leaving the old model/thinking intact, state retrieval, thinking clamp, explicit unsupported loading/compaction/tree, and disposal preventing new admissions.
Reject unsupported prompt blocks and MCP configuration explicitly; do not accept input that is silently lost.

Current Agent public controls are Prompt, Continue, Abort, WaitForIdle, Dispose, SetModel, SetThinkingLevel, Reset, State, Steer, FollowUp, Remove, and Follow.
The adapter needs no second versions of these controls.
A host constructor, typed session factory, session lookup, and idempotent host disposal are sufficient new lifecycle contracts.
Use functions for injected model resolution and auth operations rather than adding an interface with one implementation.

The acp README permits agent, sessions, bus, protocol and SDK imports.
Keep auth and provider construction in app.
An injected model callback can call SetModel without giving ACP a second model authority.
If direct provider imports are selected instead, update the package boundary explicitly.
Core packages must not import acp or leader; depguard enforces this.

The private catalog has six rows, with two Token Plan rows for the same provider/model on different APIs.
Find resolves provider/model/API and rejects an ambiguous unqualified Token Plan choice.
Return qualified IDs that round-trip through Find; do not invent another catalog.
Compiled model capability is not proof of account entitlement.

## Phase 3: Ordered updates, follow, queues, and cancellation

Create `internal/acp/updates.go`, `meta.go`, `ask_methods.go`, and focused tests as those responsibilities gain behavior.
Extend `pkg/protocol/acp.go` and its tests for queue/follow and Ask event methods.
Change Agent or bus only if an executable test proves a missing public contract.
Delete no existing files.

Consume Agent.Follow rather than synchronous listeners for transport writes.
One ordered sender owns outbound updates for each connection.
Never hold a session-map or Agent state lock across a write.
A prompt response must wait until updates through its settled event have reached the sender's completion barrier.
Queue admission acknowledgment is not prompt completion.
Prompt remains busy-rejecting; Steer and FollowUp keep the existing claim/wake semantics.
Remove reports the existing bool result and must not invent an admission result for already claimed input.

Follow returns a consistent log cut, Cursor(epoch,seq), a StreamBaseline for any open assistant message, and events after the cut.
The baseline has AttemptID and raw open block bytes, including incomplete tool arguments.
Preserve it in the Ask follow contract; a transcript snapshot alone cannot restore an open block.
Resume returns no snapshot only for a valid retained cursor.
Invalid epoch, gaps, future/unavailable cursor, oversized events, and slow consumers require explicit resync.
Do not auto-resubmit a command after reconnect or resync.
Close each follower on detachment.

Red checks: text/thinking/tool projection, safe metadata and run/sequence identity, retry facts and exact attempt usage, settled order, baseline plus later deltas without duplicates, bad cursor fresh snapshot, overflow explicit resync, two-session isolation, idle/busy steering and follow-up, input removal, abort clearing queues, cancel then new prompt, cancellation during preparation/retry/tool drain, and blocked/failed output cleanup.
Cancel calls Abort; the pending prompt resolves only after settled and started tool bodies drain.
Abort is separate from Dispose.
Do not call Dispose or WaitForIdle from a synchronous listener.

## Phase 4: Stdio/auth composition and complete E2E

Create `internal/acp/stdio.go`, `cmd/tui/acp.go`, parser/command tests, and real subprocess stdio E2E tests using existing local HTTP fixtures.
Create the app ACP module and fx.ValidateApp test.
Modify `cmd/tui/headless.go` dispatch, `args.go` help, root README, app/acp README files, and AGENTS.md for actual new command/composition.
Preserve `main.go` terminal demo unless the dispatch change requires it.
Delete no files.

Register `acp` before ordinary prompt parsing and mode selection, like the existing `auth` command.
Today bare `acp` is a positional prompt, so a new parser field alone does not register a command.
Stdin becomes exclusively framed ACP input; stdout becomes exclusively protocol output; diagnostics use stderr.
Editor composition must start no database, leader, gateway, or network listener.
Reuse NewNativeAuth and BindAuth, and keep the auth HTTP client outside inference capture.

No privateAuthSnapshot symbol exists in current source.
The credential-bearing providers.AuthSnapshot is not an ACP login interaction object and must never enter wire DTOs or logs.
Service.Login accepts cancellation-aware Input and Notify callbacks; LoginNotice carries private sign-in instructions, not issued tokens.
Service.Logout is local deletion.
cmd/tui runAuth reads raw private stdin and is unsuitable inside a framed connection.
If authenticate performs an actual login, inject interaction through a negotiated protocol method; never compete for stdin.
The accepted M1 guidance permits login through existing `ask auth` on the inference host.
Full TUI/gateway login dialogs remain M2.
Do not advertise an interactive auth method that the stdio adapter cannot complete.

EOF and explicit output failure end the editor-owned host, close admission, stop auth refresh, cancel work, drain started tool bodies, close followers and join sender/connection tasks.
Host disposal is idempotent.
Future leader client detach must close only client followers; it must not dispose shared session Agents.
Keep that lifetime boundary available through composition, rather than putting shared-host disposal in every connection Close.
AuthWait supplies the existing independent 20-second auth drain.
Do not copy the headless two-second grace into ordinary cancel completion; tool drain is unbounded by contract.

Red subprocess checks: real built ask acp, clean framing with stderr diagnostics, initialize/new/prompt/cancel/model/queue/follow, two sessions, EOF, signal exit codes, output pipe failure, provider error/retry, API-key plus saved native credential/refresh, no secret in stdout or capture, no TCP listener, and shutdown without leaked children/goroutines.
Run focused packages first, then affected package tests, lint and build.
Do not replace actual stdio process acceptance with direct handler calls.

## Source test inventory

Counts match top-level `func Test...(` declarations in immediate package `_test.go` files.
They exclude nested provider packages, subtests, benchmarks, examples and fuzz targets.
They do not state that tests pass.

| Package | Test files | Top-level tests |
|---|---:|---:|
| internal/acp | 0 | 0 |
| internal/agent | 32 | 345 |
| internal/bus | 2 | 13 |
| internal/app | 3 | 13 |
| internal/auth | 6 | 25 |
| internal/providers | 15 | 94 |
| internal/sessions | 5 | 11 |
| pkg/protocol | 13 | 114 |
| cmd/tui | 18 | 81 |

## Protected call surface

These are lexical call-site counts in cmd/internal/pkg Go source, excluding declaration and full-line comment lines.
Counts can include same-name methods on other types and are navigation bounds, not type-resolved call graphs.
Do not use them as evidence to change public signatures.

| Symbol | Production sites | Test sites |
|---|---:|---:|
| Prompt | 1 | 271 |
| Continue | 0 | 8 |
| Abort | 1 | 58 |
| WaitForIdle | 0 | 9 |
| Dispose | 1 | 52 |
| Reset | 4 | 16 |
| SetModel | 0 | 17 |
| SetThinkingLevel | 0 | 4 |
| Steer | 0 | 9 |
| FollowUp | 0 | 9 |
| Remove | 2 | 12 |
| Follow | 1 | 26 |
| NewNativeAuth | 2 | 0 |
| BindAuth | 1 | 8 |
| AuthWait | 2 | 0 |
| newHeadlessAgentWithAuth | 2 | 0 |
| runWithDependencies | 1 | 2 |
| parseArgs | 1 | 1 |
| runAuth | 1 | 0 |
| openProvider | 1 | 0 |

Protect existing headless JSON, provider request preparation, sole log writer, retry billing binding, queue claims, tool drain, and Follow consistency.
Agent errors include ErrBusy, ErrDisposed, ErrNoAPIKey, ErrQueueFull and Continue state errors.
Map them once at the adapter boundary with safe error data.
Do not pass arbitrary provider error bodies or credential context into JSON-RPC errors.
SetThinkingLevel currently checks active execution but does not check disposed state; adapter host lookup must reject closed sessions, and a planned disposed-control test can establish whether the Agent contract needs a narrow correction.

## Material decisions

The user confirmed ACP v1 and one host with one Agent per session.
D17 still needs executable SDK conformance and pin selection.
Use the confirmed ACP v1 completion and independently owned per-session Agents.
Use a composition-owned shared host, existing compiled model authority, and explicit unsupported deferred-owner methods as maintainable defaults from accepted decisions.
Do not ask again about those boundaries.
Interactive authenticate transport is a material scope decision only if the required H13a behavior extends beyond existing host-side native login and readiness.
Frame-byte and queue limits are implementation constants with source tests unless deployment requirements make them user-configurable.

Unresolved questions: the selected SDK's exact capabilities and notification barrier; whether H13a authenticate must perform new interactive login rather than return actionable host-side login guidance.
