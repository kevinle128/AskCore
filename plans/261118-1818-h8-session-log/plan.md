---
title: "H8: session log (JSONL session store, resume, ACP session/load)"
description: "Save each session as an append-only JSONL file behind one session-store interface, and resume it with -c, --session and ACP session/load."
status: completed
priority: P1
effort: 47h
branch: task/plan-next
tags: [feature, backend, sessions, acp, critical]
blockedBy: []
blocks: []
created: 2026-11-18
---

# H8: session log

## Outcome

Every mode (`ask -p`, leader, in-process TUI, `ask acp`) saves the typed session log to `<ASK_HOME>/sessions/<encoded-cwd>/<id>.jsonl`. A killed process resumes with `-c`, `--session <id>` or ACP `session/load`, with the same model and thinking level. A second writer fails at once. `--no-session` saves nothing.

## Constraints

- Binding user decisions U1-U6 and the approved defaults below. No GORM, no migration, no database in H8. `ask -p` and `ask acp` start no database and no listener (`TestACPE2ENoListener`, `cmd/tui/acp_e2e_test.go:1355`).
- Import rules: `docs/ask-architecture-reference.md` sections 4, 8, 9 and `.golangci.yml` depguard. Storage interfaces live in `internal/store` (section 4.2).
- Tests run the real code path (built `ask` binary or real subprocesses). No test-only harness that bypasses `ask -p`.
- No plan labels, phase numbers or decision codes in code, comments, test names or commit messages.
- Write docs in ASD-STE100 Simplified Technical English.

## Non-goals

