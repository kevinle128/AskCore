# Xia (improve): H8 session log, lessons from DeepSeek Harness and omp

Date: 2026-11-18. Mode: `--improve`. Status: challenge phase; the decisions in section 5 are open until the user answers.

## 1. Source manifest

| Source | Path | Commit | License | Report |
|---|---|---|---|---|
| DeepSeek Harness | `/Users/dale/Desktop/workspace/opensources/deepseek-harness` | `ca33fd6843b2` (2026-11-17) | MIT | [researcher-261118-1547-deepseek-session-persistence.md](researcher-261118-1547-deepseek-session-persistence.md) |
| oh-my-pi (omp) | `/Users/dale/Desktop/workspace/opensources/oh-my-pi` | `eb6132a38b0a` (2026-11-18) | MIT | [researcher-261118-1547-omp-session-persistence.md](researcher-261118-1547-omp-session-persistence.md) |
| Pi (upstream, contrast) | `/Users/dale/Desktop/workspace/opensources/pi` | `4c6fb7cfe8c5` | MIT | inside the omp report |
| Ask (local) | this worktree | `734773c` | - | scout summary in section 3 |

Evidence limits: the agents read source and test assertions. They ran no source script and no source test. The omp GitNexus index is from August 2026, so omp evidence comes from direct reads.

## 2. What the two sources do

| Topic | DeepSeek Harness | omp | Ask today |
|---|---|---|---|
| Unit of storage | One append-only event log per session, gap-free `seq` | One JSONL file per session, header v3, id/parentId tree | In-memory slice of 12 closed entry types (`internal/sessions/entry.go:200-211`) |
| Backend | JSONL (default) or SQLite (`segments`, `traces`, `frames`; WAL, `synchronous=FULL`, 5 s busy timeout) | JSONL; an optional SQL backend keeps the whole file in one TEXT cell | None; server-only SQLite with no PRAGMA (`internal/store/gormstore/db.go:12-14`) |
| Tree | None inside a session; a branch is a fork into a new session | id/parentId tree; active leaf not stored | Integer positions (`ToolCall.AssistantEntry`, `CommitRef`) |
| Write durability | Batches every 200 ms, plus a fail-closed flush before each model call and each tool | Synchronous `O_APPEND`, no fsync | `Append` is atomic in memory; an error ends the run (`internal/agent/loop_run.go:239-244`) |
| Second writer | Rejected at once (`SessionAlreadyOwnedError`); `flock`, no expiry, takeover after a crash | OS lease per session id; the loser moves to a linked new file | Not handled |
| Resume | Takes the lock, reads, appends closing events (`TOOL_OUTCOME_UNKNOWN`, `TOOL_NOT_STARTED`); model, provider, thinking from the log; tools and prompt from current setup, difference logged | Restores model (warning if missing), thinking, tier, mode; synthetic tool results plus an aborted message for an interrupted turn | No resume; no entry records a model or thinking change (`internal/agent/agent.go:408,498`) |
| Context edit | Append a "replace" event that names the hidden entries | Prune or discard in place (history lost); Pi has append-only `context_edit` | None |
| Session creation | At start | Lazy: on the first assistant message; ACP and `/new` create at once | Random id per run (`cmd/tui/headless.go:563-569`) |
| Selection | Headless needs `--session-id`; no "latest" option | `-c` latest in cwd, `--session`, `--resume` picker, cwd-scoped folder | No flags |

## 3. Dependency matrix (file design, after section 6)

