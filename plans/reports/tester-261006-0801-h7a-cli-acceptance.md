# H7a CLI acceptance

Work context: `/Users/dale/orca/workspaces/AskCore/master-2`.
The test owner changed only `cmd/tui/auth_command_test.go`, `cmd/tui/auth_oauth_command_test.go`, and `cmd/tui/auth_signal_test.go`.
This report records offline command checks.
It does not claim live provider acceptance.

## Connected command path

The compiled Go test subprocess calls `runWithDependencies` with the user's argv and real stdin, stdout, and stderr.
The production wrapper calls this same function.
The tests use the real command parser, app constructor, native strategies, credential store, identity verifier, auth resolver, agent, tool registry, and provider adapters.
The tests replace only external HTTP and clock/wait boundaries.
The HTTP boundary checks logical production URLs before it sends each request to a local test server.
Private auth, JWKS, and model discovery requests use a different transport from inference.
No test endpoint, issuer, signature bypass, or credential setting is added to production.

ChatGPT tests sign RS256 tokens with a test key and serve the matching JWKS at the logical trusted URL.
The maintained OIDC verifier checks the real issuer, audience, nonce, signature, expiry, and signed subject.
The ChatGPT tests also check that the token request's PKCE verifier matches the authorization challenge.

## Checks added

- Anthropic browser and copy-code login each save credentials through the native command.
- The browser test rejects a callback request target greater than 16 KiB and then accepts a valid callback in the same pending attempt.
- A second process uses the saved Anthropic credential with bearer auth and no API key.
- Each provider's prompt executes the real echo tool, sends its result in the next request, and emits the canonical `tool_execution_end` event in JSON mode.
- Actual Anthropic cassette files contain inference only, with no access token, refresh token, callback code, or token exchange.
- ChatGPT first login uses the dynamic client, and returning login uses the issued client.
- The host identity stays the same across these processes.
- A changed signed subject rejects ordinary returning login and preserves the old file.
- Explicit new-account login accepts the new signed identity.
- Missing inference scopes reject login and preserve the old file.
- Lost persisted subject identity rejects ordinary login before a provider exchange.
- Explicit new-account login recovers from this lost identity through signed verification.
- ChatGPT's next process performs authenticated account model discovery and uses the Responses profile with `store: false`.
- A ChatGPT token response supplies an earliest-refresh gate 56 minutes ahead.
- The next prompt advances its clock by 55 minutes, inside the eight-minute refresh lead but before that gate.
- The saved token serves discovery and the real tool turn without another token request.
- xAI waits two seconds before its first poll and each pending poll.
- A slow-down response increases the next wait from two to seven seconds.
- Both `access_denied` and `authorization_denied` preserve the old file, which still serves the next prompt and tool turn.
- Every provider's logout stops the next process from using the deleted credential.
- Missing noninteractive method and excessive key input preserve the old record.
- Excessive private OAuth input stops before token exchange and preserves the old record.
- A pending login cannot restore a credential after a different process logs out.
- SIGINT, SIGTERM, and SIGHUP stop blocked private input and browser callback waits with codes 130, 143, and 129.
- Repeated browser starts after each signal prove that the listener closes.
- Each signal also drains an active rotating refresh before exit.
- A cancellation notice forms a barrier before the second signal.
- An external response barrier keeps the refresh active until the test releases it.
- The test holds the external response barrier for 2.2 seconds after the acknowledged first signal and the second signal.
- This is longer than the old two-second grace limit.
- The second signal cannot stop the pending rotation.
- The rotated refresh credential is committed before the expected signal exit code.
- A process is killed with actual SIGKILL while its rotating token exchange is blocked at the external server.
- Before SIGKILL, the test reads the real store and checks the durable pending generation fence and attempt ID.
- A second compiled process with the same home rejects the prompt with a sanitized recovery message and sends no auth or inference request.
- A separate test closes the accepted rotating exchange connection without a token response.
- After this lost response, the second process also sends no auth or inference request and requests recovery.
- Both restart cases keep the exact fenced file unchanged.
- Output scans reject credential and callback-code canaries.

