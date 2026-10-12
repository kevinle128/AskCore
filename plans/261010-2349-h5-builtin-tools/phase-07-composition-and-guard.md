---
phase: 7
title: "Composition, session close and command-guard"
status: completed
priority: P1
effort: "2d"
dependencies: [2, 4, 6]
---

# Phase 7: Composition, session close and command-guard

## Context

- Architecture: [Recommended approach](../architectures/h5-builtin-tools-architecture.md#recommended-approach) (composition root creates the job table next to each session's Agent and closes it after the Agent drains), [Jobs: cleanup](../architectures/h5-builtin-tools-architecture.md#jobs-jobs), [Boundaries & contracts](../architectures/h5-builtin-tools-architecture.md#boundaries--contracts) (command-guard, D19 reading, import rules), [Missing pieces](../architectures/h5-builtin-tools-architecture.md#missing-pieces) (the session factory must create and close the table; leader, in-process client and print mode all go through it).
- Decisions: [Q3 command-guard](../architectures/h5-builtin-tools.md#q3-critical-commands-and-the-no-approval-stance-d3), [Q8 cleanup](../architectures/h5-builtin-tools.md#q8-orphaned-children-after-a-leader-crash-and-the-job-cap), [Q12 env](../architectures/h5-builtin-tools.md#q12-child-environment-of-bash), [Q13 composition](../architectures/h5-builtin-tools.md#q13-package-placement), [Q14 notice shape](../architectures/h5-builtin-tools.md#q14-message-shape-of-system-notices), [Ask constraints](../architectures/h5-builtin-tools.md#ask-constraints-verified-in-code).
- xia H5: [Decisions](../reports/xia-261010-1727-h5-builtin-tools.md#decisions-user-2026-10-10) item 3 (command-guard) and item 4 (the leader owns background processes).
- Roadmap: D3, D7, D19, D29 in [section 1](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#1-decisions-in-the-order-they-block-phases); [H5](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#h5-built-in-tools) ("Configuration: hard-coded defaults"); [T1](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#t1-tui-core) (the in-process TUI agent is the fallback client and uses the same host).
- Inventory: H-TOOL-11 (prompt contribution), H-TOOL-22 (`ASK_*` session variables), H-SEC-03.

## Overview

Wire everything into the one composition that print mode, `ask acp`, `ask leader` and the in-process TUI agent share (`newNativeAgentOn`, `cmd/tui/headless.go:242`): per-session tools and job table, the prompt section, the child environment, the opt-in `command-guard`, and a close path that kills every job when the Agent is disposed. Print mode gets a defined end: wait for idle, print, dispose, then a SIGKILL sweep.

## Requirements

- Functional:
  - Close carrier (P2, revised): `agent.Config.Release func() error`. `Agent.dispose` (`internal/agent/agent.go:248-285`) calls it once after the active run and its started tools drained (`<-r.done`, `:261`) and before `src.Close()` (`:263`); its error joins the error of `Dispose`. Because `Session.Close` (`internal/acp/host.go:902-918`) and the print-mode signal path (`cmd/tui/headless.go:352`) already call `Dispose`, every composition path closes the table with no change to `acp.Factory` (`host.go:42`) or `ACPParams.NewAgent` (`internal/app/module_acp.go:49`).
  - `newNativeAgentOn` (`cmd/tui/headless.go:242`):
    - creates `fs := tools.NewFileState()` and `jt := jobs.New(jobs.Options{Clock: sandbox.SystemClock, Env: env, OnError: ...})`; registers `sessionTools(jt, fs)` = `read`, `write`, `edit`, `ls`, `grep`, `find`, `bash`, `job_output`, `job_kill`, `job_list`, `monitor`, plus `tools.Echo{}` (P11), all `SourceBuiltin`; then the `extra` tools as today;
    - gives `read` a `modelInput` closure that returns `ag.State().Model.Input` (late bound like the env closure), so image results follow the current model;
    - sets `cfg.SystemPrompt = tools.PromptSection(reg.Snapshot())` (P8; nothing sets it today, `headless.go:263-274`);
    - sets `cfg.Release = jt.Close`;
    - when `getenv("ASK_COMMAND_GUARD") == "1"`: `pl := pipeline.NewRegistry(); pl.OnBeforeTool(pipeline.CommandGuard)`; `cfg.LoopConfig.Pipeline = pl` (P13; per-agent registry, `internal/agent/types.go:40-46`, `internal/agent/loop_run.go:52-53`);
    - after `app.NewNativeAgent` returns `ag`: `jt.SetNotifier(jobs.Notifier{FollowUp: ag.FollowUp, Steer: ag.Steer, Remove: ag.Remove})`; on a constructor error it calls `jt.Close()` and returns the error;
    - `env` closure (P7): `sandbox.ChildEnv(o.environ(), map{ASK_SESSION_ID: sessionID, ASK_PROVIDER, ASK_MODEL, ASK_REASONING_LEVEL from ag.State()})`, evaluated at each job start so a model switch is seen;
    - returns `(*agent.Agent, *jobs.Table, error)`.
  - `options.environ func() []string` (`cmd/tui/args.go:26-41`) and `runDependencies.environ` (`cmd/tui/headless.go:48-54`); nil means `os.Environ`. `acpParams` (`cmd/tui/acp.go:108-137`) copies it into `o`.
  - Print mode end (N2 option A):
    - `promptAll` (`headless.go:385-396`) calls `ag.WaitForIdle(ctx)` before each `Prompt`; when `Prompt` returns `agent.ErrBusy` (a notice started a run in between), it waits for idle again and retries the same prompt.
    - After the last prompt, the prompt goroutine calls `ag.WaitForIdle(ctx)` once more (a notice that arrived during the last prompt starts one more cycle and the model answers it), then signals `done`.
    - The `done` branch of `runHeadless` (`headless.go:322-336`) takes `msgs := ag.State().Messages`, prints with `printReply(msgs, ...)`, and then calls `ag.Dispose()` (which runs `Release` and kills the remaining background jobs). The `bash` description says background jobs end when `ask -p` ends.
    - `printReply` (`cmd/tui/headless_print.go:16-40`) skips trailing `user` messages for which `protocol.IsSystemReminder` is true before it looks for the last assistant message, so a notice that arrives after the idle wait cannot hide the reply.
    - `runWithDependencies` print path (`headless.go:159-170`): `ag, jt, err := newHeadlessAgentWithAuth(...)`; `code := runHeadless(...)`; `jt.KillRemaining()`; `return code`. The sweep runs on every exit path, including the signal path where `abortGrace` (2 s, `headless.go:45`) is shorter than the 3 s kill grace.
  - Callers that change (Contract Verifier list):
    - `builtinTools()` (`headless.go:211`) is removed; callers: `headless.go:248` (now `sessionTools`), `cmd/tui/headless_lifecycle_test.go:148` and `:353` (use `sessionTools(jobs.New(jobs.Options{}), tools.NewFileState())` and close the table in `t.Cleanup`).
    - `newNativeAgentOn` callers: `headless.go:236`, `cmd/tui/acp.go:134` (drops the table; `Release` closes it).
    - `newNativeAgent` (`headless.go:231`) caller: `headless.go:226`.
    - `newHeadlessAgentWithAuth` (`headless.go:221`) callers: `headless.go:159`, `headless.go:218`.
    - `newHeadlessAgent` (`headless.go:217`) keeps its signature `(*agent.Agent, error)`; its 16 test call sites in 9 files (`headless_cassette_test.go` 3, `headless_fault_test.go` 2, `headless_test.go` 2, `headless_tool_abort_test.go` 1, `headless_record_test.go` 3, `headless_request_log_test.go` 1, `headless_live_test.go` 1, `headless_lifecycle_test.go` 2, `headless_dispose_test.go` 1) do not change; their tables close through `Release` when `runHeadless` disposes.
    - `runHeadless` (`headless.go:306`) keeps its signature; its 25 test call sites see one behavior change: the Agent is disposed after a normal end (state stays readable; `streamJSON` ignores `agent_disposed`, `cmd/tui/headless_json.go:19`).
  - Parallel host close (P17): `Host.Close` (`internal/acp/host.go:299-316`) closes its sessions concurrently and joins their errors sorted by session ID. Each `Session.Close` now waits for its job table (about 6 s worst case), and the callers wait with fixed bounds: `ask leader stop` 15 s per step (`cmd/tui/leader.go:53`) and the in-process TUI client 10 s (`inProcessStopWait`, `cmd/tui/tui_client.go:23`). With sequential close, two sessions with jobs that ignore SIGTERM pass the in-process bound and the TUI exits while their groups still live.
  - `command-guard` (Q3, P13): `internal/pipeline/command_guard.go` exports `CommandGuard` of type `pipeline.BeforeToolHandler` (`internal/pipeline/middleware.go:38`). For calls of `bash` and `monitor` it reads `command` from `in.Args` and returns `&BeforeToolCallResult{Block: true, Reason: "blocked by command-guard: <reason>"}` (`internal/pipeline/points.go:93-105`) on: `rm -r`/`-rf`/`-fr` of `/`, `/*`, `~`, `~/`, `$HOME`; `mkfs` and `mkfs.*`; `dd` with `of=/dev/` a disk; `> /dev/sd*` or `/dev/nvme*` or `/dev/disk*`; the fork bomb `:(){ :|:& };:`; `chmod -R` / `chown -R` on `/`. Else it calls `next`. It never reads the environment. The run continues after a block (tool error result).
- Non-functional: no new import of `agent` from `tools`, `jobs` or `sandbox`; `cmd/tui` may import all of them; `internal/pipeline` imports nothing new.

## Architecture

```
newNativeAgentOn(session)
  fs, jt := NewFileState(), jobs.New(env)
  reg := sessionTools(jt, fs) + echo + extra
  cfg.SystemPrompt = PromptSection(reg)    cfg.Release = jt.Close
  [ASK_COMMAND_GUARD=1] cfg.Pipeline = NewRegistry + CommandGuard
  ag := app.NewNativeAgent(cfg); jt.SetNotifier(ag.FollowUp, ag.Steer, ag.Remove)

close paths (all through Agent.Dispose → drain → Release → jt.Close):
  ACP session/close, leader shutdown → Host.Close → Session.Close → Dispose
  in-process TUI client close → same host
  ask -p normal end → WaitForIdle → print → Dispose → KillRemaining
  ask -p signal → Dispose (goroutine) … abortGrace … return → KillRemaining
  Host.Close closes its sessions in parallel
```

## Runtime Flow

- Actor and trigger: a user starts `ask -p`, `ask acp`, `ask leader` or the TUI; a client opens or closes a session; a user sets `ASK_COMMAND_GUARD=1`.
- Registered entry point: `runWithDependencies` (`cmd/tui/headless.go:60`) and `acpParams` → `app.NewACPRuntime` → `acp.Config.Factory` (`internal/app/module_acp.go:117-129`) → `Host.NewSession` (`internal/acp/host.go:259-293`).
- Internal path: as in the Architecture block.
- Prepared data and external mocks: scripted model HTTP; background `sleep 300` jobs; a child that traps TERM.
- Observable result: tools in the request, prompt section in `system`, group gone after every close path, blocked command error.
- End-to-end proof: `TestPrintRequestCarriesToolPromptSection`, `TestPrintCommandGuardBlocksWhenEnabled`, `TestPrintCommandGuardOffByDefault`, `TestPrintExitKillsBackgroundJobs`, `TestPrintSIGINTDuringBashKillsProcessGroup`, `TestPrintNoticeAfterLastPromptIsAnswered`, `TestLeaderSessionCloseKillsBackgroundJobs`, `TestLeaderShutdownKillsBackgroundJobs` (phase 9).

## File inventory

| File | Action | Size | Test impact |
|---|---|---|---|
| `internal/agent/types.go` | modify (`Config.Release`) | +6 | new `internal/agent/dispose_release_test.go` |
| `internal/agent/agent.go` | modify (`dispose` calls `Release`) | +6 | same |
| `internal/pipeline/command_guard.go` | create | ~90 | `command_guard_test.go` |
| `cmd/tui/headless.go` | modify (`sessionTools`, `newNativeAgentOn`, `newNativeAgent`, `newHeadlessAgentWithAuth`, `promptAll`, `runHeadless`, `runWithDependencies`, `runDependencies.environ`) | +80 / -10 | existing headless tests |
| `cmd/tui/headless_print.go` | modify (`printReply` skips trailing reminders) | +10 | new `cmd/tui/headless_print_test.go` |
| `cmd/tui/args.go` | modify (`options.environ`) | +3 | none |
| `cmd/tui/acp.go` | modify (`environ`, discard table) | +3 | ACP tests |
| `cmd/tui/headless_lifecycle_test.go` | modify (two `builtinTools()` sites) | ±6 | itself |
| `internal/acp/host.go` | modify (`Host.Close` closes sessions in parallel) | +10 | `internal/acp/host_test.go` (new case) |

## Tests Before

`go test ./cmd/tui ./internal/agent ./internal/acp ./internal/app ./internal/pipeline` passes on the phase 6 tree; record the counts. Known exception: the six cassette tests (N4) fail once the request body changes; they are fixed in phase 9.

## Tests After (write first)

| Test | Scenario | Priority |
|---|---|---|
| `TestDisposeRunsReleaseAfterToolsDrain` (`internal/agent`) | a tool that blocks until cancelled; `Release` sees the tool returned; error joined | critical |
| `TestDisposeCallsReleaseOnce` | `Dispose` twice | high |
| `TestCommandGuardBlocksCatastrophicCommands` (`internal/pipeline`) | table of 12 commands, real `pipeline.Registry` | critical |
| `TestCommandGuardAllowsOrdinaryCommands` | `rm -rf ./build-out`, `dd if=a of=b`, `echo mkfs` | critical |
| `TestCommandGuardIgnoresOtherTools` | `write` with `rm -rf /` in content | high |
| `TestNativeAgentRegistersSessionTools` (`cmd/tui`) | registry names; echo kept | critical |
| `TestNativeAgentSetsToolPromptSection` | `SystemPrompt` lists the tools with snippets, not echo | high |
| `TestNativeAgentEnablesGuardOnlyWithEnv` | getenv `ASK_COMMAND_GUARD=1` vs unset | high |
| `TestNativeAgentSessionsHaveSeparateTables` | two sessions; job in one is not listed in the other | critical |
| `TestPromptAllRetriesWhenNoticeRunIsActive` | fake notice started between prompts | high |
| `TestPrintReplySkipsTrailingSystemReminders` | messages end with a reminder user message | high |
| `TestHostCloseClosesSessionsInParallel` (`internal/acp`) | three sessions whose `Release` each sleeps 1 s → `Host.Close` < 2 s; errors joined | high |

## Function and interface checklist

- [x] `agent.Config.Release`; `dispose` order: drain → `Release` → `src.Close`
- [x] `pipeline.CommandGuard`
- [x] `sessionTools`, `newNativeAgentOn` (3 results), `options.environ`, `runDependencies.environ`
- [x] `promptAll` idle wait and busy retry; `runHeadless` normal end order; `printReply` reminder skip; exit sweep

## Dependency map

Needs phases 2, 4, 6 (tools), 5 (table), 1 (prompt section, reminder helper). Blocks phase 8 and phase 9. Shares no file with any parallel phase (it runs alone).

## Related Code Files

- Create: `internal/pipeline/command_guard.go` (+ test), `internal/agent/dispose_release_test.go`, `cmd/tui/headless_print_test.go`, `cmd/tui/native_agent_test.go` (the `TestNativeAgent*` and `TestPromptAll*` cases).
- Modify: the files in the inventory (including `internal/acp/host.go` and `internal/acp/host_test.go`).

## Implementation Steps

1. Write the tests; they fail.
2. Add `Config.Release` and the call in `dispose`.
3. Add `CommandGuard`; make `Host.Close` close sessions in parallel.
4. Change `newNativeAgentOn` and its callers in the order: `headless.go`, `acp.go`, `args.go`, the two lifecycle test sites.
5. Change `promptAll`, `runHeadless`, `printReply` and the print path in `runWithDependencies`.
6. Run the gate; the cassette tests are expected to fail until phase 9 (N4).

## Regression Gate

`go test ./internal/agent ./internal/pipeline ./internal/acp ./internal/app ./cmd/tui -race` (minus the six N4 cassette tests, listed in the phase report) `&& go vet ./... && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...`

## Success Criteria

- [x] All new tests pass; all existing tests pass except the six N4 cassette tests.
- [x] `acp.Factory` and `ACPParams.NewAgent` signatures are unchanged.
- [x] Every print-mode exit path ends with `KillRemaining`.
- [x] `ASK_COMMAND_GUARD` is read in one place only (`newNativeAgentOn`).

## Risk Assessment

| Risk | L x I | Mitigation |
|---|---|---|
| `Release` blocks `Dispose` and so `Session.Close`, leader shutdown and the in-process TUI exit | M x M | `jobs.Close` waits one shared grace (about 6 s worst case per session) and `Host.Close` closes sessions in parallel (P17), so the whole host closes in about 6 s, under the 10 s and 15 s bounds; signal: `TestLeaderShutdownKillsBackgroundJobs` with three sessions takes more than 8 s; response: inspect for a serial wait. A crash or SIGKILL of the TUI or the leader still leaves children (Q8, accepted). |
| Normal-end `Dispose` changes a test that reuses an Agent after `runHeadless` | L x M | Grep found no test that prompts the same Agent after `runHeadless`; state stays readable after `Dispose`. |
| A notice run starts between the idle wait and the snapshot | M x L | `printReply` skips trailing reminders; a late run is aborted by `Dispose` and appears only in JSON mode as an aborted cycle (documented). |
| ACP `session/prompt` gets `busy` while a notice cycle runs (`host.go:770`) | M x M | Accepted (same as any queued input); TUI queues input; documented in phase 10. |
| Background jobs live across an ACP `reset` of the same session (`host.go:824`) and notify the new conversation | L x L | The job table is per session by design (architecture data model); documented. |

## Security Considerations

- `command-guard` is off by default and is not a security boundary (Q3); the README says so (phase 10).
- `ASK_*` variables of the leader process (for example `ASK_HOME`, `ASK_COMMAND_GUARD`) never reach commands; only the four session variables do.
