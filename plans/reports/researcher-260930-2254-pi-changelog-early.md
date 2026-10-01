# Pi coding-agent evolution: 0.10.0 (2025-11-25) to 0.56.2 (2026-03-05)

Source: /Users/dale/Desktop/workspace/opensources/pi/packages/coding-agent/CHANGELOG.md lines 3017-5979 (commit 2bbfcca4). Every row cites a version from that file. I did not confirm items in source code; all claims are changelog claims. Abbreviations: PR/issue numbers omitted.

## 0. Outcome (read this first)

In 15 weeks Pi went from a plain TUI agent with 7 tools to a small kernel plus an extension platform. The biggest lessons for the Go rewrite:

1. The extension model was rebuilt three times in 4 weeks (hooks 0.18 -> custom tools 0.23 -> hook/tool API restructure 0.31 -> unified "extensions" 0.35). Design ONE extension concept up front (one API object, one context, one discovery dir, one CLI flag, one settings key).
2. The session file was rebuilt as a tree (0.31, v1 -> v2, then v3 in 0.35). Start with `id`/`parentId` entries and typed custom entries. Do not start linear.
3. Message delivery during streaming had to be split into `steer` (interrupt after current tool) and `followUp` (wait until idle) at 0.32. Start with two queues.
4. Compaction was simplified early (0.17) by REMOVING proactive mid-turn compaction. Only two triggers survive: overflow error (compact then auto-retry) and post-turn threshold.
5. The largest recurring churn was provider quirks (thinking signatures, tool-call IDs, replay of reasoning items, OAuth flows) and TUI input/render edge cases (Kitty protocol, IME, wide chars, resize). Both are long-tail work; budget for them or defer.
6. Settings/auth persistence moved from "write whole file" to locked read-merge-write with error buffering (0.50.2, 0.53.0). Do this from day one.

## 1. Timeline in eras

| Era | Versions | Dates | Theme |
|---|---|---|---|
| 1. Bootstrap | 0.10.0 - 0.10.6 | 2025-11-25 to 11-28 | Initial TUI, 7 tools, 3 providers, HTML export, sessions; custom models.json; RPC mode v1; multimodal attachments |
| 2. Daily-driver features | 0.11.0 - 0.13.2 | 11-28 to 12-07 | Slash commands (file based), `/branch`, model/session search, print mode `-p`, compaction, bash mode `!`, truncation policy, Windows shell config |
| 3. Refactor and ops hardening | 0.14.0 - 0.17.0 | 12-08 to 12-09 | `AgentSession` extracted, RPC protocol redesigned, compaction simplified, xhigh thinking, themes require new tokens |
| 4. Extensibility v1 (hooks, custom tools, skills) | 0.18.0 - 0.24.5 | 12-10 to 12-20 | Hooks (0.18), skills (0.19), custom tools (0.23), auto-retry (0.18.2), inline images (0.21), Copilot OAuth (0.22), subagent example, Kitty keyboard (0.24) |
| 5. Embedding and policy | 0.25.0 - 0.30.2 | 12-20 to 12-26 | Interruptible tool execution, Gemini CLI and Antigravity OAuth, SDK `createAgentSession` (0.26), project settings, session lifecycle hooks, `auth.json`/`ModelRegistry` (0.28), `/settings`, SYSTEM.md |
| 6. Session tree and API restructure | 0.31.0 - 0.34.2 | 2026-01-02 to 01-04 | `/tree`, tree-shaped session file, structured compaction, hook/tool API rewrite, steer/followUp split (0.32), keybindings.json (0.33), plan-mode example |
| 7. Unified extensions | 0.35.0 - 0.45.x | 01-05 to 01-13 | hooks + tools merged into extensions (0.35), Codex OAuth, custom footer/header/editor/overlays, pluggable tool operations (0.39), `input` event (0.47), fuzzy edit fallback (0.46) |
| 8. Packages and hot reload | 0.46 - 0.50.0 | 01-15 to 01-26 | Pi packages (npm/git), `pi install`, `/reload`, `registerProvider`, SDK `ResourceLoader`, `.agents/skills` |
| 9. Polish, provider breadth, embedding safety | 0.50.1 - 0.56.2 | 01-26 to 03-05 | Many providers, per-model overrides, settings/auth locking, bash spawn hook, dynamic tool registration, `terminal_input`, `ctx.reload()`, project-first resource precedence |

## 2. Added features by era

Area key: harness, tools, session, compaction, extension, TUI, provider, config, distribution, RPC/SDK.

### Era 1 (0.10.x)

