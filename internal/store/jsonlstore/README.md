# `internal/store/jsonlstore`

The session store on JSONL files. It implements `store.SessionStore` and passes the suites in `internal/store/storetest`. It works on Unix only.

## What belongs here

- The file layout, the file lock, and the repair of a damaged file end.
- Nothing about the entry format: `internal/sessions` owns the codec, so every backend reads and writes the same lines.

## File names

| File | Content |
|---|---|
| `store.go` | `Store`: `New`, `Create`, `Open`, `Read`, `List` |
| `writer.go` | The open session: append, fsync rules, first write, rollback, `Close` |
| `scan.go` | Reads a file and finds the committed part |
| `path.go` | Folder and file names, id check |
| `lock_unix.go` | `flock` on the lock file, close-on-exec file open |

## Layout

```
<root>/<encoded-cwd>/<id>.jsonl    header line, then one entry line each
<root>/<encoded-cwd>/<id>.lock     lock file, never removed
```

`encoded-cwd` is the working directory with `/`, `\` and `:` changed to `-`, between `--` and `--`. A name over 200 bytes is cut and gets a hash of the path. Two directories can have the same name. The `cwd` in the header decides: `Open` and `Read` refuse a file of another directory, and `List` does not show it. The store makes folders with mode 0700 and files with mode 0600. The caller checks the root.

## Write ownership

- `Open` takes an exclusive, non-blocking `flock` on the lock file. A busy lock gives `store.ErrSessionOwned` at once, also for a second open in the same process.
- The kernel drops the lock when the holder dies, also after SIGKILL, so the next process takes the session without a wait.
- Files are opened with `os.OpenFile`, which sets close-on-exec. A child of the holder does not keep the lock.
- `flock` is not reliable on some network file systems. Keep `ASK_HOME` on a local disk.

## Write rules

- A lazy session writes nothing until the first assistant message. Then it writes the header and the held lines to a temporary file, syncs it, links it to `<id>.jsonl`, removes the temporary name and syncs the folder. `link` never replaces a file. If the id is taken, the writer takes a new id from `sessions.NewID` and `ID()` reports it.
- `Append` encodes every entry first, writes the lines with one `write` call, and syncs when the batch holds `RequestDelta`, `ToolCall`, `TurnOpened` or `CycleClosed`. `Close` syncs too.
- If a write or a sync fails, the file is cut back to its old size and the writer is lost: every later `Append` returns `sessions.ErrWriterLost`. If the file size is not what the writer expects, someone else wrote. The writer is lost and leaves the file as it is.
- Reads (`Entries`, `Messages`, `LastSystemSnapshot`) use the copy in memory.

## Repair rules

| Finding | Result |
|---|---|
| Last line has no newline | Torn tail. `Read` hides it. `Open` cuts it, also when it decodes. |
| Bad line, no valid `turn_closed` after it | The process died in a turn. `Open` cuts from that line. `Read` stops before it. |
| Bad line, a valid `turn_closed` after it | `store.ErrSessionCorrupt` with the line number. The file is not changed. |
| Line number out of order | A bad line. |
| Unknown entry type, or a newer header version | `store.ErrSessionFormatNewer`. A line marked `ignorable` is skipped and has no log position. |

## Allowed imports

`internal/sessions`, `internal/store` and the standard library. Only `internal/app` imports this package (`jsonlstore-only-in-root` in `.golangci.yml`).
