---
phase: 3
title: "Model cache store and metadata overlay"
status: completed
priority: P1
effort: "1d"
dependencies: [2]
---

# Phase 3: Model cache store and metadata overlay

<!-- Updated: Red Team 2026-10-28 - no fingerprint; schema guard; newer fetchedAt wins; stream decode; keyed hash -->

## Goal

Store model data in one cache file in `ASK_HOME`. Fetch the models.dev metadata at run time with ETag revalidation, so that a new id gets metadata without a new Ask build.

## Context

- Plan decisions P7, P11 and U4. Red-team findings 4, 12 and 15.
- omp reference: `provider-models/openai-compat.ts:193-260`. It shares one in-flight request, sends `If-None-Match`, and returns the last payload when a fetch fails.
- Storage pattern:
  - `internal/settings/auth.go:227` (atomic write);
  - `internal/settings/lock.go` (sidecar lock);
  - `internal/settings/README.md`;
  - `internal/settings/paths.go:69-86`.
- Rule: `internal/settings` imports the standard library only (`.golangci.yml:144-149`).

## Files

- Create `internal/settings/model_cache.go`. It is the `ModelCache` store for `<ASK_HOME>/model-cache.json`.
  - Contents:
    - `schema`, a version number;
    - `salt`, 32 random bytes, created once;
    - `overlay`: `{etag, providers, fetchedAt, payload}`. `providers` is the provider filter list, and `payload` is the raw filtered JSON;
    - `accounts`: a map from account key to `{provider, method, fetchedAt, retryAfter, state: known|unknown, items[{id, displayName}]}`.
  - Methods:
    - `Read`;
    - `Update(func(*ModelCacheData) error)`, run under the sidecar lock;
    - `DeleteProvider(provider)`.
  - Modes: 0600 for the file and 0700 for the directory.
- Create `internal/settings/model_cache_test.go`.
- Create `internal/providers/modelsdev_fetch.go` with this function:
  `FetchModelsDev(ctx, client *http.Client, etag string, providers []string) (payload []byte, newETag string, notModified bool, err error)`.
  - It decodes the body as a stream and keeps only the named providers and the fields the mapper uses.
  - It caps the raw body at 32 MB.
  - It never sends a credential.
- Create `internal/providers/modelsdev_fetch_test.go`. It uses an httptest server for: a 200, a 304, a 500, an oversize body, bad JSON, and a payload with extra providers (which it drops).
- Modify `internal/settings/README.md` to describe the new file in `ASK_HOME`.

## Steps

1. Write these tests first:
   - a round trip;
   - mode 0600;
   - two goroutines and two processes (a helper test binary) that `Update` at the same time: both changes stay;
   - for one entry, the newer `fetchedAt` wins;
   - a corrupt file reads as empty and is replaced at the next write;
   - a file with a newer `schema` is read-only: `Update` returns an error and does not write;
   - a file with an older `schema` is discarded at the next write.
2. ETag rule: send `If-None-Match` only when the stored `providers` list equals the requested list. A 304 returns `notModified`.
3. Account keys are built by the caller (phase 4). The store only saves them. The salt gives a keyed hash for API keys (HMAC-SHA256, first 16 bytes, hex).
4. Secret scan test: write entries built from test credentials. Then scan the file for each raw credential value and for the plain SHA-256 of each value. Fail on any match.

## Verification

- `go test ./internal/settings/... ./internal/providers/...` passes.
- The lint command passes (`settings` still imports the standard library only).
