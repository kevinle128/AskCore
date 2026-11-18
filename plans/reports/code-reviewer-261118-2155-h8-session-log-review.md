# Review: H8 session log (uncommitted changes)

Scope: JSONL store (`internal/store/jsonlstore`), codec and ids (`internal/sessions`), agent wiring (`internal/agent`), composition (`internal/app`), `cmd/tui`, ACP load (`internal/acp`), router (`internal/leader/router.go`), `internal/tuiclient`, `internal/tui`.

Checks run:
- `go build ./...` is clean.
- `go test -race -count=1` passes for store, sessions, leader, acp, app, agent and tuiclient.
- `internal/tui` has one race in `TestModelCycleKeyIgnoredWhileSwitching`. It is in the test rig (`session_rig_test.go:168` against `:598`, a cleanup closing a channel that `fakeClient.push` still sends on). The diff does not touch that file, so I treat it as a pre-existing test flake.
- I built `ask` and ran it against a scratch `ASK_HOME` for finding 1.

Overall: the JSONL writer is sound. I found no data loss or corruption in the normal failure cases. Lock release was right on every error path I traced (`Open`, `openLocked`, `tryStore`, `Create`, `discard`, `Close`). `O_CLOEXEC` comes from `os.OpenFile`. Ids and cwd cannot traverse paths: `validID` limits ids and `encodeCwd` removes separators. The only proven defect of weight is finding 1.

## Findings (ranked)

### 1. MEDIUM - Saving is on by default with no fallback: an unwritable Ask home now fails the run (verified)
`internal/store/jsonlstore/writer.go:154` (`MkdirAll`) and `:202` (`os.Link`), reached through `loop.commit` (`internal/agent/loop_run.go:239`).
- Verified run: `ASK_HOME` set to a scratch directory with mode `0500`, then `ask -p hello` in a scratch cwd.
  - Default: no reply. Stderr shows `agent: the session file can no longer be written and must be reopened: sessions: writer lost` and `mkdir .../home/sessions: permission denied`.
  - `--no-session` on the same home: prints `hello`. Other home writes (settings, task-output sweep) only warn, so the old behavior was a working run.
- `ask -p` is lazy: nothing is created at start. The first assistant message commit fails and its reply is dropped.
- `CreateNow` callers (ACP `session/new`, TUI start, Reset) fail at session open: `New` returns `agent: open session log`. I read this path but did not run it.
- The product runs as a remote agent in the cloud, where a read-only or missing HOME is common. File systems with no hard links (some FUSE, SMB, FAT) fail the same way at `os.Link`; I did not test that.
- Fix: probe or degrade at start. For example, when the first store attempt fails, fall back to `MemoryLog` with one warning, or fail with a text that names `--no-session`.

### 2. LOW - Resume reuses the login method of an older model
`internal/agent/selection_log.go:84-96` (`LoggedSelection`) into `internal/app/default_model.go` (`restoredModel`) and `internal/app/agent_native.go:209-217`.
- `method` is the newest `AttemptSettled` that has a binding, whatever model that attempt used. A later `ModelChange` does not clear it.
- Proven path: session runs on `openai` (binding method `oauth`), the user switches to the demo `faux` model and runs once (`faux` settles with no binding, `lifecycle.go:176`). `ask -p -c`: `restored.Model.Provider == "faux"`, so `printStartModel` skips the model list, but `ChooseStartModel` still sets `ready`. `nativeAuthReady` returns `auth.ErrMethod` for faux with a non-empty method. Resume fails with "the session used faux/... with the login method oauth".
- I did not prove the "real provider attempt that failed before a credential was bound" variant, so I rate this Low. The path needs the demo model mid-session.
- Fix: reset `method` on each `ModelChange`.

### 3. DESIGN QUESTION - `-c` picks the newest stored file, which can be an unused session
`internal/app/session_log.go:83-95` (`Latest` takes `headers[0]`), `internal/app/session_log.go:156` (Reset creates `CreateNow`), `internal/app/module_acp.go:161` (`CreateNow: true`).
- `plans/261118-1818-h8-session-log/plan.md:116` says ACP `session/new` and `/new` store at once so that `-c` finds them. So this is intent, not a defect.
- Trade-off: a `CreateNow` file holds the header plus seed entries (`openSource` calls `seedLog` after `Create`), so "no entries after the header" is not the test. Scenario: chat in session A, quit; run `ask`, quit at once (leader keeps the new session live and stored); `ask -c` opens the empty session, not A.
- Options: `Latest` skips sessions that hold no user or assistant message; or keep as is. Needs the owner's call.

