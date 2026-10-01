# Pi extension side: merged feature inventory for the Go rewrite (Ask)

Date 2026-09-30. Pi commit 2bbfcca4 (0.99.1). Synthesis part S2. Sources: the eight lane reports in `plans/reports/` (extensions, docs-inventory, changelog-early, changelog-late, edge-cases, package-triage, go-agent-libraries, go-tui-bubbletea). The 41 event names were re-checked against `packages/coding-agent/src/core/extensions/types.ts:1545-1612` (all 41 present). Everything else is taken from the lane reports and was not re-executed.

Path shorthand: `X/` = `packages/coding-agent/src/core/extensions/`; `S/` = `packages/coding-agent/src/core/`; `D/` = `packages/coding-agent/docs/`; `E/` = `packages/coding-agent/examples/extensions/`; `CL` = `packages/coding-agent/CHANGELOG.md`. "Pi status" values: current, experimental (not in published build), removed.

Ask fixed decisions applied: Charm TUI stack; `src/experimental` is marked "experimental"; mcp and codemode are marked "very new" (first release 0.99.0, 2026-09-29).

## 0. Outcome first

1. Pi has one extension mechanism: a TypeScript module `(pi: ExtensionAPI) => void`, loaded in-process by jiti, no sandbox, no timeouts, no API version field. One API object carries 41 events, 10 `register*` methods, 10 `ctx.ui` groups and 5 tool exposure levels (D/extensions.md:5, X/types.ts:1545-1612).
2. 19 of the 41 events need a synchronous answer before the harness continues (block, modify, replace, or supply data). 22 are notifications. This split is the key input for the runtime choice (section 9). Two of the 19 (`tool_call`, `user_bash`) are fail-closed: a handler crash blocks the action.
3. Recommendation (needs user decision): a subprocess JSON-RPC protocol over stdio as the main extension tier, plus shell hooks as a cheap tier, plus compiled-in Go hooks for first-party features. Details and trade-offs in section 9.
4. About one-third of the Pi API is TUI-host API (component factories, renderers, editor replacement). It cannot cross a process boundary. Pi's own RPC mode already defines the serializable subset (section 7). That subset is the wire contract to copy.
5. API churn is the main lesson: hooks were rebuilt three times in four weeks (0.18 to 0.35), and there has been a breaking change every 2 to 4 minor releases since (section 10).

## 1. Discovery, loading, ordering, reload, isolation (area LD)

| id | feature / API | detail | Pi status | since | source | edge cases | tier | Go note |
|---|---|---|---|---|---|---|---|---|
| E-LD-01 | Directory scan | One level: `*.ts/js` files, `sub/index.ts|js`, `sub/package.json` with `pi.extensions`. No deeper recursion. | current | 0.24.0 (index.ts), unified 0.35.0 | X/loader.ts:737-811; CL 0.24.0, 0.35.0 | Single-file custom tools were dropped in 0.24.0 in favor of directory entry | P0 MVP: any extension needs a discovery rule | Scan `<config>/extensions/*` for a manifest file (`extension.json`) instead of guessing entry files. |
| E-LD-02 | Auto dirs | `<agentDir>/extensions/` (user) and `<cwd>/.pi/extensions/` (project, trust-gated). | current | 0.35.0 | D/configuration.md:21,34 | Project dir needs trust before load | P0 MVP | Use Ask config dir + `<workspace>/.ask/extensions`. Same trust gate. |
| E-LD-03 | Settings lists | `extensions: string[]` with `!pattern`, `+path`, `-path` overrides and `builtin:<name>` entries. | current | 0.35.0; `builtin:` 0.99.0 | D/settings.md:150-158 | Filters narrow, never widen | P1: enable/disable list is needed, the glob override syntax is not | One `enabled`/`disabled` list per scope is enough. |
| E-LD-04 | CLI temp load | `-e/--extension <path|npm:|git:>`; `--no-extensions` disables settings and auto (and built-ins). | current | -e 0.35.0; --no-extensions 0.38.0 | D/packages.md:30; CL 0.38.0, 0.99.0 | `--no-extensions` also disables built-ins since 0.99.0 | P1 | Daemon flag `--extension` for tests and dev loops. |
| E-LD-05 | Load order and precedence | CLI first, then rank: project local settings 0, project auto-dir 1, user settings 2, user auto-dir 3, package 4, builtin 5; inline SDK factories last; duplicates by canonical path dropped. Handler order = load order, then registration order. | current | project-first 0.55.0 | S/package-manager.ts:192-198, 2650-2656; S/resource-loader.ts:763-768; X/runner.ts:268 | Order is load-bearing and inconsistent per feature (tool first-wins, shortcut last-wins, command suffixes) | P0 MVP: fix ONE rule for all registries | Choose one rule (recommend: first-registered wins, conflict is an error listed in diagnostics). Document it. |
| E-LD-06 | Runtime loader | jiti, `moduleCache:false`; module must export a function; host packages shared via virtual modules. | current | 0.35.0 | X/loader.ts:547-580; X/virtual-modules.ts:1-38 | Bun binaries, Windows alias paths caused many fixes (0.29.1 to 0.56.1) | skip: TS-runtime specific | Replaced by the runtime decision (section 9). |
| E-LD-07 | Async factory | Factory awaited before startup continues. | current | 0.38.0 | X/loader.ts:601-620; D/extensions.md:47 | Do not start processes in factory; start in `session_start`, stop idempotently in `session_shutdown` | P0 MVP | Model as `Initialize` RPC with a deadline, then `session_start`. |
| E-LD-08 | Load failure isolation | Failed factory recorded in `errors[]`; others continue. Registration validation: tool needs object `parameters`, command needs name and handler, flag default type must match. | current | 0.55.2 (leftover subs), Unreleased (validation) | X/loader.ts:288-345,622-650; CL 0.55.2 | Failed factory used to leave subscriptions and providers registered | P0 MVP | Validate the registration payload in the host; register atomically at the end of `Initialize`. |
| E-LD-09 | Two-phase API state machine | `loading` (registration works, action methods throw), `commit()` applies pending flags/providers/MCP/virtual models, `discard()` on throw; `bindCore` wires real actions and flushes queued registrations. | current | 0.55.2+ | X/loader.ts:156-262; X/runner.ts:409-543 | After bind, provider register is immediate (no reload) since 0.55.2 | P0 MVP: host must enforce, not rely on discipline | Host state per extension: `loading -> active | failed`. Reject action calls in `loading`. |
| E-LD-10 | Isolation | None. In-process, same OS rights, no sandbox, no timeouts, no memory limit. Project trust is the only gate. | current | n/a | D/extensions.md:5; D/security.md:3-17 | A slow `message_update` handler stalls the stream | P0 MVP: Ask must do better (deadline + crash policy) | Process boundary gives crash isolation for free. Add per-call deadlines (hooks were given no timeout in Pi; `hookTimeout` was removed in 0.31.0). |
| E-LD-11 | Reload | `/reload` or `ctx.reload()` (command ctx only). No file watcher. Sequence: `session_shutdown{reload}`, invalidate old runner, reload settings and resources, new runner and model registry, keep CLI flag values, `session_start{reload}`, `resources_discover{reload}`. | current | /reload 0.50.0; ctx.reload 0.52.x | S/agent-session.ts:3569-3600; D/extensions.md:37 | Active tools preserved; event-bus listeners survived reload before a fix (0.58.1) | P0 MVP | Restart the child process. Generation counter per runtime. |
| E-LD-12 | Stale-object invalidation | After reload or session replacement (`newSession`, `fork`, `switchSession`) captured `pi`/`ctx` throw a guidance error. `withSession(ctx)` hands out the fresh context. | current | 0.69.0 | X/loader.ts:165-181; X/runner.ts:721-733; CL 0.69.0 | Calling command-only methods from event handlers can deadlock | P0 MVP | Host stamps every request with a runtime generation; stale generation returns an error code. |
| E-LD-13 | Tools cannot be unregistered | Withdraw by re-registering with `exposure:"hidden"`. | current | 0.99.0 | D/extensions.md | none found | P1 | Provide `tool/unregister` directly; simpler than Pi's workaround. |
| E-LD-14 | Built-in extensions | `builtin:<name>` (mcp, llama.cpp, codemode, tool-search). `replaceable`: a user extension that registers the same tool/command/flag replaces the built-in entirely, with a warning. SDK sessions do not load built-ins. | current, very new | 0.99.0 | S/resource-loader.ts:120-160; X/types.ts:1992-2020 | Any name overlap drops the built-in wholesale | P2: Ask has compiled first-party packages already | Use fx groups for first-party; allow a config switch to disable one. Skip "replace on name clash". |
| E-LD-15 | Project trust (staged loader) | Pass 1 loads only user and CLI extensions and asks `project_trust`; pass 2 loads project resources. Decision order: extension handler, saved `trust.json`, `defaultProjectTrust` (ask/always/never). Non-interactive modes cannot prompt. Built-ins load after trust. | current | 0.79.0 (default 0.79.1) | D/security.md:25-80; S/project-trust.ts:57; S/resource-loader.ts:497-515 | Context files (AGENTS.md) are not gated but still untrusted input | P0 MVP: the daemon runs in arbitrary workspaces | Two-stage loader; store decisions in SQLite. |
| E-LD-16 | Extension-load side effects | Some invocations (`pi mcp`, `pi list`) load no extensions; factories must be side-effect free. | current | 0.99.0 | D/extensions.md:47 | none found | P2 | Not needed if load is an explicit `Initialize` call. |
| E-LD-17 | Chord facet plugin runtime | Second system: plugins split into facets (`session` worker, `tui`), typed services, replicated state, strict-JSON remote boundary. `PI_EXPERIMENTAL=1`. Host services: AgentController, PresentationUI, SlashCommands. | experimental | 2026-08-28 (chord); plugin.ts later | packages/chord/README.md:1-50; src/experimental/plugin.ts:1-60 | "Not a stable public API contract yet" (chord PLANNING.md); protocol wire v8 in two months | skip: experimental, not published | Read only as precedent for a split host/UI extension. Ask already has gateway + `cmd/tui`. |

## 2. The API object handed to an extension (area API)

