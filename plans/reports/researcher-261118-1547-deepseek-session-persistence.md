# DeepSeek Harness session persistence: study for AskCore H8

## Source manifest

| Item | Value |
|---|---|
| Path | `/Users/dale/Desktop/workspace/opensources/deepseek-harness` (paths below are relative to it, unless they start with `AskCore:`) |
| Commit | `ca33fd6843b205e279c7ccb6cb73a472c78f5760` (verified with `git rev-parse HEAD`) |
| License | MIT (`LICENSE`) |
| Language | TypeScript on Node (`node:sqlite`, `node:fs`), Cordis plugin framework |
| Method | Read-only. `rg` and Read on source, docs and tests. The GitNexus MCP tools were not available in this session, and I did not try the `gitnexus` CLI. No GitNexus query is cited. |
| Evidence limits | No tests were run. "Test proves X" means that I read the assertion, not that I saw it pass. Windows paths and zstd internals were not read. The three-independent-sources rule does not apply to a single-repository study. The source is untrusted data; I ran no scripts from it. |
| Docs drift found | The JSONL README layout calls `session.v3.jsonl` "current" (`packages/session/session-persistence-jsonl/README.md`, "On-disk layout"). The code says v4: `SESSION_FORMAT_VERSION = 4` (`packages/core/session/src/types.ts:90`). |

## Outcome in short

DeepSeek keeps one append-only, typed event log for each session. The log is the only source of truth, and the model context is a fold over it. One abstract seam (`create/open/stat/list`, with handles that do `read/append/flush/close`) has two backends. JSONL is the default. SQLite is optional, and it imports JSONL on first open. Write ownership uses a kernel `flock` on a lock file. It has no expiry, no renewal and no generation. A second writer is rejected at once with `SessionAlreadyOwnedError`. It does not wait and it cannot take over. If the holder process dies, the kernel releases the lock, so another process can take over immediately. Durability is write-behind (a 200 ms window) plus explicit flush checkpoints before each model call and each tool side effect. Crash repair adds synthetic closers at resume. It never truncates the valid contiguous prefix; only a torn or uncommitted tail is dropped.

For AskCore, the most important difference is in ownership. The AskCore H8 plan specifies an expiring SQLite lease row with an ownership generation (AskCore: `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md`, H8 "H-SESS-15"). DeepSeek rejects expiry on purpose: "there is deliberately no expiry that could expropriate a stalled writer whose resumed appends would tear the log" (`packages/session/session-persistence-sqlite/src/lease.ts:11-13`). Topic 9 puts both side by side and does not change the user-confirmed plan.

---

## 1. Storage model

**Logical model.** Each session is a header plus a contiguous `SessionEvent[]`. Every event has `type`, `seq` (with `seq` equal to its log index), `time` and `data`, and it can have `ignorable: true` (`docs/subsystems/session.md:228-291`). Every surface event also has `surfaceOp` and an optional `sourceEventSeqs`. Header fields are `version`, `id`, `createdAt`, `cwd`, `parentSession`, `isSeeded`, `origin`, `delegationDepth` and `agentPreset` (`docs/subsystems/persistence.md:147-192`). The header is not in the log and never reaches the model (`persistence.md:139`).

**JSONL backend (default).**
- Location: `<root>/--<normalized-cwd>--/<encoded-id>/session.vN.jsonl[.zstd]`. `root` is required and has no default (`session-persistence-jsonl/README.md`, "Minimal configuration" and "On-disk layout"). Path helpers: `projectDir` and `sessionDir` (`session-persistence-jsonl/src/format.ts:254,267`) and `logPath` (`format.ts:298`).
- Format: row 1 is a header (`{"type":"session","version":4,...}`), as in `snapshots/session/workspace-edit/session.v4.jsonl:1`. After that comes one JSON row for each event (`format.ts:313-324`). The default encoding is concatenated zstd frames: one frame for the header and one frame for each append batch. `compression: 'none'` gives plain lines (README, "Physical encoding").
- Generation file name: `session.jsonl` for v0 and `session.vN.jsonl` after that (`packages/session/session-format/src/filename.ts:14-16`).

