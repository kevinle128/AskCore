# `internal/acp`

The Agent Client Protocol adapter over the agent's Go API. It is the server side: it lets clients (the leader, an editor over stdio) talk to the agent. It is not `internal/providers/acp`, which is the client side and runs other coding agents as subprocess providers. ACP plus the `_ask/*` methods is the only protocol between the agent process and anything outside it. Inside the process, packages call each other as Go code.

## What belongs here

- The implementation of the ACP `Agent` interface on top of the `agent` Go API (`agent.go`): `Adapter`, its `Config` callbacks and the connection binding
- The session host (`host.go`): one Agent and one ordered writer for each session, and the write barrier that holds a prompt result until the settled event is written
- The mapping from internal events to `session/update` notifications and the follow subscriptions (`updates.go`)
- `_meta` fields for Ask-only data such as `seq`, `epoch` and `runId` (`meta.go`)
- The `_ask/*` method handlers: state, models, controls, steer and follow-up, usage; compact, fork and tree answer "unsupported" (`ask_methods.go`)
- The one error mapper (`errors.go`)
- The stdio server for `ask acp` (`stdio.go`)
- The route context of the shared leader link: parsing, the driver generation guard, `take` and the quiesce check (`route.go`, `host.go`)
- The stdio guards `wire.go`: `CheckedWriter` turns a lost write into a connection failure, and `LineLimitReader` caps one inbound frame before the SDK scanner buffers it

## What does not belong here

| Code | Put it in |
|---|---|
| Frame, method and `_meta` type definitions | `pkg/protocol` |
| The agent loop | `internal/agent` |
| Subprocess agents as providers | `internal/providers/acp` |
| Socket routing | `internal/leader` |

## Main interfaces

- Implements the ACP `Agent` interface from the ACP Go SDK

## File names

`agent.go`, `host.go`, `updates.go`, `meta.go`, `ask_methods.go`, `errors.go`, `stdio.go`, `wire.go`, `route.go`

## Imports

- Allowed: `agent`, `sessions`, `bus`, `providers` (model types and `Ref` only), `tools` (tests only), `pkg/protocol`, the ACP Go SDK (`github.com/coder/acp-go-sdk` v0.13.5, stable schema 0.13.5, ACP wire v1; the repository holds a copy with one patch, see below)
- Denied: `internal/leader`; `internal/gateway`, `internal/http`, `internal/channels/<vendor>`; `internal/config`

## The SDK copy

`go.mod` replaces `github.com/coder/acp-go-sdk` with `third_party/acp-go-sdk`: upstream v0.13.5 with one change. The scanner of the SDK stops a line at 10 MiB. The leader carries messages of up to 64 MiB, so the copy raises the limit to 65 MiB plus one byte. [PATCH.txt](../../third_party/acp-go-sdk/PATCH.txt) records the source, the licence (Apache-2.0), the change and the steps to update it. `TestSDKAcceptsLeaderLineLimit` fails if the patch is lost. The conformance tests still pin the version and the schema hashes. `ask acp` keeps its own 8 MiB input guard.

## SDK contract limits

The conformance tests in `conformance_test.go` pin the SDK and its schema, and record these limits. The report is [conformance-261007-h13a-sdk.md](../../plans/reports/conformance-261007-h13a-sdk.md).

- Wrap the SDK output in `CheckedWriter` and the input in `LineLimitReader`. The SDK ignores short writes and discards the error of a response write.
- Do not block in a notification handler: `session/cancel` is queued behind it. `$/cancel_request` and requests are not.
- Map every error before it returns. The SDK puts the text of an unmapped error into the error data.
- Send Ask counters as decimal strings. Typed `_meta` decodes numbers as float64. Extension params are raw JSON and stay exact.
- `usage_update` is not in the stable schema. Report usage with `_ask/session/usage`.

## Adapter contract

- Dependencies from the application come as function fields of `Config` (`Authenticate`, `Models`, `FindModel`, `ModelAuth`). The package never imports `auth`, `app` or `config`. A callback error is shown as "not signed in"; its text never leaves.
- `Bind(conn, checkedWriter, out)` connects the SDK connection. The adapter learns from the checked writer when a follow result is on the wire, and it closes `out` when the output fails. The owner of the writer calls `Fail` from the failure hook of the writer and `CloseOutput` on a signal path.
- Every error goes through `requestError`: a fixed text for each kind from `pkg/protocol` and the kind in the error data. Only a short failure code of a model request is added to the text.
- `session/prompt` and `_ask/session/continue` return after the run settled and every update of the run was written. A request that the Agent refuses (busy, disposed, empty log) returns at once and never waits for the events of another run.
- `session/cancel` calls `Agent.Abort`. A cancelled request (`$/cancel_request`, or a second prompt on the same session) does not stop a run and does not release the write barrier. The run ends by `session/cancel` only, and a started tool body drains without a time limit.
- While a reset runs, steer and follow-up answer busy and admission stays closed until the held events are queued, so every event carries the epoch it belongs to and leaves in Agent order.
- A `session/cancel` that comes after admission but before the run exists is kept and stops the run when it binds. A prompt waits up to 250 ms for an Agent that is busy with work it does not own (a queue_update delivery or a run that a queued input started); a longer run still gives busy.
- Repeating `_ask/session/unfollow` succeeds after the subscription has ended. A subscription of another session gives `unknown_subscription`.
- A model request that fails gives a failure frame after its updates: `no_api_key` for an AUTH failure, `internal` with the failure code for any other.
- Model names are `provider/id@api`. Two catalog rows can share provider and id, so a name without `@api` that matches both is refused.
- An explicit follow keeps its own follower. The result holds the cut (snapshot, open stream baseline, cursor). Events above the cut leave only after the result is on the wire. A follower that falls behind, or meets an event over the ring size limit, ends with `_ask/session/resync`; the client follows again and nothing is sent twice.
- Standard `session/update` frames are written by the session writer, so an explicit follow does not duplicate them.
The writer waits for each active subscription to write through the same event sequence, or to end with detach/resync, before it advances the prompt barrier.
- Usage rows come from the committed `AttemptSettled` entries of the session log. A row has the outcome and the usage, never the failure text.
- Follow entries leave out request deltas and system snapshots.
- A follower gets Agent events as the Agent publishes them. Those events, and the message entries of a snapshot, hold the cleaned failure text of a model request, as the headless JSON stream does. The adapter does not rewrite them. Any redaction for a wider audience belongs to the stdio and gateway composition.

