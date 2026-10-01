# Pi coding-agent changelog, late era (0.56.3 to Unreleased)

Source: `/Users/dale/Desktop/workspace/opensources/pi/packages/coding-agent/CHANGELOG.md` lines 1-3016, commit 2bbfcca4. Lane B2.
Scope note: I read every Added, Changed, Removed, Breaking, and New Features entry in full. Fixed entries were only filtered by keyword for design signals (revert/restore/no longer/removed). Versions cited below are the source of each row. "Inherited" in the changelog means a change that came from pi-ai, pi-agent-core, or pi-tui, not from coding-agent itself.

Honest headline: this window is mostly provider/model churn, terminal UI polish, and extension-API growth. The agent loop itself barely changes. Almost every architectural change is about (a) who owns session state, (b) how models/auth are resolved, (c) how tools are exposed to the model.

## 1. Timeline (8 eras)

| Era | Versions | Dates | Theme |
|---|---|---|---|
| 1. Extension API hardening | 0.56.3 to 0.61.1 | Mar 2026 | Keybinding ids namespaced, JSONL export/import, `--fork`, RPC strict LF framing, `before_provider_request`, parallel tool execution via agent-core hooks |
| 2. Everything is a ToolDefinition | 0.62.0 to 0.65.0 | 2026-03-23 to 04-03 | Built-in tools become overridable `ToolDefinition`s, `sourceInfo` provenance, `prepareArguments`, edit tool becomes `edits[]` only, session runtime API replaces in-session replacement, unified diagnostics |
| 3. Stateless SDK, tool allowlists, hooks | 0.66.0 to 0.70.x | Apr 2026 | Tools selected by name not by cwd-bound instance, no ambient `process.cwd()`, `/clone`, `terminate: true` tool results, TypeBox 1.x, `after_provider_response`, `message_end` replacement, Google CLI/Antigravity removed |
| 4. Distribution and supply chain | 0.71 to 0.79.x | Apr to Jun 2026 | npm shrinkwrap, SHA256SUMS, `pi update` self-only, package rename to `@earendil-works`, Node 22.19 minimum, pi.dev catalog and update endpoint, telemetry ping (0.67.1), project trust (0.79.0), `thinkingLevelMap`, image generation |
| 5. Provider runtime consolidation | 0.80.0 to 0.82.x | Jun to Jul 2026 | pi-ai old global API moved to `/compat`, `ModelRuntime` replaces `AuthStorage`+`ModelRegistry` in SDK, provider-owned `/login`, dynamic catalogs, extension-registered full providers, llama.cpp router, `max` thinking level, constrained sampling |
| 6. Fullscreen TUI and harness v4 | 0.83.0 to 0.84.4 | Jul to Aug 2026 | Fullscreen TUI mode, Mermaid/LaTeX, PowerShell tool, `defaultTools`, harness v4 lane-based Session/SessionRepo, experimental remote-session client, message_update deltas only, staged atomic self-update |
| 7. Transcript-canonical context and cache economics | 0.85.x to 0.87.x | Sep 2026 | `SessionManager` canonical for provider context, `ContextEditEntry`, actionable `turn_end`/`agent_before_settle`, cache warming, `/bug`, mid-conversation system prompt/tool changes stored in transcript |
| 8. Codemode, MCP, virtual models | 0.99.0 to Unreleased | 2026-09-29 to 09-30 | MCP as built-in extension, QuickJS `codemode` tool, `tool_search`, tool `exposure` levels, `ctx.executeTool()`, virtual models, classifier models, system theme |

Note: version jump 0.87.1 (2026-09-22) to 0.99.0 (2026-09-29). Changelog gives no reason. Treat as a marketing or milestone bump, not a semantic one.

## 2. Added features

Area codes: harness, tools, session, compaction, extension, TUI, provider, config, distribution, RPC/SDK, server/client, durable.
Inherited provider/model additions (new model IDs, new OpenAI-compatible gateways) are grouped at the end, since they are catalog data, not features.

### 2.1 Harness, tools, context

