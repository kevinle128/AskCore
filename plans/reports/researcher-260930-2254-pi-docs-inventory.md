# Pi coding agent: documentation feature inventory (lane A)

Scope: `/Users/dale/Desktop/workspace/opensources/pi/packages/coding-agent/{README.md,docs/*}` (39 .md + docs.json), pi 0.99.1, commit 2bbfcca43. Purpose: feature list for a Go rewrite (Ask). Not a code walkthrough.

Conventions. Source column: doc file name (all under `packages/coding-agent/docs/`), or `src:` = path under `packages/coding-agent/src/` confirmed in code. Ring: 0 core flow, 1 user-visible feature, 2 surface (key/flag/env/cmd/event), 3 feature interaction, 4 edge case/limit. "Go" = note for the rewrite. `[doc gap]` = doc is silent or inconsistent, confirmed in source.

Prior reports (`/Users/dale/Desktop/workspace/opensources/ask/plans/reports/researcher-260930-2100-pi-feature-inventory.md`, `-2230-pi-099-delta-inventory.md`) are in Vietnamese. Verified: RPC command count 33 matches `src/modes/rpc/rpc-types.ts:22-74`; tool defaults and settings defaults below match code. Extended: this report adds docs-only material the delta report did not carry (terminal/tmux/Windows/Termux, security model, containerization, llama.cpp classifier prompt, MCP OAuth details, virtual-model routing contract, theme system, packages semantics).

Top findings for Ask (read first):
1. Pi is an agent harness with 4 run modes over one core. The wire contracts worth copying exactly are: session JSONL (v3, tree), JSON/RPC event stream (delta-only `message_update`, `agent_settled`), 33 RPC commands, extension-UI subprotocol.
2. There is no permission system. Security = project trust (gates loading of project resources) plus external isolation. Ask needs to decide this early (rows S1-S9).
3. Compaction is threshold + overflow + manual, with a fixed structured summary, split-turn handling, and file-op tracking (section 6). Cut-point rules are precise and portable to Go.
4. Extensions are in-process TypeScript (jiti). Not portable to Go as-is. MCP + codemode + tool_search are the language-neutral extension path (section 12).
5. Much of the doc surface is TUI-only (keybindings, themes, terminal quirks). Out of scope for Ask (no web UI; TUI is a separate client per AskCore CLAUDE.md).

---

## 1. Run modes and invocation

| Feature | Ring | Detail | Source | Go note |
|---|---|---|---|---|
| Interactive TUI | 0 | Default when stdin+stdout are TTYs and no `--print`/`--mode json|rpc`. Regular (native scrollback) or fullscreen (alt-screen, fixed editor) via `--tui-mode` / `tuiMode`. | cli.md, usage.md | Ask TUI is separate client (bubbletea). |
| Print mode | 0 | `-p/--print`: run prompts, write final assistant text to stdout, exit. Auto-selected when stdin or stdout is redirected and mode not json/rpc. Errors to stderr. Exit non-zero when final stop reason is `error`/`aborted`. | cli.md, cli-integration.md | Cheap to build first. |
| JSON mode | 0 | `--mode json`: one session header line then agent/session events as JSONL, then exit. All prompts given at start; no later input. Failed/aborted assistant response does NOT set non-zero exit (only thrown errors do). | cli-integration.md, json.md | |
| RPC mode | 0 | `--mode rpc`: long-lived, JSONL commands on stdin, responses+events on stdout. No session header. `@file` args rejected. stdin close = orderly shutdown; extension `ctx.shutdown()` completes after command or `agent_settled`. | rpc.md | Ask already has gRPC/WS; keep RPC command set as semantic reference. |
| SDK mode | 0 | `createAgentSession()` in Node/Bun; `AgentSessionRuntime` adds newSession/switchSession/fork/importFromJsonl (replaces session, subscriptions must be rebound). | sdk.md | Go equivalent = importable package + gRPC. |
| `--mode text` | 2 | Text output selector; does not force one-shot on TTY (use `--print`). | cli.md | |
| Invocation grammar | 2 | `pi [options] [--] [@files...] [messages...]`; `@path` includes text file or image in first prompt (resolved from cwd); piped stdin is prepended to first prompt; `--` stops option parsing. | cli.md | |
| `--export <in> [out]` | 2 | Export session file to HTML and exit; derive dest if omitted. | cli.md | |
| `--offline` | 2 | Disable automatic network incl. model catalog refresh (= `PI_OFFLINE=1`). | cli.md | |
| `--verbose` | 2 | Verbose startup info; overrides `quietStartup`. | cli.md | |
| `-h/--help`, `-v/--version` | 2 | Help includes flags registered by loaded extensions. Unknown short options rejected; extensions may add long options (`pi.registerFlag`). | cli.md | |
| Subcommands | 2 | `install`, `remove`/`uninstall`, `update`, `list`, `config`, `auth`, `mcp` (see sections 9, 10, 13). Experimental `server`/`client` exist behind `PI_EXPERIMENTAL=1` (undocumented in docs/). | cli.md; src: `cli/experimental/commands/{server,client}.ts`, `experimental/services/README.md` | Ignore experimental. |
| Fork/rebrand | 4 | `package.json` `piConfig: {name, configDir}` and `bin` change CLI name, config dir, derived env var names. | cli-integration.md | Config-dir naming should be a constant in Go. |
| Install | 1 | `npm i -g --ignore-scripts @earendil-works/pi-coding-agent` (Node >=22.19); `curl https://pi.dev/install.sh | sh` (macOS/Linux, has Uninstall option). Uninstall leaves `~/.pi/agent`. | README.md, quickstart.md | Go = single binary; skip. |

## 2. CLI flags (complete list from cli.md, cross-checked with `src/cli/args.ts`)

| Flag | Ring | Detail | Source |
|---|---|---|---|
| `--provider <name>` | 2 | Restrict `--model` lookup to provider. | cli.md |
| `--model <pattern>` | 2 | Exact ID or fuzzy ID/name; accepts `provider/id` and `:<thinking>` suffix (e.g. `sonnet:high`). | cli.md |
| `--api-key <key>` | 2 | Non-persistent override; requires model via `--model`/`--models`. Highest credential precedence. | cli.md, models.md |
| `--thinking <level>` | 2 | `off,minimal,low,medium,high,xhigh,max`; overrides `--model` suffix; clamped to model capability. | cli.md |
| `--models <patterns>` | 2 | Comma list: exact, fuzzy, case-insensitive globs, `:<thinking>` suffix. Scope for startup and Ctrl+P cycling. | cli.md |
| `--list-models [search]` | 2 | List models (fuzzy filter), exit. | cli.md |
| `-c/--continue` | 2 | Most recent session for cwd. | cli.md |
| `-r/--resume` | 2 | Session picker. | cli.md |
| `--session <path|id>` | 2 | File path, exact or partial ID. Searches current project first; offers to fork a cross-project match. | cli.md |
| `--session-id <id>` | 2 | Open or create exact ID. Chars `[A-Za-z0-9._-]`, must start and end alnum. Not with `--session/-c/-r`; may combine with `--fork` to choose new ID. | cli.md |
| `--fork <path|id>` | 2 | Fork into new session for current project. Not combinable with `--session,-c,-r,--no-session`. | cli.md |
| `--session-dir <dir>` | 2 | Overrides `PI_CODING_AGENT_SESSION_DIR` and `sessionDir`. | cli.md |
| `--no-session` | 2 | In-memory, unresumable. | cli.md |
| `-n/--name <name>` | 2 | Session display name. | cli.md |
| `-t/--tools <list>` | 2 | Replace default selection with allowlist (built-in, extension, custom). No `+/-` syntax. | cli.md |
| `-xt/--exclude-tools <list>` | 2 | Disable names after all other selection. | cli.md |
| `-nbt/--no-builtin-tools` | 2 | Disable default built-ins, keep extension/custom tools. | cli.md |
| `-nt/--no-tools` | 2 | Disable everything. | cli.md |
| `-e/--extension <path>` | 2 | File, dir, npm/git package spec, or `builtin:<name>`; repeatable. | cli.md, packages.md |
| `-ne/--no-extensions` | 2 | Disable discovered+configured+built-in; explicit `-e` still loads (`pi -ne -e builtin:mcp`). | cli.md |
| `--skill <path>` / `-ns/--no-skills` | 2 | Repeatable / disable discovery (explicit still loads). | cli.md |
| `--prompt-template <path>` / `-np/--no-prompt-templates` | 2 | Same pattern. | cli.md |
| `--theme <path>` / `--no-themes` / `--use-theme <name[/name]>` | 2 | Load theme files; disable discovery; select initial theme (`light/dark` pair allowed). | cli.md, themes.md |
| `-nc/--no-context-files` | 2 | Disable AGENTS.md / CLAUDE.md discovery. | cli.md |
| `--system-prompt <text|path>` | 2 | Replace system prompt (text or file). | cli.md |
| `--append-system-prompt <text|path>` | 2 | Append; repeatable. | cli.md |
| `--tui-mode regular|fullscreen` | 2 | | cli.md |
| `-a/--approve`, `-na/--no-approve` | 2 | Trust / ignore project-local config for this process; also on package/mcp commands. Applies before any extension/saved decision. | cli.md, security.md |

## 3. Subcommands

| Command | Ring | Detail | Source |
|---|---|---|---|
| `pi install <src> [-l]` | 1 | Sources: `npm:pkg@ver`, `git:host/repo@ref`, https URL (git), local path. `-l/--local` writes `.pi/settings.json`. | cli.md, packages.md |
| `pi remove|uninstall <src>` | 1 | Removes package and settings entry. | cli.md |
| `pi list` | 1 | Configured packages. | cli.md |
| `pi update [target]` | 1 | No target = update Pi itself. `--extensions` all packages, `<source>` one, `--models` refresh model catalogs, `--all`, `--force` (reinstall Pi). Aliases `--self/self/pi`, `--extension <src>`. | cli.md |
| `pi config [-l]` | 1 | TUI to enable/disable discovered resources and built-in extensions; Tab switches user/project scope. | packages.md, cli.md |
| `pi auth check` | 1 | Prints `ready|not_ready|invalid`, exit 0/1/2. Needs `--provider` or `--model`. Flags: `--json`, `--credentials`, `--no-refresh` (refresh expired OAuth by default). | cli.md |
| `pi auth print-api-key` / `print-bearer-token [--min-expiry 30m]` | 1 | Print secrets to stdout (`ms|s|m|h` durations). | cli.md |
| `pi mcp add/remove/list/login/logout` | 1 | See section 13. Do not load extensions; work outside a session. | cli.md, mcp.md |

