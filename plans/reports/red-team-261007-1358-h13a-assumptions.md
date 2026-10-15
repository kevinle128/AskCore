# H13a assumption and contract review

Status: DONE_WITH_CONCERNS

Read all four phases, the plan index, the phase scout, the SDK research, the Grok port report, and the cited local owners.
Checked 10 claims per phase, for 40 claims total.
No execution, lint, type, or build test was run.
Public call-site counts remain lexical navigation bounds, not consumer counts or evidence that signatures can change.
The immediate-package source test counts match the scout: ACP 0, Agent 345, bus 13, app 13, auth 25, providers 94, sessions 11, protocol 114, and CLI 81.

## Findings

### High: admission binding assumes every admitted run has agent_start

Plan: `phase-02-session-adapter.md:72` binds a request to the next matching `agent_start`.
Source: `internal/agent/agent.go:469` opens the session log before entering the loop; `internal/agent/loop_run.go:252` emits `agent_start` only after that step.
If the initial log append fails, `internal/agent/agent.go:519` emits failure content and `agent_end`, then `internal/agent/agent.go:485` emits settled, without a start event.
An adapter that waits only for start can hang a failed prompt or bind a later queued run to that request.
Continue can also return an empty-log or assistant-tail error without any event at `internal/agent/agent.go:150`.
Specify a terminal path when the Agent call returns before start is observed.
Bind failure envelopes safely where present, preserve the error, and never wait for an event that the call cannot emit.
Add real failing-log and both Continue rejection checks, followed by another admission, to prove no cross-run binding.

### Medium: Reset must replace the mandatory completion observer

Plan: `phase-02-session-adapter.md:76` resets the epoch, while `phase-03-ordered-events-and-control.md:82` requires a mandatory observer independent of optional subscriptions.
Source: `internal/agent/follow.go:213` resets the publication epoch and ring; `internal/bus/follow.go:133` detaches all old followers with resync.
The plan defines failure during an active turn, but does not define replacement of the mandatory stream after an idle Reset.
If the old stream is retained, the next prompt can wait on a closed observer or fail a healthy connection.
Establish the new-epoch mandatory observation before reopening admissions or completing Reset, and fence old queued frames.
Add Reset followed immediately by Prompt, with an old optional cursor still present, to the built-process matrix.

## Claim checks

The source references below use repository-relative paths.
“Gate” means the plan correctly leaves the claim for executable conformance; it is not a PASS result.

### Phase 1

| # | Claim and plan reference | Source evidence and result |
|---|---|---|
| 1 | SDK selection remains gated, phase-01:99–101 | Root go.mod has no ACP SDK pin; correct Gate. |
| 2 | Wire/schema/SDK identities differ, phase-01:107 | SDK generated stable and unstable types coexist; correct Gate for exact schema identity. |
| 3 | Metadata can lose integer precision, phase-01:108 | SDK types_gen.go:5614–5619 decodes a union through map[string]any; supported concern. |
| 4 | Requests can run while Prompt waits, phase-01:110 | SDK connection.go:381–397 starts request handlers in goroutines; supported, pressure still Gate. |
| 5 | Request cancellation uses request identity, phase-01:111 | SDK connection.go:506–529 canonicalizes ID and cancels its context; supported, Agent cancellation meaning still Gate. |
| 6 | Notification pressure differs from request dispatch, phase-01:112 | SDK connection.go:399–419 queues notifications; bounded channel declared at :101; supported. |
| 7 | Short writes need a checked boundary, phase-01:129 | SDK connection.go:578–581 discards n; supported. |
| 8 | Handler response errors can be discarded, phase-01:130 | SDK connection.go:568 discards sendMessage error; supported. |
| 9 | First-ingress cap needs proof, phase-01:131 | SDK connection.go:345–353 uses a 10 MiB Scanner cap; explicit stricter mitigation remains Gate. |
| 10 | Stream baseline includes unfinished bytes, phase-01:61 | internal/agent/follow.go:58–67 carries protocol.Partial plus AttemptID and Seq; supported. |

