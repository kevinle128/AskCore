---
status: accepted
---

# Session logs are JSONL files behind one store interface

The roadmap first planned to save the typed session log in SQLite, with a lease row that expires and renews, and an entry tree inside one file.
`ask -p` and `ask acp` start no database and no listener, and a migration lock across processes was never proved.

Decision (user, 2026-11-18): keep one session-store interface and write a file backend first.

- `store.SessionStore` ([session_store.go](../../internal/store/session_store.go)) is the interface. `internal/store/jsonlstore` is the first backend. A SQLite backend can follow behind the same interface and must pass the same suite, `internal/store/storetest`.
- Every mode (`ask -p`, the leader, the in-process TUI, `ask acp`) saves a session as `<ASK_HOME>/sessions/<encoded-cwd>/<id>.jsonl`. `--no-session` saves nothing. Server data (users, posts) still uses SQLite; the two do not mix.
- The log is linear. `seq` equals the position of the entry, and an append with a wrong `seq` fails. A branch is a fork into a child session (header `parentSession` and `parentSeq`), not a tree inside one file.
- One writer for each session. The writer holds an exclusive `flock` on `<id>.lock`. A second writer fails at once. The kernel drops the lock when the holder dies, so a new process takes the session after a crash without a wait. The `seq` check also stops a stale writer.
- The id is the decimal text of a snowflake id (`github.com/bwmarrin/snowflake`, one random node number for each process). Ids are strings everywhere and are compared as strings, so for 19-digit ids the string order is the time order. `-c` uses that order to find the newest session.
- The line format is plain JSON with a `version` in the header and no migration chain. An unknown entry type is refused as "newer format" unless the line is marked `ignorable`.
- The entry codec lives in `internal/sessions`, so every backend writes the same lines.

## Why

- The "no database" rule of print mode and `ask acp` stays true: a read or a write of a log starts no database.
- A file lock has the failure behavior that we want: the lock ends with the process, with no timer to tune and no row to clean.
- Plain files can be read, copied and repaired with ordinary tools.

## Rejected alternatives

SQLite first, with a lease row, a generation and a renew timer: rejected because it needs a database and a migration in the commands that must start none, and it adds a lease that expires although the kernel lock gives the same safety with less code.

A tree of entries (`id` and `parentId`) in one file: rejected because a linear `seq` makes append, repair and resume simple, and a fork into a child session gives the same branch with no change to the writer.

Compressed lines: not used. The lines are plain, so a file can be read with ordinary tools and the repair of a torn tail is a cut at a line end.

## Limits

- Unix only. The lock uses `flock`; Windows is out of scope. The file system must support `flock` (a network file system can fail; keep `ASK_HOME` on a local disk).
- A write error stops the run, so an `ASK_HOME` that cannot be written fails a run unless it uses `--no-session`.
- Joining a live session (a second client that loads a session that the leader runs) does not apply flags such as `--model`.

## Where to read next

- File layout and repair rules: [jsonlstore README](../../internal/store/jsonlstore/README.md).
- Entry types and codec: [sessions README](../../internal/sessions/README.md).
- Flags and errors: [cli.md](../cli.md#sessions).
- `session/load` on ACP: [acp README](../../internal/acp/README.md).
