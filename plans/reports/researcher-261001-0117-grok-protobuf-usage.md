# Grok Build: where protobuf, gRPC and binary formats are used

Repo: /Users/dale/Desktop/workspace/opensources/grok-build, commit 2bdd1d6a. All paths below are relative to `crates/` unless marked otherwise. Date: 2026-10-01.

## Outcome

1. Grok's agent protocol is ACP over JSON. Nothing on the user-turn path uses protobuf or gRPC. The local leader, the relay to grok.com and the pager-to-agent link are all JSON text. The model call is HTTP with JSON and SSE.
2. The repo has exactly one product `.proto` file: `codegen/xai-grok-tools-api/proto/grok-tools.proto`. It defines two services, `GrokToolsService` (line 11) and `GrokToolsCallbackService` (line 189). In this open-source tree, no code implements or calls either service. The generated tonic client and server stubs are compiled and never referenced. Only the generated message types are used, and they are used as serde JSON structs, not as protobuf bytes.
3. The real protobuf on the wire is OpenTelemetry OTLP (http/protobuf by default, optional gRPC). It carries telemetry only. It is never on the turn path.
4. The Computer Hub, the tool server and the remote workspace use WebSocket text frames with JSON-RPC 2.0. The only protobuf inside them is base64 OTLP blobs in `*.donate` notifications.
5. The only true binary frames are audio chunks in the voice STT WebSocket, plus the 4-byte big-endian length prefix on leader frames. The payload after the prefix is JSON. I found no CBOR, bincode, MessagePack, flatbuffers, Cap'n Proto, postcard or rkyv anywhere. The grep returned nothing in any `Cargo.toml` or `.rs` file.
6. Several crates list `tonic` or `prost` in Cargo.toml but never use them in source. See "Dead or stale dependencies" below.

Answer for AskCore: keep ACP JSON-RPC as the only external agent protocol. Grok's own design confirms this choice. See the last section.

## Map

