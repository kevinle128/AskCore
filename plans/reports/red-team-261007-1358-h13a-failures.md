# H13a failure-mode review

Status: DONE_WITH_CONCERNS.
Scope: planning only.
Read all five plan files, the SDK report, the phase scout, and cited source.
No product test was run and no plan or code was changed.

## Findings

### High: the built-process started-tool-drain fixture has no supported owner

`plans/261007-0700-h13a-acp-stdio/plan.md:60` requires a blocking cancellable real tool path.
Lines 50–52 require the normal built command and allow only external provider/OAuth mocks.
Current production tools are Echo only: `cmd/tui/headless.go:199`.
Extra tools are explicitly test inputs and are not passed by ask: `cmd/tui/headless.go:201`.
Echo checks context once, parses its argument, and returns: `internal/tools/echo.go:25`.
Holding provider HTTP does not hold an already-started tool body.
Thus that fixture cannot prove the requested drain behavior under the current test restrictions.

Repair the validation contract.
Keep normal built-process inference cancellation E2E.
Add an explicit support-test composition exception for a controlled tool passed through the existing tool injection seam, with the real Agent executor, host, and stdio transport.
Label this as integration proof of started-body drain, not proof that the unmodified production binary contains a blocking tool.
Do not add an unrequested production tool or a private CLI test flag.
Alternatively name an existing supported production tool that provides a controllable body; current source provides none.

### Medium: Reset must replace the mandatory observer, not only reject old explicit cursors

`phase-02-session-adapter.md:76` and `phase-03-ordered-events-and-control.md:104` require Reset and later observation in a new epoch.
The mandatory observer is independent of explicit subscriptions at `phase-03-ordered-events-and-control.md:83`.
The planned recovery/failure rule at lines 74–76 addresses resync during a turn.
It does not define recovery of that mandatory observer after idle Reset.

Actual Reset calls resetPublished: `internal/agent/agent.go:350`.
resetPublished resets the existing ring: `internal/agent/follow.go:218`.
Ring.Reset detaches every existing follower: `internal/bus/follow.go:133`.
Therefore successful Reset leaves the mandatory observer unusable unless the adapter replaces it.
A later Prompt could have no sender/barrier source.

Add an explicit atomic reset/observer replacement transition with an epoch fence.
Establish the new mandatory Follow before admitting the next Prompt.
Test Reset → new Prompt with no explicit follow subscriber, then verify final update/write ordering.
Retain explicit old-cursor resync.
This is a missing adapter rule, not a proposed change to Agent.Reset.

## Source fact verification

Each phase below has ten checked claims.
“Verified” means source inspection, not an executed behavior result.
SDK source facts use the pinned primary URLs already inspected for the SDK report.
Fork/schema gaps remain unresolved.

### Phase 1

| # | Claim | Result and evidence |
|---|---|---|
| 1 | ACP adapter is server-side | Verified, internal/acp/README.md:3 |
| 2 | Wire DTOs belong in protocol | Verified, internal/acp/README.md:17 |
| 3 | Headless uses direct Agent calls | Verified, internal/acp/README.md:41 |
| 4 | ACP source tests do not exist | Verified, immediate package glob has zero test files |
| 5 | Protocol has 114 top-level tests | Verified by source declaration count across 13 immediate test files |
| 6 | coder v0.13.5 is module github.com/coder/acp-go-sdk, Go 1.21 | Verified, pinned go.mod primary source |
| 7 | coder requests run concurrently | Verified, pinned connection.go:387 |
| 8 | Notifications have ordered bounded dispatch | Verified, pinned connection.go:83,399,462 |
| 9 | SDK response watermark is inbound | Verified, pinned connection.go:496,606 |
| 10 | Count/error checking requires physical output proof | Verified concern, pinned connection.go:580 ignores count; :568 discards handler response send error |

