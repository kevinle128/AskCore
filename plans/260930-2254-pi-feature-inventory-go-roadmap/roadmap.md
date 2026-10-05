# Ask roadmap: rebuild the Pi harness in Go

Date: 2026-09-30, revision 3 (the review and its re-check are applied). The review in `plans/reports/code-reviewer-260930-2254-roadmap-review.md` is applied; section 8 maps each finding to its fix.
Revision 4 (2026-10-01): the H2 port analysis `plans/reports/xia-261001-h2-agent-loop-pi-port-analysis.md` is applied (D18 to D21, and the H2, H3, H9 text).
Revision 5 (2026-10-05): the H4 port analysis `plans/reports/xia-261005-1408-h4-more-wire-apis-pi-port-analysis.md` and the user decisions recorded there are applied (D11, D22, H1, H3, H4, H9, H17 text, and section 9).
Reference: Pi 1.0.1, commit `4c6fb7cfe`, at `/Users/dale/Desktop/workspace/opensources/pi` (user, 2026-10-05; before that Pi 0.99.1, commit `2bbfcca4`). Line citations written before 2026-10-05 are at `2bbfcca4`. Each phase checks and fixes the citations it uses.
Inputs: `inventory-harness.md` (H-*), `inventory-extensions.md` (E-*), `inventory-tui.md` (T-*), `timeline.md`, the edge-case report `plans/reports/researcher-260930-2254-pi-edge-cases.md` (E§N; the top-30 list is E§24#N), and the waku report `plans/reports/researcher-260930-2254-waku-watch-it-think.md`.

## 0. How to read this roadmap

- **Goal of the user:** learn how to build an agent harness. The harness comes first. Each harness phase teaches **one concept** and ends with a **runnable, testable result**.
- **Milestones:** see section 0b. M1 is must-have; M2 is nice-to-have.
- **Order (M1 first, then M2):**
  1. M1: Phase 0, then H1 to H13, then W1, H14, H15 and X1, then T1. T0 (the inline prototype) runs in parallel from H2 on, because it lives in a scratch module.
  2. M2: H16, X2, H17, T2, X3 and the built-in permission policy, in any order the user picks.
- **Each phase lists:** the decisions it waits on, the concept, the Pi files to read, the inventory ids it owns, its tests (edge cases), what it must not rebuild, and its exit.
- **Ownership rule:** each P0 inventory row is owned by exactly one phase. Other phases may use a row, but they do not own it. A P1 row that a P0 exit needs is pulled forward and marked "(P1, pulled)". Section 6 places or defers all other P1 rows.
- **Pi path shorthand:** `A:` = `packages/agent/src/`, `AI:` = `packages/ai/src/`, `C:` = `packages/coding-agent/src/`, `CD:` = `packages/coding-agent/docs/`.

## 0b. Milestones (user rule: must-have first, every nice-to-have later)

| Milestone | Phases | Decided |
|---|---|---|
| **M1: must-have** | Phase 0, H1 to H13, H14 (session tree), H15 (skills, templates, `/reload`), W1 (web monitoring dashboard), X1 (external Go extensions), T0 (inline prototype gate), T1 (TUI core, inline only) | User, 2026-10-01. W1 and H14 were chosen as must-have. |
| **M2: nice-to-have** | `/login` and `/logout` (API key and OAuth), extension UI dialogs in the TUI, built-in permission policy (allow, deny, ask), H16 (MCP client), X2 (shell hooks), H17 (more providers, OAuth, proxy), T2 (overlays, images, TUI inspector, fullscreen), X3 (packages), Windows | User, 2026-10-01: MCP and shell hooks are not must-have. |

Rule for M1 work: when a phase has a P0 part and a P1 part, M1 builds the P0 part and the P1 parts that an M1 exit needs. Every other P1 or P2 row waits for M2.

## 1. Decisions, in the order they block phases

The user answers these one per turn, in this order. Each blocked phase starts with a "Waits on" line.

| # | Decision | Blocks | Options | Recommendation |
|---|---|---|---|---|
| D1 | Runtime model inside the confirmed dewee package model | Phase 0 | The scaffold plan's confirmed decision 0 says: "B: dewee model (packages by capability) plus Ask import rules." The packages stay. The question is what the `agent`, `pipeline`, `sessions`, `scheduler` and `hooks` READMEs describe. **A:** Pi semantics. `agent` owns a two-level loop. `pipeline` becomes the ordered hook points of one turn (or is removed). Sessions become an entry tree. The queues become steer/follow-up. **B:** keep the dewee 8-stage pipeline and map Pi onto it. | **Decided (user, 2026-10-01): A, Pi semantics.** The dewee packages stay. `pipeline` holds the turn's hook points. |
| D2 | Credential and runtime-settings storage | Phase 0 (README text), H6, H7 | **Credentials:** a file with a lock (Pi's `auth.json`) or a SQLite row encrypted with `crypto` (as `internal/providers/README.md` says). **Runtime settings:** the depguard rule `config-only-in-root` lets only `app` and `cmd/*` import `config`, so the per-project, trust-gated, writable settings need a new loader. That loader lives in `workspace` or in a new `settings` package and gets typed defaults from `config`. **Also decide:** is multi-tenant (credentials per tenant) a real requirement? | **Decided (user, 2026-10-01): B, files as in Pi.** Credentials in `~/.ask/auth.json` (mode 0600, file lock, read-merge-write). Settings in `~/.ask/settings.json` and the project `.ask/settings.json` (after trust). A new `internal/settings` package owns both files, because `config` may be imported only by `app` and `cmd/*`. Cloud mode keeps the file on the server. |
| D3 | Permission layer for tools | Phase 0 (README text), H5 (stance) | **A:** Pi's stance: no approval, and blocking happens only through `tool_call` hooks (H-SEC-03). **B:** a policy (allow, deny, ask) from day 1. | **Decided (user, 2026-10-01): as Pi, no permission popups and no built-in policy in M1.** Tools run without approval. A user who wants to block a command writes an external extension (X1) that handles `tool_call`. A built-in allow/deny/ask policy handler is M2. (History: the user chose A, then changed to "no permission popups, not now", then removed the whole policy handler from M1.) |
| D4 | Hook deadlines | Phase 0 (README text), H11 (scope), X1 (mechanism) | Pi has no hook timeouts (removed in 0.31.0). The proposal is a deliberate departure: a deadline only for out-of-process (tier B, X1) calls. When the deadline expires, `tool_call` and `user_bash` fail closed and the other events are logged. Compiled-in (tier A) handlers get no deadline. | **Decided (user, 2026-10-01): A.** Only out-of-process hooks (X1) get a deadline. On expiry, `tool_call`/`user_bash` fail closed and other events are logged. Compiled-in Go handlers get no deadline. The default value is set in X1. |
| D5 | How `ask` runs the harness | H2 | `cmd/tui` is the `ask` binary with three modes: interactive TUI (ACP client of the leader), headless `ask -p` / `--mode json` (agent in process, direct Go API), `ask leader` (holds one agent). `cmd/server` (daemon) is also an ACP client of the leader. | **Decided (user, 2026-10-01), Grok model.** See `docs/ask-architecture-reference.md` section 7.3. H2 exit is `ask -p` in process, with no leader and no socket. The leader comes in H13. The old rule "`cmd/tui` imports no `internal/*`" was Claude's inference, not a user decision, and is removed. |
| D6 | Minimum provider set for launch | H4 (Anthropic is in every option, so the first provider phase does not wait) | **A:** Anthropic + OpenAI-compatible. **B:** A + OpenAI Responses. **C:** more. | **Decided (user, 2026-10-01): B.** `anthropic-messages` (H3), `openai-completions` with the compat record and `openai-responses` (H4). Google, Bedrock and Mistral stay in H17. |
| D7 | Ask names: config dir, project dir, env prefix | H5 (child env names), H6 | Suggested: `~/.ask/`, `.ask/` in the project, `ASK_*` | **Decided (user, 2026-10-01):** `~/.ask/` (override `ASK_HOME`), project `.ask/`, env prefix `ASK_*`, binaries `ask` (`cmd/tui`) and `ask-server` (`cmd/server`). |
| D8 | Windows and WSL at launch | H5, T1 | Yes or no | **Decided (user rule "must have first", 2026-10-01): no.** Unix only at launch (macOS, Linux). Windows, WSL and PowerShell are later. |
| D9 | Anthropic OAuth with Claude Code identity headers | H7 (precedence), H17 (flows) | **A:** API keys only. **B:** copy Pi's OAuth identity. | **Decided (must have first): A, API keys only.** Anthropic OAuth with Claude Code identity is not built. |
| D10 | Crash-resume in the middle of a turn | H8 | **A:** no (Pi's shipping behavior). **B:** yes (Pi's experimental v4 and `durable`). | **Decided (must have first): A, no crash-resume.** Keep the session schema open for it. |
| D11 | Retry jitter | H9 | **A:** copy Pi (no jitter, `AI:utils/retry.ts:122-126`). **B:** full jitter. | **Decided (must have first): A, copy Pi (no jitter)** for now; add jitter when many sessions share one daemon. Scope (2026-10-05): this is the agent-level retry delay. Pi's provider-level retry has jitter (`AI:utils/provider-retry.ts:66`), but it is off while provider `maxRetries` is 0, which is the Ask default. |
| D12 | "Watch it think" (from waku-agent) | H12, W1, T2 | **Decided (user, 2026-09-30):** a read-only web monitoring dashboard served by the daemon, plus a TUI inspector and OTel export. The web has no chat and no control. | Done. The docs rule is narrowed. |
| D13 | External-extension runtime (internal extensions are compiled-in Go, decided) | X1 | **A:** stdio JSON-RPC subprocess in any language. **B:** Go source that Ask builds on load into a separate binary, cached, over stdio (needs the Go toolchain). **C:** TypeScript through esbuild (Go library) + goja, in process (closest to Pi; no Node APIs, no isolation). **D:** WASM (wazero), sandboxed. Options can be layered. | **Decided (user, 2026-10-01): B only.** External extensions are Go source. Ask builds each one on load (`go build`, cached by content hash) into a separate binary and runs it as a child process over stdio. The stdio protocol exists as the transport, but Go through the `pkg/` SDK is the only supported way to write one. TypeScript (C) and WASM (D) are not planned. A machine without the Go toolchain gets no external extensions; skills, templates, MCP and shell hooks still work there. |
| D14 | bubbletea v2.0.10 pin with a repo-wide Go 1.26.0; how inline scrollback is written | T1 | **Pin:** A: v2. B: stay on v1. **Committer route**, if the T0 gate needs more than the five kiln-class fixes: (a) a raw-write scrollback committer, or (b) a vendored bubbletea/ultraviolet fork. | Open. v2 is a recommendation, not a pin, until the M1 T0 gate (G1, G2, G3, G6) passes. The Go part is settled: the repo is on Go 1.27.0 since D22 (2026-10-01), which is above v2's 1.26.0 floor. |
| D15 | Fullscreen (alt-screen) in the first TUI milestone | T1, T2 | Inline only first, or both | **Decided (must have first): inline only** in the first TUI milestone. Fullscreen is later. |
| D16 | External agent protocol | H13 | **Decided (user, 2026-10-01): ACP** (Agent Client Protocol, JSON-RPC 2.0, JSON) plus `_ask/*` extension methods and `_meta` is the only protocol between the agent process and anything outside it (leader socket, remote agent over WebSocket, editors over stdio). Inside the process and in headless mode: direct Go calls. No protobuf on ACP links; protobuf/gRPC only for internal service APIs and OTLP (Grok does the same: `plans/reports/researcher-261001-0117-grok-protobuf-usage.md`). | Done. |
| D17 | ACP Go SDK | H13 | **A:** `coder/acp-go-sdk` v0.13.5 (no release or maintainer reply since June 2026, per a fork's README). **B:** the fork `lx-wnk/acp-go-sdk` v1.21.0, tracks ACP schema 1.21.0, same module path (needs `replace`). **C:** `caelis-labs/acp-go-sdk` v1.4.0. **D:** generate our own types from the ACP JSON schema. | Keep open until a conformance check passes at H13a: pin the SDK and the ACP schema version separately; prove custom `_ask/*` methods, preserved `_meta`, notifications and `session/cancel` through the leader; map each M1 Go API call to ACP. Note: an ACP `session/prompt` response ends the turn and carries a stop reason, while Pi's `prompt` response only means "accepted"; define when the ACP response is sent relative to `agent_settled` (after retries and compaction). Check the current ACP schema for standard usage updates before adding `_ask/*` usage methods. The same Go API tests run against the direct and the remote client. |
| D18 | Terminal record of JSON mode before H9 | H2 | **A:** pull a minimal `agent_settled` into H2. **B:** end at `agent_end` until H9. **C:** defer JSON mode to H8. | **Decided (user, 2026-10-01): A.** H2 emits `agent_settled` right after `agent_end`. H9 moves the emit point after retry and compaction. A JSON reader never changes. |
| D19 | Abort in the middle of a tool batch | H2, H3 | **A:** deviate: every unstarted call gets "Operation aborted". **B:** as Pi: prepared calls get "Operation aborted"; calls after the break point get no events and no result; the loop makes one more stream call with the aborted signal; `transformMessages` (H3) synthesizes "No result provided" at replay. | **Decided (user, 2026-10-01): B, as Pi** (`A:agent-loop.ts:575-577,613-643`; `AI:api/transform-messages.ts:158-180`). The H2 pairing test covers the loop part only. The full pairing test after abort is in H3. |
| D20 | Hook and stream failure contract | H2 | **A:** recover inside the loop. **B:** as Pi. | **Decided (user, 2026-10-01): B, as Pi.** A throw or panic in a tool, `prepareArguments`, validation, `beforeToolCall` or `afterToolCall` becomes an error tool result (`A:agent-loop.ts:769-775,841-847,892-895`). An error from `transformContext`, `convertToLlm`, `getApiKey`, `prepareRequest`, `finishTurn` or the stream function is not caught by the loop. The loop returns it, and the `Agent` wrapper builds the error assistant message plus `message_start`, `message_end`, `turn_end` and `agent_end` (`A:agent.ts:523-548`). |
| D21 | Tool argument coercion | H2 | Pi runs TypeBox `Value.Convert` for TypeBox schemas and a custom table (`AI:utils/validation.ts:59-131`) for plain JSON schemas. Ask has only JSON schemas. | **Decided (user, 2026-10-01):** one Go coercion table for all schemas, based on Pi's custom table plus the union rule (an arm that already validates is kept). No truncation of floats to integers (TypeBox turns `"5.7"` into 5; Ask does not). |
| D22 | Provider wire layer | H2 (Go version), H3, H4, H17 | **A:** Ask's own adapters over HTTP and the H1 SSE reader (Pi's way). **B:** `charm.land/fantasy` (Apache-2.0, v0.45.2, needs Go 1.27.0) behind one Ask adapter. | **Decided (user, 2026-10-01): B.** Go is raised to 1.27.0 (done: build, `go test ./...` and `golangci-lint` pass; a scratch copy with fantasy v0.45.2 also passes `go test -race ./...` and lint). Ask's `pkg/protocol` messages and `providers.Stream` stay the core contract. One adapter maps Ask messages to `fantasy.Call` and `fantasy.StreamPart` to the H1 `Assembler`. Fantasy types never leave that adapter package. Fantasy's own agent loop, retry and tool runner are not used: the loop is H2, retry is H9 (fantasy v0.45.2 already sets the SDK retries to 0 in its Anthropic and OpenAI providers, `providers/anthropic/anthropic.go:277`, `providers/openai/openai.go:168`; keep a lock-in test). `transformMessages` stays in Ask (H3). The H1 SSE reader stays unused unless a provider needs it. The dependency is added by the first PR that imports it. **Placement (user, 2026-10-02, option A):** the fantasy adapter is built in H3, not H2. H2 runs on faux only. **Update (user, 2026-10-05):** (1) Fantasy v0.45.2 cannot replay OpenAI Responses reasoning statelessly. The user chose to patch fantasy ("sửa fantasy, chúng ta có source mà"), so D22 stays. Ask uses the fork through `replace charm.land/fantasy => github.com/kevinle128/fantasy <pseudo-version>` in `go.mod` (a local `go.work` is allowed during fork work). The fork already has `bff4512` (inline reasoning replay with `store:false`, encrypted content from `output_item.done`), `76fdec8` (Chat stream must end with `finish_reason`) and `9a5405c` (Anthropic large tool numbers). Fork work still open: F1 message item id and `phase` on replay, F2 function-call item id, F3 Responses `ExtraBody`, F4 status and raw reason on Responses stream errors, F8 the echoed `service_tier` (H4 port analysis, "State of the fantasy fork"). (2) Fantasy is infrastructure ("fantasy là ở tầng infrastructure, các adapter sử dụng cái gì là việc của adapter"). There is one adapter package for each wire API, named for what it serves: `internal/providers/anthropic/` and `internal/providers/openai/`. Each adapter chooses its own libraries. The shared fantasy plumbing (StreamPart-to-Assembler fold, idle timeout, error mapping, witness) is one infrastructure package, for example `internal/providers/fantasykit/`. A vendor such as Alibaba Token Plan is data (provider and model records), not a package. Core files in `internal/providers/*.go` and packages outside providers do not import fantasy or the vendor SDKs (depguard). |

## 2. Phase 0: architecture alignment (documentation only)

Status: done (2026-10-01).

- **Waits on:** D1, D2, D3, D4.
- **Why:** the scaffold READMEs describe a dewee chat-bot runtime. Every later phase builds on these contracts, so fix them first.

| Package or doc | Scaffold text | Pi evidence | Change if D1 = A |
|---|---|---|---|
| `agent` + `pipeline` | 8 stateless stages: context, history, prompt, think, act, observe, memory, summarize. `Router` resolves an agent by key. | Two-level loop with the hooks `transformContext`, `prepareRequest`, `beforeToolCall`, `afterToolCall`, `finishTurn` (H-LOOP-02, H-LOOP-14, `A:agent-loop.ts:163-330`). One agent per session. | `agent` owns the loop (`loop_*.go`). `pipeline` holds the ordered hook points of one turn. Summarize becomes compaction (H10). `Router` is not needed until multi-agent routing is. |
| `sessions` + `store` | Key `agent:{agentId}:{channel}:direct:{peerId}`. `sessions` imports only `store`. | A tree of typed entries with `parentId`, fork and branch. Context is a projection of the log (H-SESS-03, H-SESS-06). | `sessions` owns the entry log and the context builder, and may import the message types. `store` gets `session` + `session_entry` tables. A channel key can map to a session id later. |
| `scheduler` | Queue modes `queue`, `followup`, `interrupt` | Steer (delivered after the whole tool batch) and follow-up (delivered at idle). Modes are `all` or `one-at-a-time` (H-LOOP-03..05). Pi fixed "skip the remaining tools" in 0.58.4. | The queue semantics move into `agent`. `scheduler` keeps lanes and concurrency limits. `interrupt` is removed; abort is a separate call. |
| `hooks`, `hooks/handlers` | Sync hooks block or change input; async hooks run in a pool. "Timeouts are fail-closed." Imports are `store`, `tracing`, `crypto`, `sandbox`. | 19 of 41 events are sync and 22 are notify (this matches). Only `tool_call` and `user_bash` fail closed. Pi has no timeouts. | Keep the sync/async split. Fail-closed applies only to the two events above. Deadlines follow D4. Handlers may import the event and content types. |
| `bus` | Inbound and outbound messages, and events that notify subscribers. Stdlib only. | 41 events, one stream, many watchers (H11, H12, waku report 7(b)) | One owner rule: event types live in `pkg/protocol`, the publisher and bounded fan-out live in `bus`, sync dispatch lives in `hooks`. |
| `tools` / `permissions` | `tools` has "policy: allow, deny, approval". `permissions` is role-based access. | Pi has no permission system (H-SEC-03). Project trust is a separate concept (H-SEC-01). | No built-in tool policy in M1 (D3): the `policy.go` line in the `tools` README is removed. `permissions` keeps role-based access for the gateway. The trust store is not role-based access: it goes to `workspace` or `settings` (D2). |
| `memory` | Auto-injection into the prompt; flush at the end of the run | Pi has no memory. Auto-injection breaks the byte-stable prompt (H-PROMPT-09). | Parked. If it comes back, it injects only as a named section outside the cached prefix. |
| `skills` | BM25 and embedding search; hot reload (`watcher.go`) | Only metadata goes into the prompt. Reload is explicit (`/reload`). There is no watcher (E-LD-11). | Metadata discovery plus explicit reload. Search and the watcher are parked. |
| `bootstrap` | Seeding for a new agent or user; truncation | `AGENTS.md` is discovered from cwd and its ancestors (H-PROMPT-04) | `bootstrap` owns context-file discovery. Seeding is parked. |
| `providers`, `crypto`, `config` | `providers`: "API keys at rest -> `internal/crypto` + `internal/store`". `crypto`: encrypts keys "before they go to the database". `config`: "Secrets in files -> environment variables only". | Pi keeps credentials in `auth.json` with a lock (H-AUTH-02, H-AUTH-03). | Per D2: credentials live in `~/.ask/auth.json`, owned by the new `internal/settings` package. Update the three READMEs. `crypto` is parked for credentials. |
| `workspace` | Resolver for each run kind | Every tool and trust decision needs an explicit session cwd and a canonical project root (H-TOOL-19, H-SEC-01) | `workspace` owns the session cwd, the project root and (per D2) the project trust store. |
| `providers` | `Provider`, `ProviderAdapter`, `ThinkingCapable`; "retry and error classification" | Api, Provider and Model are separate, with a compat record as data (H-PROV-01). Agent-level retry lives in the agent (H9). | Describe the Api/Provider/Model split. `providers` keeps provider-level retry and error classification. |
| `docs/ask-architecture-reference.md` | Sections 2, 3, 4 (`pipeline.Stage`), 7.2 (runtime flow), 9 ("a new pipeline stage"), 10 (entry points) | as above | Align all six sections with section 7.3. |

- **Exit:** the READMEs and the architecture doc are updated, and `golangci-lint` passes. Depguard changes only where the table above says so.

## 3. Dependency order

```
P0 ─► H1 events ─► H2 loop ─► H3 first provider ─► H4 more providers ─► H5 tools ─► H6 settings+trust+prompt
                                                                                            │
      H7 models+credentials ◄──────────────────────────────────────────────────────────────┘
          │
          ▼
      H8 session log ─► H9 queues+retry ─► H10 compaction ─► H11 bus+hooks ─► H12 observability ─► H13 gateway
                                                                                                    │
            ┌────────────────────┬────────────────────────┬─────────────────────────────────────────┤
            ▼                    ▼                        ▼                                         ▼
       W1 web dashboard    H14 session tree ─► H15 commands ─► H16 MCP ─► H17 auth+providers    X1 extension protocol ─► X2, X3
                                                                                                    
T0 inline prototype (scratch module, parallel from H2) ─► T1 TUI core (after H13) ─► T2 TUI P1 + inspector (also after W1 and X1)
```

Rules behind this order (all checked by the review):
- The faux provider and the partial-JSON parser (H1) come before the loop (H2).
- H2 runs in memory (`--no-session` semantics) behind a context-source interface. H8 swaps in the session projection without a rewrite.
- The settings (H6) come before the catalog, credentials and resolution (H7). Tools (H5) use hard-coded defaults until H6 wires the settings.
- The session log (H8) comes before retry (H9) and compaction (H10), because both use `context_edit`.
- Agent-core events come in H1. Session events (`agent_settled`, `queue_update`, `compaction_*`, `auto_retry_*`, `entry_appended`) come in the phase that emits them.
- The bus (H11) comes before the observability core (H12). The gateway event feed (H13) comes before W1 and the T2 inspector.

## 4. Harness track

### H1: Messages, agent-core events and a fake model

- **Concept:** a harness is a message log plus a stream of events.
- **Read in Pi:** `AI:types.ts:389-610`, `A:types.ts:514-529` (the 10 agent-core events), `C:core/agent-session.ts:180-200` (the session events, for later phases), `CD:json.md`, `AI:providers/faux.ts`, `AI:utils/json-parse.ts`.
- **Owns:**
  - H-SESS-04 (message roles and `convertToLlm`).
  - H-SESS-05 (the assistant message record).
  - H-MODE-05 (delta-only `message_update`).
  - H-LOOP-16 (the stream-function contract).
  - H-PROV-09 (the faux provider).
  - H-PROV-11 (stream completeness and the tolerant partial-JSON parser).
  - H-MODE-04, agent-core part.
- **Also builds:**
  - The `Usage` type in micro-USD integers. H4 owns the pricing part of H-RETRY-07, and H9 owns the rest.
  - The event envelope `seq`, `ts`, `sessionId`, `runId`, `type`, in `pkg/protocol`. `message_end` carries `usage`, `model` and `provider`. Changing the envelope later breaks every subscriber (waku report 7(a)).
  - An SSE reader without `bufio.Scanner`.
- **Packages:** `providers` (types, SSE reader, faux provider), `pkg/protocol`.
- **Tests:**
  - A stream without a terminal event is an error (E§24#4).
  - Malformed partial JSON is tolerated.
  - A line longer than 64 KB passes (E§25).
- **Do not rebuild:** cumulative `message`/`partial` in updates (removed in 0.84.0).
- **Exit:** a scripted faux stream becomes a correct event sequence and a final message in a unit test.

### H2: The agent loop, plus print and JSON mode (in memory)

- **Waits on:** D5. Uses D18 to D21 (all decided).
- **Concept:** call the model, run the tool calls, feed the results back, and stop when the model stops.
- **Read in Pi:** `A:agent-loop.ts` (940 lines), `A:types.ts`, `A:agent.ts:230-612`, `AI:utils/validation.ts`, `C:modes/print-mode.ts`, `C:modes/json-event.ts`, `C:core/output-guard.ts`, `C:cli/args.ts`, `C:cli/initial-message.ts`, `C:cli/file-processor.ts`, `C:main.ts:80-122`. The port analysis with `file:line` evidence: `plans/reports/xia-261001-h2-agent-loop-pi-port-analysis.md`.
- **Owns:**
  - H-LOOP-01 (entry points `Run` and `Continue`; `Continue` with an assistant tail or an empty context is an error).
  - H-LOOP-02 (two-level loop). Port `runLoop` (`A:agent-loop.ts:163-321`) 1:1, with `lastCompletedTurn` and `explicitContinuation`. `finishTurn` `continue` gives exactly one more request.
  - H-LOOP-08 (abort through `context.Context`). Abort in the middle of a batch follows Pi (D19).
  - H-LOOP-09 (parallel and sequential modes). One sequential tool makes the whole batch sequential. Parallel: preflight in source order, `tool_execution_end` in completion order, result messages in source order after the batch. No concurrency limit.
  - H-LOOP-10 (the tool pipeline). Events and hooks see the raw arguments; `execute` sees the validated ones. `afterToolCall` runs only for calls that executed. Updates after settle are ignored.
  - H-LOOP-11 (the truncated-output guard). The error text is byte-exact with `A:agent-loop.ts:493`.
  - H-LOOP-13 (error ends the run). `finishTurn` is still called; its decision is ignored.
  - H-LOOP-14, core hook set: `transformContext`, `convertToLlm`, `getApiKey`, `prepareRequest`, `finishTurn`, `beforeToolCall`, `afterToolCall`, plus the two poll hooks `getSteeringMessages` and `getFollowUpMessages`. The poll hooks return empty lists until H9 fills them. `prepareNextTurn` and the rest are in H11. Failure contract per D20.
  - H-LOOP-17, part: state, one active run, ordered listeners, `WaitForIdle`, `Reset` (an error while running), and the run-failure path of D20. The queues are in H9.
  - H-TOOL-12 (argument coercion, per D21). The validation error echoes the raw arguments with a size cap (Pi has no cap).
  - H-TOOL-13 (the result shape). The tool result message holds `content` (empty is `[]`), `details`, `usage`, `isError` and `timestamp`. `structuredContent` and `terminate` are in events only. `toolName` is the name that the model sent.
  - H-TOOL-21, loop part: the length guard and the error/aborted path give one result per call; prepared calls get "Operation aborted" on abort. Unprocessed calls after an abort are repaired in H3 (D19).
  - H-MODE-01 (mode selection: `--mode json` > print if `-p` or stdin/stdout is not a TTY > interactive; `--mode text` does not force print).
  - H-MODE-02 (print mode), H-MODE-03 (JSON mode; P1, pulled; the session header record is added in H8, and Pi skips it when there is no header).
  - H-CONF-09, basic flags: `-p`, `--mode`, `--provider`, `--model`, `--api-key`, `--thinking`, `--`, positional messages, piped stdin and `@file` (text only).
- **Also builds:**
  - A minimal `agent_settled` event right after `agent_end` (D18; H-LOOP-21 stays owned by H9).
  - One tool-context struct: ctx, cwd, abort signal, update callback (timeline lesson 5).
  - `SourceInfo` provenance on tools (lesson 6), kept in the registry, as in Pi. The registry rejects a tool without a schema and a duplicate name.
  - A context-source interface with an in-memory log. It plugs in at `prepareRequest`, the hook that Pi's session uses to project the session log into each request (`C:core/agent-session.ts:746-769`). H8 swaps in the session projection there.
  - The initial system message with the system prompt and all tool declarations (`createInitialSystemMessage`, timestamp 0). The tool set is fixed for a run; the tool-delta messages (H-LOOP-15) come in H8.
  - Hook points as typed function fields, one for each hook point. Ordered multi-handler steps come in H11. The `pipeline` README is updated to match.
  - The output guard: in print and JSON mode, stdout carries protocol output only, through one writer. Logs go to stderr. A stdout write error (for example EPIPE) gives exit 1. Stdout is flushed before exit.
- **CLI and mode rules (from Pi unless marked):**
  - Stdin (trimmed), `@file` text and `messages[0]` are joined with no separator. The other messages run as sequential prompts. A thrown error stops the rest; an assistant error does not.
  - Print mode writes the text blocks of the last assistant message, each followed by `\n`. Error or aborted: the message on stderr and exit 1. JSON mode: exit 0 on an assistant error; exit 1 only on a thrown error.
  - SIGTERM exits 143 and SIGHUP exits 129, after cleanup. **Deviation:** SIGINT cancels the run context and exits 130 (Pi has no handler).
  - `-p` consumes the next token as the message unless it starts with `@` or `-`. `--` makes the rest messages or files.
  - `--mode` with a missing or invalid value: an error, exit 1. `--thinking` with an invalid value: a warning, the default is kept. `--api-key` without a model: an error, exit 1.
  - **Deviation:** `--provider`, `--model` and `--api-key` without a value, and unknown `--flags`, are errors until X1 (Pi stores them for extensions).
  - A prompt that starts with `/` is literal text until H15 (Pi gives it to commands, skills and templates).
  - `@file`: a missing file or a read error gives exit 1; an empty file is skipped; the text is wrapped in `<file name="...">`. Images wait for H3 (H-PROV-27).
  - JSON mode: one record per LF, each prompt ends with `agent_settled`, and a slow reader stalls the loop (backpressure, as in Pi).
- **Packages:** `agent`, `pipeline` (hook points, per D1), `tools` (interface, registry, validation, a test `echo` tool), `cmd/tui` (headless `ask -p`, per D5).
- **Tests:**
  - The full event order of a two-turn run with tools.
  - `finishTurn`: `continue` without tools gives exactly one extra request; `end` skips the poll hooks.
  - An error or aborted assistant message: `finishTurn` is called, then `turn_end(msg, [])` and `agent_end`, and its tool calls do not run.
  - The pairing invariant, loop part (E§24#1): after length truncation, and on the error/aborted path. Abort in the middle of a batch: prepared calls get "Operation aborted", later calls get no events, and one more stream call returns aborted (D19). The full invariant after abort is tested in H3.
  - A stream that ends without a terminal event becomes an error message, not a hang.
  - D20: a panic in a tool, in `beforeToolCall` and in `afterToolCall` each give an error result; an error from `prepareRequest` or the stream function gives the wrapper's error message and `agent_end`.
  - Result order versus event order (the second tool finishes first). One sequential tool makes the batch sequential.
  - Pipeline: unknown tool, validation error text, block with and without a reason, hook mutates the arguments, update after settle ignored, `afterToolCall` skipped for blocked and invalid calls.
  - Coercion table (D21): `"5"` to 5 for an integer, `"5.7"` is not an integer, null in `anyOf` kept, optional null removed, missing input is `{}`, an arm that already validates is kept.
  - A goroutine-leak test with `goleak` (E§24#19): after a normal end, after abort, and after the JSON reader closes the pipe.
  - JSON-mode LF framing with no line limit, U+2028 inside a string, `agent_settled` per prompt (E§24#29).
  - Output guard: a stray stdout write goes to stderr; EPIPE gives exit 1.
  - CLI: stdin plus message join, `-p` token rule, `--`, invalid `--mode`, missing flag values, exit codes 0, 1, 130, 143.
  - Print mode opens no network listener.
- **Do not rebuild:**
  - `shouldStopAfterTurn` (removed in 0.87.0).
  - Cumulative `message`/`partial` in `message_update` (removed in 0.84.0).
  - Agent state as the history. This is temporary here and replaced by the log in H8.
- **Exit:** `ask -p "hello"` and `ask --mode json` run in process against the faux provider and the echo tool, with the right exit codes.

### H3: The first real provider (Anthropic)

- **Concept:** one internal message model, one adapter for each wire API.
- **Wire layer (D22):** the wire APIs come from `charm.land/fantasy`. H3 builds the `anthropic-messages` adapter (Ask messages to `fantasy.Call`, `fantasy.StreamPart` to the H1 `Assembler`) and the shared fantasy plumbing that H4 reuses for the OpenAI adapter (D22 update, 2026-10-05: one adapter for each wire API, shared code in one infrastructure package). The Pi adapter files below are read for behavior and quirks that the adapter must keep, not for HTTP code to port.
- **Read in Pi:** `AI:types.ts:1056-1143`, `AI:api/anthropic-messages.ts`, `AI:env-api-keys.ts`, `AI:api/simple-options.ts`.
- **Owns:**
  - H-PROV-01 (the Api/Provider/Model layers).
  - H-PROV-02 (`anthropic-messages`).
  - H-PROV-07 (common request options, with the `maxTokens` clamp).
  - H-PROV-12 (thinking levels and clamp).
  - H-PROV-14 (Anthropic prompt caching).
  - H-PROV-18 (the Model struct). H3 to H6 use one hard-coded model record for each provider. H7 replaces it with the catalog.
  - H-PROV-25 (idle timeout, not a total timeout).
  - H-PROV-26 (the Anthropic quirk data).
  - H-AUTH-05 (the Anthropic env key).
  - H-AUTH-08 (per-request auth).
  - H-PROV-10, function part: the pure `transformMessages` function (`AI:api/transform-messages.ts:64-235`). The Anthropic adapter is its first caller (`AI:api/anthropic-messages.ts:1057`). Moved from H4 (user, 2026-10-01, option C).
  - H-PROV-27 (image input and the non-vision placeholder). Moved from H4 with the function.
  - H-TOOL-21, replay part (synthesize missing results). Moved from H4 with the function.
- **Packages:** `providers`.
- **Tests:**
  - Thinking signatures round-trip unchanged (E§24#2).
  - An idle stream times out, but a long active stream does not.
  - `transformMessages` table tests: errored and aborted assistant messages dropped, missing tool results synthesized (also at the end of the list), foreign thinking becomes text, signatures dropped across models, image placeholder for a non-vision model.
  - A multi-turn session with an errored assistant message and an unfinished tool call is accepted by the API.
  - The full pairing invariant after abort (E§24#1, D19): abort in the middle of a parallel batch leaves calls without results in the loop; the next request through `transformMessages` has exactly one result per call ("No result provided" for the orphans), and the API accepts it.
- **Exit:** `-p` works against a real Anthropic model.

### H4: More wire APIs and cross-provider replay

- **Waits on:** D6 (decided B), D22 (updated 2026-10-05), and the fantasy fork work F1 to F4 and F8 that the Responses adapter needs.
- **Concept:** "compatible" APIs hide real differences. Keep the quirks as data. Replay history across vendors.
- **Analysis:** `plans/reports/xia-261005-1408-h4-more-wire-apis-pi-port-analysis.md` and its five lane reports (`researcher-261005-1354-h4-*`). The edge-case lists there (Completions 38, Responses 36, replay scenarios S1 to S35) are the test source.
- **Targets (user, 2026-10-05, option A):**
  - `openai-completions`: Alibaba Token Plan at `https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1`, model `deepseek-v4.1-flash`, the same key as H3. A live probe on 2026-10-05 confirmed it: thinking in `reasoning_content`, `enable_thinking:false` turns it off, the `developer` role is rejected. Compat as Pi's `qwen-token-plan`: `thinkingFormat:"qwen"`, `supportsDeveloperRole:false`, `supportsStore:false`. Cost 0 (subscription).
  - `openai-responses`: OpenAI `gpt-5.5`, key from `OPENAI_API_KEY` only (user, 2026-10-05, plain Pi name). Prices from OpenAI's price page.
- **Read in Pi:** `AI:api/openai-completions.ts`, `AI:api/openai-responses.ts`, `AI:api/openai-responses-shared.ts`, `AI:api/openai-prompt-cache.ts`, `AI:api/transform-messages.ts` (built in H3; H4 adds the per-vendor parts), `AI:models.ts:1193-1247` (`calculateCost`, `clampThinkingLevel`), `C:core/agent-session.ts:2430-2637` (`setModel`, thinking re-clamp), `C:main.ts:827-834` (per-provider `--api-key`).
- **Owns:**
  - H-PROV-03 (`openai-completions` with the compat record).
  - H-PROV-04 (`openai-responses`), including the `prompt_cache_key` from the session id.
  - H-PROV-10, cross-API part: the per-vendor tool-call id rules and the replay tests across all chosen APIs. The function itself is in H3 (user, 2026-10-01, option C).
  - H-PROV-21, in memory (switch model mid-conversation).
  - H-AUTH-05, OpenAI part (`OPENAI_API_KEY`) and the Token Plan Completions key.
  - H-PROV-26, OpenAI and Token Plan Completions quirk data.
  - H-PROV-14, OpenAI part (`prompt_cache_key`, `prompt_cache_retention`).
  - H-PROV-12, OpenAI part (the `thinkingLevelMap` data and the effort mapping for each API).
  - H-RETRY-07, pricing part (user, 2026-10-05, "làm giống PI"): prices and `tiers` in the model record, one shared `calculateCost`, and the Responses service tier multiplier using the tier echoed in the response. H9 keeps the totals.
- **Also builds:**
  - Model record fields: `BaseURL`, `Headers`, `ThinkingLevelMap`, `Cost` with `Tiers`, `Compat` (Completions and Responses fields), `SamplingParams` (merge in H7). The record replaces `reasoningEffortMap` and `sendSessionIdHeader` with `thinkingLevelMap` and `compat.sessionAffinityFormat`.
  - A provider registry that picks the stream function by `Model.API`, plus `Agent.SetModel` and `Agent.SetThinkingLevel` (auth check first, then re-clamp). The loop reads the model per request through the existing `PrepareRequest` hook.
  - Thinking clamp as in Pi: only the clamped level is stored (user, 2026-10-05, option A). Until H6 settings exist, `high`, then a non-reasoning model, then back gives `off`.
  - Per-provider API keys through the `GetAPIKey` hook. `--api-key` applies only to the initial provider. A key must never go to another provider's host.
  - A guard against the openai-go SDK reading `OPENAI_BASE_URL`, `OPENAI_ORG_ID`, `OPENAI_PROJECT_ID`, `OPENAI_CUSTOM_HEADERS` and the other `OPENAI_*` variables on its own: always pass the key and base URL from the record, and remove the org, project and custom headers.
  - Shared tool-call id normalizers (Anthropic, Completions, Responses) and a deterministic `shortHash`. No collision guard (user, 2026-10-05, follow Pi).
  - Responses replay slots: the reasoning item JSON in `ThinkingSignature`, `{v:1,id,phase}` in `TextSignature`, `call_id|item_id` in `ToolCall.ID`. Port Pi `bc2d8dc1c`: drop the item id when the model differs or the prefix does not match the item type (`fc_` or `ctc_`).
  - Per-wire rendering of mid-conversation system messages and tool deltas for Anthropic, Completions and Responses (user, 2026-10-05). H8 only creates the entries (H-LOOP-15).
  - One session id per headless process for the prompt cache key. H8 replaces it.
  - Error data for H9: status and body in the error text, the stream-end errors mapped to `ErrStreamIncomplete`, and the retry pattern `model is at capacity` (Pi `3874b3e98`).
- **Packages:** `providers` (core types, registry, normalizers, `calculateCost`), `providers/openai` (new adapter), `providers/anthropic` (the H3 adapter, moved from `tokenplan`), `providers/fantasykit` (shared plumbing), `agent` (`SetModel`).
- **Tests:**
  - Replay across all chosen APIs (E§24#2, E§24#3), as offline golden-payload tests on the request JSON (scenarios S1 to S35, except S7 because there is no collision guard). The servers accept invalid input, so live acceptance proves little.
  - Stream completeness for each protocol (E§24#4), with Pi's error texts.
  - A golden test for each compat flag (role, token-limit field, `store`, `stream_options`, thinking format, `reasoning_effort` by map).
  - Usage and cost: cached tokens subtracted, reasoning as a subset of output, the long-context tier, the service tier multiplier.
  - The model switch, modeled on Pi `test/suite/agent-session-model-extension.test.ts`: two `httptest` servers and one agent that switches model between turns (S29); thinking re-clamp (S30 expects `off`); no key for the target fails before any change (S32).
  - With `--api-key` set and a switch, the second server never sees the first key. With every `OPENAI_*` variable set, no org, project or custom header and no foreign base URL reaches the server.
  - SDK retries stay 0 for the Completions and Responses clients (lock-in).
  - A `goleak` test on a real `httptest` server for each adapter: after a normal end, after abort, and after an idle timeout. The adapters add no goroutine; they run inside the `providers.Stream` producer goroutine.
  - One env-gated live test on Token Plan that switches between the Anthropic route and the Completions route with the same key (modeled on Pi `packages/ai/test/cross-provider-handoff.test.ts`). It is manual, because the Token Plan terms limit it to interactive use. A live Responses test runs only when `OPENAI_API_KEY` is set.
- **Do not rebuild:**
  - `reasoningEffortMap` and `sendSessionIdHeader`.
  - Per-thinking-level model variants.
  - The Gemini CLI and Antigravity providers.
  - The ChatGPT sign-in heuristic (`AI:api/openai-responses.ts:40-47`).
- **Exit:** one in-memory conversation switches between Anthropic and OpenAI with correct thinking replay, shown by the Go tests above (user, 2026-10-05: tests only; the user-facing switch is H13 `set_model` and `/model`, and H17 `cycle_model`).

### H5: Built-in tools

- **Waits on:** D3 (stance), D7 (child env names), D8.
- **Concept:** most of a coding agent's quality is in its tools: limits, truncation, process control and safe edits.
- **Read in Pi:** `C:core/tools/` (read, write, edit, edit-diff, bash, grep, find, ls, truncate, path-utils, file-mutation-queue), `C:utils/shell.ts`, `C:core/exec.ts`.
- **Owns:**
  - H-TOOL-01 to H-TOOL-07 (the seven tools, with the 2000-line / 50 KB limits).
  - H-TOOL-11 (prompt snippets).
  - H-TOOL-19 (path handling).
  - H-TOOL-22 (shell and child env; Unix only if D8 = no).
  - H-TOOL-23 (process-tree kill with `Setpgid` and `WaitDelay`).
  - H-SEC-03 (the no-approval stance, per D3).
- **Configuration:** hard-coded defaults here. H6 wires the settings.
- **Packages:** `tools` (`filesystem_*`, `shell*`, `search_*`), `sandbox` (local implementation), `workspace` (session cwd).
- **Tests:**
  - E§24#12, #13 (process kill, bash output).
  - E§24#15, #16, #17, #18 (edit normalization, write serialization, paths, content sniffing).
- **Do not rebuild:**
  - The `glob` and `think` tools.
  - The single-shape edit.
  - An ambient cwd.
  - Native image libraries.
- **Exit:** in print mode the agent reads a repo, edits a file and runs its tests.

### H6: Settings, trust, system prompt, context files and skills

- **Waits on:** D2 (settings loader), D7 (names).
- **Concept:** the system prompt is assembled from named sections, and project-local config is untrusted input.
- **Read in Pi:** `C:core/settings-manager.ts`, `C:core/trust-manager.ts`, `C:core/project-trust.ts`, `C:core/system-prompt.ts:120-216`, `C:core/resource-loader.ts:184-268`, `C:core/skills.ts`, `CD:settings.md`, `CD:security.md`.
- **Owns:**
  - H-CONF-01 (layering), H-CONF-02 (write policy).
  - H-CONF-03 to H-CONF-06 (the key groups).
  - H-CONF-09 (the remaining flags), H-CONF-12 (env vars), H-CONF-14 (config files).
  - H-SEC-01, H-SEC-02 (the trust gate), H-SEC-05 (hygiene).
  - H-PROMPT-01 (named sections), H-PROMPT-02 (`SYSTEM.md`, `APPEND_SYSTEM.md`), H-PROMPT-04 (context files), H-PROMPT-05 (skills), H-PROMPT-09 (prompt stability).
  - H-PROMPT-10 (P1, pulled; self-extension level 1, user decision 2026-10-01): a `docs` prompt section that points to Ask's own docs and examples (skills, templates, MCP, shell hooks, external extensions, settings), so Ask can extend itself as Pi does (`C:core/system-prompt.ts:153-160`). Read only when the user asks about Ask itself.
  - H-PROMPT-08 (the input chain; the `input` and `before_agent_start` slots are no-op stubs until H11).
  - H-TOOL-10 (tool selection with `defaultTools`).
- **Packages:** `settings` (new, per D2), `workspace` (session cwd, project root, trust store), `bootstrap`, `skills`, `agent` (`systemprompt*.go`).
- **Tests:**
  - E§24#22 (config value resolution).
  - A corrupt settings file is never overwritten.
  - No project skill loads before trust.
  - The system prompt is byte-identical across two runs.
- **Do not rebuild:**
  - Any `*.md` file as a skill.
  - The `{baseDir}` placeholder.
  - A pre-trust read of `sessionDir`.
- **Exit:** an `AGENTS.md` file and a skill change the agent's behavior, and an untrusted project's `.ask/` config is ignored.

### H7: Model catalog, resolution and credentials

- **Waits on:** D2 (credential store), D9 (precedence without OAuth).
- **Concept:** model data is data. Credentials have one write path, protected by a lock.
- **Read in Pi:** `C:core/model-resolver.ts`, `C:core/model-config.ts`, `C:core/auth-storage.ts`, `CD:models.md`.
- **Owns:**
  - H-PROV-16 (bundled catalog; P1, pulled; the remote refresh is later).
  - H-PROV-17 (`models.json`).
  - H-PROV-19 (resolution and patterns; the session restore part is in H8).
  - H-AUTH-01 (precedence).
  - H-AUTH-02, H-AUTH-03 (`~/.ask/auth.json` with mode 0600, a cross-process file lock and read-merge-write, per D2).
- **Packages:** `providers`, `settings`.
- **Tests:**
  - A model-id parsing table.
  - Credential concurrency from two processes (E§24#20).
  - E§24#30 (data files with overrides).
- **Do not rebuild:** API keys in the settings.
- **Exit:** `--model anthropic/<id>:high` resolves, and a key saved in `auth.json` is used.

### H8: The session log

- **Waits on:** D10.
- **Concept:** an append-only, typed entry tree is the only source of truth. The provider context is a projection of it.
- **Read in Pi:** `C:core/session-manager.ts`, `CD:session-format.md`, `C:core/agent-session.ts:1096-1125`.
- **Owns:**
  - H-SESS-03 (entry types).
  - H-SESS-06 (the context builder).
  - H-SESS-07 (single-writer ordering).
  - H-SESS-10, P0 part (`-c`, `--session`).
  - H-SESS-15 (a lock across processes: a SQLite lease row, so headless `ask -p` and the leader cannot write one session together). The lease contract:
    - Acquire, renew and release are explicit. Each lease has an ownership generation.
    - Every write transaction checks the generation, so a paused process that resumes after its lease expired cannot write after a new owner took the session.
    - When a process loses ownership, it stops the run.
    - Every database-owning mode (headless, leader, daemon) uses the same bounded busy timeout for SQLite, and one serialized migration path at startup (today migrations run only from the server module: `internal/app/app.go:43-44,87-88`).
  - H-SESS-01, H-SESS-02 (P1, pulled: entry types kept, SQLite storage, schema version).
  - H-COMPACT-13 (`context_edit`).
  - H-LOOP-15 (P1, pulled: system-section and tool-declaration entries, which H10's checkpoint needs).
  - H-PROV-19, session restore part.
  - The session header record in JSON mode.
- **Packages:** `sessions`, `store`, `store/gormstore`, `migrations`.
- **Tests:**
  - Write ordering (E§24#10).
  - Resume restores the model and thinking level.
  - `context_edit` omits an entry from context and keeps it in history.
  - Two processes cannot write one session.
  - A paused writer whose lease expired cannot write after a new owner took the lease.
  - Two processes write two different sessions at the same time without SQLite busy errors.
  - Two processes start on a fresh database at the same time, and migrations run once.
  - After headless changes an API key in `auth.json`, a running leader uses the new key on its next request.
  - Schema migration.
- **Do not rebuild:**
  - `agent.state.messages` as the history (removed in 0.87.0).
  - The v4 lane store.
- **Exit:** kill the process after a turn, resume with `-c`, and continue with the same model.

### H9: Queues, abort, retry, usage

- **Waits on:** D11.
- **Concept:** one run is a state machine. Steering, follow-ups, abort and retry are its transitions.
- **Read in Pi:** `A:agent.ts:299-304`, `C:core/agent-session.ts:1883-1940` (prompt), `:2317-2380` (`clearQueue` and `abort`), `:3611-3712` (retry), `AI:utils/retry.ts`, `AI:utils/overflow.ts`, `C:core/usage-totals.ts`.
- **Owns:**
  - H-LOOP-03 to H-LOOP-07 (the queues, with ids instead of text matching).
  - H-LOOP-17, queue part.
  - H-LOOP-21 (`agent_settled` and the session events). H2 already emits a minimal `agent_settled` right after `agent_end` (D18). H9 moves it after retry, compaction and queued work, and adds `agent_end.willRetry`.
  - H-RETRY-01 to H-RETRY-04 (retry, with the patterns as data).
  - H-RETRY-06 (overflow detection).
  - H-RETRY-07, except the pricing part, which H4 owns (user, 2026-10-05). H9 keeps the usage totals across turns and the usage entries.
  - H-RETRY-08, H-RETRY-09 (totals and the context-usage estimate).
  - H-RETRY-10 (P1, pulled: usage entries).
- **Packages:** `agent`, `providers` (error classification), `scheduler` (lanes only).
- **Tests:**
  - E§24#28 (steer waits for the tool batch).
  - E§24#5, #6, #8 (classification, retry, usage).
  - A 429 is not classified as overflow.
- **Do not rebuild:**
  - `queueMessage` and `queueMode`.
  - Allowing `prompt()` while streaming.
- **Exit:** a faux provider that returns 429 twice and then succeeds gives one turn, correct `auto_retry_*` events and a correct cost.

### H10: Compaction

- **Concept:** context is finite. Summarize at a safe cut point, record the summary as an entry, and rebuild context from the latest compaction.
- **Read in Pi:** `C:core/compaction/compaction.ts`, `C:core/compaction/utils.ts`, `CD:compaction.md`, `C:core/agent-session.ts:2862-3000`.
- **Owns:**
  - H-COMPACT-01 to H-COMPACT-09.
  - H-COMPACT-11 (events).
  - H-COMPACT-14 (P1, pulled: serialized summaries).
  - H-RETRY-05 (P1, pulled: retry for summaries).
  - H-SLASH-01 (`/compact`, as `Session.Compact()`; H13 wires the RPC).
- **Hook points:** `session_before_compact` and `session_compact` are no-op stubs here. H11 wires them (H-COMPACT-12).
- **Packages:** `agent` (`compaction_*.go`), `sessions`.
- **Tests:**
  - E§24#7 (the compaction state machine).
  - A 429 never triggers compaction.
  - The pairing invariant holds after compaction.
  - A second overflow gives the fixed error text.
- **Do not rebuild:**
  - Proactive mid-turn compaction.
  - A separate turn-prefix summary store.
- **Exit:** a long faux session crosses the threshold, compacts, and continues with the correct context and totals.

### H11: Event bus and internal extensions (compiled-in Go)

- **Waits on:** D4.
- **Concept:** every extension point is an event. Some events only notify. Others must answer before the harness continues.
- **Read in Pi:** `C:core/extensions/types.ts:1545-1612`, `C:core/extensions/runner.ts`, `CD:extensions.md`, `inventory-extensions.md` section 4.
- **Build:**
  - The 41-event taxonomy.
  - The sync/notify split (19/22) with the merge rules.
  - Fail-closed on `tool_call`; `user_bash` gets only its type and policy here and is tested in H15.
  - The rest of the H-LOOP-14 hooks.
  - No deadline for these compiled-in handlers (D4). The deadline mechanism for out-of-process calls is built in X1 and documented as a departure from Pi 0.31.0.
- **Owns (P1, pulled):**
  - H-LOOP-12 (early termination).
  - H-COMPACT-12 (compaction hooks).
  - H-PROMPT-03 (`forceSystemPrompt`).
- **Packages:** `hooks`, `hooks/handlers`, `bus`.
- **Tests:**
  - A blocking handler stops the tool, and the model sees the reason.
  - A panic on a notify event does not stop the run.
  - A panic on `tool_call` blocks the call.
  - Handlers run in load order.
- **Do not rebuild:** system messages inside the `context` event (removed in 0.87.0).
- **Exit:** a test-only handler (not shipped) that denies `rm -rf` blocks the call in print mode, and the model sees the reason.

### H12: Observability core ("Watch it think", part 1)

- **Waits on:** D12 (decided).
- **Concept:** one event stream, many watchers. The loop never knows who watches. This is the waku pattern (`waku/app.py:63`, `waku/ops/tracing.py:161-167`).
- **Read:** the waku report, sections 4, 5 and 7.
- **Build:**
  - Bus fan-out with one bounded queue for each subscriber and a drop counter.
  - Redaction and bounded previews before publish.
  - A tracing collector with real-duration spans (run, then turn, then tool attempt), exported through `tracing/otelexport`.
  - Hang detection.
- **Owns (P1, pulled):**
  - H-TELEM-04 (telemetry spans).
  - H-SEC-07 (secrets stay out of logs).
- **Packages:** `bus`, `tracing`, `tracing/otelexport`, `app`.
- **Tests:**
  - A blocked subscriber does not slow a faux run.
  - A secret never reaches a subscriber.
  - Queue overflow, then reconnect during a run: replay has no gap and no duplicate.
  - A cursor older than the buffer, and a leader restart (new epoch), both give `resync` plus a snapshot.
  - Spans have real durations.
- **Do not copy:**
  - Waku's per-event file writes, its line-count cursor and its unredacted traces.
  - Anything under `waku/ops/static/` or `hosted/`.
- **Replay contract (needed by H13 and W1):**
  - `seq` is monotonic per leader process. A leader `epoch` (new on each start) goes with it, so a client can tell a restart.
  - The bus keeps a bounded replay buffer **before** fan-out. A slow live subscriber may drop events, but a reconnecting client replays from the buffer, and the switch from replay to live is atomic (no gap, no duplicate).
  - A cursor older than the buffer, or from another epoch, gets an explicit `resync` answer plus a state snapshot. It never gets a silent gap.
  - Replay covers events only. Commands are never resubmitted.
- **Exit:** a print-mode run exports a trace tree to a local OTel viewer, and a test subscriber sees every event in `seq` order.

### H13: Leader, ACP adapter and gateway (Pi's RPC mode, multi-client)

- **Waits on:** D16 (decided), D17.
- **Concept:** the agent gets one external protocol, ACP. The leader (`ask leader`) holds one agent and routes many ACP clients to it (id rewrite, per-session subscribers, driver client), as Grok's leader does. The daemon is one more ACP client and adds the network gateway. Design: `docs/ask-architecture-reference.md` section 7.3.
- **Split into three steps, each with its own runnable exit:**
  - **H13a ACP adapter over stdio.** `internal/acp` over the agent's Go API, plus the `_ask/*` methods. Exit: `ask acp` works with a scripted ACP client over stdio (prompt, cancel, one `_ask/*` method), and starts no network listener.
  - **H13b Leader over the Unix socket.** `internal/leader`: handshake with a hard version gate, id rewrite, per-session subscribers and driver, `ConnectOrSpawn`, flock, pid, log, 0700/0600 permissions, peer-UID check. Exit: two `ask` TUI stubs share one auto-started leader.
  - **H13c Daemon over ACP plus the network gateway.** `cmd/server` becomes an ACP client of the leader and adds the network gateway (security rules below). Exit: an authenticated remote WS client runs a prompt through the daemon.
  - Each fx composition (headless, leader, editor `ask acp`, daemon) gets its own `fx.ValidateApp` test. Headless and editor modes must start no network listener.
- **External protocol rule (D16):** external agent links carry ACP JSON-RPC only: the leader socket, the remote WS link, and editor stdio. The Pi RPC response envelope stops at this boundary. gRPC stays for internal service APIs only (for example health and admin), never for prompting or tool execution. The X1 extension stdio link is a separate extension-host contract, not an agent link.
- **Sessions and drivers in one agent (from Grok's per-session subscribers):**
  - The one agent in the leader holds many sessions. Each client creates a session or attaches to one.
  - The client that creates or explicitly takes a session is its driver. Other clients are subscribers.
  - `/new` and session switch act on the calling client's view only; they never replace another client's session. A switch on a busy session the caller does not drive is rejected.
  - The multiplexer keeps each client's `initialize` state (capabilities, cwd) and passes only the driver's capabilities for that session to the adapter.
  - Reverse calls go only to eligible clients (driver, or all subscribers for shared questions). When the driver disconnects during a question, the question is cancelled; late or duplicate answers are ignored.
- **Spawn from two binaries (Grok uses one binary, so this part is new):** `ConnectOrSpawn` must start `ask leader` even when `ask-server` calls it. It finds `ask` next to its own executable first, then on `PATH`, checks the version before the spawn, and fails with a clear message when `ask` is absent or incompatible. Connect and readiness waits are bounded, including a live lock holder that has no usable socket.
- **Version mismatch:** reject by default with an upgrade hint. Restart a client-spawned leader automatically only after a verified idle shutdown handshake and the lock release. Before any pid-based signal, check that the pid still belongs to an `ask leader` process.
- **Read in Pi:** `CD:rpc.md`, `CD:rpc-commands.md`, `C:modes/rpc/rpc-types.ts:22-74`, `CD:sdk.md`.
- **Security first (review B1):** the scaffold defaults `host=0.0.0.0` and `cors_origin=*` (`internal/config/config.go:40,45`). Prompting means `bash`, so:
  - Agent methods bind to loopback by default.
  - Every WS connection (and every gRPC admin connection) needs a client token, stored in a 0600 file in the Ask config dir, or mTLS in cloud mode.
  - WS upgrades need an `Origin` allow-list, never `*`.
- **Owns:**
  - H-MODE-06 (framing semantics mapped to ACP over the leader socket and WS; no gRPC agent API).
  - H-MODE-07, P0 part (prompting, state, `set_model`, `get_available_models`, thinking level, compact). `cycle_model` comes in H17 with scoped models.
  - H-MODE-08, for the commands above. The `bash` semantics come in H15 and the `get_entries` cursor in H14.
  - H-MODE-09 (closes Pi's gaps: auth, multi-client).
  - H-MODE-13 (stateless SDK rules).
  - H-SLASH-02 (`/new`) with H-SESS-18 (P1, pulled: session replacement).
  - H-SLASH-03 (`/model`, `/thinking`).
- **Also builds:** the live event feed with `after=<seq>` resume over WS and SSE. `seq` and `runId` travel in ACP `_meta`. `ask acp` (ACP over stdio for editors) comes free with the adapter.
- **Packages:** `acp` (new), `leader` (new), `gateway`, `gateway/methods`, `pkg/protocol`, `cmd/tui` (`ask leader`), `cmd/server`.
- **Tests:**
  - An unauthenticated client is rejected.
  - A cross-origin WS upgrade is rejected.
  - E§24#29 (protocol hygiene).
  - Stale frames are fenced after re-attach, with no auto-replay after reconnect and one writer for each session (timeline section 6).
  - `seq` resume has no gaps.
- **Do not rebuild:** the removed slash-command names. Fix the names before the protocol ships.
- **Tests (leader):** two TUIs share one leader and see the same session updates; two clients with different cwd and capabilities create different sessions; a driver disconnects during a question; `ask-server` starts first from another cwd, and with `ask` missing from `PATH`; a version mismatch arrives while another client runs a tool; a stale, reused pid is not signalled; ids of two clients never collide; a client of a different protocol version is rejected; a peer with another UID is rejected; a stale socket is replaced by the leader that wins the flock; a client disconnect does not cancel the run.
- **Exit (all of H13):** `ask` (TUI stub) and `cmd/server` both connect to one auto-started leader over ACP and run prompt, steer, abort, compact and new session, and receive `agent_settled`. An authenticated remote WS client does the same through the daemon.

### W1: Web monitoring dashboard ("Watch it think", part 2)

- **Waits on:** D12 (decided).
- **Scope (user, 2026-09-30):** monitoring only, read-only, no chat and no control. It is the one allowed web UI (`docs/ask-architecture-reference.md:9`).
- **Depends on:** H1 (envelope), H12 (bus and redaction), H13 (event feed and token).
- **Build:**
  - Embedded static files (`go:embed`) served by `gateway` on a separate `127.0.0.1` listener, with a token and `Host`/`Origin` checks (waku has none).
  - Plain HTML, CSS and JS with no build step. This is a recommendation.
  - SSE `GET /events?after=<seq>`.
- **Views:**
  - Overview: a harness diagram. Each stage lights up for its event (model call, tool, hook, compaction, retry), with a minimum display time. The mapping is one table with a test.
  - Runs: each turn with its model calls, tokens, cost and tool previews.
  - Hang alerts.
  - Session list and cost totals.
- **Packages:** `gateway` (`dashboard*.go` plus assets), `bus`.
- **Tests:**
  - Wrong token, `Host` or `Origin` is rejected.
  - Reconnect has no gaps.
  - A closed tab does not slow the loop.
  - The stage table covers every event type.
- **Do not copy:**
  - Waku's static files, name or look (brand license).
  - Its 450 ms whole-file polling.
  - Its SQL console.
- **Exit:** open the dashboard, run a prompt, and watch the stages light up with correct tokens and cost.

### H14: Session tree

- **Concept:** a session is a tree, not a list. Branches, forks and summaries of abandoned branches are operations on the entry log.
- **Read in Pi:** `C:core/session-manager.ts:1579-1750,1856-1861`, `C:core/agent-session-runtime.ts`, `C:core/compaction/branch-summarization.ts`, `CD:sessions.md`.
- **Owns:**
  - H-SESS-08 (navigation), H-SESS-09 (fork, clone), H-SESS-11 (session dir), H-SESS-12 (names, labels), H-SESS-16 (robustness), H-SESS-17 (custom state), H-SESS-20 (stats).
  - H-COMPACT-10 (branch summary).
  - H-SLASH-04 (session commands).
  - H-MODE-07, session commands, including the `get_entries{since}` cursor.
- **Tests:**
  - E§24#11 (fork and branch correctness).
  - The pairing invariant after a fork.
- **Exit:** fork, clone, resume and a branch summary work over the gateway.

### H15: Commands and resources

- **Concept:** user input is expanded before it reaches the model. Skills, templates and `!cmd` are input transforms, and project resources pass the trust gate first.
- **Read in Pi:** `C:core/agent-session.ts:1883-2062,3745-3843`, `CD:skills.md`, `CD:prompt-templates.md`, `CD:usage.md`, `CD:configuration.md`.
- **Owns:**
  - H-PROMPT-06 (`/skill:name`), H-PROMPT-07 (templates).
  - H-TOOL-24 (`!cmd`).
  - H-SEC-06 (`user_bash` fail-closed test).
  - H-SLASH-06, H-SLASH-08 (`/trust`), H-SLASH-09 (`/reload`).
  - H-CONF-07 (resource keys).
  - Self-extension level 1 (user decision, 2026-10-01): `/reload` also reloads external extensions (after X1), and in M2 also MCP config (H16) and shell hooks (X2), so a resource that Ask writes for itself works without a restart. This includes Go code: Ask can write an external extension in Go under `~/.ask/extensions/<name>/`, and `/reload` builds it (D13 auto-build) and loads it. Only the extension is built, never Ask itself (level 2 is out). Test (M1): Ask writes a skill, runs `/reload`, and uses it in the same session. Test (M2, with H16): the same with an MCP config. After X1: Ask writes a Go extension that registers a tool, runs `/reload`, and calls that tool in the same session.
  - A settings get/set gateway method. This closes the "settings changes" gap of H-MODE-09.
- **Tests:**
  - Template argument expansion (`$1`, `$@`, `${1:-default}`).
  - A failing `user_bash` handler blocks the command.
  - `!!cmd` output stays out of context.
  - `/reload` picks up a new skill without losing the session.
- **Exit:** a template and a skill command expand in print mode and over the gateway.

### H16: MCP client

- **Concept:** external tool servers become tools with a namespace. Their config is project input, so it passes the trust gate.
- **Read in Pi:** `CD:mcp.md`, `C:core/mcp-servers.ts`, `packages/mcp/src`.
- **Owns:**
  - H-TOOL-18 (stdio and HTTP, `mcp.json` after trust, official go-sdk).
  - H-SLASH-07 (`/mcp`).
  - H-TOOL-15, `direct` exposure only.
- **Tests:**
  - A 60 s tool timeout.
  - Text over 20 KB is cut and spilled to a temp file.
  - MCP tool calls are never retried.
  - An untrusted project `mcp.json` does not load.
- **Exit:** an MCP server's tools are callable, and an untrusted project's `mcp.json` is ignored.

### H17: Provider breadth, auth flows and network

- **Waits on:** D9.
- **Concept:** breadth is data plus flows. Each new vendor is a compat record and quirk data. Each login is a flow with timeouts and fallbacks.
- **Read in Pi:** `AI:auth/oauth/pkce.ts`, `AI:auth/oauth/callback-server.ts`, `AI:auth/oauth/device-code.ts`, `AI:api/bedrock-converse-stream.ts`, `AI:api/google-generative-ai.ts`, `CD:providers.md`.
- **Owns:**
  - H-PROV-05 (Bedrock, Google, Mistral), H-PROV-13 (thinking budgets), H-PROV-14 (vendors other than Anthropic and OpenAI), H-PROV-20 (scoped models and the `cycle_model` RPC).
  - H-AUTH-04 (command keys), H-AUTH-05 (vendors other than Anthropic, OpenAI and Alibaba Token Plan), H-AUTH-06 (the P1 subset: ChatGPT and Copilot, plus Anthropic only if D9 = B; not legacy Codex), H-AUTH-07 (flow mechanics), H-AUTH-10 and H-SLASH-10 (`/login`, `/logout` as a gateway method, which T1 `/login` needs), H-AUTH-12 (proxy).
  - H-TOOL-20 (image resize).
  - H-PKG-05 (offline mode).
- **Tests:**
  - E§24#21 (OAuth flows).
  - Cross-provider replay for each new vendor.
  - Offline mode makes no network call.
- **Exit:** OAuth login through the gateway, and a request through a proxy.

## 5. Extension track and TUI track

### X1: External extensions (loaded at run time, no Ask rebuild)

- **Waits on:** D13 (runtime), D4.
- **Model (user, 2026-10-01):** two kinds of extension share one event contract. Internal extensions are Go code compiled into Ask (H11). External extensions are loaded at run time without rebuilding Ask; end users write them, and Ask writes them for itself. An external extension goes through an adapter that speaks the same contract, so it can later be promoted to an internal one without a logic rewrite.
- **Runtime (D13 = B):** Go source in `~/.ask/extensions/<name>/` (and the project `.ask/extensions/`, after trust). On load or `/reload`, Ask runs `go build`, caches the binary by content hash, and starts it as a child process over stdio. Build errors are shown to the user and the model; an old cached binary is never used for changed source.
- **SDK:** a public Go package in `pkg/` (for example `pkg/askext`), because code outside the module cannot import `internal/`. It hides the stdio framing and shares the event and content types with internal extensions (from `pkg/protocol`).
- **Build input contract (needed on a user machine with no Ask checkout):**
  - The module path is `AskCore` today (`go.mod:1`), which an extension cannot fetch. Either publish the SDK under a fetchable module path, or ship the pinned SDK source inside the Ask install and point the build at it with a generated `replace`.
  - An extension is a small Go module (`go.mod` + `main.go`). Ask states the supported Go version.
  - The cache key covers the extension source, its `go.mod`/`go.sum`, the SDK version, the Go version and the target platform.
- **Reload contract:**
  - Reload happens at a safe boundary: after the current tool call finishes. A reload asked for by an extension command is queued, never awaited inside that command.
  - Each child has a generation number. Replies from an older generation are dropped. Pending calls to a stopping child are cancelled.
  - New registrations replace old ones only after the new child is ready.
  - A configured fail-closed hook whose child is not running (crash, build error) stays in an error state, and `tool_call` stays blocked. It is never treated as an unsubscribe.
- **Build:**
  - A handshake with an API version, event subscriptions and `tool_call` tool-name filters.
  - Tool, command and keybinding registration **and unregistration**.
  - Hook methods for the 19 sync events, with deadlines.
  - Droppable notify delivery.
  - The RPC extension UI subset as the wire contract (defined in M1 so the protocol does not break later; rendered in the TUI only in M2, T2).
  - Provider registration (H-PROV-23).
  - Discovery gated by trust.
  - Reload by restarting the child.
- **Read:** `CD:extensions.md`, `CD:rpc-extension-ui.md`, `inventory-extensions.md` sections 1-6 and 10.
- **Lesson:** Pi rebuilt hooks three times in four weeks. Freeze one versioned protocol before the first external author.
- **Tests:**
  - Unchanged source is not rebuilt; changed source is.
  - A build error is reported and the extension stays unloaded.
  - A crash on `tool_call` blocks the call; a crash on a notify event is logged.
  - A hook that passes its deadline (D4) is handled per D4.
  - A project extension does not load before trust.
  - Reload during a `tool_call`, reload asked by an extension command, a late reply from an old child, and a build failure after a loaded guard (the guard keeps blocking).
  - A clean machine with no Ask checkout, with Go missing, with an unsupported Go version, and with an unreachable dependency. Skills and templates keep working in each case.
- **M1 scope (user, 2026-10-01):** extensions register tools and commands and handle hooks. Extension UI dialogs (`confirm`, `select`, `input`, `notify`) are M2, with T2; in M1 a dialog request gets a documented default answer (for example `confirm` = false) and a log line.
- **Exit:** drop `safe-bash/main.go` into `~/.ask/extensions/`, run `/reload`, and see it register `todo_list` and block `rm -rf`, with no Ask rebuild.

### X2: Shell hooks (tier C)

- **Build:** a thin adapter that maps `tool_call`, `tool_result`, `input` and `session_*` to commands.
- **Tests:** exit codes, timeouts, and malformed output.
- **Exit:** a shell script blocks a command by its pattern.

### X3: Later

- Package sources and install.
- Skill and prompt packages.
- Each item gets its own exit when it is scheduled.

### T0: Inline prototype gate (parallel from H2)

- **Where:** a scratch Go module outside the repo `go.mod`, so it needs neither Go 1.26.0 nor D14 first. This is a deliberate change from `inventory-tui.md` section 3, which puts the testkit in `cmd/tui/internal/testkit/`. The testkit moves there in T1.
- **Tests (M1 gate):** G1, G2, G3 and G6 (`inventory-tui.md` section 3). G4 (Kitty image in scrollback) and G5 (fullscreen switch) are M2 checks: record their result, but they do not block D14 or T1.
- **Exit:** the M1 gate result decides D14, including the committer route.

### T1: TUI core

- **Waits on:** H13, D14, D15, D8.
- **Build:** the P0 rows of `inventory-tui.md`:
  - the scrollback committer and the live area;
  - the custom editor core with undo, kill ring and paste markers;
  - markdown and syntax highlighting;
  - footer and status;
  - selectors;
  - keybindings;
  - themes;
  - width and Unicode handling.
- **Harness rows that T1 commands need:** H-SESS-18 (H13), H-SESS-20 (H14) and the H-SESS-10 picker (built in T1), H-AUTH-10 (H17). `/session` and `/resume` are enabled when their phase is done. **`/login` is M2** (user, 2026-10-01): in M1 the user sets an API key with an environment variable (for example `ANTHROPIC_API_KEY`) or in `~/.ask/auth.json`, and the TUI shows that hint when no key is found.
- **Constraint:** the TUI uses one `AgentClient` interface: `remote` (ACP to the leader, or to a remote agent) and `direct` (in-process Go API, the fallback when no leader is reachable). Headless mode uses `direct` (D5).
- **Tests:** E§24#23 to #27.
- **Exit:** a full session in the terminal works: prompt, stream, steer, abort, compact, and resume. The terminal is restored on every exit path.

### T2: TUI P1 and the inspector

- **Waits on:** D12 (decided), D15. Needs W1 (the shared stage table) and X1 (the extension UI subset).
- **Build:**
  - Overlays, images (Kitty placeholders), and the extension UI subset.
  - Optional alt-screen mode (D15).
  - The "Watch it think" TUI inspector: a view over the gateway event feed with the same stage table as W1.
- **Exit:** the inspector and the web dashboard show the same run with the same stages.

## 6. P1 rows not placed above, and later or skip items

| Row or item | Placement | Reason |
|---|---|---|
| H-PROV-23 (provider extension point) | X1 | Needs the extension protocol. |
| H-SESS-10 (picker) | T1 | UI only. |
| H-MODE-12 (SDK as an importable Go package) | After H13, on demand | The gateway covers external clients. |
| H-TOOL-15 exposure modes other than `direct` | With codemode (wait) | Only needed for large tool sets. |
| Harness v4 lanes, `durable`, `sqlite-node`, `pi server`/`client`, `protocol`, `chord` | skip / wait | Experimental and not in Pi's build. The gateway fills the server/client role. |
| Codemode, `tool_search` | wait | One day old in Pi (0.99.0) and redesigned the next day. Needs `sandbox` and `permissions` first. |
| Virtual and classifier models | skip | Experimental. |
| Cache warming timers | P2 | Keep only the prompt-stability invariant. |
| llama.cpp router, `pi-messages`, deferred requests, image APIs | skip | Niche. Local servers work through the OpenAI-compatible `models.json`. |
| Share, bug report, HTML export, `/debug` | P2 / skip | Structured logs replace `/debug`. |
| Telemetry pings, version check, self-update, npm packaging | skip | A Go static binary, and no telemetry by default. |
| Windows, PowerShell, Termux, Nix | per D8 | |
| oh-my-pi comparison | later | Deferred by the user. |
| Self-upgrade (level 2): Ask edits its own source, rebuilds, swaps the binary, rolls back | skip | User decision, 2026-10-01: level 1 yes, level 2 no. Ask's source is changed by developers, who build it as usual. |

## 7. Acceptance of this roadmap

- Every P0 row of `inventory-harness.md` has exactly one owner phase. When a row has a P0 part and a P1 part, each part names its phase (for example H-MODE-07: P0 commands in H13, session commands in H14). Checked by script: 110 of 110 P0 rows are placed, and all 47 P1 rows are placed or listed in section 6. P1 rows are either "(P1, pulled)" in a phase or listed in section 6.
- No phase builds an item from the do-not-rebuild lists. The one deliberate departure (hook deadlines, D4) is documented.
- Each harness phase names its concept, its Pi files, its tests and a runnable exit.
- Decisions are listed in the order of the phase they block. Each blocked phase starts with "Waits on".

## 8. Review findings and their fixes

| Finding | Fix in this revision |
|---|---|
| B1: unauthenticated gateway | H13 "Security first", plus the print-mode no-listener test in H2 |
| B2: P0 coverage | Ownership rule in section 0. H-LOOP-14 (H2, H11), H-TOOL-21 (H2, H3), H-PROV-10 (H3, H4), H-PROV-26 (H3), H-CONF-14 (H6), H-SLASH-01..03 (H10, H13), H-SEC-03 (H5). H-RETRY-07 is owned by H9 (since 2026-10-05: the pricing part by H4) and H-SESS-04/05 by H1. |
| B3: H9b stale | H12 and W1 filled in from the waku report. D12 decided by the user. The envelope is in H1. |
| M1: forward dependencies | New phase order: settings before catalog and credentials; in-memory H2; two event layers; stubs in H6; the 429 test split between H9 and H10 |
| M2: decision order | Section 1 renumbered by the phase each decision blocks. "Waits on" lines added. |
| M3: storage decision | D2 added |
| M4: missing README conflicts | Phase 0 table extended to 12 rows |
| M5: phases too big | Old H3 split into H3, H4 and H7. Old H10 split into H13 to H17. |
| M6: P1 placement | "(P1, pulled)" marks, plus section 6 |
| M7: T1 depends on harness P1 | The T1 "harness rows" line |
| M8: D1 claim | D1 quotes confirmed decision 0. `pipeline` is kept. |
| M9: deadline versus Pi | D4 scopes the departure |
| Minor 1-12 | Applied: E§3 citation, abort range, session-event read, T0 location note, OAuth subset, tool unregister, server/client rules, tool-context struct, Ask names (D7), exits for X/T, cross-process session lock, pairing tests after compaction and fork |

## 9. H4 port analysis decisions (2026-10-05)

Source: `plans/reports/xia-261005-1408-h4-more-wire-apis-pi-port-analysis.md`. The user answered one question per turn. The text above is updated to match.

| Question | Decision (user, 2026-10-05) | Where applied |
|---|---|---|
| H4 targets | A: Token Plan `/compatible-mode/v1` for `openai-completions`; OpenAI `gpt-5.5` for `openai-responses`. One live probe of the Token Plan route was approved and run. | H4 "Targets" |
| Responses wire layer | Patch fantasy ("sửa fantasy, chúng ta có source mà"). D22 stays. | D22 update, H4 "Waits on" |
| Fork use | (a) `go.mod` `replace` to `github.com/kevinle128/fantasy` | D22 update |
| OpenAI key env name | (b) plain `OPENAI_API_KEY` only, as Pi. H3 keeps its two Token Plan names. | H4 "Targets" |
| Thinking level on a switch | (a) as Pi: store only the clamped level | H4 "Also builds", test S30 |
| Package shape | Fantasy is infrastructure. One adapter package for each wire API, shared plumbing in one infrastructure package, vendors as data. | D22 update, H3, H4 "Packages" |
| Tool-call id collisions | (b) as Pi: no guard | H4 "Also builds", tests (S7 removed) |
| Mid-conversation system messages | (a) H4 renders them on all three wires; H8 only creates the entries | H4 "Also builds" |
| Cost | (c) as Pi: prices, tiers, `calculateCost` and service tier pricing in H4 | H1, H4, H9, section 8 |
| Switch demo | (a) Go tests only, as Pi's agent-level tests | H4 "Exit" |
| Pi reference | (a) move to `4c6fb7cfe` (v1.0.1) | Header |
| Apply corrections | (a) roadmap, inventory, providers README and architecture reference | This revision |

Upstream (user, 2026-10-05, option a): send each fantasy fix to `charmbracelet/fantasy` as a PR once it has tests and works in Ask; use the fork until upstream accepts it, then remove the `replace`.

Fork scope (user, 2026-10-05): fix F1, F2, F3, F4 and F8 in the fork before H4 starts ("bao giờ xong thì mới bắt đầu làm H4"). The work is handed off to a Codex agent: brief `plans/reports/handoff-261005-1600-fantasy-fork-responses-fixes.md`, result report `plans/reports/codex-261005-fantasy-fork-responses-fixes.md`. F1 and F2 are not proven necessary by a live OpenAI test (no key yet). **Done 2026-10-05:** all five fixes are pushed (final SHA `08976763bfea`, commits `b8c979f`, `9a2008a`, `a855426`, `88f560d`, `0897676`). `replace charm.land/fantasy => github.com/kevinle128/fantasy v0.0.0-20261005094512-08976763bfea` is in `go.mod` (applied 2026-10-05 at the user's request; `go build`, `go test ./...`, `go test -race` on providers and agent, and golangci-lint pass; `go mod tidy` also moved `github.com/charmbracelet/x/exp/slice` to v0.1.0); the result report gives the adapter mapping for the new metadata types. **Upstream (2026-10-05):** PR #407 (closes #406) is restored to its six commits; the five Responses fixes are in issue #411 and PR #412 (depends on #407). Fork `main` and branch `feat/openai-responses-replay-metadata` both hold `08976763bfea`. Remove the `replace` when both PRs are merged and released.