## 4. Slash commands and `!` shell prefix

| Feature | Ring | Detail | Source | Go note |
|---|---|---|---|---|
| Built-ins (24) | 2 | `/settings /model [p/m] /thinking [lvl] /scoped-models /login [p] /logout /new /resume /name [n] /session /tree /fork /clone /compact [instr] /import <path> /copy /export [path] /share /bug [desc] /trust /reload /hotkeys /changelog /quit`. | slash-commands.md; src: `core/slash-commands.ts:19-43` (exact 24) | Map to server ops where meaningful; TUI-only ones stay client-side. |
| Extension-provided commands | 2 | `/llama` (builtin:llama.cpp), `/mcp [login|logout|reconnect <server>]` (builtin:mcp). Not in `BUILTIN_SLASH_COMMANDS`. | llama-cpp.md, mcp.md; src: `extensions/{llama,mcp}` | [doc gap] slash-commands.md lists `/llama` but omits `/mcp`. |
| `/debug` | 2 | Writes rendered terminal lines + session messages to `<agent-dir>/pi-debug.log` (may contain secrets). | usage.md; src: `modes/interactive/interactive-mode.ts:3217` | [doc gap] Not in slash-commands.md nor `BUILTIN_SLASH_COMMANDS`; special-cased in the interactive loop. |
| `/skill:name [args]` | 1 | Skill as command; args appended as user request; controlled by `enableSkillCommands` (manual entry always works). | skills.md, slash-commands.md | |
| Prompt templates as `/name` | 1 | See section 8. | prompt-templates.md | |
| `!cmd` / `!!cmd` | 1 | `!` runs via resolved bash and includes output in conversation; `!!` runs without sending output to model. Not given `PI_*` session env. Uses `shellCommandPrefix`. Stored as `bashExecution` message. | usage.md, environment-variables.md, message-types.md | |
| RPC excludes TUI commands | 3 | `get_commands` returns only extension/prompt/skill commands; `/settings`, `/hotkeys` etc. do not run through `prompt`. | rpc-commands.md | |
| Extension command precedence | 3 | Extension command with same name wins over template; extensions see raw input first via `input` event. | prompt-templates.md | |

## 5. Session features

| Feature | Ring | Detail | Source | Go note |
|---|---|---|---|---|
| Persistence | 0 | Auto-saved JSONL under `~/.pi/agent/sessions/--<path>--/<timestamp>_<id>.jsonl`; path: leading sep removed, `/ \ :` become `-`. | session-format.md | Ask uses SQLite (gorm); keep tree model + entry types, not file layout. |
| Tree model | 0 | Entries have `id` (8-hex, may fall back to UUID) and `parentId`; leaf = current position; branching adds children in same file. Multiple roots possible (`resetLeaf()`, `branchWithSummary(null,...)`). | session-format.md, how-pi-works.md | |
| Session versions | 4 | v1 linear, v2 tree, v3 `hookMessage`->`custom`; auto-migrated on load. (Prior report notes v4 exists in newer harness; docs say v3.) | session-format.md | |
| Entry types (12) | 2 | `session`(header), `message`, `model_change`, `thinking_level_change`, `usage`, `compaction`, `context_edit`, `branch_summary`, `custom`, `custom_message`, `label`, `session_info`. | session-format.md | Copy as DB enum. |
| Header | 2 | `{type:"session",version:3,id,timestamp,cwd,parentSession?}`; `parentSession` set by fork/clone/newSession. | session-format.md | |
| System messages in transcript | 3 | First request persists a leading system message with `sections` (named prompt parts) + `toolsAdded`; later changes are patch system messages (`sections` null removes, `toolsAdded/Removed`, `replace:true` = new baseline). No separate prompt-state entry. Old sessions replay from later system message. | session-format.md, message-types.md, extensions.md | Design item: prompt = event-sourced. |
| Message roles | 2 | `system, user, assistant, toolResult, bashExecution, custom, branchSummary, compactionSummary`. Timestamps: message = unix ms; entry = ISO-8601. | message-types.md | |
| Assistant message | 2 | `api, provider, model, responseModel?, responseId?, providerThinkingLevel?, diagnostics?, usage, stopReason(pending|stop|length|toolUse|error|aborted|deferred), deferred?, errorMessage?, rawStopReason?, endTurn?`; `pending` never persisted. Newer messages also record `thinkingLevel`. | message-types.md, session-format.md | |
| Deferred responses | 4 | `stopReason:"deferred"` + `DeferredHandle{provider,modelId,api,id,expiresAt?,pollAfterMs?,data?}` for later retrieval. | message-types.md | Likely skip in v1. |
| Content blocks | 2 | `text(+textSignature)`, `image(base64+mimeType)`, `thinking(+thinkingSignature, redacted)`, `toolCall(id,name,arguments,thoughtSignature?,namespace?)`. Signatures opaque, replay data. | message-types.md | |
| Usage | 2 | `input,output,cacheRead,cacheWrite,cacheWrite1h?,reasoning?(included in output),totalTokens,cost{...}`. Tool results can carry `usage` (nested model work). | message-types.md | |
| Usage entries | 2 | Non-context usage (e.g. `kind:"cache_warm"`); counts toward totals; hidden from tree; unknown `kind` treated as normal usage. | session-format.md | |
| `context_edit` | 3 | Append-only omit/replace of a earlier user/assistant/toolResult/custom_message entry for future context only; latest edit on active branch wins; branch-relative. Used by overflow/length recovery to drop failed attempt. | session-format.md, compaction.md | Powerful, small. Consider. |
| Labels | 1 | `label` entries bookmark a target; `/tree` `Shift+L` edits; `Shift+T` toggles timestamps; `labeled-only` filter. | session-format.md, keybindings.md | |
| Session name | 1 | `/name`, `-n`, `pi.setSessionName()`; shown in `/resume` instead of first message. | session-format.md | |
| Continue/resume | 1 | `-c`, `-r`, `/resume`, `/new`. Picker: search, rename (Ctrl+R), delete (Ctrl+D, uses `trash` CLI if available), toggle path (Ctrl+P), sort (Ctrl+S), named-only (Ctrl+N), delete-when-query-empty (Ctrl+Backspace). | sessions.md, keybindings.md, session-format.md | |
| `/tree` | 1 | Navigate in same file; selecting user message puts text back in editor to create a new branch; selecting other entry continues after it. Filters `default,no-tools,user-only,labeled-only,all`; fold/unfold branch segments. `doubleEscapeAction` (empty editor) = tree|fork|none. | sessions.md, keybindings.md, settings.md | |
| `/fork` | 1 | New session file from earlier user message. | sessions.md | |
| `/clone` | 1 | Copy active branch to new session at current position. | sessions.md | |
| `/import <path>` | 1 | Import and resume a JSONL. | slash-commands.md | |
| `/export [path]` | 1 | HTML (default) or JSONL; themed HTML; includes nested tool call records. | slash-commands.md, themes.md, extensions.md | |
| `/share` | 1 | Radius artifact if Radius auth configured (visible to org), else private GitHub gist via `gh`. `PI_SHARE_VIEWER_URL` overrides viewer. | sessions.md, usage.md | Skip. |
| `/bug` | 1 | Private report: optional transcript, or model-generated summary; env + provider config without credential values + error diagnostics; upload to `radius.pi.dev` (no login needed) or export zip. `PI_RADIUS_GATEWAY` overrides. | sessions.md, environment-variables.md | Skip. |
| `/session` | 1 | File, ID, message count, tokens, cost per physical model, next cache-warming decision. | sessions.md, virtual-models.md, settings.md | |
| Ephemeral | 1 | `--no-session`. | sessions.md | |
| Session dir precedence | 2 | `--session-dir` > `PI_CODING_AGENT_SESSION_DIR` > `sessionDir` setting (relative resolves from cwd) > `<agent-dir>/sessions`. | sessions.md, settings.md | |
| `sessionDir` before trust | 4 | Project `sessionDir` is read before project trust is resolved; declining trust cannot undo that lookup. | configuration.md, security.md | Security gotcha to avoid in Go. |
| Model/thinking restore | 3 | Session records model + thinking changes; resume restores without changing new-session defaults. Virtual selection restored from latest `model_change`, fallback to last physical model if virtual model gone. | models.md, virtual-models.md | |
| Session-branch state | 3 | Extension state via tool-result `details`, `custom` entries, `custom_message`; rebuild from `getBranch()` not all entries. Router state, codemode `store()`, tool_search loads all follow the branch. | extensions.md, virtual-models.md, cli.md | |
| Message-kind flow | 3 | Steering enters after current assistant turn's tools, before next LLM call; follow-up after agent finishes; abort returns queued messages to editor. | how-pi-works.md, usage.md | |

## 6. Compaction and branch summaries