| Area | Format | Peer | Hot path of a user turn? | Evidence |
|---|---|---|---|---|
| ACP agent protocol (pager/editor to agent) | JSON lines: each message is written to the agent with a `\n` | Local process | Yes | `codegen/xai-grok-shell/src/agent/app.rs:985-1008` (`ipc_to_agent_rx` and `ws_to_agent_rx` both `write_all(msg)` then `write_all(b"\n")`) |
| Leader IPC (Unix socket) | 4-byte big-endian length prefix, then JSON (`serde_json`). Max size 64 MiB. | Local process | Yes, when the leader mode is on | `codegen/xai-grok-shell/src/leader/protocol.rs:9-10,37,83,125,133` |
| Relay to grok.com | WebSocket. Text frames are parsed with `serde_json`. Binary frames are accepted only if they are valid UTF-8, then handled as text. Non-UTF-8 binary is dropped. The decoded message goes into the same ACP channel. | xAI cloud | Yes, when the relay is enabled (`--relay-on-demand` or always on) | `codegen/xai-grok-shell/src/agent/relay.rs:513-520,557-572,639`; channel wiring `app.rs:395,581-610` |
| Model inference (sampler) | HTTP POST with JSON bodies, response as SSE (`text/event-stream`). Endpoints `chat/completions` and `/v1/responses`. Uses `reqwest`, `async-openai` and `eventsource-stream`. | xAI API, or any OpenAI-compatible base URL | Yes (the core of the turn) | `codegen/xai-grok-sampler/src/client.rs:6,958,1099,2427`; `codegen/xai-grok-sampler/Cargo.toml:16,21,24` |
| Tool proto: `GrokToolsService` and `GrokToolsCallbackService` | Protobuf over gRPC, defined but not wired in this tree | None in this tree | No | `codegen/xai-grok-tools-api/proto/grok-tools.proto:11,189`; no reference to the generated client or server anywhere (grep) |
| Tool proto messages as data (`ToolConfigEntry` and others) | Generated prost structs with `serde` derives, used as JSON | xAI backend, hub `session.bind` metadata, agent-config JSON | Indirectly (tool config at session start) | `codegen/xai-grok-tools-api/build.rs` (serde derive on every type, `#[serde(default)]` per field); `tests/wire_shape.rs:1-8,40-55` |
| Computer Hub / tool server / harness SDK | One WebSocket per (url, principal). Text frames carry JSON-RPC 2.0. | xAI hub, or a local `workspace_server` | Only when tools or a workspace run through the hub (remote sandbox, cursor worker) | `common/xai-computer-hub-sdk/src/lib.rs:1-20`; `connection.rs:50,1578,1600,1700,1719` (`Message::Text`); `common/xai-tool-protocol/src/lib.rs:4` |
| OTLP donation inside the hub (traces, logs, metrics) | JSON-RPC notification whose `otlp_request` field is base64 of a protobuf `Export*ServiceRequest`. Cap 1 MiB. | Hub | No (background) | `common/xai-tool-protocol/src/frames.rs:66-115`; `common/xai-computer-hub-sdk/src/trace_donate.rs:18,61-66,181` |
| Remote workspace RPC (`workspace.*`) | JSON over the hub. Types are in `xai-grok-workspace-types`. | Hub / remote workspace server | Only in remote workspace mode | `codegen/xai-grok-workspace-client/src/lib.rs:7-9`; `codegen/xai-grok-workspace-types/src/lib.rs:26-50` |
| Telemetry, external OTLP (user-configured) | http/protobuf (`Protocol::HttpBinary`) or gRPC via tonic | User's own collector | No | `codegen/xai-grok-telemetry/src/external/providers.rs:406,414-432,456,463-483`; `config.rs:143` |
| Telemetry, internal OTLP traces | http/protobuf, pinned | xAI | No | `codegen/xai-grok-otel/src/provider.rs:129` |
| Product analytics | Mixpanel HTTP (JSON) | Third party | No | `codegen/xai-grok-telemetry/src/config.rs:132-133,189` |
| Distributed trace propagation | gRPC client helpers (trace metadata on `tonic::Request`) | No caller found | No | `common/xai-tracing/src/grpc_client.rs:4-5,125`; `fastrace.rs:56-60` |
| Retry policy for gRPC status codes | Classifier over `tonic::Code`, behind a feature flag | No caller found | No | `common/xai-circuit-breaker/src/grpc.rs:1-50`; `Cargo.toml:10` (feature); grep found no user outside the crate |
| Voice STT streaming | WebSocket. Audio is sent as `Message::Binary` (raw audio chunks). Control messages are JSON text (`{"type":"audio.done"}`). | `wss://api.x.ai/v1/stt` | No (side channel for dictation) | `codegen/xai-grok-voice/src/stt/streaming.rs:26,82,87,94` |
| Voice clip transcription | Host-provided `ClipTranscriber` trait. The wire format is not in this crate. | xAI (by doc) | No | `codegen/xai-grok-voice/src/transcriber.rs:1-3,52` |
| Privacy mode | Stored as the protobuf enum number in an `i32`. No protobuf bytes. | xAI backend (source of value) | No | `codegen/xai-grok-config-types/src/privacy.rs:6-9` |

## Ring 0: sources of protobuf and binary formats

