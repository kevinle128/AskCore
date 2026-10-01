# Pi edge-case catalogue for the Ask (Go) rewrite

Lane C. Pi repo `/Users/dale/Desktop/workspace/opensources/pi` at 2bbfcca4, read only. Date 2026-09-30.

## 1. Outcome

Pi's changelogs record about 1,820 distinct fixes (after removing "inherited" duplicates that coding-agent re-lists from ai and tui, and pure feature "Changed" lines). Roughly 60% of them are a lower-level protocol problem that Go will meet too: provider wire quirks, session persistence, process control, terminal protocol, and Unicode width. About 25% are Pi-specific to TypeScript, Node, Bun, npm, or jiti packaging. The rest is product churn (model catalogs, defaults, docs).

Three hard areas dominate: provider quirks (thinking replay, tool-call IDs, usage and cost), TUI input and rendering across terminals, and session state races (compaction, retry, queueing, fork). A Go rewrite that designs these three as first-class subsystems avoids most of the recorded pain.

## 2. Method and coverage

- Pass 1: extracted every bullet under `### Fixed` and `### Changed` in all 12 changelogs with awk (2,464 bullets), then read them all in chunks. Bullets were truncated to about 215 characters; where a bullet was vague I checked source.
- Pass 2: clustered by area (sections 4 to 22).
- Pass 3: counted repeat offenders (section 3).
- Per-file result: coding-agent (214 Fixed sections, 1,463 bullets), ai (466), tui (203), agent (35), durable (2), server (2), sqlite-node (2), mcp (1), codemode (1). `protocol`, `client`, and `telemetry` changelogs have no Fixed section (only Added or Breaking), so they contribute nothing here.
- Not covered: bullet text beyond about 215 characters, linked issues and PR discussions, and code paths behind each fix. Only the spot checks in section 23 were read at source.

"Go?" column key: **Y** = applies to Go as is. **G** = applies to Go but the failure looks different. **P** = Pi-specific (TypeScript, Node, Bun, npm, jiti); skip. Version is the first release with the fix. Packages: CA = coding-agent, AI = ai, TUI = tui, AG = agent.

## 3. Repeat offenders (hard areas)

Counts are keyword hits over about 1,820 fixes and overlap between rows, so treat them as relative size, not exact.

| Rank | Area | Approx. fixes | Reading |
|---|---|---|---|
| 1 | Session, fork, branch, resume, JSONL | 240 | State machine with many entry points; fixed at every minor version. |
| 2 | Thinking/reasoning replay and levels | 190 | Every vendor differs; regressions recur (OpenAI Responses replay fixed at 0.49.2, 0.56.3, 0.56.2, 0.80.3, 0.84.3). |
| 3 | OpenAI-compatible and Responses streams | 189 | The "compatible" label hides per-server behaviour. |
| 4 | Packaging, install, update | 177 | Mostly Pi-specific (npm, pnpm, Bun). Skip. |
| 5 | Extensions | 142 | Lifecycle and stale-context bugs. Applies if Ask has hooks. |
| 6 | Width, Unicode, ANSI, OSC | 136 | Fixed about 40 times in tui alone. |
| 7 | Context overflow and compaction | 134 | Detection regexes per vendor plus the compaction state machine. |
| 8 | Rendering, flicker, overlay, scrollback | 129 | Differential renderer bugs. |
| 9 | Autocomplete, paste, editor | 128 | Paste markers, IME, history. |
| 10 | Tool call/args/schema/strict | 124 | Each vendor rejects different schema keywords. |
| 11 | Cache, pricing, cost, usage | 113 | Wrong price tier or double-counted tokens, fixed about 25 times. |
| 12 | Catalog and defaults | 111 | Product churn. Data-driven design avoids it. |
| 13 | Symlink, path, find, grep | 115 | Cross-platform path handling. |
| 14 | Abort, cancel, race, serialize | 108 | Concurrency bugs; see section 12. |
| 15 | Bash, process, shell | 106 | See section 5. |
| 16 | Auth, OAuth, credentials | 103 | Locking and refresh. |
| 17 | Keyboard, Kitty, tmux | 97 | Terminal protocol matrix. |
| 18 | Retry, backoff | 82 | Pattern list keeps growing (14 additions). |
| 19 | Windows/WSL | 68 | Every subsystem has a Windows variant. |
| 20 | Images | 74 | Format sniffing, resize, terminal protocols. |

Single most repeated fix: **retry/overflow error classification by string match**. New vendor error strings were added in at least 25 releases (sections 8 and 9). Second: **Kitty/legacy key decoding**, fixed in at least 30 releases.

## 4. Tool execution (agent loop)

| Edge case | Ver | Pkg | Go? | Suggested test |
|---|---|---|---|---|
| Steering message queued while a tool-call batch runs must wait until the whole batch finishes, not skip pending calls | 0.58.4 | AG | Y | Queue steer mid-batch; assert all N tool results precede the steer message |
| Parallel tools: emit `tool_execution_end` when each finishes, but persist tool results in assistant source order | 0.68.1 | AG | Y | Two tools, second finishes first; assert event order vs persisted order |
| Hook (`afterToolCall`) that throws must become an error tool result, not abort the batch | 0.67.67 | AG | Y | Panicking hook on tool 2 of 3; other results intact |
| `tool_result` hook on error results dropped `details`/`isError` overrides | 0.67.67 | CA | Y | Hook overrides isError; assert persisted |
| After abort, stop preparing sibling tool calls (preflight) | 0.75.4 | AG | Y | Cancel during preflight of call 1; call 2 never starts |
| Late progress callback after tool settled must be ignored (no stale `tool_execution_update`) | 0.79.2 | AG | Y | Fire progress after Done; assert no event |
| Tool call from a length-truncated assistant message must fail, not wait for a result that never comes | 0.80.4 | AG | Y | `stop_reason=length` with partial tool call; run ends with error |
| Tool that returns `isError:true` is not a failure; only throwing/error result counts (doc bug that misled authors) | 0.56.3 | CA | Y | One error convention only; test both paths |
| Extension messages with `triggerTurn:false` inserted between a tool call and its result; providers reject bad message order | 0.84.4 | CA | Y | Inject custom message during tool run; assert it lands after results |
| Context/message filter handlers that slice messages dropped prompt and tool declarations | 0.87.0 | CA | Y | Filter that keeps last N; tools still declared |
| Tools registered after session start must become active immediately | 0.55.4 | CA | Y | Register mid-session; next request lists tool |
| Tool set changes must apply before the next provider request in the same run | 0.80.3 | CA | Y | Change tools in a hook; assert next request payload |
| Empty `tools` array sent to providers that reject it | 0.70.3 | CA | Y | Tools disabled; request has no `tools` key |
| Tool args: malformed (object where string expected) crashed | 0.52.0 | CA | Y | Fuzz args; no panic |
| Tool args: string numbers ("5") coerced to numbers during validation | 0.48.0 | AI | Y | Schema int, input "5" |
| Coercion must not convert `null` inside `anyOf`/`oneOf` to another primitive | 0.84.0 | AI | Y | Nullable union keeps null |
| Provider omits tool inputs; default to empty object | 0.50.0 | AI | Y | Tool call with no args |
| `edit` tool: model sends `edits` as a JSON string, a single object, or invents extra fields | 0.68.0, 0.84.3, 0.80.4 | CA, AG | Y | Three payload shapes accepted; extra fields ignored |
| Models sent `null` for optional `offset`/`limit` | 0.99.0 | CA | Y | Null treated as omitted |
| Flag-like search patterns (`-e ...`) injected as options into grep/find | 0.71.0 | CA | Y | Pattern `--files` is literal |
| Tools ignored `ctx.cwd` (SDK and harness) | 0.26.1, 0.85.0 | CA | Y | Run tool with cwd != process cwd |
| Extension tool without schema broke every provider request; reject at registration | 0.86.0 | CA | Y | Register schema-less tool; error, not later 400 |
| Tool usage (cost of nested LLM calls from a tool) dropped from session cost | 0.99.0 | CA | Y | Nested call cost appears in total |
| `codemode` `image()` accepted bad base64, persisted an invalid block, and every later request returned 400 | Unreleased | codemode | Y | Validate image blocks before persisting |
| Concurrent `edit`/`write` to the same file interleaved; serialize per real path (`realpath`), keep different files parallel | 0.61.0, 0.63.0 | CA | Y | Two writers on a symlink and its target; sequential order preserved |

## 5. Bash and process control