**SQLite backend (optional).**
- Location: `<root>/.segments/sessions.sqlite` (`session-persistence-sqlite/README.md`, "Minimal configuration").
- DDL (`session-persistence-sqlite/src/sqlite.ts:270-313`):
  - `segments(segment_uuid PK, parent_segment_uuid FK, parent_end_seq, base_seq)`: a segment points to a prefix of its parent.
  - `traces(trace_uuid PK, header TEXT JSON, tail_segment_uuid UNIQUE, inherited_count)`: one row for each session.
  - `frames(segment_uuid, start_seq, end_seq, codec, payload BLOB, PK(segment_uuid,start_seq))`: each row holds up to 64 events as JSON lines, zstd-compressed when that makes it smaller (`frameEvents`, README config table).
  - `segment_ancestors`: a closure table that a trigger fills (`sqlite.ts:298-312`).
  - Triggers make frames append-only (`frames_no_update` and `frames_no_delete`, `sqlite.ts:288-289`) and make ancestry immutable (`sqlite.ts:290-291,309-312`).
- Pragmas: `foreign_keys=ON`, `synchronous=FULL`, `journal_mode=WAL` (retried on BUSY), busy timeout 5000 ms (`sqlite.ts:42,203-217`).

**Schema version and migration.**
- The logical format is v4 (`core/session/src/types.ts:90`). Old JSONL generations go through a chain of adjacent codecs: v0→v1→v2→v3→v4, one package for each step (`packages/session/session-format-v*-to-v*`, chain in `session-format/src/chain.ts:41`).
- A read open migrates in memory and publishes nothing. A write open encodes a temporary successor, checks it in a Worker, checks the source revision again, and publishes `session.v4.jsonl` without overwrite. The old file stays byte-identical (`persistence.md:197`; JSONL README, "Reading the logs").
- SQLite has its own `user_version = 1` and `application_id`. The backend refuses an incompatible database and never resets it. It also refuses an unrelated database that has tables (`sqlite.ts:31-33,218-227`). SQLite stores only the current format, so a future format upgrade needs its own SQLite migration (SQLite README, "Known Limitations").
- Vocabulary is fail-closed: a reader refuses an unknown event type unless the event has `ignorable: true` (`session-persistence/src/storage-contract.ts:73-80`). A `SessionFormatUnsupportedError` (a newer writer) is kept separate from `SessionPersistenceCorruptionError` (`session-persistence/src/errors.ts:93,111`).

## 2. Entry model

- **Append-only, linear `seq`, no parent id.** "events are contiguous from seq 0 and never rewritten" (`persistence.md:381`). Every append is checked against the stored next-seq (`storage-contract.ts:146-151`). No entry has an id/parentId tree.
- **Branching means a fork into a new session.** `fork(source, boundary?)` creates a separate session. Its seed is the prefix, and a `session/end-seed {inherited:true}` marker sits at the cut. If the boundary is inside an open turn, the fork closes that turn with a `forked` reason (`session.md:707-713,768-776`). SQLite stores the shared prefix by reference: the child segment points to `(parent_segment_uuid, parent_end_seq)`, and the backend finds the cut through the ancestor index (`sqlite.ts:428-445,486-519`). The tree exists across sessions at the storage level, not inside one log.
- **What is persisted** (generated catalog, `docs/persistence-catalog.md:167-1100`):
  - Turn and step brackets: `turn/start`, `turn/end{reason}`, `step/*`.
  - Messages: `user/message`, `assistant/message` (with the exact compact provider stream and `usage`; no separate usage record, `session.md:79-97`), `assistant/attempt` (failed or retried attempts), `tool/call` (raw argument string), `tool/result` (with error identity and tool `meta`).
  - The rendered system prompt as a surface node: `system/message`.
  - `request/header`: a full request envelope (config, adapter defaults, tool schemas). It is written as a snapshot with reason `initial|resume|change|series`. It is not a delta; the old `request/header-delta` is refused (`session.md:183-208`).
  - `request/context` (route capacity), `model/selection`, `compaction/start|summary|prune|end`, `session/title`, and plugin records `plugin:*` (always `ignorable`, `session.md:786-827`).
