# Grok CLI comparison for the triage issues (2026-11-10)

Source: `/Users/dale/Desktop/workspace/opensources/grok-build` at `2bdd1d6a`. All paths below are relative to `crates/codegen/`. Read only; no grok code was run.
Ask background: [triage summary](./triage-261110-1809-ocr-summary.md).

Summary: Grok has a direct lesson for R1 (stranded steer input) and R6 (header order). For R3, R7, and Q-write, Grok has the same gaps as Ask, so the Ask findings stay at their severity. Grok has no equivalent for R5 and R8.

## R1: continue/resume with nothing to run while input arrives

Grok behavior:
- There is no separate "continue" call. A session actor owns one FIFO (`state.pending_inputs`) and one `running_task` slot. `maybe_start_running_task` first checks under the state lock and returns when a task runs or the queue is empty (`xai-grok-shell/src/session/acp_session_impl/notification_drain.rs:119-157`). It does config and memory I/O outside the lock, then takes the lock again and checks the same conditions again before it pops the front item (`notification_drain.rs:205-211`, `279`).
- So the check comes before the claim, and the slot is never held for an empty run. The actor calls this pump after each event that can add input or end a turn, for example a queue removal (`run_loop.rs:1086-1092`) and a turn end (`run_loop.rs:2086`, `2124`, `2183`).
- Steer (interjection) input goes into the running turn only if a turn runs. When the session is idle, it becomes a queued prompt and the actor pumps the queue (`run_loop.rs:2057-2090`). The comment names the race: the client "running" state can be stale at turn end (`run_loop.rs:2067-2069`).
- At turn end, an interjection that missed the final drain moves to the front of the queue as its own prompt turn (`run_loop.rs:2384-2390`; `interjection.rs:26-29`, `85-95`). The cancel path does the same (`cancel.rs:139`).

