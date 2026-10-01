# Pi extension system: full inventory (lane E)

Date 2026-09-30. Pi commit 2bbfcca43 (Pi 0.99.x). Read-only research for the Go rewrite (Ask).
Path convention: `X/` = `pi/packages/coding-agent/src/core/extensions/` (so `X/types.ts` is `src/core/extensions/types.ts`); `S/` = `src/core/`; `D/` = `docs/`; `E/` = `examples/extensions/`; `CL` = `CHANGELOG.md` (line, version).
Method note: emit sites found by grep over `src/core`, `src/modes`, `src/cli`, `src/main.ts` (GitNexus not used; grep gives the same complete call-site list). Every row carries a source.

---

## 0. Outcome first

- Pi has ONE in-process extension mechanism: a TS module with `export default (pi: ExtensionAPI) => void | Promise<void>`, loaded by jiti, no sandbox, same OS rights (D/extensions.md:5, D/security.md:3). Hooks, tools, commands, shortcuts, flags, renderers, providers, virtual models, MCP servers all go through this one API object. Even Pi's own MCP, codemode, tool-search and llama.cpp support are extensions written against it (`src/extensions/index.ts:7-13`).
- Surface today: 41 event names (`X/types.ts:1545-1612`), 10 `register*` methods (3 with `unregister*` pairs), 10 `ctx.ui` groups (section 7), 5 tool exposure levels. Most of it is the API of a TUI host; a smaller core is host-neutral and is what a Go port must keep.
- Isolation, timeouts, resource limits: none for extensions. Only model-written codemode scripts run in a sandbox (QuickJS-WASM), which is a different thing.
- A second, experimental system exists (Chord facets, `PI_EXPERIMENTAL=1`, out-of-process Session worker plus TUI facet). It is the closest precedent to a Go host and is worth reading before designing (section 12).

### Delta vs prior report (`ask/plans/reports/researcher-260930-1259-pi-extension-system.md`)

The prior report was made from an older tree (`types.ts` 1614 lines; now 2245 lines, `X/types.ts`). It says "30 events". Verified: now 41 `on()` overloads. Line numbers in the prior report are stale. Its core claims still hold (sequential awaited dispatch, tool_call fail-closed, jiti, manual reload, stale-ctx). NEW since then (all verified in source below):

| Area | New since prior report | Source |
|---|---|---|
| Events | `agent_before_settle`, `agent_settled`, `turn_end` boundary result (entries + continue), `ui_prompt_start/end`, `mcp_servers_change`, `provider_stream_event`, `before_provider_headers`, `after_provider_response`, `cache_warming_decision`, `session_compact_failed`, `session_info_changed`, `context_with_system`, `thinking_level_select` | X/types.ts:1545-1612; CL 0.87.0, 0.99.0 |
| Tools | `exposure` (5 levels), `namespace`, `annotations`, `outputSchema` + `structuredContent`, `prepareLoadout`, `defaultActive`, `ctx.executeTool` nested calls, `parentToolCallId`, `nestedCalls` record, `terminate` | X/types.ts:509-645, 368-395; CL 0.99.0 |
| Registration | `registerMcpServer`, `registerVirtualModel`, `registerMarkdownTransformer`, `registerEntryRenderer`, `getSettings`, `getMcpServers`, `ctx.scopedModels`, `ctx.mode`, `ctx.isProjectTrusted` | X/types.ts:1540-1863 |
| Loading | `builtin:<name>` extensions, `replaceable` inline extensions, pre-trust load pass, `project_trust` event | X/types.ts:1992-2020; S/resource-loader.ts:120,497,739 |
| Lifecycle | deferred actions during `agent_settled`; user_bash fail-closed (0.86.0); stale-ctx invalidation on session replacement (0.69.0) | CL 0.87.0, 0.86.0, 0.69.0 |
| Second system | Chord facets plugin runtime (experimental) | section 12 |

---

## 1. Ring 0: load lifecycle

### 1.1 Discovery (what paths become extension paths)

| Step | Rule | Source |
|---|---|---|
| Directory scan (one level) | `*.ts`/`*.js` files load directly; `sub/index.ts|js` loads; `sub/package.json` with `pi.extensions` lists entries. No deeper recursion. | X/loader.ts:737-811 |
| Auto dirs | `<agentDir>/extensions/` (user, default `~/.pi/agent`) and `<cwd>/.pi/extensions/` (project, trust-gated) | D/configuration.md:21,34; S/package-manager.ts:2452 |
| Settings lists | `extensions: string[]` in user/project settings; `!pattern`, `+path`, `-path` overrides; `builtin:<name>` entries | D/settings.md:150-158 |
| CLI | `-e/--extension <path|npm:|git:>` temporary scope; `--no-extensions` disables settings+auto (and built-ins) | D/packages.md:30; CL 0.99.0 |
| Packages | `packages` setting (npm/git/local) resolved to extension paths through the package manifest or `extensions/` dir | S/package-manager.ts:920-990 |
| Legacy helper | `discoverAndLoadExtensions()` exists but the CLI path uses `ResourceLoader` + `PackageManager` | X/loader.ts:816; S/resource-loader.ts:660-700 |

### 1.2 Order (this is handler order and registration precedence)

1. CLI `-e` paths first, then settings/package/auto paths (`mergePaths(cli, enabled)`, defined S/resource-loader.ts:1012; called in both the main load and `loadCurrentExtensionSet` ~666).
2. Within resolved paths: stable sort by `resourcePrecedenceRank`: project local-settings 0, project auto-dir 1, user local-settings 2, user auto-dir 3, any package 4, builtin 5 (S/package-manager.ts:192-198). Ties keep insertion order (project packages before user packages, package-manager.ts:925-935).
3. Then inline SDK factories, appended last (S/resource-loader.ts:763-768). Built-ins load in the final pass because project settings may disable them (resource-loader.ts:665-668 comment).
4. Project-first precedence since 0.55.0 (CL 3166).
5. Duplicate canonical paths are dropped (package-manager.ts:2650-2656).

Handler order = this load order, then registration order inside each extension (`X/runner.ts:268`, `snapshotEventHandlers`).

### 1.3 Loading (TS at runtime)

| Aspect | Behavior | Source |
|---|---|---|
| Loader | `jiti` `createJiti(import.meta.url, {moduleCache:false, ...})`; `jiti.import(path, {default:true})`; must export a function else "does not export a valid factory" | X/loader.ts:547-580 |
| No build step | TS/ESM/CJS run straight from source | D/extensions.md:31 |
| Host module sharing | Extensions import `@earendil-works/pi-coding-agent`, `pi-ai`, `pi-agent-core`, `pi-tui`, `typebox` (+ legacy `@mariozechner/*`, `@sinclair/typebox` aliases). Source runtime: `virtualModules`; compiled binary/SEA: embedded virtual modules with `tryNative:false`; unbundled Node: filesystem `alias` map | X/virtual-modules.ts:1-38; X/loader.ts:36-125,552-563 |
| Factory cache | Cache of factory functions keyed by path, invalidated when cwd changes or `clearExtensionCache()` (called on reload) | X/loader.ts:128-153 |
| Async factory | Awaited before startup continues (`await factory(api)`) | X/loader.ts:601-620; D/extensions.md:47 |
| Load failure | Recorded in `errors[]` as `Failed to load extension: <msg>`; other extensions continue | X/loader.ts:622-650 |
| Registration validation | tool needs an object `parameters`; command needs non-empty name and `handler`; flag default type must match | X/loader.ts:288-345; CL Unreleased ("commands without name or handler fail load") |

### 1.4 Two-phase API and factory state machine

| State | Meaning | Source |
|---|---|---|
| `loading` | Factory running. Registration writes into the `Extension` record. Action methods (`sendMessage`, `appendEntry`, `getActiveTools`, ...) are throwing stubs ("Extension runtime not initialized") | X/loader.ts:156-181,243-262 |
| `commit()` | On factory success: pending flag defaults and pending runtime changes (`registerProvider`, `registerMcpServer`, `registerVirtualModel`, unregisters) are applied; state to `active` | X/loader.ts:243-262 (createExtensionAPI end) |
| `discard()` | On factory throw: state `failed`, event-bus subscriptions made during load are unsubscribed, pending changes dropped. API calls afterwards throw "failed to load and its API is no longer active" | X/loader.ts:243-262 |
| `bindCore` | Runner copies real action implementations into the shared runtime, then flushes queued provider and virtual-model registrations. From now on register/unregister provider is immediate, no `/reload` needed | X/runner.ts:409-543 |
| Rule | Do not start processes/sockets/timers in the factory; start in `session_start`, stop idempotently in `session_shutdown` | D/extensions.md:47-51 |

### 1.5 Isolation

None. In-process, same OS permissions, can read prompts, files, credentials (D/extensions.md:5; D/security.md:3-17). Packages get separate module roots but no sandbox (D/packages.md "Installed packages load with separate module roots"). Project trust is the only gate (section 8).

### 1.6 Reload and staleness

