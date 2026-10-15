# H7a final CLI refresh proof

Work context: `/Users/dale/orca/workspaces/AskCore/master-2`.
The test owner changed only `cmd/tui/auth_signal_test.go` and `cmd/tui/auth_oauth_command_test.go` for these final proof tasks.
The existing command helper and external HTTP relay are reused.
The tests use isolated homes and synthetic credentials.
They do not use the live account or its credential store.

## Replacement write failure and restart

`TestAuthCommandRestartDoesNotReuseUncertainRefreshGrant/replacement-write-failed` starts at native Anthropic copy-code login through the compiled command subprocess.
It uses the same `runWithDependencies` entry as production, with the real parser, app constructor, native strategy, store, resolver, agent, and adapters.
The next command advances only its external clock by 55 minutes to require refresh.
A server barrier stops the rotating exchange after the real store has written its pending fence.
The test reads `auth.json` and checks the pending status, attempt ID, and matching generation.

The parent test changes only its owned home directory to mode 0500.
A real `os.CreateTemp` probe confirms that the directory rejects replacement writes.
The external server then returns a valid rotated access token, refresh token, expiry, and scopes.
The production store's replacement commit uses real OS file operations and cannot create its temporary file.
The command returns an error with sanitized recovery guidance.
The test restores directory mode 0700 before it starts the next command.

A second compiled prompt process uses the same home.
It returns recovery guidance without an auth or inference request.
The total external request count remains exactly two: initial login and the one rotating exchange.
The persisted file remains byte-for-byte equal to the pending fence that existed before the replacement response.
No internal store operation, write function, resolver, or lifecycle is replaced.

The same parent test still covers actual SIGKILL during the rotating exchange and a lost response through a closed external connection.
Both cases also check the second command's zero grant reuse and unchanged pending fence.

## Original internal commit barrier gap

This section records the gap before the user requested the external filesystem fixture.
The final closure below supersedes its pending status.

The literal phase-2 and phase-6 requirement remains more specific than the current actor tests.
It requires a barrier inside the validated replacement commit, held beyond two seconds, while the command receives the first and second signals.
The existing 2.2-second CLI barrier is inside the external token exchange.
It proves that registered work outlives the old grace period and that the real replacement commit completes before the signal exit.
It does not prove that the signal arrives while a local file write or sync is blocked.

The current CLI dependencies expose external HTTP and clock/wait only.
The real `AuthStore.Refresh` holds the same sidecar lock before the fence, through exchange, and through replacement commit.
Another process cannot acquire that lock between response validation and replacement commit.
Ordinary advisory file locks do not block regular-file writes or sync calls.
Directory permissions and file flags cause immediate failures, which can prove safe uncertainty but cannot create a timed commit barrier.
Stopping the whole process with SIGSTOP also stops signal handling, so it cannot prove registered auth drain during commit.
Watching temporary files and racing a signal against them cannot give a deterministic barrier.

The nearest store test, `TestAuthStoreRefreshCommitHasIndependentBound`, delays the second file sync through the existing private `fileOps` seam.
It proves that validated replacement can survive caller cancellation and that a sync delay beyond the five-second local budget retains the fence.
That check uses the real store transaction but is not a compiled CLI actor test.
Source inspection shows that `Service.Resolve` registers the operation before `Store.Refresh` and unregisters it only after that call returns.
The CLI waits for this registered work through `app.AuthWait` before its signal exit.
These checks support the design, but they do not erase the literal actor proof gap.

## Correct way to close the gap

Use an isolated, externally controlled filesystem that can stop and release the actual replacement-file sync on an owned test mount.
The mount must keep normal file mode, lock, rename, and sync behavior.
The test can identify the second credential transaction after the external exchange completes and the replacement bytes are written.
It must stop that sync, send each signal and a second ordinary signal, wait more than two seconds, and confirm that the command remains alive.
Releasing the filesystem barrier before the five-second commit budget must let the real command complete its commit and return the expected signal code.
A separate release after budget expiry must leave the fence and make the next prompt refuse grant reuse.
This uses a real external I/O barrier and needs no production endpoint or internal callback hook.

This workspace has no configured filesystem fault fixture with that capability.
Adding one would require a controlled mount facility, such as an approved FUSE test environment, and separate setup and cleanup work.
No production hook, debugger-based suspension, timing race, or internal replacement was added as a substitute.
The exact internal-commit actor proof must remain an explicit pending acceptance item until that facility is available or the accepted proof requirement is changed by its owner.

## Executed checks

- `go test ./cmd/tui -run 'TestAuthCommandRestartDoesNotReuseUncertainRefreshGrant/replacement-write-failed' -count=1` passed: `ok AskCore/cmd/tui 0.906s`.
- `go vet ./cmd/tui` passed with no diagnostics.
- The three-case restart matrix passed with `-race`: `ok AskCore/cmd/tui 6.203s`.
- The three-case restart matrix repeated three times passed: `ok AskCore/cmd/tui 1.197s`.
- With an explicit OS permission-error assertion added, the replacement-write-failed case passed: `ok AskCore/cmd/tui 0.889s`.
- No fixed-port callback tests were run while the controller used the live ChatGPT callback.

## Account model list and explicit CLI selection

The controller added the static `gpt-5.6-sol` catalog row after live account discovery listed it and hid `gpt-5.5`.
The CLI test uses the current catalog and the real parser; it adds no model or endpoint bypass.
The default remains `gpt-5.5` and its earlier listed-account prompt still passes.

The connected signed ChatGPT test then changes its external discovery response to `gpt-5.5` with visibility `hide` and `gpt-5.6-sol` with visibility `list`.
A new process receives explicit `--model gpt-5.5` and returns model-access denial.
The inference count remains unchanged, so no alternate model or billed fallback reaches HTTP.
Another process receives explicit `--model gpt-5.6-sol` and reaches the logical production Responses endpoint.
The server checks the selected model, bearer credential, and `store: false` on the actual request.
The response triggers the real canonical echo tool.
The second request contains its real result, and JSON output contains the successful canonical tool event and final reply.
The original signed OIDC login, stable identity, returning client, rejection, recovery, earliest-refresh, and logout checks remain in the same command path.

- `go test ./cmd/tui -run 'TestAuthChatGPTCommandVerifiedIdentityReturningAndNewAccount' -count=1` passed: `ok AskCore/cmd/tui 1.146s`.
- The same final connected model-selection test passed with `-race`: `ok AskCore/cmd/tui 9.638s`.
- `go vet ./cmd/tui` passed with no diagnostics.

## Related evidence

The [earlier CLI acceptance report](./tester-261006-0801-h7a-cli-acceptance.md) records login-to-prompt/tool, identity, discovery, polling, capture, logout, signal, forced-death, and lost-response checks.
This report adds the real replacement filesystem fault and gives the exact internal-commit coverage disposition.
No plan status or production file was changed.

## Final exact actor closure

The user retained the literal proof requirement and requested a filesystem fault environment.
The [external FUSE actor](./tester-261006-0855-h7a-fsync-actor.md) now supplies that environment without a production hook.
The final Linux `-race -count=2` run passed all five cases twice in 38.186 seconds.
SIGINT, SIGTERM, and SIGHUP each arrived during blocked actual replacement file sync.
A second ordinary signal did not shorten commit drain.
An independent observer verified the complete durable replacement after real directory sync and before CLI exit.
Budget expiry and actual SIGKILL retained the durable pending fence, and the next process sent zero additional HTTP requests.
The exact internal-commit acceptance gap is closed.
