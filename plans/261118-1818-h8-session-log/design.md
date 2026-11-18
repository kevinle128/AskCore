# H8 design detail

Companion of [plan.md](plan.md). Line citations are from commit `734773c`, checked with `rg -n` on 2026-11-18.

## Current state (verified)

| Fact | Evidence |
|---|---|
| 12 closed entry types; no entry id; references are slice positions | `internal/sessions/entry.go:13-15,200-211`; `ToolCall.AssistantEntry` `entry.go:77-80`; `CommitRef` `writer.go:7-10` |
| `protocol.Message` is an interface; decode needs `protocol.UnmarshalMessage` | `pkg/protocol/message.go:65`; used by `MessageEntry.Message`, `RequestDelta.Removed/Added` (`entry.go:19-25,171-183`) |
| `NewContext func() sessions.Writer` has no error and no id; no production caller sets it | `internal/agent/types.go:56-59`; default `MemoryLog` `agent.go:187-190`; `rg NewContext` finds only tests outside `agent` |
| `openSource` calls `newContext()`; New and `swapContext` (Reset) reach it | `internal/agent/seed.go:26-29`; `agent.go:551` |
| `trackedLog` reads `len(w.Entries())` at wrap, so a preloaded log gives a correct follow cut | `internal/agent/follow.go:21-25` |
| A failed `Append` ends the run | `internal/agent/loop_run.go:239-244` |
| `projectContext` copies `src.Messages()` per request | `internal/agent/context_source.go:18-34` |
| `repairTools` closes tool calls only for a turn that this run opened | `internal/agent/tool_repair.go:15-27,60-72` |
| `SetModel` and `SetThinkingLevel` write no entry | `internal/agent/agent.go:408,498` |
| ACP `/new` is `_ask/session/reset` -> `Session.Reset` -> `agent.Reset`; the ACP id stays | `pkg/protocol/acp.go:16`; `internal/acp/host.go:922-940` |
| Two id generators | `cmd/tui/headless.go:563-569` (UUID layout); `internal/acp/host.go:347-352` (`sess_`+hex) |
| Leader refuses `session/load`; drops `session/update` for an unregistered session | `internal/leader/policy.go:81-86`; `router.go:840-845` |
| Adapter has no `LoadSession`; `Initialize` lists only `Close` | `internal/acp/agent.go:254-256` |
| TUI renders history from the follow snapshot and ignores `session/update` | `internal/tui/translate_snapshot.go:20`; `internal/tuiclient/events.go:109-110` |
| The in-process TUI also talks ACP over a pipe | `internal/tuiclient/rpc/transport.go:77`; `cmd/tui/tui_client.go:60` |
| The `ask` binary already links `internal/store` and gorm | `go list -deps ./cmd/tui` |

## File format

```
{"type":"session","version":1,"id":"1863...","cwd":"/abs/canonical","createdAt":"2026-11-18T11:00:00Z","parentSession":"","parentSeq":0}
{"seq":0,"type":"message","data":{"message":{"role":"custom",...}}}
{"seq":1,"type":"system_snapshot","data":{...}}
{"seq":2,"type":"model_change","data":{"model":{"provider":"openai","api":"responses","id":"gpt-5"}}}
```

- `seq` equals the slice position. Append checks that the first new `seq` equals the stored count.
- `parentSession` and `parentSeq` are `omitempty` and unused in H8. They keep the header open for fork into a child session (U3, H14).
- `ignorable: true` on an envelope lets an old reader skip a type that it does not know. No H8 type is ignorable.
- Wire type names are fixed snake_case words, one per entry type, in one table in `internal/sessions/codec.go`.

## Data flow

1. Composition (`internal/app`) resolves `<ASK_HOME>/sessions`, checks it private, and builds one `jsonlstore.Store` per process.
2. New session: `Store.Create(id, header)` returns a `sessions.Writer`. Lazy mode keeps entries in memory until the first assistant `MessageEntry`; then it takes `<id>.lock`, writes the header and the held entries to a temp file, fsyncs, `link()`s it to `<id>.jsonl` and fsyncs the directory.
3. Resume: `Store.Open(cwd, id)` takes the flock (fails at once if held), scans, cuts a torn tail, decodes every line and returns a writer that holds the entries in memory.
4. The agent's `NewContext` closure returns that writer. The agent repairs an open turn, then runs. Each `Append` encodes, writes one line per entry, and fsyncs when the batch holds a `RequestDelta`, `ToolCall`, `TurnOpened` or `CycleClosed` (the side-effect and step boundaries). Reads come from memory.
5. `Close` fsyncs and releases the flock. The lock file stays (stable inode, as `internal/leader/lock.go`).

## Interface sketch (names may change in review)

