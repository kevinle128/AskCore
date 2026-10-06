# H7a native authentication flows

Status: DONE_WITH_CONCERNS

The native authentication strategies and shared login/logout operations are implemented.
No live account or credential was used.

## Files

New files are `internal/auth/login.go`, `http.go`, `callback.go`, `anthropic.go`, `chatgpt.go`, `xai.go`, `discovery.go`, `login_test.go`, and `native_test.go`.
The controller approved changes to `identity.go` and `identity_test.go` for refreshed ID-token verification and bounded JWKS retrieval.
The shared worker owns `Method.Login` and the existing resolver changes.

## Result

`NativeMethods(NativeOptions)` returns the three OAuth methods with fixed provider destinations and profiles.
The composition root must add API-key methods.
`Service.Login` reads the revision before user interaction and commits one credential with the captured revision after validation.
`Service.Logout` deletes the local record with a revision check.
Login is registered with the shared shutdown drain.
Private input and callback request targets are limited to 16 KiB.

Anthropic uses PKCE and the native browser or hosted copy-code redirect.
Browser login requires its fixed port and never changes the port automatically.
Wrong-state or oversized callbacks leave the attempt active.
A valid state-bound provider denial ends the attempt.
The callback page reports receipt only.
Callback server shutdown closes spare connections and joins the serve goroutine.
Unused manual input is canceled and joined.
Browser EOF closes only the manual path.
The supplied input function must honor cancellation.

ChatGPT uses separate state and nonce, the stable supplied host UUID, and native dynamic registration.
Returning login reuses the saved issued client and requires the same verified subject.
`NewAccount` starts dynamic registration and replaces the old account only after successful verification and commit.
The real OIDC verifier checks signature, issuer, audience, expiry, nonce, subject, and authorized party.
A returned refresh ID token uses the same signature and identity checks without requiring the original login nonce.
Refresh requires a replacement refresh token and inference scopes.
Granted ChatGPT scopes include `resource.invoke` and `chatgpt.tokens.use.direct`.

Authenticated ChatGPT discovery uses fixed `GET https://api.openai.com/v1/models` and the documented `models[].slug` and `visibility` fields.
Visible slugs stay in server order and only compiled model metadata controls inference capabilities.
The private access cache is bound to method, client, subject, issuer, and credential generation.
Unknown discovery permits valid compiled inference; a known denial blocks it.
The controller added a post-discovery store check to reject a credential replacement during discovery.

xAI waits before the first poll and after each pending result.
Slow-down never reduces the current interval and adds five seconds when no valid server interval is supplied.
Both native denial names are terminal.
Each poll is bounded by device lifetime and the shared HTTP timeout.
Only xAI retains a refresh token when the provider omits that field.
An explicit null or empty replacement is rejected.
No verified account identity is invented for opaque xAI tokens.

HTTP exchanges use a cloned private client with a 15-second limit, no redirects, and a 1 MiB response limit.
JWKS retrieval has a five-second limit, no redirects, and a 1 MiB response limit.
There are no endpoint, TLS, issuer, or signature bypass options.
Actual token expiry is retained; refresh lead is ten minutes for Anthropic/xAI and eight minutes for ChatGPT.

## Verification

The first login and callback tests were written before implementation and failed because the new APIs were absent.
The native strategy tests then checked logical outgoing destinations through an external HTTP RoundTripper.
Store operations, loopback callbacks, PKCE, and OIDC verification remain real.

- `go test -race ./internal/auth`: passed.
- `go vet ./internal/auth`: passed.
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./internal/auth`: passed with zero issues.

Tests cover key save and revision conflict, private input bounds, copy-code exchange, repeated browser callback cleanup, oversize callback followed by valid callback, browser EOF, state-bound denial, signed ChatGPT registration/reuse/replacement, invalid nonce preservation, discovery cache generation and denial, xAI first wait/pending/slow-down/denial/expiry/hung poll/cancellation, method-specific refresh retention, malformed tokens, no redirects, and response limits.
Existing identity tests independently cover signature, issuer, audience, expiry, nonce, subject, authorized party, and algorithm failures.

## Concerns

The controller owns compiled command integration tests and next-prompt inference proof.
Live provider acceptance remains pending and is not represented as an offline pass.
Official discovery and identity references are https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference and https://developers.openai.com/siwc/website.

## Refresh timing correction

The advisor found that native token decoding omitted `earliest_refresh_at` and cleared the saved refresh gate.
A failing signed ChatGPT login test reproduced the lost gate before this correction.
Failing native refresh tests also showed that null, negative, malformed, boolean, and out-of-range gate values were accepted silently.

Token decoding now preserves the optional field and validates it before a login or refresh replacement is committed.
The parser accepts finite nonnegative Unix seconds and strict RFC3339 timestamp strings.
It rejects explicit null and invalid values without guessing a millisecond conversion.
An absent gate remains an absent gate.
Known past gates are permitted.
The actual access-token expiry remains independent of the earliest refresh time.

The signed native `Service.Login` → `Service.Resolve` test verifies login persistence, zero refresh exchanges before the gate, one exchange when the gate opens, persistence of the new refresh gate, and no refresh while an expired access token remains behind a future gate.
An invalid login gate leaves the previous credential and revision unchanged.
Refresh field tests cover both supported forms, fractional Unix seconds, omitted and past gates, malformed values, and invalid RFC3339 timezone/fraction syntax.

Primary OpenAI documentation at https://developers.openai.com/siwc/token-sharing-open-source/token-reference lists the field but does not specify its type or unit.
The pinned Pi OAuth implementation does not parse the field.
The approved numeric-second and timestamp-string interpretation comes from the inspected client implementation at `/Users/dale/Desktop/workspace/opensources/t3code/apps/server/src/provider/CodexChatGptAuth.ts:428`.
That implementation multiplies numeric values by 1000 and parses strings as dates.
This is client implementation evidence, not a claim that OpenAI has published the complete value schema.
Production live acceptance remains required.

The omitted xAI `expires_in` fallback of 3600 seconds is explicit in the pinned Pi `xai.ts:136`.
Explicit null still fails validation.
No expiry default was added to ChatGPT or Anthropic.

The three native browser tests now use the controller's shared `testsupport.LockOAuthPorts` helper.
The helper serializes tests across packages and worktrees without changing product callback ports.

After the timing correction, `go test -race ./internal/auth`, `go vet ./internal/auth`, and narrow auth lint passed with zero lint issues.
The added invalid-login-gate test was then checked with the focused signed-login and refresh-field race tests.
