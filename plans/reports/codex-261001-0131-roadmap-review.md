# Independent roadmap review

Date: 2026-10-01, Asia/Saigon.
Scope: roadmap, confirmed decisions, architecture section 7.3, supporting inventories, timeline, and earlier reports.
This is a document review, not a runtime test.
Only this report was written.

Paths below are relative to AskCore.
`R` = `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md`.
`P` = `plans/260930-2254-pi-feature-inventory-go-roadmap/plan.md`.
`H`, `E`, and `T` = `inventory-harness.md`, `inventory-extensions.md`, and `inventory-tui.md` in that folder.
`A` = `docs/ask-architecture-reference.md`.
Each shorthand citation identifies a file and its line numbers.

## Result

M1 is not ready for implementation without contract repairs.
The fixed decisions are compatible, but the completion gates and process contracts do not yet implement them consistently.
The findings below concern current conflicts or missing contracts, not the earlier review's repaired ownership and provider-order findings.
Design risks are stated as risks; no implementation failure is claimed.

## Blocker

### B1. M1 completion still requires M2 login and extension UI

Evidence: `R:25-28`, `R:505`, `R:527-539`, `R:561-574`, `R:578-580`; `T:270`; `E:181-193`.
T1 promises all P0 TUI rows, but assigns H-AUTH-10 to M2 H17 and enables `/login` only after that phase.
That row includes API-key login, not only OAuth.
X1 must ask a `confirm` at its exit, but the TUI extension UI subset is assigned to M2 T2.
A protocol definition or headless default-false response does not provide the required user interaction.
Fix: put API-key login/logout in M1 H7 or H13, and leave OAuth in H17.
Put the minimum serializable extension dialogs and their responses in T1; keep the other T2 features in M2.
Make T1 depend on H14, H15, and the needed X1 contract, and test X1 through the actual M1 TUI.
This preserves the user’s milestone decisions.

## Major

### M1. T0 makes image and fullscreen work a gate for M1

Evidence: `R:26`, `R:49`, `R:553-557`; `T:64-69`.
T0 requires G1 through G6 before it decides D14.
G4 requires a Kitty image in scrollback; G5 requires switching to fullscreen and back.
Both features belong to M2, but T1 waits for this gate.
Fix: make G1, G2, G3, and G6 the M1 pass criteria.
Keep G4 and G5 as recorded M2 checks without making them prerequisites for D14 or T1.
State that D14 remains open until the M1 gate passes; the current recommendation “v2” is not a confirmed pin.

### M2. H13 retains a second external agent contract after D16

Evidence: `R:51`, `R:400-407`; `H:235-238`; `A:189-199`, `A:201`.
D16 permits ACP plus `_ask/*` only, but H13 still owns Pi framing “mapped to gRPC and WS” and protects gRPC connections that can prompt and run bash.
The inventory also recommends gRPC agent commands.
An internal daemon-to-leader ACP hop does not make a public gRPC prompt API consistent with D16.
Fix: explicitly carry ACP JSON-RPC on external WS agent links and retire the Pi response envelope at that boundary.
Restrict gRPC to the permitted internal service surface and identify those methods.
Clarify that the separate extension stdio contract in X1 is an extension-host link; narrow `A:189` and “always direct Go calls” in `A:197` accordingly.

### M3. One agent has no defined session and driver ownership rules

Evidence: `R:410`, `R:421-422`; `A:207`, `A:227-230`; `H:39`, `H:201`.
The leader holds one agent, while `/new` and session replacement abort the outgoing turn and rebind subscriptions.
The leader tests cover two clients watching the same session, not two clients creating or switching different sessions.
The document does not say whether another client's `/new` replaces the shared session, is rejected while busy, or waits.
It also forwards each client's ACP traffic into one agent connection without defining how each client's `initialize` capabilities are kept separate.
Fix: specify one active session and run for the M1 leader, with explicit attach, switch, busy, and driver rules.
Keep client initialization state at the multiplexer and define which capabilities are supplied to the shared adapter.
Route reverse calls only to eligible clients; define driver loss and late or duplicate responses.
Test two clients with different cwd, capabilities, and session requests, including a driver disconnect during a question.

### M4. Droppable events cannot provide the promised gap-free replay

