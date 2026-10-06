# Shared auth hardening

Status: DONE.

## Changes

The auth service validates method, account, client, issuer, access and refresh tokens, and actual expiry inside the store exchange callback.
An invalid response cannot activate a replacement or clear the pending fence.
The clock is read after the exchange.
Expired credentials refresh before account access discovery.
A successful refresh access check is not repeated.

The store uses a separate five-second context for the replacement commit.
This context retains request values and does not inherit request cancellation.
The existing exchange limit remains fifteen seconds.
Local writes check cancellation before each I/O step and after directory sync.
A blocked OS sync call cannot be interrupted by context cancellation.
A timeout before rename keeps the fence.
A timeout after rename returns an indeterminate result.

Store writes keep unknown fields in the selected provider record and its nested OAuth object.
Owned optional fields are removed when absent from the new record.
Legacy credential fields are removed when the record is replaced.

The app validates typed overrides against the service method registry, including provider, method, API, profile and endpoint.
A partially filled typed override is checked even if its access token is empty.
Competing typed and legacy overrides still fail before dispatch.

## Integration

Call `Service.StopRefresh()` on the first shutdown signal before canceling inference.
Call `app.AuthWait(service)` with an independent context after that cancellation.
`AuthWait` calls `Service.DrainRefresh(ctx)` with a twenty-second maximum wait.
`DrainRefresh` closes the new-work gate and waits for registered work.
`WaitRefresh` remains an observational wait and does not stop new work.
New refresh work after the gate closes returns `auth.ErrShuttingDown`.

`Method.Login` now has type `func(context.Context, LoginRequest, settings.Credential) (settings.Credential, error)`.
The native login worker owns `LoginRequest` and the native implementation files.
The service reuses the native worker's `s.now()` helper.

## Evidence

The first regression run failed because invalid subject, client and expiry responses committed generation two and cleared the fence.
It also showed that invalid issuer and empty access or refresh tokens were accepted.
Unsupported typed method, profile, API and endpoint overrides reached the registry.
The first commit-budget test failed because a replacement sync lasting 5.1 seconds still reported success.
The first unknown-field test failed during the fence write.
The first access-order test failed because discovery received an expired token before refresh.

Passed before native files were added: `go test ./internal/auth ./internal/settings ./internal/app -count=1`.
Passed: `go test -race ./internal/settings ./internal/app` before the app native constructor was added.
Passed after the latest service changes: `go test -race internal/auth/service.go internal/auth/login.go internal/auth/service_test.go internal/auth/refresh_test.go`.
The named-file command isolates this worker's shared tests while native production files are incomplete.

Passed after native production files were added: `go test -race ./internal/settings ./internal/auth ./internal/app`.

A new real `Agent.Prompt` test checks invalid rotation, one assistant error settlement, no inference dispatch, no billed fallback, and no grant reuse after reopening the real store.
This test passed in the full package race run.

## Remaining checks

Narrow lint found two settings issues and three native-file issues.
The settings issues were fixed.
The native worker received the three remaining findings for files it owns.
Rerun the lint gate after those fixes.
The CLI signal and second-process prompt checks belong to the root integration task.

## Controller integration correction

Connected model-access testing found that access discovery was still inside the refresh exchange callback.
A model denial could discard a validated rotating grant and leave an unnecessary pending fence.
The controller moved discovery after the durable replacement commit.
`TestModelDenialDoesNotDiscardValidatedRotation` proves that generation two remains saved while the request reports an access denial.
The resolver now checks the fresh saved method, generation, client and subject after discovery to reject a stale result after replacement or logout.
Discovery therefore uses the committed generation and does not run under the rotating transaction lock.
The earlier exchange-callback access-check description is superseded by this correction.
