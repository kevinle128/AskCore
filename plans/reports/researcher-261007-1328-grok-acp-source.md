# Grok agent-side ACP source evidence for H13a

Source root: `/Users/dale/Desktop/workspace/opensources/grok-build`.
Verified HEAD: `2bdd1d6a6369de0e8c68132ea4539e9abd9e14a8`.
This is read-only source research.
No source commands, builds, dependency installs, or tests were run.
All source paths below are relative to that root.

## Contract and version

`Cargo.toml:123` selects `agent-client-protocol` 0.10.4 with `unstable` features.
`crates/codegen/xai-acp-lib/Cargo.toml:8` uses that workspace dependency and enables `unstable` again.
The implementation returns `ProtocolVersion::V1` in `crates/codegen/xai-grok-shell/src/agent/mvp_agent/acp_agent.rs:566`.
Its prompt handler waits for the turn result; it does not return an immediate acceptance receipt.
Do not infer ACP version from the terminal UI framework version.
Pin the schema version and Go SDK version separately for Ask.

## Entry and transport

The CLI routes `AgentCmd::Stdio` to `agent_command::run_stdio` at `crates/codegen/xai-grok-pager-bin/src/main.rs:1597`.
`crates/codegen/xai-grok-pager-bin/src/agent_command.rs:63` selects between the stdio runtime and process signals.
Its signal listener has a 30-second shutdown deadline and handles a second signal (`:14`, `:39`).
`crates/codegen/xai-grok-pager/src/agent_runtime.rs:20` selects the shell backend and calls `run_stdio_agent`.
The CLI also contains a leader bridge and reconnect logic; the direct embedded branch above is the relevant H13a reference.
The bridge is not a requirement for H13a.

`crates/codegen/xai-grok-shell/src/agent/app.rs:220` owns direct stdio setup.
It binds process lifetime to parent death when possible (`:226`), reserves stdout for the ACP connection (`:246`), and routes incoming lines through an 8 MiB simplex stream (`:248`; constant `:26`).
A dedicated stdin reader feeds that stream (`:254`).
EOF closes the simplex writer after 100 ms (`:273`), then the connection I/O future can return.
Shutdown cancels the agent token, closes PTYs, drains pending uploads and telemetry, and waits two seconds (`:316`).
This is concrete cleanup behavior, not proof that every active session has completed a durable flush on EOF.

`spawn_agent_local` constructs the existing `MvpAgent`, stores it in an `Rc`, wraps incoming bytes in `LineBufferedRead`, creates `AgentSideConnection`, and starts the reverse gateway (`agent/app.rs:125–160`).
The SDK owns JSON-RPC parsing, IDs, wire serialization, and request dispatch.
The local library mainly supplies channel routing and transport workarounds.
A Go port should keep transport separate from the existing Agent execution path.

## Initialization and session creation

`MvpAgent::initialize` is the protocol owner (`agent/mvp_agent/acp_agent.rs:130`).
It reads client metadata and starts supporting services.
It advertises load-session, embedded context, HTTP/SSE MCP, and selected session capabilities (`:556–594`).
It returns auth methods and vendor metadata; this includes the default auth method and model information (`:467–594`).
The single-call initialization invariant is documented at `:126`; do not treat that comment as a general concurrency guarantee.

`new_session` delegates to `new_session_inner` (`acp_agent.rs:989–1004`).
`crates/codegen/xai-grok-shell/src/agent/mvp_agent/session_setup.rs:388` requires a stored initialize request and rejects creation before initialize (`:395`).
It resolves workspace CWD, MCP servers, trust settings, and session metadata (`:401–422`).
It accepts a vendor-provided session UUID only after UUID validation; otherwise it creates a UUID v7 (`:469–481`).
A Go adapter should validate session IDs and absolute workspace paths at the boundary and use Ask's workspace rules.
Do not copy Grok vendor metadata as a new Ask public contract.

## Prompt, concurrency, and cancellation

`prompt` looks up a session and reports `invalid_params` for an unknown ID (`acp_agent.rs:1032–1054`).
It also has model availability gates that can return `EndTurn` without sampling (`:1055–1065`, `:1140`).
It validates vendor `outputSchema` and tool override input before dispatch (`:1359–1385`).
The send-now path sends `SessionCommand::Prompt` with a per-prompt oneshot result sender (`:1394`).
The normal path creates a human delivery envelope and sends it to the session queue (`:1418–1454`).
It releases the dispatch lock before waiting for the turn result (`:1460–1475`).
Thus a pending prompt response must not block the connection reader or cancellation dispatch.

