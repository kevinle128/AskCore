---
title: "Secure credential store and contracts"
status: todo
---

# Secure credential store and contracts

## Context and baseline

Read the [main contract and proof matrix](./plan.md), [local scout](../reports/researcher-261006-0157-h7a-local-runtime-scout.md) and [Pi runtime proof](../reports/researcher-261006-0157-h7a-pi-runtime-proof.md).
The scout supplies baseline test counts and production function callers.
The [contract review](./reports/contract-review.md) supplies option copies/literals and test constructor consumers.
These are source counts, not executed-test claims.
Use the current working tree, including unfinished H4 work, rather than HEAD alone.
All source paths below are relative to `/Users/dale/orca/workspaces/AskCore/master-2`.
New paths are proposed; existing paths are verified in source or the linked scout.
Use existing Go testing, httptest and subprocess patterns; mock only external provider endpoints and JWKS.

## Overview

Priority: P1.
Deliver the secure file transaction and typed credential/runtime boundaries without full settings layering.
Dependencies: H2/H3 public provider contracts only; no H4 blocker.
This is a library checkpoint; phase 2 adds the resolver consumer and phase 3 proves real commands.

## Execution record — 2026-10-06

This phase passed its three criteria and was checked through the CLI.
Real store and process checks cover legacy and unknown-data preservation, absence revisions, lock coordination, pre-rename failure, post-rename uncertainty, and commit-budget failure.
Immutable snapshot redaction and import gates pass.
Full tests, vet, compilation, lint, and required races pass.
The store supports one authoritative local home with reliable OS locks and rename; it does not provide distributed replica coordination.
The [exact filesystem actor](../reports/tester-261006-0855-h7a-fsync-actor.md) passed its five-case matrix once with race instrumentation.
Its final repeated race run passed all ten cases in 38.186 seconds, and the reviewer closed the actor gate.
See the [full progress report](../reports/pm-261006-0823-h7a-progress.md), [live acceptance](../reports/tester-261006-0835-h7a-live-acceptance.md), and [final review](../reports/reviewer-261006-0843-h7a-final-review.md).

The source inventory and TDD steps below retain the accepted creation-time plan.
Use this execution record and the linked reports for implemented paths and executed checks.

## Requirements and architecture

Settings owns the persisted schema, owner path, stable host metadata, revision, sidecar lock and durable read/merge/write.
It uses standard library packages only and imports no Ask internal package.
Keep one tagged API-key or OAuth record per provider and preserve unrelated records and unknown JSON fields.
Legacy API-key records stay readable; reject ambiguous legacy OAuth unless method identity is explicit.
OAuth data binds method, tokens, actual expiry, scopes, refresh-not-before and method metadata as one record.
ChatGPT metadata holds issued client, verified subject/issuer binding and sign-in hint while that record exists.
Host identity is nonsecret runtime installation metadata, not a retained account registration.
Logout removes the entire provider credential/client mapping.
Use a store-wide revision in nonsecret file metadata under the same transaction so an absent-record logout still changes the revision.
A schema envelope must preserve Pi-style provider records and unknown data or have an explicit tested backward reader.
Reject unknown schema versions that cannot be safely merged.
A stable sidecar is locked with nonblocking OS-lock attempts and cancellable bounded waits under platform build tags.
Use a whole-file lock first; there is no measured need for per-provider coordination.
Read fresh, merge one provider, write a unique same-directory 0600 temp, sync/close, rename, then sync the owner directory.
Check every I/O result and reject symlinks, unsafe ownership and unexpected file types at the boundary.
Do not overwrite corrupt JSON as an empty store.
Do not invent crash recovery across a remote token server and disk.
Provider runtime auth is a separate immutable snapshot because persisted and inference material differ.
It contains provider/method/profile, source, account/generation, allowed endpoint, billing hint and access material only.
Protect String/log representations and exclude refresh/ID tokens, codes and store handles.

<!-- Updated: Review 2026-10-06 - durable rotation fence and option inventory. -->
## Accepted review contract

Add a settings-owned nonsecret refresh-attempt state to the existing provider record, bound to its committed generation and an attempt ID.
Before a rotating grant can leave the process, durably write `refresh_state=pending` under the same lock and advance the revision.
If the fence write or its durability confirmation fails, send no token request.
Only a validated replacement committed by that attempt clears the fence during refresh.
A successful explicit login or local logout can replace or remove the fenced record through the normal revision transaction.
A later resolver that sees an unresolved fence returns a classified reauthentication/recovery error; it must not resubmit the saved grant or fall back to a key.
Do not clear a fence from elapsed time, process death, a lease timeout or an unverified server response.
A crash before the request can cause conservative reauthentication; document this limit instead of claiming remote/disk atomicity.
The fence is one record field, not a separate journal, background worker or recovery framework.
Test process death after server rotation, lost response, failed replacement and restart through a second real prompt with zero extra token requests.
Keep the marker durable before network I/O, so a failed replacement can leave safe pending bytes even when the old token remains.
Use the [complete contract inventory](./reports/contract-review.md) for direct option consumers, 73 current literals, constructors and copy boundaries.
The scout inventories production function callers only.
New runtime auth and named-choice values must be immutable values or copied at the listed option/record boundaries; no shared mutable map or slice is allowed.
Redact auth material when faux records, diagnostics or test artifacts are rendered.

