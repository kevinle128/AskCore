---
phase: 4
title: "Tools, context files, skills and sandbox"
status: pending
priority: P1
effort: 7h
dependencies: []
---

# Phase 4: Tools, context files, skills and sandbox

## Goal

Fix R2 (context-file containment), R3 (bounded `read`), R7 (`edit` re-stat), N24, N25, N26 and N27, with no change to any tool description.

## File ownership

- Modify: `internal/bootstrap/load.go`, `internal/bootstrap/truncate.go`
- Modify: `internal/tools/file_read.go`, `file_edit.go`, `file_atomic.go`, `file_image.go`, `coerce.go`, `shell_bash.go`
- Modify: `internal/sandbox/view.go`
- Modify: `internal/skills/loader_listing.go`
- Tests: `internal/bootstrap/load_test.go`, `internal/tools/file_read_test.go`, `file_edit_test.go`, `file_atomic_test.go`, `file_image_test.go`, `coerce_test.go`, `shell_bash_test.go`, `internal/sandbox/view_test.go`, `internal/skills/loader_listing_test.go`

The renderers of the new skip reason are owned by phase 3 (`internal/app/session_start.go`) and phase 6 (`cmd/tui/settings_command.go`).

## Constraints

- Tool descriptions do not change. Text that a tool returns may change only where a step says so. Then `docs/reference/` and the six cassettes stay valid; phase 7 proves it.
- Tests use `t.TempDir()`, real files and the real tool entry points. Time-dependent checks use `os.Chtimes`, not the clock.

## Steps

### 1. R2 (Medium): skip a project context file whose target is outside the project

User decision: skip only targets outside the repo (git root, or cwd when there is no git root). In-repo symlinks keep loading. Files of the Ask home (`UserDir`) are not checked.

Evidence: `readRegular` follows links (`internal/bootstrap/read.go:17-21`). `LoadContext` reads each project folder from the git root down to cwd (`load.go:64-108`, `discover.go:14-41`) with no trust (H6a, `docs/cli.md:147`). A cloned repo can ship `AGENTS.md -> ~/.aws/credentials`, and Ask sends the text at session start.

Tests first in `load_test.go`:

1. `TestLoadContext_LinkOutsideProjectIsSkipped`: a git root with `AGENTS.md` linked to a file in another temp folder; expect no file, one `Skip{Reason: SkipOutsideProject}` with the link path, and no target text in `Text`.
2. `TestLoadContext_LinkInsideProjectLoads`: `CLAUDE.md` in a subfolder linked to `docs/rules.md` of the same repo; expect it loaded.
3. No git root: cwd `AGENTS.md` linked into cwd loads; linked to the parent folder is skipped.
4. The root reached through a symlinked path (macOS `/var` -> `/private/var`): an in-repo link still loads.
5. A `UserDir` file linked outside still loads.
6. Change `TestLoadContext_FileKinds` (`load_test.go:158-166`): its link targets another temp folder, so it now expects `SkipOutsideProject`. This is the intended change; say so in the commit message.

Commit order: make the enum (fix item 1) the first commit of this phase, so phases 3 and 6 can add their renderer lines at once.

Fix:

1. Add `SkipOutsideProject` at the end of the `SkipReason` list (`load.go:28-40`), with a doc line. Add it to the no-op case of `truncate.go:43`.
2. In `LoadContext`, the project root is the first folder that `contextDirs` returns (git root, or cwd). Resolve it once with `filepath.EvalSymlinks`. When that fails, return the error, as `contextDirs` does for a failed `.git` probe (`discover.go:30-32`): the load must not guess a root.
3. For each project file: `real, err := filepath.EvalSymlinks(path)`. Not exist -> continue, as today. Other error -> `SkipUnreadable`. A `real` that is not the root or under it (`filepath.Rel` starts with `..`) -> `SkipOutsideProject`, and nothing is read. Else call `readRegular(real)`, so the file that was checked is the file that is read.
4. Skip the check for the `UserDir` folder (`load.go:70-73`).

Command: `go test -run '^TestLoadContext' ./internal/bootstrap`

### 2. R3 (Medium): `read` keeps memory bounded on a large file

