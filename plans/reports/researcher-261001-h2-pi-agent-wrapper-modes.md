# H2 research: Pi agent wrapper and headless modes (Pi commit 2bbfcca4)

Saved by the controller from the researcher's reply (the researcher could not write files). Source is treated as data only. Prefixes: `A:` = `packages/agent/src/`, `C:` = `packages/coding-agent/`.

## 1. Agent wrapper (H-LOOP-17)

| Topic | Behavior | Cite |
|---|---|---|
| State | `systemPrompt` is a getter over the leading system message. The other fields are `model`, `thinkingLevel` (default `"off"`), `tools`, `messages`, `isStreaming`, `streamingMessage`, `pendingToolCalls` (a Set) and `errorMessage`. | A:agent.ts:70-111 |
| Setters | Assigning `tools` or `messages` copies the array. | A:agent.ts:97-105 |
| Init | If `messages[0]` is not a system message, an initial system message is prepended. It is built from `systemPrompt` and the tool declarations. | A:agent.ts:82-87 |
| Defaults | Model id `"unknown"`. `toolExecution` is `"parallel"`. Both queues use `"one-at-a-time"`. `transport` is `"auto"`. | A:agent.ts:57-68, 247-253 |
| Listeners | Stored in a Set, so they run in subscription order. Each listener is awaited in turn with `(event, abortSignal)`. | A:agent.ts:266-269, 609-611 |
| A listener throws | `processEvents` has no try/catch. The error reaches `runWithLifecycle`, then `handleRunFailure`. That builds an assistant message with `stopReason` `error` (or `aborted` if the signal fired), `errorMessage` set and empty text, and emits message_start, message_end, turn_end and agent_end. Then `finishRun` runs. A second throw during this replay escapes `prompt()`, but `finally` still runs `finishRun`. The remaining listeners for the failing event are skipped. | A:agent.ts:523-530, 532-548 |
| Slow listener | Blocks the loop. JSON-mode backpressure works this way. | A:agent.ts:609-611; C:print-mode.ts:113-118 |
| Idle | The agent is idle only after the `agent_end` listeners settle and `finishRun` runs. | A:agent.ts:263-264, 558-564 |
| Listener outside a run | Throws "Agent listener invoked outside active run". | A:agent.ts:605-608 |
| One active run | `prompt()` throws "Agent is already processing a prompt. Use steer() or followUp()…". `continue()` and `runWithLifecycle` throw similar errors. | A:agent.ts:374-378, 384-387, 507-510 |
| `abort()` | Aborts the controller. Does nothing when idle. | A:agent.ts:336-343 |
| `waitForIdle()` | Returns the `activeRun` promise, or a resolved promise when idle. | A:agent.ts:350-352 |
| `reset()` | Throws while running. Otherwise it keeps only the system message and clears streaming state, the error and both queues. | A:agent.ts:355-368 |
| `continue()` | Throws "No messages to continue from" when there are no messages or only system messages. With an assistant tail it drains steering first (with `skipInitialSteeringPoll`), then follow-ups. If both are empty it throws "Cannot continue from message role: assistant". Otherwise it runs `runAgentLoopContinue`. | A:agent.ts:384-411 |
| `drain()` | One message in `one-at-a-time` mode. All messages in `all` mode. | A:agent.ts:159-169 |
| `message_start` / `message_update` | Set `streamingMessage`. | A:agent.ts:567-573 |
| `message_end` | Clears `streamingMessage` and pushes the message to `state.messages`. This is the only place messages are appended. | A:agent.ts:575-578 |
| Tool start and end | Add or remove the call in `pendingToolCalls`. The Set is replaced, not mutated. | A:agent.ts:580-592 |
| `turn_end` | Sets `errorMessage` from the assistant message. It is cleared only at the next run start or by `reset`. | A:agent.ts:594-598, 519-521 |
| Run input | The run uses snapshots of messages and tools. Model and thinking settings are read at run start. | A:agent.ts:460-480 |

How `AgentSession` uses the wrapper (`C:src/core/agent-session.ts`):