| id | feature / API | detail | Pi status | since | source | edge cases | tier | Go note |
|---|---|---|---|---|---|---|---|---|
| E-API-01 | `pi.on(event, handler)` | Returns unsubscribe. 41 overloads. Snapshot of handlers per dispatch, so unsubscribe does not affect an in-flight dispatch. | current | unsubscribe return 0.86.0 | X/types.ts:1545-1612; X/runner.ts:268 | A handler removing itself mid-dispatch used to skip the next listener (fixed 0.99.0) | P0 MVP | RPC `subscribe{events[]}` per extension; host keeps the snapshot rule. |
| E-API-02 | `registerTool` | See section 3. Later calls trigger `refreshTools()`. Same name in same extension overwrites. | current | runtime registration 0.55.4 | X/loader.ts:288 | Tool needs object schema | P0 MVP | `tool/register` at init and at runtime. |
| E-API-03 | `registerCommand` | `(name, {description, getArgumentCompletions, handler})`. Dynamic. Duplicates both kept with suffix `name:1`, `name:2`. | current | 0.31.0 | X/runner.ts:798-838 | Extension commands run before `input` handlers and even while streaming | P0 MVP | Reject duplicates instead of suffixing (see E-LD-05). |
| E-API-04 | `registerShortcut` | `(KeyId, {description, handler(ctx)})`. Restricted built-ins block the shortcut; non-restricted are overridden with a warning; two extensions: last wins. | current | 0.34.0 | X/runner.ts:672-720 | Warning-only conflict handling | P2: client-side concern | Shortcut list is data sent to `cmd/tui`; TUI owns dispatch and calls back. |
| E-API-05 | `registerFlag` / `getFlag` | `{type: boolean|string, default, description}`; values from CLI; first registration wins; values survive reload; `getFlag` returns only the caller's flags. | current | 0.34.0 | X/loader.ts:330-360; S/resource-loader.ts:1238-1273 | Flag conflict is an error entry | P1 | Config keys under `extensions.<id>.*` are simpler than CLI flags for a daemon. |
| E-API-06 | `registerMessageRenderer` / `registerEntryRenderer` / `registerMarkdownTransformer` | Renderers return TUI components; first registrant wins; markdown transformer is display-only, one per extension. | current | renderer 0.31.0; entry renderer 0.80.4; transformer 0.84.0 | X/runner.ts:774-788 | Live TUI objects | P2 (see section 7) | Declarative: extension returns styled text/markdown blocks; the client renders. |
| E-API-07 | `sendMessage` | `(msg{customType,content,display,details}, {triggerTurn, deliverAs: steer|followUp|nextTurn})`. Rules: nextTurn queues; streaming steers/follow-ups; streaming + `triggerTurn:false` held until end of turn so it cannot land between a tool call and its result. | current | 0.32.0 | S/agent-session.ts:2208-2250 | Deferred if inside `agent_settled` | P0 MVP | Single host queue with the same four rules. |
| E-API-08 | `sendUserMessage` | Always triggers a turn; goes through `input` with `source:"extension"`; templates and commands not expanded by default. | current | 0.37.3 | S/agent-session.ts:2280 | Extension commands cannot be queued via steer/follow_up | P0 MVP | Same. |
| E-API-09 | `appendEntry` | Persist a `custom` session entry, not sent to the LLM; emits `entry_appended`. | current | 0.31.0 | S/agent-session.ts:3315 | Needs an entry taxonomy in the store | P0 MVP | Table `session_entries(kind='custom', ext_id, type, data)`. |
| E-API-10 | Session name and labels | `setSessionName/getSessionName` (emits `session_info_changed`), `setLabel(entryId,label)`. | current | setLabel 0.49.0 | S/agent-session.ts:3846 | none found | P1 | Simple RPCs. |
| E-API-11 | `exec` | Plain process exec `(cmd,args,opts)`; works during load. | current | 0.31.0 (was `ctx.exec`) | X/loader.ts | Not routed through the tool pipeline or permissions | P1 | Out-of-process extension can exec itself; offer host `exec` only if sandbox policy must apply. |
| E-API-12 | Tool set control | `getActiveTools/setActiveTools/getAllTools`; unknown or hidden names ignored; changes are transcript deltas; providers that cannot express a delta get a full checkpoint (cache prefix may break). | current | 0.34.0; objects 0.44.0 | D/extensions.md; X/types.ts:2067 | Cache prefix break | P1 | Needed for plan-mode style extensions. |
| E-API-13 | `getCommands` | Extension + prompt + `skill:` commands with `sourceInfo`. | current | 0.51.1 | S/agent-session.ts:3260 | `sourceInfo` replaced ad hoc path fields in 0.62.0 | P1 | Reuse one `SourceInfo` struct from day one. |
| E-API-14 | `getSettings` | Copy of merged settings. No per-extension settings namespace. | current | n/a | D/settings.md | Extensions keep their own files | P1 | Give each extension a settings namespace and a private data dir (Pi gap). |
| E-API-15 | Model and thinking control | `setModel` (false if no auth; session-scoped), `getThinkingLevel/setThinkingLevel` (clamped). | current | 0.37.5 | X/types.ts | none found | P1 | RPC. |
| E-API-16 | `pi.events` bus | Node EventEmitter `emit(channel,data)`, `on(channel,handler)`; handler errors only logged; dropped on invalidate. | current | 0.34.0 | S/event-bus.ts:1-33 | Listeners survived reload before 0.58.1 | P2 | Host-routed pub/sub between extensions, only if a use case appears. |
| E-API-17 | `ExtensionContext` (`ctx`) | `ui, mode (tui|rpc|json|print), hasUI, cwd, sessionManager (read-only), modelRegistry, model, scopedModels, thinkingLevel, isIdle(), isProjectTrusted(), signal, abort(), hasPendingMessages(), shutdown(), getContextUsage(), compact(), getSystemPrompt()`. Created per dispatch, reads live values. | current | mode 0.78.1; scopedModels 0.83.0; signal 0.63.2 | X/types.ts:325-365; X/runner.ts:868 | `getContextUsage` tokens/percent nullable after compaction (0.52.10) | P0 MVP (subset) | Send a small immutable snapshot with each event; heavy reads become RPCs. |
| E-API-18 | `ExtensionCommandContext` | Adds `waitForIdle`, `newSession`, `fork`, `navigateTree`, `switchSession`, `reload`, `getSystemPromptOptions`. Command handlers only. | current | 0.51.1 (switchSession) | X/types.ts:401-435 | Calling from event handlers can deadlock | P1 | Only expose to command calls; deny in event handlers by construction. |
| E-API-19 | `ExtensionToolContext` | Adds `tools` and `executeTool(name,args,{signal,onUpdate})` as `execute()` 5th arg. | current | 0.99.0 | X/types.ts:383-395 | Nested calls bounded (see E-TOOL-08) | P1 | See E-TOOL-08. |
| E-API-20 | `ReplacedSessionContext` / `ProjectTrustContext` | Fresh context for `withSession`; limited context (`cwd, mode, hasUI, ui` with select/confirm/input/notify) for `project_trust`. | current | 0.69.0; 0.79.0 | X/types.ts:442-452, 678-683 | Only 4 UI methods during trust | P1 | Two narrow RPC surfaces. |
| E-API-21 | Session read access | `ctx.sessionManager.getEntries/getBranch/getLeafEntry/getLabel/getSessionFile` (read-only). Branch-aware state: store in tool-result `details`, rebuild from `getBranch()` on `session_start` and `session_tree`. | current | 0.31.0 | D/extensions.md "State"; E/todo.ts, E/bookmark.ts | Rebuild on `session_tree` or state goes stale after navigation | P0 MVP | `session/getBranch` RPC; paginate. Extension state must be branch-aware. |
| E-API-22 | Session entry taxonomy | append-only JSONL tree: message, custom, custom_message, context_edit, compaction, branch_summary, label, session_info, model_change, thinking_level_change. Session version bumped 2 to 3 in 0.35.0 (`hookMessage` role became `custom`). | current | 0.31.0 tree; 0.87.0 context_edit | D/session-format.md | Needs versioned migrations | P0 MVP (the harness lane owns the schema) | Ask store needs `custom` and `custom_message` kinds before extensions can persist. |
| E-API-23 | Cross-check: `ExtensionAPI` count | 10 `register*` methods (3 with `unregister*`), 41 events, plus action methods above. | current | n/a | X/types.ts:1540-1863 | Prior report said 30 events; stale | info | n/a |

## 3. Tools, commands, keybindings, providers, models (area REG)

### 3.1 Tools

| id | feature / API | detail | Pi status | since | source | edge cases | tier | Go note |
|---|---|---|---|---|---|---|---|---|
| E-TOOL-01 | `ToolDefinition` core | `name, label, description, parameters (TypeBox object schema), execute(toolCallId, params, signal, onUpdate, ctx)`; result `{content, details, structuredContent, isError, usage, terminate}`; throw becomes error result. | current | custom tools 0.23.0; execute order fixed 0.51.0 | X/types.ts:565-645 | Parameter order changed once (0.51.0) | P0 MVP | JSON Schema in, `tools/call` request/response with streaming partial updates. Follow MCP result shape. |
| E-TOOL-02 | Prompt fields | `promptSnippet`, `promptGuidelines`. Without `promptSnippet` the tool is left out of the "Available tools" section. | current | 0.55.4; rule 0.59.0 | X/types.ts; CL 0.59.0 | Breaking in 0.59.0 | P1 | Two optional strings. |
| E-TOOL-03 | `prepareArguments`, `constrainedSampling`, `outputSchema` | Compat shim before validation; provider-side constrained decoding; JSON Schema of `structuredContent`. | current | 0.99.0 for outputSchema | X/types.ts | Replacing `content` without `structuredContent` deletes structured content in `tool_result` | P2 | Defer. |
| E-TOOL-04 | `executionMode` | `sequential` or `parallel` per tool; tool calls of one message run in parallel by default; `tool_call` preflight stays sequential. | current | 0.58.0 | D/extensions.md | none found | P0 MVP | Host scheduler flag. |
| E-TOOL-05 | Exposure levels | `direct`, `model-only`, `codemode`, `deferred`, `hidden` (matrix: declared to model / callable via executeTool / activated on register / listed by codemode). | current, very new | 0.99.0 | X/types.ts:509; D/extensions.md | Design was still moving in Unreleased (`codemode-deferred` alias) | P2: needed only with codemode or tool search | Ship `direct` + `hidden` first (boolean `active`). |
| E-TOOL-06 | `namespace`, `annotations` | Namespace `{name, description, instructions}` groups an MCP server; annotations are MCP hints (readOnly, destructive, idempotent, openWorld). | current, very new | 0.99.0 | X/types.ts:509-645 | Annotations use MCP defaults when missing | P1: permissions need hints | Carry annotations on every tool from day one (cheap). |
| E-TOOL-07 | `defaultActive`, `prepareLoadout` | Activation on registration (direct/model-only only); orchestrating tool adjusts other tools' descriptions and hidden declarations. | current, very new | 0.99.0 | X/types.ts | none found | P2 | Skip `prepareLoadout`. |
| E-TOOL-08 | Nested tool calls | `ctx.executeTool()` runs through validation and `tool_call`/`tool_result`; ids `<parent>/<n>`; events carry `parentToolCallId`; `nestedCalls` record on parent (256 calls, 8 KiB args, 32 KiB result); usage rolled up. | current, very new | 0.99.0 | D/extensions.md; S/nested-tool-calls.ts | Permission gates must cover nested calls | P2 (needed for codemode) | Design the pipeline so every call, nested or not, passes one permission path. |
| E-TOOL-09 | `terminate` | Result `terminate:true` skips the automatic follow-up only if every finalized tool in the batch agrees; `tool_call` handler `terminate` on a blocked call also skips it. | current | 0.66.0 to 0.70; blocked 0.84.1 | D/extensions.md; CL 0.84.1 | Batch agreement rule | P1 | Host loop rule. |
| E-TOOL-10 | Built-in override | Register a tool with a built-in name; first registration wins on conflict; built-in renderers kept. | current | 0.34.0 | E/tool-override.ts; X/runner.ts:629 | Conflict error entry, later extension not unloaded (0.55.0) | P1 | Explicit `override: true` field, error otherwise. |
| E-TOOL-11 | Pluggable tool operations | Read/Write/Edit/Bash/Ls/Grep/Find `Operations` for remote (SSH) execution; bash spawn hook. | current | 0.39.0; spawn hook 0.51.0 | CL 0.39.0; E/ssh.ts, E/bash-spawn-hook.ts | none found | P2 | Interface per tool in `internal/tools` (harness lane). |
| E-TOOL-12 | Helpers | `defineTool()` (types only), `withFileMutationQueue()` (serialize file edits), `renderShell`, `renderCall/renderResult`. | current | defineTool 0.65.0; renderers must return Component 0.62.0 | X/types.ts:656; X/wrapper.ts | TUI components | skip (renderers) / P1 (mutation queue) | Mutation queue belongs in the host tool layer. |
| E-TOOL-13 | Argument mutation | `tool_call` handlers may mutate `event.input` in place; not re-validated. | current | 0.52.7 (documented) | X/types.ts:1198-1203 | Later handlers see mutations | P0 MVP, but as an explicit `modifiedInput` result and re-validate | Go note: in-place mutation cannot cross a process. Re-validate after the patch (fixes a Pi gap). |