- Product `.proto`: one file, `codegen/xai-grok-tools-api/proto/grok-tools.proto`. Two more `.proto` files are test fixtures of the codegen helper: `build/xai-proto-build/test_data/debug_redact_plain.proto` and `debug_redact_test.proto`.
- No checked-in `*.pb.rs` files. Code is generated at build time into `OUT_DIR` and included at `codegen/xai-grok-tools-api/src/lib.rs` (`pub mod pb` with `include!(concat!(env!("OUT_DIR"), "/xai.grok.tools.v1.rs"))`).
- `build/xai-proto-build/src/lib.rs:347-355` is the wrapper around `tonic_prost_build::configure()`. It keeps tonic's defaults (client and server stubs on). It can also run `pbjson_build` for canonical proto-JSON, but `gen_pbjson` defaults to false and the tools-api `build.rs` does not turn it on. Instead it adds plain serde derives.
- Only one `build.rs` calls the proto builder: `codegen/xai-grok-tools-api/build.rs`. The other `build.rs` files (shell, tools, pager-bin, version, markdown) do not compile protos. I confirmed by finding no `include_proto`/`compile_protos` anywhere else. Inference: they do version stamping and similar.
- `#[derive(prost::Message)]` by hand: no hits. All message types come from generation.
- Other formats (CBOR, bincode, MessagePack, flatbuffers, Cap'n Proto, postcard, rkyv): none in any manifest or source file.
- `common/xai-tool-protocol/generated/{kotlin,swift}` holds client bindings for the JSON-RPC protocol, for example `BotRelayProtocol.swift`. Inference: these are hand-checked-in or schema-derived JSON types, not protobuf.

## Ring 2: GrokToolsService in depth

- Who serves it? Nobody in this tree. No `impl GrokToolsService for ...` exists, and no `*Server`/`*Client` symbol from the generated module is referenced outside `xai-grok-tools-api`. The crate doc says it is "used by both the tools library and the gRPC server" (`xai-grok-tools-api/src/lib.rs:1-5`), so the server exists in a closed-source xAI service. Inference: a hosted backend that wraps the tools runtime, and hosts `GrokToolsCallbackService` on the client side (the proto says "client-hosted callback service", `grok-tools.proto:245`).
- Who is the client? Also not in this tree. The callback service doc (`grok-tools.proto:180-194`) says a host process receives `SendNotification` and `SpawnSubagent` from the Rust tool server. Inference: the host is a closed-source xAI service, not the CLI.
- Normal CLI/TUI flow? No. The CLI uses `xai-grok-tools` as an in-process library. The `tonic` dependencies in `xai-grok-tools` (`Cargo.toml:81`) and `xai-grok-workspace` (`Cargo.toml:101`) are declared, and my grep found no `tonic` use in their `src/`. Also `xai-grok-tools-api/Cargo.toml:22` lists `tonic` under `ignored`, which marks it as an unused-dependency exception.
- `proto_convert.rs` (`codegen/xai-grok-tools/src/registry/proto_convert.rs:1-35`): pure type conversion from wire `ToolConfigEntry` to the runtime `ToolConfig`, plus validation (`parse_params_json`, `validate_name_override`). It is not an RPC path. No network code in it.
- `tests/wire_shape.rs:1-8`: "wire shape" means the serde JSON shape of `ToolConfigEntry`. The test pins the JSON so a proto field rename cannot break "session-bind metadata and backend JSONB config storage", where producer and consumer are separate services. So the proto file is used as a schema source for a JSON contract, not as a byte format.
- A comment in `codegen/xai-grok-workspace/src/session/tool_config.rs:908` calls it "the gRPC `ToolConfigEntry`", which confirms the origin is the gRPC server contract even where JSON carries the type.

## Ring 3: the model API

- HTTP and JSON with SSE. The sampler POSTs to `chat/completions` (`client.rs:958,1096`) and `/v1/responses` (test server at `client.rs:2427`), asks for `text/event-stream` (`client.rs:1099,1479,1812`) and parses with `eventsource-stream`. It depends on `async-openai` and `reqwest`, with no tonic or prost (`xai-grok-sampler/Cargo.toml:16,21,24`).
- The base URL is configurable (`client.rs:311,367`), which fits any OpenAI-compatible endpoint.
- A comment at `xai-grok-sampler/src/events.rs:181` mentions "gRPC adapters" as possible consumers of the error info. Inference: other hosts wrap the sampler in gRPC, outside this tree. The CLI itself does not.
- I did not see the xAI gRPC `xai-sdk` path in this repo.

## Ring 4: relay and remote agent

- The relay (`xai-grok-shell/src/agent/relay.rs`) is a WebSocket client to grok.com with reconnect and auth recovery. It only builds for a first-party grok.com session (`relay.rs:70`). Frames are JSON text, forwarded as-is into the same newline-delimited ACP stream that stdio uses (`app.rs:995-1008`). So a remote client speaks ACP JSON to the local agent through the relay. The relay code adds no other envelope that I found, but I read only the receive loop, not every send path. The agent-to-WS sender also uses `Message::Text` (`relay.rs:639`).
- `--relay-on-demand` (`xai-grok-pager/src/app/cli.rs:387`, `app.rs:581-610`): delays opening the relay until asked. It changes when the WebSocket opens, not the format.
- Computer Hub SDK: a tool-server and harness channel (`xai-computer-hub-sdk/src/lib.rs:1-20`). Harness sends `tool.call` and the hub routes `tool_call_request` to a tool server. It also carries `session.bind`, hooks, `servers.list`, notifications and OTLP donations. It is a tool and workspace routing channel for remote sandboxes (`workspace_server.rs:1`: "Standalone workspace ToolServer for remote sandboxes"). It is not the agent conversation channel, and it is JSON-RPC text with base64 protobuf only for telemetry.

## Dead or stale dependencies (inference from grep, not from a build)

- `xai-grok-pager`: `prost` is a dev-dependency with the comment "Decodes the clip-transcription request the voice tests capture off wiremock" (`xai-grok-pager/Cargo.toml:204-205`). I found no `prost::` use in the pager sources or tests. Inference: the voice clip upload to xAI is protobuf in the closed host code, and the test was removed or is missing. The only two `prost` text hits in pager sources are comments (`follow_ups.rs:14,28`) saying some notification payloads use prost-style snake_case keys, meaning an upstream proto type reaches the pager as JSON.
- `xai-grok-login`: `tonic`, `tonic-prost`, `prost` are optional (`Cargo.toml:68-71`) with the comment "gRPC stack for the gated login path". No feature in `[features]` enables them, and no source uses them. Dead in this tree.
- `xai-grok-tools`, `xai-grok-workspace`: `tonic` declared, unused in `src/`.
- `xai-tracing`, `xai-circuit-breaker`: real gRPC helper code, but no caller found in this tree.

## What this means for AskCore

Answers, with the reasoning:

1. Should any part of the Ask agent protocol use protobuf? No. Grok (the reference you are comparing to) speaks JSON on every turn-path link: ACP on stdio, length-prefixed JSON on the leader socket, JSON text on the WebSocket relay, JSON-RPC on the hub. Grok's own protobuf is server-side and telemetry-side. The one place protobuf matters on the agent side is a schema source for a JSON contract (`wire_shape.rs` pins the JSON shape because the proto is the origin of the type). That is a cost Ask does not need to pay. Fact: ACP is JSON-RPC 2.0 by spec, so a binary alternative would break editor compatibility.
2. Do not add a second wire format for the leader socket. Grok uses a 4-byte length prefix plus JSON for the Unix socket. Inference: that is the lowest-risk choice if Ask wants framing on the local leader. ACP over stdio uses newline-delimited JSON, so one framing choice per transport (newline for stdio, length prefix for socket, WebSocket text for remote) is enough. This is a design choice for Ask to confirm, not something I verified for ACP's own rules.
3. Where protobuf and gRPC do fit in Ask (AskCore already has `proto/`, buf and grpc-go):
   - Keep gRPC for the daemon's management and service surface (the `gateway/` gRPC services), which is outside the agent protocol: admin, sessions listing, health, TUI-to-daemon control if you want a typed API. Grok has no equivalent in this tree, so there is no reference to copy. This is inference.
   - Use OTLP (already protobuf, through the opentelemetry Go SDK) for telemetry. This matches Grok's exporters (`providers.rs:406-483`). You get it from the library and write no proto.
   - If Ask later needs a typed contract for a tool-server boundary like `GrokToolsService`, treat it as a separate, optional internal API. Grok defines one and then does not use it in the open-source CLI path, which is evidence that it is not needed for a local harness.
4. The one cautionary pattern from Grok: proto messages reused as JSON structs with serde (`xai-grok-tools-api/build.rs`). It needs a pinned-shape test to stay stable. If Ask does the same with buf-generated Go types and `protojson`, add a golden JSON test from day one. Do it only if a proto contract already exists for another reason.
5. Decision record suggestion: "ACP JSON-RPC is the only external agent protocol. gRPC/protobuf is allowed for internal service APIs and OTLP. No protobuf on ACP links." This keeps the user's decision intact; nothing in Grok's code contradicts it.

## Not covered

- MCP transports (`xai-grok-mcp`), the sandbox, sqlite session storage (`xai-sqlite-journal`), and the egress proxy were not read for binary formats.
- I did not build the project, so "generated but unused" stubs come from grep, not from a compiler report. I did not use GitNexus call graphs, because grep on the symbol names already returned no references.
- I did not read every send path of the relay or the hub handshake (`xai-tool-protocol/src/handshake.rs`).
- Closed-source xAI services (the `GrokToolsService` server, the relay server, the voice clip endpoint) are outside this tree, so their wire formats are inferred.

## Unresolved questions

1. Is the voice clip upload to xAI protobuf? The pager's `prost` dev-dependency comment says it is, but no test using it exists here.
2. Does the hub handshake or any `tool.call` path ever send `Message::Binary`? I saw only `Message::Text` sends in `connection.rs`.
3. Is the `GrokToolsService` server what the hosted grok.com product runs for remote tools? Not checkable from this repo.
4. Does Ask want a length-prefixed JSON framing on the local leader socket, or newline-delimited JSON like stdio ACP? Grok uses the former; ACP itself only defines stdio.
