# Phase 08 support implementation

Status: DONE

## Changes

Provider registrations own a value retry policy with five retries, a 500 ms base delay, and a 10 s maximum delay.
`Registry.Prepare` captures the serving registration's policy on every request, including registrations without a `Preparer`.
A plain stream registration sets `Prepared.PolicyOnly` so the adapter can compute its own wire values.
Preparation clones adapter values before it adds the policy.
The optional policy argument preserves existing registration callers.

`AuthRunner` checks `RequireBinding` after credential resolution and before the adapter starts.
The check compares Method, Profile, and BillingHint.
A changed binding returns `auth.ErrBindingChanged` through an `AUTH` provider failure.
A token refresh and a provider route change with the same billing binding are allowed.
The binding projection contains no access token or account ID.

Session retry records add no model message.
The prepared session record stores the captured policy in milliseconds and the policy-only marker.
All retry fields are values, so existing session clones preserve isolation.
Faux request records now clone prepared values and the required binding.

The protocol supports typed `auto_retry_start` and `auto_retry_end` events through encoding, decoding, the event union, and the message builder.
Every encoded `agent_end` carries `willRetry`, with false as the default.
The successful retry end omits `finalError`.

Interrupted messages replay nonblank text and thinking without tool calls.
Unsigned interrupted thinking replays as plain text.
Signed same-model thinking stays thinking.
Error messages and empty interrupted messages stay out of replay.
Existing OpenAI replay tests now assert this intentional contract change.

Headless options pass the retry wait function into the agent configuration.
HTTP 429, HTTP 500, and connection reset tests assert six requests and production delays of 0.5, 1, 2, 4, and 8 seconds.
The JSON failure-code test also checks five 5-second waits when `Retry-After` is 5 seconds.
The clean premature stream-close test asserts one request.
HTTP 429 followed by success returns exit code 0 with the final text.
Fault transports perform no paid live requests.
Provider, session, and protocol owner READMEs describe the contracts.

## TDD evidence

The first focused policy and protocol test run failed to compile because the retry policy, registration override, retry event types, and WillRetry field were absent.
After implementation, the same focused tests passed.
The interrupted replay tests first failed because nonblank interrupted messages were dropped.
The auth pin tests first failed because changed billing bindings reached the adapter.
Both test groups passed after the shared transform and auth resolver checks were added.
The faux ownership test first failed because a caller could mutate the stored prepared policy.
It passed after the existing option clone copied Prepared and RequireBinding.

## Validation

`go test ./internal/providers/... ./internal/sessions ./internal/app ./internal/auth ./pkg/protocol` passed.
`go test ./cmd/tui -run 'Fault|JSON|TokenPlanMissingKey'` passed.
`go vet ./internal/providers/... ./internal/sessions ./internal/app ./internal/auth ./pkg/protocol ./cmd/tui` passed.
The race run passed providers and all adapter packages, sessions, app, auth, and protocol.
The full CLI race run exposed the old one-request failure-code test assumption; its assertions and wait seam were then updated.
`go test -race ./cmd/tui -run 'HeadlessFailureCode|Fault|JSON|TokenPlanMissingKey'` passed after that change.
The support reviewer reported no functional concerns and passed narrow lint after the import-group fixes.
The controller owns the final integrated suite and conformance matrix.
No background processes remain from this support task.

## Open questions

None.

## Final conformance additions

`TestRefusedConnectionRecoversAfterBackoff` covers matrix SL13 with the real headless setup and Anthropic HTTP adapter.
The operating system refuses the first dial to a closed loopback port with ECONNREFUSED.
The recording wait opens a real HTTP server on that port and checks the production 500 ms delay.
The second request succeeds with the same prepared body, one input cycle, one retry start and end, stdout `pong`, and exit code 0.
An isolated temporary source copy with TRANSPORT retries disabled failed this exact test with exit code 1 and connection-refused text.
The temporary copy was removed after the check.
No paid live request was made.

`TestRetryNeverSwitchesSubscriptionToApiKey` runs the Agent through AuthRunner and replaces a saved subscription credential with an API key during backoff.
It checks that the second adapter request never starts and the final assistant reports the changed-binding error.
`TestRetryFailsWhenBillingClassChangedDuringBackoff` changes only BillingHint during backoff and checks the final AUTH code.
`TestRetryRebindsSameMethodAfterTokenRefresh` now covers a real Agent retry and auth-store token refresh during the wait.
It checks the unchanged safe binding, old and new tokens, one refresh, one production delay, and successful completion.
`TestBindingProjectionHoldsNoToken` is the exact name of the existing safe-binding assertion.
`TestInterruptedUnsignedThinkingIsReplayedAsText` directly checks the replayed block and unchanged history.
An unknown registry API failure now names both the API and provider, as matrix SL9 requires.

`TestJSONPrepareFailureHasNoUserMessageBeforeError` uses the real headless JSON runner with a failing Prepare function and real adapter registration.
It checks exit code 1, the D20 assistant error wrapper, no user event, no committed input or error message, no HTTP request, no attempt event, and no request record.

Focused race checks passed for these additions in app, providers, and cmd/tui.
The final JSON preparation-failure and refused-connection race check passed together.
The final narrow lint run for app, providers, and cmd/tui passed with 0 issues after disposal error checks and a tagged-switch cleanup.

## SL13 durable assertion correction

The review found that the refused-connection test checked the wire and cycle but not the durable retry and turn records.
The same named test now runs two concrete paths against a real closed loopback port that opens in the 500 ms wait.
The first path preserves the headless constructor check.
The second uses the existing Agent constructor with NewContext, MemoryLog, the real Anthropic registry, and the app-composed auth runner.
Both paths use the real headless print runner and retain the connection-refusal, request-byte, retry-wire, stdout, and exit-code checks.
The second path checks exactly one RetryScheduled with TRANSPORT, retry 1, delay 500 ms, and turn 1.
It checks exactly one TurnOpened in the same cycle, one matching RetryStarted, and failed then completed AttemptSettled records.
No production log API or test-only source seam was added.
The focused race run passed in 2.112 seconds.