| Item | Behavior | Source |
|---|---|---|
| Trigger | `/reload` or `ctx.reload()` (command context only). No file watcher | S/agent-session.ts:3569-3600; D/extensions.md:37 |
| Sequence | `session_shutdown{reason:"reload"}` to old runner; `oldRunner.invalidate()`; reload settings; `resetApiProviders()`; `resourceLoader.reload()` (fresh jiti import); `_buildRuntime` creates a new `ExtensionRunner` and `ModelRegistry`; carry over CLI flag values; `session_start{reason:"reload"}`; `resources_discover{reason:"reload"}` | S/agent-session.ts:3569-3598 |
| Stale objects | After `invalidate()` any captured `pi` or `ctx` throws a long guidance message; event-bus subscriptions of the old runtime are dropped | X/loader.ts:165-181; X/runner.ts:721-733 |
| Same for session replacement | `ctx.newSession()`, `fork()`, `switchSession()` invalidate captured objects; use the `ReplacedSessionContext` passed to `withSession` | CL 0.69.0 (line 2145); X/types.ts:442 |
| Tools cannot be unregistered | Withdraw by re-registering with `exposure:"hidden"` | D/extensions.md ("Tool exposure") |

---

## 2. Ring 1: capabilities at a glance

| Capability | Entry point | Section |
|---|---|---|
| Observe/modify lifecycle | `pi.on(event, handler)` returns unsubscribe | 4 |
| Model-callable tool | `pi.registerTool(def)` | 5 |
| Slash command | `pi.registerCommand(name, {description, handler, getArgumentCompletions})` | 6 |
| Keyboard shortcut / CLI flag | `pi.registerShortcut`, `pi.registerFlag` + `pi.getFlag` | 6 |
| Terminal UI | `ctx.ui.*`, renderers, editor/footer/header replacement | 7 |
| Providers, models, OAuth | `pi.registerProvider`, `registerVirtualModel` | 9 |
| MCP servers | `pi.registerMcpServer` | 11 |
| Inject messages | `pi.sendMessage`, `pi.sendUserMessage` | 3 |
| Persist state | `pi.appendEntry`, tool-result `details`, `pi.sendMessage` | 3 |
| Session control | `ctx.newSession/fork/navigateTree/switchSession/reload/compact/shutdown` | 3 |
| Cross-extension comms | `pi.events` (EventEmitter bus) | 3 |
| Contribute resources | `resources_discover` returns skill/prompt/theme paths | 4 |
| Distribution | Pi packages (npm/git/local) | 10 |

---

## 3. Ring 2: the object handed to an extension

### 3.1 `ExtensionAPI` (`pi`), X/types.ts:1540-1863

| Method | Signature (short) | Valid during load? | Notes / source |
|---|---|---|---|
| `on` | `(event, handler) => unsubscribe` | yes | 41 overloads; unsubscribe does not affect an in-flight dispatch (snapshot slice) X/runner.ts:268 |
| `registerTool` | `(ToolDefinition)` | yes | later calls trigger `runtime.refreshTools()`; duplicate name in same ext overwrites (Map.set) X/loader.ts:288 |
| `registerCommand` | `(name, {description?, getArgumentCompletions?, handler})` | yes | dynamic: commands are recomputed on every lookup X/runner.ts:798 |
| `registerShortcut` | `(KeyId, {description?, handler(ctx)})` | yes | X/loader.ts:317 |
| `registerFlag` / `getFlag` | `(name, {type:"boolean"|"string", default?, description?})` | yes | `getFlag` returns only flags this extension registered X/loader.ts:330-360 |
| `registerMessageRenderer` | `(customType, (msg, opts, theme) => Component|undefined)` | yes | first registrant wins X/runner.ts:774 |
| `registerEntryRenderer` | `(customType, (entry, opts, theme) => Component|undefined)` | yes | for `custom` entries (not in LLM context) X/runner.ts:788 |
| `registerMarkdownTransformer` | `(md, ctx) => string` | yes | one per extension; all applied X/runner.ts:784 |
| `sendMessage` | `(msg{customType,content,display,details}, {triggerTurn?, deliverAs?: steer|followUp|nextTurn})` | no (stub) | rules below. S/agent-session.ts:2208 |
| `sendUserMessage` | `(content, {deliverAs?: steer|followUp, expandPromptTemplates?})` | no | always triggers a turn; goes through `input` event with `source:"extension"`; templates/commands not expanded by default S/agent-session.ts:2280 |
| `appendEntry` | `(customType, data?)` | no | `custom` session entry, not sent to LLM; emits `entry_appended` S/agent-session.ts:3315 |
| `setSessionName` / `getSessionName` | | no | emits `session_info_changed` S/agent-session.ts:3846 |
| `setLabel` | `(entryId, label|undefined)` | no | bookmark on a session entry |
| `exec` | `(cmd, args, opts?: ExecOptions)` (type in `S/exec.ts`, not read; loader uses `opts.cwd ?? cwd`) | yes (works, unlike others) | plain process exec X/loader.ts `exec` |
| `getActiveTools` / `setActiveTools` | names[] | no | unknown or hidden names ignored |
| `getAllTools` | `ToolInfo[]` incl. `exposure`, `namespace`, `annotations`, `sourceInfo` | no | X/types.ts:2067 |
| `getCommands` | `SlashCommandInfo[]` (extension + prompt + `skill:`) | no | S/agent-session.ts:3260 |
| `getSettings` | copy of merged settings | no | there is no per-extension settings namespace (section 8.3) |
| `setModel` | `(Model) => Promise<boolean>` (false if no auth) | no | session-scoped, does not change defaults |
| `getThinkingLevel` / `setThinkingLevel` | | no | clamped to model capabilities |
| `registerProvider` / `unregisterProvider` | `(name, ProviderConfig)` or native `Provider` | yes (queued) | section 9 |
| `registerMcpServer` / `unregisterMcpServer` / `getMcpServers` | | yes (queued) | section 11 |
| `registerVirtualModel` / `unregisterVirtualModel` | | yes (queued) | section 9 |
| `events` | `{emit(channel,data), on(channel,handler)=>unsub}` | yes | node EventEmitter; handler errors console.error only; tracked and dropped on invalidate S/event-bus.ts:1-33 |

`sendMessage` delivery rules (S/agent-session.ts:2208-2250):
- `deliverAs:"nextTurn"`: queued and attached to the next user prompt.
- streaming, `triggerTurn !== false`: `followUp` uses the follow-up queue, otherwise steer queue.
- not streaming + `triggerTurn`: append and start a run (deferred if inside `agent_settled`).
- streaming + `triggerTurn:false`: held until end of turn so it cannot land between a tool call and its result.
- otherwise append only, emit `message_start/message_end`.

### 3.2 `ExtensionContext` (`ctx`), X/types.ts:325-365

`ui`, `mode` ("tui"|"rpc"|"json"|"print"), `hasUI` (true in tui and rpc), `cwd`, `sessionManager` (read-only), `modelRegistry`, `model`, `scopedModels`, `thinkingLevel?`, `isIdle()`, `isProjectTrusted()`, `signal` (undefined when idle), `abort()`, `hasPendingMessages()`, `shutdown()`, `getContextUsage()` (`tokens`/`percent` may be null after compaction, CL 0.52.10), `compact({customInstructions, onComplete, onError})` (fire-and-forget), `getSystemPrompt()`.
Context is created per dispatch (`createContext()` X/runner.ts:868) and reads live values, so it reflects bindings made after load.

### 3.3 Other contexts

| Context | Adds | Where handed out | Source |
|---|---|---|---|
| `ExtensionToolContext` | `tools` (callable tools), `executeTool(name,args,{signal,onUpdate})` | tool `execute()` 5th arg | X/types.ts:383-395; X/runner.ts:952 |
| `ExtensionCommandContext` | `getSystemPromptOptions`, `waitForIdle`, `newSession`, `fork`, `navigateTree`, `switchSession`, `reload` | command handlers only (calling these from event handlers can deadlock) | X/types.ts:401-435 |
| `ReplacedSessionContext` | command ctx + `sendMessage`, `sendUserMessage` bound to the new session | `withSession(ctx)` callbacks | X/types.ts:442-452 |
| `ProjectTrustContext` | `cwd`, `mode`, `hasUI`, `ui` limited to select/confirm/input/notify | `project_trust` handlers | X/types.ts:678-683 |

### 3.4 Session access and state persistence

| Need | Mechanism | Source |
|---|---|---|
| Read history | `ctx.sessionManager.getEntries()`, `getBranch()`, `getLeafEntry()`, `getLabel()`, `getSessionFile()` (read-only view) | examples: E/todo.ts, E/bookmark.ts |
| Branch-aware state | Store in tool-result `details`; rebuild from `getBranch()` on `session_start` and `session_tree` | D/extensions.md "State" |
| Durable, not in LLM context | `pi.appendEntry(type,data)` (+ `registerEntryRenderer`) | S/agent-session.ts:3315 |
| Durable and in LLM context | `pi.sendMessage` (`custom_message`) | S/agent-session.ts:2208 |
| Outside session | own files/external storage | D/extensions.md |
| Session format | append-only JSONL tree, entries: message, custom, custom_message, context_edit, compaction, branch_summary, label, session_info, model_change, thinking_level_change | D/session-format.md; CL 0.87.0 |

---

## 4. Ring 2: EVERY event (41 names)

Legend. Fire: where emitted. Effect: N = notify only; M = can modify/replace; B = can block/cancel. Merge = how several handlers combine. Await = awaited in load order (A) or fire-and-forget (F). Errors = what happens when a handler throws: C = caught, reported via `ExtensionError`, dispatch continues; FC = fail-closed.
All A events dispatch through `ExtensionRunner.emit*` sequentially over a snapshot of handlers (X/runner.ts:268). No handler has a timeout (grep "timeout" in runner.ts: none).

