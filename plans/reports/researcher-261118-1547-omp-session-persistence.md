# Research: oh-my-pi (omp) session persistence, for Ask H8 "session log"

Date: 2026-11-18. Read-only source study. No source script was run.

## Source manifest

| Source | Path | Commit | License |
|---|---|---|---|
| oh-my-pi (omp) | `/Users/dale/Desktop/workspace/opensources/oh-my-pi` | `eb6132a38b0a16a7220071589b692f5df1fa9055` (2026-11-18), clean tree except one generated file | MIT (`LICENSE:1`) |
| Pi (upstream) | `/Users/dale/Desktop/workspace/opensources/pi` | `4c6fb7cfe8c538a668726f6f8b3554098c39faee` (2026-10-03) | MIT (`LICENSE:1`) |

Path prefixes used below: `S/` = `oh-my-pi/packages/coding-agent/src/session/`, `C/` = `oh-my-pi/packages/coding-agent/src/`, `T/` = `oh-my-pi/packages/coding-agent/test/`, `D/` = `oh-my-pi/docs/`, `P/` = `pi/packages/coding-agent/src/core/`.

Evidence limits:
- All evidence comes from `rg` and direct reads of the local path. The GitNexus index was not used (it is an older copy).
- Line numbers hold for the commits above only. omp moves fast: `session-manager.ts` plus `session-storage.ts` have at least 375 commits since 2026-05-18 (`git log` without `--follow`, so this is a lower bound).
- omp docs sometimes describe dead code. Claims tagged "doc only" were not confirmed in code.
- No test was run. Test names are cited as evidence of intent, not as proof that they pass.
- `packages/agent`, `packages/mnemopi` and `packages/snapcompact` hold no session store. mnemopi is a separate SQLite memory engine (`packages/mnemopi/README.md:3`). snapcompact renders discarded history to PNG frames; the frames persist inside `compaction.preserveData` (`S/session-persistence.ts:18-19`).

## Outcome first

omp keeps Pi's model (an append-only JSONL tree with id/parentId, header v3, lazy creation, leaf from last line), and adds a large durability layer around one mutable file: a cross-process ownership lease, a publish lock, byte-size compare-and-swap on full rewrites, foreign-entry adoption, and "move to a sibling file" when another process owns the session. Most of that layer exists because the store is one mutable file that is sometimes rewritten in full. A store with one row per entry and a transaction per append does not need it. One important divergence: upstream Pi has an append-only `context_edit` entry (omit or replace one entry for future context only); omp has none (zero `rg` hits for `context_edit|contextEdit` in omp `src`). For Ask H-COMPACT-13, Pi is the model, not omp.

Ranked recommendation for Ask H8 (details in section 9):
1. Adopt the entry model: closed entry types, id/parentId tree, state changes (model, thinking level, mode) as entries, a context builder that walks leaf to root and derives state, and Pi's `context_edit` (not an omp feature) instead of deleting or blanking rows.
2. Adopt the policies: claim on first write and not on open; key the lease by session id, not by path; the lazy creation gate; a synthetic abort for an interrupted tail at resume; persist the active leaf explicitly.
3. Do not copy the storage layer (title slot, publish lock, size CAS, sibling move, continuation chains, whole-transcript SQL cell). Keep the roadmap decision: a SQLite lease row with a generation, checked in each write transaction.

## 1. Storage model

