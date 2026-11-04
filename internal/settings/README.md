# `internal/settings`

The files under `~/.ask/` (override with `ASK_HOME`) and the project `.ask/` folder. It owns three things. The first is `auth.json`, the credentials file: mode 0600, a file lock, and read-merge-write so that two processes do not lose each other's keys. The second is `settings.json`, the user settings, and the project `.ask/settings.json`, which loads only after the project is trusted. The third is the bytes of `trust.json`, the saved trust decisions. It also keeps `model-cache.json`, a cache of model metadata and account model lists. `config` may be imported only by `app` and `cmd/*`, so runtime settings need this separate package. Cloud mode keeps the same files on the server.

## What belongs here

- `auth.json` read, merge and write with a lock (`auth.go`, `lock.go`)
- User and project settings: load, merge, typed read and write (`settings.go`, `merge.go`, `project.go`)
- `trust.json` read and write with a lock (`trust.go`)
- `model-cache.json` read and write with a lock (`model_cache.go`)
- Path rules for `~/.ask` and `.ask` (`paths.go`)

## What does not belong here

| Code | Put it in |
|---|---|
| Start-up config of the process | `internal/config` |
| The trust decision rule, the project root and the gated files | [internal/workspace](../workspace/README.md) (this package receives the decision through `NewLayered`) |
| The composition of the decision and the settings load of a session | [internal/app/trust.go](../app/trust.go) |
| Encryption at rest | `internal/crypto` (parked) |
| Login, refresh, and account access checks | [internal/auth](../auth/README.md) |
| Sending a resolved credential to a model | [internal/providers](../providers/README.md) |

## Credential transaction

The credential transaction owner is [AuthStore](auth.go).
`NewAuthStore` selects `ASK_HOME` or `~/.ask` unless given an absolute home.
`Read` returns one provider record and the store-wide revision.
`Replace` and `Logout` compare that revision, so a logout of an absent provider still rejects an earlier login attempt.
Native sign-in and logout use `ReadProvider`, `ReplaceProvider`, and `LogoutProvider` to compare only that provider's revision.
This permits a pending sign-in to finish after another provider changes, while a removal of the same provider still rejects it.
The optional `providerRevisions` object records the last store revision, a migration baseline, and the last revision of each changed provider, including removed providers.
The existing global-revision API and schema version 1 stay the same.
An older writer keeps this unknown object but advances only the global revision.
When those revisions differ, the next provider transaction uses the global revision as its fence and starts new provider metadata after a successful write.
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

## Layered settings

[Layered](settings.go) is the settings of one session. `NewLayered(home, Project{Root, Trusted})` receives the canonical project root and the trust decision. `Load` returns `Effective`: the typed values with the defaults applied, `Sources` (each shipped key to `default`, `user` or `project`) and `Diagnostics` (layer, path, key and a fixed text, never file content). `UserLayer` reads the user file only; the trust decision uses it for `defaultProjectTrust`. A read takes no lock and creates no file or folder.

- The user file is `<home>/settings.json`. It must be an owner-only 0600 regular file in a 0700 home.
- The project file is `<root>/.ask/settings.json` ([project.go](project.go)). `Load` opens it only when `Trusted` is true. It must resolve to a regular file of at most 1 MiB; the open does not follow a link and does not block on a FIFO. A committed file can have mode 0644.
- A layer that cannot be read counts as empty and gives one diagnostic.

The project override rule ([merge.go](merge.go)): the project file overrides the user file. JSON objects merge key by key at each level. A project array or scalar replaces the user value. A project `null` is ignored. The keys that a project cannot set are dropped first, each with one diagnostic, and the rest of the file applies:

- user-only: `defaultProjectTrust`;
- deny list (the key and its whole subtree): `trust`, `httpProxy`, `env`, `shellPath`, `shellCommandPrefix`, `npmCommand`, `providers`, `hooks`, `cacheWarming`, `deviceId`.

The match ignores letter case. The typed read takes the exact key names from the merged map; a project key that differs from a shipped key only by case gives a diagnostic and is not read.

| Key | Values | Layers |
|---|---|---|
| `defaultModel` | `provider/id@api` | user, project |
| `thinkingFold` | `expanded` (default), `truncated`, `collapsed` | user, project |
| `defaultProjectTrust` | `ask` (default), `always`, `never` | user only |
| `defaultTools` | array of tool names (`["read","grep"]`), or of `+name`/`-name` entries over every built-in (`["-bash"]`); not both. `[]` selects no built-in tools. Unset is `Effective.DefaultTools == nil` | user, project |

A bad value of a known key gives the default and a diagnostic for the layer that set it. A bad `defaultTools` value (not an array of strings matching `^[+-]?[a-z][a-z0-9_]*$`, or plain names mixed with `+`/`-` entries) leaves it unset. The array replaces across layers, so a trusted project's `defaultTools` replaces the user's. Entry names are checked against the built-in tools by `tools.Selection.Resolve`, not here.

## Settings writes

