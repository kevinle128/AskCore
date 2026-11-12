# Code review: review-fix commits 6d4bf8c..ac05eb3

Date: 2026-11-12. Branch `develop`. Scope: `git diff 6d4bf8c~1..ac05eb3 -- . ':!docs' ':!*.md'` (91 files, +2325/-179). Read-only review; no tests run (scope was reading and targeted greps). User decisions R6, R2, R3, N7, Q-write were treated as fixed. Only how they are implemented was reviewed.

## Findings

### M1 (Medium): PrepareRequest keeps only element replacement, not true in-place edits
- Where: `internal/pipeline/registry.go:277-290` (the `slices.Clone` and `reflect.DeepEqual` comparison).
- Defect: the clone is shallow. Messages are interface values with value receivers (`pkg/protocol/message.go:206-215,636`). Some are stored as pointers (for example `*protocol.CustomMessage`, see `internal/agent/tasks.go` `checkNoticeMessage`), and value messages hold slices (`Content`). A handler that changes a field through a pointer, or changes an element of a value message's `Content` slice (`um := msgs[0].(protocol.UserMessage); um.Content[0] = ...`), changes the shared backing data, and so changes `orig` too. So `DeepEqual` reports "unchanged", no update is returned, and the session's own stored message is changed silently. The commit message says "a request that a handler edits in place is compared with a copy taken before dispatch, so the edit is kept". That is true only for `Messages[i] = x`. The new test (`registry_test.go` `TestPrepareRequestKeepsInPlaceMessageEdit`) covers only that case.
- Failure scenario: a redaction or compaction handler changes a pointer message in place. The provider gets the edited text through the shared pointer, but the update is nil. The session history is now changed without a recorded update, and the next turn replays the changed text.
- Fix: deep-clone the messages before dispatch (`protocol.CloneMessage` per element, the same helper `enqueue` uses). Then edits through a pointer stay on the handler's copy and the comparison sees them. Add a test that changes a pointer message in place.

### M2 (Medium): cassette files are briefly world-readable, and a pre-existing capture folder is not tightened
- Where: `internal/providers/cassette/cassette.go:159-165`, `cmd/tui/capture.go` (`MkdirAll(dir, 0o700)`); go-vcr `pkg/cassette/fs.go:59,64` (`MkdirAll 0o755`, `os.Create`).
- Defect: go-vcr writes the first save with `os.Create` (mode 0666 masked by umask, usually 0644). The full payload is written before the new `os.Chmod(0o600)` runs. Later saves call `os.Create` on an existing file, which truncates it but keeps its mode. So the code comment ("creates the file again at each save with the default mode") is wrong, and the exposure window is the first save. `MkdirAll(dir, 0o700)` does not change the mode of a folder that already exists, such as `ASK_CAPTURE=/tmp/caps`.
- Failure scenario: on a shared host, `ASK_CAPTURE` points to an existing 0755 folder. Another user can read the first cassette (prompts, tool output, file contents) between the write and the chmod. They can also open it during that window and keep the file descriptor after the chmod.
- Fix: before the recorder starts, create the cassette file with `os.OpenFile(path, O_CREATE|O_EXCL|O_WRONLY, 0o600)`, so that go-vcr's later `os.Create` keeps 0600. For the capture folder, `os.Chmod(dir, 0o700)` after `MkdirAll`, or refuse a folder that the group or others can access. Keep the chmod after save as a second line of defense, and correct the comment.

### L1 (Low): `ask connect` finish ignores signals after the leader closes
- Where: `cmd/tui/connect.go` `finish`, branch `case <-c.done: <-settled`.
- Defect: the wait for `settled` has no `ctx.Done()` case. It depends on every in-flight prompt goroutine returning at once. A prompt goroutine that is blocked in `c.out.printf` on a full stdout pipe (the consumer stopped reading) never returns, and SIGINT/SIGTERM are then ignored.
- Fix: `select { case <-settled: case <-ctx.Done(): code,_ := signalExit(ctx); return code }`.