## Leader link

The shared host behind `ask leader` uses the same `Adapter` with `Config.RequireRoute` set. `internal/app` builds the byte link; `internal/leader` is the router on the other end and never imports this package.

- **Route context.** Every call on the link carries `_meta["ask.dev/route"]` (client, the session that the router checked, driver generation as decimal text, live-driver flag, driver capabilities). The host refuses a call whose own session id differs from the route, and on the leader link a call without the session in the route. `route.go` reads it for standard calls and for `_ask` calls (checked once in `HandleExtensionMethod`). A route that is present but not valid is refused on every link, so it never falls back to the editor path. The editor link has no route and no generation check, and its results carry no `_meta`.
- **Driver generation.** The host is the only place that changes it. A session starts at generation 1. `Session.guard` checks the generation at admission and counts the change as in flight.
A separate session commit lock checks it again and orders the actual mutation against `take`, including cancel.
Prompt and continue check at Agent run admission, and set model checks after model/auth readiness.
The commit lock is released before the run or tool drain; Agent listeners never acquire it.
`session/cancel` is dropped when its generation is old.
`_ask/session/take` commits under the same lock and returns the new generation: with a live driver it needs an idle session (otherwise `busy`), without one it works at any time and the run goes on.
- **New session.** With a route, `session/new` stores the capabilities of the creator and returns the generation in the result `_meta` for the router.
- **Idle.** `Adapter.QuiesceIfIdle` closes admission of new sessions and new changes, only when no session is being built, no authentication check is active, no run is active, no input is queued and no change is in flight. It holds the admission lock while it looks for work, so no work is admitted during the check, and a false answer leaves the host exactly as it was. `ActiveRuns` counts the sessions that are not idle. `Adapter.Session` gives the composition a live session.
- A client leaving the router never reaches `Adapter.Fail`, `Adapter.Close` or the host.

## Stdio server

`ServeStdio` ([stdio.go](stdio.go)) serves one connection over a reader and a writer. `cmd/tui/acp.go` registers it as `ask acp`, before prompt and mode parsing, with the composition of `internal/app.ACPModule`. The command takes no other argument. There is no TCP or Unix listener.

- One framed reader owns stdin and one checked writer owns stdout. Diagnostics go to stderr only, with no raw input and no attribute (`NewQuietLogger`). An inbound frame over 8 MiB ends the connection.
- Authentication uses the credentials that `ask auth login` saved on the host, or the environment key. `authenticate` and `_ask/session/set_model` with `authMethodId` check readiness and refresh an expired native credential through the normal resolver. They never read stdin, never start a sign-in and never change a credential. A mismatch answers `no_api_key` with the text that names `ask auth`.
- Shutdown runs on end of input, on a lost output and on SIGINT, SIGTERM or SIGHUP. It closes admission, disposes the host (an ordinary cancel keeps the unbounded drain of a started tool), runs the owned cleanup (`AuthWait`) in parallel, waits for the active handlers, and then closes the output. Exit codes: 0, 1, 130, 143, 129.
- A signal closes the output first, so a blocked write ends and no prompt result can follow. A second signal returns at once with `Forced` set and the command prints that the cleanup did not drain; it never reports completed work.
- The server keeps the ID of each JSON-RPC request from the input line, including a final line without a newline, and waits until its response frame is written, so end of input never closes the output before a response. During shutdown a single output write that runs longer than `WriteStall` (default 2 s) closes the output; an ordinary tool drain is not bounded by it.
- The host never gets a success after a lost output: the failure is latched by the checked writer.
- A model failure text reaches `_ask/session/event` as the provider layer cleaned it. The stdio tests send a credential in a provider error body and check that no credential appears on any frame or on stderr.
- The SDK sets its logger when it is bound, and reads at once. The server holds the first read until the bind ends, so an input that fails at once cannot race with the logger.

Tests: `stdio_test.go` uses real pipes and Agent owners with the approved controlled tool and faulting log.
`cmd/tui/acp_e2e_test.go` drives the built binary for every public operation.
Provider and OAuth tests use an external HTTPS fixture through a CONNECT proxy and a temporary CA file.
The tests use production HTTP clients, supported host login, real retry delays, and real credential expiry.

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- Hooks never travel over ACP. ACP has no sync-hook concept.
- Headless mode does not use this package. It calls the `agent` Go API directly.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