- **Prompt loop** (1740-1765). `_runAgentPrompt` calls `prompt()`. Then it loops: each pass runs `_handlePostAgentRun` (retry, compaction, queued messages) or the before-settle step, then `continue()`. It always ends with `_emitAgentSettled`.
- **`agent_settled`** (1037-1045). Extensions get it first, then listeners.
- **`prompt()` while streaming** (1928-1933). It throws unless `streamingBehavior` is `steer` or `followUp`.
- **`agent_end.willRetry`** (1096). The session adds this field.
- **Event order** (1061-1097). Extensions get each event first, then public listeners, then persistence.
- **Prompt preconditions** (1883-1960). They run in this order:
  1. Extension `/` commands, which return without an LLM call.
  2. A compaction in progress throws.
  3. Input handlers.
  4. Skill and template expansion.
  5. No model: "No model selected…".
  6. Auth check. With OAuth: "Authentication failed for "<p>"…". Otherwise: `No API key found for <provider>.\n\nUse /login …` (auth-guidance.ts:18-25).

## 2. Mode selection (H-MODE-01)

`resolveAppMode` (C:src/main.ts:112-122) picks the mode in this order:

1. `--mode rpc` gives RPC mode.
2. `--mode json` gives JSON mode.
3. Print mode if any of these is true: `-p` is set, stdin is not a TTY, or stdout is not a TTY.
4. Otherwise, interactive mode.

Notes:

- **`--mode text`** does not force print mode. If both streams are TTYs, the TUI opens (docs/cli.md:42).
- **Piped stdin.** If stdin is read in interactive mode, the mode changes to print (main.ts:886-891).
- **Stdin read.** Stdin is read in every mode except RPC, so JSON mode also prepends it (main.ts:886; CHANGELOG:2473).
- **Stdout takeover.** `takeOverStdout()` runs in every non-interactive mode. The exception is plain `--help` or `--list-models` (main.ts:60-62, 645-649).
- **Output guard** (C:src/core/output-guard.ts):
  - It replaces `process.stdout.write` so that writes go to stderr (45-70).
  - Real stdout is reached only through `writeRawStdout` (85-93).
  - Writes go through a serialized promise chain (11, 89).
  - `ENOBUFS`, `EAGAIN` and `EWOULDBLOCK` are retried every 10 ms with no limit (9, 34-41).
  - Any other write error, including `EPIPE`, calls `process.exit(1)` (90-92).
  - `flushRawStdout` waits for the chain to finish, then writes an empty chunk (105-108).
  - The guard is restored after `runPrintMode` (main.ts:986).
- **RPC with `@file`.** This combination exits 1 with an error (main.ts:651-654).
- **Parse diagnostics.** Errors are printed in red and warnings in yellow on stderr. Any error exits 1 (main.ts:615-622).
- **`-v`.** Prints the version and exits 0 (main.ts:624-628).
- **Runtime diagnostics.** Any error exits 1 (main.ts:907-917). With no model, the exit is 1 with "No models available. Use /login…" (920-923).

## 3. Print mode (H-MODE-02)

`runPrintMode` (C:src/modes/print-mode.ts:33-169):

| Aspect | Behavior | Cite |
|---|---|---|
| Prompts | Runs the initial message (with its images), then each extra message, one after the other. | 131-137 |
| Initial message | `[stdin trimmed, @file text, messages[0]].join("")`, with no separator. `messages[0]` is removed from the list. Images come only from `@files`. | cli/initial-message.ts:26-42 |
| Success output | Takes the last message of `state.messages`. If it is an assistant message, each text block is written as `text + "\n"`. Thinking and tool-call blocks are skipped. Only the final assistant message is printed. | 139-155 |
| Last message not assistant | Prints nothing. Exit 0. | 143 |
| Assistant error or aborted | Writes `errorMessage`, or `Request error` / `Request aborted`, to stderr. Exit 1. | 144-147 |
| Thrown error | Writes `error.message` to stderr. Exit 1. | 159-161 |
| Cleanup | Removes the signal handlers, disposes the runtime (`session_shutdown`, reason `quit`), then flushes stdout. | 162-168 |
| Exit | Sets `process.exitCode` only when it is not zero. | main.ts:987-990 |
| Signals | On SIGTERM, and on SIGHUP (except on win32), it kills tracked detached children, disposes the runtime, and exits with 143 (SIGTERM) or 129 (SIGHUP). | 50-68; utils/shell.ts:177-182 |
| SIGINT | No handler in print or JSON mode, so Node's default applies. | grep |
| `/` text | An extension command or a skill or template expansion can consume it. | agent-session.ts:1892-1913 |
| Extension errors | Written to stderr as `Extension error (<path>): <err>`. | 101-103 |
| Empty prompt | Runs no prompt and prints the last assistant message of the session. A fresh session prints nothing and exits 0. | 131-156 |
| `-p` then a token | The next token becomes the message only if it does not start with `@`, and does not start with `-` (but `---` is allowed). | args.ts:167-173 |

