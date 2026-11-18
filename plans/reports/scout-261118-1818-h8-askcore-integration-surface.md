# Scout: AskCore integration surface for H8 (session log)

Date: 2026-11-18. Base: commit `734773c`. Read-only scout. The storage decisions changed after this scout (files like DeepSeek, see [xia report section 6](xia-261118-1603-h8-session-log-improve.md)); the SQLite-only notes are kept in section 5 as background.

## 1. Session model and writer (`internal/sessions`)

- `entry.go:13-15` closed `Entry` interface with 12 types: `MessageEntry` :19, `CycleOpened` :27, `CycleClosed` :35, `TurnOpened` :43, `TurnClosed` :48, `AttemptSettled` :63, `ToolCall` :77, `RetryScheduled` :83, `RetryStarted` :96, `RequestDelta` :171, `SystemSnapshot` :185, `InputOutcome` :194. Markers :200-211. `MessagesOf` :297.
- No entry has an id field. References are integer log positions: `ToolCall.AssistantEntry` (:77-80), `CommitRef` (`writer.go:7-10`), `trackedLog.end` (`internal/agent/follow.go:16-34`), `commitIndex` (`internal/agent/agent.go:212`). A persisted `seq` must equal the slice position.
- `writer.go:16-31` `Writer`: `Append` (atomic), `Messages`, `Entries`, `LastSystemSnapshot`, `Close`. Sole-writer contract :12-15.
- `memory.go:16-23` `MemoryLog`; `Append` :32-50; `checkEncodable` (json.Marshal) :83-94.
- `README.md`: "Current storage is in memory". Allowed imports: `store` and `pkg/protocol` (not confirmed as a lint rule).

## 2. Agent seam (`internal/agent`)

- `types.go:56-59` `Config.NewContext func() sessions.Writer` takes no session id. `Seed` :60-65, `SessionID` :53-55, `Release` :85.
- `agent.go:187-190` default `NewContext` is `MemoryLog`. `seed.go:26-48` `seedLog` reads `w.Entries()` first and skips seed items already present, so a preloaded log is not re-seeded.
- `agent.go:519-586` `Reset`/`swapContext` call `NewContext` again (:551). With persistence, `/new` must create a new session.
- `agent.go:727-770` `execute`; `openLog` :776-783 appends a `SystemSnapshot` only on change; `repairTools` :761.
- `loop_stream.go:13-60` builds each request from `l.context().Messages`; `logRequest` :188-191.
- `loop_run.go:239-244` an `Append` error ends the run (fits a lost-lock error; it needs a sentinel).
- `context_source.go:18-34` `projectContext` copies the whole log on every request. A file writer must keep an in-memory copy and write through.
- `request_log.go`: `projectHistory` :112-115 (calls `providers.ConvertToLLM`, so the context builder stays in `agent`), `newRequestDelta` :119-138, `rebuildRequest` :188-217 (D23).
- `SetModel` (:408) and `SetThinkingLevel` (:498) write no entry. The last `RequestDelta` has `ModelRef` and `RequestOptions.Reasoning` (`entry.go:143-163`), but only after an attempt. Resume "with the same model" needs a model/thinking change entry.
- `loop_tool_changes.go` records tool-set changes as `protocol.SystemMessage` entries.

## 3. Modes and session ids

- `ask -p`: `cmd/tui/headless.go:194-196`, `:412-416`; `newSessionID` :563-569 (random UUIDv4 layout).
- ACP host: `internal/acp/host.go:270-292` `NewSession`/`NewSessionWith` overwrite `req.ID` with `newSessionID()` (:347-352, `sess_` + 8 hex bytes).
- `internal/app/module_acp.go:182` `NewAgent(service, AgentSpec{SessionID: req.ID, ...})`; `AgentSpec` in `internal/app/session_start.go:60-78`.
- `internal/app/agent_native.go:126` reads `ASK_HOME`; `:143` per-session task output under the home.
- Leader: `internal/leader/policy.go:81-86` refuses `session/load`, `session/resume`, `session/list` ("Durable session calls have no owner yet").
- Per-session flags travel in `_meta["ask.dev/session"]` on `session/new` (`pkg/protocol/acp_session.go:7`, read in `internal/acp/session_options.go:27`, sent by `internal/tuiclient/client.go:471`).
- `cmd/tui/args.go:115-153,249-271`: no `-c`, `--session`, `--no-session`.

## 4. Locks and tests to reuse

- `flock` precedent: `internal/settings/lock_unix.go:14-20` (`LOCK_EX|LOCK_NB`), `internal/leader/lock.go:55,151` (lock file is never removed), `internal/leader/spawn.go:60`.
- Cross-process test precedent: `internal/settings/auth_test.go:197` `storeChild`, `:700` `TestEnsureHomeConcurrentCreate`; `internal/leader/helper_process_test.go`, `lock_test.go`.
- No-DB guards that stay valid with files: `cmd/tui/acp_e2e_test.go:1355-1392` `TestACPE2ENoListener` (fails on `.db`, `-wal`, `.sock` files), `internal/app/module_acp_test.go:38-41`, `cmd/tui/settings_command_test.go:206-215` (asserts no file in the Ask home for `ask settings show`).
- Real-path tests: `cmd/tui/headless_request_log_test.go:25` (`RequestDelta`), cassettes `cmd/tui/headless_cassette_test.go:38-63`.
- Unread: `internal/agent/session_log_test.go`, `attempt_test.go`.

## 5. Background (SQLite path, now deferred)

- `internal/store/stores.go:5-8` has only `Users`, `Posts`; `internal/store/README.md:7` plans a session store "added with the session tree".
- `internal/store/gormstore/db.go:12-14` sets no PRAGMA; `internal/migrations/migrations.go:15-33` has no lock; only `app.Module` (server) opens a DB.
- Lint `gormstore-only-in-root` allows `gormstore` only in `internal/app`, `cmd/server`, `store/gormstore`.

## Stale docs after the storage decision

- `docs/ask-architecture-reference.md:414` ("SQLite for both daemon and cloud mode") and `:189` ("SQLite persistence and branch selection remain roadmap work").
- `internal/store/README.md:7` (session store with the session tree).
- `plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md` H8 (lease row, SQLite tests) and H14 (in-file tree).
- Memory `ask-rebuilds-pi-harness-in-go` D1 line "session = entry tree".