- The SQLite backend (a later phase behind the same interface; see [design.md](design.md#extension-trace-the-sqlite-backend)).
- Fork, tree navigation, session picker, names, deletion or retention (H14 and later).
- A writer of `context_edit` entries (retry and overflow recovery in H9/H10 write them).
- TUI reconnect that reloads the session after a leader replacement.

## Acceptance criteria

1. Exit test: a built `ask` run on a non-default model and thinking level is killed with SIGKILL after one turn. `ask -p -c "next"` (no model flags) continues. Its `RequestDelta` has the same model and reasoning level, and the log holds the closing entries of the cut turn.
2. The shared contract suite passes on `MemoryLog` (writer part) and on the JSONL backend (writer and store parts).
3. A second process that opens a held session fails at once with a clear error. After SIGKILL of the holder, a new process takes the session at once.
4. ACP `session/load` works on `ask acp`, on the leader and on the in-process TUI; the TUI shows the old conversation.
5. `go test ./...`, `go vet ./...`, golangci-lint and the touched `python3 e2e/tui/run.py` cases pass.

## Phases

| # | Phase | Effort | Depends on | Status |
|---|-------|--------|------------|--------|
| 1 | [Entry codec, ids, store interface and contract suite](./phase-01-entry-codec-and-store-contract.md) | 6h | - | Done |
| 2 | [JSONL backend](./phase-02-jsonl-backend.md) | 10h | 1 | Done |
| 3 | [Agent wiring](./phase-03-agent-wiring.md) | 8h | 1 | Done |
| 4 | [Headless resume and composition](./phase-04-headless-resume-and-composition.md) | 8h | 2, 3 | Done |
| 5 | [ACP session/load, leader, TUI, ask acp](./phase-05-acp-session-load.md) | 12h | 4 | Done |
| 6 | [Docs sync](./phase-06-docs-sync.md) | 3h | 5 | Done |

Phases 2 and 3 can run in parallel: their file sets do not overlap (see each phase). All other phases are sequential.

## Dependencies

- New module `github.com/bwmarrin/snowflake` (BSD-2-Clause, checked on GitHub 2026-11-18).
- Vendored ACP SDK `AgentLoader` and `AgentCapabilities.LoadSession` (`third_party/acp-go-sdk/types_gen.go:46,9357`).
- Design detail, data flow, test matrix and the SQLite extension trace: [design.md](design.md).

## Roadmap H8 walk (roadmap.md:364-400)

| Roadmap line | Result |
|---|---|
| Owns: persist H-SESS-03 entry types in SQLite | Changed (U1): persist in JSONL behind `store.SessionStore`; same closed entry types plus `model_change`, `thinking_change`, `context_edit` |
| Owns: H-SESS-06 context builder | Kept; stays in `agent` (`request_log.go:112` calls `providers.ConvertToLLM`); applies `context_edit` |
| Owns: single-writer contract to SQLite transactions and cross-process ownership | Changed (U2): flock per session plus the seq check on append; SQLite transactions moved |
| Owns: H-SESS-10 P0 (`-c`, `--session`) | Kept, plus `--no-session` (U4); TUI path through `session/load` (U5) |
| Owns: H-SESS-15 SQLite lease row, generation, renew, busy timeout, one migration path | Changed (U2): flock, no expiry, no renew, no generation; busy timeout and migration path moved |
| Owns: H-SESS-01, H-SESS-02 (SQLite storage, schema version) | Changed (U1): JSONL file; header `version`; no migration chain |
| Owns: H-COMPACT-13 `context_edit` | Kept: entry type, persistence, projection; writer is a non-goal |
| Owns: persist snapshots, request deltas, tool declarations for D23 rebuild | Kept: they round-trip; rebuild after reopen is tested |
| Owns: H-PROV-19 session restore part | Kept: restored model sits after `--model` and before the saved default |
| Owns: session header record in JSON mode (H-MODE-03) | Kept: `ask -p --mode json` writes the header record first |
| Packages: sessions, store, store/gormstore, migrations | Changed: sessions, store, store/jsonlstore, agent, app, acp, leader, tuiclient, cmd/tui |
| Test: write ordering (E§24#10) | Kept |
| Test: resume restores provider/model, method provenance, thinking; no token snapshot | Kept |
| Test: `context_edit` omits from context, keeps in history | Kept |
| Test: two processes cannot write one session | Kept (flock, real subprocesses) |
| Test: paused writer with expired lease cannot write | Changed (U2): crashed holder, immediate takeover; stale writer stopped by seq check |
| Test: two processes write two sessions without SQLite busy errors | Changed: without lock errors on JSONL; busy part moved |
| Test: two processes on a fresh database, migrations run once | Moved to the SQLite phase; JSONL analogue: two processes create the first session of a fresh home |
| Test: credential replaced by headless; method mismatch needs explicit selection | Kept |
| Test: schema migration | Dropped |
| Exit: kill after a turn, resume with `-c`, same model | Kept, plus same thinking level (U4) |

## Dropped or moved items

Approved by the user (2026-11-18): the SQLite lease row and generation are dropped (test becomes "crashed holder, immediate takeover"). The SQLite busy-timeout test, the migrations-run-once test and SQLite transactions move to the SQLite phase. The schema migration test is dropped from H8. Lease-expiry test: dropped. Needs user approval: none beyond the defaults below.

## Approved defaults (2026-11-18)

Source: [xia report section 6](../reports/xia-261118-1603-h8-session-log-improve.md#6-user-decisions-2026-11-18).

- Plain JSONL lines, no zstd. Header line has `version`. No format migration chain.
- Lazy file creation.
- Synchronous append. Flush and fsync before each model call, each tool run and each step. No batching window. A write error stops the run.
- First write: temp file, fsync, `link()` (EEXIST on a race), directory fsync. A failed append truncates the file back to its old size.
- A torn last line is cut before the next append and never shown. A bad line before a valid turn end is corruption.
- Location `<ASK_HOME>/sessions/<encoded-cwd>/<id>.jsonl` plus `<id>.lock`.
- Resume repair appends closing entries: unknown outcome, not started, interrupted turn end.
- An unknown entry type is refused unless marked ignorable. "Newer format" and "corrupt" are separate errors.
- Resume restores the model from the log in every mode. An explicit `--model` wins.
- Session id: `github.com/bwmarrin/snowflake` with package defaults; random node 0-1023 per process. The id is a Go `string` everywhere (JSON, file names, ACP, header), made by `ID.String()` (decimal, 19 digits for ids made from about 2017 to 2080). `-c` compares ids as strings, so string order is time order; it never parses them. One format for all modes. EEXIST on first `link()` makes the creator retry with a new id.

## Planner defaults (approved by the user, 2026-11-18)

- Interface `store.SessionStore` in `internal/store/session_store.go` (architecture section 4.2); backend `internal/store/jsonlstore`; codec in `internal/sessions` so every backend shares it. New depguard rule `jsonlstore-only-in-root`.
- Store calls take the canonical cwd and the id (`Open(ctx, cwd, id)`); `internal/workspace` exports its canonical-path helper for this.
- The contract suite has a writer part (runs on `MemoryLog` and the JSONL writer) and a store part (JSONL now, SQLite later).
- `encoded-cwd` is the Pi form (`--` + cwd with `/`, `\`, `:` replaced by `-` + `--`); the header cwd decides, so two cwds that encode the same never mix.
- fsync triggers: a batch that holds `RequestDelta` (before a model call), `ToolCall` (before a tool run), `TurnOpened` (step) or `CycleClosed`, plus `Close`.
- An unknown entry type gives the "newer format" error; a bad line gives "corrupt" only when a valid turn end follows it, else it is a crash tail and is cut.
- `model_change` and `thinking_change` are appended at run start when the selection differs from the last logged one; a change with no later run is not saved. The auth method comes back from the last `AttemptSettled.Binding` (the Agent does not know a requested method).
- Lazy creation waits for the first assistant message; ACP `session/new` and `/new` create the file at once, so their id can be loaded and `-c` finds them.
- `/new` keeps the live ACP session id and opens a new file with a new id.
- `--no-session` travels to the leader as `noSession` in `_meta["ask.dev/session"]`.
- `--session` takes an exact id. A session whose header cwd differs from the canonical cwd is refused.
- New ACP error kind `session_in_use` for a session that another writer holds.
- A new ADR records the storage decision (phase 6).

## Answered questions (user, 2026-11-18)

1. Seed on resume: keep the logged seed and append the current text of a changed context file at resume (phase 3, requirement 7).
2. Restored auth method no longer available: refuse the resume until the user picks a model; the roadmap test "a method mismatch requires explicit selection" already decides this.
3. Leader `session/load` of a live session: attach to the live session through the existing attach path, with the history replay (phase 5, step 7).
4. A snowflake collision on the first lazy write: accepted (practically unreachable).
5. Windows: out of scope; Unix only at launch (user rule, 2026-10-01).

## Unresolved questions

None.
