# Pi development history and lessons for the Go rewrite (Ask)

Date: 2026-09-30. Pi source: `/Users/dale/Desktop/workspace/opensources/pi` at commit `2bbfcca4`, version 0.99.1, 281 coding-agent releases. Evidence is the changelogs plus four lane reports in `plans/reports/`. Version numbers cite `packages/coding-agent/CHANGELOG.md` unless a package is named. Reasons are given only when the changelog states them.

## 0. Outcome

1. Pi took 10 months from a private repo (2025-08-09) to today, and 10 weeks from 0.10.0 (2025-11-25) to a full extension platform (0.50.0, 2026-01-26). The agent loop barely changed after 0.17.0. The churn is in four places: the extension API, session ownership, the provider layer, and terminal behavior.
2. The extension contract broke roughly every 2 to 4 minor releases across the whole history, with very short deprecation windows (0.56.3 to 0.87.0 late-era note). Ask should freeze a small extension surface early and grow it on demand.
3. Pi keeps a small core. Plan mode, sub-agents, permission gates, and MCP are examples or built-in extensions, not core (0.34.0, 0.24.0, 0.99.0).
4. The "current direction" (server/client, durable, harness v4) is a second, experimental architecture. It is not the shipping path. Ask should wait on it, not follow it (section 6).

## 1. Merged timeline (14 stages)

Dates are the changelog headings. Sources: early report for stages 1-7, late report for stages 8-14.

### Stage 1: Bootstrap and daily-driver (0.10.0 to 0.13.2, 2025-11-25 to 2025-12-07)

- Theme: a plain TUI agent that people can live in.
- Added: streaming TUI, sessions (`--continue`, `--resume`), 7 tools, thinking mode, `@` autocomplete, `/export` HTML, Anthropic/OpenAI/Google (0.10.0); `models.json` (0.10.1); retry with backoff (0.10.2); RPC mode v1 (0.10.3); multimodal `@path` (0.10.5); file-based slash commands, `/branch` (0.11.0); print mode `-p` (0.12.0); compaction `/compact` (0.12.7); `!cmd` later at 0.14.0; Windows shell config (0.13.1); uniform truncation 2000 lines or 50 KB (0.13.2).
- Architecture: one process, TUI plus agent in one module. RPC added as a second frontend at 0.10.3 (internals split "to support multiple frontends").

### Stage 2: Refactor and ops hardening (0.14.0 to 0.17.0, 2025-12-08 to 2025-12-09)

- Theme: extract the harness from the UI.
- Added: `AgentSession` as the central class (0.15.0, dirs `core/ modes/ utils/ cli/`); RPC protocol redesigned with `RpcClient` (0.16.0); bash mode `!cmd` (0.14.0); `compat` overrides in `models.json` (0.14.0); `xhigh` thinking (0.14.0).
- Architecture: `AgentSession` is the seam every mode (TUI, print, RPC) shares. Compaction simplified in 0.17.0 by removing proactive mid-turn compaction (only overflow-error and post-turn threshold remain).

### Stage 3: Extensibility v1 (0.18.0 to 0.24.5, 2025-12-10 to 2025-12-20)

- Theme: hooks, skills, custom tools, first extension surface.
- Added: hooks with `tool_call` block and `tool_result` modify (0.18.0); auto-retry on 429/5xx (0.18.2); skills (0.19.0, SKILL.md only at 0.20.0); Kitty/iTerm2 inline images (0.21.0); Copilot OAuth (0.22.0); custom tools plus custom TUI render (0.23.0); subagent example, Kitty keyboard protocol, OAuth refresh before each LLM call (0.24.0).
- Architecture: hooks and custom tools are two separate concepts with separate dirs, flags, and settings. That split is the origin of stages 5 and 6 churn.

### Stage 4: Embedding, policy, and auth (0.25.0 to 0.30.2, 2025-12-20 to 2025-12-26)

- Theme: use Pi as a library.
- Added: interruptible tool execution (0.25.0); Gemini CLI and Antigravity OAuth (0.25.0); SDK `createAgentSession()` and project settings `.pi/settings.json` (0.26.0); session lifecycle hooks with `before_*` cancel (0.27.0); `auth.json` and `ModelRegistry` (0.28.0); `SYSTEM.md`, unified `/settings` (0.29.1).
- Architecture: secrets moved out of `settings.json` one release after being put in (0.27.3 then 0.28.0).

### Stage 5: Session tree and API rewrite (0.31.0 to 0.34.2, 2026-01-02 to 2026-01-04)

- Theme: sessions become trees; hook and tool APIs are restructured.
- Added: `/tree`, `id`/`parentId` entries, custom entries, structured compaction with file tracking, `context` event (0.31.0); `steer()` and `followUp()` (0.32.0, `queueMessage` removed); `keybindings.json` (0.33.0); active-tool registry, `registerFlag`, `registerShortcut`, event bus, `plan-mode.ts` example (0.34.0).
- Architecture: session file v1 to v2 (tree). The 0.31.0 break list is the largest in Pi history (see section 4).