| Version | Feature | Area |
|---|---|---|
| 0.10.0 | Interactive TUI with streaming | TUI |
| 0.10.0 | Sessions with `--continue`, `--resume`, `--session` | session |
| 0.10.0 | Tools: `read`, `write`, `edit`, `bash`, `glob`, `grep`, `think` (initial list; later builds use read/bash/edit/write/grep/find/ls, see 0.34.0) | tools |
| 0.10.0 | Thinking mode for Claude, `/thinking` selector | harness |
| 0.10.0 | `@` file autocomplete, slash command autocomplete | TUI |
| 0.10.0 | `/export` HTML, `/model`, `/session` stats | TUI/session |
| 0.10.0 | Providers: Anthropic, OpenAI, Google | provider |
| 0.10.0 | Message queueing while streaming | harness |
| 0.10.0 | OAuth for Gmail and Google Calendar access (no later mention in this range) | provider |
| 0.10.1 | Custom models via `models.json` | config |
| 0.10.2 | Thinking level persistence (settings + per-session) | config/session |
| 0.10.2 | Model cycling shortcut, scoped models via `-m` | TUI/config |
| 0.10.2 | Auto retry with exponential backoff for transient API errors | harness |
| 0.10.2 | `--system-prompt`, cumulative token and USD cost in footer | harness/TUI |
| 0.10.3 | RPC mode (`--rpc`), JSON on stdin/stdout; internals split to support multiple frontends | RPC/SDK |
| 0.10.5 | Multimodal: images and PDFs via `@path` or `--file` | tools/harness |

### Era 2 (0.11 - 0.13)

| Version | Feature | Area |
|---|---|---|
| 0.11.0 | File-based slash commands (`~/.pi/slash-commands/*.txt`, `{{selection}}`) | config |
| 0.11.0 | `/branch` (new session from earlier user message) | session |
| 0.11.0 | Unified attachments, drag and drop files, searchable model/session selectors | TUI |
| 0.11.1 | `fd` integration for autocomplete | tools |
| 0.12.0 | Print mode `-p`, `-P` streaming, `--print-turn`, `--no-markdown`, `--thinking-*` flags | harness |
| 0.12.1 | gpt-4.1 family, o3, o4-mini | provider |
| 0.12.4 | RPC worker exits when parent dies (orphan safeguard) | RPC/SDK |
| 0.12.5 | Rebranding via `piConfig` in package.json (name, configDir) | distribution |
| 0.12.7 | Context compaction: `/compact`, `/autocompact`, keep ~20k recent tokens, trigger at `contextWindow - reserveTokens` (16k) | compaction |
| 0.12.7 | Branch source tracking (`branchedFrom` in header) | session |
| 0.12.9 | `/copy` last agent message | TUI |
| 0.12.11 | `--append-system-prompt`, thinking block toggle Ctrl+T, `authHeader` in models.json | harness/config |
| 0.12.14 | Double-Escape opens branch selector | TUI |
| 0.12.12 | Prompt history (up/down), `/resume` mid-conversation, fuzzy search models and sessions | TUI/session |
| 0.13.1 | Windows shell resolution: `shellPath` setting, Git Bash, PATH bash | config |
| 0.13.2 | Uniform truncation: 2000 lines OR 50KB, never partial lines; actionable notices to model; bash tail truncation with temp file; grep lines cut at 500 chars | tools |

### Era 3 (0.14 - 0.17)

| Version | Feature | Area |
|---|---|---|
| 0.14.0 | `compat` overrides in models.json (`supportsStore`, `supportsDeveloperRole`, `supportsReasoningEffort`, `maxTokensField`) | provider |
| 0.14.0 | `xhigh` thinking level (codex-max only at first) | harness |
| 0.14.0 | Bash mode: `!cmd` runs in editor, output enters LLM context and history; also RPC `bash` | tools/TUI |
| 0.14.0 | `collapseChangelog` setting | config |
| 0.14.2 | `/debug` includes messages as JSONL | TUI |
| 0.15.0 | `AgentSession` class as central abstraction; dirs `core/ modes/ utils/ cli/` | harness |
| 0.16.0 | New RPC protocol (`RpcClient` class shipped) | RPC/SDK |
| 0.17.0 | `isCompacting`, "compacted N times" indicator, input blocked during compaction | compaction |

### Era 4 (0.18 - 0.24)

| Version | Feature | Area |
|---|---|---|
| 0.18.0 | Hooks: TS modules in `~/.pi/agent/hooks`, `.pi/hooks`; events `session_start`, `session_switch`, `agent_start/end`, `turn_start/end`, `tool_call` (can block), `tool_result` (can modify), `branch`; `pi.send()`; `ctx.ui.select/confirm/input/notify`; `--hook` flag | extension |
| 0.18.1 | Mistral provider | provider |
| 0.18.2 | Auto-retry on 429/500/502/503/504 with backoff 2s/4s/8s, `retry.*` settings, RPC events | harness |
| 0.19.0 | Skills: SKILL.md discovery from Claude, Codex and Pi locations; listed in system prompt, loaded via `read` | extension |
| 0.20.0 | Skill names must be `SKILL.md` in a dir (Codex convention) | extension |
| 0.20.1 | Skills API exported (for the `mom` package) | RPC/SDK |
| 0.21.0 | Inline image rendering (Kitty, iTerm2), Gemini 3 thinking levels | TUI/provider |
| 0.22.0 | GitHub Copilot via OAuth (incl. Enterprise) | provider |
| 0.22.3 | Streaming bash output, collapsed view shows LAST N lines | tools/TUI |
| 0.22.4 | `--list-models [search]` | harness |
| 0.23.0 | Custom tools (TS), custom TUI render, `pi.ui`, state via `onSession` | extension |
| 0.24.0 | Subagent orchestration example (scout/planner/reviewer/worker) | extension |
| 0.24.0 | `pi.exec()` signal+timeout, Kitty keyboard protocol, refresh OAuth token before each LLM call, `/hotkeys`, Agent Skills standard validation | harness/TUI |