- **Unknown future data.** A writer marks only purely informational events `ignorable`. An unknown required event refuses reconstruction (`session.md:276-286`).

## 3. Write path

- **Who writes.** Live `session/event` notifications go into the session's single write handle. A `LiveWriteBuffer` does the batching: a fixed 200 ms window, and 256 events also start a write on SQLite (`sqlite.ts:43-46,973-986`; JSONL `storage.ts:37,281-284`). `append()` resolves when the batch is accepted. Only `flush()` promises durability (`persistence.md:67-91`).
- **Checkpoints.** `session-checkpoint-policy` flushes before the model stream is built, before a top-level tool body runs, and at every pre-step. If the flush fails, the adapter or tool does not run (`session-checkpoint-policy/src/index.ts:62-82`).
- **SQLite atomicity.**
  - Each batch is one `BEGIN IMMEDIATE` transaction. Inside the transaction, the contiguity check uses `event_count`, which comes from the last frame (`sqlite.ts:769-779`).
  - If COMMIT or ROLLBACK fails, the outcome is unknown, and **the whole store is unusable until restart** (`sqlite.ts:86-88,315-337`).
  - A failed background write keeps its batch in order and pauses automatic writes. The next explicit flush retries and reports the error (`SQLite README`, "Durability and shutdown").
- **JSONL atomicity.**
  - The first write publishes the header and the first batch through a temporary file, fsync, `link()` (not `rename`, so that a race fails with EEXIST), and a directory fsync (`session-persistence-jsonl/src/index.ts:1380-1440`).
  - Each later batch is appended to the file, then fsynced. If the write or the sync fails, the file is truncated back to its old size and fsynced, so a retry cannot duplicate seqs (`index.ts:1524-1567`).
- **Torn tail.**
  - In raw mode, the scanner drops a last line that has no newline. In zstd mode, the complete records of a torn last frame are recovered.
  - The write path truncates the torn bytes and rewrites the recovered records **before** the first new append. Readers never see a torn tail (`storage.ts:322-347`; `index.ts:1023-1030,1130-1150,1569-1579`).
- **Corrupt rows.**
  - A corrupt complete zstd frame rejects as corruption (`index.ts:1118`).
  - The raw scanner in `recoverable` mode (the default, `format.ts:404`) treats a corrupt row as a crash boundary only when no later `turn/end` follows it. A later `turn/end` turns that row into a hard corruption error (`format.ts:478-521`).
- **Lazy materialization.** `create()` writes nothing. A session that is never appended or flushed leaves no trace, and closing it erases it (`persistence.md:102`; contract tests below).

## 4. Concurrency across processes

- **In process.** A `writers` map. A second `open(id,'write')` gets `SessionAlreadyOwnedError` (`sqlite.ts:638`).
- **Across processes.**
  - Non-blocking `flock` on `session.lock`. On Windows, a named semaphore (`sqlite/src/lease.ts:1-28,72-118`; the JSONL backend has an identical copy).
  - The holder checks that the locked inode is still the file at the lock path, and it retries 3 times (`lease.ts:93-117`). Release closes the descriptor and never removes the file (`lease.ts:120-136`).
  - SQLite holds **two** locks: one for each session id beside the database (`<db>.session-locks/<sha256(id)>`) and the JSONL artifact lock, so that a JSONL writer is also excluded (`sqlite.ts:601-630`).
- **Second process behavior.**
  - A write open is **rejected immediately**. There is no wait and no takeover.
  - Reads still work, and they see the new appends of the live writer.
  - When the holder dies (SIGKILL), takeover is immediate (`session-persistence-jsonl/tests/lease.two-process.e2e.ts:35-68`; SQLite `normal-fork-concurrency-validation.spec.ts:328-347`).
  - A process that is alive but stuck keeps the lock until it exits (JSONL README, "Known Limitations").