### Stage 6: Unified extensions (0.35.0 to 0.45.x, 2026-01-05 to 2026-01-13)

- Theme: one extension concept.
- Added: hooks plus custom tools merged into `extensions/` with one `ExtensionAPI` (0.35.0); Codex OAuth (0.36.0); headless OAuth (0.37.0); `setFooter`, `setEditorComponent`, dialog timeouts (0.37.3, 0.38.0); pluggable tool operations for SSH, `user_bash` event, overlays (0.39.0); `/scoped-models`, `/skill:name` (0.43.0); Bedrock (0.45.0).
- Architecture: 0.35.0 states the goal: "one concept, one discovery location, one CLI flag, one settings entry". Session file v2 to v3.

### Stage 7: Packages, hot reload, and provider breadth (0.46.0 to 0.56.2, 2026-01-15 to 2026-03-05)

- Theme: distribution of extensions plus long-tail hardening.
- Added: fuzzy edit fallback (0.46.0); `input` event (0.47.0); `ctx.compact()` (0.49.0); Pi packages (`pi install`), `/reload`, `registerProvider()` (0.50.0); settings/auth locking (0.50.2, 0.53.0); bash spawn hook (0.51.0); `terminal_input`, `ctx.reload()`, `modelOverrides` (0.52.x); `.agents/skills` (0.54.0); `--offline` (0.55.1); runtime `registerTool` (0.55.4); project-first resource precedence (0.55.0).
- Architecture: the `ResourceLoader` became the only resource entry in the SDK (0.50.0).

### Stage 8: Extension API hardening (0.56.3 to 0.61.1, March 2026)

- Theme: make the contract explicit.
- Added: `before_provider_request` and non-capturing overlays (0.57.0); strict LF-only RPC framing (0.57.0); tool interception moved to agent-core `beforeToolCall`/`afterToolCall`, tools run in parallel by default (0.58.0); tools need `promptSnippet` to appear in the prompt (0.59.0); `--fork` and no startup auto-update of packages (0.60.0); keybinding ids namespaced, `/export`/`/import` JSONL (0.61.0).
- Architecture: hook logic moved down into agent-core (0.58.0). This is the point where extension hooks became part of the core loop contract.

### Stage 9: Everything is a ToolDefinition, stateless SDK (0.62.0 to 0.70.x, 2026-03-23 to 2026-04-28)

- Theme: tools and sessions become explicit values, not ambient state.
- Added: built-in tools as overridable `ToolDefinition` plus `sourceInfo` provenance (0.62.0); multi-edit (0.63.0) then `edits[]` only (0.63.2); `prepareArguments` (0.64.0); `createAgentSessionRuntime()` and `session_start.reason` (0.65.0); tool allowlist by name, explicit cwd, no `process.cwd()` (0.68.0); `/clone` (0.68.0); `terminate: true` tool result (0.69.0); typebox 1.x (0.69.0); `after_provider_response` (0.67.4/0.67.6).
- Architecture: session replacement moved from `AgentSession` methods to a runtime factory (0.65.0). Captured `pi`/`ctx` go stale after `newSession/fork/switchSession` (0.69.0).

### Stage 10: Distribution, supply chain, trust (0.71.0 to 0.79.x, 2026-04-30 to 2026-06)

- Theme: ship safely, with trust.
- Added: Gemini CLI and Antigravity removed (0.71.0); `thinkingLevelMap` (0.72.0); package scope rename to `@earendil-works`, Node 22.19 minimum (0.74.0, 0.75.0); shrinkwrap and disabled lifecycle scripts on self-update (0.75.4); `--exclude-tools` (0.77.0); `ctx.mode` (0.78.1); project trust gate, `project_trust` event (0.79.0); `SHA256SUMS` (0.79.4); `pi update` updates only pi (0.79.7).
- Architecture: project-local config is untrusted until approved (0.79.0).

### Stage 11: Provider runtime consolidation (0.80.0 to 0.82.x, 2026-06-23 to July 2026)

- Theme: model layer decouples from the agent.
- Added: pi-ai global API moved to `/compat` (0.80.0); `agent_settled`, `before_provider_headers` (0.80.4); cache-friendly dynamic tool loading (0.80.7); `ModelRuntime` replaces `AuthStorage` plus `ModelRegistry`, provider-owned `/login`, dynamic catalogs (0.80.8); extensions register full providers, llama.cpp router (0.81.0); constrained sampling, bash env `PI_SESSION_*` (0.82.0).
- Architecture: `ModelRuntime` async facade (0.80.8) is the single provider entry. Catalog data is remote-refreshable with ETag (0.82.1).

### Stage 12: Fullscreen TUI, harness v4, remote client (0.83.0 to 0.84.4, 2026-07-29 to 2026-08-28)

