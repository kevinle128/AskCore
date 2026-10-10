# Architecture — H5 built-in tools

Decision record of 2026-10-10. The question-by-question record is in [h5-builtin-tools.md](h5-builtin-tools.md). Analysis sources: [xia report](../reports/xia-261010-1727-h5-builtin-tools.md), roadmap H5 (`plans/260930-2254-pi-feature-inventory-go-roadmap/roadmap.md`), inventory rows H-TOOL-01 to H-TOOL-23 and H-SEC-03.

## Problem & goals

Ask's agent has only a demo `echo` tool. H5 gives it the tools of a coding agent so that, in print mode, it reads a repository, edits a file and runs its tests (roadmap H5 exit). The goals are a model-facing contract that stays compatible with Pi, behavior that is safe on the user's machine (processes are killed as a group, secrets do not leak into commands), and code that is small enough to maintain. Long-running commands must not block or be killed: they move to the background and the agent is told when they finish.

## Approaches considered

1. **Grok-faithful process layer** (actor that owns every process, strategy over local and ACP-client backends, sandbox decorator, three wake paths, re-key on handoff). Proven at scale, but large (one 6,000-line actor) and its own report lists the wake design as a weak point.
2. **Draft synthesis** (per-session task manager, actor per task, executor strategy with decorators, file-as-stdout spill, on-disk registry with a sweep, one wake path). Clean, but judged too complex for the need.
3. **DeepSeek-style job table** (chosen). Every `bash` call is a job; a job table per session with a mutex and a `done` channel per job; consuming reads; one completion listener. About 500 to 550 Go lines for the process and job part.

Recommended and chosen: approach 3, with one addition from Grok (a separate `monitor` tool) and Grok's notice format.

## Recommended approach

Tool contracts follow Pi (H5 contract of 2026-10-06), with omp and Grok improvements that do not change the contract. Commands run through a small Unix process runner in `sandbox`. A per-session job table in a new `jobs` package owns background work, the monitor and the notices to the Agent. Tools in `tools` are thin: they validate arguments, call the runner or the job table, and shape results (model text separate from UI details). The composition root creates the job table next to each session's Agent and closes it when the session closes, after the Agent drains.

## Key decisions

### Tool contracts (`tools`)

- Seven Pi tools: `read`, `write`, `edit`, `bash`, `grep`, `find`, `ls`; Pi limits of 2000 lines or 50 KB; Claude Code names where semantics match.
- `edit` keeps Pi's `edits[{oldText,newText}]` (Q1). Matching ladder: exact, then rstrip, trim and Unicode normalization, then a scored fuzzy line match; a unique match is required; the replacement is re-indented; errors name the closest match with its similarity and line; no-op edits are rejected, with a hard error after three identical ones; CRLF and BOM are preserved; overlapping edits are rejected (omp, Grok, Codex).
- New over both sources: an edit is rejected when the file changed since this session last read it (mtime and size, or a hash); writes are atomic (temporary file and rename).
- `read`: numbers on every line, binary detection, long lines capped only in the model view, a past-end hint, `Use offset=N to continue`.
- `write`: refuses to overwrite generated files; sets the executable bit for a shebang.
- `grep` uses `rg` per Pi with per-file caps and early stop; `find` uses `fd` when present, else `rg --files`, else a Go walk that respects `.gitignore`.
- Paths: `@`, `~`, `file://`, macOS screenshot name variants.

### Process runner (`sandbox`)

- `bash -c` in its own process group, stdin closed, explicit working directory from the session.
- Environment (Q12): inherited from the leader minus credential-shaped variables, plus `TERM=dumb`, `NO_COLOR=1`, pagers set to `cat`, `GIT_TERMINAL_PROMPT=0`, a no-op editor, and `ASK_*` session variables (D7).
- Output: one bounded buffer keeping the head 20 KB and the tail 30 KB inside 50 KB and 2000 lines (Q2), counting dropped bytes; a spill file (mode 0600, under the session directory) only on overflow, capped at 64 MB; past the cap the file stops growing, the job keeps running and the result says `[spill file capped at 64 MB]` (Q15).
- Kill: SIGTERM to the group, SIGKILL to the group after 3 s; the kill timer stays armed after the main process exits; the pipe drain is bounded by the same grace so a grandchild holding the pipes cannot block.
- No PTY and no Linux cgroup in H5 (defaults; reversible).

### Jobs (`jobs`)

- One job table per session: a map with a mutex; a job holds id (`bash-N`), label, status (`running`, `stopping`, `completed`, `killed`, `failed`), exit code or signal, buffer, read cursor, a `done` channel, kill reason, and the flags "awaited" and "killed by the model".
- Every `bash` call is a job. It waits up to `timeout` (default 120 s, cap 600 s); a job done in time returns a normal result and is removed; otherwise the call returns the job id with a "moved to background" text and the job keeps running; `run_in_background` starts in the background at once; a cancelled call kills the job (Q4 as revised by Q9). No background time limit.
- At most 10 active jobs per session (Q8); the error tells the model to use `job_kill`.
- Tools: `job_output(job_id, wait?, timeout_ms?)` returns output since the last read and a status line; `job_kill(job_id, reason?)`; `job_list()`; `monitor` (Q10).
- `monitor`: lines that match a pattern become events; a flood guard of about 10 events refilled 1 every 2 s, with a "[N events suppressed]" note; on sustained overload the process is killed, not only the reader.
- Notices (Q11, Q14): a finished job sends one notice through the Agent's `FollowUp`, skipped when the model already saw the end through a wait or its own kill, or when the session is closing; monitor events go through `Steer`, coalesced per delivery. A notice is a `user`-role message wrapped in `<system-reminder>…</system-reminder>`, hidden from the conversation view, and kept in the session log.
- Cleanup: closing the session kills and waits for all its jobs; leader shutdown closes every session. A leader crash can leave children running; this is accepted, as DeepSeek does (Q8).