- **No lease row, no renewal, no generation.** The kernel ends the lock when the process dies. The SQLite contiguity check inside the transaction works as fencing by log position: a stale writer whose seq is behind the stored count fails with a seq mismatch (`sqlite.ts:774`, `storage-contract.ts:146-151`). This check alone does not prove who the owner is.
- **Limits.** Advisory `flock` is not reliable on NFSv3, and the Windows semaphore name is per login session (JSONL README, "Known Limitations").

## 5. Resume

- **Entry.** `ctx.agents.resume({ resumeSessionId })` (`persistence.md:234`) goes to `resumeWith` (`core/agent-loop/src/index.ts:770-841`). The steps:
  1. **Open for write first.** This excludes a concurrent resume (`index.ts:794-801`).
  2. Read the whole log (`:806`).
  3. Compute `interruptedTurnClosers` and append them through the same handle (`:809-810`). The closers cover missing tool results and open `step/end` and `turn/end{interrupted}` events (`core/session/src/repair.ts:53,209-211`). A recorded call with no result gets `TOOL_OUTCOME_UNKNOWN` with "Do not retry blindly". A call that was never started gets `TOOL_NOT_STARTED` (`repair.ts:15-35`).
  4. Prepare the session from the seed (`:811-816`).
- **Selecting a session.**
  - At startup, the agent-loop config has `sessionId` (restore or create) or `resumeSessionId`, and the two are mutually exclusive (`index.ts:258-279,339-368`). The TUI gets `--resume <session>` through the launcher (`apps/cli/src/args.ts:10,97`).
  - **Headless (the analogue of `ask -p`)** takes `--session-id <id>`. "An unknown id is an error" (`packages/bundle/headless/src/startup.ts:59,71`; `index.ts:294-301`). The command fails before resume unless the persistence and query services are present (`index.ts:263-273`). It refuses an id that is live in the same process (`index.ts:274-286`). It checks preset and cwd adoptability before and after it takes the write lock (`index.ts:287-293,202-215`). A resumed headless run uses the model configured for this run (`index.ts:369-375`).
  - I found no "continue the latest session" flag. A search of non-test `apps/` and `packages/` for `--continue|--resume|mostRecent|latestSession|lastSession` found only `--resume <id>` and `--session-id <id>`.
- **Context builder.** `Session.deriveMessages()` folds the surface. The fold uses only the five surface types, honours `replace` shadowing, skips assistant messages with empty content, and caches each node once (`session.md:696-705`). `deriveEventMessage()` is public, so offline readers apply the same rules.
- **Restored from the log.**
  - Model, provider and reasoning effort. The `modelSelection` projection prefers `pending` (the latest `model/selection` that no request has used yet), then `lastUsed` (from the latest `request/header`), then config (`core/agent/src/model-selection.ts:73-121`).
  - Exception: a startup-configured resume applies the configured route on purpose (`agent-loop/src/index.ts:357-362,382`).
  - `agentPreset`, `cwd` and `delegationDepth` come from the header (`persistence.md:180-191`).
- **Not restored from the log** (the current composition wins):
  - The tool set and the request envelope. The first request after resume logs a new `request/header{reason:'resume'}`. Tool additions and removals compared with the last logged header become a `developer/message` (`agent-loop/src/agent.ts:681-709`).
  - The system prompt is rendered again. The prompt-change rules then replace or append system nodes (`session.md:65-78`).
  - Credentials are not in the log.

## 6. Context edits and compaction

- **Mechanism.** A surface event with `surfaceOp: {op:'replace', startSeq, endSeq}` shadows a range and puts itself in that place. Its `sourceEventSeqs` must cover every shadowed node (`session.md:319-340`; enforced in `core/session/src/surface.ts:361-363`). The old events stay in the log. Only the surface changes.
- **Summary compaction.** The sequence is `compaction/start`, then `compaction/summary` (summary, shadowed seqs, token price, model), then the replacing `user/message`, then `compaction/end`. Because the lock is released last, a crash leaves an orphaned start that can be detected. A start that comes before a newer `session/end-seed` is ignored as stale (`docs/subsystems/compaction.md:9-21`; `session.md:774`).
- **Tool-result pruning.** `compaction/prune` comes first, then a replacing `tool/result`. The replacement must rewrite exactly one current `tool/result` and change only its content (`surface.ts:455-475`; `compaction.md:105-138`).
- **Content projection without replace.** Image offload uses a registered `messageProjection` and keeps node identities (`session.md:365-398`; `compaction.md:26-38`).
- **History compared with context.** The human transcript reads append-origin events (`isAppendSurfaceEvent`), so a replacement never erases what the user saw (`surface.ts:78-95`).
- **No pure "omit".** Every edit is a replacement node or a projection. To omit an entry, write a replacement placeholder that cites it.