`cancel` finds the session and enqueues `SessionCommand::Cancel` under its session dispatch lock (`acp_agent.rs:1878–1946`).
It returns `Ok(())` even when no session exists.
Its vendor metadata controls subagent cancellation, rewind behavior, and trigger classification.
Its immediate return proves command dispatch, not turn termination.
The eventual prompt result carries cancellation.
A Go adapter should keep a per-turn context cancellation function and settle the prompt result exactly once.
Define Ask's overlapping-prompt policy explicitly; Grok's queue and send-now policy is substantial runtime behavior, not simple wire glue.

## Auth and model operations

`authenticate` enforces configured preferred auth methods and administrator policy (`acp_agent.rs:635–682`).
The API-key branch loads existing configuration or environment credentials and can return `auth_required` (`:681–704`).
Other branches implement Grok login flows; those are not reusable Ask auth semantics.
Use Ask's existing native auth service and return supported method IDs rather than inventing a second credential store.
`set_session_model` delegates to `set_model_gated` (`acp_agent.rs:1966`).
`set_session_mode` sends a session actor command and waits for its oneshot response (`:1948–1964`).
These operations target the session rather than mutate one process-wide inference model.

## Event mapping and reverse calls

Reasoning maps to `AgentThoughtChunk` containing a text content block in `crates/codegen/xai-grok-shell/src/session/acp_session_impl/sampling_events.rs:4`.
`updates.rs:575–594` distinguishes assistant text, thought chunks, tool calls, and tool call updates.
`updates.rs:251–325` creates `SessionNotification`, stamps session ID and optional chunk/prompt IDs, then sends it into the session event pipeline.
`updates.rs:354–378` emits buffered ACP notifications through the direct persistence/forward path, while vendor tool delta chunks use extension notifications.
The forward path does not wait for a meaningful notification ACK.

`crates/codegen/xai-acp-lib/src/gateway.rs:239–301` dispatches agent-to-client permission, file, terminal, and extension methods.
The file adapter includes the correct session ID in every request (`crates/codegen/xai-grok-workspace/src/file_system/adapter.rs:22–49`).
Permission prompts include session ID, tool update, and supplied options (`crates/codegen/xai-grok-workspace/src/permission/prompter.rs:771`).
The caller waits for `Selected` or `Cancelled`; unknown outcomes and request failures become errors (`:782–799`).
A Go adapter must continue reading responses while a turn waits for a permission response.
Only offer client file and terminal operations when their advertised capabilities permit them.
The gateway's support for a method is not itself proof that the client has enabled that method.

## Turn completion and retries

`crates/codegen/xai-grok-shell/src/session/acp_session_impl/turn.rs:1686–1736` maps completed turns to `EndTurn`, `Refusal`, or `MaxTokens`; stationarity to `EndTurn`; and cancellation or max-turn limits to `Cancelled`.
It freezes prompt usage and persists live usage first (`:1679–1682`).
Errors preserve attached prompt usage (`:1739`).
The outer prompt handler awaits the actor result and returns the ACP prompt response (`acp_agent.rs:1468`); queue removal also returns `Cancelled` (`:1514`).
Do not interpret the last text chunk or a completed tool call as turn completion.

The runtime retries sampling failures within the same prompt, not JSON-RPC calls.
`turn.rs:3050–3064` passes step and prompt retry counters into the sampler.
`turn.rs:3123–3168` handles transient retry outcomes, computes jittered backoff, updates prompt totals, emits vendor retry status, then resubmits.
`turn.rs:3170–3174` resets transient counters after compaction.
`turn.rs:3176–3298` contains separate auth-recovery incident budgets, suspend handling, uncharged submissions, and exhaustion handling.
These are inference policies and should remain with Ask's Agent/provider layer.
Do not retry an ACP prompt after a transport failure unless its admission and completion state is known.
The exact retry ceilings were not fully audited in this bounded trace.

## Channels, errors, and EOF limits

`crates/codegen/xai-acp-lib/src/channel.rs:30–35` creates two unbounded internal message channels.
`channel.rs:38–66` allocates one oneshot per request and distinguishes send failure from dropped response sender.
`gateway.rs:156–183` spawns each dispatched request; receiving a message does not await handler completion.
`gateway.rs:102–108` also uses an unbounded gateway channel.
`gateway.rs:314–355` offers completion receivers and fire-and-forget enqueue results.
`gateway.rs:438–461` returns `Ok(())` for outbound notifications even if the enqueue failed.
This avoids a stalled stream but does not guarantee delivery and does not bound internal memory.
For Go, use one serialized output writer, bounded queues, typed pending-call channels, and cancellation-aware wait paths.
Do not hold a session lock across reverse-call waits or write waits.