### Boundaries & contracts

- Security posture (D3, Q3): no approval prompts and no built-in policy. A compiled-in extension `command-guard` (off by default, `ASK_COMMAND_GUARD=1`) registered through `pipeline.Registry.OnBeforeTool` blocks a short list of catastrophic commands; it tests the extension and denied-tool path and is not a security boundary.
- Secrets: credential-shaped variables never reach commands; spill files are 0600 and are removed with the session.
- Import rules: `sandbox`, `jobs` and `tools` are core packages and import neither `acp` nor `leader`; `jobs` reaches the Agent through a small interface with `FollowUp` and `Steer`, so it does not import `agent`.
- Lifecycle rule D19: the `bash` tool body drains until it returns; when a job moves to the background, the job table owns the process from then on under the session's lifetime, not the tool call's.
- Results reuse H2's contract: model-facing content, UI details (truncation range, next offset, spill path, exit code), and an ACP diff with whole-file old and new text plus `locations` for read and edit.

### Data model

Session 1 — 1 job table; job table 1 — 0..10 active jobs; job 1 — 0..1 monitor; job 1 — 0..1 spill file. All in memory; nothing persists across a leader restart.

### Other

- Build versus reuse: ripgrep and fd are external binaries with fallbacks; no Go ripgrep port.
- Tests: real short-lived processes, an injected clock for timers, `goleak.VerifyTestMain`, and after each test the process group must be gone (`kill(-pgid, 0)` returns `ESRCH`). Scenario checklist from DeepSeek and Grok: a command done in time leaves no job; a timed-out command keeps running and hands over its output; a cancelled call kills and waits; a grandchild holding the pipe does not block; SIGTERM then SIGKILL; the job cap; no notice after a wait or a model kill; monitor flood guard and overload kill; CRLF, BOM, multi-match and stale-edit cases.

## Missing pieces

- `sandbox` and `workspace` are documentation scaffolds with no code; both are new.
- `tools.Context` has no clock; the runner and jobs need an injected clock.
- The session factory must create and close the job table; the leader, the in-process client and print mode all go through it.
- The argument coercion table (D21) does not exist yet.
- TUI: hiding `<system-reminder>` messages and showing job status needs a TUI design (T1b, ask-tui-design rule).

## Spikes & experiments

```
Question:      Does a `<system-reminder>` user message with a hidden view keep the model's behavior right across providers (Anthropic, OpenAI, Token Plan)?
Spike:         One print-mode run per provider with a background job that finishes during a later turn, over half a day.
Decision rule: keep the Grok shape if every provider reacts to the notice and does not echo it as the user's words; otherwise add a provider-specific wrapper.
```

```
Question:      Is a mutex-guarded buffer with head and tail retention enough under heavy output (for example `yes`), or does it stall the child?
Spike:         A runner test with a command that writes as fast as it can for 10 s, measuring CPU, memory and the time to kill, over two hours.
Decision rule: keep the buffer if memory stays bounded and kill completes within the grace; otherwise switch to file-as-stdout for that case.
```

### Spike results (2026-10-10)

Spike 1, notice shape ([report](../reports/spike-261010-system-reminder-notice.md)): 5 tagged runs (gpt-5.5 once, gpt-5.6-sol twice, claude-sonnet-4-6 twice) and 1 untagged control (claude-sonnet-4-6). Every run read the job output, reported the failure correctly, did not address the user as the author and did not echo the tags. The Sonnet control behaved the same as the tagged runs, so the tags neither helped nor hurt there; gpt-5.5 and gpt-5.6-sol have no control (the call cap ran out); Alibaba was not tested (key expired). Decision: keep the Grok shape; no provider needs a wrapper. Design input: models poll `job_output` while a job is running (gpt-5.6-sol 4 or more times, Sonnet 1 to 5 times) with or without the notice, so `job_output` must answer a running job at once and say that a notice will arrive, and the `bash` description must say the same (DeepSeek has this prompt text).

Spike 2, output buffer ([report](../reports/spike-261010-output-buffer.md)): the mutex-guarded head and tail buffer stays bounded (peak heap 1 to 4 MB with 58 GB of `yes` output in 10 s), does not stall the child (5.8 GB/s through the pipe), and every kill finished within the 3 s grace, including a child that ignores SIGTERM and a grandchild that holds the pipe. Decision: keep the in-memory buffer for every case. Facts for the plan: 32 KB reads, one reader goroutine per stream, `os.Pipe` files as stdout and stderr, a 3 s bounded drain, and tests that check the process group is gone (`ESRCH`) after cleanup, not at `Wait` return. File-as-stdout was slower for the child and grew without bound. macOS only; Linux not run.

## Open questions

- The exact credential-name filter for the environment (which patterns); settle it in the plan with a test list.
- How the TUI shows job status and hides notices; settle it in a T1b TUI design.
- Whether the monitor flood guard numbers (10 events, 1 per 2 s) fit real dev-server logs; settle them in the spike above or the first real use.