Evidence: `R:118`, `R:377-392`, `R:412`, `R:419`, `R:432-441`; `A:232`.
H12 permits bounded subscriber queues to drop events.
H13 and W1 promise reconnect without gaps, but name no replay owner, retained event buffer, cursor scope, or leader restart identity.
Putting `seq` in `_meta` cannot recover a lost streaming delta.
Fix: define the sequence scope and leader epoch, retain events before fan-out, and make replay-to-live handoff atomic.
When a cursor is too old, return an explicit resync result and a state snapshot.
Test queue overflow, reconnect during a run, a cursor older than retention, and a leader restart.
Keep command resubmission disabled; replaying events is a separate operation.

### M5. The SQLite lease has no loss-of-ownership or startup contract

Evidence: `R:283-295`; `A:252`, `A:264-266`; `internal/store/gormstore/db.go:12-13`; `internal/app/app.go:43-44`, `internal/app/app.go:87-88`.
The cross-process lease is a good addition, but “two processes cannot write one session” is not enough to define it.
Risk: a paused process can resume after its lease expires and write after a new owner acquires the session.
Different sessions also share SQLite, and the current opener does not explicitly set a contention policy.
Migrations currently run from the server module; headless and leader startup now need a database before the daemon exists.
Fix: define lease acquisition, renewal, release, and ownership checks in each write transaction, using an ownership generation to reject stale writes.
Stop the run when ownership is lost.
Give every database-owning mode the same bounded busy handling and serialized migration path.
Test lease expiry with a paused writer, crash recovery, writes to different sessions, and simultaneous fresh-database startup.
These are design risks, not observed corruption.

### M6. The daemon cannot copy Grok's executable lookup unchanged

Evidence: `R:40`, `R:42`, `R:398`; `A:236`; `plans/reports/researcher-261001-0022-grok-leader-code.md:72-73`.
Grok starts the leader from its current executable because its client and leader use one binary.
Ask has `ask-server` and `ask`; the shared `ConnectOrSpawn` must launch `ask` even when called by `ask-server`.
No executable resolution or installation contract is stated.
Fix: define how the daemon finds the matching `ask` executable and checks its version before spawn.
Return a clear startup error when that binary is absent or incompatible.
Bound connection and readiness waits, including a live lock holder with no usable socket.
Test daemon-first startup from a different cwd and with `ask` absent from PATH.

### M7. Version mismatch restart can interrupt other clients' work

Evidence: `A:232`, `A:238-247`; `R:421`.
The document permits stopping and restarting a client-spawned leader on version mismatch.
The spawn flag distinguishes a supervisor-owned leader, but does not prove that the leader is idle or unused by another client.
SIGTERM fallback trusts a pid file without a stated process-identity check.
Fix: reject mismatch with an upgrade hint by default.
Permit automatic restart only after a verified idle shutdown handshake and lock release.
Check process identity before a pid-based signal, and reject startup errors rather than silently selecting an unrelated leader.
Test mismatch during another client's tool call and with a stale, reused pid.

### M8. D13 names the Go requirement but does not define a buildable SDK input

Evidence: `R:48`, `R:520-521`, `R:534-539`; `go.mod:1`, `go.mod:3`.
The exit accepts a `main.go` file that imports a public SDK, but does not specify who provides its module file or how the installed binary supplies that SDK.
The current module name is `AskCore`, not a remote module address that an external extension can fetch.
Risk: the example works only inside a developer checkout or with an undocumented `replace` directive.
A source hash also needs a defined scope to cover dependency and toolchain changes.
Fix: define a small extension module layout, a resolvable or host-supplied pinned SDK, the supported Go version, and build/cache inputs including module files and target platform.
Test a clean user machine with no Ask checkout, missing Go, unsupported Go, and unavailable dependencies.
Keep skills and templates usable when extension builds cannot run, as D13 requires.

### M9. Reload can remove a blocking hook or accept an old reply

Evidence: `R:474`, `R:520`, `R:524-537`; `E:60`, `E:129`, `E:215`.
Reload restarts the child and removes registrations, but has no defined ordering with an active tool call or an extension command that requests `/reload`.
Risk: an offline child's blocking subscription is removed and the next tool call proceeds without the configured guard.
Risk: a reply from the previous child is accepted after its replacement starts, or reload waits for the command that is waiting for reload.
Fix: define reload at a safe boundary, child generations, pending-call cancellation, and registration replacement order.
Keep configured fail-closed hooks in an error state when their child is unavailable; do not treat that as an intentional unsubscribe.
Test reload during `tool_call`, reload requested by an extension command, late replies, and build failure after a previously loaded guard.
Keep the rule that changed source never uses an old cached binary.