### 3.2 Commands, keybindings, prompt input

| id | feature / API | detail | Pi status | since | source | edge cases | tier | Go note |
|---|---|---|---|---|---|---|---|---|
| E-CMD-01 | Command dispatch | `prompt()` checks `startsWith("/")` first; extension command runs immediately even while streaming; a throw becomes `ExtensionError{command:<name>}` and the command still counts as handled. | current | 0.32.2 | S/agent-session.ts:1893, 2031-2055 | `steer`/`followUp` and RPC queue reject extension commands | P0 MVP | Same rule in the gateway. |
| E-CMD-02 | Slash namespaces | Extension commands, prompt templates `/name`, skills `/skill:name`, 24 built-ins. Extension commands win over same-name templates. | current | prompts 0.35.0; skill: 0.43.0 | S/slash-commands.ts:20-44 | Collisions resolved by suffix (see E-API-03) | P0 MVP | One registry, one collision rule. |
| E-CMD-03 | Arg completions | `getArgumentCompletions(prefix)` returns items or null, sync or async. | current | 0.31.0 | X/types.ts:1521 | none found | P1 | Async RPC with short deadline. |
| E-CMD-04 | Keybinding ids | Namespaced ids (`app.tools.expand`, `tui.select.confirm`); `keybindings.json` migrated automatically. | current | 0.33.0 file; 0.61.0 namespacing | CL 0.61.0 | Extensions had to update `keyHint()`/`matches()` names | P2 | Owned by the TUI lane. Extension shortcuts are data. |
| E-CMD-05 | Input transforms | `input` event `continue | transform | handled`; examples inline-bash, input-transform. | current | 0.47.0 | X/runner.ts:1511; E/inline-bash.ts | Extension commands run before `input` handlers | P0 MVP | See event table. |

### 3.3 Providers, models, virtual models

| id | feature / API | detail | Pi status | since | source | edge cases | tier | Go note |
|---|---|---|---|---|---|---|---|---|
| E-PRV-01 | `registerProvider(name, config)` | `baseUrl, apiKey (literal, $ENV, !command), api, headers, authHeader, models[]` (replaces all models of that provider; only `baseUrl` = override existing), `images`, `classifiers`. | current | 0.50.0 | X/types.ts:1876-1932 | Registration flushed in `bindCore`; immediate afterwards (0.55.2) | P1 | Config-only providers (OpenAI-compatible) can be declarative JSON; no code needed. |
| E-PRV-02 | `unregisterProvider` | Restores overridden built-ins. | current | 0.55.2 | X/types.ts:1759-1819 | none found | P1 | Trivial. |
| E-PRV-03 | Custom streaming | `streamSimple(model, context, options)`; must call `options.onPayload` (so `before_provider_request` applies) and `onResponse`; may call `onProviderStreamEvent`; context is a normalized transcript. | current | full provider 0.81.0; context normalized 0.86.0 | X/types.ts:1886-1894 | Bidirectional streaming: needs host callbacks | P2 | Out-of-process needs a bidirectional stream. Start with config-only providers; allow "proxy provider" (extension hosts a local OpenAI-compatible endpoint). |
| E-PRV-04 | OAuth for providers | `oauth{name, login, refreshToken(creds, signal), getApiKey, modifyModels}`. | current | 0.50.0; signal required 0.84.0 | X/types.ts:1876-1932 | `getApiKey` became `getApiKeyAndHeaders` (0.63.0) | P2 | Host does OAuth; extension supplies endpoints. |
| E-PRV-05 | `refreshModels(ctx)` | Dynamic catalogs. | current | 0.80.8 | CL 0.80.8 | `ModelRegistry.refresh()` became async | P2 | Periodic RPC. |
| E-PRV-06 | Model types | `chat` (reasoning, contextWindow, maxTokens, cost, thinkingLevelMap, promptCache, compat, samplingParams), `image`, `classifier`. | current | thinkingLevelMap 0.72.0 | X/types.ts:1934-1987 | `compat.reasoningEffortMap` renamed | P1 (chat only) | Reuse the harness model struct. |
| E-PRV-07 | `ctx.modelRegistry` for extensions | `find, complete, streamSimple, classify, findOfType, hasConfiguredAuth, refresh, getApiKeyAndHeaders`. | current | complete 0.31.0; stream 0.86.0 | E/custom-compaction.ts, E/summarize.ts | Auth objects changed twice (0.63.0, 0.84.0) | P1: `complete` only | Offer `model/complete` RPC so extensions never see API keys. |
| E-PRV-08 | Virtual models | `registerVirtualModel({provider,id,name,route(request,ctx)=>ModelRoute})`; selection stored as `model_change`; dispatch recorded on each assistant message. | current, experimental feature flag in docs, very new | 0.99.0 | D/virtual-models.md:1-40; X/types.ts:1856 | Router state comes from session branch | P2 | Later. |
| E-PRV-09 | Cache warming | `cache_warming_decision` event lets an extension warm or stop. | current | 0.86.0 | S/cache-warmer.ts:309 | none found | P2 | Skip until prompt caching exists in Ask. |

## 4. Event and hook table: all 41 events (area EVT)

Columns: effect N = notify only; M = modify or replace; B = block or cancel. "Sync?" = needs sync in-process answer? A yes means the harness must wait for the extension's reply before it continues (this drives the runtime choice). Await: A = awaited in load order, F = fire-and-forget. Errors: C = caught and reported, next handler runs; FC = fail-closed. Pi has no handler timeouts (`hookTimeout` removed in 0.31.0).