### 4.1 Startup and resources

| Event | Payload | Fire (source) | Effect / return | Merge | Await / errors |
|---|---|---|---|---|---|
| `project_trust` | `{cwd}`; ctx = limited `ProjectTrustContext` | Pre-trust pass, before project resources load; only user-level and CLI extensions are loaded for it (S/project-trust.ts:57; S/resource-loader.ts:497-515; X/runner.ts:283-312) | B/M: `{trusted:"yes"|"no"|"undecided", remember?}` | first yes/no wins; `undecided` falls through | A; C (errors collected) |
| `resources_discover` | `{cwd, reason:"startup"|"reload"}` | after `session_start` (S/agent-session.ts:3203, 3309) | M: `{skillPaths?, promptPaths?, themePaths?}` | concatenated across handlers; tagged with extension path, scope "temporary" | A; C |
| `session_start` | `{reason:"startup"|"reload"|"new"|"resume"|"fork", previousSessionFile?}` | `bindExtensions` S/agent-session.ts:3194; reload 3597; replacement in runtime | N | none | A; C |
| `mcp_servers_change` | `{servers: RegisteredMcpServer[]}` | when `registerMcpServer`/`unregister` runs after bind (X/runner.ts:459 listener) | N (having a handler marks this extension as the MCP connector) | none | F (`void this.emit`); C |

### 4.2 Session

| Event | Payload | Fire | Effect / return | Merge | Await / errors |
|---|---|---|---|---|---|
| `session_info_changed` | `{name|undefined}` | S/agent-session.ts:3846-3848 | N | | F |
| `session_before_switch` | `{reason:"new"|"resume", targetSessionFile?}` | S/agent-session-runtime.ts:142 | B: `{cancel?}` | `cancel:true` short-circuits; otherwise last defined result kept | A; C |
| `session_before_fork` | `{entryId, position:"before"|"at"}` | S/agent-session-runtime.ts:159 | B: `{cancel?, skipConversationRestore?}` | same | A; C |
| `session_before_compact` | `{preparation, branchEntries, customInstructions?, reason:"manual"|"threshold"|"overflow", willRetry, signal}` | S/agent-session.ts:2708 (manual), 3040 (auto) | B/M: `{cancel?, compaction?: CompactionResult}` (extension supplies the summary) | cancel short-circuits; last result wins | A; C |
| `session_compact` | `{compactionEntry, fromExtension, reason, willRetry}` | S/agent-session.ts:2773, 3103 | N | | A; C |
| `session_compact_failed` | `{reason, errorMessage?, aborted, willRetry, fromExtension}` | S/agent-session.ts:1014 | N | | A; C |
| `session_before_tree` | `{preparation: TreePreparation, signal}` | S/agent-session.ts:3928 | B/M: `{cancel?, summary?, customInstructions?, replaceInstructions?, label?}` | same as above | A; C |
| `session_tree` | `{newLeafId, oldLeafId, summaryEntry?, fromExtension?}` | S/agent-session.ts:4043 | N | | A; C |
| `session_shutdown` | `{reason:"quit"|"reload"|"new"|"resume"|"fork", targetSessionFile?}` | `emitSessionShutdownEvent`: runtime teardown S/agent-session-runtime.ts:171,405; reload S/agent-session.ts:3578 | N (cleanup) | | A; C |

### 4.3 Agent loop and model I/O

| Event | Payload | Fire | Effect / return | Merge | Await / errors |
|---|---|---|---|---|---|
| `input` | `{text, images?, source:"interactive"|"rpc"|"extension", streamingBehavior?}` | start of `prompt()` (extension commands `/x` are dispatched BEFORE `input` handlers run: S/agent-session.ts:1893; D/prompt-templates.md:33), S/agent-session.ts:1842 (X/runner.ts:1511) | M/B: `{action:"continue"}`, `{action:"transform", text, images?}`, `{action:"handled"}` | transforms chain (each sees prior text); `handled` short-circuits (prompt not sent) | A; C |
| `before_agent_start` | `{prompt, images?, systemPrompt (readonly getter), systemPromptOptions (mutable, shared object)}` | S/agent-session.ts:1977 (X/runner.ts:1411) | M: `{message?: custom msg to inject, systemPrompt?: string}` | messages from all handlers collected; `systemPrompt` sets `forceSystemPrompt` on shared options, last wins; handlers may also mutate `systemPromptOptions` in place | A; C |
| `agent_start` | `{}` | S/agent-session.ts:1245 | N | | A |
| `turn_start` | `{turnIndex, timestamp}` | S/agent-session.ts:1252 | N | | A |
| `context` | `{messages}` (system messages hidden) | before each LLM call, S/sdk.ts:415 (X/runner.ts:1289) | M: `{messages?}` or edit in place | chained over a structuredClone; Pi restores system messages after each handler | A; C |
| `context_with_system` | `{messages}` (full transcript) | after all `context` handlers, same call | M: `{messages?}` | chained; result used as returned; dropped leading system message is reported but honored | A; C |
| `before_provider_request` | `{payload}` (provider wire payload) | S/sdk.ts:359-362 (X/runner.ts:1352) | M: return any non-undefined value replaces payload | chained | A; C |
| `before_provider_headers` | `{headers}` | S/sdk.ts:338 (X/runner.ts:1383) | M by in-place mutation; return ignored; `null` deletes a header | shared mutable object | A; C |
| `after_provider_response` | `{status, headers}` | S/sdk.ts:364-372 | N | | A |
| `provider_stream_event` | `{provider, api, model, data}` (parsed, pre-normalization; read-only) | S/sdk.ts:374-386 | N | | A (slow handler delays stream); C |
| `cache_warming_decision` | `{action, ...}` | S/cache-warmer.ts:309; sdk.ts:314 | M: `{action:"warm"|"stop"}` | last returned action wins | A; C |
| `message_start` | `{message}` | S/agent-session.ts:1261 | N | | A |
| `message_update` | `{message, assistantMessageEvent}` (delta) | S/agent-session.ts:1268 | N | | A (per token; backpressure) |
| `message_end` | `{message}` | S/agent-session.ts:1274-1290 | M: `{message?}` same role required | chained; role mismatch reported and skipped; null content normalized | A; C |
| `turn_end` | `{turnIndex, message, toolResults, messageEntryId, toolResultEntryIds, entries, continue, context, outcome}` | boundary: `finishTurn` hook, S/agent-session.ts:806-830 (X/runner.ts:1020) | M: `{entries?: custom|custom_message|context_edit|compaction drafts, continue?: true}` | each handler replaces `entries`/`continue`; context preview rebuilt after every handler; a preview build failure drops all entries and forces `continue:false` | A; C |
| `agent_before_settle` | same boundary state, no message | S/agent-session.ts:1813 | M: same as `turn_end` (one continuation request) | same | A; C |
| `agent_end` | `{messages}` | S/agent-session.ts:1247 | N | | A |
| `agent_settled` | `{}` | S/agent-session.ts:1042 (`_emitAgentSettled`) | N; runs requested from handlers are deferred until all settled handlers finish | | A |
| `ui_prompt_start` / `ui_prompt_end` | `{reason:"ui_prompt", kind: select|confirm|input|editor|custom, title?}` | X/runner.ts:569-616 (only outermost nested prompt) | N | | F (`queueMicrotask`) |
| `model_select` | `{model, previousModel, source:"set"|"cycle"|"restore"}` | S/agent-session.ts:2378 | N | | A |
| `thinking_level_select` | `{level, previousLevel}` | S/agent-session.ts:2544 | N | | F (`void`) |

### 4.4 Tools and bash

| Event | Payload | Fire | Effect / return | Merge | Await / errors |
|---|---|---|---|---|---|
| `tool_call` | `{toolName, toolCallId, parentToolCallId?, input}` typed per built-in (bash, powershell, read, edit, write, grep, find, ls) else `Record<string,unknown>`; `isToolCallEventType()` guard | agent hook `_beforeToolCall`, S/agent-session.ts:615-640; also nested calls (parent id set) | B/M: `{block?, reason?, terminate?}`; mutate `event.input` in place to patch args (no re-validation) | later handlers see mutations; last defined result wins unless `block:true`, which returns immediately | A; **FC**: no try/catch in `emitToolCall` (X/runner.ts:1233-1250), `_beforeToolCall` rethrows, the tool is blocked (D/extensions.md "Errors and cleanup") |
| `tool_execution_start/update/end` | `{toolCallId, toolName, args|result|partialResult, isError, parentToolCallId?}` | S/agent-session.ts:1289-1320; nested via `_executeNestedToolCall` :670-705 | N | | A; C |
| `tool_result` | `{toolCallId, toolName, input, content, structuredContent?, details, isError, usage?, parentToolCallId?}` typed per built-in | `_afterToolCall`, S/agent-session.ts:642-690 (X/runner.ts:1174) | M: `{content?, details?, structuredContent?, isError?, usage?}` | field-by-field compose; replacing `content` without `structuredContent` deletes structured content | A; C |
| `user_bash` | `{command, excludeFromContext, cwd}` (user `!`/`!!`) | interactive S/modes/interactive/interactive-mode.ts:6788; rpc S/modes/rpc/rpc-mode.ts:562 (X/runner.ts:1253) | B/M: `undefined` = pass on; `{operations: BashOperations}` or `{result: BashResult}` = handled | first defined result wins; invalid result throws | A; **FC** since 0.86.0 (error reported then rethrown, command not run locally) |

