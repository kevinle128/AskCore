---
phase: 2
title: "JSONL backend"
status: pending
priority: P1
effort: "10h"
dependencies: [1]
---

# Phase 2: JSONL backend

## Goal

`internal/store/jsonlstore` implements `store.SessionStore` on JSONL files with a per-session flock, durable appends and torn-tail repair, and passes the shared contract suite.

## Context

- flock precedent: `internal/settings/lock_unix.go:13-19` (`syscall.Flock` with `LOCK_EX|LOCK_NB`, `EWOULDBLOCK`/`EAGAIN` mean busy); `internal/leader/lock.go:55` (lock file never removed).
- flock locks belong to the open file description, so a second open in the same process also fails. No in-process owner map is needed.
- Private directory helpers: `settings.EnsurePrivateDir`, `settings.CheckPrivateDir` (used in `internal/app/agent_native.go:145,166`). The app checks the root; the store makes the per-cwd folder with mode 0700 and files with 0600.
- Cross-process test pattern: `internal/settings/auth_test.go:197` (`storeChild` reruns the test binary with a mode variable); `internal/leader/helper_process_test.go`.
- Approved defaults: [plan.md](plan.md#approved-defaults-2026-11-18).

## Requirements

1. Layout: `<root>/<encoded-cwd>/<id>.jsonl` and `<id>.lock`. `encoded-cwd` is the canonical cwd with path separators and `:` replaced by `-`, framed as `--...--` (Pi form). The header `cwd` is the authority: `List(cwd)` and `Open` refuse a header whose cwd differs, so two cwds that encode the same never mix.
2. `Create(CreateLazy)`: keep entries in memory, write nothing. At the first assistant `MessageEntry`: take the flock, write header plus held entries to `.<id>.<rand>.tmp`, fsync, `link()` to `<id>.jsonl`, remove the temp, fsync the directory. On EEXIST, the writer itself makes a new id with `sessions.NewID()`, moves its lock to that id and retries (the Agent's `Append` cannot retry); `ID()` then reports the new id. `Create(CreateNow)` does the same before it returns.
3. `Create(CreateNow)`: the same first write at once, with the header only.
4. `Open(cwd, id)`: take the flock (busy -> `ErrSessionOwned` with the id and lock path in the text). Scan lines. A last line with no newline, or a bad last line, is the torn tail: cut it (truncate plus fsync) before the first append. A bad line with a valid `turn_closed` line after it is `ErrSessionCorrupt` naming the line number. A bad line with no valid `turn_closed` after it is a crash boundary: cut from that line to the end before the first append (approved rule). Unknown type or newer header -> `ErrSessionFormatNewer` (wrapping `sessions.ErrUnknownEntryType` for a type), never `ErrSessionCorrupt`.
5. `Read(cwd, id)`: no lock; returns the committed prefix without the torn tail; never writes.
6. `List(cwd)`: reads the first line of each `*.jsonl` only; skips files with a bad header; sorts by id as strings, newest first.
7. Writer `Append`: check that the first new seq equals the held count (else `sessions.ErrWriterLost`). Encode all entries first (all or none). Write in one `write` call. fsync when the batch holds a `RequestDelta`, `ToolCall`, `TurnOpened` or `CycleClosed`. On a write or fsync error, truncate to the old size, fsync, latch the writer as lost and return `sessions.ErrWriterLost` joined with the cause. Reads (`Entries`, `Messages`, `LastSystemSnapshot`) serve the in-memory copy.
8. `Close`: fsync, release the flock, close the files. The lock file stays. Close of a lazy writer that never wrote leaves no file.
9. Unix only (`//go:build unix`), as the settings lock. Open the lock and log files with `os.OpenFile` (Go sets close-on-exec), never with a raw `syscall.Open` without `O_CLOEXEC`. A `bash` child of the holder must not inherit the descriptor, or the lock outlives a killed `ask`.
10. Lint: add depguard rule `jsonlstore-only-in-root` (only `internal/app` and `internal/store/jsonlstore` import it), next to `gormstore-only-in-root` (`.golangci.yml:296-305`).

## Files owned

- Create: `internal/store/jsonlstore/store.go`, `writer.go`, `scan.go`, `path.go`, `lock_unix.go`, `doc.go`, `README.md`
- Create tests: `internal/store/jsonlstore/contract_test.go`, `torn_tail_test.go`, `append_failure_test.go`, `lock_process_test.go` (helper process), `path_test.go`
- Modify: `.golangci.yml`

## Tests written first

- `TestJSONLWriterContract`, `TestJSONLStoreContract` (shared suite).
- `TestTornTailIsCutBeforeNextAppend` and `TestTornTailIsHiddenFromRead`; `TestBadLineBeforeTurnEndIsCorrupt`, `TestBadLineAfterLastTurnEndIsCut`, `TestUnknownTypeIsNewerFormat`.
- `TestFailedAppendTruncatesToOldSize` (fault from a closed or read-only descriptor; no fake file system).
- `TestFirstWriteCollisionReportsExist` (pre-create `<id>.jsonl`).
- `TestLazySessionLeavesNoFileUntilAssistantMessage`.
- Cross-process with real child processes of the test binary: `TestHeldSessionRefusesSecondProcess`, `TestKilledHolderReleasesSessionAtOnce` (the holder child starts a long-running grandchild first; SIGKILL the holder while the grandchild still runs; open succeeds with no wait), `TestTwoProcessesWriteTwoSessions`, `TestTwoProcessesCreateFirstSessionOfFreshHome`.
- `TestSameCwdEncodingDifferentCwdIsRefused`.

## Steps

1. Write the tests; run them to see them fail.
2. Path and lock helpers; then scan; then writer; then store.
3. Add the depguard rule and run lint.
4. Measure one faux turn with fsync on this macOS host and record the time in the phase review (input for the fsync risk).

## Validation

```
go test ./internal/store/... ./internal/sessions/...
go test -race ./internal/store/jsonlstore/...
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...
```

## Risks

| Risk | L x I | Mitigation |
|---|---|---|
| `F_FULLFSYNC` makes each boundary fsync slow on macOS | M x M | fsync only at boundaries; record the measured cost; a change of policy needs the user |
| A torn-tail cut removes a record that the writer reported as done | L x H | a record counts as written only after its newline is written, so only an unreported append can lose a line; the cut applies to the last line only, and a test pins that a last line with no newline is cut even when it decodes |
| Lock file residue grows | L x L | one small file per saved session; no deletion in H8 (non-goal) |
| NFS home breaks flock | L x M | documented limit in the package README |

## Rollback

Revert the commits. No composition imports the package until phase 4.

## Code hygiene

No plan labels, phase numbers or decision codes in code, comments, test names or commit messages.
