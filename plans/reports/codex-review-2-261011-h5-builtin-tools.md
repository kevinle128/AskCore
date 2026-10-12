# H5 built-in tools: second independent review

Date: 2026-10-11.
Status: Review and fixes complete; all required final gates pass.

## Scope and authority

Reviewed the uncommitted implementation against [the plan and its ten phases](../261010-2349-h5-builtin-tools/plan.md), [the architecture](../architectures/h5-builtin-tools-architecture.md), and [the fixed decisions](../architectures/h5-builtin-tools.md).
Read the phase reports, [real-provider evidence](real-provider-acceptance-261011-h5-tools.md), and [the first review](codex-review-261011-h5-builtin-tools.md) before checking for new defects.
The architecture and decision files were not changed by this review.
No commit, stash, live provider request, or credential update was made.
Dirty plan files outside the requested plan and reports scope were left alone.

The review checked these contracts:

| Inventory rows | Checks |
|---|---|
| H-TOOL-01..03 | Read paging, binary and image handling, file mutations, generated-file refusal, edit matching, stale reads, BOM and CRLF, path locks and atomic replacement. |
| H-TOOL-04 | Wait-budget defaults and units, background handoff, live updates, exit errors, cancellation, head and tail limits, spill modes and cap. |
| H-TOOL-05..07 | Search parameters, limits, messages, ignore rules, fallback order, file names, Unicode, long lines and cancellation. |
| H-TOOL-11, 19, 20 | Prompt contributions, cwd and path forms, screenshot variants, image size and format limits, and the non-vision result. |
| H-TOOL-22, 23; H-SEC-03 | Shell selection, credential-name filter, inherited ASK variables, group signals, pipe drain, normal and signal exits, optional guard, and OS-permission scope. |

Checked tool registration in the shared native-agent composition, ACP result kinds and diffs, Release ordering, parallel Host.Close, print idle waits, job cursors and notices, monitor batching and overload, cleanup, and the owning documentation.
Fixed decisions take precedence over inventory defaults: bash timeout is a 120-second wait budget with a 600-second cap; output keeps head and tail; kill grace is three seconds; monitor has the selected separate-tool contract.

## Defects fixed

Each defect below has a regression that failed before its fix.
File, search, guard and shell regressions use the real print-mode composition with only the provider HTTP reply replaced.
Job races use real child processes and the production job table; controlled clocks or callback gates select the race order.
The signal cleanup regression runs the CLI as a separate process.