### Era 5 (0.25 - 0.30)

| Version | Feature | Area |
|---|---|---|
| 0.25.0 | Interruptible tool execution: queued message skips remaining tools of the batch | harness |
| 0.25.0 | Google Gemini CLI and Antigravity OAuth providers | provider |
| 0.25.3 | External editor Ctrl+G, Ctrl+Z suspend, configurable skill sources, `--skills` glob filter | TUI/config |
| 0.26.0 | SDK `createAgentSession()` ("omit to discover, provide to override"), `SettingsManager`/`SessionManager` static factories | RPC/SDK |
| 0.26.0 | Project settings `.pi/settings.json` with deep merge over global | config |
| 0.27.0 | Session lifecycle hooks with `before_*` cancellable variants, `shutdown` | extension |
| 0.27.2 | `skipConversationRestore` from `before_branch` (checkpoint hooks) | extension |
| 0.27.3 | API keys in `settings.json` (moved next release, see removals) | config |
| 0.27.6 | `before_compact` event carries `previousSummary`, `messagesToKeep` | compaction/extension |
| 0.28.0 | `auth.json` (`AuthStorage`), `ModelRegistry` | config/RPC/SDK |
| 0.29.1 | `SYSTEM.md` auto-load (project beats global), unified `/settings` menu | config |
| 0.30.0 | `--session-dir`, reverse model cycling | session |

### Era 6 (0.31 - 0.34)

| Version | Feature | Area |
|---|---|---|
| 0.31.0 | Session tree (`id`/`parentId`), `/tree`, labels, branch summaries, custom entries and custom message entries | session |
| 0.31.0 | Structured compaction (Goal / Progress / Key Info / File Ops), file tracking `readFiles`/`modifiedFiles`, conversation serialized to text before summarizing | compaction |
| 0.31.0 | `context` event (non-destructive message edit before each LLM call), `before_agent_start`, `session_before_tree` | extension |
| 0.31.0 | `/share` (secret GitHub gist), HTML export tree sidebar, `enabledModels` setting | distribution/session |
| 0.31.0 | Hook `registerCommand`, `registerMessageRenderer`, `appendEntry`, `ui.custom`, `ui.editor`, `ui.setStatus` | extension |
| 0.32.0 | `steer()` and `followUp()` queue API, `steeringMode`/`followUpMode` | harness/RPC |
| 0.32.0 | Vertex AI provider, built-in provider overrides in models.json, image auto-resize (2000x2000), terminal title | provider/config |
| 0.32.1 | `!!cmd`: run bash shown in TUI, saved to session, excluded from LLM context | tools |
| 0.32.2 | Slash commands and hook commands usable during streaming; `streamingBehavior` on prompt | harness |
| 0.33.0 | `keybindings.json`, clipboard image paste, `/quit` | TUI/config |
| 0.34.0 | Hook API: `getActiveTools/setActiveTools/getAllTools`, `registerFlag`, `registerShortcut`, `setWidget`, event bus `pi.events`, `sendMessage deliverAs:"nextTurn"`; tool registry holds all built-ins, system prompt rebuilds on tool change | extension |
| 0.34.0 | Example `plan-mode.ts` (read-only mode as extension, not a core feature) | extension |

### Era 7 (0.35 - 0.45)

| Version | Feature | Area |
|---|---|---|
| 0.35.0 | Extensions unify hooks + custom tools; prompt templates (renamed slash commands) | extension |
| 0.36.0 | OpenAI Codex OAuth (ChatGPT plan) | provider |
| 0.37.0 | Headless OAuth (paste URL/code, works over SSH) | provider |
| 0.37.3 | `ui.setFooter`, `pi.sendUserMessage`, `blockImages`, session ID forwarded to providers for caching | extension/config |
| 0.37.5 | `setModel`/`getThinkingLevel`/`setThinkingLevel` for extensions; truncation utils exported | extension |
| 0.38.0 | `thinkingBudgets` setting, `--no-extensions`, async extension factories, `ctx.shutdown()`, `setEditorComponent`, dialog timeouts | extension/config |
| 0.39.0 | Pluggable tool operations (Read/Write/Edit/Bash/Ls/Grep/Find Operations) for remote (SSH) execution; `user_bash` event; `--no-tools`; overlays (experimental); theme APIs | tools/extension |
| 0.42.0 | OpenCode Zen provider | provider |
| 0.43.0 | `/scoped-models`, `model_select` hook, `/skill:name` commands, `/tree` summary options (none / summarize / custom prompt) | extension/session |
| 0.44.0 | Session naming `/name` | session |
| 0.45.0 | MiniMax, Amazon Bedrock (experimental) providers, sandbox example | provider |

### Era 8 (0.46 - 0.50.0)

