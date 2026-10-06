---
status: passed
---

# Exact CLI local commit actor

## Summary

The actor is `TestAuthCommandSignalsDuringLocalDurableCommit` in `cmd/tui/auth_fsync_test.go`.
It requires the explicit `fsfault` build tag and an external Linux FUSE fixture.
The corrected actor passed all five cases twice in a clean Linux run with race instrumentation.
The first Linux run found a fixture-observer error, which was corrected.
A later resource failure was resolved with owned cache cleanup and a cold rebuild.
The final ten-case run passed without race or cleanup errors.
The controller owns Linux runner setup and execution.
No production code was changed by this test owner.

## Actor and proof boundaries

Each case starts with a compiled native Anthropic copy-code login command in an isolated credential home.
The existing command helper calls the real `runWithDependencies` parser, dispatch, app composition, auth service, strategy, resolver, store, agent, and provider adapter.
Only external HTTP and the clock use the existing command dependencies.
All credentials are synthetic.
The actor does not use a store mock, production callback, alternate endpoint, signature bypass, or live credential home.

After login, the next prompt advances its external clock by 55 minutes.
The fixture allows the first regular-file sync to publish the pending fence.
The external token endpoint returns a valid rotated token response without a response barrier.
The fixture blocks the second actual regular-file sync.
At `WAIT -> BLOCKED`, a separate compiled observer reads the pending `auth.json` and replacement temporary file from the owned backing filesystem.
It checks the pending attempt ID and generation, the new generation, rotated tokens, and absent replacement fence.
Thus the barrier is after token validation and replacement-file write, inside actual local file sync.

For SIGINT, SIGTERM, and SIGHUP, the actor sends the first signal and a second ordinary SIGTERM.
It checks that the command remains alive beyond the old two-second grace.
It releases file sync before the local five-second budget.
The fixture then performs real backing directory sync and holds that syscall's reply.
At `WAIT_SYNC -> SYNCED`, a separate compiled observer reads the new `auth.json` from that backing filesystem after its real directory sync.
The entire saved credential must match the replacement temporary file, including expiry and scopes.
The command must still be alive when that observation finishes.
`RELEASE_EXIT` permits the syscall to return and the command to exit with 130, 143, or 129.

The budget-expired case holds file sync beyond five seconds, then releases it.
The command must retain the exact pending fence and preserve signal exit code 130.
The forced-death case sends actual SIGKILL while replacement sync is blocked.
Both cases start a second compiled prompt command.
That command must request explicit sign-in recovery, send zero extra auth or inference requests, and leave the pending fence unchanged.
The external request count must remain exactly two: initial login and one refresh.
All signal cases check that stdout and diagnostics do not contain synthetic access or refresh tokens.

## Fixture contract and cleanup

`ASK_FSYNC_GATE_BINARY` selects the fixture executable.
The actor starts it with owned backing directory, mount path, and Unix control socket arguments.
`ASK_TEST_FSYNC_HOME`, `ASK_TEST_FSYNC_BACKING`, and `ASK_TEST_FSYNC_CONTROL` can instead select an external fixture.
All three values are required together; the actor creates a separate home below that mount for each case.
The actor checks the Linux FUSE filesystem type before use.
The owned control socket uses a short temporary path below `/tmp`.
The actor logs the fixture command, PID, mount, and socket.

One newline command is sent on each control connection.
The commands are `ARM`, `WAIT`, `RELEASE`, `WAIT_SYNC`, `RELEASE_EXIT`, and `STATUS`.
The actor stops its CLI children, releases both fixture gates, unmounts its owned mount, stops its fixture process, and removes owned temporary data.
It does not stop an externally owned mount or fixture process.
Missing Linux support, fixture configuration, mount permission, or a FUSE mount causes explicit test failure.
The dedicated target does not silently skip or pass when the fixture is absent.

## Executed checks

- `go test -tags fsfault ./cmd/tui -run '^TestAuthSyncReadSubprocessHelper$' -count=1` passed on macOS: `ok AskCore/cmd/tui 0.838s`.
- `GOOS=linux CGO_ENABLED=0 go test -tags fsfault -c -o /tmp/askcore-fsfault-linux.test ./cmd/tui` passed.
- `go vet -tags fsfault ./cmd/tui` passed with no diagnostics.
- `go test ./cmd/tui -run 'TestAuthCommandSignalsDrainRotatingRefreshBeforeExit|TestAuthCommandRestartDoesNotReuseUncertainRefreshGrant' -count=1` passed: `ok AskCore/cmd/tui 7.981s`.