| Component | Status | Note |
|---|---|---|
| Closed entry types | EXISTS | Keep the 12 types; new types join the closed list |
| Type-tagged JSON envelope and decoder for every entry type | NEW | `{"seq":n,"type":"...","data":{...}}`; a round-trip test for all types; an unknown type is refused |
| Session header record (line 1) | NEW | id, format version, cwd, created time, parent session and cut `seq` for a fork |
| Model and thinking change entries | NEW | Both sources record them; `SetModel` and `SetThinkingLevel` write none today |
| `seq` equal to the slice position | NEW | Keeps `CommitRef`, `ToolCall.AssistantEntry`, `commitIndex` valid; append checks the next `seq` |
| Session-store interface (create, open, list, stat; handle append, entries, close) | NEW | Where it lives must respect the import rules (`sessions` may import only `store` and `pkg/protocol`) |
| JSONL backend | NEW | Built in `internal/app` or a new package, passed through `agent.Config.NewContext` (`internal/agent/types.go:56-59`); `NewContext` needs a session identity |
| Atomic first write | NEW | Temp file plus rename, as DeepSeek publishes the file |
| Torn tail rule | NEW | A last line with no newline or bad JSON is cut off before the next append, never shown to readers |
| fsync policy | NEW | Sync at side-effect boundaries (before each model call and each tool run) |
| Per-session `flock` | NEW | Reuse the pattern of `internal/leader/lock.go:55` and `internal/settings/lock_unix.go:14`; lock file is never removed |
| Context builder | EXISTS in `agent` | `projectContext`, `projectHistory`, `rebuildRequest`; it calls `providers.ConvertToLLM`, so it stays in `agent` |
| Resume repair of an open turn | NEW | Append closing entries; reuse `repairTools` (`internal/agent/agent.go:761`) |
| `context_edit` | NEW | Append-only, names the omitted entries (Pi, DeepSeek) |
| `-c`, `--session`, `--no-session` | NEW | `cmd/tui/args.go`; wire path to the leader is decision W1 |
| In-memory copy | NEW | Each request reads the whole log (`context_source.go:20`); the writer keeps entries in memory and writes through |

## 4. Adopt and avoid

Adopt:

1. One JSON line for each entry, with a gap-free `seq` that append checks (DeepSeek).
2. A synchronous flush before each model call and each tool run; a write error stops the run (DeepSeek; Ask already ends a run on an `Append` error).
3. Repair at resume only by appending closing entries, never by deleting history (both).
4. Model, provider, and thinking level come back from the log; tools and system prompt come from the current setup, and a difference is recorded (DeepSeek order; omp warns on a missing model).
5. `context_edit` is append-only and names what it hides (Pi, DeepSeek). Do not copy omp's in-place prune.
6. A second writer fails at once with a clear error; it never waits and never forks silently (DeepSeek).
7. Lazy file creation on the first assistant message, except ACP and `/new` (omp).
8. One shared contract test suite that runs against `MemoryLog` and the JSONL backend, and later the SQLite backend (DeepSeek).

Avoid:

- DeepSeek's chain of migrations over five log formats, the import step between backends, zstd frames, and the 200 ms batching window.
- omp's file-concurrency layer (title slot, publish lock, sibling-file move, continuation chains, full rewrites) and its SQL backend that keeps a whole file in one cell.

## 5. Decision matrix (challenge)

Decisions 1 to 4 of the first draft are answered in section 6 (U1 to U4). Open and default items:

| # | Decision | DeepSeek | omp / Pi | Ask now | Recommendation | Who decides |
|---|---|---|---|---|---|---|
| W1 | How `-c` / `--session` reach the leader for the TUI | - | Pi: in process | Leader refuses `session/load` (`internal/leader/policy.go:82`); per-session flags travel in `_meta["ask.dev/session"]` | U5: ACP `session/load` | User |
| W2 | `ask acp` in H8 | - | - | No listener, no database; files would not break `TestACPE2ENoListener` | U6: save like other modes | User |
| 5 | File location | `<root>/--<cwd>--/<id>/session.vN.jsonl` | `~/.omp/agent/sessions/<cwd>/` | - | `<ASK_HOME>/sessions/<encoded-cwd>/<id>.jsonl` plus `<id>.lock` | Default in the plan |
| 6 | `-c` scope | No "latest" | Latest in cwd | - | Latest session of the canonical cwd | U4 |
| 7 | Interrupted turn | Closing events | Synthetic results plus aborted message | `repairTools` exists | Append closing entries at resume | Default (both agree) |
| 8 | Encoding | zstd by default | Plain lines | - | Plain lines | Default in the plan |