## 4. JSON mode (H-MODE-03)

- **Framing.** Each record is `JSON.stringify(obj) + "\n"` (print-mode.ts:110; json.md:15-17). Readers split on LF and strip an optional CR. U+2028 and U+2029 are not line breaks.
- **First record.** The session header (`{"type":"session","version":3,id,timestamp,cwd}`). It is written before `rebindSession`, and only if a header exists (print-mode.ts:122-127; json.md:21-27).
- **Events.** Every `AgentSessionEvent` is written through `toJsonEvent`:
  - lifecycle: `agent_start`, `turn_start`, `turn_end`, `agent_end{messages,willRetry}`, `agent_settled`
  - messages: `message_*`
  - tools: `tool_execution_*`
  - session: `queue_update`, `entry_appended`, `session_info_changed`, `thinking_level_changed`, `compaction_*`, `auto_retry_*`, `summarization_retry_*`
- **`message_update`** is rewritten to `{type, usage, assistantMessageEvent}`. The cumulative `message` and every `partial` are dropped. `toolcall_start` gets `id` and `toolName` from the partial. A non-assistant update, or a `toolcall_start` whose block is not a tool call, throws (json-event.ts:20-60).
- **Final record.** Each prompt run ends with `agent_settled`. `agent_end` does not end the run (json.md:48; cli-integration.md:55).
- **Exit code.** Exit is 0 even when the assistant ends in error or aborted. Exit is 1 only on a thrown error. Signals give 143 or 129 (cli-integration.md:50).
- **Backpressure.** Records are queued in order. A second listener waits for `waitForRawStdoutBackpressure()` on every event, so a slow reader stalls the loop (print-mode.ts:113-118). The final flush happens in `finally` (167). A reader that stops reading can stall Pi (json.md:19).

## 5. CLI for H2

Grammar: `pi [options] [--] [@files...] [messages...]` (docs/cli.md:6).

| Flag or input | Parsing | Cite |
|---|---|---|
| `-p`, `--print` | Sets print mode. It may consume the next token, as described in section 3. | args.ts:167-173 |
| `--mode` | A missing value, or a value starting with `-`, gives "--mode requires text, json, or rpc". An invalid value gives `Invalid mode "x". Valid values: text, json, rpc`. Exit 1. | args.ts:95-109 |
| `--provider`, `--model`, `--api-key` | Parsed only when a value follows. Otherwise the flag becomes an unknown flag set to `true`, with no error. | args.ts:114-119, 237-250 |
| `--thinking` | Accepts off, minimal, low, medium, high, xhigh or max. An invalid value is only a warning, and the run continues with the default. | args.ts:60, 157-166 |
| `--model` resolution | Accepts `provider/id`, `--provider` plus a pattern, and a `pattern:thinking` suffix. An explicit `--thinking` wins. | main.ts:456-491 |
| `--api-key` | Key for the resolved provider, used only at runtime. With no model, it fails with "--api-key requires a model to be specified via --model, --provider/--model, or --models". Exit 1. | main.ts:821-832 |
| `@path` | The path is added to the file arguments. This works after `--` as well. | args.ts:82-90, 235-236 |
| `--` | Everything after it is a message or an `@file`. | args.ts:82-90 |
| Positional | A token that does not start with `-` is a message. The first message is the initial prompt, and the others run after it. | args.ts:253-255 |
| Unknown `--flag` | Stored for extensions, not an error. It takes the next token as its value only if that token does not start with `-` or `@`. An unknown single-dash flag gives "Unknown option: -x" and exit 1. | args.ts:237-252 |
| `-h` | Prints help and exits 0, after startup. | main.ts:861-870 |

