# Ask roadmap: rebuild the Pi harness in Go

Date: 2026-09-30, revision 3 (the review and its re-check are applied). The review in `plans/reports/code-reviewer-260930-2254-roadmap-review.md` is applied; section 8 maps each finding to its fix.
Revision 4 (2026-10-01): the H2 port analysis `plans/reports/xia-261001-h2-agent-loop-pi-port-analysis.md` is applied (D18 to D21, and the H2, H3, H9 text).
Revision 5 (2026-10-05): the H4 port analysis `plans/reports/xia-261005-1408-h4-more-wire-apis-pi-port-analysis.md` and the user decisions recorded there are applied (D11, D22, H1, H3, H4, H9, H17 text, and section 9).
Revision 6 (2026-10-06): distribute the accepted provider/auth and tool-contract design across H4, H5, H7, new H7a, H8, H9, H13, H17 and the TUI phases; H4 is not the whole provider program.
Revision 7 (2026-10-06): prioritize Anthropic/ChatGPT/xAI subscription delivery because the user's Alibaba Token Plan expires soon; pull credential persistence into H7a and remove full H5-H7 prerequisites.
Revision 8 (2026-10-06): D19 is revised by the user to the DeepSeek tool-outcome model; the earlier H2/H3 Pi abort model is historical.
Revision 9 (2026-10-06): add D23 (user): every model request can be rebuilt from the session log, by logging the hook changes for each Attempt.
Revision 10 (2026-10-06): add D24 (user): the lifecycle and Event Pipeline redesign is one phase before H8, with a DeepSeek behavior conformance check.
Revision 11 (2026-10-06): add D25 (user): DeepSeek is the default for the lifecycle redesign, with listed exceptions.
Revision 12 (2026-10-06): D25 overrides D1 inside the lifecycle redesign (user); the Pi queue modes `all` / `one-at-a-time` are removed there.
Revision 13 (2026-10-06): add D26 (user): admitted input is committed after request preparation, as DeepSeek; D20 is narrowed on that path.
Revision 14 (2026-10-06): after the source audit `plans/reports/review-261006-1104-deepseek-lifecycle-source-audit.md`: D19 reaffirmed (no drain bound), add D27 (input removal, non-waking added context), D28 (provider Prepare step for the D23 record), D29 (Dispose separate from abort). D29 queue handling corrected to "cleared", as DeepSeek (user, 2026-10-06).
Revision 15 (2026-10-06): the lifecycle redesign is implemented (plan `261006-0933-lifecycle-event-pipeline-redesign`).
The implemented control points, in-memory request reconstruction, queues, retry, tool outcomes, disposal, and follow path are removed from the remaining H8, H9, H11, and H12 work.
Revision 16 (2026-10-06): prioritize the local Leader ACP and TUI path after completed H4, H7a and lifecycle work.
Revision 17 (2026-10-08): H13b (leader over the Unix socket) is implemented (plan `261008-1033-h13b-leader-unix-socket`). The line client `ask connect` is its runnable exit; the T1a TUI replaces it. The repository holds a patched copy of the ACP SDK (`third_party/acp-go-sdk`, one scanner limit raised to 65 MiB).
Reference: Pi 1.0.1, commit `4c6fb7cfe`, at `/Users/dale/Desktop/workspace/opensources/pi` (user, 2026-10-05; before that Pi 0.99.1, commit `2bbfcca4`). Line citations written before 2026-10-05 are at `2bbfcca4`. Each phase checks and fixes the citations it uses.
Inputs: `inventory-harness.md` (H-*), `inventory-extensions.md` (E-*), `inventory-tui.md` (T-*), `timeline.md`, the edge-case report `plans/reports/researcher-260930-2254-pi-edge-cases.md` (E§N; the top-30 list is E§24#N), and the waku report `plans/reports/researcher-260930-2254-waku-watch-it-think.md`.

## 0. How to read this roadmap

- **Goal of the user:** learn how to build an agent harness. The next priority is a TUI that tests the completed harness through Leader ACP. Each harness phase teaches **one concept** and ends with a **runnable, testable result**.
- **Milestones:** see section 0b. M1 is must-have; M2 is nice-to-have.
- **Order (M1 first, then M2):**
  1. M1 next: T0 → H13a → H13b → T1a.
     Then H5-H7 and H8-H12, with T1b integration at each exit, followed by H13c, W1, H14, H15, X1 and remaining T1b work.
     Use the completed H4, H7a and lifecycle implementations; do not rebuild them.
  2. M2: H16, X2, H17, T2, X3 and the built-in permission policy, in any order the user picks.
- **Each phase lists:** the decisions it waits on, the concept, the Pi files to read, the inventory ids it owns, its tests (edge cases), what it must not rebuild, and its exit.
- **Ownership rule:** each P0 inventory row is owned by exactly one phase. Other phases may use a row, but they do not own it. A P1 row that a P0 exit needs is pulled forward and marked "(P1, pulled)". Section 6 places or defers all other P1 rows.
- **Pi path shorthand:** `A:` = `packages/agent/src/`, `AI:` = `packages/ai/src/`, `C:` = `packages/coding-agent/src/`, `CD:` = `packages/coding-agent/docs/`.

## 0b. Milestones (user rule: must-have first, every nice-to-have later)

| Milestone | Phases | Decided |
|---|---|---|
| **M1: must-have** | Phase 0, H1-H3, priority H7a plus required H4 wire work, remaining H4-H7, H8-H13, H14, H15, W1, X1, T0 and T1 | User, 2026-10-01; subscription delivery pulled first on 2026-10-06. |
| **M2: nice-to-have** | Full TUI/gateway login/logout and extension UI dialogs, built-in permission policy (allow, deny, ask), H16 (MCP client), X2 (shell hooks), H17 (more providers/auth methods, scoped models, proxy), T2 (overlays, images, TUI inspector, fullscreen), X3 (packages), Windows | User, 2026-10-01: MCP and shell hooks are not must-have. |

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
| D9 | Subscription auth and Anthropic identity profiles | H7a (store/flows/profiles) | **A:** API keys only. **B:** copy Pi's OAuth identity. | **Superseded by the provider interview and priority update (2026-10-05/06).** Required Anthropic, ChatGPT and xAI access is prioritized after H3, alongside required Responses work. One provider supports injected auth methods but saves one credential. Full TUI login remains later. |
| D10 | Crash-resume in the middle of a turn | H8 | **A:** no (Pi's shipping behavior). **B:** yes (Pi's experimental v4 and `durable`). | **Decided (must have first): A, no crash-resume.** Keep the session schema open for it. |
| D11 | Retry jitter | H9 | **A:** copy Pi (no jitter, `AI:utils/retry.ts:122-126`). **B:** full jitter. | **Decided (must have first): A, copy Pi (no jitter)** for now; add jitter when many sessions share one daemon. Scope (2026-10-05): this is the agent-level retry delay. Pi's provider-level retry has jitter (`AI:utils/provider-retry.ts:66`), but it is off while provider `maxRetries` is 0, which is the Ask default. |
| D12 | "Watch it think" (from waku-agent) | H12, W1, T2 | **Decided (user, 2026-09-30):** a read-only web monitoring dashboard served by the daemon, plus a TUI inspector and OTel export. The web has no chat and no control. | Done. The docs rule is narrowed. |
| D13 | External-extension runtime (internal extensions are compiled-in Go, decided) | X1 | **A:** stdio JSON-RPC subprocess in any language. **B:** Go source that Ask builds on load into a separate binary, cached, over stdio (needs the Go toolchain). **C:** TypeScript through esbuild (Go library) + goja, in process (closest to Pi; no Node APIs, no isolation). **D:** WASM (wazero), sandboxed. Options can be layered. | **Decided (user, 2026-10-01): B only.** External extensions are Go source. Ask builds each one on load (`go build`, cached by content hash) into a separate binary and runs it as a child process over stdio. The stdio protocol exists as the transport, but Go through the `pkg/` SDK is the only supported way to write one. TypeScript (C) and WASM (D) are not planned. A machine without the Go toolchain gets no external extensions; skills, templates, MCP and shell hooks still work there. |
| D14 | Bubble Tea version and inline output route | T1 | Tested v2 with a minimal local renderer patch; exact upstream Ultraviolet | **Decided after T0 acceptance (2026-10-07):** `charm.land/bubbletea/v2` v2.0.10 with the tested local Bubble Tea patch; unmodified Ultraviolet `v0.0.0-20260703014108-f5a850f9c2b7`; retain Go 1.27.0. Use one output owner with ordinary exact-once commits and 120 ms resize purge/replay. T0 physical acceptance is user-reported for Terminal.app/iTerm2, without independent captures. [Decision and limits](../reports/pm-261007-1312-d14-tui-decision.md). Root migration and reproducible product packaging belong to T1; no immutable remote fork release exists yet. |
| D15 | Fullscreen (alt-screen) in the first TUI milestone | T1, T2 | Inline only first, or both | **Decided (must have first): inline only** in the first TUI milestone. Fullscreen is later. |
| D16 | External agent protocol | H13 | **Decided (user, 2026-10-01): ACP** (Agent Client Protocol, JSON-RPC 2.0, JSON) plus `_ask/*` extension methods and `_meta` is the only protocol between the agent process and anything outside it (leader socket, remote agent over WebSocket, editors over stdio). Inside the process and in headless mode: direct Go calls. No protobuf on ACP links; protobuf/gRPC only for internal service APIs and OTLP (Grok does the same: `plans/reports/researcher-261001-0117-grok-protobuf-usage.md`). | Done. |
| D17 | ACP Go SDK | H13 | **A:** `coder/acp-go-sdk` v0.13.5 (no release or maintainer reply since June 2026, per a fork's README). **B:** the fork `lx-wnk/acp-go-sdk` v1.21.0, tracks ACP schema 1.21.0, same module path (needs `replace`). **C:** `caelis-labs/acp-go-sdk` v1.4.0. **D:** generate our own types from the ACP JSON schema. | Keep open until a conformance check passes at H13a: pin the SDK and the ACP schema version separately; prove custom `_ask/*` methods, preserved `_meta`, notifications and `session/cancel` through the leader; map each M1 Go API call to ACP. Note: an ACP `session/prompt` response ends the turn and carries a stop reason, while Pi's `prompt` response only means "accepted"; define when the ACP response is sent relative to `agent_settled` (after retries and compaction). Check the current ACP schema for standard usage updates before adding `_ask/*` usage methods. The same Go API tests run against the direct and the remote client. **H13b note (2026-10-08):** the module stays v0.13.5, but the repository holds a copy at `third_party/acp-go-sdk` (a relative `replace`) with one change: the scanner limit for one line is 65 MiB, not 10 MiB, because the leader carries messages of up to 64 MiB. Record: `third_party/acp-go-sdk/PATCH.txt`. |
| D18 | Terminal record of JSON mode before H9 | H2 | **A:** pull a minimal `agent_settled` into H2. **B:** end at `agent_end` until H9. **C:** defer JSON mode to H8. | **Decided (user, 2026-10-01): A.** H2 emits `agent_settled` right after `agent_end`. The lifecycle redesign now places it after retry and queued work; future compaction must remain inside that final boundary. A JSON reader never changes. |
| D19 | Abort in the middle of a tool batch | H2, H3 | **A:** deviate: every unstarted call gets "Operation aborted". **B:** as Pi: prepared calls get "Operation aborted"; calls after the break point get no events and no result; the loop makes one more stream call with the aborted signal; `transformMessages` (H3) synthesizes "No result provided" at replay. | **Decided (user, 2026-10-01): B, as Pi** (`A:agent-loop.ts:575-577,613-643`; `AI:api/transform-messages.ts:158-180`). The H2 pairing test covers the loop part only. The full pairing test after abort is in H3. **Revised (user, 2026-10-06): DeepSeek model** (answer "B" to the question in `plans/reports/review-261006-0848-lifecycle-pipeline-report-audit.md`). Normal cancellation records an outcome for every call of the batch: `TOOL_ABORTED_BEFORE_DISPATCH` for a call that never started, `ABORTED` for a started body, and drains started bodies before the Step closes. Crash or unexpected failure repair appends `TOOL_NOT_STARTED` or `TOOL_OUTCOME_UNKNOWN` results for the open tail only, and never re-executes a tool (DeepSeek `5badb15`: `packages/core/agent-loop/src/tool-calls.ts:221-260`, `packages/core/session/src/repair.ts:14-197`). **Implemented by the lifecycle redesign.** The earlier H2/H3 Pi abort behavior is history, not the current contract. Design: `plans/reports/architecture-261006-ask-lifecycle-event-pipeline.md` section 9.4. **Reaffirmed (user, 2026-10-06):** started bodies drain with no time bound, as DeepSeek (`tool-calls.ts:233`) and Pi (`A:agent-loop.ts:646`); the Agent stays busy until they return. Each tool must return promptly on `ctx` cancel; process tools kill the whole process group (Pi `bash.ts:126-144`), enforced in H5. |
| D20 | Hook and stream failure contract | H2 | **A:** recover inside the loop. **B:** as Pi. | **Decided (user, 2026-10-01): B, as Pi.** A throw or panic in a tool, `prepareArguments`, validation, `beforeToolCall` or `afterToolCall` becomes an error tool result (`A:agent-loop.ts:769-775,841-847,892-895`). An error from `transformContext`, `convertToLlm`, `getApiKey`, `prepareRequest`, `finishTurn` or the stream function is not caught by the loop. The loop returns it, and the `Agent` wrapper builds the error assistant message plus `message_start`, `message_end`, `turn_end` and `agent_end` (`A:agent.ts:523-548`). |
| D21 | Tool argument coercion | H2 | Pi runs TypeBox `Value.Convert` for TypeBox schemas and a custom table (`AI:utils/validation.ts:59-131`) for plain JSON schemas. Ask has only JSON schemas. | **Decided (user, 2026-10-01):** one Go coercion table for all schemas, based on Pi's custom table plus the union rule (an arm that already validates is kept). No truncation of floats to integers (TypeBox turns `"5.7"` into 5; Ask does not). |
| D22 | Provider wire layer | H2 (Go version), H3, H4, H17 | **A:** Ask's own adapters over HTTP and the H1 SSE reader (Pi's way). **B:** `charm.land/fantasy` (Apache-2.0, v0.45.2, needs Go 1.27.0) behind one Ask adapter. | **Decided (user, 2026-10-01): B.** Go is raised to 1.27.0 (done: build, `go test ./...` and `golangci-lint` pass; a scratch copy with fantasy v0.45.2 also passes `go test -race ./...` and lint). Ask's `pkg/protocol` messages and `providers.Stream` stay the core contract. One adapter maps Ask messages to `fantasy.Call` and `fantasy.StreamPart` to the H1 `Assembler`. Fantasy types never leave that adapter package. Fantasy's own agent loop, retry and tool runner are not used: the loop is H2 and Agent retry is implemented by the lifecycle redesign (fantasy v0.45.2 already sets the SDK retries to 0 in its Anthropic and OpenAI providers, `providers/anthropic/anthropic.go:277`, `providers/openai/openai.go:168`; keep a lock-in test). `transformMessages` stays in Ask (H3). The H1 SSE reader stays unused unless a provider needs it. The dependency is added by the first PR that imports it. **Placement (user, 2026-10-02, option A):** the fantasy adapter is built in H3, not H2. H2 runs on faux only. **Update (user, 2026-10-05):** (1) Fantasy v0.45.2 cannot replay OpenAI Responses reasoning statelessly. The user chose to patch fantasy ("sửa fantasy, chúng ta có source mà"), so D22 stays. Ask uses the fork through `replace charm.land/fantasy => github.com/kevinle128/fantasy <pseudo-version>` in `go.mod` (a local `go.work` is allowed during fork work). The fork already has `bff4512` (inline reasoning replay with `store:false`, encrypted content from `output_item.done`), `76fdec8` (Chat stream must end with `finish_reason`) and `9a5405c` (Anthropic large tool numbers). Fork work still open: F1 message item id and `phase` on replay, F2 function-call item id, F3 Responses `ExtraBody`, F4 status and raw reason on Responses stream errors, F8 the echoed `service_tier` (H4 port analysis, "State of the fantasy fork"). (2) Fantasy is infrastructure ("fantasy là ở tầng infrastructure, các adapter sử dụng cái gì là việc của adapter"). There is one adapter package for each wire API, named for what it serves: `internal/providers/anthropic/` and `internal/providers/openai/`. Each adapter chooses its own libraries. The shared fantasy plumbing (StreamPart-to-Assembler fold, idle timeout, error mapping, witness) is one infrastructure package, for example `internal/providers/fantasykit/`. A vendor such as Alibaba Token Plan is data (provider and model records), not a package. Core files in `internal/providers/*.go` and packages outside providers do not import fantasy or the vendor SDKs (depguard). |
| D23 | Rebuild each model request from the session log | H8, H12 | **A:** as DeepSeek ("model-visible means logged", DeepSeek `5badb15` `docs/architecture.md:127`): for each Attempt, log enough to rebuild the exact request. **B:** do not save the transient request projection. **C:** save only a safe summary and hash for each Attempt. **D:** defer to H8. | **Decided (user, 2026-10-06): A.** For each Attempt, log the difference that request and context hooks made against the selected history (added, removed or changed messages, system prompt, tool set, model and options), not a full copy of the request. The selected history plus these entries must rebuild the exact request the model saw. Credentials, tokens and account identity are never logged. These entries are not ordinary model messages. The entry types and D23 request rebuild are implemented in memory; H8 persists them in SQLite and the dashboard (H12) can show the rebuilt request. |
| D24 | Placement of the lifecycle and Event Pipeline redesign | H2 to H12 | **A:** one redesign phase now, before H8. **B:** a small lifecycle core before H8, the rest in H8, H9, H11 and H12. **C:** all in H9. **D:** defer. | **Decided (user, 2026-10-06): A.** One thorough redesign phase lands before H8, based on `plans/reports/architecture-261006-ask-lifecycle-event-pipeline.md` and its audit `plans/reports/review-261006-0848-lifecycle-pipeline-report-audit.md`. It must be tested in depth and its behavior compared against DeepSeek (`5badb15`) test by test; each divergence from DeepSeek is deliberate and recorded. |
| D25 | Default when Ask and DeepSeek differ in the lifecycle redesign | D24 phase | **A:** DeepSeek by default. **B:** Pi / current Ask by default. **C:** decide each case. | **Decided (user, 2026-10-06): A.** Follow DeepSeek (`5badb15`) by default. Exceptions: decisions already recorded as decided (D4, D11, D18, D20, the tool registry snapshot of 2026-10-05, `End > Continue > Proceed`, all-results tool termination, no proactive mid-turn compaction) and the external JSON event stream of `ask -p --mode json`, which stays Pi-compatible. Each exception is a divergence row in the conformance matrix with a reason and its own test. The user is asked again only for exceptions that are real trade-offs. **Scope against D1 (user, 2026-10-06): D25 overrides D1 inside the lifecycle redesign.** Where Pi and DeepSeek differ in lifecycle behavior (for example, the Pi `all` / `one-at-a-time` queue modes), DeepSeek wins. D1 still holds outside the lifecycle (the dewee package model, sessions as an entry tree). |
| D26 | When the user message is saved: before or after request preparation | D24 phase | **A:** as DeepSeek (`agent-loop/src/agent.ts:408-423`): save admitted input only after request preparation succeeds. **B:** as Pi: save at admission, before preparation. **C:** DeepSeek timing plus a saved D20 error message. **D:** defer to H8. | **Decided (user, 2026-10-06): A.** Admitted input is committed right after the first attempt's request preparation succeeds. On a preparation failure or a cancel during preparation, nothing is committed: no user message and no error message. This narrows D20 on this path: the Agent wrapper still publishes the error events (message_start, message_end, turn_end, agent_end), but the error message is not saved to history. The JSON stream differs from Pi on this one failure path (no user message before the error). A retry never commits the input twice. A missing credential fails inside the stream call, after the commit, so it still gives a saved assistant error. **Implemented by the lifecycle redesign.** |
| D27 | Input removal by ID and non-waking added context | D24 phase | **A:** both, as DeepSeek (`inbox.ts:152`; `agent.ts:535-536`). **B:** removal only. **C:** neither. **D:** defer. | **Decided (user, 2026-10-06): A.** The Agent gets `Remove(inputID)` for a pending input that the model has not received. `AfterTool` can return added context that enters the next step without waking the Agent (DeepSeek `additionalContexts`, used by `hooks-codex` PostToolUse). **Implemented by the lifecycle redesign.** |
| D28 | Where the D23 request record is taken | D24 phase, H8 | **A:** a provider `Prepare` step, as DeepSeek (`llm/src/index.ts:889-891`, `adapterDefaults` in `agent-loop/src/agent.ts:609-613`). **B:** log the redacted wire body. **C:** narrow D23 to the logical request. **D:** defer to H8. | **Decided (user, 2026-10-06): A.** Each adapter computes the safe effective values once in `Prepare` (clamped max tokens, sampling, endpoint path, non-secret headers). The Agent logs that result, and `Stream` uses the same prepared values. Every value that is not logged (credentials, secret headers) is named. Tests compare the rebuilt request with the body the real adapter sends (`httptest`), with defaults and after a model switch. **Implemented by the lifecycle redesign.** |
| D29 | Disposal separate from abort | D24 phase | **A:** `Dispose()` as DeepSeek (`agent-loop/src/index.ts:526-556`; disposed cause blocks wake, `agent.ts:200,220`). **B:** abort only, with a no-wake flag for headless exit. **C:** defer to H13. | **Decided (user, 2026-10-06): A.** `Dispose()` is memoized, closes admission (later calls return `ErrDisposed`), cancels with cause `disposed` (no wake; queues cleared, as DeepSeek `agent-loop/src/index.ts:543` with `agent.ts:175-177`; corrected by the user on 2026-10-06 because the earlier "queues kept" text misdescribed DeepSeek), waits for started tools to drain (D19), closes the writer, and publishes `agent_disposed`. Headless uses it for SIGINT and SIGTERM. **Implemented by the lifecycle redesign.** |

## 2. Phase 0: architecture alignment (documentation only)

Status: done (2026-10-01).
The comparison below is historical scaffold and Pi evidence; current lifecycle ownership is in the package guides and the implemented H2 contracts.

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

The next path is T0 → H13a ACP → H13b Leader → T1a local TUI E2E.
H4 is merged; its sibling verification plan records `DONE_WITH_CONCERNS` with vendor live limits.
H7a records 6/6 completed phases; lifecycle records 12/12.
Use those implementations now; T1a does not wait for H8 storage, H12 monitoring or H13c networking.

After T1a, deliver each row below with its T1b integration before moving to the next row.
Each exit uses a built `ask` through its real Leader socket and ACP.
Use the affected provider and contract checks; a full live-provider matrix is not required at every exit.

| Order | Backend owner and prerequisite | Runnable TUI exit (T1b) |
|---|---|---|
| 1 | H5 tools, then H6 settings/trust | Call builtins and see results/errors; test trusted and untrusted project settings. |
| 2 | H7 catalog, after H6; reuse H7a credentials | Select a catalog model and thinking level; show readiness and failed selection without changing the prior target. |
| 3 | H8 durable log, after H7 and completed lifecycle | Exit, resume and continue the same session; test a competing writer and selection restore without saved tokens. |
| 4 | H9 overflow/usage, after H8 | Show cumulative usage and distinguish overflow, quota exhaustion and transient retry. |
| 5 | H10 compaction, after H9 | Run `/compact`, continue and resume; preserve tool pairing and show compaction events. |
| 6 | H11 internal extensions, after H10 | Show a denied tool outcome and compaction hooks through the existing dispatch. |
| 7 | H12 redaction/tracing, after H11; H13c gateway, after H12 and H13b | Compare local TUI lifecycle updates with an authenticated remote ACP run; test redaction and reconnect cursors. |
| 8 | W1, after H12/H13c | Run a TUI prompt while the read-only dashboard shows its stages and cost. |
| 9 | H14 session tree, after H8/H10 and local ACP | Fork, switch and resume from the session picker; show branch summaries and session stats. |
| 10 | H15 resources, after H6/H11/H14 | Use a template and skill, enforce trust, and reload a new skill without losing the session. |
| 11 | X1 external Go extensions, after H11/H15 | Reload an extension, call its tool, and show its blocked command outcome; keep UI dialogs in M2. |

H13 owns ACP methods added at each feature exit; feature ownership does not move to T1b.
T1a and T1b together retain all T1 P0 inventory rows and full M1 scope.
M2 scope stays unchanged.
Rules behind this order:
- The faux provider and the partial-JSON parser (H1) come before the loop (H2).
- H2 runs in memory (`--no-session` semantics) behind a context-source interface. H8 swaps in the session projection without a rewrite.
- The settings (H6) come before the catalog, credentials and resolution (H7). Tools (H5) use hard-coded defaults until H6 wires the settings.
- The in-memory lifecycle log and retry are implemented before H8.
  H8 makes that log durable; H10 adds compaction entries and projection.
- Agent-core events come in H1. Session events (`agent_settled`, `queue_update`, `compaction_*`, `auto_retry_*`, `entry_appended`) come in the phase that emits them.
- The follow ring and typed dispatch are implemented; H11 maps extension handlers onto them.
  The gateway event feed (H13) comes before W1 and the T2 inspector.

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

- **Status:** implemented, including the lifecycle redesign.
- **Concept:** one Agent driver owns execution, history order, and lifecycle boundaries.
- **Current owners:** [Agent API](../../internal/agent/agent.go), [turn stages](../../internal/agent/loop_stage.go), [typed control points](../../internal/pipeline/README.md), and [headless entry](../../cmd/tui/headless.go).
- **Delivered contracts:**
  - H-LOOP-08 and H-TOOL-21 use the revised D19 outcome model.
    Every requested call gets an outcome on normal cancellation; started bodies drain without a time limit and no further model request is made.
    The earlier Pi model that left unanswered calls for replay is historical.
  - H-LOOP-09: a call runs alone unless its tool declares itself concurrency-safe for these arguments; an exclusive call is a barrier.
    Classification uses the turn snapshot and frozen validated arguments, with a bounded rolling pool.
    See [tool coordinator](../../internal/agent/tool_coordinator.go).
  - H-LOOP-10 and H-LOOP-14 use the typed pipeline registry.
    A pre-tool hook can allow, deny, or cancel; arguments are frozen.
    Host controls use validated copies while Pi tool events preserve the raw arguments.
  - H-LOOP-11 drops tool calls from a truncated max-tokens message and stops normal continuation.
    A Continue decision alone cannot start another request; queued steering can continue the same cycle with its max-tokens reason retained.
    See [model attempt](../../internal/agent/loop_stream.go) and [turn stages](../../internal/agent/loop_stage.go).
  - H-LOOP-17 includes the input queues and separate disposal contract, not a future H9 queue implementation.
  - D26 commits admitted input after preparation; D27 supplies removal and non-waking AfterTool context; D28 captures provider preparation; D29 separates disposal from abort.
- **Evidence:** the [lifecycle conformance matrix](../261006-0933-lifecycle-event-pipeline-redesign/conformance-matrix.md) owns the named acceptance checks and deliberate divergences.
- **CLI contract:** the [headless runner](../../cmd/tui/headless.go), [print projection](../../cmd/tui/headless_print.go), and [JSON projection](../../cmd/tui/headless_json.go) own framing, errors, output failure, and signal exit codes.
  JSON remains Pi-compatible, except for the D26 preparation failure path.
- **Exit:** headless prompts run in process with no leader or database.
  Persistent session selection remains H8 and the interactive client remains T1/H13.

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
  - `transformMessages` table tests: error messages and empty interrupted messages are dropped; nonblank aborted text and thinking replay without tool calls, and unsigned thinking becomes plain text.
    Missing tool results are synthesized, foreign thinking becomes text, signatures are dropped across models, and non-vision models get an image placeholder.
  - A multi-turn session with an errored assistant message and an unfinished tool call is accepted by the API.
  - The earlier Pi orphan-result replay model is historical.
    The implemented D19 coordinator gives every batch call an outcome and repairs an uncertain open turn without re-executing tools; see H2 and the lifecycle conformance matrix.
- **Exit:** `-p` works against a real Anthropic model.

### H4: More wire APIs and cross-provider replay

- **Waits on:** D6 (decided B), D22 (updated 2026-10-05), and the fantasy fork work F1 to F4 and F8 that the Responses adapter needs.
- **Concept:** "compatible" APIs hide real differences. Keep the quirks as data. Replay history across vendors.
- **Boundary (2026-10-06):** API-key wire foundation and in-memory replay only. H5 owns builtin contracts, H7 catalog/store, H7a subscription lifecycle/profiles/headless login, and H13 public switching. Native login is no longer an H4 exit.
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
  - Per-wire rendering of mid-conversation system messages and tool deltas for Anthropic, Completions and Responses (user, 2026-10-05). The lifecycle redesign creates in-memory entries (H-LOOP-15); H8 persists them.
  - One session id per headless process for the prompt cache key. H8 replaces it.
  - Typed provider failures and recovery policy capture are implemented by the lifecycle redesign.
    H9 retains overflow detection and usage accounting, not another text-pattern retry classifier.
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
  - Token-prefix sign-in heuristics; H7a uses explicit method/profile identity.
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
- **Contract (2026-10-06):** reference Pi for builtin input, behavior and output; use its Claude Code compatibility names where semantics match. Preserve custom/MCP contracts and existing names. No `find -> Glob` semantic alias.
- **Output:** reuse H2's result contract: model-facing content, UI/log details and optional programmatic structured content; provider serialization remains in wire adapters.
- **Configuration:** hard-coded defaults here. H6 wires the settings.
- **Packages:** `tools` (`filesystem_*`, `shell*`, `search_*`), `sandbox` (local implementation), `workspace` (session cwd).
- **Tests:**
  - E§24#12, #13 (process kill, bash output).
  - E§24#15, #16, #17, #18 (edit normalization, write serialization, paths, content sniffing).
  - Pi contract parity for names, fields, units, defaults, errors and output limits; the same canonical arguments work across providers.
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

### H7: Model catalog and resolution

- **Waits on:** H6, D2 and the accepted provider interview; use H4's model/wire contracts.
- **Concept:** model data is data; configuration and refreshed catalogs reuse one validated registry and H7a's credential resolver.
- **Read in Pi:** `C:core/model-resolver.ts`, `C:core/model-config.ts`, `C:core/auth-storage.ts`, `CD:models.md`.
- **Owns:**
  - H-PROV-16 (P1, pulled): generated baseline plus Pi remote overlay through an Ask schema adapter, validated cache, ETag/304 and offline catalog operation.
  - H-PROV-17 (`models.json`): compatible provider/model registration without rebuilding; explicit field overrides win after remote refresh. New custom JSON models use Pi defaults (128000 context, 16384 output, text-only, reasoning off, zero omitted prices).
  - H-PROV-19 (resolution and patterns; the session restore part is in H8).
- **Uses:** H7a owns H-AUTH-01/02/03 precedence and credential persistence; model configuration integrates that same store and resolver, without another implementation.
- **Packages:** `providers`, `settings`.
- **Tests:**
  - A model-id parsing table.
  - Model-config resolution uses H7a's saved credentials and respects account/method replacement; the two-process store tests are owned by H7a.
  - E§24#30 (data files with overrides); malformed remote/config data retains the last valid catalog, custom defaults follow Pi, and background refresh does not replace the selected model snapshot.
- **Do not rebuild:** API keys in the settings.
- **Exit:** `--model anthropic/<id>:high` resolves, and a key saved in `auth.json` is used.

### H7a: Subscription auth and request profiles (completed)

- **Status:** completed; the [execution record](../261006-0157-h7a-subscription-auth/plan.md#current-execution-record) records 6/6 phases.
- **Original prerequisites:** H3 Messages and H2 tool/result contracts; OpenAI/xAI inference needed H4 Responses, not full H5-H7.
- **Concept:** credential lifecycle and request shaping are separate from wire serialization.
- **Owns:** H-AUTH-01/02/03 shared precedence and locked credential persistence, H-AUTH-06 for Anthropic/ChatGPT/xAI, H-AUTH-07 shared mechanics, H-AUTH-11 shaping and H-AUTH-10 shared operations/headless commands; breadth and gateway/TUI transport stay H17/T2.
- **Build:** injected supported auth methods, explicit profiles, native login/refresh, safe one-credential replacement and per-request auth. Use compiled provider/model records and existing tools; remote catalog, project settings and complete builtins follow later.
- **Profiles:** Anthropic names/headers/system/betas including named tool choice; ChatGPT public Responses restrictions; xAI device auth and model-gated reasoning. Unknown account access permits inference; known denial does not.
- **Exit:** headless login/logout and inference work for the three subscriptions on the inference host; refresh/replacement and key/subscription request tests pass without billed fallback.
- **Execution detail:** [H7a subscription auth](./phase-h7a-subscription-auth.md); [accepted design](../261005-2139-provider-auth-design/plan.md).

### H8: The session log

- **Waits on:** D10.
- **Already delivered:** typed in-memory entries, the sole-writer log, request preparation capture, and D23 rebuild.
  See [session owners](../../internal/sessions/README.md) and [request log](../../internal/agent/request_log.go).
- **Concept:** an append-only, typed entry tree is the only source of truth. The provider context is a projection of it.
- **Read in Pi:** `C:core/session-manager.ts`, `CD:session-format.md`, `C:core/agent-session.ts:1096-1125`.
- **Owns:**
  - Persist the implemented H-SESS-03 entry types in SQLite; do not define a second lifecycle entry model.
  - H-SESS-06 (the context builder).
  - Extend the implemented single-writer contract to SQLite transactions and cross-process ownership.
  - H-SESS-10, P0 part (`-c`, `--session`).
  - H-SESS-15 (a lock across processes: a SQLite lease row, so headless `ask -p` and the leader cannot write one session together). The lease contract:
    - Acquire, renew and release are explicit. Each lease has an ownership generation.
    - Every write transaction checks the generation, so a paused process that resumes after its lease expired cannot write after a new owner took the session.
    - When a process loses ownership, it stops the run.
    - Every database-owning mode (headless, leader, daemon) uses the same bounded busy timeout for SQLite, and one serialized migration path at startup (today migrations run only from the server module: `internal/app/app.go:43-44,87-88`).
  - H-SESS-01, H-SESS-02 (P1, pulled: entry types kept, SQLite storage, schema version).
  - H-COMPACT-13 (`context_edit`).
  - Persist the implemented system snapshots, request deltas, and tool declarations for H10 checkpoints and D23 rebuild after resume.
  - H-PROV-19, session restore part.
  - The session header record in JSON mode.
- **Packages:** `sessions`, `store`, `store/gormstore`, `migrations`.
- **Tests:**
  - Write ordering (E§24#10).
  - Resume restores the qualified provider/model, method provenance and effective thinking level; it never restores a token snapshot.
  - `context_edit` omits an entry from context and keeps it in history.
  - Two processes cannot write one session.
  - A paused writer whose lease expired cannot write after a new owner took the lease.
  - Two processes write two different sessions at the same time without SQLite busy errors.
  - Two processes start on a fresh database at the same time, and migrations run once.
  - After headless replaces a credential, a leader resolves the latest record for its next request; a method mismatch requires explicit selection instead of silently changing billing.
  - Schema migration.
- **Do not rebuild:**
  - `agent.state.messages` as the history (removed in 0.87.0).
  - The v4 lane store.
- **Exit:** kill the process after a turn, resume with `-c`, and continue with the same model.

### H9: Overflow detection and usage accounting

- **Already delivered:** queues, abort, typed transient retry, billing binding pins, and final agent_settled placement.
  See [Agent owners](../../internal/agent/README.md) and [provider failures](../../internal/providers/failure.go).
- **Concept:** keep context overflow separate from transient failures and account for all completed work.
- **Read in Pi:** `AI:utils/overflow.ts` and `C:core/usage-totals.ts`.
- **Remaining work:**
  - H-RETRY-06: overflow detection.
  - H-RETRY-07: usage totals across turns and usage entries; H4 owns pricing.
  - H-RETRY-08 and H-RETRY-09: totals and the context-usage estimate.
  - H-RETRY-10: persistent usage entries.
- **Retained retry decision:** a retry after visible output is shown on the wire through the Pi auto_retry sequence; the failed attempt is log-only.
  Retry does not erase visible output or silently change a subscription request to a billed API-key route.
- **Tests:** a 429 is not overflow; subscription quota exhaustion is not transient; totals include usage across attempts and turns; the estimate agrees with the selected context.
- **Exit:** overflow classification and cumulative cost agree with the recorded requests and usage entries.

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

### H11: Internal extensions (compiled-in Go)

- **Waits on:** D4.
- **Already delivered:** typed around and decision dispatch, handler ownership and scopes, early termination, and ordered observation.
  See [pipeline contracts](../../internal/pipeline/README.md) and [Agent publication](../../internal/agent/emit.go).
- **Concept:** map extension events onto the existing control and observation paths without adding a second loop.
- **Read in Pi:** `C:core/extensions/types.ts`, `C:core/extensions/runner.ts`, `CD:extensions.md`, and `inventory-extensions.md` section 4.
- **Remaining work:**
  - Map the 41-event taxonomy, including sync versus notify policy, to the existing typed dispatch.
  - Add compiled-in handlers and the extension adapter.
  - Fail closed on tool_call; user_bash policy is completed and tested in H15.
  - Connect compaction hooks (H-COMPACT-12) and forceSystemPrompt (H-PROMPT-03).
  - Keep compiled-in handlers without deadlines; X1 owns out-of-process deadlines under D4.
- **Packages:** hooks and hooks/handlers, registered through pipeline rather than new turn stages.
- **Tests:** load-order mapping, notify failure containment, and fail-closed tool_call.
- **Exit:** a test-only extension denial reaches the model through the existing tool result path.

### H12: Observability core ("Watch it think", part 1)

- **Waits on:** D12.
- **Already delivered:** Agent.Follow, its consistent snapshot and stream baseline, the bounded replay ring and followers, cursor resume, and explicit resync.
  See [follow owner](../../internal/agent/follow.go) and [ring owner](../../internal/bus/follow.go).
- **Concept:** monitoring reads the execution record without owning execution or changing its outcome.
- **Read:** the waku report, sections 4, 5, and 7.
- **Remaining work:**
  - Define dashboard redaction policy and bounded previews; the current trusted in-process follow path is not a redaction boundary.
  - Add run, turn, model attempt, and tool spans with real durations.
  - Export through tracing/otelexport and add hang detection.
  - Integrate the existing cursor and resync contract with H13 and W1.
- **Owns:** H-TELEM-04 and the dashboard/log exposure part of H-SEC-07.
- **Tests:** secrets never reach dashboard subscribers, span durations are real, and hang detection distinguishes a slow operation from a lost completion.
  Existing follow tests remain the regression owner for replay, cursor gaps, epochs, and slow followers.
- **Do not copy:** Waku's per-event file writes, line-count cursor, unredacted traces, or hosted/static application.
- **Exit:** a print-mode run exports a trace tree to a local OTel viewer; a subscriber receives the redacted follow projection that H13c/W1 will expose.

### H13: Leader, ACP adapter and gateway (Pi's RPC mode, multi-client)

- **Waits on:** H13a needs completed H4/H7a/lifecycle and D16/D17; H13b needs H13a; H13c needs H13b, H8, H10 and H12.
- **Schedule:** H13a and H13b are complete; T1a precedes the remaining harness phases.
  H13c follows H12.
- **Early scope:** expose the completed agent API, lifecycle events, follow path, queues, retry, cancellation, model switching and H7a auth through local ACP.
  Reuse the current in-memory log and session boundary.
  Durable resume and compaction become available after H8 and H10.
  Methods whose owners are incomplete return an explicit unsupported error.
  H13 remains the owner of the transport contract.
- **Concept:** the agent gets one external protocol, ACP. The leader (`ask leader`) holds one agent and routes many ACP clients to it (id rewrite, per-session subscribers, driver client), as Grok's leader does. The daemon is one more ACP client and adds the network gateway. Design: `docs/ask-architecture-reference.md` section 7.3.
- **Split into three steps, each with its own runnable exit:**
  - **H13a ACP adapter over stdio.** `internal/acp` over the agent's Go API, plus the `_ask/*` methods. Exit: `ask acp` works with a scripted ACP client over stdio (prompt, cancel, model selection, queues and follow updates), and starts no network listener.
  - **H13b Leader over the Unix socket.** `internal/leader`: handshake with a hard version gate, id rewrite, per-session subscribers and driver, `ConnectOrSpawn`, flock, pid, log, 0700/0600 permissions, peer-UID check. Exit: two `ask` TUI stubs share one auto-started leader.
    **Completed (2026-10-09): repairs and final gates passed.**
    The stub is `ask connect`.
    `ask leader`, `ask leader status|list|stop` and `ask version --json` are the other commands.
    The [plan](../261008-1033-h13b-leader-unix-socket/plan.md) records the original exit evidence and current repair checks.
    The [repair report](../reports/pm-261009-h13b-repairs.md) owns final gate results.
    The [built-client tests](../../cmd/tui/connect_e2e_test.go), [command regression tests](../../cmd/tui/leader_regression_test.go), [shared-host frame tests](../../internal/app/leader_frame_test.go) and [lifecycle tests](../../internal/app/leader_lifecycle_test.go) own the repaired acceptance paths.
    Accepted decisions remain: injected-owner UID evidence with a literal second-user manual gap; take after driver loss even when busy; unbounded client output queues; the outer protocol integer as the version gate.
    See [the leader contract](../../internal/leader/README.md).
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
- **Version mismatch:** reject by default with an upgrade hint.
  Restart a client-spawned leader automatically only after a verified idle shutdown handshake and lock release.
  Linux PID fallback requires a verified pidfd and a matching lock owner.
  macOS and platforms without a stable process handle refuse that fallback.
  The [leader process contract](../../internal/leader/README.md) owns stop safety.
- **Read in Pi:** `CD:rpc.md`, `CD:rpc-commands.md`, `C:modes/rpc/rpc-types.ts:22-74`, `CD:sdk.md`.
- **Security first (review B1):** the scaffold defaults `host=0.0.0.0` and `cors_origin=*` (`internal/config/config.go:40,45`). Prompting means `bash`, so:
  - Agent methods bind to loopback by default.
  - Every WS connection (and every gRPC admin connection) needs a client token, stored in a 0600 file in the Ask config dir, or mTLS in cloud mode.
  - WS upgrades need an `Origin` allow-list, never `*`.
- **Owns:**
  - H-MODE-06 (framing semantics mapped to ACP over the leader socket and WS; no gRPC agent API).
  - H-MODE-07, P0 part (prompting, state, `set_model`, `get_available_models`, thinking level, compact). Shared resolution uses qualified provider/model/method, readiness and capabilities; selection commits at a safe request boundary and failed selection leaves the previous target intact. `cycle_model` comes in H17 with scoped models.
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
- **Exit (all of H13):** `ask` (T1 TUI) and `cmd/server` both connect to one auto-started leader over ACP and run prompt, steer, abort, compact and new session, and receive `agent_settled`. An authenticated remote WS client does the same through the daemon.

### W1: Web monitoring dashboard ("Watch it think", part 2)

- **Waits on:** D12 (decided).
- **Scope (user, 2026-09-30):** monitoring only, read-only, no chat and no control. It is the one allowed web UI (`docs/ask-architecture-reference.md:9`).
- **Depends on:** H1 (envelope), H12 (redaction), H13c (event feed and token).
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

- **Waits on:** H8 durable entries, H10 branch-summary primitives and H13a/H13b local ACP.
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

- **Waits on:** H6 trust/settings, H11 extension handlers, H14 session commands and H13a/H13b local ACP.
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

### H17: Provider breadth, additional auth flows and network

- **Waits on:** H7a for shared auth operations and H13 for gateway/scoped selection.
- **Concept:** breadth is data plus flows. Each new vendor is a compat record and quirk data. Each login is a flow with timeouts and fallbacks.
- **Read in Pi:** `AI:auth/oauth/pkce.ts`, `AI:auth/oauth/callback-server.ts`, `AI:auth/oauth/device-code.ts`, `AI:api/bedrock-converse-stream.ts`, `AI:api/google-generative-ai.ts`, `CD:providers.md`.
- **Owns:**
  - H-PROV-05 (Bedrock, Google, Mistral), H-PROV-13 (thinking budgets), H-PROV-14 (vendors other than Anthropic and OpenAI), H-PROV-20 (scoped models and the `cycle_model` RPC).
  - H-AUTH-04 (command keys), H-AUTH-05 (vendors other than Anthropic, OpenAI and Alibaba Token Plan), H-AUTH-06 (additional methods such as Copilot, not legacy Codex), H-AUTH-07 (additional flow bindings), H-AUTH-10 gateway transport and H-SLASH-10 (`/login`, `/logout`; reuse H7a operations), H-AUTH-12 (proxy).
  - H-TOOL-20 (image resize).
  - H-PKG-05 (offline mode).
- **Tests:**
  - E§24#21 (OAuth flows).
  - Cross-provider replay for each new vendor.
  - Offline mode makes no network call.
- **Exit:** OAuth login through the gateway, and a request through a proxy.

## 5. Extension track and TUI track

### X1: External extensions (loaded at run time, no Ask rebuild)

- **Waits on:** H11 event adapter, H15 reload/resources and H6 trust, plus D13 (runtime) and D4.
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

### T0: Inline prototype gate (next priority)

- **Execution plan:** [deep TDD plan](../261006-1649-t0-inline-prototype-gate/plan.md), with an isolated terminal fixture and iTerm2 acceptance.
- **Where:** a scratch Go module outside the repo `go.mod`, so it needs neither Go 1.26.0 nor D14 first. This is a deliberate change from `inventory-tui.md` section 3, which puts the testkit in `cmd/tui/internal/testkit/`. The testkit moves there in T1.
- **Tests (M1 gate):** G1, G2, G3 and G6 (`inventory-tui.md` section 3). G4 (Kitty image in scrollback) and G5 (fullscreen switch) are M2 checks: record their result, but they do not block D14 or T1.
- **Exit:** the M1 gate result decides D14, including the committer route.
- **Measured resize route:** the user accepted 120 ms inline purge and transcript reconstruction through one output owner.
  The [isolated resize plan](../261007-1156-inline-resize-fix/plan.md) records independent passing headless checks, the separate Ghostty capacity limit, and unchanged root dependencies.
  T0 is accepted and D14 is closed after user-reported Terminal.app/iTerm2 acceptance.
  Exact manual artifacts remain unavailable; the known engine limits remain.

### T1: TUI core

- **Waits on:** T0, H13a, H13b, D14, D15, D8 for T1a.
  T1b waits on each feature's owning phase, not the whole H13 gateway.
- **T1a: test the completed harness through the local TUI.**
  - Build the inline editor, transcript, live markdown stream, status, provider/model selector, error display and terminal restoration.
  - Connect through `ConnectOrSpawn`: TUI → Leader Unix socket → ACP adapter → agent.
    On this acceptance path, a connection failure is visible and must not trigger a silent direct fallback.
  - Use H7a credentials and profiles on the inference host, including refresh and readiness errors.
    Keep full TUI login dialogs in M2; use the completed headless auth commands for login.
  - Expose current model switching, input queues, input removal, cancellation, retry, usage and follow updates through ACP.
    Preserve lifecycle completion, request preparation, tool-outcome and unbounded cancellation-drain rules.
    Map ACP prompt completion to the settled lifecycle boundary; cancellation must not report completed work before that boundary.
  - Test repeated prompts, cross-provider replay, API-key and subscription inference, cancellation followed by a new prompt, visible retries, and provider errors.
  - Test two clients, request-id isolation, session isolation, disconnect and reconnect without automatic prompt resend.
    TUI exit detaches its client; it must not dispose the shared leader agent.
    Explicit cancellation is separate from client exit.
  - Run acceptance with a built `ask`, its actual Unix socket and real providers.
    Verify terminal restoration after normal exit, errors and signals.
    Protocol and local HTTP tests support acceptance but do not replace the real-provider check.
- **T1b: complete the remaining M1 TUI scope.**
  - Connect H5 builtin tools, H6 settings/trust, H7 catalog, H8 durable resume, H9 remaining overflow/usage, H10 compaction, H11 extensions and H12 observability as each phase completes.
  - Connect H14 session tree, H15 commands/resources and X1 extension tools within their M1 scope.
  - Each phase exit includes its real TUI → Leader ACP flow.
- **Build (T1a and T1b together):** the P0 rows of `inventory-tui.md`:
  - the scrollback committer and the live area;
  - the custom editor core with undo, kill ring and paste markers;
  - markdown and syntax highlighting;
  - footer and status;
  - selectors;
  - keybindings;
  - themes;
  - width and Unicode handling.
- **Harness rows that T1 commands need:** H-SESS-18 (H13), H-SESS-20 (H14) and the H-SESS-10 picker (built in T1), H7/H7a model/auth readiness. `/session` and `/resume` are enabled when their phase is done. Full TUI `/login` remains M2 (H17 transport, T2 dialogs); M1 can use H7a headless login on the inference host or environment API keys. Show provider, model, auth method and readiness without assuming entitlement.
- **Constraint:** the TUI uses one `AgentClient` interface: `remote` (ACP to the leader, or to a remote agent) and `direct` (in-process Go API, the fallback when no leader is reachable). Headless mode uses `direct` (D5).
- **Tests:** E§24#23 to #27.
- **Exit (T1a):** the completed H4, H7a and lifecycle behavior works from the terminal through Leader ACP.
- **Exit (full T1):** a full session in the terminal works: prompt, stream, steer, abort, compact, and resume.
  The terminal is restored on every exit path.

### T2: TUI P1 and the inspector

- **Waits on:** D12 (decided), D15. Needs W1 (the shared stage table) and X1 (the extension UI subset).
- **Build:**
  - Overlays, images (Kitty placeholders), extension UI and full login/logout dialogs using H7a operations through H17 gateway transport. Scoped/favorite cycling uses H17; tool/MCP contracts stay with their owners.
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
| Tool result after an aborted or errored call | Drop the result when its tool call does not survive replay, an intentional departure from Pi pass-through behavior (user, 2026-10-06). | H4 replay scenario S16 and `TransformMessages` |
| Mid-conversation system messages | (a) H4 renders them on all three wires; H8 only creates the entries | H4 "Also builds" |
| Cost | (c) as Pi: prices, tiers, `calculateCost` and service tier pricing in H4 | H1, H4, H9, section 8 |
| Switch demo | (a) Go tests only, as Pi's agent-level tests | H4 "Exit" |
| Pi reference | (a) move to `4c6fb7cfe` (v1.0.1) | Header |
| Apply corrections | (a) roadmap, inventory, providers README and architecture reference | This revision |

Upstream (user, 2026-10-05, option a): send each fantasy fix to `charmbracelet/fantasy` as a PR once it has tests and works in Ask; use the fork until upstream accepts it, then remove the `replace`.

Fork scope (user, 2026-10-05): fix F1, F2, F3, F4 and F8 in the fork before H4 starts ("bao giờ xong thì mới bắt đầu làm H4"). The work is handed off to a Codex agent: brief `plans/reports/handoff-261005-1600-fantasy-fork-responses-fixes.md`, result report `plans/reports/codex-261005-fantasy-fork-responses-fixes.md`. F1 and F2 are not proven necessary by a live OpenAI test (no key yet). **Done 2026-10-05:** all five fixes are pushed (final SHA `08976763bfea`, commits `b8c979f`, `9a2008a`, `a855426`, `88f560d`, `0897676`). `replace charm.land/fantasy => github.com/kevinle128/fantasy v0.0.0-20261005094512-08976763bfea` is in `go.mod` (applied 2026-10-05 at the user's request; `go build`, `go test ./...`, `go test -race` on providers and agent, and golangci-lint pass; `go mod tidy` also moved `github.com/charmbracelet/x/exp/slice` to v0.1.0); the result report gives the adapter mapping for the new metadata types. **Upstream (2026-10-05):** PR #407 (closes #406) is restored to its six commits; the five Responses fixes are in issue #411 and PR #412 (depends on #407). Fork `main` and branch `feat/openai-responses-replay-metadata` both hold `08976763bfea`. Remove the `replace` when both PRs are merged and released.

## 10. Provider/auth interview allocation (2026-10-06)

The user requested roadmap allocation because H4 cannot deliver the complete design.
This scheduling update supersedes interview Q1's H4 timing only; native headless login stays with subscription delivery in H7a, and the accepted behavior remains unchanged.
The [H4 report](../reports/xia-261005-1408-h4-more-wire-apis-pi-port-analysis.md) records the decisions; the [design plan](../261005-2139-provider-auth-design/plan.md) holds shared contracts and validation.
H4 owns wire/replay, H5 builtin contracts, H6 settings/trust, H7 catalog/credential persistence, H7a subscription lifecycle/profiles, H8 durable selection provenance, H9 overflow and usage accounting, H13 public switching, H17 breadth/scoped models/proxy, T1 model/auth status and T2 full auth dialogs.
Keep one provider with injected supported auth methods and one saved credential; each model record selects one API.
Pi is the initial remote catalog source; user overrides win, active model snapshots survive background refresh, and custom JSON defaults follow Pi.
All phase exits are cumulative dependencies; no phase may weaken an accepted contract to avoid work.

## 11. Completed subscription priority (2026-10-06)

H7a delivered the subscription priority; its [execution record](../261006-0157-h7a-subscription-auth/plan.md#current-execution-record) records completion.
The next priority is the local TUI path in section 3, using the shared credential store and Anthropic/ChatGPT/xAI request profiles.
H7 later integrates catalog/configuration; H17/T2 keep gateway auth transport and full login dialogs.
Preserve the accepted security, refresh, profile and no-billed-fallback contracts.
Do not require an expired Alibaba Token Plan for acceptance; record live-provider access limits.