| Version | Feature | Area |
|---|---|---|
| 0.46.0 | Edit tool fuzzy-match fallback (trailing whitespace, smart quotes, Unicode dashes); `APPEND_SYSTEM.md`; session search modes (fuzzy, quoted, `re:`) | tools/config |
| 0.47.0 | OpenAI Codex official support; `input` event (continue / transform / handled); `/skill:` expansion moved into AgentSession so it works in RPC/print | provider/extension |
| 0.48.0 | `quietStartup`, `shellCommandPrefix`, `navigateTree` `replaceInstructions`/`label` options, prompt template arg slicing | config |
| 0.49.0 | `ctx.compact()`, `ctx.getContextUsage()`, `pi.setLabel` | extension |
| 0.49.1 | Share URLs use hash fragments (privacy), `!cmd` API keys in models.json | distribution/config |
| 0.50.0 | Pi packages (`pi install/remove/update/list`, `pi config`), `/reload`, `pi.registerProvider()`, Azure OpenAI Responses, `disable-model-invocation` skill flag, header env/shell resolution | distribution/extension |

### Era 9 (0.50.1 - 0.56.2)

| Version | Feature | Area |
|---|---|---|
| 0.50.2 | Hugging Face provider, `PI_CACHE_RETENTION=long`, `/files`, RPC `get_commands` | provider/RPC |
| 0.50.3, 0.50.4 | Kimi provider; OSC 52 clipboard over SSH; Vercel AI Gateway routing; RPC `set_session_name` | provider/TUI/RPC |
| 0.50.8 | `retry.maxDelayMs` (fail fast on 5h quota delays), `resources_discover` hook, threaded `/resume` | harness/extension |
| 0.51.0 | Termux/Android, Nix `PI_PACKAGE_DIR`, bash spawn hook, `isToolCallEventType`, RPC extension UI protocol docs | distribution/extension |
| 0.51.1 - 0.51.3 | `ctx.switchSession`, `terminal.clearOnShrink`, `getCommands()`, local paths for `pi install` | extension/config |
| 0.52.0 - 0.52.12 | Opus 4.6, GPT-5.3 Codex, `auth.json` `!command` keys, `modelOverrides`, `ctx.reload()`, `terminal_input` event, message/tool lifecycle events to extensions, `transport` setting (sse/websocket/auto), CLI `--model provider/id:thinking` | provider/extension |
| 0.54.0 | `.agents/skills` auto-discovery (project ancestors and `~/.agents/skills`) | extension |
| 0.55.1 | `--offline` mode (`PI_OFFLINE`) | distribution |
| 0.55.2 | `pi.unregisterProvider()` | extension |
| 0.55.4 | Runtime `registerTool` visible to LLM without reload; `promptSnippet`/`promptGuidelines` on tool definitions | extension |
| 0.56.0 | OpenCode Go provider, `branchSummary.skipPrompt` | provider/config |
| 0.56.2 | GPT-5.4, Mistral native conversations SDK, `treeFilterMode` | provider/config |

## 3. Removed, deprecated, reversed (do NOT rebuild)