| Feature | Ring | Detail | Source | Go note |
|---|---|---|---|---|
| Auto trigger (threshold) | 0 | `contextTokens > contextWindow - reserveTokens` (defaults reserve 16384). `src: core/compaction/compaction.ts:269`. | compaction.md | |
| Check points | 3 | (a) between turns after tool results appended, before next assistant response (in `prepareNextTurn`), skipped if batch terminates the run and nothing queued; (b) before a new user prompt; (c) final-attempt overflow recovery after run ends. | compaction.md | |
| Overflow / length recovery | 3 | Provider context-overflow error or early `stopReason:"length"` selects ONE compact-and-retry. Order: persist assistant -> `turn_end` -> `agent_end` -> append `context_edit` omissions for failed attempt -> `session_before_compact` + append compaction -> fresh retry run. If recovery compaction fails/cancelled: keep omissions, no compaction, no retry. `length` with tool calls keeps synthetic failed tool results. | compaction.md | |
| Manual | 1 | `/compact [instructions]`; works even when auto disabled. RPC `compact {customInstructions}`. | sessions.md, rpc-commands.md | |
| Cut point algorithm | 0 | Walk backward accumulating token estimates until `keepRecentTokens` (20000). Valid cuts: user, assistant, bashExecution, custom messages (custom_message, branch_summary). Never cut at tool results. | compaction.md | Port verbatim. |
| Iterative summary | 0 | Summarized span starts at previous compaction's `firstKeptEntryId`; previous summary passed as context; `tokensBefore` recomputed from rebuilt, context-edited projection. Retain-none compaction stores its own ID as `firstKeptEntryId`. | compaction.md | |
| Split user-message span | 3 | If one user span exceeds `keepRecentTokens`, cut inside it at an assistant message; two summaries (history + span prefix) merged. | compaction.md | |
| Cut advance over omitted suffix | 4 | Kept boundary moves into a context-invisible suffix only if suffix has omitted assistant attempt and no unomitted context-producing entries; replacement edits touching candidate input block advancement. | compaction.md | Advanced; defer. |
| Summary format | 0 | Markdown: Goal, Constraints & Preferences, Progress (Done/In Progress/Blocked), Key Decisions, Next Steps, Critical Context (compaction only); branch summary stops at Next Steps. Appended `<read-files>` / `<modified-files>` lists. | compaction.md | |
| Serialization for summarizer | 2 | `[User]:`, `[Assistant thinking]:`, `[Assistant]:`, `[Assistant tool calls]: name(k="v"); ...`, `[Tool result]:`; tool results truncated to 2000 chars with a marker. | compaction.md | |
| Prompt cache | 3 | Summarization requests disable prompt-cache writes. | compaction.md | |
| CompactionEntry | 2 | `summary, firstKeptEntryId, tokensBefore, usage?, fromHook?, details?, systemMessage?` (checkpoint of prompt+tools). `details` default `{readFiles, modifiedFiles}`. Usage counted in session totals. | compaction.md, session-format.md | |
| Context rebuild | 0 | Latest compaction on path: compaction summary first, then non-system entries from `firstKeptEntryId` to before the entry, then entries after; pre-compaction system messages folded into checkpoint; `context_edit` projection applied after. | session-format.md | |
| Branch summarization | 1 | On `/tree` navigation Pi offers to summarize the abandoned branch (common ancestor -> old leaf, newest-first within token budget `branchSummary.reserveTokens`); entry `branch_summary{fromId,summary,usage,details}` appended at navigation point. `branchSummary.skipPrompt` defaults to no summary. | compaction.md, settings.md | |
| Cumulative file tracking | 3 | File lists from tool calls carried across default compactions and nested default branch summaries; not carried from extension summaries (`fromHook:true`). Nested tool calls (codemode) contribute via `nestedCalls`. | compaction.md, extensions.md | |
| Per-model overrides | 2 | `compaction.modelOverrides["provider/modelId"].{reserveTokens,keepRecentTokens}`; keys exact/case-sensitive; each field falls back independently: model override, ordinary setting, default. Non-negative safe integers; invalid values in matching override error at read. `enabled` global only. `reserveTokens` also caps summary output tokens. Files merge recursively (global model-specific beats project-wide fallback). | compaction.md, settings.md | |
| Virtual model interplay | 3 | Compaction uses limits of the routed physical model per request; if window too small, compacts before send; route unchanged. Context usage uses last responding physical model, else the virtual model's declared limits. | virtual-models.md | |
| Post-compaction context usage | 4 | `contextUsage.tokens/percent` are `null` until first post-compaction assistant response. `estimatedTokensAfter` is heuristic. | rpc-commands.md | |
| Events | 2 | `compaction_start{reason manual|threshold|overflow}`, `compaction_end{reason,result?|aborted|errorMessage,willRetry}`; `summarization_retry_scheduled/attempt_start/finished` (source compaction|branchSummary). | json.md | |
| Extension hooks | 2 | `session_before_compact` (cancel or supply summary; gets preparation, branchEntries, customInstructions, reason, willRetry, signal), `session_compact_failed`, `session_before_tree` (cancel or custom summary; `userWantsSummary`). | compaction.md | |
| Failure mode | 4 | Provider unavailable or cannot take summary request: fix and rerun `/compact`. Disabling auto keeps manual. | sessions.md | |

## 7. Models, providers, auth

| Feature | Ring | Detail | Source | Go note |
|---|---|---|---|---|
| Model catalog | 1 | Bundled catalog; overlays newer data from pi.dev; cached copy works offline; `pi update --models` forces refresh; `PI_OFFLINE` disables. | models.md | Ask providers package. |
| Picker | 1 | `/model` shows only models whose provider has usable auth; Ctrl+L opens; Ctrl+S saves default. `/thinking` (Shift+Tab cycles; Ctrl+S saves). Ctrl+P / Shift+Ctrl+P cycle models. `/scoped-models` selector (Ctrl+A all, Ctrl+X clear, Ctrl+P toggle provider, Alt+Up/Down reorder). Opening `/model` reloads `models.json`. | models.md, keybindings.md | |
| Thinking levels | 0 | `off,minimal,low,medium,high,xhigh,max` (default `medium`; `src: core/defaults.ts:3`). Clamped to model; `xhigh/max` only when supported. Per-model startup level: `modelThinkingLevels`. Budgets: `thinkingBudgets` for minimal/low/medium/high. | settings.md, rpc-commands.md | |
| Credential precedence | 0 | `--api-key` > `auth.json` > `models.json` `apiKey` > provider env vars / ambient cloud creds. Provider extensions can define their own. | models.md | Port this order. |
| `/login` `/logout` | 1 | OAuth (browser/device flow; headless: paste redirect URL or code) or API key; stored in `auth.json`. Logout does not unset env, does not revoke. | providers.md | |
| Key from command | 2 | `auth.json` `key: "!cmd"`: run when first needed, stdout cached for process lifetime; empty/timeout/non-zero leaves unresolved until restart. | providers.md | |
| Per-credential env | 2 | `auth.json` entry may have `env{}` taking priority over process env for that provider. | providers.md | |
| Value syntax (models.json, custom providers) | 2 | `$NAME`, `${NAME}`, leading `!command` (whole value, run at request time, not cached), `$$` literal `$`, `$!` literal leading `!`. | models.md, custom-provider.md, mcp.md | Common resolver. |
| API-key env vars | 2 | Anthropic `ANTHROPIC_API_KEY` (also `ANTHROPIC_OAUTH_TOKEN`, `ANTHROPIC_AUTH_TOKEN` bearer), Ant Ling, OpenAI, DeepSeek, NVIDIA NIM, Gemini `GEMINI_API_KEY`, Copilot `COPILOT_GITHUB_TOKEN`, Mistral, Groq, Cerebras, xAI, OpenRouter, Vercel `AI_GATEWAY_API_KEY`, ZAI (global/CN), OpenCode Zen/Go, Radius, TypeSafe, Hugging Face `HF_TOKEN`, Fireworks, Together, Baseten, Kimi, Meta, MiniMax (global/CN), Moonshot, Qwen token plan (global/CN), Xiaomi MiMo (5 regions). | providers.md | 35+ providers; pick a short list for Ask. |
| Cloud providers | 2 | Azure OpenAI (`AZURE_OPENAI_API_KEY` + `_BASE_URL` or `_RESOURCE_NAME`; normalizes ai.azure.com/cognitiveservices/openai.azure.com roots); Bedrock (`AWS_PROFILE`, keys+`AWS_SESSION_TOKEN`, `AWS_BEARER_TOKEN_BEDROCK`, `AWS_REGION`/`AWS_DEFAULT_REGION`, ECS/IRSA); Cloudflare AI Gateway (`CLOUDFLARE_API_KEY/ACCOUNT_ID/GATEWAY_ID`); Workers AI; Vertex (`GOOGLE_CLOUD_API_KEY` or ADC with `GOOGLE_CLOUD_PROJECT|GCLOUD_PROJECT`+`GOOGLE_CLOUD_LOCATION`, or `GOOGLE_APPLICATION_CREDENTIALS`). | providers.md | |
| Radius | 4 | Gateway catalog, cached for offline start; custom gateway in `models.json` uses its own catalog. | providers.md | Skip. |
| Wire APIs supported | 2 | Anthropic Messages, OpenAI Chat Completions, OpenAI Responses, Google Generative AI, Vertex, Azure OpenAI Responses, Mistral Conversations, Bedrock Converse; plus image + classifier operations. | custom-provider.md | Ask's `providers/` must cover a subset. |
| `models.json` | 1 | `providers.<name>{baseUrl, api, apiKey, headers, models[], modelOverrides}`; `models` add/replace by id; `modelOverrides` patch built-in/extension models (unknown IDs ignored). Model fields: `id, name, api, baseUrl, reasoning, input[text|image], contextWindow, maxTokens, cost{input,output,cacheRead,cacheWrite per M}, inputLimits, promptCache, compat flags`. | models.md, rpc-commands.md | |
| Image handling | 3 | `inputLimits.images.resize{maxWidth,maxHeight,maxBytes,jpegQuality}`; defaults 2000x2000, 4.5 MiB base64, JPEG q80; encoded once at attach (not rewritten on model switch). Catalog fields `maxRequestBytes`, `images.maxPerMessage/maxPerRequest` exist but Pi does NOT enforce them yet. `images.autoResize`, `images.blockImages` settings. | models.md, settings.md | |
| Prompt cache lifetime | 2 | `promptCache:{short,long}` seconds per retention tier; `PI_CACHE_RETENTION=long`. | models.md, environment-variables.md | |
| Cache warming | 3 | `cacheWarming` off|streaming(default)|idle (global only). Runs only if model declares lifetime AND expected saving >= $0.05 (`src: core/cache-warmer.ts:20`). Refresh usage recorded as `usage` entries, not in context. Extension `cache_warming_decision` overrides. `/session` shows next decision. `showCacheMissNotices` shows notices. | settings.md, models.md, session-format.md | Provider-specific; defer. |
| Classifier models | 2 | Non-chat models answering typed questions (`choice`, `bool`, `score`) about JSON state with probabilities (TypeSafe Jev across typesafe/openrouter/cloudflare/vercel/opencode; llama.cpp chat models auto-listed as classifiers). Not in `/model`; reached via codemode `models.classify()`, `ctx.modelRegistry.classify()`, or virtual-model routers. | models.md, llama-cpp.md | Niche; defer. |
| Virtual models | 1 | See section 11. | virtual-models.md | |
| Custom provider (extension) | 1 | `pi.registerProvider(name, ProviderConfig | Provider)`; unregister restores built-ins; `refreshModels` with `context.signal`; OAuth provider appears in `/login`; `stream`/`streamSimple` custom streaming contract. | custom-provider.md | Not portable; Ask needs a Go provider interface instead. |
| Stream contract | 2 | One `start`, balanced text/thinking/toolcall events, exactly one terminal `done|error`, abort -> aborted result, honor `onPayload`, `onResponse`, `onProviderStreamEvent`, abort signal, env. Normalize provider overflow message to `context_length_exceeded` (only that); rate limits use normal retry. | custom-provider.md | Good conformance checklist. |
| Provider test checklist | 4 | text/empty, tool calls, image in/out, usage+cost, abort, overflow, malformed streams, Unicode boundaries, cross-provider handoff, auth refresh/cancel. | custom-provider.md | |
| Transport | 2 | `transport` auto|sse|websocket|websocket-cached; `websocketConnectTimeoutMs` 15000. | settings.md | |
| Retry (agent-level) | 2 | `retry.enabled=true, maxRetries=3, baseDelayMs=2000` (exponential 2/4/8s), `maxAgentDelayMs=60000`. Events `auto_retry_start/end`. RPC `set_auto_retry`, `abort_retry`. | settings.md, json.md; src: `core/settings-manager.ts:1001-1003` | |
| Retry (provider-level) | 2 | `retry.provider.timeoutMs` (=`httpIdleTimeoutMs`), `maxRetries=0`, `maxRetryDelayMs=60000` (0 = no limit). Keep 0 so Pi can handle quota/usage-limit errors itself. | settings.md | |
| HTTP | 2 | `httpIdleTimeoutMs=300000` (0 disables), `httpProxy` (agent-dir settings only), env `HTTP_PROXY/HTTPS_PROXY`. | settings.md | |
| Anthropic warning | 4 | `warnings.anthropicExtraUsage` warns when subscription auth may incur extra usage. | settings.md | |
| llama.cpp integration | 1 | Router mode server (no `--model`); `/login llama.cpp` (default `http://127.0.0.1:8080`) or `LLAMA_BASE_URL`/`LLAMA_API_KEY`; `/llama` loads/unloads models, downloads from Hugging Face (`owner/repo[:quant]`), uses `HF_TOKEN` chain, asks before unloading others, never deletes files, Esc cancels load/download, Retry/Close on disconnect. Loaded + sleeping models listed in `/model`; sleeping wake on select. Classification prompt: state twice, questions, single-token label logits normalized; per-request `temperature` divides logits; up to 62 choice labels, 10 score levels; hybrid models need `--ctx-checkpoints 32 --checkpoint-min-step 0`. | llama-cpp.md | Ollama/vLLM etc. covered by `models.json` OpenAI-compat. |