Primary URLs: [manifest](https://raw.githubusercontent.com/coder/acp-go-sdk/v0.13.5/go.mod), [connection](https://raw.githubusercontent.com/coder/acp-go-sdk/v0.13.5/connection.go).
No exact schema release/commit is verified.
The plan correctly leaves selection open.

### Phase 2

| # | Claim | Result and evidence |
|---|---|---|
| 1 | Prompt blocks through execution | Verified, internal/agent/agent.go:135 |
| 2 | Prompt returns no public run ID | Verified signature, internal/agent/agent.go:135 |
| 3 | Continue rejects empty context | Verified, internal/agent/agent.go:157 |
| 4 | Continue rejects assistant-tail context | Verified, internal/agent/agent.go:160 |
| 5 | Reset is idle-only | Verified, internal/agent/agent.go:328,337 |
| 6 | Reset creates a new epoch | Verified, internal/agent/agent.go:350; follow.go:216 |
| 7 | SetModel checks readiness before mutation | Verified, internal/agent/agent.go:284,288,304 |
| 8 | Thinking has no disposed guard | Verified, internal/agent/agent.go:311 |
| 9 | Catalog has six rows, excluding faux | Verified, internal/providers/catalog.go:9 |
| 10 | Qualified API resolves model ambiguity | Verified, internal/providers/catalog.go:105,122 |

Additional factory facts are sound.
The current constructor obtains process cwd and creates its own session ID at cmd/tui/headless.go:230,241.
It registers extra real provider wires only when the initial provider is not faux at :236.
The plan correctly calls for an ACP-specific complete registration policy while preserving direct headless behavior.

### Phase 3

| # | Claim | Result and evidence |
|---|---|---|
| 1 | Abort clears queues by default | Verified, internal/agent/agent.go:179 |
| 2 | Abort does not wait | Verified implementation, internal/agent/agent.go:173 |
| 3 | WaitForIdle includes queued later runs | Verified, internal/agent/agent.go:191 |
| 4 | Dispose closes admission first | Verified, internal/agent/agent.go:226 |
| 5 | Dispose waits for active run | Verified, internal/agent/agent.go:237 |
| 6 | Follow has consistent cursor and snapshot | Verified, internal/agent/follow.go:112,131 |
| 7 | Open baseline includes raw bytes and attempt ID | Verified, internal/agent/follow.go:58,63 |
| 8 | Oversize events require resync | Verified, internal/agent/follow.go:126; internal/bus/follow.go:114 |
| 9 | Idle queue admission starts its own run | Verified, internal/agent/queue.go:181,193 |
| 10 | Attempt usage is committed separately from AttemptEnd | Verified, internal/agent/lifecycle.go:168–183 |

AttemptEnd alone does not carry complete accounting.
The plan correctly requires committed settlement plus Follow facts.
Agent.emit supplies seq/session/run identity at internal/agent/emit.go:52.
Local listener dispatch precedes publication at :69–70, so transport writes must remain outside that path.

### Phase 4

| # | Claim | Result and evidence |
|---|---|---|
| 1 | Auth command dispatch precedes ordinary parser | Verified, cmd/tui/headless.go:70,108 |
| 2 | Bare acp has no existing command branch | Verified in runWithDependencies and parser positional handling, cmd/tui/args.go:94 |
| 3 | Native auth constructor is shared | Verified, internal/app/auth_native.go:11 |
| 4 | BindAuth installs stream and readiness together | Verified, internal/app/module_auth.go:70 |
| 5 | BindAuth reuses Registry.Prepare | Verified, internal/app/module_auth.go:72 |
| 6 | AuthWait has independent 20-second bound | Verified, internal/app/module_auth.go:55 |
| 7 | Headless grace is two seconds | Verified, cmd/tui/headless.go:46 |
| 8 | Signals map to 130/143/129 | Verified, cmd/tui/headless.go:39 |
| 9 | runAuth reads private raw stdin | Verified, cmd/tui/auth.go:49,70 |
| 10 | AuthSnapshot contains access material | Verified, internal/providers/auth.go:19 |

LoginRequest has cancellation-aware interaction callbacks at internal/auth/login.go:13.
LoginNotice contains instructions at :22.
Logout uses local store deletion at :134.
The plan's confirmed CLI-login decision avoids stdin competition and adds no interactive editor login.

## Accepted safeguards and limits

The current plan correctly distinguishes inbound SDK watermark from actual outbound writes.
It requires error latching even when SDK handler response writes discard errors.
It does not copy a bounded shutdown grace into ordinary cancellation.
It keeps shared-host detach separate from exclusive editor-host disposal.
These concerns need no duplicate finding.

The PASSED table is explicitly limited to planned wiring.
It is not executed proof, and unchecked acceptance/D17 gates remain open.
Do not publish those rows as passing product tests.

Unresolved: selected SDK/schema pins and executed conformance.
The two findings above require plan clarification before implementation acceptance.