`Update(ctx, scope, edit)` is the only writer. It reads the file of the scope fresh under a lock, applies `edit` to a copy and replaces the file atomically (0600 temporary file, fsync, rename, directory sync). Keys that the edit does not change keep their bytes. `Update` refuses, writes nothing and returns an `*UpdateError` (with a `Diagnostic`) when:

- the project write has no trusted project (`ErrUntrusted`); it then takes no lock and touches no file;
- the file cannot be read safely (`ErrUnsafeStore`); a corrupt file keeps its bytes;
- a project edit changes a key that a project cannot set (`ErrProjectKeyBarred`);
- a known key of the new fields has a bad value (`ErrInvalidSetting`), also when the edit did not change that key;
- a user edit changes a key that the trusted project file sets (`ErrShadowed`): the user value would not apply in this project.

The user file lock is `<home>/settings.json.lock`. The lock of a project file is a sidecar in the home, `<home>/project-settings-<first 16 hex digits of the SHA-256 of the root>.lock`, so no lock file appears in the project. The temporary file of a project write is in `<root>/.ask/` for the rename. A project write creates `<root>/.ask` with mode 0755 only when a write follows, and a new project file gets mode 0644.

## Default model

`Layered.SetDefaultModel` saves `defaultModel` in the user file for the leader when a client sets a model with `saveDefault` (the `/model` choice). It refuses with `ErrShadowed` when the trusted project file sets another `defaultModel`; the same id in both files is saved. It also refuses an unsafe file or an invalid known key of the user file. A refusal does not undo the switch: the answer says `defaultSaved: false` and gives the reason in `defaultNotSaved` (`shadowed`, `unsafe_file` or `failed`).

[SettingsStore](settings.go) is the settings of a session with no project layer. `SettingsStore.DefaultModel` returns the saved string as it is, so a caller can name a bad id in a fallback note.

## Thinking fold

`SettingsStore.ThinkingFold` reads the `thinkingFold` key of the user `settings.json`: the fold mode that a thinking step of the TUI takes when it ends.
The values are `"expanded"` (all lines, the default), `"truncated"` (the last lines) and `"collapsed"` (one row).
A missing file, a missing key, a value that is not a string, or an unknown word gives `"expanded"` and no error. A file that cannot be read gives `"expanded"` and the error.
The read takes the lock of the user file, so it creates the home (mode 0700) and `settings.json.lock` when they are missing.
The TUI entry ([cmd/tui/thinking_fold.go](../../cmd/tui/thinking_fold.go)) reads the key once at start, with a 500 ms bound, for the first frame. A trusted project can override the key: the session state then carries the effective value, and the TUI applies it. Nothing writes the key: the TUI and `ctrl+t` do not change the file.

## Trust store

[TrustStore](trust.go) keeps `<home>/trust.json`: one JSON object of canonical folder paths to `true` (trusted) or `false` (not trusted). A `null` value is the same as no entry. This package owns the bytes only; the decision rule and the path rules are in [internal/workspace](../workspace/README.md).

- `Read` takes no lock and creates nothing. A missing home or file is an empty store. A corrupt file, a file that is not an owner-only 0600 regular file, or an unsafe home gives `ErrTrustStoreCorrupt` (it wraps `ErrUnsafeStore`) with the path.
- `Update` takes `trust.json.lock`, reads fresh data, and applies the edit (a nil value removes the key). It refuses a corrupt or unsafe file and never overwrites it. Each key must be an absolute, clean path.

## Model cache

[ModelCache](model_cache.go) keeps `model-cache.json` in the home.
The file holds the last filtered models.dev metadata (the overlay with its ETag and provider filter) and the model id list of each account.
It never holds a token, a key, or a header.
An account key can hold an API key identity only as `KeyedHash`: HMAC-SHA256 with a random 32-byte salt of the home, first 16 bytes, in hex.
The store makes the salt once and an `Update` callback cannot change it.
`KeyedHash` returns an empty string for a salt that is not 32 bytes, for example the empty salt that `Read` returns for a missing file.
The caller builds the account keys.

`Read` does not take the lock, because each replace is atomic. It never creates the home.
A missing, corrupt, older-schema or newer-schema file gives empty data and a status, not an error.
A file or home with a bad owner, type or mode gives `ErrUnsafeStore`.

`Update` and `DeleteProvider` lock `model-cache.json.lock`, read fresh data, and write with the same 0600 temporary file, rename and directory sync as `auth.json`.
For each account entry and for the overlay, a stored value with a later `fetchedAt` stays, so a slow fetch cannot replace a newer result.
The next write replaces a corrupt file and discards a file of an older schema.
A file of a newer schema is read-only: `Update` returns `ErrModelCacheNewerSchema` and writes nothing.
`DeleteProvider` removes the account entries of one provider and keeps the overlay.

## File names

`auth.go`, `lock.go`, `lock_unix.go`, `settings.go`, `merge.go`, `project.go`, `trust.go`, `paths.go`, `host-id.go`, `model_cache.go`

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