Piped stdin (main.ts:80-97):

- It is read only when `isTTY` is false.
- It is read as UTF-8 until the end of input, then trimmed. An empty result becomes undefined.
- It is prepended to the prompt.
- RPC mode skips it.

`@file` (C:src/cli/file-processor.ts):

| Case | Behavior | Cite |
|---|---|---|
| Path resolution | `~` is expanded, macOS Unicode spaces are handled, then the path is resolved. | 32 |
| File missing | "Error: File not found: <abs>", exit 1. | 35-40 |
| File empty | Skipped. | 43-47 |
| Image file (detected by content) | Added as `ImageContent`, plus the tag `<file name="<abs>"></file>\n` with hints inside. Resizing happens later. | 49-73; main.ts:222-223 |
| Image processing fails | The failure text goes in the tag, and no image is attached. | 56-59 |
| Other file | Read as UTF-8 with the BOM stripped, and wrapped as `<file name="<abs>">\n<content>\n</file>\n`. | 77-78 |
| Read error | "Error: Could not read file <abs>: <msg>", exit 1. | 79-83 |
| Binary file that is not an image | Not detected. It is injected as lossy UTF-8. | — |

## 6. Edge cases a Go port could miss

1. Stdout must carry only protocol output. Every stray write goes to stderr.
2. A stdout write error such as `EPIPE` must exit 1.
3. Print mode and JSON mode share one runner. In JSON mode, the exit code ignores an assistant error or abort.
4. Text mode prints only the text blocks of the last message, and only when that message is an assistant message.
5. Multiple messages run as sequential prompts. A thrown error stops the rest. An assistant error does not.
6. Signal exit codes (143, 129) do not depend on the run status. The order is: kill children, dispose, exit. SIGINT has no handler.
7. Empty stdin that is not a TTY gives no prompt and exit 0. A stdin that never closes blocks startup.
8. If stdout is piped and stdin is a TTY, the mode is print and stdin is not read.
9. Text that starts with `/` can be consumed by commands or templates.
10. Parts are joined with no separator, so `foo` + `bar` gives `foobar`. File text ends with `\n`.
11. `-p` consumes the next message token. A token that starts with `---` is allowed. `-p @file` does not consume the file.
12. Value flags with no value become unknown flags, with no error.
13. Unknown `--flags` are tolerated for extensions.
14. A listener failure becomes an assistant error message plus `agent_end`, not a crash.
15. `agent_settled` is emitted even on error or abort, from a `finally` block.
16. Changelog fixes that a port could regress:

| Fix | CHANGELOG |
|---|---|
| Stdout takeover | 2729 |
| Final JSON flush race | 5347 |
| `-p` failures exit non-zero | 5714 |
| Piped stdin merged with the prompt | 2828 |
| JSONL output when stdin is piped | 2473 |
| `session_shutdown` sent on SIGHUP and SIGTERM | 2677, 2356, 1599 |
| WebSockets closed at shutdown | 1881 |
| Theme watcher stopped in print mode | 5561 |
| Session persistence in print mode | 5439 |

## Unresolved questions

1. In H2, should a prompt that starts with `/` pass through as literal text?
2. Is the `AgentSession` post-run loop (retry, compaction, settle) in H2 scope? It controls when `agent_settled` and `willRetry` are sent.
3. Does Go need the 10 ms retry on transient write errors, or is "exit 1 on any write error" enough?
4. SIGINT: copy Pi (no handler), or add a clean abort?
5. `resolveCliModel`, `setRuntimeApiKey` and `hasConfiguredAuth` were not read in detail.