## 7. Listing, naming, metadata and cleanup

- `stat`/`list` read only headers, never bodies. A snapshot holds `header`, `formatStatus`, an opaque `revision` change token, `eventCount` and `sizeBytes` when they are cheap to get, and `location` (`persistence.md:302-339`). In SQLite, the revision is `<tail_segment>:<event_count>` (`sqlite.ts:832-837`). Listing skips rows with a corrupt or unsupported header, but a direct open still refuses them (`sqlite.ts:863-901`).
- Order: session-query sorts by `createdAt` descending, then by id (`packages/session-query/session-query/src/corpus.ts:313-315`). Activity ordering ignores `session/end-seed` (`session.md:776`).
- Names: the log-only `session/title` event is latest-wins and never goes to the model. Its sources are the first eligible prompt, an optional LLM provider, or a user rename. A fallback has limits on word count and UTF-8 bytes (`session-title/src/index.ts:56-61,78,287`).
- A projection cache stores checkpoints, so that lists and stats do not load full logs. It can be stale, but never ahead of the log (`session-projection-cache/README.md`, Summary).
- **No deletion or retention.** "No deletion or retention API" (`session-persistence/README.md:155`). The SQLite triggers forbid `DELETE` on frames (`sqlite.ts:288-289`).

## 8. Tests that prove these behaviors

The shared contract suite runs against both backends (`session-persistence/tests/contract.ts`, `live-write-contract.ts`; called from `jsonl.spec.ts` and `sqlite/tests/persistence.spec.ts`).

| Test (file:line) | Assertion |
|---|---|
| `contract.ts:203` concurrent duplicate creates | One create wins. The other gets `SessionAlreadyExistsError`. |
| `contract.ts:252` write ownership is single-holder | A second write open is rejected, and close releases ownership. |
| `contract.ts:345,376,391` created-but-unappended | The session is visible only in this instance. Close erases it. Flush materializes it. |
| `contract.ts:415` freshness | A read started after an append resolves sees that prefix. |
| `contract.ts:439,462` continuation and contiguity | A fresh instance continues at the next seq. A gap is rejected, and the error names the expected seq. |
| `contract.ts:504` vocabulary fail-closed | An unknown stored type refuses read opens and write opens. |
| `contract.ts:540` torn tail | Readers get only the committed prefix. The first write truncates the tail, and the repair survives a reopen. |
| `live-write-contract.ts:56,118,263` | Batching within one window. Flush shows a background failure that was kept. A failed drain keeps order and recovers once. |
| `jsonl.spec.ts:1795` | A failed append truncates its partial bytes, so a retry makes no seq gap. |
| `jsonl.spec.ts:2208,2404` | A corrupt row is tolerated only when no later `turn/end` comes after it. |
| `lease.two-process.e2e.ts:35` | A live holder process excludes a writer, reads still work, and takeover is immediate after SIGKILL. |
| `jsonl/tests/lease.spec.ts:159,228,407` | A second instance is excluded and then admitted after close. A create that is never materialized leaves no footprint. Release never removes the lock file. |
| `sqlite/.../normal-fork-concurrency-validation.spec.ts:328` | A reader in another store sees the live writer's appends. Write ownership moves to the other store after close. |
| `...:349` | Of two overlapping fork creations, exactly one wins. |
| `sqlite/tests/unknown-outcome.spec.ts:17` | After an unknown COMMIT or ROLLBACK result, every handle is isolated and still released. |
| `sqlite/tests/recovery-regressions.spec.ts:74,122` | The same append retries after SQLite rolls back by itself (SQLITE_FULL). |
| `sqlite/tests/legacy-open.two-process.e2e.ts:78` | Two processes start on a fresh root at the same time and open different sessions. Both open, the database initializes once, and both sessions are listed and readable. |
| `sqlite/tests/legacy-open.two-process.e2e.ts:96,120` | A second process waits for another process's import and then hands off. After a crash during import, the import retries in full. |
| `session-checkpoint-policy/tests/crash-recovery.e2e.ts:93` | A hard crash after dispatch: the request is durable and the turn is closed `interrupted`. |
| `...crash-recovery.e2e.ts:106` | Tool intent is durable before the side effect. The missing result becomes `TOOL_OUTCOME_UNKNOWN`. |