- Theme: alternate-screen UI and a durable session model.
- Added: fullscreen TUI (sticky editor/footer, scrollbars, Mermaid, LaTeX), `AGENTS.override.md`, `samplingParams`, cumulative `message_update` dropped for deltas (0.84.0, quadratic growth); harness v4 lane-based `Session`/`SessionStorage`/`SessionRepo` with durable operation records (0.84.0, also `packages/agent/CHANGELOG.md` 0.84.0, verified at line 100); experimental `PiClient` with CBOR over Unix socket (0.84.0); `powershell` tool, staged atomic self-update (0.84.3); `ui_prompt_start/end` (0.84.4).
- Architecture: a second session model (v4 lanes) appears in `packages/agent`. `chord`, `protocol`, `client`, `server`, `telemetry` packages are born in this window (section 2).

### Stage 13: Transcript-canonical context and cache economics (0.85.0 to 0.87.1, 2026-09-04 to 2026-09-22)

- Theme: the transcript is the only source of context truth.
- Added: `SessionManager.inMemory()` restores external entries (0.85.0); experimental `client` published by accident and pulled in 0.85.1; transcript-backed mid-conversation system prompt and tool changes, prompt-cache warming, `/bug`, `TranscriptContext` (0.86.0); append-only `ContextEditEntry`, actionable `turn_end`/`agent_before_settle`, `SessionManager` canonical, `context_with_system` (0.87.0).
- Architecture: extensions no longer write `session.agent.state.messages`. They append entries (0.87.0).

### Stage 14: Codemode, MCP, virtual models (0.99.0 to Unreleased, 2026-09-29 to 2026-09-30)

- Theme: fewer tokens through tool indirection; model routing.
- Added: MCP client as a built-in extension (stdio, streamable HTTP, OAuth, `mcp.json`); `codemode` tool (QuickJS sandbox, model-written JS calling tools); `tool_search`; tool `exposure` levels; `ctx.executeTool()` nested calls; virtual models; classifier models; `system` theme; session file created at first user message (0.99.0). Unreleased: `codemode` description no longer lists tools or server instructions (cache stability), `describeNamespace()`.
- Architecture: the tool list the model sees is a runtime decision (five exposure levels). Version jumps 0.87.1 (2026-09-22) to 0.99.0 (2026-09-29) with no stated reason.

## 2. Package birth chart (14 packages)

"First commit" is `git log` over the package dir, which is the fair birth date. `package.json` add dates in git for chord, client, codemode, durable, protocol, telemetry all read 2026-07-30 (and mcp/server 2026-06-18), which looks like a rename or move, so they are not used. The repo's first commit is 2025-08-09.

| Package | Role | First commit | First changelog | Source |
|---|---|---|---|---|
| `ai` | providers, streaming, OAuth | 2025-08-17 | before 0.10.0 | git log |
| `agent` (agent-core) | loop, hooks, harness v4 | 2025-08-09 | before 0.10.0 | git log |
| `tui` | terminal UI library | 2025-08-09 | before 0.10.0 | git log |
| `coding-agent` | CLI, tools, sessions, extensions | 2025-10-17 (this path) | 0.10.0, 2025-11-25 | git log, CHANGELOG line 5959 |
| `evals` | behavioral evals | 2026-07-25 | none | git log |
| `server` | routed session server (was "orchestrator") | 2026-07-21 | 0.80.3, 2026-06-30 | triage report |
| `session-backends/sqlite-node` | SQLite session backend (was "storage") | 2026-08-05 | 0.81.0, 2026-07-21 | triage report |
| `protocol` | CBOR envelopes, version 8 | 2026-07-30 | 0.84.0 | git log |
| `client` | transport-neutral client | 2026-07-31 | 0.84.0 | git log |
| `telemetry` | vendor-neutral span contracts | 2026-08-05 | 0.84.0 | triage report |
| `chord` | facet/service runtime, Context, Delta | 2026-08-28 | none | git log |
| `durable` | durable agent harness (was "Pico") | 2026-09-18 | 0.86.0, 2026-09-19 | git log, triage |
| `codemode` | QuickJS tool sandbox | 2026-09-29 | 0.99.0 | git log |
| `mcp` | MCP client | 2026-09-29 | 0.99.0 | git log |

Reading the chart: three packages (`ai`, `agent`, `tui`) carried Pi for its first 12 months. Ten more appeared in the last 10 weeks (2026-07-21 to 2026-09-29). Of those ten, only `mcp`, `codemode`, and `telemetry` are on the shipping path; `chord` has partial use (Context/Delta, 32 files in agent-core per triage). `server`, `client`, `protocol`, `durable`, `sqlite-node`, and `evals` are experimental or dev tooling. Class per package is in the package-triage report.

Conflict check: the triage report dates `durable` and `server` first release (0.86.0, 0.80.3); the late changelog report says harness v4 (0.84.0) lives in coding-agent's world. Verified: harness v4 is in `packages/agent/CHANGELOG.md` 0.84.0, line 100. `durable` is a separate package with no dependents (triage). See unresolved question 1.

## 3. Removed, reversed, or deprecated (do not rebuild)

Merged from both changelog reports. Rows with a version pair show a reversal. "Ask action" is: skip (do not build), design-around, or note.