SDK source was read at [pinned connection.go](https://raw.githubusercontent.com/coder/acp-go-sdk/v0.13.5/connection.go) and [pinned generated types](https://raw.githubusercontent.com/coder/acp-go-sdk/v0.13.5/types_gen.go).
Tag immutability, schema identity, and executable conformance remain unresolved as the plan states.

### Phase 2

| # | Claim and plan reference | Source evidence and result |
|---|---|---|
| 11 | Existing constructor cannot accept independent requested cwd/ID, phase-02:64 | cmd/tui/headless.go:230 and :241 use process cwd and internal ID; supported extraction. |
| 12 | One Agent owns one conversation, phase-02:8 | internal/agent/agent.go:39–65 holds one active run and one publication state; supported. |
| 13 | Prompt does not return a run ID, phase-02:70 | internal/agent/agent.go:135 returns error only; supported. |
| 14 | Binding only to agent_start is sufficient, phase-02:72 | agent.go:469–485 and :519–562 contradict this; High finding above. |
| 15 | Busy Prompt is rejected, phase-02:84 | agent.go:395–401 calls idleLocked; :364–369 checks busy/disposed; supported. |
| 16 | Continue has real state errors, phase-02:84 | agent.go:150–163 rejects empty/assistant-tail context before events; supported. |
| 17 | Model readiness failure leaves state unchanged, phase-02:87 | agent.go:265–306 resolves readiness before assigning cfg.Model; supported. |
| 18 | Thinking currently lacks disposed guard, phase-02:112 | agent.go:311–318 checks active only; correct measured gap, narrow guard remains Gate. |
| 19 | Qualified model lookup is required, phase-02:86 | internal/providers/catalog.go:99–122 rejects ambiguous unqualified choices; supported. |
| 20 | Faux-start sessions need real API registration, phase-02:101 | cmd/tui/headless.go:236–239 registers extra APIs only for nondefault provider; supported planned correction. |

### Phase 3

| # | Claim and plan reference | Source evidence and result |
|---|---|---|
| 21 | Follow supplies a consistent cut, phase-03:64 | internal/agent/follow.go:131–160 coordinates publication and snapshot commit; supported. |
| 22 | Incoming SDK watermark is not outbound completion, phase-03:65 | SDK connection.go:495–502 observes received notification progress; supported. |
| 23 | Prompt completion is not global idle, phase-03:72–73 | agent.go:135 and :417–432 allow queued next run at end; supported per-run barrier requirement. |
| 24 | Abort clears queues by default, phase-03:99 | agent.go:173–188 clears inbox/wake unless KeepQueued; supported. |
| 25 | Started tools must drain before cancellation completion, phase-03:71 | internal/agent/tool_coordinator.go:65 drains workers before repair; tool_abort_test.go:121 rejects settled before body drain; supported contract. |
| 26 | Mandatory observer needs replacement after Reset, phase-03:82 | follow.go:213–218 and bus/follow.go:133–139 close old followers; Medium gap above. |
| 27 | Fresh Follow has a baseline and later events, phase-03:87–89 | follow.go:147–160 snapshots open Builder partial state and cursor; supported, wire cutover remains Gate. |
| 28 | Invalid cursor needs resync, phase-03:104 | bus/follow.go:179–190 rejects unavailable epoch/cursor; supported. |
| 29 | Oversized event cannot be silently delivered, phase-03:74–75 | agent/follow.go:125–129 and bus/follow.go:102–130 encode gaps and detach; supported. |
| 30 | Queue IDs and removal preserve existing semantics, phase-03:101 | agent/queue.go:166–194 admits IDs; :202–211 returns false after claim/disposal; supported. |

### Phase 4

| # | Claim and plan reference | Source evidence and result |
|---|---|---|
| 31 | ACP must dispatch before positional parsing, phase-04:114 | cmd/tui/headless.go:70 dispatches auth before normal parse; ACP is absent; supported new command boundary. |
| 32 | Native auth construction already exists, phase-04:127 | internal/app/auth_native.go:11 calls existing native auth composition; supported reuse. |
| 33 | Auth binding is private, phase-04:127 | internal/app/module_auth.go:70 installs shared stream/readiness; token-bearing binding stays inside stream; supported. |
| 34 | AuthWait has an independent 20-second drain, phase-04:131 | internal/app/module_auth.go:53–58 uses context.WithTimeout then DrainRefresh; supported. |
| 35 | runAuth must not read ACP stdin, phase-04:105 | cmd/tui/auth.go:49 and :70 call readPrivateLine; supported exclusion. |
| 36 | Prelogin/readiness is the accepted auth scope, phase-04:93–95 | Recorded user answer plus existing app callbacks; supported scope, no interactive ACP login inferred. |
| 37 | EOF must dispose owned host, not future shared leader, phase-04:76–83 | Agent.Dispose closes admission and joins publisher at agent.go:210–259; ownership policy remains adapter implementation Gate. |
| 38 | Closing I/O is needed for blocked SDK writes, phase-04:77 | SDK connection.go:578–581 calls writer synchronously under lock; cancellation alone does not interrupt it; supported Gate. |
| 39 | No listener is possible without leader/server composition, phase-04:109 | Existing native/headless path calls app auth and Agent directly at headless.go:209–262; new fx graph requires actual Gate. |
| 40 | No credential DTO or raw provider error may escape, phase-04:105–106 | providers.AuthSnapshot enters app/module_auth.go:20–43 privately; providers/diagnostic.go:28 supplies cleaned diagnostics; mandatory DTO/error mapping remains Gate. |

## Additional implementation constraints

SDK connection.go:362 logs malformed input with the raw line through its logger.
Phase 4 already requires secret-free stderr and malformed input checks in Phase 1.
Make that check use a token sentinel in malformed input and own/redact SDK logging; default logging is not safe evidence of that requirement.
This is an implementation constraint, not a separate finding that overrides the recorded threat model.

The flow table's PASSED labels are explicitly qualified as planned wiring, with execution pending.
Do not convert them into conformance or implementation results.
The plan correctly preserves full H13a scope, current owner APIs, no leader/gateway, no fake H8/H10 capabilities, and no second Agent loop.
Fix the two admission/observer assumptions before implementing those paths.