Blind-spot check: `grep` of `src/modes`, `src/cli`, `src/main.ts` shows extension event emission only for `user_bash` (modes) and `project_trust` (`S/project-trust.ts`). All others come from `S/agent-session.ts`, `S/agent-session-runtime.ts`, `S/sdk.ts`, `S/cache-warmer.ts`.

---

## 5. Tools: registration, execution, rendering

### 5.1 `ToolDefinition` (X/types.ts:565-645)

| Field | Purpose |
|---|---|
| `name`, `label`, `description` | LLM name, UI label, LLM description |
| `parameters` | TypeBox schema, must be a plain object schema (X/loader.ts:290) |
| `promptSnippet`, `promptGuidelines` | line in "Available tools" and bullets in Guidelines; without `promptSnippet` the tool is omitted from that section (CL 0.59.0) |
| `prepareArguments(args)` | compatibility shim before validation |
| `constrainedSampling` | provider-side constrained decoding config |
| `outputSchema` | JSON Schema of `structuredContent` |
| `exposure` | `direct` (default) / `model-only` / `codemode` / `deferred` / `hidden` (X/types.ts:509) |
| `namespace` | `{name, description?, instructions?}` grouping (MCP server) |
| `annotations` | MCP-style hints readOnly/destructive/idempotent/openWorld (unverified) |
| `defaultActive` | activation on registration; only meaningful for direct/model-only |
| `prepareLoadout(loadout)` | orchestrating tool adjusts descriptions and `hiddenDeclarations` of other tools |
| `executionMode` | `"sequential"` or `"parallel"` per tool |
| `renderShell` | `"default"` or `"self"` |
| `execute(toolCallId, params, signal, onUpdate, ctx)` | result `{content, details, structuredContent?, isError?, usage?, terminate?}`; throw = error result; parameter order fixed in 0.51.0 (CL 3540) |
| `renderCall(args, theme, ctx)` / `renderResult(result, {expanded,isPartial}, theme, ctx)` | TUI components; if defined they must return a `Component` (CL 0.62.0) |

Helpers: `defineTool()` (type inference only, X/types.ts:656), `withFileMutationQueue()` (serialize file edits), `wrapRegisteredTool()` adapts to `AgentTool` (X/wrapper.ts).

### 5.2 Execution semantics

| Item | Behavior | Source |
|---|---|---|
| Validation | Arguments validated against schema, then `tool_call` hooks may mutate without re-validation | X/types.ts:1198-1203 |
| Parallelism | Tool calls of one assistant message may run in parallel unless `executionMode:"sequential"` | D/extensions.md "Events and concurrency" |
| Nested calls | `ctx.executeTool()` runs through validation and `tool_call`/`tool_result`; ids `<parent>/<n>`; events carry `parentToolCallId`; bounded `nestedCalls` record on parent result (256 calls, 8 KiB args, 32 KiB result); usage rolled up | D/extensions.md; S/nested-tool-calls.ts |
| `terminate:true` | skip automatic follow-up only if every finalized tool in the batch agrees | D/extensions.md |
| Override built-ins | register a tool with a built-in name; first registration wins on conflict | E/tool-override.ts; X/runner.ts:629 |
| Active set | `setActiveTools`; changes recorded as transcript deltas; providers that cannot express a delta get a full checkpoint (cache prefix may break) | D/extensions.md "Activate tools dynamically" |

### 5.3 Exposure matrix

| Exposure | Declared to model | Callable via `executeTool` | Activated on register | Listed by codemode |
|---|---|---|---|---|
| direct | when active | when active | yes | no (unless codemode mode "only") |
| model-only | when active | never | yes | no |
| codemode | only if explicitly activated | always | no | yes |
| deferred | after `tool_search` loads it | always | no | no (found by `tool_search`) |
| hidden | no | no | no | no |

---

## 6. Commands, shortcuts, flags

| Item | Behavior | Source |
|---|---|---|
| Command dispatch | `prompt()` checks `text.startsWith("/")` first; runs extension command immediately even while streaming; command manages its own LLM interaction via `sendMessage`/`sendUserMessage`; a throw becomes `ExtensionError{extensionPath:"command:<name>", event:"command"}` | S/agent-session.ts:1893, 2031-2055 |
| Not allowed in queue | `steer`/`followUp` reject extension commands | D/rpc-commands.md:48,72; S/agent-session.ts:2186 |
| Duplicate command names | Both stay; invocation names `name:1`, `name:2` (unique-suffix logic); no conflict error | X/runner.ts:798-838 |
| Completions | `getArgumentCompletions(prefix)` returns `AutocompleteItem[]|null` (sync or async) | X/types.ts:1521 |
| Slash namespaces | extension commands, prompt templates (`/name`), skills (`/skill:name`), built-ins (`BUILTIN_SLASH_COMMANDS`, 24 entries) | S/slash-commands.ts:20-44; S/agent-session.ts:3260-3290 |
| Shortcut conflicts | built-in with `restrictOverride:true` blocks the extension shortcut (warning); non-restricted built-ins are overridden (warning); two extensions: last registered wins (warning); diagnostics list | X/runner.ts:672-720 |
| Flags | typed boolean/string; CLI values stored in shared `flagValues`; first registration wins; conflicts reported as errors; values survive reload | X/runner.ts:652-670; S/resource-loader.ts:1238-1273; S/agent-session.ts:3546 |

---

## 7. UI primitives (`ctx.ui`, X/types.ts:149-300)

| Group | Methods | Mode support |
|---|---|---|
| Dialogs | `select(title, options[], {signal, timeout})`, `confirm(title, message, opts)`, `input(title, placeholder, opts)`, `editor(title, prefill)` | tui + rpc (rpc via sub-protocol) |
| Notifications | `notify(message, "info"|"warning"|"error")` | tui + rpc |
| Status/footer | `setStatus(key, text|undefined)`, `setFooter(factory)` (gets `FooterDataProvider` with git branch and statuses), `setHeader(factory)`, `setTitle` | status/title in rpc; footer/header tui only |
| Widgets | `setWidget(key, string[] | factory, {placement:"aboveEditor"|"belowEditor"})` | string[] in rpc; factory tui only |
| Working indicator | `setWorkingMessage`, `setWorkingVisible`, `setWorkingIndicator({frames, intervalMs})`, `setHiddenThinkingLabel` | tui only |
| Custom components | `custom(factory(tui, theme, keybindings, done), {overlay, overlayOptions, onHandle})` returns `Promise<T>`; overlays with anchors/margins/stacking | tui only; rpc returns `undefined` |
| Editor | `setEditorText`, `getEditorText`, `pasteToEditor`, `setEditorComponent(factory)` (extend `CustomEditor` to keep app keybindings), `getEditorComponent`, `addAutocompleteProvider(wrapper)` | tui; rpc only `setEditorText` |
| Raw input | `onTerminalInput(handler => {consume?, data?})` | tui only |
| Theme | `theme`, `getAllThemes`, `getTheme`, `setTheme(name|Theme)` | tui only |
| Tool output | `getToolsExpanded`, `setToolsExpanded` | tui only |

Extension prompts are wrapped so `ui_prompt_start/end` fire for select/confirm/input/editor/custom (X/runner.ts:569-616). Non-TUI modes use a no-op UI (`noOpUIContext`: `confirm` returns false, `select`/`input` return undefined, X/runner.ts:314-350).
Renderer hooks outside `ctx.ui`: `registerMessageRenderer`, `registerEntryRenderer`, `registerMarkdownTransformer`, tool `renderCall/renderResult`.
Key API note: keybinding ids are namespaced (`app.tools.expand` etc.) since 0.61.0 (CL 2771); `ctx.ui.custom` factory gained `keybindings` in 0.38.0 (CL 4250); key detection moved to `matchesKey()` in 0.33.0 (CL 4658).

### 7.1 RPC extension UI protocol (D/rpc-extension-ui.md; src/modes/rpc/rpc-types.ts:252-297; rpc-mode.ts:85-320)

Transport: JSONL on stdio, LF-delimited only (CL 0.57.0).

| Direction | Message | Fields |
|---|---|---|
| Pi to client | `{type:"extension_ui_request", id, method:"select"}` | `title, options[], timeout?` |
| | `method:"confirm"` | `title, message, timeout?` |
| | `method:"input"` | `title, placeholder?, timeout?` |
| | `method:"editor"` | `title, prefill?` |
| | `method:"notify"` | `message, notifyType?` (fire-and-forget) |
| | `method:"setStatus"` | `statusKey, statusText?` |
| | `method:"setWidget"` | `widgetKey, widgetLines?, widgetPlacement?` (string arrays only) |
| | `method:"setTitle"` | `title` |
| | `method:"set_editor_text"` | `text` |
| Client to Pi | `{type:"extension_ui_response", id, value}` | select/input/editor |
| | `{..., id, confirmed}` | confirm |
| | `{..., id, cancelled:true}` | any dialog; yields `undefined` or `false` |

