---
phase: 5
title: "Settings and auth"
status: pending
priority: P3
effort: 2h
dependencies: []
---

# Phase 5: Settings and auth

## Goal

Fix N3/N4 (a `baseUrl` with a trailing `/`), N21, N22 and N23.

## File ownership

- Modify: `internal/settings/models_file.go`, `internal/settings/auth.go`, `internal/settings/host-id.go`
- Modify: `internal/auth/broker.go`
- Tests: `internal/settings/models_file_test.go`, `internal/settings/auth_test.go`, `internal/settings/host-id_test.go`, `internal/auth/broker_test.go`

## Steps

### 1. N3 and N4: trim a trailing `/` of `baseUrl` once, where the value enters

Evidence: the Responses guard expects `u.Path+"/responses"` (`internal/providers/openai/profiles.go:58`) and the Completions guard `u.Path+"/chat/completions"` (`:154`). With `https://x/v1/` the expected path has `//`, and the request fails with `ErrAuthentication`. `docs/cli.md:550` allows any http(s) URL with a host.

Where to trim: the accepted fix trims once where the row is built. The base URL also becomes the endpoint of a models.json key (`internal/app/session_models.go:427`), which `configSnapshot` compares with the row (`internal/auth/service.go:407`) and `AuthSnapshot.ValidFor` checks (`internal/providers/auth.go:39-41`). A trim in the provider rows only would make these disagree. The one point that feeds all of them is the decoder (`internal/settings/models_file.go:171-176`), so the trim goes there. The Anthropic guard trims on its own (`internal/providers/anthropic/profile.go:97`); it stays.

Test first in `models_file_test.go`: `"baseUrl": "http://127.0.0.1:11434/v1/"` decodes to `http://127.0.0.1:11434/v1`; `"https://h/"` decodes to `https://h`; a URL with no trailing slash is unchanged.

Fix: after `validBaseURL` passes, `p.BaseURL = strings.TrimRight(p.BaseURL, "/")`.

Command: `go test -run 'BaseURL|ModelsFile' ./internal/settings`

### 2. N21: `Logout` refuses an unknown method

Evidence: `Broker.Logout` checks only the provider (`internal/auth/broker.go:622-629`); `Service.Logout` checks the method only when a credential is saved (`internal/auth/login.go:174-176`). The broker then sends a removal notice (`broker.go:668`) for a method that does not exist.

Test first in `broker_test.go`: `Logout(ctx, provider, "no-such-method")` with nothing saved returns `ErrMethod` and sends no change notice; `Logout(ctx, provider, "")` and a known method keep today's result.

Fix: refuse a non-empty `method` that matches no `LoginOptions()` entry with the same `Provider` and `Method` (`internal/auth/status.go:19`), in the same loop that checks the provider.

Command: `go test -run 'Logout' ./internal/auth`

### 3. N22: `CommitRefresh` takes only an OAuth replacement of the same method

Evidence: `CommitRefresh` runs only the generic decode (`internal/settings/auth.go:529-554`). No production code calls it today (the real path is `Refresh`, `:557`), but it is exported.

Test first in `auth_test.go`: start a refresh fence on an OAuth credential; `CommitRefresh` with an `api-key` replacement, or with the same method and `OAuth == nil`, returns an error and leaves the stored credential and its fence unchanged; a valid OAuth replacement commits.

Fix: inside the transaction, after the fence check, return an error when `replacement.Method != c.Method || replacement.OAuth == nil`.

Command: `go test -run 'CommitRefresh' ./internal/settings`

### 4. N23: `HostID` on a reader store writes nothing

Evidence: `HostID` calls `ensureHome` and writes `host-id` with no `s.readOnly` check (`internal/settings/host-id.go:15-19`), against `internal/settings/README.md:33` ("every write gives `ErrReadOnlyStore`"). The other writers check first (`auth.go:239-241`).

Test first in `host-id_test.go`: a store from `NewAuthStoreReader` on an empty home returns `ErrReadOnlyStore` and creates no file and no folder.

Fix: `if s.readOnly { return "", ErrReadOnlyStore }` at the top.

Command: `go test -run 'HostID' ./internal/settings`

## Verification

- `go test -race ./internal/settings ./internal/auth`
- `go vet ./internal/settings ./internal/auth`

## Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| N23 breaks a caller that reads a host id through a reader store | Low | Medium | The one caller uses the writing store (`internal/auth/broker.go:332`). |

## Rollback

Revert the phase commits. No file format changes; a trimmed `baseUrl` is not written back.

## Docs impact

Phase 7: `docs/cli.md:550` (a trailing `/` is removed), `CHANGELOG.md` Fixed line.
