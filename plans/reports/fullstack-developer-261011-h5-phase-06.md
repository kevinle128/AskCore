# H5 phase 6: shell and job tools

Status: completed. Tools `bash`, `job_output`, `job_kill`, `job_list` and `monitor` over the session `jobs.Table`. Tests were written first (compile failure on `tools.NewShellTools` seen), then the code.

## Verification

- `go test ./internal/tools/... ./internal/jobs/... ./internal/sandbox/... -race -count=3`: pass.
- Shell tests alone: `-race -count=15` pass (one flake in my own test found and fixed, see below).
- `go vet ./...` clean. golangci-lint on `./internal/tools/... ./internal/jobs/...`: 0 issues (depguard allows `tools` -> `jobs`; no rule denies it, and `tools` still does not import `agent`).
- `ps` after the runs: no stray `sleep`, `seq` or shell loops; no `ask-jobs-*` directory left. Every test that records a pid checks `kill(-pgid, 0) == ESRCH` in cleanup, and the cancel test checks it when the body returns.

## Files (all new, in `internal/tools`)

`shell_bash.go` (Bash, `NewShellTools`, trailing `&` check, view and details helpers), `shell_jobs.go` (JobOutput, JobKill, JobList), `shell_monitor.go` (Monitor), and tests `shell_bash_test.go` (harness, fake clock, bash tests), `shell_jobs_test.go`, `shell_monitor_test.go`. No other file changed except the phase file (checklist and `status: completed`).

Every row of "Tests After" exists under its name. Extra tests: exit 0 and stderr, empty output, non-positive timeout, no update after the move to background, signal header, leading `cd`, job cap, empty command, declaration contract, wait with `job_output`, oversized `job_output`, kill of a finished job, unknown kill id, non-positive monitor timeout, monitor lines reach the notifier.

## Exported API for phase 7

- `tools.NewShellTools(table *jobs.Table) []Tool` returns `bash`, `job_output`, `job_kill`, `job_list`, `monitor` in that order. It panics on a nil table (a composition bug).
- Types `tools.Bash`, `tools.JobOutput`, `tools.JobKill`, `tools.JobList`, `tools.Monitor`; fields are unexported, so use the constructor. All implement `Decl`, `Execute`, `PromptSnippet`, `PromptGuidelines`. `JobList` is always `ConcurrencySafe`; `JobOutput.ConcurrencySafe(args)` is true unless `wait` is true. The other three run alone.
- Wiring: build one `jobs.Table` per session with `jobs.New(jobs.Options{Env: ..., OnError: ...})`, call `table.SetNotifier(...)` after the Agent exists, register the five tools, and close the table from `Config.Release`. The tools take the working directory from `tools.Context.Cwd` (the coordinator sets it per call), and the environment from `Options.Env` of the table (the `sandbox.ChildEnv` closure with the session variables ASK_SESSION_ID, ASK_PROVIDER, ASK_MODEL, ASK_REASONING_LEVEL). The tools hold no cwd or env of their own.
- Table cap errors (`jobs.ErrTooManyJobs`) reach the model as `Too many active jobs (10). Stop one with job_kill or wait for one to finish.`

## Behavior notes and deviations

1. Tool-level refusals return `failf` errors (the coordinator turns them into error results), like the file tools: trailing `&`, non-positive `timeout`, unknown job id, monitor timeout above max, job cap. A non-zero exit and an aborted command are results with `IsError=true` so that the output stays visible.
2. `monitor` details are `protocol.ToolResultDetails` plus `timeoutMs` and `persistent` (embedded struct in `shell_monitor.go`, flat JSON). `pkg/protocol/tool.go` belongs to phase 1 and has no such fields, so phase 8 reads the two extra keys from the raw details if it needs them. The ToolResultDetails decode ignores them.
3. `timeout` above 600 is cut to 600 and every result then ends with `[timeout limited to 600 s]`. A timeout of exactly 600 gives no note. `timeout` of 0 or less, or NaN, is an argument error.
4. `run_in_background` calls `Start` only (no `Wait`), so a job that ends at once still gets its notice and the result is not misleading.
5. Partial updates: a 50 ms ticker on real time, one update only when the output grew since the last one, sent from the body goroutine; `Wait` runs on its own goroutine and the body returns after it. No update is sent after the call returns.
6. `Details.Truncation.TotalLines` stays 0 for `bash`, because the runner does not count lines it drops. `TruncatedBy` is `lines` when the view shows 2000 lines, else `bytes`.
7. `job_output` text: `ReadResult.Text` is used as it is; when its output part is over 2000 lines or 50 KB it is cut to head and tail with `sandbox.View` and the status line and the "do not poll" line are kept.
8. `job_kill` of an already finished job says `Job bash-1 had already finished.` instead of `killed`, because `Table.Kill` only removes a finished job.
9. A killed command that ended by a signal shows the kill reason in the header, for example `signal: SIGTERM (session closed)`.
10. Trailing `&` check scans quotes, escapes and comments, and treats `&&`, `>&`, `<&` and `|&` as not background. A heredoc body that ends in `&` is not parsed.
11. `monitor` also rejects `timeout_ms` of 0 or less (the table would turn it into 10 h); the spec does not list this, review the text in phase 10.
12. The tools carry no `//go:build unix` tag, like `search_grep.go`, which already uses `sandbox.Start`. `internal/jobs` has the tag, so the package builds on unix only, as before.

## Test notes

- The fake clock in `shell_bash_test.go` is a copy of the one in `internal/jobs` tests (it is package-private there).
- Timing asserts follow the phase table: `run_in_background` under 100 ms and `job_output` under 50 ms. They held in 15 runs with `-race` and in the parallel package run.
- Found and fixed a flake in `TestBashTimeoutMovesToBackground`: the fake clock fired before the reader goroutine saw the first line. The test now waits for a ready file and 100 ms.

## Unresolved questions

- `job_output` with `wait: true` that times out on a running job returns the table's "do not poll" line (from `jobs.Read`); the phase text reserves that line for reads without `wait`. The text is true; left as is.
- Should phase 8 map `timeoutMs` and `persistent` of the monitor details into the ACP frame, or are `kind` and `jobId` enough? Not decided here.
- `job_list` shows no exit code for finished-but-unread jobs (spec format only). Say if the model should see it there.