Semantics: id is a random UUID; dialogs resolve to a default on `timeout` or `AbortSignal` (agent-side, client need not track timeouts); `custom()` returns undefined; `onTerminalInput` no-op; footer/header/editor component/autocomplete/working-indicator/theme methods are no-ops; `ctx.hasUI` is true in RPC and `ctx.mode` is "rpc". Also `get_commands` RPC returns extension commands with `sourceInfo` (D/rpc-commands.md:788-830). Example client: `examples/rpc-extension-ui.ts`; demo extension `E/rpc-demo.ts`.

---

## 8. Settings, trust, errors, security

### 8.1 Project trust (D/security.md:25-80; S/project-trust.ts)

| Item | Behavior |
|---|---|
| Gated | `.pi/extensions|skills|prompts|themes`, project packages, `.pi/settings.json`, `.pi/mcp.json`, SYSTEM.md files |
| Not gated | context files AGENTS.md/CLAUDE.md (still untrusted input) |
| Decision order | 1) user-level and CLI extensions' `project_trust` handlers (first yes/no wins), 2) saved decision in `~/.pi/agent/trust.json` (closest parent), 3) `defaultProjectTrust` (`ask`/`always`/`never`, agent-dir settings only) |
| Non-interactive | print/json/rpc cannot prompt; `always` loads, `ask`/`never` skips |
| `--approve/-a`, `--no-approve/-na` | one-command override (D/cli.md:286) |
| Built-ins | load after trust is resolved, so cannot handle `project_trust` (X/types.ts:2011-2018) |

### 8.2 Error handling matrix

| Failure | Result | Source |
|---|---|---|
| Factory throws or import fails | extension skipped, error listed; API discarded | X/loader.ts:601-650 |
| Event handler throws (most events) | `ExtensionError{extensionPath,event,error,stack}` sent to listeners; next handler runs | X/runner.ts:736-750 |
| `tool_call` handler throws | tool blocked (fail-closed) | X/runner.ts:1233-1250; S/agent-session.ts:626-640 |
| `user_bash` handler throws or invalid result | command aborted, error rethrown | X/runner.ts:1253-1287 |
| Boundary entry invalid | all entries of that boundary dropped, continue false; error "Invalid boundary entries" | X/runner.ts:1020-1105 |
| Tool `execute()` throws | error tool result to model | D/extensions.md |
| Command handler throws | error reported, command counted as handled | S/agent-session.ts:2046-2053 |
| `sendMessage`/`sendUserMessage` async failure | reported as `<runtime>` errors `send_message`/`send_user_message` | S/agent-session.ts:3302-3320 |
| Provider/virtual-model registration throws on flush | `register_provider`/`register_virtual_model` error, others continue | X/runner.ts:439-520 |
| `registerMcpServer` with no connector | `register_mcp_server` error per server | X/runner.ts:751-763 |
| Use of stale `pi`/`ctx` | throws guidance message | X/loader.ts:165-181 |
| Event-bus subscriber throws | `console.error` only | S/event-bus.ts:15-27 |
| Timeouts | none for extension handlers or tools | grep of runner.ts |

Synthetic `extensionPath` values: `<runtime>`, `<boundary>`, `command:<name>`, skill file path (`skill_expansion`); sources of built-ins are `builtin:<name>` (CL 0.99.0).

### 8.3 Settings for extensions

There is no per-extension settings namespace. Extensions read merged Pi settings via `pi.getSettings()` (copy) and keep their own config in their own files (examples: E/preset.ts reads `presets.json`; E/sandbox reads its own config; E/claude-rules scans `.claude/rules`). Extension flags are the only declared config surface. Built-in extension toggles: `extensions: ["-builtin:mcp"]`, `defaultTools: ["+codemode"]`, `codemode.mode`, `autoEnableCodemode` in `mcp.json` (D/settings.md:41,158; D/mcp.md).

### 8.4 Security facts

- No sandbox; extension code equals user code (D/security.md:3-17). Documented isolation only via OS/VM boundary around the whole process; example E/sandbox and E/gondolin route tools/bash into a sandbox or micro-VM through `registerTool` overrides and `user_bash`.
- Package install runs `git clone` then `npm install --omit=dev` (with `--omit=peer` or `--legacy-peer-deps` variants) in the checkout when `package.json` exists (S/package-manager.ts:1891-1920, 1820-1866). No `--ignore-scripts` found by grep, so dependency lifecycle scripts run at install. Not verified by execution.
- Npm project installs go under `.pi/npm`, user installs under `<agentDir>/npm` (package-manager.ts:2087-2093). Git install root paths not read.
- Versioned npm specs and git tags/commits are pinned; `pi update` does not move a pinned ref (D/packages.md). Startup no longer auto-updates unpinned packages (CL 0.60.0).

---

## 9. Providers, models, virtual models

| Item | Detail | Source |
|---|---|---|
| `registerProvider(name, ProviderConfig)` | `baseUrl`, `apiKey` (literal, `$ENV`, `!command`), `api`, `headers`, `authHeader`, `models[]` (replaces all models of that provider; only `baseUrl` = override existing), `streamSimple(model, context, options)` custom stream handler, `images`, `classifiers`, `refreshModels(ctx)`, `oauth{name, login, refreshToken(creds, signal), getApiKey, modifyModels?}` | X/types.ts:1876-1932 |
| Native provider | `registerProvider(provider: Provider)` (pi-ai object) | X/types.ts:1803 |
| Model types | `chat` (reasoning, contextWindow, maxTokens, cost, thinkingLevelMap, promptCache, compat, samplingParams), `image`, `classifier` | X/types.ts:1934-1987 |
| Timing | queued during load, flushed in `bindCore`; immediate afterwards; `unregisterProvider` restores overridden built-ins | X/runner.ts:439-543; X/types.ts:1759-1819 |
| `streamSimple` obligations | must call `options.onPayload` (then `before_provider_request` applies), `options.onResponse`, may call `onProviderStreamEvent`; context is a normalized transcript, read prompt/tools via `getCurrentSystemPrompt()`/`getCurrentTools()` | X/types.ts:1886-1894; CL 0.86.0 |
| `ctx.modelRegistry` | `find`, `complete`, `streamSimple`, `classify`, `findOfType`, `hasConfiguredAuth`, `refresh()` (async since 0.80.8), `getApiKeyAndHeaders()` (was `getApiKey`, CL 0.63.0) | examples: E/custom-compaction.ts, E/summarize.ts, E/jev-router.ts |
| Virtual models | `registerVirtualModel({provider,id,name,route(request, ctx) => ModelRoute})`; selection stored as `model_change`, dispatch recorded on each assistant message; router state comes from session branch | X/types.ts:1856,1870; D/virtual-models.md:1-40 |
| OAuth examples | E/custom-provider-anthropic (617 lines, own streaming), E/custom-provider-gitlab-duo (405, reuses pi-ai streams via proxy) | examples |
| Docs | D/custom-provider.md | |

---

## 10. Packages, skills, prompt templates

### 10.1 Package format and CLI (D/packages.md; D/cli.md:248-288; S/pi-manifest.ts)

| Item | Detail |
|---|---|
| Manifest | `package.json` key `pi`: `{extensions[], skills[], prompts[], themes[]}` (arrays, globs, `!` exclusions; `+path`/`-path` exact); no manifest = conventional dirs `extensions/ skills/ prompts/ themes/`. Keyword `pi-package` for gallery; `pi.image`/`pi.video` previews. Parser accepts only string arrays (S/pi-manifest.ts:13-33) |
| Deps | runtime deps in `dependencies`; host-provided (`pi-ai`, `pi-agent-core`, `pi-coding-agent`, `pi-tui`, `typebox`) as `peerDependencies: "*"`, never bundled; Pi warns when listed in `dependencies` (CL 0.99.0) |
| Sources | `npm:name[@ver]`, `git:host/path[@ref]` (bare `github.com/...` needs `git:` prefix since 0.52.10, CL 3249), `https://` URLs as git, local paths (not copied) |
| Commands | `pi install <src> [-l]`, `pi remove|uninstall <src>`, `pi list`, `pi update [<src>|--extensions|--models|--all|--self]`, `pi config [--local]` (enable/disable per resource) |
| Settings | `packages: (string | {source, extensions?, skills?, prompts?, themes?, autoload?})[]` in user or project settings; filters narrow but never widen the manifest |
| Identity/dedupe | npm by name, git by repo URL without ref, local by resolved path; project entry replaces user entry (or acts as delta with `autoload:false`) |
| Install | npm: install into managed root (`--prefix`/`--cwd`, peer skipped); git: `clone`, `checkout ref`, npm install; missing sources auto-installed on resolve (`onMissing` install/skip/error) |
| Migration | packages moved from `extensions` to `packages` array in 0.50.0 (CL 3758) |
| Project packages | installed and loaded only after project trust |

### 10.2 Skills (D/skills.md; S/skills.ts)

| Item | Detail |
|---|---|
| Format | Agent Skills spec: directory with `SKILL.md`, frontmatter `name` (<=64 chars, lowercase/digits/hyphen), `description` (<=1024, required), optional `license`, `compatibility`, `metadata`, `allowed-tools`, `disable-model-invocation`; assets/scripts/references alongside (S/skills.ts:11-14,95-123,341) |
| Loading | Startup scans locations; only name+description+path go into the system prompt as `<available_skills>`; model reads `SKILL.md` on demand (S/skills.ts:355-380) |
| Locations | `~/.pi/agent/skills`, `.pi/skills` (trust-gated), `~/.agents/skills`, `.agents/skills` (cwd and ancestors up to repo root), `skills` setting, packages, `resources_discover` |
| Recursion | directories with `SKILL.md` discovered recursively, `.gitignore`/`.ignore`/`.fdignore` respected (S/skills.ts:16) |
| Invocation | `/skill:name args` expands to `<skill name location>` block plus args (S/agent-session.ts:2065-2085); setting `enableSkillCommands`; `disable-model-invocation` hides from prompt |
| Collisions | first discovered wins, warning; malformed skills not loaded |
| Not an extension | no code execution surface; it is instructions plus files |