These checks prove compilation and preserve the existing connected command checks.
They do not prove the new FUSE actor matrix.

## First Linux run and correction

The controller started the exact actor with race instrumentation and count two in the Linux container.
The initial interrupt and terminate cases failed with `first signal ended blocked commit: exit status 1`.
The run had not supplied useful signal-during-commit evidence at that point.
The controller observed the separate replacement-file reader in Linux uninterruptible I/O state while the CLI file sync was held.

The observer had read the temporary file through its mounted FUSE inode.
Linux serialized that inode read behind the active FUSE sync request.
The observer waited until the fixture's 30-second safety timeout released the sync.
The CLI's five-second commit budget had then expired before the actor could send its first signal.
This was a test-observer failure, not evidence of a production signal or drain defect.

The corrected actor reads the same files through the owned backing directory.
The CLI continues to use the actual mounted filesystem for writes, file sync, rename, directory sync, and locks.
The observer uses only raw external file reads and does not replace any product operation.
The final before-exit read still follows successful real backing directory sync.
The corrected source passes tagged vet.

## Corrected Linux run and repeat limit

The controller reported a complete five-case pass in 19.50 seconds with race instrumentation.
All three ordinary signals, budget expiry, and actual SIGKILL passed the actor's assertions.
This includes the new durable bytes from a separate process while the command is alive, the required signal codes, and both unchanged-fence restarts with zero extra HTTP.

In the second iteration, interrupt and terminate passed in 3.54 and 3.55 seconds.
Hangup then reached the fixture `WAIT` timeout.
Later native login failed at file sync with an I/O error.
The controller observed only 123 MiB of free host disk space and a 6.1 GiB owned VM disk.
A concurrent lint run also failed with `no space`.
Resource exhaustion was the leading explanation for that failed repeat.
That resource-limited `-count=2` invocation failed.

Source inspection shows that actor fixture cleanup releases both gates, unmounts its owned mount, stops and waits for the fixture process, then removes its temporary root.
CLI child cleanup runs first through the testing cleanup order.
The first complete pass reported no cleanup error.
The actor writes only small credential, lock, and temporary replacement files under its owned root.
The later native login sync error occurred before the actor's `ARM` command.
These facts do not support an actor gate or retained credential file as the cause of the large VM disk allocation.
A fresh repeat after space recovery was required.

## Final clean Linux execution

The controller recovered space and removed its owned build cache volume after a follow-up cached link failed with cache corruption.
A cold rebuild then ran `go test -tags fsfault -race -count=2 ./cmd/tui -run '^TestAuthCommandSignalsDuringLocalDurableCommit$'` in the real Linux FUSE container.
The final runner session was `5367` and returned exit code zero.
Both complete five-case matrices passed, in 18.55 and 18.60 seconds.
The package result was `ok AskCore/cmd/tui 38.186s`.
There were no race diagnostics.

| Case | First replacement sync hold | Second replacement sync hold | Result |
| --- | --- | --- | --- |
| SIGINT | 2.373426139s | 2.367633165s | Exit 130 after durable replacement observation. |
| SIGTERM | 2.362359157s | 2.375341234s | Exit 143 after durable replacement observation. |
| SIGHUP | 2.370548414s | 2.368222836s | Exit 129 after durable replacement observation. |
| Commit budget expiry | 5.204305263s | 5.204844927s | Exit 130; exact pending fence retained; next command sends zero HTTP. |
| Forced death | 60.609487ms | 59.033614ms | Actual SIGKILL during replacement sync; exact pending fence retained; next command sends zero HTTP. |

Every ordinary-signal case delivered its second SIGTERM while the real replacement file sync was held.
Every successful commit case passed `WAIT_SYNC`, read the complete new credential from a separate compiled process after real directory sync, and confirmed that the CLI had not exited.
Only then did it release the directory-sync reply and check the required signal exit code.
The budget and forced-death cases each passed the separate next-command check for sanitized recovery, unchanged fence bytes, and exactly two total external requests.
All ten cases passed the credential-leak checks.
Fixture cleanup reported no error.
The controller confirmed automatic container removal and successful VM stop through the outer cleanup trap.
The final repeat resolves the earlier resource-limited repeat failure; the failed invocation remains recorded above.

## Reproduction

Run the fixture-owned `internal/testsupport/fsfault/run-tests.sh` in the configured Linux container environment.
Its selected actor is `^TestAuthCommandSignalsDuringLocalDurableCommit$` with build tag `fsfault`.
The completed Linux repeat supplies the exact local-commit actor evidence required by the accepted phase contract.
The controller owns final review and plan status changes.

## Unresolved questions

None.