- **Format.** JSONL, one JSON object per line. Current files start with a fixed-width 256-byte `type:"title"` slot, then the header, then entries (`D/session.md:63-69`; `S/session-entries.ts:21-36`). The slot lets a rename change the title in place without a full rewrite (`D/session.md:378`). Pi has no slot.
- **Location.** `~/.omp/agent/sessions/<encoded-cwd>/<timestamp>_<sessionId>.jsonl` (`D/session.md:41-45`). The cwd encoding is in `S/session-paths.ts:46-55` and `S/session-paths.ts:222-224`. Pi uses `~/.pi/agent/sessions/--<path>--/<timestamp>_<id>.jsonl` (`pi/packages/coding-agent/docs/session-format.md:11`). omp also carries a migration for a reverted hashed directory scheme (`S/session-paths.ts:57-62`), which is a warning about how costly a directory-layout change is.
- **Header.** `SessionHeader` at `S/session-entries.ts:42-75`: `type:"session"`, `version`, `id` (UUIDv7 for new sessions, `D/session.md:92`), `timestamp`, `cwd`, optional `additionalDirectories`, `parentSession`, `continuationOf`, `previousSessionFiles`, and two prompt-cache keys.
- **Version.** `CURRENT_SESSION_VERSION = 3` (`S/session-entries.ts:17`), the same as Pi (`P/session-manager.ts:41`).
- **Migration.** `S/session-migrations.ts:14-41` (v1 to v2: add id/parentId as a linear chain, `firstKeptEntryIndex` to `firstKeptEntryId`), `:44-58` (v2 to v3: role `hookMessage` to `custom`), `:64-76` (run on load; also re-indexes a file whose entries have no ids). A migration does not write at once. It sets `#rewriteRequired` (`S/session-manager.ts:2488`), and the next write does a full rewrite.
- **Optional SQL and Redis backends.** `S/sql-session-storage.ts` exists, but it emulates a file system: one row per session path with the whole JSONL in a `content TEXT` column (`S/sql-session-storage.ts:184-192`), and an append is `content = content || excluded.content` (`:210-213`). It is SDK-only: the only users are `C/index.ts`, `packages/coding-agent/examples/sdk/13-sql-sessions.ts`, and tests. The CLI uses `FileSessionStorage`.

## 2. Entry model

- **Base.** Each entry has `type`, `id`, `parentId` (null for a root), `timestamp`, optional `runtime` (writer build), and optional `activeLeafId` (`S/session-entries.ts:88-106`). Ids are 8 hex chars with a collision check, and a Snowflake id is the fallback (`S/session-migrations.ts:5-11`).
- **Types.** The union is at `S/session-entries.ts:350-366`: `message`, `model_usage`, `thinking_level_change`, `model_change`, `service_tier_change`, `compaction`, `branch_summary`, `custom`, `custom_message`, `label`, `title_change`, `ttsr_injection`, `session_init`, `mode_change`, `credential_pin`, `reset_boundary`. Pi has 11 types (union at `P/session-manager.ts:183-194`), including `usage`, `context_edit` (`:175-180`), and `session_info` for the name (`:143-144`). omp has no `context_edit`. omp replaces `session_info` with the title slot plus a `title_change` audit entry.
- **What is persisted.**
  - Messages: the full `AgentMessage`, including usage and provider replay data (`D/session.md:141-175`).
  - Model change: `"provider/modelId"` plus role (`S/session-entries.ts:143-151`). Thinking level: resolved and configured values (`:132-141`).
  - Usage outside the transcript: `model_usage` (`:114-130`).
  - Compaction: summary, `firstKeptEntryId`, `tokensBefore`, method, provider-native replay data in `preserveData` (`S/session-entries.ts:158-183`).
  - Custom (not in model context; `S/session-entries.ts:216-220`) and custom_message (in model context; `:339-347`). Core reserves some custom types, for example `session_exit`, `tool_execution_start`, `system-prompt-digest` (`D/session.md:328-341`).
  - Labels: `label` with `targetId`; an undefined label clears it (`S/session-entries.ts:223-227`).
- **System prompt and tools snapshot.** `session_init` stores the system prompt blocks, task, and tool names (`S/session-entries.ts:272-311`). Only subagent sessions write it (`C/task/executor.ts:5384`). For a main session, `#recordModelCallSystemPrompt` returns at `S/agent-session.ts:7641` when no `session_init` exists. Main sessions store only a hash of the base prompt as the custom entry `system-prompt-digest` (`S/session-tools.ts:303`, appended at `:1815`). The tool list of a main session is not persisted.
- **Branching.** `branch(id)` moves only the in-memory leaf (`S/session-manager.ts:4072-4075`). `resetLeaf()` sets the leaf to null (`:4078-4080`). There is no persisted leaf. On load, the leaf is the last physical entry, or its `activeLeafId` (`S/session-entries.ts:369-394`). A pointer-only branch is lost on reload until the next append (`D/session.md:506`). omp patched this with `activeLeafId` for off-branch writes and with `discardEntryDurably` (section 6).

## 3. Write path

