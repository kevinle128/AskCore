# Pi harness core: feature inventory for the Go rewrite (lane F)

Date: 2026-09-30. Pi repo `/Users/dale/Desktop/workspace/opensources/pi`, commit `2bbfcca43`, packages at version 0.99.1.
Method: direct source and doc reads. GitNexus was not used; the files were small enough to read directly.

Path shorthand (all under `/Users/dale/Desktop/workspace/opensources/pi/packages/`):
`A:` = `agent/src/`, `AI:` = `ai/src/`, `C:` = `coding-agent/src/`, `CD:` = `coding-agent/docs/`, `AD:` = `agent/docs/`.
"CL" = CHANGELOG.md of the package named. "Go:" = optional note for the rewrite.

Scope note: interactive TUI, extension internals, `packages/{chord,client,codemode,durable,mcp,server,telemetry,tui}` are out of lane. Extension and MCP/codemode appear only where they change core behavior.

---

## 0. Answer to the coordinator's added question: which session store ships?

**The shipping coding-agent uses the older `SessionManager` JSONL tree (session file version 3). The "harness v4" lane-based store exists in `packages/agent` but is used only by code under `src/experimental`, which is excluded from the published build.**

| Fact | Evidence |
|---|---|
| The SDK entry that every mode uses builds `Agent` (the plain wrapper) and imports `SessionManager` from `core/session-manager.ts`. | `C:core/sdk.ts:2` (`Agent`), `C:core/sdk.ts:18` (`SessionManager`) |
| `AgentSession` imports only `Agent`-level pieces (`runToolCall`, `AgentState`, `PrepareNextTurnContext`) from `pi-agent-core`. It never imports `AgentHarness`, `Session`, `SessionRepo`, or `JsonlSessionRepo`. | `C:core/agent-session.ts:25-33` |
| Current session version is 3; the writer is `SessionManager._persist` (create with `wx`, then `appendFileSync`). | `C:core/session-manager.ts:41`, `:1172-1188` |
| `AgentHarness` is imported in coding-agent only under `src/experimental/*` (`session-worker.ts:14`, `services/worker.ts:14`). | grep result, `C:experimental/session-worker.ts:14,542,834` |
| `src/experimental` and `src/cli/experimental` are excluded from the TypeScript build and from the npm `files` list. | `coding-agent/tsconfig.build.json:19`, `coding-agent/package.json:32-33` (`!dist/experimental`, `!dist/cli/experimental`) |
| Runtime gate for the experimental commands is `PI_EXPERIMENTAL=1`. | `C:core/experimental.ts:1-3`, `C:experimental/commands.ts:94` |
| The v4 lane-based `Session`/`SessionStorage`/`SessionRepo` replaced the older harness session model in **pi-agent-core** 0.84.0 (not 0.87.0; the coordinator's "0.87.0" pointer is the coding-agent CL entry that re-lists it as "inherited"). The 0.87.0 entry in coding-agent CL is about `ContextEditEntry` and making `SessionManager` canonical. | `agent/CHANGELOG.md:96-104` (0.84.0), `coding-agent/CHANGELOG.md:145-165` (0.87.0), `coding-agent/CHANGELOG.md:656` |
| The harness spec itself says WP00-WP07 are implemented but several parts are missing (JSONL snapshot compaction, `watchSession`, search, schema migrations). | `AD:harness.md` section 0.9 |
| CL 0.87.0 states `SessionManager` is now the canonical source of provider context for `AgentSession`; assigning `agent.state.messages` no longer replaces history. | `coding-agent/CHANGELOG.md:157` |

Consequence for the Go rewrite: the on-disk format to match is the v3 entry tree in section 12, not the harness "three stores" model (entries, values/lists, usage ledger with operation state). The harness model is the better long-term reference for crash recovery (intent/settlement commits, `replay: "never" | "safe"` per tool), and it is the only place where Pi solves "resume after process death mid-tool". See section 17.

---

## 1. Verification of prior research (deltas against commit 2bbfcca43)

Prior reports: `ask/plans/reports/researcher-260930-1259-pi-ai-agent-core.md` (R-core) and `researcher-260930-2018-pi-reference-for-open-questions.md` (R-open). Both were checked against current source.

| Prior claim | Status now | Evidence |
|---|---|---|
| 9 known APIs | **Outdated.** 10: `pi-messages` (Radius gateway wire format) was added. Plus 1 image API and 3 classifier APIs. | `AI:types.ts:17-37` |
| 35 built-in providers | **Outdated.** `builtinProviders()` now lists 42 factory calls; `KnownProvider` union has 43 ids. | `AI:providers/all.ts:136`, `AI:types.ts:39-80` |
| Loop callback `shouldStopAfterTurn` | **Removed in agent 0.87.0.** Replaced by `finishTurn` (`{action:"end"|"continue"}`), `prepareRequest`, and `prepareNextTurn`. | `agent/CHANGELOG.md:18-32`, `A:types.ts:155-165` |
| "Retry lives only in coding-agent; pi-ai has no retry loop" | **Partly outdated.** Agent-turn retry is still in `AgentSession`. But `ai/utils/retry.ts` now has a reusable `retryAssistantCall` (used for compaction and branch-summary calls) and a `RetryPolicy`. Provider-level SDK retry exists behind `retry.provider.maxRetries` (default 0). | `AI:utils/retry.ts:122,185,246`, `C:core/agent-session.ts:3611,3670` |
| Message types: user, assistant, toolResult | **Incomplete.** A `system` role now exists in the transcript (prompt sections, tool add/remove). | `AI:types.ts:522-538`, `AI:types.ts:610` |
| Session tree, `/tree`, `/fork`, `/clone`, `branch_summary` (R-open T3) | **Confirmed.** Also new since: `context_edit`, `usage` entries. | `CD:session-format.md`, `C:core/session-manager.ts:80,175` |
| "PI has no reconnect; provider streams not resumable" (R-open T4) | **Confirmed for the shipping product.** The harness adds `assistant_frame` persistence for replay display only, and `deferred` requests exist in `pi-ai`. | `AD:harness.md` section 0.6; `AI:types.ts:353,500-520` |
| Anthropic subscription via third-party harness is billed as extra usage (R-open T7) | **Confirmed; Pi warns.** Setting `warnings.anthropicExtraUsage`. | `CD:settings.md` (Updates section) |
| Prior line numbers | **All shifted.** Use the ones in this report. | n/a |

---

## 2. Ring 0: one prompt from input to final answer (shipping path)

Steps use `AgentSession.prompt` (`C:core/agent-session.ts:1883`) and the loop (`A:agent-loop.ts:163`).

1. **Entry.** CLI resolves mode (`C:main.ts:112-122`): `--mode rpc` > `--mode json` > print if `--print` or stdin/stdout is not a TTY > interactive. Piped stdin is prepended to the first prompt; `@file` args become text or image content (`C:cli/file-processor.ts`).
2. **Extension command check.** A leading `/` is tried as an extension command first; it runs immediately, even during streaming (`agent-session.ts:1890-1900`).
3. **Guard.** Rejects while compaction runs (`:1902-1906`).
4. **Input hook.** Extensions may transform or consume the input (`_runInputHandlers`, `:1909`).
5. **Expansion.** `/skill:name args` expands to the skill body; `/template args` expands a prompt template (`:1920-1924`, `:2062`).
6. **Queueing rule.** If already streaming, `streamingBehavior` must be `steer` or `followUp`, otherwise it throws (`:1927-1940`).
7. **Preflight.** Flush pending bash and custom messages; require a model; require configured auth (OAuth-specific error message if refresh failed) (`:1943-1964`).
8. **Stale-usage compaction check.** If the last assistant message was an aborted or errored turn near the limit, compact before sending (`:1968-1972`).
9. **`before_agent_start` hook.** Extensions can change system-prompt options and inject custom messages (`:1975-1990`).
10. **Message assembly.** User message (text plus resized images) + pending "nextTurn" messages + extension messages; system-prompt or tool-loadout diffs are prepended as a `system` message (`:1992-2040`).
11. **`agent.prompt`** starts `runAgentLoop`. Loop, per turn (`agent-loop.ts:163-330`):
    1. Poll steering queue once at start.
    2. Optional `prepareNextTurn` (compaction runs here), then `turn_start`.
    3. Emit queued/prepared messages as `message_start`/`message_end`; auto-insert a system message declaring tool adds/removes (`declareToolChanges`, `:333`).
    4. `prepareRequest`: `AgentSession` swaps in the canonical context from `SessionManager.buildSessionProjection()`, routes virtual models, and runs threshold compaction (`agent-session.ts:746-811`).
    5. `transformContext` then `convertToLlm` then `normalizeContext` then `getApiKey` (fresh per call) then stream function (`agent-loop.ts:381-430`).
    6. Stream events become `message_start` / `message_update` / `message_end` (`:432-476`).
    7. `stopReason` of `error` or `aborted` ends the run at once (`:220-234`).
    8. Tool calls: if `stopReason == "length"` every tool call fails with a fixed error text (`:478-506`); otherwise execute (parallel by default) (`:508-529`).
    9. Append tool results, run `finishTurn`, emit `turn_end`. `end` stops; else poll steering; loop while tool calls or steering exist.
    10. When idle: poll follow-ups; if any, loop again; else `agent_end`.
12. **Persistence.** On every `message_end`, `AgentSession._handleAgentEvent` appends a `message` (or `custom_message`) entry (`agent-session.ts:1096-1125`). Nothing is buffered until the end of the run.
13. **Post-run.** `agent_end` then recovery: retry on transient error, or compact-and-retry on overflow, then extension `agent_settled` (`:1040-1050`). `agent_settled` means no more automatic work.
14. **Output.** Print mode writes assistant text blocks of the last message; exit code 1 if `stopReason` is `error` or `aborted` (`C:modes/print-mode.ts:131-153`).

---

## 3. Ring 1a: agent loop (packages/agent)

| # | Feature | Behavior | Source | Go note |
|---|---|---|---|---|
| 3.1 | Two loop entry points | `agentLoop` (new prompt messages) and `agentLoopContinue` (from existing transcript; last message must be user or toolResult). | `A:agent-loop.ts:38,71,102,128` | One `Run(ctx, cfg, msgs)`; `Continue` shares the body. |
| 3.2 | Turn definition | One assistant response plus its tool calls and results. | `A:types.ts:511-513` | |
| 3.3 | Two-level loop | Inner: tool calls or pending steering. Outer: follow-ups after natural stop. | `A:agent-loop.ts:171-330` | |
| 3.4 | Steering queue | `steer(msg)`; injected after current turn's tools finish, before next LLM call. Tool calls of the current message are not skipped. | `A:agent.ts:299`, `A:types.ts:229-243` | Channel or slice plus mutex. |
| 3.5 | Follow-up queue | `followUp(msg)`; delivered only when the agent would stop. | `A:agent.ts:304`, `A:agent-loop.ts:314-322` | |
| 3.6 | Queue modes | `all` drains everything; `one-at-a-time` (default for both) drains the oldest. | `A:types.ts:55`, `A:agent.ts:247-248` | |
| 3.7 | Steering poll after long prep | After `prepareNextTurn`, polls again only if the earlier poll returned nothing (avoids two messages in one turn under one-at-a-time). | `A:agent-loop.ts:186-191` | Subtle; keep. |
| 3.8 | Tool execution modes | `parallel` (default) or `sequential`. A single tool with `executionMode: "sequential"` forces the whole batch sequential. | `A:agent-loop.ts:508-522`, `A:agent.ts:253` | |
| 3.9 | Parallel semantics | Preflight (lookup, arg prepare, validate, `beforeToolCall`) is sequential in source order; execution starts only after all preflights; `tool_execution_end` emits in completion order; tool-result messages emit in source order. | `A:agent-loop.ts:586-682`, `A:types.ts:40-46` | Use `errgroup`; keep the ordering rule. |
| 3.10 | Tool pipeline | find tool (`Tool X not found` error result) then `prepareArguments` shim then schema validation then `beforeToolCall` (may block, with reason and `terminate`) then `execute(id, params, signal, onUpdate)` then `afterToolCall` (field-wise override). Any throw becomes an error result. | `A:agent-loop.ts:707-777`, `:820-903` | |
| 3.11 | Abort inside tool pipeline | After `beforeToolCall` and before execute, `signal.aborted` yields error result `"Operation aborted"`; remaining unstarted calls in a parallel batch get the same. | `A:agent-loop.ts:731-760,614-624` | |
| 3.12 | Early termination | `terminate: true` on results; the run stops after the batch only if **every** result in the batch sets it. | `A:agent-loop.ts:689-691`, `A:types.ts:66-81` | |
| 3.13 | Truncated output guard | `stopReason == "length"` with tool calls: all fail with "hit the output token limit ... Re-issue". Tool-call JSON is salvaged by a partial parser, so truncation is otherwise silent. | `A:agent-loop.ts:478-506` | Important safety rule. |
| 3.14 | Error/aborted ends run | Assistant `error` or `aborted` gives `turn_end` then `agent_end`; `finishTurn` still called but its decision is ignored. | `A:agent-loop.ts:220-234` | |
| 3.15 | Hooks | `transformContext`, `convertToLlm`, `getApiKey`, `prepareRequest`, `prepareNextTurn`, `finishTurn`, `beforeToolCall`, `afterToolCall`, `getSteeringMessages`, `getFollowUpMessages`. Contract: hooks must not throw. | `A:types.ts:193-318` | Go interfaces or func fields. |
| 3.16 | `finishTurn` | `{action:"end"}` stops before queue polling; `{action:"continue"}` guarantees one more request even with no tool calls or queue. | `A:types.ts:130-165`, `A:agent-loop.ts:288-309` | |
| 3.17 | Tool loadout as transcript | Runtime `tools` vs tools declared to the model differ; the delta becomes `toolsAdded`/`toolsRemoved` on a system message before each request. | `A:agent-loop.ts:333-380`, `AI:types.ts:522-538` | Design the transcript this way from day 1; it makes deferred tools and cache-safe changes possible. |
| 3.18 | `Agent` wrapper | Owns state, listeners (awaited in order, included in run settlement), queues, one `activeRun`, `abort()`, `waitForIdle()`, `reset()`. `prompt()` while active throws. | `A:agent.ts:230-395` | |
| 3.19 | `continue()` | From assistant tail: drains one steering batch, else one follow-up batch, else throws. | `A:agent.ts:384-410` | |
| 3.20 | Stream function contract | Must not throw for runtime failures; must return a stream whose final message has `stopReason` `error`/`aborted`. | `A:types.ts:26-36` | |
| 3.21 | Proxy stream | `streamProxy` lets an app route LLM calls through a server; delta-only events, client rebuilds partial. | `A:proxy.ts:36,120` | Matches ask's server-owns-credentials idea. |
| 3.22 | Tool result shape | `content` (text/image, model-facing), `details` (UI), `structuredContent` (programmatic, needs `outputSchema`), `usage`, `isError`, `terminate`. | `A:types.ts:415-450` | |
| 3.23 | Tool `replay` field | `"never" | "safe"`: recovery policy for durable harness only; unused by shipping loop. | `A:types.ts:476` | |
| 3.24 | Nested tool calls | Tool can call other tools (codemode); calls are recorded on the result as `nestedCalls`, usage summed. | `C:core/nested-tool-calls.ts`, `AI:types.ts:573-608` | |

## 4. Ring 1b: built-in tools (coding-agent)

Default active set: `read`, `bash`, `edit`, `write` (`C:core/settings-manager.ts:213`, `C:core/tools/index.ts:164-171`). Read-only set: `read`, `grep`, `find`, `ls`. Also `powershell` on Windows. Extra built-in extensions `codemode` and `tool_search` are off by default (`CD:cli.md`, tools section).

Shared limits (`C:core/tools/truncate.ts:11-13`): `DEFAULT_MAX_LINES = 2000`, `DEFAULT_MAX_BYTES = 51200`, `GREP_MAX_LINE_LENGTH = 500`. Head truncation for reads and lists, tail truncation for bash.

| Tool | Params | Limits and behavior | Source | Go note |
|---|---|---|---|---|
| `read` | `path`, `offset?` (1-indexed line), `limit?` (lines) | Text truncated to 2000 lines or 50 KB, whichever first; hint `Use offset=N to continue`. First line larger than 50 KB gives a `sed -n ... | head -c` hint instead. Images (jpg, png, gif, webp, bmp) detected by MIME sniff, returned as image content, auto-resized (default on; model `inputLimits.images.resize` overrides), with a text note. | `C:core/tools/read.ts:14-17,76,108-135,161-171` | Image resize needs a Go lib (`disintegration/imaging` or `bimg`); keep the cache-safe "encode once" rule. |
| `write` | `path`, `content` | Creates parent dirs; overwrite. Serialized per file via `withFileMutationQueue` (keyed by realpath). No size count returned (removed as misleading, agent CL 0.85.0). | `C:core/tools/write.ts:11-13,31-36`, `C:core/tools/file-mutation-queue.ts:32` | Per-path mutex map. |
| `edit` | `path`, `edits[]` of `{oldText, newText}` | Every `oldText` matched against the **original** file; must be unique and non-overlapping. Exact match first, then fuzzy (trailing whitespace, smart quotes, Unicode dashes and spaces normalized). BOM stripped before matching; CRLF/LF detected and restored. Legacy single `oldText/newText` input accepted via `prepareArguments`. Result carries a unified diff plus first changed line. | `C:core/tools/edit.ts:21-40`, `C:core/tools/edit-diff.ts:11-34,207-300,364-376` | Port `normalizeForFuzzyMatch` exactly; models depend on it. |
| `bash` | `command`, `timeout?` (seconds, no default) | Output truncated to last 2000 lines or 50 KB; if truncated the full output is streamed to a temp file `pi-bash-*.log` and its path returned. Live partial updates while running. Max timeout 2,147,483 s. Structured output `{output, truncated, full_output_path?, exit_code, wall_time_seconds}` up to 1 MiB for programmatic callers. Non-zero exit is an error result. Session env vars injected (section 15). | `C:core/tools/bash.ts:22-25,40-64,258,355,393`, `C:core/tools/output-accumulator.ts` | Rolling tail buffer plus spill file. |
| `powershell` | same as bash | Windows shell variant; reuses bash machinery. | `C:core/tools/powershell.ts:18-49` | Out of v1 unless Windows is in scope. |
| `grep` | `pattern`, `path?`, `glob?`, `ignoreCase?`, `literal?`, `context?`, `limit?` | Runs ripgrep (`--json --line-number --hidden`), respects .gitignore; default 100 matches; 50 KB cap; lines cut at 500 chars with notice. `rg` is auto-downloaded by `ensureTool`. | `C:core/tools/grep.ts:21-41,119-168,282-299` | Shell out to `rg`; or use a Go regexp walker. |
| `find` | `pattern` (glob), `path?`, `limit?` | Uses `fd`; default 1000 results; 50 KB cap. Relative paths. | `C:core/tools/find.ts:26-41,172-216` | |
| `ls` | `path?`, `limit?` | Alphabetical, `/` suffix for dirs, includes dotfiles; default 500 entries; 50 KB cap. | `C:core/tools/ls.ts:11-23,62` | |
| Tool prompt contribution | Each tool supplies `promptSnippet` (one line) and `promptGuidelines` (bullets) merged into the system prompt. | | `C:core/tools/edit.ts:41-50,153`, `C:core/tools/read.ts:77-78` | |
| Constrained sampling | `read` sets `constrainedSampling: json_schema, strict: "prefer"`. Providers that support grammar or strict JSON use it. | | `C:core/tools/read.ts:79`, `AI:api/constrained-sampling.ts` | |
| Tool arg validation | TypeBox schema; primitives are coerced by JSON-schema type (e.g. `"5"` to 5) before validation. | | `AI:utils/validation.ts:59-200,317` | Implement coercion; models emit stringly-typed args. |
| Path handling | Relative paths resolve against session cwd; `~` and `@` prefixes normalized. | | `C:core/tools/path-utils.ts` | |
| Tool exposure modes | `direct`, `model-only`, `codemode`, `deferred`, `hidden`. | | `CD:extensions.md` "Tool exposure" | Keep the enum; it drives MCP scale. |
| `codemode` | QuickJS sandbox script calling other tools (`tools.name(args)`), `Promise.allSettled`, output cap 10,000 tokens, temp file for overflow, `store/load` persisted as `codemode-store` custom entries. | | `CD:cli.md` "How codemode works" | Out of scope for v1 unless needed. |
| `tool_search` | Searches non-declared tools and declares matches for the next call. | | `CD:cli.md` | |
| MCP | stdio and streamable-HTTP only; tools named `mcp__<server>__<tool>`; 60 s default per-request timeout; OAuth for HTTP; `pi mcp add/list/login/logout`. | | `CD:mcp.md`, `C:core/mcp-servers.ts` | |

## 5. Ring 1c: system prompt assembly

Source: `C:core/system-prompt.ts:120-216`. The prompt is a set of **named sections** in one `system` message; later system messages patch sections by name (`null` removes).

| Section (in order) | Content | Condition |
|---|---|---|
| `preamble` | "You are an expert coding assistant operating inside pi, a coding agent harness..." or `SYSTEM.md`/`--system-prompt` text | Always; custom prompt replaces preamble, tools, rules, and docs sections |
| `tools` | `- name: snippet` per active tool with a snippet, plus "other custom tools" line | Default prompt only |
| `rules` | Deduped bullets: fallback hint when bash without grep/find/ls; per-tool guidelines; extension guidelines; "Be concise"; "Show file paths clearly" | Default prompt only |
| `docs` | Pi docs and examples paths, with rules to read only when asked about pi | Default prompt only |
| `addendum` | `APPEND_SYSTEM.md` / `--append-system-prompt` (repeatable) | If set |
| `project_context` | Each context file as `<project_instructions path="...">` | If any files |
| `skills` | `<available_skills>` XML: name, description, location (skills with `disable-model-invocation` hidden) | Only if `read` or `bash` is active and skills exist |
| `cwd` | Working directory, backslashes normalized | Always |
| extension sections | XML-wrapped by tag name; name regex `^[a-z][a-z0-9_-]*$`, `preamble` reserved | If extensions add |

Other rules:
- Each non-preamble section is wrapped in `<name>...</name>` so later patches can be matched (`system-prompt.ts:198-201`).
- `forceSystemPrompt` (from `before_agent_start`) replaces everything as opaque text (`:206-208`).
- `diffSystemPromptSections` makes a patch only for changed sections; the diff is written as a new system message, not a rewrite (`:211-216`).
- Context-file discovery (`C:core/resource-loader.ts:184-268`): candidates per directory in order `AGENTS.override.md`, `AGENTS.md`, `AGENTS.MD`, `CLAUDE.md`, `CLAUDE.MD` (first hit wins per directory). Order of injection: agent dir first, then ancestors from filesystem root down to cwd. A linked git worktree shadows the main repo's copy so the same file is not loaded twice (`:208-234`). Context files load without project trust; `-nc` disables them.
- `SYSTEM.md` / `APPEND_SYSTEM.md`: project file (needs trust) wins over agent-dir file; not merged (`CD:configuration.md`).
- Skills (`CD:skills.md`, `C:core/skills.ts`): directory with `SKILL.md`, frontmatter `name` (max 64, lowercase-hyphen), `description` (max 1024), optional `disable-model-invocation`. Discovered recursively from `~/.pi/agent/skills`, `.pi/skills`, `~/.agents/skills`, ancestor `.agents/skills` up to repo root, settings paths, packages. Honors `.gitignore/.ignore/.fdignore`. Name collision: first wins, warning. Only metadata enters the prompt; the model reads `SKILL.md` on demand. `/skill:name args` force-loads.
- Prompt templates (`CD:prompt-templates.md`): `.md` in `prompts/` become `/name`; args `$1`, `$@`, `${1:-default}`, `${@:N:L}`; shell-like quoting.
- Go: keep sections as an ordered map and implement patch-by-name from the start; it is what enables cache-stable tool/prompt changes.

## 6. Ring 1d: message types and streaming events

### 6.1 Core messages (`AI:types.ts:389-610`)

| Message | Fields | Notes |
|---|---|---|
| `system` | `content`, `sections?`, `toolsAdded?`, `toolsRemoved?`, `timestamp` | First = base prompt; later = patches. Providers that cannot take mid-conversation system messages rebuild the leading one. |
| `user` | `content: string | (text|image)[]` | |
| `assistant` | `content: (text|thinking|toolCall)[]`, `api`, `provider`, `model`, `responseModel?`, `responseId?`, `providerThinkingLevel?`, `thinkingLevel?`, `diagnostics?`, `usage`, `stopReason`, `errorMessage?`, `rawStopReason?`, `endTurn?`, `deferred?` | Every reply records which model produced it; this drives replay. |
| `toolResult` | `toolCallId`, `toolName`, `content`, `details?`, `usage?`, `nestedCalls?`, `isError` | `details` JSON-only, not sent to model. |

Content blocks: `text` (+`textSignature`), `thinking` (+`thinkingSignature`, `redacted`), `image` (base64 + mime), `toolCall` (`id`, `name`, `arguments`, `thoughtSignature?`, `namespace?`).
`StopReason`: `pending | stop | length | toolUse | error | aborted | deferred`.

### 6.2 Coding-agent extra roles (`C:core/messages.ts`)

| Role | Purpose | To LLM |
|---|---|---|
| `bashExecution` | user `!cmd` / `!!cmd` shell run | User message "Ran `cmd`" with fenced output; `!!` (`excludeFromContext`) is dropped |
| `custom` | extension message with `customType`, `display` | User message with same content |
| `branchSummary` | summary of abandoned branch | User message wrapped in `<summary>` with a fixed prefix |
| `compactionSummary` | summary of compacted history | User message wrapped in `<summary>` with a fixed prefix |

`convertToLlm` (`messages.ts:141-196`) is the only place these map to LLM roles. Note that summaries are **user** messages.

### 6.3 Provider stream events (`AI:types.ts:767-787`)

`start`, `text_start/delta/end`, `thinking_start/delta/end`, `toolcall_start/delta/end`, `done{reason: stop|length|toolUse|deferred}`, `error{reason: aborted|error}`. Each carries `contentIndex` and a cumulative `partial`. `error` carries the full `AssistantMessage` with partial content and usage.

### 6.4 Agent events (`A:types.ts:514-529`)

`agent_start`, `agent_end{messages}`, `turn_start`, `turn_end{message,toolResults}`, `message_start`, `message_update{message,assistantMessageEvent}`, `message_end`, `tool_execution_start/update/end`.

### 6.5 Session-level events added by AgentSession (`CD:json.md`)

`agent_end` gains `willRetry`; new `agent_settled`, `queue_update{steering,followUp}`, `entry_appended`, `session_info_changed`, `thinking_level_changed`, `compaction_start{reason}`, `compaction_end{reason,result,aborted,willRetry,errorMessage}`, `auto_retry_start{attempt,maxAttempts,delayMs,errorMessage}`, `auto_retry_end{success,attempt,finalError}`, `summarization_retry_scheduled/attempt_start/finished`. RPC only: `bash_execution_update{id,delta}`, `extension_error`, `extension_ui_request`.

### 6.6 Wire size reduction

JSON/RPC `message_update` drops the cumulative `message` and every `partial`; adds top-level `usage` and, for `toolcall_start`, `id` and `toolName` (`CD:json.md` "Reconstruct streaming messages", `C:modes/json-event.ts`). The final `message_end` is authoritative. Go: emit delta-only from the start (R-core noted the same problem for multi-tenant).

### 6.7 Compact frames

`AssistantMessageFrameEncoder` and `reduceAssistantMessageFrames` give a persistable compact form of a stream (`AI:utils/assistant-message-frame.ts`, ai CL 0.85.0). Used by the harness for crash display only.

## 7. Ring 1e: provider abstraction

### 7.1 Model of the layer

- `Api` = wire protocol (10 known), `Provider` = runtime unit (id, auth, models, stream), `Model` = plain data (`AI:types.ts:1097-1143`). `createProvider({id, auth, models, api | {apiId: impl}})` (R-core section 1.2; still valid). API modules load lazily (`AI:api/lazy.ts`).
- Chat, image (`openrouter-images`), and classifier models share `BaseModel` since ai 0.99.0 (`AI:types.ts:1097,1145,1152`).
- Failure is data: after a stream returns, errors are events (`AI:types.ts:371-388` contract comment).
- Cross-provider replay: `transformMessages` (`AI:api/transform-messages.ts:64`) converts foreign thinking to `<thinking>` text, normalizes tool-call ids per target API, downgrades images to placeholders `(image omitted: model does not support images)` when the model lacks image input (`:11-50`).

### 7.2 APIs and what they carry

| API id | Used by (provider ids) | Source | Special features |
|---|---|---|---|
| `anthropic-messages` | anthropic, kimi-coding, minimax, minimax-cn, vercel-ai-gateway, fireworks (mixed), opencode(+go), openrouter (mixed), github-copilot (mixed), cloudflare-ai-gateway (mixed) | `AI:api/anthropic-messages.ts` (1567 lines) | Adaptive thinking with `effort`; budget thinking (`budget_tokens`); interleaved-thinking beta; redacted thinking replay; `cache_control` ephemeral with optional `ttl:"1h"`; eager tool-input streaming; deferred tool placeholder; OAuth mode sends Claude Code identity headers and the line "You are Claude Code, Anthropic's official CLI for Claude." and Claude Code tool names (lines 90-93, 952-1111). Mid-conversation effort changes via empty system messages with `output_config` (1454-1458). |
| `openai-completions` | about 25 providers (count not tabulated) incl. deepseek, groq, cerebras, together, baseten, huggingface, nvidia, moonshotai(+cn), zai(+cn), qwen-token-plan (3), xiaomi (4), ant-ling, cloudflare-workers-ai, openrouter, opencode, and any user endpoint (Ollama, vLLM, LM Studio, SGLang, llama.cpp) | `AI:api/openai-completions.ts` (1726) | Big compat matrix `OpenAICompletionsCompat` (`AI:types.ts:789-868`): store field, developer role, reasoning effort field, max tokens field, strict tools, thinking format, `chat_template_kwargs`, `thinking_token_budget` field, `vllmPriority`, OpenRouter routing, Vercel gateway routing. |
| `openai-responses` | openai, xai, meta, github-copilot (GPT), opencode, cloudflare-ai-gateway | `AI:api/openai-responses.ts`, `openai-responses-shared.ts` | `prompt_cache_key` from session id; `prompt_cache_retention` or `prompt_cache_options.ttl:"30m"` for GPT-5.6+; reasoning summary; text-signature phases (commentary vs final_answer); service tier pricing (fast). Stream ends in error if `output_index` missing (llama.cpp guard). |
| `azure-openai-responses` | azure-openai-responses | `AI:api/azure-openai-responses.ts` | Resource-name or base-URL config; URL normalization for `openai.azure.com`, `cognitiveservices.azure.com`, `ai.azure.com`. |
| `openai-codex-responses` | openai-codex (legacy), "Sign in with ChatGPT" | `AI:api/openai-codex-responses.ts` (1697) | The only API with WebSocket transports (`websocket`, `websocket-cached` with `previous_response_id` continuation); SSE fallback; `transport: auto`. |
| `bedrock-converse-stream` | amazon-bedrock | `AI:api/bedrock-converse-stream.ts` | SigV4; `cachePoint` with optional 1 h TTL; bearer-token mode; custom headers injected before signing; reserved headers ignored. Node-only SDK. |
| `google-generative-ai` | google, opencode | `AI:api/google-generative-ai.ts` | `thoughtSignature` on tool calls; thinking budget `-1` dynamic, `0` off; implicit cache reads reported as `cacheRead`. |
| `google-vertex` | google-vertex | `AI:api/google-vertex.ts` | API key, or ADC with project+location, or service-account key file. |
| `mistral-conversations` | mistral | `AI:api/mistral-conversations.ts` | Reasoning effort per model family; handles empty-delta thinking chunks. |
| `pi-messages` | radius | `AI:api/pi-messages.ts` | Pi's own gateway protocol with a dynamic gateway catalog. |
| Image API | `openrouter-images` | `AI:api/openrouter-images.ts` | `Models.generateImages()`. |
| Classifier APIs | `typesafe-system-one`, `cloudflare-workers-ai-system-one`, `llama-cpp-classify` | `AI:api/*system-one*.ts`, `llama-cpp-classify.ts` | `choice | score | bool` questions with probabilities; `Models.classify()`. |
| Faux provider | tests | `AI:providers/faux.ts` | Scripted responses for tests; Go should copy the idea. |

Full provider id list (43, `AI:types.ts:39-80`): amazon-bedrock, ant-ling, anthropic, google, google-vertex, openai, azure-openai-responses, openai-codex, radius, typesafe, nvidia, deepseek, github-copilot, xai, groq, cerebras, openrouter, vercel-ai-gateway, zai, zai-coding-cn, mistral, minimax, minimax-cn, moonshotai, moonshotai-cn, huggingface, fireworks, together, baseten, opencode, opencode-go, kimi-coding, meta, cloudflare-workers-ai, cloudflare-ai-gateway, qwen-token-plan, qwen-token-plan-cn, qwen-token-plan-individual, xiaomi, xiaomi-token-plan-cn, xiaomi-token-plan-ams, xiaomi-token-plan-sgp.

Go rewrite reading: three wire APIs (anthropic-messages, openai-completions, openai-responses) cover roughly 38 of the 43 providers. The rest need Bedrock, Google, Mistral, Codex (WebSocket) or Radius.

### 7.3 Request options common to all (`AI:types.ts:132-240`, `:350-360`)

`signal`, `apiKey`, `fetch`, `env` (provider-scoped env overrides), `headers` (null value removes a default), `timeoutMs`, `maxRetries` (SDK-level), `maxRetryDelayMs` (default 60 s; above cap the request fails so the outer loop can show it), `onPayload` (inspect or replace request body), `onResponse`, `onProviderStreamEvent`, `temperature`, `samplingParams` (free-form, merged into body for OpenAI-compatible APIs), `maxTokens`, `transport`, `cacheRetention`, `sessionId`, `metadata`, `toolChoice: auto|none`, `reasoning`, `thinkingBudgets`, `deferred`.

`maxTokens` is clamped to `contextWindow - estimated_input - 4096` (`AI:api/simple-options.ts:12-17`).

### 7.4 Deferred requests

`deferred: true | {window: "15m"|"1h"|"24h"}` asks a capable provider to return a handle (`DeferredHandle`, `stopReason: "deferred"`) to poll later (`AI:types.ts:353,500-520`). Batch-style async. Not used by the shipping loop.

## 8. Ring 1f: thinking and reasoning

| # | Feature | Detail | Source |
|---|---|---|---|
| 8.1 | Levels | Pi levels: `off, minimal, low, medium, high, xhigh, max`. Default startup level `medium`. | `AI:types.ts:85-86`, `CD:settings.md` |
| 8.2 | Per-model support | `Model.reasoning` bool plus `thinkingLevelMap`: `null` = unsupported, string = provider-native value, missing `xhigh`/`max` = unsupported. | `AI:types.ts:1111-1120`, `AI:models.ts:1217-1226` |
| 8.3 | Clamp rule | Requested level not supported: pick the nearest **higher** supported level first, then the nearest lower. Non-reasoning model: `off`. | `AI:models.ts:1228-1247` |
| 8.4 | Budget levels | Token-budget providers use defaults minimal 1024, low 2048, medium 8192, high 16384; `xhigh/max` clamp to high; user can override via `thinkingBudgets` setting. At least 1024 tokens stay for the answer. | `AI:api/simple-options.ts:44-91` |
| 8.5 | Effort levels | Adaptive Anthropic and OpenAI reasoning models take a native effort string from `thinkingLevelMap`. | `AI:api/anthropic-messages.ts:846-895` |
| 8.6 | Recorded on reply | `AssistantMessage.thinkingLevel` (Pi level requested) and `providerThinkingLevel` (native value used). | `AI:types.ts:556-559`, agent CL 0.99.0 |
| 8.7 | Thinking replay | Signed/encrypted thinking replays only to the same provider+model; foreign thinking becomes tagged text; redacted blocks keep opaque payload. Unsigned thinking from some endpoints is replayed as plain text (workaround list in ai CL). | `AI:api/transform-messages.ts`, ai CL 0.99.0 |
| 8.8 | Cycling and persistence | `setThinkingLevel` clamps, writes a `thinking_level_change` entry only when the level really changes; per-model levels in `modelThinkingLevels`. | `C:core/agent-session.ts:2527-2600` |
| 8.9 | Hide | `hideThinkingBlock` UI setting only. | `CD:settings.md` |

## 9. Ring 1g: prompt caching, images, cache warming

| # | Feature | Detail | Source |
|---|---|---|---|
| 9.1 | Retention | `cacheRetention`: `none | short | long`, default `short`; env `PI_CACHE_RETENTION=long`. Mapped per provider: Anthropic `ttl:"1h"` if compat allows; Bedrock `cachePoint` TTL 1 h; OpenAI `prompt_cache_retention`/`prompt_cache_options`. | `AI:types.ts:110`, `AI:api/anthropic-messages.ts:83-86`, `AI:api/bedrock-converse-stream.ts:901` |
| 9.2 | Cache breakpoints (Anthropic) | `cache_control` on system blocks, last tool, and the last user/system message. | `AI:api/anthropic-messages.ts:1089-1111,1411-1432,1536` |
| 9.3 | Session affinity | `sessionId` becomes `prompt_cache_key` (OpenAI) or routing header formats `openai`, `openai-nosession`, `openrouter`. | `AI:types.ts:124`, `AI:api/openai-responses.ts:334` |
| 9.4 | Usage split | `cacheRead`, `cacheWrite`, `cacheWrite1h` (Anthropic only). | `AI:types.ts:427-448` |
| 9.5 | Cache warming | `cacheWarming: off | streaming | idle` (default streaming, global only). Sends a refresh request at 90% of TTL (min 10 s margin) only if model declares `promptCache` lifetimes and expected savings are at least $0.05; streaming warming stops 60 min after the real request, idle 30 min; idle assumes 15% chance a real request arrives. Refresh cost is written as `usage` entries (`kind:"cache_warm"`) that count in totals but not context. | `C:core/cache-warmer.ts:14-45`, `CD:settings.md` |
| 9.6 | Cache-miss notices | `showCacheMissNotices`. | `C:core/cache-stats.ts` |
| 9.7 | Image input | Only if `model.input` includes `image`. New images (attachments, read results, tool-result images) resized once before entering history: defaults 2000x2000 px, 4.5 MiB base64, JPEG quality 80; per-model `inputLimits.images.resize`. `images.blockImages` blocks all; `images.autoResize` toggles. Catalog also knows `maxRequestBytes`, `images.maxPerMessage/maxPerRequest` but Pi does not yet enforce them. | `CD:models.md`, `CD:settings.md`, `AI:types.ts:1073-1095` |
| 9.8 | Non-vision fallback | Images replaced by a placeholder text at request time. | `AI:api/transform-messages.ts:11-50` |
| 9.9 | Unicode safety | Unpaired surrogates stripped from outgoing text. | `AI:utils/sanitize-unicode.ts` |

## 10. Ring 1h: model registry, selection, cycling

| # | Feature | Detail | Source |
|---|---|---|---|
| 10.1 | Catalog layers | Bundled generated catalog (`models.generated.ts`, per-provider `*.models.ts`), overlaid by a pi.dev remote catalog cached with ETag and refresh interval; `pi update --models` forces; `PI_OFFLINE` / `--offline` disables. | `AI:models.generated.ts`, `C:core/remote-catalog-provider.ts:13,60-150`, `CD:models.md` |
| 10.2 | User models | `~/.pi/agent/models.json`: `providers.<id>{baseUrl, api, apiKey, headers, models[], modelOverrides}`; `apiKey`/headers accept literal, `$NAME`/`${NAME}`, or `!command` (run per request, not cached). Reloaded when `/model` opens. Schema validated with TypeBox, comments allowed. | `C:core/model-config.ts`, `CD:models.md` |
| 10.3 | Model fields | `id, name, api, provider, baseUrl, input[], reasoning, thinkingLevelMap, contextWindow, maxTokens, cost{input,output,cacheRead,cacheWrite,tiers[]}, promptCache, inputLimits, samplingParams, compat, headers`. Cost is USD per million tokens. | `AI:types.ts:1056-1143` |
| 10.4 | Classifier models | Not chat models; hidden from `/model`; reachable via `codemode` `models.classify()` or `ctx.modelRegistry.classify()`. | `CD:models.md` |
| 10.5 | Virtual models | Extension-registered selectable model that routes each request to a physical model and thinking level. Reasons: `user, continuation, retry, direct`. Router state is stored as `custom` entries (`pi.virtual-model-state`) and follows the branch. Selection is recorded in `model_change`; each assistant message records the physical model. Compaction and context-usage use the routed model's limits. | `CD:virtual-models.md`, `C:core/virtual-models.ts`, `C:core/agent-session.ts:746-811` |
| 10.6 | Initial model priority | CLI `--provider/--model` > first of `--models` scope (if not resuming) > model restored from session > saved default in settings > first model with valid auth. | `C:core/model-resolver.ts:608-711` |
| 10.7 | Pattern syntax | `provider/id`, exact id, fuzzy id/name, case-insensitive globs, optional `:thinking` suffix; last-colon split handles ids like `model:exacto`. Alias preferred over dated version; else latest dated. Ambiguous bare id across providers is rejected. | `C:core/model-resolver.ts:70-380` |
| 10.8 | Scoped models | `enabledModels` setting or `--models` limit the cycle set; each entry may carry its own thinking level. | `CD:settings.md`, `C:core/model-resolver.ts:282` |
| 10.9 | Cycling | `cycleModel(forward|backward)` over scoped set (if more than one available) else all available; index wraps; writes `model_change` entry; re-clamps thinking; `persist` option saves the default. | `C:core/agent-session.ts:2434-2530` |
| 10.10 | Restore on resume | Model and thinking come from the latest `model_change`/`thinking_level_change` on the active path; fall back to defaults if unavailable. | `C:core/model-resolver.ts:711`, `C:core/session-manager.ts:576` |
| 10.11 | Provider extension | `pi.registerProvider(name, {baseUrl, api, models, oauth, streamSimple, refreshModels})`. | `CD:custom-provider.md` |
| 10.12 | llama.cpp router | Built-in extension `builtin:llama.cpp`; `/llama` manages router; loaded models appear in `/model`. | `CD:llama-cpp.md` |

## 11. Ring 1i: auth and credential storage

| # | Feature | Detail | Source |
|---|---|---|---|
| 11.1 | Resolution order | Runtime `--api-key` > `auth.json` credential > `apiKey` from `models.json` > provider env vars or ambient cloud credentials. | `CD:models.md` "Authenticate" |
| 11.2 | `auth.json` | Map providerId to `{type:"api_key", key, env?}` or `{type:"oauth", access, refresh, expires, ...}`. Created mode `0600`, dir `0700`. | `AI:auth/types.ts`, `C:core/auth-storage.ts:25,84-90` |
| 11.3 | Locking | `proper-lockfile`, stale 30 s, sync path retries 10 x 20 ms; all writes are read-modify-write via `modify(providerId, fn)`; OAuth refresh runs inside `modify` so concurrent processes do not double-refresh a rotated token. | `C:core/auth-storage.ts:65-200`, `AI:auth/types.ts` (CredentialStore comment) |
| 11.4 | Command keys | `key: "!cmd"` runs at first need, stdout cached for process lifetime; empty output, timeout, or non-zero exit leaves it unresolved until restart. | `CD:providers.md` "Load an API key from a command" |
| 11.5 | Per-credential env | `env` object inside a credential (Cloudflare account and gateway ids); wins over process env for that provider. | `CD:providers.md` |
| 11.6 | Env keys | One primary var per provider (list in `CD:providers.md`); Anthropic also `ANTHROPIC_OAUTH_TOKEN` and `ANTHROPIC_AUTH_TOKEN`; Bedrock: profile, IAM keys, session token, bearer token, ECS and IRSA; Vertex: API key or ADC or key file; Azure: key plus base URL or resource name; Cloudflare: token, account id, gateway id. | `AI:env-api-keys.ts:29-148`, `CD:providers.md` |
| 11.7 | OAuth providers | Anthropic (Claude Pro/Max), OpenAI "Sign in with ChatGPT", OpenAI Codex (legacy), GitHub Copilot (device code), OpenRouter (PKCE, mints an API key), xAI, Kimi Coding, Meta (Muse), Radius. | `AI:auth/oauth/*.ts` |
| 11.8 | Flow mechanics | PKCE, shared local callback server, `manual_code` prompt raced against callback (paste redirect URL on headless), device-code flow, `AuthInteraction.prompt/notify` protocol. Anthropic falls back to paste when port is busy. | `AI:auth/oauth/pkce.ts`, `callback-server.ts`, `device-code.ts`, ai CL 0.99.0 |
| 11.9 | Per-request auth | `getApiKey(provider)` is called for every LLM call in the loop so short-lived tokens refresh mid-run. | `A:agent-loop.ts:392-395` |
| 11.10 | CLI | `pi auth check` (exit 0 ready, 1 not ready, 2 invalid), `print-api-key`, `print-bearer-token --min-expiry`. | `CD:cli.md` "Credential commands" |
| 11.11 | Login/logout | `/login`, `/logout` (interactive only). Logout does not revoke or unset env. | `CD:providers.md` |
| 11.12 | Anthropic OAuth disguise | With an OAuth token the request presents Claude Code identity (headers, system line, tool names). Legal and ToS risk; flag before copying. | `AI:api/anthropic-messages.ts:952,1029,1089-1094` |

Go: keep `CredentialStore.Modify(provider, func(cur) next)` as the only write path; use a flock or SQLite row lock. ask is multi-tenant, so key by (tenant, provider).

## 12. Ring 1j: session storage (shipping: `SessionManager`, format v3)

### 12.1 Files and layout

- Path: `~/.pi/agent/sessions/--<cwd with / \ : replaced by ->--/<ISO-timestamp>_<uuid>.jsonl` (`C:core/session-manager.ts:596`, `CD:session-format.md`). Overrides: `--session-dir` > `PI_CODING_AGENT_SESSION_DIR` > `sessionDir` setting.
- Lazy file creation: nothing is written until the session has a user or assistant message (`session-manager.ts:1160-1188`); first flush writes all buffered entries with exclusive create (`wx`), later entries are single `appendFileSync` lines. No fsync, no lock, no batching.
- Malformed lines are skipped on load; a file needs a valid header (`session` type, string `id`) (`:627-670`).
- Migrations on load: v1 (linear) to v2 (tree ids) to v3 (`hookMessage` role renamed `custom`) (`:287-346`).
- IDs: 8-hex, falling back to full UUID on collision. Session ids allow letters, digits, `.`, `_`, `-`, must start and end alphanumeric (`:268`).
- Ephemeral: `--no-session` uses `SessionManager.inMemory` (`:1804`).

### 12.2 Entry types (`C:core/session-manager.ts:43-200`, `CD:session-format.md`)

| Type | Key fields | In model context? |
|---|---|---|
| header `session` | `version:3, id, timestamp, cwd, parentSession?` | no |
| `message` | any `AgentMessage` incl. `system` | yes |
| `model_change` | `provider, modelId` | no (sets state) |
| `thinking_level_change` | `thinkingLevel` | no (sets state) |
| `usage` | `kind, provider, model, usage, note?` | no; counted in totals |
| `compaction` | `summary, firstKeptEntryId, tokensBefore, systemMessage?, usage?, details?, fromHook?` | yes (summary + kept range) |
| `branch_summary` | `fromId, summary, usage?, details?, fromHook?` | yes |
| `context_edit` | `targetId, replacement (null = omit, or {content})` | edits target's projection only |
| `custom` | `customType, data` | no (extension state) |
| `custom_message` | `customType, content, display, details?` | yes |
| `label` | `targetId, label?` | no |
| `session_info` | `name` | no |

### 12.3 Tree operations

| Operation | Behavior | Source |
|---|---|---|
| Append | Child of current leaf; leaf advances. | `session-manager.ts:1204,1195` |
| `branch(id)` / `resetLeaf()` | Move leaf; abandoned branch stays. Multiple roots are allowed. | `:1579-1600` |
| `/tree` navigate | `navigateTree(target, {summarize, customInstructions, replaceInstructions, label})`; optional LLM branch summary attached at the new position. Selecting a user message puts its text back in the editor. | `C:core/agent-session.ts:3866-4060`, `CD:sessions.md` |
| `/fork` | New file from an earlier **user** message; header gets `parentSession`. | `C:core/agent-session-runtime.ts`, `CD:sessions.md` |
| `/clone` | New file with the active branch copied. Labels are re-created; compaction `firstKeptEntryId` is remapped when label entries are removed from the path. | `session-manager.ts:1632-1750` |
| Resume | `--continue` (latest in cwd), `--resume` (picker), `--session <path|id|partial>` (search current project first, offer to fork cross-project), `--session-id`, `--fork`. Constraints between flags listed in `CD:cli.md`. | `C:cli/session-picker.ts`, `CD:cli.md` |
| Import/export | `/import` JSONL; `/export` HTML (self-contained template with vendored highlight) or JSONL; `/share` uploads to a Radius artifact or a private GitHub gist. | `C:core/session-export.ts`, `C:core/export-html/`, `CD:sessions.md` |
| Naming | `--name`, `/name`, RPC `set_session_name`; stored as `session_info`. | |
| Delete | delete the file; picker uses `trash` CLI if present. | `CD:session-format.md` |
| Concurrency | No cross-process lock on session files. Two processes appending the same file would interleave. | grep for lock in `session-manager.ts` finds none |

### 12.4 Context building (`session-manager.ts:439-594`)

1. Walk from leaf to root.
2. If a `compaction` is on the path, use the **latest**: emit the compaction first, then non-system entries from `firstKeptEntryId` up to it, then everything after.
3. Apply `context_edit` per target (latest wins); omitted targets vanish.
4. Convert: `message` as-is; `compaction` gives system checkpoint + `compactionSummary`; `branch_summary` gives `branchSummary`; `custom_message` gives `custom`.
5. State (model, thinking level) comes from the full path, not the compacted range.

Go: store `parent_seq` and derive the path by walking parents; JSONL is optional for ask (R-open T3 already leans that way). Keep `context_edit` semantics: they are how overflow recovery removes the failed attempt without rewriting history (section 14.3).

## 13. Ring 1k: retry, backoff, overflow

### 13.1 Agent-level retry (`AgentSession`)

- Trigger: assistant `stopReason == "error"` whose `errorMessage` matches the retryable regex list and not the non-retryable list, and is not a context overflow (`C:core/agent-session.ts:3611-3616`, `AI:utils/retry.ts:246-251`).
- Retryable patterns include: overloaded, rate limit, 429/500/502/503/504/520/524, service unavailable, provider returned error, network and socket errors, `ENOTFOUND`, timeout, websocket closed, premature stream end, "retry delay", "you can retry your request", `ResourceExhausted`, ChatGPT-subscription transient codes (`AI:utils/retry.ts:44-100`).
- Non-retryable (quota/billing): `insufficient_quota`, "quota exceeded", "billing", "out of budget", OpenCode usage limits, `subscription_sharing_usage_limit_exceeded` (`:6-30`).
- Backoff: `delay = min(baseDelayMs * 2^(attempt-1), maxAgentDelayMs)`; defaults 3 retries, base 2000 ms, cap 60000 ms. **No jitter** is applied (the doc comment says "before jitter" but `retryDelayMs` has none) (`AI:utils/retry.ts:122-126`).
- Flow: emit `auto_retry_start`; durably omit the failed attempt from context with a `context_edit` (raw history keeps it); abortable sleep; `agent.continue()`; on next non-error assistant message emit `auto_retry_end{success:true}` and reset the counter (`agent-session.ts:3670-3708`, `:1120-1130`).
- `abort_retry` RPC / `abortRetry()` cancels the sleep; emits `auto_retry_end{success:false, finalError:"Retry cancelled"}` (`:3648-3712`).
- Toggle: `retry.enabled`; `set_auto_retry` RPC.
- Retry also applies to compaction and branch-summary calls through `retryAssistantCall` with `summarization_retry_*` events (`AI:utils/retry.ts:185-235`, `agent-session.ts:3620-3646`).

### 13.2 Provider-level

`retry.provider.maxRetries` default 0 (SDK retry), `timeoutMs` default equals `httpIdleTimeoutMs` (300000), `maxRetryDelayMs` default 60000. Docs advise keeping SDK retries at 0 so Pi can handle quota errors itself (`CD:settings.md` "Network and retries").

### 13.3 Context-overflow detection (`AI:utils/overflow.ts:137-181`)

Three signals: (a) `stopReason==error` and message matches one of ~25 provider regexes (Anthropic, OpenAI, Google, xAI, Groq, OpenRouter, Together, llama.cpp, LM Studio, Copilot, MiniMax, Kimi, Mistral, z.ai, DashScope, Ollama, generic), excluding throttling texts; (b) silent overflow: `stopReason==stop` but `usage.input + cacheRead > contextWindow`; (c) `stopReason==length`, zero output, and input at least 99% of the window. Bodyless HTTP 400/413 counts only for Cerebras (ai CL 0.86.0).

`isRecoverableLength`: `stopReason==length` with `usage.output < desiredMaxOutput` allows one compact-and-retry (`:179-181`).

## 14. Ring 1l: cost, tokens, context window, compaction

### 14.1 Usage and cost

- `Usage {input, output, cacheRead, cacheWrite, cacheWrite1h?, reasoning?, totalTokens, cost{...}}`; `reasoning` is a subset of `output` (`AI:types.ts:427-448`).
- `calculateCost` (`AI:models.ts:1193-1213`): rates in $/M tokens. Tiers: highest matching `inputTokensAbove` (measured on input+cacheRead+cacheWrite) applies to the whole request. 1 h cache writes cost 2x base input. Floats. R-open T12 already decided micro-USD ints for ask.
- Totals per session sum assistant usage, `usage` entries, and compaction/branch-summary usage (`C:core/usage-totals.ts`, `agent-session.ts:4085 getSessionStats`, RPC `get_session_stats`).
- Context usage for the footer: last valid assistant usage + chars/4 estimate of trailing messages; images count as 4800 chars; aborted/error/all-zero usage messages are skipped; usage before a compaction or `context_edit` is not trusted (`C:core/compaction/compaction.ts:140-260,298`, `AI:utils/estimate.ts:15-16`).

### 14.2 Compaction trigger and algorithm

Defaults: `enabled true`, `reserveTokens 16384`, `keepRecentTokens 20000` (`compaction.ts:126-130`). Per-model overrides in `compaction.modelOverrides` keyed `provider/modelId`.

| Trigger | Condition | Source |
|---|---|---|
| Threshold, between turns | `contextTokens > contextWindow - reserveTokens`, checked in `prepareRequest` after tools finish (so mid-run) and in `_checkCompaction` after a run | `compaction.ts:267`, `agent-session.ts:726-745,2862-3000` |
| Threshold with virtual model | Check again against the routed model's window, then re-prepare | `agent-session.ts:796-800` |
| Overflow | `isContextOverflow` on a same-model reply; one attempt only per user turn (`_overflowRecoveryAttempted`) | `agent-session.ts:2925-2960` |
| Truncated length | `isRecoverableLength`; same one-shot recovery | `:2927` |
| Manual | `/compact [instructions]`, RPC `compact` | `:2679` |

Cut point (`compaction.ts:446-580`): walk backwards from newest accumulating chars/4 token estimates until `keepRecentTokens` is reached. Valid cuts: user, assistant, bash-execution, custom, branch-summary messages; never a tool result. If one user-message span exceeds the budget the cut lands inside it (split turn), producing two summaries (history and turn prefix) that are merged.

Summarization call:
- System prompt: "You are a context summarization assistant ... Do NOT continue the conversation" (`compaction/utils.ts:161`).
- History is serialized to labeled text (`[User]:`, `[Assistant thinking]:`, `[Assistant tool calls]: name(args)`, `[Tool result]:`) with tool results cut to 2000 chars (`utils.ts:94,149`).
- Output format: Goal, Constraints & Preferences, Progress (Done, In Progress, Blocked), Key Decisions, Next Steps, Critical Context, then `<read-files>` and `<modified-files>` lists.
- Iterative: previous summary is passed in for merge (update prompt); file lists accumulate across Pi-generated compactions.
- Limits: `maxTokens = min(0.8 * reserveTokens, model.maxTokens)`; `cacheRetention: "none"`; fresh `sessionId` if none (`compaction.ts:610-617,712-715`).
- Output: `compaction` entry with `summary, firstKeptEntryId, tokensBefore, usage, details{readFiles, modifiedFiles}`; the entry also stores a `systemMessage` checkpoint of the full prompt sections and tool declarations (`CD:session-format.md`).
- Retain-none compaction stores its own id as `firstKeptEntryId`.
- Result events: `compaction_start{reason}`, `compaction_end{result{summary, firstKeptEntryId, tokensBefore, estimatedTokensAfter, usage, details}, aborted, willRetry, errorMessage}`.

### 14.3 Overflow/length recovery order (`CD:compaction.md` "Overflow and Length Recovery Ordering")

persist final assistant reply; extension `turn_end`; extension `agent_end`; append `context_edit` omissions for the failed attempt; `session_before_compact`; append compaction; start retry as a **fresh run**. If compaction fails or is cancelled, omissions stay, no compaction is appended, no retry is scheduled.

### 14.4 Branch summarization (`compaction/branch-summarization.ts`)

On `/tree` navigation: find deepest common ancestor; collect entries from old leaf back to it; include messages newest first until token budget `contextWindow - branchSummary.reserveTokens` (default 16384); output max 4096 tokens; same section format minus Critical Context; `branchSummary.skipPrompt` defaults to no summary. File ops are collected from all entries even those that miss the budget (`:195-240,305-352`).

### 14.5 Extension hooks (compaction)

`session_before_compact` (cancel, or supply `compaction{summary, firstKeptEntryId, tokensBefore, usage?, details?}`), `session_compact`, `session_before_tree`, `session_tree`, plus `agent_before_settle`.

## 15. Ring 1m: settings, config layering, trust

### 15.1 Files

| Scope | Path | Notes |
|---|---|---|
| User | `<agent-dir>/settings.json`; agent dir = `PI_CODING_AGENT_DIR` or `~/.pi/agent` | Written under a `proper-lockfile` lock; migrations run on load (e.g. `queueMode` to `steeringMode`, `websockets:bool` to `transport`, old skills object to array) (`C:core/settings-manager.ts:291-360,501-540`) |
| Project | `<cwd>/.pi/settings.json` | Loaded only after project trust; otherwise treated as `{}` (`:471-475`) |

Merge: deep merge of nested objects, project over user (`deepMergeSettings`, `:248`). `defaultTools` merges specially: plain names replace; `+name`/`-name` modify the inherited list. Resource arrays combine; entries support `!glob` exclusion, `+path` inclusion, `-path` exclusion (`CD:settings.md`).

Global-only settings: `defaultProjectTrust`, `httpProxy`, `cacheWarming`.

### 15.2 Settings keys (complete list from `CD:settings.md`)

Model: `defaultProvider`, `defaultModel`, `defaultThinkingLevel`, `modelThinkingLevels`, `thinkingBudgets`, `enabledModels`, `hideThinkingBlock`, `showCacheMissNotices`, `cacheWarming`.
Interaction: `steeringMode`, `followUpMode`, `externalEditor`, `doubleEscapeAction`, `treeFilterMode`, `defaultProjectTrust`.
Tools: `defaultTools`, `codemode.mode`, `codemode.inlineBudget` (3000).
Sessions: `sessionDir`.
Compaction: `compaction.{enabled,reserveTokens,keepRecentTokens,modelOverrides}`; `branchSummary.{reserveTokens,skipPrompt}`.
Network: `transport`, `httpProxy`, `httpIdleTimeoutMs` (300000), `websocketConnectTimeoutMs` (15000), `retry.{enabled,maxRetries,baseDelayMs,maxAgentDelayMs}`, `retry.provider.{timeoutMs,maxRetries,maxRetryDelayMs}`.
Shell: `shellPath`, `shellCommandPrefix`, `npmCommand`.
Resources: `packages`, `extensions`, `skills`, `prompts`, `themes`, `enableSkillCommands`.
Images: `images.autoResize`, `images.blockImages`.
Telemetry and warnings: `enableInstallTelemetry`, `enableAnalytics`, `collapseChangelog`, `warnings.anthropicExtraUsage`.
Display keys (theme, TUI, terminal, markdown, fullscreen) are TUI-lane.

### 15.3 Project trust (`CD:security.md`, `C:core/trust-manager.ts`, `C:core/project-trust.ts`)

Protected resources: `.pi/settings.json`, `.pi/mcp.json`, `.pi/{extensions,skills,prompts,themes}`, `.pi/SYSTEM.md`, `.pi/APPEND_SYSTEM.md`, `.agents/skills` in cwd or ancestors. Decision order: `--approve`/`--no-approve` > `project_trust` extension event (user-level and CLI extensions only) > saved decision in `~/.pi/agent/trust.json` (nearest ancestor wins, canonical paths) > `defaultProjectTrust` (`ask` default). In print/JSON/RPC there is no prompt: only `always` loads protected resources. Project `sessionDir` is read before the trust decision (documented gap). Context files (AGENTS.md etc.) load regardless of trust.

There is no per-tool permission system: tools run with the process's OS permissions (`CD:security.md`). Approval is only possible through extension `tool_call` hooks (block/allow). Go: ask needs its own permissions layer (R-open T8).

## 16. Ring 1n: modes and protocols

### 16.1 Mode selection

`--mode rpc` > `--mode json` > print if `--print` or non-TTY stdin or stdout > interactive (`C:main.ts:112-122`). Invalid `--mode` value exits non-zero (coding-agent CL 0.99.x). RPC rejects `@file` args (`main.ts:651`). JSON/RPC/print take over stdout via `output-guard` so stray writes cannot corrupt the stream (`C:core/output-guard.ts`).

### 16.2 Print mode (`C:modes/print-mode.ts`)

Runs `initialMessage` then each `messages[]`; `text` mode writes text blocks of the final assistant message, exit 1 on `error`/`aborted`, error text to stderr; `json` mode writes the session header line then every event through `toJsonEvent`. SIGTERM (143) and SIGHUP (129) dispose the runtime and kill tracked detached children. Stdout backpressure honored per event in JSON mode.

### 16.3 JSON mode

JSONL, LF-terminated, split only on LF (not Unicode separators). First record is the session header, then events of section 6.4-6.6, ends with `agent_settled`. Diagnostics go to stderr.

### 16.4 RPC mode (`C:modes/rpc/*`, `CD:rpc.md`, `CD:rpc-commands.md`)

Framing: same JSONL. Commands have optional `id`; responses `{id?, type:"response", command, success, data?|error}`; parse failure yields a response with command `parse` and no id. Closing stdin shuts down cleanly. A `prompt` response means accepted, not finished: `data.disposition` is `started | queued | handled`; wait for `agent_settled` unless `handled`. Failures after acceptance appear only in events.

Commands (33; `C:modes/rpc/rpc-types.ts:22-74`):

| Group | Commands |
|---|---|
| Prompting | `prompt{message,images?,streamingBehavior?}`, `steer`, `follow_up`, `abort`, `clear_queue`, `new_session{parentSession?}` |
| State | `get_state` (model, thinkingLevel, isStreaming, isCompacting, steeringMode, followUpMode, sessionFile, sessionId, sessionName, autoCompactionEnabled, messageCount, pendingMessageCount), `get_messages` |
| Model | `set_model{provider,modelId}`, `cycle_model`, `get_available_models` |
| Thinking | `set_thinking_level`, `cycle_thinking_level`, `get_available_thinking_levels` |
| Queue | `set_steering_mode`, `set_follow_up_mode` |
| Compaction | `compact{customInstructions?}`, `set_auto_compaction` |
| Retry | `set_auto_retry`, `abort_retry` |
| Bash | `bash{command,excludeFromContext?}` (streams `bash_execution_update`), `abort_bash` |
| Session | `get_session_stats`, `export_html`, `switch_session`, `fork{entryId}`, `clone`, `get_fork_messages`, `get_entries{since?}`, `get_tree`, `get_last_assistant_text`, `set_session_name` |
| Discovery | `get_commands` (extension commands, prompt templates, skills; not TUI commands) |

Extension UI sub-protocol: `extension_ui_request` (select, confirm, input, notify, etc.) and `extension_ui_response` (`CD:rpc-extension-ui.md`). RPC client class `RpcClient` in `C:modes/rpc/rpc-client.ts` (617 lines).

Gaps for a server: no reconnect, no multi-client, no auth, single session per process, `get_messages` is the only catch-up.

### 16.5 SDK

`createAgentSession({cwd, model, thinkingLevel, scopedModels, modelRuntime, settingsManager, sessionManager, resourceLoader, tools, noTools, excludeTools, customTools})` returns `{session}`. `AgentSession` API: `prompt(text, {images, streamingBehavior, expandPromptTemplates})`, `steer`, `followUp`, `abort`, `waitForIdle`, `subscribe`, `setModel`, `cycleModel`, `setThinkingLevel`, `compact`, `navigateTree`, `executeBash`, `setActiveToolsByName`, `reload`, `dispose`, `getSessionStats`, `getContextUsage`, `exportToHtml/Jsonl`. `AgentSessionRuntime` adds `newSession`, `switchSession`, `fork`, `importFromJsonl` (each replaces the active session). Codemode, tool_search, MCP are not auto-loaded in the SDK (`CD:sdk.md`).

### 16.6 Env vars set for child processes

`AI_AGENT=pi`, `PI_CODING_AGENT=true` (CLI/RPC only). Shell tools receive `PI_SESSION_ID`, `PI_SESSION_FILE`, `PI_PROVIDER`, `PI_MODEL`, `PI_REASONING_LEVEL`, resolved per command; not injected for user `!` commands (`CD:environment-variables.md`).

Process config env: `PI_CODING_AGENT_DIR`, `PI_CODING_AGENT_SESSION_DIR`, `PI_PACKAGE_DIR`, `PI_OFFLINE`, `PI_SKIP_VERSION_CHECK`, `PI_TELEMETRY`, `PI_CACHE_RETENTION`, `PI_SHARE_VIEWER_URL`, `PI_RADIUS_GATEWAY`, `PI_EXPERIMENTAL`, `HTTP(S)_PROXY`.

### 16.7 CLI surface (`CD:cli.md`)

Flags: `-p/--print`, `--mode`, `--export`, `--provider`, `--model`, `--api-key`, `--thinking`, `--models`, `--list-models`, `-c/--continue`, `-r/--resume`, `--session`, `--session-id`, `--fork`, `--session-dir`, `--no-session`, `-n/--name`, `-t/--tools`, `-xt/--exclude-tools`, `-nbt`, `-nt`, `-e/--extension`, `-ne`, `--skill`, `-ns`, `--prompt-template`, `-np`, `--theme`, `-nc/--no-context-files`, `--system-prompt`, `--append-system-prompt`, `-a/--approve`, `-na`, `--offline`, `--verbose`, `--tui-mode`, `-h`, `-v`.
Subcommands: `install`, `remove`/`uninstall`, `update` (`--extensions`, `--models`, `--all`, `--self`), `list`, `config`, `auth`, `mcp`.

## 17. Ring 1o: other core features

| # | Feature | Detail | Source |
|---|---|---|---|
| 17.1 | Bash user command | `!cmd` runs and enters context; `!!cmd` runs but is excluded; recorded as `bashExecution`; flushed before next prompt if a run is active. | `C:core/agent-session.ts:3745-3843` |
| 17.2 | Shell resolution | `shellPath` setting, then Git Bash on Windows, then `/bin/bash`, `bash` on PATH, `sh`. `shellCommandPrefix` prepended to every command. | `C:utils/shell.ts:63-111` |
| 17.3 | Process kill | SIGTERM then SIGKILL after 5 s; detached children tracked and killed on exit. | `C:core/exec.ts:52-62`, `C:modes/print-mode.ts:66` |
| 17.4 | Packages | npm/git/local Pi packages (extensions, skills, prompts, themes); `pi install/update/list/config`; project-scoped with `-l`. 2760 lines, largest single file in core. | `C:core/package-manager.ts`, `CD:packages.md` |
| 17.5 | Slash commands (built-in, TUI) | `/settings /model /thinking /scoped-models /login /logout /llama /new /resume /name /session /tree /fork /clone /compact /import /copy /export /share /bug /trust /reload /hotkeys /changelog /quit` | `CD:slash-commands.md` |
| 17.6 | Bug report | `/bug` builds a private report (env, provider config without secrets, error diagnostics, optional transcript or model summary) and uploads to radius.pi.dev or exports zip. | `C:core/bug-report*.ts` |
| 17.7 | Telemetry | Anonymous install/update ping and provider attribution headers, on by default (`enableInstallTelemetry`), off with `PI_TELEMETRY=0` or `--offline`. Version check to pi.dev unless `PI_SKIP_VERSION_CHECK`. Agent telemetry schemas (spans) are contracts only. | `C:core/telemetry.ts`, `C:core/provider-attribution.ts`, `CD:environment-variables.md` |
| 17.8 | Diagnostics | Redacted `AssistantMessage.diagnostics`, crash log, settings diagnostics. | `C:core/diagnostics.ts`, `C:core/crash-log.ts` |
| 17.9 | Config value resolution | Literal, `$ENV`, `${ENV}`, `!command` in `models.json` and `auth.json`. | `C:core/resolve-config-value.ts` |
| 17.10 | HTTP | Custom dispatcher for proxy and idle timeout (`httpIdleTimeoutMs`). | `C:core/http-dispatcher.ts`, `AI:utils/node-http-proxy.ts` |
| 17.11 | Event bus | In-process bus for extensions. | `C:core/event-bus.ts` |
| 17.12 | Extension lifecycle events touching core | `input`, `before_agent_start`, `context`, `context_with_system`, `tool_call`, `tool_result`, `turn_end`, `agent_end`, `agent_before_settle`, `agent_settled`, `session_before_compact`, `session_compact`, `session_before_tree`, `session_tree`, `project_trust`, `model_select`. Extensions can queue `sendMessage` (custom message), `sendUserMessage`, `appendEntry`, `setActiveTools`, `registerProvider`, `registerVirtualModel`. | `CD:extensions.md`, `C:core/agent-session.ts:3275-3410` |
| 17.13 | Durable harness (not shipping) | Operation state machine (run/compaction/navigation), intent and settlement commits, tool `replay` policy, usage ledger, SQLite backend `packages/session-backends/sqlite-node`, Postgres future. | `AD:harness.md` |

---

## 18. Ring 3: interactions

| # | Interaction | What Pi does | Source |
|---|---|---|---|
| 18.1 | Abort x queue | `session.abort()` sets abort flags, cancels retry sleep, compaction, and branch summary, calls `agent.abort()`, waits for idle. It does **not** clear queues. The TUI calls `clearQueue()` and puts the texts back in the editor. RPC `abort` does not clear either; `clear_queue` is separate. | `C:core/agent-session.ts:2349-2360,2317-2330` |
| 18.2 | Abort x tools | Signal reaches `execute`; bash kills process tree; unstarted parallel tools return `Operation aborted`. Run ends when the assistant message is `aborted`. | `A:agent-loop.ts:614-624`, `C:core/exec.ts:52` |
| 18.3 | Abort x compaction | `abortCompaction()`; entry not appended; `compaction_end{aborted:true}`. Overflow omissions persist. | `agent-session.ts:2829` |
| 18.4 | Abort x retry | Sleep aborted; `auto_retry_end` with "Retry cancelled". | `:3648` |
| 18.5 | Prompt during compaction | Throws "Cannot submit a prompt while compaction is in progress". | `:1902` |
| 18.6 | Steering during a tool batch | Waits for the batch to finish; tools are never interrupted by steering. | `A:agent-loop.ts:288-300` |
| 18.7 | Compaction mid-run | Runs in `prepareNextTurn`/`prepareRequest` after tool results; steering queued meanwhile is picked up before `turn_start`. Skipped between-turn when the run is about to end. | `A:agent-loop.ts:183-193`, `CD:compaction.md` "When It Triggers" |
| 18.8 | Compaction x session tree | Compaction is an entry on the current branch; other branches keep their own history. Forking/cloning a branch copies its latest compaction with a remapped `firstKeptEntryId`. Context building always uses the latest compaction on the **path**. | `session-manager.ts:476-543,1632` |
| 18.9 | Compaction x branch summary | Branch summaries can be summarized by later compactions like any message; file-op lists carry through nested Pi-generated summaries, not extension-generated (`fromHook:true`). | `CD:compaction.md` "Cumulative File Tracking" |
| 18.10 | Compaction x virtual model | Threshold checked with the physical model's window per request; if the routed model's window is too small the request compacts first. | `agent-session.ts:796-800`, `CD:virtual-models.md` |
| 18.11 | Compaction x system prompt | The `compaction` entry carries the full prompt/tool checkpoint; older system messages among kept entries are dropped in its favor. | `CD:session-format.md` (CompactionEntry) |
| 18.12 | Compaction x extensions | `session_before_compact` can cancel or supply the summary; `fromHook` marks it; extension `details` stored verbatim. | `CD:compaction.md` |
| 18.13 | Retry x context edit | Failed attempt stays in raw history but is omitted from projection by a `context_edit{replacement:null}`; the next request never sees it. Retry x virtual model: router gets `reason:"retry"` and `failed` (model, message). | `agent-session.ts:3695-3700`, `CD:virtual-models.md` |
| 18.14 | Model switch mid-session | Allowed any time; prompt cache is lost; foreign thinking becomes tagged text; images downgrade if new model lacks vision; thinking level re-clamped; `model_change` and maybe `thinking_level_change` written. | `AI:api/transform-messages.ts`, `agent-session.ts:2444-2500` |
| 18.15 | Tool changes mid-session | A `system` message with `toolsAdded/toolsRemoved` is inserted before the next request; providers with native support (Anthropic deferred tools) keep cache, others collapse into a rebuilt system message. | `A:agent-loop.ts:333-380`, `AI:api/anthropic-messages.ts:193-200,1060-1129` |
| 18.16 | `context_edit` x accounting | Edits change future context only; usage, UI, exports keep the original. Usage captured before an edit is discarded for context-size estimates. | `CD:session-format.md`, `compaction.ts:227-260` |
| 18.17 | Nested tools x session | Codemode tool results record `nestedCalls` and merge usage; those are not sent to the model. | `agent-session.ts:1060-1073` |
| 18.18 | Trust x settings x sessions | Project `sessionDir` is read before trust; everything else project-level waits for the decision. | `CD:security.md` |
| 18.19 | `agent_end` vs `agent_settled` | `agent_end` can be followed by retry, compaction retry, queued messages; only `agent_settled` means done. Prompts arriving during `agent_settled` emission are deferred. | `agent-session.ts:1018-1050,1883-1888`, `CD:json.md` |
| 18.20 | Steering text matching | Queue removal on `message_start` matches by text equality with the queued string, so two identical queued messages remove the first match only. | `agent-session.ts:1074-1093` |

## 19. Ring 4: edge cases

| # | Edge case | Behavior | Source |
|---|---|---|---|
| 19.1 | Tool call args truncated by `length` | All calls in the message fail; no partial execution. | `A:agent-loop.ts:478` |
| 19.2 | Unknown tool | Error result `Tool X not found`; loop continues. | `A:agent-loop.ts:715` |
| 19.3 | Invalid tool args | Validation error text becomes the tool result. | `A:agent-loop.ts:759-775` |
| 19.4 | Stream ends with no `done` | Final message taken from `result()`; if no partial was added, `message_start` is emitted first. | `A:agent-loop.ts:466-476` |
| 19.5 | Empty error assistant messages | Must not break tool_use/tool_result chain; filtered in `transformMessages` (ai CL). | `AI:api/transform-messages.ts` |
| 19.6 | Orphaned tool calls | Synthetic error results are inserted for tool calls without results before the next request (ai tests, CL). | `AI:api/transform-messages.ts` |
| 19.7 | Tool-call id incompatibility across providers | ids normalized per target API (length and charset rules). | `AI:api/transform-messages.ts:64-140` |
| 19.8 | Silent overflow | `usage.input` above window on a "stop" reply triggers compaction on the next check. | `AI:utils/overflow.ts:153` |
| 19.9 | Xiaomi MiMo truncation | Length stop with zero output and full window counts as overflow. | `AI:utils/overflow.ts:163` |
| 19.10 | Bedrock throttling text "Too many tokens" | Excluded from overflow by `NON_OVERFLOW_PATTERNS`. | `AI:utils/overflow.ts:130` |
| 19.11 | Stale usage after compaction | Kept pre-compaction messages carry old large usage; explicitly ignored so compaction does not loop. | `agent-session.ts:2965-2990` |
| 19.12 | Second overflow in same turn | Fails with "Context overflow recovery failed after one compact-and-retry attempt". | `agent-session.ts:2934-2955` |
| 19.13 | Compaction summarizer refused by model | Split-turn summaries were refused by one model; prompt now separates conversation clearly and uses continuation wording. | `coding-agent/CHANGELOG.md` (0.99.x Fixed) |
| 19.14 | Session file created late | No file if only setup entries exist. First user message triggers create. | `session-manager.ts:1160-1188` |
| 19.15 | Partial last line in JSONL | Blank and malformed lines skipped; a torn tail is not repaired in the shipping manager (the harness repairs and publishes atomically). | `session-manager.ts:627-670`, `agent/CHANGELOG.md` 0.84.0 |
| 19.16 | Cross-project `--session` | Searches current project, offers fork. | `CD:cli.md` |
| 19.17 | `edit` with BOM or CRLF | BOM stripped for matching, restored on write; line endings preserved. | `C:core/tools/edit.ts:190`, `edit-diff.ts:11-25` |
| 19.18 | `edit` overlapping or duplicate `oldText` | Error, no partial write. | `edit-diff.ts:300-360` |
| 19.19 | `read` of huge single line | Message suggests `sed -n Np | head -c`. | `read.ts:161` |
| 19.20 | `bash` output flood | Tail kept in memory, full output spilled to temp file; snapshot notes "Full output: path". | `output-accumulator.ts`, `bash.ts:355` |
| 19.21 | Stdout backpressure in JSON/RPC | Loop awaits drain per event; a stalled reader stalls Pi. | `print-mode.ts:110-115`, `CD:rpc.md` "Framing" |
| 19.22 | Unicode separators in JSONL | Must split on LF only (U+2028/2029 valid inside strings). | `CD:json.md` |
| 19.23 | `auth.json` lock compromised | Operation throws; stale lock after 30 s. | `auth-storage.ts:166-200` |
| 19.24 | Command-based key fails once | Stays unresolved until restart (no retry). | `CD:providers.md` |
| 19.25 | Missing model on resume | Falls back to available model; virtual model missing falls back to last physical model. | `model-resolver.ts:711`, `CD:virtual-models.md` |
| 19.26 | Anthropic signed-thinking mismatch through relays | Recovery path exists for supported models; relays reporting a different response model can break replay (fixed in ai CL). | ai CL 0.85.0, 0.99.0 |
| 19.27 | OpenAI Responses stream lacks `output_index` | Ends in error instead of running mixed-up commands. | ai CL 0.99.0 |
| 19.28 | Non-JSON-safe tool details | Type-level restriction to JSON values since ai 0.86.0. | ai CL 0.86.0 |

## 20. Ring 5: history (what changed and why)

Timeline from `agent/CHANGELOG.md` and `ai/CHANGELOG.md` (dates 2026):

| Version (date) | Change | Why it matters for Go |
|---|---|---|
| ai 0.86.0 (Sep 19) | Providers take normalized `TranscriptContext`; system prompt and tools live in transcript system messages; mid-conversation prompt and tool changes; `ToolCall.arguments` and tool `details` JSON-only. | Start with the system-message-in-transcript design. |
| agent 0.87.0 (Sep 21) | `shouldStopAfterTurn` removed; `finishTurn`, `prepareRequest`, `peekQueuedMessages` added. | Hook set has stabilized around these three. |
| coding-agent 0.87.0 | `ContextEditEntry`; `SessionManager` canonical for provider context; retain-none compaction; extension boundary hooks. | Context = projection of tree, never `agent.state.messages`. |
| agent 0.84.0 (Aug 6) | v4 lane-based `Session`/`SessionStorage`/`SessionRepo`; atomic JSONL publish via `renameFile`; telemetry schemas. | Harness path exists but not shipped in CLI. |
| ai 0.85.0 (Sep 4) | Assistant-message frames; Anthropic per-turn effort persistence; Meta provider; `vllmPriority`. | |
| ai 0.99.0 (Sep 29) | Image and classifier models join `Provider`/`Models`; catalog schema v6; `Models.classify()`; `onProviderStreamEvent`; Sign in with ChatGPT on `openai`; `thinkingLevel` recorded on assistant messages. | Model type dimension is now first-class. |
| ai 0.99.1 (Sep 29) | New model additions only. | Catalog churn is weekly; generate, do not hand-write. |
| Earlier (from CL headings) | 0.72.0 to 0.80.0 (Apr to Jun): session tree, extension unification (`hookMessage` renamed `custom`, session v3), compaction, OAuth providers, the `models.json` custom endpoint flow. Exact per-version content not reviewed. | Not read in detail. |

Breaking-change cadence: `ai/CHANGELOG.md` has 15+ "Breaking Changes" sections across 207 releases; API surface changes often. Adoption note: copy behavior, not TypeScript type shapes.

---

## 21. Suggested split for the Go rewrite (for the planner; not a decision)

Tier A: needed for a working harness.
- Agent loop with steer and follow-up queues, parallel tool execution, truncation guard, hooks (3.x).
- Transcript with `system` sections/tool patches; four message roles; content blocks (5, 6).
- Providers: anthropic-messages, openai-completions, openai-responses; usage and cost; thinking clamp and budgets; retry classifier; overflow detector (7, 8, 13, 14.1).
- Tools: read, write, edit (with fuzzy match), bash (tail truncation and spill), grep, find, ls (4).
- Session tree with `parent` links; `compaction`, `branch_summary`, `context_edit`, `model_change`, `thinking_level_change`, `usage`, `custom`, `custom_message` entry types (12).
- Threshold and overflow compaction with the exact summary format (14).
- Settings layering with trust gate; AGENTS.md discovery; skills listing; prompt templates (5, 15).

Tier B: soon after.
- Bedrock, Google, Mistral, Codex WebSocket; OAuth flows (Anthropic, ChatGPT, Copilot); cache warming; image resize; virtual models; MCP; JSON and RPC mode parity for tools like `pi` clients.

Tier C: skip or defer.
- Codemode, tool_search, packages, TUI commands, export HTML, `/share`, bug reports, telemetry, classifier and image models, deferred requests, llama.cpp router.

---

## 22. Limitations of this research

- Extension internals, TUI, `packages/mcp`, `packages/codemode`, `packages/durable`, `packages/server`, `packages/client`, `packages/chord` were not read.
- Per-provider quirks in `openai-completions.ts` (1726 lines), `openai-codex-responses.ts`, `bedrock-converse-stream.ts`, and `mistral-conversations.ts` were sampled through headers, grep and changelog only; the compat matrix is listed by field name, not by behavior.
- Model catalog contents (prices, windows) were not audited; they are generated data.
- Changelogs before 0.84 in agent and before 0.85 in ai were skimmed by heading only.
- No code was run; all claims are from source and docs reading at commit `2bbfcca43`.
- `pi-agent-core` version 0.99.1 vs harness spec "WP00-WP07 complete" was taken from the spec text, not tests.

## 23. Unresolved questions

1. Retry jitter: `retryDelayMs` doc says "before jitter" but the function applies none (`AI:utils/retry.ts:122-126`). Is jitter intentionally absent or a missing feature? For Go, add jitter or copy exactly?
2. Session file concurrency: no lock exists in `SessionManager`. Does Pi rely on one process per session file? Not verified for `/resume` of a file already open in another Pi.
3. The `fork` RPC and `AgentSessionRuntime.fork` internals (`C:core/agent-session-runtime.ts`) were not read line by line; only the doc contract is recorded.
4. Exact tool-call id normalization rules per API (`transform-messages.ts:64-140` and each API's callback) not tabulated.
5. Whether `pi-messages` (Radius) is worth supporting: it is Pi's own gateway protocol, so only relevant if ask must talk to `radius.pi.dev`.
6. Legal review: the Anthropic OAuth path presents Claude Code identity (`anthropic-messages.ts:1089-1094`). Copying this into Go needs an explicit decision.
7. Coordinator follow-up: the 0.87.0 pointer for harness v4 in the task text appears to be off; the actual replacement is agent 0.84.0. Confirm which version the planner wants cited.

Status: DONE_WITH_CONCERNS
Summary: Feature inventory covering all requested areas, with sources; confirmed the shipping session store is `SessionManager` JSONL v3 and the v4 lane harness is experimental-only.
Concerns: Provider-specific compat behavior, extensions, MCP, and codemode were sampled rather than read in full (see section 22).