## 9. What to adopt and what to avoid (AskCore: Go, SQLite via GORM, leader plus `ask -p`)

### Adopt (ranked)

1. **Seq contiguity checked inside the write transaction.** Store `seq` as `PRIMARY KEY(session_id, seq)` and check `max(seq)+1` inside the same `BEGIN IMMEDIATE` transaction (`sqlite.ts:772-775`). This is free fencing by log position, and it fits the AskCore rule "lease checks must share the write transaction" (AskCore: `internal/sessions/README.md`). Put the generation check (if you keep it) in the same statement group.
2. **Flush checkpoints at side-effect boundaries, not on every event.** Flush before the provider call, before a tool body, and at pre-step, and fail closed (`session-checkpoint-policy/src/index.ts:62-82`). For AskCore, a simpler variant is to commit each atomic append (which is already the sole-writer unit) synchronously and drop write-behind. SQLite WAL with `synchronous=FULL` is cheap enough at agent rates.
3. **Repair at resume, never truncate history.** Append synthetic closers under write ownership. Separate *intent recorded, outcome unknown* from *not started* (`repair.ts:15-35`). AskCore already records tool intent (AskCore: `internal/sessions/README.md`), and this maps to it directly.
4. **Model selection restore rule.** Use pending selection, then last used request header, then config (`model-selection.ts:73-121`). This matches the AskCore H8 test "Resume restores the qualified provider/model ... effective thinking level". Keep tools and the system prompt from the current composition, and log the difference as an entry.
5. **Context edit as a replace with cited sources, never a delete.** A `replace{start,end}` with full `sourceEventSeqs` coverage, plus a transcript built from append-origin entries (`surface.ts:78-95,361-363`). This fits H-COMPACT-13 `context_edit`. To omit an entry, use a placeholder replacement that cites it.
6. **Fail-closed vocabulary with `ignorable`, and a separate "newer writer" error.** (`storage-contract.ts:73-80`.) A small cost that prevents silent bad resumes after a downgrade.
7. **One shared contract suite for every store implementation** (`contract.ts`). In Go: one `sessionstoretest.Run(t, factory)` for the in-memory store and the GORM store.
8. **`application_id` and `user_version` guard; refuse, never reset** (`sqlite.ts:218-227`). With golang-migrate, also refuse a database whose version is newer than the binary.

### The ownership decision (do not change the plan silently)

- **AskCore H8 plan (user-confirmed roadmap):** a SQLite lease row with explicit acquire, renew and release, an ownership generation checked in every write transaction, and a stopped run on loss. The H8 test list includes "a paused writer whose lease expired cannot write after a new owner took the lease".
- **DeepSeek:** a kernel `flock` with no expiry. It needs no renew loop and gives immediate takeover after a crash, but a stuck holder blocks until it exits (`lease.ts:1-28`).
- **Trade-off matrix:**

