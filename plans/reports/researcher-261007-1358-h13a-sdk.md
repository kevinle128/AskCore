# H13a SDK and schema selection evidence

Research date: 2026-10-07.
Scope: planning only for `kk:plan --deep --tdd`.
No dependency was installed, and no conformance test was executed.

## Decision

Keep D16: ACP JSON-RPC, `_ask/*`, and `_meta`.
Keep D17 open until the Phase 1 executable gate passes.
Use coder v0.13.5 as the first tested baseline because pinned source was available.
Do not select a fork from README claims or infer schema identity from a Go module tag.

The roadmap names coder v0.13.5, lx-wnk v1.21.0, and Caelis v1.4.0.
These are candidate pins, not verified schema pins.
Record module path, module version, module sum, tag commit, schema tag, schema commit, asset digest, and stable/unstable inputs separately.

## Primary source findings

| Candidate | Verified evidence | Limit |
|---|---|---|
| coder v0.13.5 | Manifest has module `github.com/coder/acp-go-sdk` and Go 1.21; pinned connection and generated types were readable | Schema release provenance and tag commit were not resolved |
| lx-wnk v1.21.0 | Roadmap candidate only | Current manifest, tag identity, connection and schema were not accessible in this research |
| Caelis v1.4.0 | Pinned go.mod confirms module `github.com/caelis-labs/acp-go-sdk`, Go 1.23, and x/sys v0.29.0 | Schema identity, connection behavior, tag commit and independent CI results were not resolved; published claims are not conformance proof |

[coder manifest](https://raw.githubusercontent.com/coder/acp-go-sdk/v0.13.5/go.mod).
[Caelis repository](https://github.com/caelis-labs/acp-go-sdk).
[Caelis pinned manifest](https://raw.githubusercontent.com/caelis-labs/acp-go-sdk/v1.4.0/go.mod).
[lx-wnk repository](https://github.com/lx-wnk/acp-go-sdk).

coder's pinned connection dispatches requests concurrently and notifications in a bounded ordered queue.
Its response watermark waits for inbound notifications that precede an inbound response.
This is not an Ask outbound delivery watermark.
The reader uses a 10 MiB Scanner limit.
`$/cancel_request` bypasses notification ordering; `session/cancel` does not.
Writes are serialized, but one Write ignores the returned byte count.
Inbound-handler response write errors are discarded.
EOF cancels pending calls, closes the notification queue, and permits a bounded drain.
The application must check blocked-writer shutdown and stream ownership.
Source: [connection.go](https://raw.githubusercontent.com/coder/acp-go-sdk/v0.13.5/connection.go), lines 345–581 and 584–619.

Generated types contain `Meta map[string]any`, standard `usage_update`, and configuration-option updates.
They also include explicitly unstable variants and methods.
A union marshal path decodes payloads through `map[string]any`.
Thus JSON integers above 2^53 in metadata require an executable preservation check.
Do not confuse field presence with a lossless round trip.
Do not implement experimental interfaces or advertise their capabilities merely because the generated API contains them.
Source: [types_gen.go](https://raw.githubusercontent.com/coder/acp-go-sdk/v0.13.5/types_gen.go), lines 4329–4337, 5616–5643, and 8972–9076.
[agent dispatch](https://raw.githubusercontent.com/coder/acp-go-sdk/v0.13.5/agent_gen.go) is another required Phase 1 inspection surface.

The roadmap's claim that coder has no release or reply since June comes from a fork README.
This research did not verify repository release timestamps, maintainer replies, or CI history.
Do not repeat that claim as a fact.
A maintenance comparison must use tag commits, release dates, issue/PR replies, and actual workflow results at the selected revision.

## Current protocol boundary

ACP v1 keeps the prompt request open until the turn finishes.
Cancellation requires aborted work and pending updates before a cancelled prompt result.
Current v1 includes configuration updates and cumulative session usage.
Per-attempt accounting is a separate Ask contract.
Source: [v1 prompt turn](https://agentclientprotocol.com/protocol/v1/prompt-turn).

The v2 documentation still labels the overall surface draft.
Prompt returns admission; state updates carry later completion.
It changes capabilities and removes client filesystem, terminal execution, and mode methods.
H13a must serve v1 unless the accepted roadmap changes.
Source: [v2 migration](https://agentclientprotocol.com/protocol/v2/migration).

`session/cancel` cancels a session turn.
`$/cancel_request` targets a JSON-RPC request ID and is cooperative.
An SDK context cancellation does not by itself prove that Ask stopped a run or drained pending updates.
Source: [request cancellation](https://agentclientprotocol.github.io/rust-sdk/request-cancellation.html).
Extensions use underscore-prefixed methods and metadata.
Source: [v1 extensibility](https://agentclientprotocol.com/protocol/v1/extensibility).

## Phase 1 executable selection gate

Run this gate before production dependency selection.
Use a temporary independent consumer module and pipe/subprocess peers.
Keep it outside Ask's production module until selection.
Test every viable candidate against the same fixtures.
A unavailable candidate must remain unresolved; do not fabricate a result.

1. Resolve immutable module and schema identities.
Verify the generated source matches its declared stable schema and identify any unstable input.
Build the minimal required Agent implementation and extension handler.
2. Round-trip `_ask/*` requests, responses, notifications, errors, escaped method names, and unknown extension notifications.
Test `_meta` at initialize, request, response, nested content, and session-update payloads.
Include null, nested objects, arrays, unknown keys, and integers above 2^53.
Use raw JSON or explicit decimal strings for precision-sensitive Ask counters if the selected API cannot preserve numeric values.
3. Hold Prompt open while reverse permission replies and session cancellation arrive.
Prove that the reader remains active and cancellation reaches the right session.
Separately test request cancellation by string and numeric request ID, unknown IDs, and cancellation races.
State the expected JSON-RPC error versus v1 cancelled stop-reason behavior.
4. Block the notification callback, fill its queue, and send control traffic.
Prove bounded memory or explicit connection failure.
Record callback order, terminal update order, and prompt response order.
Do not let an incoming SDK watermark replace Ask's final-run output barrier.
5. Inject short writes, write errors, blocked writes, peer EOF, partial frames, oversized lines, invalid JSON, and duplicate IDs.
Verify error propagation, no successful response after lost terminal output, and bounded exit.
Set and test a first-ingress byte cap; do not allocate the entire line before checking it.
6. Test standard usage and configuration DTOs against the pinned schema.
Verify unsupported methods return explicit errors and unimplemented capabilities remain absent.
Run race checks and goroutine-leak checks for this gate.
Capture the exact command, pins, exit status, and frame trace in its report.

Gate acceptance: all required Ask contracts pass, or each SDK limitation has a small tested adapter fix with explicit ownership.
Generate private types only if the tested candidates fail and existing extension/raw JSON hooks cannot meet the contract.
Do not write a second JSON-RPC engine before proving that need.
H13a proves stdio; the later H13b gate must repeat routing and cancellation through the leader.

## Evidence limits and unresolved questions

Shell HTTP reads failed because DNS/network access is restricted.
Several web raw-source reads returned internal errors.
No maintenance ranking or fork conformance claim is supported by executed evidence.
Unresolved: exact schema identities for all candidates, current fork tag commits, metadata precision behavior, writer failure lifecycle, and cancellation under queue pressure.
