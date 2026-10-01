# Pi secondary package triage for the Go rewrite (lane G)

Date: 2026-09-30. Pi repo: /Users/dale/Desktop/workspace/opensources/pi at 2bbfcca4, all packages at version 0.99.1. Method: READMEs, package.json, src trees, CHANGELOGs, `git log` per directory, and grep of real imports (not only package.json). GitNexus was not used; grep over the workspace was enough. LOC are TypeScript under `src/` only.

## Verdict first

1. The only secondary package that the shipping Pi agent already needs is `chord`, and only its small `Context` and `Delta` parts. `agent-core` imports `@earendil-works/chord`, `/context`, and `/delta` in 32 files. Go already has `context.Context`, so Chord's context needs no port.
2. `codemode` and `mcp` are shipped features (default `pi` CLI, added 2026-09-29). Both are extension-adjacent. `mcp` maps onto the existing AskCore `internal/mcp`. `codemode` is a new, optional capability.
3. `protocol`, `client`, `server`, and most of `chord` and `durable` form an experimental second architecture (durable sessions, replicated facet services, remote attach). It lives in `coding-agent/src/experimental`, is gated by `PI_EXPERIMENTAL=1`, and is excluded from the published build (`!dist/experimental` in `coding-agent/package.json`). It is out of scope now. AskCore already has its own equivalents (`gateway`, `pkg/protocol`, `sessions`, `store`).
4. `telemetry` is a small contract package; AskCore already has `tracing` + OpenTelemetry. `evals` is dev tooling. `sqlite-node` has zero consumers in the repo and AskCore has `store/gormstore` with SQLite.

## Classification table

| Package | LOC | First appeared | Class | AskCore target |
|---|---|---|---|---|
| chord | 8.8k | 2026-08-28 (foundation commit; no CHANGELOG file) | Split: Context/Delta = core-harness (trivial in Go); services/facets/bundler = out-of-scope | `agent`, `pipeline` use `context.Context`; rest none |
| durable | 16.6k | 2026-09-18 (moved from "Pico"); 0.86.0 2026-09-19 | out-of-scope (experimental, zero consumers) | Design reference for `sessions` + `store` + `scheduler` recovery only |
| codemode | 1.6k | 2026-09-29, 0.99.0 | extension-adjacent | New `tools/codemode*.go` later, or skip |
| server | 2.0k | 2026-07-21 (rename of orchestrator); CHANGELOG from 0.80.3 (2026-06-30) | out-of-scope | `gateway` (+ `gateway/methods`) |
| client | 1.1k | 2026-07-31; CHANGELOG from 0.84.0 (2026-08-06) | out-of-scope | `cmd/tui` client side of `pkg/protocol` |
| protocol | 0.9k | 2026-07-30; CHANGELOG from 0.84.0 | out-of-scope (contract-only reference) | `pkg/protocol` + `proto/` |
| session-backends/sqlite-node | 2.0k | 2026-08-05 (rename from "storage"); CHANGELOG from 0.81.0 (2026-07-21) | out-of-scope | `store/gormstore` + `migrations/` |
| telemetry | 0.9k | 2026-08-05 (extracted); CHANGELOG from 0.84.0 | core-harness contract, but already covered | `tracing` + `tracing/otelexport` |
| evals | 1.4k | 2026-07-25 | out-of-scope (dev tooling) | none; later a Go test harness |
| mcp (placement only) | 3.1k | 2026-09-29, 0.99.0 | extension-adjacent (other lane covers it) | `mcp` |

Note on dates: "first appeared" is the first git commit touching the directory. Some packages existed earlier under other names (server = "orchestrator", sqlite-node = "storage"), so the true origin is older than the listed date.

## Per-package detail

### chord (`@earendil-works/chord`)

Purpose: an application-composition runtime for systems assembled from plugins. It offers facets (a plugin split into per-environment bundles), typed services (singleton or keyed, local or remote), replicated state with delta transport, and a pluggable remote boundary. The README says it is not a Pi package and depends on no other Pi package (`chord/README.md`).

