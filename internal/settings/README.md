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
| Using a credential to call a model | `internal/providers` (the caller passes a resolver function) |

## Main interfaces

- None required

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