- **Writer.** `SessionManager` is the only writer. Every `append*` goes through `#recordEntry` to `#appendToSessionFile` (`S/session-manager.ts:1889-1932`).
- **Lazy creation.** A new session is not written until it holds an assistant message, `ensureOnDisk()` was called, or the file is already current (`S/session-manager.ts:1668-1670`, gate at `:1905-1911`). Crossing the gate writes the header and all buffered entries in one rewrite. `newSession()` calls `ensureOnDisk()` (`:2513`) so that `/new` is a durable boundary, except for the `/delete` successor. ACP `session/new` uses `ensureOnDisk()` too (`:2789-2798`). Pi's gate is "has a user or assistant message" (`P/session-manager.ts:1165-1188`).
- **Hot path.** One synchronous `writeSync` of a newline-terminated line on an `O_APPEND` descriptor (`S/session-manager.ts:1960-1974`; `S/session-storage.ts:458-479`). If a write fails part-way, the writer truncates the file back to its size before the write (`S/session-storage.ts:416-430`).
- **No fsync.** This is stated in the interface (`S/session-storage.ts:68-76`, `:87-89`). A completed append survives a process crash, not a power loss.
- **Atomic full rewrites.** A full rewrite stages a temp file `.<name>.<snowflake>.tmp` (`S/session-storage.ts:33-37`). Then, with no await in between, it checks the expected byte size and renames the temp file over the target (`:1131-1178`; size check `:726-737`). This happens on migration, after malformed lines, on prune, move, fork, and recovery.
- **Errors do not stop the turn.** An append failure sets a latched `#diskFailure`. The next append retries with a full rewrite of the in-memory journal (`S/session-manager.ts:1897-1903`). `flush()` and `close()` report the failure (`:2911-2924`).
- **Crash handling on load.**
  - Malformed lines are skipped and counted (`S/session-loader.ts:166-181`).
  - A truncated last record at end of input counts as malformed (`S/session-loader.ts:340-346`).
  - Any malformed count sets `#rewriteRequired` (`S/session-manager.ts:2488`). The next write therefore rewrites the file without those lines, so the corrupt bytes are dropped for good.
  - An invalid header fails the resume and leaves the file unchanged (`S/session-manager.ts:2427-2431`).
  - Pi instead appends a `"\n"` when the last line has none (`P/session-manager.ts:668`), so the next append does not join a partial line. I found no omp equivalent for a complete last record with no newline (see unresolved questions).
- **Exit record.** At disposal, omp appends `custom` `session_exit` and calls `flushSync()` (`S/agent-session.ts:4092-4093`). At resume, an interrupted tail gets synthetic tool results and an aborted assistant message (`C/sdk.ts:2281-2286`).
- **Size controls.** Strings over 500,000 chars are truncated (`S/session-persistence.ts:13-14`), except signed provider blocks. Images of 1,024 chars or more go to a content-addressed blob store (`S/session-persistence.ts:16`; `D/session.md:576-585`).

## 4. Concurrency across processes

- **Ownership lease, keyed by session id.** The lease is an OS lock: a `flock` sidecar on macOS, an abstract socket on Linux, a named mutex on Windows, under `~/.omp/run/session-owners/<id>` (`S/session-storage.ts:600-632`; `D/session.md:558`). The kernel releases it when the process exits, so a crashed owner leaves no stale lease.
- **Claim on first write, not on open.** See `S/session-manager.ts:1151-1169`. Inspection commands never take the lease.
- **Second writer policy: move, not fail.** A process that finds the lease held does not write the file. Its first write moves the session to a sibling `<ts>_<newId>.jsonl` with `parentSession` and `continuationOf` set to the old id, and copies artifacts in the background (`S/session-manager.ts:1305-1358`, trigger at `:1704`). Later, `--resume` and `--continue` of the old file follow the continuation (`C/main.ts:1327-1329`; `S/session-listing.ts:892-900`).
- **Publish lock.** A per-file OS gate plus a `.<name>.lock` file serializes an append against a concurrent rewrite's check-and-rename (`S/session-storage.ts:740-768`, `withPublishLock` at `:750`; append side `:458-479`). Lock contention raises `SessionLockError`.
- **Writers without the lease** (older omp, external tools). A size mismatch raises `SessionWriteConflictError`. The manager reads the file back, adopts the unknown entries as a side branch, and retries up to 3 times (`S/session-manager.ts:148`, `:1210-1278`). After that it moves to a sibling with reason `contested` (`:1263`).
- **Different home directories do not share a lease** (`D/session.md:558`).

## 5. Resume