| Item | Added | Removed / changed | Reason (as stated, else "not stated") | Ask action |
|---|---|---|---|---|
| Wrapper `prompt` RPC command | early | 0.12.0 | align with message format | skip |
| Whole RPC protocol v1 | 0.10.3 | replaced 0.16.0 | "completely redesigned" | design typed protocol first |
| Lenient readline RPC framing | 0.10.3 | 0.57.0 | U+2028/2029 corrupted JSON | strict LF framing |
| Proactive mid-turn compaction | 0.12.7 | 0.17.0 | simplification, race avoidance | skip |
| Separate turn-prefix summary | 0.12.7 | 0.17.0 | simplification | one summary per compaction |
| Any `*.md` as a skill | 0.19.0 | 0.20.0 | match Codex convention | only `SKILL.md` in a dir |
| `{baseDir}` placeholder in skills | 0.19.0 | 0.24.0 | Agent Skills standard | relative paths |
| Single-file custom tool `tools/x.ts` | 0.23.0 | 0.24.0 | allow multi-file tools | dir with entry file |
| Hook `session_start`/`session_switch` | 0.18.0 | folded into `session` 0.23.0, split back 0.31.0 | unification then granularity | granular events from day one |
| `branch` event | 0.18.0 | folded 0.27.0, back 0.31.0 | same | same |
| `tool_result` as string | 0.18.0 | 0.24.0 | text-only lost images | content blocks |
| `apiKeys` in `settings.json` | 0.27.3 | 0.28.0 (next release) | separate secrets from settings | separate secrets file |
| `resolveApiKey` callback | 0.27.6 | 0.31.0 | replaced by `ctx.modelRegistry.getApiKey` | note |
| Settings key over OAuth token | 0.27.3 | fixed 0.27.8/0.31.0 | plan users billed PAYG | OAuth over key |
| Separate `/thinking`, `/queue`, `/theme`, `/autocompact` | 0.14 to 0.25 | 0.29.1 | consolidated to `/settings` | one settings menu |
| `hookTimeout` setting | 0.18.0 | 0.31.0 | "no timeouts; use Ctrl+C" | no hook timeouts |
| `Attachment` type | 0.10.5 | 0.31.0 | one content model | image content blocks |
| `queueMessage()`, `queue_message` RPC | 0.10.0 | 0.32.0 | split into steer/follow-up | two queues |
| `hooks/`, `tools/`, `--hook`, `--tool`, `HookAPI`, `CustomToolAPI` | 0.18/0.23 | 0.35.0 | one concept, one dir, one flag | one extension type |
| slash commands renamed prompt templates | 0.11.0 | 0.35.0 | clash with extension commands | name it once |
| Per-thinking-level Codex model variants | 0.36.0 | 0.37.0 | thinking level is separate | do not multiply models |
| Codex model aliases | 0.36.0 | 0.38.0 | canonical IDs only | note |
| `systemPromptAppend` return | 0.31.0 | 0.39.0 (`systemPrompt` full) | hooks need read plus replace | full prompt in and out |
| Anthropic OAuth `/login` | earlier | removed 0.40.1, restored 0.41.0 | not stated | keep OAuth behind provider abstraction |
| `/branch` (name) | 0.11.0 | renamed `/fork` 0.43.0 | `/tree` arrived | branch = in place, fork = new file |
| `sharp` image lib | early | `wasm-vips` 0.45.4, `photon-node` 0.46.0 | native build failures | no native image deps |
| `pi-internal://` path scheme | 0.47.0 | 0.49.0 | not stated | normal paths |
| `strictResponsesPairing` compat flag | 0.49.1 | 0.49.2 | fixed generally | fix in core |
| Hardware cursor on by default | early | off 0.48.0 | terminal compatibility | default off |
| Startup auto-update of packages | 0.50.0 | 0.60.0 | made explicit | never auto-update |
| Wrapper-based tool interception | 0.39.0 | 0.58.0 | replaced by `beforeToolCall`; enables parallel tools | loop-level hooks |
| Tool prompt text fallback to `description` | early | 0.59.0 | prompt bloat | require snippet |
| `location`/`path`/`source` fields on commands | early | 0.62.0 | replaced by `sourceInfo` | provenance record |
| `ModelRegistry.getApiKey(model)` | 0.28.0 | 0.63.0 | per-request auth and headers | per-request resolve |
| Single-edit shape `oldText/newText` | 0.10.0 | 0.63.2 | mixed shapes caused invalid calls | one edit shape |
| `session_switch`, `session_fork`, `session_directory` | 0.31.0 / 0.57.1 | 0.65.0 | replaced by `session_start.reason` and runtime | one start event with reason |
| Prebuilt cwd-bound tool exports; ambient `process.cwd()` | early | 0.68.0 | explicit cwd everywhere | pass cwd explicitly |
| OSC 9;4 progress default on | 0.69.0 | 0.70.0 | reversed in one release | opt-in |
| Gemini CLI, Antigravity providers | 0.25.0 | 0.71.0 | not stated | skip |
| `compat.reasoningEffortMap` | early | 0.72.0 | model-level `thinkingLevelMap` | per-model map |
| Web UI in CLI package | early | 0.75.4 | not stated | skip |
| Vercel AI Gateway attribution headers | 0.79.5 | 0.80.4 | not stated | skip |
| `/base` selective provider entrypoints | 0.79.8 | 0.80.0 | not stated | note |
| pi-ai global stream API | early | `/compat` 0.80.0 | scheduled for removal | avoid global registry |
| `compat.sendSessionIdHeader` | early | 0.80.7 | `sessionAffinityFormat` | data field |
| SDK `authStorage` + `modelRegistry` | 0.28.0 | 0.80.8 | async `ModelRuntime` | one runtime object |
| Cumulative `message`/`partial` in `message_update` | early | 0.84.0 | quadratic output (#7290) | deltas only |
| Legacy JSONL/in-memory repo APIs | early | 0.84.0 | replaced by v4 lane Session | note |
| Experimental `client` subpaths published | 0.85.0 | pulled 0.85.1 | broke SDK imports (#9132) | never publish experiments |
| `user_bash` continue on handler error | early | 0.86.0 | fails closed | fail closed |
| pi-ai stream `Context` | early | `TranscriptContext` 0.86.0 | system prompt and tools in messages | transcript-first |
| `shouldStopAfterTurn` | 0.72.0 | 0.87.0 | replaced by `finishTurn` | note |
| Assigning `session.agent.state.messages` | early | 0.87.0 | `SessionManager` canonical | append-only |
| Handlers see system messages in `context` | early | 0.87.0 | handlers dropped prompt and tools (#9789) | hide from hooks |
| Tool lists/instructions in `codemode` description | 0.99.0 | Unreleased | broke cache when servers changed | stable description |
| `codemode-deferred` mode | 0.99.0 | Unreleased (alias of `codemode`) | same | skip |

Conflict resolved: the early report says Anthropic OAuth was restored "one day later". CHANGELOG lines 4186 and 4192 show both 0.40.1 and 0.41.0 dated 2026-01-09. Same day, two releases.

Not found removed (these were never added in the range read): built-in sub-agents, built-in plan mode, built-in permission prompts. They exist only as examples (0.24.0, 0.34.0, tool_call gates). Not rebuilding them is a Pi decision, not an omission.

Unclear: 0.12.0 print flags `-P`, `--print-turn`, `--no-markdown`; early report says "treat as superseded, not confirmed removed".

## 4. Extension API evolution

Extension, hook, tool, and SDK-adjacent breaks only. Source: both reports' breaking-change tables.

| Version | Breaking change | Migration |
|---|---|---|
| 0.18.0 | Hooks introduced (`tool_call`, `tool_result`, `pi.send`) | n/a |
| 0.23.0 | Custom tools introduced; hook `session_start`/`session_switch` become `session` with `reason` | use `reason` |
| 0.24.0 | `tool_result.result: string` replaced by `content[]` plus `details`; custom tool dir with `index.ts` | rewrite |
| 0.27.0 | `branch` event folded into `session`; `reset/switch/branch` return `cancelled` | update handlers |
| 0.28.0 | Auth and SDK model APIs replaced (`auth.json`, `ModelRegistry`) | migrate |
| 0.31.0 | Largest break: tree sessions; split events; `pi.send` to `sendMessage`; `ctx.exec` to `pi.exec`; `execute` signature `(id, params, onUpdate, ctx, signal?)`; `entryIndex` to `entryId`; `AppMessage` to `AgentMessage`; `messageTransformer` to `convertToLlm`; `dispose()` removed | rewrite hooks |
| 0.32.0 | `queueMessage` to `steer`/`followUp`; `sendMessage` options object; `prompt()` during streaming throws | use queues |
| 0.33.0 | `isEnter()/isEscape()` removed; `matchesKey()` only | replace |
| 0.35.0 | Hooks plus custom tools merged into extensions; ~25 type/function renames; session v3 | rewrite as extension |
| 0.38.0 | `ctx.ui.custom` factory adds `keybindings`; `LoadedExtension` to `Extension`; `ExtensionRunner` signature | update |
| 0.39.0 | `before_agent_start` returns `systemPrompt` not `systemPromptAppend`; `discoverSkills()` returns object | update |
| 0.43.0 | `/branch` to `/fork` across RPC/SDK/events; async `SessionManager.list` | rename |
| 0.44.0 | `getAllTools()` returns `ToolInfo[]` | update |
| 0.47.0 | `Editor(tui, theme)` constructor | update |
| 0.50.0 | `packages` setting; `ResourceLoader`; SDK discover functions removed | migrate |
| 0.51.0 | `ToolDefinition.execute` order `(id, params, signal, onUpdate, ctx)` (third signature) | reorder |
| 0.52.10 | `ContextUsage` tokens/percent nullable | handle null |
| 0.53.0 | `SettingsManager` async writes (`flush()`); `AuthStorage` factory-only | update |
| 0.55.0 | Project-first precedence; extension name conflict: first registration wins | none/check |
| 0.57.0 | RPC LF-only framing | split on `\n` |
| 0.59.0 | Tools omitted from prompt unless `promptSnippet` | add snippet |
| 0.61.0 | Keybinding ids namespaced (`app.tools.expand`) | auto-migrates config |
| 0.62.0 | `renderCall/renderResult` return `Component`; `sourceInfo` replaces path fields | field map |
| 0.63.0 | `getApiKey` to `getApiKeyAndHeaders` | update |
| 0.65.0 | `session_switch`, `session_fork`, `session_directory` removed; `AgentSession.newSession/switchSession/fork` removed | `session_start.reason`, runtime factory |
| 0.68.0 | `createAgentSession({tools})` takes names; prebuilt tool exports removed; explicit cwd | factories |
| 0.69.0 | typebox 1.x; captured `pi`/`ctx` throw after session replacement | `withSession` |
| 0.72.0 | `reasoningEffortMap` to `thinkingLevelMap` | move |
| 0.80.0 | pi-ai global API to `/compat` | imports |
| 0.80.8 | `modelRuntime` replaces `authStorage` + `modelRegistry` | await runtime |
| 0.84.0 | `message_update` deltas only; headers may be `null`; harness v4 Session API; provider refresh context read-only | rewrite consumers |
| 0.86.0 | `TranscriptContext`; `ToolCall.arguments` and `details` JSON-only; `user_bash` fails closed | update |
| 0.87.0 | `shouldStopAfterTurn` removed; `SessionManager` canonical; `ContextEditEntry`; `turn_end` via `emitBoundary` | append entries |

Pattern: the tool `execute` signature changed 3 times (0.23.0, 0.31.0, 0.51.0). Session events flipped folded/split/replaced (0.23.0, 0.31.0, 0.65.0). The one durable win was 0.35.0 (single extension concept).

## 5. Hard areas

Combines the lane C keyword counts (approximate, overlapping, about 1,820 fixes) with churn seen in the changelog history. "Churn" is how often the design itself changed, not only bug fixes.

| Rank | Area | Fixes (lane C) | Design churn (changelog) | Why it stays hard | Ask stance |
|---|---|---|---|---|---|
| 1 | Session, fork, branch, resume, JSONL | ~240 | linear to tree (0.31.0), v3 (0.35.0), runtime (0.65.0), v4 lanes (0.84.0), canonical (0.87.0) | many entry points, races, file durability | design early; single-writer goroutine |
| 2 | Thinking/reasoning replay | ~190 | `xhigh` (0.14.0), `thinkingLevelMap` (0.72.0), `max` (0.80.6) | each vendor differs; OpenAI Responses replay fixed at 0.49.2, 0.56.3, 0.80.3, 0.84.3 | per-model data record |
| 3 | OpenAI-compatible and Responses streams | ~189 | `compat` block (0.14.0) | "compatible" hides server differences | compat record as data |
| 4 | Packaging/install/update | ~177 | packages (0.50.0), no auto-update (0.60.0), staged update (0.84.3) | npm/pnpm/Bun specific | mostly skip |
| 5 | Extensions | ~142 | four restructures in 5 weeks, then more | lifecycle, stale context | small stable surface |
| 6 | Width, Unicode, ANSI, OSC | ~136 | none | fixed about 40 times in `tui` | use uniseg/runewidth, property tests |
| 7 | Context overflow and compaction | ~134 | proactive removed (0.17.0), structured (0.31.0), retain-none (0.87.0) | per-vendor error strings; state machine | data-driven classifier, one mutex |
| 8 | Rendering, flicker, overlay, scrollback | ~129 | fullscreen (0.84.0) | differential renderer | see TUI report |
| 9 | Autocomplete, paste, editor | ~128 | paste markers | IME, history | test fixtures |
| 10 | Tool call/args/schema | ~124 | edit shape (0.63.2), `prepareArguments` (0.64.0) | vendors reject different schema keywords | normalize before validate |
| 11 | Cache, pricing, cost, usage | ~113 | cache warming (0.86.0) | wrong tier, double counts, fixed about 25 times | price table with TTL |
| 12 | Catalog and defaults | ~111 | dynamic catalogs (0.80.8) | model churn monthly | generate from data |
| 13 | Symlink, path, find, grep | ~115 | none | cross-platform paths | canonical path helper |
| 14 | Abort, cancel, race | ~108 | `ctx.signal` (0.63.2) | concurrency | `context.Context` + goleak |
| 15 | Bash, process, shell | ~106 | operations (0.39.0), `powershell` (0.84.3) | process tree kill, Windows shells | `Setpgid`, `WaitDelay` |
| 16 | Auth, OAuth, credentials | ~103 | `auth.json` (0.28.0), lock (0.53.0), `ModelRuntime` (0.80.8) | locking, refresh, precedence | cross-process lock |
| 17 | Keyboard, Kitty, tmux | ~97 | keybindings (0.33.0), namespaced (0.61.0) | protocol matrix | key decode table tests |
| 18 | Retry, backoff | ~82 | 2/4/8 s (0.18.2), per-vendor list grows in 25+ releases | error strings | data table |
| 19 | Windows/WSL | ~68 | shellPath (0.13.1) | every subsystem has a variant | decide launch scope |
| 20 | Images | ~74 | `sharp` to WASM (0.45.4 to 0.46.0), resize (0.32.0) | format sniffing, limits | magic bytes, pure Go |

Two single most repeated fixes: retry/overflow error classification by string match (new vendor strings in 25+ releases) and Kitty/legacy key decoding (30+ releases). Both are best expressed as data tables plus fixtures.

Top-5 cross-check: the changelog design churn (extensions, sessions, provider runtime) agrees with lane C counts. Where the counts say packaging is #4, the design churn says it is Pi-specific and Ask can skip it.

## 6. Current direction (0.84.4 to Unreleased) and what it means for Ask

Evidence base: late report, package-triage report, CHANGELOG lines 3-310 and 577-600, `tui-plan.md` at the repo root.

| Direction | What Pi is doing | Evidence | Ask decision | Reason |
|---|---|---|---|---|
| Server/client (`server`, `client`, `protocol`, `chord`) | Experimental remote-session client, CBOR over Unix socket, multi-presentation attach | 0.84.0 client; 0.85.0 published by mistake; 0.85.1 pulled ("source-only through `pi-test.sh`", CHANGELOG line ~300); protocol at version 8 in about 2 months | Skip | Ask is already a daemon with `gateway`, `pkg/protocol`, gRPC. Copy three rules only: fence stale frames after re-attach, no auto-replay after reconnect, single writer per session |
| Durable (`durable`, harness v4 lanes) | Crash-resume mid-turn, replay-safe tool reruns, structured concurrency | agent 0.84.0 line 100; `durable` first changelog 0.86.0; no dependents in the workspace | Wait | Experimental, breaks every release. Decide first whether Ask wants mid-turn crash resume (unresolved 2). If yes, design on `store/gormstore` with an idempotency flag on tools, and read `durable/README.md` |
| Transcript-canonical context | Append-only entries are the only writer; `ContextEditEntry`; system prompt and tools stored in transcript | 0.86.0, 0.87.0 | Follow | Strongest signal and cheap to adopt at design time in `sessions`. Fixes real bugs (0.87.0, #9789) |
| Cache economics | Cache warming, stable descriptions, cache-safe tool loading | 0.80.7, 0.86.0, Unreleased | Follow (invariant only) | Keep volatile data out of system prompt and tool descriptions. Skip cache warming timers at first |
| MCP | Built-in extension: stdio and HTTP, OAuth, `mcp.json`, project trust gate | 0.99.0 | Follow | Ask already has `internal/mcp`. Add the trust gate from 0.79.0 for project-local config |
| Codemode and tool exposure | Model-written JS in QuickJS; `tool_search`; five exposure levels; nested `ctx.executeTool()` | 0.99.0, Unreleased (design already changed once, the day after) | Wait | One day old, still changing. Verify a Go JS engine (goja, or QuickJS through wazero) supports async before committing. Needs `sandbox` and `permissions` first |
| Virtual and classifier models | Extension routes each request to a physical model | 0.99.0 (experimental) | Skip | Experimental, no clear Ask need |
| Alt-screen TUI (fullscreen mode) | Sticky editor/footer, scrollable transcript, mouse, Mermaid, LaTeX; a `TuiAltScreen` layout system (`VStack`, `HStack`, `ScrollView`) | 0.84.0, 0.84.2, 0.84.4; `tui-plan.md` core decision 2: main-screen keeps terminal scrollback | Wait for main-screen; alt-screen is a later option | Pi kept main-screen as the primary mode; fullscreen is an opt-in with about 60 mouse and selection fixes. bubbletea makes alt-screen easy, main-screen with fixed bottom editor hard. Do main-screen first, see the TUI report |
| Supply chain / trust | Project trust, checksums, staged atomic update | 0.79.0, 0.79.4, 0.84.3 | Follow (trust gate only) | Project config is untrusted input. Skip self-update unless Ask ships binaries |
| Model layer decoupling | `ModelRuntime`, remote catalog with ETag | 0.80.8, 0.82.1 | Follow | Provider config as data; matches AskCore `providers` |

## 7. Lessons for the Go rewrite

Fifteen lessons, each tied to a version. They are ordered by leverage.

1. Design ONE extension concept up front: one API object, one context, one discovery dir, one flag, one settings key. Pi built hooks (0.18.0), custom tools (0.23.0), restructured both (0.31.0), and merged them (0.35.0) in 4 weeks. Ask maps this to `hooks`, `skills`, `tools`, `mcp` plus one registry.
2. Start the session as a tree with typed entries. `id`/`parentId`, custom entries, labels, and a versioned header belong in v1 (0.31.0 broke everything; 0.35.0 bumped to v3). Add a migration path for old files from day one.
3. Make an append-only entry log the only writer of context. Hooks append entries; they do not mutate agent state (0.87.0 `ContextEditEntry`). Do not hand system prompt and tool declarations to handlers (0.87.0, #9789).
4. Two queues from the start: steer (after the current tool batch) and follow-up (when idle). `prompt()` while streaming is an error (0.32.0). Steer waits for the whole tool batch (0.58.4).
5. Fix tool context injection before shipping a tool API. The `execute` signature changed three times (0.23.0, 0.31.0, 0.51.0). Pass `ctx`, cwd, abort signal, and update callback in one struct.
6. Tools are selected by name allowlist per session. Never use ambient cwd (0.68.0). Give every tool, command, and skill a provenance record (0.62.0 `sourceInfo`).
7. Keep a small tool set (read, write, edit, bash, grep, find, ls, 0.34.0). Plan mode, sub-agents, and permission gates are extensions or examples. Do not build them into core.
8. Use exactly one edit shape. Mixed single and multi edits caused invalid calls (0.63.2). Add a `prepareArguments` step before schema validation because old sessions outlive schemas (0.64.0).
9. Put per-model quirks in a data record, not URL checks: roles, strict schemas, thinking format, token-limit field, cache header (0.14.0 `compat`, 0.72.0 `thinkingLevelMap`, 0.80.7 `sessionAffinityFormat`). Ship the model catalog as data, not code (0.82.1 ETag refresh).
10. Classify errors (overflow, rate limit, transient) with a data table plus a fixture per vendor. Pi added new strings in 25+ releases. A 429 never triggers compaction (0.50.2).
11. Compaction has two triggers only: overflow error and post-turn threshold (0.17.0 removed proactive). Use one mutex across manual, threshold, and overflow. Latest compaction entry wins (0.52.10). Structured summary with file tracking (0.31.0).
12. Secrets and settings live in separate files, written with lock plus read-merge-write, and never overwritten on parse failure (0.27.3 to 0.28.0, 0.53.0; invalid JSON wiped at 0.50.4). Precedence: OAuth token over stored key (0.27.8).
13. Send streaming deltas only, never cumulative partials (0.84.0, quadratic). Use strict framing for line protocols and avoid `bufio.Scanner` line limits (0.57.0 U+2028/2029; Go 64 KB scanner limit is a Go-specific version of the same bug).
14. Project-local config is untrusted (0.79.0). No auto-update of extensions at startup (0.60.0). Never publish experimental packages in a release (0.85.0 to 0.85.1).
15. Decide names before publishing the wire protocol: clear/new, branch/fork, template/prompt, exit/quit each touched RPC, SDK, events, and settings (0.29.0, 0.43.0, 0.51.3, 0.52.6). A wire rename after release is a full break.

Extra (not counted): prompt-prefix stability is an invariant (ISO date only in the system prompt 0.58.0; stable `codemode` description, Unreleased). Process-tree kill and terminal restore on every exit path are day-one work (0.84.4, 0.50.6, 0.79.4).

## 8. Limits of this synthesis

- Built from four lane reports plus spot checks: CHANGELOG lines cited above, `packages/agent/CHANGELOG.md` line 100, git first-commit dates, `tui-plan.md` head. No source-code behavior was verified beyond that.
- Stage boundaries inside 0.56.3 to 0.82.x use month-level dates from the late report, except where a heading was checked (0.57.0, 0.62.0, 0.65.0, 0.68.0, 0.71.0, 0.75.0, 0.79.0, 0.80.0, 0.83.0).
- Reasons for removals are recorded only when the changelog states them.
- Package git dates can hide earlier history under other names (server was "orchestrator", durable was "Pico", sqlite-node was "storage").

## Unresolved questions

1. Resolved: the shipping coding-agent uses `SessionManager` JSONL v3. This is verified by `coding-agent/src/core/session-manager.ts:41` (`CURRENT_SESSION_VERSION = 3`). Harness v4 is imported only under `src/experimental`, which the build excludes (`tsconfig.build.json:19`) and `PI_EXPERIMENTAL=1` gates (`src/core/experimental.ts:1`).
2. Does Ask want crash-resume mid-turn? It decides whether `durable` is a design reference or ignorable.
3. Is alt-screen mouse UI in scope for `cmd/tui`? Pi's roughly 60 fullscreen fixes apply only if yes.
4. Why were Gemini CLI and Antigravity removed (0.71.0), and why did Anthropic OAuth vanish and return on the same day (0.40.1, 0.41.0)? The changelog gives no reason.
5. Why did the version jump from 0.87.1 (2026-09-22) to 0.99.0 (2026-09-29)? Treated as a milestone bump; unconfirmed.
6. Resolved: `0.67.67` is a real release, dated 2026-04-17. It is in `agent/CHANGELOG.md:381`, `ai/CHANGELOG.md:1176` and `tui/CHANGELOG.md:489`, but not in the coding-agent changelog. The edge-case rows that cite it are valid.
7. Will Pi's experimental server/client/durable branch become the default in a later release? If so, this "wait" call changes.
8. Does Ask ship an in-process extension API, or only MCP subprocess tools? It decides how much of the extension churn in section 4 applies.