## 8. Tools (built-in), context, prompts

| Feature | Ring | Detail | Source | Go note |
|---|---|---|---|---|
| Default tools | 0 | `read, bash, edit, write`. Others built-in: `powershell` (native Windows only), `grep`, `find`, `ls`. Inactive built-in-extension tools: `codemode`, `tool_search`. | cli.md, settings.md | |
| Tool selection | 2 | `defaultTools`: plain names replace; `+name`/`-name` modify inherited; empty array disables built-ins only (extension/SDK tools stay). Project list with only +/- edits user's; a plain name replaces. In one list: plain names first, then +/- in order. CLI `--tools` overrides (no +/-). | settings.md | |
| `read` | 0 | `path, offset(1-indexed), limit`; text + supported images. Output cap 2000 lines / 50 KB (`src: core/tools/truncate.ts:11-12`). Images resized per `inputLimits`. | cli.md; src: `core/tools/read.ts` | |
| `bash` | 0 | `command, timeout?`(seconds, no default timeout). Output tail-truncated to 2000 lines/50KB for model with full output saved to temp file. Non-interactive `bash -c`. | src: `core/tools/bash.ts`, truncate.ts | |
| `edit` | 0 | `path, edits[{oldText,newText}]` exact text replacement in existing file; multiple targeted edits per call. | cli.md; src: `core/tools/edit.ts` | |
| `write` | 0 | `path, content`; create/overwrite. | src: `core/tools/write.ts` | |
| `grep` | 1 | `pattern, path, glob, ignoreCase, literal, context, limit(100)`; respects .gitignore; lines cut at 500 chars; 50KB cap. | src: `core/tools/grep.ts:41,78` | |
| `find` | 1 | `pattern(glob), path, limit(1000)`; respects .gitignore; 50KB cap. | src: `core/tools/find.ts:41` | |
| `ls` | 1 | `path, limit(500)`; sorted, `/` suffix dirs, includes dotfiles. | src: `core/tools/ls.ts:23` | |
| `powershell` | 1 | `pwsh.exe` else Windows PowerShell; `-NoProfile -NonInteractive -ExecutionPolicy Bypass`; native Windows only; `!`/`!!` still use Bash. | windows.md | Skip in v1. |
| File mutation queue | 3 | `withFileMutationQueue()` serializes read-modify-write per file across parallel tool calls. | extensions.md | Needed in Go for parallel tools. |
| Parallel tool calls | 3 | Tool calls from one assistant message can run in parallel; handlers must not assume sibling ordering. | extensions.md | |
| Tool result contract | 2 | `content` (model-facing) + `details` (render/state) + optional `structuredContent`, `isError`, `usage`, `terminate:true` (skip auto follow-up only if every tool in batch agrees). Throw = failed result. | extensions.md | |
| Nested tool calls | 3 | `ctx.executeTool(name,args)` goes through validation and `tool_call`/`tool_result` hooks; ids `<parent>/<n>`; bounded `nestedCalls` record (args >8 KiB, results >32 KiB omitted; max 256; `complete:false` if lossy); nested usage rolled up into parent. | extensions.md | |
| System prompt | 0 | Base instructions + discovered context files + tool defs + skill descriptions (name, description, path). Replace: `SYSTEM.md` (project beats agent-dir; no merge), `--system-prompt`. Append: `APPEND_SYSTEM.md`, `--append-system-prompt`. | configuration.md, how-pi-works.md | |
| Context files | 1 | `AGENTS.override.md` > `AGENTS.md`/`AGENTS.MD`/`CLAUDE.md`/`CLAUDE.MD` from agent dir, cwd, and ancestors; override replaces only in same dir. Discovery does NOT need project trust. `-nc` disables. | configuration.md, security.md | |
| Prompt templates | 1 | Markdown in `<agent-dir>/prompts/`, `.pi/prompts/`, settings `prompts`, packages. Frontmatter `description`, `argument-hint`. Name = filename. Description fallback = first non-empty line. Conventional dirs: direct `.md` children only. Substitutions: `$1..`, `$@`/`$ARGUMENTS`, `${1:-default}`, `${@:-default}`, `${@:N}`, `${@:N:L}`; shell-like quoting. Expanded before agent input. | prompt-templates.md | Small; port. |
| Skills | 1 | Agent Skills spec. Dir with `SKILL.md`; frontmatter `name`(<=64, lowercase/digits/hyphens, no leading/trailing/double hyphen), `description`(<=1024), `license`, `compatibility`, `metadata`, `allowed-tools`(experimental), `disable-model-invocation`. Only name+description+path in system prompt; model reads file on demand. Locations: `<agent-dir>/skills`, `.pi/skills`, `~/.agents/skills`, `.agents/skills` (cwd through ancestors, stops at repo root), settings, packages. Recursive discovery. Name collision: first wins + warning. Malformed or description-less skills not loaded; name != dir not warned. | skills.md | Portable, very cheap; high value. |
| Input features | 1 | `@` file search, Tab path completion, paste/drag images, `Ctrl+G` external editor (`externalEditor`/`$VISUAL`/`$EDITOR`/Notepad on Windows/nano), prompt history, `Shift+Enter`/`Ctrl+J` newline. Autocomplete `autocompleteMaxVisible` 3-20. | usage.md, keybindings.md | TUI concern. |
| Queued messages | 1 | While running: `Enter` = steer, `Alt+Enter` = follow-up (`Ctrl+Q` Win/WSL), `Alt+Up` = restore to editor (`Alt+Q` Win/WSL), `Esc` = abort. Modes `steeringMode`/`followUpMode` all|one-at-a-time (default one-at-a-time). | usage.md, settings.md | |
| Shell resolution | 2 | Unix: `/bin/bash`, then `bash` on PATH, then `sh`. Windows: `shellPath` > Git Bash > `bash.exe` on PATH. `shellPath` supports `~`. `shellCommandPrefix` joined with newline before each command (built-in bash tool and `!`/`!!`; not extension shell tools). Alias recipe: prefix `shopt -s expand_aliases\nsource ~/.bash_aliases`. | shell-aliases.md, windows.md | Fresh non-interactive process per command; no persistent shell. |
| Shell tool env | 3 | Injected into LLM `bash`/`powershell` (not `!`): `PI_SESSION_ID`, `PI_SESSION_FILE` (unset if ephemeral), `PI_PROVIDER`, `PI_MODEL`, `PI_REASONING_LEVEL`; resolved per command. `createBashTool(..., {exposeSessionEnvironment:false, spawnHook})`. When disabled inherited values stripped so nested Pi does not expose stale metadata. | environment-variables.md | Cheap, useful; copy. |
| Process markers | 2 | CLI and RPC entry set `AI_AGENT=pi` and `PI_CODING_AGENT=true` (not in SDK). | environment-variables.md | |
| Codemode tool | 1 | See section 12. | cli.md | |

## 9. Configuration files, settings (every key with default)

Files. Agent dir `~/.pi/agent` (`PI_CODING_AGENT_DIR` or SDK `agentDir`). Project `.pi/` under cwd (gated by trust). Project settings override agent settings; resource arrays combine; nested objects merge recursively.

| File | Purpose |
|---|---|
| `<agent-dir>/settings.json`, `.pi/settings.json` | Settings |
| `keybindings.json` | Keybinding overrides (agent dir only) |
| `mcp.json`, `.pi/mcp.json` | MCP servers |
| `models.json` | Endpoints/models/overrides |
| `auth.json` | Keys + OAuth |
| `trust.json` | Saved project trust decisions (canonical paths; closest wins) |
| `mcp-auth.json`, `mcp.log` (+`mcp.log.1` at 5 MB) | MCP OAuth tokens, server logs |
| `pi-debug.log` | `/debug` output |
| `SYSTEM.md`, `APPEND_SYSTEM.md`, `AGENTS*.md`/`CLAUDE*.md` | Prompts/instructions |
| `extensions/ skills/ prompts/ themes/` | Resources |
| `sessions/` | Sessions |

