---
title: "H5 built-in tools"
description: "Give the Ask agent the seven Pi coding tools, a process runner, a per-session job table with background jobs, a monitor and completion notices, so that in print mode the agent reads a repo, edits a file and runs its tests."
status: in-progress
priority: P1
effort: "20.5d"
branch: new-session
tags: [feature, backend, tools, sandbox, jobs, acp, tdd]
blockedBy: [261010-1424-t1c-provider-setup]
blocks: []
created: 2026-10-10
mode: deep
tdd: true
---

# H5 built-in tools

## Outcome

`ask -p` and every leader, `ask acp` or in-process TUI session get the tools `read`, `write`, `edit`, `bash`, `grep`, `find`, `ls` with Pi's contract and limits, plus `job_output`, `job_kill`, `job_list` and `monitor`.
Commands run in their own process group with a filtered environment; a command that runs past its wait budget moves to the background and the agent gets a `<system-reminder>` notice when it ends.
Closing a session kills its jobs; no process group survives a normal exit or a signal exit of `ask -p` (checked with `kill(-pgid, 0) == ESRCH`).
Roadmap exit: in print mode the agent reads a repository, edits a file and runs its tests, with a real provider.

## Authority and context

Read these before a phase starts. They are the authority; this plan only orders the work and names the tests. Every phase file has a "Context" list that links the exact sections it depends on.