Main features (sources: `chord/README.md`, `chord/src/`):
- Plugin/facet host and loader (`src/facets/host.ts`, `loader.ts`) with dependency-graph validation, provider-before-consumer activation, reverse disposal.
- Services (`src/services/`): provider, consumer, keyed instances, loopback, remote wire grammar (`$chord.service` control calls).
- Replicated state (`src/services/state.ts`, `state-codec.ts`): atomic `change(ctx, cb)` overlays, bounded 100-update queue per subscription, explicit `reset` snapshot on overflow.
- Delta tracker (`src/delta/`): JSON diff, draft proxies, validated apply of untrusted ops. Exposed as `@earendil-works/chord/delta`.
- Context (`src/context/index.ts`): cancellation and values, exposed as `@earendil-works/chord/context`.
- Node bundler and bundle loader for facets (`src/bundler.ts`, `src/node/`).

Public API: package root, `/context`, `/delta`, `/node` (per README).

Who depends on it (by real imports, files): coding-agent 35, agent (agent-core) 32, durable 87, server 8, client 4, protocol 2, plus one example plugin. Chord depends on nothing.

Limits: `PLANNING.md` says it is "not a stable public API contract yet"; symmetric RPC and structural generation replacement are still planned. It is being built by the same author as the experimental runtime, so its shape is still moving.

History: first commit 2026-08-28 ("add Chord runtime foundation"), 51 commits in about a month, no CHANGELOG.

Class: split. The `Context` type is core, but Go's `context.Context` already covers cancellation and values, and `agent`/`pipeline` should take `ctx` as the first argument. The facet/service/state machinery serves the experimental TUI-over-network design and the plugin reload story. AskCore does not need it now: extensions in AskCore are `hooks`, `skills`, `mcp`, `tools`. Delta only matters if AskCore replicates state to a remote UI; the current design sends WS events through `pkg/protocol`.

### durable (`@earendil-works/pi-durable`)

Purpose: "a durable agent harness" (README is marked Experimental; API changes without notice). Conversations, model turns, tool calls, and app state commit to storage before anything is shown, so a crashed process resumes mid-turn. Built on `pi-ai` and `chord`.

Main features (sources: `durable/README.md`, `durable/CHANGELOG.md` [Unreleased], `durable/src/`):
- `Harness.open(storage, {models, registry}, ctx)`, sessions, conversations, forks, reset/handoff (`src/harness/`, `src/session/`).
- Storage backends: memory, JSONL, SQLite (`src/storage/`), plus a conformance and benchmark kit (`src/testing/`).
- Tool chain: `pi.tool` task with argument validation, durable intent, replay policy, rerun only of replay-safe tools, bounded live output (changelog Unreleased).
- Structured concurrency: tasks own child tasks, `waiting` states, `failFast`/`allSettled`, abort cascade bottom-up.
- Inbox for steer and follow-up inputs while busy; usage and cost ledger (`pi.usage`).
- Hook dispatch: `beforeRequest`, `afterResponse`, `onYield`, `afterTools`, `beforeTool`, `afterTool`.
- Built-in `read`, `bash`, `edit`, `write` tools in `@earendil-works/pi-durable/tools`.

Who depends on it: nobody. grep over the whole workspace finds imports only inside `packages/durable`. It is not in the dependency list of `coding-agent`, `server`, or any other package.

Limits: breaking changes every release (0.99.0 and Unreleased both list many). Roughly 1/3 of the entire secondary-package code (16.6k of about 34k LOC).

History: moved out of an earlier prototype named "Pico" on 2026-09-18 (`docs/pico-v5*.md` are the design notes); first CHANGELOG entry 0.86.0 on 2026-09-19.

Class: out-of-scope for the Go rewrite now. It is a second harness competing with `agent-core`, not an add-on. Do not port it. Use it as a design reference for two ideas that could matter to a daemon: crash recovery by replaying only replay-safe tool calls, and the steering inbox at tool boundaries. Map to `sessions` + `store` + `scheduler` if AskCore later wants crash-resume. Go approach: transactions in `store/gormstore` (SQLite WAL) and an idempotency flag on `tools`.

### codemode (`@earendil-works/pi-codemode`)

Purpose: runs model-written JavaScript in a QuickJS VM compiled to WebAssembly, where the only capability is calling injected tools. Nested tool calls never enter the LLM context; only script output and return value do. Standalone, no Pi dependencies (README).