Settings keys. Source of type is `settings.md` unless noted; `src:` list `core/settings-manager.ts` interface.

| Key | Type | Default | Notes |
|---|---|---|---|
| `defaultProvider` / `defaultModel` | string | auto | |
| `defaultThinkingLevel` | enum(7) | `medium` | |
| `modelThinkingLevels` | object | none | keyed `provider/modelId` |
| `thinkingBudgets` | object | built-in | minimal/low/medium/high tokens |
| `enabledModels` | string[] | all | same format as `--models` |
| `hideThinkingBlock` | bool | false | |
| `showCacheMissNotices` | bool | false | |
| `cacheWarming` | off/streaming/idle | streaming | global only |
| `steeringMode` / `followUpMode` | all/one-at-a-time | one-at-a-time | |
| `externalEditor` | string | $VISUAL, $EDITOR, platform | |
| `doubleEscapeAction` | tree/fork/none | tree | |
| `treeFilterMode` | 5 values | default | |
| `defaultProjectTrust` | ask/always/never | ask | agent-dir only |
| `defaultTools` | string[] | read,bash,edit,write | +/- syntax |
| `codemode.mode` | on/only | on | |
| `codemode.inlineBudget` | number | 3000 | est tokens (chars/4); 0 = namespaces only |
| `sessionDir` | string | agent sessions | read before trust |
| `compaction.enabled` | bool | true | |
| `compaction.reserveTokens` | int | 16384 | |
| `compaction.keepRecentTokens` | int | 20000 | |
| `compaction.modelOverrides` | object | none | |
| `branchSummary.reserveTokens` | int | 16384 | |
| `branchSummary.skipPrompt` | bool | false | |
| `theme` | string | `system` | `light/dark` pair allowed |
| `quietStartup` | bool | false | |
| `tuiMode` | regular/fullscreen | regular | |
| `fullscreenExitOutput` | transcript/resume-hint | transcript | |
| `fullscreenScrollbar` | auto/always/hidden | auto | |
| `fullscreenCopyOnSelect` | bool | true | |
| `fullscreenWheelScrollLines` | auto or 1-100 | auto | auto: 1 line on local macOS, up to 6 elsewhere/SSH; Alt+wheel x5 |
| `editorPaddingX` | 0-3 | 0 | |
| `outputPad` | 0/1 | 1 | |
| `autocompleteMaxVisible` | 3-20 | 5 | |
| `showHardwareCursor` | bool | false | IME positioning |
| `terminal.showImages` | bool | true | |
| `terminal.imageWidthCells` | int | 60 | |
| `terminal.clearOnShrink` | bool | false | |
| `terminal.showTerminalProgress` | bool | false | OSC 9;4 |
| `terminal.hyperlinks` | bool/auto | auto | OSC 8 |
| `terminal.images` | kitty/iterm2/auto/false | auto | |
| `terminal.trueColor` | bool/auto | auto | |
| `images.autoResize` | bool | true | 2000x2000 |
| `images.blockImages` | bool | false | |
| `markdown.codeBlockIndent` | string | two spaces | |
| `markdown.mermaid` | off/final/streaming | streaming | |
| `transport` | auto/sse/websocket/websocket-cached | auto | |
| `httpProxy` | string | none | agent-dir only |
| `httpIdleTimeoutMs` | ms | 300000 | 0 = off |
| `websocketConnectTimeoutMs` | ms | 15000 | 0 = off |
| `retry.enabled/maxRetries/baseDelayMs/maxAgentDelayMs` | | true/3/2000/60000 | |
| `retry.provider.timeoutMs/maxRetries/maxRetryDelayMs` | | =httpIdle/0/60000 | legacy `retry.maxDelayMs` migrated (src: settings-manager.ts:536) |
| `shellPath` / `shellCommandPrefix` | string | platform/none | |
| `npmCommand` | string[] | `npm` | |
| `packages` | array | [] | string or object with per-type filters, `autoload:false` |
| `extensions/skills/prompts/themes` | string[] | [] | `!glob`, `+path`, `-path` |
| `enableSkillCommands` | bool | true | |
| built-in extension toggles | via `extensions` | on | `-builtin:mcp`, `builtin:llama.cpp`, `builtin:codemode`, `builtin:tool-search`; project overrides user |
| `collapseChangelog` | bool | false | |
| `enableInstallTelemetry` | bool | true | anonymous install/update + provider attribution headers; not update checks |
| `enableAnalytics` | bool | false | first-run experimental setup only |
| `warnings.anthropicExtraUsage` | bool | true | |
| in code only | | | `lastChangelogVersion`, `trackingId`, `deviceId`, `projectTrusted` [doc gap] (src: `core/settings-manager.ts` Settings interface) |

Other config behaviors. `/settings` edits common ones; `/reload` re-reads settings, keybindings, extensions, skills, templates, themes, context files. Settings resource-path bases: user relative to agent dir; project relative to `.pi`; `~` and absolute OK.

## 10. Environment variables

| Var | Ring | Meaning | Source |
|---|---|---|---|
| `PI_CODING_AGENT_DIR` | 2 | Agent dir (default `~/.pi/agent`) | environment-variables.md |
| `PI_CODING_AGENT_SESSION_DIR` | 2 | Sessions dir (below `--session-dir`) | " |
| `PI_PACKAGE_DIR` | 2 | Override package dir (Nix/Guix) | " |
| `PI_OFFLINE` | 2 | No automatic network; 11 use sites in src | " |
| `PI_SKIP_VERSION_CHECK` | 2 | Skip pi.dev latest-version request | " |
| `PI_TELEMETRY` | 2 | Override install telemetry/attribution (`1/true/yes`,`0/false/no`) | " |
| `PI_CACHE_RETENTION` | 2 | `long` = extended cache | " |
| `PI_SHARE_VIEWER_URL`, `PI_RADIUS_GATEWAY` | 2 | Share and bug-report backends | " |
| `PI_HARDWARE_CURSOR`, `PI_HYPERLINKS`, `PI_IMAGE_PROTOCOL`, `PI_TRUE_COLOR` | 2 | Terminal capability overrides; settings beat env | terminal-setup.md |
| `PI_TUI_ESC_TIMEOUT` | 2 | ms to disambiguate lone ESC: 100 over SSH, 10 else | environment-variables.md |
| `PI_TUI_WRITE_LOG` | 2 | Raw ANSI stream capture | tui.md |
| `VISUAL`, `EDITOR`, `HTTP_PROXY`, `HTTPS_PROXY`, `COLORFGBG` | 2 | Standard vars | environment-variables.md, themes.md |
| `AI_AGENT=pi`, `PI_CODING_AGENT=true` | 2 | Markers set by Pi | environment-variables.md |
| `PI_SESSION_ID/FILE`, `PI_PROVIDER`, `PI_MODEL`, `PI_REASONING_LEVEL` | 2 | Set for LLM shell tools | environment-variables.md |
| `LLAMA_BASE_URL`, `LLAMA_API_KEY`, `HF_TOKEN`, `HF_TOKEN_PATH`, `HF_HOME`, `XDG_CACHE_HOME` | 2 | llama.cpp / HF | llama-cpp.md |
| Provider keys | 2 | Section 7 | providers.md |
| In code, not in docs | 4 | `PI_TIMING`, `PI_STARTUP_BENCHMARK`, `PI_CLEAR_ON_SHRINK`, `PI_OAUTH_CALLBACK_HOST`, `PI_TUI_DEBUG`, `PI_TUI_DEBUG_REDRAW`, `PI_MANAGED_INSTALL_ROOT`, `PI_INSTALLER_API_BASE`, `PI_EXPERIMENTAL`, `PI_SERVER_DIR/ID`, `PI_SESSION_WORKER_*` [doc gap] | src grep (`core/`, `tui/`, `ai/`, `cli/`) |

## 11. Virtual models

| Feature | Ring | Detail | Source |
|---|---|---|---|
| Concept | 1 | Selectable model that routes each request to a physical model+thinking level. Appears in `/model`, `--model`, scoped models, settings. Registered only by extension (`pi.registerVirtualModel`) or SDK `modelRuntime.registerVirtualModel`. | virtual-models.md |
| Selection vs dispatch | 3 | Selection recorded as `model_change`/`thinking_level_change`; dispatch recorded per assistant message. Providers only see physical models. Footer shows `auto • high -> gpt-5.6-luna • medium`. | virtual-models.md |
| Definition | 2 | `provider, id, name, thinkingLevels (default ["off"]), contextWindow?, maxTokens?, input?, route(request, ctx)`. `id` must not equal a physical model ID of that provider (a later catalog addition would be hidden). Availability: with provider credentials, or always if provider ID unused. Re-register replaces; `unregisterVirtualModel`. | virtual-models.md |
| Route request | 2 | Fields `model, thinkingLevel, reason, previous, failed, state, messages, signal`; `reason` = `user`, `continuation`, `retry`, `direct`. | virtual-models.md |
| Rules | 4 | Cannot route to another virtual model; routing to a model without credentials or a throw = error response; thinking level clamped; sticky routing keeps prompt cache; retry may switch models. | virtual-models.md |
| Router state | 3 | JSON state stored on session branch as `custom` entry `pi.virtual-model-state {provider,modelId,state}`; follows tree, survives compaction; stored before request even if equal; `direct` requests have no state. | virtual-models.md, session-format.md |

Go note: small, self-contained interface (`Route(req) -> (model, level, state)`); no dependency on JS. Candidate for Ask `providers/` v2.

## 12. Extensions, codemode, tool_search, packages, themes