### 10.3 Prompt templates (D/prompt-templates.md; S/prompt-templates.ts)

Markdown file with frontmatter `description`, `argument-hint`; filename = command. Substitutions `$1..`, `$@`/`$ARGUMENTS`, `${1:-default}`, `${@:-default}`, `${@:N}`, `${@:N:L}`; shell-like quoting. Conventional dirs load direct `.md` children only; settings/packages can select nested files. `input` event sees raw text before template expansion; extension commands win over same-name templates. Reload with `/reload`.

---

## 11. MCP and codemode

| Item | Detail | Source |
|---|---|---|
| Client package | `packages/mcp`: standalone client, stdio + Streamable HTTP transports, no official SDK, `toLlmContent()`; legacy SSE rejected | packages/mcp/README.md:1-45; D/mcp.md |
| Built-in extension | `builtin:mcp` (replaceable): reads `~/.pi/agent/mcp.json` and `.pi/mcp.json` (trust-gated; project entry replaces user entry), connects on `session_start`, registers tools `mcp__<server>__<tool>` with `namespace`, `annotations`, `exposure`, plus resource tools `list_mcp_resources`, `list_mcp_resource_templates`, `read_mcp_resource`, and `/mcp` command | src/extensions/mcp/index.ts:239-320,792-902 |
| Config keys | stdio: `command,args,env,cwd`; http: `url,headers,oauth{clientId,clientSecret,callbackPort,callbackUrl,scope,clientName}`; common: `timeout` (s, default 60), `enabled`, `exposure`, `toolExposure` (glob map), `description`; `${ENV}` and `!command` value resolution | D/mcp.md:44-75 |
| Exposure default | `codemode`; `deferred` = via `tool_search`; `direct` = declared; `hidden`. `codemode-deferred` alias for `codemode` | D/mcp.md:150-190; CL Unreleased |
| Permissions | MCP calls go through the normal `tool_call`/`tool_result` pipeline, so permission extensions cover them; codemode calls carry `parentToolCallId` | D/mcp.md "Permissions" |
| Extension-added servers | `pi.registerMcpServer(name, config)`: session-scoped, not persisted, re-register each load, `mcp.json` entry of same name wins, names owned by another extension throw; connector is whichever extension handles `mcp_servers_change`; `getMcpServers()` for alternative MCP extensions | D/extensions.md "MCP servers"; X/loader.ts registerMcpServer |
| Replace built-in | any extension that registers `/mcp`, `codemode`, or `tool_search` supersedes the built-in (`replaceable` logic) | S/resource-loader.ts:120-160 |
| OAuth | dynamic client registration, tokens in `~/.pi/agent/mcp-auth.json`, log in `~/.pi/agent/mcp.log` (rotates at 5 MB) | D/mcp.md |
| Codemode | model writes JS run in QuickJS compiled to WASM in a worker; only capability is `tools.<name>()`, `ALL_TOOLS`, `text`, `image`, `exit`, `store/load`, `searchTools`, `describeTool`, `describeNamespace`; no timers, fetch, require; memory cap; timeout; nested calls do not enter LLM context | packages/codemode/README.md:1-60 |
| Tool search | `tool_search` tool (registered inactive) finds and declares `deferred` tools; recorded in transcript so the set survives `/tree` and resume | src/extensions/tool-search/index.ts:14; D/mcp.md:160 |
| SDK | SDK sessions do not load built-ins; add `createMcpExtension()`, `createCodemodeExtension()`, `createToolSearchExtension()` to `extensionFactories` and call `bindExtensions()` | D/sdk.md "codemode-mcp"; E/sdk/14-codemode-mcp.ts |

Size of the built-in support (5035 lines across mcp, codemode, tool-search extension dirs) shows how much host logic a Go MCP layer needs.

---

## 12. Second system: Chord facets (experimental)

