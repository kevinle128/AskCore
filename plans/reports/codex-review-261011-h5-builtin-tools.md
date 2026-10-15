# H5 builtin tools review

Date: 2026-10-11.
Status: Review fixes complete; the acceptance and proof limits below remain open.

## Scope and authority

Reviewed the uncommitted implementation against [the H5 plan](../261010-2349-h5-builtin-tools/plan.md), its ten phase files, [the architecture](../architectures/h5-builtin-tools-architecture.md), and [the fixed decision record](../architectures/h5-builtin-tools.md).
Read the available phase reports and [the real-provider acceptance report](real-provider-acceptance-261011-h5-tools.md).
The review did not change the architecture files, user decisions, or commits.
Changes to those files that were already in the working tree were left in place.
No live provider request was needed for this review.

Checked these areas:

- Tool registration and the shared native-agent composition for print mode, ACP, leader sessions, and the in-process TUI.
- File and search parameters, output limits, images, edit matching, atomic writes, generated-file guards, cancellation, and path variants.
- Command parsing, environment filtering, process groups, pipe readers, bounded output drain, spill limits, and cleanup on normal and signal exits.
- Job limits, output waits, completion notices, monitor events, event ordering, flood control, and session release.
- ACP tool kinds and diffs, parallel session close, and the runtime proof matrix and its named tests.

## Defects fixed

The first seven items below were reproduced before their fixes.
The file-tool and command-guard reproductions used the real print-mode agent composition with a scripted provider response.
The monitor reproductions used real child commands and the job table.
These tests do not replace the production tool or process runner.