- **Flags.** `-c`/`--continue` (`C/cli/args.ts:360`). `--resume`, `-r`, and `--session` are aliases with an optional value (`C/cli/flag-tables.ts:304-307`). A bare `--resume` opens the picker (`C/main.ts:2300-2323`). `-c <uuid>` becomes `--resume <uuid>` (`C/main.ts:1234-1251`). In Pi, `--session` takes a required path and is separate from `--resume`/`-r` (`pi/packages/coding-agent/src/cli/args.ts:110-137`).
- **Resolution.** See `createSessionManager` (`C/main.ts:1272-1402`). A path value opens the file. An id value resolves by id prefix, file-name prefix, or id suffix, first locally and then globally (`D/session.md:637`).
- **`-c` logic.** `continueRecent` (`S/session-manager.ts:4565-4693`) first uses a per-terminal breadcrumb file. Without one, it uses the newest non-empty session in the cwd directory (`S/session-listing.ts:872-881`). It can also re-root a moved project when the directory inode matches.
- **Context rebuild.** `buildSessionContext` (`S/session-context.ts:270-`):
  1. Resolve the leaf (`:285-311`).
  2. Walk parentId to the root with cycle protection (`:313-323`).
  3. Derive the thinking level, service tier, model map, and mode from the path (`:325-400`).
  4. Choose the boundary: the later of reset boundary or compaction (`:411`, `:539-559`). A compaction emits its summary, then the kept entries from `firstKeptEntryId` (`:559-620`).
  5. Drop dangling tool calls and retry or error turns (`:476-482`; `D/session.md:532`).
- **What is restored.**
  - Model: from `model_change` entries, unless the caller gives a model. A model that cannot be found gives a warning, `Could not restore model …` (`C/sdk.ts:2343-2366`).
  - Thinking level: from the latest `thinking_level_change` (`C/sdk.ts:2390-2411`).
  - Also service tier, mode, TTSR rules (`C/sdk.ts:2494`), credential pin, and todo state through custom entries.
- **What is not restored.** For a main session, the tool list and the system prompt text are not stored. The resumed process builds its base prompt from its current tools. The `system-prompt-digest` entry only decides if a rebuilt prompt may commit, and only for models that bind signed thinking to the prompt prefix; without a recorded digest the prompt stays frozen (`S/session-tools.ts:1825-1843`, digest append `:1812-1817`). Token counters are not restored as a snapshot; usage lives on messages.

## 6. Context edits and compaction

omp has no per-entry "omit from context, keep in history" entry: `rg -l -i "context_edit|contextEdit"` finds nothing in omp `src`. Upstream Pi has one: `ContextEditEntry {targetId, replacement: {content} | null}` (`P/session-manager.ts:167-180`). Pi's projection applies the latest edit for each target (`P/session-manager.ts:551-565`), and Pi's overflow recovery appends `context_edit(targetId, null)` (`pi/packages/coding-agent/src/core/agent-session.ts:1217-1222`). Ask's H-COMPACT-13 row already cites Pi for this (`plans/260930-2254-pi-feature-inventory-go-roadmap/inventory-harness.md:175`).

Mechanisms in omp that keep history:
- **`/clear`** appends a payload-free `reset_boundary` (`S/session-manager.ts:3776-3780`). Model context starts after it, and full history stays on disk (`S/session-context.ts:539-558`). It is all-or-nothing.
- **Compaction** is an entry. Older entries stay in the file. The builder emits the summary plus the kept tail from `firstKeptEntryId` (`S/session-context.ts:559-620`). The full transcript view shows compactions inline (`:498-532`).
- **Branching.** `branch()` leaves the abandoned path in the file (`S/session-manager.ts:4072-4075`).
- **Builder filters.** The builder skips retry-recovery and empty-error assistant turns in model context only (`S/session-context.ts:476-482`).

Mechanisms in omp that destroy history:
- **`discardEntryDurably`** (`S/session-manager.ts:4096-4136`). When all children are metadata, it removes the entry from the journal, keeps its usage as an off-branch `model_usage`, appends a marker `branch_summary`, and does a full rewrite. When a child may carry content, the subtree stays off-branch instead (`:4084-4094`).
- **Prune and image drop** blank tool results in place and call `rewriteEntries()` (`S/session-maintenance.ts:764-770`, `:931`; `S/session-manager.ts:3788-3796`). The original bytes are gone.

## 7. Listing, naming, cwd scoping, cleanup

