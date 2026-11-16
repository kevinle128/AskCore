# omp (oh-my-pi) behavior for the ocr triage issues

Source: `/Users/dale/Desktop/workspace/opensources/oh-my-pi` at `efbc171`. Paths below are relative to `packages/`. Read-only study; no omp code was run.
Background: [triage summary](./triage-261110-1809-ocr-summary.md).

Outcome: omp has a direct, stronger model for R1 (claim, then drain queued input, then restore on exit), R3 (4 MiB buffer cap, streamed pages, binary sniff), and R9 (clone, then return the clone).
omp has no equivalent or a weaker one for R2, R4, R5, R11, and Q-write; for R6 it documents that a user `Authorization` header wins.
R7: omp detects staleness by content tags, not by mtime, and has no lock.

## R1: continue with nothing to continue, while input arrives

omp behavior:
- `Agent.continue()` claims the run first: it throws `AgentBusyError` when `isStreaming`, then sets `#runningPrompt`, a new `AbortController`, and `isStreaming = true` before it reads the transcript (`agent/src/agent.ts:1613-1625`).
- After the claim it does not fail on an empty or assistant-tail transcript. It drains the steering queue, then the follow-up queue, and starts the run with them (`agent/src/agent.ts:1628-1676`). Only when both queues are empty does it throw `No messages to continue from` or `Cannot continue from message role: assistant`. The comment cites issue #6344: an earlier throw left a queued message undeliverable and the idle drain re-armed `continue()` in a loop until OOM (`agent/src/agent.ts:1631-1636`).
- An unpaired tool-call tail wins over queued input, so a message is never put between tool calls and results (`agent/src/agent.ts:1652-1659`).
- `finally` calls `#restoreUndeliveredQueuedMessages(controller)`, which puts input that the run took but did not deliver back at the front of the queues (`agent/src/agent.ts:1680`, `1043-1066`).
- The session layer re-checks after every settle: `#drainStrandedQueuedMessages` calls `#scheduleQueuedMessageDrain`, which runs `continue()` only when idle and `hasQueuedMessages()` (`coding-agent/src/session/agent-session.ts:1138-1170`, `8303-8331`). On a drain error it sets `#queuedMessageDrainBlocked` to stop a retry loop (`agent-session.ts:8326-8329`).

