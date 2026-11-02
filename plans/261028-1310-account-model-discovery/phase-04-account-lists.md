---
phase: 4
title: "Account model lists"
status: completed
priority: P1
effort: "1.5d"
dependencies: [1, 3]
---

# Phase 4: Account model lists

<!-- Updated: Red Team 2026-10-28 - Method list function on the native client; GET-only transport; shared request builder; unknown for 401/403; identity key; paging bound; sanitize -->

## Goal

Give `internal/auth` one call that returns the model ids that a provider's current credential can use. The result has a state, `known` or `unknown`, and covers every sign-in method Ask has.

## Context

- Plan decisions P6, P10, P11, P12 and U5. Red-team findings 4, 10 and 15. Phase 1 endpoint report.
- Existing code:
  - `internal/auth/discovery.go:30-89`: `checkChatGPTAccess`, with a 1-min memory cache.
  - `internal/auth/keycheck.go:53-100`: `NativeKeyCheck` and `refusalCode`.
  - `internal/auth/http.go:20-42`: `boundedClient`, the 1 MB body limit, and `NativeMethods`.
  - `internal/auth/service.go:45-55,128-150,202-213`: `Service`, `Resolve`, the env key fallback, and `CheckAccess`.
  - `internal/settings/auth.go:44`: `Credential`.
- Accepted design: unknown access permits inference (Q4=A).

## Files

- Create `internal/auth/model_list.go` with:
  - a `ListModels func(ctx, credential) (AccountModels, error)` field on `Method`, built in `NativeMethods` and `app/auth_native.go` (API-key methods) so that it shares the native bounded client;
  - `func (s *Service) ListModels(ctx context.Context, provider string) (AccountModels, error)`, which resolves the credential and calls the method function;
  - `AccountModels{Key string; Provider, Method string; State AccessState; Items []AccountModel{ID, DisplayName}}`;
  - `AccountKey(provider, method, credential, salt) string`.
- Create `internal/auth/list_request.go`. It has one request builder and one status classifier per provider. `NativeKeyCheck` and `ListModels` both use them. The list transport allows only `GET`, one fixed host and path per provider, one auth header, and no redirect.
- Create `internal/auth/model_list_test.go`.
- Modify these files:
  - `internal/auth/keycheck.go`: use the shared builder. Its behavior does not change.
  - `internal/auth/discovery.go`: `checkChatGPTAccess` uses the shared ChatGPT parser and keeps its own 1-min memory cache.
  - `internal/auth/README.md`.

## Steps

1. Resolve the credential with the existing resolver, which refreshes an expired OAuth token first. With no credential, return `State: unknown` and send no request. An env key counts as a credential (`service.go:141-150`).
2. Requests, for each provider and method. The body limit is 1 MB per page.
   - **openai, `api-key`:** `GET https://api.openai.com/v1/models` with `Authorization: Bearer`. Items come from `data[].id`.
   - **openai, `openai-chatgpt`:** the same URL with the OAuth token. Keep `models[]` entries with `visibility == "list"`. Keep `slug` and `display_name`.
   - **anthropic, `api-key`:** `GET https://api.anthropic.com/v1/models?limit=1000` with `x-api-key` and `anthropic-version`. Follow the paging fields from phase 1. Read at most 20 pages, and stop when a cursor repeats.
   - **anthropic, `anthropic-oauth`:** the same request with `Authorization: Bearer` and the headers that phase 1 confirms. If phase 1 records "not used", the method has no list function, and the state is `unknown`.
   - **xai, `api-key` and `xai-oauth`:** `GET https://api.x.ai/v1/models` with `Authorization: Bearer`.
   - **alibaba-token-plan, `api-key`:** `GET <TokenPlanCompletionsURL>/models`, if phase 1 confirms it. Otherwise the state is `unknown`.
3. Status:
   - A 2xx answer with a parsed list is `known`.
   - Any other answer, including 401 and 403, is `unknown`. The exception is a method for which phase 1 proved that a list refusal means an inference refusal. Even then, the result is a transient `denied` that is never persisted.
   - Keep the error only for the log. Provider message text never goes to the user (`refusalCode` rule).
4. Items:
   - Drop an id or a name that fails the phase 2 id check: printable ASCII, no `@`, at most 128 bytes.
   - Keep the server order in the result. The display order is decided in phase 5.
5. Key:
   - The key is the provider, the method, and an identity part.
   - The identity part is the OAuth subject when the method has one. For an API key, it is the keyed hash from phase 3. Otherwise it is empty.
   - The key never holds a token, a key, or the credential generation.
6. Tests:
   - each provider and method;
   - paging, the page bound, and a repeated cursor;
   - a 401, a 403, a 500 and a timeout;
   - an empty `known` list;
   - the ChatGPT visibility filter;
   - an id with an escape sequence;
   - the transport refuses a POST, another host, and a redirect;
   - the existing ChatGPT access tests (`native_test.go`, `keycheck_test.go`, `access_commit_test.go`) still pass.

## Verification

- `go test ./internal/auth/...` passes.
- The lint command passes.

## Implementation notes (2026-10-28)

- `Service.ListModels(ctx, provider, salt []byte)` takes the cache salt, because the API-key part of the account key needs it. Phase 5 must read the salt inside a no-op `ModelCache.Update` (that also creates the file), fetch outside the lock, and write in a second `Update`. An empty `Key` means "do not cache".
- `internal/auth/service.go`: the OAuth refresh block of `Resolve` moved to `freshOAuth` with no change in behavior (the refresh slot is still held through `CheckAccess`). `Method.ListModels` was added there.
- `xai-oauth` has no subject, so its key has an empty identity part; login and logout delete the provider's entries (phase 5).
- With ChatGPT sign-in, `CheckAccess` keeps its own 1-min list cache. Phase 5 may seed it from the list result to save one request.