Main features (sources: `codemode/README.md`, `codemode/src/`):
- `CodemodeSandbox({timeoutMs, tools, globals})`, `execute(code)` returning output items and value or typed error.
- Script globals: `tools`, `ALL_TOOLS`, `text`, `image`, `exit`, `store`, `load`; optional `// @options:` header.
- Worker-based runtime (`src/runtime/worker.ts`, `host.ts`, `protocol.ts`); TypeScript declaration generation for tools (`src/declarations.ts`).
- `image()` validation: base64 of PNG/JPEG/GIF/WebP only (changelog fix, issue 10215).

Who depends on it: coding-agent (3 files under `src/extensions/codemode/`), one file in `agent` (an example). Confirmed by `coding-agent/package.json` dependencies.

Limits: depends on a WASM QuickJS binding (`quickjs-wasi` in coding-agent). Recent churn: 0.99.x changelogs still alter the tool description, MCP exposure defaults (`codemode-deferred` becomes an alias), and add `describeNamespace()`.

History: first commit and first release 0.99.0, 2026-09-29, in the same commit as MCP ("codemode and MCP"). It is one day old at this commit.

Class: extension-adjacent. It is a tool that wraps other tools, not part of the loop. Skip for the first Go milestone. If wanted later: `tools/codemode_*.go`, using a pure-Go JS engine (goja or the QuickJS-in-wazero route) with an injected-tool bridge; keep it behind `permissions`/`sandbox` because it executes model code. Risk: goja lacks full async/await ergonomics that the script API relies on ("top-level await works"), so verify before committing.

### server (`@earendil-works/pi-server`)

Purpose: experimental local server for the new durable Session and Agent Harness interfaces. It routes opaque service calls and subscriptions between connections and per-session workers, and manages multi-presentation attachment (several UIs on one session).

Main features (sources: `server/README.md`, `server/src/`):
- `RoutedServerServiceHost.attachClient()` and `RoutedSessionHandle.attachClient()`; route triple `{serverId, sessionId, attachmentId}` with stale-route rejection.
- Application-owned `SessionDirectory` and `SessionManagement`; Unix-socket listener (`@earendil-works/pi-server/unix`).
- Worker retirement when no presentation demand remains; shutdown closes every session handle.

Who depends on it: coding-agent, as a devDependency only, used by `src/experimental/server.ts`, `session-worker-manager.ts`, `coordinator.ts`. Depends on agent-core, chord, protocol.

Limits: the server never decodes business payloads. The lifecycle protocol is deliberately private and the coordinator is "opaque message router".

History: rename of "orchestrator" on 2026-07-21; changelog since 0.80.3 (2026-06-30); 109 commits, the most of any secondary package.

Class: out-of-scope. AskCore's `gateway` + `gateway/methods` + `sessions` already cover this, and AskCore is a daemon by design. What to steal: the attachment-fencing idea (route triple prevents delayed frames after re-attach) as a rule for `gateway` WS sessions.

### client (`@earendil-works/pi-client`)

Purpose: transport-neutral client for the experimental protocol. It connects over any ordered byte transport, verifies the logical `serverId`, and issues requests and subscriptions.

Main features (sources: `client/README.md`, `client/src/`):
- `Client.connect({serverId, transportFactory})`, `request()`, `subscribeService()`, `reconnect()`.
- Unix transport (`src/unix.ts`); `createClientServiceTransport()` for Chord bindings.
- No automatic reconnect or replay: after disconnect the caller re-attaches and repeats only safe operations.

Who depends on it: coding-agent devDependency (8 files, `src/experimental/client-*.ts`, and `src/client/index.ts`).

History: 2026-07-31; changelog from 0.84.0 (2026-08-06).

Class: out-of-scope. The AskCore TUI is already a separate binary (`cmd/tui`, no `internal/*` imports) speaking `pkg/protocol`. The no-auto-replay rule is worth copying into that client.

### protocol (`@earendil-works/pi-protocol`)

Purpose: runtime-neutral routed envelopes, CBOR encoding, and byte-stream framing. Protocol version 8. Payloads stay opaque strict JSON; Chord owns their meaning.

Main features (sources: `protocol/README.md`, `protocol/src/`):
- Frame: 4-byte big-endian length + one definite-length CBOR item (`framing.ts`, `cbor/`).
- Messages: version handshake with `serverId`, server and session targets, correlated request/response, cancellation, subscription updates, out-of-band attachment changes.
- Streaming decoders tolerate arbitrary fragmentation and coalescing.