Lesson for Ask: Do the check before taking the run slot, then re-check after taking it, as Grok does; never return early while holding the slot. Every exit path of a run must also move input that arrived late into the next run (Grok's "flush stranded interjections"), so that no queued input waits in an idle Agent.

## R2: project context-file symlinks

Grok behavior:
- Discovery uses `path.exists()`, which follows symlinks (`xai-grok-agent/src/prompt/agents_md.rs:46-54`). The read uses `std::fs::read_to_string`, which also follows symlinks, and has no size cap (`agents_md.rs:342`).
- Grok does not check where a symlink points. The only use of the canonical path is de-duplication (`agents_md.rs:81-82`, `127-160`). A test puts a `rules/alias.md -> ../AGENTS.md` symlink in the repo and expects one entry (`agents_md.rs:946-975`).
- The control is folder trust: every project-scope file (the cwd-to-git-root chain) is dropped when the folder is not trusted (`agents_md.rs:315-317`; source kinds at `agents_md.rs:30-41`). User-configured rule dirs are not trust-gated.
- The content is escaped so that a file cannot forge harness tags (`agents_md.rs:382-387`).

Lesson for Ask: Grok gates the whole project context set on folder trust and does not restrict symlink targets. Ask loads `AGENTS.md` with no trust decision, so the proposed "skip a target outside the git root" check is stricter than Grok and is the minimum for Ask; an in-repo symlink can still load, as Grok allows.

## R3: `read` on very large files

Grok behavior:
- `classify_before_io` runs before the read but only records telemetry; it does not check the size (`read_file/observation.rs:36-55`). The `read_file` tool then calls `fs.read_file(&path)` (`xai-grok-tools/src/implementations/grok_build/read_file/mod.rs:451`). For the local backend this is `tokio::fs::read`, an unbounded whole-file read (`xai-grok-tools/src/computer/local/file_system.rs:138-152`; `xai-grok-tools/src/util/file_reader.rs:41-43`).
- All caps apply after the read: 1,000 lines by default (`read_file/mod.rs:69`, `627-633`), an optional byte budget (`read_file/mod.rs:641-651`), and a 25,000-token cap that returns an error with an offset/limit hint (`read_file/mod.rs:66-74`, `652-700`).
- Binary detection uses the extension and the content (`read_file/mod.rs:580-592`). PPTX has a 50 MiB cap (`read_file/mod.rs:95`).
- A bounded read exists: `read_file_bounded` reads at most `max_bytes + 1` bytes with `take()` and returns `FileTooLarge` (`computer/types.rs:56-72`; `util/file_reader.rs:44-79`). The `read_file` tool does not use it; the MCP file input does (`xai-grok-shell/src/session/acp_session_impl/mcp_file_input.rs:461`).

Lesson for Ask: Grok has the same whole-file read, so it is not a model for the fix. Its `read_file_bounded` (read `max+1` bytes through a limited reader, fail when over) is the simple pattern to copy for Ask's size cap.

## R4: envelope parsing (duplicate or case-folded keys)

Grok behavior:
- The leader IPC uses length-prefixed frames with a 64 MiB cap (`xai-grok-shell/src/leader/protocol.rs:9`; frame read at `protocol.rs:86-100`). The outer envelope is a typed serde enum, `#[serde(tag = "type", rename_all = "snake_case")]` (`protocol.rs:483-500`). `protocol.rs` has no `deny_unknown_fields`.
- The ACP payload is an opaque `String` in the envelope (`protocol.rs:492-494`). For routing, the leader parses it into `serde_json::Value` and reads the session id (`xai-grok-shell/src/leader/server.rs:1794-1821`). It does no duplicate-key check.
- The ACP stdio path re-encodes a line only to fix escaped slashes, and passes other lines through unchanged (`xai-acp-lib/src/normalize.rs:30-70`).
- Library behavior, not verified in this repo: serde field names are case-sensitive, so a case-folded key does not match; a derived struct rejects a duplicate field, but `serde_json::Value` keeps the last value. Thus Grok can have the same router-versus-agent difference on duplicate keys.

Lesson for Ask: Grok does not defend against this, which supports Low severity. Because Ask's router reads `_meta` from a parse that differs from the agent's parse, the proposed `hasFoldDuplicate` check is a cheap improvement over Grok, not a parity item.

## R5: executable check before spawning the leader

Grok behavior:
- `resolve_binary_impl` returns `current_exe()`, or the managed `~/.grok/bin/grok` symlink when the running binary is under the Grok home (`xai-grok-shell/src/leader/mod.rs:1568-1602`). `path_is_under` canonicalizes both paths only to compare them (`mod.rs:1604-1608`).
- `spawn_leader_subprocess` runs `Command::new(exe)` with no mode, owner, or symlink check on the binary (`mod.rs:1626-1628`).
- Grok checks permissions on the lock directory instead: it must be a real directory (not a symlink), owned by the euid, with no group or other bits; it is tightened to 0700 or refused (`xai-grok-file-lock/src/slot.rs:117-145`).

Lesson for Ask: no equivalent; Grok does not check the binary at all. Ask's `0002` mask is already more than Grok does, so fix the comment to match the code, and do not add a stricter check that would break umask 002 builds.

## R6: custom provider headers versus the resolved API key

Grok behavior:
- The sampler client first sets the key (`x-api-key` or `Authorization: Bearer`) (`xai-grok-sampler/src/client.rs:515-540`). Then it applies `extra_headers` with `HeaderMap::insert`, which replaces a header of the same name. The comment calls this "the single injection point for proxy-auth headers" (`client.rs:543-551`). Env-sourced headers are applied after that and also replace (`client.rs:553-558`, `277-295`).
- So for a static key, a user `Authorization` header wins by design.
- For a refreshing credential (a bearer resolver), each request removes `Authorization` and `x-api-key` and inserts the live bearer, so the resolved credential wins (`client.rs:697-715`).
- Logs redact any header whose name contains `authorization`, `api-key`, `apikey`, `token`, or `secret` (`client.rs:815-822`).

Lesson for Ask: Grok treats a user `Authorization` header as a supported proxy-auth override for static keys, which supports the DESIGN answer to decision 3 if the Ask docs say that it wins. Ask should choose one rule for both the Completions and Responses paths; today they differ, and that difference is the real defect.

## R7: `edit` race with an external writer

Grok behavior:
- `search_replace` reads with `fs.read_file` (`xai-grok-tools/src/implementations/grok_build/search_replace/mod.rs:529`) and writes with `fs.write_file` (`search_replace/mod.rs:719`). There is no stat or hash check between the two.
- The local write is a plain `tokio::fs::write` with retries on transient lock errors; it is not an atomic rename (`xai-grok-tools/src/computer/local/file_system.rs:56-86`, `183-206`).
- A `FileOperationLockManager` serializes in-process file operations (`xai-grok-tools/src/implementations/editor_infra/file_operation_lock.rs:1-11`), but outside `editor_infra/` no code calls it at this commit. It cannot see other processes in any case.
- When `old_string` does not match, the error tells the model that the user may have changed the file (`search_replace/mod.rs:113-117`).
- Only memory v2 files have a real guard: the read stores a blake3 hash (`xai-grok-memory/src/v2_access.rs:413-426`), and the write re-reads and fails with `ReadRequired` or `Stale` when the hash differs (`v2_access.rs:486-499`), then writes atomically (`v2_access.rs:1-5`).

Lesson for Ask: Grok accepts this race for ordinary files, so R7 stays Low. If Ask fixes it, the memory v2 pattern (hash at read, compare just before the rename) is cheaper and more reliable than mtime alone.

## R8: invalid or empty messages injected into the agent

Grok behavior:
- Rust has no typed nil, so Ask's defect class does not exist in Grok. Injected items are owned values: `PendingInterjection` entries (`interjection.rs:14`) and `InputItem` queue entries (`interjection.rs:46-60`).
- Empty checks are at the call sites: a task-completion reminder is pushed only when its text is not empty (`notification_drain.rs:102-104`), and a queue edit with empty text is a no-op (`prompt_queue.rs:998-999`). The `Interject` command handler has no empty-text check (`run_loop.rs:2057-2090`).

Lesson for Ask: no direct equivalent. The Grok pattern (owned values plus an empty check where the notice is made) suggests that Ask should reject a nil or empty message at the one injection entry point with a generic check, and not add checks per type.

## R9/R10: hooks that change the request or the tool result

Grok behavior:
- Hooks are external commands that return a result object. They do not get a reference to the live request. A PreToolUse result carries `updated_input: Option<InputRewrite>` with a new JSON map (`xai-grok-hooks/src/dispatcher.rs:57-75`).
- A rewrite is validated and then applied as a new value: Grok builds a new envelope and a `GateRewrite` that keeps the hook name (`xai-grok-shell/src/session/acp_session_impl/tool_calls.rs:1349-1375`).
- A PostToolUse result carries an explicit `output_replacement`; for several hooks the later one wins, and blocks and context append (`dispatcher.rs:630-672`).
- Change detection is "the hook returned `Some(...)`", not a compare of the old and new values.

Lesson for Ask: Grok avoids in-place edits by its design (a returned replacement value). For Ask's in-process handlers, cloning the slice and the result before dispatch (the proposed fix) gives the same property; an explicit "replaced" return value is the stronger option if handlers are added later.

## R11: capture, session, and debug files on disk

Grok behavior:
- Session directories are created owner-only (`xai-grok-shell/src/session/storage/jsonl/mod.rs:194-210`). Files in them use `OpenOptions` with no explicit mode (`jsonl/mod.rs:427`, `551`), so the 0700 directory is the protection.
- The lock slot directory must be 0700 and owned by the user (`xai-grok-file-lock/src/slot.rs:117-145`), and slot files use 0600 (`slot.rs:303`). Heap profiles use a 0700 directory and 0600 files (`xai-grok-shell/src/heap_profile/monitor.rs:633`, `657`). The telemetry id file is set to 0600 (`xai-grok-telemetry/src/id.rs:205`). The workflow registry writes 0600 with `O_NOFOLLOW` (`xai-grok-shell/src/session/workflow/registry.rs:622`).
- The leader log is opened with no mode, so it gets the umask default (`xai-grok-shell/src/leader/mod.rs:1611-1622`).

Lesson for Ask: Grok makes files that hold conversation data private, by a 0700 directory or 0600 files. The proposed `ASK_CAPTURE` fix (0700 directory, 0600 cassette) matches Grok's practice for session data.

## Q-write: stale-read guard on write/create

Grok behavior:
- The grok_build toolset has no separate write tool; `search_replace` with an empty `old_string` creates or overwrites a file (`search_replace/mod.rs:4`, `284-331`). By default it overwrites a non-empty file; the opt-in `empty_old_string_does_not_override` refuses that (`search_replace/mod.rs:62-67`, `103-107`, `306`). This check is "file is not empty", not "file changed since read".
- The opencode-compatible `write` tool reads the old content only for the diff and then writes (`xai-grok-tools/src/implementations/opencode/write/mod.rs:119-159`).
- Only memory v2 writes need an unchanged read snapshot (see R7).

Lesson for Ask: Grok, like Pi, does not refuse to overwrite a changed file. If Ask adds the Claude Code guard, the memory v2 snapshot map (path to content hash, set by `read`, checked by `write`/`edit`) is the reference shape.

## Limitations and unresolved questions

- The GitNexus index was not used; every claim comes from the local files at `2bdd1d6a`. Upstream changes after this commit were not checked.
- R4 statements about serde duplicate-key and case behavior come from library knowledge, not from code or tests in this repo.
- R8: an empty check for `Interject` can exist in the ACP extension handler before the actor; that layer was not read.
- R11: the mode of `create_dir_all_owner_only` was taken from its name and comment, not from its body; the leader log directory mode was not checked.