| Version | Feature | Area |
|---|---|---|
| 0.58.0 | Extension tool interception moved to agent-core `beforeToolCall`/`afterToolCall`; tools run in parallel by default, extension `tool_call` preflight stays sequential | harness |
| 0.62.0 | Built-in read/write/edit/bash/grep/find/ls exposed as `ToolDefinition` with overridable `renderCall`/`renderResult`; tool prompt snippets come from tool metadata | tools |
| 0.63.0 | `edit` multi-edit: several disjoint regions in one call matched against original content | tools |
| 0.63.2 | `edit` input is `edits[]` only (removes mixed single/multi shape that caused invalid calls) | tools |
| 0.64.0 | `ToolDefinition.prepareArguments` normalizes raw model args before schema validation; used to migrate old-session `oldText/newText` | tools |
| 0.63.2 | `ctx.signal` for extensions: forwards turn cancellation into nested model calls and fetch | extension |
| 0.69.0 | `terminate: true` tool result ends the batch without an automatic follow-up model call | tools |
| 0.84.1 | `tool_call` handler `terminate` on blocked calls skips follow-up call | extension |
| 0.65.0 | `defineTool()` helper with typed params | extension |
| 0.67.1 | `PI_CODING_AGENT=true` env marker; 0.84.0 adds `AI_AGENT=pi` | harness |
| 0.67.3 | `renderShell: "self"` lets a tool own its outer render shell (stable large diffs) | TUI |
| 0.67.4 | `--no-context-files`/`-nc`; `loadProjectContextFiles()` exported | config |
| 0.67.2 | Multiple `--append-system-prompt` flags; inline extension factories passed to `main()` | config, extension |
| 0.68.0 | `before_agent_start` exposes `systemPromptOptions` (structured prompt inputs) | extension |
| 0.77.0 | `--exclude-tools`/`-xt` disables named tools | config |
| 0.80.7 | Cache-friendly dynamic tool loading: tools activated by tool results get definitions where they appear, keeping the cached prefix (Anthropic, OpenAI Responses); 0.80.9 adds Kimi native deferred loading; 0.86.0 adds Fireworks | tools |
| 0.82.0 | Constrained tool sampling (`constrainedSampling`: strict JSON schema prefer/require, OpenAI Lark/regex grammar), gated by model capability flags | tools, provider |
| 0.82.0 | `PI_SESSION_ID/FILE/PROVIDER/MODEL/REASONING_LEVEL` exposed to bash tool commands | tools |
| 0.82.0 | Streaming `bash_execution_update` events for RPC bash | RPC/SDK |
| 0.84.0 | `AGENTS.override.md` per-directory replaces AGENTS.md/CLAUDE.md in that directory | config |
| 0.84.2 | `defaultTools` setting (global or project) chooses startup built-in tools | config |
| 0.84.2 | Experimental strict JSON-schema sampling for default read/bash/edit/write under `PI_EXPERIMENTAL=1`; 0.86.0 makes it default-on ("strict-prefer"), opt-out per tool via `constrainedSampling: false` | tools |
| 0.84.3 | Optional `powershell` tool for Windows | tools |
| 0.99.0 | `codemode` built-in tool: model-written JS in a QuickJS sandbox calling pi tools in parallel; `tool_search` declares non-declared tools; `codemode.mode`, `codemode.inlineBudget` settings | tools, extension |
| 0.99.0 | Tool exposure levels `direct`/`model-only`/`codemode`/`deferred`/`hidden`, `namespace`, `annotations`, `outputSchema`+`structuredContent`, `isError`, `prepareLoadout()`, `ctx.executeTool()` nested calls (events carry `parentToolCallId`, bounded `nestedCalls` on result) | tools, extension |
| 0.99.0 | `defaultTools` accepts `+name`/`-name` deltas | config |
| 0.99.0 | bash/powershell structured results (for scripts) keep up to 1 MiB with `truncated` and `full_output_path` | tools |
| Unreleased | MCP server `description`, `oauth.clientName`; `describeNamespace(name)` codemode helper | tools |

### 2.2 MCP and extensibility

| Version | Feature | Area |
|---|---|---|
| 0.99.0 | MCP client as built-in extension: stdio and streamable HTTP, OAuth, `mcp.json` (global, or project after trust), `pi.registerMcpServer()`, `/mcp`, `pi mcp add|remove|list|login|logout` | extension |
| 0.99.0 | Built-in extensions (`mcp`, `llama.cpp`, `codemode`, `tool-search`) toggled in `pi config` as `-builtin:<name>` | config |
| 0.99.0 | Warning when an extension replaces a built-in extension's tool/command/flag | extension |

### 2.3 Extension events and hooks (API surface)