### L2 (Low): the `lost` flag depends on one failure kind
- Where: `cmd/tui/connect.go` `prompt` (`if f.Kind == "connection_lost"`).
- Defect: a prompt that fails for another reason while the leader goes away (for example a transport error mapped to a different kind, or a call cancelled through `c.ctx`) does not set `lost`. `finish` then prints `bye` and exits 0 although the reply never arrived. I did not verify the full set of kinds that `rpc.Conn` returns on EOF.
- Fix: set `lost` when `c.done` is closed and the prompt had no result, not only for one kind string. Or check `c.done` in `finish` after `settled`: any prompt that failed after `done` closed counts as lost.

### L3 (Low): edit re-check misses a replacement that keeps size and mtime
- Where: `internal/tools/file_edit.go` `unchanged` closure.
- Defect: the check compares only size and `ModTime().UnixNano()`. `cp -p`, `rsync -t`, `touch -r`, or a write on a filesystem with coarse timestamps (HFS+ 1 s, FAT 2 s, some network filesystems) gives the same pair. The other process's write is then lost by the rename.
- Fix: also require `os.SameFile(readInfo, now)` (this catches replacement by rename). Document the timestamp granularity limit.

### L4 (Low): Authorization header selection is not deterministic when two spellings exist
- Where: `internal/providers/openai/profiles.go:127-135` (`profileAuthorization` iterates over `m.Headers`).
- Defect: when `models.json` has both `authorization` and `Authorization` with different values, map iteration picks one at random for each request. The guard then accepts whichever was picked. I did not verify that the models.json decoder refuses keys that differ only in case.
- Fix: refuse folded duplicate header keys when `models.json` is decoded, or pick deterministically (for example the exact canonical key first).

### L5 (Low): PrepareRequest now deep-compares the whole history on every request
- Where: `internal/pipeline/registry.go:286-289`.
- Defect: the comment says DeepEqual "returns early for the same slice header", but the clone guarantees that the headers always differ. So every request with at least one PrepareRequest handler walks the whole message history, including the raw JSON and image bytes. The cost grows with the session length. This is not a correctness problem.
- Fix: correct the comment. Optional: a cheap pointer or length pre-check per element before `DeepEqual`.

### L6 (Low): context-file containment has a residual TOCTOU, and the pre-read stat does not stop a FIFO swap
- Where: `internal/bootstrap/load.go:97-114`, `internal/bootstrap/read.go:21-29` (`os.Stat` then `os.Open`, both follow links).
- Defect: the code reads the resolved path. A folder component inside the project can still be swapped for a link between `EvalSymlinks` and `Open`. A swap to a FIFO between `Stat` and `Open` blocks session start. Both cases need a local writer to the project in the same moment, so this is not a real remote risk. The second case is older than this diff.
- Fix (optional): open with `O_NOFOLLOW|O_NONBLOCK` and `fstat` the descriptor (the "safe reader" pattern that config already uses), then compare the path from `/dev/fd` or the `fstat` result.

### Informational (no action needed unless intended otherwise)
- I1: `fantasykit/fold.go:211-221`: a reply that has only thinking, or only whitespace text, with stop reason `stop` now fails with `CodeEmptyResponse`. Earlier, thinking counted as content. This matches the new comment. Confirm that callers retry this code or report it clearly.
- I2: `fold.go:176`: a final `ToolCall` part without an earlier `ToolInputStart` is still dropped (this is older than the diff). It now becomes an "empty response" error and not a silent empty success. That is better, but the dropped call should be checked separately.
- I3: `internal/acp/host.go:471-474`: `guard` now refuses `session/close` and `cancel` on a failed host. In stdio this is harmless (the process exits after `a.Fail`, `stdio.go:99`, and the host close disposes the sessions). It was not verified for the leader, where `ErrQueueOverflow` latches the host (`host.go:711`). If the leader keeps running after a latch, the sessions and their background jobs stay until host shutdown.