| Version | What was dropped or reversed | Reason (if stated) | Note for Go |
|---|---|---|---|
| 0.12.0 | `prompt` wrapper RPC command replaced by raw message objects | Align with message format | Then reversed at 0.16.0 (below) |
| 0.16.0 | Entire RPC protocol from 0.10.3/0.12.0 dropped and redesigned (typed commands, `RpcClient`) | Not stated; "completely redesigned" | Design typed command/event protocol first |
| 0.12.0 | `--print-turn`, `-P/--print-streaming`, `--no-markdown`, `--thinking-*` flags (present through 0.12.x; not in later docs) | Not stated | Absent from later entries; treat as superseded, not confirmed removed |
| 0.12.2 | gpt-4.5-preview and o3 removed | "not yet available" | Model catalog noise |
| 0.17.0 | Proactive compaction (abort mid-turn when near threshold) removed | Simplification and race avoidance; only overflow-error and post-turn threshold remain | Do not build mid-turn compaction |
| 0.17.0 | Separate turn-prefix summary storage merged into main summary | Simplification | One summary per compaction |
| 0.20.0 | Any `*.md` file as a skill | Match Codex CLI | Only `SKILL.md` in a directory |
| 0.24.0 | `{baseDir}` placeholder in skills, replaced by relative paths; prompt format to XML | Agent Skills standard | Use relative paths |
| 0.24.0 | Auto-discovered custom tool as single file `tools/x.ts` | Allow multi-file tools | Entry `index.ts` in dir (later formalised as extension discovery in 0.35, 0.50.7) |
| 0.23.0 | Hook events `session_start`, `session_switch` replaced by single `session` event | Unification | Reversed at 0.31.0 (split into granular events) |
| 0.27.0 | Separate `branch` event merged into `session` event | Unification | Reversed at 0.31.0 |
| 0.31.0 | Granular events return: `session_start`, `session_before_switch`, ... `session_shutdown` | Better granularity, cancellable | Design granular from start |
| 0.24.0 | `tool_result` hook: `result: string` removed, replaced by `content` array + `details` | Text-only lost images and structure | Tool results are content blocks, not strings |
| 0.27.6 | `apiKey` string in compaction event replaced by `resolveApiKey` function | Flexibility | Then removed at 0.31.0 in favour of `ctx.modelRegistry.getApiKey(model)` |
| 0.28.0 | `apiKeys` in `settings.json` removed; credentials moved to `auth.json` (migrated). Removed `configureOAuthStorage`, `defaultGetApiKey`, `findModel`, `discoverAvailableModels`, `getApiKey` callback | Separation of secrets from settings (added at 0.27.3, one day earlier, then reversed) | Secrets in their own file from day one |
| 0.28.0 / 0.27.8 | Settings key beat OAuth token, causing PAYG billing for plan users | Precedence bug | Priority: OAuth over settings key; 0.31.0 also `ANTHROPIC_OAUTH_TOKEN` over `ANTHROPIC_API_KEY` |
| 0.29.0 | `/clear` renamed `/new` (hook reasons `before_clear`/`clear` -> `before_new`/`new`) | Naming | |
| 0.29.1 | Separate `/thinking`, `/queue`, `/theme`, `/autocompact`, `/show-images` commands replaced by `/settings` | Consolidation | Few commands, one settings menu |
| 0.31.0 | `hookTimeout` setting removed | "hooks no longer have timeouts; use Ctrl+C" | No hook timeouts; user abort |
| 0.31.0 | `Attachment` type removed, use `ImageContent` in message content | One content model | |
| 0.31.0 | `session_before_compact`-era entry-index APIs (`entryIndex`) replaced by `entryId` | Tree sessions | |
| 0.31.0 | `dispose()` on custom tools removed | Use `onSession` with `shutdown` | |
| 0.32.0 | `queueMessage()`, `queueMode`, `queue_message`, `set_queue_mode` removed | Split into steer and followUp | |
| 0.32.0 | `AgentSession.prompt()` during streaming now throws | Race prevention; use steer/followUp | |
| 0.33.0 | `isEnter()`, `isEscape()`, `isCtrlC()` key functions removed; `matchesKey(data, "ctrl+c")` only | Configurable keybindings | |
| 0.34.0 | Image placeholders on paste removed; file path inserted instead | Simpler | |
| 0.35.0 | `hooks/` and `tools/` dirs, `--hook`/`--tool`, `hooks`/`customTools` settings, `HookAPI` and `CustomToolAPI` types removed. Unified into `extensions/`, `-e/--extension`, `ExtensionAPI`. `commands/` dir renamed `prompts/`; "slash commands" renamed "prompt templates" | "one concept, one discovery location, one CLI flag, one settings entry"; avoid clash between prompt files and extension-registered commands | Highest-value lesson |
| 0.35.0 | `CreateAgentSessionOptions.hooks` removed; `customTools` becomes `ToolDefinition[]` | Simplification | |
| 0.35.0 | Session version bump 2 -> 3 (`hookMessage` role -> `custom`) | Rename | Sessions need versioned migrations |
| 0.37.0 | Per-thinking-level Codex model variants removed | Thinking level is set separately and provider clamps | Model list should not multiply by thinking level |
| 0.38.0 | Codex model aliases (`gpt-5`, `gpt-5-mini`, `codex-mini-latest` ...) removed | Canonical IDs only | |
| 0.38.0 | `LoadedExtension` -> `Extension`; `setUIContext()` -> `runtime`; `getHasUI()` -> `hasUI()` | Runtime object holds shared state | |
| 0.39.0 | `systemPromptAppend` return of `before_agent_start` replaced by `systemPrompt` (full replacement) | Extensions need read plus replace | Hooks get current prompt and return full prompt |
| 0.40.1 -> 0.41.0 | Anthropic OAuth (`/login`) REMOVED in 0.40.1, RESTORED in 0.41.0 (one day later) | Not stated | Policy or vendor-side decision; keep OAuth behind provider abstraction |
| 0.43.0 | `/branch` renamed `/fork` (RPC, SDK, events, `doubleEscapeAction`) | Naming after `/tree` arrived | "branch" = in-place tree; "fork" = new session file |
| 0.43.0 | Extension editor: Enter submits, Shift+Enter newline (was Ctrl+Enter) | Match main editor | |
| 0.43.0 | `SessionManager.list()` async | Perf | |
| 0.44.0 | `pi.getAllTools()` returns `ToolInfo[]` not `string[]` | Richer info | Later adds parameters (0.52.9) |
| 0.45.4 | `sharp` replaced by `wasm-vips` | Native build failures on some systems | Then replaced by `photon-node` at 0.46.0 (Bun binary, WASM); avoid native image deps |
| 0.47.0 | `Editor` constructor requires `tui` first arg | Needed for render invalidation | |
| 0.47.0 / 0.49.0 | `pi-internal://` path scheme in `read` tool added (0.47.0) then removed (0.49.0) | Not stated | Model reads docs via normal paths instead |
| 0.49.2 | `strictResponsesPairing` compat option added 0.49.1, removed 0.49.2 | "no longer needed" (fixed generally) | Fix replay bugs in core rather than exposing compat toggles |
| 0.48.0 | Hardware cursor disabled by default (was on; env inverted to `PI_HARDWARE_CURSOR=1`) | Terminal compatibility | |
| 0.50.0 | External packages configured under `packages`, not `extensions`; auto-migrated. Resource loading via `ResourceLoader` only; `discoverAuthStorage` and `discoverModels` removed from SDK | Packages feature | |
| 0.50.0 | Header values in `models.json` resolve env vars | Behaviour change | |
| 0.51.0 | `ToolDefinition.execute` params reordered `(id, params, signal, onUpdate, ctx)` | Wrapping built-in tools; first four match `AgentTool.execute` | |
| 0.51.3 | RPC `SlashCommandSource` `"template"` renamed `"prompt"` | Consistency | |
| 0.52.6 | `/exit` removed, `/quit` only | Autocomplete shadowing by skills | |
| 0.52.7 | `models.json` provider `models` now MERGE by `id` with built-ins (was full replacement) | User wants to add a model without listing all; adds `modelOverrides` | |
| 0.52.10 | `ContextUsage.tokens/percent` nullable after compaction; internal fields removed | Token count unknown until next response | Model "unknown" explicitly |
| 0.52.10 | Git source without `git:` prefix accepted only for protocol URLs | Security/ambiguity | |
| 0.53.0 | `AuthStorage` constructor private; factories only. `SettingsManager` setters queue writes; call `flush()` | Locked merge-on-write | |
| 0.55.0 | Resource precedence flipped: project (`cwd/.pi`) before user-global | Project-local should win | Same for package dedupe (0.50.0: project wins) |
| 0.55.0 | Extension name conflicts no longer unload the later extension; first registration wins | Robustness | |
| 0.56.0 | Scoped models without explicit `:thinking` inherit session thinking level | Startup-captured default was surprising | |
| 0.56.0 | Node OAuth exports moved to `@mariozechner/pi-ai/oauth` subpath | Browser bundling | Keep OAuth out of core client path |
| 0.14.0, 0.31.0 | Custom themes must add new tokens (2 at 0.14, 4 more at 0.31: 46 -> 50 colors) | Themes are versioned by required tokens | If Go supports themes, give defaults for missing tokens |

