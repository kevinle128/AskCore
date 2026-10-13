# H5 phase 7: composition, session close and command-guard

Status: completed. The tools, job table, prompt section, child environment, opt-in command-guard and close path are wired into `newNativeAgentOn`, which print mode, `ask acp`, `ask leader` and the in-process TUI agent share. Tests were written first (compile or assertion failures seen), then the code.

## Verification

- `go test ./...`: every package passes except `cmd/tui`, which fails only the six cassette tests listed below (N4, phase 9).
- `go test -race ./cmd/tui` with those six skipped: pass (237 s). `go test -race ./internal/agent ./internal/pipeline ./internal/acp ./internal/app ./internal/jobs`: pass.
- `go vet ./...` clean. `golangci-lint run ./...`: 0 issues.
- `ps` and `/tmp`: no stray `sleep` processes and no `ask-jobs-*` directory after the runs.

### Cassette tests that fail only because the request body changed (N4)

Each prints `request 1 body does not match the recording; re-record it with ASK_RECORD=1`; the diff shows the new `system` block and `tools`. They stay failing and are listed, not weakened:

1. `TestCassetteTextReply`
2. `TestCassetteOneToolCall`
3. `TestCassetteParallelToolCalls`
4. `TestCassetteJSONEventOrder`
5. `TestCassetteThinkingReply`
6. `TestCapturedSessionReplays`

## Files changed

- `internal/agent/types.go`, `agent.go`: `Config.Release`; `dispose` calls it after the run and its tools drained, before the log closes, and joins its error. New `dispose_release_test.go`.
- `internal/pipeline/command_guard.go` + `command_guard_test.go`: `CommandGuard` (a `BeforeToolHandler`).
- `internal/acp/host.go`, `host_test.go`: `Host.Close` closes sessions in parallel and joins errors sorted by session ID; new test.
- `cmd/tui/headless.go`: `sessionTools`, `newNativeAgentOn` (returns the table), `newNativeAgent`, `newHeadlessAgentWithAuth`, `promptAll` (idle wait, busy retry through a small `promptTarget` interface), `runHeadless` (dispose at normal end), print path sweep, `runDependencies.environ`.
- `cmd/tui/headless_print.go`: `printReply` skips trailing system reminders.
- `cmd/tui/args.go`, `acp.go`: `options.environ`, copied in `acpParams`; the ACP factory drops the table.
- New tests: `cmd/tui/native_agent_test.go`, `cmd/tui/headless_print_test.go`.
- `cmd/tui/headless_lifecycle_test.go`: two `builtinTools()` sites now use `sessionTools`; the "slow listener" comparison leaves out `agent_disposed`, because `runHeadless` now disposes the Agent.
- Phase file: checklist ticked, `status: completed`.

## Deviations and decisions for the orchestrator

1. `sessionTools(jt, fs, modelInput)` takes a third argument. `NewFileTools` needs the model-input closure, and the phase text shows two arguments. Test sites pass `nil` (images always allowed).
2. `cmd/tui/headless_test.go` (not on the ownership list) changed: the `TestJSONHelloLines` golden holds faux usage numbers, which count the system prompt. With the tool prompt section the input estimate moved from 54 to 2513 (total 108 to 5026, 110 to 5028). Only those numbers changed. The golden now depends on the length of the tool snippets, so phase 8 to 10 edits to snippet text will move it again; consider normalizing usage in that test.
3. `OnError` of the job table writes one `jobs: <error>` line to stderr. There is no logger in `cmd/tui`; a refused notice is rare, and silence would hide it. In the full-screen TUI that line could disturb the screen; say if it should be dropped there.
4. On a `readPrompts` error in print mode the Agent is now disposed (no job table is left open).
5. Extra coverage beyond "Tests After": `TestNativeAgentKeepsExtraTools`, `TestNativeAgentDisposeClosesTable`, `TestNativeAgentJobsGetSessionEnvironment` (secret and `ASK_*` filtering, session variables), `TestPromptAllStopsOnOtherErrors`, `TestPromptAllWaitsForNoticeRunOfRealAgent`, `TestPrintEndDisposesAgentAndKillsBackgroundJobs` (real `runWithDependencies`, a job that traps TERM, group gone at return), `TestCommandGuardPassesOnMalformedArguments`, and a second print-reply error case.
6. `command-guard` scope beyond the listed names: it looks through `sudo`/`env`-style wrappers, `NAME=value` prefixes, chains (`;`, `&&`, `|`) and `bash -c "..."` strings (three levels), and also checks disk names `hd`, `vd`, `xvd`, `mmcblk`, `rdisk`. `chgrp -R /` is included with `chmod` and `chown`. It is not a security boundary.
7. The N1 TUI notice hiding is untouched, as instructed. In the in-process TUI and the leader TUI, notices stay visible until that slice lands.

8. Out-of-ownership edit: `cmd/tui/headless_test.go` (see 2). It is a golden update caused by this phase, not a signature change; reported here as a file-ownership exception.
9. Signal path, Review 2 update (2026-10-11): `Table.KillRemaining` and `Table.Close` both go through `stopAll` under the table mutex.
   The earlier claim that `KillRemaining` never touches the spill directory is superseded.
   A built-binary SIGINT test found that exit could leave the private spill directory while `Dispose` still waited for a job that ignored SIGTERM.
   `KillRemaining` now removes the spill directory under its separate lock and reports a cleanup error through `Options.OnError`, without waiting for the job owners.
   Open file handles can drain after unlink on Unix, and the closed spill state prevents a new spill file.
   `TestPrintSignalRemovesSpilledJobOutput` and `TestKillRemainingRemovesSpillDirectory` failed before the fix and pass after it.
   The built-binary test checks exit 130, an empty print stream, removal of the private output directory, and removal of the process group.
   See [the exit sweep](../../internal/jobs/table.go) and [the built-binary regression](../../cmd/tui/tools_lifecycle_review_test.go).
10. The "every print-mode exit path ends with `KillRemaining`" box is ticked with one exception: the `readPrompts` error path disposes the Agent but does not sweep, because no prompt ran and no job can exist.
11. `status: completed` was set by editing the phase frontmatter, as the task asked. `ak plan` mutates plans in the local plan store (not repo-local plan folders), so it was not used.

## Unresolved questions

- Should the `jobs:` stderr line stay for the TUI paths (deviation 3)?
- Should the `TestJSONHelloLines` golden normalize usage (deviation 2)?