### M10. The ACP SDK gate must check semantics and Ask schema support

Evidence: `R:52`, `R:405-412`; `H:235`; `A:199`, `A:261-263`.
D17 checks activity and schema version, but does not require proof that the selected SDK supports custom methods, metadata, notifications, and cancellation through the leader.
Pi's prompt response means accepted; ACP's prompt response ends the interaction and carries a stop reason.
The mapping is unspecified, so a literal Pi port can report completion before tools or retries finish.
The current official ACP schema also includes standard usage updates, which makes the claim that usage and cost always require Ask extensions too broad.
Fix: pin the SDK and ACP schema separately, define each M1 Go API-to-ACP mapping, and specify prompt completion versus `agent_settled`.
Version and advertise `_ask/*` capabilities; test unknown methods, preserved `_meta`, cancel notifications, and direct/remote behavior against the same Go API tests.
Keep D17 open until this conformance check passes.
Sources: [ACP prompt lifecycle and cancellation](https://agentclientprotocol.com/protocol/v1/prompt-turn), [ACP extension capability rules](https://agentclientprotocol.com/protocol/v1/extensibility).

## Minor

### N1. Current decisions and architecture still contain conflicting statements

Evidence: `P:65`, `P:67`, `P:69-72`, `P:78`; `A:197`, `A:222`, `A:316`; `R:73`.
Old rows still present an M1 policy handler, internal extensions for self-upgrade, and `cmd/server` as the leader.
Later confirmed rows supersede them, but the table does not mark the old rows as history.
Section 10 still calls the daemon “the leader,” and the headless diagram still labels the direct call ACP.
Phase 0 lists architecture sections to align but omits section 10.
Fix: mark superseded rows explicitly without changing the user decisions, correct the two process statements, and add section 10 to Phase 0.
The old dewee flow is already marked pending replacement at `A:183`; that known transition is not a new finding.

### N2. The reading order and one H15 test still pull M2 forward

Evidence: `R:13-16`, `R:25-28`, `R:89`, `R:474`, `R:571`, `R:590`.
The stated order builds H14 through H17 before extensions and TUI, contrary to the must-have-first rule.
H15 correctly defers MCP reload to M2, then states an unqualified test that writes MCP config and uses it.
T1 also credits H14 with the picker, while section 6 assigns the picker to T1.
Fix: publish an M1-only execution order before the M2 track, label the MCP test M2, and correct picker ownership.
H15's shell-hook mention is already qualified as M2; it is not an M1 shell-hook dependency.

### N3. H13a and H13b have no separate runnable exits or module checks

Evidence: `R:9`, `R:398`, `R:414-422`, `R:608`; `A:164-165`, `A:264-266`; `internal/app/app.go:23-45`.
H13a combines adapter mapping, routing, process startup, locking, and socket security.
Only the combined H13 exit is specified, so the named split does not provide a small learning checkpoint.
The new fx module combinations also have no explicit validation gate; the existing module starts network servers.
Fix: give adapter-over-stdio, leader-over-socket, and daemon-over-ACP separate runnable exits within H13.
Validate the headless, leader, editor, and gateway compositions separately, and test that headless and editor modes start no network listeners.
The Go API primary rule is already clear at `A:197`; it needs these execution checks, not a different architecture.

## Coverage and limits

H1 through H12 and H14 through H15 name concepts and runnable exits.
H13 is the main learning-size problem.
W1 explicitly waits for H13's feed (`R:428`), and H15 correctly waits for X1 before loading Go extensions (`R:474`).
Credential locking and read-merge-write are explicit (`R:264-268`); this review does not claim they are missing.
Add a reader-visibility check when implementing them: after headless changes an API key, the already-running leader must use the new key on its next request (`H:137`).
No SDK was selected, no product was run, and no source or plan was changed.

## Unresolved questions

1. D14: which pin and committer route pass the M1-only T0 gate?
2. D17: which SDK passes the pinned ACP and `_ask/*` conformance checks?
3. With one agent, does `/new` replace the session for every attached client, or require explicit driver ownership?
4. What retained event window and resync behavior should clients receive after a long disconnect or leader restart?
5. How will an installed Ask binary supply the pinned extension SDK without a source checkout?

Status: DONE_WITH_CONCERNS
Summary: The review found one M1 completion blocker, ten major contract gaps, and three minor consistency or learning gaps.
The fixes preserve the confirmed decisions and keep M2 work out of M1.