| id | event | payload | fires when | effect and return | merge rule | sync? | since | source | edge cases | tier | Go note |
|---|---|---|---|---|---|---|---|---|---|---|---|
| E-EVT-01 | `project_trust` | `{cwd}`; limited ctx | Pre-trust pass, before project resources load; only user and CLI extensions loaded | B/M `{trusted: yes|no|undecided, remember}` | first yes/no wins; undecided falls through. A, C | yes | 0.79.0 | S/project-trust.ts:57; X/runner.ts:283-312 | Built-ins cannot handle it; non-interactive modes cannot prompt | P1 (only if third-party extensions exist) | Ask can start with a config-only trust store. |
| E-EVT-02 | `resources_discover` | `{cwd, reason: startup|reload}` | After `session_start` | M `{skillPaths, promptPaths, themePaths}` | concatenate; tagged scope "temporary". A, C | yes | 0.50.8 | S/agent-session.ts:3203, 3309 | none found | P1 | Return paths; host loads them. |
| E-EVT-03 | `session_start` | `{reason: startup|reload|new|resume|fork, previousSessionFile}` | Bind on startup, reload, or session replacement | N | none. A, C | no (awaited for ordering, no answer used) | 0.65.0 (reason field) | S/agent-session.ts:3194,3597 | Fired before UI ready (bug fixed 0.70.0); replaced `session_switch`/`session_fork` | P0 MVP | Extension starts processes here. |
| E-EVT-04 | `mcp_servers_change` | `{servers[]}` | After `registerMcpServer`/`unregister` post-bind | N; handling it marks the extension as the MCP connector | none. F, C | no | 0.99.0, very new | X/runner.ts:459 | No connector yields `register_mcp_server` error | P2 | Skip: Ask owns MCP in-core. |
| E-EVT-05 | `session_info_changed` | `{name}` | Session name change | N | F | no | 0.80.3 | S/agent-session.ts:3846 | none found | P2 | |
| E-EVT-06 | `session_before_switch` | `{reason: new|resume, targetSessionFile}` | Before `/new`, `/resume` | B `{cancel}` | cancel short-circuits; else last defined. A, C | yes | 0.27.0 (before_*) | S/agent-session-runtime.ts:142 | Cancel visible in RPC responses | P1 | Needed by dirty-repo guard, confirm-destructive. |
| E-EVT-07 | `session_before_fork` | `{entryId, position: before|at}` | Before fork or clone | B `{cancel, skipConversationRestore}` | same | yes | 0.27.0; position 0.68.0 | S/agent-session-runtime.ts:159 | `skipConversationRestore` for checkpoint hooks (0.27.2) | P1 | |
| E-EVT-08 | `session_before_compact` | `{preparation, branchEntries, customInstructions, reason: manual|threshold|overflow, willRetry, signal}` | Before manual or auto compaction | B/M `{cancel, compaction: CompactionResult}` (extension supplies the summary) | cancel short-circuits; last result wins. A, C | yes | 0.27.0; reason/willRetry 0.79.10 | S/agent-session.ts:2708, 3040 | Payload carries live abort signal | P1 | Payload must be serializable; drop `signal`, use RPC cancel. |
| E-EVT-09 | `session_compact` | `{compactionEntry, fromExtension, reason, willRetry}` | After compaction | N | A, C | no | 0.27.0 | S/agent-session.ts:2773, 3103 | none found | P2 | |
| E-EVT-10 | `session_compact_failed` | `{reason, errorMessage, aborted, willRetry, fromExtension}` | Compaction failed | N | A, C | no | 0.84.3 | S/agent-session.ts:1014 | none found | P2 | |
| E-EVT-11 | `session_before_tree` | `{preparation, signal}` | Before `/tree` navigation with summary | B/M `{cancel, summary, customInstructions, replaceInstructions, label}` | cancel short-circuits; last wins. A, C | yes | 0.31.0 | S/agent-session.ts:3928 | none found | P2 (depends on tree UI in Ask) | |
| E-EVT-12 | `session_tree` | `{newLeafId, oldLeafId, summaryEntry, fromExtension}` | After navigation | N | A, C | no | 0.31.0 | S/agent-session.ts:4043 | Branch-aware extension state must rebuild here | P1 | |
| E-EVT-13 | `session_shutdown` | `{reason: quit|reload|new|resume|fork, targetSessionFile}` | Runtime teardown, reload, SIGTERM/SIGHUP | N (cleanup) | A, C | no (awaited, so cleanup finishes) | 0.27.0; reason 0.68.0 | S/agent-session-runtime.ts:171,405; S/agent-session.ts:3578 | Non-interactive runs must emit it to terminate (0.60.0, 0.87.1) | P0 MVP | Host waits for ack with a deadline, then kills the child. |
| E-EVT-14 | `input` | `{text, images, source: interactive|rpc|extension, streamingBehavior}` | Start of `prompt()` after extension-command dispatch, before template expansion | M/B `continue | transform{text,images} | handled` | transforms chain; `handled` short-circuits. A, C | yes | 0.47.0; streamingBehavior 0.77.0 | S/agent-session.ts:1842; X/runner.ts:1511 | RPC `steer`/`follow_up` must pass through it (0.83.0) | P0 MVP | |
| E-EVT-15 | `before_agent_start` | `{prompt, images, systemPrompt (read-only), systemPromptOptions (mutable shared)}` | After user prompt accepted, before the agent loop | M `{message, systemPrompt}` | messages collected from all; `systemPrompt` last wins; options mutated in place. A, C | yes | 0.31.0; full prompt 0.39.0; options 0.68.0 | S/agent-session.ts:1977; X/runner.ts:1411 | `systemPromptAppend` was replaced in 0.39.0 | P0 MVP | Replace in-place mutation with a `systemPromptPatch` result. |
| E-EVT-16 | `agent_start` | `{}` | Loop begins | N | A | no | 0.18.0 | S/agent-session.ts:1245 | none found | P1 | |
| E-EVT-17 | `turn_start` | `{turnIndex, timestamp}` | Each LLM turn begins | N | A | no | 0.18.0 | S/agent-session.ts:1252 | none found | P1 | |
| E-EVT-18 | `context` | `{messages}` (system messages hidden) | Before each LLM call | M `{messages}` or in-place edit | chained over structuredClone; system messages restored after each handler. A, C | yes | 0.31.0 | S/sdk.ts:415; X/runner.ts:1289 | Handler removing leading system message reported, honored (context_with_system) | P1 | Full message list over RPC each call is expensive; allow filter subscriptions. |
| E-EVT-19 | `context_with_system` | `{messages}` (full transcript) | After all `context` handlers | M `{messages}` | chained | yes | 0.87.0 | X/runner.ts:1330-1345 | Dropped leading system message honored | P2 | Fold into E-EVT-18. |
| E-EVT-20 | `before_provider_request` | `{payload}` (wire payload) | Before each provider HTTP request | M: any non-undefined return replaces payload | chained. A, C | yes | 0.57.0 | S/sdk.ts:359-362; X/runner.ts:1352 | Custom `streamSimple` must call `onPayload` or hook is skipped | P2 | Payload is provider-specific JSON. |
| E-EVT-21 | `before_provider_headers` | `{headers}` | Before request | M by in-place mutation; `null` deletes a header; return ignored | shared object. A, C | yes | 0.80.4 | S/sdk.ts:338; X/runner.ts:1383 | | P2 | Return a header map patch. |
| E-EVT-22 | `after_provider_response` | `{status, headers}` | Response received | N | A | no | 0.67.4 | S/sdk.ts:364-372 | | P2 | |
| E-EVT-23 | `provider_stream_event` | `{provider, api, model, data}` (parsed, pre-normalization, read-only) | Every raw provider stream event | N | A (slow handler stalls stream); C | no (but blocking: awaited in stream order) | 0.99.0, very new | S/sdk.ts:374-386 | Per-token backpressure | P2 | Make it fire-and-forget with a bounded queue. |
| E-EVT-24 | `cache_warming_decision` | `{action,...}` | Cache warmer tick | M `{action: warm|stop}` | last wins. A, C | yes | 0.86.0 | S/cache-warmer.ts:309 | | P2 | |
| E-EVT-25 | `message_start` | `{message}` | Message begins | N | A | no | 0.52.x | S/agent-session.ts:1261 | | P1 | |
| E-EVT-26 | `message_update` | `{message, assistantMessageEvent}` | Per delta | N | A (awaited) | no | 0.52.x | S/agent-session.ts:1268 | Slow handler delays stream | P1, fire-and-forget | Bounded, droppable queue. |
| E-EVT-27 | `message_end` | `{message}` | Message finalized | M `{message}` (same role) | chained; role mismatch skipped; null content normalized to `[]`. A, C | yes | 0.52.x; replace 0.71.0 | S/agent-session.ts:1274-1290 | e.g. override cost | P2 | |
| E-EVT-28 | `turn_end` | `{turnIndex, message, toolResults, messageEntryId, toolResultEntryIds, entries, continue, context, outcome}` | Turn boundary | M `{entries: custom|custom_message|context_edit|compaction drafts, continue}` | each handler replaces; context preview rebuilt after each; preview failure drops all entries and forces `continue:false`. A, C | yes | 0.18.0; actionable 0.87.0 | S/agent-session.ts:806-830; X/runner.ts:1020 | Required fields added 0.87.0 (break); replaced `shouldStopAfterTurn` (0.72.0 to 0.87.0) | P1 | Boundary drafts need the entry taxonomy (E-API-22). |
| E-EVT-29 | `agent_before_settle` | boundary state, no message | Just before the run settles | M same as `turn_end` (one continuation request) | same | yes | 0.87.0 | S/agent-session.ts:1813 | Guard against loops | P2 | |
| E-EVT-30 | `agent_end` | `{messages}` | Loop ends | N | A | no | 0.18.0 | S/agent-session.ts:1247 | | P1 | |
| E-EVT-31 | `agent_settled` | `{}` | All settle work done (also RPC) | N; runs requested inside are deferred until all handlers finish | A | no | 0.80.4 | S/agent-session.ts:1042 | No reentrant `agent_start` (0.87.0) | P1 | The right point for notifications and auto-commit. |
| E-EVT-32 | `ui_prompt_start` | `{reason, kind: select|confirm|input|editor|custom, title}` | Outermost extension prompt opens | N | F (`queueMicrotask`) | no | 0.84.4 | X/runner.ts:569-616 | Nested prompts collapse to outermost | P2 | Lets timers/idle logic tell "waiting on user". |
| E-EVT-33 | `ui_prompt_end` | same | Prompt closes | N | F | no | 0.84.4 | same | | P2 | |
| E-EVT-34 | `model_select` | `{model, previousModel, source: set|cycle|restore}` | Model change | N | A | no | 0.43.0 | S/agent-session.ts:2378 | | P1 | |
| E-EVT-35 | `thinking_level_select` | `{level, previousLevel}` | Level change | N | F (`void`) | no | 0.71.0 | S/agent-session.ts:2544 | | P2 | |
| E-EVT-36 | `tool_call` | `{toolName, toolCallId, parentToolCallId, input}` typed per built-in; `isToolCallEventType()` guard | Before every tool run (also nested) | B/M `{block, reason, terminate}`; may mutate `input` in place | later handlers see mutations; `block:true` returns at once. A. **FC**: no try/catch, a throw blocks the tool | yes | 0.18.0 | S/agent-session.ts:615-640; X/runner.ts:1233-1250 | Mutations not re-validated; moved to agent-core `beforeToolCall` in 0.58.0 | P0 MVP | This is THE event that forces a synchronous round trip. |
| E-EVT-37 | `tool_execution_start` | `{toolCallId, toolName, args}` | Tool run begins | N | A, C | no | 0.52.x | S/agent-session.ts:1289-1320 | Nested via `_executeNestedToolCall` | P1 | |
| E-EVT-38 | `tool_execution_update` | `{toolCallId, toolName, args, partialResult}` | Streaming tool output | N | A, C | no | 0.52.x | same | `toolcall_start` events lacked id and name (fixed 0.84.2) | P1 | Droppable. |
| E-EVT-39 | `tool_execution_end` | `{toolCallId, toolName, result, isError}` | Tool run ends | N | A, C | no | 0.52.x | same | | P1 | |
| E-EVT-40 | `tool_result` | `{toolCallId, toolName, input, content, structuredContent, details, isError, usage, parentToolCallId}` | After a tool returns | M `{content, details, structuredContent, isError, usage}` | field-by-field compose; patches chain across handlers. A, C | yes | 0.18.0; content array 0.24.0 | S/agent-session.ts:642-690; X/runner.ts:1174 | Text-only `result` string removed in 0.24.0 | P0 MVP | |
| E-EVT-41 | `user_bash` | `{command, excludeFromContext, cwd}` (user `!` / `!!`) | User bash prefix in interactive or RPC | B/M `undefined` = pass; `{operations}` or `{result}` = handled | first defined result wins; invalid result throws. A. **FC** since 0.86.0 | yes | 0.39.0; FC 0.86.0 | S/modes/interactive/interactive-mode.ts:6788; S/modes/rpc/rpc-mode.ts:562; X/runner.ts:1253 | Direct RPC `bash` must go through it (0.86.0) | P1 | Sandbox extensions use it to route into a VM. |