| Edge case | Ver | Pkg | Go? | Suggested test |
|---|---|---|---|---|
| Use `bash`, not `/bin/sh`, on Unix; fall back to PATH lookup when `/bin/bash` is absent (Termux) | 0.31.0, 0.51.6 | CA | Y | Missing `/bin/bash` uses PATH bash |
| Spawn errors (bad cwd, bad shell path) crashed the agent | 0.37.2 | CA | Y | Nonexistent cwd returns tool error |
| Binary output (`curl` of a video) crashed | 0.14.2 | CA | Y | Emit NUL/invalid bytes; no panic |
| Incomplete ANSI stripping (string terminators, OSC 133 shell-integration marks corrupted the terminal after exit) | 0.11.2, 0.12.11 | CA | Y | Golden test of stripper vs OSC/DCS/APC/PM |
| Unicode format characters U+0600-0604 crashed width code | 0.31.0 | CA, TUI | Y | Bash output with those code points |
| UTF-8 split across chunks corrupted remote output; use a streaming decoder | 0.34.0, 0.42.4 | CA | G | Split a 4-byte rune across reads (Go `[]byte` is safe, `string(chunk)` per read is not) |
| Output must stream while the command runs | 0.73.0 | CA | Y | Command prints, sleeps; first line arrives early |
| Keep draining stdout/stderr after the child exits while descendants still write | 0.79.4 | CA | Y | `sh -c 'sleep 1 & echo late'`; late output kept |
| Windows: descendants that inherit stdio handles made bash hang forever | 0.61.0 | CA | Y | Detached grandchild holding the pipe; command returns |
| Kill the process tree, not the PID: negative-PID SIGKILL on Unix, `taskkill /T` on Windows; must not crash when `taskkill.exe` is absent | 0.84.4 | CA, AG | Y | Child spawns grandchild; abort kills both. Missing taskkill returns error only |
| Track detached children and kill them on exit signal (no orphans) | 0.67.4 | CA | Y | SIGTERM to host; no child survives |
| Signal-terminated local command was reported as success with partial output | 0.86.0 | CA | Y | `kill -9 $$`; exit status is failure with signal name |
| Timeout must be positive and bounded; non-positive or oversized values were clamped to "immediately" | 0.80.4 | CA, AG | Y | timeout 0, -1, 1e12 return validation errors |
| Truncation: 2000 lines / 50 KB defaults (`src/core/tools/truncate.ts:11-12`); full output must always go to a temp file, even when only the line limit trips | 0.65.1 | CA | Y | 5000 short lines; temp file has all lines |
| Truncation off-by-one: trailing newline counted as an extra hidden line; single very long line ending in newline | 0.50.0, 0.75.4, 0.75.5 | CA, AG | Y | Table test of line/byte edge counts |
| File descriptors leaked when output was truncated by line count | 0.70.3 | CA | Y | Loop 1,000 truncated runs; FD count flat |
| Windows cwd with backslashes broke the bash tool; normalize to forward slashes for Git Bash | 0.58.1 | CA | Y | `C:\x\y` cwd |
| Legacy WSL `bash.exe`: pass script over stdin so variables expand inside WSL | 0.79.9 | CA, AG | Y | `echo $HOME` under WSL shim |
| Helper console windows flashed on Windows for background spawns | 0.75.4 | CA, AG | Y | `CREATE_NO_WINDOW` |
| PowerShell: only detach on Unix (detached spawn broke output on Windows) | 0.71.0 | CA | Y | Windows CI run |
| User `!cmd` bash: cancel all concurrently running user commands; RPC bash must go through the same hook path | 0.83.0 | CA | Y | Two user commands, abort cancels both |
| Shell path resolution read ambient `process.cwd()` instead of session cwd | 0.68.0 | CA | Y | Per-session `shellPath` |
| Env for children must be explicit and not inherited by accident | 0.82.0 | AG | Y | Env allow-list test |
| Bun sandbox had empty `process.env`; fell back to `/proc/self/environ` | 0.70.3 | AI, CA | P | Skip |

## 6. Read, edit, write (files, encoding, images)