`crates/codegen/xai-acp-lib/src/stdin_reader.rs:71–112` has a 64-line bounded channel and a dedicated blocking stdin thread.
Its `forward_lines` (`:185–204`) ends on EOF or read error and drops the sender.
The line-reader stage still uses an unbounded `read_until` allocation before downstream capping.
`crates/codegen/xai-acp-lib/src/line_reader.rs:28` defines a 64 MiB line cap and `:59` uses a bounded line channel.
The cap therefore does not cover the first blocking stdin allocation.
Apply the Go frame limit at the first reader, with an explicit large-message policy.

Comments in `line_reader.rs:1` and `normalize.rs:1` still describe ACP 0.6 defects.
The actual installed dependency is 0.10.4.
`normalize.rs:86` has a regression test that confirms the newer borrowed-or-owned envelope accepts escaped slashes unchanged.
Do not port stale workarounds without reproducing the defect against the chosen Go SDK.
Malformed-line behavior remains owned by the upstream SDK; this research did not inspect that dependency's full parser.

## Test evidence and limits

No tests were run.
Existing source tests support further verification:

- `xai-acp-lib/src/channel.rs:89` and `:102`: send failure versus dropped response sender.
- `xai-acp-lib/src/gateway.rs:560`: notification completion drain ordering.
- `xai-acp-lib/src/gateway.rs:599`: replay-before-response and live-update cutover.
- `xai-acp-lib/src/stdin_reader_tests.rs`: dedicated stdin reader and persistent stream behavior.
- `xai-grok-shell/src/session/acp_session_tests/turn_completion_emit_tests.rs:641`: persisted cancellation completion.
- `xai-grok-shell/src/session/acp_session_tests/cancel_running_task_tests.rs:2413`: cancellation reaches sampler and stops further output.
- `xai-grok-shell/src/session/acp_session_tests/turn/transient_retry_loop_tests.rs`: transient runtime retry behavior.
- `xai-grok-shell/src/session/acp_session_tests/turn/auth_retry_budget_tests.rs`: auth retry budgets.
- `xai-grok-shell/src/session/acp_session_tests/reverse_request_session_id_tests.rs`: reverse request session identity.
- `xai-grok-shell/src/agent/mvp_agent/tests.rs:1320`: session model switch isolation.

For H13a acceptance, a real stdio client should initialize, create a session, prompt, receive text/tool notifications and final stop reason, answer a reverse permission call, cancel an active turn, reject bad IDs, and close stdin during active work.
Include two concurrent sessions and a slow reader to check isolation and backpressure.
These are recommendations from source behavior, not executed results or an implementation plan.


## Completion rails: standard response versus vendor signals

`agent/mvp_agent/acp_agent.rs:1533–1566` emits `x.ai/session/prompt_complete` after it receives the actor result.
It derives the payload from the same mapped stop reason or error and forwards it without waiting.
The handler then continues through trace collection and returns the standard ACP prompt response (`:1606` onward).
Thus this vendor notification supplements the standard V1 response; it is not an immediate prompt acknowledgement and is not required for a standard ACP client.
Do not promise that fire-and-forget enqueue order proves a wire delivery barrier.

`session/acp_session_impl/run_loop.rs:2326–2332` flushes the replay buffer before completion handling emits the durable terminal.
`session/acp_session_impl/turn_end.rs:329–342` treats `TurnCompleted` as the durable counterpart for reconnecting viewers.
`turn_end.rs:387–420` uses the same outcome mapping and writes the vendor notification with `AppendDurability::Durable`.
Its finalization lease prevents duplicate terminal delta publication (`:422`).
These are three separate contracts: standard request response, transient vendor completion notification, and persisted replayable vendor completion event.
For a standard ACP client, the standard prompt response and ordered standard updates are the basic completion contract.
Accepted H13a scope also includes Follow, retry, queue, and settled extensions; the basic standard contract alone does not meet that scope.
The Go adapter must project those existing Ask behaviors as agreed Ask extensions without copying Grok persistence.

Unresolved questions: exact Go SDK selection and schema pin; Ask's overlapping-prompt policy; frame-size policy; required client reverse capabilities; EOF durability requirement.