Summary: 19 events need a synchronous answer (`project_trust`, `resources_discover`, `session_before_switch`, `session_before_fork`, `session_before_compact`, `session_before_tree`, `input`, `before_agent_start`, `context`, `context_with_system`, `before_provider_request`, `before_provider_headers`, `cache_warming_decision`, `message_end`, `turn_end`, `agent_before_settle`, `tool_call`, `tool_result`, `user_bash`). 22 do not. Only `tool_call` and `user_bash` are fail-closed; all others log and continue. Events with per-token frequency (`message_update`, `provider_stream_event`, `tool_execution_update`) need a bounded, droppable queue in any out-of-process design. Also in Pi but not among the 41 names: `ctx.ui.onTerminalInput` (raw input hook, added as `terminal_input` in 0.52.x, TUI only).

Payload rule for Ask: mutation-in-place events (`tool_call.input`, `before_provider_headers`, `systemPromptOptions`, `context`) become explicit patch or replace results. Pi's own docs note `tool_call` mutation is not re-validated, so Ask should re-validate.

## 5. Message injection, session access and state (area STATE)

| id | feature / API | detail | Pi status | since | source | edge cases | tier | Go note |
|---|---|---|---|---|---|---|---|---|
| E-STATE-01 | Injection paths | `sendMessage` (custom message, optional turn), `sendUserMessage` (always a turn), `before_agent_start` `message` return, `turn_end` `entries` drafts. | current | see E-API-07..08 | S/agent-session.ts | Deferred inside `agent_settled` | P0 MVP | Single injection queue in the host. |
| E-STATE-02 | State: branch-aware | Tool-result `details` plus rebuild from `getBranch()` on `session_start`/`session_tree` (`todo.ts` pattern). | current | 0.31.0 | D/extensions.md "State" | Forgetting `session_tree` leaves stale state | P0 MVP | Document as the recommended pattern. |
| E-STATE-03 | State: durable outside LLM | `appendEntry` + `registerEntryRenderer`. | current | 0.31.0; renderer 0.80.4 | S/agent-session.ts:3315 | | P0 MVP | |
| E-STATE-04 | State: durable in LLM | `sendMessage` creates `custom_message`. | current | 0.31.0 | S/agent-session.ts:2208 | Session v2 to v3 rename `hookMessage` to `custom` (0.35.0) | P0 MVP | |
| E-STATE-05 | Context editing | `context_edit` entries via `turn_end` boundary drafts; `context` event for non-destructive per-call edits. | current | 0.31.0; 0.87.0 | X/runner.ts:1020-1105 | Invalid entry drops the whole boundary batch | P2 | |
| E-STATE-06 | Compaction interaction | Extension can cancel or supply the summary; `ctx.compact({customInstructions,onComplete,onError})` is fire-and-forget. | current | 0.49.0 | S/agent-session.ts:2708-2790 | `willRetry` and `reason` added 0.79.10 | P1 | |
| E-STATE-07 | Extension private storage | None in Pi. Extensions write their own files. | current (gap) | n/a | D/settings.md | | P1 | Give each extension `data_dir` and a KV table. Cheap, avoids ad hoc files. |

## 6. UI primitives for extensions (area UI)

Constraint from the Bubbletea lane: no Charm equivalent exists for the extension UI API (item A40, difficulty high). Design: bridge goroutine converts each request into a `tea.Msg`; pending-dialog queue; slots and widget map in the model; timeouts enforced by the agent side (`researcher-...-go-tui-bubbletea.md` section 5).

| id | feature / API | detail | Pi status | since | source | edge cases | tier | Go note |
|---|---|---|---|---|---|---|---|---|
| E-UI-01 | Dialogs | `select(title, options, {signal, timeout})`, `confirm`, `input`, `editor(title, prefill)`. | current | 0.18.0; timeouts 0.38.0 | X/types.ts:149-300 | Dialogs resolve to default on timeout or abort; no-op UI: `confirm` false, others undefined | P0 MVP | Serializable; the RPC sub-protocol carries them. |
| E-UI-02 | Notifications | `notify(message, info|warning|error)`. | current | 0.18.0 | same | | P0 MVP | |
| E-UI-03 | Status and title | `setStatus(key, text|undefined)`, `setTitle`. | current | 0.31.0 | same | Keyed slots | P0 MVP | Keyed map in the TUI model. |
| E-UI-04 | Widgets | `setWidget(key, string[] | factory, {placement: aboveEditor|belowEditor})`. | current | 0.34.0 | same | RPC supports string arrays only | P1 (string arrays) | Factory form is skip. |
| E-UI-05 | Footer and header | `setFooter(factory)` (FooterDataProvider with git branch and statuses), `setHeader(factory)`. | current | 0.37.3 | same | TUI only | P2 | Replace with a structured slot: list of `{text, style}` segments. |
| E-UI-06 | Working indicator | `setWorkingMessage`, `setWorkingVisible`, `setWorkingIndicator({frames,intervalMs})`, `setHiddenThinkingLabel`. | current | 0.68.0; visible 0.70.3 | same | TUI only | P2 | Data-only; cheap. |
| E-UI-07 | Custom component | `custom(factory(tui, theme, keybindings, done), {overlay, overlayOptions, onHandle})` returns `Promise<T>`; overlays with anchors, margins, stacking, non-capturing focus (0.57.0). | current | 0.23.0 (custom), overlays 0.39.0 experimental | same | Out of process returns `undefined` (Pi RPC does the same) | skip out-of-process; P2 declarative tree | Not portable. Options: (a) unsupported, (b) declarative UI tree, (c) PTY. Recommend (a), then (b) on demand. |
| E-UI-08 | Editor | `setEditorText`, `getEditorText`, `pasteToEditor`, `setEditorComponent(factory)` (subclass `CustomEditor`), `getEditorComponent`, `addAutocompleteProvider(wrapper)`. | current | 0.38.0; providers 0.69.0; getEditorComponent 0.71.0 | same | RPC: only `setEditorText` | P1 (`setEditorText`), skip the rest | Autocomplete provider as an RPC with deadline is possible later. |
| E-UI-09 | Raw input | `onTerminalInput(handler => {consume,data})`. | current | 0.52.x | same | TUI only | skip | |
| E-UI-10 | Theme | `theme`, `getAllThemes`, `getTheme`, `setTheme(name|Theme)`. | current | 0.39.0 | same | RPC: `setTheme` returns failure | skip | TUI lane owns themes. |
| E-UI-11 | Tool output toggles | `getToolsExpanded/setToolsExpanded`. | current | n/a | same | | skip | |
| E-UI-12 | Prompt wrapping events | `ui_prompt_start/end` around select/confirm/input/editor/custom. | current | 0.84.4 | X/runner.ts:569-616 | | P2 | |
| E-UI-13 | No-op UI in non-TUI modes | `noOpUIContext`; `hasUI` true in tui and rpc; `mode` tells the difference. | current | 0.78.1 | X/runner.ts:314-350 | Extensions must check `hasUI` | P0 MVP | Same in Ask: a headless daemon has no UI; the capability set is negotiated per client. |
| E-UI-14 | Untrusted output hygiene | Strip control sequences (OSC 52, kitty graphics, cursor moves) from extension text; allow SGR only. | not in Pi (advice from TUI lane) | n/a | go-tui-bubbletea report section 5 | Otherwise extension output corrupts the cell model | P0 MVP | `x/ansi.Strip` then allow-list. |
| E-UI-15 | Renderer hooks | Tool `renderCall/renderResult`, message and entry renderers, markdown transformer. | current | 0.23.0+ | X/types.ts | Live components | skip out-of-process; P2 declarative | A tool result may carry an optional `display` block (markdown or a diff) instead of a component. |

### 6.1 RPC extension UI protocol (the serializable subset; wire contract for Ask)

Source: `D/rpc-extension-ui.md`, `src/modes/rpc/rpc-types.ts:252-297`, `rpc-mode.ts:85-320`. Transport: JSONL on stdio, LF-delimited only (0.57.0; `U+2028/2029` broke `readline`). In Go do not use `bufio.Scanner` default limit; use `bufio.Reader.ReadBytes` (edge-cases report).

| id | direction and message | fields | Pi status | since | edge cases | tier | Go note |
|---|---|---|---|---|---|---|---|
| E-RPC-01 | Pi to client `extension_ui_request` `select` | `id, title, options[], timeout` | current | 0.51.0 (docs) | id is random UUID; agent side enforces timeout | P0 MVP | gRPC oneof or WS message |
| E-RPC-02 | `confirm` | `title, message, timeout` | current | 0.51.0 | | P0 MVP | |
| E-RPC-03 | `input` | `title, placeholder, timeout` | current | 0.51.0 | | P0 MVP | |
| E-RPC-04 | `editor` | `title, prefill` | current | 0.51.0 | no timeout field | P1 | |
| E-RPC-05 | `notify` | `message, notifyType` | current | 0.51.0 | fire-and-forget | P0 MVP | |
| E-RPC-06 | `setStatus` | `statusKey, statusText` | current | 0.51.0 | | P0 MVP | |
| E-RPC-07 | `setWidget` | `widgetKey, widgetLines, widgetPlacement` | current | 0.51.0 | string arrays only | P1 | |
| E-RPC-08 | `setTitle` | `title` | current | 0.51.0 | | P1 | |
| E-RPC-09 | `set_editor_text` | `text` | current | 0.51.0 | `getEditorText()` returns "" in RPC | P1 | |
| E-RPC-10 | Client to Pi `extension_ui_response` | `id` + `value` (select/input/editor), `confirmed` (confirm), or `cancelled:true` (any) | current | 0.51.0 | Cancel yields `undefined` or `false` | P0 MVP | |
| E-RPC-11 | No-op set in RPC | `custom()` returns undefined; `onTerminalInput`, footer, header, editor component, autocomplete, working indicator, theme are no-ops; `setTheme` returns `{success:false}` | current | 0.51.0 | Extensions must not depend on them | P0 MVP (documented degradation) | Ask advertises a capability set per client. |
| E-RPC-12 | `get_commands` | Extension, prompt, skill commands with `sourceInfo{path, source local|auto|cli, scope user|project|temporary, origin top-level|package, baseDir}`. Excludes TUI built-ins. | current | 0.51.1 | | P1 | |
| E-RPC-13 | Client rules from the RPC lane | Unknown command returns error with request id; client rejects pending requests when child exits; stdout is protocol only, stray writes go to stderr. | current | 0.61.0 to 0.99.0 | Many fixes (edge-cases report section 20) | P0 MVP | Applies to the extension stdio channel too. |

## 7. Package manager and sources (area PKG)