| Link | Read it for |
|---|---|
| [Approved architecture](../architectures/h5-builtin-tools-architecture.md) | **The authority.** Key decisions per package (`tools`, `sandbox`, `jobs`), boundaries, data model, spike results, open questions. |
| [Decision record Q1-Q15](../architectures/h5-builtin-tools.md) | User decisions with supersession notes (Q9 replaces the Grok wait budget of Q4; Q7 replaces the first draft). Fixed: do not reopen. |
| [xia: H5 tools from omp and Grok](../reports/xia-261010-1727-h5-builtin-tools.md) | What to take from omp and Grok per tool ("What to take" table), what to skip, with source citations. |
| [xia: T1b TUI surfaces](../reports/xia-261010-1642-t1b-tui-surfaces.md) | TUI surfaces that depend on H5: tool output display (row 1), `/copy` (row 3), notices. H5 does not build them. |
| [Spike 1: notice shape](../reports/spike-261010-system-reminder-notice.md) | Keep the Grok `<system-reminder>` shape; models poll `job_output`, so it answers at once and says a notice will arrive. |
| [Spike 2: output buffer](../reports/spike-261010-output-buffer.md) | "Facts the plan must keep": 32 KB reads, one reader per stream, `os.Pipe` ends as stdout and stderr, 3 s bounded drain, ESRCH after cleanup. |
| [Roadmap H5 section](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#h5-built-in-tools), [decisions D3, D7, D8, D19, D21, D25-D29](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#1-decisions-in-the-order-they-block-phases), [T1 rows](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#t1-tui-core) | Scope, exit criterion, H6 boundary (hard-coded defaults), process-kill rule (D19), `Remove` (D27), `Dispose` (D29), T1b link. |
| [Inventory rows H-TOOL-01..07, 11, 19, 22, 23, H-SEC-03](../260930-2254-pi-feature-inventory-go-roadmap/inventory-harness.md) | Pi parameters, limits and messages per tool (lines 51-57, 61, 69, 72, 73, 271). |
| [Architecture reference](../../docs/ask-architecture-reference.md) section 8 and 9, [internal/README.md](../../internal/README.md) | Import rules (depguard), where new code goes. |
| [LLM cassette testing](../../docs/testing-llm-cassettes.md) | Test layers; why `bash` runs never become cassettes; re-record rules. |

## Decisions

### User decisions (fixed)

Q1-Q15 in the [decision record](../architectures/h5-builtin-tools.md) and D3, D7, D8, D19, D21 in the roadmap. Short form used in this plan:
Pi `edits[{oldText,newText}]` (Q1); bash model view head 20 KB + tail 30 KB (Q2); `command-guard` compiled-in, off by default, `ASK_COMMAND_GUARD=1` (Q3); every `bash` call is a job, `timeout` = wait budget, default 120 s, cap 600 s, `run_in_background`, no background time limit (Q4 as revised by Q9); DeepSeek-style job table (Q7); no orphan sweep, cap 10 active jobs per session (Q8); separate `monitor` tool like Grok (Q10); monitor events by `Steer`, completion by `FollowUp` (Q11); environment inherited minus credential-shaped names plus non-interactive and `ASK_*` variables (Q12); packages `sandbox`, `jobs`, `tools` (Q13); notice = `user` message in `<system-reminder>`, hidden from the conversation view, kept in the log (Q14); spill file capped at 64 MB, job keeps running (Q15); no PTY, no cgroup (defaults); `monitor` contract exactly as Grok: `monitor(command, description, timeout_ms?, persistent?)`, no pattern, every stdout line is an event (N3, user 2026-10-11); `read` handles images exactly like Pi, with conversion and auto-resize (user 2026-10-11; this pulls H-TOOL-20, P1, into H5; `images.autoResize` = true and `images.blockImages` = false are fixed at Pi's defaults and H6 makes them settings).

### Plan decisions (planner, verified in code)

| # | Decision | Evidence |
|---|---|---|
| P1 | The D21 coercion table exists and runs in `Prepare`; H5 adds no coercion code. `edit` adds an `ArgumentPreparer` shim for Pi's legacy input (top-level `oldText`/`newText`, `edits` as a JSON string or one object). The xia matrix row "Argument coercion table: NEW" is stale. | `internal/tools/coerce.go:182-184` ("the D21 table"), `internal/tools/validate.go:28-40`, `internal/agent/tool_coordinator.go:200-208` |
| P2 | Close carrier (revised by red-team RT2): `agent.Config.Release func() error`, called once by `Agent.dispose` after the run and its started tools drained and before the log closes. `newNativeAgentOn` sets it to the job table's `Close` and also returns the table (print mode needs it for the exit sweep, P18). `acp.Factory` and `ACPParams.NewAgent` do not change: `Session.Close` and the signal path already call `Dispose`, and print mode now calls it at a normal end. Not an `AgentDisposed` listener: listeners must return quickly, and a table close waits up to one 3 s grace plus the drain bound. | `internal/agent/agent.go:248-285` (`<-r.done` :261, `src.Close` :263), `internal/acp/host.go:42,902-918`, `internal/app/module_acp.go:49,117-129`, `cmd/tui/acp.go:133-134`, `cmd/tui/headless.go:242-286,322-381`, `internal/agent/types.go:60-63` |
| P3 | `jobs` reaches the Agent through `Notifier{FollowUp, Steer, Remove}`, set after the Agent exists (late binding). `Remove` (D27) lets the table replace a still-pending monitor message with one merged message ("coalesced per delivery"). | `internal/agent/queue.go:152-216` |
| P4 | Injected clock: `sandbox.Clock` with `Now()` and `AfterFunc(d, f) (stop func() bool)`; `jobs` reuses it. `testing/synctest` is not used: a goroutine in a pipe read is not durably blocked, so fake time would not move. | `internal/providers/faux/faux.go:28-39` (same pattern) |
| P5 | Spill directory: one per session, created on the first spill with `os.MkdirTemp("", "ask-jobs-*")`, mode 0700, files `<job-id>.log` mode 0600, removed by the table's `Close`. Ask has no session directory yet (the log is in memory). An unused table owns no file. | `internal/sessions` in-memory writer (`docs/ask-architecture-reference.md:187`) |
| P6 | Child environment filter: drop every inherited name that matches `(?i)(KEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL)` and every inherited `ASK_*` name (DeepSeek drops its own prefix); then add `TERM=dumb`, `NO_COLOR=1`, `CI=true`, `PAGER=cat`, `GIT_PAGER=cat`, `MANPAGER=cat`, `GIT_TERMINAL_PROMPT=0`, `EDITOR=true`, `VISUAL=true`, `GIT_EDITOR=true`, `SSH_ASKPASS=` (empty), and the session variables `ASK_SESSION_ID`, `ASK_PROVIDER`, `ASK_MODEL`, `ASK_REASONING_LEVEL`. `ASK_SESSION_FILE` is not set until H8 adds a session file. The test list is in phase 3. | DeepSeek `subprocess/src/index.ts:47` (`/KEY|PASSWORD|SECRET|TOKEN/i`); omp `non-interactive-env.ts` (xia "bash" row); H-TOOL-22 |
| P7 | Provider, model and thinking level reach the runner through a closure from the composition root that reads `Agent.State()` at each job start; `tools` and `jobs` must not import `agent`. The base environment is injected (`runDependencies.environ`, nil = `os.Environ`) so tests do not change process env. | `.golangci.yml:116-126`, `internal/agent/agent.go:433-443`, `cmd/tui/headless.go:48-54` |
| P8 | Prompt snippets (H-TOOL-11): optional interface `tools.PromptContributor{PromptSnippet() string; PromptGuidelines() []string}`; `tools.PromptSection(*Snapshot)` renders the tool list and guidelines; `newNativeAgentOn` sets `Config.SystemPrompt` to it. H6 later merges it into the full system prompt. Tools without a snippet are not listed (Pi). | `internal/agent/types.go:49`, `internal/agent/agent.go:530`, nothing sets `SystemPrompt` in `cmd/tui/headless.go:263-274` |
| P9 | Concurrency: `read`, `ls`, `grep`, `find`, `job_list`, `job_output` without `wait` are `ConcurrencySafe`; `write`, `edit`, `bash`, `monitor`, `job_kill`, `job_output` with `wait` run alone. `write` and `edit` also take a process-wide lock per `filepath.EvalSymlinks` path (H-TOOL-02), because one leader holds many sessions. | `internal/tools/types.go:20-25`, `internal/agent/tool_coordinator.go` |
| P10 | UI details: a typed `protocol.ToolResultDetails` (path, diff old/new text, locations, truncation, next offset, spill path, exit code, job id, status, kind) travels in `ToolExecutionResult.Details`; `acp` decodes it. `acp` may import `tools` in tests only. | `pkg/protocol/message.go:395-402`, `internal/acp/README.md` "Imports", `internal/acp/updates.go:22-63` |
| P11 | The demo `echo` tool stays registered (no change): many tests and the faux demo use it. | `cmd/tui/headless_faux.go:24-44`, `cmd/tui/auth_oauth_command_test.go:289` |
| P12 | A notice that the Agent refuses (`ErrNoModel`, `ErrQueueFull`, `ErrDisposed`) is dropped and reported through `jobs.Options.OnError`; the job stays readable with `job_list` and `job_output`. | `internal/agent/queue.go:166-185` |
| P13 | `command-guard` lives in `internal/pipeline/command_guard.go` ("a new control handler", architecture reference section 9) and is registered on a per-agent `pipeline.Registry` (`LoopConfig.Pipeline`) that `newNativeAgentOn` creates only when `ASK_COMMAND_GUARD=1`. The handler never reads the environment. | `docs/ask-architecture-reference.md:386`, `internal/pipeline/registry.go:215,334`, `internal/pipeline/points.go:89-105`, `internal/pipeline/middleware.go:38`, `internal/agent/types.go:40-46`, `internal/agent/loop_run.go:52-53` |
| P14 | Monitor contract and limits as Grok (N3, user 2026-10-11): `monitor(command, description, timeout_ms?, persistent?)`; each stdout line is an event and the command's exit ends the watch; filtering is the command's job (the description says so). `timeout_ms` default 36,000,000 (10 h), max 10 h; `persistent: true` means no timeout; `timeout_ms` above the max with `persistent` false is a validation error. Line cap 500 chars, batch cap 3,000 chars, 200 ms debounce batching, token bucket 10 refilled 1 per 2,000 ms, `[N events suppressed ...]` note, auto-stop after 30,000 ms of continuous rate-limit violation. Ask improvement: auto-stop kills the process group (Grok only stops the pipeline). The monitor timeout differs from `bash` jobs, which have no background time limit (Q9). | grok-build `crates/codegen/xai-grok-tools/src/implementations/grok_build/monitor/types.rs:1-28,38-80,90-110`, `tool.rs:29-36` (read 2026-10-11) |
| P15 | `find`'s last fallback uses an in-house `**` glob and a `.gitignore` subset; no new module (go.mod has no glob or gitignore library). | `go.mod` (no `doublestar`, no gitignore module) |
| P16 | `job_output` `wait` timeout: default 30 s, cap 600 s (same cap as `bash`). DeepSeek's default is not in the decision record; this is a planner default. | decision record "How DeepSeek does it" (no default given) |
| P17 | `Host.Close` closes its sessions in parallel, so the job-table closes of all sessions share one grace and fit the in-process TUI bound (10 s) and `ask leader stop` (15 s per step). | `internal/acp/host.go:299-316`, `cmd/tui/tui_client.go:23`, `cmd/tui/leader.go:53` |
| P18 | Print-mode end: `promptAll` waits for idle before each prompt and retries a prompt that gets `ErrBusy` (a notice started a run); after the last prompt it waits for idle once more; `runHeadless` takes the messages, prints, then disposes; `printReply` skips trailing `<system-reminder>` user messages; `runWithDependencies` calls `jobs.Table.KillRemaining()` (SIGKILL to every live group) after `runHeadless` returns on every path, because `abortGrace` (2 s) is shorter than the 3 s kill grace. | `cmd/tui/headless.go:45,159-170,306-396`, `cmd/tui/headless_print.go:16-40`, `internal/agent/agent.go:452-467` (`ErrBusy` from `begin`) |
| P19 | Image libraries for `read`: the standard `image`, `image/png`, `image/jpeg`, `image/gif` packages plus the new module `golang.org/x/image` (pure Go: `webp` and `bmp` decode, `draw` for scaling). It is not a native image library, so the roadmap rule "do not rebuild native image libraries" stays satisfied. EXIF orientation uses a small in-house parser (no EXIF module). The current model's image support comes from `providers.Model.Input`. | `go.mod` (no `golang.org/x/image` yet), `internal/providers/model.go:55`, `internal/providers/catalog.go:44`; Pi `utils/image-resize-core.ts:5-28` |

## Needs decision

These items were `NEEDS_DECISION` in the Runtime Flow Proof; N1, N2 and N3 are decided by the user (2026-10-11) and kept here for the record. N4 (cassette suite) is a gating item outside the matrix.

| # | Question | Options (recommended first) | Affects |
|---|---|---|---|
| N1 | **Decided (user, 2026-10-11): A.** How does the TUI hide `<system-reminder>` notices? Today every user `message_end` becomes a prompt block (`internal/tui/translate.go:172-177,238-241`) and every follow-up text becomes a queue row (`internal/tui/update_session.go:464,624-627`). Q14 forbids both, and the in-process TUI and the leader TUI both get notices once H5 lands. Standard ACP clients are not affected (`standardUpdates` sends no user frames). | A (Recommended): a T1b design slice (ask-tui-design gate) that hides notices and shows job status lands before or with H5; H5 adds only `protocol.IsSystemReminder` for it; owner `internal/tui`, test a TUI golden with a notice event. B: H5 adds a hide-only filter in `internal/tui` (no new visuals, notices and their queue rows are dropped from the view) after a small design amendment is approved; owner `internal/tui/translate.go`, `update_session.go`. C: ship H5 with notices visible in the TUI until T1b (violates Q14 for a time). | Phase 1 (helper), roadmap T1b, release order |
| N2 | **Superseded for print mode (user, 2026-10-26): `ask -p` now waits for background work, and each completion wakes one more turn; see `docs/adr/0005-shell-jobs-use-the-notify-port.md`.** **Decided (user, 2026-10-11): A, as DeepSeek's headless runner (wait for idle, print, dispose cancels jobs with SIGTERM then SIGKILL after 3 s, teardown notices dropped; `packages/bundle/headless/src/index.ts:365-380`, `packages/jobs/tool-jobs/src/index.ts:278`), plus Ask's SIGKILL sweep after dispose and a test for a job still running at the final answer, which DeepSeek lacks.** What happens to running background jobs when `ask -p` ends? | A (Recommended): after the last prompt, print mode waits for idle (a notice that came during the last prompt gets its answer, P18), prints, then closes the table (kills what still runs) and sweeps with SIGKILL; the `bash` description says so; tests `TestPrintNoticeAfterLastPromptIsAnswered`, `TestPrintExitKillsBackgroundJobs`. B: print mode also waits for every running job to finish (no bound) before it closes; same owner, a different exit test. C: close at once without the idle wait (a notice from the last prompt is lost). | Phase 7, phase 9 |
| N3 | Monitor contract. | **Decided (user, 2026-10-11): Grok's shape.** `monitor(command, description, timeout_ms?, persistent?)`, no pattern parameter; each stdout line is an event; see P14. Supersedes the architecture line "lines that match a pattern become events" (supersession note in the [decision record](../architectures/h5-builtin-tools.md#n3-monitor-contract-user-2026-10-11)). | Phase 5, phase 6, phase 9 |
| N4 | **Decided (user, 2026-10-11): A (re-record the six cassette tests on OpenAI with `OPENAI_API_KEY`; replay uses a fake key and the same request body).** Adding tools and a system prompt changes every request body, so the six Token Plan cassettes in `cmd/tui/testdata/cassettes/` stop replaying (body match, `docs/testing-llm-cassettes.md:39`; they record `"tools":[echo]`), and the Token Plan key has expired (spike 1). | A (Recommended): move the six cassette tests to OpenAI with an API key and re-record them with `OPENAI_API_KEY` from the environment; replay uses a fake key and the same body. B: native Anthropic from a copy of `~/.ask-t1a-oauth`; the OAuth profile adds the Claude Code system block and beta header (`internal/providers/anthropic/document.go:35`, `profile.go:18-19`), so replay needs a stored fake OAuth credential in a temp `ASK_HOME`. C: renew the Token Plan key and re-record as the cassette doc says. | Phase 9, `docs/testing-llm-cassettes.md` (phase 10) |

## Cross-plan dependencies

| Relationship | Plan | Status | Note |
|---|---|---|---|
| blockedBy (frontmatter) | [T1c provider setup](../261010-1424-t1c-provider-setup/plan.md) | in_progress (phase 8 only) | Real dependency for phase 10 only: both edit `CLAUDE.md` and the roadmap; H5 phase 10 runs after T1c phase 8. H5 code builds on T1c's committed state (commit `94a70d8`: `newNativeAgentOn`, `acpParams`, faux opt-in). The reverse `blocks` entry in T1c's `plan.md` is not written by this run (write scope is this plan directory). |
| Related | [T1a local TUI](../261009-2239-t1a-local-tui/plan.md) | in-progress (phase 8) | T1a draws a tool call as one line; T-LAY-09, T-MD-10, T-CMD-08 moved to T1b with H5. No shared file. |
| Blocks | T1b (no plan yet; roadmap section "T1: TUI core", T1b rows) | not planned | T1b tool output display, `/copy` and notice hiding need H5. Recorded in the roadmap T1b line (phase 10), not in frontmatter, because no plan directory exists. N1 decides whether part of it moves into or before H5. |
| None | [TUI E2E automation](../261007-1104-tui-e2e-automation/plan.md) | in-progress | No overlap. |

## Phases

| # | Phase | Status | Depends on | Effort |
|---|---|---|---|---|
| 1 | [Shared contracts, paths and import rules](phase-01-shared-contracts-and-paths.md) | completed | - | 1.5d |
| 2 | [File tools: read, write, edit, ls](phase-02-file-tools.md) | completed | 1 | 4d |
| 3 | [Process runner in sandbox](phase-03-process-runner.md) | completed | 1 | 2d |
| 4 | [Search tools: grep, find](phase-04-search-tools.md) | completed | 1, 3 | 1.5d |
| 5 | [Job table, notices and monitor](phase-05-job-table.md) | completed | 3 | 3d |
| 6 | [Shell and job tools](phase-06-shell-and-job-tools.md) | completed | 5 | 1.5d |
| 7 | [Composition, session close and command-guard](phase-07-composition-and-guard.md) | completed | 2, 4, 6 | 2d |
| 8 | [ACP tool mapping](phase-08-acp-tool-mapping.md) | completed | 7 | 1d |
| 9 | [End-to-end acceptance](phase-09-e2e-acceptance.md) | completed | 8 | 3d |
| 10 | [Real-provider acceptance and docs](phase-10-real-provider-and-docs.md) | completed | 9 | 1d |

Parallel groups: phases 2 and 3 after phase 1; phases 4 and 5 after phase 3 (phase 4 also needs 1). Everything from phase 7 on is sequential.

## File ownership

| Phase | Owns (create or modify) |
|---|---|
| 1 | `pkg/protocol/tool.go` (details type, reminder helper), `internal/workspace/path.go`, `internal/tools/truncate.go`, `internal/tools/prompt.go`, `internal/tools/file_lock.go`, `internal/tools/file_state.go`, `internal/tools/file_atomic.go`, `.golangci.yml` |
| 2 | `internal/tools/file_read.go`, `file_write.go`, `file_edit.go`, `file_edit_match.go`, `file_ls.go`, `file_generated.go`, `file_image.go` and their tests; `go.mod`, `go.sum` |
| 3 | `internal/sandbox/**` (except README, phase 10) |
| 4 | `internal/tools/search_grep.go`, `search_find.go`, `search_walk.go` and tests |
| 5 | `internal/jobs/**` (except README, phase 10) |
| 6 | `internal/tools/shell_bash.go`, `shell_jobs.go`, `shell_monitor.go` and tests |
| 7 | `internal/agent/types.go`, `internal/agent/agent.go`, `internal/agent/dispose_release_test.go`, `internal/pipeline/command_guard.go` (+ test), `internal/acp/host.go`, `internal/acp/host_test.go`, `cmd/tui/headless.go`, `cmd/tui/headless_print.go`, `cmd/tui/headless_print_test.go`, `cmd/tui/native_agent_test.go`, `cmd/tui/args.go`, `cmd/tui/acp.go`, `cmd/tui/headless_lifecycle_test.go` |
| 8 | `internal/acp/updates.go`, `internal/acp/updates_test.go` |
| 9 | `cmd/tui/tools_e2e_test.go`, `cmd/tui/tools_leader_e2e_test.go`, `cmd/tui/headless_record_test.go`, `cmd/tui/testdata/cassettes/*` (per N4) |
| 10 | READMEs of `internal/tools`, `internal/sandbox`, `internal/jobs`, `internal/workspace`; `internal/README.md`, `docs/ask-architecture-reference.md`, `docs/testing-llm-cassettes.md`, `CLAUDE.md`, roadmap H5 and T1b lines, `plans/reports/real-provider-acceptance-*-h5-tools.md` |

No two parallel phases touch the same file (phases 2 and 3, 4 and 5 own disjoint sets; phase 7 and later run alone).

## Test matrix

| Level | What | Where |
|---|---|---|
| Unit (real files, real processes) | matcher ladder, truncation, path variants, runner kill and drain, buffer, env filter, job states, flood guard (fake clock), `Release` order, command-guard table | `internal/tools`, `internal/workspace`, `internal/sandbox`, `internal/jobs`, `internal/agent`, `internal/pipeline`; new packages run `goleak.VerifyTestMain` |
| Integration | tool + real job table + real runner; command-guard + real `pipeline.Registry`; `Host.Close` in parallel; ACP frame mapping | `internal/tools`, `internal/pipeline`, `internal/acp` |
| E2E print mode | PM and PB rows below | `cmd/tui/tools_e2e_test.go` |
| E2E leader and in-process client | LE and in-process rows below | `cmd/tui/tools_leader_e2e_test.go` |
| Real provider | exit criterion | phase 10 |

Model replacement rule: scripted Anthropic SSE through the real adapter (pattern `cmd/tui/headless_lifecycle_test.go:82,136-160`, `sseResponse` at `cmd/tui/headless_cassette_test.go:71`) or the provider fixture for built binaries (`cmd/tui/acp_e2e_test.go:1493`). The faux demo script is not changed (it is a documented product demo). Never record a cassette of a `bash` run: tools run again on replay and a changed output changes the next request body (cassette doc, "Turn a real session into a test").

## Backwards compatibility and rollback

- Pi contract parity: names, fields, units and defaults follow Pi; `bash` `timeout` changes meaning from kill to background (Q9, user decision).
- JSON event stream of `ask -p --mode json` keeps its shape; notices appear as ordinary `user` messages; a notice cycle that `Dispose` cuts at the end appears as an aborted cycle.
- Internal API changes (all callers in this repo, listed in phase 7): `agent.Config.Release` (new, optional); `newNativeAgentOn`, `newNativeAgent`, `newHeadlessAgentWithAuth` return the job table too; `builtinTools()` is replaced by `sessionTools`. `acp.Factory`, `ACPParams.NewAgent`, `newHeadlessAgent` and `runHeadless` keep their signatures. `runHeadless` now disposes the Agent after a normal end; `Host.Close` closes sessions in parallel.
- Rollback per phase: phases 1-6 add new files and packages only; revert the phase commit. Phase 7 is the switch: reverting it restores `builtinTools() = [echo]`, sequential `Host.Close` and the old print end. Phase 8 reverts to `ToolKindOther` frames. Phase 9 reverts tests and cassettes together. No data migration; nothing persists.

## Acceptance

- Every PASSED row has its named passing test; N1-N4 are answered and their rows are PASSED.
- `go test ./...`, `go vet ./...` and `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` pass; new packages run `goleak.VerifyTestMain`.
- No process group of a test survives its cleanup (`kill(-pgid,0)` returns ESRCH).
- Real-provider run (phase 10): print mode reads the repo, edits a file, runs its tests; report has no secret.
- Docs listed in phase 10 describe the implemented state.

## Validation Log

### Runtime Flow Proof Matrix

Entry points. PM = `runWithDependencies([]string{"-p", "--provider", "alibaba-token-plan", ...})` (`cmd/tui/headless.go:60`) with `deps.inferenceHTTP` set to a scripted Anthropic Messages SSE server; the real adapter, Agent, coordinator, tools, runner and job table run. Path: `parseArgs` → `newHeadlessAgentWithAuth` → `newNativeAgentOn` (tools, job table, prompt section, `Release`) → `runHeadless` → `Agent.Prompt` → coordinator (`tool_coordinator.go:204` prepare, `:264` execute) → tool body → next request (the script asserts the tool result in the body) → `printReply`. PB = the built `ask -p` (`acpBinary`, `cmd/tui/acp_e2e_test.go:46`) with the provider fixture, for signals and process exit. LE = the built `ask leader` with `ASK_HOME` isolated, the provider fixture (`cmd/tui/acp_e2e_test.go:1493`), and a client on the Unix socket: `session/new` → `Host.NewSession` (`host.go:259`) → `Factory` → `session/prompt` → tools → `session/update`. IP = `openInProcessClient` (`cmd/tui/tui_client.go:88`), the TUI's fallback entry. External mocks for all: only the model HTTP service. Prepared data: a temp git repo with a Go module and one failing test, unless the row says otherwise.

| Feature | Actor | Runtime trigger | Entry point | Internal path | Observable result | End-to-end test | External mocks | Prepared data | Status |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `read` (numbers, offset/limit, binary, past-end hint, `@`/`~` paths) | Model | tool call `read` | PM | → `tools.Read` → `workspace.ResolvePath` → `TruncateHead` | `N\|text` lines, `Use offset=N to continue`, binary error | `TestPrintReadsFileWithLineNumbers`, `TestPrintReadPagesWithOffset` | model HTTP | 3000-line file, binary file | PASSED |
| `write` (atomic, parents, shebang, generated guard) | Model | tool call `write` | PM | → `tools.Write` → `PathLock` → `WriteFileAtomic` | file content, mode 0755 for shebang, refusal for a generated file | `TestPrintWritesFileAtomically`, `TestPrintWriteRefusesGeneratedFile` | model HTTP | empty dir, generated file | PASSED |
| `edit` (ladder, unique match, CRLF/BOM, stale reject) | Model | tool call `edit` | PM | → `tools.Edit` → `FileState.Check` → matcher → `WriteFileAtomic` | changed file; stale error after an outside change | `TestPrintEditsFileAfterRead`, `TestPrintEditRejectsStaleFile` | model HTTP | CRLF file with BOM | PASSED |
| `ls` | Model | tool call `ls` | PM | → `tools.Ls` | sorted names, `/` suffix, dotfiles | `TestPrintListsDirectory` | model HTTP | dir with dotfile | PASSED |
| `grep` (`rg --json`, caps, early stop) | Model | tool call `grep` | PM | → `tools.Grep` → `sandbox.Run(rg)` | `path:line: text`, `Found at least N` | `TestPrintGrepFindsMatches` | model HTTP | repo with matches | PASSED |
| `find` via `rg --files` | Model | tool call `find` | PM (`PATH` without `fd`) | → `tools.Find` → `sandbox.Run(rg --files)` | relative paths, gitignore respected | `TestPrintFindWithoutFdUsesFallback` | model HTTP | repo with `.gitignore` | PASSED |
| `find` via Go walk | Model | tool call `find` | PM (`PATH` without `fd`, `rg`) | → `tools.Find` → walk + gitignore stack | same list as the rg fallback | `TestPrintFindWithoutFdAndRgUsesWalk` | model HTTP | nested `.gitignore` | PASSED |
| `bash` foreground, exit code | Model | tool call `bash` | PM | → `tools.Bash` → `jobs.Start` → `sandbox.Start` → `jobs.Wait` done | `exit: 1` header, `IsError`; job removed | `TestPrintBashRunsTestsAndReportsExit` | model HTTP | failing Go test | PASSED |
| `bash` head+tail view and spill file 0600 | Model | `bash` with 300 KB output | PM | → buffer → spill tee → `sandbox.View` | elision marker, spill path in details, mode 0600 | `TestPrintBashBigOutputKeepsHeadAndTail` | model HTTP | `seq` output | PASSED |
| Spill cap 64 MB, job kept | Model | `bash` with 70 MB output | PM | → spill tee stops at cap | `[spill file capped at 64 MB]`, exit 0, file size 64 MB | `TestPrintBashSpillStopsAtCap` | model HTTP | `head -c` from `/dev/zero` | PASSED |
| `bash` live partial updates | Model | long `bash` | PM `--mode json` | → `tc.Update` at most each 50 ms | `tool_execution_update` events in the stream | `TestPrintJSONStreamCarriesBashPartialUpdates` | model HTTP | script printing for 400 ms | PASSED |
| `bash` move to background on wait budget | Model | `bash` with `timeout` 1 | PM | → `jobs.Wait` budget → `Background` | "continues as background job bash-1"; process alive | `TestPrintBashMovesToBackgroundAfterTimeout` | model HTTP | gate file | PASSED |
| `bash` `run_in_background` | Model | `bash` with `run_in_background: true` | PM | → `jobs.Start`, budget 0 | "Started background job bash-1" | `TestPrintBashRunInBackgroundReturnsJobID` | model HTTP | gate file | PASSED |
| Trailing `&` rejected | Model | `bash` `sleep 5 &` | PM | → `tools.Bash` validation | error with `run_in_background` hint | `TestPrintBashRejectsTrailingAmpersand` | model HTTP | none | PASSED |
| Cancelled `bash` kills its group | User | SIGINT during `bash` | PB (signal path `headless.go:337-381`) | Dispose → run ctx cancel → `jobs.Wait` cancelled → group kill; `KillRemaining` after return | exit 130, group gone (ESRCH) even for a child that traps TERM | `TestPrintSIGINTDuringBashKillsProcessGroup` | model HTTP | `trap '' TERM` child + grandchild | PASSED |
| `job_output` on a running job | Model | tool call `job_output` | PM | → `jobs.Read` without wait | output since last read, `[status: running]`, "do not poll" line, < 50 ms | `TestPrintJobOutputAnswersRunningJobAtOnce` | model HTTP | gate file | PASSED |
| `job_output` with `wait`; no notice after | Model | `job_output` `wait: true` | PM | → `jobs.Read` wait → final status, `awaited` | final status; next request has no notice | `TestPrintJobOutputWaitReturnsFinalStatus` | model HTTP | gate file | PASSED |
| `job_kill`; no notice after | Model | tool call `job_kill` | PM | → `jobs.Kill` → group kill | `[status: killed]`, no notice later | `TestPrintJobKillStopsJobWithoutNotice` | model HTTP | long job | PASSED |
| `job_list` | Model | tool call `job_list` | PM | → `jobs.List` | `bash-1 [bash] running ...` lines | `TestPrintJobListShowsJobs` | model HTTP | two jobs | PASSED |
| Job cap 10 (monitors count) | Model | 11th background `bash` | PM | → `jobs.Start` refuses | error text names `job_kill` | `TestPrintEleventhJobIsRefused` | model HTTP | 9 gated jobs and 1 monitor | PASSED |
| Completion notice via `FollowUp` | Job table | background job ends | PM (two prompts) | owner goroutine → `Notifier.FollowUp` → Agent cycle; `promptAll` busy retry | next request has a `user` message `<system-reminder>…bash-1 … finished [status: completed, exit code: 0]…` | `TestPrintCompletionNoticeStartsFollowUpCycle` | model HTTP | pid gate | PASSED |
| Notice kept in the session log and JSON stream | Job table | background job ends | PM `--mode json` | Agent log → `streamJSON` | `message_end` of a `user` message with the reminder text | `TestPrintJSONStreamCarriesNotice` | model HTTP | pid gate | PASSED |
| Notice after the last prompt is answered, then exit | Job table, user | job ends during the last prompt | PM | `promptAll` idle wait → snapshot → print → `Dispose` | printed reply answers the notice | `TestPrintNoticeAfterLastPromptIsAnswered` | model HTTP | pid gate | PASSED (N2 = A) |
| Monitor events via `Steer` (every stdout line) | Model, then job table | tool call `monitor` | PM | → `tools.Monitor` → `jobs.StartMonitor` → stdout lines → 200 ms batch → bucket → `Steer`/`Remove` | `<monitor-event>` in a `<system-reminder>` user message, one per batch; result has task id, resolved `timeout_ms`, `persistent` | `TestPrintMonitorDeliversEveryStdoutLine` | model HTTP | script printing `DONE build` | PASSED |
| Monitor flood guard | Job table | a line every 100 ms for 6 s | PM | → 200 ms batches → bucket | first 10 events delivered, later ones suppressed with `[N events suppressed ...]` | `TestPrintMonitorFloodGuardSuppressesEvents` | model HTTP | steady printer | PASSED |
| Monitor `timeout_ms` ends the watch | Model | `monitor` with `timeout_ms: 500` | PM | → table timer → group kill | completion notice with status killed, reason timeout; group gone | `TestPrintMonitorStopsAtTimeout` | model HTTP | endless quiet script | PASSED |
| Monitor `timeout_ms` above the max | Model | `monitor` with `timeout_ms: 36000001`, `persistent` false | PM | → `tools.Monitor` validation | validation error result; no job started | `TestPrintMonitorRejectsTimeoutAboveMax` | model HTTP | none | PASSED |
| Monitor `persistent` runs until `job_kill` | Model | `monitor` with `persistent: true`, then `job_kill` | PM | → no timer → `jobs.Kill` | result `timeout_ms` 0, `persistent` true; still running after 2 s; stopped by `job_kill` without a notice | `TestPrintMonitorPersistentRunsUntilJobKill` | model HTTP | endless quiet script | PASSED |
| Monitor overload kill | Job table | 30 s of continuous lines | PM | → bucket empty 30 s → `Process.Kill` | final "stopped: output too fast" event; group gone | `TestPrintMonitorOverloadKillsProcess` | model HTTP | endless printer | PASSED |
| `command-guard` on | User (env), model | `ASK_COMMAND_GUARD=1` + `bash rm -rf /` | PM | `newNativeAgentOn` registers handler → `BeforeTool` (`registry.go:334`) → Block | tool error `blocked by command-guard: <reason>`, run continues | `TestPrintCommandGuardBlocksWhenEnabled` | model HTTP | none | PASSED |
| `command-guard` off by default | Model | `bash` with a guarded-looking harmless probe | PM | no handler | command reaches the runner | `TestPrintCommandGuardOffByDefault` | model HTTP | none | PASSED |
| Cleanup on ACP `session/close` | Client | `session/close` | LE | `Session.Close` → `Dispose` → `Release` → `jobs.Close` | background group gone (ESRCH), spill dir removed | `TestLeaderSessionCloseKillsBackgroundJobs` | model HTTP | background `sleep 300` | PASSED |
| Cleanup on leader shutdown | User | `ask leader stop` | LE | `LeaderRuntime.Stop` → `Adapter.Close` → `Host.Close` (parallel) → each `Session.Close` | all groups gone, stop < 8 s with three sessions | `TestLeaderShutdownKillsBackgroundJobs` | model HTTP | `trap '' TERM` jobs in three sessions | PASSED |
| Cleanup on in-process client close | User | TUI exit with the in-process agent | IP | client close → `ServeStdio` ends → `Adapter.Close` → `Host.Close` | group gone before `OnClose` returns (< 10 s) | `TestInProcessClientCloseKillsBackgroundJobs` | model HTTP | background `sleep 300` | PASSED |
| Background jobs at `ask -p` exit | User | last prompt ends with a job running | PB | `runHeadless` → `Dispose` → `Release`; `KillRemaining` | no group left after exit (ESRCH) | `TestPrintExitKillsBackgroundJobs` | model HTTP | background `trap '' TERM; sleep 300` | PASSED (N2 = A) |
| Child env filter and `ASK_*` vars | Model | `bash env` | PM (`deps.environ` with fake secrets) | → `sandbox.ChildEnv` | no `*_KEY`/`*TOKEN*` names; `ASK_SESSION_ID`, `ASK_MODEL` present; `TERM=dumb` | `TestPrintBashEnvironmentHidesSecrets` | model HTTP | env with fake secrets | PASSED |
| Prompt snippets in system prompt | Model | first request | PM | `newNativeAgentOn` → `PromptSection` → `Config.SystemPrompt` | request `system` lists tools and guidelines | `TestPrintRequestCarriesToolPromptSection` | model HTTP | none | PASSED |
| ACP kind, locations, diff | Client | `session/prompt` with read/edit/bash | LE | `standardUpdates` (`updates.go:22`) kind by name, details decode | `tool_call` frames: kinds `read`/`edit`/`execute`/`search`, `locations`, diff old/new text | `TestLeaderToolUpdatesCarryKindLocationsAndDiff` | model HTTP | small file | PASSED |
| Notice not sent as a standard ACP frame | Client | notice during a session | LE | `standardUpdates` has no user case | no `user_message_chunk` for the notice | `TestLeaderNoticeIsNotSentAsUserFrame` | model HTTP | pid gate | PASSED |
| Notice hidden from the TUI conversation view | User | notice reaches the TUI | TUI (`translate.go:172`) | `protocol.IsSystemReminder` in the TUI filter | no prompt block, no queue row | T1b design slice (ask-tui-design gate) lands before or with H5; H5 adds `protocol.IsSystemReminder` | model HTTP | notice | PASSED (N1 = A; the TUI part belongs to the T1b slice and gates the H5 release) |
| `read` image block | Model | `read` of a PNG | PM | → `tools.Read` → detect → keep → image block | next request holds `Read image file [image/png]` and the image block | `TestPrintReadImageReturnsImageBlock` | model HTTP | small PNG | PASSED |
| `read` image auto-resize | Model | `read` of a 4000x3000 PNG | PM | → resize ladder | image block ≤ 2000x2000 and ≤ 4.5 MiB base64, dimension note | `TestPrintReadLargeImageIsResized` | model HTTP | large PNG | PASSED |
| `read` image on a non-vision model | Model | `read` of a PNG | PM (model without `image` input) | → `modelInput` check | note `[Current model does not support images. ...]`, no image block | `TestPrintReadImageForNonVisionModelAddsNote` | model HTTP | small PNG | PASSED |
| Pi argument coercion | Model | `read` with `"offset":"5"` | PM | `Prepare` (`validate.go:28`) | call succeeds | `TestPrintCoercesStringArguments` | model HTTP | small file | PASSED |
| Legacy `edit` shape | Model | `edit` with top-level `oldText` | PM | `ArgumentPreparer` (`tool_coordinator.go:204`) | call succeeds | `TestPrintEditAcceptsLegacyShape` | model HTTP | small file | PASSED |
| Exit criterion with a real provider | User | `ask -p "fix the failing test"` | real binary | full path | file edited, `go test` passes, transcript shows read, edit, bash | phase 10 run report | none (real OpenAI API key; Anthropic and ChatGPT OAuth from a copy of `~/.ask-t1a-oauth` under `/private/tmp`) | repo with one failing test; OAuth copy | PASSED |

- **Rows:** 46. **PASSED:** 46. **NEEDS_DECISION:** 0 (N1, N2 and N3 decided by the user on 2026-10-11). **FAILED:** 0. Gating item outside the matrix: N4 (cassette suite), decided by the user on 2026-10-11 (option A).
- **Gate status:** PASSED. Implementation of the TUI part waits on the T1b notice-hiding design slice (N1); the H5 release waits on that slice.

### Verification Results

- **Tier:** Full (10 phases): Runtime Flow Verifier, Fact Checker, Flow Tracer, Scope Auditor, Contract Verifier.
- **Claims checked:** 52 (symbols, file:line citations, caller counts, behavior traces).
- **Verified:** 47 | **Failed:** 4 (fixed) | **Unverified:** 1 group (external sources).

#### Failures (all fixed in this run)

1. [Contract Verifier] Draft P2 changed `acp.Factory` and `ACPParams.NewAgent`; grep found the factory wrapped in 5 test files (`internal/acp/adapter_peer_test.go:68`, `stdio_test.go:96-121`, `route_test.go:260`, `provider_methods_test.go:623`, `agent_test.go:86`) and `NewAgent` in 6 test sites. Replaced by `agent.Config.Release` (P2): no signature change on those paths.
2. [Contract Verifier] Phase 7 first counted 30 `newHeadlessAgent` test sites in 10 files; grep gives 16 in 9 files, and 25 `runHeadless` test sites. Corrected in phase 7.
3. [Fact Checker] Draft N4 option A assumed an Anthropic OAuth recording replays with the same body; `internal/providers/anthropic/document.go:35` adds a system block for OAuth. N4 options rewritten.
4. [Fact Checker] Phase 8 said "create `updates_test.go` if absent"; the file exists (`internal/acp/updates_test.go`). Corrected.

#### Traces (Flow Tracer and Scope Auditor)

- Notice start: `Agent.enqueue` on an idle Agent starts a run on its own goroutine (`internal/agent/queue.go:180-197`); a later `Prompt` then fails in `begin` with `ErrBusy` (`internal/agent/agent.go:452-467`) → P18 retry.
- Print signal path: `Dispose` runs on a goroutine and the exit waits at most `abortGrace` 2 s (`cmd/tui/headless.go:45,349-381`) → P18 sweep.
- Dispose order: cancel → `<-r.done` (started tools drained) → `src.Close()` → `agent_disposed` (`internal/agent/agent.go:248-285`) → `Release` fits between drain and log close.
- Lifetime: `newNativeAgentOn` runs once per session for print mode (`headless.go:226`) and for every ACP session through `acpParams` (`cmd/tui/acp.go:133`), which `ask acp`, `ask leader` (`cmd/tui/leader.go:245`) and the in-process TUI (`cmd/tui/tui_client.go:89`) all use. The job table, `FileState` and the edit no-op counter are per session; `PathLock` is process-wide by design (P9).

#### Unverified

- Pi, omp, Grok and DeepSeek source line citations (not in this repository); they come from the inventory, the decision record and the xia report, and are marked as such where used.

### Red Team Review

Four personas reviewed the plan against the code. Each finding has evidence; accepted findings are applied.

| # | Persona | Finding | Evidence | Decision and change |
|---|---|---|---|---|
| RT1 | Failure | On SIGINT `ask -p` exits after 2 s, before the 3 s SIGKILL; a child that traps TERM survives, which breaks the Outcome promise. | `cmd/tui/headless.go:45,361-381`; spike 2 scenario 5 | Accepted: `jobs.Table.KillRemaining` and `sandbox.Process.KillNow`; `runWithDependencies` sweeps after `runHeadless` on every path (P18); PB test with `trap '' TERM` (phases 3, 5, 7, 9). |
| RT2 | Failure | Changing `acp.Factory` and `ACPParams.NewAgent` touches about 13 sites and still needs a separate print-mode path; a missed path leaks jobs. | Contract Verifier failure 1 | Accepted: close through `agent.Config.Release` inside `Dispose` (P2 revised); every path already calls `Dispose` or now does. |
| RT3 | Failure | A notice can start a run between prompts (`Prompt` → `ErrBusy`, exit 1) or after the last prompt (`printReply` sees a user message last and prints nothing). | `internal/agent/queue.go:180-197`, `agent.go:452-467`, `cmd/tui/headless_print.go:20-23` | Accepted: P18 (idle wait, busy retry, snapshot, reminder skip); tests `TestPromptAllRetriesWhenNoticeRunIsActive`, `TestPrintReplySkipsTrailingSystemReminders`, `TestPrintNoticeAfterLastPromptIsAnswered`. |
| RT4 | Failure | `Host.Close` closes sessions one by one; each now waits for its table (about 6 s worst case). The in-process TUI waits only 10 s and `ask leader stop` 15 s per step. | `internal/acp/host.go:299-316`, `cmd/tui/tui_client.go:23`, `cmd/tui/leader.go:53` | Accepted: parallel `Host.Close` (P17); `TestHostCloseClosesSessionsInParallel`, leader test with three sessions. |
| RT5 | Assumptions | N4 option A (Anthropic OAuth cassettes) assumed the same body at replay. | `internal/providers/anthropic/document.go:35`, `profile.go:18-19,72` | Accepted: N4 options rewritten; recommendation is the OpenAI API-key path (phase 9). |
| RT6 | Assumptions | A job that ends exactly when the wait budget passes could give both a "moved to background" result and a notice, or neither. | architecture "Jobs" (notice skipped when awaited) | Accepted: waiter registration rule under the table mutex; `TestJobEndingAtBudgetIsReportedOnce` (phase 5). |
| RT7 | Assumptions | Proving SIGINT and exit cleanup in process would signal the whole test binary. | `cmd/tui/headless.go:64-66` (`signal.Notify` in `runWithDependencies`) | Accepted: PB entry (built binary) for those rows (phase 9). |
| RT8 | Scope | The first matrix grouped independent behaviors (skip rules, spill cap, flood guard, overload kill, find fallbacks, cleanup paths) under unit tests or one E2E test. | verification-roles "one row per atomic capability" | Accepted: 40 atomic rows, each with its own E2E test; 7 tests added to phase 9. |
| RT9 | Scope | Planner additions beyond the decisions: P15 in-house glob, P16 `job_output` timeout default, P17 parallel close, P18 print end, `Config.Release`. | this table | Kept: each closes a failure mode above or a gap in the decisions; none adds a user-visible feature. P16 is listed as an unresolved default. |
| RT10 | Scope | A Go `grep` fallback (inventory note "Go fallback") and a byte-rate kill (spike 2 idea) were proposed. | inventory line 55; spike 2 "Facts" | Rejected: the architecture says `grep` uses `rg` per Pi; spike 2 says the rate kill was not needed. Recorded as risks in phases 4 and 3. |
| RT11 | Security | Spill files under a shared `/tmp` on Linux could be read by other users. | P5 | Accepted as already handled: directory 0700 from `os.MkdirTemp`, files 0600, removed on close (phase 3 tests the mode). |
| RT12 | Security | The real-provider run could expose the key in `ps` or in the report. | phase 10 | Accepted: key only from the environment, never `--api-key`; report scanned for key-shaped strings. |
| RT13 | Security | The leader's own `ASK_*` variables (`ASK_HOME`, `ASK_COMMAND_GUARD`) could reach commands. | Q12, P6 | Accepted as already handled: every inherited `ASK_*` name is dropped; `TestChildEnvReplacesInheritedAskVars`. |
| RT14 | Security | An on-disk job registry would let a restarted leader kill orphans after a crash. | Q8 | Rejected: user decision Q8 (accept, as DeepSeek). |
| RT15 | Failure | The table is created before the Agent; a constructor error or a notice before `SetNotifier` could leak or panic. | phase 7 composition order | Accepted: `jt.Close()` on a constructor error; lazy spill directory; notices before `SetNotifier` go to `OnError` (`TestNoticeBeforeNotifierSetIsReported`). |

### Whole-Plan Consistency Sweep

- Files reread: plan.md, phase-01-shared-contracts-and-paths.md, phase-02-file-tools.md, phase-03-process-runner.md, phase-04-search-tools.md, phase-05-job-table.md, phase-06-shell-and-job-tools.md, phase-07-composition-and-guard.md, phase-08-acp-tool-mapping.md, phase-09-e2e-acceptance.md, phase-10-real-provider-and-docs.md.
- Decision deltas checked: 11 (images like Pi, H-TOOL-20 pulled into H5, `golang.org/x/image`; N3 decided as Grok's monitor shape, no pattern; real-provider scope includes the OAuth copy; P2 carrier; N4 provider; P16; P17; P18; caller counts; split matrix rows; `NewFileState`/`NewFileTools` constructors).
- Reconciled stale references: 13 (phase 10 OAuth runs made required; `cwd` details field; `Output.SpillErr` and nil `SpillFile`; `jobs.Options` zero value; phase 1 dependency map and checklist; phase 2 constructor; phase 5 close risk; phase 7 caller counts, host file, test files; phase 8 test file; phase 9 N4 text and test list; plan file ownership and backwards compatibility).
- Unresolved contradictions: 0.

## Unresolved questions

1. Decided (user, 2026-10-11): `read` handles images exactly like Pi (detection, conversion, EXIF orientation, auto-resize, non-vision note); see phase 2 and P19.
2. Decided (user, 2026-10-11): after the OAuth real-provider runs (phase 10), if a refresh token rotated, back up `~/.ask-t1a-oauth/auth.json` as `auth.json.before-h5` and copy the refreshed `auth.json` from the run copy back (as spike 1 did); never print token values.
3. Resolved (2026-10-11): `job_output` `wait` default 30 s and cap 10 min match DeepSeek (`packages/jobs/tool-jobs/src/index.ts:310-349`, recorded in the DeepSeek research of 2026-10-10).
4. N1, N2, N3 and N4 are decided by the user (2026-10-11); see "Needs decision".

## Review

Status of the plan: phases 1 to 10 are completed; the plan stays `in-progress` because two acceptance items are open (listed below).

### Results

- Tools, process runner, job table, monitor, notices, `command-guard`, close path (`agent.Config.Release`), ACP tool mapping and the OpenAI cassette suite are implemented. `go vet ./...` passes; `go test ./...` passes except one flake in the first full run of phase 10: `TestACPE2ERequestCancel` timed out waiting for its first update (the request failed with `UNKNOWN`). It passed alone and in a second full `go test ./cmd/tui` run. See open item 4.
- Real-provider acceptance ([report](../reports/real-provider-acceptance-261011-h5-tools.md)): `ask -p` with OpenAI `gpt-5.5` (API key) and Anthropic `claude-sonnet-4-6` (OAuth) read the repository, edited the file and ran its tests; `go test` passes after each run; no process was left. Background jobs and the notice were seen end to end with a third prompt.
- Docs: READMEs of `tools`, `sandbox`, `jobs` (new), `workspace`, `internal/README.md`, the architecture reference, the cassette doc, `CLAUDE.md` and the roadmap H5 and T1b lines describe the implemented state.

### Deviations from the plan

- `NewFileTools` and `sessionTools` take a model-input closure (phases 2 and 7); `golang.org/x/image` is pinned to v0.45.0.
- `find` through `rg` applies the pattern in Go; notices in `grep` and `find` are separate lines; `edit` locations are new-file line numbers.
- `cmd/tui/headless_test.go`, `cmd/tui/output.go` and the TUI/ACP wiring for the job-table error sink changed outside the phase ownership lists, each for a stated reason (phase 7 and 9 reports).
- The OpenAI cassettes use one fixed session ID because the Responses wire sends it as `prompt_cache_key`.
- Print mode cannot show a notice for a job that is still running after the last reply (N2 = A); the real-provider report uses a third prompt to show the notice.

### Open items

1. **Release gate (N1):** closed on 2026-10-12 by [`261012-0155-t1b-job-notices`](../261012-0155-t1b-job-notices/plan.md). Before that the TUI did not hide `<system-reminder>` notices or their queue rows. That is a separate T1b design slice; H5 provides only `protocol.IsSystemReminder`. The in-process and leader TUI show the notices until it lands.
2. **`session/close`:** closed on 2026-10-12 by [`261012-0230-leader-session-close`](../261012-0230-leader-session-close/plan.md); `TestLeaderSessionCloseKillsBackgroundJobs` now runs and passes. Before that the leader wire had no `session/close` and the test skipped. Cleanup is proven by `TestLeaderShutdownKillsBackgroundJobs` and `TestInProcessClientCloseKillsBackgroundJobs`. The matrix row 193 is therefore not a passing test of that exact path.
3. **ChatGPT OAuth run:** unverified. The stored refresh token was rejected by the provider; a new `ask auth login` for ChatGPT and one more run close it.
4. `--mode json` exits 0 on an assistant error (before H5); `monitor` rejects `timeout_ms` of 0 or less (not in the spec text); the global gitignore and `.ignore` files are not read by the `find` walk; `TestJSONHelloLines` depends on the tool snippet length; one unreproduced hang of the bash/job test batch (phase 9 report, item 8); the `TestACPE2ERequestCancel` flake above, not seen in the phase 9 runs.
5. Coordination: `plans/261010-1424-t1c-provider-setup` phase 8 also touches `CLAUDE.md` and the roadmap; the edits in this phase are small and local to the named lines.

### Independent review (Codex gpt-6.1-sol, 2026-10-11)

Report: [codex-review-261011-h5-builtin-tools.md](../reports/codex-review-261011-h5-builtin-tools.md). The review fixed seven defects, each with a regression test and no weakened test:

- `read` produced a negative paging hint for very large numeric limits.
- `edit` treated overlapping matches (`aaa` with `aa`) as one unique match.
- `read` and `edit` accepted named pipes; `edit` could replace one with a regular file.
- `grep`, `find` and `write` did not try path variants.
- `command-guard` missed a blocked command after wrapper options such as `command -p` or `sudo -n`.
- `monitor` dropped a long final stdout line with no newline.
- `monitor` completion could pass an event delivery still in progress.

It also added a portable cap for the `job_output` wait conversion and strengthened four matrix tests. Verified again after the review: `go vet ./...`, `go test ./internal/... ./pkg/...`, `go test ./cmd/tui` (230 s) and lint (0 issues) pass; the secret scan and stray-process check are clean. One `TestFindTimeoutReturnsPartialResult` failure was seen once and did not reproduce in ten repeat runs.

Proof limits kept open: no leader `session/close` operation; N1 TUI notice hiding; the non-vision image test selects the model through the native agent, not the catalog; print-mode `job_output` timing is bounded by 5 s there and by 50 ms at tool level; not every runtime test asserts process-group cleanup itself; the monitor flood test shows suppression, not a first-ten-only count.

### Second independent review (Codex gpt-6.1-sol, 2026-10-11)

Report: [codex-review-2-261011-h5-builtin-tools.md](../reports/codex-review-2-261011-h5-builtin-tools.md). It fixed 13 more defects, each with a regression test that failed before its fix and no weakened test:

- `find`: Unicode patterns and ignore rules failed; file names with spaces lost their spaces.
- `write`: a generated file with a long header could be overwritten.
- `grep`: a match on a line above 4 MiB was dropped.
- `monitor`: a fractional `timeout_ms` selected the ten-hour default; pending merges had no size bound; a delivery could follow a close.
- `bash`: an immediate background result omitted the timeout-cap note.
- Job table: a canceled `job_output` read consumed output or removed a finished job; completion used stale read and close state; the spill-cap warning was missing from `job_output`.
- Signal exit left a private spill directory behind.
- `command-guard` refused harmless quoted fork-bomb text.

The flood test now counts delivered lines inside monitor events and still checks the first ten ticks, the refill bound, suppression and group cleanup. Re-verified by the controller: `go vet ./...`, `go test ./internal/... ./pkg/...`, `go test ./cmd/tui` (240 s) and lint (0 issues) pass; no stray process, no leftover `ask-jobs-*` directory, no secret match, `git diff --check` clean.

Open items are unchanged: N1 TUI notice hiding, the missing leader `session/close`, and the unverified ChatGPT OAuth run. (2026-10-12: N1 and `session/close` are done; see open items 1 and 2 above.)