```go
// internal/store/session_store.go
type SessionStore interface {
    Create(ctx context.Context, h sessions.Header, mode CreateMode) (SessionWriter, error) // CreateLazy | CreateNow
    Open(ctx context.Context, cwd, id string) (SessionWriter, error)                         // takes the write lock
    Read(ctx context.Context, cwd, id string) (sessions.Header, []sessions.Entry, error)   // no lock, committed prefix only
    List(ctx context.Context, cwd string) ([]sessions.Header, error)                       // header lines only
}
type SessionWriter interface { sessions.Writer; ID() string } // ID can change once: a lazy writer retries a first-write collision with a new id
var ErrSessionOwned, ErrSessionNotFound, ErrSessionCorrupt, ErrSessionFormatNewer error
```

`sessions.ErrWriterLost` (in `internal/sessions/writer.go`) is the sentinel that `Append` returns after a seq mismatch or a failed write; the writer stays failed until it is reopened.

## Extension trace: the SQLite backend

| Part | JSONL (H8) | SQLite (later) | Stays stable |
|---|---|---|---|
| Interface | `store.SessionStore` | same | yes |
| Codec | `sessions` codec, one line per entry | same codec, one JSON payload column per row | yes |
| Ownership | flock on `<id>.lock` | flock on the same lock path plus `seq` check inside `BEGIN IMMEDIATE` | contract: second writer fails at once |
| Durability | fsync at boundaries | `synchronous=FULL`, one transaction per append | contract: append is atomic |
| Schema | header `version` | golang-migrate, busy timeout, migrate-once test | header fields |
| Placement | `internal/store/jsonlstore`, only `internal/app` imports it | `internal/store/gormstore`, only `internal/app` imports it | depguard rules |

The SQLite phase adds `gormstore` session files, one migration and a factory for `storetest.RunSessionStore`. If that suite passes unchanged, the agent, ACP and CLI code need no change. A contract test that only JSONL can pass (torn tail) sits in the `jsonlstore` package, not in the suite.

## Test matrix

| Level | What | Where |
|---|---|---|
| Unit | codec round trip of every entry type; unknown type refused; ignorable skipped; newer version error; id format (19 digits, string order, uniqueness across goroutines) | `internal/sessions` |
| Contract | atomic append, copies, seq contiguity, reopen continues, single owner, lazy create leaves no file, list order | `internal/store/storetest` on `MemoryLog` and `jsonlstore` |
| Unit | torn tail, corrupt line, failed append truncates, EEXIST retry, fsync points | `internal/store/jsonlstore` |
| Cross-process | held lock refuses a child; SIGKILL holder, immediate takeover; two children write two sessions; two children create the first session of a fresh home | `internal/store/jsonlstore` helper process (pattern of `internal/settings/auth_test.go:197`) |
| Agent | resume repair entries; model and thinking entries at run start; `context_edit` projection; `Reset` opens a new log; lost writer ends the run | `internal/agent` |
| Built binary | exit test; `--session`; `--no-session` leaves no file; JSON header record; no token in file; method mismatch | `cmd/tui` |
| Built binary | `ask acp` `session/load`, no listener, no database | `cmd/tui/acp_e2e_test.go` |
| Real host | leader routes `session/load`, replay reaches the caller, live id refused | `internal/leader`, `internal/app/leader_routing_test.go` |
| Terminal | TUI `-c` shows the old conversation | `e2e/tui/run.py` new case `ask-resume` |

## Overall rollback

- Each phase is one or more focused commits; revert them in reverse order.
- Until phase 4 lands, no production composition sets `NewContext`, so the `MemoryLog` default (`agent.go:187-190`) keeps today's behavior.
- After phase 4, `--no-session` gives the in-memory path at run time. Tests keep `MemoryLog` as their default log.
- Session files are additive data under `<ASK_HOME>/sessions`. A revert leaves them unused; no other file changes format.

## Risks (overall)

| Risk | L x I | Mitigation |
|---|---|---|
| fsync cost on macOS (`File.Sync` uses `F_FULLFSYNC`) slows turns | M x M | fsync only at boundaries; measure one faux turn in phase 2; record the number |
| Leader drops replay updates of a load before the session is registered | H x H (verified) | router pre-registers the session on forward and forgets it on failure |
| `NewContext` signature change breaks many tests | H x L | mechanical edit in phase 3 only; list of files in the phase |
| Existing tests write session files into a real home | M x H | `cmd/tui` already isolates `ASK_HOME` (`headless_test.go:44,84`); phase 4 checks other packages |
| `ask settings show` must create no file in the home | L x M | session store is built lazily; `settings_command_test.go:206-215` stays green |
| A hand-started older leader answers `session/load` with "unsupported" | M x L | the TUI shows "the leader does not support session load; restart it with ask leader stop" and exits 1; `--no-leader` still works |
| A file descriptor of the lock leaks into a `bash` child, so the lock outlives a killed `ask` | M x H | open lock and log files with `os.OpenFile` (close-on-exec); a test kills a holder that has a live child |
| Golden JSON-mode tests change (header line first) | H x L | update `cmd/tui/headless_test.go` goldens in the same phase |