| id | feature / API | detail | Pi status | since | source | edge cases | tier | Go note |
|---|---|---|---|---|---|---|---|---|
| E-PKG-01 | Package manifest | `package.json` key `pi: {extensions[], skills[], prompts[], themes[]}` with globs, `!` exclusions, `+path`/`-path`; no manifest = conventional dirs; keyword `pi-package`; `pi.image`/`pi.video` previews. | current | 0.46 to 0.50.0 | S/pi-manifest.ts:13-33; D/packages.md | Parser accepts only string arrays | P1 | Use `ask-package.json` (name, version, `apiVersion`, entries, requested capabilities). Pi has no API version (a lesson). |
| E-PKG-02 | Sources | `npm:name[@ver]`, `git:host/path[@ref]`, `https://` git URLs, local path (not copied). Bare `github.com/...` needs `git:` since 0.52.10. | current | 0.50.0 | D/packages.md; CL 0.52.10 | Strict source parsing was a breaking change | P1 (git + local); skip npm | npm dependency is a Node-specific burden. Go: git clone (pinned ref) or release tarball with checksum. |
| E-PKG-03 | CLI | `pi install <src> [-l]`, `remove|uninstall`, `list`, `update [<src>|--extensions|--models|--all|--self]`, `config [--local]`. | current | 0.50.0; local paths 0.51.3 | D/cli.md:248-288 | `pi update` updates Pi only unless `--all` (0.79.7) | P1 | `ask ext install|remove|list|update`. |
| E-PKG-04 | Settings | `packages: (string | {source, extensions, skills, prompts, themes, autoload})[]`; filters narrow but never widen; identity: npm by name, git by repo URL without ref, local by resolved path; project entry replaces user entry. | current | 0.50.0 | D/packages.md | Migration from `extensions` array to `packages` in 0.50.0 | P1 | Same identity rule. |
| E-PKG-05 | Install mechanics | npm: managed root (`.pi/npm` project, `<agentDir>/npm` user), peer deps skipped; git: clone, checkout ref, `npm install --omit=dev`; missing sources auto-install on resolve. | current | 0.50.0 | S/package-manager.ts:1820-1920, 2087-2093 | No `--ignore-scripts` found: dependency lifecycle scripts run at install (unverified by execution) | P1 | Do not run install scripts. Verify checksum. |
| E-PKG-06 | Pinning and updates | Versioned npm specs and git tags/commits are pinned; startup no longer auto-updates unpinned packages. | current | 0.60.0 | D/packages.md; CL 0.60.0 | Auto-update at startup was removed (surprise mutation, supply chain) | P0 MVP (never auto-update) | |
| E-PKG-07 | Host-provided deps | `pi-ai`, `pi-agent-core`, `pi-coding-agent`, `pi-tui`, `typebox` must be `peerDependencies "*"`, never bundled; warning if in `dependencies`. | current | 0.99.0 | D/packages.md | Coupling to host version | skip | With a process boundary this problem disappears. |
| E-PKG-08 | Security of git packages | Reject unsafe host/path, keep clones inside root, force-push safe update, partial install cleanup, production-only deps. | current | 0.62.0 to 0.84.0 | edge-cases section 21 | Path traversal in source string | P1 | Test path traversal. |
| E-PKG-09 | Project packages | Installed and loaded only after project trust. | current | 0.79.0 | D/security.md | | P0 MVP | |
| E-PKG-10 | Temp files | Private `0700` dir, not shared temp. | current | 0.75.4 | edge-cases section 21 | | P1 | |
| E-PKG-11 | Skills discovery locations | `~/.pi/agent/skills`, `.pi/skills` (trust-gated), `~/.agents/skills`, `.agents/skills` (cwd and ancestors to repo root), `skills` setting, packages, `resources_discover`. | current | .agents 0.54.0 | S/skills.ts | Symlinks deduped by canonical path | P0 MVP | |

## 8. Skills, prompt templates, MCP, codemode, tool search (area RES)

| id | feature / API | detail | Pi status | since | source | edge cases | tier | Go note |
|---|---|---|---|---|---|---|---|---|
| E-RES-01 | Skills format | Agent Skills spec: `SKILL.md` with frontmatter `name` (<=64, lowercase/digits/hyphen), `description` (<=1024, required), `license`, `compatibility`, `metadata`, `allowed-tools`, `disable-model-invocation`. Only name, description and path enter the system prompt as `<available_skills>`; the model reads the file on demand. | current | 0.19.0; dir rule 0.20.0; disable flag 0.50.0 | S/skills.ts:11-14,95-123,341-380 | BOM, multi-line YAML, unknown fields ignored, name/dir mismatch only a warning, stop recursion at `SKILL.md`, respect `.gitignore` | P0 MVP (data feature, low risk) | Pure Go parsing; no code execution. |
| E-RES-02 | `/skill:name args` | Expands to a `<skill name location>` block plus args; setting `enableSkillCommands`. | current | 0.43.0; moved to AgentSession 0.47.0 | S/agent-session.ts:2065-2085 | Must work in RPC and print | P1 | |
| E-RES-03 | Prompt templates | Markdown with frontmatter `description`, `argument-hint`; filename = command. Substitutions `$1..`, `$@`/`$ARGUMENTS`, `${1:-default}`, `${@:-default}`, `${@:N}`, `${@:N:L}`; shell-like quoting. Direct `.md` children only in conventional dirs. | current | renamed from slash commands 0.35.0 | D/prompt-templates.md; S/prompt-templates.ts | `input` sees raw text before expansion; extension command wins over same-name template | P1 | Small parser; test quoting. |
| E-RES-04 | MCP client | stdio + Streamable HTTP; legacy SSE rejected. No official SDK in Pi (own client). Tools named `mcp__<server>__<tool>`; names `[A-Za-z0-9_-]`. | current, very new | 0.99.0 | packages/mcp/README.md; D/mcp.md | Invalid entries reported and skipped | P1 | Use `modelcontextprotocol/go-sdk` behind `internal/mcp` (Go lane). |
| E-RES-05 | MCP config | `~/.pi/agent/mcp.json`, `.pi/mcp.json` (trust-gated; project replaces user by name). Fields: stdio `command,args,env,cwd`; http `url,headers,oauth`; common `timeout` (60 s default, progress resets), `enabled`, `exposure`, `toolExposure` (glob map), `description`. `${VAR}` and whole-value `!command` resolution. Conversion table for Claude/Cursor/VS Code/Codex/OpenCode. | current, very new | 0.99.0 | D/mcp.md:44-75 | `~/` expansion; relative `cwd` from session dir | P1 | Same `mcpServers` map format to keep users' existing files usable. |
| E-RES-06 | MCP lifecycle | Connect at session start; first prompt waits up to 10 s; HTTP errors 408/429/5xx retried twice; dropped connection reconnects on next call; `tools/list_changed` handled; stdio stop = close stdin, SIGTERM, SIGKILL to process group; tool calls never retried. | current, very new | 0.99.0 | D/mcp.md | Process-group kill needed | P1 | Same rules; setpgid + kill group. |
| E-RES-07 | MCP results and resources | Text over 20 KB: middle elided, full text in temp file. Tools `list_mcp_resources`, `list_mcp_resource_templates`, `read_mcp_resource`. MCP Apps (`ui://`) omitted. | current, very new | 0.99.0 | D/mcp.md | Binary resources saved to temp file | P2 | |
| E-RES-08 | MCP OAuth | Dynamic client registration as client name `pi`; loopback redirect (RFC 8252); tokens in `mcp-auth.json`; log `mcp.log` rotated at 5 MB; login via `/mcp login`, `pi mcp login`. | current, very new | 0.99.0 | D/mcp.md | Paste redirect URL for remote browsers | P2 | Needed only for hosted MCP servers. |
| E-RES-09 | `/mcp` and `pi mcp add|remove|list|login|logout` | State list, reconnect, sign in/out, exposure change, enable/disable (persisted without rewriting unrelated content). `list` exits 1 on invalid entry or disconnected enabled server. | current, very new | 0.99.0 | D/mcp.md; D/cli.md | | P1 | |
| E-RES-10 | MCP permissions | Every MCP call passes the normal tool pipeline so `tool_call` gates apply; annotations exposed. | current, very new | 0.99.0 | D/mcp.md | Codemode calls carry `parentToolCallId` | P0 MVP (as design rule) | One pipeline for built-in, extension, MCP and nested calls. |
| E-RES-11 | Extension-registered MCP | `registerMcpServer`, `unregisterMcpServer`, `getMcpServers`; session-scoped; `mcp.json` entry of same name wins; connector is the extension handling `mcp_servers_change`. Any extension registering `/mcp`, `codemode` or `tool_search` supersedes the built-in. | current, very new | 0.99.0 | D/extensions.md; S/resource-loader.ts:120-160 | Names owned by another extension throw | P2 | Skip the connector indirection. |
| E-RES-12 | Codemode | Model-written JS runs in QuickJS compiled to WASM in a worker; only capability is injected tools. Globals `tools`, `ALL_TOOLS`, `text`, `image`, `exit`, `store`/`load`, `searchTools` (BM25), `describeTool`, `describeNamespace`, `models.*`; no timers, fetch, require. Options line `// @options`. Output over max keeps start and end, full text in temp file; nested calls stay out of LLM context. Settings `codemode.mode`, `inlineBudget`. | current, very new (1 day old at this commit) | 0.99.0 | packages/codemode/README.md; D/cli.md | Description churned (tool lists moved out to keep cache stable: `codemode-deferred` became alias, Unreleased); `image()` accepts PNG/JPEG/GIF/WebP base64 only | P2, defer; do not build first | Needs a JS engine: goja (async/await gaps, verify) or QuickJS in wazero. Executes model code, so gate behind sandbox. |
| E-RES-13 | Tool search | `tool_search` tool (registered inactive) finds and declares `deferred` tools by BM25; recorded in transcript so the set survives `/tree` and resume. | current, very new | 0.99.0 | src/extensions/tool-search/index.ts:14 | | P2 | Useful once many MCP tools exist. |
| E-RES-14 | SDK embedding | SDK sessions load no built-ins; add `createMcpExtension()`, `createCodemodeExtension()`, `createToolSearchExtension()` to `extensionFactories`, call `bindExtensions()`. | current | 0.99.0 | D/sdk.md; E/sdk/14-codemode-mcp.ts | | P2 | Go embedding via fx modules. |
| E-RES-15 | Size signal | 5035 lines across mcp, codemode, tool-search extension dirs in Pi. | info | 0.99.0 | extensions report section 11 | | info | Budget accordingly. |
| E-RES-16 | Themes as resources | Package/extension may contribute theme JSON via `resources_discover` and `themes` manifest key. | current | 0.34.0 | D/themes.md | TUI-only | skip (TUI lane) | |

## 9. Example extensions (area EX)

79 files and directories in `examples/extensions`. Grouped by what they prove the API must support. Counts and sizes from the extensions report section 16.