| Version | Feature | Area |
|---|---|---|
| 0.57.0 | `before_provider_request` (inspect/replace payload); non-capturing overlays with focus control | extension |
| 0.57.1 | `session_directory` event (later removed in 0.65.0) | extension |
| 0.65.0 | `session_start` with `reason` (startup/reload/new/resume/fork) replaces `session_switch`/`session_fork` | extension |
| 0.67.4/0.67.6 | `after_provider_response` (status, headers) | extension |
| 0.68.0 | `session_shutdown` gets `reason` and `targetSessionFile`; `ctx.fork(..., {position: before|at})` | extension |
| 0.69.0 | `ctx.ui.addAutocompleteProvider()` stacked providers | extension |
| 0.70.3 | `ctx.ui.setWorkingVisible()`; 0.68.0 `ctx.ui.setWorkingIndicator()` | extension |
| 0.71.0 | `message_end` may replace finalized message (e.g. override cost); `ctx.ui.getEditorComponent()`; `thinking_level_select` event; `registerProvider` `name` | extension |
| 0.73.1 | Multi-choice interactive OAuth login | provider |
| 0.77.0 | `InputEvent.streamingBehavior` (idle prompt vs steer vs follow-up) | extension |
| 0.78.1 | `ctx.mode` (TUI/RPC/JSON/print); `ctx.getSystemPromptOptions()` | extension |
| 0.79.0 | `project_trust` event; `ctx.isProjectTrusted()` (0.79.1) | extension |
| 0.79.10 | `reason`, `willRetry` on `session_before_compact`/`session_compact` | extension, compaction |
| 0.80.3 | `session_info_changed` | extension |
| 0.80.4 | `agent_settled` (also RPC), `before_provider_headers`, entry renderers for display-only persisted entries, `InlineExtension` | extension |
| 0.80.8 | Extension provider `refreshModels(context)` | extension, provider |
| 0.81.0 | Extensions register complete pi-ai providers (auth, refresh, filter, stream) | extension, provider |
| 0.83.0 | `ctx.scopedModels` | extension |
| 0.84.0 | `pi.registerMarkdownTransformer()` (display-only) | extension |
| 0.84.3 | `session_compact_failed` | extension, compaction |
| 0.84.4 | `ui_prompt_start`/`ui_prompt_end` (distinguish agent work from waiting on user) | extension |
| 0.86.0 | `pi.on()` returns unsubscribe; `ctx.modelRegistry.stream()/streamSimple()`; `cache_warming_decision` event; hook types exported | extension |
| 0.87.0 | Actionable `turn_end` and `agent_before_settle` boundaries (return `{entries, continue:true}`); `context_with_system` event | extension |
| 0.99.0 | `provider_stream_event` (raw parsed provider events); `registerVirtualModel()` | extension |

### 2.4 Session, compaction

| Version | Feature | Area |
|---|---|---|
| 0.60.0 | `--fork <path|id>` | session |
| 0.61.0 | `/export <path.jsonl>`, `/import <path.jsonl>` | session |
| 0.63.0 | `sessionDir` setting; 0.71.0 `PI_CODING_AGENT_SESSION_DIR` | config |
| 0.65.0 | `createAgentSessionRuntime()`/`AgentSessionRuntime` for new/switch/fork/import; `/tree` label timestamps | session, RPC/SDK |
| 0.68.0 | `/clone` (duplicate active branch), keeps `/fork` for prior user message | session |
| 0.76.0 | `--session-id` exact ID; RPC bash `excludeFromContext` | session, RPC/SDK |
| 0.78.0 | `--name`/`-n` session name at startup | session |
| 0.79.9 | Estimated post-compaction token counts in results/events | compaction |
| 0.80.4 | `showCacheMissNotices`; JSONL header custom metadata | session |
| 0.81.0 | Usage accounting persisted for tools, compaction, branch summaries; 0.81.1 compaction/summary follow retry policy with lifecycle events | compaction |
| 0.84.0 | Harness v4 lane-based `Session`/`SessionStorage`/`SessionRepo` with durable operation records, global facts, shared sequence numbers, tree-scoped lane views; `JsonlSessionRepo` append-only | session, durable |
| 0.85.0 | `SessionManager.inMemory()` restores externally stored entries | session, RPC/SDK |
| 0.86.0 | Transcript-backed mid-conversation system prompt and tool changes (survive resume and branch nav, preserve cache prefix) | session |
| 0.86.0 | Per-model compaction `reserveTokens`/`keepRecentTokens` via `compaction.modelOverrides` | compaction |
| 0.86.0 | Prompt-cache warming (long tool runs, optional idle), cost-aware | harness |
| 0.86.0 | `/bug` redacted report, crash log `~/.pi/agent/crashes.json`, `pi.bug-report` session entry | harness |
| 0.87.0 | Append-only `ContextEditEntry` (omit/replace message in provider context without touching raw history); retain-none compaction | session, compaction |
| 0.87.0 | Per-model image resize limits (`inputLimits.images.resize`) | provider, config |
| 0.99.0 | Session file created at first user message (fix for lost sessions) | session |