Who depends on it: client (9 files), server (8), coding-agent (8, dev). Depends on chord.

History: 2026-07-30; version 8 by 0.99, so the wire format has changed eight times in two months.

Class: out-of-scope. AskCore already has its own wire contract in `pkg/protocol` (WS) and `proto/` (gRPC). Do not adopt CBOR framing: AskCore's contract is already decided. Useful read: the "bounded transport messages, non-empty opaque error codes" rules.

### session-backends/sqlite-node (`@earendil-works/pi-session-backend-sqlite-node`)

Purpose: `node:sqlite` Session backend for `agent-core` (`SqliteSessionRepo`). One file per session by default, or a shared container with `databasePath`.

Main features (sources: README, `src/sqlite/`):
- Session IDs map to filenames (safe chars kept, others base64url with `~` prefix).
- Creation / read-write open / read-only open are distinct factory modes; open and delete never create a missing DB.
- Fork reads a source snapshot in one deferred WAL transaction; no cross-process lock (host must guarantee one writer).

Who depends on it: no package and no file in the workspace imports it. Depends on agent-core and ai.

History: 2026-08-05 rename from "storage"; changelog from 0.81.0 (2026-07-21).

Class: out-of-scope. AskCore uses `store/gormstore` + SQL in `migrations/`. Carry over one lesson: single-writer ownership must be enforced by the host (in Go: `sessions` owns a per-session lock; SQLite WAL with `busy_timeout`).

### telemetry (`@earendil-works/pi-telemetry`)

Purpose: vendor-neutral contracts for spans, attributes, events, status, with explicit context passing (no global current span) and typed schema definitions. Ships a no-op context and an in-memory reference adapter; no exporter.

Main features (sources: README, `src/index.ts`, `memory.ts`, `noop.ts`):
- Callback-based `TelemetryContext` / `TelemetrySpan`; `NOOP_TELEMETRY_CONTEXT`; `InMemoryTelemetryContext`.
- Serializable schema definitions with inferred types; adapter conformance kit (`src/testing/`).

Who depends on it: `ai` (2 files), `agent` (6 files). This makes it a real, small dependency of the core path.

History: extracted 2026-08-05; changelog from 0.84.0.

Class: core-harness contract, but no port needed. Go idiom is OpenTelemetry directly (already in the stack: `tracing` + `tracing/otelexport`). Take one idea: pass spans through `ctx` and keep providers and agent free of a global tracer. Define a small `tracing.Span` interface only if `providers` must not import OTel.

### evals (`@earendil-works/pi-evals`)

Purpose: behavioral evals of the coding agent using `vitest-evals`, including a documentation-lift comparison run in paired Docker containers (`without_docs` versus `with_docs`).

Main features (sources: README, `src/`): `cli.ts` orchestrator, `docker.ts` image build and arm runs, `plan.ts` task expansion, `report.ts` lift computation, `harness.ts` adapter; needs `PI_PROVIDER` and `PI_MODEL`.

Who depends on it: nobody (private workspace). Depends on ai and coding-agent.

History: 2026-07-25 (originally under coding-agent, PR 7085).

Class: out-of-scope. Later, a Go `testsupport` scenario runner with the faux provider could take the host-eval idea; the Docker lift comparison is Pi-specific.

### mcp (placement only)

`@earendil-works/pi-mcp`: standalone MCP client, no dependency on the official SDK or on other Pi packages. Stdio and Streamable HTTP transports, OAuth (`src/oauth/`), roots, progress, in-memory test transport, `toLlmContent()` converter. 3.1k LOC in the package plus about 4.7k LOC of glue in `coding-agent/src/extensions/{mcp,codemode}`. Introduced 0.99.0 (2026-09-29). Used by coding-agent (7 files) and agent examples (2). Class: extension-adjacent. Maps to AskCore `internal/mcp`. The per-server `codemode` exposure mode (`direct` versus `codemode`) is the only link to the codemode package.

## Dependency picture (all 14 packages, from package.json plus verified imports)