Deprecation warnings (not removals) seen: `hooks/`, `tools/` dirs after 0.35.0 (startup warning); managed binaries moved from `tools/` to `bin/` at 0.37.0.

## 4. Breaking changes, especially extension, hook and SDK

Ordered by version. This is the churn map.

| Version | Break | Area |
|---|---|---|
| 0.12.0 | RPC: `prompt` wrapper -> raw message objects | RPC |
| 0.16.0 | RPC protocol replaced entirely | RPC |
| 0.14.0 | Themes need `thinkingXhigh`, `bashMode` | TUI |
| 0.20.0 | Skills must be `SKILL.md` | skills |
| 0.23.0 | Hook `session_start`/`session_switch` -> `session` with `reason` | hooks |
| 0.23.3 | `turn_end.toolResults` type `ToolResultMessage[]` | hooks |
| 0.24.0 | Custom tools require `index.ts` in subdir; `tool_result` event restructured | tools/hooks |
| 0.27.0 | `branch` event folded into `session` event; `reset/switch_session/branch` return `cancelled` | hooks/RPC |
| 0.28.0 | Credential storage and SDK auth/model APIs replaced | SDK |
| 0.29.0 | `/clear` -> `/new` | commands |
| 0.30.0 | `SessionManager` second param `agentDir` -> `sessionDir` | SDK |
| 0.31.0 | Massive: tree sessions (v2), split hook events, `pi.send` -> `sendMessage`, `ctx.exec` -> `pi.exec`, custom tool `execute` signature, `AppMessage` -> `AgentMessage`, `reset` -> `newSession`, `entryIndex` -> `entryId`, `saveXXX` -> `appendXXX`, `messageTransformer` -> `convertToLlm`, RPC `attachments` -> `images`, themes +4 tokens | all |
| 0.32.0 | `queueMessage` -> `steer`/`followUp`; RPC commands and settings renamed; `sendMessage` options object | harness/RPC/hooks |
| 0.33.0 | Key detection API replaced by `matchesKey` | TUI |
| 0.35.0 | Hooks + custom tools -> extensions; slash commands -> prompt templates; session v3; ~25 type/function renames | all |
| 0.38.0 | `ctx.ui.custom` factory adds `keybindings`; `ExtensionRunner` signature changes; Codex aliases removed | extension |
| 0.39.0 | `before_agent_start` returns `systemPrompt` not `systemPromptAppend`; `discoverSkills()` returns `{skills, warnings}` | extension/SDK |
| 0.43.0 | `/branch` -> `/fork` across RPC/SDK/events/settings; async `SessionManager.list` | all |
| 0.44.0 | `getAllTools()` returns objects | extension |
| 0.47.0 | `Editor(tui, theme)` | TUI |
| 0.50.0 | `packages` setting, `ResourceLoader`, removed SDK discover functions | SDK/config |
| 0.51.0 | `ToolDefinition.execute` param order | extension |
| 0.51.3 | RPC `"template"` -> `"prompt"` | RPC |
| 0.52.6 | `/exit` removed | commands |
| 0.52.7 | `models.json` merge semantic | config |
| 0.52.10 | `ContextUsage` nullable; git source parsing strict | extension/config |
| 0.53.0 | `SettingsManager` async persistence; `AuthStorage` factory-only | SDK |
| 0.55.0 | Project-first precedence; extension conflict policy | config/extension |
| 0.56.0 | Scoped-model thinking semantics; OAuth subpath | config/SDK |