### 2.5 Provider/auth runtime (design, not model IDs)

| Version | Feature | Area |
|---|---|---|
| 0.63.0 | Dynamic `models.json` auth and headers resolved per request (`getApiKeyAndHeaders`) | provider |
| 0.70.1 | `retry.provider.{timeoutMs,maxRetries,maxRetryDelayMs}`; 0.76.0 provider retries controlled by pi, not hidden SDK defaults | provider |
| 0.72.0 | Model-level `thinkingLevelMap` (replaces `compat.reasoningEffortMap`) | provider |
| 0.73.1 | `models.json` allows comments and trailing commas | config |
| 0.74.1 | Image generation APIs (OpenRouter) | provider |
| 0.79.5 | `auth.json` API-key `env` overrides; global `httpProxy` | config |
| 0.79.7 | Automatic light/dark theme mode | TUI |
| 0.80.4 | `/login <provider>` autocomplete | provider |
| 0.80.6 | Opt-in `max` thinking level; input-token pricing tiers | provider |
| 0.80.8 | `ModelRuntime` async facade; provider-owned `/login`; file-backed dynamic catalogs (`models-store.json`); per-provider pi.dev catalog overlays; `pi update --models` | provider |
| 0.81.0 | llama.cpp router with `/llama` HF search/download/load/unload | provider |
| 0.82.0 | OpenRouter and Kimi Code OAuth in `/login` | provider |
| 0.82.1 | pi.dev catalog revalidation with `If-None-Match` (304) | provider |
| 0.83.0 | `pi auth print-api-key`, `pi auth print-bearer-token` for external clients | RPC/SDK |
| 0.84.0 | Arbitrary `samplingParams`; vLLM `thinking_token_budget`; deferred provider request handles; vendor-neutral telemetry schema | provider |
| 0.84.1 | `pi auth check` preflight | provider |
| 0.99.0 | ModelRuntime image generation and classifier models (`classify()`), Jev classifier, ChatGPT sign-in for OpenAI provider, `deviceId` in global settings | provider |
| 0.99.0 | Experimental virtual models (extension routes each request to a physical model, footer shows routed model, `/session` cost per physical model) | provider, extension |

### 2.6 RPC/SDK, server/client, distribution, TUI

| Version | Feature | Area |
|---|---|---|
| 0.57.0 | RPC strict LF-only JSONL framing | RPC/SDK |
| 0.60.0 | `createLocalBashOperations()` export | RPC/SDK |
| 0.80.3 | RPC `get_entries`, `get_tree`; `./rpc-entry` export | RPC/SDK |
| 0.81.0 | RPC `get_available_thinking_levels` | RPC/SDK |
| 0.84.0 | Experimental remote-session client: transport-neutral `PiClient`, CBOR protocol, Unix-socket transport, `RemoteSession` controller with transcript reducers | server/client |
| 0.84.0 | JSON/RPC `message_update` emits only delta (drops cumulative `message`/`partial`, quadratic growth) | RPC/SDK |
| 0.84.4 | RPC `clear_queue` | RPC/SDK |
| 0.99.0 | RPC `prompt/steer/follow_up` return per-input disposition | RPC/SDK |
| 0.60.0 | Startup no longer auto-updates unpinned packages | distribution |
| 0.73.1 | npm scope rename support in `pi update --self` | distribution |
| 0.75.4 | `npm-shrinkwrap.json`, lifecycle scripts disabled on self-update, isolated install smoke tests | distribution |
| 0.74.1 | Windows ARM64 standalone binaries | distribution |
| 0.79.4 | `SHA256SUMS` for standalone binaries | distribution |
| 0.79.7 | `pi update` updates pi only; `--all` for packages too | distribution |
| 0.81.1 | Deterministic checksummed source archives per release | distribution |
| 0.84.3 | Installer-managed update: stage, verify, atomically activate | distribution |
| 0.79.0 | Project trust gate for project-local settings, resources, instructions, packages; `defaultProjectTrust` (0.79.1); `--approve`/`--no-approve` | config |
| 0.79.4 | First-run theme detection | TUI |
| 0.84.0 | Fullscreen TUI (sticky editor/footer, scrollbar, Mermaid, LaTeX); 0.84.2 transcript search; 0.84.4 selection-copy controls | TUI |
| 0.99.0 | `system` theme (terminal palette, default), `oklch/okhsl` colors | TUI |