### 4. LOW/MEDIUM - File I/O under `a.mu` and `emitMu` in `swapContext`
`internal/agent/agent.go:555` calls `openSource` while holding `a.mu`; the field comment says `mu` "is never held while ... the log ... runs".
- `openSource` now does `Create(CreateNow)` (`MkdirAll`, write, fsync, link, dir fsync) plus the `closeOpenScopes` and `seedLog` appends.
- Scenario: `Reset` on a slow or network disk blocks `State`, `Notify`, `Subscribe` and task notices for the whole I/O time.
- Fix: open and seed the new log before taking `a.mu`; swap the pointers under the lock.

### 5. LOW - Router: a stale join response can be taken as the response of a newer load
`internal/leader/router.go` (`finishLoadSession`): it picks its branch from `s.loading` of `r.sessions[tag.session]` at response time, not from the tag. The tag only carries `joined`.
- The SDK runs each request in its own goroutine (`third_party/acp-go-sdk/connection.go:415`), so a slow join (long replay) can answer after a quick `session/close`.
- Sequence: client B joins live session S (in flight); client A closes S (router forgets it); client C loads the same id (new `loading` entry); B's join response arrives.
- Result: the router treats it as C's load response. It sets `s.gen` from the old host session and clears `loading`. C's real response then goes through `finishJoin`. C can hold a stale driver generation.
- Needs three actions in one short window, so Low. Hardening: compare the session pointer in the tag with `r.sessions[id]`.

### 6. LOW - A failed directory sync after the link leaves a stored file but reports "lost"
`internal/store/jsonlstore/writer.go:202-213`: `os.Link` succeeds, `syncDir` fails, `abort` unlocks and returns the error, but `logPath` stays on disk with the header and the held entries. The agent was told the reply failed; `List` and `-c` still see it.
- Fix: `os.Remove(logPath)` before the abort, or treat the sync error as non-fatal after the link.

### 7. LOW - New parent folders are not fsynced
`internal/store/jsonlstore/writer.go:154,211`: `syncDir` covers only the cwd folder. A newly made `sessions/` or `--cwd--/` entry is not synced in its parent, so a power loss can drop a fully fsynced first session. Process kill is not affected.

### 8. LOW - The "owned" text points the headless user at the wrong command
`internal/app/session_log.go:192` and the `ErrSessionInUse` text in `pkg/protocol/acp.go`: `ask -p -c "next"` against a leader-held session says "open it with `ask -c`". Headless opens in process and cannot join; only the TUI `ask -c` joins through the leader.

## Checked and not defects
- Torn tail, partial multi-line batch, truncate-on-open: `scan` keeps the committed prefix. A bad line followed by a `TurnClosed` gives `ErrSessionCorrupt`. Ignorable lines have no log position (tests pin it).
- First-write link race: `flock` on `<id>.lock` serializes, then `link()` returns `EEXIST` and a new id is taken. Cross-process and same-process double open both return `ErrSessionOwned`.
- Resume repair: `closeOpenScopes` runs in `New` and in Reset; the order is results, `TurnClosed`, `CycleClosed`. A closed log is unchanged. A kill mid-stream leaves a `RequestDelta` with no `AttemptSettled`; that is safe because attempt ids are random (`newRunID`), `rebuildRequest` finds by id with no settle needed, and `LoggedSelection` takes the model from the delta. `AttemptStart.Number` restarts at 1 after a resume (`a.attempts` is not persisted); this is an event number only.
- `seedLog` skips a seed equal to the newest logged item with timestamps ignored. `MessagesOf` applies `ContextEdit` correctly (entries from position 0, latest edit wins, edits before the attempt only).
- Error mapping: `loadError` gives fixed client text for owned, not found, corrupt and newer-format. Raw I/O errors fall to the adapter's generic internal error.
- Router, ordered case: `loading` blocks other calls with `busy`; failure forgets the session; alias join drops the loading entry; a refused join removes the added membership; replay frames go only to the named client.
- Pre-existing, out of scope: the Windows build fails (`lock_unix.go` has no non-unix file; `internal/settings` fails the same way).

## Unresolved questions
- Finding 3: is "newest stored file, even if unused" the intended `-c` rule?
- Finding 1: should a store failure end the run or degrade to memory with a warning?