Pattern: the extension `execute` signature changed 3 times (0.23 `(id, params, signal, onUpdate)`, 0.31 `(id, params, onUpdate, ctx, signal?)`, 0.51 `(id, params, signal, onUpdate, ctx)`). The rewrite should fix context injection before shipping a tool API.

## 5. Cross-links: added, then changed or removed inside this range

| Feature | Trajectory |
|---|---|
| Hooks | Added 0.18.0 -> tool_result restructure 0.24.0 -> event churn 0.23/0.27/0.31 -> absorbed into extensions 0.35.0 |
| Custom tools | Added 0.23.0 -> dir rule 0.24.0 -> context added 0.31.0 -> signature changes 0.51.0 -> merged with hooks 0.35.0 |
| Slash commands | File `.txt` in `slash-commands/` 0.11.0 -> `commands/` `.md` -> renamed prompt templates in `prompts/` 0.35.0; `$@` -> `$ARGUMENTS` added 0.32.2; bash arg slicing 0.48.0 |
| Queue while streaming | Message queue (0.10.0) -> interruptible tools (0.25.0) -> steer/followUp (0.32.0) -> Alt+Up restore (0.42.2) -> commands allowed during streaming (0.32.2) -> compaction-time queues (0.37.0, 0.52.7 fix) |
| Compaction | 0.12.7 added with proactive path -> 0.17.0 simplified -> pre-prompt check 0.23.3 -> structured format and file tracking 0.31.0 -> ctx.compact/getContextUsage 0.49.0 -> nullable usage 0.52.10 -> cascade fix 0.56.0. Hook `before_compact` payload changed 0.27.6 then 0.31.0 |
| Branching | `/branch` file copy (0.11.0) -> `branchedFrom` header (0.12.7) -> in-place tree `/tree` (0.31.0) -> `/branch` renamed `/fork` (0.43.0) -> `parentSession` in header (0.31.0) |
| Auth | settings.json keys (0.27.3) -> `auth.json` (0.28.0) -> `!command` keys (0.49.1/0.52.0) -> locked merge storage (0.53.0) |
| Anthropic OAuth | Present -> removed 0.40.1 -> restored 0.41.0 |
| Image handling | `sharp` -> `wasm-vips` (0.45.4) -> `photon-node` (0.46.0); resize policy (0.32.0), retry-shrink 5MB (0.32.3); paste placeholder removed (0.34.0) |
| Skills | Discovery (0.19) -> SKILL.md-only (0.20) -> Agent Skills standard (0.24) -> configurable sources (0.25.3) -> `/skill:` commands (0.43.0) -> expand in AgentSession (0.47.0) -> `disable-model-invocation` (0.50.0) -> `.agents/skills` (0.54.0) |
| Provider config | models.json custom (0.10.1) -> compat block (0.14.0) -> built-in override (0.32.0) -> registerProvider (0.50.0) -> merge by id + modelOverrides (0.52.7) -> unregister (0.55.2) |
| Retry | 0.10.2 -> 0.12.3 (Anthropic 10s base) -> 0.18.2 (2/4/8s) -> counter reset per successful response 0.50.2 -> maxDelayMs 0.50.8; 429 must NOT trigger compaction (0.50.2) |
| System prompt | `--system-prompt` (0.10.2) -> append flag (0.12.11) -> SYSTEM.md (0.29.1) -> APPEND_SYSTEM.md (0.46.0) -> hook can replace fully (0.39.0) -> tools add snippet/guidelines (0.55.4) |
| Extension loading | TS via jiti; Bun binary needed `@mariozechner/jiti` fork with virtual modules (0.45.2); repeated Windows/global-install alias fixes (0.56.1, 0.55.1, 0.29.1) |

## 6. Lessons (patterns)

What kept changing (design instability):
1. Extension API surface: 4 restructurings in 5 weeks. Root cause was starting with separate hooks and tools without shared context. A single `Extension` with events + tools + commands + UI from the start avoids it.
2. Session model: linear -> tree. Any later reconstruction path (compaction, export, RPC entry IDs) then had to change. The header's `parentSession`, entry `id`/`parentId`, and typed entries (custom, label, branch summary) should be in v1.
3. Naming: `clear/new`, `branch/fork`, `template/prompt`, `slash command/prompt template`, `exit/quit`. Each rename touched RPC, SDK, events and settings. Decide names before publishing the wire protocol.
4. RPC protocol replaced once (0.16.0), then grew commands steadily (bash, compact, get_commands, set_session_name, extension UI sub-protocol 0.51.0). A Go rewrite that exposes gRPC/WS should define these commands early (prompt, steer, follow_up, abort, bash, compact, new_session, fork, switch_session, get_commands, set_session_name, model/thinking setters, extension-UI request/response).
5. Credentials and settings persistence: three rewrites (0.27.3, 0.28.0, 0.53.0) plus repeated bugs of overwritten user edits (0.30.x fixed, 0.38.0, 0.50.2, 0.50.4 invalid JSON wiped). Write policy: never overwrite on parse failure; locked merge on write.