## Runtime Flow

Actor: the real auth service, later called by login or a model request.
Nearest entry: planned exported settings credential read/transaction boundary.
Path: owner path validation → sidecar lock → fresh JSON/revision read → compare/merge → durable replace.
Prepared state: temp home with other providers/unknown fields, two OS processes and injected syscall failure barriers.
Observable result: complete JSON, preserved records, revision conflict or classified storage result.
Tests use real filesystem and OS locks; internal transaction code is not mocked.
Phase 3 adds command-to-save/logout and phase 2 adds prompt-to-refresh consumer proofs.

## Related Code Files

| Action | File | Rough size | Test impact |
| --- | --- | --- | --- |
| New | `internal/settings/auth.go`, `auth_test.go` | Large. | Schema, merge, revision and failure boundaries. |
| New | `internal/settings/paths.go`, `paths_test.go` | Small. | ASK_HOME, owner, unsafe paths and modes. |
| New | `internal/settings/lock.go`, `lock_unix.go`, `lock_test.go` | Medium. | Unix subprocess lock/exit/cancel behavior. |
| New | `internal/providers/auth.go`, `auth_test.go` | Medium. | Typed snapshot and binding invariants. |
| Existing, modify | `internal/providers/types.go` | Small. | Optional typed auth retains APIKey compatibility. |
| Existing, modify | `.golangci.yml` | Small. | Settings internal-import ban; providers auth/settings ban. |
| Existing, modify | `internal/settings/README.md` | Small. | Owning schema and durability contract. |

No deletion, database migration or generated-file edit is planned.

## Protected functions and consumers

Preserve StreamFn and Provider.Stream signatures in `internal/providers/types.go`.
Preserve NewStream, Stream.Result and Assembler settlement in `stream.go`/`assembler.go`.
StreamOptions consumers include agent LoopConfig/loop_stream, headless setup, anthropic producer and openai Completions/Responses producers.
The complete StreamOptions literal/copy and test-caller inventory is in the contract review; the local scout supplies production function callers.
New auth fields are optional for existing callers; they must not turn every APIKey string into OAuth.

## Test scenario matrix

| Priority | Scenario | Required observation |
| --- | --- | --- |
| Critical | Two writers and reader during rename. | No lost provider or partial JSON. |
| Critical | Login begins absent, logout commits absent. | Revision rejects late activation. |
| Critical | Write, temp sync/close or rename fails. | Prior bytes survive; no false success. |
| Critical | Directory sync fails after rename. | Indeterminate result, replacement may be visible. |
| Critical | Corrupt JSON, symlink, unsafe owner or file type. | Fail closed without overwrite. |
| High | API-key legacy and unknown fields. | Readability and round-trip preservation. |
| High | Process dies while holding sidecar lock. | Another process can acquire OS lock. |
| High | Canceled waiter and bounded contention. | No late acquisition or orphan process. |
| Medium | New home and existing mode repair/validation. | Correct 0700/0600 and explicit permission failures. |

## Tests Before

1. Write failing public settings-boundary tests for the matrix with isolated homes and subprocess barriers.
2. Add compatibility assertions to provider auth tests for unchanged key-only StreamOptions and redacted snapshot formatting.
3. Record the current provider stream suites as the protected baseline; do not claim they passed before running them.

## Refactor

1. Define schema, revisions, classified pre-commit/indeterminate errors and private I/O seams for deterministic faults.
2. Implement the stdlib-only store and platform lock with constructor path/clock/deadline injection.
3. Add immutable provider snapshot/binding types and optional StreamOptions field.
4. Enforce the import bans and document the storage contract only after behavior is verified.

## Tests After

1. Pass each failing transaction test and run a continuous reader with two independent writers.
2. Exercise process exit, absent ABA, unknown-schema refusal and post-rename failure separately.
3. Confirm persisted credentials never enter runtime snapshot serialization or diagnostics.

## Regression gate

Run `go test ./internal/settings ./internal/providers` first.
Then run `go test -race ./internal/settings ./internal/providers` and required depguard/lint.
Keep a deterministic lock/rotation barrier harness for phases 2 and 6.

## Success Criteria

- [x] Store and optional runtime contracts preserve legacy key callers and unknown data.
- [x] Two-process coordination, absence revisions and every durable failure boundary pass.
- [x] Secrets are redacted and imports are acyclic.

## Risk Assessment and rollback

A post-rename error cannot promise old bytes; return indeterminate and stop activation.
If an existing credential schema cannot be safely merged, stop that write and add an explicit compatibility reader before continuing.
Local locks and rename require one authoritative reliable filesystem; do not claim distributed replica safety.
Rollback removes registrations/runtime use first and preserves files and unknown fields.