| Item | Detail | Source |
|---|---|---|
| Enablement | `PI_EXPERIMENTAL=1` | S/experimental.ts:1-3 |
| Concept | Application-composition runtime: plugins split into facets (e.g. `session` worker facet and `tui` facet), typed services (singleton or keyed), replicated state, remote-service boundary with strict-JSON calls; facets bundled separately and run where they belong | packages/chord/README.md:1-50; packages/chord/src/api.ts:1-110 |
| Example | `examples/plugins/pi-example-plugin`: `session.ts` provides `ExampleFacetService` with replicated state; `tui.ts` uses `SlashCommands`, `AgentController`, `PresentationUI` host services to register `/hello` | examples/plugins/pi-example-plugin/src/*.ts; src/experimental/plugin.ts:1-60 |
| Host services offered to facets | `AgentController`, `PresentationUI`, `SlashCommands` (only three exported) | src/experimental/plugin.ts |
| Reload | server rebuilds package, reloads Session-worker generation atomically, serves new TUI artifact | example README |
| Sources | `pi -e <package>` selects plugin for a Session; server keeps per-Session facets so resume needs no args | example README |
| Status | experimental, tiny host surface, separate from the 41-event API. Not a replacement for it today | this report |

---

## 13. Ring 3: interactions

| Interaction | Behavior | Source |
|---|---|---|
| Extension x compaction | `session_before_compact` can cancel or supply the whole `CompactionResult`; `session_compact`/`session_compact_failed` observe; `turn_end`/`agent_before_settle` boundary drafts can add `compaction` entries; `ctx.compact()` starts one; compaction reasons `manual|threshold|overflow`; extension summarizer via `ctx.modelRegistry.complete` | S/agent-session.ts:2708-2790,3040-3110; E/custom-compaction.ts |
| Extension x session tree | `session_before_tree` may replace the branch summary; `session_tree` after; `ctx.navigateTree` command-only; branch-aware state must be rebuilt from `getBranch()` | S/agent-session.ts:3928,4043 |
| Extension x fork/new/switch | `session_before_*` can cancel; `withSession` gets fresh ctx; old ctx stale; `session_shutdown` with `reason` and `targetSessionFile` on the old runner, `session_start` with `reason` on the new | S/agent-session-runtime.ts:142-175 |
| Extension x agent loop | `turn_end`/`agent_before_settle` return `continue:true` for one more request; guard loops | D/extensions.md; X/types.ts:953-985 |
| Extension x system prompt | prefer mutating `systemPromptOptions` sections; returning `systemPrompt` forces the whole prompt for the run; transcript records structured sections, appends deltas | D/extensions.md; X/runner.ts:1411-1463 |
| Extension x TUI | all `ctx.ui` component factories get live `TUI`/`Theme`; `ui_prompt_*` events wrap dialogs | section 7 |
| Extension x RPC | degraded UI subset; commands run via `prompt`; `get_commands`; `session_before_*` cancel is visible in RPC responses ("If an extension canceled") | D/rpc-commands.md:132-150,594-660 |
| Extension x SDK | `DefaultResourceLoader` accepts `extensionFactories` (inline, optionally named, `replaceable`, `builtin`) and `additionalExtensionPaths`; `noExtensions`; call `bindExtensions()` to fire `session_start` | D/sdk.md; E/sdk/06-extensions.ts |
| Extension x MCP/codemode | permission gates cover nested calls via `parentToolCallId` | D/mcp.md |
| Extension x agent-settled | runs requested from `agent_settled` handlers are deferred until all handlers finish (no reentrant `agent_start`) | CL 0.87.0 (line 159); S/agent-session.ts:1042-1060 |

---

## 14. Ring 4: edge cases

| Case | Behavior | Source |
|---|---|---|
| Two extensions register same tool | both loaded; first in load order wins at lookup; error entry `Tool "x" conflicts with <path>` | X/runner.ts:629; S/resource-loader.ts:1238-1266; CL 0.55.0 |
| Same flag | first wins, conflict error | S/resource-loader.ts:1262 |
| Same command | both kept, `name:1`, `name:2` | X/runner.ts:798 |
| Same shortcut | last wins with warning; restricted built-ins block | X/runner.ts:672 |
| Same renderer customType | first wins | X/runner.ts:774,788 |
| `replaceable` extension shares any tool/command/flag name | dropped entirely, warning for built-ins | S/resource-loader.ts:120-160 |
| Unsubscribe during dispatch | in-flight dispatch unaffected (handler list snapshot) | X/runner.ts:268 |
| Handler order across extensions | load order (section 1.2); within extension registration order | |
| Mutation semantics differ per event | in-place mutation: `tool_call.input`, `before_provider_headers.headers`, `systemPromptOptions`, `context` messages (detected by comparing); return-value replacement: most others | X/runner.ts |
| Handler removes leading system message | reported, honored | X/runner.ts:1330-1345 |
| Untyped extension returns null content | normalized to `[]` | S/agent-session.ts:1279-1290 |
| Reload while running | reload clears cache, new runner; flag values survive; active tools preserved and all extension tools included | S/agent-session.ts:3569-3595 |
| Extension registers tool during load and after | after: `refreshTools()` makes it available immediately | X/loader.ts:288-300 |
| Slow `message_update`/`provider_stream_event` handler | delays stream consumption | D/extensions.md; S/agent-session.ts:1268 |
| Tool call arguments over limits in nestedCalls | omitted, `complete:false` | D/extensions.md |
| Node version | >= 22.19.0 (0.75.0); source runs with Node type stripping (0.99.0) | CL 0.75.0, 0.99.0 |
| Extension loaded without session | some invocations (e.g. `pi mcp`, `pi list`) load no extensions; factories must be side-effect free | D/extensions.md:47; D/mcp.md |

---

## 15. Ring 5: history of API breaks (CHANGELOG)

Versions ran 0.20 to 0.99.1 with frequent breaking changes in extension-facing API.

| Version | Break | CL line |
|---|---|---|
| 0.20.0 | Skills must be `SKILL.md` in a directory | 5517 |
| 0.23.0 | Hooks: `session_start`/`session_switch` merged into `session` event with `reason` | 5433 |
| 0.24.0 | Custom tools require `index.ts` entry point | 5353 |
| 0.27.0 | Session hooks redesign: `branch` merged into `session` (`before_branch`/`branch`) | 5187 |
| 0.32.0 | `sendMessage` second param became `{triggerTurn, deliverAs}` | 4725 |
| 0.33.0 | Key detection `isXxx()` removed; use `matchesKey` | 4658 |
| 0.35.0 | `hooks` and `customTools` unified into `extensions`; `--hook`/`--tool` became `-e`; dirs `hooks/`, `tools/` became `extensions/`; `commands/` became `prompts/` | 4584 |
| 0.38.0 | Rename `LoadedExtension` to `Extension`; `runtime` object; `ctx.ui.custom` gains keybindings arg; runner constructor/initialize signatures | 4250 |
| 0.50.0 | `packages` array separates npm/git from `extensions`; `ResourceLoader` only | 3758 |
| 0.51.0 | `ToolDefinition.execute` parameter order changed | 3540 |
| 0.52.10 | `ContextUsage` tokens/percent nullable; strict git source parsing (`git:` prefix) | 3249 |
| 0.55.0 | Resource precedence project-first; conflicts no longer unload later extension | 3166 |
| 0.59.0 | Tools appear in prompt "Available tools" only with `promptSnippet` | 2843 |
| 0.60.0 | Unpinned packages no longer auto-updated at startup | 2811 |
| 0.61.0 | Keybinding ids namespaced | 2771 |
| 0.62.0 | `sourceInfo` replaces `extensionPath`/`location`/`source`; `renderCall/renderResult` must return Component | 2707 |
| 0.63.0 | `getApiKey` became `getApiKeyAndHeaders` | 2635 |
| 0.65.0 | Removed `session_switch`/`session_fork` events (use `session_start.reason`), `session_directory` | 2487 |
| 0.69.0 | Stale-object invalidation after session replacement; TypeBox 1.x | 2144-2145 |
| 0.72.0 | `compat.reasoningEffortMap` became `thinkingLevelMap` | see CHANGELOG section for that version |
| 0.80.8 | `ModelRegistry.refresh()` async; `ModelRuntime` replaces `authStorage`/`modelRegistry` SDK options | 996 |
| 0.83.0 | TypeBox 1.3.7 removes deprecated APIs | 783 |
| 0.84.0 | `getApiKeyAndHeaders` returns `null` header markers; OAuth `refreshToken(creds, signal)` required signal | 591-594 |
| 0.86.0 | `user_bash` fail-closed; provider stream context normalized to `TranscriptContext` | see CHANGELOG section for that version |
| 0.87.0 | `turn_end` boundary fields required; `agent_before_settle` added; `agent_settled` deferral | 158-159 |
| 0.99.0 | Additive: tool exposure, executeTool, MCP, virtual models, builtin extensions, `provider_stream_event` | 42-120 |

Note: hooks were merged into one `session` event in 0.23.0 and 0.27.0 but are per-event names again today; the split back happened before 0.65.0 (exact version not located).

Pattern: about one API break every 2 to 4 minor releases; hooks were renamed and merged repeatedly in the first 40 releases; since 0.62 the churn is mostly around models/auth and session model. No versioned extension API contract exists: extensions are typed against the same package that hosts them, and TS types are the only contract.

---

## 16. Examples matrix (`examples/extensions`, 79 files and dirs plus README; API used per file, grouped)

Legend: on = events; api = `pi.*`; ui = `ctx.ui.*`; ctx = context members. Source is each file in `E/`.

### Safety, gating, trust
| Example | on | api | ui | ctx |
|---|---|---|---|---|
| permission-gate.ts (34L) | tool_call | | select | |
| protected-paths.ts (30L) | tool_call | | notify | |
| confirm-destructive.ts (59L) | session_before_fork, session_before_switch | | confirm, select, notify | sessionManager.getEntries |
| dirty-repo-guard.ts (56L) | session_before_fork, session_before_switch | exec | select, notify | |
| project-trust.ts (64L) | project_trust, session_start | | select, input, notify | |
| sandbox/ (321L) | session_start, session_shutdown, user_bash | registerTool, registerCommand, registerFlag, getFlag | setStatus, notify | OS sandbox via `@anthropic-ai/sandbox-runtime` |
| gondolin/ (531L) | before_agent_start, session_start, session_shutdown, user_bash | registerTool, registerCommand | setStatus, notify | routes built-ins and `!` into micro-VM |
| plan-mode/ (390L) | agent_end, before_agent_start, context, session_start, tool_call, turn_end | registerCommand, registerFlag, getFlag, registerShortcut, setActiveTools, getActiveTools, appendEntry, sendMessage, sendUserMessage | select, editor, notify, setStatus, setWidget | sessionManager.getEntries |

### Tools
| Example | on | api | ui | ctx |
|---|---|---|---|---|
| hello.ts (26L) | | registerTool | | |
| todo.ts (297L) | session_start, session_tree | registerTool, registerCommand | custom, notify | sessionManager.getBranch (state in details) |
| dynamic-tools.ts (74L) | session_start | registerTool, registerCommand | notify | tools added at runtime |
| structured-output.ts (65L) | | registerTool | | `terminate:true` |
| tool-override.ts (144L) | | registerTool, registerCommand | notify | overrides `read` with logging/ACL |
| built-in-tool-renderer.ts (225L) | | registerTool | | renderCall/renderResult for built-ins |
| minimal-mode.ts (354L) | | registerTool | | collapsed rendering of built-ins |
| truncated-tool.ts (195L) | | registerTool | | ripgrep with 50KB/2000-line truncation |
| bash-spawn-hook.ts (30L) | | registerTool | | custom bash spawn |
| ssh.ts (220L) | before_agent_start, session_start, user_bash | registerTool, registerFlag, getFlag | setStatus, notify | pluggable remote operations |
| question.ts (286L) / questionnaire.ts (448L) | | registerTool | custom | multi-question UI as a tool |
| subagent/ (1038L) | | registerTool | confirm | spawns isolated agents; ships agent definitions and prompts |
| with-deps/ (32L) | | registerTool | | own package.json, jiti module resolution |
| tools.ts (146L) | session_start, session_tree | registerCommand, setActiveTools, getActiveTools, getAllTools, appendEntry | custom, notify | sessionManager.getBranch |
| tic-tac-toe.ts (1008L) | before_agent_start, session_start, session_tree | registerTool, registerCommand, registerMessageRenderer, sendMessage, appendEntry, setSessionName | custom, notify | `executionMode:"sequential"` |

### Commands and UI
| Example | on | api | ui | ctx |
|---|---|---|---|---|
| commands.ts (72L) | | registerCommand, getCommands | select, confirm, notify | |
| preset.ts (436L) | before_agent_start, session_start, turn_start | registerFlag, getFlag, registerCommand, registerShortcut, setModel, setThinkingLevel, setActiveTools, getActiveTools, getAllTools, appendEntry | custom, notify, setStatus | modelRegistry.find, sessionManager.getEntries |
| handoff.ts (190L) | | registerCommand | custom, editor, setEditorText, notify | newSession, modelRegistry.complete, sessionManager.getBranch/getSessionFile |
| qna.ts (118L) | | registerCommand | custom, setEditorText, notify | modelRegistry.complete |
| summarize.ts (199L) | | registerCommand | custom, notify | modelRegistry.complete/find/hasConfiguredAuth |
| status-line.ts (32L) | session_start, turn_start, turn_end | | setStatus | |
| model-status.ts (31L) | model_select | | setStatus, notify | |
| custom-footer.ts (64L) / custom-header.ts (73L) | session_start | registerCommand | setFooter, setHeader, setStatus, notify | sessionManager.getBranch |
| border-status-editor.ts (150L) | agent_start, agent_settled, session_start, session_shutdown | exec | setEditorComponent, setFooter | getContextUsage |
| modal-editor.ts (85L) / rainbow-editor.ts (88L) | session_start | | setEditorComponent | `CustomEditor` subclasses |
| github-issue-autocomplete.ts (185L) | session_start | exec | addAutocompleteProvider, notify | |
| widget-placement.ts (9L) | session_start | | setWidget | |
| hidden-thinking-label.ts (53L) | session_start | registerCommand | setHiddenThinkingLabel, notify | |
| working-indicator.ts (123L) / working-message-test.ts (25L) | session_start | registerCommand | setWorkingIndicator, setWorkingMessage, setStatus | |
| titlebar-spinner.ts (58L) | agent_start, agent_settled, session_shutdown | | setTitle | |
| notify.ts (57L) | agent_settled | | | OSC 777 desktop notification |
| mac-system-theme.ts (47L) | session_start, session_shutdown | | setTheme | |
| timed-confirm.ts (70L) | | registerCommand | confirm, select, notify | AbortSignal/timeout dialogs |
| overlay-test.ts (153L) / overlay-qa-tests.ts (1450L) / doom-overlay/ (74L) | | registerCommand | custom (overlay), notify, setEditorText | overlay anchors, stacking, 35 FPS render |
| snake.ts (343L) / space-invaders.ts (560L) | | registerCommand, appendEntry | custom, notify | sessionManager.getEntries (game state) |
| interactive-shell.ts (196L) | user_bash | | custom | full-terminal child process |
| shutdown-command.ts (63L) | | registerCommand, registerTool, exec | | shutdown |
| reload-runtime.ts (37L) | | registerCommand, registerTool, sendUserMessage | | reload |
| rpc-demo.ts (118L) | session_before_switch, session_start, tool_call, turn_start, turn_end | registerCommand | confirm, input, select, editor, notify, setStatus, setWidget, setTitle, setEditorText | all RPC-supported UI |
| session-name.ts (27L) | | registerCommand, setSessionName | notify | |
| bookmark.ts (50L) | | registerCommand, setLabel | notify | sessionManager.getEntries/getLabel |
| trigger-compact.ts (50L) | turn_end | registerCommand | notify | compact, getContextUsage |

### Input, prompt, messages, compaction, providers, misc
| Example | on | api | ui | ctx |
|---|---|---|---|---|
| inline-bash.ts (94L) | input | exec | notify | expands `!{cmd}` |
| input-transform.ts (43L) / input-transform-streaming.ts (39L) | input | exec | notify | `streamingBehavior` |
| pirate.ts (47L) | before_agent_start | registerCommand | notify | system prompt append |
| claude-rules.ts (86L) | before_agent_start, session_start | | notify | |
| prompt-customizer.ts (49L) | before_agent_start | | | mutate `systemPromptOptions` |
| system-prompt-header.ts (17L) | agent_start, session_shutdown | | setStatus | getSystemPrompt |
| custom-compaction.ts (117L) | session_before_compact | | notify | modelRegistry.complete/find |
| git-checkpoint.ts (53L) | turn_start, tool_result, agent_settled, session_before_fork | exec | select, notify | sessionManager.getLeafEntry |
| git-merge-and-resolve.ts (115L) | agent_end | exec, sendUserMessage | notify | |
| auto-commit-on-exit.ts (49L) | session_shutdown | exec | notify | sessionManager.getEntries |
| message-renderer.ts (59L) | | registerMessageRenderer, registerCommand, sendMessage | | |
| entry-renderer.ts (41L) | | registerEntryRenderer, appendEntry, registerCommand | | |
| debug-provider.ts (90L) | provider_stream_event, message_end, turn_start, turn_end | registerEntryRenderer, appendEntry, registerCommand | setStatus, notify | |
| provider-payload.ts (18L) | before_provider_request, after_provider_response | | | |
| event-bus.ts (43L) | session_start | events.on, events.emit, registerCommand | notify | |
| file-trigger.ts (41L) | session_start | sendMessage | notify | watcher started in `session_start` |
| send-user-message.ts (97L) | | sendMessage, sendUserMessage, registerCommand | notify | |
| dynamic-resources/ (15L) | resources_discover | | | returns skill/prompt/theme paths |
| jev-router.ts (113L) | | registerVirtualModel | | modelRegistry.classify/find/findOfType |
| custom-provider-anthropic/ (617L), custom-provider-gitlab-duo/ (405L) | | registerProvider | | OAuth + streamSimple |

SDK examples (`examples/sdk/01-14`, README): minimal session, model, prompt override, skills, tools allowlist, extensions (logging, blocking, result modification, inline factories), context files, prompt templates, credentials/OAuth, settings, sessions, full control, session runtime (replace session safely), codemode+MCP.

---

## 17. Hard parts for Go

Pi's design assumes extension code shares the host process, the host's live objects, and a UI toolkit. A Go host cannot copy that. What each piece demands:

1. Dynamic code loading. No Go equivalent of jiti. Options ranked for Ask: (a) out-of-process extension over JSON-RPC/gRPC (Ask already has gRPC and WS; Pi's own Chord and MCP are the precedent); (b) embedded interpreter (goja, or wazero/QuickJS-WASM which Pi already uses for codemode); (c) compile-time Go registration through fx groups for first-party features. Go `plugin` package is not viable cross-platform.
2. Live-object surfaces do not cross a process boundary: `setFooter/setHeader/setWidget(factory)/setEditorComponent/custom()` component factories, `renderCall/renderResult`, `registerMessageRenderer`, `ctx.modelRegistry.streamSimple`, `registerProvider({streamSimple})` stream adapters, `ctx.sessionManager` reads, `ctx.executeTool`. Pi's RPC mode already defines the degraded, serializable subset (section 7.1). That subset is the natural wire contract; TUI-specific pieces stay in a client.
3. In-place mutation semantics (`tool_call.input`, `before_provider_headers`, `systemPromptOptions`, in-place `context` edits) must become explicit replace or patch results across a process boundary.
4. Per-event merge rules must be reproduced exactly (section 4): chain, first-wins, last-wins, short-circuit-on-cancel, concatenate. Include the fail-closed set (`tool_call`, `user_bash`) and the fire-and-forget set (`mcp_servers_change`, `ui_prompt_*`, `thinking_level_select`, `session_info_changed`).
5. No timeouts exist in Pi, but a remote/subprocess extension needs deadlines, cancellation (`ctx.signal`), backpressure policy for per-token events (`message_update`, `provider_stream_event`), and a crash policy (a dead extension process on a fail-closed hook).
6. Two-phase lifecycle (load registers only; actions after bind) plus stale-context invalidation must be enforced by the host, not left to code discipline. Generation counters per runtime are the cheap equivalent of `invalidate()`.
7. Ordering and conflict rules are load-bearing and inconsistent (tool first-wins, shortcut last-wins, command suffixes). Decide one rule for Ask up front and document it.
8. Trust is two-pass: user/CLI extensions load first to decide `project_trust`, then project resources load. Built-ins load after trust. Reproduce as a staged loader.
9. Session model coupling: branch-aware state depends on the JSONL tree with `custom`/`custom_message`/`context_edit`/`compaction` entries and boundary drafts validated against a context preview. Ask's store (GORM/SQLite) needs an equivalent entry taxonomy before extension state can persist the same way.
10. Distribution: npm/git package installs run lifecycle scripts and load host-bundled modules with version coupling. A Go host would ship extensions as manifests plus binaries/WASM or scripts, with an explicit API version field (Pi has none and paid for it, section 15).
11. Security: Pi has no boundary. Go with process or WASM isolation can add capability grants; keep MCP-style annotations and route every tool (built-in, extension, MCP, nested) through one pipeline so permission hooks see everything.
12. Provider adapters: `streamSimple` requires calling back into host hooks (`onPayload`, `onResponse`, stream events); out of process this becomes a bidirectional stream protocol.

Recommended cut for Ask (ranked): Tier A compiled fx-registered Go hooks for first-party; Tier B subprocess JSON-RPC with the RPC-UI subset and section 4 events as the contract; Tier C embedded WASM/JS only if untrusted user scripts are required. Skills, prompt templates and MCP are pure data/protocol features and port with low risk.

---

## Limitations

- Did not run Pi or execute any example; behavior is from source reading.
- GitNexus not used; call sites found with grep (complete for `extensionRunner.emit*`, `runner.emit*`).
- Not read line by line: `interactive-mode.ts` (UI bind), `session-manager.ts`, `settings-manager.ts`, MCP client internals, Chord internals, `src/core/tools/*`, themes/keybindings docs.
- Package install root paths for git checkouts not confirmed; lifecycle-script behavior is inferred from command lines only.
- Line numbers for functions in `loader.ts`/`runner.ts` cited from grep output; some `agent-session.ts` lines are approximate to the emit site region.

## Unresolved questions

1. Does any Pi extension API version or capability negotiation exist beyond package semver? (None found; `peerDependencies "*"` is the only signal.)
2. Whether `pi install` for git/npm passes `--ignore-scripts` anywhere (not found by grep of package-manager.ts).
3. Exact semantics of `registerShortcut` collection timing outside interactive mode (only `getShortcuts()` seen).
4. How Chord will map onto the 41-event API, or whether it will replace it (no statement in the repo).
5. Whether Ask needs TUI-parity extensions at all, given Ask's TUI (`cmd/tui`) imports no internal package (Ask CLAUDE.md): this decides how much of section 7 is in scope.

Status: DONE_WITH_CONCERNS
Summary: Inventory of Pi's extension system written from source at commit 2bbfcca43, with all 41 events, API objects, tools, UI, RPC UI protocol, packages, skills, MCP, examples matrix, changelog break history and Go hard parts.
Concerns: Behavior verified by reading only (nothing executed); GitNexus was replaced by grep; git/npm install script behavior unconfirmed.