| Option | Renew loop | Crash takeover | Stuck live holder | Effect on H8 as written |
|---|---|---|---|---|
| (a) Lease row with expiry and generation (the plan) | Yes (goroutine; expiry and clock choice) | After expiry | Expires; generation fences its late writes | Keeps H8 as written, including the "lease expired" test |
| (b) `flock` plus a generation column bumped on each acquire and checked in each write transaction | No | Immediate (the kernel drops the lock) | Blocks until the process exits. The generation fences only a forced takeover (lock file removed, `jsonl/tests/lease.spec.ts:194`, or an explicit "steal") | **Changes H8.** There is no expiry, so the "lease expired" test becomes a "forced takeover" test |
| (c) `flock` only (DeepSeek) | No | Immediate | Blocks until the process exits; nothing fences it if the lock file is removed | **Changes H8.** Drops expiry and generation |

  - `flock` is trivial in Go (`golang.org/x/sys/unix.Flock`; DeepSeek needs a native Node addon). It is advisory, and it does not work on NFS.
  - Under `flock`, a generation guards against only one thing: a second owner that appears while the first one is still alive, because the lock file was removed or a takeover was forced. Without such a path the generation is idle. That is why (b) makes sense only if AskCore wants a forced takeover command.
- **This needs a user decision.** Only (a) keeps the confirmed H8 contract. If the user accepts a change, I recommend (b): both writers are local processes on one machine, and (b) removes the renew loop but keeps a fencing path. In every case, copy the DeepSeek behavior for the second writer: **reject at once with a typed "already owned" error, and do not wait.** For `ask -p --session X` while the leader holds X, this gives a clear exit code instead of a hang (`lease.two-process.e2e.ts:52`).

### Avoid (ranked by cost)

1. **The multi-generation file migration machinery.** It has about 19 k lines of format packages and tests (counted with `wc -l`) across v0→v4 codecs, Worker verification and corpus fingerprints (JSONL README, "Reading the logs"). With one SQLite store, golang-migrate plus `ignorable` covers the need.
2. **Two backends plus routing records** (a JSONL-to-SQLite import and handoff with double locks, `sqlite.ts:601-630`; `migration.ts`). AskCore has no legacy files. Use one store.
3. **Frames as JSON-line blobs (64 events, zstd).** These are not queryable and not GORM-friendly. Store one row for each entry, with a JSON payload column.
4. **Write-behind with `append` resolving before durability.** It needs checkpoint plugins, kept failures and a paused drain state (`live-write-contract.ts:118,263`). AskCore's atomic append makes synchronous commit simpler.
5. **Store-wide poisoning on an unknown COMMIT outcome** (`sqlite.ts:86-88`). This is correct, but plan it as "stop the run and reopen", not as a whole-daemon outage in the leader.
6. **Lazy materialization with pending in-memory sessions.** It adds pending branches to `stat`, `list`, `open` and `create` (`sqlite.ts:453-483,804-830`). Write the session header row at create.
7. **Lock-file residue.** Lock files are never removed (`lease.ts:120-124`). If you use `flock`, put the lock files under one `ASK_HOME/locks/` directory.

### Gaps compared with AskCore H8

- **Entry tree.** DeepSeek has no in-session tree. Its branch model is fork-to-new-session with a shared prefix, using a segment ancestry with `parent_end_seq` (`sqlite.ts:271-313`). If AskCore keeps an in-session id/parentId tree (Pi style), DeepSeek gives no evidence for that part.
- **Migrations racing at startup.** DeepSeek only creates the schema inside one IMMEDIATE transaction with a WAL retry (`sqlite.ts:207-227`). A two-process test shows that the database initializes once (`legacy-open.two-process.e2e.ts:78`). But the repository has no general versioned migration runner to copy for the H8 test "migrations run once".

## Unresolved questions

1. Ownership model: (a), (b) or (c) in topic 9? This needs the user, because it touches a confirmed H8 lease contract.
2. Does AskCore keep an in-session entry tree, or does it adopt fork-to-new-session? This affects the schema (`parent_id` column compared with a segment/prefix table).
3. Should AskCore keep write-behind batching at all, or commit each atomic append synchronously? The answer depends on measured append rates during streaming.
4. Not verified: whether the raw-mode tolerance rule (a corrupt row is dropped as a crash tail unless a later `turn/end` follows it, `format.ts:478-521`) applies the same way inside zstd frames, and how the Windows lock behaves.