The callback-owning parent tests use `testsupport.LockOAuthPorts`.
This prevents separate test packages and worktrees from using the same registered callback ports at the same time.
The subprocess helper does not acquire this lock.
All child processes have test cleanup handlers.
All local HTTP servers close at the end of their test.

## Fault found

The first connected Responses tool check failed for both ChatGPT and xAI.
The next request contained `{}` instead of the streamed echo arguments.
The real echo validator therefore rejected the tool call.
`fantasykit.Fold` changed the public tool ID through `ToolEnd` with a final object that contained only the ID.
The assembler treated that object as the complete final call and set empty arguments to `{}`.
The test owner sent this cause to the controller.
The wire owner fixed the shared fold path and added an adapter regression check.
The strict CLI assertions were kept.
The connected ChatGPT and xAI tool checks then passed.

## Executed checks

- `go test ./cmd/tui -run '^TestAuth' -count=1` passed: `ok AskCore/cmd/tui 3.148s`.
- `go test ./cmd/tui -run 'TestAuthChatGPTCommandVerifiedIdentityReturningAndNewAccount|TestAuthXAICommandPollTimingDenialRetainsCredential' -count=1` passed: `ok AskCore/cmd/tui 1.228s`.
- `go test ./cmd/tui -run 'TestAuthCommandSignalsDrainRotatingRefreshBeforeExit|TestAuthCommandSignalsBlockedPrivateInputAndCallback|TestAuthCommandLoginLogoutFence|TestAuthCommandPrivateInputLimitRetainsCredential' -count=3` passed: `ok AskCore/cmd/tui 5.580s`.
- `go vet ./cmd/tui` passed with no diagnostics.
- With the earliest-refresh gate check added, `go test ./cmd/tui -run 'TestAuthChatGPTCommandVerifiedIdentityReturningAndNewAccount' -count=1` passed: `ok AskCore/cmd/tui 1.230s`.
- The same final ChatGPT test passed with `-race`: `ok AskCore/cmd/tui 8.696s`.
- `go test -race ./cmd/tui -run '^TestAuth' -count=1` passed: `ok AskCore/cmd/tui 39.320s`.
- After the small fence-test URL parsing cleanup, `go test ./cmd/tui -run 'TestAuthCommandLoginLogoutFence' -count=1` passed: `ok AskCore/cmd/tui 0.832s`.

## Final process-death and lost-response gate

- `go test ./cmd/tui -run 'TestAuthCommandRestartDoesNotReuseUncertainRefreshGrant' -count=1` passed: `ok AskCore/cmd/tui 0.937s`.
- The same restart test passed with `-race`: `ok AskCore/cmd/tui 4.733s`.
- With both restart cases added, `go test ./cmd/tui -run '^TestAuth' -count=1` passed: `ok AskCore/cmd/tui 3.877s`.
- `go vet ./cmd/tui` passed again with no diagnostics.
- The complete CLI auth race suite with both restart cases passed: `ok AskCore/cmd/tui 42.499s`.
- The restart test repeated three times passed: `ok AskCore/cmd/tui 0.958s`.
- After increasing the signal barrier to 2.2 seconds, `go test ./cmd/tui -run 'TestAuthCommandSignalsDrainRotatingRefreshBeforeExit' -count=1` passed: `ok AskCore/cmd/tui 9.808s`.
- The final strengthened drain and restart tests passed together with `-race`: `ok AskCore/cmd/tui 14.471s`.

## Limits

These tests make no live requests and use no user credentials.
Live login, eligibility, provider approval, and live inference remain separate acceptance gates.
The CLI suite does not independently cover every native malformed token/JWKS case or every filesystem commit fault.
The native auth and store owners must supply those checks.
The CLI refresh drain test models an external exchange that ignores cancellation and then returns a valid rotation.
It does not intercept file writes or the internal commit path.
It proves that the command waits beyond the former grace limit for an external rotating exchange and then completes the real commit.
It does not place a separate barrier inside the commit write itself.
The store owner supplies the separate commit-budget and filesystem-fault checks.