## Verified correct (brief)
- Continue: the decision and `r.closing = true` are in one `a.mu` critical section (`agent.go:269-273`). `enqueue` reads `r.closing` under the same lock and sets `wake` (`queue.go:344-348`), and `end` starts the next run under the lock (`agent.go:684-686`). No lost wakeup, no double run (one `a.active`), and the run slot is released by `defer a.end(r)` on every path. The failure path emits no event and does not call `notifyRunStart`. `in.len()` counts the same queues as `end`. The new tests use `synctest` and a probe, without timing.
- `pendingResponses` counting: the SDK writes one response for each request with an id (`third_party/acp-go-sdk/connection.go:575-581`). `closeAndWait` also returns on failure or close. The test is deterministic.
- `foldKey` (`leader/rpc.go:72-97`) gives the same result as `encoding/json` `foldRune` (the smallest rune of the `SimpleFold` orbit; for ASCII the uppercase letter). Both decode invalid UTF-8 to RuneError in the same way.
- Image limits: `toInline` is the only caller that decodes. The pixel check runs before any decode and has no overflow (`Width > max/Height`, `file_image.go:229-236`). The 20 MiB cap applies before the whole read, and `readRest` stops at limit+1 when the file grows.
- Read scan: the regular-file check is at `file_read.go:132`. Memory is bounded by `bufio` 64 KiB plus a capped line. ctx is checked for each chunk. CR handling works across `ErrBufferFull` boundaries. Small files and large files use the same `scanPage` formatter.
- Atomic edit: the check runs after the temp file is closed and before the rename. The temp file is removed when the check fails (`file_atomic.go:23-57`).
- R6: OAuth always uses `Bearer <token>` (`staticKey=false` for `openai-chatgpt`). A user header wins only for `api-key`. The guard checks the same value that `Isolate` sets. Errors contain no secret.
- N7: both decode paths default `SupportsReasoningEffort` to true (`session_models.go:244`, `model_config.go:431`, `catalog.go:68`). Compat replacement is whole-struct and older than this diff.
- baseUrl trim runs after `validBaseURL`, which requires a host, so a bare `/` is already refused (`models_file.go:175-179`).
- partialjson colon check, the replay signature type and id check, `quoteExtraOf` (a match of `reminderTag` can only end at `r`/`R`, `pkg/protocol/reminder.go:18`), the task registry closed pre-check, and `checkNoticeMessage` typed-nil handling are correct.
- Conventions: no `time.Sleep`, ticker, or polling in the added product code; `require.Eventually` is test-only. No plan ids or finding codes in the added code or test names.

- N7 constructors: every Completions-API model gets a `CompletionsCompat` with effort on. The sources are the catalog (`TokenPlanCompletions`, `catalog.go:57-68`) and `models.json` (`zeroCompat`, `model_config.go:264`, or `mm.compat`). The family rules make only Anthropic and Responses models (`catalog_rules.go:79,85`). So no path drops effort silently.
- max_tokens estimate (`anthropic/prepare.go:60-63`): bytes/4, plus 10 %, plus 64. Base64 image bodies were ASCII before too, so nothing changes for them. CJK text is now counted higher (intended).
- Leader setup cleanup: after `NewACPRuntime`, both early returns call `acpRT.Cleanup` (`module_leader.go:98,125`). The return at :83 is before the runtime exists.
- connect signal goroutine: it ends on the first signal or on `stop()` (`ctx.Done`), so it does not leak. The deferred `stop` runs on every return.
- Tests: none of the added tests set or read the real `~/.ask` (the connect test passes `ASK_HOME` through injected env) or use the network.

## Counts
Critical 0, High 0, Medium 2, Low 6, Informational 3.

Status: DONE_WITH_CONCERNS
Top risk: a PrepareRequest handler that edits a message's shared content in place still loses the edit without notice and changes the stored session history (M1). The commit message claims this case is fixed.