| id | group | examples (lines) | API and events used | what it proves | tier | Go note |
|---|---|---|---|---|---|---|
| E-EX-01 | Permission and safety gates | permission-gate (34), protected-paths (30), confirm-destructive (59), dirty-repo-guard (56) | `tool_call` block, `session_before_fork/switch`, `select`, `confirm`, `exec` | Blocking hooks plus a dialog are the main use case | P0 MVP (ship as conformance tests) | Reference tests for the sync-hook path. |
| E-EX-02 | Sandbox and remote | sandbox/ (321), gondolin/ (531), ssh (220) | override built-in tools, `user_bash`, flags, `setStatus` | Tool override and `user_bash` used for isolation | P1 | |
| E-EX-03 | Plan mode | plan-mode/ (390) | `context`, `before_agent_start`, `turn_end`, `agent_end`, `setActiveTools`, `appendEntry`, `sendMessage`, `registerShortcut`, `registerFlag`, `setWidget` | Widest single API use; plan mode is an extension, not core | P1 (good acceptance test) | |
| E-EX-04 | Tools | hello (26), todo (297), dynamic-tools (74), structured-output (65), tool-override (144), built-in-tool-renderer (225), minimal-mode (354), truncated-tool (195), bash-spawn-hook (30), question (286), questionnaire (448), subagent/ (1038), with-deps/ (32), tic-tac-toe (1008) | `registerTool` variants, `terminate`, renderers, `executionMode` sequential | Branch-aware state via `details`; subagent spawns isolated agents | P0 MVP (hello, todo); P2 (subagent) | Subagent is an extension in Pi, not core. |
| E-EX-05 | Commands and models | commands (72), preset (436), handoff (190), qna (118), summarize (199), session-name (27), bookmark (50), trigger-compact (50), shutdown-command (63), reload-runtime (37) | `registerCommand`, `setModel`, `setThinkingLevel`, `newSession`, `modelRegistry.complete`, `setLabel`, `compact`, `reload` | `modelRegistry.complete` is the common need | P1 | |
| E-EX-06 | UI | status-line (32), model-status (31), custom-footer (64), custom-header (73), border-status-editor (150), modal-editor (85), rainbow-editor (88), github-issue-autocomplete (185), widget-placement (9), hidden-thinking-label (53), working-indicator (123), titlebar-spinner (58), notify (57, OSC 777), mac-system-theme (47), timed-confirm (70), overlay-test (153), overlay-qa-tests (1450), doom-overlay (74), snake (343), space-invaders (560), interactive-shell (196) | `ctx.ui.*` broad | Most use TUI-only APIs; games prove overlay and 35 FPS render | P2; only status, widget, notify, title, timed-confirm map to the RPC subset | Test set for section 6.1. |
| E-EX-07 | Input and prompt | inline-bash (94), input-transform (43), input-transform-streaming (39), pirate (47), claude-rules (86), prompt-customizer (49), system-prompt-header (17) | `input`, `before_agent_start`, `systemPromptOptions` | System prompt shaping via extension | P1 | |
| E-EX-08 | Compaction and git | custom-compaction (117), git-checkpoint (53), git-merge-and-resolve (115), auto-commit-on-exit (49) | `session_before_compact`, `tool_result`, `agent_settled`, `session_shutdown`, `exec` | Custom summarizer via `modelRegistry.complete` | P1 | |
| E-EX-09 | Rendering and debug | message-renderer (59), entry-renderer (41), debug-provider (90), provider-payload (18) | renderers, `provider_stream_event`, `before_provider_request` | | P2 | |
| E-EX-10 | Messaging and resources | event-bus (43), file-trigger (41), send-user-message (97), dynamic-resources/ (15) | `pi.events`, `sendMessage`, `resources_discover` | File watcher must start in `session_start` | P1 | |
| E-EX-11 | Providers and routing | jev-router (113), custom-provider-anthropic (617), custom-provider-gitlab-duo (405) | `registerVirtualModel`, `registerProvider` with OAuth and `streamSimple` | | P2 | |
| E-EX-12 | RPC UI demo | rpc-demo (118), `examples/rpc-extension-ui.ts` client | all RPC-supported UI methods | Reference client for section 6.1 | P0 MVP (conformance test) | |
| E-EX-13 | SDK examples | `examples/sdk/01-14`: minimal session, model, prompt override, skills, tools allowlist, extensions, context files, prompt templates, credentials/OAuth, settings, sessions, full control, session runtime, codemode+MCP | | Embedding surface | P2 | |
| E-EX-14 | Chord example plugin | `examples/plugins/pi-example-plugin` (`session.ts`, `tui.ts`) | facets, `SlashCommands`, `AgentController`, `PresentationUI` | | experimental | Read only. |

## 10. Extension API breaking-change history

Source: changelog-early, changelog-late and the extensions report section 15. Cross-checked lines in `CL`.

Timeline of major breaks:

| version | break |
|---|---|
| 0.18.0 | Hooks introduced (TS modules, `--hook`). |
| 0.23.0 | `session_start`/`session_switch` merged into one `session` event; custom tools introduced. |
| 0.24.0 | Custom tools need a directory `index.ts`; `tool_result.result: string` replaced by `content` array and `details`. |
| 0.27.0 | `branch` folded into `session`; `before_*` cancellable variants; `reset`/`switch`/`branch` return `cancelled`. |
| 0.31.0 | Massive: tree sessions (v2), events split again, `pi.send` to `sendMessage`, `ctx.exec` to `pi.exec`, custom tool `execute` signature, many renames; `hookTimeout` removed. |
| 0.32.0 | `queueMessage` to `steer`/`followUp`; `sendMessage` gets an options object. |
| 0.33.0 | `isXxx()` key checks removed, `matchesKey()` instead. |
| 0.35.0 | hooks + custom tools unified into `extensions`; dirs, flags, settings, types renamed (about 25 renames); session v2 to v3. |
| 0.38.0 | `LoadedExtension` to `Extension`; `runtime` object; `ui.custom` factory gains `keybindings`. |
| 0.39.0 | `before_agent_start` returns `systemPrompt`, not `systemPromptAppend`. |
| 0.44.0 | `getAllTools()` returns objects. |
| 0.50.0 | `packages` array separate from `extensions`; `ResourceLoader` only. |
| 0.51.0 | `ToolDefinition.execute` parameter order changed. |
| 0.52.10 | `ContextUsage` nullable; strict `git:` source parsing. |
| 0.55.0 | Resource precedence flipped to project-first; conflict no longer unloads the later extension. |
| 0.59.0 | Tools listed in the prompt only with `promptSnippet`. |
| 0.60.0 | No startup auto-update of unpinned packages. |
| 0.61.0 | Keybinding ids namespaced. |
| 0.62.0 | `sourceInfo` replaces `extensionPath`/`location`/`source`; renderers must return `Component`. |
| 0.63.0 | `getApiKey` to `getApiKeyAndHeaders`. |
| 0.65.0 | `session_switch`, `session_fork`, `session_directory` removed (`session_directory` lived 8 releases); session replacement moved to `AgentSessionRuntime`. |
| 0.69.0 | Stale captured `pi`/`ctx` invalidated; TypeBox 1.x. |
| 0.72.0 | `compat.reasoningEffortMap` to `thinkingLevelMap`. |
| 0.80.8 | `ModelRegistry.refresh()` async; `ModelRuntime` replaces `authStorage`/`modelRegistry` in SDK. |
| 0.83.0 | TypeBox 1.3.7 removes deprecated APIs. |
| 0.84.0 | OAuth `refreshToken(creds, signal)` requires a signal; header null markers. |
| 0.86.0 | `user_bash` fail-closed; provider stream context normalized to `TranscriptContext`. |
| 0.87.0 | `turn_end` boundary fields required; `agent_before_settle` added; `agent_settled` deferral. |
| 0.99.0 | Additive: exposure, `executeTool`, MCP, virtual models, built-in extensions, `provider_stream_event`. |

Which parts churned most (ranked):

1. Session and lifecycle events (0.23, 0.27, 0.31, 0.65, 0.87): merged, split, removed, redefined. Five breaks in about 60 releases.
2. Extension packaging and discovery (0.24, 0.35, 0.50, 0.52.10, 0.55, 0.60): entry point, directory names, flags, settings keys, precedence rule.
3. Model, auth and provider objects (0.63, 0.72, 0.80.8, 0.84, 0.86): keys and OAuth objects changed at least five times.
4. Tool definition and result shape (0.24, 0.31, 0.51, 0.59, 0.62, 0.99): `execute` signature, `result` string to `content`, prompt visibility, renderer contract.
5. Naming and type renames (0.31, 0.35, 0.38, 0.62): about 25 renames in one release.
6. Keybinding ids (0.33, 0.61).

Lessons for Ask's API stability:

1. Design one concept up front: one extension type, one discovery dir, one flag, one settings key. Pi rebuilt this three times in four weeks (0.18 to 0.35) and called the unification its highest-value change.
2. Put an `apiVersion` field in the manifest and in the `Initialize` handshake. Pi has none, so extensions are typed against the host package and break on every upgrade. Add capability negotiation (the client's UI capability set, the events an extension wants).
3. Make payloads additive-only inside a major version (new optional fields, new events). Do not rename events. Pi's `session_directory` event lived 8 releases; `shouldStopAfterTurn` returned as `turn_end` boundary in 0.87.
4. Keep the wire format small and versioned; Pi's experimental protocol package reached wire v8 in two months, an anti-pattern for anything third parties depend on.
5. Version the stored data: session entries changed shape (v2 to v3 in 0.35.0). Extension `custom` entries need `ext_id` plus a `schema_version`.
6. Give a stable `SourceInfo` and a stable error shape from day one (Pi added `sourceInfo` in 0.62.0 after three ad hoc field sets).
7. Mark young surfaces "experimental" in the protocol itself (virtual models, codemode, exposure levels); keep them behind capability flags until they settle. Pi pulled `experimental/plugin` from the published package one release after shipping it by accident (0.85.0 to 0.85.1).
8. Avoid in-place mutation and live object handles in event payloads. They forced 0.69.0's stale-object invalidation.

## 11. Extension runtime options for Go (needs user decision)

Requirements from the event table: 19 events need a synchronous answer; the fail-closed pair (`tool_call`, `user_bash`) needs a defined crash policy; three per-token events need droppable delivery; `tool_call` is on the hot path of every tool execution; UI dialogs need a request/response channel with agent-side timeouts. Runtime data comes from the Go libraries lane (repo metadata verified on 2026-09-30, latency numbers are NOT measured).

| option | sync hooks (19 events) | isolation | reload | author DX | Windows and cross-compile | fit with event table | risk |
|---|---|---|---|---|---|---|---|
| Stdio JSON-RPC subprocess (LSP/MCP style) | Yes: request/response, host awaits; about 0.1 to 1 ms per call after spawn (unmeasured); spawn 10 to 100+ ms | OS process; add sandbox yourself | Restart child | Any language; needs a TS/Python SDK to feel good | Excellent | Covers all 41 events, UI dialogs, streaming. Needs deadlines and a crash policy for fail-closed hooks | Protocol design and versioning cost |
| Embedded JS (goja + esbuild for TS) | Yes, in-process, microseconds | Weak: no OS isolation; CPU/memory limits need `Interrupt()` and own accounting | Cheap | ES5.1 plus most ES6+; no Node APIs beyond shims; TS needs transpile step | Excellent (pure Go) | Fits sync hooks best; cannot run real Pi extensions (no fs/net/child_process); one thread per runtime | Async/await gaps; JS ecosystem expectations |
| WASM (wazero or Extism) | Yes, host functions both ways; microseconds | Strong (capability-based, memory/time limits) | Recompile module | Poor to medium: Rust/TinyGo/JS-in-WASM toolchains; wazero has no component model; Extism Go SDK last push 2025-05 (stagnant) | Excellent (pure Go) | Fits sync hooks; poor for streaming UI dialogs and long-lived I/O; extension cannot exec or open sockets without host functions | Author friction; library stagnation |
| hashicorp go-plugin (gRPC subprocess) | Yes (unary gRPC) | Process only; mTLS and checksum | Restart child | Go-first; TS authors need codegen | Good | Same as stdio JSON-RPC but heavier codegen | MPL-2.0 dependency; poor for TS authors |
| yaegi (Go interpreter) | Yes | None | Yes | Plain Go; generics and new language features incomplete | Excellent | Fits sync hooks | Slow, activity slowed (last push 2026-02-09), no isolation: reject |
| Shell hooks (Claude Code / Crush style) | Yes for a small set: `PreToolUse`-style block, rewrite, inject context, auto-approve | Process per call | Edit a config file | Trivial, any language | Windows shell differences | Covers only `tool_call`, `tool_result`, `input`, `session_*` in practice; no UI dialogs, no state, no registration | Spawn cost per hook on hot path (about 10 ms scale, unmeasured); limited power |
| Compiled-in Go hooks (fx groups) | Yes, zero cost | None (same process) | Rebuild | Go only | Excellent | Fits all events for first-party features | Not an extension system for users |
| Native `plugin` package, v8go | reject | | | | Linux/macOS only, cgo, unmaintained | | |
| Layered: A compiled-in Go hooks; B stdio JSON-RPC subprocess; C shell hooks; D optional WASM or goja later | see above | per tier | | | | A for first-party; B carries the full 41-event contract and the RPC-UI subset; C covers the simple 80% | One protocol to maintain plus a thin shell-hook adapter |

Evidence: the only well-funded Go peer (Crush, 28k stars) ships MCP + LSP + skills + shell `PreToolUse` hooks and no in-process plugin runtime; opencode's original Go version was archived and rewritten in TypeScript, where in-process TS plugins are the norm (go-agent-libraries report section 5). This shows market pull toward in-process TS plugins, the thing that is hard in Go. Crush license is FSL-1.1-MIT: read its design, do not copy code.

Recommendation (ranked):

1. Layered B + A + C. Make stdio JSON-RPC (2.0, MCP-shaped for tools/prompts/resources, plus hook methods such as `hook/tool_call` returning allow/deny/modify) the extension contract. Keep first-party features as compiled Go hooks through the same event bus. Add shell hooks as a zero-dependency adapter that translates a few events into commands.
2. Add WASM (wazero) only if untrusted third-party in-process code becomes a requirement.
3. Add goja + esbuild only if users ask for tiny inline scripts in config.
4. Reject yaegi, v8go, native `plugin`. Use go-plugin only if extensions are Go-only.

Trade-off: the subprocess route gives isolation, crash containment, any-language authors and reuse of Pi's RPC-UI subset, at the price of a round trip per synchronous hook (fine for tool calls; needs a batching or filter mechanism for `context` and `message_update`), a versioned protocol to design, and no ability to run existing Pi TypeScript extensions unchanged. Running Pi extensions unchanged would need a Node/Bun sidecar plus a bridge, which is subprocess again. Mitigation for the hot path: extensions declare in the handshake which events they subscribe to and, for `tool_call`, which tool names they match, so the host never calls an extension that does not care.

Needs user decision: (a) accept the layered B + A + C plan; (b) choose an in-process scripting tier instead (goja) for a smaller first release; (c) require Pi-extension compatibility (Node/Bun sidecar).

Crash policy needs a decision too: when an extension process dies on a fail-closed hook, block the action (Pi default), or allow it with a warning. Pi has no deadlines for hooks; Ask needs a per-call deadline with a documented default.

## 12. Removed or deprecated: do not rebuild

| item | version | why removed or changed | Ask action |
|---|---|---|---|
| `hooks/` and `tools/` directories, `--hook`/`--tool`, `HookAPI`, `CustomToolAPI` | removed 0.35.0 (deprecation warning) | Unified into `extensions` | Build one concept only. |
| `session_start`/`session_switch` merged `session` event | 0.23.0, reversed 0.31.0 | Granular events preferred | Use granular events with a `reason` field. |
| `session_switch`, `session_fork`, `session_directory` events | removed 0.65.0 | Replaced by `session_start.reason` and runtime rebuild | Do not add. |
| `hookTimeout` setting | removed 0.31.0 | "No hook timeouts; use Ctrl+C" | Ask still needs a deadline (server context, no human at Ctrl+C). Decide explicitly. |
| `systemPromptAppend` return | replaced 0.39.0 | Extensions need to read and replace | Use replace plus a structured patch. |
| `tool_result.result: string` | replaced 0.24.0 | Text-only lost images and structure | Use content blocks. |
| `queueMessage` | replaced 0.32.0 | Split into `steer` and `followUp` | Use steer/followUp from day one. |
| `isXxx()` key helpers | removed 0.33.0 | Replaced by `matchesKey()` | TUI lane. |
| `extensionPath`, `location`, `source` fields | replaced 0.62.0 | Uniform `sourceInfo` | Use `SourceInfo`. |
| Packages under `extensions` setting | moved 0.50.0 | `packages` array | Separate from day one. |
| Startup auto-update of unpinned packages | removed 0.60.0 | Surprise mutation, supply chain | Never auto-update. |
| `pi update` updating packages too | changed 0.79.7 | Updates Pi only unless `--all` | Same. |
| `getApiKey` | replaced 0.63.0 | Headers also needed | Return key plus headers. |
| `compat.reasoningEffortMap` | replaced 0.72.0 | `thinkingLevelMap` | Use `thinkingLevelMap`. |
| pi-ai global `stream/complete/getModel/registerApiProvider` on root, `/base` entrypoints | moved 0.80.0 | Provider runtime consolidation | Not relevant to Go. |
| Google Gemini CLI and Antigravity providers, Qwen CLI OAuth example | removed 0.71.0 | Discontinued; no reason given | Do not rebuild. |
| Web UI workspace references in the CLI package | removed 0.75.4 | Web UI dropped | Out of scope for Ask too. |
| `shouldStopAfterTurn` | added 0.72.0, replaced 0.87.0 | Superseded by actionable `turn_end` boundary | Use boundary result. |
| `codemode-deferred` mode; tool lists inside the codemode description | alias/removed Unreleased | Description changed with server tool lists and broke prompt cache | Keep tool lists out of stable prompt text. |
| Experimental `client`, `experimental/plugin` subpaths in published package | pulled 0.85.1 | Accidentally published, broke SDK imports | Anything under `src/experimental` is experimental; do not port. |
| Chord, durable, server, client, protocol, sqlite-node, evals packages | experimental, source-only | Second architecture; wire v8 in two months; durable has zero consumers | Do not port. Ask has gateway, sessions, store. Reference ideas: attachment fencing, no auto replay in client, replay-safe tool rerun. |
| Extension tools cannot be unregistered (workaround) | current design | Design limitation | Provide real unregister. |

Not removed but explicitly not to be copied: jiti/virtual-modules loader, TypeBox peer-dependency rule, npm lifecycle-script install, `location`-style ad hoc source fields, and "first wins / last wins / suffix" inconsistent conflict handling.

## 13. Coverage, limitations, unresolved questions

Coverage: all 41 events (verified by name against `types.ts`), API object methods, contexts, tools, commands, providers, virtual models, UI groups, RPC UI protocol, packages, skills, prompt templates, MCP, codemode, tool search, examples, break history, removed features, runtime options.

Limitations:
- Behavior is from source reading and changelogs in the lane reports; Pi was not run.
- Some "since" values are the first changelog mention found and may lag the true introduction; `message_start/update/end` and `tool_execution_*` are dated to 0.52.x (changelog line "message/tool lifecycle events to extensions"), and `onTerminalInput` was `terminal_input` in that release.
- Latency and library-cost numbers for runtime options are unmeasured. A no-op hook benchmark (about 30 minutes) would settle them.
- Not read: `interactive-mode.ts` UI bind, MCP client internals, Chord internals, `S/exec.ts` `ExecOptions`.
- Whether `pi install` passes `--ignore-scripts` was not found.
- The claim that Crush deprioritized a plugin API is unconfirmed.

Unresolved questions:
1. Runtime tier: accept layered stdio JSON-RPC + compiled Go hooks + shell hooks (section 11)? This is the gate for the roadmap phase on extensions.
2. Must Ask load existing Pi TypeScript extensions unchanged? If yes a Node/Bun sidecar is needed and the Go runtime choice is mostly moot.
3. Trust model: user-authored extensions only, or a third-party marketplace? Third-party pushes toward WASM or OS sandboxing and capability grants.
4. Crash and deadline policy for fail-closed hooks (`tool_call`, `user_bash`): block or allow, and which default deadline.
5. How much extension UI is in scope: only the RPC subset (dialogs, notify, status, widget strings, title, editor text), or also a declarative UI tree for `custom()`?
6. Is Windows a supported daemon target? It affects shell hooks, process-group kill and sandboxing.
7. Ask session store needs the entry taxonomy (`custom`, `custom_message`, `context_edit`, boundary drafts) before extension state and `turn_end` drafts can work. Confirm with the harness-core lane.
8. Should extension state get a private data dir and KV table (Pi has none)?
9. One conflict rule for tools, commands, shortcuts and flags: which one (recommend first wins plus an error diagnostic)?
10. Is `since` accuracy for events dated "0.52.x" worth a targeted changelog grep, or good enough for planning?