- **Listing.** Listing is scoped to the cwd directory. It reads a 4 KiB prefix plus a bounded 32 KiB tail per file, cached by stat (`D/session.md:624-637`). The picker hides empty stubs (`S/session-listing.ts:725-736`). Linked git worktrees are listed together (`S/session-paths.ts:226-240`).
- **Naming.** The title slot is the current title. `title_change` entries are the audit trail. `titleSource:"user"` blocks automatic renames (`D/session.md:103`, `:376-380`). A SQLite index `history.db/session_titles` speeds up listing (`S/session-index.ts:36-49`).
- **Cleanup.** `close()` deletes a file that holds no user or assistant messages (`T/session-manager/close-drop-empty.test.ts:42`). `omp gc` previews by default and needs `--apply`. It sweeps blobs, archives cold sessions to `.jsonl.gz`, removes stale temp files, and can optionally compress or strip provider replay data (`D/session.md:599-618`; doc only for the phase details, with `C/cli/gc-cli.ts` as the owner).

## 8. Tests that prove these behaviors

| Behavior | Test (file:line) |
|---|---|
| v1 migration adds id/parentId; idempotent | `T/session-manager/migration.test.ts:6`, `:44` |
| Malformed lines skipped, valid ones kept | `T/session-manager/file-operations.test.ts:46` |
| Tree parent chain, leaf advance | `T/session-manager/tree-traversal.test.ts:7`, `:50` |
| Context: thinking level, model, compaction | `T/session-manager/build-context.test.ts:150`, `:161`, `:260`, `:742` |
| Dangling tool call stripped | `T/session/session-context.test.ts:133` |
| Lazy gate / ensureOnDisk | `T/session-manager-on-disk-8860.test.ts:20`, `:28` |
| Empty session dropped at close | `T/session-manager/close-drop-empty.test.ts:42`, `:298` |
| `-c` breadcrumb and `/new` boundary | `T/session-manager/new-session-boundary.test.ts:39`, `:66`, `:130` |
| First writer keeps file; second moves to sibling | `T/session/session-manager-shared-session-file.test.ts:153` |
| Opener is not owner; crash hands over lease | same file `:195`, `:208` |
| Lease probe without claim | `T/session/session-lease-probe.test.ts:95`, `:118` |
| Resume follows continuation | `T/session/resume-follows-continuation.test.ts:319`, `:437` |
| Racing writer without lease ends in sibling | `T/session-manager-atomic-rewrite-race.test.ts:430` |
| Exit record durable during atomic rewrite | same file `:272`; `T/session/session-manager-dispose-recovers-write-race.test.ts:146` |
| Off-branch late result keeps active branch | `T/session/off-branch-leaf-replay.test.ts:47` |
| Prune rewrite matches from-disk rebuild | `T/agent-session-prune-persistence.test.ts:149` |
| Publish lock gate | `T/session/publish-lock-os-gate.test.ts:41`, `:75` |
| Resume by session id prefix | `T/session-manager/file-operations.test.ts:134` |

## 9. Adopt, avoid, and fit for Ask (SQLite via GORM, leader plus `ask -p`)

The Ask constraints come from the roadmap H8 section (`plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md:364-394`) and `internal/sessions/README.md`. That section already decided on a SQLite lease row with an ownership generation, a check in every write transaction, and that the losing process stops the run. This report keeps that decision. omp's policy is shown below as a contrast, not as a replacement.

### Trade-off matrix: what drives omp's complexity

| omp mechanism | Why omp needs it | Needed with one row per entry in SQLite? |
|---|---|---|
| Lazy gate (assistant message) | Avoid empty files | Yes, as a policy: do not insert the session row until the first assistant entry, or until an explicit "create" (ACP `session/new`) |
| Title slot (256 bytes) | Rename without file rewrite | No: `UPDATE sessions SET title` |
| Publish lock + byte-size CAS | Append and full rewrite race on one file | No: each append is one transaction |
| Full rewrite after migration or malformed lines | Fix a mutable text file | No: schema migrations with golang-migrate; a row cannot be half-written |
| Foreign-entry adoption (3 retries) | Writers without the lease | No, if every writer goes through the lease check in the transaction |
| Move to sibling + `continuationOf` + resume-follows-continuation | Never block a second writer | Policy choice. The roadmap chose "stop the run". Cost of omp's way: continuation chains, artifact copy, a second id for one conversation, and picker logic (`S/session-listing.ts:892-900`, a 755-line test file `T/session/resume-follows-continuation.test.ts`) |
| OS lease, kernel-released | Crashed owner must not block | Partly. A lease row needs an expiry plus a generation instead, because SQLite has no process-bound lock. The roadmap covers this |
| Leaf derived from last line | No separate leaf field in a log | No: store `active_leaf_id` on the session row in the same transaction |
| In-place prune/blank + rewrite | Free context | Avoid: use Pi's append-only `context_edit` (roadmap H-COMPACT-13) so history stays intact |