| Feature | Ring | Detail | Source | Go note |
|---|---|---|---|---|
| Extension runtime | 1 | TS modules via `jiti`; factory `(pi: ExtensionAPI)`; async allowed (startup waits); locations `<agent-dir>/extensions`, `.pi/extensions` (files or dirs with `index.ts/js`), settings, packages, `-e`. Runs in-process with full OS permissions. | extensions.md | Not portable. |
| Lifecycle | 2 | Do not start processes in factory; use `session_start` / `session_shutdown` (idempotent). Reload replaces runtime; old state must not be reused after `ctx.reload()`. Run: input -> `before_agent_start` -> model/message/tool events -> `agent_end` -> `agent_before_settle` (actionable, may request one continuation) -> `agent_settled` (notification-only). | extensions.md | |
| API surface | 2 | `pi.on`, `registerTool`, `registerCommand`, `registerShortcut`, `registerFlag`, `sendUserMessage`, `sendMessage`, `appendEntry`, `setActiveTools/getActiveTools/getAllTools`, model + thinking control, `registerProvider`, `registerMcpServer/unregisterMcpServer/getMcpServers`, `registerVirtualModel`, `registerEntryRenderer`, `pi.events` bus, `setSessionName`. | extensions.md | |
| Events (named in docs) | 2 | `project_trust`, `input`, `before_agent_start` (+`systemPromptOptions`, `forceSystemPrompt`), `context`, `context_with_system`, `message_end` (may replace), `tool_call` (mutate/block), `tool_result` (compose), `turn_end`, `agent_before_settle`, `provider_stream_event`, `cache_warming_decision`, `user_bash`, `session_start/shutdown`, `session_before_switch/fork/compact/tree`, `session_compact_failed`, `mcp_servers_change`. Delta report counts 41 total. | extensions.md, compaction.md, rpc-commands.md | |
| Handler semantics | 4 | Run in load order; `pi.on` returns unsubscribe; handler error reported and continues, except `tool_call` failure blocks the tool (fail-safe); `user_bash` handler failure blocks command; `provider_stream_event` awaited in stream order (slow handlers stall stream). | extensions.md | |
| Tool exposure modes | 2 | `direct`(default) / `model-only` / `codemode` / `deferred` / `hidden`. Registering direct/model-only activates; tools cannot be unregistered (re-register `hidden`). `namespace{name,description,instructions}`. `annotations` (MCP hints). `prepareLoadout(loadout)` hook. | extensions.md | |
| Modes and UI | 3 | `ctx.mode` = tui/rpc/json/print; `ctx.hasUI` true in TUI and RPC. Interactive: full UI. RPC: dialogs + notify/status/widget/title/editor-text only. JSON/print: none. | extensions.md, rpc-extension-ui.md | |
| Session replacement | 4 | `ExtensionCommandContext` has `waitForIdle`, `reload`, tree navigation, session replace (command-only; from lifecycle handlers they can deadlock). Old ctx invalidated; use `withSession`. | extensions.md | |
| Codemode | 1 | Built-in extension, inactive by default. QuickJS sandbox; scripts call `tools.<name>(args)`; helpers `ALL_TOOLS, searchTools(query,{limit,namespace}) (BM25), describeTool, describeNamespace, text, image, console.*, return, exit, store/load (persist as `codemode-store` custom entries, per-branch), models.{getModelsOfType,getAvailableOfType,getModelOfType,classify}` (max 4 classifier calls concurrently). Options line `// @options: {"max_output_tokens":10000 default,"timeout_ms":...}`. Output over max keeps start+end and full text in temp file. Result begins `Script completed|failed`. Bash returns `{output,truncated,full_output_path?,exit_code,wall_time_seconds}` with 1 MiB cap (first/last 512 KiB). Settings `codemode.mode`, `inlineBudget`. | cli.md | Interesting but big (needs a JS sandbox in Go, e.g. goja). Optional. |
| tool_search | 1 | Built-in, inactive; BM25 over undeclared tools; declares matches for next call; recorded in session, stays declared on that branch. | cli.md | |
| Packages | 1 | npm / git / URL / local sources. Versioned npm and git ref/tag/commit are pinned; local loaded in place. `package.json` `pi{extensions,skills,prompts,themes,image,video}` manifest or conventional dirs (`extensions/ skills/ prompts/ themes/`); keyword `pi-package` for gallery. Host-provided packages (`@earendil-works/pi-ai|pi-agent-core|pi-coding-agent|pi-tui`, `typebox`) must be `peerDependencies "*"`, not bundled. Separate module roots per package. Filters: omit = all, `[]` = none, `!glob`, `+path`, `-path`; filters narrow only. Identity: npm by name, git by repo URL sans ref, local by absolute path. Project entry replaces user entry unless `autoload:false` (then a filtering delta). `-e npm:pkg` for one-off. | packages.md | Package mgr = npm/git wrapper; Go equivalent could be git/tarball only. |
| Themes | 1 | Built-in `system` (default; derives from terminal foreground/background/16 ANSI, WCAG 4.5:1 body contrast, re-queries on light/dark switch, waits <=100 ms at startup), `dark`, `light`. Custom JSON: `name`(unique, no `/`, not `system`), `appearance`, `vars`, `colors` (roles: accent, border*, text, muted, dim, success/error/warning, selectedBg, searchMatch*, scrollbar*, userMessage*, customMessage*, thinkingText, tool*Bg/Title/Output, md*, toolDiff*, syntax*, thinking*, bashMode), `export{pageBg,cardBg,infoBg}`. Color forms: hex3/6, `oklch()`, `okhsl()`, 0-255 index, var ref, `""` terminal default. Optional-with-fallback: scrollbarTrack/Thumb, searchMatchBg/Text, thinkingMax. `theme: "light/dark"` auto pair; light/dark detection: reported bg/fg > terminal notification > `COLORFGBG` > dark. Hot reload only for `<agent-dir>/themes/<name>.json`; else `/reload`. | themes.md | TUI-only. Skip except HTML export colors. |
| MCP as extension | 3 | Extension registering `/mcp` (e.g. pi-mcp-adapter) replaces built-in MCP entirely (no `mcp.json` read in session); extension registering `codemode`/`tool_search` replaces those; shell `pi mcp` always built-in. SDK sessions load no built-ins by default. | mcp.md, sdk.md | |

## 13. MCP (built-in)

| Feature | Ring | Detail | Source | Go note |
|---|---|---|---|---|
| Transports | 0 | stdio and streamable HTTP. Legacy SSE rejected. | mcp.md | mark3labs/mcp-go or official SDK. |
| Config files | 1 | `~/.pi/agent/mcp.json`, `.pi/mcp.json` (trust-gated; project entry replaces user of same name). `mcpServers` map in standard client format. | mcp.md | |
| Entry fields | 2 | stdio `command,args,env,cwd`; HTTP `url,headers,oauth`; common `timeout`(s, default 60, progress resets), `enabled`, `exposure`, `toolExposure`, `description`; optional `type` in `stdio|http|streamable-http`; top-level `autoEnableCodemode` (default true; project beats user). `~/` expansion in command/args/cwd; relative `cwd` from session dir. `env`/`headers` support `${VAR}` and whole-value `!command`. | mcp.md | |
| Naming | 2 | Server names `[A-Za-z0-9_-]`; tool names `mcp__<server>__<tool>`. Invalid entries reported and skipped. | mcp.md | |
| Exposure | 2 | `codemode`(default; alias `codemode-deferred`), `deferred`, `direct`, `hidden`; per-tool `toolExposure` (exact name > pattern with `*`, first pattern wins). codemode auto-activated when a codemode server connects; tool_search for deferred. | mcp.md | |
| Result handling | 2 | Text over 20 KB: middle elided with `...N chars truncated...`, full text in temp file; codemode scripts get full `CallToolResult`. `isError` resolves in scripts, is an error for direct calls. | mcp.md | |
| Connection lifecycle | 3 | Connect on session start; first prompt waits up to 10 s; slower servers join later. HTTP network errors and 408/429/5xx retried twice; dropped connection reconnects on next call; `tools/list_changed` handled (withdrawn tools unreachable). stdio stop = close stdin, SIGTERM, SIGKILL to process group. Tool calls are not retried (may have executed). Resource list/read retried once on transient HTTP error. | mcp.md | Process-group kill needed. |
| Resources | 2 | Tools `list_mcp_resources`, `list_mcp_resource_templates`, `read_mcp_resource` (text as text, images as images, other binary saved to temp file). Exposure = widest among servers with resources. MCP Apps (`ui://`, `text/html;profile=mcp-app`) and resource icons omitted. | mcp.md | |
| OAuth | 2 | HTTP servers without `Authorization` header. Dynamic client registration as client name `pi` (`oauth.clientName` override, sent only at registration); `clientId/clientSecret/callbackPort/callbackUrl/scope`; loopback redirect `http://127.0.0.1:<port>/callback`, `callbackUrl` must be HTTP on localhost/127.0.0.1/[::1] (RFC 8252). Tokens in `mcp-auth.json`; refresh on expiry/reject; re-auth on added scope. Login by `/mcp` menu, `/mcp login <s>`, `pi mcp login <s> [--timeout 300]`, paste redirect URL for remote browsers. | mcp.md | |
| Logging | 4 | Server logging notifications appended to `~/.pi/agent/mcp.log`, rotated at 5 MB to `mcp.log.1`. | mcp.md | |
| `/mcp` command | 1 | List servers by state, tool count, exposure, source; reconnect, sign in/out, change exposure, enable/disable (persisted to defining file without rewriting unrelated content). Non-TUI prints status; `/mcp login|logout|reconnect <server>`. | mcp.md | |
| `pi mcp add` | 1 | `pi mcp add <name> [--env K=V ...] [--cwd d] -- cmd args...` or `--url u [--header K=V] [--bearer-token-env-var N] [--oauth-*]`; `--exposure`, `--description`, `-l`. Does not connect. `pi mcp list [--json]` exits 1 if invalid entry or enabled server disconnected. | cli.md, mcp.md | |
| Permissions | 3 | Every MCP call passes the tool pipeline so extension `tool_call`/`tool_result` gates apply; codemode-originated calls carry `parentToolCallId`; annotations (`readOnlyHint,destructiveHint,idempotentHint,openWorldHint`, MCP defaults when missing) exposed for permission extensions. | mcp.md, extensions.md | |
| Migration | 4 | Conversion table for Claude/Cursor (copy), VS Code (`servers`, `${input:}`), Codex (TOML), OpenCode (`local/remote`). | mcp.md | |
| Session-scope registration | 3 | `pi.registerMcpServer` not saved; file config with same name wins (shown in `/mcp`). | extensions.md, mcp.md | |

## 14. RPC protocol (33 commands, verified against `src/modes/rpc/rpc-types.ts:22-74`)