| Defect | Effect and fix | Code | Regression test |
|---|---|---|---|
| Large read page numbers | A large numeric limit could produce an invalid end index and a negative paging hint. Bounds now apply before integer conversion; large offsets return the normal past-end hint. | [file_read.go:181](../../internal/tools/file_read.go#L181), [file_read.go:210](../../internal/tools/file_read.go#L210) | `TestPrintReadLargePageNumbers` |
| Overlapping edit matches | `aaa` with `oldText=aa` was treated as one match and changed the file. Normalized matches had the same defect. Both searches now count overlapping matches and reject an ambiguous edit without changing the file. | [file_edit_match.go:95](../../internal/tools/file_edit_match.go#L95), [file_edit_match.go:401](../../internal/tools/file_edit_match.go#L401) | `TestPrintEditRejectsOverlappingOccurrences` |
| Named pipes accepted by file tools | `read` could wait on a pipe; `edit` could read it and replace it with a regular file. Both tools now reject non-regular files before reading. `read` also checks the opened file. | [file_read.go:101](../../internal/tools/file_read.go#L101), [file_edit.go:186](../../internal/tools/file_edit.go#L186) | `TestPrintFileToolsRejectNamedPipes` |
| Missing path variants | `grep` and `find` could report a missing path, while `write` could create a second file when an existing screenshot path used a narrow space. These tools now try the shared existing-path variants. A new write still uses the given path when no variant exists. | [search_grep.go:109](../../internal/tools/search_grep.go#L109), [search_find.go:85](../../internal/tools/search_find.go#L85), [file_write.go:59](../../internal/tools/file_write.go#L59) | `TestPrintFileAndSearchToolsTryPathVariants` |
| Wrapper option parsing in the command guard | The shared option list could consume the command after `command -p` or `sudo -n`. The optional guard could then miss a blocked command. Value-taking options are now defined per wrapper. | [command_guard.go:138](../../internal/pipeline/command_guard.go#L138) | `TestPrintCommandGuardChecksCommandWrapperOptions` and the command-guard unit cases |
| Lost final monitor line | A long stdout line with no final newline was dropped after the partial-line buffer reached its cap. Finish now sends the retained final line through the normal event limits. | [monitor.go:234](../../internal/jobs/monitor.go#L234) | `TestMonitorDeliversLongUnterminatedLine` |
| Monitor completion before an event | Completion could pass a debounce delivery that was still in progress. A shared mutex now makes finish wait for that delivery before job completion and the follow-up notice. | [monitor.go:92](../../internal/jobs/monitor.go#L92), [monitor.go:216](../../internal/jobs/monitor.go#L216), [monitor.go:231](../../internal/jobs/monitor.go#L231) | `TestMonitorCompletionWaitsForEventDelivery` |

Added one portable numeric guard in [shell_jobs.go:83](../../internal/tools/shell_jobs.go#L83).
`job_output` now applies the maximum wait before converting a float to `time.Duration`.
An out-of-range conversion can otherwise produce a negative duration on some CPU targets and select the default wait instead of the maximum wait.
`TestJobOutputLargeTimeoutUsesMaximumWait` checks the 600-second cap with a fake clock and a real gated command.
This test passed before the change on the review host, which is Darwin ARM64; this item is a portability fix, not a failure reproduced on that host.

Updated the owning [workspace README](../../internal/workspace/README.md) to describe path variants for all file and search tools.
No dependency was added.
No test was weakened or deleted.

## Security and lifecycle checks

Reviewed the environment filter against the fixed inheritance rule, including credential-shaped names and the non-interactive and `ASK_*` variables.
Reviewed spill-file creation and its 0600 mode, the 0700 private directory, the 64 MB cap, and continued process output drain after that cap.
Reviewed process-group creation, group signals, child wait, pipe closure, bounded drain, and the final print-mode kill sweep.
The process and job tests, including the race and leak checks, passed.
No further concrete defect was found in these paths.

The optional command guard remains a safety check, not an operating-system isolation boundary.
Unrestricted file paths, inherited environment names outside the credential filter, no crash-time orphan sweep, no PTY, and no cgroup remain the fixed product contract.
The review did not add new restrictions to those decisions.

## Runtime proof matrix

The named phase-9 runtime tests exist.
Checked their assertions and call paths rather than treating a test name as proof.
Strengthened [tools_e2e_test.go](../../cmd/tui/tools_e2e_test.go) in four places:

- `TestPrintReadsFileWithLineNumbers` now uses a controlled home directory and exercises `~/calc.go` through the real tool.
- `TestPrintGrepFindsMatches` now checks a real `rg` search with 3,000 matches, a ten-match limit, and the limit message.
- `TestPrintFindWithoutFdAndRgUsesWalk` now checks that the Go walk excludes a file through `.gitignore`.
- `TestPrintMonitorFloodGuardSuppressesEvents` now checks that the recorded process group is gone after print mode exits.

The new print-mode regression tests are in [tools_review_e2e_test.go](../../cmd/tui/tools_review_e2e_test.go) and [tools_guard_e2e_test.go](../../cmd/tui/tools_guard_e2e_test.go).

These proof limits remain:

| Item | Evidence and reason left open |
|---|---|
| Leader session close | `TestLeaderSessionCloseKillsBackgroundJobs` skips because the leader wire has no session-close operation. This is the accepted protocol limit. Other leader and job cleanup tests do not prove that missing operation. |
| TUI notice hiding | N1 belongs to the separate TUI design slice. `protocol.IsSystemReminder` exists, but these H5 tests do not certify that the TUI hides notice blocks and queue rows. |
| Non-vision image model | `TestPrintReadImageForNonVisionModelAddsNote` uses the real native agent, HTTP provider path, and read tool with a supplied text-only model. It does not prove CLI catalog selection of a text-only model. |
| Immediate job output | The print-mode test allows five seconds. The tool-level `TestJobOutputRunningAnswersAtOnceWithNoticeLine` enforces the 50 ms bound. The print-mode row alone does not prove a 50 ms end-to-end bound. |
| Group cleanup in every runtime test | Dedicated cleanup tests record process groups and check `ESRCH`. Some foreground output and wait tests do not make that assertion themselves. The phase wording that every runtime test does so is stronger than the test evidence. |
| Monitor event count | The bucket starts with ten tokens and refills while the flood command runs. A six-second test can deliver more than ten events. The test proves suppression; a literal first-ten-only claim does not describe the refill contract. |
| ChatGPT OAuth acceptance | The existing real-provider report leaves this run unverified. This review did not use or renew credentials. |

The existing trailing-newline read behavior and the documented Go-find ignore-file subset remain implementation-report deviations.
They were not changed in this review.
The Go walk covers nested `.gitignore` files, but does not claim all external `fd` ignore sources.
The pre-existing JSON-mode exit behavior for assistant errors is outside this H5 review.

## Validation

| Check | Final result |
|---|---|
| `go vet ./...` | PASS |
| `go test ./internal/... ./pkg/...` | PASS |
| `go test ./cmd/tui` | PASS, 236.495 seconds |
| `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./...` | PASS, zero issues |
| `go test -race ./internal/tools ./internal/jobs ./internal/sandbox ./internal/workspace ./internal/pipeline ./internal/acp ./internal/agent -count=1` | PASS |
| New print-mode regression tests, race detector, three runs | PASS |
| Monitor and sandbox focused race tests, three runs | PASS |
| Strengthened runtime proof tests and command-guard tests | PASS |
| `git diff --check` | PASS |

Initial sandboxed runs failed because process inspection and Go cache access were blocked.
The first full TUI run also stalled when process-group readiness could not be recorded through `ps`.
Stopped that review-owned runner, then ran the full suite with the required access.
The final full runs above passed.
No review-owned background runner remains.

One intermediate tools run failed `TestFindTimeoutReturnsPartialResult` because the result lacked the expected partial file.
Ten focused repeat runs and the final full internal suite passed.
The failure remains a possible timing flake; no test change hides it.
The previously reported `TestACPE2ERequestCancel` flake was not seen in the final full TUI run.

## Open items and questions

- The separate TUI slice must satisfy N1 before the release can claim hidden notices on all TUI surfaces.
- Leader session-close proof remains unavailable until the protocol supports that operation.
- ChatGPT OAuth real-provider acceptance remains unverified.
- The intermittent find timeout result needs a reproducible trace if it recurs.
- Matrix timing and per-test cleanup claims should be read with the proof limits above.

No new product decision is required by the fixes in this review.
