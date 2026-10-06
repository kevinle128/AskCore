# `internal/settings`

The files under `~/.ask/` (override with `ASK_HOME`) and the project `.ask/` folder. It owns two things. The first is `auth.json`, the credentials file: mode 0600, a file lock, and read-merge-write so that two processes do not lose each other's keys. The second is `settings.json`, the user settings, and the project `.ask/settings.json`, which loads only after the project is trusted. `config` may be imported only by `app` and `cmd/*`, so runtime settings need this separate package. Cloud mode keeps the same files on the server.

## What belongs here

- `auth.json` read, merge and write with a lock (`auth.go`, `lock.go`)
- User and project settings: load, merge, typed read and write (`settings.go`, `merge.go`)
- Path rules for `~/.ask` and `.ask` (`paths.go`)

## What does not belong here

| Code | Put it in |
|---|---|
| Start-up config of the process | `internal/config` |
| The trust decision for a project | `internal/workspace` (this package receives it through the constructor) |
| Encryption at rest | `internal/crypto` (parked) |
| Login, refresh, and account access checks | [internal/auth](../auth/README.md) |
| Sending a resolved credential to a model | [internal/providers](../providers/README.md) |

## Credential transaction

The credential transaction owner is [AuthStore](auth.go).
`NewAuthStore` selects `ASK_HOME` or `~/.ask` unless given an absolute home.
`Read` returns one provider record and the store-wide revision.
`Replace` and `Logout` compare that revision, so a logout of an absent provider still rejects an earlier login attempt.
The store keeps unknown JSON fields and reads Pi-style root provider records with API keys.
An OAuth record needs an explicit method tag.

Each transaction locks `auth.json.lock`, reads fresh data, writes a 0600 temporary file, syncs it, renames it, and syncs the 0700 owner directory.
A failure before rename keeps the old file.
A directory-sync failure returns `ErrIndeterminate`, because the replacement may be visible.
`FenceRefresh` first stores a pending attempt for the current generation; only the matching `CommitRefresh`, an explicit login, or logout can clear it.
`Refresh` holds the sidecar lock while it stores the fence, runs one bounded exchange, and commits the validated replacement.
If exchange fails or replacement commit fails before rename, the pending fence stays in the file.
A failure after rename has an indeterminate outcome.
A crash before the token request can therefore require a new sign-in.
Do not clear a pending fence by hand or retry an uncertain rotating grant.
Recover through explicit login or local logout on the inference host.
See the [operating commands](../../README.md#native-auth-and-headless-prompts).

Refresh permits a 15-second exchange and a separate five-second replacement commit that does not inherit caller cancellation.
The [app shutdown wait](../app/module_auth.go) stops new auth work and permits a 20-second drain.
These budgets cannot interrupt a blocked OS sync call, and a forced kill can interrupt any operation.
The durable fence prevents automatic grant reuse after an uncertain rotation.
A network rotation and a disk commit cannot form one atomic transaction.
This store assumes one authoritative local filesystem and does not coordinate distributed replicas.

## File names

`auth.go`, `lock.go`, `settings.go`, `merge.go`, `paths.go`

## Imports

- Allowed: standard library
- Denied: all other `AskCore/internal/*` packages; `internal/config` (typed defaults arrive through the constructor)

## Rules

- One model is shared everywhere. Add a DTO or a separate type with a mapper only when the data is really different (design section 6).
- Receive dependencies and typed config through constructors. `internal/app` wires them with fx.
- Create an interface only when there is a second implementation or a test seam.
- A settings file write is read-merge-write under the lock. Never write a whole file from a stale copy.
- Credentials are never logged.

Design reference: [docs/ask-architecture-reference.md](../../docs/ask-architecture-reference.md)