User decisions: whole read up to 4 MiB; above that a windowed line read; the binary sniff runs before any decode (2026-11-10, earlier). An image file larger than 20 MiB (omp's cap) is refused before it is read whole, and the page text stays the same for every file size, with the total line count (2026-11-10).

Evidence: `io.ReadAll(f)` (`internal/tools/file_read.go:123`), `string(data)` (`:138`) and `strings.Split` (`:175`) keep about three times the file in the leader, and each page reads the whole file again. One multi-GB read can stop the leader for all sessions.

Design: keep the text that the model gets the same for every file size. The page text needs the total line count (`file_read.go:187`, `:205`, `:233-234`, `:238`), so the windowed reader counts the newlines of the rest of the file with one reused 8 KiB buffer and stores only the page. Memory is then bounded by the page cap (50 KB plus one buffer). The cost is one sequential pass over a large file for each page, which `bash` would also pay.

Tests first in `file_read_test.go`:

1. `TestReadLargeFileMatchesWholeFileText`: a 5 MiB text file; for offsets 1, the middle and the end, with and without `limit`, and an offset past the end, expect the exact text that the 4 MiB path gives for the same content (call `readText` on the same bytes; it is a package function).
2. `TestReadLargeFileAllocatesOnlyThePage`: a 32 MiB file; the growth of `runtime.MemStats.TotalAlloc` during one `read` stays below 4 MiB. Today it is above 64 MiB. Do not mark this test `t.Parallel()`, and call `runtime.GC()` before each reading.
3. `TestReadLargeSingleLine`: a 5 MiB file with no newline gives the existing `[Line 1 is <size>, exceeds <limit> limit ...]` text (`internal/tools/file_read.go:224-227`) with the full line size.
4. `TestReadLargeBinaryIsRefusedBeforeDecode`: a 5 MiB file with a NUL byte in the first 8 KiB gives the binary text.
5. A cancelled context stops the scan and returns the context error.
6. `TestReadRefusesImageOver20MiB`: a file that starts with PNG magic bytes and is 20 MiB + 1 byte gives the refusal text below and no image block; the `TotalAlloc` growth stays below 4 MiB (the file is not read whole; same rules as test 2: no `t.Parallel()`, `runtime.GC()` first).
7. `TestReadImageUpTo20MiBReachesPixelCheck`: the 4097 x 4097 PNG of step 5 (well under 20 MiB) read through `read` gives the 16 M pixel refusal, and a small valid PNG gives an image block. This proves that an image under the byte cap still goes through the pixel check.

Fix:

1. Add `readWholeMax = 4 << 20`. Read the first `binarySniffBytes` (8 KiB, which covers `imageSniffBytes`, `file_image.go:26`) and run `detectImage` and `looksBinary` on them first.
2. Add `readImageMax = 20 << 20`. An image whose `info.Size()` is above it is refused before the whole read, with `failf("Cannot read %s: the image is %s, over the %s image limit.", in.Path, formatSize(size), formatSize(readImageMax))`. An image of 20 MiB or less keeps today's whole read, and step 5 bounds its decode.
3. Text of 4 MiB or less: today's path (read the rest, `readText`).
4. Text over 4 MiB: a `bufio.Reader` scan that skips to `offset`, keeps lines up to the limit and up to `DefaultMaxBytes` + one line of output, measures (without storing) the first line when it is over the limit, and counts the remaining lines. Share the page and hint formatting with `readText`, so both paths give one text (DRY: split `readText` into "collect the page" and "format the page").
5. Check `ctx.Err()` between buffers. Keep `r.fs.Record(abs, info)`.

Command: `go test -run '^TestRead' ./internal/tools`

### 3. R7: `edit` fails when the file changed between read and rename

Evidence: under `PathLock` (in-process only) the order is `Check` (`internal/tools/file_edit.go:193`), `os.ReadFile` (`:199`), match, `WriteFileAtomic` (`:231`) with no second check. A write by another process in that window is lost. `Check` compares with the older record, so it cannot see this change.

Tests first in `file_edit_test.go`:

1. `TestEditFailsWhenFileChangesBeforeRename`: an unexported seam `editBeforeCommit func(path string)` (nil in production) runs before the rename; the test writes the file with the same size and sets a new mtime with `os.Chtimes`. Expect the "changed on disk since you last read it" error (`file_edit.go:195`) and the other writer's content on disk.
2. A row where only the size changes.
3. Regression: with no change, the edit is written.

Fix:

1. Open the file once, `Stat` the open file, and read from it, so the stat and the bytes belong together.
2. Give `WriteFileAtomic` an unexported variant that runs a check after the temp file is closed and before `os.Rename` (`internal/tools/file_atomic.go:44`). `WriteFileAtomic` keeps its signature and passes no check (`file_write.go:88` is unchanged, because the user kept the `write` contract).
3. The `edit` check stats the path and compares `ModTime().UnixNano()` and `Size()` with the first stat. On a difference it returns an error that wraps `ErrFileChanged`; `edit` maps it to the existing text.

Limit: a change inside the same mtime tick with the same size is not seen. Write this in `internal/tools/README.md` (phase 7).

Command: `go test -run '^TestEdit|^TestWriteFileAtomic' ./internal/tools`

### 4. N24: the skills listing budget counts escaped bytes

Evidence: `descBytes` counts raw bytes (`internal/skills/loader_listing.go:155`), while `writeEntry` writes the description through `ReminderText.Quote` (`:161`), which turns each `system-reminder` tag into `&lt;...` (`pkg/protocol/reminder.go:33`).

Test first in `loader_listing_test.go`: skills whose descriptions contain `<system-reminder>` many times, with a budget near the total; expect the listing to fit the budget.

Fix: measure the cut description through `Quote` (an empty `ReminderText`, as `fixedBytes` does at `:149-152`).

Command: `go test -run 'Listing' ./internal/skills`

### 5. N25: refuse an image above 16 M pixels before the decode

Evidence: `toInline` decodes BMP (`internal/tools/file_image.go:229`) and `decodeImage` decodes the other types (`:245`, `:326-341`) with no `DecodeConfig`. A small file can claim a huge canvas; the decode runs in the shared leader. These two calls are the only image decode paths in `internal`, `cmd` and `pkg`.

Accepted rule (Grok): read the header with `image.DecodeConfig`, refuse when width times height is above 16 M pixels, on every path.

Tests first in `file_image_test.go`: a PNG of 4097 x 4097 gray pixels (compresses to a small file) gives the new refusal text and no image block; a 2000 x 2000 PNG still returns an image.

Fix: at the top of `toInline`, call the `DecodeConfig` of the sniffed type; refuse with a new `imageError` text `"[Image omitted: the image has more than 16 million pixels.]"` when `w*h > 16<<20`; a header error gives `imageNotConvertible`. This text is model-visible, but it is a tool result, not a description, so no cassette changes.

Command: `go test -run 'Image' ./internal/tools`

### 6. N26: integer strings keep their exact value

Evidence: `parseNumber` uses `strconv.ParseFloat` (`internal/tools/coerce.go:264-273`) and `formatNumber` marshals the float (`:278-281`); `"9007199254740993"` becomes `...992`.

Accepted rule (Grok): parse as an integer first.

Test first in `coerce_test.go`: `"9007199254740993"` for `integer` and for `number` gives `json.Number("9007199254740993")`; `"1e3"` for `integer` still gives `1000`; an integer string above int64 for `integer` is not coerced (validation reports it), it is not rounded.

Fix: in the string case of `number`/`integer` (`coerce.go:196-200`), try `strconv.ParseInt(strings.TrimSpace(x), 10, 64)` first and return `json.Number(strconv.FormatInt(n, 10))`. For `integer`, a range error returns no coercion. Else keep the float path.

Command: `go test -run 'Coerce' ./internal/tools`

### 7. N27: the sandbox view records which limit cut the output

Evidence: `toolTruncation` sets `"lines"` when `ShownLines >= sandbox.DefaultMaxLines` (`internal/tools/shell_bash.go:300-303`); `sandbox.Truncation` has no field for the limit (`internal/sandbox/view.go:25-39`). A byte cut that keeps exactly 2000 lines is labelled `lines`.

Tests first: in `view_test.go`, output where the byte limit binds while the kept lines equal the line limit gives `TruncatedBy == "bytes"`; output with many short lines gives `"lines"`; output with dropped bytes (spill) gives `"bytes"`. In `shell_bash_test.go`, the details carry the label of the view.

Fix: add `TruncatedBy string` to `Truncation`. `cutHead` and `cutTail` report the limit that stopped them; `View` sets `"lines"` when a line limit stopped a cut, else `"bytes"`. `toolTruncation` copies the field.

Command: `go test -run 'View|Truncation' ./internal/sandbox ./internal/tools`

## Verification

- `go test -race ./internal/bootstrap ./internal/tools ./internal/sandbox ./internal/skills`
- `go vet` on the same packages; golangci-lint.
- `go test -run '^TestReferenceDocs$' ./cmd/tui` passes with no regenerate (no description changed).

## Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| R2 skips a valid monorepo link that points above the git root | Medium | Low | The skip is listed in `ask settings show` (`outside_project`) and in the session warning; the user decision accepts it. |
| R3 page text differs between the two paths | Medium | Medium | Test 1 compares both paths on the same bytes. |
| R3 time on a multi-GB file | Medium | Low | Linear scan with an 8 KiB buffer and context checks; no memory growth. |
| R3 image memory | Low | Medium | An image is read whole only up to 20 MiB (user decision); step 5 bounds its decode to 16 M pixels. |
| R7 false failure on a file system with coarse mtime | Low | Low | The first stat comes from the open file; only a real change between read and rename fails. |

## Rollback

Revert the phase commits. Revert phases 3 and 6 first if they use `SkipOutsideProject`, or keep them: their switches fall back to the default text.

## Docs impact

Phase 7: `docs/cli.md:147`, `:237`, `:336`, `:391`; `internal/bootstrap/README.md`; `internal/tools/README.md:52`, `:54`; `internal/sandbox/README.md`; `CHANGELOG.md` Fixed lines for R2, R3 (including the 20 MiB image limit), R7, N25, N26.