Lesson for Ask: claim the slot, then treat queued steer/follow-up as valid start input before the "nothing to continue" check, and re-check the queue when the slot is released. A re-check without a block flag can loop (omp #6344).

## R2: project context files that are symlinks

omp behavior:
- `readFile` uses `fs.stat`, which follows symlinks, and only refuses non-regular files (FIFO, socket) so that startup cannot hang. The comment says symlinked context files must still resolve (`coding-agent/src/capability/fs.ts:17-30`).
- `loadStandaloneContextFiles` walks from cwd up to a boundary (repo root or home) and reads `AGENTS.md`/`CLAUDE.md` in each directory with that `readFile`. It does no realpath and no containment check on the target (`coding-agent/src/discovery/helpers.ts:713-753`). `.omp/AGENTS.md` loads the same way from the nearest project `.omp/` (`coding-agent/src/discovery/builtin.ts:925-944`, `96-107`).
- No project-trust gate exists for context files. The only "trusted" concept is `--trusted-extensions` for extension modules (`coding-agent/src/main.ts:498-512`, `sdk.ts:4820`); the other `trust` hits are unrelated (`modes/print-mode.ts:51`, `modes/interactive-mode.ts:597`).
- omp does have a containment helper (`realpath` then lexical check, fail closed), but uses it only for plugin packages and `skill://` paths (`coding-agent/src/discovery/contained-path.ts:1-80`).

Lesson for Ask: omp accepts the same exposure that ocr flagged, so blocking only targets outside the repo is stricter than omp, not a parity gap. If Ask adds the check, a realpath-plus-lexical helper like `resolveContainedPath` is the simple model.

## R3: read tool on very large files

omp behavior:
- Output caps are 3000 lines and 50 KiB per call (`tui/src/tools/streaming-output.ts:11-13`).
- A file is buffered whole only when it is 4 MiB or less (`SNAPSHOT_MAX_BYTES`, `coding-agent/src/tools/read.ts:185`, `1979`). A larger file is read by `streamLinesFromFile` with one reused 8 KiB buffer (`read.ts:436-446`, `tools/read-format.ts:241`), and it stops the scan after the window is collected (`stopScanAfterCollect: fileSize > SNAPSHOT_MAX_BYTES`, `read.ts:1534`, `2294`).
- A tail read keeps the byte offsets of at most 64 Ki lines so the next window can seek (`read.ts:659-717`).
- Binary detection runs on the bytes before any UTF-8 decode, from the buffered header or from a file sniff when not buffered. A binary file is refused with a message that names `:raw` (`read.ts:2094-2125`).

Lesson for Ask: use a size threshold (omp: 4 MiB) below which a whole read is fine and above which a buffered line scanner reads only the page. Sniff for binary on the first bytes before decode.

## R4: duplicate or case-folded keys in JSON-RPC envelopes

omp behavior:
- RPC mode reads newline-delimited frames with plain `JSON.parse` and passes the value to the dispatcher. A parse error is reported and the next line is read (`coding-agent/src/modes/rpc/rpc-input.ts:46-64`, `rpc-mode.ts:2671-2675`). `JSON.parse` keeps the last value of a duplicate key and is case-sensitive, and omp adds no duplicate or case-fold check.
- The ACP transport also uses `JSON.parse` and checks only `jsonrpc === "2.0"` (`utils/src/acp/stream.ts:71-77`).
- Unknown command types get an error response (`rpc-mode.ts:2627`).

Lesson for Ask: omp has no equivalent check. Go `encoding/json` matches struct fields case-insensitively, which JS does not, so the Ask risk is specific to Go; a fold-duplicate check in `parseMessage` closes it, and omp gives no reason to skip it.

## R5: executable check before a background or daemon spawn

omp behavior:
- The daemon broker is started detached from omp's own binary: `resolveWorkerSpawnCmd` uses `process.execPath` (or `argv0` / `PATH` when the old path was removed by an upgrade) (`coding-agent/src/launch/client.ts:31`, `318-331`; `coding-agent/src/subprocess/worker-client.ts:115-178`).
- It checks only that the file is executable (`isExecutable`, `worker-client.ts:123`). No owner or group-write check exists on the binary.
- The protection is on the rendezvous instead: runtime dirs `0700`, pid file `0600`, socket `chmod 0600` (`launch/client.ts:70`, `launch/broker.ts:229`, `394`, `478`).

Lesson for Ask: omp does not check write bits on the binary it spawns. Fixing the comment to match the `0002` mask is consistent with omp; a stricter group rule has no precedent there.

## R6: models config `Authorization` header vs the resolved API key

omp behavior:
- On the OpenAI-compatible path the headers start as `{ ...model.headers }` (`ai/src/providers/openai-shared.ts:230`), and the key is added with `headers.Authorization ??= "Bearer <key>"`. The comment states: "A caller-supplied Authorization in `model.headers` still wins" (`openai-shared.ts:340-348`).
- The `??=` test is case-sensitive, so a lowercase `authorization` header is not seen (my inference from the code; not tested).
- `authHeader: true` in models config makes omp set `Authorization: Bearer <apiKey>` after the configured headers, so then the key wins (`coding-agent/src/config/resolve-config-value.ts:171-200`).
- omp does not filter credential headers; it makes the user header the documented override. Completions and Responses both import the same builder `resolveOpenAIRequestSetup` (`ai/src/providers/openai-shared.ts:215`; imported at `openai-completions.ts:120`, `openai-responses.ts:114`), so the rule is the same on both paths.

Lesson for Ask: omp treats a user `Authorization` header as a supported proxy sign-in path that wins. If Ask keeps that design, make it the same on Completions and Responses, compare names case-insensitively, and document it.

## R7: edit race with an external writer

omp behavior:
- The hashline edit mode anchors each edit to a 4-character content tag of a session snapshot that `read`/`search`/`write` recorded (`coding-agent/src/edit/store.ts:1-6`; `crates/pi-edit/src/store.rs:24-31`).
- When the live file no longer matches the tag, omp tries recovery: from a cached snapshot when an external write is found, from an earlier in-session snapshot, or by remapping anchors to unchanged lines, each with a warning. Head/tail inserts apply with a warning (`crates/pi-edit/src/modes/hashline/messages.rs:680-702`; diagnostics in `mismatch.rs:1-27`).
- Right before the write, omp reads the pre-image bytes, and after the write it compares disk with the pre-image to find a write that did not land (`coding-agent/src/edit/index.ts:704-740`). The comment says a stat cannot do this because of coarse mtimes.
- No file lock, and no re-stat between the edit's own read and its write.

Lesson for Ask: omp accepts the small read-to-write window and spends its effort on content-based (not mtime-based) staleness. For Ask, a size+mtime re-stat before the rename is cheap but weak; a content hash check is the stronger match to omp.

## R8: validation of input messages (steer, notices)

omp behavior:
- `Agent.steer()` and `followUp()` push the typed `AgentMessage` with no runtime check (`agent/src/agent.ts:1343-1356`). TypeScript types are the only guard in-process.
- At the wire edge, RPC mode validates per command with `typeof` checks and returns an error response, for example `message must be a string` (`coding-agent/src/modes/rpc/rpc-mode.ts:1888-1899`).

Lesson for Ask: the typed-nil case has no TS analogue. omp's pattern is to validate at the external boundary and trust in-process callers, which supports a single generic nil check at Ask's notice entry, not deep checks.

## R9/R10: handlers that change the request context or a tool result

omp behavior:
- `emitContext` returns early when no handler exists. Otherwise it gives handlers a `structuredClone` of the messages (shallow copy when cloning fails), and it returns the clone. Thus an in-place edit by a handler is kept, not lost. A handler that returns `messages` replaces the array. Change detection per message uses `Bun.deepEquals` against the original (`coding-agent/src/extensibility/extensions/runner.ts:1969-2030`).
- `emitToolResult` gives handlers a shallow copy `{ ...event }`. Only returned `content`, `details`, or `isError` fields count as a change (`modified = true`); an in-place edit of `event.content` changes the shared array but is not reported as modified (`runner.ts:1744-1793`).

Lesson for Ask: for the context, copy before dispatch and return the copy, as omp does, so in-place edits are not lost. For tool results, detect change only from returned values and document that in-place edits are unsupported.

## R11: request capture and session files on disk

omp behavior:
- The only request capture to disk is `appendRawHttpRequestDumpFor400`: it writes the raw request of an HTTP 400 as JSON under `<logs>/<dump dir>` with `Bun.write` and no mode, and skips this under the test runner (`ai/src/utils/http-inspector.ts:82-98`). The file gets the umask default (usually `0644`).
- Session JSONL writer: the parent dir is created with default mode and the file is opened with no mode (`coding-agent/src/session/session-storage.ts:249-258`; `utils/src/fs-open.ts:29-31`), so permissions come from the umask (usually `0755`/`0644`).
- The logs dir is also created with default mode (`utils/src/logger.ts:183-187`).
- Private modes are used for specific state: the session lease dir is `0700` and is refused when another user owns it or it has group/other bits (`session-storage.ts:572-582`); agent DB dir `0700` (`session/agent-storage.ts:431`); pins and btw history files `0600` (`session/session-pins.ts:52`, `session/btw-history.ts:183`).

Lesson for Ask: omp is not a model here; it leaves transcripts at umask defaults. Ask's planned `0700`/`0600` for `ASK_CAPTURE` matches omp's own pattern for sensitive state, and a capture holds full requests, so it is sensitive.

## Q-write: stale-read guard on write

omp behavior:
- `write` strips a hashline `[path#TAG]` wrapper only to find the target path; it does not compare the tag with the file (`coding-agent/src/tools/write.ts:465-468`, `768-772`).
- `write` has no mtime or snapshot check, and overwrites a file changed since the session read it. No `stat` call is used for staleness; the only `stat` is the shebang chmod (`coding-agent/src/tools/write.ts:366-376`).
- The guard it has is content-based: it refuses to overwrite a file when the new content ends with an omp read-truncation notice and covers less than the current file (`write.ts:308`).
- Staleness detection lives in `edit` (see R7), not in `write`.

Lesson for Ask: omp matches Pi and Ask today (overwrite). If Ask adds a guard, the omp precedent is a narrow one (refuse a write that would drop unseen content), not a full Claude Code read-before-write rule.

## Limitations

- Line numbers are at `efbc171`. I did not run omp or its tests, so runtime claims (for example the `??=` case-sensitivity effect in R6) are from code reading.
- R7: I did not trace the hashline mismatch path to the exact hard-fail branch; the recovery messages show the behavior.
- R5: I did not read `isExecutable` itself.
- R11: I did not check whether the 400 dump redacts the `Authorization` header.

## Unresolved questions

- R6: does Ask want a user `Authorization` header to win (omp: yes)? This is the open user decision from the triage.
- Q-write: full stale-read guard, omp's narrow truncation guard, or no change?