Framing: strict JSONL, LF terminated, strip optional CR; do not use readers that split on U+2028/2029; stdout reserved for protocol; backpressure honored; stderr diagnostics only (rpc.md). Every command has optional string `id`; response `{id?, type:"response", command, success, data?|error}`; malformed JSON returns `{command:"parse", success:false}` without id. A successful `prompt` response only means accepted/queued/handled; wait for `agent_settled`.

| Group | Commands | Notes (source: rpc-commands.md) |
|---|---|---|
| Prompting | `prompt` (images[], `streamingBehavior` steer|followUp required while streaming or error; extension commands run immediately even while streaming; disposition started|queued|handled), `steer`, `follow_up` (no extension commands; templates+skills expanded; disposition queued|handled), `abort` (waits idle), `clear_queue` (returns steering[] followUp[]; do before abort to emulate Esc), `new_session {parentSession?}` (cancellable by `session_before_switch`) | |
| State | `get_state` (model, thinkingLevel, isStreaming, isCompacting, steeringMode, followUpMode, sessionFile, sessionId, sessionName?, autoCompactionEnabled, messageCount, pendingMessageCount), `get_messages` | |
| Model | `set_model{provider,modelId}`, `cycle_model` (null if only one; returns model, thinkingLevel, isScoped), `get_available_models` | Model object: id, name, api, provider, baseUrl, reasoning, input[], contextWindow, maxTokens, cost per M |
| Thinking | `set_thinking_level`, `cycle_thinking_level` (null if unsupported), `get_available_thinking_levels` (`["off"]` if none) | |
| Queue modes | `set_steering_mode`, `set_follow_up_mode` (all|one-at-a-time) | |
| Compaction | `compact{customInstructions?}` (returns summary, firstKeptEntryId, tokensBefore, estimatedTokensAfter, usage, details), `set_auto_compaction` | |
| Retry | `set_auto_retry`, `abort_retry` | |
| Bash | `bash{command, excludeFromContext?}` (streams `bash_execution_update{id,delta}`; result output/exitCode/cancelled/truncated/fullOutputPath; output reaches model on NEXT prompt as user text "Ran `cmd`" + fenced output), `abort_bash` | |
| Session | `get_session_stats` (counts, tokens, cost, contextUsage{tokens,contextWindow,percent}), `export_html{outputPath?}`, `switch_session{sessionPath}`, `fork{entryId}` (returns text), `clone`, `get_fork_messages`, `get_entries{since?}` (append-order cursor, includes pre-compaction and abandoned branches, returns `leafId`; unknown `since` = failure), `get_tree` (nodes `{entry,children,label?,labelTimestamp?}`, multiple roots, orphans as roots), `get_last_assistant_text` (null if none), `set_session_name` | |
| Discovery | `get_commands` (extension/prompt/skill; `sourceInfo{path,source local|auto|cli,scope user|project|temporary,origin top-level|package,baseDir?}`; excludes TUI built-ins) | |

Gaps in RPC vs interactive (from docs): no command for `/tree` navigation, labels, `/import`, `/share`, model scoped list, `/login`, settings changes (prior report 9.C lists engine capabilities without RPC). Ask should expose these via gRPC (BB needs).

RPC extension-UI subprotocol (rpc-extension-ui.md): dialogs `select, confirm, input, editor` (request `extension_ui_request{id,method,...,timeout?}`; response `extension_ui_response{id, value|confirmed|cancelled}`; agent auto-resolves on timeout: `undefined`/`false`); fire-and-forget `notify(notifyType info|warning|error)`, `setStatus{statusKey,statusText}`, `setWidget{widgetKey,widgetLines,widgetPlacement aboveEditor|belowEditor}` (string arrays only), `setTitle`, `set_editor_text`. Degraded/no-op in RPC: `custom()` returns undefined, `onTerminalInput` no-op, working-indicator/header/footer/autocomplete/editor-component setters no-op, `getEditorText()` = "", `getTheme()` undefined, `setTheme()` returns `{success:false,error:"Theme switching not supported in RPC mode"}`; `ctx.mode="rpc"`, `ctx.hasUI=true`.

## 15. Event stream (json.md; identical shapes in RPC)

| Event | Fields | Notes |
|---|---|---|
| `session` header (JSON mode only) | version 3, id, timestamp, cwd | |
| `agent_start`, `agent_end{messages,willRetry}`, `agent_settled` | | `agent_end` = one low-level run; settled = no automatic work remains |
| `turn_start`, `turn_end{message,toolResults}` | | turn = one assistant response + its tool results |
| `message_start`, `message_update{usage, assistantMessageEvent}`, `message_end{message}` | | Delta-only on wire: `partial`/cumulative message removed. Sub-events: `start,text_start/delta/end,thinking_start/delta/end,toolcall_start{id,toolName}/delta/end{toolCall},done,error`. `message_end` authoritative. Normal loop maps provider start/done/error to message_start/end. |
| `tool_execution_start/update/end` | toolCallId, toolName, args, partialResult/result, isError | Nested calls carry `parentToolCallId` and id `<parent>/<n>` |
| `queue_update{steering,followUp}` | full queues | |
| `entry_appended{entry}`, `session_info_changed{name}`, `thinking_level_changed{level}` | | |
| `compaction_start/end`, `auto_retry_start/end`, `summarization_retry_*` | | Section 6/7 |
| RPC only | `bash_execution_update{id?,delta}`, `extension_error{extensionPath,event,error}` | |

## 16. Interactive UI and keybindings

Keybinding config: `<agent-dir>/keybindings.json`, map action id -> key or list; empty list disables; `/reload` applies; syntax `modifier+key` with `ctrl,shift,alt,super` (super needs Kitty protocol); keys a-z, 0-9, named keys, f1-f12, symbols. Defaults (action id : key):

| Area | Actions (default) |
|---|---|
| Cursor | up/down history at edges; left/`ctrl+b`; right/`ctrl+f`; word `alt+left|ctrl+left|alt+b`, `alt+right|ctrl+right|alt+f`; line start `home|ctrl+home|ctrl+a`; end `end|ctrl+end|ctrl+e`; jump char `ctrl+]` / `ctrl+alt+]`; page `pageUp|ctrl+pageUp`; `tui.editor.historyPrevious/Next` unbound (take precedence over app actions) |
| Edit | backspace; delete `delete|ctrl+d`; word back `ctrl+w|alt+backspace`; word fwd `alt+d|alt+delete`; to line start `ctrl+u`; to end `ctrl+k`; yank `ctrl+y`, yank-pop `alt+y`; undo `ctrl+-` (`ctrl+z` Windows, `alt+z` WSL) |
| Input/select | newline `shift+enter|ctrl+j`; submit `enter`; tab; copy `ctrl+c`; select up/down/pageUp/pageDown/confirm `enter`/cancel `escape|ctrl+c` |
| Fullscreen | `tui.altScreen.*`: pageUp/pageDown, halfPage/line (unbound), previous/nextPrompt `ctrl+shift+up/down` (+`ctrl+up/down`; only ctrl on Win/WSL), search `ctrl+shift+f` (`ctrl+f` Win/WSL), searchNext `enter|ctrl+g`, searchPrevious `shift+enter|ctrl+shift+g`, close `escape`, top `home`, bottom `end` (follow output) |
| App | interrupt `escape`; clear `ctrl+c` (1st clears, 2nd exits); exit `ctrl+d` (empty editor); suspend `ctrl+z` (none on native Windows; status message if bound); external editor `ctrl+g`; paste image/files/text `ctrl+v` (`alt+v` Win/WSL) |
| Sessions | new/tree/fork/resume unbound; picker `ctrl+p` path, `ctrl+s` sort, `ctrl+n` named, `ctrl+r` rename, `ctrl+d` delete, `ctrl+backspace` delete on empty query |
| Model/thinking | select `ctrl+l`; cycle `ctrl+p`, back `shift+ctrl+p` (`alt+p` Win/WSL); save model `ctrl+s`; thinking cycle `shift+tab`, save `ctrl+s`, toggle block `ctrl+t` |
| Display/queue | tools expand `ctrl+o`; copy `ctrl+x` (selected message in /tree; last assistant); follow-up `alt+enter` (`ctrl+q` Win/WSL); dequeue `alt+up` (`alt+q` Win/WSL) |
| Tree | fold `ctrl+left|alt+left`, unfold `ctrl+right|alt+right`, label `shift+l`, label time `shift+t`, filters default `ctrl+d`, no-tools `ctrl+t`, user-only `ctrl+u`, labeled `ctrl+l`, all `ctrl+a`, cycle `ctrl+o` / `shift+ctrl+o` |
| Scoped models | enableAll `ctrl+a`, clearAll `ctrl+x`, toggleProvider `ctrl+p`, reorder `alt+up/down` |

All from keybindings.md. Go note: TUI client only; server needs no keybinding knowledge.

UI facts (usage.md, tui.md): transcript + editor + footer (folder, session, model, context usage, accumulated usage/cost); startup header lists loaded resources; editor border color shows thinking level; `Ctrl+O` tool output expand; `Ctrl+T` thinking blocks; Mermaid rendering (`markdown.mermaid`); inline images (kitty/iTerm2); OSC 8 hyperlinks; OSC 9;4 progress; fullscreen mouse (wheel scroll, drag selection, copy on select, OSC 8 click precedence); extension UI components (`Text, Markdown, Image, TruncatedText, Container, VStack, HStack, Box, Spacer, Input, Editor, SelectList, SettingsList, ScrollView, Loader, CancellableLoader, MouseRegion`), overlays, `CURSOR_MARKER` for IME, `CustomEditor`.

## 17. Terminal, tmux, Windows, Termux, shell notes