```
Legend: A --> B means A depends on B. [dev] = devDependency only. (0) = no Pi dependencies.

LEAVES (0):   chord   telemetry   tui   codemode   mcp   evals*(*depends downward, see below)

ai ------------> telemetry
agent-core ----> ai, chord, telemetry
coding-agent --> agent-core, ai, chord, tui, codemode, mcp
coding-agent --> client, protocol, server          [dev; experimental only]
protocol ------> chord
client --------> chord, protocol
server --------> agent-core, chord, protocol
durable -------> ai, chord                          (no dependents)
sqlite-node ---> agent-core, ai                     (no dependents)
evals ---------> ai, coding-agent                   (no dependents)

Layered view:

 L0  chord   telemetry   tui   codemode   mcp
       |         |
 L1    |        ai ---------------------+
       |         |                      |
 L2    +------ agent-core               |
       |         |                      |
 L3  protocol    |                      |
       |  \      |                      |
 L4  client  server                     |
                                        |
 L5  coding-agent  (core deps: agent-core, ai, chord, tui, codemode, mcp)
       ^ dev-only, experimental: client, protocol, server
 L6  evals

 Orphans (no dependents): durable, sqlite-node, evals
```

Core shipping path: `tui` + `ai` + `agent-core` + `coding-agent` (+ `chord` Context/Delta, `telemetry`, `mcp`, `codemode`). Experimental branch: `chord` (full), `protocol`, `client`, `server`, `durable`, `sqlite-node`.

## Mapping to AskCore (`internal/README.md`)

| Pi package | AskCore package | Note |
|---|---|---|
| ai | `providers` | other lanes |
| agent-core | `agent`, `pipeline`, `sessions` | other lanes |
| tui | `cmd/tui` (bubbletea) | other lanes |
| coding-agent | `agent` + `tools` + `skills` + `hooks` | other lanes |
| chord (Context) | stdlib `context` in `agent`/`pipeline`/`tools` | no package |
| chord (services/facets/state) | none | `hooks`+`mcp`+`skills` are the AskCore extension seams |
| telemetry | `tracing`, `tracing/otelexport` | already present |
| mcp | `mcp` | already present |
| codemode | future `tools/codemode_*.go` | optional |
| server | `gateway`, `gateway/methods`, `http` | already present |
| client | `cmd/tui` transport code | already present |
| protocol | `pkg/protocol`, `proto/` | already present |
| sqlite-node | `store/gormstore`, `migrations` | already present |
| durable | reference for `sessions`/`store`/`scheduler` | design only |
| evals | `testsupport` | later |

## Recommendation (ranked)

1. Do now: nothing new from these packages except `mcp` (already has a home) and stdlib `context`. Keep the Go rewrite focused on harness core, extension system (`hooks`, `skills`, `mcp`), and TUI.
2. Do next, cheap: copy three rules into AskCore docs or code: attachment fencing on WS sessions (from server/protocol), no auto-replay after reconnect (from client), single writer per session enforced by the host (from sqlite-node).
3. Study, do not port: `durable`. If crash-resume matters, read `durable/README.md` "Persist and Resume" and the tool `replay policy` in the changelog, then design it on `store/gormstore`.
4. Defer: `codemode` until the tool and permission layers are stable.
5. Skip: `chord` services and facets, CBOR protocol, `evals` Docker lift.

Adoption risk: every experimental package changes API in each release (protocol at version 8, durable with pages of breaking changes in 0.99.0 and Unreleased, chord has no changelog and states it is unstable). Porting any of it now means porting a moving target.

## Limitations

- I read READMEs, package.json, CHANGELOGs, and grep results. I did not read the internals of `durable` (16k LOC) or `chord` beyond the entry points.
- "Who depends" uses static imports; dynamic imports and non-TS consumers (docs, scripts) were not searched beyond the `.ts` files and package.json files.
- The experimental gate (`PI_EXPERIMENTAL=1`) was confirmed in `coding-agent/src/cli/startup-ui.ts`, and the build exclusion in `coding-agent/package.json`; I did not run the CLI.
- Did not verify goja or wazero support for the codemode script API.

## Unresolved questions

1. Does the user want crash-resume mid-turn in AskCore? This decides whether `durable` is a design reference or ignorable.
2. Does the TUI need live replicated state from the daemon (Chord Delta idea), or are WS events enough?
3. Is codemode (model-written JS calling tools) wanted at all, given AskCore's `sandbox` and `permissions` scaffolds?
4. Is Pi's experimental branch expected to become the default in a later Pi release? If so, `chord`, `server`, and `protocol` move up in priority and this triage should be redone.

Status: DONE