What was hard (persistent bug classes):
1. Provider replay and cross-model handoff: thinking signatures, unsigned thinking to text, tool-call ID formats (Bedrock, Responses pipe-separated IDs, Gemini), empty reasoning items, aborted turns, orphaned tool results after error messages (0.49.0), `store:false` (0.52.7). Design a canonical message model with per-provider converters and a normalization step for cross-provider switches.
2. Context overflow detection across providers (`context_length_exceeded` text patterns 0.38.0, 429 not overflow 0.50.2, cascades 0.56.0, checking latest not first compaction 0.52.10).
3. Terminal input: Kitty protocol, Caps/Num Lock, non-Latin layouts, IME, wide chars, SSH batching, resize. The Go TUI (bubbletea) has its own answers; treat these as a test checklist.
4. Cross-platform shells: Windows bash resolution (0.13.1), CRLF and BOM in edit (0.31.0), `fd`/`rg` bootstrapping (0.11.1, 0.37.0, 0.52.9, 0.55.1 offline).
5. Tool argument robustness: string numbers coerced (0.48.0), malformed args objects (0.52.0), missing inputs default (0.50.4), truncated trailing JSON (0.52.10).
6. Edit tool matching: needed fuzzy fallback (0.46.0), plus CRLF, BOM, macOS curly quote and NFD path names in read (0.50.4), Narrow No-Break Space in screenshots (0.21.0), read path handling.

Design choices that survived and look right to copy:
- Small tool set: read, write, edit, bash, grep, find, ls (0.34.0); everything else via extensions. No built-in plan mode or subagents: both are shipped as examples (0.34.0 plan-mode, 0.24.0 subagent).
- Tool output policy: 2000 lines OR 50KB, never partial lines, actionable notices (0.13.2). Bash tail truncation with full output in a temp file.
- `!cmd` / `!!cmd` user bash with or without LLM context (0.14.0, 0.32.1).
- Tool registry contains all tools; active set is dynamic; system prompt rebuilt (0.34.0).
- Skills follow the open Agent Skills standard with progressive disclosure via `read` (0.19.0, 0.24.0).
- "Omit to discover, provide to override" SDK philosophy (0.26.0).
- Project settings overlay global with deep merge; project wins on resource conflicts (0.26.0, 0.55.0).
- Structured compaction with file tracking; serialize conversation to text before summarizing (0.31.0).
- No hook timeouts; user abort instead (0.31.0).

Ideas that were tried and regretted or short-lived (avoid): settings-file API keys, proactive compaction, `pi-internal://`, `strictResponsesPairing`, per-thinking-level model variants, separate commands for each setting, wrapper `prompt` RPC, Node OAuth in the core client entry, `sharp`.

Scope traps the Go rewrite can defer (Pi added them late and they are large): package manager for extensions (0.50.0), HTML export/share (0.31.0, 0.49.x), overlays and custom editors (0.38.0-0.45.6), many provider-specific OAuth flows (Copilot 0.22, Gemini CLI/Antigravity 0.25, Codex 0.36, Anthropic 0.41), Bedrock/Vertex/Azure, Termux/Nix support, Kitty image rendering.

## 7. Limits of this research

- Changelog only; "Fixed" entries were skimmed for design signals, not audited. Another lane covers them.
- No source confirmation was done; a changelog entry can lag or misdescribe code.
- Versions 0.12.6, 0.34.2, 0.37.6-8 and a few others have empty or trivial entries.
- Reasons are recorded only when the changelog states them; many removals have none.
- Dates are the changelog's, not git tags.

## Unresolved questions

1. Why was Anthropic OAuth removed in 0.40.1 and restored in 0.41.0 (vendor policy, ToS, or a bug)? The changelog gives no reason. Needs the git log or issue tracker.
2. Are the 0.12.0 print flags (`-P`, `--print-turn`, `--no-markdown`) still in the current CLI? Later changelog (lines 1-3016) or `src/cli` should confirm.
3. Was the initial `glob`/`think` tool (0.10.0) replaced by `find` and dropped? Tools seen at 0.34.0 are read, bash, edit, write, grep, find, ls; no changelog line records the rename or removal of `glob` and `think`.
4. What happened to the Gmail and Calendar OAuth integration listed in 0.10.0? No later mention in this range.
5. Does the `mom` package (0.20.1 note) matter for the Go scope (a Slack-style agent bot reusing skills)?

Status: DONE
Summary: Read all lines 3017-5979 of the changelog and wrote the era timeline, feature tables, removals/reversals, breaking changes, cross-links and lessons.
Concerns: Changelog-only evidence, no source confirmation; five open questions listed above.