| Defect | Repair and code | Regression |
|---|---|---|
| Unicode find patterns and ignore rules failed | Consume complete UTF-8 runes when translating glob literals and escaped literals. [search_walk.go:87](../../internal/tools/search_walk.go#L87) | `TestPrintFindMatchesUnicodeNamesAndIgnoreRules`; Unicode glob cases in `search_walk_test.go`. |
| Find removed spaces from valid file names | Remove only line terminators from fd/rg output. [search_find.go:158](../../internal/tools/search_find.go#L158) | `TestPrintFindKeepsFilenameSpaces`; `TestFindKeepsUnicodeAndSpacedNamesAcrossSources`. |
| Write overwrote generated files with long headers | Check all five header lines in the already-read file, without a 4 KiB prefix cut. [file_write.go:75](../../internal/tools/file_write.go#L75) | `TestPrintWriteRefusesGeneratorMarkerAfterLongHeader`; `TestWriteChecksAllFiveHeaderLinesForGeneratorMarkers`. |
| Grep lost matches on lines above 4 MiB | Project the JSON stream to bounded preview strings and discard unused arrays before decoding, while keeping match metadata. [search_grep.go:295](../../internal/tools/search_grep.go#L295) | `TestPrintGrepKeepsMatchesOnLargeLines`; split-input, escaped Unicode, base64 and large-array tests in `search_grep_test.go`. |
| Positive fractional monitor timeout selected ten hours | Round the resolved timeout up to a whole millisecond, so a positive value cannot become the default sentinel. [shell_monitor.go:88](../../internal/tools/shell_monitor.go#L88) | `TestPrintMonitorFractionalTimeoutDoesNotSelectDefault`; `TestMonitorFractionalTimeoutUsesOneMillisecond`. |
| Immediate background bash omitted its timeout-cap note | Use the same cap note for foreground and immediate-background results. [shell_bash.go:116](../../internal/tools/shell_bash.go#L116) | `TestPrintBackgroundBashReportsClampedTimeout`. |
| Canceled job reads consumed output or removed a final job | Preserve the cursor and final job when the reader is canceled; restore a notice if that reader had suppressed it. [read.go:75](../../internal/jobs/read.go#L75) | `TestCancelledReadKeepsUnreadOutput`, `TestCancelledReadKeepsFinishedJob`, `TestCancelledReadAtCompletionRestoresNotice`; tool check `TestJobOutputCanceledWaitKeepsUnreadOutput`. |
| Completion used stale read/close state | Recheck live flags and claim one notice immediately before delivery, after stopping the timeout timer. [notice.go:77](../../internal/jobs/notice.go#L77) | `TestNoticeRechecksReadAndCloseAfterTimerStop`. |
| Pending monitor merges grew without a limit | Keep the latest 3,000 characters and a cut marker in one queued event. [monitor.go:324](../../internal/jobs/monitor.go#L324) | `TestMonitorBoundsPendingMessage`. |
| Monitor could deliver after close during a merge | Recheck mute state after removing the prior pending message. [monitor.go:333](../../internal/jobs/monitor.go#L333) | `TestMonitorDropsDeliveryWhenClosedDuringMerge`. |
| Job output omitted the spill-cap warning and detail | Carry SpillCapped through ReadResult and tool details; show the cap warning even after the read cursor passes the gap. [read.go:90](../../internal/jobs/read.go#L90), [shell_jobs.go:104](../../internal/tools/shell_jobs.go#L104) | `TestPrintJobOutputReportsSpillCap`; `TestReadReportsCappedSpillAfterCursorPassesGap`. |
| Signal exit left private spill directories | The final forced sweep unlinks the directory and prevents later spill creation, without waiting for owner callbacks. [table.go:581](../../internal/jobs/table.go#L581) | `TestPrintSignalRemovesSpilledJobOutput`; `TestKillRemainingRemovesSpillDirectory`. |
| Guard refused harmless quoted fork-bomb examples | Reuse the quote scanner to check executable syntax, while retaining shell-string and command-substitution checks. [command_guard.go:40](../../internal/pipeline/command_guard.go#L40) | `TestPrintCommandGuardAllowsQuotedForkBombText`; command-guard ordinary and catastrophic command cases. |

No dependency was added.
No existing test was weakened or deleted.
The earlier seven fixes remain in place.
The fractional job_output wait already worked; an additional print-mode check preserves that behavior without changing its conversion.
The first full gate found an old flood-test assertion that counted XML wrappers instead of delivered events.
`TestPrintMonitorFloodGuardSuppressesEvents` now checks the first ten event lines, the refill bound, suppression, and process-group cleanup inside monitor messages.
It excludes raw output returned by job_kill.
The signal spill-cleanup regression covers SIGINT and SIGTERM, including a child that traps TERM.

Updated the jobs and tools READMEs for the repaired behavior, corrected two stale sandbox examples in [the architecture reference](../../docs/ask-architecture-reference.md), and corrected the old forced-cleanup claim in [the phase-7 report](fullstack-developer-261011-h5-phase-07.md).
CLAUDE.md and the cassette guide match the current tool composition, provider, session-ID rule and capture behavior.

## Security, cleanup and proof limits

The environment filter matches the accepted credential-name pattern and removes inherited ASK variables before adding the four session variables.
Spill directories are private, files are 0600, and the 64 MiB cap does not stop the job.
The process and job tests check group removal and goroutine leaks.
Notifier calls remain outside the table mutex; a delivery claimed before a later read or close can overlap that operation.
The new checks prevent the reproduced stale-state deliveries without holding the table mutex across an Agent callback.

Checked all 46 Runtime Flow Proof Matrix rows and all 47 named test functions; every named function exists.
The matrix's blanket PASSED count is broader than the available proof:

- Accepted N1: TUI notice hiding belongs to the separate design slice and still gates release.
- Accepted leader limitation: `TestLeaderSessionCloseKillsBackgroundJobs` skips because the wire has no session/close operation.
  Leader shutdown and in-process client close have real cleanup tests.
- Accepted provider limitation: ChatGPT OAuth remains unverified after refresh rejection.
  The prior acceptance report proves the OpenAI API-key and Anthropic OAuth runs.
- The non-vision image case uses the native-agent factory because the catalog has no text-only model.
  Monitor result metadata also has tool-level checks; its stdout-event E2E alone does not prove every metadata field.
- The last find fallback implements a documented gitignore subset, not global ignore files or .ignore files.
  The optional command guard remains a plain-command safety check, not an isolation boundary.
  Crash-time or escaped-process-group cleanup remains outside the fixed contract.

The scoped file scan found no environment credential value or credential-shaped secret.
No new scoped binary, pycache, log or temporary artifact was found.
Unrelated dirty plan artifacts were left unchanged as requested.
The final process check found no review-owned process and no new spill directory.
The existing Ask process predates this review and was left alone.
Task-specific test caches were removed after their runners stopped.

## Final gates

| Gate | Result |
|---|---|
| `go vet ./...` | PASS |
| `go test ./internal/... ./pkg/...` | PASS |
| `go test ./cmd/tui` | PASS; 237.726 s |
| `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` | PASS; zero issues |
| `go test -race ./internal/tools ./internal/jobs ./internal/pipeline ./cmd/tui` | PASS; CLI 330.542 s; internal packages pass |
| `git diff --check` | PASS |

Jobs race tests also passed three consecutive runs.
The revised monitor flood E2E passed three consecutive race runs.
The prior find timeout flake did not recur in 20 focused runs.

## Questions for the user

None.
The three accepted open items above remain separate work.