| Edge case | Ver | Pkg | Go? | Suggested test |
|---|---|---|---|---|
| CRLF files: LLM sends LF; normalize before matching, then restore original endings | 0.31.0 | CA | Y | CRLF file plus LF `oldText`; CRLF preserved after edit |
| UTF-8 BOM: strip before matching, re-add on write (`edit-diff.ts:533`) | 0.31.0 | CA | Y | BOM file edit round trip |
| Fuzzy match: NFKC plus smart quotes, dashes, spaces (`edit-diff.ts:34-49`); must not fail on CJK and full-width text | 0.58.2 | CA | Y | Curly quote in file, straight in `oldText` |
| Fuzzy match must rewrite only the matched block, not the whole file through normalized content | 0.79.9 | CA | Y | Fuzzy edit leaves untouched lines byte-identical |
| Edit on empty file crashed | 0.11.5 | CA | Y | Empty file, replace with content |
| Edit errors rendered twice; permission dialog raced with diff preview | 0.63.1, 0.71.0, 0.32.1 | CA | P | UI only |
| Edit/preview filesystem access errors mis-reported | 0.71.0 | CA | Y | EACCES vs ENOENT messages |
| macOS screenshot names: U+202F narrow no-break space before AM/PM, lowercase am/pm, curly quote U+2019, NFD filenames (`path-utils.ts:5-72`) | 0.21.0, 0.50.4, 0.67.3 | CA | Y | Try exact path, then NFC/NFD, then variants |
| `@`-prefixed paths must resolve after stripping `@` | 0.51.2 | CA | Y | `@src/a.go` |
| `@` references with spaces, quotes, Unicode | 0.10.5 | CA | Y | Quoted path token |
| Path traversal guard on read; later relaxed for ancestor dirs (monorepo configs) | 0.11.7, 0.10.6 | CA | Y | Decide policy once; test `../` and absolute paths |
| Git Bash, MSYS, Cygwin, WSL drive paths resolved against the wrong Windows drive | 0.84.0 | CA | Y | `/c/Users/x`, `/mnt/d/x` mapping |
| Absolute glob patterns lost their leading `/` | 0.11.8 | CA | Y | `/abs/**/*.go` |
| `find` with path-containing globs returned nothing (fd needs full-path mode) and mangled roots | 0.67.6, 0.84.0 | CA | Y | `src/**/*.spec.ts`; root `/` and `C:\` |
| `find` applied nested `.gitignore` across siblings; ignored nested repos wrongly | 0.67.6, 0.79.10 | CA | Y | Nested repo under an ignored parent |
| `find`/`grep` must be abort-aware and non-blocking on huge trees; `grep context=0` did sync per-match reads | 0.67.4 | CA | Y | Cancel mid-search returns quickly |
| Directories named like context files (`AGENTS.md/`) caused `EISDIR` | 0.82.1 | CA | Y | Dir named AGENTS.md is skipped |
| Context file discovery: both `AGENTS.md` and `AGENTS.MD`; nested worktrees load twice; Windows parent traversal hung | 0.71.0, 0.83.0, 0.80.4 | CA | Y | Case variants, linked worktree, drive root |
| Text file starting with `GIF` misdetected as image; magic must be `GIF87a`/`GIF89a` (`coding-agent/src/utils/mime.ts:13`) | 0.87.0 | CA, AG | Y | File "GIF is a format" is text |
| Detect image type by magic bytes, not extension | 0.23.2 | CA | Y | PNG named `.jpg` |
| BMP converted to PNG; WebP/JPEG/GIF converted to PNG for Kitty | 0.80.3, 0.32.3 | CA | Y | Per-format golden |
| EXIF orientation must be applied on resize; also skip non-EXIF APP1 segments | 0.58.0, 0.85.0 | CA | Y | Rotated phone JPEG; JPEG with XMP APP1 first |
| Provider image limit (Anthropic 5 MB): retry with progressive quality/size reduction; enforce on final base64 payload; text fallback when unsafe | 0.32.3, 0.62.0, 0.84.0 | CA | Y | Oversized image from extension tool also resized |
| Non-vision model: replace images with explicit text placeholders, never drop silently | 0.68.0 | AI | Y | Placeholder present |
| Tool result with empty text and no image: send `(no tool output)`, not "(see attached image)" | 0.80.4 | AI | Y | Empty result |
| Image-only user message: no empty text part | 0.87.1 | AI | Y | Payload has no `{"text":""}` |
| Write tool reported UTF-16 unit counts as bytes | 0.85.0 | AG | G | Go `len(s)` is bytes, but keep rune vs byte explicit |
| Windows CRLF write preview artifacts | 0.56.2 | CA | P | UI only |
| Session-wide writes to `auth.json`/`models-store.json` overrode admin-managed permissions and ACLs | 0.84.3 | CA | Y | Write in place or preserve mode; do not chmod |
| UTF-8 BOM in frontmatter/config files prevented loading | 0.84.3 | CA | Y | BOM config parses |
| `.gitignore`, `.ignore`, `.fdignore` respected when scanning skills and resources; skip `node_modules` | 0.52.0, 0.27.1 | CA | Y | Ignored dir not scanned |

## 7. Streaming and partial JSON

| Edge case | Ver | Pkg | Go? | Suggested test |
|---|---|---|---|---|
| Malformed trailing tool-call JSON in partial chunks must not fail the parse (`ai/src/utils/json-parse.ts:104` uses partial-json) | 0.52.10 | AI | Y | Truncated JSON at every byte offset |
| Anthropic tool JSON: own SSE parser with defensive JSON repair | 0.68.1 | AI | Y | Fuzz corpus |
| `partialJson` scratch buffer leaked into persisted tool calls and corrupted resumed conversations | 0.67.2 | AI | Y | Persisted message has no scratch field |
| Null chunk, or chunk with no `choices`, crashed | 0.62.0, 0.55.2 | AI | Y | `data: null`, `{}` |
| Deltas that interleave content and tool-call deltas in one choice | 0.73.1 | AI | Y | Mixed delta fixture |
| Gateways mutate tool-call IDs mid-stream; coalesce by stable tool index | 0.70.0 | AI | Y | ID changes between chunks |
| Mistral continuation chunks omit the tool-call ID | 0.84.4 | AI | Y | Fragmented tool call |
| Servers omit `output_index` (llama.cpp); unfinished tool calls must not run; end stream with error | 0.99.0 | AI | Y | Missing index |
| Function args only in `response.function_call_arguments.done` | 0.65.0 | AI | Y | Emit one delta |
| Tool call with both valid `function` and empty `custom` object lost its args | 0.83.0 | AI | Y | Malformed delta |
| Stream ends without a terminal event is an error, not success: OpenAI Responses (0.80.0, 0.81.0), Anthropic before `message_stop` (0.71.0), completions with no `finish_reason` (0.74.1) | multiple | AI | Y | Truncate each stream type; error and retry |
| SSE terminal event not followed by a blank line (Codex) | 0.85.0 | AI | Y | EOF without `\n\n` |
| Anthropic SSE: ignore unknown proxy events (`done`); drop text or thinking in the first content-block event | 0.70.3, 0.84.0 | AI | Y | Unknown event names |
| Abort response body reads after terminal event (avoid hung connections) | 0.78.0 | AI | Y | Server keeps socket open after `completed` |
| Late SSE events after abort must not update state | 0.39.0 | AI | Y | Cancel then feed events |
| Quadratic CPU when draining a buffered event stream | 0.86.0 | AI | Y | 100k events benchmark |
| Thinking signature serialization ran per delta instead of once | 0.84.4 | AI | Y | Signature appended once |
| Usage missing in stream or in `message_delta` from proxies | 0.80.7, 0.50.2, 0.84.3 | AI, CA | Y | Fall back to estimate; do not zero |
| Usage in `choice.usage` (Moonshot) or `cached_tokens` top level (Kimi) | 0.58.0, 0.84.3 | AI | Y | Fixtures |
| Long local-model SSE dropped at 5 minutes (undici `bodyTimeout`) | 0.70.3 | CA | G | Go `http.Client.Timeout` kills streams too; use idle timeout only |
| Streaming syntax highlight froze UI on large `write` args | 0.54.2 | CA | P | Use incremental render |
| Extension provider `streamProxy` dropped tool-call metadata/namespaces | 0.84.2 | AG | Y | Round-trip |

## 8. Retry, backoff, rate limits

| Edge case | Ver | Pkg | Go? | Suggested test |
|---|---|---|---|---|
| 429 must retry with backoff, not trigger compaction | 0.50.2 | AI | Y | 429 leads to retry with no compaction |
| Anthropic rate limit: exponential backoff (base 10 s, max 5) | 0.12.3 | CA | Y | Fake clock |
| Retry counter resets after each successful LLM response, not across tool turns | 0.50.2 | CA | Y | 3 failures across 3 turns |
| Retry cap: `retry.maxAgentDelayMs` (60 s) so long outages stay responsive; defaults 3 retries, 2 s base (`settings-manager.ts:48-50`) | 0.86.0 | CA | Y | Delay never exceeds cap |
| `Retry-After` unparseable date fired immediately; use backoff | Unreleased | AI | Y | Header `Retry-After: soon` |
| Honor `retry-after-ms` and `retry-after` before falling back | 0.74.1 | AI | Y | Both headers |
| SDK retries compete with app retries; disable SDK retry (`maxRetries:0`), and never retry quota/billing 429 | 0.18.2, 0.76.0 | AI | Y | Single retry layer only |
| Retry waits must honor abort and delay limits | 0.82.0 | AI | Y | Cancel during wait |
| `session.prompt()` returned before the retry cycle (including tool use) finished | 0.55.4, 0.61.0, 0.65.0 | CA | Y | Prompt call blocks until retries settle |
| Abort message shows correct attempt count; Escape works during "Working" after retry | 0.39.0 | CA | Y | State reset |
| Retryable error strings added over time: connection error, `upstream connect`, `reset before headers`, `terminated`, `Network connection lost.`, `request ended without sending any chunks`, `server_error`/`internal_error`, Bedrock `http2 request did not get a response`, socket-drop, `ResourceExhausted` (gRPC), Cloudflare 520/524, Azure peak-load, DNS `ENOTFOUND`/`EAI_AGAIN`, buffer-limit, z.ai `network_error`, "provider says retry" | 0.25.0 to 0.86.0 | AI, CA | Y | Table-driven classifier test with every string; make the list data, not code |
| Provider error messages returned as a "successful" assistant message must be treated as errors | 0.59.0, 0.60.0 | CA, AI | Y | Fixture |
| Prefix HTTP status onto error text so 5xx/429 classifiers match | 0.75.1 | AI | Y | Status in message |
| Bodyless 400/413 misclassified as overflow | 0.86.0 | AI | Y | Empty-body 400 not overflow |
| Bedrock throttling misidentified as overflow | 0.65.0 | AI | Y | NON_OVERFLOW list (`overflow.ts:76-80`) |
| Transient HTTP failures in version check, catalog, tool download, package manager were not retried | 0.84.0 | CA | Y | Flaky server |
| Hung catalog request consumed the entire refresh deadline without retry | 0.84.3 | CA | Y | Per-attempt timeout |
| Connect timeout too short on slow links; make configurable; idle timeout separate from connect | 0.84.0, 0.75.4, 0.78.1 | CA | Y | Three separate timeouts |
| Summarization (compaction, branch summary) calls did not retry | 0.81.1 | CA | Y | Same policy for summary calls |
| Split-turn compaction summaries ran concurrently and hit 429 on single-concurrency local servers | 0.80.4 | AG, CA | Y | Serialize |
| Retry lifecycle events for UI/RPC/SDK (`willRetry` on `agent_end`) | 0.75.4, 0.81.1 | CA | Y | Event contract |

## 9. Context overflow and compaction

| Edge case | Ver | Pkg | Go? | Suggested test |
|---|---|---|---|---|
| Overflow detection is per-vendor regex (`ai/src/utils/overflow.ts:37`): Anthropic 413 `request_too_large`, OpenAI "exceeds the context window", "maximum context length (N)", `context_length_exceeded`, Ollama "prompt too long; exceeded max context length", z.ai `model_context_window_exceeded` and CN "Prompt exceeds max length" and "Prompt too long", OpenRouter/Poolside "maximum allowed input length", LiteLLM wording, DS4 "Prompt has N tokens", Cerebras | 0.38.0 to Unreleased | AI | Y | Fixture per vendor; make patterns configurable data |
| Silent overflow: provider accepts request, returns usage above the window; check `usage.input > contextWindow` | 0.38.0 | AI | Y | Fixture |
| Overflow error from a different model, or already handled, must skip compaction | 0.38.0 | CA | Y | Model switched after error |
| Overflow compaction cascades in a loop | 0.56.0 | CA | Y | One overflow yields one compaction |
| Successful overflow-triggered compaction must not re-run the completed assistant response | 0.79.8 | CA | Y | Assert single retry |
| Compaction refuses sessions with no eligible messages (empty summaries) | 0.79.8 | CA | Y | Empty session |
| Repeated compactions dropped earlier-kept messages; re-summarize from the previous kept boundary and use the latest compaction entry, not the first | 0.63.1, 0.52.10 | CA | Y | Three compactions in a row |
| Stale pre-compaction assistant usage re-triggered compaction on the next prompt; also skewed footer % and output budget | 0.56.3, 0.52.10, 0.80.6 | CA, AI | Y | After compaction, usage unknown until next response |
| Estimate context from last successful response when the API keeps failing (529) so compaction can still fire | 0.56.3 | CA | Y | Persistent 529 |
| Skip aborted and error messages in usage estimation; malformed all-zero usage after truncation ignored | 0.17.0, 0.80.0 | CA | Y | Zero-usage message |
| Threshold compaction skipped when provider omits streaming usage (llama.cpp reported zero) | 0.84.3, 0.83.0 | CA | Y | Fall back to token estimate |
| Compaction check before submitting the user prompt (catches abort mid-response near the limit) | 0.23.3 | CA | Y | Near-limit abort |
| Large tool result crossing the threshold reached the provider before compaction; compact between tool execution and next response | 0.84.4 | CA | Y | 200k-token tool result |
| Mid-run threshold compaction skipped oversized trailing tool results | 0.86.0 | CA | Y | Trailing huge tool result |
| Summarization input must be bounded: truncate tool results to 2k chars | 0.56.3 | CA | Y | Huge tool output |
| Compaction with branched sessions included abandoned-branch entries and overflowed | 0.31.0 | CA | Y | Compact after branch switch |
| Truncated summaries must not be persisted when output limit hit; summary output cap 2048 too small when reasoning consumes it; must throw on LLM error, not return empty | 0.84.3, 0.85.0, 0.31.0 | CA | Y | `stop_reason=length` during summary |
| Compaction summary requests: no tools exposed; no forced `toolChoice:none` | 0.84.3, 0.84.4 | CA | Y | Request payload check |
| Compaction: use session thinking level (not forced high); no reasoning for non-reasoning models; fresh routing session ID and cache off | 0.68.0, 0.56.0, 0.82.0 | CA | Y | Payload check |
| Summarizer refusal by some models; separate conversation and instructions clearly | 0.87.1 | CA | Y | Prompt shape |
| Custom-message tokens must count in retained-token budget | 0.80.4 | CA | Y | Custom message in budget |
| User image attachments counted the same as tool-result images in estimates | 0.76.0 | AG, CA | Y | Image token count |
| Truncated response below intended output limit: compact and retry once instead of ending run; bounded recovery | 0.84.0, 0.84.3 | CA, AI | Y | One recovery only |
| Manual compaction racing with threshold compaction; input blocked during auto-compaction; queued messages delivered afterward | 0.84.0, 0.17.0, 0.37.0 | CA | Y | Concurrent triggers use one mutex |
| Compaction failure must not crash (quota exceeded); expose reason via event | 0.48.0, 0.84.3 | CA | Y | Summarizer returns 429 |
| Post-compaction handoff used raw pre-compaction messages | 0.71.0 | CA | Y | Handoff uses compacted context |
| Session tree navigation racing active compaction; `session_before_tree` cancel left compaction state stuck | 0.86.0, 0.70.3 | CA | Y | Cancel hook |
| Context window metadata wrong per model (272k vs 1M vs 400k, Copilot extended, OpenRouter top-provider) causing 400s or early compaction | many | AI | Y | Load from data file, allow user override |

## 10. Session file and branching

| Edge case | Ver | Pkg | Go? | Suggested test |
|---|---|---|---|---|
| Fork/torn-tail repair must publish atomically (temp file plus rename) | 0.84.0 | AG | Y | Kill during fork; file valid |
| Resumed JSONL lacking trailing newline corrupted next append | 0.84.4 | CA | Y | Missing `\n` at EOF |
| Empty or invalid session file must not be overwritten or corrupt state; non-empty invalid rejected | 0.50.0, 0.80.3 | CA | Y | Garbage file untouched |
| `findMostRecentSession` validates header, ignores non-session JSONL | 0.31.0 | CA | Y | Stray `.jsonl` |
| Session file created only when first user message sent (lost sessions on early exit vs empty files) | 0.99.0 | CA | Y | Exit before first response |
| Duplicate session headers when forking before any assistant message | 0.55.2 | CA | Y | Fork at first user entry |
| Fork wrote into parent file; forked session must persist user message; parent chain must survive labels; fork keeps compaction boundary; in-memory fork before turn settles | 0.51.6, 0.79.2, 0.85.0 | CA | Y | Fork matrix |
| Clone/fork before first assistant response needs clear message | 0.80.9 | CA | Y | UX text |
| Fork menu ignored duplicate selection | 0.80.4 | CA | P | UI |
| Branch summary `fromId` recorded destination instead of source leaf | 0.84.3 | CA | Y | Tree navigation entry |
| Deep branches took quadratic time to build context | 0.79.9 | CA | Y | 10k-entry chain |
| Huge JSONL files: stream line by line; do not read twice at open; cap in-flight loads on `--resume` (OOM) | 0.78.1, 0.81.0, 0.74.1 | CA | Y | 1 GB session opens with bounded memory |
| Session listing: header-only reads for ID lookup; mtime order; stop at first match | 0.86.0 | CA | Y | Perf test |
| Session "modified" = last message time, not file mtime (rename reordered list) | 0.50.0 | CA | Y | Rename does not reorder |
| Session ID uniqueness scoped per working directory, not global; short entry IDs used timestamp prefix (near-constant) so take random tail; UUIDv7 for time locality | 0.84.0, 0.80.4, 0.67.1 | AG, CA | Y | 10k IDs no collision |
| Imported session overwrote an existing file with the same name; concurrent shares overwrote each other | 0.85.0 | CA | Y | Unique names |
| Import: quoted paths with spaces; missing file is non-fatal | 0.67.67 | CA | Y | Path fixtures |
| Stored session cwd no longer exists on resume/import: prompt or fall back | 0.65.1 | CA | Y | Deleted cwd |
| Symlinked session dirs: dedupe by canonical path, discover through symlinks | 0.70.3, 0.84.0, 0.68.0 | CA | Y | Symlink loop |
| Custom session dir: current-folder lookups stay scoped to cwd | 0.77.0 | CA | Y | Two cwds one dir |
| `--session <id>` search globally if not found locally; warn when creating new ID; ephemeral runs can still take a deterministic ID | 0.48.0, 0.80.4, 0.80.3 | CA | Y | ID lookup |
| Session replace/tree navigation mid-response must abort and persist the outgoing turn (dangling tool calls) | 0.83.0 | CA | Y | Switch during tool call |
| `toolResult` persisted before its assistant message because event handling was not serialized | 0.55.4 | CA | Y | Ordering invariant test |
| `switchSession` appended spurious `thinking_level_change` on resume; initial model and thinking not saved so resume reset to off | 0.50.8, 0.31.0 | CA | Y | Resume is idempotent |
| Session name: normalize newlines; strip control chars in display; empty title clears | 0.80.0, 0.56.0, 0.59.0 | CA | Y | `\n`, ESC in name |
| `null` message content from imported transcripts normalized at ingestion | 0.80.4 | CA, AG, AI | Y | Null content fixture |
| Cost/tokens in footer must include pre-compaction messages | 0.31.0 | CA | Y | Totals across compaction |
| SQLite backend: filters and limits in SQL, bounded log reads, listings must not take writer claims | 0.84.0, 0.84.1 | sqlite-node | Y | Concurrent list while writer active |
| Durable documents: reading with newer definition version must migrate and persist | 0.99.0 | durable | Y | Version bump test |

## 11. Token and cost accounting

| Edge case | Ver | Pkg | Go? | Suggested test |
|---|---|---|---|---|
| Anthropic 1-hour cache writes priced at 2x input, not the 5-minute rate; also via Vercel gateway and Bedrock | 0.79.4, 0.86.0, 0.99.0 | AI | Y | Price table with TTL |
| Response model differs from requested (fallback model, relay): price by returned model | 0.84.3, 0.86.0 | AI | Y | Fallback model |
| OpenAI fast/priority tier: trust requested tier when API echoes default; `service_tier:"fast"` price; GPT-5.5 2.5x | 0.67.67, 0.70.0, 0.99.0 | AI | Y | Tier fixtures |
| Long-context pricing tier above 272k; default window kept short to avoid it | 0.80.6, 0.81.0 | AI | Y | Threshold |
| Reasoning tokens double-counted (already in `completion_tokens`) | 0.70.0 | AI | Y | Fixture |
| Google/Vertex cached tokens double-counted in billable input | 0.63.0 | AI | Y | Subtract cached |
| OpenRouter `cached_tokens` semantics; cache-write tokens preserved | 0.65.1, 0.74.1 | AI | Y | Fixtures |
| DeepSeek `prompt_cache_hit_tokens` | 0.71.0 | AI | Y | Fixture |
| Retried requests must accumulate usage from all attempts | 0.12.3 | CA | Y | Retry adds usage |
| NaN token counts crashed footer | 0.11.5 | CA | G | Zero-value guard |
| Subscription vs API pricing: zero-cost subscription models show API-equivalent cost | 0.81.0 | AI | Y | Flag |
| System prompt date broke prompt cache: use ISO date only (no time, no locale format) | 0.58.0, 0.67.67, 0.80.7 | CA | Y | Prompt bytes stable across days if date removed |
| Idle cache warming rebuilt expired caches when timers delayed | 0.87.0 | CA | Y | Delayed timer |
| Cache keys: clamp session-derived keys to 64 chars; `cacheRetention:"none"` must disable implicit writes; not every server supports long retention | 0.75.4, 0.82.0, 0.70.0 | AI | Y | Length clamp; per-model compat flag |
| Usage from tools that call models (nested) added to session cost | 0.99.0 | CA | Y | See section 4 |

## 12. Abort, cancel, concurrency

| Edge case | Ver | Pkg | Go? | Suggested test |
|---|---|---|---|---|
| Abort signal must propagate through auth resolve, OAuth refresh, lock waits, catalog refresh, retry sleeps, and HTTP body reads | 0.82.0, 0.84.0 | AI, CA | G | `context.Context` everywhere; leak test with `goleak` |
| Cancelled lock waiters must not run later | 0.84.0 | CA | G | Cancel waiter then release |
| Session disposal aborts in-flight agent, compaction, branch summary, retry, and bash | 0.77.0 | CA | Y | Dispose during each |
| `Agent.reset()` during active run rejected until idle | 0.84.1 | AG | Y | Reset while running |
| `prompt()` while streaming must error; use steer/follow-up queue | 0.32.0 | CA | Y | Concurrent prompts |
| Slash/hook commands during streaming crashed | 0.32.2 | CA | Y | Command mid-stream |
| Queued steering/follow-up survive threshold compaction and manual `/compact` | 0.52.7, 0.81.0, 0.84.0 | CA | Y | Queue then compact |
| Queued messages with images dropped when `streamingBehavior` set | 0.52.0 | CA | Y | Image in queue |
| Follow-ups queued by `agent_end` handlers drain before idle | 0.77.0 | CA | Y | Handler queue |
| `continue()` after assistant-ending context resumes queued messages one at a time | 0.52.7 | AG | Y | Ordering |
| RPC `abort` must cancel manual compaction | 0.85.0 | CA | Y | Abort compaction |
| Cancellation races left stale retry state or auto-compaction started after cancel | 0.86.0 | CA | Y | Race test with `-race` |
| Newer catalog refresh generation must not be overwritten by an older stalled one | 0.84.0 | AI, CA | Y | Generation counter |
| In-memory credential read-modify-write must be serialized; auth file lock contention caused false "No API key" | 0.84.0, 0.56.3 | AI, CA | Y | Parallel writers |
| Stale credentials: long-running session must see `auth.json` changes from other processes without lock convoy | 0.84.0 | CA | Y | Two processes |
| Extension `ctx.abort()` in preflight restores queued input | 0.75.4 | CA | Y | Abort in hook |
| Retry/compaction/event settlement uses awaited lifecycle, not a separate event queue | 0.75.4 | CA | Y | Single event bus with ordering |
| Late `session_start` UI operations before TUI ready | 0.58.1 | CA | Y | Init order |

## 13. Provider quirks by vendor

Pi's own code shows the pattern: a per-model **compat** record (`supportsDeveloperRole`, `supportsLongCacheRetention`, `sendSessionIdHeader`, `supportsEagerToolInputStreaming`, `allowEmptySignature`, `thinkingFormat`, `requiresThinkingAsText`, `cacheControlFormat`, `supportsFinishReason`, `forceAdaptiveThinking`) replaced URL and name heuristics (0.48.0, 0.80.2, 0.70.0). Ask should ship this as data from day one.

| Vendor | Quirk | Ver |
|---|---|---|
| Anthropic | Signed thinking blocks must be replayed verbatim; empty-text-with-signature blocks must be kept; `redacted_thinking` must be kept | 0.55.2, 0.80.6, 0.84.0 |
| Anthropic | No `temperature` with extended thinking; suppress deprecated temperature on Opus 4.7+; `thinking:{type:"disabled"}` unsupported on some models; deprecated interleaved beta header on adaptive models | 0.55.2, 0.78.1, 0.79.2 |
| Anthropic | Adaptive thinking and `xhigh` mapping per model (Opus 4.6/4.7, Sonnet 4.6/5); `max_tokens` must exceed thinking budget | 0.49.0, 0.52.5, 0.55.1, 0.67.5 |
| Anthropic | `cache_control` on last tool definition and on string-format user content; `sensitive` and refusal stop reasons with `stop_details`; 5 MB image cap; OAuth: no `scope` on refresh, localhost callback for pasted code, outdated Claude Code version string, `(external, cli)` UA gave 401, `ANTHROPIC_AUTH_TOKEN` env leak into unrelated compatible APIs | multiple |
| Anthropic proxies | Omit `usage` in `message_delta`; unsigned thinking after aborted streams leaked as text (`</thinking>` mimicry); relay reports different model | 0.27.7, 0.50.2, 0.80.7, 0.86.0 |
| OpenAI Responses | 400 "reasoning without following item" (skip errored/aborted assistant messages; omit `id` when switching models); foreign tool-call IDs hashed to `fc_<hash>`; pipe-separated IDs; oversized IDs; `store:false`; min `max_output_tokens`; only `max_output_tokens` is a length stop; out-of-order output items; namespaces | 0.49.2, 0.49.3, 0.50.2, 0.60.0, 0.62.0, 0.80.3, 0.84.0, 0.84.2, 0.52.7 |
| OpenAI Codex | WebSocket: 60-minute connection limit (rotate), reconnect once on limit, `previous_response_not_found` retry without continuation, cache per session but not across accounts, fall back to SSE; SSE header wait bounded; session ID max 64 chars; UUIDv7 request ids; non-empty system prompt; header `session-id` not `session_id`; window 272k not 400k | 0.38.0, 0.58.1, 0.73.0, 0.73.1, 0.76.0, 0.80.0, 0.80.4, 0.80.8, 0.81.0, 0.82.0, 0.84.0 |
| OpenAI-compatible completions | `developer` role rejected (DeepSeek, OpenCode, OpenRouter Kimi); strict tool schemas only when advertised; `toolChoice` wrapper shape; `max_tokens` vs `max_completion_tokens`; assistant content must be a plain string for some servers; `reasoning_content` replay (DeepSeek V4, Kimi, Xiaomi, Qwen chat-template `preserve_thinking`); `reasoning_details` order; empty `reasoning_content` needed on assistant messages | many |
| Google/Gemini/Vertex | Signed thought parts and tool-call IDs needed for replay; unsigned tool calls: sentinel for Gemini API but not Vertex; `isThinkingPart` means `thought===true`; thinking level vs budget by model family (2.5 Flash Lite 512 min); FinishReason new values; tool-result shape `{output}`/`{error}`; JSON schema meta keys (`$schema`, `$defs`) rejected in Cloud Code Assist; ADC marker strings must not be sent as API keys; 403/404 endpoint cascade (Antigravity) | 0.23.4, 0.43.0, 0.56.2, 0.61.0, 0.67.3, 0.68.0, 0.71.0, 0.84.0 |
| Bedrock | Tool-call IDs alphanumeric only; empty object keys in args; blank text rejected; unknown content blocks skipped; prompt cache only for Claude; inference-profile ARN needs model-name normalization and region from ARN; `AWS_PROFILE` region; explicit profile beats ambient keys; bearer token path; default max tokens 4096 truncation; HTTP/2 transport errors retryable; redacted reasoning from non-Anthropic models | many |
| Mistral | 400 after aborted (empty) assistant message; `reasoning_effort` vs `prompt_mode` per model; "at most one leading ThinkChunk"; continuation chunk ID missing | 0.18.1, 0.67.67, 0.86.0, 0.99.0 |
| Copilot | Some models require Responses adapter; empty `reasoning` field rejected; content array vs string re-answered all prompts; device-code poll: wait before first poll, honor `slow_down`, clock-drift hint; model policy enablement rate-limited login; Business/Enterprise endpoint from credential | 0.23.2, 0.58.0, 0.63.0, 0.80.4, 0.84.0, 0.84.3 |
| z.ai / Kimi / Moonshot / Qwen / Fireworks / Cerebras / Groq / Xiaomi / DeepSeek / xAI | Thinking toggles use vendor-specific parameter names (`enable_thinking`, `thinking:{type}`, `reasoning_effort`, `thinking:"none"`); `max_tokens` field name; `prompt_cache_retention` rejected; strict schemas 400 when mixed; UA required; empty thinking signatures; hostname case in detection | many |
| Local servers (llama.cpp, LM Studio, vLLM, Ollama) | Missing `output_index`; zero usage; strict boolean only; default output cap reserves entire window (impossible request); context window should be actual loaded ctx; Ollama silent truncation vs explicit error | 0.42.2, 0.75.0, 0.75.4, 0.82.0, 0.83.0, 0.99.0 |

Cross-provider handoff rules (each fixed at least once, all Y for Go):
- Tool-call IDs must be re-normalized per target provider and remain unique when many calls share one provider call ID (0.49.0, 0.50.2, 0.81.0).
- Thinking blocks converted to plain text (without tags) when the target model cannot replay them (0.39.0, 0.25.0).
- Errored or aborted assistant messages are dropped, and their tool calls are excluded from pending results (0.48.0, 0.49.0, 0.49.2).
- Missing trailing tool results are synthesized when history ends in unresolved tool calls (0.69.0).
- Model switch through a non-reasoning model must preserve the saved thinking level, and clamp on capability (0.25.0, 0.56.3).

## 14. Auth and OAuth

| Edge case | Ver | Pkg | Go? | Suggested test |
|---|---|---|---|---|
| `auth.json` shared by parallel processes: lock, read-merge-write, preserve external edits, handle compromised lock files, surface load/persist errors | 0.53.0, 0.52.7, 0.56.3 | CA | Y | Multi-process write test |
| `/login` claimed success when `auth.json` locked | 0.80.4 | CA | Y | Read-only file |
| OAuth refresh failure at startup must not crash; must not log out when multiple instances run; refresh window 5 min before expiry; stalled refresh must release lock | 0.37.4, 0.37.0, 0.83.0, 0.84.0 | CA, AI | Y | Fake IdP timeouts |
| Callback server: port in use falls back to paste; provider redirect with error must fail fast; exchange code before showing success page | 0.99.0 | AI | Y | Bind conflict; `error=access_denied` |
| OAuth needs HTTP proxy env; OAuth failures must not write to stderr while TUI active | 0.51.0, 0.73.1 | AI | Y | Proxy test |
| Device-code polling: `slow_down` interval, first-poll wait, no auto-open browser in headless | 0.58.0, 0.80.4, 0.79.5 | AI | Y | RFC 8628 fixtures |
| API key resolution order: OAuth over settings key; `ANTHROPIC_OAUTH_TOKEN` over `ANTHROPIC_API_KEY`; do not delete env var after first OAuth use | 0.27.8, 0.31.0, 0.18.1 | AI | Y | Precedence table |
| Config values: plain strings are literals, `$ENV`/`${ENV}` interpolation, `!cmd` shell resolution, resolved at request time not cached | 0.49.1, 0.63.0, 0.77.0, 0.79.4 | CA | Y | Literal `ABC` stays literal |
| Ambient GitHub tokens (`GH_TOKEN`) must not enable Copilot | 0.74.1 | AI | Y | Env fixture |
| Startup without API keys allowed; skip unauthenticated saved default model | 0.27.3, 0.80.4 | CA | Y | Empty auth |
| Auth uses model headers and request-scoped `env`/`apiKey` | 0.80.2, 0.80.8 | AI | Y | Cloudflare per-request base URL |
| `NO_PROXY` for root domain and subdomains; plain-HTTP proxied requests need CONNECT; `HTTP(S)_PROXY` for Bedrock/Codex WebSocket | 0.74.1, 0.85.0 | AI, CA | G | Go's `httpproxy` handles most; test WebSocket dialer |

## 15. Network, timeouts, proxies

| Edge case | Ver | Pkg | Go? | Suggested test |
|---|---|---|---|---|
| Global body/header timeout killed long local-LLM streams at 5 min | 0.70.3, 0.74.1 | CA | G | Idle timeout, not total timeout |
| HTTP idle timeout setting ignored for non-Codex providers; WebSocket idle and connect timeouts | 0.75.4, 0.76.0, 0.78.1 | CA, AI | Y | Three timeouts |
| Provider request options: `timeout`/`maxRetries` undefined must be omitted | 0.70.2 | AI | P | Skip |
| HTTP/2 destroyed-session races; compressed response JSON failures; Node 26 fetch | 0.75.3, 0.75.1 | CA | P | Skip; Go equivalent: test HTTP/2 GOAWAY mid-stream |
| Offline mode: startup must not hang; bound network waits for managed tools | 0.55.1, 0.87.0 | CA | Y | `PI_OFFLINE`-style flag test |
| Update/version check must not block startup or send session data; separate registry support | 0.12.13, 0.67.1 | CA | Y | Check runs async |

## 16. TUI rendering: width, Unicode, ANSI

| Edge case | Ver | Pkg | Go? | Suggested test |
|---|---|---|---|---|
| Line wider than terminal crashes; footer, branch selector, settings list, scroll indicator, padded text, image fallback paths all overflowed at narrow widths | 0.11.11, 0.24.3, 0.50.2, 0.51.6, 0.58.0, 0.82.0, 0.83.0, 0.84.3 | TUI | Y | Property test: no rendered line exceeds width for widths 1 to 200 |
| Wide chars (CJK, emoji, full-width) at wrap boundary; word wrap for mixed Latin/CJK; overlay compositing that splits a wide cell | 0.12.15, 0.58.0, 0.79.1, 0.79.2, 0.79.4 | TUI | Y | Use a grapheme width library (uniseg + runewidth); golden tests |
| Grapheme clusters: emoji ZWJ, regional-indicator flags mid-stream (partial flag `🇨`), Indic conjuncts, Thai Sara Am, Lao AM, emoji in input cursor | 0.24.1, 0.51.1, 0.51.4, 0.56.0, 0.71.0, 0.84.0 | TUI | Y | Table of tricky strings |
| `visibleWidth` must ignore OSC (133 marks), OSC 8 hyperlinks, and CSI; tabs normalized to spaces | 0.56.0, 0.78.1, 0.80.8, 0.58.0 | TUI | Y | Strings with escapes |
| Truncation must always close SGR and OSC 8; never cut mid-escape | 0.62.0, 0.84.0 | TUI | Y | Truncate at every offset |
| Style must survive wrapping across lines; CRLF and lone CR in wrap; underline bleeding into padding; blockquote/heading/table colour leaks after inline code or links | 0.12.12, 0.56.1, 0.62.0, 0.63.0, 0.65.0, 0.81.0, 0.84.3 | TUI | Y | Wrap golden with styles |
| Stack overflow from spread `push(...lines)` and long wrap lines | 0.67.0, 0.78.0 | TUI | P | Go has no arg-spread limit; keep an iteration test on 1M-line output |
| Markdown: streaming partial closing fence made code blocks shrink or flicker; task lists; strikethrough only `~~x~~`; loose lists; blockquote with nested lists; trailing blank lines; table wrap and dividers; list marker preservation; huge files | 0.21.0 to 0.99.0 | TUI | Y | Render at every prefix of a document |
| Extremely large image-heavy output exceeded the JS string limit | 0.84.4 | TUI | P | Skip |
| Regex/`marked`-based highlighting mislabelled prose as code (auto-detect) | 0.62.0 | CA | Y | Do not auto-detect language |
| LaTeX rendering (many fixes 0.84 to 0.86) | 0.84.x | TUI | Y | Only if Ask renders math (probably out of scope) |

## 17. TUI rendering: redraw, resize, flicker, overlays

| Edge case | Ver | Pkg | Go? | Suggested test |
|---|---|---|---|---|
| Differential rendering: only changed lines; full redraw on width change but not height change; stale content when content shrinks; empty rows below footer; append past viewport must commit to scrollback | 0.42.5, 0.50.0, 0.50.6, 0.51.1, 0.56.2, 0.63.0, 0.79.0 | TUI | Y | Virtual terminal model plus frame diff test |
| Render throttle (16 ms) coalescing; tests need `waitForRender`; input must preempt throttle on Windows | 0.65.2, 0.67.0, 0.84.0 | TUI | Y | Deterministic clock in tests |
| Viewport tracking with overlays and shrink; overlay padding inflated scrollback on widen; overlay stays centered on resize; focus order and non-capturing overlay focus restore | 0.49.3, 0.50.0, 0.57.0, 0.65.0, 0.78.1 | TUI | Y | Overlay lifecycle test |
| Suspend/resume: resize while suspended; SIGINT while suspended; keep process alive until SIGCONT; `ctrl+z` on Windows must not crash | 0.42.5, 0.55.2, 0.61.1, 0.67.4 | TUI, CA | Y | Signal integration test (Unix) |
| Cursor hidden after exit if render pending, or after overlay closed during shutdown; software cursor not cleared; SIGTERM/SIGHUP shutdown must restore terminal before re-raising | 0.50.6, 0.79.4, 0.81.0, 0.99.0 | TUI, CA | Y | `defer` restore plus signal test; verify escape sequence output |
| Uncaught exception must restore terminal before exit; interactive session must exit if stdin is lost | 0.74.1, 0.73.0 | CA | Y | Panic recover restores tty |
| `stop()` drains stdin up to 1 s so Kitty release events do not leak to parent shell over SSH; Ctrl+D closed parent SSH | 0.51.1, 0.51.2 | TUI | Y | Termios test with pty |
| Terminal size from `COLUMNS`/`LINES` before 80x24 default; restricted seccomp rejects `SIGWINCH` self-signal | 0.71.0, 0.85.0 | TUI | Y | Env fixture |
| Termux soft keyboard height change forced full redraw and replay of history | 0.61.1 | TUI | Y | Height-only resize does not clear |
| Scrollback stale after session switch: clear screen before wiping scrollback | 0.58.2 | TUI | Y | Sequence order |
| Working indicator/spinner shrinking TUI with clear-on-shrink; loader timers must stop on dispose | 0.80.3, 0.67.2, 0.75.4 | TUI | Y | Lifecycle test |
| Keep input responsive: incremental highlight, cached footer totals, syncless FS ops during streaming, image resize off UI thread, quadratic search | 0.54.2, 0.75.5, 0.99.0 | TUI, CA | Y | Benchmark with 10k-message session |
| Fullscreen mouse: wheel step, SGR release codes, drag selection, hover recentering, phantom selection on focus change, reduced event volume in tmux/Zellij/Screen | 0.84.x, 0.85.x | TUI | Y | Only if Ask ships alternate-screen mouse UI |

## 18. Input, editor, paste, keybindings

| Edge case | Ver | Pkg | Go? | Suggested test |
|---|---|---|---|---|
| Paste: tabs converted to spaces (crash); paste must be atomic (not per-char, avoids autocomplete storms); literal content preserved; bracketed-paste with CSI-u inside must not become escape text; large paste markers (`[paste #1 +24 lines]`) must be expanded when submitted, queued, exported, or sent to an external editor; marker deletion/undo must keep registry consistent | 0.11.2, 0.34.0, 0.50.0, 0.56.0, 0.58.1, 0.70.1, 0.81.0 | TUI, CA | Y | Paste fixtures; submit with deleted/undone marker |
| Pasted text containing Kitty release-like patterns (MAC addresses `:3F`) was filtered | 0.42.5 | TUI | Y | Paste `aa:bb:3F` |
| Finder-copied files pasted as icon image instead of paths; image paste on WSL (BMP to PNG), Wayland (`wl-paste`), musl, sandboxed macOS pasteboard denial | 0.37.3, 0.38.0, 0.50.8, 0.71.0, 0.74.1, 0.99.0 | CA | Y | Backend probe order and error handling |
| Prompt history: draft restored when leaving history; cursor at start when browsing up, end when down; non-empty draft first goes to line start | 0.79.0, 0.79.1, 0.79.5 | TUI | Y | State machine test |
| Word navigation/deletion with Unicode boundaries; readline Ctrl+W skips trailing whitespace; wide-char-aware Input scroll | 0.29.0, 0.58.0, 0.76.0 | TUI | Y | Word boundary tests |
| IME: hardware cursor position (autocomplete open, extension editor, search inputs); hardware cursor off by default | 0.48.0, 0.49.1, 0.56.0, 0.79.1 | TUI | Y | Cursor position golden |
| Editor: sticky column across paste markers; cursor reserve column in wrapping; backslash input buffering delayed echo; vertical cursor in wrapped lines | 0.11.0, 0.50.0, 0.50.2, 0.67.0 | TUI | Y | Cursor motion tests |
| Large prompt via `setEditorText` corrupted display; scroll with indicators | 0.47.0 | TUI | Y | 10k-line prompt |
| Autocomplete: async providers, debounce, cancel in-flight `fd`; quoted paths with spaces; keep `./` prefix; `@` with hidden files and symlinks; CJK punctuation boundaries; wrappers `(`,`[`; ranking (exact first, shallow first, segment matches); Tab after command name must not chain; results re-query on cursor move; Windows drive letters | 0.50.4, 0.52.10, 0.57.1, 0.58.1, 0.63.0, 0.63.0, 0.79.0, 0.84.4, 0.86.0, 0.99.0 | TUI | Y | Table tests; cancel test |
| Queued steering/follow-ups must not wipe unsent editor input; tree summarization must not overwrite typed content | 0.37.4, 0.51.0 | CA | Y | Typed text preserved |
| Early input typed before the prompt loop starts must be buffered | 0.78.0 | CA | Y | Pre-read stdin |
| Slash commands: shadowing (`/quit` by fuzzy skill match); extension name conflicts get numeric suffix; `--` end-of-options; prompt starting with `-` or with YAML frontmatter parsed as flag | 0.52.6, 0.62.0, 0.73.1, 0.84.3 | CA | Y | Arg parser tests |
| Prompt templates: `$1` before `$@` to avoid recursive substitution; defaults `${@:-x}`; multiline unquoted args | 0.32.2, 0.74.1, 0.81.0 | CA | Y | Substitution table |
| Keybindings: user overrides must shadow defaults globally without evicting unrelated defaults; reload from disk; extension shortcut conflicts detected at startup | 0.49.1, 0.60.0, 0.61.0, 0.61.1, 0.70.0 | TUI, CA | Y | Registry unit test |
| Ctrl+C mashing must always exit selector; Ctrl+I collides with Tab | 0.11.9, 0.32.0 | TUI | Y | Key alias table |

## 19. Terminal specifics

| Terminal / OS | Edge case | Ver |
|---|---|---|
| Kitty keyboard protocol | Negotiation is response-driven, must ignore mismatched or delayed replies (false detection); Caps Lock/Num Lock bits in modifiers; non-QWERTY base-layout fallback; non-Latin layouts (Russian) still trigger Ctrl shortcuts; duplicate chars with CSI-u plus raw (Italian layout); keypad keys map to logical keys; modifier-only events insert nothing; key release events; `Input` needs CSI-u printable decoding (VS Code 1.110+) | 0.24.1, 0.24.2, 0.46.0, 0.50.8, 0.56.0, 0.56.2, 0.64.0, 0.70.3, 0.77.0, 0.79.0 |
| xterm modifyOtherKeys / tmux | Shift+Tab, Ctrl+Alt letters, Backspace/Escape/Space, uppercase letters; tmux 3.5 `extended-keys-format csi-u` needed, fallback for 3.2-3.4; OSC 8 forced off under tmux/screen; Ghostty inline images in tmux via env var; warning hidden when tmux server unreachable | 0.57.1, 0.58.0, 0.60.0, 0.67.2, 0.67.6, 0.68.0, 0.79.0 |
| Windows Terminal / Windows console | `0x08` is Ctrl+Backspace; Shift+Enter needs native helper; truecolor detect without `WT_SESSION`; VT input mode init; `alt+v` for image paste; Windows keybinding defaults avoid reserved shortcuts; right-click paste duplicates in VS Code terminals; Ctrl+Z; external editor handoff to vim; `EDITOR="code --wait"` shell commands | 0.55.1, 0.55.3, 0.58.1, 0.75.2, 0.84.0, 0.84.3 |
| macOS Terminal.app / iTerm2 | 256-colour fallback; Shift+Enter sends plain Return; Ctrl+C needed several presses (cell-size reply parser held back bytes); iTerm2 image size metadata; keypad keys | 0.24.5, 0.49.3, 0.64.0, 0.76.0, 0.84.0 |
| Ghostty / WezTerm / Zellij / Zed / cmux / JetBrains / GNU screen / Alacritty | WezTerm Kitty image clears and IME cursor; Zellij Shift+Enter regression; Zed image capability; cmux disables inline images; JetBrains truecolor but no OSC 8; Screen 256-colour; Ghostty needs OSC 9;4 progress keepalive | 0.56.0, 0.67.5, 0.70.0, 0.73.1, 0.76.0, 0.85.0 |
| Truecolor | Assume truecolor except `dumb`, empty, `linux`; `TERM=*-direct`; RGB to 256 grayscale mapping; light/dark detection order (background report, then scheme report, then `COLORFGBG`); batched colour-scheme replies parsed as one | 0.22.3, 0.37.0, 0.84.0, 0.99.0 |
| Images | Kitty protocol requires PNG; image IDs allocated by terminal, bounded; images must stay in TUI-owned regions; tall images capped by height; retransmit avoidance; spacer accumulation on expand; iTerm2 payload metadata | 0.25.2, 0.32.3, 0.50.0, 0.73.1, 0.74.1, 0.84.0 |
| Termux / Android | `/bin/bash` absent; `fd` package name; soft keyboard resize; clipboard via `termux-clipboard-get` | 0.51.6, 0.52.10, 0.61.1 |
| SSH | Batched input dropped key presses; split `Alt+Enter` read as Escape (Escape timeout env var); Kitty release leak on exit; Ctrl+D closes parent | 0.38.0, 0.51.1, 0.51.2, 0.84.2 |
| Clipboard | OSC 52 unbounded writes broke terminals; success reported when terminal ignored OSC 52; Wayland `wl-copy` failure fallback; containers/WSL without display; sandbox denial must not abort | 0.70.1, 0.73.1, 0.82.0, 0.86.1 |

Suggested Go test approach for section 19: keep a table-driven `parseKey(bytes) -> key` test with captured byte sequences per terminal (legacy, CSI-u, modifyOtherKeys), plus a pty-based smoke test for shutdown restoration. Detection tests take `(env, TERM)` maps as input.

## 20. Extensions, hooks, RPC, SDK

Applies to Go only if Ask exposes hooks and a JSONL RPC. Ask's `pkg/protocol` WS contract and gateway make the RPC lessons relevant.

| Edge case | Ver | Pkg | Go? | Suggested test |
|---|---|---|---|---|
| RPC framing: strict LF-delimited JSONL; `U+2028`/`U+2029` broke `readline` | 0.57.0 | CA | G | Go `bufio.Scanner` line limit (64 KB) breaks large messages; use `bufio.Reader.ReadBytes` |
| RPC/JSON/print modes: stdout is the protocol; redirect stray stdout writes to stderr; suppress process warnings; take over stdout during startup; retry transient backpressure; flush queued output on shutdown | 0.61.0, 0.61.1, 0.62.0, 0.76.0 | CA | Y | Noisy startup does not corrupt stdout |
| JSON mode exited before flushing final events | 0.24.0 | CA | Y | Large final event, immediate exit |
| Unknown RPC command must return an error with the request id; `prompt` response only after preflight success; client rejects pending requests when child exits; listeners unsubscribing during dispatch skipped the next listener | 0.67.3, 0.76.0, 0.79.7, 0.99.0 | CA | Y | Client tests |
| Direct RPC `steer`, `follow_up`, `bash` must go through extension `input`/`user_bash` handlers | 0.83.0, 0.86.0 | CA | Y | Hook parity test |
| `toolcall_start` events lacked ID and name; `message_update` dropped cumulative usage | 0.84.2, 0.84.3 | CA | Y | Schema test |
| `get_session_stats` exposes `contextUsage` for headless clients | 0.63.0 | CA | Y | Field present |
| Print mode: piped stdin plus explicit prompt merged; `echo foo | pi` implies print; errors exit code 1; missing/invalid `--mode` exits non-zero; `session_shutdown` emitted so non-interactive runs terminate; CLI `--help` exits even if extensions keep the loop alive; WebSocket sessions closed at shutdown | 0.47.0, 0.60.0, 0.63.0, 0.73.0, 0.52.0, 0.87.1 | CA | Y | Exit code tests |
| Extension lifecycle: failed factory leaves subscriptions/providers; event-bus listeners survive reload; stale `ctx` after session replacement; `session_start` before UI ready; shutdown order (UI teardown after handlers, before invalidation); SIGTERM/SIGHUP emit `session_shutdown`; provider registration after load takes effect; duplicate command names; invalid registrations must not block others | 0.55.2, 0.58.1, 0.61.0, 0.67.3, 0.70.0, 0.77.0, 0.84.0, 0.84.3, Unreleased | CA | Y | Lifecycle matrix (load fail, reload, replace, shutdown) |
| Hook mutation semantics: `tool_call` input mutation not re-validated (documented); `tool_result` patches chain across handlers; chained system-prompt changes visible to later handlers | 0.52.7, 0.63.1, 0.69.0 | CA | Y | Ordering tests |
| Built-in and extension tool overrides keep custom renderers; builtin-only disable flag must keep custom tools | 0.63.1, 0.70.0 | CA | P | UI-specific |
| Extension loading: jiti, virtual modules, Bun binaries, Windows alias resolution | 0.29.1 to 0.56.1 | CA | P | Skip (use Go plugins or subprocess MCP) |

## 21. Packaging, install, update (mostly Pi-specific)

About 177 fixes concern npm, pnpm, Bun, Node versions, git packages, standalone binaries, and self-update. Go relevance is limited to these:

| Edge case | Ver | Go? | Suggested test |
|---|---|---|---|
| Self-update must verify and atomically activate; not downgrade when installed is newer; skip when current; Windows cannot replace a running/loaded binary | 0.70.6, 0.75.2, 0.84.3 | Y | Update flow on Windows CI |
| Managed tool downloads (`fd`, `rg`): musl, macOS x86_64, no GitHub API dependency, Windows zip via tar, stream error handling, must not block TUI start, offline-safe | 0.55.1, 0.58.1, 0.74.1, 0.85.0 | Y | Only if Ask downloads helpers; else bundle or require them |
| Standalone binaries: baseline CPU (no AVX2), bunfig autoload, embedded asset paths, WASM images | 0.48.0, 0.84.0, 0.84.1 | G | Go static binary avoids most; keep `GOAMD64=v1` |
| Git packages: reject unsafe host/path, keep clones inside root, force-push safe update, partial install cleanup, production-only deps | 0.62.0, 0.78.1, 0.83.0, 0.84.0 | Y | Path traversal in package source |
| Temp files in private `0700` dir, not shared `os.tmpdir()`; cloud-sync ignore metadata | 0.75.4, 0.78.1 | Y | Perms test |
| HTML export XSS: sanitize link/image URLs with a scheme allow-list after stripping control chars; escape attribute quotes, image data, session metadata | 0.31.0, 0.69.0, 0.70.6, 0.75.5, 0.78.1 | Y | Malicious session export fuzz (`javascript:`) |
| Settings: invalid JSON must not be overwritten with empty; merge-on-write to keep external edits; `~` expansion in dirs and env; `.pi` dir not created on read | 0.38.0, 0.48.0, 0.50.4, 0.53.0, 0.54.2, 0.68.1 | Y | Corrupt settings file untouched |
| Skills discovery: symlinks deduped by canonical path, stop recursion at `SKILL.md`, BOM/multi-line YAML frontmatter, ignore unknown fields, root `README.md` not a broken skill, name/dir mismatch only a warning | 0.24.0, 0.27.4, 0.28.0, 0.47.0, 0.63.1, 0.74.1, 0.84.3 | Y | Fixture tree |

## 22. Other (cross-cutting)

- Error text from providers must include response bodies, not opaque SDK messages (0.80.3, 0.58.0 for `response.failed`). Test: error message contains status, code, body.
- Provider error normalization must not treat arrays or class instances as structured bodies (0.84.0). In Go: unmarshal into `any` and check type.
- `ModelsError` appends the underlying cause (0.82.1). Go: wrap errors with `%w`.
- Sequence-generation and ID libraries: UUIDv7 sequence bug after dependency removal (0.74.1). Test monotonic IDs under same-millisecond calls.
- Lazily resolved home directory (`os.homedir()` at module load) broke sandboxes (0.42.2). Go: resolve at use.
- `PI_CODING_AGENT_DIR` must be honored by every path, including logs, crash logs, error messages, examples (0.49.3, 0.82.0). Test: one config-root function, grep for hard-coded `~/.pi`.
- Model resolution: `provider/model:thinking` suffix; model IDs containing `/` or `:` (OpenRouter `:free`, LM Studio `unsloth/...`); ambiguous bare ID across providers must error; literal bracket IDs before glob (0.24.1, 0.58.2, 0.82.0, 0.84.0). Test: ID parsing table.
- Scoped models: glob patterns, remember last selection, keep order, still reachable after logout (0.31.0, 0.46.0, 0.51.1, 0.67.3).
- Thinking level: clamp to model capability on switch; idempotent set; `off` must truly disable for every vendor, not just omit the field (0.22.3, 0.25.0, 0.50.8, 0.62.0).
- System prompt: XML tag boundaries for context files (0.75.0); custom prompt must not concatenate cwd with appended content (0.84.2); forced prompt with mid-conversation system messages (0.86.0).

## 23. Source spot checks (path:line)

| Claim | Location |
|---|---|
| Truncation defaults 2000 lines / 50 KB | `packages/coding-agent/src/core/tools/truncate.ts:11-12` |
| Edit normalization: NFKC plus quote/dash folding | `packages/coding-agent/src/core/tools/edit-diff.ts:34-49` |
| BOM stripped before matching | `packages/coding-agent/src/core/tools/edit-diff.ts:533-535` |
| macOS screenshot U+202F, NFD, curly-quote variants | `packages/coding-agent/src/core/tools/path-utils.ts:5-72` |
| Kill process tree, `taskkill.exe` under `SystemRoot`, negative-PID SIGKILL | `packages/coding-agent/src/utils/shell.ts:187-212` |
| Per-file mutation queue keyed by `realpath` | `packages/coding-agent/src/core/tools/file-mutation-queue.ts:4-30` |
| Overflow patterns and non-overflow exclusions | `packages/ai/src/utils/overflow.ts:37-80` |
| Overflow errors bypass retry, go to compaction | `packages/coding-agent/src/core/agent-session.ts:3611-3614` |
| Retry defaults: 3 retries, 2 s base, 60 s cap | `packages/coding-agent/src/core/settings-manager.ts:48-50` |
| GIF magic requires `GIF87a`/`GIF89a` | `packages/coding-agent/src/utils/mime.ts:13` |
| Partial JSON parser for streaming tool args | `packages/ai/src/utils/json-parse.ts:104` |
| Auth file uses `proper-lockfile` with lock-compromised handling | `packages/coding-agent/src/core/auth-storage.ts:9,76,128,166` |

## 24. Top 30 edge cases a Go rewrite MUST design for

Ranked by (recurrence in changelogs) x (severity) x (applies to Go). Each has a matching test above.

1. **Tool-call/tool-result pairing invariant.** Every assistant tool call has exactly one result, in order, even after abort, error, length truncation, compaction, branch switch, or provider handoff. Synthesize missing results; drop errored assistant messages on replay (0.49.0, 0.69.0, 0.80.4).
2. **Thinking replay as per-vendor data.** Signed, unsigned, redacted, empty-signature, and cross-model cases. Convert to text without tags when the target cannot replay (sections 13, 4).
3. **Per-model compat record instead of URL heuristics.** Roles, strict schemas, cache headers, token-limit field name, thinking format, usage location.
4. **Stream completeness.** A stream without a terminal event is an error and is retried, for every protocol; tolerate malformed partial JSON, null chunks, missing IDs, missing `output_index`, unknown events.
5. **Overflow vs rate-limit vs transient classification as a data-driven table**, with fixtures per vendor; never classify bodyless 400/413 or throttling as overflow.
6. **Retry policy as one layer.** Disable SDK retries; honor `Retry-After` (ms and seconds, bad dates); cap delay; abort-aware sleeps; never retry quota; reset counter per successful response; also apply to summarizer calls.
7. **Compaction state machine.** One mutex for manual, threshold, and overflow triggers; latest compaction entry wins; no stale pre-compaction usage; bounded summary input; refuse empty; never persist truncated summaries; no tools in summary requests; queued messages preserved.
8. **Usage and cost.** Cache TTL tiers, returned-model pricing, service tiers, reasoning-token double counts, cached-token subtraction, retry accumulation, nested tool usage, and estimate fallback when usage is missing or zero.
9. **JSONL session durability.** Atomic publish on fork and repair; tolerate missing trailing newline; never overwrite invalid files; header validation; lazy file creation; streaming reads for huge files.
10. **Session write ordering.** Serialize event persistence so results never precede their calls; abort and persist the outgoing turn on session switch.
11. **Fork and branch correctness.** Parent chains through labels, compaction boundary retained, compaction ignores abandoned branches, branch summary source vs destination.
12. **Process-tree kill and no-orphans.** Unix process groups, Windows `taskkill /T` with graceful absence, signal handlers that kill children, SIGTERM/SIGHUP shutdown that restores the terminal.
13. **Bash output pipeline.** Stream incrementally; keep draining after exit; strip all ANSI/OSC/DCS; streaming UTF-8 decode; full output to temp file when truncated by either limit; bounded FDs; signal exit not success; validated timeouts.
14. **Windows shell matrix.** Git Bash, MSYS, Cygwin, WSL (including legacy `bash.exe` via stdin), PowerShell, drive-letter mapping, backslash cwd, no console flash, detached descendants holding pipes.
15. **Edit tool normalisation.** CRLF, BOM, NFKC/smart quotes, fuzzy match that rewrites only the matched block, empty file, unique-match enforcement, tolerant input shapes (JSON-string `edits`, single object, extra fields).
16. **Per-path write serialization** keyed by resolved real path.
17. **File name and path oddities.** macOS NFD, U+202F, curly quotes, `@` prefix, quoted paths with spaces, symlinks (dedupe by canonical path), `.gitignore` nesting, ancestor reads policy.
18. **Binary and content sniffing.** Magic bytes only (`GIF8` is not enough), EXIF orientation with foreign APP1, size limits with progressive downscale, non-vision placeholders, empty tool output placeholder.
19. **Context propagation for cancellation everywhere** (auth, OAuth, locks, HTTP bodies, retries, subprocess, search) plus a goroutine-leak test.
20. **Credential store concurrency.** Cross-process file lock, read-merge-write, compromised-lock handling, refresh with expiry margin, stalled refresh releases lock, no logout race between instances, never clobber file ACLs.
21. **OAuth flows.** Port-in-use fallback, provider error redirect fails fast, device-code `slow_down` and first-poll wait, proxy support, no stderr writes under TUI.
22. **Config value resolution.** Literal by default, explicit `$ENV`/`!cmd`, resolved at request time, JSON with comments/trailing commas tolerated, corrupt file never overwritten, external edits preserved on save.
23. **Terminal cleanup guarantee.** Cursor, alternate screen, keyboard protocol flags, mouse modes, bracketed paste restored on every exit path including panic, SIGTERM, SIGHUP, SIGTSTP/SIGCONT, and lost stdin; drain stdin on exit over SSH.
24. **Width engine.** Grapheme-aware width (emoji ZWJ, regional indicators, Indic, Thai/Lao, CJK), OSC/CSI ignored in width, tabs normalized, truncation closes SGR and OSC 8; property test "no line exceeds width".
25. **Differential renderer rules.** Full redraw only on width change; handle shrink, overlays, scrollback commit; throttle with immediate-flush option; deterministic clock for tests.
26. **Key decoding matrix.** Legacy, CSI-u/Kitty (with lock bits, base layout, keypad, release events), modifyOtherKeys (tmux), Windows console quirks, Alt+Enter over SSH with Escape timeout; capture-based tests.
27. **Paste handling.** Bracketed paste atomic, marker expansion at submit/queue/export, undo-safe registry, tab normalization, no filtering of look-alike release sequences.
28. **Queueing semantics.** Steer waits for the tool batch; follow-ups drain before idle; messages typed during compaction/summary/retry are delivered later and do not clobber the editor; `prompt` while streaming is an error.
29. **RPC/print protocol hygiene.** stdout is protocol-only, unlimited line length, strict LF framing, flush before exit, errors carry request ids, hook parity between RPC and UI paths, non-zero exit on failure.
30. **Model/config data as versioned data files** (context window, output cap, pricing tiers, thinking levels, compat flags, provider defaults) with user overrides and remote refresh that never blocks startup and never overwrites newer bundled data; deprecated defaults get a fallback instead of a hard failure.

Runner-up items worth a test: HTML/markdown export sanitization; hyperlink capability detection (default off in unknown terminals, tmux, screen); system-prompt cache stability (ISO date only, deterministic format); offline mode with bounded network waits.

## 25. What Go changes (do not port blindly)

- Skip: stack overflow from spread (`push(...x)`), V8 string limits, undici and fetch dispatcher issues, jiti, Bun virtual FS, npm/pnpm behaviours, `readline` U+2028 bug, UTF-16 length confusion (but keep byte vs rune vs cell explicit), `process.env` empty under Bun, `Illegal invocation` on Workers.
- Go-specific risks the Pi changelog never had to fix: `bufio.Scanner` 64 KB token limit for JSONL and SSE; `http.Client.Timeout` killing long streams; goroutine leaks on cancel; data races in event ordering; `os/exec` `Wait` blocking while grandchildren hold the pipe (the Windows hang above appears on Unix as well with `cmd.Wait()` and `StdoutPipe`); `exec.CommandContext` kills only the direct child; default HTTP/2 transport reuse after GOAWAY; `filepath` vs `path` on Windows.
- Design consequence: use `context.Context`, `exec.Cmd.SysProcAttr{Setpgid:true}` (and `WaitDelay`), and a single-writer goroutine for session persistence.

## 26. Limitations

- Bullets were read truncated to about 215 characters; where they were vague (for example "fixed streaming for Z.ai") I did not open the diff. Version numbers are from the changelog headings, not from git blame.
- Approximate counts in section 3 come from keyword matching and overlap.
- I did not read issues, PR threads, or tests behind each fix; suggested tests are inferred from the bullet text.
- The vendor list will age quickly: Pi's own changelog shows model IDs and API behaviour changing monthly. Treat section 13 as a checklist of categories, not a compatibility matrix.
- Section 20 and 21 depend on whether Ask ships extensions, RPC, self-update, or downloads helper binaries. That scope is not decided in the material I read.

## 27. Unresolved questions

1. Does Ask ship an in-process extension/hook API, or only MCP subprocess tools? This decides whether section 20 lifecycle bugs (stale context, reload, shutdown order) apply.
2. Is alternate-screen mouse UI (fullscreen mode) in scope for `cmd/tui`? Pi's roughly 60 fullscreen mouse, selection, and Kitty-image fixes only matter if yes.
3. Which providers does Ask target at launch? Section 13 is large; the minimum viable set changes which quirks to implement first.
4. Should Ask persist sessions as JSONL files (Pi's model) or in SQLite (Ask already has `store` and migrations)? Pi added a SQLite backend (`sqlite-node`) late; JSONL issues in section 10 partly disappear with SQLite, but fork, branch-summary, and compaction-boundary issues stay.
5. Does Ask need Windows and WSL support at launch? Windows added about 68 fixes; deferring it changes sections 5, 6, 18, 19.

Status: DONE
Summary: Catalogued about 1,820 Pi fixes across all 12 changelogs into 22 areas with version, package, Go-applicability, and suggested test, ranked repeat offenders, and produced a top-30 must-design list.
Concerns: Changelog bullets were read truncated to about 215 characters and only 12 items were verified at source; area counts are approximate keyword tallies.