| Topic | Ring | Detail | Source |
|---|---|---|---|
| Extended keys | 2 | Pi uses Kitty keyboard protocol or xterm modifyOtherKeys to tell `Shift+Enter`/`Alt+Enter` from Enter. Proxies/multiplexers/IDE terminals may drop it. | terminal-setup.md |
| tmux | 4 | 3.5+: `set -g extended-keys on` + `extended-keys-format csi-u`; 3.2-3.4: `extended-keys on` (modifyOtherKeys); older: upgrade. Requires new server. Verify Shift+Enter newline, Enter submit, Alt+Enter follow-up. | tmux.md |
| Terminal quirks | 4 | Kitty fine; iTerm2 fullscreen scroll slow (Advanced > "Trackpad scrolls fast?" = No); Apple Terminal Shift+Enter fallback only when Pi runs locally; Ghostty `alt+backspace=text:\x1b\x7f`, remove `shift+enter=text:\n`; WezTerm optional `enable_kitty_keyboard`, Option+Enter -> `\x1b[13;3u`; Alacritty Option+Enter mapping; VS Code >=1.109.5 OK else sendSequence `\u001b[13;2u`; Zed keymap entries; Windows Terminal sendInput `\u001b[13;2u` for shift+enter, Alt+Enter is fullscreen so Pi uses Ctrl+Q; xfce4-terminal/Terminator/IntelliJ cannot distinguish modified Enter (use `Ctrl+J`). | terminal-setup.md |
| Capability overrides | 2 | Hyperlinks/images/truecolor: env vs setting; setting wins; `auto` keeps detection. | terminal-setup.md |
| IME | 4 | `PI_HARDWARE_CURSOR=1` / `showHardwareCursor` for WezTerm-on-WSL and IntelliJ. | terminal-setup.md |
| Windows | 1 | Native (Git Bash default; optional `powershell` tool via `defaultTools` `["-bash","+powershell"]`) or WSL. Windows/WSL default key differences (undo `ctrl+z`/`alt+z`, follow-up `ctrl+q`, dequeue `alt+q`, cycle-back `alt+p`, paste `alt+v`, search `ctrl+f`). JSON path backslashes must be doubled. | windows.md, keybindings.md |
| Termux | 1 | Android; text + file tools + shell; clipboard via `termux-clipboard-set/get` (needs Termux:API app + `termux-api` pkg); text only, no image paste; shared storage needs `termux-setup-storage`; Pi detects Termux; recommended `~/.pi/agent/AGENTS.md` snippet; avoid Play Store build. | termux.md |

## 18. Security and isolation

| ID | Feature | Detail | Source | Go note |
|---|---|---|---|---|
| S1 | No approval prompts | Pi does not ask before tool calls; tools run with OS permissions of the Pi process; extensions in-process. | security.md, usage.md | Decision for Ask: permissions package is scaffold in AskCore. |
| S2 | Threat stance | Model output, files, comments, instructions are untrusted (prompt injection); transcript watching, trust, and diff review are not a security boundary. Sandboxing = user's responsibility. | security.md | |
| S3 | Project trust scope | Gates: `.pi/settings.json`, `.pi/mcp.json`, `.pi/{extensions,skills,prompts,themes}`, `.pi/SYSTEM.md`, `.pi/APPEND_SYSTEM.md`, project `.agents/skills` (cwd and ancestors). Bare `.pi` dir needs no decision. Granting loads project settings, MCP, resources, missing packages, project-local extensions. | security.md | |
| S4 | Context files not gated | AGENTS/CLAUDE files always load (unless `-nc`). | security.md | |
| S5 | Decision order | 1. `--approve/--no-approve`; 2. `project_trust` event from user-level/CLI extensions (first yes/no wins); 3. saved decision in `trust.json` for cwd or nearest parent; 4. `defaultProjectTrust` (`ask|always|never`). `/trust` saves. Non-interactive modes: `always` loads, `ask`/`never` skip. | security.md | |
| S6 | `sessionDir` leak | Read before trust. | security.md | |
| S7 | Isolation methods | Plain Docker (whole process; `-e KEY`, bind mount cwd to /workspace, named volume for `/root/.pi/agent`, image `node:24-bookworm-slim` + bash git ripgrep); Docker Sandboxes `sbx run --kit docker.io/sbx/pi-kit:latest pi` (proxy substitutes placeholder credentials, never `/login` inside, custom secret for `ANTHROPIC_OAUTH_TOKEN`); NVIDIA OpenShell (`openshell sandbox create --name pi-sandbox --from pi -- pi`, remote needs upload/download, inference routing); Gondolin micro-VM extension (host Pi, tools routed into QEMU VM, cwd mounted at `/workspace`; overrides read/write/edit/bash/grep/find/ls; env vars leak into VM; Node >=23.6). | containerization.md | Ask as daemon in container fits "Entirely inside" model; a `sandbox/` backend for tools maps to Gondolin. |
| S8 | Exposure checklist | RW mounts, mounting `~/.pi/agent`, env vars, network, tool-only isolation leaves extensions on host. | containerization.md | |
| S9 | Session hygiene | Exported/shared sessions and `/debug` log can contain credentials/file contents. Vuln reports via SECURITY.md; prompt-injection/no-sandbox/user extensions out of scope. | security.md, sessions.md | |

## 19. Stated limitations and edge cases (ring 4 index)

1. Tool output cap 2000 lines / 50 KB to model (bash full text in temp file); MCP text >20 KB elided; codemode `bash` 1 MiB; serialization for summaries 2000 chars per tool result. (cli.md, mcp.md, compaction.md)
2. Catalog `inputLimits.maxRequestBytes`/`images.maxPerMessage`/`maxPerRequest` are NOT enforced. (models.md)
3. Images encoded once; model switch does not re-encode history. (models.md)
4. Prompt with `streamingBehavior` missing while streaming is an error (RPC and SDK `prompt()`). (rpc-commands.md, sdk.md)
5. RPC `bash` output enters context only at next prompt. (rpc-commands.md)
6. `agent_end` != done; use `agent_settled`. JSON mode exit code ignores failed assistant response. (json.md, cli-integration.md)
7. `contextUsage` null right after compaction. (rpc-commands.md)
8. Session `pending` messages never persisted. (message-types.md)
9. Skills: `allowed-tools` is experimental; name-vs-dir mismatch not warned. (skills.md)
10. Project trust cannot restrict tool calls; `sessionDir` read pre-trust. (security.md)
11. MCP: no SSE; MCP Apps unsupported; tool calls never retried; only text-only Termux clipboard. (mcp.md, termux.md)
12. `system` theme name reserved; theme names cannot contain `/`. (themes.md)
13. Custom Windows `shellPath` must have doubled backslashes; `powershell` tool native Windows only; `app.suspend` unavailable on native Windows. (windows.md, keybindings.md)
14. Provider retry >0 can delay Pi's own quota handling. (settings.md)
15. Virtual model cannot route to virtual model; `id` shadows future physical model. (virtual-models.md)
16. `--tools` cannot use +/-; `--fork` and `--session-id` combination limits. (cli.md)
17. Nested-call record limits (8 KiB args, 32 KiB results, 256 calls). (extensions.md)
18. RPC rejects `@file`; extension UI degraded in RPC. (rpc.md, rpc-extension-ui.md)
19. Local packages not installed/modified (dependency tree is author's responsibility); duplicates of host packages in `dependencies` produce warning. (packages.md)
20. Cache warming only above $0.05 expected saving and when model declares lifetime. (settings.md)

## 20. Suggested Ask priorities from this lane (ranked)

| Rank | Item | Why |
|---|---|---|
| 1 | Session tree + entry types + context rebuild + compaction (sections 5, 6) | Core value, language-neutral, exact algorithms documented |
| 2 | Event stream + 33 RPC command semantics mapped to gRPC/WS (sections 14, 15) | Already Ask's gateway; reuse `prompt/steer/follow_up/abort/agent_settled` semantics |
| 3 | 7 built-in tools with the caps above, file mutation queue, PI_* shell env | Small, fully specified |
| 4 | Skills + prompt templates + context files + SYSTEM/APPEND_SYSTEM | Cheap, portable, no code execution |
| 5 | Model registry: `models.json`, credential precedence, `!cmd`/`$VAR` resolver, thinking levels, retry | Needed for providers package |
| 6 | MCP client (stdio+HTTP, OAuth, exposure) | Only language-neutral extension route |
| 7 | Project trust decision chain | Cheap if designed in from day one |
| 8 | Virtual-model router interface, codemode/tool_search | Optional; large or niche |
| Skip | Themes, terminal quirks, keybindings, llama.cpp UI, share/bug/Radius, packages via npm, jiti extensions | TUI/JS-specific |

## Coverage checklist (39/39 doc files + README + docs.json read)

cli-integration.md [x], cli.md [x], compaction.md [x], configuration.md [x], containerization.md [x], custom-provider.md [x], environment-variables.md [x], extensions.md [x], how-pi-works.md [x], index.md [x], json.md [x], keybindings.md [x], llama-cpp.md [x], mcp.md [x], message-types.md [x], models.md [x], packages.md [x], prompt-templates.md [x], providers.md [x], quickstart.md [x], rpc-commands.md [x], rpc-extension-ui.md [x], rpc.md [x], sdk.md [x], security.md [x], session-format.md [x], sessions.md [x], settings.md [x], shell-aliases.md [x], skills.md [x], slash-commands.md [x], terminal-setup.md [x], termux.md [x], themes.md [x], tmux.md [x], tui.md [x], usage.md [x], virtual-models.md [x], windows.md [x]; plus README.md [x], docs.json [x] (nav plus redirects `development.md->index.md`, `session.md->session-format.md`, `tree.md->sessions.md`, `process-integration.md->cli-integration.md`).

Spiral passes: ring 0-2 in first pass over all files; ring 3 (interactions) second pass; third pass over source cross-checks (slash list, CLI args, env grep, settings interface, tool schemas, RPC types) found only the doc gaps marked above; a further pass found nothing new.

## Unresolved questions

1. Session format version: docs say v3; prior delta report claims a v4 in the newer harness (`pi-agent-core`). Which does Ask target? Not verified in this lane (docs only).
2. Are `/mcp` and `/debug` intended to be public (missing from slash-commands.md)? Affects whether Ask exposes them.
3. Exact list of provider-attribution headers sent when `enableInstallTelemetry` is true (docs vague). Ask should default to none.
4. Precise system-prompt text and section names (`preamble`, `tools`, `cwd`, `skills`, ...) are not documented beyond examples; need `src/core/system-prompt.ts` reading (other lane).
5. How `thinkingBudgets` map per provider is not in docs.
6. Whether Ask wants Pi's "no approval" stance or a permission layer (AskCore has `permissions/` and `sandbox/` scaffolds); docs give no design guidance beyond isolation.

Status: DONE
Summary: All 39 doc files, README and docs.json were read and cross-checked against source (slash commands, CLI args, settings interface, env vars, tool schemas, RPC types); report has feature tables by area with settings defaults, flags, env vars, keybindings, RPC, compaction, MCP, security and ranked Go priorities.
Concerns: Report is docs-centric; system prompt text, provider wire behavior, and session v4 were not read in this lane.