## 6. User decisions (2026-11-18)

The user chose "do it like DeepSeek". These answers replace decisions 1 to 4 and the SQLite-only parts of section 3 and section 4:

| # | Decision | User answer | Effect |
|---|---|---|---|
| U1 | Storage | One session-store interface. H8 builds the JSONL file backend and a shared contract test suite. A later phase adds a SQLite backend behind the same interface. | `ask -p` keeps its "no database" rule (`AGENTS.md:143`). H8 needs no migration and no GORM model. `docs/ask-architecture-reference.md:414` and roadmap H8 text must change. |
| U2 | Write lock | Kernel `flock` for each session, as DeepSeek does. No expiry and no renewal. A second writer fails at once with a clear error. The kernel frees the lock when the holder dies. A stale writer is also stopped by the `seq` check on append. | Roadmap H-SESS-15 lease row, generation, and the "expired lease" test are replaced by "crashed holder, immediate takeover". |
| U3 | Branching | A session log is linear (`seq` only). A branch is a fork into a new session that points to its parent session and cut point. | Roadmap H14 changes from an in-file id/parentId tree to fork into a child session. |
| U4 | Selection | `-c` resumes the latest session of the canonical cwd; `--session <id>` picks one; `--no-session` does not save. | The H8 exit test "resume with `-c`" stays. |

| U5 | Resume wire | Standard ACP `session/load` in the shared `acp.Adapter`; it opens the file, takes the lock, and replays history as `session/update`. The leader routes it (today `internal/leader/policy.go:82` refuses it). | The TUI shows the old conversation; editors on `ask acp` get resume too. |
| U6 | `ask acp` | Saves session files and serves `session/load`, like the leader. | No database and no listener still hold, so `TestACPE2ENoListener` stays valid. |

Defaults the plan states (not asked; change on request): plain JSONL lines (no zstd); an interrupted turn is closed at resume by appending closing entries; files live under `<ASK_HOME>/sessions/`.

User approval of defaults (2026-11-18, answer "ok" to the DeepSeek comparison): plain JSONL lines (no zstd); lazy file creation; synchronous write with a forced flush and fsync before each model call, each tool run, and each step (no 200 ms batching window); atomic first write by temp file, fsync, `link()` (EEXIST on a race), directory fsync; truncate back to the old size on a failed append; drop a torn last line before the next append, and a bad line before a valid `turn/end` is corruption; location `<ASK_HOME>/sessions/<encoded-cwd>/<id>.jsonl` plus `<id>.lock`; resume repair appends closing entries (unknown outcome, not started, interrupted turn end); header `version`, unknown entry type refused unless ignorable, "newer format" error separate from "corrupt"; no format migration chain. Roadmap SQLite items: lease row dropped (test becomes crashed holder, immediate takeover); busy timeout, migrations-run-once, and SQLite transactions move to the SQLite phase; schema migration test dropped from H8. Resume restores the model from the log in every mode; an explicit `--model` wins. Session id (user, 2026-11-18): snowflake through the library `github.com/bwmarrin/snowflake` (Twitter layout: 41-bit milliseconds since 2010-11-04, 10-bit node, 12-bit step; keep the package defaults). Each process (leader, `ask -p`, `ask acp`) takes a random node number 0-1023. The session id is a `string` everywhere (Go type, JSON, file name, ACP), made by `ID.String()` (decimal); no code passes it as `int64`. Every id made between about 2017 and 2080 has 19 digits, so `-c` compares ids as strings. One format for every mode (UUID and `sess_` + hex go away). A collision gets EEXIST on the first `link()`; the creator makes a new id and retries. New dependency: add to `go.mod` and the AGENTS.md tech stack.

Risk score: medium-high. The change touches the agent seam, every composition module, a stated "no database" rule, migrations, and a cross-process contract.

## Unresolved questions

1. Decisions 1 to 4 in section 5 need the user.
2. Unverified: golang-migrate `sqlite3` locking across processes. Run an experiment before the plan fixes a migration lock.
3. Unverified: whether lint enforces the `sessions` import list from its README.