Model/catalog additions (inherited, low design value): Claude Opus 4.7/4.8/5/5.5, Sonnet 5/5.5, Fable 5/5.1; GPT-5.5/5.6/6 Astra/Sol/Luna/6.1 Sol; Kimi K3; Grok 4.5/4.6/4.7; DeepSeek V4; MiniMax-M3; GLM-5.2; providers Cloudflare AI Gateway, Cloudflare Workers AI, Moonshot, Fireworks, Together, DeepSeek, Xiaomi MiMo, Qwen Token Plan, Baseten, Ant Ling, NVIDIA NIM, Meta Muse, Radius, Mistral prompt caching.

## 3. Removed, deprecated, reversed (do not rebuild)

| Version | What was dropped or reversed | Reason (as stated) |
|---|---|---|
| 0.57.0 | Lenient readline-based RPC framing | Unicode separators U+2028/2029 inside JSON corrupted streams (#1911). Use strict `\n` JSONL |
| 0.58.0 | Wrapper-based tool interception | Replaced by agent-core `beforeToolCall/afterToolCall`; enables parallel tool execution |
| 0.59.0 | Fallback of custom tool prompt text to `description` | Tools only appear in "Available tools" if they give `promptSnippet` (#2285): prompt bloat |
| 0.60.0 | Auto-update of unpinned packages at startup | Made explicit via `pi update`; background check only notifies. Reason implied: surprise mutation, supply chain |
| 0.62.0 | `location`/`path` on slash commands, `source` on Skill/PromptTemplate, `ResourceLoader.getPathMetadata()`, `extensionPath` | Replaced by uniform `sourceInfo` (#1734) |
| 0.63.0 | `ModelRegistry.getApiKey(model)` | Auth and headers can resolve dynamically per request; use `getApiKeyAndHeaders` |
| 0.63.0 | Direct MiniMax deprecated model IDs | Catalog cleanup |
| 0.63.2 | Single-edit shape in `edit` (`oldText/newText`) | Mixed shapes caused invalid calls (#2639). Legacy sessions kept working by `prepareArguments` shim (0.64.0) |
| 0.64.0 | Public `ModelRegistry` constructor | Use `create()`/`inMemory()` |
| 0.65.0 | Extension events `session_switch`, `session_fork`; `session_directory` (added 0.57.1, gone 8 releases later); session-replacement methods on `AgentSession`; unknown single-dash CLI flags silently ignored | Replaced by `session_start.reason` and `AgentSessionRuntime`; closure-based runtime rebuilds cwd-bound services on every switch. Also invalidates stale captured `pi`/`ctx` (0.69.0) |
| 0.68.0 | Prebuilt cwd-bound tool exports (`readTool`, `codingTools`, ...); ambient `process.cwd()`/agent-dir fallbacks; `Tool[]` in `createAgentSession` | Tool selection by name allowlist; explicit cwd everywhere (#3452). `--no-tools` now disables all tools |
| 0.69.0 | `@sinclair/typebox` 0.34 | Move to `typebox` 1.x; TypeBox-native validation works in eval-restricted runtimes |
| 0.70.0 | OSC 9;4 terminal progress on by default (added 0.69.0, default-off in 0.70.0) | Reversed within one release; now opt-in `terminal.showTerminalProgress` |
| 0.71.0 | Google Gemini CLI and Google Antigravity providers and example | "Removed"; no reason given here (Qwen CLI OAuth example also removed as discontinued) |
| 0.72.0 | `compat.reasoningEffortMap` | Replaced by model-level `thinkingLevelMap` |
| 0.73.0 | Xiaomi Token Plan AMS as `xiaomi` provider | Repointed to API billing; token plans split into regional providers |
| 0.74.0 / 0.75.0 | Old package scope `@mariozechner`; Node below 22.19 | Org move; strip-only TypeScript compatibility (0.75.4 avoids TS emit-requiring syntax) |
| 0.75.4 | Web UI workspace references from the CLI package | Web UI dropped from CLI package |
| 0.75.1 | Non-working Codex fast model variants | Did not work |
| 0.80.0 | pi-ai global `stream/complete/getModel/registerApiProvider` on root | Moved to `/compat`; compat and loader alias "will be removed in a future release". Selective-provider `/base` entrypoints (added 0.79.8) removed in the same release, 2 releases later |
| 0.80.2 | `ApiKeyCredential` type discriminator `"api-key"` | Now `"api_key"` for auth.json compatibility |
| 0.80.4 | Vercel AI Gateway default attribution headers (added 0.79.5) | Reversed within 3 releases (no reason) |
| 0.80.7 | `compat.sendSessionIdHeader` | Replaced by `compat.sessionAffinityFormat` |
| 0.80.8 | SDK `authStorage` and `modelRegistry` options; `AuthStorage` exports; sync `ModelRegistry.refresh()`; `ModelRuntime.getAll/find/getSnapshot/getAuthOptions` | Consolidated into async `ModelRuntime` |
| 0.83.0 | TypeBox deprecated APIs (`Type.Base`, `Type.Promise`, ...) | Upgrade to 1.3.7 |
| 0.84.0 | Legacy JSONL and in-memory repo APIs of agent harness; experimental subpaths; cumulative `message`/`partial` in `message_update`; remote-session list summaries showing runtime phase/model/lock | Replaced by v4 lane-based Session; quadratic output growth (#7290) |
| 0.85.1 | Experimental `client`, `experimental/plugin` subpaths and server/client commands from the published package | Accidentally published in 0.85.0, broke SDK imports (#9132). Now source-only via `pi-test.sh`; 0.85.1 fix entry also "restored the client compatibility entry point" so the direction wobbled |
| 0.86.0 | pi-ai stream input `Context` | Now normalized `TranscriptContext`; system prompt and tools read from messages |
| 0.86.0 | `user_bash` silently continuing on handler error | Now fails closed (#9068) |
| 0.87.0 | `shouldStopAfterTurn` (added 0.72.0, gone after ~15 releases) | Replaced by `finishTurn` returning `{action:"end"}`, also sees error/abort responses |
| 0.87.0 | Assigning `session.agent.state.messages` as source of request history | `SessionManager` is canonical; use `appendContextEdit`, `refreshContext()`, `navigateTree()` |
| 0.87.0 | Handlers seeing system messages in `context` event | Handlers filtering messages dropped prompt and tool declarations (#9789); Pi restores them; `context_with_system` added for full transcript |
| 0.99.0 | `[Themes]` block in startup banner; `<inline:name>` naming | Renamed to `builtin:<name>` |
| 0.99.0 | Legacy "OpenAI Codex" provider | Renamed "(legacy)", superseded by ChatGPT sign-in on OpenAI provider |
| Unreleased | Tool lists and MCP server instructions inside `codemode` description; `codemode-deferred` mode | Description changed whenever a server's tool list changed (breaks cache). Now alias of `codemode`; scripts call `searchTools()` and `describeNamespace()` |

Pattern of reversals inside this window: OSC progress (0.69 to 0.70), Vercel attribution (0.79.5 to 0.80.4), `/base` entrypoints (0.79.8 to 0.80.0), telemetry ping added with opt-out (0.67.1), experimental client published then pulled (0.85.0 to 0.85.1), session_directory event (0.57.1 to 0.65.0), shouldStopAfterTurn (0.72.0 to 0.87.0), codemode description (0.99.0 to Unreleased).

Things Pi conspicuously never added in this window (absence of evidence in these lines only; other lane may contradict): built-in sub-agents, built-in plan mode (`plan-mode` is an example extension, see fix in 0.67.3 entry), built-in permission prompts (extensions do it via `tool_call`), built-in web UI (removed from CLI package in 0.75.4). MCP arrived only in 0.99.0 and as an extension.

## 4. Breaking changes, especially extension/hook/SDK

| Version | Break | Migration |
|---|---|---|
| 0.57.0 | RPC framing LF-only | Split on `\n` only, not generic line readers |
| 0.59.0 | Tools omitted from prompt unless `promptSnippet` | Add `promptSnippet` |
| 0.60.0 | No startup auto-update of packages | Run `pi update` |
| 0.61.0 | Keybinding ids namespaced (`app.tools.expand`, `tui.select.confirm`) | Auto-migrates `keybindings.json`; extensions must update `keyHint()`/`matches()` names |
| 0.62.0 | `renderCall/renderResult` must return `Component` if defined; `sourceInfo` replaces path/location/source/extensionPath | Field mapping table in changelog |
| 0.63.0 | `getApiKey` to `getApiKeyAndHeaders` (returns `{ok, apiKey, headers}` or error) | Code sample in changelog |
| 0.64.0 | `ModelRegistry` constructor private | `create`/`inMemory` |
| 0.65.0 | `session_switch`/`session_fork` removed; `AgentSession.newSession/switchSession/fork/importFromJsonl` removed; `session_directory` removed | `session_start.reason`; `createAgentSessionRuntime(factory)` |
| 0.68.0 | `createAgentSession({tools})` takes names; prebuilt tool exports removed; explicit cwd/agentDir required | `createReadTool(cwd)` factories |
| 0.69.0 | typebox 1.x; captured pre-replacement `pi`/`ctx` throw after `newSession/fork/switchSession` | Use `withSession` for post-replacement work |
| 0.70.0 | OSC 9;4 progress default off | Setting |
| 0.71.0 | Gemini CLI and Antigravity providers gone | Switch provider |
| 0.72.0 | `reasoningEffortMap` to `thinkingLevelMap` | Move mapping |
| 0.75.0 | Node 22.19 minimum | Upgrade |
| 0.80.0 | pi-ai old global API to `/compat` | Loader aliases keep extensions running; typecheck imports change; compat will be removed later |
| 0.80.7 | `sendSessionIdHeader` to `sessionAffinityFormat` | Replace flag |
| 0.80.8 | SDK `modelRuntime` replaces `authStorage`+`modelRegistry`; `ModelRegistry.refresh()` async | Await; use `ModelRuntime.getAuth()` |
| 0.83.0 | TypeBox deprecated APIs removed | Migrate |
| 0.84.0 | `message_update` deltas only; `ProviderHeaders` values can be `null` (deletion markers); refresh returns result; provider refresh context uses `context.stored` + generation-checked `context.publish()`; harness v4 Session API; legacy repos removed; OAuth `refreshToken` must honor abort signal | Before/after code in changelog |
| 0.86.0 | Provider stream input `TranscriptContext`; `ToolCall.arguments` and `ToolResultMessage.details` JSON-only; `user_bash` fails closed | Use `getCurrentSystemPrompt()`/`getCurrentTools()` |
| 0.87.0 | `shouldStopAfterTurn` removed; `ContextEditEntry` added to `SessionEntry` union; `SessionManager` canonical; `TurnEndEvent` expanded, `ExtensionRunner.emit()` rejects `turn_end`, use `emitBoundary`; `agent_settled` reruns deferred | Documented |

Rate: the extension/SDK contract broke roughly every 2 to 4 minor releases. Deprecation windows were short, sometimes zero (see reversals above).

## 5. Current direction (Unreleased and last ~10 releases: 0.84.4 through Unreleased)

1. Transcript as single source of truth. 0.86.0 and 0.87.0 move system-prompt changes, tool changes, context edits, compaction retention, and bug reports into append-only session entries. Raw history is never rewritten; the provider view is derived. This is the strongest architectural signal.
2. Cache economics are first-class. Cache-hit rate in footer (0.79.0), cache-miss notices (0.80.4), cache warming (0.86.0), cache-safe image resizing (0.87.0), cache-safe tool loading (0.80.7), stable `codemode` description (Unreleased). Expect Go design to treat prompt-prefix stability as an invariant.
3. Fewer tokens through tool indirection. `codemode` (JS sandbox over tools), `tool_search`, tool `exposure` levels, `ctx.executeTool()` nested calls, and MCP as a lazy namespace (0.99.0). The tool list the model sees is now a runtime decision, not a static list.
4. Model layer decoupled from the agent. `ModelRuntime` (0.80.8), virtual models, classifier models, image models, extension-registered full providers (0.81.0), remote catalogs from pi.dev. A request can be routed to a different physical model per turn (0.99.0).
5. Durable/remote session groundwork. Harness v4 lanes with durable operation records (0.84.0), `PiClient` with CBOR over Unix socket, remote-session controller. It is still experimental and was pulled from the published package in 0.85.1. Repo now also has `server`, `client`, `durable`, `session-backends` packages (directory listing of `/Users/dale/Desktop/workspace/opensources/pi/packages`), but this changelog does not describe them beyond 0.84.0 and 0.85.1. Other lanes should cover.
6. Trust and supply chain. Project trust gate (0.79.0), no auto-update, staged atomic self-update, checksums, shrinkwrap, source archives.
7. Extension boundaries become "actionable": hooks that can persist entries and force one more provider request (`turn_end`, `agent_before_settle`), fail-closed `user_bash`, `terminate`.
8. Continuing TUI investment (fullscreen, system theme), but it is orthogonal to the core.

## 6. Lessons for a Go rewrite

1. Do not port the whole hook list. Pi grew ~40 events across this window; several were replaced within 1 to 15 releases (`session_switch`, `session_fork`, `session_directory`, `shouldStopAfterTurn`). Start with a small set: session start (with reason), before/after provider call, tool_call (block/terminate), turn boundary that can return entries, agent settled. Add others on demand.
2. Session state ownership was the recurring pain. Pi twice moved ownership (runtime object in 0.65.0, SessionManager canonical in 0.87.0) after extensions mutated agent state directly. In Go: make an append-only entry log the only writer of context; derive provider context from it; expose edits as entries (context edit, compaction with kept boundary).
3. Never let handlers see or drop system prompt and tool declarations. This caused real bugs (0.87.0, #9789/#9822). Keep them out of the message list handed to hooks.
4. Tool selection by name allowlist with per-session factories, not global cwd-bound singletons (0.68.0). Carry cwd and agent dir explicitly. Give every tool/command/resource a `sourceInfo`-style provenance record from day one (0.62.0).
5. Tool args need a pre-validation normalization hook (0.64.0) because saved sessions outlive tool schemas. Keep one edit shape (0.63.2).
6. Tool result model: structured content, `isError`, `terminate`, bounded nested calls with parent id (0.69.0, 0.99.0). Full output for scripts vs truncated for the model (0.99.0: 1 MiB vs 2000 lines/50KB).
7. Prompt-prefix stability is a design constraint: never put volatile data (tool counts, server instructions) into a static description (Unreleased). Load tools where they appear (0.80.7).
8. Provider layer: per-request auth and headers (0.63.0), retry owned by the harness not SDKs (0.76.0), capability metadata driving behavior (`thinkingLevelMap`, constrained sampling flags), overflow classification per provider. Catalog data should be remote-refreshable with ETag (0.82.1) and cached locally.
9. Streaming protocol: send deltas only, never cumulative partials (0.84.0, quadratic). Use strict framing for line protocols (0.57.0). Consider a structured protocol (Pi's CBOR client) only after the JSONL RPC is stable.
10. Project-local config is untrusted input (0.79.0): gate settings, resources, instructions, packages, and MCP config behind a trust decision.
11. No auto-updating of user-installed extensions at startup (0.60.0); updates are explicit and staged/atomic (0.84.3).
12. A large share of releases are model-catalog churn. Generate the catalog from data (models.dev, pi.dev), do not hand-code models.
13. Do not publish experimental subpackages accidentally (0.85.0 to 0.85.1).
14. Features that arrived as extensions rather than core: MCP (built-in extension, 0.99.0), plan mode (example), permission gates (example), containerization (Gondolin example, 0.78.1). Matches a "small core, everything else extension" stance.

## Unresolved questions

1. Why were Google Gemini CLI and Antigravity removed (0.71.0)? Changelog gives no reason; likely provider ToS or discontinuation.
2. Is harness v4 (lane-based Session with durable operation records) the storage model actually used by the coding-agent today, or only by `packages/agent`? 0.87.0 still speaks of `SessionManager` with JSONL entries, which suggests two session systems coexist. Needs source confirmation in `packages/agent`, `packages/durable`, `packages/session-backends`.
3. Does `packages/server` and `packages/client` ship in a release yet? Changelog says source-only since 0.85.1.
4. Why the 0.87.1 to 0.99.0 version jump?
5. I did not read Fixed entries in depth, per the lane split; a few (context-handler fix 0.87.0, thinking-signature recovery) reveal design constraints that another lane should confirm.
6. Lines 1-3016 only; features whose introduction predates 0.56.3 (e.g., original compaction, skills, packages) are not dated here.

Status: DONE
Summary: Timeline, feature tables, removal/reversal list, breaking-change table, direction, and lessons for CHANGELOG lines 1-3016 are in this report.
Concerns: Fixed sections were only keyword-scanned; no source-code confirmation was done, so claims are changelog-only.
