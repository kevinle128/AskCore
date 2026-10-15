---
phase: 3
title: "Process runner in sandbox"
status: completed
priority: P1
effort: "2d"
dependencies: [1]
---

# Phase 3: Process runner in sandbox

## Context

- Architecture: [Process runner (`sandbox`)](../architectures/h5-builtin-tools-architecture.md#process-runner-sandbox) (process group, environment, head and tail buffer, spill, two-wave kill), [Other](../architectures/h5-builtin-tools-architecture.md#other) (test rules: real processes, injected clock, `goleak`, ESRCH), [Spike results](../architectures/h5-builtin-tools-architecture.md#spike-results-2026-10-10).
- Decisions: [Q2 bash output truncation](../architectures/h5-builtin-tools.md#q2-bash-output-truncation), [Q12 child environment](../architectures/h5-builtin-tools.md#q12-child-environment-of-bash), [Q13 package placement](../architectures/h5-builtin-tools.md#q13-package-placement), [Q15 spill file cap](../architectures/h5-builtin-tools.md#q15-spill-file-cap), [default calls: no PTY, no cgroup](../architectures/h5-builtin-tools.md#low-cost-reversible-calls-decided-by-default-user-may-override).
- Spike 2: [Facts the plan must keep](../reports/spike-261010-output-buffer.md#facts-the-plan-must-keep), [scenario 4, grandchild holds the pipe](../reports/spike-261010-output-buffer.md#4-grandchild-keeps-the-pipe-open-sleep-30--echo-started), [scenario 5, SIGTERM ignored](../reports/spike-261010-output-buffer.md#5-process-that-ignores-sigterm-trap--term-while-true-do-echo-x-done-kill-after-2-s), [Findings](../reports/spike-261010-output-buffer.md#findings).
- xia H5: ["What to take"](../reports/xia-261010-1727-h5-builtin-tools.md#what-to-take-no-conflict-with-the-contract) row `bash` (non-interactive env, process group, `exit: N`, ANSI strip, `\r` collapse); [Risks](../reports/xia-261010-1727-h5-builtin-tools.md#risks) (process-tree kill races, spill files leak secrets).
- Inventory: H-TOOL-04 `bash` output, H-TOOL-22 shell and env, H-TOOL-23 process kill ([inventory-harness.md](../260930-2254-pi-feature-inventory-go-roadmap/inventory-harness.md) lines 54, 72, 73); roadmap [H5](../260930-2254-pi-feature-inventory-go-roadmap/roadmap.md#h5-built-in-tools) and D7, D8, D19.

## Overview

Build the Unix process runner that `jobs`, `grep` and `find` use: one command in its own process group, a filtered environment, a bounded head and tail buffer with an overflow spill file, a two-wave group kill and a bounded pipe drain. No job table, no tool code.

## Requirements

- Functional:
  - `sandbox.Start(spec Spec) (*Process, error)`. `Spec{Argv []string; Dir string; Env []string; Clock Clock; SpillFile func() (*os.File, error); Observe func(stream Stream, chunk []byte)}`. `Argv` is run as is (grep and find pass `rg`/`fd` argv); `sandbox.ShellArgv(command)` gives `[<shell>, "-c", command]`.
  - Shell (H-TOOL-22, Unix only per D8): `/bin/bash` when it exists, else `bash` on `PATH`, else `/bin/sh`. Resolved once per process with `sync.OnceValue`.
  - Process: `SysProcAttr{Setpgid: true}`; `Stdin` nil (reads `/dev/null`); `Dir` explicit (never the daemon cwd); stdout and stderr are the write ends of two `os.Pipe` pairs, closed in the parent right after `Start` (spike 2: `Wait` then does not wait for the readers).
  - Readers: one goroutine per stream, 32 KB read buffer, one shared buffer under one mutex. `Observe` (optional) gets each chunk in reader order, after the buffer write; it must not block (the job table copies and queues).
  - Buffer: keeps the first 20 KB (head) and a 30 KB tail ring; counts `total`; `dropped = total - len(head) - len(tail)` (invariant tested). `Process.Snapshot() Output{Head, Tail []byte; Total, Dropped int64; SpillPath string; SpillCapped bool; SpillErr error}` copies under the lock; the caller builds views outside the lock.
  - Byte cursor reads for `job_output`: `Process.ReadFrom(cursor int64) (data []byte, next int64, skipped int64)` returns the bytes from `cursor` that the buffer still holds; `skipped` counts bytes between `cursor` and the oldest kept byte that are no longer in memory (they are in the spill file when one exists).
  - Spill (Q15): when `SpillFile` is nil (every `sandbox.Run` caller, for example `grep` and `find`), there is no spill: the buffer keeps head and tail only. Else, when `total` first passes 50 KB, call `SpillFile()` (the caller creates the file, mode 0600), write head plus ring, then tee every later chunk. At 64 MB the file stops growing, `SpillCapped` becomes true, and the process keeps running. A spill write error stops the tee, keeps the process and is reported in `Output.SpillErr`.
  - View: `sandbox.View(out Output, maxLines=2000, maxBytes=50*1024) (text string, t Truncation)` gives the model text: head, then `\n[... N bytes elided ...]\n`, then tail, then the spill line `[full output: <path>]` and `[spill file capped at 64 MB]` when they apply; strips ANSI CSI/OSC sequences; collapses `\r` progress (keep the text after the last `\r` of each line); keeps whole UTF-8 runes at both cuts; works with a tail that has no newline.
  - Kill: `Process.Kill()` sends SIGTERM to `-pgid` and arms a 3 s timer that sends SIGKILL to `-pgid`; the timer stays armed after the main process exits (grandchildren). `Process.KillNow()` sends SIGKILL to `-pgid` at once and does not wait (used by the print-mode exit sweep, phase 7). Both are idempotent and safe from any goroutine.
  - Wait: `Process.Done() <-chan struct{}` closes after: `cmd.Wait` returned; the readers ended or the 3 s drain bound passed (then SIGKILL to the group and close both read ends, which unblocks the readers); and `kill(-pgid, 0)` returned ESRCH (else SIGKILL and poll every 10 ms up to 1 s). `Process.Result() Result{ExitCode *int; Signal string; Err error}`.
  - Environment (Q12, P6): `sandbox.ChildEnv(base []string, session map[string]string) []string`: drop names matching `(?i)(KEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL)` and every `ASK_*` name; then set `TERM=dumb`, `NO_COLOR=1`, `CI=true`, `PAGER=cat`, `GIT_PAGER=cat`, `MANPAGER=cat`, `GIT_TERMINAL_PROMPT=0`, `EDITOR=true`, `VISUAL=true`, `GIT_EDITOR=true`, `SSH_ASKPASS=`; then the session map (`ASK_SESSION_ID`, `ASK_PROVIDER`, `ASK_MODEL`, `ASK_REASONING_LEVEL`). Output sorted by name for stable tests.
  - `sandbox.Run(ctx, spec) (Output, Result, error)`: start, wait for `Done` or `ctx` done (then `Kill` and wait for `Done`). For grep and find (phase 4).
  - Clock (P4): `type Clock interface{ Now() time.Time; AfterFunc(d time.Duration, f func()) (stop func() bool) }`, `sandbox.SystemClock`. The kill timer and the drain bound use it; tests use a fake clock for timer order and real time only where the OS must act.
- Non-functional: every exported function is safe for concurrent use; `goleak.VerifyTestMain` in the package; after each process test `kill(-pgid, 0)` returns ESRCH; no PTY, no cgroup; Unix only (`//go:build unix` on the process files).

## Architecture

```
Start(spec)
  os.Pipe x2 → exec.Cmd{Argv, Dir, Env, Stdout: wOut, Stderr: wErr, Setpgid}
  cmd.Start → close wOut, wErr in parent
  reader(out) ─┐ 32 KB reads → buffer.write (lock) → spill tee (≤64 MB) → Observe
  reader(err) ─┘
  waiter: cmd.Wait → drain bound (Clock.AfterFunc 3 s → SIGKILL group, close read ends)
          → readers joined → group gone check (ESRCH) → close(done)
Kill(): SIGTERM -pgid; AfterFunc(3 s, SIGKILL -pgid) stays armed
KillNow(): SIGKILL -pgid
```

Lifetime: a `Process` lives from `Start` to `Done`; the job table (phase 5) or `Run` owns it. Nothing in `sandbox` is process-global except the cached shell path.

## Runtime Flow

- Actor and trigger: the model calls `bash`, `grep`, `find` or `monitor`; the runner never runs on its own.
- Registered entry point: the tools registered in `newNativeAgentOn` (phase 7) through `jobs` (phase 5) or `sandbox.Run` (phase 4).
- Internal path: tool body → `jobs.Table.Start` or `sandbox.Run` → `sandbox.Start` → OS process group.
- Prepared data and external mocks: real short-lived processes (`bash -c`, `yes`, `sleep`, `trap`); no mocks.
- Observable result: output view, exit code, spill file, group gone.
- End-to-end proof: `TestPrintBashRunsTestsAndReportsExit`, `TestPrintBashBigOutputKeepsHeadAndTail`, `TestPrintSIGINTDuringBashKillsProcessGroup`, `TestPrintBashEnvironmentHidesSecrets` (phase 9).

## File inventory

| File | Action | Size | Test impact |
|---|---|---|---|
| `internal/sandbox/clock.go` | create | ~40 | `clock_test.go` (fake clock helper in `sandbox_test` package file `fakeclock_test.go`) |
| `internal/sandbox/env.go` | create | ~70 | `env_test.go` |
| `internal/sandbox/shell.go` | create | ~30 | `shell_test.go` |
| `internal/sandbox/buffer.go` | create | ~150 | `buffer_test.go` |
| `internal/sandbox/view.go` | create | ~120 | `view_test.go` |
| `internal/sandbox/process.go` | create (`//go:build unix`) | ~220 | `process_test.go` |
| `internal/sandbox/run.go` | create | ~40 | in `process_test.go` |
| `internal/sandbox/main_test.go` | create (`goleak.VerifyTestMain`) | ~10 | all |
| `internal/sandbox/doc.go` | modify (package comment: process runner, no `Sandbox`/`Manager` yet) | ~5 | none |

## Tests Before

`go test ./internal/sandbox` (package has only `doc.go`; passes with no tests). `go vet ./internal/sandbox`.

## Tests After (write first, see them fail)

| Test | Scenario | Priority |
|---|---|---|
| `TestChildEnvDropsCredentialShapedNames` | `OPENAI_API_KEY`, `GITHUB_TOKEN`, `AWS_SECRET_ACCESS_KEY`, `DB_PASSWORD`, `PGPASSWD`, `GOOGLE_APPLICATION_CREDENTIALS`, `npm_config__authToken`, `MY_KEYRING_PATH` (dropped: name has KEY; documented false positive) | critical |
| `TestChildEnvKeepsOrdinaryNames` | `PATH`, `HOME`, `LANG`, `GOPATH`, `SSH_AUTH_SOCK`, `USER` kept | critical |
| `TestChildEnvReplacesInheritedAskVars` | inherited `ASK_HOME`, `ASK_SESSION_ID=old`, `ASK_COMMAND_GUARD` dropped; session map wins | critical |
| `TestChildEnvAddsNonInteractiveVars` | `TERM=dumb` overrides `TERM=xterm-256color`; `SSH_ASKPASS` empty; pagers `cat`; editors `true` | high |
| `TestShellPrefersBinBash` | `/bin/bash` exists on macOS and Linux CI | medium |
| `TestBufferKeepsHeadAndTailAndCountsDropped` | 1 MB written in 7 KB chunks; invariant `dropped = total - head - tail` | critical |
| `TestBufferReadFromReportsSkippedBytes` | cursor inside the dropped middle; cursor at end | high |
| `TestViewElidesMiddleAndNamesSpill` | head, marker, tail, spill line | critical |
| `TestViewCutsAt2000Lines` | 5000 short lines | high |
| `TestViewHandlesTailWithoutNewline` | 50 MB without newline (spike 2 scenario 3 shape, 200 KB in test) | high |
| `TestViewStripsAnsiAndCollapsesCarriageReturns` | `\x1b[31mred\x1b[0m`, `10%\r50%\r100%` | high |
| `TestViewNeverSplitsRune` | multibyte runes at both cuts | medium |
| `TestRunnerReportsExitCodeAndOutput` | `echo hi; exit 3` | critical |
| `TestRunnerUsesDirAndClosedStdin` | `pwd; cat` returns at once with dir | high |
| `TestRunnerSpillStartsOnOverflowWithMode0600` | 200 KB output; file mode 0600; file holds every byte | critical |
| `TestRunnerSpillStopsAtCap` | cap lowered by an unexported test hook to 1 MB; `yes` for 1 s; file size = cap, `SpillCapped`, process still running until killed | critical |
| `TestRunnerKillStopsGroupWithGrandchild` | `sleep 300 & sleep 300`; `Kill`; ESRCH after `Done` | critical |
| `TestRunnerKillEscalatesWhenTermIgnored` | `trap '' TERM; while :; do echo x; done`; `Done` within 3 s + 1 s; signal is SIGKILL | critical |
| `TestRunnerDrainIsBoundedWhenGrandchildHoldsPipe` | `(sleep 30 &); echo started` → `Done` at about 3 s, output `started`, group gone | critical |
| `TestRunnerKillNowSendsSigkillWithoutWait` | returns in < 50 ms; group gone after `Done` | high |
| `TestRunnerFloodStaysBounded` | `yes` for 2 s; heap delta < 16 MB; `Kill` → `Done` < 1 s | high |
| `TestRunnerObserveSeesEveryChunkInOrder` | 3 writes with sleeps | high |
| `TestRunCancelledContextKillsAndWaits` | ctx cancel during `sleep 300` | critical |

## Function and interface checklist

- [x] `Clock`, `SystemClock`
- [x] `ChildEnv`, `ShellArgv`
- [x] `Start`, `Spec`, `Stream`, `Process` (`Kill`, `KillNow`, `Done`, `Result`, `Snapshot`, `ReadFrom`, `PGID`)
- [x] `Output`, `Result`, `Truncation`, `View`
- [x] `Run`

## Dependency map

Needs phase 1 only for the depguard entry of `internal/sandbox`. Blocks phases 4 (`Run`), 5 (`Start`, `View`, `Clock`). No shared file with phase 2.

## Related Code Files

- Create: the files in the inventory above.
- Modify: `internal/sandbox/doc.go`.
- Do not modify: `internal/sandbox/README.md` (phase 10).

## Implementation Steps

1. Write `main_test.go` with `goleak.VerifyTestMain` and a helper `requireGroupGone(t, pgid)` that polls `syscall.Kill(-pgid, 0)` for ESRCH up to 2 s.
2. Write the tests above; run; they fail to compile.
3. `env.go`, `shell.go`, `buffer.go`, `view.go` (pure code first).
4. `process.go`: pipes, readers, waiter, kill timer, drain bound, group check; `run.go`.
5. Run the regression gate with `-race -count=3` to catch timing flakes.

## Regression Gate

`go test ./internal/sandbox -race -count=3 && go vet ./internal/sandbox && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./internal/sandbox/...`

## Success Criteria

- [x] All tests pass three times in a row with `-race`; `goleak` reports nothing.
- [x] Every process test asserts ESRCH after `Done`, not at `cmd.Wait` return (spike 2, finding 8).
- [x] The env tests list every name above; the filter regexp is one constant.
- [x] Spill file mode is 0600 and its size never passes the cap.

## Risk Assessment

| Risk | L x I | Mitigation |
|---|---|---|
| Linux pipe behavior differs from the macOS spike | M x M | Design does not depend on pipe size; signal: a process test fails on Linux CI; response: adjust the drain test bound, keep the design (spike 2 "Limits"). |
| A secret with an unusual name passes the env filter | M x M | Accepted by Q12 ("Known risk"); documented in phase 10. The filter also catches false positives such as `MY_KEYRING_PATH`; accepted (fail closed). |
| A child that calls `setsid` leaves the group and survives the kill | L x M | Pi and DeepSeek have the same limit; documented; no cgroup in H5 (default call). |
| Flood costs one core until killed (spike 2) | M x L | Bounded by the model's `job_kill`, session close or the monitor overload kill; no byte-rate limit (spike 2 did not need it). |
| Fake clock and real OS time mix and flake | M x M | Fake clock only for timer order; OS-facing tests use real time with generous bounds and `-count=3`. |

## Security Considerations

- The env filter is the only secret barrier for commands (Q12); it is not a sandbox (D3, H-SEC-03).
- Spill files are 0600 in a 0700 directory that the job table owns (P5) and removes on close.
- `KillNow` and `Kill` signal only the group the runner created (`-pgid` of its own child); a reused PID cannot be hit because the group is checked gone before `Done` closes and no signal is sent after `Done`.