### Adopt (ranked)

1. **Entry tree plus a context builder that derives state.** Model, thinking level and mode are entries. The builder walks leaf to root with cycle protection and picks the later of reset boundary or compaction (`S/session-context.ts:313-323`, `:539-559`). This matches Ask's "projection of the log" rule.
2. **Claim on first write, not on open; key the lease by session id.** Read-only commands (`ask settings`, export, a future dashboard) must never take or block the lease (`S/session-manager.ts:1151-1169`).
3. **Interrupted-tail repair at resume.** Write synthetic tool results plus an aborted assistant entry when the last turn did not end (`C/sdk.ts:2281-2286`), and write an exit record at shutdown (`S/agent-session.ts:4092-4093`). Ask already records tool intent, so it can tell intent from outcome.
4. **Lazy creation gate plus explicit materialize.** Use one gate predicate (`S/session-manager.ts:1668-1670`) and a forced path for ACP and `/new`.
5. **Resume rules.** An explicit flag beats the session's model. When the model is unknown, keep going and warn (`C/sdk.ts:2343-2366`). Restore the configured thinking selector, not only the resolved level. Never restore a token snapshot (this matches the roadmap test at line 389).
6. **Store reasons, not only results.** For example, `model_usage` for calls outside the transcript, and the usage of a discarded turn that was still billed.

### Avoid

- **Whole-transcript-in-one-cell SQL** (`S/sql-session-storage.ts:184-213`). It is a counter-example for GORM: O(n) appends, no per-entry query, and size CAS on a TEXT length.
- **Destructive in-place edits** (prune, `discardEntryDurably`). They break "history kept".
- **A leaf that is not persisted.** omp had to add `activeLeafId` and durable discard markers to fix this.
- **Silent loss of corrupt lines** on the next rewrite (`S/session-manager.ts:2488`). With SQLite this risk goes away. If Ask ever imports Pi or omp JSONL, keep the skipped lines in a quarantine.
- **Path-encoded directory names as the cwd key.** omp needed two directory migrations (`S/session-paths.ts:57-62`; `D/session.md:47`). Store the canonical cwd as a column and index it.
- **The size of the layer.** 4,839 lines in `session-manager.ts` plus 1,803 in `session-storage.ts`, and at least 375 commits in 6 months. Most of the volume is file-concurrency repair.

### Adoption risk

| Item | Risk |
|---|---|
| Entry model, builder, resume rules | Low. They are stable since Pi v3, and omp kept version 3 |
| Sibling-move policy | High. It is omp-only, new, and has a lot of churn. It conflicts with the roadmap's stop-the-run decision |
| omp storage layer | High. It depends on the file model and has a lot of churn |

## Limitations

- Exact run-time behavior was not checked, because no test was run. The analysis of race windows comes from comments and code reading.
- The details of the `omp gc` phases, the Redis and indexed backends, and collab replicas were read only from docs or skipped.
- The Pi comparison covers `session-manager.ts`, `session-format.md`, and the CLI args at the commit above, not Pi's newer releases.

## Unresolved questions

1. Does omp repair a complete last record that has no trailing newline before it appends? I found no code for it (`S/session-storage.ts:416-430` only rolls back a partial write). Pi does repair it (`P/session-manager.ts:668`). This matters only if Ask imports JSONL.
2. Ask policy for a second writer: the roadmap already decided that the loser stops the run (`roadmap.md:376-379`). omp gives a tested contrast (move to a linked sibling), with the cost shown in the matrix. Change the decision only if the user wants `ask -p` to go on when the leader owns the session.
3. Exact-prompt resume of a main session: the roadmap wants system snapshots and tool declarations persisted (`roadmap.md:383`). omp gives no model for this. It stores only a digest for main sessions. Its subagent `session_init` (`S/session-entries.ts:272-311`) is the nearest example of a stored prompt and tool list.
